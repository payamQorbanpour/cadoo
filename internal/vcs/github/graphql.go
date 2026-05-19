package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// graphqlEndpoint derives the GraphQL URL from go-github's REST base URL.
// github.com REST base is https://api.github.com/ ; GHES is
// https://<host>/api/v3/ and its GraphQL endpoint is https://<host>/api/graphql.
func graphqlEndpoint(rest *url.URL) string {
	if rest.Host == "api.github.com" {
		return "https://api.github.com/graphql"
	}
	return rest.Scheme + "://" + rest.Host + "/api/graphql"
}

type graphqlError struct {
	Message string `json:"message"`
}

// doGraphQL POSTs a GraphQL request using the supplied (already
// authenticated) http client and decodes the `data` field into out.
func doGraphQL(ctx context.Context, hc *http.Client, endpoint, query string, vars map[string]any, out any) error {
	reqBody, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("graphql marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("graphql post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("graphql http %d", resp.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphqlError  `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("graphql decode: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("graphql data unmarshal: %w", err)
		}
	}
	return nil
}
