package tools

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// changedLineRanges parses a unified-diff patch and returns new-file
// line numbers of all + (added/changed) lines as sorted inclusive
// [start, end] ranges. Context lines advance the new-file counter,
// removed (-) lines do not, and anything before the first hunk header
// (file headers, index lines) is ignored. Returns nil when the patch
// adds no lines.
func changedLineRanges(patch string) [][2]int {
	var ranges [][2]int
	var newLine int
	inHunk := false

	for line := range strings.SplitSeq(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			if start, ok := parseHunkNewStart(line); ok {
				newLine = start
				inHunk = true
			}
		case !inHunk:
			// File headers / index lines before the first hunk: skip.
			continue
		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file" — metadata, no line advance.
			continue
		case strings.HasPrefix(line, "+"):
			ranges = appendLine(ranges, newLine)
			newLine++
		case strings.HasPrefix(line, "-"):
			// Removed line: present only in the old file, no new-file advance.
		default:
			// Context line (leading space) or blank line: advances new file.
			newLine++
		}
	}
	return ranges
}

// parseHunkNewStart extracts the new-file start line from a hunk header
// of the form "@@ -a,b +c,d @@" (or "@@ -a +c @@"), returning c.
func parseHunkNewStart(header string) (int, bool) {
	_, rest, ok := strings.Cut(header, "+")
	if !ok {
		return 0, false
	}
	end := strings.IndexAny(rest, " ,")
	if end >= 0 {
		rest = rest[:end]
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// appendLine records a single changed new-file line, extending the last
// range when contiguous and starting a new one otherwise.
func appendLine(ranges [][2]int, line int) [][2]int {
	if n := len(ranges); n > 0 && ranges[n-1][1] == line-1 {
		ranges[n-1][1] = line
		return ranges
	}
	return append(ranges, [2]int{line, line})
}

// InChangedLines reports whether line (1-based new-file) falls in any
// range. Line 0 (a file-level finding) always returns true.
func InChangedLines(ranges [][2]int, line int) bool {
	if line == 0 {
		return true
	}
	for _, r := range ranges {
		if line >= r[0] && line <= r[1] {
			return true
		}
	}
	return false
}

// BuildChangedMap indexes changed-line ranges by file path from packed
// files. Files with no added lines map to nil, so only their file-level
// findings survive InChangedLines.
func BuildChangedMap(files []vcs.FileChange) map[string][][2]int {
	m := make(map[string][][2]int, len(files))
	for _, f := range files {
		m[f.Path] = changedLineRanges(f.Patch)
	}
	return m
}

// scopeConstraintSection renders the "## Scope constraint" prompt block
// listing, per file, the exact new-file line numbers the model may flag.
// Returns "" when the diff adds no lines (nothing to constrain).
func scopeConstraintSection(files []vcs.FileChange) string {
	var b strings.Builder
	emitted := false
	for _, f := range files {
		ranges := changedLineRanges(f.Patch)
		if len(ranges) == 0 {
			continue
		}
		if !emitted {
			b.WriteString("## Scope constraint — ONLY flag lines marked `+`\n\n")
			b.WriteString("These are the exact new-file line numbers added or changed in this diff. ")
			b.WriteString("Restrict ALL findings and suggestions to these lines only. Context lines, ")
			b.WriteString("unchanged lines, and removed (`-`) lines are completely out of scope. ")
			b.WriteString("File-level findings (line_start=0) are always allowed.\n\n")
			emitted = true
		}
		fmt.Fprintf(&b, "- `%s`: %s\n", f.Path, formatRanges(ranges))
	}
	if !emitted {
		return ""
	}
	b.WriteString("\n")
	return b.String()
}

// formatRanges renders ranges as a comma-separated list, e.g. "12–14, 27, 35–40".
func formatRanges(ranges [][2]int) string {
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r[0] == r[1] {
			parts = append(parts, strconv.Itoa(r[0]))
		} else {
			parts = append(parts, fmt.Sprintf("%d–%d", r[0], r[1]))
		}
	}
	return strings.Join(parts, ", ")
}
