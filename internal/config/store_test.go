package config

import (
	"path/filepath"
	"testing"
)

func TestConfigureLocalAndMerge(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")

	// Simulate global
	global := defaultRaw()
	global.Supervisor.Model = "gpt-4.1"
	global.Cursor.Model = "composer-2.5-fast"
	if err := Save(globalPath, global); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GOOLOOP_GLOBAL_CONFIG", globalPath)

	_, err := Configure(ConfigureOptions{
		ProjectRoot: dir,
		Objective:   "Build a todo app",
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Overrides{ProjectRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Goal != "Build a todo app" {
		t.Fatalf("goal: %q", cfg.Goal)
	}
	if cfg.SupervisorModel != "gpt-4.1" {
		t.Fatalf("supervisor model from global: %q", cfg.SupervisorModel)
	}
	if cfg.CursorModel != "composer-2.5-fast" {
		t.Fatalf("cursor model from global: %q", cfg.CursorModel)
	}
	if cfg.CheckpointPath != filepath.Join(dir, ".goloop", "checkpoint.md") {
		t.Fatalf("checkpoint: %s", cfg.CheckpointPath)
	}
}

func TestLocalConfigPath(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, ".goloop", "config.yaml")
	if got := LocalConfigPath(dir); got != want {
		t.Fatalf("got %s", got)
	}
}
