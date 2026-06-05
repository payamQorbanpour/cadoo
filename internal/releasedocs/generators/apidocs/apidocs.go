// Package apidocs implements the API-docs Generator for the release-docs
// subsystem. The generator fetches a committed OpenAPI/Swagger spec via
// FileFetcher, validates it, and emits three artifacts: the raw spec, a
// self-contained Redoc HTML reference, and a deterministic Markdown reference.
// The generator is fully deterministic (no LLM). When no valid spec exists,
// it skips all three artifacts with a logged reason (D-10).
//
// Implementation: Plans 03-05 fill in parse.go, render_html.go, and
// render_markdown.go. This file provides the Generator/MultiGenerator
// scaffolding and the Enabled gate (D-07/D-08). The stub GenerateMulti
// returns (nil, nil) until Plans 03-05 land.
package apidocs

import (
	"context"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
)

// Generator implements releasedocs.MultiGenerator for the API docs artifact
// family. It emits three artifacts per GenerateMulti call: the raw OpenAPI
// spec (openapi.yaml), a self-contained Redoc HTML reference
// (api-reference.html), and a deterministic Markdown reference
// (api-reference.md). It is safe for concurrent use.
type Generator struct{}

// New returns a new apidocs Generator.
func New() *Generator { return &Generator{} }

// Kind returns KindAPIDocs, identifying the apidocs generator family.
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindAPIDocs }

// Enabled reports whether the apidocs generator should run. It returns false
// when cfg.Artifacts.APIDocs.Enabled is false. When the When field is empty,
// it is coerced to "always" (D-08) before delegating to releasedocs.Enabled.
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	artifactCfg := cfg.Artifacts.APIDocs.ArtifactConfig
	// D-08: coerce empty When to "always" — apidocs runs on every release by default.
	if artifactCfg.When == "" {
		artifactCfg.When = "always"
	}
	return releasedocs.Enabled(artifactCfg, bump)
}

// Generate satisfies the releasedocs.Generator interface. The apidocs generator
// implements MultiGenerator instead of the single-artifact Generator path, so
// this method is never called by the dispatcher when GenerateMulti is present.
// It returns an empty artifact and no error.
func (g *Generator) Generate(_ context.Context, _ releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	// The dispatcher type-asserts MultiGenerator first and calls GenerateMulti.
	// This path is unreachable in normal operation; it exists only to satisfy
	// the releasedocs.Generator interface required by the registry.
	return releasedocs.Artifact{Kind: releasedocs.KindAPIDocs}, nil
}

// GenerateMulti implements releasedocs.MultiGenerator. It fetches the committed
// OpenAPI/Swagger spec via FileFetcher, validates it, and emits three artifacts:
//   - openapi.yaml: raw spec bytes (D-03, D-09)
//   - api-reference.html: self-contained Redoc HTML (D-05)
//   - api-reference.md: deterministic Markdown reference (D-06)
//
// On any skip condition (spec not found, parse failure, validation failure,
// unsupported version), it returns (nil, nil) and logs the reason (D-10).
// It never returns a non-nil error for skip conditions.
//
// NOTE: Plans 03-05 implement discoverSpec, parseSpec, buildRedocHTML, and
// renderMarkdown. Until those plans land, GenerateMulti returns (nil, nil).
func (g *Generator) GenerateMulti(_ context.Context, _ releasedocs.ReleaseContext) ([]releasedocs.Artifact, error) {
	// TODO(03-03): implement discoverSpec + parseSpec + render pipeline.
	// Until Plans 03-05 land, return nil (skip-with-no-error, D-10 stub).
	return nil, nil
}

// Compile-time assertions: Generator implements both releasedocs.Generator
// and releasedocs.MultiGenerator.
var _ releasedocs.Generator = (*Generator)(nil)
var _ releasedocs.MultiGenerator = (*Generator)(nil)
