package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FileReader provides read access to source files at a particular ref.
// Implementations typically wrap a VCS adapter's FetchFileFromRef call.
type FileReader interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
}

// ReadFileTool builds a "read_file" Tool. It returns up to maxLines lines of
// the requested file, optionally constrained to [line_start, line_end].
func ReadFileTool(reader FileReader) Tool {
	return Tool{
		Name:        "read_file",
		Description: "Read a file from the PR head. Returns up to 200 lines of content. Args: path (required), line_start (optional 1-based), line_end (optional 1-based).",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "file path relative to repo root"},
    "line_start": {"type": "integer"},
    "line_end": {"type": "integer"}
  },
  "required": ["path"]
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Path      string `json:"path"`
				LineStart int    `json:"line_start"`
				LineEnd   int    `json:"line_end"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if a.Path == "" {
				return "", fmt.Errorf("path required")
			}
			data, err := reader.ReadFile(ctx, a.Path)
			if err != nil {
				return "", err
			}
			return excerpt(string(data), a.LineStart, a.LineEnd, 200), nil
		},
	}
}

// GrepTool builds a "grep" Tool that searches for a substring across the
// supplied set of files. Returns up to 30 hits.
func GrepTool(reader FileReader, files []string) Tool {
	return Tool{
		Name:        "grep",
		Description: "Search for a substring across the PR's changed files. Returns up to 30 matches as 'path:line: content' lines.",
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "literal substring to search for"}
  },
  "required": ["pattern"]
}`),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct {
				Pattern string `json:"pattern"`
			}
			if err := json.Unmarshal(args, &a); err != nil {
				return "", fmt.Errorf("parse args: %w", err)
			}
			if a.Pattern == "" {
				return "", fmt.Errorf("pattern required")
			}
			const maxHits = 30
			var b strings.Builder
			hits := 0
			for _, path := range files {
				if hits >= maxHits {
					break
				}
				data, err := reader.ReadFile(ctx, path)
				if err != nil {
					continue
				}
				for i, line := range strings.Split(string(data), "\n") {
					if strings.Contains(line, a.Pattern) {
						fmt.Fprintf(&b, "%s:%d: %s\n", path, i+1, line)
						hits++
						if hits >= maxHits {
							break
						}
					}
				}
			}
			if hits == 0 {
				return "no matches", nil
			}
			return b.String(), nil
		},
	}
}

// excerpt returns lines [start, end] from content, capped at maxLines.
func excerpt(content string, start, end, maxLines int) string {
	lines := strings.Split(content, "\n")
	if start < 1 {
		start = 1
	}
	if end < start {
		end = len(lines)
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		return fmt.Sprintf("(file has %d lines; line_start=%d is out of range)", len(lines), start)
	}
	if end-start+1 > maxLines {
		end = start + maxLines - 1
	}
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%4d: %s\n", i, lines[i-1])
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "... (truncated at line %d of %d)\n", end, len(lines))
	}
	return b.String()
}
