package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path: %s", r.URL.Path)
		}
		var got embedRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if got.Model != DefaultModel {
			t.Errorf("model: %q", got.Model)
		}
		_ = json.NewEncoder(w).Encode(embedResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
				{Embedding: []float32{0.4, 0.5, 0.6}, Index: 1},
			},
		})
	}))
	defer srv.Close()
	c := New(srv.URL, "", "")
	got, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0][0] != 0.1 || got[1][2] != 0.6 {
		t.Errorf("got %+v", got)
	}
}

func TestEmbedEmpty(t *testing.T) {
	c := New("http://nowhere", "", "")
	got, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
