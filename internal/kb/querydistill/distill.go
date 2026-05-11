// Package querydistill rewrites a verbose PR title+body into a focused
// retrieval query before embedding. Plain title+body embeddings often pick
// up routine boilerplate ("Fixes #123", sign-offs, test plans) rather than
// the change's intent — a one-line LLM rewrite lifts retrieval quality
// enough to be worth the extra cheap call.
//
// Lives outside `internal/kb` so it can depend on internal/llm without the
// kb store ever needing to.
package querydistill

import (
	"context"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

// Distiller is the optional sidecar.
type Distiller struct {
	LLM   llm.Provider
	Model string
}

const systemPrompt = `Rewrite a pull-request title + body into a single retrieval query (one or two sentences max) that captures what the PR is actually about. Drop boilerplate, ticket references, sign-offs, and test plans. Output the query as plain prose only — no preamble, no quotes.`

// Distill returns a focused retrieval query for the PR. On any error or
// empty model output it returns the original prTitle+prBody concatenation
// so callers don't need nil-checks.
func (d *Distiller) Distill(ctx context.Context, prTitle, prBody string) string {
	original := prTitle
	if prBody != "" {
		original += "\n\n" + prBody
	}
	if d == nil || d.LLM == nil {
		return original
	}
	resp, err := d.LLM.Chat(ctx, llm.ChatRequest{
		Model:       d.Model,
		Temperature: 0.1,
		MaxTokens:   200,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: original},
		},
	})
	if err != nil {
		return original
	}
	out := strings.TrimSpace(resp.Content)
	if out == "" {
		return original
	}
	return out
}
