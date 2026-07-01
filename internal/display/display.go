package display

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	statusStyles = map[string]lipgloss.Style{
		"success": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
		"partial": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")),
		"blocked": lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208")),
		"failed":  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")),
	}
	actionStyles = map[string]lipgloss.Style{
		"delegate":        lipgloss.NewStyle().Foreground(lipgloss.Color("14")),
		"delegate_tools":  lipgloss.NewStyle().Foreground(lipgloss.Color("51")),
		"evaluate":        lipgloss.NewStyle().Foreground(lipgloss.Color("12")),
		"ask_user":        lipgloss.NewStyle().Foreground(lipgloss.Color("13")),
		"checkpoint_only": lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		"complete":        lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")),
	}
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true)
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type Display struct {
	RichOutput            bool
	Interactive           bool
	mu                    sync.Mutex
	workerStreamReasoning bool
	workerStreamResponse  bool
	workerReasoningShown  bool
	mdRenderer            *glamour.TermRenderer
	width                 int
}

func New(plain, noInteractive bool) *Display {
	interactive := !noInteractive && isTerminal(os.Stdin)
	rich := !plain && isTerminal(os.Stdout)

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(100),
	)

	width := 100
	if termWidth := terminalWidth(); termWidth > 0 {
		width = termWidth - 4
		if width < 60 {
			width = 60
		}
	}

	return &Display{
		RichOutput:  rich,
		Interactive: interactive,
		mdRenderer:  renderer,
		width:       width,
	}
}

func (d *Display) Banner(goal, supervisorLabel, workerBackend, workerModel string, maxIterations int, additionalPrompt string) {
	if !d.RichOutput {
		return
	}

	subtitle := fmt.Sprintf("Supervisor: %s  ·  Worker: %s (%s)  ·  Max iterations: %d",
		supervisorLabel, workerBackend, workerModel, maxIterations)

	body := titleStyle.Render("Goloop Agentic Loop") + "\n" +
		dimStyle.Render(subtitle) + "\n\n" +
		strings.TrimSpace(goal)

	if strings.TrimSpace(additionalPrompt) != "" {
		preview := strings.TrimSpace(additionalPrompt)
		if len(preview) > 400 {
			preview = preview[:400] + "…"
		}
		body += "\n\n" + titleStyle.Render("Operator prompt") + "\n" + dimStyle.Render(preview)
	}

	panel := panelStyle.
		Width(d.width).
		BorderForeground(lipgloss.Color("39")).
		Render(body)

	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render("⟳ Goloop") + "\n" + panel)
	fmt.Println()
}

func (d *Display) IterationHeader(iteration, limit int, phase string) {
	if !d.RichOutput {
		return
	}
	line := titleStyle.Render(fmt.Sprintf("Iteration %d/%d", iteration, limit))
	if phase != "" {
		line += dimStyle.Render("  ·  phase: ") + lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(phase)
	}
	rule := strings.Repeat("─", d.width)
	fmt.Println()
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render(rule))
	fmt.Println(line)
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Render(rule))
}

func (d *Display) PlanningStart() {
	if !d.RichOutput {
		fmt.Println("Planning next step…")
		return
	}
	fmt.Println(dimStyle.Render("◌ Planning next step…"))
}

func (d *Display) PlanningEnd() {}

func (d *Display) Working(label string) {
	if !d.RichOutput {
		fmt.Println(label)
	}
}

func (d *Display) ShowPlan(plan map[string]any, label string) {
	if !d.RichOutput {
		return
	}
	if label == "" {
		label = "Plan"
	}

	action := str(plan, "action", "checkpoint_only")
	status := str(plan, "status", "partial")
	phase := str(plan, "phase", "—")

	header := fmt.Sprintf("%s %s\n%s %s\n%s %s",
		dimStyle.Render("Action:"), styleFor(actionStyles, action, action),
		dimStyle.Render("Status:"), styleFor(statusStyles, status, status),
		dimStyle.Render("Phase:"), phase,
	)

	fmt.Println(d.panel(header, label, "14"))

	reasoning := strings.TrimSpace(str(plan, "reasoning", ""))
	if reasoning != "" {
		d.printMarkdownPanel("Reasoning", reasoning, "8")
	}
	if summary := strings.TrimSpace(str(plan, "summary", "")); summary != "" && summary != reasoning {
		d.printMarkdownPanel("Summary", summary, "8")
	}

	if ckpt, ok := plan["checkpoint_update"].(map[string]any); ok {
		d.showCheckpointPreview(ckpt)
	}

	switch action {
	case "delegate", "delegate_tools":
		taskKey := "delegate_task"
		panelTitle := "Delegate task"
		border := "10"
		if action == "delegate_tools" {
			taskKey = "tools_task"
			panelTitle = "Tools task"
			border = "51"
		}
		if title := str(plan, "delegate_title", ""); title != "" {
			fmt.Println(dimStyle.Render("Session:") + " " + titleStyle.Render(title))
		}
		if task := strings.TrimSpace(str(plan, taskKey, "")); task != "" {
			d.printMarkdownPanel(panelTitle, task, border)
		}
		if action == "delegate_tools" {
			fmt.Println(dimStyle.Render("On success the loop exits code 75 and auto-restarts."))
		}
	case "evaluate":
		if criteria := strings.TrimSpace(str(plan, "evaluation", "")); criteria != "" {
			d.printMarkdownPanel("Evaluation criteria", criteria, "12")
		}
	case "ask_user":
		if ctx := strings.TrimSpace(str(plan, "user_context", "")); ctx != "" {
			d.printMarkdownPanel("Why we need this", ctx, "13")
		}
		if question := strings.TrimSpace(str(plan, "user_question", "")); question != "" {
			d.printMarkdownPanel("Question for you", question, "13")
		}
	}
	fmt.Println()
}

func (d *Display) showCheckpointPreview(ckpt map[string]any) {
	sections := []struct {
		name  string
		color string
		key   string
	}{
		{"Completed", "10", "completed"},
		{"In progress", "11", "in_progress"},
		{"Blockers", "9", "blockers"},
		{"Next steps", "14", "next_steps"},
	}

	var rows []string
	for _, s := range sections {
		items := stringSlice(ckpt[s.key])
		if len(items) == 0 {
			continue
		}
		var bullets []string
		for _, item := range items {
			bullets = append(bullets, lipgloss.NewStyle().Foreground(lipgloss.Color(s.color)).Render("•")+" "+item)
		}
		rows = append(rows, dimStyle.Render(s.name)+"\n"+strings.Join(bullets, "\n"))
	}
	if len(rows) == 0 {
		return
	}
	fmt.Println(d.panel(strings.Join(rows, "\n\n"), "Checkpoint update", "8"))
}

func (d *Display) BeginWorkerStream() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.workerStreamReasoning = false
	d.workerStreamResponse = false
	d.workerReasoningShown = false
	if !d.RichOutput {
		return
	}
	fmt.Println(titleStyle.Foreground(lipgloss.Color("14")).Render("Worker running…") + dimStyle.Render(" (streaming reasoning)"))
}

func (d *Display) WorkerThinkingDelta(text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.RichOutput {
		return
	}
	if !d.workerStreamReasoning {
		d.workerStreamReasoning = true
		fmt.Print("\n" + dimStyle.Italic(true).Render("Reasoning: "))
	}
	fmt.Print(dimStyle.Italic(true).Render(text))
}

func (d *Display) WorkerAssistantDelta(text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.RichOutput {
		return
	}
	if d.workerStreamReasoning {
		fmt.Println()
		d.workerStreamReasoning = false
	}
	if !d.workerStreamResponse {
		d.workerStreamResponse = true
		fmt.Print(titleStyle.Render("Response: "))
	}
	fmt.Print(text)
}

func (d *Display) EndWorkerStream() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.RichOutput {
		return
	}
	if d.workerStreamReasoning || d.workerStreamResponse {
		fmt.Println()
	}
	d.workerReasoningShown = d.workerStreamReasoning
	fmt.Println()
}

func (d *Display) ShowWorkerResult(backend string, ok bool, output, reasoning string) {
	if !d.RichOutput {
		return
	}
	border := "9"
	title := "FAILED"
	if ok {
		border = "10"
		title = "OK"
	}

	if strings.TrimSpace(reasoning) != "" && !d.workerReasoningShown {
		body := reasoning
		if len(body) > 4000 {
			body = body[:4000] + "\n\n… [truncated]"
		}
		fmt.Println(d.panel(body, "Worker reasoning", "8"))
	}

	body := stripExitPrefix(output)
	if body == "" {
		body = "_(no output)_"
	}
	if len(body) > 6000 {
		body = body[:6000] + "\n\n… [truncated]"
	}
	fmt.Println(d.panel(body, fmt.Sprintf("Worker (%s) — %s", backend, title), border))
	fmt.Println()
}

func (d *Display) AskUser(question, context string) string {
	if !d.Interactive {
		if d.RichOutput {
			fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Non-interactive mode — cannot prompt."))
		}
		return ""
	}

	fmt.Println()
	if strings.TrimSpace(context) != "" {
		d.printMarkdownPanel("Context", context, "13")
	}
	d.printMarkdownPanel("Your input needed", question, "13")

	fmt.Println(dimStyle.Render("Type your answer below. Press Enter twice (empty line) when done, or Ctrl+D to finish.\n"))

	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" && len(lines) > 0 {
			break
		}
		if line == "" && len(lines) == 0 {
			continue
		}
		lines = append(lines, line)
	}

	answer := strings.TrimSpace(strings.Join(lines, "\n"))
	if answer != "" {
		fmt.Println(d.panel(answer, "Recorded", "10"))
	} else {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("No answer provided."))
	}
	fmt.Println()
	return answer
}

func (d *Display) CheckpointSaved(path string) {
	if !d.RichOutput {
		return
	}
	fmt.Println(dimStyle.Render("Checkpoint saved → ") + path)
	fmt.Println()
}

func (d *Display) RestartForTools(exitCode int) {
	if !d.RichOutput {
		return
	}
	body := titleStyle.Foreground(lipgloss.Color("51")).Render("New supervisor tools installed.") + "\n" +
		fmt.Sprintf("Exiting with code %d — wrapper will restart the loop.", exitCode)
	fmt.Println(d.panel(body, "↻ Restart", "51"))
}

func (d *Display) GoalComplete() {
	if !d.RichOutput {
		return
	}
	body := titleStyle.Foreground(lipgloss.Color("10")).Render("Goal complete!") + "\n" +
		"The orchestrator believes the objective has been achieved end-to-end."
	fmt.Println(d.panel(body, "Done", "10"))
}

func (d *Display) Info(message string) {
	if d.RichOutput {
		fmt.Println(dimStyle.Render(message))
	} else {
		fmt.Println(message)
	}
}

func (d *Display) Warn(message string) {
	if d.RichOutput {
		fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Render(message))
	} else {
		fmt.Println("WARNING:", message)
	}
}

func (d *Display) panel(body, title, color string) string {
	return panelStyle.
		Width(d.width).
		BorderForeground(lipgloss.Color(color)).
		Render(titleStyle.Render(title) + "\n\n" + body)
}

func (d *Display) printMarkdownPanel(title, content, color string) {
	rendered := content
	if d.mdRenderer != nil {
		if md, err := d.mdRenderer.Render(content); err == nil {
			rendered = strings.TrimRight(md, "\n")
		}
	}
	fmt.Println(d.panel(rendered, title, color))
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

func styleFor(styles map[string]lipgloss.Style, key, fallback string) string {
	if style, ok := styles[key]; ok {
		return style.Render(key)
	}
	return fallback
}

func stripExitPrefix(output string) string {
	lines := strings.Split(output, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "exit=") {
		return strings.Join(lines[1:], "\n")
	}
	return output
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func terminalWidth() int {
	// Avoid importing golang.org/x/term in display only - use env COLUMNS
	if cols := os.Getenv("COLUMNS"); cols != "" {
		var w int
		if _, err := fmt.Sscanf(cols, "%d", &w); err == nil && w > 0 {
			return w
		}
	}
	return 100
}
