package tools

import (
	"reflect"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestChangedLineRanges(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  [][2]int
	}{
		{
			name:  "empty patch",
			patch: "",
			want:  nil,
		},
		{
			name:  "stub non-diff text has no added lines",
			patch: "d",
			want:  nil,
		},
		{
			name:  "single hunk all added contiguous",
			patch: "@@ -0,0 +1,3 @@\n+line1\n+line2\n+line3",
			want:  [][2]int{{1, 3}},
		},
		{
			name:  "context lines advance new-file counter",
			patch: "@@ -1,4 +1,5 @@\n ctx1\n+added2\n ctx3\n ctx4\n+added5",
			want:  [][2]int{{2, 2}, {5, 5}},
		},
		{
			name:  "removed lines do not advance new-file counter",
			patch: "@@ -1,3 +1,2 @@\n ctx1\n-removed\n ctx3",
			want:  nil,
		},
		{
			name:  "add after removal lands on correct new-file line",
			patch: "@@ -1,3 +1,3 @@\n ctx1\n-removed\n+added2\n ctx3",
			want:  [][2]int{{2, 2}},
		},
		{
			name:  "multiple hunks each offset by its own header",
			patch: "@@ -1,2 +1,3 @@\n a\n+b\n c\n@@ -10,2 +11,3 @@\n x\n+y\n z",
			want:  [][2]int{{2, 2}, {12, 12}},
		},
		{
			name:  "single-line hunk header without count",
			patch: "@@ -5 +5 @@\n+only",
			want:  [][2]int{{5, 5}},
		},
		{
			name:  "file headers before first hunk are ignored",
			patch: "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -0,0 +1,2 @@\n+a\n+b",
			want:  [][2]int{{1, 2}},
		},
		{
			name:  "no-newline marker does not advance counter",
			patch: "@@ -1,1 +1,2 @@\n a\n+b\n\\ No newline at end of file",
			want:  [][2]int{{2, 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changedLineRanges(tt.patch)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("changedLineRanges(%q) = %v, want %v", tt.patch, got, tt.want)
			}
		})
	}
}

func TestInChangedLines(t *testing.T) {
	ranges := [][2]int{{2, 2}, {5, 8}}
	tests := []struct {
		name   string
		ranges [][2]int
		line   int
		want   bool
	}{
		{"file-level finding always allowed", ranges, 0, true},
		{"file-level allowed even with nil ranges", nil, 0, true},
		{"exact single-line match", ranges, 2, true},
		{"range start", ranges, 5, true},
		{"range middle", ranges, 6, true},
		{"range end", ranges, 8, true},
		{"between ranges", ranges, 3, false},
		{"after last range", ranges, 9, false},
		{"before first range", ranges, 1, false},
		{"non-file-level with nil ranges is out of scope", nil, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InChangedLines(tt.ranges, tt.line); got != tt.want {
				t.Errorf("InChangedLines(%v, %d) = %v, want %v", tt.ranges, tt.line, got, tt.want)
			}
		})
	}
}

func TestBuildChangedMap(t *testing.T) {
	files := []vcs.FileChange{
		{Path: "a.go", Patch: "@@ -0,0 +1,2 @@\n+x\n+y"},
		{Path: "b.go", Patch: "@@ -1,2 +1,1 @@\n a\n-gone"},
	}
	got := BuildChangedMap(files)
	if !reflect.DeepEqual(got["a.go"], [][2]int{{1, 2}}) {
		t.Errorf("a.go ranges = %v, want [[1 2]]", got["a.go"])
	}
	// b.go has only deletions — no added lines to flag.
	if got["b.go"] != nil {
		t.Errorf("b.go ranges = %v, want nil", got["b.go"])
	}
	// A file absent from the diff maps to nil → only file-level findings allowed.
	if !InChangedLines(got["missing.go"], 0) {
		t.Error("file-level finding for absent file should be allowed")
	}
	if InChangedLines(got["missing.go"], 7) {
		t.Error("line-level finding for absent file should be dropped")
	}
}
