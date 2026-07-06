package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

// defaultMaxTokens is the completion-token budget for tool LLM calls. The
// previous hardcoded 4096 truncated /describe's JSON on real merge requests
// (finish_reason=length → unparseable output); 8192 is a safer floor and is
// overridable via CADOO_MAX_TOKENS for models/gateways with larger budgets.
const defaultMaxTokens = 8192

// maxTokens returns the configured completion-token budget. CADOO_MAX_TOKENS
// overrides defaultMaxTokens when set to a positive integer; invalid or
// non-positive values fall back to the default.
func maxTokens() int {
	if v := os.Getenv("CADOO_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultMaxTokens
}

// CallJSON sends one chat completion and unmarshals the first JSON object
// out of the response into dst. Tools share this helper since they all want
// structured output. An empty completion or a length-truncated completion is
// reported as an explicit, actionable error rather than a cryptic JSON
// parser failure, and finish_reason is surfaced for diagnosability.
func CallJSON(ctx context.Context, p llm.Provider, model, system, user string, dst any) error {
	resp, err := p.Chat(ctx, llm.ChatRequest{
		Model:       model,
		Temperature: 0.2,
		MaxTokens:   maxTokens(),
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
	})
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}
	if resp.Content == "" {
		return fmt.Errorf("llm: empty completion (finish_reason=%q) — gateway returned no content", resp.FinishReason)
	}
	if resp.FinishReason == "length" {
		return fmt.Errorf("llm: completion truncated at max_tokens (finish_reason=length, %d chars) — raise CADOO_MAX_TOKENS", len(resp.Content))
	}
	if err := ExtractJSON(resp.Content, dst); err != nil {
		return fmt.Errorf("%w (finish_reason=%q, raw: %q)", err, resp.FinishReason, truncate(resp.Content, 200))
	}
	return nil
}

// CallText sends one chat completion and returns the trimmed content. Empty
// or length-truncated completions are reported as explicit errors instead of
// silently returning an empty / partial string.
func CallText(ctx context.Context, p llm.Provider, model, system, user string) (string, error) {
	resp, err := p.Chat(ctx, llm.ChatRequest{
		Model:       model,
		Temperature: 0.3,
		MaxTokens:   maxTokens(),
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm call: %w", err)
	}
	trimmed := strings.TrimSpace(resp.Content)
	if trimmed == "" {
		return "", fmt.Errorf("llm: empty completion (finish_reason=%q) — gateway returned no content", resp.FinishReason)
	}
	if resp.FinishReason == "length" {
		return "", fmt.Errorf("llm: completion truncated at max_tokens (finish_reason=length, %d chars) — raise CADOO_MAX_TOKENS", len(resp.Content))
	}
	return trimmed, nil
}

// ExtractJSON finds the first {...} object in s and unmarshals it into dst.
// Tolerates fence-wrapped or prose-prefixed responses, and raw control
// characters (tabs/newlines) inside string literals — some models emit code
// snippets with literal tabs in a "suggestion" field, which encoding/json
// rejects per spec.
func ExtractJSON(s string, dst any) error {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object found")
	}
	obj := escapeControlCharsInStrings(s[start : end+1])
	if err := json.Unmarshal([]byte(obj), dst); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	return nil
}

// escapeControlCharsInStrings rewrites raw control characters (< 0x20) that
// appear inside JSON string literals into their valid escape sequences, so a
// response containing e.g. a literal tab inside a value parses cleanly.
// Control characters outside strings (structural whitespace) are left as-is.
func escapeControlCharsInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			case c < 0x20:
				switch c {
				case '\t':
					b.WriteString(`\t`)
				case '\n':
					b.WriteString(`\n`)
				case '\r':
					b.WriteString(`\r`)
				case '\f':
					b.WriteString(`\f`)
				case '\b':
					b.WriteString(`\b`)
				default:
					fmt.Fprintf(&b, `\u%04x`, c)
				}
				continue
			}
		} else if c == '"' {
			inString = true
		}
		b.WriteByte(c)
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
