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
