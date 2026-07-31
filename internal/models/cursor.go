package models

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// FetchCursor lists models from the Cursor agent CLI (`cursor agent --list-models`
// or `agent --list-models`). On failure it returns the static fallback list plus an error.
func FetchCursor(ctx context.Context, binary string) ([]Info, error) {
	if binary == "" {
		binary = "cursor"
	}
	if ctx == nil {
		return defaultCursorWorkerModels(), fmt.Errorf("nil context")
	}

	out, err := runCursorListModels(ctx, binary)
	if err != nil {
		return defaultCursorWorkerModels(), err
	}
	items := parseCursorListModels(string(out))
	if len(items) == 0 {
		return defaultCursorWorkerModels(), fmt.Errorf("no models parsed from cursor CLI")
	}
	return items, nil
}

// CursorWorkerModels is a convenience wrapper that fetches live models or falls back.
func CursorWorkerModels() []Info {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	items, _ := FetchCursor(ctx, "cursor")
	return items
}

// DefaultCursorWorkerModels returns the static fallback list (no CLI call).
func DefaultCursorWorkerModels() []Info {
	return defaultCursorWorkerModels()
}

func runCursorListModels(ctx context.Context, binary string) ([]byte, error) {
	// Preferred: `cursor agent --list-models` (matches how the worker is invoked).
	if out, err := execCommand(ctx, binary, "agent", "--list-models"); err == nil {
		return out, nil
	}
	// `agent --list-models` when binary is the standalone agent CLI.
	if out, err := execCommand(ctx, binary, "--list-models"); err == nil {
		return out, nil
	}
	if binary != "agent" {
		if out, err := execCommand(ctx, "agent", "--list-models"); err == nil {
			return out, nil
		}
	}
	return nil, fmt.Errorf("cursor model list failed (tried %q agent --list-models)", binary)
}

func execCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return stdout.Bytes(), nil
}

// parseCursorListModels parses `agent --list-models` text output:
//
//	Available models
//
//	auto - Auto (default)
//	gpt-5.6-sol-high - GPT-5.6 Sol 1M High
//	...
//	Tip: use --model <id> ...
func parseCursorListModels(output string) []Info {
	var out []Info
	seen := map[string]struct{}{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if lower == "available models" || strings.HasPrefix(lower, "tip:") {
			continue
		}
		id, name, ok := splitCursorModelLine(line)
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		desc := ""
		if strings.Contains(strings.ToLower(name), "default") {
			desc = "Default"
		}
		out = append(out, Info{
			ID:          id,
			DisplayName: name,
			Provider:    "cursor",
			Description: desc,
		})
	}
	return out
}

func splitCursorModelLine(line string) (id, name string, ok bool) {
	// Format: "<id> - <display name>"
	idx := strings.Index(line, " - ")
	if idx <= 0 {
		return "", "", false
	}
	id = strings.TrimSpace(line[:idx])
	name = strings.TrimSpace(line[idx+3:])
	if id == "" || name == "" {
		return "", "", false
	}
	// IDs are slug-like (no spaces). Reject malformed lines.
	if strings.ContainsAny(id, " \t") {
		return "", "", false
	}
	return id, name, true
}

func defaultCursorWorkerModels() []Info {
	return []Info{
		{ID: "composer-2.5-fast", DisplayName: "Composer 2.5 Fast", Provider: "cursor", Description: "Default — fast Cursor agent"},
		{ID: "composer-2.5", DisplayName: "Composer 2.5", Provider: "cursor", Description: "Balanced Cursor agent"},
		{ID: "gpt-5.6-sol-high", DisplayName: "GPT-5.6 Sol High", Provider: "cursor"},
		{ID: "gpt-5.6-sol-high-fast", DisplayName: "GPT-5.6 Sol High Fast", Provider: "cursor"},
		{ID: "gpt-5.6-terra-medium", DisplayName: "GPT-5.6 Terra", Provider: "cursor"},
		{ID: "gpt-5.5-medium", DisplayName: "GPT-5.5 Medium", Provider: "cursor"},
		{ID: "gpt-5.3-codex-high-fast", DisplayName: "GPT-5.3 Codex High Fast", Provider: "cursor"},
		{ID: "claude-4.6-sonnet-medium-thinking", DisplayName: "Claude 4.6 Sonnet (thinking)", Provider: "cursor"},
		{ID: "claude-opus-4-8-thinking-high", DisplayName: "Claude Opus 4.8 (thinking)", Provider: "cursor"},
	}
}
