package configureui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mantyx-io/goloop/internal/config"
)

type InitOptions struct {
	ProjectRoot string
	ConfigPath  string
	Initial     config.InitOptions
}

type initStep int

const (
	initStepGoal initStep = iota
	initStepOutput
	initStepIters
	initStepInteractive
	initStepReview
)

type initModel struct {
	step       initStep
	opts       config.InitOptions
	root       string
	configPath string

	width, height int
	quitting      bool
	done          bool
	errMsg        string

	objective textarea.Model
	outputDir textinput.Model
	iters     textinput.Model
	list      list.Model
}

// RunInit launches the project init wizard.
func RunInit(opts InitOptions) (config.InitOptions, error) {
	snap, _ := config.LoadMergedSnapshot(opts.ProjectRoot)
	applyInitSnapshot(&opts.Initial, snap)

	m := newInitWizard(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return config.InitOptions{}, err
	}
	fm := final.(initModel)
	if fm.errMsg != "" && !fm.done {
		return config.InitOptions{}, fmt.Errorf("%s", fm.errMsg)
	}
	if fm.quitting && !fm.done {
		return config.InitOptions{}, fmt.Errorf("cancelled")
	}
	return fm.opts, nil
}

func applyInitSnapshot(initial *config.InitOptions, snap *config.Snapshot) {
	if snap == nil {
		return
	}
	if initial.Objective == "" {
		initial.Objective = strings.TrimSpace(snap.Objective)
		if initial.Objective == "" {
			initial.Objective = strings.TrimSpace(snap.Goal)
		}
	}
	if initial.OutputDir == "" {
		initial.OutputDir = snap.OutputDir
	}
	if initial.MaxIterations == 0 {
		initial.MaxIterations = snap.MaxIterations
	}
	if initial.Interactive == nil {
		v := snap.Interactive
		initial.Interactive = &v
	}
}

func newInitWizard(opts InitOptions) initModel {
	if opts.Initial.OutputDir == "" {
		opts.Initial.OutputDir = "."
	}
	if opts.Initial.MaxIterations == 0 {
		opts.Initial.MaxIterations = 50
	}
	interactive := true
	if opts.Initial.Interactive != nil {
		interactive = *opts.Initial.Interactive
	}

	ta := textarea.New()
	ta.Placeholder = "What should the agentic loop build or accomplish in this project?"
	ta.SetWidth(70)
	ta.SetHeight(6)
	ta.CharLimit = 8000
	if opts.Initial.Objective != "" {
		ta.SetValue(opts.Initial.Objective)
	}
	ta.Focus()

	out := textinput.New()
	out.Placeholder = "."
	out.CharLimit = 128
	out.SetValue(opts.Initial.OutputDir)

	it := textinput.New()
	it.SetValue(strconv.Itoa(opts.Initial.MaxIterations))

	idx := 0
	if !interactive {
		idx = 1
	}

	m := initModel{
		step:       initStepGoal,
		opts:       opts.Initial,
		root:       opts.ProjectRoot,
		configPath: opts.ConfigPath,
		objective:  ta,
		outputDir:  out,
		iters:      it,
		list:       newMenuList(28, 6, interactiveItems(), idx),
	}
	m.syncInitStep()
	return m
}

func (m initModel) Init() tea.Cmd {
	return initBlinkCmd(m.step)
}

func (m initModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.objective.SetWidth(clamp(msg.Width-6, 40, 90))
		m.list.SetSize(clamp(msg.Width-4, 30, 80), 6)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			if m.step > initStepGoal {
				m.step--
				m.errMsg = ""
				m.syncInitStep()
				return m, initBlinkCmd(m.step)
			}
		case "ctrl+enter", "ctrl+j", "alt+enter":
			if m.step == initStepGoal {
				return m.advanceInit()
			}
		case "enter":
			if m.step == initStepInteractive {
				return m.onInteractiveSelect()
			}
			if m.step == initStepGoal && goalReadyToSubmit(m.objective) {
				return m.advanceInit()
			}
		}
	}

	if key, ok := msg.(tea.KeyMsg); ok && m.step == initStepReview && key.String() == "enter" {
		m.done = true
		return m, tea.Quit
	}
	if key, ok := msg.(tea.KeyMsg); ok && (m.step == initStepOutput || m.step == initStepIters) && key.String() == "enter" {
		return m.advanceInit()
	}

	var cmd tea.Cmd
	switch m.step {
	case initStepGoal:
		m.objective, cmd = m.objective.Update(msg)
	case initStepOutput:
		m.outputDir, cmd = m.outputDir.Update(msg)
	case initStepIters:
		m.iters, cmd = m.iters.Update(msg)
	case initStepInteractive:
		m.list, cmd = m.list.Update(msg)
	}
	return m, cmd
}

func (m initModel) onInteractiveSelect() (initModel, tea.Cmd) {
	item, ok := m.list.SelectedItem().(menuItem)
	if !ok {
		return m, nil
	}
	v := item.value == "true"
	m.opts.Interactive = &v
	return m.advanceInit()
}

func (m *initModel) advanceInit() (initModel, tea.Cmd) {
	m.errMsg = ""
	switch m.step {
	case initStepGoal:
		obj := strings.TrimSpace(m.objective.Value())
		if obj == "" {
			m.errMsg = "Goal is required"
			return *m, nil
		}
		m.opts.Objective = obj
		m.step = initStepOutput
		m.outputDir.Focus()
	case initStepOutput:
		dir := strings.TrimSpace(m.outputDir.Value())
		if dir == "" {
			dir = "."
		}
		m.opts.OutputDir = dir
		m.step = initStepIters
		m.iters.Focus()
	case initStepIters:
		n, err := strconv.Atoi(strings.TrimSpace(m.iters.Value()))
		if err != nil || n <= 0 {
			m.errMsg = "Enter a positive number of iterations"
			return *m, nil
		}
		m.opts.MaxIterations = n
		m.step = initStepInteractive
		m.syncInitStep()
	case initStepInteractive:
		m.step = initStepReview
	default:
		m.step++
	}
	return *m, nil
}

func (m *initModel) syncInitStep() {
	switch m.step {
	case initStepGoal:
		m.objective.Focus()
	case initStepOutput:
		m.outputDir.Focus()
	case initStepIters:
		m.iters.Focus()
	case initStepInteractive:
		interactive := true
		if m.opts.Interactive != nil {
			interactive = *m.opts.Interactive
		}
		idx := 0
		if !interactive {
			idx = 1
		}
		m.list = newMenuList(clamp(m.width-4, 30, 80), 6, interactiveItems(), idx)
	}
}

func (m initModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Goloop Init"))
	b.WriteString(subStyle.Render(fmt.Sprintf("  — step %d/5 · %s", int(m.step)+1, initStepName(m.step))))
	b.WriteString("\n\n")
	b.WriteString(subStyle.Render(fmt.Sprintf("Project: %s", m.root)))
	b.WriteString("\n\n")

	switch m.step {
	case initStepGoal:
		b.WriteString(focusStyle.Render("Goal") + "\n")
		b.WriteString(m.objective.View())
	case initStepOutput:
		b.WriteString(focusStyle.Render("Output directory") + "\n")
		b.WriteString(subStyle.Render("Where the worker writes code (relative to project root)") + "\n")
		b.WriteString(m.outputDir.View())
	case initStepIters:
		b.WriteString(focusStyle.Render("Max iterations per run") + "\n")
		b.WriteString(m.iters.View())
	case initStepInteractive:
		b.WriteString(focusStyle.Render("Interactive mode") + "\n")
		b.WriteString(subStyle.Render("Prompt stdin when the supervisor asks a question") + "\n\n")
		b.WriteString(m.list.View())
	case initStepReview:
		b.WriteString(focusStyle.Render("Review") + "\n\n")
		b.WriteString(m.initSummary())
	}

	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(errStyle.Render("✗ " + m.errMsg) + "\n")
	}
	b.WriteString(hintStyle.Render(m.initHints()))
	return b.String()
}

func (m initModel) initSummary() string {
	interactive := "yes"
	if m.opts.Interactive != nil && !*m.opts.Interactive {
		interactive = "no"
	}
	lines := []string{
		fmt.Sprintf("Config:        %s", config.LocalConfigPath(m.root)),
		fmt.Sprintf("Goal:          %s", truncate(m.opts.Objective, 60)),
		fmt.Sprintf("Output dir:    %s/", m.opts.OutputDir),
		fmt.Sprintf("Max iterations: %d", m.opts.MaxIterations),
		fmt.Sprintf("Interactive:   %s", interactive),
	}
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m initModel) initHints() string {
	switch m.step {
	case initStepGoal:
		return "type your goal · enter on a blank line (or ctrl+enter): continue · ctrl+c: quit"
	case initStepReview:
		return "enter: create project · esc: back · ctrl+c: quit"
	case initStepInteractive:
		return "enter: select · esc: back · ctrl+c: quit"
	default:
		return "enter: continue · esc: back · ctrl+c: quit"
	}
}

func initStepName(s initStep) string {
	switch s {
	case initStepGoal:
		return "goal"
	case initStepOutput:
		return "output"
	case initStepIters:
		return "iterations"
	case initStepInteractive:
		return "interactive"
	case initStepReview:
		return "review"
	default:
		return ""
	}
}

func interactiveItems() []menuItem {
	return []menuItem{
		{"Yes", "Pause for human input on ask_user", "true"},
		{"No", "Record blockers instead of prompting", "false"},
	}
}
