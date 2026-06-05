package releasedocs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// PostedStore is the persistence interface used by Dispatcher to record
// successful publish events for cross-restart idempotency (D-14,
// REQ-release-docs-idempotency). The interface is deliberately narrow —
// it matches only the Record method on internal/releasedocs/state.Store — so
// the releasedocs package does not import the state sub-package and there is
// no import cycle. A nil PostedStore is safe: the dispatcher skips Record
// calls entirely (stateless marker mode).
//
// *state.Store satisfies this interface. Tests may use a recording fake.
type PostedStore interface {
	// Record upserts a publication record keyed on (provider, repoFullName,
	// toTag, kind). org is stored for multi-tenancy. externalID is the
	// provider-specific reference (e.g. the tag name) for this publication.
	Record(ctx context.Context, org, provider, repoFullName, toTag, kind, externalID string) error
}

// Dispatcher is the single entry point for a release-docs run. It resolves the
// VCS provider, loads the per-repo config from the release tag tree, builds the
// ReleaseContext, runs every enabled Generator, and routes the resulting
// Artifacts to every Publisher. It mirrors the shape of
// internal/orchestrator.Dispatcher without importing it (D-01).
type Dispatcher struct {
	// VCSPool maps each vcs.Kind to its adapter. The dispatcher selects the
	// adapter from the pool using job.Provider.
	VCSPool map[vcs.Kind]vcs.Provider
	// LLM is the nil-tolerant LLM gateway. When nil, generators produce
	// deterministic output and no Chat call is made (D-10).
	LLM llm.Provider
	// Model is the default model name passed to LLM.Chat. Overridden by the
	// per-repo config when set.
	Model string
	// BaseCfg is the fallback repo config used when .cadoo.yaml cannot be
	// loaded from the release tag tree (D-06).
	BaseCfg config.Repo
	// Generators is the ordered slice of Generator implementations to invoke.
	// Disabled generators (Enabled returns false) are silently skipped (D-08).
	Generators []Generator
	// Publishers is the ordered slice of Publisher implementations to invoke.
	// Publishers that lack a required VCS capability degrade gracefully (D-15).
	Publishers []Publisher
	// Store is the optional PostedStore used to record successful publish
	// events for cross-restart idempotency (REQ-release-docs-idempotency).
	// When nil, no Record calls are made and the dispatcher runs in stateless
	// marker mode — identical to cadoo-cli behaviour (D-14).
	Store PostedStore
}

// Run executes a single release-docs job: provider resolution → config load
// from the ToRef tree → BuildContext → enabled generators → publishers. The
// flow mirrors orchestrator.Dispatcher.Run (reviewer.go:144) without importing
// orchestrator (D-01).
//
// If releaseDocs.enabled is false in the loaded config, Run returns nil without
// doing any generation or publishing. If the job.Provider is empty it defaults
// to vcs.KindGitHub, mirroring reviewer.go:149.
func (d *Dispatcher) Run(ctx context.Context, job ReleaseJob) error {
	// Default-fill Provider (mirrors reviewer.go:149-151).
	if job.Provider == "" {
		job.Provider = vcs.KindGitHub
	}

	// Resolve provider from pool (mirrors reviewer.go:168-172).
	provider, ok := d.VCSPool[job.Provider]
	if !ok {
		return fmt.Errorf("releasedocs: no adapter for provider %q (configured: %v)",
			job.Provider, configuredKinds(d.VCSPool))
	}

	// Load .cadoo.yaml from the ToRef tree, never from main (D-06, Pitfall 2).
	cfg := d.loadCfg(ctx, provider, job)

	// Master switch: if releaseDocs is not enabled, no-op (D-08).
	if !cfg.ReleaseDocs.Enabled {
		slog.Debug("releasedocs: disabled in config; skipping",
			"repo", job.Repo, "toRef", job.ToRef)
		return nil
	}

	// Build the shared ReleaseContext (calls BuildContext which resolves
	// FromRef, lists commits/PRs, computes bump, builds grouped model).
	rc, err := BuildContext(ctx, provider, job, cfg.ReleaseDocs, d.LLM, d.Model)
	if err != nil {
		return fmt.Errorf("releasedocs: build context: %w", err)
	}

	// Run every enabled generator, collecting artifacts. Disabled generators
	// are never called (D-08, T-07-03). Order is the slice order (D-09).
	var arts []Artifact
	for _, gen := range d.Generators {
		if !gen.Enabled(cfg.ReleaseDocs, rc.Bump) {
			slog.Debug("releasedocs: generator disabled; skipping",
				"kind", gen.Kind(), "bump", rc.Bump, "repo", job.Repo)
			continue
		}
		art, err := gen.Generate(ctx, rc)
		if err != nil {
			return fmt.Errorf("releasedocs: generator %s: %w", gen.Kind(), err)
		}
		arts = append(arts, art)
	}

	// Route artifacts to each publisher. Publishers degrade gracefully when
	// their required VCS capability is absent (D-15) — they log the reason
	// internally and return nil.
	for _, pub := range d.Publishers {
		if err := pub.Publish(ctx, rc, arts); err != nil {
			return fmt.Errorf("releasedocs: publisher %s: %w", pub.Target(), err)
		}
	}

	// Record publish state for each produced artifact when a Store is
	// configured. This runs only after all publishers succeed (no Record on
	// error). Record errors are logged as warnings but do not fail the run —
	// idempotency bookkeeping is best-effort; the publish already succeeded.
	if d.Store != nil {
		for _, art := range arts {
			if err := d.Store.Record(ctx,
				job.Org,
				string(job.Provider),
				job.Repo,
				job.ToRef,
				string(art.Kind),
				job.ToRef, // externalID is the tag for this plan
			); err != nil {
				slog.Warn("releasedocs: state record failed (best-effort)",
					"kind", art.Kind, "repo", job.Repo, "toRef", job.ToRef, "err", err)
			}
		}
	}

	return nil
}

// loadCfg reads .cadoo.yaml from the release tag tree (job.ToRef) if the VCS
// adapter implements FileFetcher. If the file is missing or the adapter lacks
// the capability, d.BaseCfg is returned instead. This mirrors
// orchestrator.Dispatcher.loadCfg (reviewer.go:505-525), substituting ToRef
// for pr.HeadSHA (D-06, Pitfall 2).
func (d *Dispatcher) loadCfg(ctx context.Context, provider vcs.Provider, job ReleaseJob) config.Repo {
	ff, ok := provider.(FileFetcher)
	if !ok || job.ToRef == "" {
		return d.BaseCfg
	}
	raw, err := ff.FetchFileFromRef(ctx, job.Repo, job.ToRef, ".cadoo.yaml")
	if err != nil {
		if !isMissingFile(err) {
			slog.Debug("releasedocs: load .cadoo.yaml failed; using base config",
				"repo", job.Repo, "ref", job.ToRef, "err", err)
		}
		return d.BaseCfg
	}
	cfg, err := config.Parse(raw)
	if err != nil {
		slog.Warn("releasedocs: parse .cadoo.yaml; using base config",
			"repo", job.Repo, "ref", job.ToRef, "err", err)
		return d.BaseCfg
	}
	return cfg
}

// isMissingFile reports whether err represents a 404 / not-found condition.
// Mirrors orchestrator.isMissingFile (reviewer.go:527-536) without importing
// orchestrator (D-01).
func isMissingFile(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	// VCS clients wrap 404s in their own error types; we don't import them
	// here to keep this package VCS-agnostic. Match by string as a soft
	// heuristic (mirrors reviewer.go:533-534).
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

// configuredKinds returns the vcs.Kind keys present in the pool, used for
// diagnostic error messages. Mirrors orchestrator.configuredKinds
// (reviewer.go:634-640).
func configuredKinds(pool map[vcs.Kind]vcs.Provider) []vcs.Kind {
	out := make([]vcs.Kind, 0, len(pool))
	for k := range pool {
		out = append(out, k)
	}
	return out
}
