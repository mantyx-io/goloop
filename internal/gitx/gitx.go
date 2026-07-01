package gitx

import (
	"fmt"
	"os/exec"
	"strings"
)

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
	}
	return text, nil
}

// IsRepo reports whether dir is inside a git working tree.
func IsRepo(dir string) bool {
	out, err := run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// ChangeSummary describes uncommitted changes: porcelain status plus a
// diffstat. Returns "" when dir is not a repo or git fails.
func ChangeSummary(dir string) string {
	status, err := run(dir, "status", "--short")
	if err != nil {
		return ""
	}
	if status == "" {
		return "(working tree clean)"
	}
	summary := status
	// Diffstat fails on a repo with no commits yet; the status alone is fine then.
	if diff, err := run(dir, "diff", "--stat", "HEAD"); err == nil && diff != "" {
		summary += "\n\n" + diff
	}
	if len(summary) > 3000 {
		summary = summary[:3000] + "\n... [truncated]"
	}
	return summary
}

// CommitAll stages everything and commits. Returns false when there was
// nothing to commit.
func CommitAll(dir, message string) (bool, error) {
	if _, err := run(dir, "add", "-A"); err != nil {
		return false, err
	}
	staged, err := run(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if staged == "" {
		return false, nil
	}
	if _, err := run(dir, "commit", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}
