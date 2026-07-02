package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type Message struct {
	Role    string
	Content string
}

type Client interface {
	ChatJSON(ctx context.Context, messages []Message) (map[string]any, error)
}

// Usage counts supervisor tokens consumed through the API.
type Usage struct {
	Input  int
	Output int
}

func (u Usage) Total() int { return u.Input + u.Output }

func (u *Usage) Add(other Usage) {
	u.Input += other.Input
	u.Output += other.Output
}

// UsageTracker is implemented by clients that can report cumulative token
// usage for the process lifetime.
type UsageTracker interface {
	TotalUsage() Usage
}

var jsonObjectRE = regexp.MustCompile(`\{[\s\S]*\}`)

func ParseJSONObject(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed != nil {
		return parsed, nil
	}

	match := jsonObjectRE.FindString(text)
	if match == "" {
		return nil, fmt.Errorf("model did not return valid JSON: %s", truncate(text, 500))
	}
	if err := json.Unmarshal([]byte(match), &parsed); err != nil {
		return nil, fmt.Errorf("parse JSON object: %w", err)
	}
	return parsed, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
