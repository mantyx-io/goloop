package configureui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mantyx-io/goloop/internal/goals"
)

const (
	startPickValueNew = "__new__"
)

type startStep int

const (
	startStepPick startStep = iota
	startStepSlug
	startStepGoalText
)

// RunStartPicker launches the goal picker for goloop start.
func RunStartPicker(projectRoot, defaultObjective string) (goals.Selection, error) {
	saved, err := goals.List(projectRoot)
	if err != nil {
		return goals.Selection{}, err
	}

	m := newStartPicker(projectRoot, defaultObjective, saved)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return goals.Selection{}, err
	}
	fm := final.(startModel)
	if fm.errMsg != "" && !fm.done {
		return goals.Selection{}, fmt.Errorf("%s", fm.errMsg)
	}
	if fm.quitting && !fm.done {
		return goals.Selection{}, fmt.Errorf("cancelled")
	}
	return fm.selection, nil
}

type startModel struct {
	step             startStep
	projectRoot      string
	defaultObjective string
	saved            []goals.Goal

	width, height int
	quitting      bool
	done          bool
	errMsg        string

	list      list.Model
	slug      textinput.Model
	objective textarea.Model

	selection goals.Selection
}

func newStartPicker(projectRoot, defaultObjective string, saved []goals.Goal) startModel {
	step := startStepPick
	if len(saved) == 0 {
		step = startStepSlug
	}

	slug := textinput.New()
	slug.Placeholder = "todo-cli"
	slug.CharLimit = 64
	if defaultObjective != "" {
		slug.SetValue(goals.SanitizeSlug(firstHeadingOrLine(defaultObjective)))
	}

	ta := textarea.New()
	ta.Placeholder = "What should the agentic loop accomplish for this goal?"
	ta.SetWidth(70)
	ta.SetHeight(6)
	ta.CharLimit = 8000
	if defaultObjective != "" {
		ta.SetValue(defaultObjective)
	}

	m := startModel{
		step:             step,
		projectRoot:      projectRoot,
		defaultObjective: defaultObjective,
		saved:            saved,
		slug:             slug,
		objective:        ta,
	}
	m.syncStartStep()
	return m
}

func firstHeadingOrLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		return line
	}
	return ""
}

func (m startModel) Init() tea.Cmd {
	return startBlinkCmd(m.step)
}

func startBlinkCmd(step startStep) tea.Cmd {
	switch step {
	case startStepGoalText:
		return textarea.Blink
	default:
		return textinput.Blink
	}
}

func (m startModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.objective.SetWidth(clamp(msg.Width-6, 40, 90))
		if m.step == startStepPick {
			m.list.SetSize(clamp(msg.Width-4, 30, 80), clamp(m.height-12, 6, 20))
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			if m.step == startStepGoalText {
				m.step = startStepSlug
				m.errMsg = ""
				m.slug.Focus()
				return m, textinput.Blink
			}
			if m.step == startStepSlug && len(m.saved) > 0 {
				m.step = startStepPick
				m.errMsg = ""
				m.syncStartStep()
				return m, nil
			}
			if m.step == startStepPick {
				m.quitting = true
				return m, tea.Quit
			}
		case "ctrl+enter", "ctrl+j", "alt+enter":
			if m.step == startStepGoalText {
				return m.advanceStart()
			}
		case "enter":
			switch m.step {
			case startStepPick:
				return m.onPickSelect()
			case startStepSlug:
				return m.advanceStart()
			case startStepGoalText:
				if goalReadyToSubmit(m.objective) {
					return m.advanceStart()
				}
			}
		}
	}

	var cmd tea.Cmd
	switch m.step {
	case startStepPick:
		m.list, cmd = m.list.Update(msg)
	case startStepSlug:
		m.slug, cmd = m.slug.Update(msg)
	case startStepGoalText:
		m.objective, cmd = m.objective.Update(msg)
	}
	return m, cmd
}

func (m startModel) onPickSelect() (startModel, tea.Cmd) {
	item, ok := m.list.SelectedItem().(menuItem)
	if !ok {
		return m, nil
	}
	if item.value == startPickValueNew {
		m.step = startStepSlug
		m.errMsg = ""
		m.slug.Focus()
		if m.slug.Value() == "" && m.defaultObjective != "" {
			m.slug.SetValue(goals.SanitizeSlug(firstHeadingOrLine(m.defaultObjective)))
		}
		if m.objective.Value() == "" && m.defaultObjective != "" {
			m.objective.SetValue(m.defaultObjective)
		}
		return m, textinput.Blink
	}

	text, err := goals.Read(m.projectRoot, item.value)
	if err != nil {
		m.errMsg = err.Error()
		return m, nil
	}
	m.selection = goals.Selection{Slug: item.value, Text: text, Saved: true}
	m.done = true
	return m, tea.Quit
}

func (m startModel) advanceStart() (startModel, tea.Cmd) {
	m.errMsg = ""
	switch m.step {
	case startStepSlug:
		slug := strings.TrimSpace(m.slug.Value())
		if err := goals.ValidateSlug(slug); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.slug.SetValue(slug)
		m.step = startStepGoalText
		m.objective.Focus()
		return m, textarea.Blink
	case startStepGoalText:
		text := strings.TrimSpace(m.objective.Value())
		if text == "" {
			m.errMsg = "Goal text is required"
			return m, nil
		}
		slug := strings.TrimSpace(m.slug.Value())
		if err := goals.Save(m.projectRoot, slug, text); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		m.selection = goals.Selection{Slug: slug, Text: text, Saved: true}
		m.done = true
		return m, tea.Quit
	default:
		return m, nil
	}
}

func (m *startModel) syncStartStep() {
	switch m.step {
	case startStepPick:
		m.list = newMenuList(clamp(m.width-4, 30, 80), clamp(m.height-12, 6, 20), startGoalItems(m.saved), 0)
	case startStepSlug:
		m.slug.Focus()
	case startStepGoalText:
		m.objective.Focus()
	}
}

func startGoalItems(saved []goals.Goal) []menuItem {
	items := make([]menuItem, 0, len(saved)+1)
	for _, g := range saved {
		desc := g.Preview
		if desc == "" {
			desc = g.Title
		}
		items = append(items, menuItem{
			title: g.Slug,
			desc:  truncate(desc, 72),
			value: g.Slug,
		})
	}
	items = append(items, menuItem{
		title: "+ New goal",
		desc:  "Create a goal and add it to the library",
		value: startPickValueNew,
	})
	return items
}

func (m startModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Goloop Start"))
	b.WriteString(subStyle.Render("  — pick a goal"))
	b.WriteString("\n\n")
	b.WriteString(subStyle.Render(fmt.Sprintf("Project: %s", m.projectRoot)))
	b.WriteString("\n\n")

	switch m.step {
	case startStepPick:
		b.WriteString(focusStyle.Render("Saved goals") + "\n")
		b.WriteString(subStyle.Render("Each goal keeps its own checkpoint and output") + "\n\n")
		b.WriteString(m.list.View())
	case startStepSlug:
		b.WriteString(focusStyle.Render("Goal slug") + "\n")
		b.WriteString(subStyle.Render("Short name for this goal (used in .goloop/goals/<slug>.md)") + "\n")
		b.WriteString(m.slug.View())
	case startStepGoalText:
		b.WriteString(focusStyle.Render("Goal") + "\n")
		b.WriteString(subStyle.Render(fmt.Sprintf("Saving as: %s", m.slug.Value())) + "\n")
		b.WriteString(m.objective.View())
	}

	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(errStyle.Render("✗ "+m.errMsg) + "\n")
	}
	b.WriteString(hintStyle.Render(m.startHints()))
	return b.String()
}

func (m startModel) startHints() string {
	switch m.step {
	case startStepPick:
		return "enter: select · ctrl+c: quit"
	case startStepGoalText:
		return "type your goal · enter on a blank line (or ctrl+enter): save & start · esc: back · ctrl+c: quit"
	case startStepSlug:
		return "enter: continue · esc: back · ctrl+c: quit"
	default:
		return "ctrl+c: quit"
	}
}
