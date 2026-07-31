package goals

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mantyx-io/goloop/internal/config"
)

// Goal is a saved objective in the goals library.
type Goal struct {
	Slug    string
	Title   string
	Preview string
}

// Selection is the goal chosen for a goloop start run.
type Selection struct {
	Slug  string
	Text  string
	Saved bool
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// GoalsDir returns <project>/.goloop/goals.
func GoalsDir(projectRoot string) string {
	return filepath.Join(config.ProjectGoloopDir(projectRoot), "goals")
}

// ValidateSlug checks that slug matches [a-z0-9-]+.
func ValidateSlug(slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("goal slug is required")
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("invalid goal slug %q: use lowercase letters, numbers, and hyphens", slug)
	}
	return nil
}

// SlugFromFilename derives a slug from a .md filename.
func SlugFromFilename(name string) (string, error) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	if err := ValidateSlug(base); err != nil {
		return "", err
	}
	return base, nil
}

// SanitizeSlug converts arbitrary text into a valid slug.
func SanitizeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "goal"
	}
	return s
}

func goalFilePath(projectRoot, slug string) string {
	return filepath.Join(GoalsDir(projectRoot), slug+".md")
}

func stateDir(projectRoot, slug string) string {
	return filepath.Join(GoalsDir(projectRoot), slug)
}

// StatePaths returns per-goal checkpoint, user context, and output paths.
func StatePaths(projectRoot, slug string) (checkpoint, userContext, outputDir string) {
	dir := stateDir(projectRoot, slug)
	return filepath.Join(dir, "checkpoint.md"),
		filepath.Join(dir, "user_context.md"),
		filepath.Join(dir, "output")
}

// List scans .goloop/goals/*.md and returns saved goals sorted by slug.
func List(projectRoot string) ([]Goal, error) {
	dir := GoalsDir(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []Goal
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		slug, err := SlugFromFilename(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		title, preview := parseGoalContent(string(data))
		out = append(out, Goal{Slug: slug, Title: title, Preview: preview})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func parseGoalContent(text string) (title, preview string) {
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "# ") {
		title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "# "))
		if len(lines) > 1 {
			preview = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	} else {
		preview = text
		title = preview
	}
	if title == "" {
		title = preview
	}
	if len(preview) > 80 {
		preview = preview[:79] + "…"
	}
	if len(title) > 80 {
		title = title[:79] + "…"
	}
	return title, preview
}

// Read returns the full text of a saved goal.
func Read(projectRoot, slug string) (string, error) {
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	data, err := os.ReadFile(goalFilePath(projectRoot, slug))
	if err != nil {
		return "", fmt.Errorf("read goal %q: %w", slug, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Save writes goals/<slug>.md.
func Save(projectRoot, slug, text string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("goal text is required")
	}
	dir := GoalsDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(goalFilePath(projectRoot, slug), []byte(text+"\n"), 0o644)
}

// ReadFile reads goal text from any file path and derives a slug from the basename.
func ReadFile(path string) (slug, text string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	slug, err = SlugFromFilename(path)
	if err != nil {
		slug = SanitizeSlug(filepath.Base(path))
	}
	return slug, strings.TrimSpace(string(data)), nil
}
