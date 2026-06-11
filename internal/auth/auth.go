package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ChatGPTOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	TokenURL             = "https://auth.openai.com/oauth/token"
)

type Credentials struct {
	Path              string
	Mode              string // "chatgpt" or "api_key"
	APIKey            string
	AnthropicAPIKey   string
	AccessToken  string
	RefreshToken string
	IDToken      string
	AccountID    string
	FedRAMP      bool
	ExpiresAt    time.Time
}

type authFile struct {
	AuthMode      string          `json:"auth_mode,omitempty"`
	OpenAIAPIKey     string          `json:"OPENAI_API_KEY,omitempty"`
	AnthropicAPIKey  string          `json:"ANTHROPIC_API_KEY,omitempty"`
	Tokens           *tokenBlob      `json:"tokens,omitempty"`
	LastRefresh      string          `json:"last_refresh,omitempty"`
	OpenAIAPIKey2    string          `json:"openai_api_key,omitempty"`
	Raw           json.RawMessage `json:"-"`
}

type tokenBlob struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	AccountID    string `json:"account_id"`
}

func DefaultAuthPath() string {
	return filepath.Join(homeDir(), ".goloop", "auth.json")
}

func CodexAuthPath() string {
	return filepath.Join(homeDir(), ".codex", "auth.json")
}

func ResolveAuthPath(configured string) string {
	if configured != "" {
		return ExpandHome(configured)
	}
	if v := os.Getenv("GOOLOOP_AUTH_PATH"); v != "" {
		return ExpandHome(v)
	}
	return DefaultAuthPath()
}

func ResolveAuthPathForRead(configured string) string {
	primary := ResolveAuthPath(configured)
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	codex := CodexAuthPath()
	if _, err := os.Stat(codex); err == nil {
		return codex
	}
	return primary
}

func ExpandHome(path string) string {
	if path == "" {
		return path
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir(), path[2:])
	}
	return path
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}

func Load(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth file not found (%s): run `goloop login` or set OPENAI_API_KEY", path)
	}

	var raw authFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse auth file: %w", err)
	}

	openAIKey := raw.OpenAIAPIKey
	if openAIKey == "" {
		openAIKey = raw.OpenAIAPIKey2
	}
	if raw.Tokens == nil {
		if openAIKey == "" && raw.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("auth file has no ChatGPT tokens or API keys")
		}
		return &Credentials{
			Path:            path,
			Mode:            "api_key",
			APIKey:          openAIKey,
			AnthropicAPIKey: raw.AnthropicAPIKey,
		}, nil
	}

	tokens := raw.Tokens
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return nil, fmt.Errorf("ChatGPT OAuth tokens incomplete; run `goloop login`")
	}

	accountID := tokens.AccountID
	if accountID == "" {
		accountID = jwtClaim(tokens.IDToken, "https://api.openai.com/auth", "chatgpt_account_id")
	}
	if accountID == "" {
		accountID = jwtClaim(tokens.AccessToken, "https://api.openai.com/auth", "chatgpt_account_id")
	}
	if accountID == "" {
		return nil, fmt.Errorf("ChatGPT account id missing; run `goloop login`")
	}

	fedramp := jwtBool(tokens.IDToken, "https://api.openai.com/auth", "chatgpt_account_is_fedramp")

	return &Credentials{
		Path:            path,
		Mode:            "chatgpt",
		APIKey:          openAIKey,
		AnthropicAPIKey: raw.AnthropicAPIKey,
		AccessToken:     tokens.AccessToken,
		RefreshToken:    tokens.RefreshToken,
		IDToken:         tokens.IDToken,
		AccountID:       accountID,
		FedRAMP:         fedramp,
		ExpiresAt:       jwtExpiry(tokens.AccessToken),
	}, nil
}

func IsAvailable(path string) bool {
	_, err := Load(path)
	return err == nil
}

func SaveChatGPT(path string, access, refresh, id, accountID string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload := authFile{
		AuthMode: "chatgpt",
		Tokens: &tokenBlob{
			AccessToken:  access,
			RefreshToken: refresh,
			IDToken:      id,
			AccountID:    accountID,
		},
		LastRefresh: time.Now().UTC().Format(time.RFC3339),
	}
	return writeAuth(path, payload)
}

func SaveAPIKey(path, apiKey string) error {
	payload, err := loadAuthFile(path)
	if err != nil {
		return err
	}
	payload.AuthMode = "api_key"
	payload.OpenAIAPIKey = apiKey
	return writeAuth(path, payload)
}

func SaveAnthropicAPIKey(path, apiKey string) error {
	payload, err := loadAuthFile(path)
	if err != nil {
		return err
	}
	if payload.AuthMode == "" {
		payload.AuthMode = "api_key"
	}
	payload.AnthropicAPIKey = apiKey
	return writeAuth(path, payload)
}

func loadAuthFile(path string) (authFile, error) {
	var payload authFile
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return payload, nil
		}
		return payload, err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

func writeAuth(path string, payload authFile) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func jwtExpiry(token string) time.Time {
	exp := jwtIntClaim(token, "exp")
	if exp == 0 {
		return time.Time{}
	}
	return time.Unix(exp, 0).UTC()
}

func jwtIntClaim(token, key string) int64 {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return 0
	}
	claims := decodeJWTPart(parts[1])
	if v, ok := claims[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		}
	}
	return 0
}

func jwtClaim(token, namespace, key string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	claims := decodeJWTPart(parts[1])
	ns, ok := claims[namespace].(map[string]any)
	if !ok {
		return ""
	}
	if v, ok := ns[key].(string); ok {
		return v
	}
	return ""
}

func jwtBool(token, namespace, key string) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	claims := decodeJWTPart(parts[1])
	ns, ok := claims[namespace].(map[string]any)
	if !ok {
		return false
	}
	v, ok := ns[key].(bool)
	return ok && v
}

func decodeJWTPart(part string) map[string]any {
	padded := part + strings.Repeat("=", (4-len(part)%4)%4)
	data, err := base64.RawURLEncoding.DecodeString(padded)
	if err != nil {
		data, _ = base64.URLEncoding.DecodeString(padded)
	}
	var claims map[string]any
	_ = json.Unmarshal(data, &claims)
	if claims == nil {
		claims = map[string]any{}
	}
	return claims
}

func PlanLabel(creds *Credentials) string {
	if creds == nil || creds.Mode != "chatgpt" {
		return ""
	}
	plan := jwtClaim(creds.IDToken, "https://api.openai.com/auth", "chatgpt_plan_type")
	if plan == "" {
		plan = jwtClaim(creds.AccessToken, "https://api.openai.com/auth", "chatgpt_plan_type")
	}
	return plan
}
