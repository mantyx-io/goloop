package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/llm"
	"github.com/mantyx-io/goloop/internal/supervisor"
)

// runDoctor checks that goloop is actually ready to run: config layers load,
// supervisor auth is present, and the worker CLI exists on PATH. With --call
// it also performs a real supervisor model call as a smoke test.
func runDoctor(args []string) int {
	fs := flag.NewFlagSet("goloop doctor", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config YAML (overrides default layer)")
	call := fs.Bool("call", false, "Make a real supervisor model call (uses tokens)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goloop doctor [directory] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Check install, config, auth, and worker readiness.\n\n")
		fs.PrintDefaults()
	}

	targetDir, flagArgs := splitTargetAndFlags(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if targetDir == "" {
		targetDir = "."
	}
	absRoot, err := filepath.Abs(targetDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid directory: %v\n", err)
		return 1
	}

	failed := 0
	pass := func(label, detail string) { printCheck("✓", label, detail) }
	warn := func(label, detail string) { printCheck("!", label, detail) }
	fail := func(label, detail string) { printCheck("✗", label, detail); failed++ }

	// Global config layer.
	if config.GlobalConfigExists() {
		pass("Global config", config.GlobalConfigPath())
	} else {
		fail("Global config", "not found — run: goloop configure")
	}

	// Project layer (informational: doctor works in uninitialized directories).
	if config.IsInitialized(absRoot) {
		pass("Project config", config.LocalConfigPath(absRoot))
	} else {
		warn("Project config", "not initialized — run: goloop init (or pass --goal to goloop run)")
	}

	// Merged config. A placeholder goal keeps Load happy in uninitialized dirs;
	// the goal itself is already reported above.
	cfg, err := config.Load(config.Overrides{
		ConfigPath:  *configPath,
		ProjectRoot: absRoot,
		Goal:        "(doctor)",
	})
	if err != nil {
		fail("Config load", err.Error())
		return doctorSummary(failed)
	}
	pass("Config load", strings.Join(cfg.ConfigSources, " + "))

	// Supervisor auth.
	if msg := supervisorNotReady(cfg); msg == "" {
		pass("Supervisor auth", cfg.SupervisorLabel())
	} else {
		fail("Supervisor auth", msg)
	}

	// Worker binary.
	bin := cfg.CursorBinary
	if cfg.WorkerBackend == config.WorkerClaudeCode {
		bin = cfg.ClaudeCodeBinary
	}
	if binPath, err := exec.LookPath(bin); err != nil {
		fail("Worker binary", fmt.Sprintf("%q not found on PATH (worker: %s)", bin, cfg.WorkerBackend))
	} else if version := binaryVersion(binPath); version != "" {
		pass("Worker binary", fmt.Sprintf("%s (%s)", binPath, version))
	} else {
		pass("Worker binary", binPath)
	}

	// Optional live smoke test: proves auth + model + JSON output end to end.
	if *call {
		if err := supervisorSmokeTest(cfg); err != nil {
			fail("Supervisor call", err.Error())
		} else {
			pass("Supervisor call", cfg.SupervisorLabel()+" returned valid JSON")
		}
	} else {
		warn("Supervisor call", "skipped — run `goloop doctor --call` for a live smoke test")
	}

	return doctorSummary(failed)
}

func printCheck(mark, label, detail string) {
	fmt.Printf("  %s %-16s %s\n", mark, label, detail)
}

func doctorSummary(failed int) int {
	fmt.Println()
	if failed > 0 {
		fmt.Printf("%d check(s) failed.\n", failed)
		return 1
	}
	fmt.Println("All checks passed.")
	return 0
}

// binaryVersion returns the worker CLI's version string, or "" if it cannot
// be determined quickly. Purely informational.
func binaryVersion(binPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	version := strings.TrimSpace(string(out))
	if line, _, found := strings.Cut(version, "\n"); found {
		version = line
	}
	if len(version) > 60 {
		version = version[:60]
	}
	return version
}

func supervisorSmokeTest(cfg *config.Config) error {
	client, err := supervisor.New(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err = client.ChatJSON(ctx, []llm.Message{
		{Role: "system", Content: "You are a connectivity check. Respond with a single JSON object only."},
		{Role: "user", Content: `Reply with exactly {"ok": true}`},
	})
	return err
}
