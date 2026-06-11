package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mantyx-io/goloop/internal/auth"
)

const chatGPTBase = "https://chatgpt.com/backend-api/codex"

type Info struct {
	ID          string
	DisplayName string
	Provider    string
	Description string
}

type FetchParams struct {
	Backend  string // chatgpt, openai, anthropic
	APIKey   string
	AuthPath string
}

func Fetch(ctx context.Context, p FetchParams) ([]Info, error) {
	switch strings.ToLower(p.Backend) {
	case "chatgpt":
		return fetchChatGPT(ctx, p.AuthPath)
	case "openai":
		key := p.APIKey
		if key == "" {
			key = os.Getenv("OPENAI_API_KEY")
		}
		if key == "" {
			if creds, err := auth.Load(auth.ResolveAuthPathForRead(p.AuthPath)); err == nil && creds.Mode == "api_key" {
				key = creds.APIKey
			}
		}
		if key == "" {
			return defaultOpenAI(), nil
		}
		return fetchOpenAI(ctx, key, "https://api.openai.com/v1")
	case "anthropic":
		key := p.APIKey
		if key == "" {
			key = os.Getenv("ANTHROPIC_API_KEY")
		}
		if key == "" {
			if creds, err := auth.Load(auth.ResolveAuthPathForRead(p.AuthPath)); err == nil {
				key = creds.AnthropicAPIKey
			}
		}
		if key == "" {
			return defaultAnthropic(), nil
		}
		return fetchAnthropic(ctx, key)
	default:
		return nil, fmt.Errorf("unknown backend: %s", p.Backend)
	}
}

func ClaudeCodeWorkerModels() []Info {
	return []Info{
		{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", Provider: "claude_code", Description: "Default — balanced"},
		{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", Provider: "claude_code", Description: "Most capable"},
		{ID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", Provider: "claude_code", Description: "Fast"},
	}
}

func CursorWorkerModels() []Info {
	return []Info{
		{ID: "composer-2.5-fast", DisplayName: "Composer 2.5 Fast", Provider: "cursor", Description: "Default — fast Cursor agent"},
		{ID: "composer-2.5", DisplayName: "Composer 2.5", Provider: "cursor", Description: "Balanced Cursor agent"},
		{ID: "gpt-5.3-codex-high-fast", DisplayName: "GPT-5.3 Codex High Fast", Provider: "cursor"},
		{ID: "claude-4.6-sonnet-medium-thinking", DisplayName: "Claude 4.6 Sonnet (thinking)", Provider: "cursor"},
		{ID: "claude-opus-4-8-thinking-high", DisplayName: "Claude Opus 4.8 (thinking)", Provider: "cursor"},
		{ID: "gpt-5.5-medium", DisplayName: "GPT-5.5 Medium", Provider: "cursor"},
	}
}

func Filter(items []Info, query string) []Info {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return items
	}
	var out []Info
	for _, m := range items {
		hay := strings.ToLower(m.ID + " " + m.DisplayName + " " + m.Description)
		if strings.Contains(hay, query) {
			out = append(out, m)
		}
	}
	return out
}

func fetchChatGPT(ctx context.Context, authPath string) ([]Info, error) {
	creds, err := auth.Load(auth.ResolveAuthPathForRead(authPath))
	if err != nil || creds.Mode != "chatgpt" {
		return defaultChatGPT(), err
	}
	if err := auth.EnsureFresh(ctx, creds); err != nil {
		return defaultChatGPT(), err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := chatGPTBase + "/models?client_version=0.125.0"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return defaultChatGPT(), err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("ChatGPT-Account-Id", creds.AccountID)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("User-Agent", "codex_cli_rs/0.125.0 goloop")

	resp, err := client.Do(req)
	if err != nil {
		return defaultChatGPT(), err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return defaultChatGPT(), fmt.Errorf("chatgpt models %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var parsed struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return defaultChatGPT(), err
	}

	var out []Info
	for _, m := range parsed.Models {
		if m.Slug == "" {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		out = append(out, Info{ID: m.Slug, DisplayName: name, Provider: "chatgpt"})
	}
	if len(out) == 0 {
		return defaultChatGPT(), nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func fetchOpenAI(ctx context.Context, apiKey, baseURL string) ([]Info, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return defaultOpenAI(), err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return defaultOpenAI(), err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return defaultOpenAI(), fmt.Errorf("openai models %d", resp.StatusCode)
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return defaultOpenAI(), err
	}

	seen := map[string]struct{}{}
	var out []Info
	for _, m := range parsed.Data {
		if m.ID == "" || strings.Contains(m.ID, "embedding") || strings.Contains(m.ID, "tts") || strings.Contains(m.ID, "whisper") {
			continue
		}
		if _, ok := seen[m.ID]; ok {
			continue
		}
		seen[m.ID] = struct{}{}
		out = append(out, Info{ID: m.ID, DisplayName: m.ID, Provider: "openai"})
	}
	if len(out) == 0 {
		return defaultOpenAI(), nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func fetchAnthropic(ctx context.Context, apiKey string) ([]Info, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return defaultAnthropic(), err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return defaultAnthropic(), err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return defaultAnthropic(), fmt.Errorf("anthropic models %d", resp.StatusCode)
	}

	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return defaultAnthropic(), err
	}

	var out []Info
	for _, m := range parsed.Data {
		if m.ID == "" {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		out = append(out, Info{ID: m.ID, DisplayName: name, Provider: "anthropic"})
	}
	if len(out) == 0 {
		return defaultAnthropic(), nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func defaultChatGPT() []Info {
	return []Info{
		{ID: "gpt-4.1", DisplayName: "GPT-4.1", Provider: "chatgpt"},
		{ID: "gpt-5.5", DisplayName: "GPT-5.5", Provider: "chatgpt"},
		{ID: "o3", DisplayName: "o3", Provider: "chatgpt"},
		{ID: "o4-mini", DisplayName: "o4-mini", Provider: "chatgpt"},
	}
}

func defaultOpenAI() []Info {
	return defaultChatGPT()
}

func defaultAnthropic() []Info {
	return []Info{
		{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", Provider: "anthropic"},
		{ID: "claude-opus-4-6", DisplayName: "Claude Opus 4.6", Provider: "anthropic"},
		{ID: "claude-haiku-4-5", DisplayName: "Claude Haiku 4.5", Provider: "anthropic"},
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
