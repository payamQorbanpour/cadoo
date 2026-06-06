package pages_test

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/pages"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
)

// TestPublish_APIDocs_Paths verifies that three apidocs artifacts
// (openapi.yaml, api-reference.html, api-reference.md) route to their correct
// deterministic paths under {dir}/releases/{toRef}/{filename} when published
// via the pages publisher (cross-cut: Artifact.Filename routing, T-03-01).
func TestPublish_APIDocs_Paths(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v2.0.0",
		Provider: provider,
		Config:   enabledPages(),
	}

	// Three apidocs artifacts, all sharing KindAPIDocs, differentiated by Filename.
	arts := []releasedocs.Artifact{
		{
			Kind:     releasedocs.KindAPIDocs,
			Filename: "openapi.yaml",
			Content:  []byte("openapi: 3.0.3\ninfo:\n  title: Test\n  version: 1.0.0\npaths: {}\n"),
		},
		{
			Kind:     releasedocs.KindAPIDocs,
			Filename: "api-reference.html",
			Content:  []byte("<!DOCTYPE html><html><body>Redoc HTML</body></html>"),
		},
		{
			Kind:     releasedocs.KindAPIDocs,
			Filename: "api-reference.md",
			Content:  []byte("# API Reference\n\nGenerated from petstore spec.\n"),
		},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if fake.UpsertFileCalls != 3 {
		t.Fatalf("UpsertFileCalls = %d; want 3 (one per apidocs artifact)", fake.UpsertFileCalls)
	}

	wantPaths := map[string]bool{
		"docs/releases/v2.0.0/openapi.yaml":       false,
		"docs/releases/v2.0.0/api-reference.html": false,
		"docs/releases/v2.0.0/api-reference.md":   false,
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

// TestIdempotent_APIDocs verifies that calling Publish twice with identical
// apidocs inputs issues UpsertFile to the same set of deterministic paths
// each time — no duplicate or extra paths across runs (idempotency requirement,
// D-13/D-14 cross-cut for apidocs).
func TestIdempotent_APIDocs(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v2.0.0",
		Provider: provider,
		Config:   enabledPages(),
	}

	arts := []releasedocs.Artifact{
		{
			Kind:     releasedocs.KindAPIDocs,
			Filename: "openapi.yaml",
			Content:  []byte("openapi: 3.0.3\ninfo:\n  title: Test\n  version: 1.0.0\npaths: {}\n"),
		},
		{
			Kind:     releasedocs.KindAPIDocs,
			Filename: "api-reference.html",
			Content:  []byte("<!DOCTYPE html><html><body>Redoc</body></html>"),
		},
		{
			Kind:     releasedocs.KindAPIDocs,
			Filename: "api-reference.md",
			Content:  []byte("# API Reference\n"),
		},
	}

	ctx := context.Background()

	// First publish.
	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	// Record paths from first run.
	if fake.UpsertFileCalls != 3 {
		t.Fatalf("after first Publish: UpsertFileCalls = %d; want 3", fake.UpsertFileCalls)
	}
	firstRunPaths := make(map[string]int)
	for _, f := range fake.CapturedFiles {
		firstRunPaths[f.Path]++
	}

	// Second publish — same inputs.
	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	// Total UpsertFile calls should be 6 (3 per run × 2 runs).
	if fake.UpsertFileCalls != 6 {
		t.Fatalf("after second Publish: UpsertFileCalls = %d; want 6 (3 per run)", fake.UpsertFileCalls)
	}

	// Paths from second run must be identical to first run.
	secondRunFiles := fake.CapturedFiles[3:] // second batch of 3
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
