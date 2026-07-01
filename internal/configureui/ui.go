package configureui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mantyx-io/goloop/internal/auth"
	"github.com/mantyx-io/goloop/internal/config"
	"github.com/mantyx-io/goloop/internal/models"
)

type Options struct {
	Global      bool
	ProjectRoot string
	ConfigPath  string
	Initial     config.ConfigureOptions
}

type wizardStep int

const (
	stepObjective wizardStep = iota
	stepBackend
	stepAuth
	stepSupervisorModel
	stepWorkerBackend
	stepWorkerModel
	stepIterations
	stepReview
)

type menuItem struct {
	title, desc, value string
}

func (i menuItem) Title() string       { return i.title }
func (i menuItem) Description() string { return i.desc }
func (i menuItem) FilterValue() string { return i.title + " " + i.desc + " " + i.value }

type modelsLoadedMsg struct {
	items []models.Info
	err   error
}

type authDoneMsg struct {
	action string
	err    error
}

type model struct {
	step       wizardStep
	global     bool
	opts       config.ConfigureOptions
	root       string
	configPath string

	width, height int
	quitting      bool
	done          bool
	errMsg        string
	status        string

	objective textarea.Model
	search    textinput.Model
	apiKey    textinput.Model
	iters     textinput.Model
	list      list.Model
	spinner   spinner.Model
	loading   bool

	supervisorModels []models.Info
	workerModels     []models.Info
	authSummary      string
	authAction       string
}

func Run(opts Options) (config.ConfigureOptions, error) {
	opts.Initial.Global = opts.Global
	var snap *config.Snapshot
	if opts.Global {
		path := config.ResolveConfigPath("", opts.ConfigPath, true)
		snap, _ = config.LoadSnapshot(path)
	} else {
		snap, _ = config.LoadMergedSnapshot(opts.ProjectRoot)
	}
	applySnapshot(&opts.Initial, snap)

	m := newWizard(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return config.ConfigureOptions{}, err
	}
	fm := final.(model)
	if fm.errMsg != "" && !fm.done {
		return config.ConfigureOptions{}, fmt.Errorf("%s", fm.errMsg)
	}
	if fm.quitting && !fm.done {
		return config.ConfigureOptions{}, fmt.Errorf("cancelled")
	}
	return fm.opts, nil
}

func applySnapshot(initial *config.ConfigureOptions, snap *config.Snapshot) {
	if snap == nil {
		return
	}
	if initial.Objective == "" {
		initial.Objective = strings.TrimSpace(snap.Objective)
		if initial.Objective == "" {
			initial.Objective = strings.TrimSpace(snap.Goal)
		}
	}
	if initial.SupervisorBackend == "" {
		initial.SupervisorBackend = snap.SupervisorBackend
	}
	if initial.SupervisorModel == "" {
		initial.SupervisorModel = snap.SupervisorModel
	}
	if initial.WorkerBackend == "" {
		initial.WorkerBackend = snap.WorkerBackend
	}
	if initial.CursorModel == "" {
		initial.CursorModel = snap.CursorModel
	}
	if initial.ClaudeCodeModel == "" {
		initial.ClaudeCodeModel = snap.ClaudeCodeModel
	}
	if initial.MaxIterations == 0 {
		initial.MaxIterations = snap.MaxIterations
	}
}

func newWizard(opts Options) model {
	step := stepObjective
	if opts.Global {
		step = stepBackend
	}
	m := model{
		step:       step,
		global:     opts.Global,
		root:       opts.ProjectRoot,
		configPath: opts.ConfigPath,
		opts:       opts.Initial,
	}

	ta := textarea.New()
	ta.Placeholder = "Describe what the agentic loop should build or accomplish…"
	ta.SetWidth(70)
	ta.SetHeight(6)
	ta.CharLimit = 8000
	if opts.Initial.Objective != "" {
		ta.SetValue(opts.Initial.Objective)
	}
	m.objective = ta

	si := textinput.New()
	si.Placeholder = "Filter…"
	si.CharLimit = 64
	m.search = si

	ak := textinput.New()
	ak.Placeholder = apiKeyPlaceholder(opts.Initial.SupervisorBackend)
	ak.EchoMode = textinput.EchoPassword
	ak.EchoCharacter = '•'
	ak.CharLimit = 256
	m.apiKey = ak

	it := textinput.New()
	if opts.Initial.MaxIterations > 0 {
		it.SetValue(strconv.Itoa(opts.Initial.MaxIterations))
	} else {
		it.SetValue("50")
	}
	m.iters = it

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	m.spinner = sp

	m.list = newMenuList(28, 14, backendItems(), 0)
	m.workerModels = workerModelsFor(opts.Initial.WorkerBackend)
	m.apiKey.Placeholder = apiKeyPlaceholder(opts.Initial.SupervisorBackend)
	m.refreshAuthSummary()
	m.syncStepUI()
	return m
}

func (m model) Init() tea.Cmd {
	return configureBlinkCmd(m.step)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.objective.SetWidth(clamp(msg.Width-6, 40, 90))
		m.list.SetSize(clamp(msg.Width-4, 30, 80), clamp(msg.Height-12, 8, 20))
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc", "shift+tab":
			if m.loading {
				return m, nil
			}
			return m.stepBack()
		case "tab":
			if m.loading {
				return m, nil
			}
			return m.stepForward()
		case "/":
			if m.step == stepSupervisorModel || m.step == stepWorkerModel {
				m.search.Focus()
				return m, textinput.Blink
			}
		case "ctrl+enter", "ctrl+j", "alt+enter":
			if m.step == stepObjective {
				return m.advance()
			}
		case "enter":
			if m.step == stepBackend || m.step == stepAuth || m.step == stepSupervisorModel || m.step == stepWorkerBackend || m.step == stepWorkerModel {
				return m.onListSelect()
			}
			if m.step == stepObjective && goalReadyToSubmit(m.objective) {
				return m.advance()
			}
		}

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case modelsLoadedMsg:
		m.loading = false
		m.supervisorModels = msg.items
		if msg.err != nil {
			m.status = "Using fallback model list: " + msg.err.Error()
		} else {
			m.status = fmt.Sprintf("Loaded %d models", len(msg.items))
		}
		idx := indexForModel(msg.items, m.opts.SupervisorModel)
		m.list = newModelList(clamp(m.width-4, 30, 80), clamp(m.height-12, 8, 20), msg.items, idx)
		m.search.SetValue("")
		m.search.Blur()
		return m, nil

	case authDoneMsg:
		m.loading = false
		m.authAction = ""
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.errMsg = ""
			m.status = "Authentication updated"
			if msg.action == "api_key" {
				saveAPIKeyForBackend(m.opts.SupervisorBackend, strings.TrimSpace(m.apiKey.Value()))
			}
		}
		m.refreshAuthSummary()
		m.syncStepUI()
		return m, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok && m.step == stepReview && key.String() == "enter" {
		m.done = true
		return m, tea.Quit
	}
	if key, ok := msg.(tea.KeyMsg); ok && m.step == stepIterations && key.String() == "enter" {
		return m.advance()
	}

	var cmd tea.Cmd
	switch m.step {
	case stepObjective:
		m.objective, cmd = m.objective.Update(msg)
	case stepBackend, stepWorkerBackend, stepAuth:
		if m.loading && m.step == stepAuth {
			return m, nil
		}
		if m.step == stepAuth && m.needsAPIKey() && !m.loading {
			if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" && m.apiKey.Focused() {
				keyVal := strings.TrimSpace(m.apiKey.Value())
				if keyVal != "" {
					if err := saveAPIKeyForBackend(m.opts.SupervisorBackend, keyVal); err == nil {
						m.status = "API key saved"
						m.refreshAuthSummary()
						m.syncStepUI()
					}
				}
				return m, nil
			}
			m.apiKey, cmd = m.apiKey.Update(msg)
			return m, cmd
		}
		m.list, cmd = m.list.Update(msg)
	case stepSupervisorModel, stepWorkerModel:
		prev := m.search.Value()
		m.search, cmd = m.search.Update(msg)
		m.list, cmd = m.list.Update(msg)
		if m.search.Value() != prev {
			m.applyModelFilter()
		}
	case stepIterations:
		m.iters, cmd = m.iters.Update(msg)
	}
	return m, cmd
}

func (m *model) applyModelFilter() {
	query := m.search.Value()
	var src []models.Info
	var selected string
	if m.step == stepSupervisorModel {
		src = m.supervisorModels
		selected = m.opts.SupervisorModel
	} else {
		src = m.workerModels
		selected = m.opts.CursorModel
		if m.opts.WorkerBackend == "claude_code" {
			selected = m.opts.ClaudeCodeModel
		}
	}
	filtered := models.Filter(src, query)
	if len(filtered) == 0 {
		filtered = src
	}
	idx := indexForModel(filtered, selected)
	m.list = newModelList(clamp(m.width-4, 30, 80), clamp(m.height-12, 8, 20), filtered, idx)
}

func (m model) onListSelect() (model, tea.Cmd) {
	item, ok := m.list.SelectedItem().(menuItem)
	if !ok {
		return m, nil
	}
	switch m.step {
	case stepBackend:
		m.opts.SupervisorBackend = item.value
		m.step = stepAuth
		m.syncStepUI()
		return m, nil
	case stepAuth:
		switch item.value {
		case "device":
			m.loading = true
			m.authAction = "device"
			return m, loginDeviceCmd()
		case "continue":
			if m.authReady() {
				return m.advance()
			}
			m.errMsg = "Authentication required before continuing"
		case "focus_api":
			m.apiKey.Focus()
			return m, textinput.Blink
		}
		return m, nil
	case stepSupervisorModel:
		m.opts.SupervisorModel = item.value
		m.step = stepWorkerBackend
		m.syncStepUI()
		return m, nil
	case stepWorkerBackend:
		m.opts.WorkerBackend = item.value
		m.step = stepWorkerModel
		m.workerModels = workerModelsFor(item.value)
		m.syncStepUI()
		return m, nil
	case stepWorkerModel:
		if m.opts.WorkerBackend == "claude_code" {
			m.opts.ClaudeCodeModel = item.value
		} else {
			m.opts.CursorModel = item.value
			if m.opts.WorkerBackend == "" {
				m.opts.WorkerBackend = "cursor"
			}
		}
		m.step = stepIterations
		m.iters.Focus()
		return m, nil
	}
	return m, nil
}

func (m *model) advance() (model, tea.Cmd) {
	m.errMsg = ""
	switch m.step {
	case stepObjective:
		obj := strings.TrimSpace(m.objective.Value())
		if obj == "" {
			m.errMsg = "Objective is required"
			return *m, nil
		}
		m.opts.Objective = obj
		m.step = stepBackend
		m.syncStepUI()
	case stepAuth:
		if !m.authReady() {
			m.errMsg = "Complete authentication first"
			return *m, nil
		}
		m.step = stepSupervisorModel
		m.loading = true
		m.status = "Loading supervisor models…"
		return *m, tea.Batch(m.spinner.Tick, fetchSupervisorModelsCmd(m.opts.SupervisorBackend))
	case stepIterations:
		n, err := strconv.Atoi(strings.TrimSpace(m.iters.Value()))
		if err != nil || n <= 0 {
			m.errMsg = "Enter a positive number of iterations"
			return *m, nil
		}
		m.opts.MaxIterations = n
		if m.opts.WorkerBackend == "" {
			m.opts.WorkerBackend = "cursor"
		}
		m.step = stepReview
	default:
		m.step++
		m.syncStepUI()
	}
	return *m, nil
}

// stepBack moves to the previous wizard step, clearing transient state.
func (m model) stepBack() (model, tea.Cmd) {
	minStep := stepObjective
	if m.global {
		minStep = stepBackend
	}
	if m.step > minStep {
		m.step--
		m.errMsg = ""
		m.status = ""
		m.loading = false
		m.syncStepUI()
		return m, configureBlinkCmd(m.step)
	}
	return m, nil
}

// stepForward advances using the current selection, mirroring the natural
// per-step action (select highlighted item, validate input, or save).
func (m model) stepForward() (model, tea.Cmd) {
	switch m.step {
	case stepObjective, stepAuth, stepIterations:
		return m.advance()
	case stepBackend, stepSupervisorModel, stepWorkerBackend, stepWorkerModel:
		return m.onListSelect()
	case stepReview:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m model) statusPanel() string {
	var lines []string
	add := func(active bool, label, val string) {
		if strings.TrimSpace(val) == "" {
			val = subStyle.Render("—")
		}
		padded := padRight(label, 11)
		marker := "  "
		if active {
			marker = focusStyle.Render("➤ ")
			padded = focusStyle.Render(padded)
		} else {
			padded = subStyle.Render(padded)
		}
		lines = append(lines, marker+padded+" "+val)
	}
	if !m.global {
		obj := strings.TrimSpace(m.objective.Value())
		if obj == "" {
			obj = m.opts.Objective
		}
		add(m.step == stepObjective, "Objective", truncate(obj, 52))
	}
	add(m.step == stepBackend, "Provider", m.opts.SupervisorBackend)
	add(m.step == stepAuth, "Auth", m.authStatusLine())
	add(m.step == stepSupervisorModel, "Supervisor", m.opts.SupervisorModel)
	add(m.step == stepWorkerBackend || m.step == stepWorkerModel, "Worker", m.workerStatusLine())
	add(m.step == stepIterations, "Iterations", m.iters.Value())
	return panelStyle.Width(clamp(m.width-4, 40, 90)).Render(strings.Join(lines, "\n"))
}

func (m model) workerStatusLine() string {
	if m.opts.WorkerBackend == "" {
		return ""
	}
	if label := workerModelLabel(m.opts); label != "" {
		return m.opts.WorkerBackend + " / " + label
	}
	return m.opts.WorkerBackend
}

func (m model) authStatusLine() string {
	s := m.rawAuthStatus()
	switch {
	case strings.HasPrefix(s, "✓"):
		return okStyle.Render(s)
	case strings.HasPrefix(s, "✗"):
		return errStyle.Render(s)
	default:
		return subStyle.Render(s)
	}
}

func (m model) rawAuthStatus() string {
	path := auth.ResolveAuthPathForRead("")
	switch strings.ToLower(m.opts.SupervisorBackend) {
	case "chatgpt":
		if creds, err := auth.Load(path); err == nil && creds.Mode == "chatgpt" {
			if plan := auth.PlanLabel(creds); plan != "" {
				return "✓ ChatGPT (" + plan + ")"
			}
			return "✓ ChatGPT signed in"
		}
		return "✗ not signed in"
	case "openai":
		if os.Getenv("OPENAI_API_KEY") != "" {
			return "✓ OPENAI_API_KEY (env)"
		}
		if creds, err := auth.Load(path); err == nil && creds.APIKey != "" {
			return "✓ API key on file"
		}
		return "✗ no API key"
	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			return "✓ ANTHROPIC_API_KEY (env)"
		}
		if creds, err := auth.Load(path); err == nil && creds.AnthropicAPIKey != "" {
			return "✓ API key on file"
		}
		return "✗ no API key"
	default:
		return ""
	}
}

func (m *model) syncStepUI() {
	switch m.step {
	case stepBackend:
		idx := indexForValue(backendItems(), m.opts.SupervisorBackend)
		m.list = newMenuList(clamp(m.width-4, 30, 80), clamp(m.height-12, 8, 20), backendItems(), idx)
	case stepAuth:
		m.apiKey.Placeholder = apiKeyPlaceholder(m.opts.SupervisorBackend)
		m.list = newMenuList(clamp(m.width-4, 30, 80), clamp(m.height-12, 8, 20), m.authItems(), 0)
		m.refreshAuthSummary()
	case stepSupervisorModel:
		idx := indexForModel(m.supervisorModels, m.opts.SupervisorModel)
		m.list = newModelList(clamp(m.width-4, 30, 80), clamp(m.height-12, 8, 20), m.supervisorModels, idx)
		m.search.SetValue("")
		m.search.Blur()
	case stepWorkerBackend:
		idx := indexForValue(workerBackendItems(), m.opts.WorkerBackend)
		m.list = newMenuList(clamp(m.width-4, 30, 80), clamp(m.height-12, 8, 20), workerBackendItems(), idx)
	case stepWorkerModel:
		selected := m.opts.CursorModel
		if m.opts.WorkerBackend == "claude_code" {
			selected = m.opts.ClaudeCodeModel
		}
		idx := indexForModel(m.workerModels, selected)
		m.list = newModelList(clamp(m.width-4, 30, 80), clamp(m.height-12, 8, 20), m.workerModels, idx)
		m.search.SetValue("")
	case stepIterations:
		m.iters.Focus()
	case stepObjective:
		m.objective.Focus()
	}
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Goloop Configure"))
	cur, total := m.displayStep()
	b.WriteString(subStyle.Render(fmt.Sprintf("  — step %d/%d · %s", cur, total, stepName(m.step))))
	b.WriteString("\n\n")

	b.WriteString(m.statusPanel())
	b.WriteString("\n\n")

	if m.loading {
		b.WriteString(m.spinner.View() + " ")
		if m.authAction == "device" {
			b.WriteString("Device code login — check the terminal below for the URL and code\n\n")
		} else {
			b.WriteString("Loading models…\n\n")
		}
	}

	if !m.loading || m.step != stepAuth {
		switch m.step {
		case stepObjective:
			b.WriteString(focusStyle.Render("Objective") + "\n")
			b.WriteString(m.objective.View())
			b.WriteString("\n")
		case stepBackend:
			b.WriteString(focusStyle.Render("Supervisor provider") + "\n\n")
			b.WriteString(m.list.View())
		case stepAuth:
			b.WriteString(focusStyle.Render("Authentication") + "\n")
			b.WriteString(panelStyle.Width(clamp(m.width-4, 40, 90)).Render(m.authSummary))
			b.WriteString("\n\n")
			if m.needsAPIKey() {
				label := "OpenAI API key"
				if strings.ToLower(m.opts.SupervisorBackend) == "anthropic" {
					label = "Anthropic API key"
				}
				b.WriteString(subStyle.Render(label) + "\n")
				b.WriteString(m.apiKey.View())
				b.WriteString("\n\n")
			}
			b.WriteString(m.list.View())
		case stepSupervisorModel:
			b.WriteString(focusStyle.Render("Supervisor model") + " ")
			b.WriteString(subStyle.Render("(type to filter, / focuses search)") + "\n")
			b.WriteString(m.search.View())
			b.WriteString("\n")
			b.WriteString(m.list.View())
		case stepWorkerBackend:
			b.WriteString(focusStyle.Render("Worker") + "\n\n")
			b.WriteString(m.list.View())
		case stepWorkerModel:
			title := "Cursor worker model"
			if m.opts.WorkerBackend == "claude_code" {
				title = "Claude Code model"
			}
			b.WriteString(focusStyle.Render(title) + "\n")
			b.WriteString(m.search.View())
			b.WriteString("\n")
			b.WriteString(m.list.View())
		case stepIterations:
			b.WriteString(focusStyle.Render("Max iterations per run") + "\n")
			b.WriteString(m.iters.View())
		case stepReview:
			b.WriteString(focusStyle.Render("Review") + "\n\n")
			b.WriteString(m.summary())
		}
	}

	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(errStyle.Render("✗ "+m.errMsg) + "\n")
	}
	if m.status != "" && !m.loading {
		b.WriteString(okStyle.Render(m.status) + "\n")
	}
	b.WriteString(hintStyle.Render(m.hints()))
	return b.String()
}

func (m model) summary() string {
	var lines []string
	if m.global {
		lines = append(lines, fmt.Sprintf("Config file:   %s", config.GlobalConfigPath()))
	} else {
		lines = append(lines, fmt.Sprintf("Directory:     %s", m.root))
		lines = append(lines, fmt.Sprintf("Objective:     %s", truncate(m.opts.Objective, 60)))
	}
	lines = append(lines,
		fmt.Sprintf("Supervisor:    %s / %s", m.opts.SupervisorBackend, m.opts.SupervisorModel),
		fmt.Sprintf("Worker:        %s / %s", m.opts.WorkerBackend, workerModelLabel(m.opts)),
		fmt.Sprintf("Max iterations: %d", m.opts.MaxIterations),
	)
	return panelStyle.Render(strings.Join(lines, "\n"))
}

func (m model) displayStep() (current, total int) {
	total = 8
	if m.global {
		total = 7
		current = int(m.step)
	} else {
		current = int(m.step) + 1
	}
	return current, total
}

func (m model) hints() string {
	nav := "tab: next · shift+tab/esc: back · ctrl+c: quit"
	switch m.step {
	case stepObjective:
		return "type your goal · enter on a blank line: continue · " + nav
	case stepIterations:
		return "enter: continue · " + nav
	case stepSupervisorModel, stepWorkerModel:
		return "enter: select · /: filter · " + nav
	case stepReview:
		return "enter/tab: save · shift+tab/esc: back · ctrl+c: quit"
	case stepAuth:
		if m.needsAPIKey() {
			return "enter: select or save API key · " + nav
		}
		return "enter: select · " + nav
	default:
		return "enter: select · " + nav
	}
}

func (m *model) refreshAuthSummary() {
	backend := strings.ToLower(m.opts.SupervisorBackend)
	path := auth.ResolveAuthPathForRead("")
	var parts []string

	switch backend {
	case "chatgpt":
		if creds, err := auth.Load(path); err == nil && creds.Mode == "chatgpt" {
			plan := auth.PlanLabel(creds)
			parts = append(parts, okStyle.Render("✓ ChatGPT OAuth ready"))
			parts = append(parts, fmt.Sprintf("  file: %s", path))
			if plan != "" {
				parts = append(parts, fmt.Sprintf("  plan: %s", plan))
			}
		} else if os.Getenv("OPENAI_API_KEY") != "" {
			parts = append(parts, okStyle.Render("✓ OPENAI_API_KEY set (not used for ChatGPT backend)"))
		} else {
			parts = append(parts, errStyle.Render("✗ Sign in with ChatGPT to continue"))
		}
	case "openai":
		if os.Getenv("OPENAI_API_KEY") != "" {
			parts = append(parts, okStyle.Render("✓ OPENAI_API_KEY set in environment"))
		} else if creds, err := auth.Load(path); err == nil && creds.APIKey != "" {
			parts = append(parts, okStyle.Render("✓ OpenAI API key on file"))
			parts = append(parts, fmt.Sprintf("  file: %s", path))
		} else {
			parts = append(parts, errStyle.Render("✗ Set OPENAI_API_KEY or enter a key below"))
		}
	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			parts = append(parts, okStyle.Render("✓ ANTHROPIC_API_KEY set in environment"))
		} else if creds, err := auth.Load(path); err == nil && creds.AnthropicAPIKey != "" {
			parts = append(parts, okStyle.Render("✓ Anthropic API key on file"))
			parts = append(parts, fmt.Sprintf("  file: %s", path))
		} else {
			parts = append(parts, errStyle.Render("✗ Set ANTHROPIC_API_KEY or enter a key below"))
		}
	default:
		parts = append(parts, errStyle.Render("✗ Select a supervisor provider"))
	}
	m.authSummary = strings.Join(parts, "\n")
}

func (m model) authReady() bool {
	switch strings.ToLower(m.opts.SupervisorBackend) {
	case "chatgpt":
		return auth.IsAvailable(auth.ResolveAuthPathForRead(""))
	case "openai":
		if os.Getenv("OPENAI_API_KEY") != "" {
			return true
		}
		creds, err := auth.Load(auth.ResolveAuthPathForRead(""))
		return err == nil && creds.APIKey != ""
	case "anthropic":
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			return true
		}
		creds, err := auth.Load(auth.ResolveAuthPathForRead(""))
		return err == nil && creds.AnthropicAPIKey != ""
	default:
		return false
	}
}

func (m model) needsAPIKey() bool {
	switch strings.ToLower(m.opts.SupervisorBackend) {
	case "openai", "anthropic":
		return true
	default:
		return false
	}
}

func (m model) authItems() []menuItem {
	var items []menuItem
	backend := strings.ToLower(m.opts.SupervisorBackend)
	if backend == "chatgpt" {
		items = append(items,
			menuItem{"Sign in with ChatGPT", "Device code — open URL in any browser", "device"},
		)
	}
	if backend == "openai" || backend == "anthropic" {
		label := "OpenAI API key"
		if backend == "anthropic" {
			label = "Anthropic API key"
		}
		items = append(items,
			menuItem{"Enter API key", label + " → ~/.goloop/auth.json", "focus_api"},
		)
	}
	if m.authReady() {
		items = append(items, menuItem{"Continue →", "Proceed to model selection", "continue"})
	}
	return items
}

func backendItems() []menuItem {
	return []menuItem{
		{"ChatGPT", "Use your ChatGPT Plus/Pro subscription", "chatgpt"},
		{"OpenAI API", "Usage-based API key billing", "openai"},
		{"Anthropic API", "Claude models via API key", "anthropic"},
	}
}

func workerBackendItems() []menuItem {
	return []menuItem{
		{"Cursor", "Cursor CLI agent worker", "cursor"},
		{"Claude Code", "Claude Code CLI (-p headless)", "claude_code"},
	}
}

func workerModelsFor(backend string) []models.Info {
	if backend == "claude_code" {
		return models.ClaudeCodeWorkerModels()
	}
	return models.CursorWorkerModels()
}

func saveAPIKeyForBackend(backend, key string) error {
	if strings.ToLower(backend) == "anthropic" {
		return auth.SaveAnthropicAPIKey(auth.DefaultAuthPath(), key)
	}
	return auth.SaveAPIKey(auth.DefaultAuthPath(), key)
}

func workerModelLabel(opts config.ConfigureOptions) string {
	if opts.WorkerBackend == "claude_code" {
		return opts.ClaudeCodeModel
	}
	return opts.CursorModel
}

func newMenuList(width, height int, items []menuItem, selected int) list.Model {
	delegate := list.NewDefaultDelegate()
	l := list.New(toListItems(items), delegate, width, height)
	l.Title = ""
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	if selected >= 0 && selected < len(items) {
		l.Select(selected)
	}
	return l
}

func newModelList(width, height int, models []models.Info, selected int) list.Model {
	items := modelMenuItems(models)
	delegate := list.NewDefaultDelegate()
	l := list.New(toListItems(items), delegate, width, height)
	l.Title = ""
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	if selected >= 0 && selected < len(items) {
		l.Select(selected)
	}
	return l
}

func modelMenuItems(models []models.Info) []menuItem {
	out := make([]menuItem, len(models))
	for i, m := range models {
		title := m.DisplayName
		if title == "" {
			title = m.ID
		}
		desc := m.ID
		if m.Description != "" {
			desc = m.Description
		}
		out[i] = menuItem{title: title, desc: desc, value: m.ID}
	}
	return out
}

func toListItems(items []menuItem) []list.Item {
	out := make([]list.Item, len(items))
	for i := range items {
		out[i] = items[i]
	}
	return out
}

func indexForValue(items []menuItem, value string) int {
	for i, it := range items {
		if it.value == value {
			return i
		}
	}
	return 0
}

func indexForModel(items []models.Info, id string) int {
	for i, m := range items {
		if m.ID == id {
			return i
		}
	}
	return 0
}

func stepName(s wizardStep) string {
	switch s {
	case stepObjective:
		return "objective"
	case stepBackend:
		return "provider"
	case stepAuth:
		return "auth"
	case stepSupervisorModel:
		return "supervisor model"
	case stepWorkerBackend:
		return "worker"
	case stepWorkerModel:
		return "worker model"
	case stepIterations:
		return "iterations"
	case stepReview:
		return "review"
	default:
		return ""
	}
}

func fetchSupervisorModelsCmd(backend string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		items, err := models.Fetch(ctx, models.FetchParams{Backend: backend})
		return modelsLoadedMsg{items: items, err: err}
	}
}

type deviceLoginExec struct{}

func (deviceLoginExec) SetStdin(io.Reader)  {}
func (deviceLoginExec) SetStdout(io.Writer) {}
func (deviceLoginExec) SetStderr(io.Writer) {}

func (deviceLoginExec) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return auth.ChatGPTLogin(ctx, auth.DefaultAuthPath())
}

func loginDeviceCmd() tea.Cmd {
	return tea.Exec(deviceLoginExec{}, func(err error) tea.Msg {
		return authDoneMsg{action: "device", err: err}
	})
}
