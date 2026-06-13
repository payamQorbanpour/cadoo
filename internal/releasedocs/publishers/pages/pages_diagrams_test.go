package pages_test

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/pages"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
)

// TestPublish_Diagrams_Paths verifies that two KindDiagrams artifacts carrying
// sub-path Filenames ("diagrams/sequence/login.md", "diagrams/class/domain.md")
// route to their deterministic paths under {dir}/releases/{toRef}/{filename}
// when published via the pages publisher. The publisher already honors arbitrary
// Filename sub-paths (no publisher change); this proves it for KindDiagrams
// (DIAG-03, D-09, T-07-07).
func TestPublish_Diagrams_Paths(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.0",
		Provider: provider,
		Config:   enabledPages(),
	}

	// Two diagrams artifacts, sharing KindDiagrams, differentiated by sub-path Filename.
	arts := []releasedocs.Artifact{
		{
			Kind:     releasedocs.KindDiagrams,
			Filename: "diagrams/sequence/login.md",
			Content:  []byte("```mermaid\nsequenceDiagram\n    A->>B: login\n```\n"),
		},
		{
			Kind:     releasedocs.KindDiagrams,
			Filename: "diagrams/class/domain.md",
			Content:  []byte("```mermaid\nclassDiagram\n    class Order\n```\n"),
		},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if fake.UpsertFileCalls != 2 {
		t.Fatalf("UpsertFileCalls = %d; want 2 (one per diagrams artifact)", fake.UpsertFileCalls)
	}

	wantPaths := map[string]bool{
		"docs/releases/v1.2.0/diagrams/sequence/login.md": false,
		"docs/releases/v1.2.0/diagrams/class/domain.md":   false,
	}
	for _, f := range fake.CapturedFiles {
		if _, ok := wantPaths[f.Path]; !ok {
			t.Errorf("unexpected path %q in UpsertFile calls", f.Path)
		} else {
			wantPaths[f.Path] = true
		}
	}
	for wantPath, seen := range wantPaths {
		if !seen {
			t.Errorf("expected path %q not seen in UpsertFile calls", wantPath)
		}
	}
}

// TestIdempotent_Diagrams verifies that calling Publish twice with identical
// diagrams inputs issues UpsertFile to the same set of deterministic sub-paths
// each time — no duplicate or distinct paths across runs. UpsertFile overwrites
// in place, so re-running the release-docs path produces no new pages (DIAG-03
// idempotency).
func TestIdempotent_Diagrams(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.0",
		Provider: provider,
		Config:   enabledPages(),
	}

	arts := []releasedocs.Artifact{
		{
			Kind:     releasedocs.KindDiagrams,
			Filename: "diagrams/sequence/login.md",
			Content:  []byte("```mermaid\nsequenceDiagram\n    A->>B: login\n```\n"),
		},
		{
			Kind:     releasedocs.KindDiagrams,
			Filename: "diagrams/class/domain.md",
			Content:  []byte("```mermaid\nclassDiagram\n    class Order\n```\n"),
		},
	}

	ctx := context.Background()

	// First publish.
	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	if fake.UpsertFileCalls != 2 {
		t.Fatalf("after first Publish: UpsertFileCalls = %d; want 2", fake.UpsertFileCalls)
	}
	firstRunPaths := make(map[string]int)
	for _, f := range fake.CapturedFiles {
		firstRunPaths[f.Path]++
	}

	// Second publish — same inputs.
	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	// Total UpsertFile calls should be 4 (2 per run × 2 runs).
	if fake.UpsertFileCalls != 4 {
		t.Fatalf("after second Publish: UpsertFileCalls = %d; want 4 (2 per run)", fake.UpsertFileCalls)
	}

	// Paths from second run must be identical to first run (no new/duplicate paths).
	secondRunFiles := fake.CapturedFiles[2:] // second batch of 2
	secondRunPaths := make(map[string]int)
	for _, f := range secondRunFiles {
		secondRunPaths[f.Path]++
	}

	for path, count := range firstRunPaths {
		if secondRunPaths[path] != count {
			t.Errorf("path %q: first run count=%d, second run count=%d (must match for idempotency)",
				path, count, secondRunPaths[path])
		}
	}
	for path, count := range secondRunPaths {
		if firstRunPaths[path] != count {
			t.Errorf("path %q: appeared in second run (count=%d) but not matching first run (count=%d)",
				path, count, firstRunPaths[path])
		}
	}
}
