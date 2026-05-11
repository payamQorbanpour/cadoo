package config

import (
	"path/filepath"
	"testing"
)

func TestLoadFileMissingReturnsDefault(t *testing.T) {
	got, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != Default().Model {
		t.Fatalf("expected default model, got %q", got.Model)
	}
}

func TestLoadFileParsesFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".cadoo.yaml")
	if err := writeFile(path, `
model: claude-opus-4-7
auto:
  review: on_sync
review:
  severity_threshold: block
  max_comments: 5
checks:
  - name: example
    prompt: do not panic
    severity: warn
`); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "claude-opus-4-7" {
		t.Errorf("model: %q", got.Model)
	}
	if got.Auto["review"] != "on_sync" {
		t.Errorf("auto.review: %q", got.Auto["review"])
	}
	if got.Review.SeverityThreshold != "block" {
		t.Errorf("severity_threshold: %q", got.Review.SeverityThreshold)
	}
	if got.Review.MaxComments != 5 {
		t.Errorf("max_comments: %d", got.Review.MaxComments)
	}
	if len(got.Checks) != 1 || got.Checks[0].Name != "example" {
		t.Errorf("checks: %+v", got.Checks)
	}
}
