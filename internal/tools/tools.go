// Package tools defines the Tool interface every Cadoo command satisfies
// (review, describe, improve, ask, changelog, add_docs, ...) and the shared
// Input/Result types the orchestrator passes through.
package tools

import (
	"context"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/contextengine"
	"github.com/payamqorbanpour/cadoo/internal/issuetrackers"
	"github.com/payamqorbanpour/cadoo/internal/kb"
	"github.com/payamqorbanpour/cadoo/internal/learnings"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/slop"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// FileReader is an optional capability the orchestrator wires up when the
// VCS adapter exposes file fetch. Tools (notably /deep_review) use this to
// read content beyond what's in the diff.
type FileReader interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
}

// PriorFinding is a slim view of an inline comment Cadoo already posted on
// the current PR. The orchestrator pre-loads these so tools can include
// them in the prompt and discourage the model from restating known issues.
type PriorFinding struct {
	Tool      string
	File      string
	LineStart int
	LineEnd   int
	Severity  string
	Title     string
}

// Input is the shared payload every tool receives from the orchestrator.
type Input struct {
	PR     *vcs.PullRequest
	Files  []vcs.FileChange
	Packed contextengine.Compressed
	Config config.Repo
	LLM    llm.Provider
	Model  string
	// Args is the free-form trailing text from a slash command, e.g. for
	// "/ask why is X done this way?", Args == "why is X done this way?".
	Args string

	// Reader is non-nil when the VCS adapter supports file fetch by ref.
	Reader FileReader

	// Analysis is the union of structured findings from any linters the
	// orchestrator ran before dispatching the tool. Tools that surface
	// inline comments may incorporate these (with severity adjustments).
	Analysis []analysis.Finding

	// Slop is the cheap pre-LLM classification of the PR's quality. Tools
	// may short-circuit on high scores instead of paying for a full review.
	Slop *slop.Report

	// KBHits is the orchestrator's pre-fetched semantic-search hits from
	// the per-repo knowledge base. May be empty if the KB is not configured
	// or no relevant chunks were found.
	KBHits []kb.Hit

	// Learnings are accumulated team-preferred rules for the repo, drawn
	// from past accept/reject reactions. Tools should treat them as
	// authoritative guidance.
	Learnings []learnings.Rule

	// Issues are external tracker tickets the orchestrator extracted from
	// the PR title/body (Jira keys, Linear identifiers, etc.) and resolved
	// against the configured trackers.
	Issues []issuetrackers.Issue

	// LearningsStore lets write-back tools (/learn, /unlearn) record
	// reactions. May be nil when the knowledge layer isn't configured.
	LearningsStore *learnings.Store

	// RepoKey is the orchestrator's pre-computed "<provider>:<repo_full_name>"
	// composite. Tools should treat it as opaque and pass it back to stores
	// that key by repo (kb.Store, learnings.Store).
	RepoKey string

	// PriorFindings is everything Cadoo has already posted as inline
	// comments on this PR (across all prior dispatches). Tools surface it
	// in the prompt so the model doesn't restate known issues.
	PriorFindings []PriorFinding

	// IncrementalFiles holds the file changes since the last reviewed SHA
	// (fetched via vcs.DiffBetweener). Both IncrementalFiles and
	// IncrementalPacked are nil/empty on a first run, after a force-push, or
	// when the provider does not implement DiffBetweener. Inline-emitting
	// tools should prefer these over Files/Packed when IsIncrementalRun is
	// true; summary-only tools (describe, changelog) always use Files/Packed.
	IncrementalFiles []vcs.FileChange

	// IncrementalPacked is the packed context built from IncrementalFiles.
	// Nil when IncrementalFiles is nil or empty.
	IncrementalPacked contextengine.Compressed

	// IsIncrementalRun is true when the orchestrator has a valid
	// lastReviewedSHA and is limiting inline tools to the incremental
	// change set. Tools that emit inline comments should scope their
	// output to IncrementalFiles when this is true.
	IsIncrementalRun bool
}

// Result is what a tool returns. Empty fields are treated as no-ops by the
// orchestrator's apply step.
type Result struct {
	Summary        string              // posted as a top-level PR comment
	InlineComments []vcs.InlineComment // posted as a single PR review
	CheckRun       *vcs.CheckRun       // upserted on the head SHA
	CheckRuns      []vcs.CheckRun      // additional check runs (e.g. /check emits one per rule)
	EditPRBody     *string             // if non-nil, replace the PR body
}

// Tool is one Cadoo command.
type Tool interface {
	Name() string
	Run(ctx context.Context, in Input) (*Result, error)
}

// Registry holds the tools the orchestrator can dispatch to.
type Registry struct {
	m map[string]Tool
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{m: map[string]Tool{}} }

// Register adds a tool. Last writer wins.
func (r *Registry) Register(t Tool) { r.m[t.Name()] = t }

// Get returns the tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.m[name]
	return t, ok
}

// Names returns registered tool names (unspecified order).
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.m))
	for k := range r.m {
		out = append(out, k)
	}
	return out
}
