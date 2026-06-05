// Package config loads Cadoo's per-repo configuration (.cadoo.yaml).
// Org-level defaults come from the dashboard and are merged at runtime;
// Phase 0 only implements the per-repo loader.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// Repo is the on-disk schema of .cadoo.yaml.
type Repo struct {
	Model            string            `yaml:"model"`
	Auto             map[string]string `yaml:"auto"`
	Review           ReviewSection     `yaml:"review"`
	PathInstructions []PathInstruction `yaml:"path_instructions"`
	Checks           []Check           `yaml:"checks"`
	AstGrep          AstGrep           `yaml:"ast_grep"`
	KB               KB                `yaml:"kb"`

	// Strictness shapes how aggressively /review surfaces findings.
	// One of: minimal | balanced | strict | pedantic. Empty == balanced.
	Strictness string `yaml:"strictness"`

	// Conventions are team-authoritative rules injected into every review's
	// user message. Use this for "we always X" / "never Y" guidance.
	Conventions []string `yaml:"conventions"`

	// StyleGuides are per-language style blurbs (key is the language name
	// or extension; value is the guidance). Surfaced to the model alongside
	// Conventions.
	StyleGuides map[string]string `yaml:"style_guides"`

	// Prompts override or extend each tool's system prompt by name.
	// Example: prompts: { review: { addendum: "Always cite the spec." } }
	Prompts map[string]PromptCustomization `yaml:"prompts"`

	// CommentPolicy keeps Cadoo from spamming clean PRs. Defaults are
	// noise-averse: silent on clean, skip nit-only runs, require at least
	// one finding to post anything.
	CommentPolicy CommentPolicy `yaml:"comment_policy"`

	// ReleaseDocs configures the automated release-artifact generation and
	// publishing subsystem. Loaded from the release tag's tree (never from
	// main — consistent with the existing "config from head" rule).
	ReleaseDocs ReleaseDocs `yaml:"releaseDocs"`
}

// ReleaseDocs is the top-level release-docs configuration block in
// .cadoo.yaml. All fields are optional; zero values disable the feature.
type ReleaseDocs struct {
	// Enabled is the master switch for the release-docs subsystem.
	// When false (the default), no artifacts are generated or published.
	Enabled bool `yaml:"enabled"`
	// Trigger controls what event activates release-docs generation.
	// Accepted values: "release" (VCS release event, default), "tag"
	// (tag push). Manual CLI invocation always runs regardless of this field.
	Trigger string `yaml:"trigger"`
	// TagPattern is a glob matched against VCS tag names to decide which tags
	// trigger release-docs generation (e.g. "v*"). Empty means "v*".
	TagPattern string `yaml:"tagPattern"`
	// Artifacts configures per-artifact enabled + condition settings.
	Artifacts ReleaseArtifacts `yaml:"artifacts"`
	// Grouping controls how commits and merged PRs are classified into
	// changelog sections.
	Grouping ReleaseGrouping `yaml:"grouping"`
	// Publish controls which publish destinations are active.
	Publish ReleasePublish `yaml:"publish"`
}

// ReleaseArtifacts groups the per-artifact configuration entries.
type ReleaseArtifacts struct {
	// Changelog configures the machine-formatted CHANGELOG.md-style artifact.
	Changelog ArtifactConfig `yaml:"changelog"`
	// ReleaseNotes configures the LLM-polished release narrative artifact.
	ReleaseNotes ReleaseNotesConfig `yaml:"releaseNotes"`
	// Blog configures the long-form release announcement artifact (blog post).
	// When Enabled, the blog generator produces a publication-ready post from the
	// release context and any configured template. Wired in Phase 2 plan 03.
	Blog ArtifactConfig `yaml:"blog"`
	// APIDocs configures the API documentation artifact family (raw spec + Redoc
	// HTML + Markdown reference). All three outputs are gated together by a
	// single enabled + when: condition (D-07). Wired in Phase 3 plan 03.
	APIDocs APIDocsConfig `yaml:"apiDocs"`
}

// ArtifactConfig holds the common per-artifact settings shared by changelog
// and (as the base) release-notes.
type ArtifactConfig struct {
	// Enabled controls whether this artifact is generated. Defaults to false.
	Enabled bool `yaml:"enabled"`
	// When is a condition expression keyed off the computed semver bump.
	// Accepted values: "always", "major", "minor", "patch", "minor_or_above",
	// "patch_or_above". Empty means "always" when Enabled is true.
	When string `yaml:"when"`
	// Preset selects the built-in template preset for this artifact.
	// Accepted values: "default", "compact", "detailed". Empty means "default".
	Preset string `yaml:"preset"`
	// Template is a repository-relative path to a custom Go text/template file
	// that overrides the preset entirely. Loaded from the release tag tree.
	// Empty means use the preset.
	Template string `yaml:"template"`
}

// ReleaseNotesConfig extends ArtifactConfig with release-notes-specific
// settings.
type ReleaseNotesConfig struct {
	ArtifactConfig `yaml:",inline"`
	// Tone shapes the LLM's writing style for the release narrative.
	// Accepted values: "concise" (default), "detailed", "marketing".
	Tone string `yaml:"tone"`
}

// APIDocsConfig extends ArtifactConfig with apidocs-specific settings. It
// mirrors the ReleaseNotesConfig inline-embed + extra-field pattern.
// All three apidocs outputs (raw spec, Redoc HTML, Markdown reference) are
// gated together by a single enabled + when: condition (D-07). The default
// when: is "always" (D-08).
type APIDocsConfig struct {
	ArtifactConfig `yaml:",inline"`
	// SpecPath is the repository-relative path to the committed OpenAPI/Swagger
	// spec file. When empty, the apidocs generator uses the conventional
	// fallback discovery list (D-02):
	//   openapi.yaml → openapi.yml → openapi.json → docs/openapi.yaml → api/openapi.yaml
	// The file is read at rc.ToRef (the release tag), never from the default
	// branch (consistent with the "config from head" rule).
	SpecPath string `yaml:"specPath"`
}

// ReleaseGrouping controls how commits and merged PRs are classified into
// changelog sections.
type ReleaseGrouping struct {
	// Source is the grouping strategy. Accepted values: "conventional"
	// (parse Conventional Commit prefixes), "labels" (group by PR label).
	// "llm" is reserved for a future phase. Empty means "conventional".
	Source string `yaml:"source"`
	// Sections is the canonical display order for changelog sections. When
	// empty, a built-in default order is used (Features, Bug Fixes, etc.).
	Sections []string `yaml:"sections"`
	// Labels maps PR label names to their display section. Used when
	// Source is "labels". Keys are label names; values are section titles.
	Labels map[string]string `yaml:"labels"`
}

// ReleasePublish controls which publish destinations are active.
type ReleasePublish struct {
	// ReleaseBody configures publishing the generated content into the VCS
	// release body via idempotent marker splice.
	ReleaseBody PublishTarget `yaml:"releaseBody"`
	// ChangelogPR configures publishing the changelog section via a single
	// deduplicated pull-request to CHANGELOG.md.
	ChangelogPR PublishTarget `yaml:"changelogPR"`
	// Pages configures publishing to a docs branch or GitHub/GitLab Pages site,
	// including branch and directory targeting. Wired in Phase 2 plan 04.
	Pages PagesPublishTarget `yaml:"pages"`
}

// PublishTarget holds the per-destination publish settings.
type PublishTarget struct {
	// Enabled controls whether this publish destination is active.
	Enabled bool `yaml:"enabled"`
}

// PagesPublishTarget configures publishing to a docs branch or GitHub/GitLab
// Pages site. It extends the basic enabled flag with branch and directory
// targeting so the pages publisher knows where to write generated artifacts.
type PagesPublishTarget struct {
	// Enabled controls whether the pages publish destination is active.
	Enabled bool `yaml:"enabled"`
	// Branch is the VCS branch used for the pages site (e.g. "gh-pages").
	// Empty defaults to "gh-pages", applied by the pages publisher.
	Branch string `yaml:"branch"`
	// Dir is the repository-relative directory within Branch where artifacts
	// are written (e.g. "docs"). Empty defaults to "docs", applied by the
	// pages publisher.
	Dir string `yaml:"dir"`
}

// PromptCustomization lets users override or extend a tool's system prompt.
// Override wins; otherwise Addendum is appended to the tool's default prompt.
type PromptCustomization struct {
	Override string `yaml:"override"`
	Addendum string `yaml:"addendum"`
}

// CommentPolicy governs when /review actually posts visible comments.
type CommentPolicy struct {
	// SilentOnClean: when true and the run has zero post-threshold findings,
	// suppress inline comments. The summary is also suppressed unless
	// StatsOnClean is true (see below).
	SilentOnClean bool `yaml:"silent_on_clean"`
	// StatsOnClean: when true (and the run is clean), still post a compact
	// one-comment stats summary ("Cadoo reviewed N files, no findings")
	// instead of leaving the PR with only a green check-run. Ignored if
	// SilentOnClean is false (the chatty path posts the full summary).
	StatsOnClean bool `yaml:"stats_on_clean"`
	// SkipIfOnlyNits: when true, drop the entire post if every finding is
	// a nit. Useful as a stronger noise filter than severity_threshold.
	SkipIfOnlyNits bool `yaml:"skip_if_only_nits"`
	// MinFindingsToPost: don't post anything if there are fewer than this
	// many post-threshold findings. 0 = always post when there's at least
	// one finding.
	MinFindingsToPost int `yaml:"min_findings_to_post"`
}

// ReviewSection controls /review behaviour.
type ReviewSection struct {
	SeverityThreshold string   `yaml:"severity_threshold"`
	IncludePaths      []string `yaml:"include_paths"`
	ExcludePaths      []string `yaml:"exclude_paths"`
	MaxComments       int      `yaml:"max_comments"`
	RequestChangesOn  []string `yaml:"request_changes_on"`
}

// PathInstruction binds a glob set to natural-language guidance.
type PathInstruction struct {
	Paths        []string `yaml:"paths"`
	Instructions string   `yaml:"instructions"`
}

// Check is a custom NL check.
type Check struct {
	Name     string   `yaml:"name"`
	Paths    []string `yaml:"paths"`
	Prompt   string   `yaml:"prompt"`
	Severity string   `yaml:"severity"`
}

// AstGrep references rule files for structural matching.
type AstGrep struct {
	Rules []string `yaml:"rules"`
}

// KB lists cross-repo knowledge sources.
type KB struct {
	CrossRepos []string `yaml:"cross_repos"`
}

// Default returns sensible defaults for an empty config.
//
// Note: Model intentionally has no default. Operators must set it either
// per-repo (.cadoo.yaml `model:`) or globally (CADOO_DEFAULT_MODEL). The
// dispatcher fails fast with a clear message when neither is set, which we
// prefer over silently routing every PR through whatever Cadoo shipped as
// the implicit default.
func Default() Repo {
	return Repo{
		Auto: map[string]string{"review": "on_open", "describe": "on_open"},
		Review: ReviewSection{
			SeverityThreshold: "warn",
			IncludePaths:      []string{"**/*"},
			MaxComments:       30,
		},
		Strictness: "balanced",
		// Noise-averse defaults: stay silent on clean PRs, drop nit-only runs.
		// Users can flip these in .cadoo.yaml to get the chattier behaviour.
		CommentPolicy: CommentPolicy{
			SilentOnClean:     true,
			StatsOnClean:      true,
			SkipIfOnlyNits:    true,
			MinFindingsToPost: 1,
		},
	}
}

// LoadFile reads and parses a YAML file. A missing file returns Default()
// with no error so the loader is safe to call against repos that don't
// ship a .cadoo.yaml.
func LoadFile(path string) (Repo, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Repo{}, fmt.Errorf("read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes raw YAML bytes into Repo. Empty input returns Default().
func Parse(raw []byte) (Repo, error) {
	if len(raw) == 0 {
		return Default(), nil
	}
	var r Repo
	if err := yaml.Unmarshal(raw, &r); err != nil {
		return Repo{}, fmt.Errorf("parse cadoo.yaml: %w", err)
	}
	return r, nil
}
