// Package defaults wires the built-in Generator and Publisher implementations
// into ordered slices ready for use by the releasedocs.Dispatcher. It is a
// thin, cycle-free entry-point package: it imports both the core releasedocs
// types and the generator/publisher sub-packages without introducing a cycle
// (the sub-packages import releasedocs for types; defaults is never imported
// by releasedocs or its sub-packages).
//
// Cmd binaries (cadoo-cli, cadoo-worker) call DefaultGenerators/DefaultPublishers
// at startup. Tests construct their own recording fakes instead.
package defaults

import (
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/apidocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/blog"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/changelog"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/releasenotes"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/changelogpr"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/pages"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/releasebody"
)

// DefaultGenerators returns the ordered slice of built-in Generator
// implementations in their canonical execution order:
//  1. changelog.Generator   — deterministic CHANGELOG.md-style artifact.
//  2. releasenotes.Generator — LLM-polished release narrative.
//  3. blog.Generator        — LLM-authored blog-post announcement (minor/major only).
//  4. apidocs.Generator     — spec + offline Redoc HTML + Markdown; emits three
//     Filename-differentiated artifacts via GenerateMulti (multi-artifact path).
//
// The caller (Dispatcher.Generators) controls execution; disabled generators
// are skipped by the dispatcher before Generate is called (D-08). The apidocs
// generator implements MultiGenerator so the dispatcher calls GenerateMulti
// rather than the single-artifact Generate path.
func DefaultGenerators() []releasedocs.Generator {
	return []releasedocs.Generator{
		changelog.New(),
		releasenotes.New(),
		blog.New(),
		apidocs.New(),
	}
}

// DefaultPublishers returns the ordered slice of built-in Publisher
// implementations in their canonical execution order:
//  1. releasebody.Publisher  — splices release-notes into the VCS release body.
//  2. changelogpr.Publisher  — maintains a single CHANGELOG.md PR per release.
//  3. pages.Publisher        — commits each artifact to a docs branch at a
//     deterministic path (REQ-publish-destinations).
//
// Publishers that lack a required VCS capability degrade gracefully (D-15).
func DefaultPublishers() []releasedocs.Publisher {
	return []releasedocs.Publisher{
		releasebody.Publisher{},
		changelogpr.Publisher{},
		pages.Publisher{},
	}
}
