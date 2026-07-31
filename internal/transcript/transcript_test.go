package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoggerWritesJSONL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	l := New(dir)
	if l == nil {
		t.Fatal("expected logger")
	}
	l.Log("run_start", 0, map[string]any{"goal": "test"})
	l.Log("plan", 3, map[string]any{"action": "delegate"})
	l.Close()

	f, err := os.Open(l.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var lines []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("invalid JSONL line: %v", err)
		}
		lines = append(lines, entry)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0]["event"] != "run_start" || lines[0]["goal"] != "test" {
		t.Fatalf("line 0: %#v", lines[0])
	}
	if lines[1]["iteration"] != float64(3) {
		t.Fatalf("line 1: %#v", lines[1])
	}
	if _, ok := lines[0]["iteration"]; ok {
		t.Fatal("iteration 0 should be omitted")
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var l *Logger
	l.Log("event", 1, nil)
	l.Close()
	if l.Path() != "" {
		t.Fatal("nil path should be empty")
	}
}
