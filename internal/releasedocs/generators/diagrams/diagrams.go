package diagrams

import (
	"context"
	"log/slog"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
)

// Generator implements releasedocs.MultiGenerator for the engineering-diagrams
// artifact family. A single GenerateMulti call fetches each user-configured
// committed Mermaid source at the release tag, sniffs it for a type-appropriate
// keyword, and emits one markdown-page Artifact per valid source (differentiated
// by its Filename sub-path, e.g. "diagrams/sequence/login.md"). It is fully
// deterministic (no LLM) and safe for concurrent use.
type Generator struct{}

// New returns a new diagrams Generator.
func New() *Generator { return &Generator{} }

// Kind returns KindDiagrams, identifying the diagrams generator family.
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindDiagrams }

// Enabled reports whether the diagrams generator should run. It returns false
// when cfg.Artifacts.Diagrams.Enabled is false. When the When field is empty,
// it is coerced to "always" (D-08) before delegating to releasedocs.Enabled.
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	artifactCfg := cfg.Artifacts.Diagrams.ArtifactConfig
	// D-08: coerce empty When to "always" — diagrams run on every release by default.
	if artifactCfg.When == "" {
		artifactCfg.When = "always"
	}
	return releasedocs.Enabled(artifactCfg, bump)
}

// Generate satisfies the releasedocs.Generator interface. The diagrams generator
// implements MultiGenerator instead of the single-artifact Generator path, so
// this method is never called by the dispatcher when GenerateMulti is present.
// It returns an empty artifact and no error.
func (g *Generator) Generate(_ context.Context, _ releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	// The dispatcher type-asserts MultiGenerator first and calls GenerateMulti.
	// This path is unreachable in normal operation; it exists only to satisfy
	// the releasedocs.Generator interface required by the registry.
	return releasedocs.Artifact{Kind: releasedocs.KindDiagrams}, nil
}

// diagramTypes is the FIXED, ordered set of diagram types (D-04). It is a slice
// (NOT a Go map) so artifacts are always emitted in this deterministic order —
// sequence, dependency, state, flowchart, class (Pitfall 2). Each entry's paths
// accessor returns the matching DiagramsConfig per-type path list.
var diagramTypes = []struct {
	name  string
	paths func(config.DiagramsConfig) []string
}{
	{"sequence", func(c config.DiagramsConfig) []string { return c.Sequence }},
	{"dependency", func(c config.DiagramsConfig) []string { return c.Dependency }},
	{"state", func(c config.DiagramsConfig) []string { return c.State }},
	{"flowchart", func(c config.DiagramsConfig) []string { return c.Flowchart }},
	{"class", func(c config.DiagramsConfig) []string { return c.Class }},
}

// GenerateMulti implements releasedocs.MultiGenerator. It fetches each
// configured Mermaid source at rc.ToRef, validates it via sniffMermaid, and
// emits one markdown-page Artifact per valid source with a Filename of
// "diagrams/<type>/<base>.md" (D-01, D-03, DIAG-02). Types are iterated in the
// fixed diagramTypes order for deterministic artifact order.
//
// All skip conditions are graceful (DIAG-04, D-08): when the provider does not
// implement FileFetcher it logs and returns (nil, nil); a per-source fetch
// error, a non-Mermaid source, or a duplicate same-type basename is logged with
// slog.Warn and skipped via continue. It NEVER returns a non-nil error for a
// skip — the dispatcher aborts ALL sibling generators on any returned error.
// No LLM call is made on any path (D-06); rc.LLM being nil changes nothing.
func (g *Generator) GenerateMulti(ctx context.Context, rc releasedocs.ReleaseContext) ([]releasedocs.Artifact, error) {
	ff, ok := rc.Provider.(releasedocs.FileFetcher)
	if !ok {
		slog.Warn("diagrams: provider does not implement FileFetcher; skipping", "repo", rc.Repo)
		return nil, nil // D-08 family-level graceful skip
	}

	dc := rc.Config.Artifacts.Diagrams
	var arts []releasedocs.Artifact
	seen := make(map[string]bool) // "<type>/<base>" → already emitted (Pitfall 3)

	for _, t := range diagramTypes {
		for _, p := range t.paths(dc) {
			b, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, p)
			if err != nil {
				slog.Warn("diagrams: source not fetched, skipping",
					"type", t.name, "path", p, "toRef", rc.ToRef, "err", err)
				continue
			}
			if !sniffMermaid(b, t.name) {
				slog.Warn("diagrams: source not valid Mermaid for type, skipping",
					"type", t.name, "path", p, "toRef", rc.ToRef)
				continue
			}
			base := diagramName(p)
			key := t.name + "/" + base
			if seen[key] {
				// Pitfall 3: first-listed wins, deterministic.
				slog.Warn("diagrams: duplicate basename within type, skipping",
					"type", t.name, "path", p, "name", base)
				continue
			}
			seen[key] = true
			arts = append(arts, releasedocs.Artifact{
				Kind:     releasedocs.KindDiagrams,
				Filename: "diagrams/" + t.name + "/" + base + ".md",
				Content:  wrapMermaidFence(b),
			})
		}
	}

	return arts, nil
}

// Compile-time assertions: Generator implements both releasedocs.Generator
// and releasedocs.MultiGenerator.
var _ releasedocs.Generator = (*Generator)(nil)
var _ releasedocs.MultiGenerator = (*Generator)(nil)
