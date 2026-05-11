package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoJSONRetries5xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			http.Error(w, "upstream blip", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := DoJSON(context.Background(), client, http.MethodPost, srv.URL, []byte(`{}`), "apikey k")
	if err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status: %d", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 calls, got %d", got)
	}
}

func TestDoJSONGivesUpAfterThreeAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "still bad", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	_, err := DoJSON(context.Background(), client, http.MethodPost, srv.URL, []byte(`{}`), "")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("expected 3 calls, got %d", got)
	}
}

func TestDoJSONNoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := DoJSON(context.Background(), client, http.MethodPost, srv.URL, []byte(`{}`), "")
	if err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("status: %d", resp.StatusCode)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 call (no retry on 4xx), got %d", got)
	}
}
