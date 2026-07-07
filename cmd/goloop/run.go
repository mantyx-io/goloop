package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mantyx-io/goloop/internal/auth"
	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/display"
	"github.com/mantyx-io/goloop/internal/orchestrator"
	"github.com/mantyx-io/goloop/internal/reset"
)

// autoInit launches the project init wizard when `goloop run` is invoked in an
// uninitialized directory, then lets the run continue automatically.
func autoInit(absRoot, configPath string) int {
	if !(isTerminal(os.Stdin) && isTerminal(os.Stdout)) {
		fmt.Fprintf(os.Stderr, "No goloop config in %s.\n", absRoot)
		fmt.Fprintln(os.Stderr, "Run `goloop init` (interactive) first, or pass --goal \"...\".")
		return 1
	}
	fmt.Fprintf(os.Stderr, "No goloop config in %s — let's set it up.\n\n", absRoot)
	interactiveVal := true
	path, err := initProject(config.InitOptions{
		ProjectRoot: absRoot,
		ConfigPath:  configPath,
		Interactive: &interactiveVal,
	}, true)
	if err != nil {
		if err.Error() == "cancelled" {
			return 130
		}
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "Wrote %s — starting run…\n\n", path)
	return 0
}

// reportConfigError prints actionable guidance when config fails to load.
func reportConfigError(err error, absRoot string) {
	fmt.Fprintf(os.Stderr, "Cannot start: %v\n\n", err)
	if !config.GlobalConfigExists() {
		fmt.Fprintln(os.Stderr, "  • No global setup found — run: goloop configure")
		fmt.Fprintln(os.Stderr, "    (pick a provider + model and sign in)")
	}
	if !config.IsInitialized(absRoot) {
		fmt.Fprintln(os.Stderr, "  • This directory isn't initialized — run: goloop init")
		fmt.Fprintln(os.Stderr, "    (or pass --goal \"...\")")
	}
}

func runLoop(args []string) int {
	fs := flag.NewFlagSet("goloop run", flag.ExitOnError)
	goal := fs.String("goal", "", "Objective for this run (overrides project config)")
	goalShort := fs.String("g", "", "Alias for --goal")
	lf := registerLoopFlags(fs)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goloop run [directory] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Run the agentic loop in the target directory (default: .).\n\n")
		fs.PrintDefaults()
	}

	targetDir, flagArgs := splitTargetAndFlags(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	setVerboseLogging(lf.verbose)

	targetDir = defaultTargetDir(targetDir)
	absRoot, err := filepath.Abs(targetDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid directory: %v\n", err)
		return 1
	}

	runGoal := strings.TrimSpace(*goal)
	if runGoal == "" {
		runGoal = strings.TrimSpace(*goalShort)
	}

	if runGoal == "" && !config.IsInitialized(absRoot) {
		if code := autoInit(absRoot, lf.configPath); code != 0 {
			return code
		}
	}

	cfg, err := config.Load(lf.configOverrides(absRoot, runGoal))
	if err != nil {
		reportConfigError(err, absRoot)
		return 1
	}

	disp := display.New(lf.plain, !cfg.Interactive)
	return executeLoop(cfg, disp, lf)
}

func resetState(cfg *config.Config) ([]string, error) {
	return reset.State(cfg.CheckpointPath, cfg.UserContextPath, cfg.OutputDir, cfg.Goal)
}

func runOrchestrator(cfg *config.Config, disp *display.Display, maxIterPtr *int) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	orch, err := orchestrator.New(cfg, disp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orchestrator error: %v\n", err)
		return 1
	}

	maxIter := cfg.MaxIterations
	if maxIterPtr != nil {
		maxIter = *maxIterPtr
	}

	if err := orch.Run(ctx, maxIter); err != nil {
		if restart, ok := err.(*orchestrator.RestartForTools); ok {
			disp.RestartForTools(cfg.ToolsRestartExitCode)
			if restart.Summary != "" {
				log.Println("Restart for tools:", restart.Summary)
			}
			return cfg.ToolsRestartExitCode
		}
		if ctx.Err() != nil {
			disp.Warn("Interrupted.")
			return 130
		}
		fmt.Fprintf(os.Stderr, "loop error: %v\n", err)
		return 1
	}

	return 0
}

func setVerboseLogging(verbose bool) {
	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	} else {
		log.SetFlags(0)
	}
}

func defaultTargetDir(target string) string {
	if target == "" {
		return "."
	}
	return target
}

func printDryRun(disp *display.Display, cfg *config.Config) {
	if len(cfg.ConfigSources) > 0 {
		disp.Info("Config: " + strings.Join(cfg.ConfigSources, " + "))
	}
	if cfg.GoalSlug != "" {
		disp.Info(fmt.Sprintf("Goal slug: %s", cfg.GoalSlug))
	}
	disp.Info(fmt.Sprintf("Goal: %s", truncateGoal(cfg.Goal, 80)))
	disp.Info(fmt.Sprintf("Checkpoint: %s", cfg.CheckpointPath))
	disp.Info(fmt.Sprintf("Supervisor: %s", cfg.SupervisorLabel()))
	switch cfg.SupervisorBackend {
	case config.SupervisorOpenAI:
		disp.Info(fmt.Sprintf("API key (%s): %s", cfg.SupervisorAPIKeyEnv, keyStatus(cfg.SupervisorAPIKey)))
	case config.SupervisorChatGPT:
		authPath := auth.ResolveAuthPathForRead(cfg.SupervisorAuthPath)
		disp.Info(fmt.Sprintf("ChatGPT auth: %s (%s)", authPath, chatgptAuthStatus(authPath)))
	case config.SupervisorAnthropic:
		disp.Info(fmt.Sprintf("API key (%s): %s", cfg.SupervisorAPIKeyEnv, keyStatus(cfg.SupervisorAPIKey)))
	}
	disp.Info(fmt.Sprintf("Worker backend: %s", cfg.WorkerBackend))
	disp.Info(fmt.Sprintf("Worker model: %s", cfg.WorkerModel()))
	disp.Info(fmt.Sprintf("Output dir: %s", cfg.OutputDir))
	disp.Info(fmt.Sprintf("Max iterations: %d", cfg.MaxIterations))
	disp.Info(fmt.Sprintf("Worker prompts: built-in (override dir: %s)", cfg.ResolvedAgentsDir()))
	disp.Info(fmt.Sprintf("Tool restart exit code: %d", cfg.ToolsRestartExitCode))
	disp.Info(fmt.Sprintf("Interactive: %v", cfg.Interactive))
	if cfg.AdditionalPrompt != "" {
		preview := cfg.AdditionalPrompt
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		disp.Info("Additional prompt: " + preview)
	}
}

// supervisorNotReady returns a human-readable reason when the supervisor cannot
// authenticate, or "" when it is ready to run.
func supervisorNotReady(cfg *config.Config) string {
	switch cfg.SupervisorBackend {
	case config.SupervisorChatGPT:
		if auth.IsAvailable(auth.ResolveAuthPathForRead(cfg.SupervisorAuthPath)) {
			return ""
		}
		return "ChatGPT sign-in required — run: goloop login"
	case config.SupervisorOpenAI:
		if cfg.SupervisorAPIKey != "" {
			return ""
		}
		return fmt.Sprintf("OpenAI API key missing — set %s or run: goloop configure", cfg.SupervisorAPIKeyEnv)
	case config.SupervisorAnthropic:
		if cfg.SupervisorAPIKey != "" {
			return ""
		}
		return fmt.Sprintf("Anthropic API key missing — set %s or run: goloop configure", cfg.SupervisorAPIKeyEnv)
	}
	return ""
}

func keyStatus(key string) string {
	if key != "" {
		return "set"
	}
	return "NOT SET"
}

func chatgptAuthStatus(path string) string {
	if auth.IsAvailable(path) {
		return "ready"
	}
	return "run `goloop login`"
}

func truncateGoal(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func splitTargetAndFlags(args []string) (target string, flagArgs []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			flagArgs = append(flagArgs, args[i+1:]...)
			return target, flagArgs
		}
		if !strings.HasPrefix(arg, "-") && target == "" {
			target = arg
			continue
		}
		flagArgs = append(flagArgs, arg)
	}
	return target, flagArgs
}
