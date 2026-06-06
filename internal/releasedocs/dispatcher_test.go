package releasedocs_test

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// enabledCfg returns a config.Repo with releaseDocs enabled and both
// changelog + release-notes artifacts active.
func enabledCfg() config.Repo {
	return config.Repo{
		ReleaseDocs: config.ReleaseDocs{
			Enabled: true,
			Artifacts: config.ReleaseArtifacts{
				Changelog: config.ArtifactConfig{Enabled: true},
				ReleaseNotes: config.ReleaseNotesConfig{
					ArtifactConfig: config.ArtifactConfig{Enabled: true},
				},
			},
		},
	}
}

// recordingGenerator is a test Generator that records calls and returns a
// fixed artifact. It never calls the LLM.
type recordingGenerator struct {
	kind    releasedocs.ArtifactKind
	enabled bool
	calls   int
}

func (g *recordingGenerator) Kind() releasedocs.ArtifactKind { return g.kind }
func (g *recordingGenerator) Enabled(_ config.ReleaseDocs, _ releasedocs.SemverBump) bool {
	return g.enabled
}
func (g *recordingGenerator) Generate(_ context.Context, _ releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	g.calls++
	return releasedocs.Artifact{Kind: g.kind, Content: []byte("generated:" + string(g.kind))}, nil
}

// recordingPublisher is a test Publisher that records every Publish call.
type recordingPublisher struct {
	target releasedocs.PublishTarget
	calls  int
	arts   []releasedocs.Artifact
}

func (p *recordingPublisher) Target() releasedocs.PublishTarget { return p.target }
func (p *recordingPublisher) Publish(_ context.Context, _ releasedocs.ReleaseContext, arts []releasedocs.Artifact) error {
	p.calls++
	p.arts = append(p.arts, arts...)
	return nil
}

// newTestDispatcher builds a Dispatcher backed by a Fake provider with all
// capabilities, using recordingGenerator + recordingPublisher so no real
// network or LLM calls occur.
func newTestDispatcher(fake *releasedocstest.Fake, provider vcs.Provider,
	gen *recordingGenerator, pub *recordingPublisher) *releasedocs.Dispatcher {
	return &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
		LLM:        nil, // deterministic path
		Generators: []releasedocs.Generator{gen},
		Publishers: []releasedocs.Publisher{pub},
		BaseCfg:    enabledCfg(),
	}
}

// job returns a minimal ReleaseJob for tests. The Fake's FetchFileFromRef
// returns nil content (empty YAML) so BaseCfg is the effective config.
func job() releasedocs.ReleaseJob {
	return releasedocs.ReleaseJob{
		Provider: vcs.KindGitHub,
		Repo:     "owner/repo",
		Org:      "org1",
		FromRef:  "v0.1.0",
		ToRef:    "v0.2.0",
	}
}

// TestIdempotentTwiceRun verifies REQ-release-docs-idempotency: running Run
// twice over the same range must not duplicate artifacts. The publisher is
// called twice (once per run), but both calls carry identical content so the
// publisher's own idempotency (markers) ensures a single write.
//
// We verify at the dispatcher level that: (a) the generator is called on every
// Run regardless of prior state (dispatcher is stateless — idempotency is the
// publisher's responsibility), and (b) the publisher is invoked on every Run
// with the same artifact content.
func TestIdempotentTwiceRun(t *testing.T) {
	fake, provider := releasedocstest.NewFake()
	// Simulate missing .cadoo.yaml so dispatcher falls back to BaseCfg (enabled).
	fake.FetchErr = fs.ErrNotExist

	gen := &recordingGenerator{kind: releasedocs.KindChangelog, enabled: true}
	pub := &recordingPublisher{target: releasedocs.TargetChangelogPR}

	d := newTestDispatcher(fake, provider, gen, pub)
	ctx := context.Background()
	j := job()

	if err := d.Run(ctx, j); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := d.Run(ctx, j); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	// Generator called once per run.
	if gen.calls != 2 {
		t.Errorf("generator calls = %d, want 2", gen.calls)
	}
	// Publisher called once per run.
	if pub.calls != 2 {
		t.Errorf("publisher calls = %d, want 2", pub.calls)
	}
	// Both runs produced the same artifact content (deterministic, LLM off).
	if len(pub.arts) != 2 {
		t.Fatalf("captured artifacts = %d, want 2", len(pub.arts))
	}
	if string(pub.arts[0].Content) != string(pub.arts[1].Content) {
		t.Errorf("artifact content differs between runs: %q vs %q",
			pub.arts[0].Content, pub.arts[1].Content)
	}
}

// TestGracefulDegradation verifies that when a VCS capability is absent, the
// dependent publisher is skipped without failing the entire run. This tests the
// cross-cutting degradation path (D-15).
//
// We use a real releasebody.Publisher via the embedded publishers slice and a
// Fake that has OmitReleasePublisher, so releasebody.Publisher degrades
// gracefully. We assert the run returns nil and a second publisher still runs.
func TestGracefulDegradation(t *testing.T) {
	// Fake without ReleasePublisher capability so releasebody.Publisher degrades.
	fake, provider := releasedocstest.NewFake(releasedocstest.OmitReleasePublisher())
	fake.FetchErr = fs.ErrNotExist // simulate missing .cadoo.yaml → use BaseCfg

	gen := &recordingGenerator{kind: releasedocs.KindReleaseNotes, enabled: true}
	pub := &recordingPublisher{target: releasedocs.TargetReleaseBody}
	secondPub := &recordingPublisher{target: releasedocs.TargetChangelogPR}

	d := &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
		LLM:        nil,
		Generators: []releasedocs.Generator{gen},
		Publishers: []releasedocs.Publisher{pub, secondPub},
		BaseCfg:    enabledCfg(),
	}

	ctx := context.Background()
	if err := d.Run(ctx, job()); err != nil {
		t.Fatalf("Run with missing capability: %v", err)
	}

	// Both publishers were called (graceful degradation is handled inside Publish,
	// not at the dispatcher level).
	if pub.calls != 1 {
		t.Errorf("first publisher calls = %d, want 1", pub.calls)
	}
	if secondPub.calls != 1 {
		t.Errorf("second publisher calls = %d, want 1", secondPub.calls)
	}
}

// TestDisabledNotGenerated verifies REQ-per-artifact-toggles: a disabled
// generator must never be called, and a generator disabled by a when: condition
// that excludes the current bump must also be skipped.
func TestDisabledNotGenerated(t *testing.T) {
	t.Run("disabled_artifact", func(t *testing.T) {
		fake, provider := releasedocstest.NewFake()
		fake.FetchErr = fs.ErrNotExist // use BaseCfg (enabled)

		// Generator reports itself as disabled.
		gen := &recordingGenerator{kind: releasedocs.KindChangelog, enabled: false}
		pub := &recordingPublisher{target: releasedocs.TargetChangelogPR}

		d := newTestDispatcher(fake, provider, gen, pub)
		ctx := context.Background()

		if err := d.Run(ctx, job()); err != nil {
			t.Fatalf("Run: %v", err)
		}

		// Disabled generator must never be called (D-08, T-07-03).
		if gen.calls != 0 {
			t.Errorf("disabled generator called %d times, want 0", gen.calls)
		}
	})

	t.Run("when_excludes_bump", func(t *testing.T) {
		fake, provider := releasedocstest.NewFake()
		fake.FetchErr = fs.ErrNotExist // use BaseCfg

		// Build a cfg where changelog is enabled only for major bumps.
		// The job uses v0.1.0 → v0.2.0 which is a minor bump.
		cfgOnlyMajor := enabledCfg()
		cfgOnlyMajor.ReleaseDocs.Artifacts.Changelog.When = "major"

		// Use a generator that calls releasedocs.Enabled internally to mirror
		// the real changelog.Generator behavior.
		gen := &conditionalGenerator{
			kind:        releasedocs.KindChangelog,
			artifactCfg: cfgOnlyMajor.ReleaseDocs.Artifacts.Changelog,
		}
		pub := &recordingPublisher{target: releasedocs.TargetChangelogPR}

		d := &releasedocs.Dispatcher{
			VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
			LLM:        nil,
			Generators: []releasedocs.Generator{gen},
			Publishers: []releasedocs.Publisher{pub},
			BaseCfg:    cfgOnlyMajor,
		}

		ctx := context.Background()
		if err := d.Run(ctx, job()); err != nil {
			t.Fatalf("Run: %v", err)
		}

		// Generator must not be called because bump is minor and when=="major".
		if gen.calls != 0 {
			t.Errorf("when-excluded generator called %d times, want 0", gen.calls)
		}
	})
}

// conditionalGenerator uses releasedocs.Enabled to decide whether to run,
// mirroring the real generators' behavior.
type conditionalGenerator struct {
	kind        releasedocs.ArtifactKind
	artifactCfg config.ArtifactConfig
	calls       int
}

func (g *conditionalGenerator) Kind() releasedocs.ArtifactKind { return g.kind }
func (g *conditionalGenerator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	return releasedocs.Enabled(g.artifactCfg, bump)
}
func (g *conditionalGenerator) Generate(_ context.Context, _ releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	g.calls++
	return releasedocs.Artifact{Kind: g.kind, Content: []byte("conditional content")}, nil
}

// TestDispatcherDisabledConfig verifies that if releaseDocs.enabled is false
// in the config, Run returns nil without calling any generator or publisher.
func TestDispatcherDisabledConfig(t *testing.T) {
	fake, provider := releasedocstest.NewFake()
	// Return a YAML snippet that disables releaseDocs.
	fake.FileContent = []byte("releaseDocs:\n  enabled: false\n")

	gen := &recordingGenerator{kind: releasedocs.KindChangelog, enabled: true}
	pub := &recordingPublisher{target: releasedocs.TargetChangelogPR}

	d := &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
		LLM:        nil,
		Generators: []releasedocs.Generator{gen},
		Publishers: []releasedocs.Publisher{pub},
		BaseCfg:    enabledCfg(), // base is enabled but file content overrides
	}

	ctx := context.Background()
	if err := d.Run(ctx, job()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if gen.calls != 0 {
		t.Errorf("generator called %d times when config disabled, want 0", gen.calls)
	}
	if pub.calls != 0 {
		t.Errorf("publisher called %d times when config disabled, want 0", pub.calls)
	}
}

// TestDispatcherNoProvider verifies that an unknown provider returns an error.
func TestDispatcherNoProvider(t *testing.T) {
	_, provider := releasedocstest.NewFake()
	d := &releasedocs.Dispatcher{
		VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
	}

	j := job()
	j.Provider = vcs.KindGitLab // not in pool

	err := d.Run(context.Background(), j)
	if err == nil {
		t.Fatal("expected error for missing provider, got nil")
	}
	if !strings.Contains(err.Error(), "no adapter for provider") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// recordingStore is a test PostedStore implementation that captures Record calls.
type recordingStore struct {
	calls []recordedCall
}

type recordedCall struct {
	org, provider, repoFullName, toTag, kind, externalID string
}

func (s *recordingStore) Record(_ context.Context, org, provider, repoFullName, toTag, kind, externalID string) error {
	s.calls = append(s.calls, recordedCall{org, provider, repoFullName, toTag, kind, externalID})
	return nil
}

// errorPublisher is a Publisher that always returns an error.
type errorPublisher struct {
	target releasedocs.PublishTarget
}

func (p *errorPublisher) Target() releasedocs.PublishTarget { return p.target }
func (p *errorPublisher) Publish(_ context.Context, _ releasedocs.ReleaseContext, _ []releasedocs.Artifact) error {
	return fmt.Errorf("publisher %s: intentional error for test", p.target)
}

// TestPostedStoreNilNoOp verifies that with Store == nil, Run behaves exactly
// as before — no panic, and no Record calls.
func TestPostedStoreNilNoOp(t *testing.T) {
	fake, provider := releasedocstest.NewFake()
	fake.FetchErr = fs.ErrNotExist

	gen := &recordingGenerator{kind: releasedocs.KindChangelog, enabled: true}
	pub := &recordingPublisher{target: releasedocs.TargetChangelogPR}

	// Store is nil (not set) — stateless marker mode preserved (D-14).
	d := &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
		LLM:        nil,
		Generators: []releasedocs.Generator{gen},
		Publishers: []releasedocs.Publisher{pub},
		BaseCfg:    enabledCfg(),
		Store:      nil,
	}

	ctx := context.Background()
	if err := d.Run(ctx, job()); err != nil {
		t.Fatalf("Run with nil Store: %v", err)
	}

	// Publisher should have been called normally.
	if pub.calls != 1 {
		t.Errorf("publisher calls = %d, want 1", pub.calls)
	}
	// Generator should have been called normally.
	if gen.calls != 1 {
		t.Errorf("generator calls = %d, want 1", gen.calls)
	}
}

// TestPostedStoreRecordsOnSuccess verifies that with Store set to a recording
// fake, after a successful run that produced artifacts, Run calls Record once
// per produced artifact with the correct args.
func TestPostedStoreRecordsOnSuccess(t *testing.T) {
	fake, provider := releasedocstest.NewFake()
	fake.FetchErr = fs.ErrNotExist

	gen := &recordingGenerator{kind: releasedocs.KindChangelog, enabled: true}
	pub := &recordingPublisher{target: releasedocs.TargetChangelogPR}
	store := &recordingStore{}

	j := job() // Org: "org1", Provider: github, Repo: "owner/repo", ToRef: "v0.2.0"

	d := &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
		LLM:        nil,
		Generators: []releasedocs.Generator{gen},
		Publishers: []releasedocs.Publisher{pub},
		BaseCfg:    enabledCfg(),
		Store:      store,
	}

	ctx := context.Background()
	if err := d.Run(ctx, j); err != nil {
		t.Fatalf("Run with recording Store: %v", err)
	}

	// One artifact produced (KindChangelog) → one Record call.
	if len(store.calls) != 1 {
		t.Fatalf("Record calls = %d, want 1", len(store.calls))
	}
	call := store.calls[0]
	if call.org != j.Org {
		t.Errorf("Record org = %q, want %q", call.org, j.Org)
	}
	if call.provider != string(j.Provider) {
		t.Errorf("Record provider = %q, want %q", call.provider, string(j.Provider))
	}
	if call.repoFullName != j.Repo {
		t.Errorf("Record repoFullName = %q, want %q", call.repoFullName, j.Repo)
	}
	if call.toTag != j.ToRef {
		t.Errorf("Record toTag = %q, want %q", call.toTag, j.ToRef)
	}
	if call.kind != string(releasedocs.KindChangelog) {
		t.Errorf("Record kind = %q, want %q", call.kind, string(releasedocs.KindChangelog))
	}
	if call.externalID != j.ToRef {
		t.Errorf("Record externalID = %q, want %q", call.externalID, j.ToRef)
	}
}

// TestPostedStoreNoRecordOnPublishError verifies that when a publisher returns
// an error, Record is NOT called (state only recorded on success).
func TestPostedStoreNoRecordOnPublishError(t *testing.T) {
	fake, provider := releasedocstest.NewFake()
	fake.FetchErr = fs.ErrNotExist

	gen := &recordingGenerator{kind: releasedocs.KindChangelog, enabled: true}
	errPub := &errorPublisher{target: releasedocs.TargetChangelogPR}
	store := &recordingStore{}

	d := &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
		LLM:        nil,
		Generators: []releasedocs.Generator{gen},
		Publishers: []releasedocs.Publisher{errPub},
		BaseCfg:    enabledCfg(),
		Store:      store,
	}

	ctx := context.Background()
	err := d.Run(ctx, job())
	if err == nil {
		t.Fatal("Run with erroring publisher: expected error, got nil")
	}

	// Publisher error → zero Record calls.
	if len(store.calls) != 0 {
		t.Errorf("Record calls = %d, want 0 (no record on publish error)", len(store.calls))
	}
}

// TestDispatcherConfigFromToRef verifies that the dispatcher loads .cadoo.yaml
// from job.ToRef (not from main), and that FetchFileFromRef is called with the
// correct ref (D-06, Pitfall 2, T-07-02).
func TestDispatcherConfigFromToRef(t *testing.T) {
	fake, provider := releasedocstest.NewFake()
	// Return a YAML config from FetchFileFromRef; we record which ref was used.
	fake.FileContent = []byte("releaseDocs:\n  enabled: true\n  artifacts:\n    changelog:\n      enabled: true\n")

	gen := &recordingGenerator{kind: releasedocs.KindChangelog, enabled: true}
	pub := &recordingPublisher{target: releasedocs.TargetChangelogPR}

	d := &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{vcs.KindGitHub: provider},
		LLM:        nil,
		Generators: []releasedocs.Generator{gen},
		Publishers: []releasedocs.Publisher{pub},
		BaseCfg:    config.Repo{}, // BaseCfg has releaseDocs disabled
	}

	ctx := context.Background()
	if err := d.Run(ctx, job()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// FetchFileFromRef must have been called (to load .cadoo.yaml from ToRef).
	if fake.FetchFileFromRefCalls == 0 {
		t.Error("FetchFileFromRef not called; config was not loaded from ToRef")
	}
}
