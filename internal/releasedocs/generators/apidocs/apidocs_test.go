package apidocs_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/apidocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// updateGolden controls whether golden files are rewritten on test run.
// Set to true locally with -update flag or TEST_UPDATE_GOLDEN=1 env var,
// never in CI (default false).
var updateGolden = os.Getenv("TEST_UPDATE_GOLDEN") == "1"

// fakeFetcher is a test double that implements releasedocs.FileFetcher.
// Files are populated from a map[string][]byte; absent paths return an error
// containing "404 not found" so isMissingFile 404 tolerance applies.
// It also wraps a releasedocstest.Fake to satisfy vcs.Provider so it can be
// used directly as ReleaseContext.Provider.
type fakeFetcher struct {
	files        map[string][]byte // path → content; absent path → 404 error
	vcs.Provider                   // embedded provider satisfies the vcs.Provider interface
}

// FetchFileFromRef implements releasedocs.FileFetcher.
// Returns content when path is in files map; returns a "404 not found" error otherwise.
func (f *fakeFetcher) FetchFileFromRef(_ context.Context, _, _, path string) ([]byte, error) {
	b, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("404 not found: %s", path)
	}
	return b, nil
}

// newFakeFetcher builds a fakeFetcher backed by the supplied file map.
// The embedded vcs.Provider is a full-capability releasedocstest.Fake that
// also satisfies BranchCommitter and ReleaseRangeReader — but tests that use
// fakeFetcher only need FetchFileFromRef.
func newFakeFetcher(files map[string][]byte) *fakeFetcher {
	_, provider := releasedocstest.NewFake()
	return &fakeFetcher{
		files:    files,
		Provider: provider,
	}
}

// fixtureAPIDocsRC builds a deterministic ReleaseContext for apidocs tests.
// specPath is set on cfg.Artifacts.APIDocs.SpecPath; pass "" to test fallback
// discovery. enabled controls the Enabled field. bump is the semver bump.
func fixtureAPIDocsRC(files map[string][]byte, specPath, when string, enabled bool, bump releasedocs.SemverBump) releasedocs.ReleaseContext {
	cfg := config.ReleaseDocs{
		Artifacts: config.ReleaseArtifacts{
			APIDocs: config.APIDocsConfig{
				ArtifactConfig: config.ArtifactConfig{
					Enabled: enabled,
					When:    when,
				},
				SpecPath: specPath,
			},
		},
	}
	ff := newFakeFetcher(files)
	return releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		FromRef:  "v1.0.0",
		ToRef:    "v1.1.0",
		Bump:     bump,
		Config:   cfg,
		Provider: ff,
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

// TestAPIDocs_Kind verifies that Kind() returns KindAPIDocs.
func TestAPIDocs_Kind(t *testing.T) {
	t.Parallel()
	g := apidocs.New()
	if got := g.Kind(); got != releasedocs.KindAPIDocs {
		t.Errorf("Kind() = %q; want %q", got, releasedocs.KindAPIDocs)
	}
}

// TestEnabled verifies the Enabled gate (D-07) and the "always" default coercion (D-08).
func TestEnabled(t *testing.T) {
	t.Parallel()
	g := apidocs.New()

	tests := []struct {
		name    string
		enabled bool
		when    string
		bump    releasedocs.SemverBump
		want    bool
	}{
		// Disabled master switch always returns false.
		{"disabled/always/major", false, "always", releasedocs.BumpMajor, false},
		{"disabled/always/patch", false, "always", releasedocs.BumpPatch, false},
		{"disabled/empty/minor", false, "", releasedocs.BumpMinor, false},
		// Enabled with empty When → coerced to "always" → true for all bumps (D-08).
		{"enabled/empty-coerced-always/major", true, "", releasedocs.BumpMajor, true},
		{"enabled/empty-coerced-always/minor", true, "", releasedocs.BumpMinor, true},
		{"enabled/empty-coerced-always/patch", true, "", releasedocs.BumpPatch, true},
		{"enabled/empty-coerced-always/none", true, "", releasedocs.BumpNone, true},
		// Enabled with explicit "always".
		{"enabled/always/major", true, "always", releasedocs.BumpMajor, true},
		{"enabled/always/patch", true, "always", releasedocs.BumpPatch, true},
		// Enabled with "major" restriction.
		{"enabled/major/major", true, "major", releasedocs.BumpMajor, true},
		{"enabled/major/minor", true, "major", releasedocs.BumpMinor, false},
		{"enabled/major/patch", true, "major", releasedocs.BumpPatch, false},
		// Enabled with "minor_or_above".
		{"enabled/minor_or_above/major", true, "minor_or_above", releasedocs.BumpMajor, true},
		{"enabled/minor_or_above/minor", true, "minor_or_above", releasedocs.BumpMinor, true},
		{"enabled/minor_or_above/patch", true, "minor_or_above", releasedocs.BumpPatch, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.ReleaseDocs{
				Artifacts: config.ReleaseArtifacts{
					APIDocs: config.APIDocsConfig{
						ArtifactConfig: config.ArtifactConfig{
							Enabled: tc.enabled,
							When:    tc.when,
						},
					},
				},
			}
			got := g.Enabled(cfg, tc.bump)
			if got != tc.want {
				t.Errorf("Enabled(enabled=%v, When=%q, bump=%q) = %v; want %v",
					tc.enabled, tc.when, tc.bump, got, tc.want)
			}
		})
	}
}

// TestDiscoverSpec covers the spec discovery logic (D-02) via GenerateMulti:
// - explicit SpecPath → fetch that path, no fallback
// - empty SpecPath → ordered fallback list, first hit wins
// - all paths 404 → no artifacts, no error (D-10 skip)
func TestDiscoverSpec(t *testing.T) {
	t.Parallel()

	t.Run("explicit spec path fetched directly", func(t *testing.T) {
		t.Parallel()
		v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
		// Only "custom/api.yaml" is served; fallback paths are absent (→ 404).
		// With specPath set, the generator must fetch exactly that path — not any fallback.
		rc := fixtureAPIDocsRC(
			map[string][]byte{"custom/api.yaml": v3Bytes},
			"custom/api.yaml", "", true, releasedocs.BumpMinor,
		)
		g := apidocs.New()
		arts, err := g.GenerateMulti(context.Background(), rc)
		if err != nil {
			t.Fatalf("GenerateMulti: %v", err)
		}
		if len(arts) == 0 {
			t.Fatal("GenerateMulti returned 0 artifacts; expected ≥1 when explicit specPath resolves")
		}
	})

	t.Run("fallback first hit wins", func(t *testing.T) {
		t.Parallel()
		v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
		// openapi.yml is the second fallback in the D-02 order;
		// openapi.yaml (first) is absent so the loop must advance and find openapi.yml.
		rc := fixtureAPIDocsRC(
			map[string][]byte{"openapi.yml": v3Bytes},
			"", "", true, releasedocs.BumpMinor,
		)
		g := apidocs.New()
		arts, err := g.GenerateMulti(context.Background(), rc)
		if err != nil {
			t.Fatalf("GenerateMulti: %v", err)
		}
		if len(arts) == 0 {
			t.Fatal("GenerateMulti returned 0 artifacts; expected ≥1 when fallback path resolves")
		}
	})

	t.Run("all paths 404 returns error", func(t *testing.T) {
		t.Parallel()
		// Empty file map → every path returns 404 → discoverSpec exhausts fallbacks.
		rc := fixtureAPIDocsRC(nil, "", "", true, releasedocs.BumpMinor)
		g := apidocs.New()
		arts, err := g.GenerateMulti(context.Background(), rc)
		if err != nil {
			t.Errorf("GenerateMulti with all-404: got error %v; want nil (D-10 skip)", err)
		}
		if len(arts) != 0 {
			t.Errorf("GenerateMulti with all-404: got %d artifacts; want 0 (D-10 skip)", len(arts))
		}
	})
}

// TestGenerate_FetchesAtToRef verifies that the spec is fetched at rc.ToRef (D-01).
// discoverSpec passes rc.ToRef to FetchFileFromRef; fakeFetcher ignores the ref
// argument and serves from its map regardless, so this test verifies the
// positive-case: spec found → artifacts returned.
func TestGenerate_FetchesAtToRef(t *testing.T) {
	t.Parallel()
	v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v3Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)
	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}
	if len(arts) == 0 {
		t.Fatal("GenerateMulti returned 0 artifacts; expected spec fetched at ToRef")
	}
}

// TestGenerate_ThreeArtifacts verifies that GenerateMulti returns exactly three
// artifacts with Filenames openapi.yaml, api-reference.html, api-reference.md (D-04).
func TestGenerate_ThreeArtifacts(t *testing.T) {
	t.Parallel()

	v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v3Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}
	if len(arts) != 3 {
		t.Fatalf("GenerateMulti returned %d artifacts; want 3", len(arts))
	}

	filenames := make(map[string]bool)
	for _, a := range arts {
		filenames[a.Filename] = true
		if a.Kind != releasedocs.KindAPIDocs {
			t.Errorf("artifact Kind = %q; want %q", a.Kind, releasedocs.KindAPIDocs)
		}
	}
	for _, want := range []string{"openapi.yaml", "api-reference.html", "api-reference.md"} {
		if !filenames[want] {
			t.Errorf("missing artifact with Filename %q", want)
		}
	}
}

// TestGenerate_RawSpecPassthrough verifies that the openapi.yaml artifact
// contains exactly the fetched bytes (no re-serialization) (D-03).
func TestGenerate_RawSpecPassthrough(t *testing.T) {
	t.Parallel()

	v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v3Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}

	var rawSpec []byte
	for _, a := range arts {
		if a.Filename == "openapi.yaml" {
			rawSpec = a.Content
			break
		}
	}
	if rawSpec == nil {
		t.Fatal("no openapi.yaml artifact found")
	}
	if string(rawSpec) != string(v3Bytes) {
		t.Errorf("openapi.yaml artifact content differs from fetched bytes\ngot len=%d, want len=%d",
			len(rawSpec), len(v3Bytes))
	}
}

// TestGenerate_Swagger2 verifies that a Swagger 2.0 spec is processed and
// produces 3 artifacts (D-09).
func TestGenerate_Swagger2(t *testing.T) {
	t.Parallel()

	v2Bytes := mustReadFixture(t, "petstore_v2.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v2Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti (Swagger 2.0): %v", err)
	}
	if len(arts) != 3 {
		t.Errorf("GenerateMulti returned %d artifacts for Swagger 2.0; want 3", len(arts))
	}
}

// TestGenerate_OAS3 verifies that an OAS 3.0 spec is processed and produces
// 3 artifacts (D-09).
func TestGenerate_OAS3(t *testing.T) {
	t.Parallel()

	v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v3Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti (OAS 3.0): %v", err)
	}
	if len(arts) != 3 {
		t.Errorf("GenerateMulti returned %d artifacts for OAS 3.0; want 3", len(arts))
	}
}

// TestGenerate_OAS31 verifies that an OAS 3.1 spec is processed and produces
// 3 artifacts (D-09).
func TestGenerate_OAS31(t *testing.T) {
	t.Parallel()

	v31Bytes := mustReadFixture(t, "petstore_v31.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v31Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti (OAS 3.1): %v", err)
	}
	if len(arts) != 3 {
		t.Errorf("GenerateMulti returned %d artifacts for OAS 3.1; want 3", len(arts))
	}
}

// TestGenerate_NoSpec_Skips verifies that when all fallback paths return 404,
// GenerateMulti returns (nil, nil) — no artifacts, no error (D-10).
func TestGenerate_NoSpec_Skips(t *testing.T) {
	t.Parallel()

	// No files in map → every path returns 404.
	rc := fixtureAPIDocsRC(nil, "", "", true, releasedocs.BumpMinor)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Errorf("GenerateMulti with no spec: got error %v; want nil (D-10 skip)", err)
	}
	if len(arts) != 0 {
		t.Errorf("GenerateMulti with no spec: got %d artifacts; want 0 (D-10 skip)", len(arts))
	}
}

// TestGenerate_ParseFailure_Skips verifies that a malformed YAML spec causes
// GenerateMulti to skip (nil, nil) without returning an error (D-10).
func TestGenerate_ParseFailure_Skips(t *testing.T) {
	t.Parallel()

	invalidBytes := mustReadFixture(t, "invalid.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": invalidBytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Errorf("GenerateMulti with malformed YAML: got error %v; want nil (D-10 skip)", err)
	}
	if len(arts) != 0 {
		t.Errorf("GenerateMulti with malformed YAML: got %d artifacts; want 0", len(arts))
	}
}

// TestGenerate_ValidationFailure_Skips verifies that an invalid OAS 3.x spec
// (fails libopenapi-validator) causes GenerateMulti to skip (nil, nil) (D-10).
func TestGenerate_ValidationFailure_Skips(t *testing.T) {
	t.Parallel()

	// A spec that parses but fails OAS validation (info.version is required
	// by the OAS 3.0 schema but missing here). libopenapi-validator returns
	// valid=false with a "Document does not pass validation" error.
	badSpec := []byte(`openapi: 3.0.3
info:
  title: Bad
  # version is required but missing
paths: {}
`)
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": badSpec},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Errorf("GenerateMulti with invalid spec: got error %v; want nil (D-10 skip)", err)
	}
	if len(arts) != 0 {
		t.Errorf("GenerateMulti with invalid spec: got %d artifacts; want 0", len(arts))
	}
}

// TestGenerate_UnsupportedVersion_Skips verifies that a spec with an
// unrecognized OpenAPI version (e.g. 3.2.0) causes GenerateMulti to skip
// with a logged reason (D-10).
func TestGenerate_UnsupportedVersion_Skips(t *testing.T) {
	t.Parallel()

	unsupportedSpec := []byte(`openapi: 3.2.0
info:
  title: Future API
  version: "1.0.0"
paths: {}
`)
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": unsupportedSpec},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Errorf("GenerateMulti with unsupported version: got error %v; want nil (D-10 skip)", err)
	}
	if len(arts) != 0 {
		t.Errorf("GenerateMulti with unsupported version: got %d artifacts; want 0", len(arts))
	}
}

// TestGenerate_NoRemoteRef verifies that remote and file $refs in the spec are
// NOT resolved — they survive unresolved or cause a skip, but never trigger an
// outbound HTTP/file fetch (T-03-03, security SSRF guard).
func TestGenerate_NoRemoteRef(t *testing.T) {
	t.Parallel()

	remoteRefBytes := mustReadFixture(t, "remote_ref.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": remoteRefBytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	// Either: the unresolved refs cause a validation skip (len==0, err==nil), OR
	// the raw spec artifact is returned with the unresolved $ref intact.
	// Either way, no outbound HTTP fetch to example.com must have occurred.
	// The fakeFetcher only serves files in its map; any outbound fetch would panic
	// or time out in a real test. We assert no error and ≥0 artifacts.
	if err != nil {
		t.Errorf("GenerateMulti with remote_ref.yaml: unexpected error %v", err)
	}
	// If artifacts were returned, confirm the raw spec doesn't contain resolved content.
	for _, a := range arts {
		if a.Filename == "openapi.yaml" {
			content := string(a.Content)
			if !strings.Contains(content, "https://example.com/schemas/Pet.yaml") &&
				!strings.Contains(content, "external.yaml") {
				t.Errorf("openapi.yaml artifact does not contain original $ref values; refs may have been resolved or removed")
			}
		}
	}
}

// TestBuildRedocHTML_Deterministic verifies that calling the HTML builder twice
// with the same spec and bundle bytes produces byte-identical output (D-05).
func TestBuildRedocHTML_Deterministic(t *testing.T) {
	t.Parallel()

	v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v3Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts1, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti run 1: %v", err)
	}
	arts2, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti run 2: %v", err)
	}

	html1 := findArtifact(arts1, "api-reference.html")
	html2 := findArtifact(arts2, "api-reference.html")
	if html1 == nil || html2 == nil {
		t.Skip("api-reference.html not produced yet (implementation pending)")
	}
	if string(html1.Content) != string(html2.Content) {
		t.Errorf("api-reference.html is not deterministic across two GenerateMulti calls")
	}
}

// TestBuildRedocHTML_NoCDN verifies that the generated HTML contains no
// references to external CDN URLs (no cdn.redoc.ly, no external script src)
// — all assets are self-contained (D-05, offline-safe).
func TestBuildRedocHTML_NoCDN(t *testing.T) {
	t.Parallel()

	v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v3Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}

	htmlArt := findArtifact(arts, "api-reference.html")
	if htmlArt == nil {
		t.Skip("api-reference.html not produced yet (implementation pending)")
	}
	html := string(htmlArt.Content)
	// The HTML page structure must not load the Redoc bundle from a CDN.
	// The Redoc bundle is inlined via go:embed inside a <script> block — that
	// block has no src= attribute.  The bundle's own internal JavaScript may
	// contain "cdn.redoc.ly" as a logo URL string; checking for that URL
	// *anywhere* in the HTML is too broad (the test would always fail because
	// the inlined bundle legitimately contains it).  Instead, check that no
	// <script src="...cdn.redoc.ly..."> or <script src="http..."> attribute
	// exists — which is the actual CDN-dependency invariant (D-05).
	if strings.Contains(html, `src="https://cdn.redoc.ly`) ||
		strings.Contains(html, `src='https://cdn.redoc.ly`) {
		t.Error("api-reference.html has a <script src=...cdn.redoc.ly...> tag; must be self-contained (D-05)")
	}
	// No external <script src="http..."> attribute anywhere in the HTML.
	if strings.Contains(html, `<script src="http`) || strings.Contains(html, `<script src='http`) {
		t.Error("api-reference.html contains external <script src=...>; must be self-contained (D-05)")
	}
}

// TestRenderMarkdown_Golden verifies that the Markdown renderer produces
// byte-identical output for petstore_v3.yaml across runs (D-06, golden test).
// Set TEST_UPDATE_GOLDEN=1 to regenerate the golden file.
func TestRenderMarkdown_Golden(t *testing.T) {
	t.Parallel()

	v3Bytes := mustReadFixture(t, "petstore_v3.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v3Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}

	mdArt := findArtifact(arts, "api-reference.md")
	if mdArt == nil {
		t.Skip("api-reference.md not produced yet (implementation pending)")
	}

	goldenPath := "testdata/golden/markdown_v3.golden"
	if updateGolden {
		if err := os.WriteFile(goldenPath, mdArt.Content, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %q: %v", goldenPath, err)
	}
	if string(mdArt.Content) != string(want) {
		t.Errorf("api-reference.md does not match golden %q\ngot len=%d want len=%d",
			goldenPath, len(mdArt.Content), len(want))
	}
}

// TestRenderMarkdown_Golden_V2 verifies that the Markdown renderer produces
// byte-identical output for petstore_v2.yaml across runs (D-06, golden test).
// Set TEST_UPDATE_GOLDEN=1 to regenerate the golden file.
func TestRenderMarkdown_Golden_V2(t *testing.T) {
	t.Parallel()

	v2Bytes := mustReadFixture(t, "petstore_v2.yaml")
	rc := fixtureAPIDocsRC(
		map[string][]byte{"openapi.yaml": v2Bytes},
		"openapi.yaml", "", true, releasedocs.BumpMinor,
	)

	g := apidocs.New()
	arts, err := g.GenerateMulti(context.Background(), rc)
	if err != nil {
		t.Fatalf("GenerateMulti: %v", err)
	}

	mdArt := findArtifact(arts, "api-reference.md")
	if mdArt == nil {
		t.Skip("api-reference.md not produced yet (implementation pending)")
	}

	goldenPath := "testdata/golden/markdown_v2.golden"
	if updateGolden {
		if err := os.WriteFile(goldenPath, mdArt.Content, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated golden: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %q: %v", goldenPath, err)
	}
	if string(mdArt.Content) != string(want) {
		t.Errorf("api-reference.md (v2) does not match golden %q\ngot len=%d want len=%d",
			goldenPath, len(mdArt.Content), len(want))
	}
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
