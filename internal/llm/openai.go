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

type OpenAIClient struct {
	Model       string
	APIKey      string
	BaseURL     string
	Temperature float64
	HTTPClient  *http.Client

	usage Usage
}

func (c *OpenAIClient) TotalUsage() Usage { return c.usage }

func NewOpenAI(model, apiKey, baseURL string, temperature float64) (*OpenAIClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API key not set (export OPENAI_API_KEY or set supervisor.api_key_env)")
	}
	return &OpenAIClient{
		Model:       model,
		APIKey:      apiKey,
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Temperature: temperature,
		HTTPClient:  &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (c *OpenAIClient) ChatJSON(ctx context.Context, messages []Message) (map[string]any, error) {
	payload := map[string]any{
		"model":    c.Model,
		"messages": toAPIMessages(messages),
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	if !isFixedTemperatureModel(c.Model) {
		payload["temperature"] = c.Temperature
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
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
		return nil, parseAPIError(resp.StatusCode, respBody)
	}

	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, err
	}
	c.usage.Add(Usage{Input: data.Usage.PromptTokens, Output: data.Usage.CompletionTokens})
	if len(data.Choices) == 0 {
		return nil, fmt.Errorf("OpenAI returned no choices")
	}
	return ParseJSONObject(data.Choices[0].Message.Content)
}

func toAPIMessages(messages []Message) []map[string]string {
	out := make([]map[string]string, len(messages))
	for i, m := range messages {
		out[i] = map[string]string{"role": m.Role, "content": m.Content}
	}
	return out
}

func isFixedTemperatureModel(model string) bool {
	name := strings.ToLower(model)
	return strings.HasPrefix(name, "gpt-5") ||
		strings.HasPrefix(name, "o1") ||
		strings.HasPrefix(name, "o3") ||
		strings.HasPrefix(name, "o4")
}

func parseAPIError(status int, body []byte) error {
	message := string(body)
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		message = parsed.Error.Message
		if parsed.Error.Param != "" {
			message += " (param: " + parsed.Error.Param + ")"
		}
	}
	return fmt.Errorf("OpenAI API error %d: %s", status, message)
}
