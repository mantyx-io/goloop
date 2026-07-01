package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		if _, err := run(dir, args...); err != nil {
			t.Skipf("git unavailable: %v", err)
		}
	}
	return dir
}

func TestIsRepo(t *testing.T) {
	dir := initRepo(t)
	if !IsRepo(dir) {
		t.Fatal("expected repo")
	}
	if IsRepo(t.TempDir()) {
		t.Fatal("expected non-repo")
	}
}

func TestCommitAllAndChangeSummary(t *testing.T) {
	dir := initRepo(t)

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := ChangeSummary(dir)
	if summary == "" || summary == "(working tree clean)" {
		t.Fatalf("expected dirty summary, got %q", summary)
	}

	committed, err := CommitAll(dir, "first")
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected a commit")
	}

	if got := ChangeSummary(dir); got != "(working tree clean)" {
		t.Fatalf("expected clean summary, got %q", got)
	}

	committed, err = CommitAll(dir, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		t.Fatal("expected no commit on clean tree")
	}
}
