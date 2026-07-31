package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultAnthropicVersion = "2023-06-01"

type AnthropicClient struct {
	Model       string
	APIKey      string
	BaseURL     string
	Temperature float64
	HTTPClient  *http.Client

	usage Usage
}

func (c *AnthropicClient) TotalUsage() Usage { return c.usage }

func NewAnthropic(model, apiKey, baseURL string, temperature float64) (*AnthropicClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Anthropic API key not set (export ANTHROPIC_API_KEY or set supervisor.api_key_env)")
	}
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &AnthropicClient{
		Model:       model,
		APIKey:      apiKey,
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Temperature: temperature,
		HTTPClient:  &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (c *AnthropicClient) ChatJSON(ctx context.Context, messages []Message) (map[string]any, error) {
	var system string
	var apiMessages []map[string]string
	for _, m := range messages {
		switch m.Role {
		case "system":
			system = mergePrompts(system, m.Content)
		default:
			role := m.Role
			if role != "user" && role != "assistant" {
				role = "user"
			}
			apiMessages = append(apiMessages, map[string]string{"role": role, "content": m.Content})
		}
	}
	if len(apiMessages) == 0 {
		return nil, fmt.Errorf("no messages for Anthropic")
	}

	payload := map[string]any{
		"model":      c.Model,
		"max_tokens": 8192,
		"messages":   apiMessages,
		"system":     system + "\n\nRespond with a single JSON object only.",
	}
	if c.Temperature > 0 {
		payload["temperature"] = c.Temperature
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", defaultAnthropicVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, parseAnthropicError(resp.StatusCode, respBody)
	}

	var data struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, err
	}
	c.usage.Add(Usage{Input: data.Usage.InputTokens, Output: data.Usage.OutputTokens})
	var textParts []string
	for _, block := range data.Content {
		if block.Type == "text" && block.Text != "" {
			textParts = append(textParts, block.Text)
		}
	}
	if len(textParts) == 0 {
		return nil, fmt.Errorf("Anthropic returned no text content")
	}
	return ParseJSONObject(strings.Join(textParts, "\n"))
}

func parseAnthropicError(status int, body []byte) error {
	message := string(body)
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		message = parsed.Error.Message
	}
	return fmt.Errorf("Anthropic API error %d: %s", status, message)
}

func mergePrompts(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}
