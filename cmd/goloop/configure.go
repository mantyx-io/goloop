package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/configureui"
)

func runConfigure(args []string) int {
	fs := flag.NewFlagSet("goloop configure", flag.ExitOnError)
	global := fs.Bool("global", false, "Write global defaults to ~/.goloop/config.yaml")
	configPath := fs.String("config", "", "Config file path (default: global or .goloop/config.yaml)")
	objective := fs.String("objective", "", "Project objective / goal")
	supervisorBackend := fs.String("supervisor-backend", "", "Supervisor: chatgpt, openai, or anthropic")
	supervisorModel := fs.String("supervisor-model", "", "Supervisor model name")
	workerBackend := fs.String("worker-backend", "", "Worker backend (cursor)")
	cursorModel := fs.String("cursor-model", "", "Cursor agent model")
	claudeModel := fs.String("claude-code-model", "", "Claude Code model")
	iters := fs.Int("iters", 0, "Default max loop iterations")
	nonInteractive := fs.Bool("non-interactive", false, "Disable human prompts during loop runs")
	yes := fs.Bool("yes", false, "Skip TUI; apply flags only (non-TTY)")
	plain := fs.Bool("plain", false, "Use plain prompts instead of TUI (non-TTY fallback)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goloop configure [directory] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "  goloop configure          Global defaults (~/.goloop/config.yaml)\n")
		fmt.Fprintf(os.Stderr, "  goloop configure .        Project objective (.goloop/config.yaml)\n\n")
		fs.PrintDefaults()
	}

	targetDir, flagArgs := splitTargetAndFlags(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	globalMode := *global || targetDir == ""
	var absRoot string
	if !globalMode {
		if targetDir == "" {
			targetDir = "."
		}
		var err error
		absRoot, err = filepath.Abs(targetDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid directory: %v\n", err)
			return 1
		}
	}

	initial := config.ConfigureOptions{
		Global:            globalMode,
		ConfigPath:        *configPath,
		ProjectRoot:       absRoot,
		Objective:         *objective,
		SupervisorBackend: *supervisorBackend,
		SupervisorModel:   *supervisorModel,
		WorkerBackend:     *workerBackend,
		CursorModel:       *cursorModel,
		ClaudeCodeModel:   *claudeModel,
		MaxIterations:     *iters,
	}
	if *nonInteractive {
		v := false
		initial.Interactive = &v
	}

	useTUI := !*yes && !*plain && isTerminal(os.Stdin) && isTerminal(os.Stdout)

	var final config.ConfigureOptions
	var err error
	if useTUI {
		final, err = configureui.Run(configureui.Options{
			Global:      globalMode,
			ProjectRoot: absRoot,
			ConfigPath:  *configPath,
			Initial:     initial,
		})
		if err != nil {
			if err.Error() == "cancelled" {
				return 130
			}
			fmt.Fprintf(os.Stderr, "configure error: %v\n", err)
			return 1
		}
	} else {
		final = initial
		if !globalMode && final.Objective == "" {
			snap, _ := config.LoadMergedSnapshot(absRoot)
			if snap != nil {
				final.Objective = snap.Objective
				if final.Objective == "" {
					final.Objective = snap.Goal
				}
			}
		}
	}

	path, err := config.Configure(final)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configure error: %v\n", err)
		return 1
	}

	fmt.Printf("Wrote %s\n", path)
	if globalMode {
		fmt.Println("Next: cd your-project && goloop init")
	} else {
		fmt.Println("Next: goloop run .  (or: goloop init for full project setup)")
	}
	return 0
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
