// Package shellcheck wraps shellcheck as an analysis.Linter.
package shellcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
)

// Image is the default container image.
const Image = "koalaman/shellcheck-alpine:stable"

// Linter wraps shellcheck.
type Linter struct {
	Image string
}

// Name implements analysis.Linter.
func (Linter) Name() string { return "shellcheck" }

// Languages implements analysis.Linter.
func (Linter) Languages() []string { return []string{".sh", ".bash"} }

// Run implements analysis.Linter.
func (l Linter) Run(ctx context.Context, runner sandbox.Runner, ws analysis.Workspace, files []string) ([]analysis.Finding, error) {
	if len(files) == 0 {
		return nil, nil
	}
	img := l.Image
	if img == "" {
		img = Image
	}
	workdir := ws.ContainerPath
	if workdir == "" {
		workdir = "/workspace"
	}
	cmd := []string{"shellcheck", "-f", "json"}
	cmd = append(cmd, files...)
	res, err := runner.Run(ctx, sandbox.Spec{
		Image:       img,
		Mounts:      map[string]string{ws.HostPath: workdir},
		WorkDir:     workdir,
		Cmd:         cmd,
		Timeout:     time.Minute,
		NoNetwork:   true,
		MemoryLimit: "256m",
	})
	if err != nil {
		return nil, err
	}
	return ParseJSON(res.Stdout)
}

type shellcheckIssue struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	EndLine int    `json:"endLine"`
	Column  int    `json:"column"`
	Level   string `json:"level"` // info | warning | error | style
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ParseJSON converts shellcheck -f json output into Findings.
func ParseJSON(stdout []byte) ([]analysis.Finding, error) {
	if len(stdout) == 0 {
		return nil, nil
	}
	var issues []shellcheckIssue
	if err := json.Unmarshal(stdout, &issues); err != nil {
		return nil, fmt.Errorf("parse shellcheck json: %w", err)
	}
	out := make([]analysis.Finding, 0, len(issues))
	for _, iss := range issues {
		sev := analysis.SeverityWarning
		switch strings.ToLower(iss.Level) {
		case "error":
			sev = analysis.SeverityError
		case "info", "style":
			sev = analysis.SeverityNote
		}
		end := iss.EndLine
		if end == 0 {
			end = iss.Line
		}
		out = append(out, analysis.Finding{
			Linter:    "shellcheck",
			Rule:      fmt.Sprintf("SC%d", iss.Code),
			Message:   iss.Message,
			Severity:  sev,
			File:      iss.File,
			LineStart: iss.Line,
			LineEnd:   end,
			ColStart:  iss.Column,
		})
	}
	return out, nil
}
