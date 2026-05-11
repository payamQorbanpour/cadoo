// Package embed defines the embedding-provider interface and a LiteLLM-style
// client. Embeddings are used by internal/kb for semantic search and by the
// learnings system for de-duplicating user-supplied rules.
package embed

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

// DefaultModel is the default embedding model name. text-embedding-3-small
// gives 1536-dim vectors, matching the kb_chunks.embedding column.
const DefaultModel = "text-embedding-3-small"

// Embedder turns text into dense vectors.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// Client targets an OpenAI-compatible /v1/embeddings endpoint.
type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

// New returns a Client. baseURL must include the OpenAI-style version prefix
// (e.g. "http://litellm:4000/v1"). apiKey is used verbatim as the Authorization
// header value (e.g. "Bearer sk-...", "apikey 1234...1234").
func New(baseURL, apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		Model:      model,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed implements Embedder.
func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Model: c.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	url := c.BaseURL + "/embeddings"
	resp, err := llm.DoJSON(ctx, c.HTTPClient, http.MethodPost, url, body, c.APIKey)
	if err != nil {
		return nil, fmt.Errorf("embeddings %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embeddings %s: status %d: %s", url, resp.StatusCode, string(b))
	}
	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode embeddings response: %w", err)
	}
	out := make([][]float32, len(parsed.Data))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("embeddings: out-of-range index %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}

var _ Embedder = (*Client)(nil)
