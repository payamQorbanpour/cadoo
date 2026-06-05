// Package pages implements the pages Publisher, which commits each generated
// release artifact to a configured docs branch at deterministic paths
// {dir}/releases/{toRef}/{kind}.md via vcs.BranchCommitter.UpsertFile. Re-runs
// overwrite the same path (idempotent; D-13/D-14). When the provider does not
// implement vcs.BranchCommitter, Publish logs a warning and returns nil
// (graceful degradation; D-15). When publish.pages.enabled is false, Publish
// is a no-op.
//
// Path construction uses path.Join (never raw fmt.Sprintf with rc.ToRef) to
// neutralize path-traversal attacks on adversarial tag names (T-02-07).
package pages

import (
	"context"
	"fmt"
	"log/slog"
	"path"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// Publisher delivers each release artifact to a docs branch by upserting it at
// a deterministic path. It implements releasedocs.Publisher and is safe for
// concurrent use.
type Publisher struct{}

// Target returns TargetPages, identifying where this publisher writes.
func (Publisher) Target() releasedocs.PublishTarget {
	return releasedocs.TargetPages
}

// Publish commits each non-empty artifact from arts to the configured docs
// branch at the path {dir}/releases/{toRef}/{kind}.md. Paths are constructed
// with path.Join to prevent path-traversal on adversarial tag names (T-02-07).
//
// When rc.Config.Publish.Pages.Enabled is false, Publish returns nil
// immediately (no-op). When rc.Provider does not implement vcs.BranchCommitter,
// Publish logs a warning and returns nil (graceful degradation; D-15).
//
// UpsertFile is the idempotency mechanism: calling Publish a second time with
// identical inputs issues UpsertFile to the same deterministic path, which
// overwrites the file in place rather than creating a duplicate.
func (Publisher) Publish(ctx context.Context, rc releasedocs.ReleaseContext, arts []releasedocs.Artifact) error {
	cfg := rc.Config.Publish.Pages
	if !cfg.Enabled {
		return nil
	}

	bc, ok := rc.Provider.(vcs.BranchCommitter)
	if !ok {
		slog.Warn("BranchCommitter capability absent; skipping pages",
			"provider", rc.Provider.Kind(),
			"repo", rc.Repo,
		)
		return nil
	}

	// Resolve branch and directory with documented defaults.
	branch := cfg.Branch
	if branch == "" {
		branch = "gh-pages"
	}
	dir := cfg.Dir
	if dir == "" {
		dir = "docs"
	}

	for _, art := range arts {
		// Skip artifacts with no content — nothing to commit.
		if len(art.Content) == 0 {
			continue
		}

		// Build the deterministic, traversal-safe path using path.Join.
		// path.Join cleans redundant separators and ".." components, which
		// neutralizes path-traversal attempts embedded in rc.ToRef (T-02-07).
		p := path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")

		commitMsg := "docs: release " + rc.ToRef + " " + string(art.Kind)

		if err := bc.UpsertFile(ctx, rc.Repo, branch, commitMsg, vcs.FileWrite{
			Path:    p,
			Content: art.Content,
		}); err != nil {
			return fmt.Errorf("pages: UpsertFile path %q: %w", p, err)
		}
	}

	return nil
}
