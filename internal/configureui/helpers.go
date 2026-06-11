package configureui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// goalReadyToSubmit reports whether a plain Enter should advance past a
// multi-line goal textarea. Since ctrl/cmd+enter is unreliable across
// terminals (notably macOS), pressing Enter on a blank line submits the
// goal — the first Enter opens a new line, the second confirms.
func goalReadyToSubmit(ta textarea.Model) bool {
	value := ta.Value()
	if strings.TrimSpace(value) == "" {
		return false
	}
	lines := strings.Split(value, "\n")
	row := ta.Line()
	if row < 0 || row >= len(lines) {
		return false
	}
	return strings.TrimSpace(lines[row]) == ""
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func apiKeyPlaceholder(backend string) string {
	if strings.ToLower(backend) == "anthropic" {
		return "sk-ant-…"
	}
	return "sk-…"
}

func configureBlinkCmd(step wizardStep) tea.Cmd {
	if step == stepObjective {
		return textarea.Blink
	}
	return textinput.Blink
}

func initBlinkCmd(step initStep) tea.Cmd {
	switch step {
	case initStepGoal:
		return textarea.Blink
	default:
		return textinput.Blink
	}
}
