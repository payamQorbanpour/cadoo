// Package releasenotes implements the release-notes Generator for the
// release-docs subsystem. The generator builds a deterministic highlight
// skeleton from the grouped change model and — when rc.LLM is non-nil —
// calls LLM.Chat once to author a tone-aware narrative on top of the skeleton.
// When rc.LLM is nil, the skeleton is returned verbatim (nil-tolerant, D-11).
package releasenotes

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	rdtemplate "github.com/payamqorbanpour/cadoo/internal/releasedocs/template"
)

// Generator implements releasedocs.Generator for the release-notes artifact
// kind. It is safe for concurrent use.
type Generator struct{}

// New returns a new release-notes Generator.
func New() *Generator { return &Generator{} }

// Kind implements releasedocs.Generator. Returns KindReleaseNotes.
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindReleaseNotes }

// Enabled implements releasedocs.Generator. It delegates to the Plan-02
// releasedocs.Enabled gate using the releaseNotes artifact config.
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	return releasedocs.Enabled(cfg.Artifacts.ReleaseNotes.ArtifactConfig, bump)
}

// Generate implements releasedocs.Generator. It builds a deterministic
// highlight skeleton from rc.GroupedModel using the tone-keyed preset (Plan 03).
// When rc.LLM is non-nil, it calls Chat once to author a tone-aware narrative
// on top of the skeleton. When rc.LLM is nil, the skeleton is returned verbatim
// — no Chat call (D-11, T-05-01). rc.Model is passed through to Chat (D-17).
func (g *Generator) Generate(ctx context.Context, rc releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	tone := rc.Config.Artifacts.ReleaseNotes.Tone
	overridePath := rc.Config.Artifacts.ReleaseNotes.Template

	// Resolve the tone-keyed preset or custom override template (Plan 03).
	tmpl, err := rdtemplate.Resolve(ctx, rc, releasedocs.KindReleaseNotes, overridePath, tone)
	if err != nil {
		return releasedocs.Artifact{}, fmt.Errorf("releasenotes: resolve template: %w", err)
	}

	// Build the template data from the grouped model. Canonical section order
	// is guaranteed by BuildGroupedModel (Pitfall 3).
	data := buildTemplateData(rc)

	// Render the deterministic skeleton.
	skeleton, err := rdtemplate.Render(tmpl, data)
	if err != nil {
		return releasedocs.Artifact{}, fmt.Errorf("releasenotes: render skeleton: %w", err)
	}

	// When rc.LLM is nil, return the skeleton verbatim (nil-tolerant D-11).
	if rc.LLM == nil {
		return releasedocs.Artifact{
			Kind:    releasedocs.KindReleaseNotes,
			Content: []byte(skeleton),
		}, nil
	}

	// LLM present: call Chat once to author a tone-aware narrative.
	narrative, err := narrateWithLLM(ctx, rc.LLM, rc.Model, skeleton, tone)
	if err != nil {
		// Narrative failure is non-fatal: fall back to skeleton (D-10).
		return releasedocs.Artifact{
			Kind:    releasedocs.KindReleaseNotes,
			Content: []byte(skeleton),
		}, nil
	}

	return releasedocs.Artifact{
		Kind:    releasedocs.KindReleaseNotes,
		Content: []byte(narrative),
	}, nil
}

// buildTemplateData converts the grouped model in rc into the
// rdtemplate.Data shape expected by the release-notes template.
func buildTemplateData(rc releasedocs.ReleaseContext) rdtemplate.Data {
	groups := make([]rdtemplate.ChangeGroup, 0, len(rc.GroupedModel.Sections))
	for _, sec := range rc.GroupedModel.Sections {
		items := make([]rdtemplate.ChangeItem, 0, len(sec.Entries))
		for _, e := range sec.Entries {
			item := rdtemplate.ChangeItem{
				Summary: e.Title,
				Author:  e.Author,
			}
			if e.PRNumber != 0 {
				item.PR = &rdtemplate.PRRef{Number: e.PRNumber}
			}
			items = append(items, item)
		}
		groups = append(groups, rdtemplate.ChangeGroup{
			Title: sec.Title,
			Items: items,
		})
	}
	return rdtemplate.Data{
		ToRef:   rc.ToRef,
		FromRef: rc.FromRef,
		Groups:  groups,
	}
}

// narrateWithLLM calls provider.Chat once to author a tone-aware release
// narrative based on the deterministic skeleton. The tone parameter selects
// the writing style (concise/detailed/marketing). model is passed through to
// Chat without a second default-model path (D-17).
func narrateWithLLM(ctx context.Context, provider llm.Provider, model, skeleton, tone string) (string, error) {
	systemPrompt := buildSystemPrompt(tone)

	req := llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: skeleton},
		},
	}
	resp, err := provider.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("releasenotes: LLM narrative: %w", err)
	}
	narrative := strings.TrimSpace(resp.Content)
	if narrative == "" {
		return skeleton, nil
	}
	return narrative, nil
}

// buildSystemPrompt returns a tone-appropriate system prompt for the LLM
// narrative call. Accepted tone values: "concise", "detailed", "marketing"
// (empty defaults to "concise").
func buildSystemPrompt(tone string) string {
	switch tone {
	case "detailed":
		return `You are a technical writer authoring detailed release notes.
Transform the provided release skeleton into a comprehensive, technically precise release narrative.
- Expand each section with context on why changes matter.
- Preserve all sections and entries.
- Output release-ready Markdown only.`
	case "marketing":
		return `You are a product marketer writing exciting release announcement notes.
Transform the provided release skeleton into a compelling, customer-focused release announcement.
- Highlight user benefits and value.
- Use enthusiastic but professional language.
- Preserve all sections and entries.
- Output release-ready Markdown only.`
	default:
		// "concise" or empty
		return `You are a technical writer authoring concise release notes.
Transform the provided release skeleton into clear, readable release notes.
- Keep it brief and scannable.
- Preserve all sections and entries.
- Output release-ready Markdown only.`
	}
}
