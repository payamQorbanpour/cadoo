// Package blog implements the blog-post Generator for the release-docs
// subsystem. The generator builds a deterministic long-form announcement
// skeleton from the grouped change model and — when rc.LLM is non-nil —
// calls LLM.Chat once to author a publication-ready blog post on top of the
// skeleton. When rc.LLM is nil, the skeleton is returned verbatim
// (nil-tolerant, D-11). Blog is only enabled on minor/major releases by
// default (When: "" coerces to "minor_or_above"), unlike release-notes
// which defaults to "always".
package blog

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
)

// Generator implements releasedocs.Generator for the blog artifact kind.
// It is safe for concurrent use.
type Generator struct{}

// New returns a new blog Generator.
func New() *Generator { return &Generator{} }

// Kind implements releasedocs.Generator. Returns KindBlog.
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindBlog }

// Enabled implements releasedocs.Generator. It applies "minor_or_above" as
// the default when condition (coercing an empty When to "minor_or_above")
// before delegating to the shared releasedocs.Enabled gate. This differs from
// other generators: blog posts are meaningful announcements for feature/major
// releases and should not fire for every patch. The shared releasedocs.Enabled
// function is NOT modified — this coercion is local to the blog generator (D-08).
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	artifactCfg := cfg.Artifacts.Blog
	// Coerce empty When to "minor_or_above" — blog's opinionated default.
	if artifactCfg.When == "" {
		artifactCfg.When = "minor_or_above"
	}
	return releasedocs.Enabled(artifactCfg, bump)
}

// Generate implements releasedocs.Generator. It builds a deterministic
// long-form skeleton from rc.GroupedModel. When rc.LLM is non-nil, it calls
// Chat once to author a publication-ready blog announcement on top of the
// skeleton. When rc.LLM is nil, the skeleton is returned verbatim — no Chat
// call (D-11). On Chat error, it falls back to the skeleton non-fatally (D-10).
// rc.Model is passed through to Chat without a second default-model path (D-17).
func (g *Generator) Generate(ctx context.Context, rc releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	skeleton := buildSkeleton(rc)

	// When rc.LLM is nil, return the skeleton verbatim (nil-tolerant D-11).
	if rc.LLM == nil {
		return releasedocs.Artifact{
			Kind:    releasedocs.KindBlog,
			Content: []byte(skeleton),
		}, nil
	}

	// LLM present: call Chat once to author a publication-ready blog post.
	narrative, err := narrateWithLLM(ctx, rc.LLM, rc.Model, skeleton)
	if err != nil {
		// Narrative failure is non-fatal: fall back to skeleton (D-10).
		return releasedocs.Artifact{
			Kind:    releasedocs.KindBlog,
			Content: []byte(skeleton),
		}, nil
	}

	return releasedocs.Artifact{
		Kind:    releasedocs.KindBlog,
		Content: []byte(narrative),
	}, nil
}

// buildSkeleton constructs a deterministic long-form blog skeleton from the
// grouped change model in rc. The skeleton is section-by-section prose rather
// than a bullet-only changelog, giving the LLM a narrative-ready starting point.
// Canonical section order is guaranteed by BuildGroupedModel (Pitfall 3).
func buildSkeleton(rc releasedocs.ReleaseContext) string {
	var b strings.Builder

	// Title and intro
	fmt.Fprintf(&b, "# Announcing %s\n\n", rc.ToRef)
	if rc.FromRef != "" {
		fmt.Fprintf(&b, "We are excited to announce **%s**, released from %s.\n\n", rc.ToRef, rc.FromRef)
	} else {
		fmt.Fprintf(&b, "We are excited to announce **%s**.\n\n", rc.ToRef)
	}

	if len(rc.GroupedModel.Sections) == 0 {
		b.WriteString("This release contains various improvements and fixes.\n")
		return b.String()
	}

	b.WriteString("## What's New\n\n")

	for _, sec := range rc.GroupedModel.Sections {
		if len(sec.Entries) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", sec.Title)
		for _, e := range sec.Entries {
			if e.PRNumber != 0 {
				fmt.Fprintf(&b, "- %s (#%d) — @%s\n", e.Title, e.PRNumber, e.Author)
			} else {
				fmt.Fprintf(&b, "- %s — @%s\n", e.Title, e.Author)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "Thank you to all contributors who made %s possible.\n", rc.ToRef)

	return b.String()
}

// narrateWithLLM calls provider.Chat once to author a publication-ready blog
// announcement based on the deterministic skeleton. model is passed through to
// Chat without a second default-model path (D-17).
func narrateWithLLM(ctx context.Context, provider llm.Provider, model, skeleton string) (string, error) {
	req := llm.ChatRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: blogSystemPrompt},
			{Role: llm.RoleUser, Content: skeleton},
		},
	}
	resp, err := provider.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("blog: LLM narrative: %w", err)
	}
	narrative := strings.TrimSpace(resp.Content)
	if narrative == "" {
		return skeleton, nil
	}
	return narrative, nil
}

// blogSystemPrompt is the system prompt used for blog post narration.
const blogSystemPrompt = `You are a skilled technical writer and product marketer authoring a blog post
to announce a software release. Your goal is to write a compelling, engaging, and
publication-ready announcement that will be shared with the developer community.

Transform the provided release skeleton into a polished blog post:
- Write an engaging introduction that sets context and excitement.
- Highlight the most impactful features and improvements for users.
- Use a professional but approachable tone.
- Preserve all sections and change entries.
- Output publication-ready Markdown only.`
