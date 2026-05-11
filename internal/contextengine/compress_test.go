package contextengine

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestCompressFilterAndPack(t *testing.T) {
	files := []vcs.FileChange{
		{Path: "vendor/big.go", Patch: strings.Repeat("x", 4000)},
		{Path: "src/small.go", Patch: "diff --git ..."},
		{Path: "src/large.go", Patch: strings.Repeat("y", 4000)},
		{Path: "image.png", IsBinary: true, Patch: ""},
	}
	got := Compress(files, CompressOptions{
		MaxTokens:    1500,
		PerFileMax:   600,
		IncludePaths: []string{"src/**"},
		ExcludePaths: []string{"vendor/**"},
	})

	paths := map[string]bool{}
	for _, f := range got.Files {
		paths[f.Path] = true
	}
	if !paths["src/small.go"] {
		t.Errorf("expected src/small.go in packed: %+v", paths)
	}
	if !paths["src/large.go"] {
		t.Errorf("expected src/large.go (truncated) in packed: %+v", paths)
	}
	if paths["vendor/big.go"] {
		t.Errorf("vendor/big.go should be excluded by glob")
	}
	if paths["image.png"] {
		t.Errorf("image.png should be skipped (binary)")
	}
	foundTrunc := false
	for _, p := range got.Truncated {
		if p == "src/large.go" {
			foundTrunc = true
		}
	}
	if !foundTrunc {
		t.Errorf("expected src/large.go to be truncated, got %v", got.Truncated)
	}
	for _, f := range got.Files {
		if f.Path == "src/large.go" {
			if !strings.Contains(f.Patch, "truncated by Cadoo") {
				t.Errorf("truncated patch missing marker: %q", f.Patch)
			}
		}
	}
}

func TestCompressSortsSmallFirst(t *testing.T) {
	files := []vcs.FileChange{
		{Path: "big.go", Patch: strings.Repeat("a", 4000)},
		{Path: "tiny.go", Patch: "x"},
	}
	got := Compress(files, CompressOptions{MaxTokens: 100_000})
	if got.Files[0].Path != "tiny.go" {
		t.Errorf("expected tiny first, got %q", got.Files[0].Path)
	}
}
