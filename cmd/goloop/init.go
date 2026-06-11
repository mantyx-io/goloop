package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/configureui"
)

func runInit(args []string) int {
	fs := flag.NewFlagSet("goloop init", flag.ExitOnError)
	configPath := fs.String("config", "", "Config file path (default: .goloop/config.yaml)")
	goal := fs.String("goal", "", "Project goal / objective")
	outputDir := fs.String("output-dir", "", "Where the worker writes code (default: . — project root)")
	iters := fs.Int("iters", 0, "Max loop iterations per run")
	interactive := fs.Bool("interactive", true, "Prompt stdin when supervisor asks questions")
	noInteractive := fs.Bool("no-interactive", false, "Disable interactive mode")
	yes := fs.Bool("yes", false, "Skip TUI; apply flags only (non-TTY)")
	plain := fs.Bool("plain", false, "Use plain prompts instead of TUI (non-TTY fallback)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goloop init [directory] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Initialize a project for goloop (.goloop/config.yaml).\n")
		fmt.Fprintf(os.Stderr, "Models and auth come from ~/.goloop/config.yaml (run: goloop configure).\n\n")
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

	interactiveVal := *interactive
	if *noInteractive {
		interactiveVal = false
	}

	initial := config.InitOptions{
		ProjectRoot:   absRoot,
		ConfigPath:    *configPath,
		Objective:     *goal,
		OutputDir:     *outputDir,
		MaxIterations: *iters,
		Interactive:   &interactiveVal,
	}

	useTUI := !*yes && !*plain && isTerminal(os.Stdin) && isTerminal(os.Stdout)

	path, err := initProject(initial, useTUI)
	if err != nil {
		if err.Error() == "cancelled" {
			return 130
		}
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		return 1
	}

	fmt.Printf("Initialized %s\n", absRoot)
	fmt.Printf("Wrote %s\n", path)
	fmt.Println("Next: goloop run .")
	return 0
}

// initProject runs the init wizard (or applies defaults for non-TTY) and writes
// the project config. It is shared by `goloop init` and the auto-init path in
// `goloop run`. Returns the written config path.
func initProject(initial config.InitOptions, useTUI bool) (string, error) {
	final := initial
	if useTUI {
		resolved, err := configureui.RunInit(configureui.InitOptions{
			ProjectRoot: initial.ProjectRoot,
			ConfigPath:  initial.ConfigPath,
			Initial:     initial,
		})
		if err != nil {
			return "", err
		}
		final = resolved
	} else {
		if final.Objective == "" {
			snap, _ := config.LoadMergedSnapshot(initial.ProjectRoot)
			if snap != nil {
				final.Objective = snap.Objective
				if final.Objective == "" {
					final.Objective = snap.Goal
				}
			}
		}
		if final.OutputDir == "" {
			final.OutputDir = "."
		}
		if final.MaxIterations == 0 {
			final.MaxIterations = 50
		}
	}

	return config.Init(final)
}
