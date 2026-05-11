// Package eslint wraps eslint as an analysis.Linter.
package eslint

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
)

// Image is the default container image. Override via Linter.Image.
const Image = "node:20-alpine"

// Linter wraps eslint.
type Linter struct {
	Image string
}

// Name implements analysis.Linter.
func (Linter) Name() string { return "eslint" }

// Languages implements analysis.Linter.
func (Linter) Languages() []string {
	return []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"}
}

// Run implements analysis.Linter.
func (l Linter) Run(ctx context.Context, runner sandbox.Runner, ws analysis.Workspace, files []string) ([]analysis.Finding, error) {
	img := l.Image
	if img == "" {
		img = Image
	}
	workdir := ws.ContainerPath
	if workdir == "" {
		workdir = "/workspace"
	}
	cmd := []string{"eslint", "--format", "json", "--no-error-on-unmatched-pattern"}
	cmd = append(cmd, files...)
	res, err := runner.Run(ctx, sandbox.Spec{
		Image:       img,
		Mounts:      map[string]string{ws.HostPath: workdir},
		WorkDir:     workdir,
		Cmd:         cmd,
		Timeout:     2 * time.Minute,
		NoNetwork:   true,
		MemoryLimit: "1g",
	})
	if err != nil {
		return nil, err
	}
	return ParseJSON(res.Stdout)
}

type fileResult struct {
	FilePath string `json:"filePath"`
	Messages []struct {
		RuleID   string `json:"ruleId"`
		Severity int    `json:"severity"` // 1=warn, 2=error
		Message  string `json:"message"`
		Line     int    `json:"line"`
		EndLine  int    `json:"endLine"`
		Column   int    `json:"column"`
	} `json:"messages"`
}

// ParseJSON converts ESLint's --format=json output into Findings.
func ParseJSON(stdout []byte) ([]analysis.Finding, error) {
	if len(stdout) == 0 {
		return nil, nil
	}
	var files []fileResult
	if err := json.Unmarshal(stdout, &files); err != nil {
		return nil, fmt.Errorf("parse eslint json: %w", err)
	}
	var out []analysis.Finding
	for _, f := range files {
		for _, m := range f.Messages {
			sev := analysis.SeverityWarning
			if m.Severity == 2 {
				sev = analysis.SeverityError
			}
			end := m.EndLine
			if end == 0 {
				end = m.Line
			}
			out = append(out, analysis.Finding{
				Linter:    "eslint",
				Rule:      m.RuleID,
				Message:   m.Message,
				Severity:  sev,
				File:      f.FilePath,
				LineStart: m.Line,
				LineEnd:   end,
				ColStart:  m.Column,
			})
		}
	}
	return out, nil
}
