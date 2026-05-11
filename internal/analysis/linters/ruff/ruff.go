// Package ruff wraps the ruff Python linter as an analysis.Linter.
package ruff

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
)

// Image is the default container image. Override via Linter.Image.
const Image = "ghcr.io/astral-sh/ruff:latest"

// Linter wraps ruff.
type Linter struct {
	Image string
}

// Name implements analysis.Linter.
func (Linter) Name() string { return "ruff" }

// Languages implements analysis.Linter.
func (Linter) Languages() []string { return []string{".py"} }

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
	cmd := []string{"ruff", "check", "--output-format", "json"}
	cmd = append(cmd, files...)

	res, err := runner.Run(ctx, sandbox.Spec{
		Image:       img,
		Mounts:      map[string]string{ws.HostPath: workdir},
		WorkDir:     workdir,
		Cmd:         cmd,
		Timeout:     time.Minute,
		NoNetwork:   true,
		MemoryLimit: "512m",
	})
	if err != nil {
		return nil, err
	}
	return ParseJSON(res.Stdout)
}

type ruffIssue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Filename string `json:"filename"`
	Location struct {
		Row    int `json:"row"`
		Column int `json:"column"`
	} `json:"location"`
	EndLocation struct {
		Row    int `json:"row"`
		Column int `json:"column"`
	} `json:"end_location"`
}

// ParseJSON converts ruff's --output-format=json into Findings.
func ParseJSON(stdout []byte) ([]analysis.Finding, error) {
	if len(stdout) == 0 {
		return nil, nil
	}
	var issues []ruffIssue
	if err := json.Unmarshal(stdout, &issues); err != nil {
		return nil, fmt.Errorf("parse ruff json: %w", err)
	}
	findings := make([]analysis.Finding, 0, len(issues))
	for _, iss := range issues {
		end := iss.EndLocation.Row
		if end == 0 {
			end = iss.Location.Row
		}
		findings = append(findings, analysis.Finding{
			Linter:    "ruff",
			Rule:      iss.Code,
			Message:   iss.Message,
			Severity:  analysis.SeverityWarning,
			File:      iss.Filename,
			LineStart: iss.Location.Row,
			LineEnd:   end,
			ColStart:  iss.Location.Column,
		})
	}
	return findings, nil
}
