package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/display"
)

type loopFlags struct {
	configPath        string
	iters             int
	maxIterations     int
	prompt            string
	promptFile        string
	doReset           bool
	dryRun            bool
	plain             bool
	noInteractive     bool
	verbose           bool
	supervisorBackend string
	supervisorModel   string
	workerBackend     string
	cursorModel       string
	claudeModel       string
}

func registerLoopFlags(fs *flag.FlagSet) *loopFlags {
	f := &loopFlags{}
	fs.StringVar(&f.configPath, "config", "", "Path to config YAML (overrides default layer)")
	fs.IntVar(&f.iters, "iters", 0, "Max loop iterations (overrides config)")
	fs.IntVar(&f.maxIterations, "max-iterations", 0, "Alias for --iters")
	fs.StringVar(&f.prompt, "prompt", "", "Additional instructions for this run")
	fs.StringVar(&f.promptFile, "prompt-file", "", "Read additional instructions from a file")
	fs.BoolVar(&f.doReset, "reset", false, "Reset loop state before starting")
	fs.BoolVar(&f.dryRun, "dry-run", false, "Validate config without calling models")
	fs.BoolVar(&f.plain, "plain", false, "Disable rich UI")
	fs.BoolVar(&f.noInteractive, "no-interactive", false, "Never prompt stdin (ask_user → blocker)")
	fs.BoolVar(&f.verbose, "verbose", false, "Debug logging")
	fs.StringVar(&f.supervisorBackend, "supervisor-backend", "", "Override supervisor backend (chatgpt, openai, anthropic)")
	fs.StringVar(&f.supervisorModel, "supervisor-model", "", "Override supervisor model")
	fs.StringVar(&f.workerBackend, "worker-backend", "", "Override worker backend (cursor, claude_code)")
	fs.StringVar(&f.cursorModel, "cursor-model", "", "Override Cursor worker model")
	fs.StringVar(&f.claudeModel, "claude-code-model", "", "Override Claude Code worker model")
	return f
}

func (f *loopFlags) maxIterPtr() *int {
	iterCount := f.iters
	if iterCount == 0 {
		iterCount = f.maxIterations
	}
	if iterCount <= 0 {
		return nil
	}
	return &iterCount
}

func (f *loopFlags) configOverrides(absRoot, goal string) config.Overrides {
	return config.Overrides{
		ConfigPath:        f.configPath,
		ProjectRoot:       absRoot,
		Goal:              goal,
		MaxIterations:     f.maxIterPtr(),
		Prompt:            f.prompt,
		PromptFile:        f.promptFile,
		NoInteractive:     f.noInteractive,
		SupervisorBackend: f.supervisorBackend,
		SupervisorModel:   f.supervisorModel,
		WorkerBackend:     f.workerBackend,
		CursorModel:       f.cursorModel,
		ClaudeCodeModel:   f.claudeModel,
	}
}

func executeLoop(cfg *config.Config, disp *display.Display, lf *loopFlags) int {
	if lf.doReset {
		removed, err := resetState(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reset error: %v\n", err)
			return 1
		}
		disp.Info("State reset — fresh checkpoint")
		if len(removed) > 0 {
			disp.Info(fmt.Sprintf("Cleared output (%d items)", len(removed)))
		}
	}

	if lf.dryRun {
		printDryRun(disp, cfg)
		return 0
	}

	if msg := supervisorNotReady(cfg); msg != "" {
		fmt.Fprintln(os.Stderr, "Supervisor not configured:")
		fmt.Fprintln(os.Stderr, "  "+msg)
		return 1
	}

	return runOrchestrator(cfg, disp, lf.maxIterPtr())
}
