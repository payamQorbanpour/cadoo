// Package releasebody implements the releasebody Publisher, which splices
// release-notes artifact content into the VCS release body using locked
// Cadoo markers. It preserves user-written text outside the markers and is
// idempotent: re-running Publish edits the managed block in place rather than
// appending a second copy (D-12, D-14). A write is issued only when the
// spliced body differs from the current body (mirrors applyPRBody, D-04).
//
// When the provider does not implement vcs.ReleasePublisher, Publish logs a
// warning and returns nil (graceful degradation, D-15).
package releasebody

import (
	"context"
	"log/slog"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// Publisher delivers release-notes content to the VCS release body by
// splicing it between the locked Cadoo markers. It implements
// releasedocs.Publisher and is safe for concurrent use.
type Publisher struct{}

// Target returns TargetReleaseBody, identifying where this publisher writes.
func (Publisher) Target() releasedocs.PublishTarget {
	return releasedocs.TargetReleaseBody
}

// Publish splices the first KindReleaseNotes artifact from arts into the VCS
// release body identified by rc.ToRef. The managed section is wrapped between
// releasedocs.ReleaseNotesBegin and releasedocs.ReleaseNotesEnd; content
// outside the markers is preserved (D-12). If the spliced body is identical to
// the current body, UpdateReleaseBody is not called (no-op; mirrors
// applyPRBody). State is reconstructed entirely from the live release body
// marker (stateless, D-14).
//
// If rc.Provider does not implement vcs.ReleasePublisher the call is skipped
// with a warning log and nil is returned (D-15).
func (Publisher) Publish(ctx context.Context, rc releasedocs.ReleaseContext, arts []releasedocs.Artifact) error {
	rp, ok := rc.Provider.(vcs.ReleasePublisher)
	if !ok {
		slog.Warn("ReleasePublisher capability absent; skipping releasebody",
			"provider", rc.Provider.Kind(),
			"repo", rc.Repo,
		)
		return nil
	}

	// Find the release-notes artifact.
	var section string
	for _, a := range arts {
		if a.Kind == releasedocs.KindReleaseNotes {
			section = string(a.Content)
			break
		}
	}
	// No release-notes artifact in this run — nothing to publish.
	if section == "" {
		return nil
	}

	// Read the current release body.
	rel, err := rp.GetReleaseByTag(ctx, rc.Repo, rc.ToRef)
	if err != nil {
		return err
	}

	// Splice the section into the body (preserves user content outside markers).
	newBody := releasedocs.SpliceReleaseBody(rel.Body, section)

	// No-op if unchanged (mirrors applyPRBody idempotency guard, D-04).
	if newBody == rel.Body {
		return nil
	}

	return rp.UpdateReleaseBody(ctx, rc.Repo, rel.ID, newBody)
}
