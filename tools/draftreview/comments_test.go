package draftreview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCommentsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "comments.json")
	content := `[
  {"path":"b.go","line":2,"side":"RIGHT","body":"second"},
  {"path":"a.go","line":1,"side":"RIGHT","body":"first"}
]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write comments file: %v", err)
	}

	comments, err := loadCommentsFile(path)
	if err != nil {
		t.Fatalf("loadCommentsFile returned error: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("unexpected comment count %d", len(comments))
	}
	if comments[0].Path != "a.go" {
		t.Fatalf("expected normalized ordering, got first path %q", comments[0].Path)
	}
}

func TestLoadCommentsFileRejectsUnknownField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "comments.json")
	content := `[{"path":"a.go","line":1,"side":"RIGHT","body":"first","position":3}]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write comments file: %v", err)
	}

	if _, err := loadCommentsFile(path); err == nil {
		t.Fatal("expected error for unknown field")
	}
}
