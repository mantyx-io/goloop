package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/mantyx-io/goloop/internal/auth"
)

const chatGPTBaseURL = "https://chatgpt.com/backend-api/codex"

type ChatGPTClient struct {
	Model      string
	AuthPath   string
	HTTPClient *http.Client
	creds      *auth.Credentials
}

func NewChatGPT(model, authPath string) (*ChatGPTClient, error) {
	path := auth.ResolveAuthPathForRead(authPath)
	creds, err := auth.Load(path)
	if err != nil {
		return nil, err
	}
	if creds.Mode != "chatgpt" {
		return nil, fmt.Errorf("auth file %s is not ChatGPT OAuth (run `goloop login`)", path)
	}
	if model == "" {
		model = "gpt-4.1"
	}
	return &ChatGPTClient{
		Model:      model,
		AuthPath:   path,
		HTTPClient: &http.Client{Timeout: 10 * time.Minute},
		creds:      creds,
	}, nil
}

func (c *ChatGPTClient) ChatJSON(ctx context.Context, messages []Message) (map[string]any, error) {
	if err := auth.EnsureFresh(ctx, c.creds); err != nil {
		return nil, err
	}

	instructions, input, err := splitInstructionsInput(messages)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"model":               c.Model,
		"instructions":        instructions,
		"input":               input,
		"tools":               []any{},
		"tool_choice":         "auto",
		"parallel_tool_calls": false,
		"stream":              true,
		"store":               false,
		"include":             []any{},
		"text":                map[string]any{"format": map[string]string{"type": "json_object"}},
	}

	text, err := c.postResponses(ctx, payload)
	if err != nil {
		return nil, err
	}
	return ParseJSONObject(text)
}

func (c *ChatGPTClient) postResponses(ctx context.Context, payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatGPTBaseURL+"/responses", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		for k, v := range c.headers() {
			req.Header.Set(k, v)
		}
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return "", err
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			if err := auth.Refresh(ctx, c.creds); err != nil {
				return "", fmt.Errorf("chatgpt auth expired: %w (run `goloop login`)", err)
			}
			updated, err := auth.Load(c.AuthPath)
			if err != nil {
				return "", err
			}
			c.creds = updated
			continue
		}

		if resp.StatusCode >= 400 {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return "", fmt.Errorf("ChatGPT API error %d: %s", resp.StatusCode, string(data))
		}

		text, err := readSSEText(resp.Body)
		resp.Body.Close()
		return text, err
	}
	return "", fmt.Errorf("ChatGPT request failed after token refresh")
}

func (c *ChatGPTClient) headers() map[string]string {
	headers := map[string]string{
		"Authorization":      "Bearer " + c.creds.AccessToken,
		"ChatGPT-Account-Id": c.creds.AccountID,
		"Content-Type":       "application/json",
		"originator":         "codex_cli_rs",
		"User-Agent":         codexUserAgent(),
	}
	if c.creds.FedRAMP {
		headers["X-OpenAI-Fedramp"] = "true"
	}
	return headers
}

func codexUserAgent() string {
	return fmt.Sprintf("codex_cli_rs/0.125.0 (%s %s; %s) goloop",
		runtime.GOOS, runtime.GOARCH, runtime.Version())
}

func splitInstructionsInput(messages []Message) (string, []map[string]any, error) {
	var instructions []string
	var input []map[string]any

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			instructions = append(instructions, msg.Content)
		case "user", "assistant":
			typ := "input_text"
			if msg.Role == "assistant" {
				typ = "output_text"
			}
			input = append(input, map[string]any{
				"type": "message",
				"role": msg.Role,
				"content": []map[string]any{
					{"type": typ, "text": msg.Content},
				},
			})
		}
	}

	if len(instructions) == 0 {
		return "", nil, fmt.Errorf("ChatGPT request requires system instructions")
	}
	return strings.Join(instructions, "\n\n"), input, nil
}

func readSSEText(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var textParts []string
	var finalOutput []map[string]any
	sawDelta := false

	var block []string
	flush := func() error {
		if len(block) == 0 {
			return nil
		}
		event, err := decodeSSEBlock(block)
		block = nil
		if err != nil {
			return err
		}
		if event == nil {
			return nil
		}

		switch event["type"] {
		case "response.output_text.delta":
			if delta, ok := event["delta"].(string); ok && delta != "" {
				sawDelta = true
				textParts = append(textParts, delta)
			}
		case "response.output_item.done":
			if item, ok := event["item"].(map[string]any); ok {
				finalOutput = append(finalOutput, item)
			}
		case "response.failed", "response.incomplete":
			return fmt.Errorf("chatgpt response %s", event["type"])
		case "response.completed":
			if response, ok := event["response"].(map[string]any); ok {
				if output, ok := response["output"].([]any); ok {
					for _, raw := range output {
						if item, ok := raw.(map[string]any); ok {
							finalOutput = append(finalOutput, item)
						}
					}
				}
			}
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return "", err
			}
			continue
		}
		block = append(block, line)
	}
	if err := flush(); err != nil {
		return "", err
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	if sawDelta {
		return strings.Join(textParts, ""), nil
	}
	return textFromResponseItems(finalOutput), nil
}

func decodeSSEBlock(lines []string) (map[string]any, error) {
	var dataLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) == 0 {
		return nil, nil
	}
	joined := strings.Join(dataLines, "\n")
	if joined == "[DONE]" {
		return nil, nil
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(joined), &event); err != nil {
		return nil, fmt.Errorf("invalid SSE JSON: %w", err)
	}
	return event, nil
}

func textFromResponseItems(items []map[string]any) string {
	var parts []string
	for _, item := range items {
		itemType, _ := item["type"].(string)
		if itemType == "output_text" || itemType == "text" {
			if text, ok := item["text"].(string); ok {
				parts = append(parts, text)
			}
			continue
		}
		if itemType != "message" {
			continue
		}
		content, ok := item["content"].([]any)
		if !ok {
			continue
		}
		for _, raw := range content {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			pt, _ := part["type"].(string)
			if pt != "output_text" && pt != "text" {
				continue
			}
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}
