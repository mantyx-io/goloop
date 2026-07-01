package orchestrator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mantyx-io/goloop/internal/checkpoint"
	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/display"
	"github.com/mantyx-io/goloop/internal/gitx"
	"github.com/mantyx-io/goloop/internal/llm"
	"github.com/mantyx-io/goloop/internal/notify"
	"github.com/mantyx-io/goloop/internal/supervisor"
	"github.com/mantyx-io/goloop/internal/tools"
	"github.com/mantyx-io/goloop/internal/transcript"
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
	cfg         *config.Config
	display     *display.Display
	supervisor  llm.Client
	worker      worker.Runner
	userContext *usercontext.Store
	checkpoint  *checkpoint.Checkpoint
	transcript  *transcript.Logger
	usageSeen   llm.Usage
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

	var tl *transcript.Logger
	if cfg.Transcript {
		tl = transcript.New(filepath.Join(cfg.GoloopDir, "logs"))
	}

	return &Orchestrator{
		cfg:         cfg,
		display:     disp,
		supervisor:  sup,
		worker:      w,
		userContext: usercontext.New(cfg.UserContextPath),
		checkpoint:  ckpt,
		transcript:  tl,
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

	defer o.transcript.Close()
	o.transcript.Log("run_start", 0, map[string]any{
		"goal":           o.cfg.Goal,
		"supervisor":     o.cfg.SupervisorLabel(),
		"worker":         string(o.cfg.WorkerBackend),
		"worker_model":   o.cfg.WorkerModel(),
		"max_iterations": maxIterations,
	})
	if o.transcript != nil {
		o.display.Info("Transcript: " + o.transcript.Path())
	}

	for i := 1; i <= maxIterations; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if o.cfg.MaxTokens > 0 {
			if used := o.supervisorUsage(); used.Total() >= o.cfg.MaxTokens {
				msg := fmt.Sprintf("Token budget reached (%d/%d supervisor tokens) — stopping.", used.Total(), o.cfg.MaxTokens)
				o.display.Warn(msg)
				o.checkpoint.AddBlocker(msg)
				if err := o.checkpoint.Save(); err != nil {
					return err
				}
				o.notify("Goloop", msg)
				o.logRunEnd("token_budget", i)
				return nil
			}
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
			o.logRunEnd("restart_for_tools", i)
			return &RestartForTools{Summary: str(plan, "summary", "")}
		}

		if str(plan, "action", "") == "complete" {
			confirmed, err := o.confirmCompletion(ctx, i)
			if err != nil {
				return fmt.Errorf("iteration %d completion audit: %w", i, err)
			}
			if confirmed {
				o.display.GoalComplete()
				o.notify("Goloop", "Objective complete: "+truncate(o.cfg.Goal, 120))
				o.logRunEnd("complete", i)
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(o.cfg.PauseSeconds) * time.Second):
		}
	}

	o.notify("Goloop", fmt.Sprintf("Run ended after %d iteration(s) without completion.", maxIterations))
	o.logRunEnd("max_iterations", maxIterations)
	return nil
}

func (o *Orchestrator) supervisorUsage() llm.Usage {
	if tracker, ok := o.supervisor.(llm.UsageTracker); ok {
		return tracker.TotalUsage()
	}
	return llm.Usage{}
}

func (o *Orchestrator) logRunEnd(reason string, iteration int) {
	usage := o.supervisorUsage()
	o.transcript.Log("run_end", iteration, map[string]any{
		"reason":        reason,
		"tokens_input":  usage.Input,
		"tokens_output": usage.Output,
	})
}

// confirmCompletion audits the supervisor's complete claim with a read-only
// evaluator pass. Premature completion is the classic failure mode of agent
// loops, so the claim only stands when the evaluator confirms it (or when
// auditing is disabled or returns no verdict). On rejection the findings are
// written to the checkpoint so the next iteration can act on them.
func (o *Orchestrator) confirmCompletion(ctx context.Context, iteration int) (bool, error) {
	// The verify command is the cheapest and most objective signal — check it
	// before spending evaluator tokens on the claim.
	if o.cfg.VerifyCommand != "" {
		if verify := o.runVerify(ctx, iteration); !verify.ok {
			o.display.Warn("Verify command failed — completion claim rejected.")
			o.checkpoint.AddBlocker(fmt.Sprintf("Completion rejected: verify command failed (exit %d): %s",
				verify.exit, truncate(verify.output, 200)))
			o.checkpoint.AppendHistory(checkpoint.Entry{
				Iteration: iteration,
				Action:    "completion_audit",
				Summary:   "Verify command failed; completion claim rejected.",
				Status:    "failed",
				Notes:     truncate(verify.summary(), 1500),
			})
			if err := o.checkpoint.Save(); err != nil {
				return false, err
			}
			o.display.CheckpointSaved(o.cfg.CheckpointPath)
			return false, nil
		}
	}

	if !o.cfg.AuditCompletion {
		return true, nil
	}

	task := o.workerTask(fmt.Sprintf(
		`The supervisor claims the objective is fully achieved. Audit that claim.

Objective: %s

Verify end-to-end against the repository: deliverables exist, build, and satisfy the objective.
Be skeptical — look for stubs, TODOs, failing builds or tests, and unmet requirements.

End your reply with exactly one final line:
VERDICT: COMPLETE
or
VERDICT: INCOMPLETE — <what is missing>`,
		o.cfg.Goal,
	))
	result, err := o.runWorker(func() (worker.Result, error) {
		return o.worker.RunEvaluator(ctx, task)
	}, fmt.Sprintf("Auditing completion claim with %s evaluator…", o.cfg.WorkerBackend))
	if err != nil {
		return false, err
	}

	notes := summarizeWorker(result)
	complete, found := parseAuditVerdict(result.Stdout)
	o.display.ShowWorkerResult(string(o.cfg.WorkerBackend)+" (completion audit)", complete || !found, notes, result.Reasoning)
	o.transcript.Log("completion_audit", iteration, map[string]any{
		"complete": complete,
		"found":    found,
		"output":   truncate(result.Stdout, 20000),
	})

	if !found {
		// Don't deadlock the loop on a formatting miss — accept, but say so.
		o.display.Warn("Completion audit returned no VERDICT line; accepting the claim.")
		return true, nil
	}
	if complete {
		return true, nil
	}

	o.display.Warn("Completion audit rejected the claim — continuing the loop.")
	o.checkpoint.AddBlocker("Completion audit rejected the supervisor's complete claim: " + truncate(notes, 200))
	o.checkpoint.AppendHistory(checkpoint.Entry{
		Iteration: iteration,
		Action:    "completion_audit",
		Summary:   "Evaluator rejected the completion claim; the loop continues.",
		Status:    "failed",
		Notes:     truncate(notes, 1500),
	})
	if err := o.checkpoint.Save(); err != nil {
		return false, err
	}
	o.display.CheckpointSaved(o.cfg.CheckpointPath)
	return false, nil
}

// parseAuditVerdict scans for the last VERDICT line the evaluator emitted.
// The second return value reports whether any verdict was found at all.
func parseAuditVerdict(text string) (complete, found bool) {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.ToUpper(strings.Trim(strings.TrimSpace(lines[i]), "*_#> "))
		if !strings.HasPrefix(line, "VERDICT:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "VERDICT:"))
		if strings.HasPrefix(rest, "INCOMPLETE") {
			return false, true
		}
		if strings.HasPrefix(rest, "COMPLETE") {
			return true, true
		}
	}
	return false, false
}

// notify posts a desktop notification when enabled; long runs happen in the
// background, so surface the moments that need the human.
func (o *Orchestrator) notify(title, message string) {
	if o.cfg.Notifications {
		notify.Send(title, message)
	}
}

type verifyResult struct {
	ok      bool
	exit    int
	command string
	output  string
}

func (v verifyResult) summary() string {
	state := "passed"
	if !v.ok {
		state = fmt.Sprintf("FAILED (exit %d)", v.exit)
	}
	out := strings.TrimSpace(v.output)
	if len(out) > 2000 {
		out = out[:2000] + "\n... [truncated]"
	}
	if out == "" {
		return fmt.Sprintf("verify `%s`: %s", v.command, state)
	}
	return fmt.Sprintf("verify `%s`: %s\n%s", v.command, state, out)
}

// runVerify executes the configured verify command in the project root. Its
// exit code is a deterministic signal the supervisor's self-reports are not.
func (o *Orchestrator) runVerify(ctx context.Context, iteration int) verifyResult {
	o.display.Working("Verifying: " + o.cfg.VerifyCommand)

	vctx, cancel := context.WithTimeout(ctx, time.Duration(o.cfg.VerifyTimeoutSeconds)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(vctx, "cmd", "/C", o.cfg.VerifyCommand)
	} else {
		cmd = exec.CommandContext(vctx, "sh", "-c", o.cfg.VerifyCommand)
	}
	cmd.Dir = o.cfg.ProjectRoot
	out, err := cmd.CombinedOutput()

	result := verifyResult{ok: err == nil, command: o.cfg.VerifyCommand, output: string(out)}
	if err != nil {
		result.exit = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.exit = exitErr.ExitCode()
		}
		if vctx.Err() == context.DeadlineExceeded {
			result.output += "\n(verify timed out)"
		}
	}

	if result.ok {
		o.display.Info("Verify passed: " + o.cfg.VerifyCommand)
	} else {
		o.display.Warn(fmt.Sprintf("Verify failed (exit %d): %s", result.exit, o.cfg.VerifyCommand))
	}
	o.transcript.Log("verify", iteration, map[string]any{
		"command": result.command,
		"ok":      result.ok,
		"exit":    result.exit,
		"output":  truncate(result.output, 20000),
	})
	return result
}

// autoCommit records the iteration's changes as a git commit so every loop
// step is a rollback point. Failures only warn — committing is a convenience.
func (o *Orchestrator) autoCommit(iteration int, plan map[string]any, workerOK bool) {
	if !gitx.IsRepo(o.cfg.ProjectRoot) {
		return
	}
	title := strings.TrimSpace(firstNonEmpty(str(plan, "delegate_title", ""), str(plan, "summary", ""), "delegated work"))
	message := fmt.Sprintf("goloop iteration %d: %s", iteration, truncate(title, 60))
	if !workerOK {
		message += " (worker failed)"
	}
	committed, err := gitx.CommitAll(o.cfg.ProjectRoot, message)
	if err != nil {
		o.display.Warn("Auto-commit failed: " + err.Error())
		return
	}
	if committed {
		o.display.Info("Committed: " + message)
		o.transcript.Log("commit", iteration, map[string]any{"message": message})
	}
}

func (o *Orchestrator) logWorkerResult(role string, iteration int, result worker.Result) {
	o.transcript.Log("worker_result", iteration, map[string]any{
		"role":      role,
		"exit":      result.ReturnCode,
		"output":    truncate(firstNonEmpty(result.Stdout, result.Stderr), 20000),
		"reasoning": truncate(result.Reasoning, 20000),
	})
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
	if err != nil {
		return plan, err
	}

	o.transcript.Log("plan", iteration, map[string]any{"plan": plan})
	if usage := o.supervisorUsage(); usage.Total() > o.usageSeen.Total() {
		o.display.Info(fmt.Sprintf("Supervisor tokens: +%d (run total %d)",
			usage.Total()-o.usageSeen.Total(), usage.Total()))
		o.usageSeen = usage
	}
	return plan, nil
}

func (o *Orchestrator) executePlan(ctx context.Context, iteration int, plan map[string]any, allowFollowUp bool) (map[string]any, error) {
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
			o.notify("Goloop needs your input", truncate(question, 120))
			answer := o.display.AskUser(question, userCtx)
			if err := o.userContext.Append(iteration, question, answer); err != nil {
				return plan, err
			}
			o.transcript.Log("ask_user", iteration, map[string]any{"question": question, "answer": answer})
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
				return o.worker.RunToolsmith(ctx, task)
			}, "Running toolsmith…")
			if err != nil {
				return plan, err
			}
			notes = summarizeWorker(result)
			o.display.ShowWorkerResult(string(o.cfg.WorkerBackend)+" (toolsmith)", result.OK(), notes, result.Reasoning)
			o.logWorkerResult("toolsmith", iteration, result)
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
				return o.worker.RunBuilder(ctx, task)
			}, fmt.Sprintf("Running %s worker…", o.cfg.WorkerBackend))
			if err != nil {
				return plan, err
			}
			notes = summarizeWorker(result)
			o.display.ShowWorkerResult(string(o.cfg.WorkerBackend), result.OK(), notes, result.Reasoning)
			o.logWorkerResult("builder", iteration, result)
			if !result.OK() {
				if status == "success" {
					status = "failed"
				}
				o.checkpoint.AddBlocker(fmt.Sprintf("%s worker exited %d: %s",
					o.cfg.WorkerBackend, result.ReturnCode, truncate(notes, 200)))
			}

			if result.OK() && o.cfg.VerifyCommand != "" {
				verify := o.runVerify(ctx, iteration)
				notes += "\n\n" + verify.summary()
				if !verify.ok {
					status = "failed"
					o.checkpoint.AddBlocker(fmt.Sprintf("Verify command failed (exit %d): %s",
						verify.exit, truncate(verify.output, 200)))
				}
			}

			if o.cfg.AutoCommit {
				o.autoCommit(iteration, plan, result.OK())
			}
		}

	case "evaluate":
		criteria := str(plan, "evaluation", "")
		evalTask := o.workerTask(fmt.Sprintf(
			"Review the project against checkpoint.md and report gaps.\nCriteria: %s",
			defaultString(criteria, "standard milestone checklist"),
		))
		result, err := o.runWorker(func() (worker.Result, error) {
			return o.worker.RunEvaluator(ctx, evalTask)
		}, fmt.Sprintf("Running %s evaluator…", o.cfg.WorkerBackend))
		if err != nil {
			return plan, err
		}
		notes = summarizeWorker(result)
		o.display.ShowWorkerResult(string(o.cfg.WorkerBackend)+" (evaluator)", result.OK(), notes, result.Reasoning)
		o.logWorkerResult("evaluator", iteration, result)
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
	if gitx.IsRepo(o.cfg.ProjectRoot) {
		if summary := gitx.ChangeSummary(o.cfg.ProjectRoot); summary != "" {
			parts = append(parts, "## Uncommitted changes (git)\n```\n"+summary+"\n```")
		}
	}
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
		result += "\n... [truncated]"
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
