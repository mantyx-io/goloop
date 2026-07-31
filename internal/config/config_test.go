package config

import (
	"path/filepath"
	"testing"
)

func TestLoadPathOverrides(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "global.yaml")

	global := defaultRaw()
	global.Supervisor.Model = "gpt-4.1"
	if err := Save(globalPath, global); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOLOOP_GLOBAL_CONFIG", globalPath)

	_, err := Configure(ConfigureOptions{
		ProjectRoot: dir,
		Objective:   "Default objective",
	})
	if err != nil {
		t.Fatal(err)
	}

	ckpt := filepath.Join(dir, ".goloop", "goals", "todo-cli", "checkpoint.md")
	uctx := filepath.Join(dir, ".goloop", "goals", "todo-cli", "user_context.md")
	out := filepath.Join(dir, ".goloop", "goals", "todo-cli", "output")

	cfg, err := Load(Overrides{
		ProjectRoot:     dir,
		Goal:            "# Todo CLI\n\nBuild it.",
		GoalSlug:        "todo-cli",
		CheckpointPath:  ckpt,
		UserContextPath: uctx,
		OutputDir:       out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GoalSlug != "todo-cli" {
		t.Fatalf("goal slug: %q", cfg.GoalSlug)
	}
	if cfg.CheckpointPath != ckpt {
		t.Fatalf("checkpoint: %s", cfg.CheckpointPath)
	}
	if cfg.UserContextPath != uctx {
		t.Fatalf("user context: %s", cfg.UserContextPath)
	}
	if cfg.OutputDir != out {
		t.Fatalf("output: %s", cfg.OutputDir)
	}
}
