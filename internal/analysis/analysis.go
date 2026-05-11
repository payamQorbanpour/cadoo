// Package analysis is Cadoo's static-analysis layer. The pattern
// is: linters narrow attention to suspicious lines; the LLM
// reasons about whether each suspicion is a real issue.
package analysis

import (
	"context"

	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
)

// Severity for linter findings. Maps to vcs.Severity at the orchestrator
// boundary; we keep our own type so analysis stays VCS-agnostic.
type Severity string

// Severity levels.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityNote    Severity = "note"
)

// Finding is one structured issue from a linter run.
type Finding struct {
	Linter    string // e.g. "golangci-lint", "ruff"
	Rule      string // linter-specific rule code (e.g. "errcheck", "E501")
	Severity  Severity
	File      string
	LineStart int
	LineEnd   int
	ColStart  int
	Message   string
}

// Workspace describes where a Linter can find source code, both on the host
// (for Mounts) and inside the sandbox container.
type Workspace struct {
	HostPath      string // path on the host
	ContainerPath string // path inside the container; defaults to /workspace
}

// Linter is one runnable static-analysis tool.
type Linter interface {
	// Name is a stable identifier (matches the Linter field in Finding).
	Name() string
	// Languages returns file extensions this linter handles (e.g. ".go", ".py").
	Languages() []string
	// Run executes the linter inside runner against the workspace, scoping to
	// files (paths relative to Workspace.ContainerPath). An empty files slice
	// means "lint everything the tool would by default".
	Run(ctx context.Context, runner sandbox.Runner, ws Workspace, files []string) ([]Finding, error)
}

// Registry holds the linters available to the orchestrator.
type Registry struct {
	byName map[string]Linter
	byExt  map[string][]Linter
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]Linter{}, byExt: map[string][]Linter{}}
}

// Register adds a linter; it is indexed by name and by every language it
// claims to handle.
func (r *Registry) Register(l Linter) {
	r.byName[l.Name()] = l
	for _, ext := range l.Languages() {
		r.byExt[ext] = append(r.byExt[ext], l)
	}
}

// Get returns the linter by name.
func (r *Registry) Get(name string) (Linter, bool) {
	l, ok := r.byName[name]
	return l, ok
}

// ForExt returns linters that handle a given file extension.
func (r *Registry) ForExt(ext string) []Linter {
	return r.byExt[ext]
}

// Names returns all registered linter names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for k := range r.byName {
		out = append(out, k)
	}
	return out
}
