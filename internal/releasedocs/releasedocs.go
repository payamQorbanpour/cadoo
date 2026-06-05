// Package releasedocs is the parallel subsystem for automatically generating
// and publishing release artifacts (changelog, release notes, blog, API docs)
// after a customer cuts a release. It is independent of the review pipeline
// (internal/orchestrator) and must not import tools.* or orchestrator.*
// (D-01). Downstream plans implement against the interfaces declared here.
package releasedocs

import (
	"context"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// ArtifactKind identifies the type of release artifact a generator produces.
type ArtifactKind string

// Recognized artifact kinds.
const (
	// KindChangelog identifies a machine-formatted CHANGELOG.md-style artifact.
	KindChangelog ArtifactKind = "changelog"
	// KindReleaseNotes identifies a human-readable LLM-polished release narrative.
	KindReleaseNotes ArtifactKind = "release_notes"
)

// PublishTarget identifies where a publisher delivers artifacts.
type PublishTarget string

// Recognized publish targets.
const (
	// TargetReleaseBody delivers artifact content by splicing it into the
	// VCS release body.
	TargetReleaseBody PublishTarget = "release_body"
	// TargetChangelogPR delivers artifact content by opening or updating a
	// pull-request that writes CHANGELOG.md.
	TargetChangelogPR PublishTarget = "changelog_pr"
)

// SemverBump classifies the magnitude of a release version increment.
type SemverBump string

// Recognized semver bump magnitudes.
const (
	// BumpMajor indicates a backward-incompatible API change (BREAKING).
	BumpMajor SemverBump = "major"
	// BumpMinor indicates new functionality added in a backward-compatible way.
	BumpMinor SemverBump = "minor"
	// BumpPatch indicates backward-compatible bug fixes.
	BumpPatch SemverBump = "patch"
	// BumpNone indicates no functional change (e.g. docs-only release).
	BumpNone SemverBump = "none"
)

// Artifact is the output of a Generator: a typed blob of content plus any
// metadata a Publisher needs to route and splice the content correctly.
type Artifact struct {
	// Kind identifies which generator produced this artifact.
	Kind ArtifactKind
	// Content is the rendered artifact bytes (Markdown for all current kinds).
	Content []byte
}

// ReleaseJob is the queue payload for a single release-docs invocation.
// Phase-1 callers pass it directly to Dispatcher.Run; Phase 2 will encode
// it as a River job argument.
type ReleaseJob struct {
	// Provider identifies the VCS kind (github, github_enterprise, gitlab).
	Provider vcs.Kind `json:"provider"`
	// Repo is the full repository name (e.g. "owner/repo").
	Repo string `json:"repo"`
	// Org is the Cadoo organisation ID for multi-tenancy.
	Org string `json:"org"`
	// FromRef is the prior release tag or commit SHA used as the range start.
	FromRef string `json:"from_ref"`
	// ToRef is the new release tag that triggered this job.
	ToRef string `json:"to_ref"`
}

// Kind implements the jobs.Job interface so ReleaseJob can later be enqueued
// with River (Phase 2). Mirrors orchestrator.ToolJob.Kind().
func (ReleaseJob) Kind() string { return "release_docs" }

// ReleaseContext is the single packed input passed to every Generator and
// Publisher. It is built once by the dispatcher and must not be mutated after
// construction (D-04).
type ReleaseContext struct {
	// Repo is the full repository name (e.g. "owner/repo").
	Repo string
	// Org is the Cadoo organisation ID for multi-tenancy.
	Org string
	// FromRef is the prior release tag or commit SHA (range start, exclusive).
	FromRef string
	// ToRef is the new release tag (range end, inclusive).
	ToRef string
	// Bump is the computed semver magnitude for this release.
	Bump SemverBump
	// Commits is the ordered slice of commits in the range (reverse-chron).
	Commits []vcs.Commit
	// MergedPRs is the slice of merged pull-requests / merge-requests in the
	// range.
	MergedPRs []vcs.MergedPR
	// Config is the per-repo releaseDocs configuration loaded from the release
	// tag tree (never from main — D-06).
	Config config.ReleaseDocs
	// Provider is the VCS adapter for this repository. Generators and
	// publishers may type-assert optional capabilities on it.
	Provider vcs.Provider
	// LLM is the nil-tolerant LLM gateway. When nil, generators must fall
	// back to deterministic output (D-10, D-11). Callers must never assume
	// non-nil.
	LLM llm.Provider
	// Model is the model name to pass to LLM.Chat. Empty when LLM is nil.
	Model string
}

// FileFetcher is an OPTIONAL capability VCS adapters may implement so the
// releasedocs dispatcher can read .cadoo.yaml from the release tag tree.
// This interface is declared here (not imported from orchestrator) so the
// releasedocs package never imports orchestrator (D-01). The method signature
// is intentionally identical to orchestrator.FileFetcher; both adapters
// (github, gitlab) already implement it. Downstream plans that need file
// content (plans 03, 07) type-assert this interface against vcs.Provider.
type FileFetcher interface {
	// FetchFileFromRef reads a repository file at the given ref (tag, branch,
	// or commit SHA). Returns the raw file bytes. A missing file (404) should
	// be reported as an error; callers apply isMissingFile-style 404 tolerance.
	FetchFileFromRef(ctx context.Context, repo, ref, path string) ([]byte, error)
}

// Generator is the interface every release-artifact generator implements.
// Each generator produces one ArtifactKind per invocation. Generators are
// stateless and must be safe for concurrent use.
type Generator interface {
	// Kind returns the ArtifactKind this generator produces.
	Kind() ArtifactKind
	// Enabled reports whether this generator should run given the per-repo
	// config and the computed semver bump. The dispatcher calls Enabled before
	// Generate; a generator must not be called when Enabled returns false.
	Enabled(cfg config.ReleaseDocs, bump SemverBump) bool
	// Generate builds the artifact. It must respect rc.LLM == nil by
	// producing deterministic output without any LLM call.
	Generate(ctx context.Context, rc ReleaseContext) (Artifact, error)
}

// Publisher is the interface every release-artifact publisher implements.
// Each publisher delivers artifacts to one PublishTarget. Publishers are
// stateless and must be safe for concurrent use.
type Publisher interface {
	// Target returns the PublishTarget this publisher writes to.
	Target() PublishTarget
	// Publish delivers the relevant subset of arts to the target. Publish
	// must be idempotent: repeated calls with identical inputs must produce
	// the same observable effect (no duplicate comments, no extra PRs).
	Publish(ctx context.Context, rc ReleaseContext, arts []Artifact) error
}

// GeneratorRegistry holds the generators the dispatcher can invoke.
type GeneratorRegistry struct {
	m map[ArtifactKind]Generator
}

// NewGeneratorRegistry returns an empty GeneratorRegistry.
func NewGeneratorRegistry() *GeneratorRegistry {
	return &GeneratorRegistry{m: map[ArtifactKind]Generator{}}
}

// Register adds a generator. Last writer wins.
func (r *GeneratorRegistry) Register(g Generator) { r.m[g.Kind()] = g }

// Get returns the generator for the given kind.
func (r *GeneratorRegistry) Get(kind ArtifactKind) (Generator, bool) {
	g, ok := r.m[kind]
	return g, ok
}

// Generators returns all registered generators in unspecified order.
func (r *GeneratorRegistry) Generators() []Generator {
	out := make([]Generator, 0, len(r.m))
	for _, g := range r.m {
		out = append(out, g)
	}
	return out
}

// PublisherRegistry holds the publishers the dispatcher can invoke.
type PublisherRegistry struct {
	m map[PublishTarget]Publisher
}

// NewPublisherRegistry returns an empty PublisherRegistry.
func NewPublisherRegistry() *PublisherRegistry {
	return &PublisherRegistry{m: map[PublishTarget]Publisher{}}
}

// Register adds a publisher. Last writer wins.
func (r *PublisherRegistry) Register(p Publisher) { r.m[p.Target()] = p }

// Get returns the publisher for the given target.
func (r *PublisherRegistry) Get(target PublishTarget) (Publisher, bool) {
	p, ok := r.m[target]
	return p, ok
}

// Publishers returns all registered publishers in unspecified order.
func (r *PublisherRegistry) Publishers() []Publisher {
	out := make([]Publisher, 0, len(r.m))
	for _, p := range r.m {
		out = append(out, p)
	}
	return out
}
