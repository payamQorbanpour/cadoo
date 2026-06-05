// Package changelog implements the deterministic-first changelog Generator for
// the release-docs subsystem. The generator renders a grouped change model into
// a markdown section deterministically (golden-file testable) and optionally
// applies an LLM polish pass when rc.LLM is non-nil. When rc.LLM is nil the
// deterministic render is returned verbatim — no LLM call is attempted.
package changelog

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	rdtemplate "github.com/payamqorbanpour/cadoo/internal/releasedocs/template"
)

// Generator implements releasedocs.Generator for the changelog artifact kind.
// It is safe for concurrent use.
type Generator struct{}

// New returns a new changelog Generator.
func New() *Generator { return &Generator{} }

// Kind implements releasedocs.Generator. Returns KindChangelog.
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindChangelog }

// Enabled implements releasedocs.Generator. It delegates to the Plan-02
// releasedocs.Enabled gate using the changelog artifact config.
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	return releasedocs.Enabled(cfg.Artifacts.Changelog, bump)
}

// Generate implements releasedocs.Generator. It renders the grouped change
// model in rc.GroupedModel into a changelog markdown section using the preset
// or custom template. Sections are rendered in canonical order (Pitfall 3 —
// never map order). When rc.LLM is nil, the deterministic render is returned
// verbatim (no Chat call). When rc.LLM is non-nil, a single optional polish
// pass is performed; the polish must not change which entries appear.
func (g *Generator) Generate(ctx context.Context, rc releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	// Resolve the template (preset or repo-level override).
	overridePath := rc.Config.Artifacts.Changelog.Template
	tmpl, err := rdtemplate.Resolve(ctx, rc, releasedocs.KindChangelog, overridePath, "")
	if err != nil {
		return releasedocs.Artifact{}, fmt.Errorf("changelog: resolve template: %w", err)
	}

	// Build the template data from the grouped model. The GroupedModel sections
	// are already in canonical order (BuildGroupedModel guarantees this). We
	// simply map them to the template.Data shape.
	data := buildTemplateData(rc)

	// Render deterministically.
	rendered, err := rdtemplate.Render(tmpl, data)
	if err != nil {
		return releasedocs.Artifact{}, fmt.Errorf("changelog: render template: %w", err)
	}

	// LLM polish is a SEPARATE, SKIPPABLE step. When rc.LLM is nil we return
	// the deterministic render verbatim — no Chat call (T-05-01).
	if rc.LLM == nil {
		return releasedocs.Artifact{
			Kind:    releasedocs.KindChangelog,
			Content: []byte(rendered),
		}, nil
	}

	// Optional polish pass: call LLM once to improve wording only. The polish
	// does NOT change which entries appear — it only refines phrasing.
	polished, err := polishWithLLM(ctx, rc.LLM, rc.Model, rendered)
	if err != nil {
		// Polish failure is non-fatal: fall back to the deterministic render
		// (D-10 — LLM is optional, deterministic output is always valid).
		return releasedocs.Artifact{
			Kind:    releasedocs.KindChangelog,
			Content: []byte(rendered),
		}, nil
	}

	return releasedocs.Artifact{
		Kind:    releasedocs.KindChangelog,
		Content: []byte(polished),
	}, nil
}

// buildTemplateData converts the grouped model in rc into the
// rdtemplate.Data shape expected by the changelog template.
func buildTemplateData(rc releasedocs.ReleaseContext) rdtemplate.Data {
	groups := make([]rdtemplate.ChangeGroup, 0, len(rc.GroupedModel.Sections))
	for _, sec := range rc.GroupedModel.Sections {
		items := make([]rdtemplate.ChangeItem, 0, len(sec.Entries))
		for _, e := range sec.Entries {
			item := rdtemplate.ChangeItem{
				Summary: stripConventionalPrefix(e.Title),
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

// stripConventionalPrefix removes the Conventional Commit type prefix from an
// entry title that was already classified (e.g. "feat: add dark mode" →
// "add dark mode"). If there is no recognized prefix the title is returned
// unchanged.
func stripConventionalPrefix(title string) string {
	prefixes := []string{
		"feat!: ", "fix!: ", "feat: ", "fix: ", "perf: ",
		"chore: ", "docs: ", "style: ", "refactor: ", "test: ", "build: ", "ci: ",
	}
	lower := strings.ToLower(title)
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			rest := title[len(p):]
			if len(rest) > 0 {
				// Capitalize the first letter of the remainder.
				return strings.ToUpper(rest[:1]) + rest[1:]
			}
			return rest
		}
	}
	return title
}

// polishWithLLM calls rc.LLM.Chat once to improve the wording of the already-
// rendered changelog section. The polish must NOT add or remove entries; it
// only refines phrasing. The model is passed via rc.Model (D-17 — no second
// default-model path).
func polishWithLLM(ctx context.Context, provider llm.Provider, model, rendered string) (string, error) {
	const systemPrompt = `You are a technical writer polishing a software changelog section.
Your task: improve the phrasing and clarity of the provided changelog entries.
Rules:
- Do NOT add, remove, or reorder any entries.
- Do NOT change section headings.
- Preserve all Markdown formatting (##, ###, -).
- Return ONLY the improved changelog text, no commentary.`

	req := llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: rendered},
		},
	}
	resp, err := provider.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("changelog: LLM polish: %w", err)
	}
	polished := strings.TrimSpace(resp.Content)
	if polished == "" {
		return rendered, nil
	}
	return polished, nil
}
