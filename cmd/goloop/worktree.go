package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mantyx-io/goloop/internal/config"
)

// ensureWorktree creates (or reuses) a git worktree named after the branch
// under ../<repo>.goloop-worktrees/ and returns the directory the loop should
// run in. Autonomous workers run with permissions bypassed, so an isolated
// checkout keeps them away from the user's working copy.
func ensureWorktree(absRoot, branch string) (string, error) {
	top, err := gitOutput(absRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("--worktree requires a git repository: %w", err)
	}
	repoRoot := strings.TrimSpace(top)

	// Preserve the position inside the repo when running from a subdirectory.
	rel, err := filepath.Rel(repoRoot, absRoot)
	if err != nil {
		return "", err
	}

	dirName := strings.ReplaceAll(branch, string(os.PathSeparator), "-")
	dirName = strings.ReplaceAll(dirName, "/", "-")
	wtPath := filepath.Join(filepath.Dir(repoRoot), filepath.Base(repoRoot)+".goloop-worktrees", dirName)

	if _, err := os.Stat(wtPath); err == nil {
		if _, err := gitOutput(wtPath, "rev-parse", "--is-inside-work-tree"); err != nil {
			return "", fmt.Errorf("%s exists but is not a git worktree", wtPath)
		}
		return runDirInWorktree(wtPath, rel, absRoot), nil
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return "", err
	}

	// Reuse the branch when it already exists, otherwise create it here.
	if _, err := gitOutput(repoRoot, "rev-parse", "--verify", "refs/heads/"+branch); err == nil {
		if _, err := gitOutput(repoRoot, "worktree", "add", wtPath, branch); err != nil {
			return "", err
		}
	} else {
		if _, err := gitOutput(repoRoot, "worktree", "add", "-b", branch, wtPath); err != nil {
			return "", err
		}
	}

	return runDirInWorktree(wtPath, rel, absRoot), nil
}

// runDirInWorktree maps the original target directory into the worktree and
// carries over the project config when it isn't tracked by git.
func runDirInWorktree(wtPath, rel, absRoot string) string {
	runDir := filepath.Join(wtPath, rel)
	if config.IsInitialized(absRoot) && !config.IsInitialized(runDir) {
		if err := copyFile(config.LocalConfigPath(absRoot), config.LocalConfigPath(runDir)); err == nil {
			fmt.Fprintf(os.Stderr, "Copied project config into worktree (not tracked by git).\n")
		}
	}
	return runDir
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
