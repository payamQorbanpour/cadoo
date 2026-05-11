// Package golangci wraps golangci-lint as an analysis.Linter.
package golangci

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
)

// Image is the default container image. Override via Linter.Image.
const Image = "golangci/golangci-lint:latest-alpine"

// Linter wraps golangci-lint.
type Linter struct {
	Image string
}

// Name implements analysis.Linter.
func (Linter) Name() string { return "golangci-lint" }

// Languages implements analysis.Linter.
func (Linter) Languages() []string { return []string{".go"} }

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
	cmd := []string{"golangci-lint", "run", "--out-format", "json"}
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

type jsonOutput struct {
	Issues []struct {
		FromLinter string `json:"FromLinter"`
		Text       string `json:"Text"`
		Severity   string `json:"Severity"`
		Pos        struct {
			Filename string `json:"Filename"`
			Line     int    `json:"Line"`
			Column   int    `json:"Column"`
		} `json:"Pos"`
	} `json:"Issues"`
}

// ParseJSON converts golangci-lint's --out-format=json output into Findings.
// Exposed for testing without touching the runner.
func ParseJSON(stdout []byte) ([]analysis.Finding, error) {
	if len(stdout) == 0 {
		return nil, nil
	}
	var out jsonOutput
	if err := json.Unmarshal(stdout, &out); err != nil {
		return nil, fmt.Errorf("parse golangci-lint json: %w", err)
	}
	findings := make([]analysis.Finding, 0, len(out.Issues))
	for _, iss := range out.Issues {
		sev := analysis.SeverityWarning
		if iss.Severity == "error" {
			sev = analysis.SeverityError
		}
		findings = append(findings, analysis.Finding{
			Linter:    "golangci-lint",
			Rule:      iss.FromLinter,
			Message:   iss.Text,
			Severity:  sev,
			File:      iss.Pos.Filename,
			LineStart: iss.Pos.Line,
			LineEnd:   iss.Pos.Line,
			ColStart:  iss.Pos.Column,
		})
	}
	return findings, nil
}
