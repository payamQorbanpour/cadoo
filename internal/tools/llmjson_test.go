package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

// fakeProvider is a minimal llm.Provider that returns a canned response and
// records the request it received (so tests can assert MaxTokens wiring).
type fakeProvider struct {
	resp llm.ChatResponse
	err  error
	got  llm.ChatRequest
}

func (f *fakeProvider) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.got = req
	if f.err != nil {
		return nil, f.err
	}
	r := f.resp
	return &r, nil
}

func TestCallJSONEmptyCompletionIsExplicit(t *testing.T) {
	p := &fakeProvider{resp: llm.ChatResponse{Content: "", FinishReason: "stop"}}
	var dst map[string]any
	err := CallJSON(context.Background(), p, "m", "sys", "user", &dst)
	if err == nil {
		t.Fatal("expected error on empty completion, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "empty completion") {
		t.Errorf("error %q should say 'empty completion'", msg)
	}
	if !strings.Contains(msg, "stop") {
		t.Errorf("error %q should include the finish_reason", msg)
	}
	if strings.Contains(msg, "no JSON object found") {
		t.Errorf("error %q must not be the cryptic parser message", msg)
	}
}

func TestCallJSONTruncatedFinishReasonLength(t *testing.T) {
	partial := "```json\n{\n  \"title\": \"X\",\n  \"intent\": \"and enf"
	p := &fakeProvider{resp: llm.ChatResponse{Content: partial, FinishReason: "length"}}
	var dst map[string]any
	err := CallJSON(context.Background(), p, "m", "sys", "user", &dst)
	if err == nil {
		t.Fatal("expected error on length-truncated completion, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "length") || !strings.Contains(strings.ToLower(msg), "truncat") {
		t.Errorf("error %q should explicitly say it was truncated at finish_reason=length", msg)
	}
	if !strings.Contains(msg, "CADOO_MAX_TOKENS") {
		t.Errorf("error %q should point at the CADOO_MAX_TOKENS knob", msg)
	}
}

func TestCallJSONParseErrorIncludesFinishReason(t *testing.T) {
	p := &fakeProvider{resp: llm.ChatResponse{Content: "not json at all", FinishReason: "stop"}}
	var dst map[string]any
	err := CallJSON(context.Background(), p, "m", "sys", "user", &dst)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no JSON object found") {
		t.Errorf("error %q should still report the parse failure", msg)
	}
	if !strings.Contains(msg, "finish_reason") || !strings.Contains(msg, "stop") {
		t.Errorf("error %q should include finish_reason for diagnosability", msg)
	}
}

func TestCallJSONSuccessUnchanged(t *testing.T) {
	p := &fakeProvider{resp: llm.ChatResponse{Content: "prefix {\"x\":1} suffix", FinishReason: "stop"}}
	var dst struct {
		X int `json:"x"`
	}
	if err := CallJSON(context.Background(), p, "m", "sys", "user", &dst); err != nil {
		t.Fatalf("happy path should not error: %v", err)
	}
	if dst.X != 1 {
		t.Errorf("dst.X = %d; want 1", dst.X)
	}
}

func TestMaxTokensDefaultAndEnv(t *testing.T) {
	t.Setenv("CADOO_MAX_TOKENS", "")
	if got := maxTokens(); got != defaultMaxTokens {
		t.Errorf("maxTokens() default = %d; want %d", got, defaultMaxTokens)
	}
	if defaultMaxTokens <= 4096 {
		t.Errorf("defaultMaxTokens = %d; want > 4096 (4096 truncated describe)", defaultMaxTokens)
	}
	t.Setenv("CADOO_MAX_TOKENS", "12000")
	if got := maxTokens(); got != 12000 {
		t.Errorf("maxTokens() with env = %d; want 12000", got)
	}
	t.Setenv("CADOO_MAX_TOKENS", "garbage")
	if got := maxTokens(); got != defaultMaxTokens {
		t.Errorf("maxTokens() invalid env = %d; want fallback %d", got, defaultMaxTokens)
	}
	t.Setenv("CADOO_MAX_TOKENS", "-5")
	if got := maxTokens(); got != defaultMaxTokens {
		t.Errorf("maxTokens() non-positive env = %d; want fallback %d", got, defaultMaxTokens)
	}
}

func TestCallJSONSendsConfiguredMaxTokens(t *testing.T) {
	t.Setenv("CADOO_MAX_TOKENS", "")
	p := &fakeProvider{resp: llm.ChatResponse{Content: "{}", FinishReason: "stop"}}
	var dst map[string]any
	_ = CallJSON(context.Background(), p, "m", "sys", "user", &dst)
	if p.got.MaxTokens != defaultMaxTokens {
		t.Errorf("sent MaxTokens = %d; want default %d", p.got.MaxTokens, defaultMaxTokens)
	}
	t.Setenv("CADOO_MAX_TOKENS", "9000")
	p2 := &fakeProvider{resp: llm.ChatResponse{Content: "{}", FinishReason: "stop"}}
	_ = CallJSON(context.Background(), p2, "m", "sys", "user", &dst)
	if p2.got.MaxTokens != 9000 {
		t.Errorf("sent MaxTokens = %d; want env-configured 9000", p2.got.MaxTokens)
	}
}

func TestCallTextEmptyCompletionIsExplicit(t *testing.T) {
	p := &fakeProvider{resp: llm.ChatResponse{Content: "  ", FinishReason: "stop"}}
	_, err := CallText(context.Background(), p, "m", "sys", "user")
	if err == nil {
		t.Fatal("CallText should error on empty/whitespace completion, not return empty string")
	}
	if !strings.Contains(err.Error(), "empty completion") {
		t.Errorf("error %q should say 'empty completion'", err.Error())
	}
}

func TestCallTextTruncatedIsExplicit(t *testing.T) {
	p := &fakeProvider{resp: llm.ChatResponse{Content: "partial output", FinishReason: "length"}}
	_, err := CallText(context.Background(), p, "m", "sys", "user")
	if err == nil {
		t.Fatal("CallText should error when finish_reason=length")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "truncat") {
		t.Errorf("error %q should say it was truncated", err.Error())
	}
}

func TestCallTextSuccessUnchanged(t *testing.T) {
	p := &fakeProvider{resp: llm.ChatResponse{Content: "  hello world  ", FinishReason: "stop"}}
	got, err := CallText(context.Background(), p, "m", "sys", "user")
	if err != nil {
		t.Fatalf("happy path should not error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("CallText = %q; want trimmed %q", got, "hello world")
	}
}
