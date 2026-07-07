package goals

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSlug(t *testing.T) {
	if err := ValidateSlug("todo-cli"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSlug(""); err == nil {
		t.Fatal("expected error for empty slug")
	}
	if err := ValidateSlug("Bad_Slug"); err == nil {
		t.Fatal("expected error for invalid slug")
	}
}

func TestSanitizeSlug(t *testing.T) {
	if got := SanitizeSlug("Build a Todo CLI!"); got != "build-a-todo-cli" {
		t.Fatalf("got %q", got)
	}
}

func TestSaveListRead(t *testing.T) {
	dir := t.TempDir()
	text := "# Build a todo CLI\n\nImplement add and list commands."
	if err := Save(dir, "todo-cli", text); err != nil {
		t.Fatal(err)
	}

	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Slug != "todo-cli" {
		t.Fatalf("list: %+v", items)
	}
	if items[0].Title != "Build a todo CLI" {
		t.Fatalf("title: %q", items[0].Title)
	}

	got, err := Read(dir, "todo-cli")
	if err != nil {
		t.Fatal(err)
	}
	if got != text {
		t.Fatalf("read: %q", got)
	}
}

func TestStatePaths(t *testing.T) {
	dir := t.TempDir()
	ckpt, uctx, out := StatePaths(dir, "todo-cli")
	wantBase := filepath.Join(GoalsDir(dir), "todo-cli")
	if ckpt != filepath.Join(wantBase, "checkpoint.md") {
		t.Fatalf("checkpoint: %s", ckpt)
	}
	if uctx != filepath.Join(wantBase, "user_context.md") {
		t.Fatalf("user context: %s", uctx)
	}
	if out != filepath.Join(wantBase, "output") {
		t.Fatalf("output: %s", out)
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my-goal.md")
	if err := os.WriteFile(path, []byte("Do the thing"), 0o644); err != nil {
		t.Fatal(err)
	}
	slug, text, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if slug != "my-goal" || text != "Do the thing" {
		t.Fatalf("slug=%q text=%q", slug, text)
	}
}

func TestSlugFromFilenameInvalid(t *testing.T) {
	if _, err := SlugFromFilename("Bad Name.md"); err == nil {
		t.Fatal("expected error")
	}
}
