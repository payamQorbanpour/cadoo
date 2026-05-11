package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

// scriptedLLM returns ChatResponses from a queue.
type scriptedLLM struct {
	queue []llm.ChatResponse
	calls int
}

func (s *scriptedLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	if s.calls >= len(s.queue) {
		return &llm.ChatResponse{Content: "(queue exhausted)"}, nil
	}
	r := s.queue[s.calls]
	s.calls++
	return &r, nil
}

type mapReader map[string]string

func (m mapReader) ReadFile(_ context.Context, path string) ([]byte, error) {
	v, ok := m[path]
	if !ok {
		return nil, &noFileError{path}
	}
	return []byte(v), nil
}

type noFileError struct{ path string }

func (e *noFileError) Error() string { return "no such file: " + e.path }

func TestLoopReturnsImmediateAnswer(t *testing.T) {
	llmStub := &scriptedLLM{queue: []llm.ChatResponse{{Content: "all good"}}}
	loop := &Loop{LLM: llmStub, Model: "x", System: "sys"}
	res, err := loop.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "all good" || res.Iterations != 1 || res.ToolCalls != 0 {
		t.Errorf("res: %+v", res)
	}
}

func TestLoopExecutesToolCallThenAnswers(t *testing.T) {
	llmStub := &scriptedLLM{queue: []llm.ChatResponse{
		{
			Content: "let me check",
			ToolCalls: []llm.ToolCall{{
				ID:        "c1",
				Name:      "read_file",
				Arguments: json.RawMessage(`{"path":"main.go"}`),
			}},
		},
		{Content: "found it"},
	}}
	reader := mapReader{"main.go": "line one\nline two\n"}
	loop := &Loop{
		LLM:    llmStub,
		Model:  "x",
		System: "sys",
		Tools:  []Tool{ReadFileTool(reader)},
	}
	res, err := loop.Run(context.Background(), "look at main.go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "found it" {
		t.Errorf("content: %q", res.Content)
	}
	if res.ToolCalls != 1 {
		t.Errorf("tool calls: %d", res.ToolCalls)
	}
	if res.Iterations != 2 {
		t.Errorf("iterations: %d", res.Iterations)
	}
}

func TestLoopMaxIter(t *testing.T) {
	// Always returns a tool call → loop must terminate at MaxIter.
	llmStub := &scriptedLLM{}
	for i := 0; i < 10; i++ {
		llmStub.queue = append(llmStub.queue, llm.ChatResponse{
			Content:   "again",
			ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file", Arguments: json.RawMessage(`{"path":"x"}`)}},
		})
	}
	loop := &Loop{
		LLM:     llmStub,
		Model:   "x",
		System:  "sys",
		Tools:   []Tool{ReadFileTool(mapReader{"x": "x"})},
		MaxIter: 3,
	}
	_, err := loop.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected MaxIter error")
	}
}

func TestGrepTool(t *testing.T) {
	reader := mapReader{
		"a.go": "package main\nfunc main() {}\n",
		"b.go": "package main\nfunc helper() {}\n",
	}
	tool := GrepTool(reader, []string{"a.go", "b.go"})
	out, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"main"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out == "no matches" {
		t.Fatal("expected matches")
	}
}
