package checkpoint

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Entry struct {
	Iteration int
	Action    string
	Summary   string
	Status    string
	Notes     string
}

type Checkpoint struct {
	Path        string
	Goal        string
	Phase       string
	Iteration   int
	Completed   []string
	InProgress  []string
	Blockers    []string
	NextSteps   []string
	History     []Entry
}

func New(path, goal string) *Checkpoint {
	return &Checkpoint{
		Path:  path,
		Goal:  goal,
		Phase: "bootstrap",
	}
}

func (c *Checkpoint) Read() error {
	if _, err := os.Stat(c.Path); os.IsNotExist(err) {
		return c.WriteInitial()
	}
	data, err := os.ReadFile(c.Path)
	if err != nil {
		return err
	}
	c.parse(string(data))
	return nil
}

func (c *Checkpoint) AppendHistory(entry Entry) {
	c.History = append(c.History, entry)
	c.Iteration = entry.Iteration
}

func (c *Checkpoint) UpdateFromPlan(completed, inProgress, blockers, nextSteps []string) {
	if completed != nil {
		c.Completed = completed
	}
	if inProgress != nil {
		c.InProgress = inProgress
	}
	if blockers != nil {
		c.Blockers = blockers
	}
	if nextSteps != nil {
		c.NextSteps = nextSteps
	}
}

func (c *Checkpoint) SetPhase(phase string) {
	if phase != "" {
		c.Phase = phase
	}
}

func (c *Checkpoint) AddBlocker(blocker string) {
	for _, b := range c.Blockers {
		if b == blocker {
			return
		}
	}
	c.Blockers = append(c.Blockers, blocker)
}

func (c *Checkpoint) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.Path, []byte(c.render()), 0o644)
}

func (c *Checkpoint) WriteInitial() error {
	c.Phase = "bootstrap"
	c.Completed = nil
	c.InProgress = []string{"Initialize project scaffold and agentic loop"}
	c.Blockers = nil
	c.NextSteps = []string{
		"Define requirements and evaluation criteria",
		"Create initial project structure",
		"Implement core functionality",
		"Verify end-to-end against the objective",
	}
	return c.Save()
}

func (c *Checkpoint) render() string {
	now := time.Now().UTC().Format("2006-01-02 15:04 UTC")
	var b strings.Builder

	fmt.Fprintf(&b, "# Goloop — Checkpoint\n\n")
	fmt.Fprintf(&b, "_Last updated: %s_\n\n", now)
	fmt.Fprintf(&b, "## Goal\n\n%s\n\n", strings.TrimSpace(c.Goal))
	fmt.Fprintf(&b, "**Phase:** %s  \n", c.Phase)
	fmt.Fprintf(&b, "**Iteration:** %d\n\n", c.Iteration)

	writeList(&b, "Completed", c.Completed, "_(none yet)_")
	writeList(&b, "In Progress", c.InProgress, "_(none)_")
	writeList(&b, "Blockers", c.Blockers, "_(none)_")
	writeList(&b, "Next Steps", c.NextSteps, "_(none)_")

	if len(c.History) > 0 {
		b.WriteString("\n## Iteration Log\n\n")
		start := 0
		if len(c.History) > 20 {
			start = len(c.History) - 20
		}
		for _, entry := range c.History[start:] {
			fmt.Fprintf(&b, "### Iteration %d — %s (%s)\n\n", entry.Iteration, entry.Action, entry.Status)
			b.WriteString(strings.TrimSpace(entry.Summary))
			b.WriteString("\n")
			if entry.Notes != "" {
				fmt.Fprintf(&b, "\n_Notes: %s_\n", strings.TrimSpace(entry.Notes))
			}
			b.WriteString("\n")
		}
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}

func writeList(b *strings.Builder, title string, items []string, empty string) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if len(items) == 0 {
		fmt.Fprintf(b, "- %s\n\n", empty)
		return
	}
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
	b.WriteString("\n")
}

func (c *Checkpoint) parse(text string) {
	var section string
	for _, line := range strings.Split(text, "\n") {
		stripped := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(stripped, "**Phase:**"):
			c.Phase = strings.Trim(strings.TrimPrefix(stripped, "**Phase:**"), "* ")
		case strings.HasPrefix(stripped, "**Iteration:**"):
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(stripped, "**Iteration:**"))); err == nil {
				c.Iteration = n
			}
		case stripped == "## Completed":
			section = "completed"
			c.Completed = nil
		case stripped == "## In Progress":
			section = "in_progress"
			c.InProgress = nil
		case stripped == "## Blockers":
			section = "blockers"
			c.Blockers = nil
		case stripped == "## Next Steps":
			section = "next_steps"
			c.NextSteps = nil
		case strings.HasPrefix(stripped, "## "):
			section = ""
		case section != "" && strings.HasPrefix(stripped, "- ") && !strings.HasPrefix(stripped, "- _"):
			item := strings.TrimSpace(stripped[2:])
			switch section {
			case "completed":
				c.Completed = append(c.Completed, item)
			case "in_progress":
				c.InProgress = append(c.InProgress, item)
			case "blockers":
				c.Blockers = append(c.Blockers, item)
			case "next_steps":
				c.NextSteps = append(c.NextSteps, item)
			}
		}
	}
}
