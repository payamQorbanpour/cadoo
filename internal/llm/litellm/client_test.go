package litellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/llm"
)

func TestChatRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if got, want := r.Header.Get("Authorization"), "apikey test-key"; got != want {
			t.Errorf("Authorization header: got %q, want %q", got, want)
		}
		var got chatRequestPayload
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Model != "claude-sonnet-4-6" {
			t.Errorf("model: %q", got.Model)
		}
		if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
			t.Errorf("messages: %+v", got.Messages)
		}
		_ = json.NewEncoder(w).Encode(chatResponsePayload{
			Model: "claude-sonnet-4-6",
			Choices: []struct {
				Message      chatMessage `json:"message"`
				FinishReason string      `json:"finish_reason"`
			}{{
				Message:      chatMessage{Role: "assistant", Content: "hi"},
				FinishReason: "stop",
			}},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4},
		})
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "apikey test-key")
	resp, err := c.Chat(context.Background(), llm.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hi" {
		t.Errorf("content: %q", resp.Content)
	}
	if resp.Usage.TotalTokens != 4 {
		t.Errorf("total tokens: %d", resp.Usage.TotalTokens)
	}
}
