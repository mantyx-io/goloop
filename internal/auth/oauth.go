package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	authURL           = "https://auth.openai.com/oauth/authorize"
	redirectURI       = "http://localhost:1455/auth/callback"
	oauthScopes       = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	originator        = "codex_cli_rs"
	deviceAuthBase    = "https://auth.openai.com/api/accounts"
	deviceVerifyURL   = "https://auth.openai.com/codex/device"
	deviceRedirectURI = "https://auth.openai.com/deviceauth/callback"
)

// ErrOAuthPortInUse means localhost:1455 is held by another process (Codex, Cursor, etc.).
var ErrOAuthPortInUse = errors.New("oauth callback port 1455 is in use")

func OAuthPortHint() string {
	return "Port 1455 is used for browser OAuth. Quit Codex/Cursor using that port, or use the default device-code login: goloop login"
}

// ChatGPTLogin is the default CLI OAuth flow (device code — no localhost callback).
func ChatGPTLogin(ctx context.Context, authPath string) error {
	return DeviceLogin(ctx, authPath)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func BrowserLogin(ctx context.Context, authPath string) error {
	pkce, err := newPKCE()
	if err != nil {
		return err
	}
	state, err := randomURLSafe(16)
	if err != nil {
		return err
	}

	authLink := fmt.Sprintf(
		"%s?response_type=code&client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&id_token_add_organizations=true&codex_cli_simplified_flow=true&state=%s&originator=%s",
		authURL,
		ChatGPTOAuthClientID,
		url.QueryEscape(redirectURI),
		url.QueryEscape(oauthScopes),
		pkce.Challenge,
		state,
		url.QueryEscape(originator),
	)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("oauth error: %s", errMsg)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		gotState := r.URL.Query().Get("state")
		if gotState == "" || gotState != state {
			errCh <- fmt.Errorf("oauth state mismatch")
			http.Error(w, "Invalid state — restart login from goloop", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("oauth callback missing code")
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("<html><body><h2>Login successful</h2><p>You can close this tab.</p><script>window.close()</script></body></html>"))
		codeCh <- code
	})

	listener, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		return fmt.Errorf("%w: %v. %s", ErrOAuthPortInUse, err, OAuthPortHint())
	}

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	fmt.Fprintln(os.Stderr, "Opening browser for ChatGPT login…")
	fmt.Fprintln(os.Stderr, "If the browser does not open, visit:")
	fmt.Fprintln(os.Stderr, authLink)
	_ = openBrowser(authLink)

	var code string
	select {
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return ctx.Err()
	case err := <-errCh:
		_ = server.Shutdown(context.Background())
		return err
	case code = <-codeCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)

	tokens, err := exchangeAuthCode(ctx, code, pkce.Verifier, redirectURI)
	if err != nil {
		return err
	}
	return persistTokens(authPath, tokens)
}

func DeviceLogin(ctx context.Context, authPath string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	reqBody, _ := json.Marshal(map[string]string{"client_id": ChatGPTOAuthClientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceAuthBase+"/deviceauth/usercode", strings.NewReader(string(reqBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, string(body))
	}

	var dc struct {
		UserCode     string  `json:"user_code"`
		DeviceAuthID string  `json:"device_auth_id"`
		Interval     flexInt `json:"interval"`
	}
	if err := json.Unmarshal(body, &dc); err != nil {
		return fmt.Errorf("parse device code response: %w", err)
	}
	interval := int(dc.Interval)
	if interval <= 0 {
		interval = 5
	}

	printDeviceCodePrompt(deviceVerifyURL, dc.UserCode)

	deadline := time.Now().Add(15 * time.Minute)
	poll := time.Duration(interval) * time.Second

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("device login timed out after 15 minutes")
		}

		pollBody, _ := json.Marshal(map[string]string{
			"device_auth_id": dc.DeviceAuthID,
			"user_code":      dc.UserCode,
		})
		pollReq, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceAuthBase+"/deviceauth/token", strings.NewReader(string(pollBody)))
		if err != nil {
			return err
		}
		pollReq.Header.Set("Content-Type", "application/json")

		pollResp, err := client.Do(pollReq)
		if err != nil {
			return err
		}
		pollData, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		if pollResp.StatusCode == http.StatusOK {
			var success struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.Unmarshal(pollData, &success); err != nil {
				return err
			}
			tokens, err := exchangeAuthCode(ctx, success.AuthorizationCode, success.CodeVerifier, deviceRedirectURI)
			if err != nil {
				return err
			}
			return persistTokens(authPath, tokens)
		}

		if pollResp.StatusCode == http.StatusForbidden || pollResp.StatusCode == http.StatusNotFound {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
			continue
		}

		return fmt.Errorf("device auth failed (%d): %s", pollResp.StatusCode, string(pollData))
	}
}

func Refresh(ctx context.Context, creds *Credentials) error {
	if creds == nil || creds.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	body := fmt.Sprintf(`{"client_id":"%s","grant_type":"refresh_token","refresh_token":"%s"}`, ChatGPTOAuthClientID, creds.RefreshToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(data))
	}

	var tokens tokenResponse
	if err := json.Unmarshal(data, &tokens); err != nil {
		return err
	}
	return persistTokens(creds.Path, tokens)
}

func EnsureFresh(ctx context.Context, creds *Credentials) error {
	if creds.Mode != "chatgpt" {
		return nil
	}
	if creds.ExpiresAt.IsZero() || time.Until(creds.ExpiresAt) > 5*time.Minute {
		return nil
	}
	if err := Refresh(ctx, creds); err != nil {
		return err
	}
	updated, err := Load(creds.Path)
	if err != nil {
		return err
	}
	*creds = *updated
	return nil
}

func exchangeAuthCode(ctx context.Context, code, verifier, redirect string) (tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", ChatGPTOAuthClientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirect)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return tokenResponse{}, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(data))
	}

	var tokens tokenResponse
	if err := json.Unmarshal(data, &tokens); err != nil {
		return tokenResponse{}, err
	}
	return tokens, nil
}

func persistTokens(authPath string, tokens tokenResponse) error {
	accountID := jwtClaim(tokens.IDToken, "https://api.openai.com/auth", "chatgpt_account_id")
	if accountID == "" {
		accountID = jwtClaim(tokens.AccessToken, "https://api.openai.com/auth", "chatgpt_account_id")
	}
	return SaveChatGPT(authPath, tokens.AccessToken, tokens.RefreshToken, tokens.IDToken, accountID)
}

type pkcePair struct {
	Verifier  string
	Challenge string
}

// flexInt unmarshals JSON numbers sent as int, float, or string (OpenAI APIs vary).
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexInt(n)
		return nil
	}
	var fl float64
	if err := json.Unmarshal(b, &fl); err == nil {
		*f = flexInt(fl)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("flexInt: invalid string %q", s)
		}
		*f = flexInt(n)
		return nil
	}
	return fmt.Errorf("flexInt: cannot unmarshal %s", string(b))
}

func newPKCE() (pkcePair, error) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		return pkcePair{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	return pkcePair{Verifier: verifier, Challenge: challenge}, nil
}

func randomURLSafe(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		if err := exec.Command("xdg-open", url).Start(); err != nil {
			return exec.Command("wslview", url).Start()
		}
		return nil
	}
}

func PortAvailable() bool {
	ln, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
