package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mantyx-io/goloop/internal/checkpoint"
	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/display"
	"github.com/mantyx-io/goloop/internal/llm"
	"github.com/mantyx-io/goloop/internal/supervisor"
	"github.com/mantyx-io/goloop/internal/tools"
	"github.com/mantyx-io/goloop/internal/usercontext"
	"github.com/mantyx-io/goloop/internal/worker"
)

const systemPrompt = `You are the Goloop Orchestrator — a supervisor agent that plans and delegates work.

Your mission: iteratively achieve the configured objective by planning, evaluating, and delegating
implementation to worker agents (e.g. Cursor CLI). You maintain state exclusively through
checkpoint.md updates.

All deliverables must be built under the configured output directory (the project root by default).
You do NOT write code directly.

To extend your own capabilities, use action delegate_tools — the worker will implement new
supervisor tools under .goloop/tools/. The loop then exits with code 75 and auto-restarts
so you can see the new tools in the next session.

Do not ask the builder agent to modify loop infrastructure — only the toolsmith agent may add tools.

When you lack domain knowledge, preferences, credentials, or a product decision, use action
ask_user to request information from the human operator. Do not guess on matters only the
human can answer.

## Each iteration you must output JSON with this schema:
{
  "reasoning": "brief analysis of current state and what to do next",
  "action": "delegate | delegate_tools | evaluate | ask_user | checkpoint_only | complete",
  "phase": "bootstrap | build | test | polish | integration",
  "delegate_task": "detailed task for the worker agent (required if action=delegate)",
  "tools_task": "detailed task for toolsmith (required if action=delegate_tools)",
  "delegate_title": "short session title (optional)",
  "evaluation": "what to verify after delegation (optional)",
  "user_question": "clear question for the human (required if action=ask_user)",
  "user_context": "why you need this answer (optional, shown to the human)",
  "checkpoint_update": {
    "completed": ["items done this iteration"],
    "in_progress": ["current focus areas"],
    "blockers": ["blockers"],
    "next_steps": ["ordered next steps"]
  },
  "status": "success | partial | blocked | failed",
  "summary": "one paragraph for the iteration log"
}

Rules:
- Prefer small, verifiable delegations over large vague tasks.
- Always update checkpoint fields to reflect reality.
- Use action=delegate_tools when you need a new supervisor tool.
- Use action=ask_user when blocked on human-only input.
- Use action=complete only when the objective is achieved end-to-end.
- If blocked, document blockers clearly and suggest concrete unblocking steps.
- Never hallucinate completed work — only mark items done when evidence exists in the repo.
- Honor ## Operator instructions for this run when present — they override general priorities.
`

type RestartForTools struct {
	Summary string
}

func (e *RestartForTools) Error() string {
	return "restart for tools: " + e.Summary
}

type Orchestrator struct {
	cfg          *config.Config
	display      *display.Display
	supervisor   llm.Client
	worker       worker.Runner
	userContext  *usercontext.Store
	checkpoint   *checkpoint.Checkpoint
}

func New(cfg *config.Config, disp *display.Display) (*Orchestrator, error) {
	sup, err := supervisor.New(cfg)
	if err != nil {
		return nil, err
	}
	w, err := worker.New(cfg, disp)
	if err != nil {
		return nil, err
	}

	ckpt := checkpoint.New(cfg.CheckpointPath, cfg.Goal)
	if err := ckpt.Read(); err != nil {
		return nil, err
	}

	return &Orchestrator{
		cfg:         cfg,
		display:     disp,
		supervisor:  sup,
		worker:      w,
		userContext: usercontext.New(cfg.UserContextPath),
		checkpoint:  ckpt,
	}, nil
}

func (o *Orchestrator) Run(ctx context.Context, maxIterations int) error {
	if maxIterations <= 0 {
		maxIterations = o.cfg.MaxIterations
	}

	o.display.Banner(
		o.cfg.Goal,
		o.cfg.SupervisorLabel(),
		string(o.cfg.WorkerBackend),
		o.cfg.WorkerModel(),
		maxIterations,
		o.cfg.AdditionalPrompt,
	)

	for i := 1; i <= maxIterations; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		o.display.IterationHeader(i, maxIterations, o.checkpoint.Phase)

		plan, err := o.planIteration(ctx, i, "")
		if err != nil {
			return fmt.Errorf("iteration %d plan: %w", i, err)
		}

		o.display.ShowPlan(plan, "Plan")
		plan, err = o.executePlan(ctx, i, plan, true)
		if err != nil {
			return fmt.Errorf("iteration %d execute: %w", i, err)
		}

		if restart, _ := plan["_restart_for_tools"].(bool); restart {
			return &RestartForTools{Summary: str(plan, "summary", "")}
		}

		if str(plan, "action", "") == "complete" {
			o.display.GoalComplete()
			return nil
		}

		time.Sleep(time.Duration(o.cfg.PauseSeconds) * time.Second)
	}

	return nil
}

func (o *Orchestrator) planIteration(ctx context.Context, iteration int, extraNote string) (map[string]any, error) {
	contextText := o.buildContext(iteration, extraNote)
	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("Iteration %d. Review state and produce the next JSON plan.\n\n%s", iteration, contextText)},
	}

	o.display.PlanningStart()
	plan, err := o.supervisor.ChatJSON(ctx, messages)
	o.display.PlanningEnd()
	return plan, err
}

func (o *Orchestrator) executePlan(ctx context.Context, iteration int, plan map[string]any, allowFollowUp bool) (map[string]any, error) {
	_ = ctx

	action := str(plan, "action", "checkpoint_only")
	status := str(plan, "status", "partial")
	summary := str(plan, "summary", str(plan, "reasoning", "No summary provided."))
	notes := ""

	if phase := str(plan, "phase", ""); phase != "" {
		o.checkpoint.SetPhase(phase)
	}

	ckptUpdate, _ := plan["checkpoint_update"].(map[string]any)

	switch action {
	case "ask_user":
		question := strings.TrimSpace(str(plan, "user_question", ""))
		userCtx := strings.TrimSpace(str(plan, "user_context", ""))
		if question == "" {
			notes = "ask_user missing user_question."
			status = "failed"
		} else if !o.cfg.Interactive {
			notes = "Human input needed (non-interactive): " + question
			status = "blocked"
			o.checkpoint.AddBlocker(notes)
		} else {
			answer := o.display.AskUser(question, userCtx)
			if err := o.userContext.Append(iteration, question, answer); err != nil {
				return plan, err
			}
			if answer != "" {
				notes = "User answered: " + truncate(answer, 500)
			} else {
				notes = "User provided no answer."
			}
			if answer != "" && allowFollowUp {
				followUp, err := o.planIteration(ctx, iteration,
					fmt.Sprintf("The human just answered your question.\n\nQ: %s\nA: %s", question, answer))
				if err != nil {
					return plan, err
				}
				o.display.ShowPlan(followUp, "Follow-up plan")
				return o.executePlan(ctx, iteration, followUp, false)
			}
		}

	case "delegate_tools":
		task := strings.TrimSpace(firstNonEmpty(str(plan, "tools_task", ""), str(plan, "delegate_task", "")))
		if task == "" {
			notes = "delegate_tools missing tools_task."
			status = "failed"
		} else {
			if title := str(plan, "delegate_title", ""); title != "" {
				task = "[" + title + "]\n\n" + task
			}
			task = o.workerTask(fmt.Sprintf(
				"Implement supervisor tool(s) under `.goloop/tools/` as YAML specs with name and description.\n\n%s",
				task,
			))
			result, err := o.runWorker(func() (worker.Result, error) {
				return o.worker.RunToolsmith(task)
			}, "Running toolsmith…")
			if err != nil {
				return plan, err
			}
			notes = summarizeWorker(result)
			o.display.ShowWorkerResult(string(o.cfg.WorkerBackend)+" (toolsmith)", result.OK(), notes, result.Reasoning)
			if result.OK() {
				plan["_restart_for_tools"] = true
				notes += fmt.Sprintf("\n\nLoop will exit %d and restart to load new tools.", o.cfg.ToolsRestartExitCode)
			} else {
				status = "failed"
				o.checkpoint.AddBlocker(fmt.Sprintf("Toolsmith failed (exit %d): %s", result.ReturnCode, truncate(notes, 200)))
			}
		}

	case "delegate":
		task := strings.TrimSpace(str(plan, "delegate_task", ""))
		if task == "" {
			notes = "Delegate action missing delegate_task; skipped worker run."
			status = "failed"
		} else {
			if title := str(plan, "delegate_title", ""); title != "" {
				task = "[" + title + "]\n\n" + task
			}
			outputName := filepath.Base(o.cfg.OutputDir)
			task = o.workerTask(fmt.Sprintf("Build all artifacts under `%s/`.\n\n%s", outputName, task))
			result, err := o.runWorker(func() (worker.Result, error) {
				return o.worker.RunBuilder(task)
			}, fmt.Sprintf("Running %s worker…", o.cfg.WorkerBackend))
			if err != nil {
				return plan, err
			}
			notes = summarizeWorker(result)
			o.display.ShowWorkerResult(string(o.cfg.WorkerBackend), result.OK(), notes, result.Reasoning)
			if !result.OK() {
				if status == "success" {
					status = "failed"
				}
				o.checkpoint.AddBlocker(fmt.Sprintf("%s worker exited %d: %s",
					o.cfg.WorkerBackend, result.ReturnCode, truncate(notes, 200)))
			}
		}

	case "evaluate":
		criteria := str(plan, "evaluation", "")
		evalTask := o.workerTask(fmt.Sprintf(
			"Review the project against checkpoint.md and report gaps.\nCriteria: %s",
			defaultString(criteria, "standard milestone checklist"),
		))
		result, err := o.runWorker(func() (worker.Result, error) {
			return o.worker.RunEvaluator(evalTask)
		}, fmt.Sprintf("Running %s evaluator…", o.cfg.WorkerBackend))
		if err != nil {
			return plan, err
		}
		notes = summarizeWorker(result)
		o.display.ShowWorkerResult(string(o.cfg.WorkerBackend)+" (evaluator)", result.OK(), notes, result.Reasoning)
	}

	o.checkpoint.UpdateFromPlan(
		stringSlice(ckptUpdate["completed"]),
		stringSlice(ckptUpdate["in_progress"]),
		stringSlice(ckptUpdate["blockers"]),
		stringSlice(ckptUpdate["next_steps"]),
	)
	o.checkpoint.AppendHistory(checkpoint.Entry{
		Iteration: iteration,
		Action:    action,
		Summary:   summary,
		Status:    status,
		Notes:     notes,
	})
	if err := o.checkpoint.Save(); err != nil {
		return plan, err
	}
	o.display.CheckpointSaved(o.cfg.CheckpointPath)
	return plan, nil
}

func (o *Orchestrator) buildContext(iteration int, extraNote string) string {
	checkpointText, _ := os.ReadFile(o.cfg.CheckpointPath)
	userText, _ := o.userContext.Read()

	var parts []string
	parts = append(parts, fmt.Sprintf("## Iteration\n%d", iteration))
	parts = append(parts, fmt.Sprintf("## Goal\n%s", o.cfg.Goal))
	parts = append(parts, "## checkpoint.md\n```markdown\n"+strings.TrimSpace(string(checkpointText))+"\n```")
	if userText != "" {
		parts = append(parts, "## user_context.md\n```markdown\n"+userText+"\n```")
	}
	parts = append(parts, "## Supervisor tools (.goloop/tools/)\n"+tools.Describe(o.cfg.ToolsDirPath()))
	outputTree := treeSummary(o.cfg.OutputDir, "project/", 3)
	parts = append(parts, "## Output project\n```\n"+outputTree+"\n```")
	repoTree := treeSummary(o.cfg.ProjectRoot, "", 3)
	parts = append(parts, "## Repository layout (repo root)\n```\n"+repoTree+"\n```")
	if o.cfg.AdditionalPrompt != "" {
		parts = append(parts, "## Operator instructions (this run)\n"+o.cfg.AdditionalPrompt+
			"\n\nTreat these as high-priority guidance from the human operator.")
	}
	if extraNote != "" {
		parts = append(parts, "## Latest human input\n"+extraNote)
	}
	return strings.Join(parts, "\n\n")
}

func (o *Orchestrator) workerTask(task string) string {
	if o.cfg.AdditionalPrompt == "" {
		return task
	}
	return "## Operator instructions\n" + o.cfg.AdditionalPrompt + "\n\n---\n\n" + task
}

func (o *Orchestrator) runWorker(fn func() (worker.Result, error), label string) (worker.Result, error) {
	streaming := o.cfg.WorkerBackend == config.WorkerCursor && o.cfg.WorkerShowReasoning
	if !streaming {
		o.display.Working(label)
	}
	return fn()
}

func summarizeWorker(result worker.Result) string {
	output := strings.TrimSpace(firstNonEmpty(result.Stdout, result.Stderr))
	if len(output) > 4000 {
		output = output[:4000] + "\n... [truncated]"
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("exit=%d", result.ReturnCode))
	if result.Reasoning != "" {
		r := result.Reasoning
		if len(r) > 1500 {
			r = r[:1500] + "\n... [truncated]"
		}
		parts = append(parts, "reasoning:\n"+r)
	}
	parts = append(parts, output)
	return strings.Join(parts, "\n")
}

func treeSummary(root, label string, maxDepth int) string {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return "(not created yet)"
	}

	var lines []string
	if label != "" {
		lines = append(lines, label)
	}

	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		if depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.Name() == ".git" || entry.Name() == ".gitkeep" {
				continue
			}
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			lines = append(lines, name)
			if entry.IsDir() && depth < maxDepth {
				walk(filepath.Join(path, entry.Name()), depth+1)
			}
			if len(lines) >= 80 {
				return
			}
		}
	}
	walk(root, 0)

	if len(lines) == 0 || (len(lines) == 1 && label != "") {
		return "(empty)"
	}
	result := strings.Join(lines, "\n")
	if len(lines) >= 80 {
		return result
	}
	return result
}

func str(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
