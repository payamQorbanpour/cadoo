// Package llm defines the provider-agnostic LLM interface Cadoo's tools and
// agents call. Concrete implementations live in subpackages
// (e.g. internal/llm/litellm).
package llm

import (
	"context"
	"encoding/json"
)

// Roles for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one entry in a chat conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolDef declares a function the model may call.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema object.
}

// ToolCall is the model's request to invoke a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ChatRequest is one chat completion request.
type ChatRequest struct {
	Model       string
	Messages    []Message
	Tools       []ToolDef
	Temperature float32
	MaxTokens   int
	Stream      bool
}

// Usage reports token accounting for a single call.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CostUSD          float64
}

// ChatResponse is one chat completion result.
type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	Usage        Usage
	Model        string
	FinishReason string
}

// Provider is the surface every LLM gateway implements. Implementations must
// be safe for concurrent use by multiple goroutines.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}
