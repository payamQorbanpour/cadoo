// Package mcp is a minimal client for Model Context Protocol servers.
// Phase 6 ships the JSON-RPC envelope + HTTP transport surface so other
// packages can speak MCP types; full bidirectional stdio + tool/resource
// dispatch is Phase 6.x.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// JSONRPC is the version constant MCP uses on every envelope.
const JSONRPC = "2.0"

// Request is one JSON-RPC request sent to the MCP server.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is one JSON-RPC response received from the MCP server.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError carries the standard JSON-RPC error envelope.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *RPCError) Error() string { return fmt.Sprintf("mcp rpc %d: %s", e.Code, e.Message) }

// ToolDef is the subset of an MCP tool description Cadoo cares about.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// HTTPClient is a Streamable-HTTP MCP client (single round-trip per call).
// Subscribed/streaming responses are Phase 6.x.
type HTTPClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	nextID     atomic.Uint64
}

// NewHTTPClient builds a client.
func NewHTTPClient(baseURL, apiKey string) *HTTPClient {
	return &HTTPClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Call sends a single JSON-RPC request and decodes the result into out.
func (c *HTTPClient) Call(ctx context.Context, method string, params any, out any) error {
	if c.BaseURL == "" {
		return errors.New("mcp: BaseURL not set")
	}
	id := c.nextID.Add(1)
	var paramsRaw json.RawMessage
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		paramsRaw = raw
	}
	body, err := json.Marshal(Request{JSONRPC: JSONRPC, ID: id, Method: method, Params: paramsRaw})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp call %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mcp call %s: status %d: %s", method, resp.StatusCode, string(b))
	}
	var parsed Response
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return fmt.Errorf("decode mcp response: %w", err)
	}
	if parsed.Error != nil {
		return parsed.Error
	}
	if out != nil && len(parsed.Result) > 0 {
		if err := json.Unmarshal(parsed.Result, out); err != nil {
			return fmt.Errorf("decode mcp result: %w", err)
		}
	}
	return nil
}

// ListTools returns the tools advertised by the server.
func (c *HTTPClient) ListTools(ctx context.Context) ([]ToolDef, error) {
	var out struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := c.Call(ctx, "tools/list", nil, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool invokes a tool by name. The result is whatever JSON the server
// returns under the "content" field; callers parse to taste.
func (c *HTTPClient) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	var raw json.RawMessage
	if err := c.Call(ctx, "tools/call", params, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}
