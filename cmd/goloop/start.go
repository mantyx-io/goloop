package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/configureui"
	"github.com/mantyx-io/goloop/internal/display"
	"github.com/mantyx-io/goloop/internal/goals"
)

func startLoop(args []string) int {
	fs := flag.NewFlagSet("goloop start", flag.ExitOnError)
	goalSlug := fs.String("goal", "", "Run a saved goal by slug")
	goalFile := fs.String("goal-file", "", "Use goal text from a file")
	newGoal := fs.String("new-goal", "", "Create or update a saved goal slug, then run")
	goalText := fs.String("goal-text", "", "Goal text (with --new-goal)")
	listGoals := fs.Bool("list", false, "List saved goals and exit")
	lf := registerLoopFlags(fs)

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goloop start [directory] [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Pick or create an ephemeral goal and run the agentic loop.\n")
		fmt.Fprintf(os.Stderr, "Each goal keeps isolated checkpoint, user context, and output under .goloop/goals/<slug>/.\n\n")
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

	if !config.IsInitialized(absRoot) {
		fmt.Fprintf(os.Stderr, "Project not initialized in %s.\n", absRoot)
		fmt.Fprintln(os.Stderr, "Run `goloop init` first.")
		return 1
	}

	if *listGoals {
		return printGoalList(absRoot)
	}

	sel, err := resolveGoalSelection(absRoot, goalSelectionFlags{
		goalSlug: *goalSlug,
		goalFile: *goalFile,
		newGoal:  *newGoal,
		goalText: *goalText,
	})
	if err != nil {
		if err.Error() == "cancelled" {
			return 130
		}
		fmt.Fprintf(os.Stderr, "goal error: %v\n", err)
		return 1
	}

	checkpoint, userContext, outputDir := goals.StatePaths(absRoot, sel.Slug)
	overrides := lf.configOverrides(absRoot, sel.Text)
	overrides.GoalSlug = sel.Slug
	overrides.CheckpointPath = checkpoint
	overrides.UserContextPath = userContext
	overrides.OutputDir = outputDir

	cfg, err := config.Load(overrides)
	if err != nil {
		reportConfigError(err, absRoot)
		return 1
	}

	disp := display.New(lf.plain, !cfg.Interactive)
	return executeLoop(cfg, disp, lf)
}

type goalSelectionFlags struct {
	goalSlug string
	goalFile string
	newGoal  string
	goalText string
}

func resolveGoalSelection(absRoot string, flags goalSelectionFlags) (goals.Selection, error) {
	flags.goalSlug = strings.TrimSpace(flags.goalSlug)
	flags.goalFile = strings.TrimSpace(flags.goalFile)
	flags.newGoal = strings.TrimSpace(flags.newGoal)
	flags.goalText = strings.TrimSpace(flags.goalText)

	if flags.newGoal != "" {
		text := flags.goalText
		if flags.goalFile != "" {
			_, fromFile, err := goals.ReadFile(flags.goalFile)
			if err != nil {
				return goals.Selection{}, fmt.Errorf("read goal file: %w", err)
			}
			text = fromFile
		}
		if text == "" {
			return goals.Selection{}, fmt.Errorf("--goal-text or --goal-file is required with --new-goal")
		}
		if err := goals.Save(absRoot, flags.newGoal, text); err != nil {
			return goals.Selection{}, err
		}
		return goals.Selection{Slug: flags.newGoal, Text: text, Saved: true}, nil
	}

	if flags.goalSlug != "" {
		text, err := goals.Read(absRoot, flags.goalSlug)
		if err != nil {
			return goals.Selection{}, err
		}
		return goals.Selection{Slug: flags.goalSlug, Text: text, Saved: true}, nil
	}

	if flags.goalFile != "" {
		slug, text, err := goals.ReadFile(flags.goalFile)
		if err != nil {
			return goals.Selection{}, fmt.Errorf("read goal file: %w", err)
		}
		return goals.Selection{Slug: slug, Text: text, Saved: false}, nil
	}

	if flags.goalText != "" {
		return goals.Selection{}, fmt.Errorf("--goal-text requires --new-goal")
	}

	if !(isTerminal(os.Stdin) && isTerminal(os.Stdout)) {
		return goals.Selection{}, fmt.Errorf("non-interactive: pass --goal, --goal-file, or --new-goal")
	}

	defaultObjective := ""
	if snap, err := config.LoadMergedSnapshot(absRoot); err == nil && snap != nil {
		defaultObjective = strings.TrimSpace(snap.Objective)
		if defaultObjective == "" {
			defaultObjective = strings.TrimSpace(snap.Goal)
		}
	}

	return configureui.RunStartPicker(absRoot, defaultObjective)
}

func printGoalList(absRoot string) int {
	items, err := goals.List(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list goals: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Println("No saved goals.")
		return 0
	}
	for _, g := range items {
		title := g.Title
		if title == "" {
			title = g.Preview
		}
		fmt.Printf("%s\t%s\n", g.Slug, title)
	}
	return 0
}
