// Package semgrep wraps semgrep as an analysis.Linter — primarily for
// security and anti-pattern detection across many languages.
package semgrep

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
)

// Image is the default container image. Override via Linter.Image.
const Image = "returntocorp/semgrep:latest"

// Linter wraps semgrep.
type Linter struct {
	Image string
	// Config is the rule pack to run. Defaults to "p/default" which runs
	// Semgrep's curated baseline.
	Config string
}

// Name implements analysis.Linter.
func (Linter) Name() string { return "semgrep" }

// Languages implements analysis.Linter. Semgrep handles many languages; we
// list the most common to advertise via Registry.ForExt.
func (Linter) Languages() []string {
	return []string{".py", ".js", ".jsx", ".ts", ".tsx", ".go", ".rb", ".java", ".php", ".c", ".cpp", ".rs"}
}

// Run implements analysis.Linter.
func (l Linter) Run(ctx context.Context, runner sandbox.Runner, ws analysis.Workspace, files []string) ([]analysis.Finding, error) {
	img := l.Image
	if img == "" {
		img = Image
	}
	cfg := l.Config
	if cfg == "" {
		cfg = "p/default"
	}
	workdir := ws.ContainerPath
	if workdir == "" {
		workdir = "/workspace"
	}
	cmd := []string{"semgrep", "--config", cfg, "--json", "--quiet", "--error", "--no-git-ignore"}
	cmd = append(cmd, files...)
	res, err := runner.Run(ctx, sandbox.Spec{
		Image:       img,
		Mounts:      map[string]string{ws.HostPath: workdir},
		WorkDir:     workdir,
		Cmd:         cmd,
		Timeout:     5 * time.Minute,
		NoNetwork:   true, // assumes rule pack baked into image; remove for online configs
		MemoryLimit: "2g",
	})
	if err != nil {
		return nil, err
	}
	return ParseJSON(res.Stdout)
}

type semgrepResult struct {
	CheckID string `json:"check_id"`
	Path    string `json:"path"`
	Start   struct {
		Line int `json:"line"`
		Col  int `json:"col"`
	} `json:"start"`
	End struct {
		Line int `json:"line"`
	} `json:"end"`
	Extra struct {
		Message  string `json:"message"`
		Severity string `json:"severity"` // INFO|WARNING|ERROR
	} `json:"extra"`
}

type semgrepOutput struct {
	Results []semgrepResult `json:"results"`
}

// ParseJSON converts semgrep --json output into Findings.
func ParseJSON(stdout []byte) ([]analysis.Finding, error) {
	if len(stdout) == 0 {
		return nil, nil
	}
	var out semgrepOutput
	if err := json.Unmarshal(stdout, &out); err != nil {
		return nil, fmt.Errorf("parse semgrep json: %w", err)
	}
	findings := make([]analysis.Finding, 0, len(out.Results))
	for _, r := range out.Results {
		sev := analysis.SeverityWarning
		switch strings.ToUpper(r.Extra.Severity) {
		case "ERROR":
			sev = analysis.SeverityError
		case "INFO":
			sev = analysis.SeverityNote
		}
		findings = append(findings, analysis.Finding{
			Linter:    "semgrep",
			Rule:      r.CheckID,
			Message:   r.Extra.Message,
			Severity:  sev,
			File:      r.Path,
			LineStart: r.Start.Line,
			LineEnd:   r.End.Line,
			ColStart:  r.Start.Col,
		})
	}
	return findings, nil
}
