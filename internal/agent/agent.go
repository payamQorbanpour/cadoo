// Package agent runs an LLM tool-use loop. The model is given a set of Tools
// it may call; the loop dispatches each call, appends the result, and asks
// the model to continue until it produces a final answer (or hits MaxIter).
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

// Tool is one function the model can call inside the loop.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON Schema for the arguments object
	Run         func(ctx context.Context, args json.RawMessage) (string, error)
}

// Loop drives a tool-use conversation.
type Loop struct {
	LLM         llm.Provider
	Model       string
	System      string
	Tools       []Tool
	MaxIter     int     // 0 == 8 default; safety bound on tool-call rounds
	MaxTokens   int     // forwarded to LLM ChatRequest
	Temperature float32 // forwarded
}

// Result is what the loop returns once the model emits a tool-call-free reply.
type Result struct {
	Content    string
	Iterations int
	Usage      llm.Usage
	ToolCalls  int // total tool calls executed
}

// Run executes the loop with userPrompt as the first user message.
func (l *Loop) Run(ctx context.Context, userPrompt string) (*Result, error) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: l.System},
		{Role: llm.RoleUser, Content: userPrompt},
	}
	toolDefs := make([]llm.ToolDef, 0, len(l.Tools))
	byName := map[string]Tool{}
	for _, t := range l.Tools {
		toolDefs = append(toolDefs, llm.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Schema,
		})
		byName[t.Name] = t
	}

	maxIter := l.MaxIter
	if maxIter == 0 {
		maxIter = 8
	}

	out := &Result{}
	for iter := 0; iter < maxIter; iter++ {
		resp, err := l.LLM.Chat(ctx, llm.ChatRequest{
			Model:       l.Model,
			Messages:    msgs,
			Tools:       toolDefs,
			Temperature: l.Temperature,
			MaxTokens:   l.MaxTokens,
		})
		if err != nil {
			return out, fmt.Errorf("chat: %w", err)
		}
		out.Iterations = iter + 1
		out.Usage.PromptTokens += resp.Usage.PromptTokens
		out.Usage.CompletionTokens += resp.Usage.CompletionTokens
		out.Usage.TotalTokens += resp.Usage.TotalTokens

		if len(resp.ToolCalls) == 0 {
			out.Content = resp.Content
			return out, nil
		}

		// Echo the assistant's tool calls back.
		msgs = append(msgs, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})
		// Execute each tool call sequentially, appending tool messages.
		for _, tc := range resp.ToolCalls {
			out.ToolCalls++
			tool, ok := byName[tc.Name]
			if !ok {
				msgs = append(msgs, llm.Message{
					Role:       llm.RoleTool,
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("error: unknown tool %q", tc.Name),
				})
				continue
			}
			result, err := tool.Run(ctx, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			msgs = append(msgs, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}
	return out, fmt.Errorf("max iterations (%d) reached without final answer", maxIter)
}
