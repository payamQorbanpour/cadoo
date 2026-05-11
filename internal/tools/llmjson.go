package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

// CallJSON sends one chat completion and unmarshals the first JSON object
// out of the response into dst. Tools share this helper since they all want
// structured output.
func CallJSON(ctx context.Context, p llm.Provider, model, system, user string, dst any) error {
	resp, err := p.Chat(ctx, llm.ChatRequest{
		Model:       model,
		Temperature: 0.2,
		MaxTokens:   4096,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
	})
	if err != nil {
		return fmt.Errorf("llm call: %w", err)
	}
	if err := ExtractJSON(resp.Content, dst); err != nil {
		return fmt.Errorf("%w (raw: %q)", err, truncate(resp.Content, 200))
	}
	return nil
}

// CallText sends one chat completion and returns the trimmed content.
func CallText(ctx context.Context, p llm.Provider, model, system, user string) (string, error) {
	resp, err := p.Chat(ctx, llm.ChatRequest{
		Model:       model,
		Temperature: 0.3,
		MaxTokens:   4096,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: system},
			{Role: llm.RoleUser, Content: user},
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm call: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// ExtractJSON finds the first {...} object in s and unmarshals it into dst.
// Tolerates fence-wrapped or prose-prefixed responses.
func ExtractJSON(s string, dst any) error {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object found")
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), dst); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
