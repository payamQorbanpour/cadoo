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
