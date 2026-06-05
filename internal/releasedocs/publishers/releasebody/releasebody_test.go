package releasebody_test

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/releasebody"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// TestSplicePreserves verifies the releasebody publisher:
//  1. Injects the Cadoo-managed block into a release body that has user content
//     outside the markers.
//  2. Running Publish twice produces exactly one managed block (idempotent).
//  3. When the spliced body equals the current body, UpdateReleaseBody is NOT called.
func TestSplicePreserves(t *testing.T) {
	t.Parallel()

	p := releasebody.Publisher{}

	if p.Target() != releasedocs.TargetReleaseBody {
		t.Fatalf("Target() = %q; want %q", p.Target(), releasedocs.TargetReleaseBody)
	}

	// Fake provider with full capabilities.
	fake, provider := releasedocstest.NewFake()
	fake.Release = &vcs.Release{
		ID:      7,
		TagName: "v1.2.3",
		Body:    "My hand-written intro.",
	}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("## Release Notes\n\nSome notes.")},
	}

	ctx := context.Background()

	// --- First Publish ---
	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	if fake.UpdateReleaseBodyCalls != 1 {
		t.Fatalf("first Publish: UpdateReleaseBody called %d times; want 1", fake.UpdateReleaseBodyCalls)
	}

	// User content must be preserved in the captured body.
	if got := fake.CapturedReleaseBody; !contains(got, "My hand-written intro.") {
		t.Errorf("user content not preserved after first Publish; body = %q", got)
	}
	// Managed section must be present.
	if got := fake.CapturedReleaseBody; !contains(got, releasedocs.ReleaseNotesBegin) {
		t.Errorf("release-notes begin marker missing; body = %q", got)
	}

	// --- Second Publish (idempotency): update fake.Release so it reflects what was written ---
	fake.Release = &vcs.Release{
		ID:      7,
		TagName: "v1.2.3",
		Body:    fake.CapturedReleaseBody, // provider now returns the already-spliced body
	}

	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	// The body has not changed, so UpdateReleaseBody must not be called again.
	if fake.UpdateReleaseBodyCalls != 1 {
		t.Fatalf("second Publish: UpdateReleaseBody called %d times; want 1 (no-op)", fake.UpdateReleaseBodyCalls)
	}

	// Marker appears exactly once.
	firstIdx := indexOf(fake.CapturedReleaseBody, releasedocs.ReleaseNotesBegin)
	lastIdx := lastIndexOf(fake.CapturedReleaseBody, releasedocs.ReleaseNotesBegin)
	if firstIdx != lastIdx {
		t.Errorf("marker appears more than once after two Publish calls; body = %q", fake.CapturedReleaseBody)
	}
}

// TestReleaseBodyDegrades verifies that when the provider does NOT implement
// vcs.ReleasePublisher, Publish returns nil (graceful degradation; D-15) and
// does not attempt any write.
func TestReleaseBodyDegrades(t *testing.T) {
	t.Parallel()

	p := releasebody.Publisher{}

	fake, provider := releasedocstest.NewFake(releasedocstest.OmitReleasePublisher())
	_ = fake // silence unused warning

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("notes")},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish with missing ReleasePublisher capability: got error %v; want nil", err)
	}
	if fake.UpdateReleaseBodyCalls != 0 {
		t.Fatalf("UpdateReleaseBodyCalls = %d; want 0 (capability absent)", fake.UpdateReleaseBodyCalls)
	}
}

// --- helpers ---

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	last := -1
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			last = i
		}
	}
	return last
}
