// Package pages implements the pages Publisher, which commits each generated
// release artifact to a configured docs branch at a deterministic path via
// vcs.BranchCommitter.UpsertFile. The path is:
//
//	{dir}/releases/{toRef}/{filename}
//
// where {filename} is art.Filename when non-empty, or string(art.Kind)+".md"
// for backward compatibility with changelog/releasenotes/blog artifacts. Re-runs
// overwrite the same path (idempotent; D-13/D-14). When the provider does not
// implement vcs.BranchCommitter, Publish logs a warning and returns nil
// (graceful degradation; D-15). When publish.pages.enabled is false, Publish
// is a no-op.
//
// Path construction uses path.Join to clean separators and ".." components, but
// path.Join alone does NOT prevent escape from the base directory — a guard that
// rejects any result not rooted under {dir}/ is required (T-02-07, T-03-01).
// This guard applies to the computed path regardless of whether art.Filename is
// set, so an adversarial Filename carrying "../" is rejected here. See Publish.
package pages

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"

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
// branch at a deterministic path. The path is:
//
//	{dir}/releases/{toRef}/{filename}
//
// where {filename} is art.Filename when non-empty, or string(art.Kind)+".md"
// for backward compatibility with changelog/releasenotes/blog artifacts. Paths
// are computed with path.Join to clean separators and ".." segments. A prefix
// guard then rejects any result that does not start with "{dir}/" — path.Join
// cleans but does not prevent escape from the base directory, so adversarial
// tag names or Filename values (e.g. "../../etc/shadow") are rejected here
// (T-02-07, T-03-01).
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

		// Build the path using path.Join to clean separators and ".." segments.
		// Use art.Filename when non-empty (e.g. apidocs emits .yaml/.html/.md);
		// fall back to string(art.Kind)+".md" for backward compat (D-13).
		filename := art.Filename
		if filename == "" {
			filename = string(art.Kind) + ".md"
		}
		p := path.Join(dir, "releases", rc.ToRef, filename)

		// Guard: path.Join cleans ".." but does not prevent escape from the base
		// directory. Reject any path that does not start with the expected prefix.
		expectedPrefix := dir + "/"
		if !strings.HasPrefix(p, expectedPrefix) {
			slog.Warn("pages: computed path escapes base dir; skipping artifact",
				"path", p, "dir", dir, "toRef", rc.ToRef, "kind", art.Kind)
			continue
		}

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
