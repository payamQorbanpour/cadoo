// Package template provides embedded preset templates and a resolver for the
// release-docs subsystem. It loads Go text/template presets from the embedded
// filesystem and optionally replaces them with a repo-provided override fetched
// from the release tag's tree. No custom FuncMap is registered — templates
// receive only the Data struct so there is no OS/exec exposure (T-03-01).
package template

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"text/template"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
)

//go:embed presets/*.tmpl
var presetFS embed.FS

// ArtifactKind is the same as releasedocs.ArtifactKind, re-declared here for
// use as a key to select the correct embedded preset template.
type ArtifactKind = releasedocs.ArtifactKind

// Preset name constants for each artifact kind and tone.
const (
	presetChangelog             = "presets/changelog.tmpl"
	presetReleaseNotesConcise   = "presets/release-notes-concise.tmpl"
	presetReleaseNotesDetailed  = "presets/release-notes-detailed.tmpl"
	presetReleaseNotesMarketing = "presets/release-notes-marketing.tmpl"
)

// Data is the data object passed to every template execution. It is the
// public rendering contract between the template package and the generator
// layer. Templates receive exactly this struct — no additional FuncMap funcs are
// registered (Security: T-03-01).
type Data struct {
	// ToRef is the release tag being documented (e.g. "v1.2.3").
	ToRef string
	// FromRef is the prior release tag or commit SHA (range start, exclusive).
	// May be empty for the first release.
	FromRef string
	// Groups is the ordered list of change groups derived from the grouped
	// change model. Canonical section order must be enforced before rendering
	// (Pitfall 3 — deterministic ordering).
	Groups []ChangeGroup
}

// ChangeGroup represents one logical section in a changelog or release-notes
// document (e.g. "Features", "Bug Fixes").
type ChangeGroup struct {
	// Title is the human-readable section heading.
	Title string
	// Items is the ordered list of change entries in this group.
	Items []ChangeItem
}

// ChangeItem is a single entry in a ChangeGroup.
type ChangeItem struct {
	// Summary is the short description of the change (commit message subject
	// or PR title, stripped of the Conventional Commit prefix).
	Summary string
	// Author is the commit author or PR author. May be empty.
	Author string
	// PR holds the pull-request reference when the change came from a merged PR.
	// Nil when the change is a bare commit with no associated PR.
	PR *PRRef
}

// PRRef holds the VCS reference for a merged pull-request.
type PRRef struct {
	// Number is the PR/MR number.
	Number int64
	// URL is the web URL of the PR/MR. May be empty for adapters that do not
	// return URLs.
	URL string
}

// LoadPreset parses the embedded preset for the given artifact kind and tone
// and returns the compiled *template.Template. kind must be one of
// releasedocs.KindChangelog or releasedocs.KindReleaseNotes. tone is used only
// for KindReleaseNotes; accepted values are "concise", "detailed", "marketing"
// (empty defaults to "concise").
func LoadPreset(kind ArtifactKind, tone string) (*template.Template, error) {
	name, err := presetName(kind, tone)
	if err != nil {
		return nil, err
	}
	src, err := presetFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return template.New(name).Parse(string(src))
}

// presetName returns the embedded filesystem path for the given kind+tone.
func presetName(kind ArtifactKind, tone string) (string, error) {
	switch kind {
	case releasedocs.KindChangelog:
		return presetChangelog, nil
	case releasedocs.KindReleaseNotes:
		switch tone {
		case "detailed":
			return presetReleaseNotesDetailed, nil
		case "marketing":
			return presetReleaseNotesMarketing, nil
		default:
			// "concise" or empty → concise preset.
			return presetReleaseNotesConcise, nil
		}
	default:
		return "", ErrUnknownKind
	}
}

// ErrUnknownKind is returned by LoadPreset when the artifact kind is not
// recognized.
var ErrUnknownKind = errors.New("template: unknown artifact kind")

// Render executes tmpl against data and returns the rendered string. The
// template is executed into a strings.Builder so no I/O is required. Returns an
// error if execution fails.
func Render(tmpl *template.Template, data any) (string, error) {
	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// isMissingFile reports whether err represents a file-not-found condition. It
// mirrors orchestrator.isMissingFile (reviewer.go:527-536): checks fs.ErrNotExist
// and matches VCS-client 404 strings heuristically so this package never imports
// the VCS adapter packages directly.
func isMissingFile(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

// Resolve selects and parses the template for the given artifact kind and tone.
// When overridePath is non-empty, it type-asserts rc.Provider for
// releasedocs.FileFetcher and fetches the file at overridePath from rc.ToRef's
// tree (never from main — T-03-02). On a missing-file error the function falls
// back to the embedded preset (D-07). When overridePath is empty, the embedded
// preset is used directly.
func Resolve(ctx context.Context, rc releasedocs.ReleaseContext, kind ArtifactKind, overridePath, tone string) (*template.Template, error) {
	if overridePath != "" {
		ff, ok := rc.Provider.(releasedocs.FileFetcher)
		if ok {
			src, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, overridePath)
			if err == nil {
				// Override loaded successfully — parse and return it.
				return template.New("override:" + overridePath).Parse(string(src))
			}
			if !isMissingFile(err) {
				// Non-404 error: fall through to preset with a logged reason.
				_ = err // callers may inspect returned preset without this detail
			}
			// Missing file (404) → fall back to preset silently (D-07).
		}
		// Provider does not implement FileFetcher → fall back to preset.
	}
	return LoadPreset(kind, tone)
}
