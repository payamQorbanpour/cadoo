package diagrams_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/diagrams"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// updateGolden controls whether golden files are rewritten on test run.
// Set TEST_UPDATE_GOLDEN=1 to regenerate; never in CI (default false).
var updateGolden = os.Getenv("TEST_UPDATE_GOLDEN") == "1"

// fakeFetcher is a test double that implements releasedocs.FileFetcher.
// Files are populated from a map[string][]byte; absent paths return a
// "404 not found" error. It embeds a releasedocstest.Fake to satisfy
// vcs.Provider so it can be used directly as ReleaseContext.Provider.
type fakeFetcher struct {
	files        map[string][]byte // path → content; absent path → 404 error
	vcs.Provider                   // embedded provider satisfies the vcs.Provider interface
}

// FetchFileFromRef implements releasedocs.FileFetcher. Returns content when
// path is in the files map; returns a "404 not found" error otherwise.
func (f *fakeFetcher) FetchFileFromRef(_ context.Context, _, _, path string) ([]byte, error) {
	b, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("404 not found: %s", path)
	}
	return b, nil
}

// newFakeFetcher builds a fakeFetcher backed by the supplied file map.
func newFakeFetcher(files map[string][]byte) *fakeFetcher {
	_, provider := releasedocstest.NewFake()
	return &fakeFetcher{files: files, Provider: provider}
}

// fixtureDiagramsRC builds a deterministic ReleaseContext for diagrams tests.
// files seeds the fakeFetcher; dc is the per-type path configuration.
func fixtureDiagramsRC(files map[string][]byte, dc config.DiagramsConfig) releasedocs.ReleaseContext {
	cfg := config.ReleaseDocs{Artifacts: config.ReleaseArtifacts{Diagrams: dc}}
	return releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		FromRef:  "v1.0.0",
		ToRef:    "v1.1.0",
		Bump:     releasedocs.BumpMinor,
		Config:   cfg,
		Provider: newFakeFetcher(files),
	}
}

// mustReadFixture reads a testdata file and fails the test if it cannot.
func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("mustReadFixture(%q): %v", name, err)
	}
	return b
}

// findArtifact returns the artifact with the given Filename, or nil.
func findArtifact(arts []releasedocs.Artifact, filename string) *releasedocs.Artifact {
	for i := range arts {
		if arts[i].Filename == filename {
			return &arts[i]
		}
	}
	return nil
}

// TestDiagrams_Kind verifies that Kind() returns KindDiagrams.
func TestDiagrams_Kind(t *testing.T) {
	t.Parallel()
	g := diagrams.New()
	if got := g.Kind(); got != releasedocs.KindDiagrams {
		t.Errorf("Kind() = %q, want %q", got, releasedocs.KindDiagrams)
	}
}

// TestDiagrams_Enabled verifies the family gate with "always" default
// coercion (DIAG-01).
func TestDiagrams_Enabled(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled bool
		when    string
		bump    releasedocs.SemverBump
		want    bool
	}{
		{"disabled", false, "", releasedocs.BumpMinor, false},
		{"enabled empty-when on minor", true, "", releasedocs.BumpMinor, true},
		{"enabled major-when on patch", true, "major", releasedocs.BumpPatch, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.ReleaseDocs{
				Artifacts: config.ReleaseArtifacts{
					Diagrams: config.DiagramsConfig{
						ArtifactConfig: config.ArtifactConfig{Enabled: tc.enabled, When: tc.when},
					},
				},
			}
			g := diagrams.New()
			if got := g.Enabled(cfg, tc.bump); got != tc.want {
				t.Errorf("Enabled(enabled=%v,when=%q,bump=%v) = %v, want %v",
					tc.enabled, tc.when, tc.bump, got, tc.want)
			}
		})
	}
}

// TestDiagrams_GenerateMulti verifies that two configured sources are fetched
// at ToRef and emitted as exactly two artifacts in fixed type order — sequence
// before class (DIAG-02).
func TestDiagrams_GenerateMulti(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"docs/login.mmd":  mustReadFixture(t, "login.mmd"),
		"docs/domain.mmd": mustReadFixture(t, "domain.mmd"),
	}
	dc := config.DiagramsConfig{
		ArtifactConfig: config.ArtifactConfig{Enabled: true},
		Sequence:       []string{"docs/login.mmd"},
		Class:          []string{"docs/domain.mmd"},
	}
	rc := fixtureDiagramsRC(files, dc)

	g := diagrams.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("got %d artifacts, want 2", len(arts))
	}
	// Fixed order: sequence before class.
	if arts[0].Filename != "diagrams/sequence/login.md" {
		t.Errorf("arts[0].Filename = %q, want diagrams/sequence/login.md", arts[0].Filename)
	}
	if arts[1].Filename != "diagrams/class/domain.md" {
		t.Errorf("arts[1].Filename = %q, want diagrams/class/domain.md", arts[1].Filename)
	}
	for _, a := range arts {
		if a.Kind != releasedocs.KindDiagrams {
			t.Errorf("artifact Kind = %q, want %q", a.Kind, releasedocs.KindDiagrams)
		}
		if !strings.HasPrefix(string(a.Content), "```mermaid\n") {
			t.Errorf("artifact %q content not wrapped in mermaid fence", a.Filename)
		}
	}
}

// TestDiagrams_Skip verifies that a configured-but-absent path and a present
// non-Mermaid source are both skipped, the one valid sibling still emits, and
// GenerateMulti returns no error (DIAG-04).
func TestDiagrams_Skip(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"docs/login.mmd": mustReadFixture(t, "login.mmd"),
		"docs/prose.txt": mustReadFixture(t, "not-mermaid.txt"),
		// "docs/missing.mmd" intentionally absent → 404
	}
	dc := config.DiagramsConfig{
		ArtifactConfig: config.ArtifactConfig{Enabled: true},
		Sequence:       []string{"docs/login.mmd", "docs/missing.mmd"},
		Class:          []string{"docs/prose.txt"},
	}
	rc := fixtureDiagramsRC(files, dc)

	g := diagrams.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti returned error on skip path: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("got %d artifacts, want 1 (only the valid sibling)", len(arts))
	}
	if got := findArtifact(arts, "diagrams/sequence/login.md"); got == nil {
		t.Errorf("valid sibling diagrams/sequence/login.md not emitted; got %v", arts)
	}
}

// TestDiagrams_NoFileFetcher verifies the family-level graceful skip when the
// provider does not implement FileFetcher (D-08).
func TestDiagrams_NoFileFetcher(t *testing.T) {
	t.Parallel()
	_, provider := releasedocstest.NewFake()
	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		ToRef:    "v1.1.0",
		Bump:     releasedocs.BumpMinor,
		Provider: noFetchProvider{Provider: provider},
		Config: config.ReleaseDocs{Artifacts: config.ReleaseArtifacts{
			Diagrams: config.DiagramsConfig{Sequence: []string{"docs/login.mmd"}},
		}},
	}
	g := diagrams.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}
	if arts != nil {
		t.Errorf("got %v artifacts, want nil (family skip)", arts)
	}
}

// noFetchProvider embeds a vcs.Provider interface value, so it satisfies
// vcs.Provider via method promotion but does NOT expose FetchFileFromRef
// (releasedocstest.Fake implements FileFetcher as a concrete method, which is
// not promoted across an interface embed). This forces the family-level
// no-FileFetcher skip path in GenerateMulti.
type noFetchProvider struct{ vcs.Provider }

// TestDiagrams_Golden verifies byte-stable output against committed golden
// files for the canonical sequence and class sources (DIAG-05). Frontmatter
// and %%-commented fixtures sniff true (Pitfall 1).
func TestDiagrams_Golden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		fixture    string
		fetchPath  string
		diagType   string
		artFile    string
		goldenPath string
	}{
		{"login.mmd", "docs/login.mmd", "sequence", "diagrams/sequence/login.md", "testdata/golden/sequence_login.golden"},
		{"domain.mmd", "docs/domain.mmd", "class", "diagrams/class/domain.md", "testdata/golden/class_domain.golden"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			src := mustReadFixture(t, tc.fixture)
			dc := config.DiagramsConfig{ArtifactConfig: config.ArtifactConfig{Enabled: true}}
			switch tc.diagType {
			case "sequence":
				dc.Sequence = []string{tc.fetchPath}
			case "class":
				dc.Class = []string{tc.fetchPath}
			}
			rc := fixtureDiagramsRC(map[string][]byte{tc.fetchPath: src}, dc)

			g := diagrams.New()
			arts, err := g.GenerateMulti(context.Background(), rc)
			if err != nil {
				t.Fatalf("GenerateMulti: %v", err)
			}
			art := findArtifact(arts, tc.artFile)
			if art == nil {
				t.Fatalf("artifact %q not produced; got %v", tc.artFile, arts)
			}

			if updateGolden {
				if err := os.WriteFile(tc.goldenPath, art.Content, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated golden: %s", tc.goldenPath)
				return
			}
			want, err := os.ReadFile(tc.goldenPath)
			if err != nil {
				t.Fatalf("read golden %q: %v", tc.goldenPath, err)
			}
			if string(art.Content) != string(want) {
				t.Errorf("%s does not match golden %q\ngot:\n%s\nwant:\n%s",
					tc.artFile, tc.goldenPath, art.Content, want)
			}
		})
	}
}

// TestDiagrams_Frontmatter_And_Comments verifies that frontmatter-prefixed and
// %%-comment-prefixed sources sniff true and are emitted (Pitfall 1).
func TestDiagrams_Frontmatter_And_Comments(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"docs/fm.mmd":  mustReadFixture(t, "frontmatter.mmd"),
		"docs/cmt.mmd": mustReadFixture(t, "commented.mmd"),
	}
	dc := config.DiagramsConfig{
		ArtifactConfig: config.ArtifactConfig{Enabled: true},
		Sequence:       []string{"docs/fm.mmd"},
		Class:          []string{"docs/cmt.mmd"},
	}
	rc := fixtureDiagramsRC(files, dc)

	g := diagrams.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("got %d artifacts, want 2 (frontmatter + commented both sniff true)", len(arts))
	}
}
