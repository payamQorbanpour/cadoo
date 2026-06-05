// Package releasebody implements the releasebody Publisher, which splices
// release-notes artifact content into the VCS release body using locked
// Cadoo markers. It preserves user-written text outside the markers and is
// idempotent: re-running Publish edits the managed block in place rather than
// appending a second copy (D-12, D-14). A write is issued only when the
// spliced body differs from the current body (mirrors applyPRBody, D-04).
//
// When the provider does not implement vcs.ReleasePublisher, Publish logs a
// warning and returns nil (graceful degradation, D-15).
//
// CR-01 fix: GitLab releases have no numeric ID — GetReleaseByTag returns
// vcs.Release.ID=0. When ID==0 and TagName!="", Publish type-asserts
// vcs.TagReleasePublisher and calls UpdateReleaseBodyByTag. If the assertion
// fails (provider returned a zero-ID release but lacks TagReleasePublisher),
// Publish returns a descriptive error so the caller knows the capability is
// missing rather than silently succeeding with a no-op.
package releasebody

import (
	"context"
	"fmt"
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
// the current body, no update is issued (no-op; mirrors applyPRBody, D-04).
// State is reconstructed entirely from the live release body marker (stateless,
// D-14).
//
// If rc.Provider does not implement vcs.ReleasePublisher the call is skipped
// with a warning log and nil is returned (D-15).
//
// CR-01: when the release has no numeric ID (ID==0) and TagName is non-empty,
// Publish type-asserts vcs.TagReleasePublisher and routes through
// UpdateReleaseBodyByTag. GitLab returns ID=0 by design; this is the correct
// update path for GitLab releases. If the type assertion fails (provider lacks
// TagReleasePublisher), a descriptive error is returned.
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

	// CR-01: GitLab releases have no numeric ID (rel.ID == 0). Route through
	// TagReleasePublisher when the release is identified only by its tag name.
	// Tag name comes from GetReleaseByTag (provider-validated), never raw user input
	// (T-02-01: mitigated).
	if rel.ID == 0 && rel.TagName != "" {
		tp, ok := rc.Provider.(vcs.TagReleasePublisher)
		if !ok {
			return fmt.Errorf("releasebody: release %q has no numeric ID and provider %s does not implement vcs.TagReleasePublisher; cannot update release body",
				rel.TagName, rc.Provider.Kind())
		}
		return tp.UpdateReleaseBodyByTag(ctx, rc.Repo, rel.TagName, newBody)
	}

	return rp.UpdateReleaseBody(ctx, rc.Repo, rel.ID, newBody)
}
