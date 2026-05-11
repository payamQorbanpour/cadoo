package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientCallSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method != "tools/list" {
			t.Errorf("method: %q", req.Method)
		}
		_ = json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0", ID: req.ID,
			Result: json.RawMessage(`{"tools":[{"name":"web.search","description":"x","inputSchema":{}}]}`),
		})
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, "")
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "web.search" {
		t.Errorf("got %+v", tools)
	}
}

func TestHTTPClientPropagatesRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0", ID: 1,
			Error: &RPCError{Code: -32601, Message: "method not found"},
		})
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, "")
	if _, err := c.CallTool(context.Background(), "nope", nil); err == nil {
		t.Fatal("expected RPC error")
	}
}
