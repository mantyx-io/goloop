package models

import (
	"strings"
	"testing"
)

func TestParseCursorListModels(t *testing.T) {
	input := `Available models

auto - Auto (default)
gpt-5.6-sol-high - GPT-5.6 Sol 1M High
gpt-5.6-sol-high-fast - GPT-5.6 Sol High Fast
composer-2.5 - Composer 2.5
composer-2.5 - Composer 2.5

Tip: use --model <id> (or /model <id> in interactive mode) to switch.
`
	items := parseCursorListModels(input)
	if len(items) != 4 {
		t.Fatalf("got %d models, want 4: %#v", len(items), items)
	}
	if items[0].ID != "auto" || items[0].Description != "Default" {
		t.Fatalf("auto entry: %+v", items[0])
	}
	if items[1].ID != "gpt-5.6-sol-high" || items[1].DisplayName != "GPT-5.6 Sol 1M High" {
		t.Fatalf("sol entry: %+v", items[1])
	}
	if items[1].Provider != "cursor" {
		t.Fatalf("provider: %q", items[1].Provider)
	}
}

func TestParseCursorListModelsRejectsBadLines(t *testing.T) {
	input := strings.Join([]string{
		"not a model line",
		"has spaces - bad id",
		"ok-id - Good Name",
		"",
	}, "\n")
	items := parseCursorListModels(input)
	if len(items) != 1 || items[0].ID != "ok-id" {
		t.Fatalf("got %#v", items)
	}
}

func TestSplitCursorModelLine(t *testing.T) {
	id, name, ok := splitCursorModelLine("gpt-5.6-sol-high - GPT-5.6 Sol 1M High")
	if !ok || id != "gpt-5.6-sol-high" || name != "GPT-5.6 Sol 1M High" {
		t.Fatalf("got %q %q %v", id, name, ok)
	}
	if _, _, ok := splitCursorModelLine("no-separator"); ok {
		t.Fatal("expected failure")
	}
}
