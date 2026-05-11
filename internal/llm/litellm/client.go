// Package litellm implements llm.Provider against any OpenAI-compatible HTTP
// endpoint. The reference deployment is the LiteLLM proxy, but this client
// also speaks to OpenAI itself, vLLM, Ollama (with its OpenAI shim), etc.
package litellm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

// Client is an OpenAI-compatible chat-completion client.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// New returns a Client targeting baseURL. baseURL must include the OpenAI-style
// version prefix (e.g. "http://litellm:4000/v1"). apiKey is used verbatim as the
// Authorization header value (e.g. "Bearer sk-...", "apikey 1234...1234"); the
// client does not add a scheme prefix.
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

type chatRequestPayload struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string         `json:"type"`
	Function chatToolSchema `json:"function"`
}

type chatToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function chatToolCallFn `json:"function"`
}

type chatToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatResponsePayload struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Chat implements llm.Provider.
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	payload := chatRequestPayload{
		Model:       req.Model,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      req.Stream,
	}
	for _, m := range req.Messages {
		cm := chatMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: chatToolCallFn{
					Name:      tc.Name,
					Arguments: string(tc.Arguments),
				},
			})
		}
		payload.Messages = append(payload.Messages, cm)
	}
	for _, t := range req.Tools {
		payload.Tools = append(payload.Tools, chatTool{
			Type: "function",
			Function: chatToolSchema{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}
	url := c.BaseURL + "/chat/completions"
	resp, err := llm.DoJSON(ctx, c.HTTPClient, http.MethodPost, url, body, c.APIKey)
	if err != nil {
		return nil, fmt.Errorf("chat completion %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("chat completion %s: status %d: %s", url, resp.StatusCode, string(b))
	}

	var parsed chatResponsePayload
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("chat completion: no choices in response")
	}
	msg := parsed.Choices[0].Message
	out := &llm.ChatResponse{
		Content:      msg.Content,
		Model:        parsed.Model,
		FinishReason: parsed.Choices[0].FinishReason,
		Usage: llm.Usage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		},
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, llm.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}
	return out, nil
}

var _ llm.Provider = (*Client)(nil)
