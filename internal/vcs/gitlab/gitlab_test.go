package gitlab

import (
	"testing"

	glab "gitlab.com/gitlab-org/api/client-go"
)

func TestParseHunkHeader(t *testing.T) {
	cases := []struct {
		in           string
		oldStart     int
		newStart     int
		ok           bool
	}{
		{"@@ -1,3 +1,4 @@", 1, 1, true},
		{"@@ -12,0 +13,5 @@ func foo()", 12, 13, true},
		{"@@ -0,0 +1,10 @@", 0, 1, true},
		{"@@ -5 +5 @@", 5, 5, true},
		{"not a header", 0, 0, false},
		{"@@ garbage @@", 0, 0, false},
	}
	for _, tc := range cases {
		o, n, ok := parseHunkHeader(tc.in)
		if ok != tc.ok || o != tc.oldStart || n != tc.newStart {
			t.Errorf("parseHunkHeader(%q) = (%d,%d,%v); want (%d,%d,%v)",
				tc.in, o, n, ok, tc.oldStart, tc.newStart, tc.ok)
		}
	}
}

func TestIndexDiffs_AddedAndContext(t *testing.T) {
	// Two hunks: lines 10-12 unchanged, 13 added; later hunk adds line 50.
	diff := "@@ -10,3 +10,4 @@\n" +
		" line10\n" +
		" line11\n" +
		" line12\n" +
		"+line13_added\n" +
		"@@ -49,1 +50,2 @@\n" +
		" ctx49\n" +
		"+line51_added\n"

	idx := indexDiffs([]*glab.MergeRequestDiff{{
		NewPath: "a/b.go",
		OldPath: "a/b.go",
		Diff:    diff,
	}})

	a, ok := idx.lookup("a/b.go", 13)
	if !ok || a.context || a.newLine != 13 || a.oldLine != 0 {
		t.Errorf("added line 13: got %+v ok=%v; want newLine=13, context=false", a, ok)
	}
	a, ok = idx.lookup("a/b.go", 11)
	if !ok || !a.context || a.newLine != 11 || a.oldLine != 11 {
		t.Errorf("context line 11: got %+v ok=%v; want newLine=11 oldLine=11 context=true", a, ok)
	}
	a, ok = idx.lookup("a/b.go", 51)
	if !ok || a.context || a.newLine != 51 {
		t.Errorf("added line 51 after second hunk: got %+v ok=%v", a, ok)
	}
	if _, ok := idx.lookup("a/b.go", 1); ok {
		t.Errorf("line 1 should be outside diff")
	}
	if _, ok := idx.lookup("other.go", 13); ok {
		t.Errorf("unknown file should miss")
	}
}

func TestIndexDiffs_DeletionsHaveNoNewLine(t *testing.T) {
	// A pure deletion: old lines 5-6 removed, no additions. There is no
	// new_line you can address them by, so the index should not produce an
	// entry for them via new-line lookup.
	diff := "@@ -5,2 +4,0 @@\n" +
		"-removed_a\n" +
		"-removed_b\n"
	idx := indexDiffs([]*glab.MergeRequestDiff{{
		NewPath: "x.go",
		OldPath: "x.go",
		Diff:    diff,
	}})
	if _, ok := idx.lookup("x.go", 5); ok {
		t.Errorf("deletion at old line 5 must not be reachable via new_line")
	}
}

func TestIndexDiffs_Rename(t *testing.T) {
	diff := "@@ -1,1 +1,2 @@\n" +
		" same\n" +
		"+added\n"
	idx := indexDiffs([]*glab.MergeRequestDiff{{
		NewPath: "new/path.go",
		OldPath: "old/path.go",
		Diff:    diff,
	}})
	a, ok := idx.lookup("new/path.go", 2)
	if !ok || a.newPath != "new/path.go" || a.oldPath != "old/path.go" {
		t.Errorf("rename anchor: got %+v ok=%v", a, ok)
	}
}
