package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.md")

	ckpt := New(path, "Build something great")
	ckpt.Completed = []string{"step one"}
	ckpt.InProgress = []string{"step two"}
	ckpt.Blockers = []string{"waiting on API"}
	ckpt.NextSteps = []string{"step three"}
	ckpt.Phase = "build"
	ckpt.AppendHistory(Entry{
		Iteration: 1,
		Action:    "delegate",
		Summary:   "Started work",
		Status:    "partial",
	})
	ckpt.AppendHistory(Entry{
		Iteration: 2,
		Action:    "evaluate",
		Summary:   "Reviewed progress\nacross two lines",
		Status:    "success",
		Notes:     "exit=0\nall checks passed",
	})

	if err := ckpt.Save(); err != nil {
		t.Fatal(err)
	}

	loaded := New(path, "ignored")
	if err := loaded.Read(); err != nil {
		t.Fatal(err)
	}

	if loaded.Phase != "build" {
		t.Fatalf("phase: got %q", loaded.Phase)
	}
	if len(loaded.Completed) != 1 || loaded.Completed[0] != "step one" {
		t.Fatalf("completed: %#v", loaded.Completed)
	}
	if len(loaded.Blockers) != 1 {
		t.Fatalf("blockers: %#v", loaded.Blockers)
	}
	if len(loaded.History) != 2 {
		t.Fatalf("history: %#v", loaded.History)
	}
	first, second := loaded.History[0], loaded.History[1]
	if first.Iteration != 1 || first.Action != "delegate" || first.Status != "partial" || first.Summary != "Started work" {
		t.Fatalf("history[0]: %#v", first)
	}
	if second.Iteration != 2 || second.Action != "evaluate" || second.Status != "success" {
		t.Fatalf("history[1]: %#v", second)
	}
	if second.Summary != "Reviewed progress\nacross two lines" {
		t.Fatalf("history[1] summary: %q", second.Summary)
	}
	if second.Notes != "exit=0\nall checks passed" {
		t.Fatalf("history[1] notes: %q", second.Notes)
	}
	if loaded.Iteration != 2 {
		t.Fatalf("iteration: %d", loaded.Iteration)
	}

	// Saving again after a restart must not drop previously logged iterations.
	loaded.AppendHistory(Entry{Iteration: 3, Action: "delegate", Summary: "More work", Status: "partial"})
	if err := loaded.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded := New(path, "ignored")
	if err := reloaded.Read(); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.History) != 3 {
		t.Fatalf("history after restart: %#v", reloaded.History)
	}
}

func TestWriteInitial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checkpoint.md")

	ckpt := New(path, "Test goal")
	if err := ckpt.WriteInitial(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
