// Package linear implements issuetrackers.Tracker against linear.app via
// its GraphQL API. Auth is by API key (raw value in the Authorization header).
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/issuetrackers"
)

// DefaultEndpoint is the Linear GraphQL endpoint.
const DefaultEndpoint = "https://api.linear.app/graphql"

// Tracker is the Linear issuetrackers.Tracker implementation.
type Tracker struct {
	Endpoint   string
	APIKey     string
	HTTPClient *http.Client
}

// New configures a Tracker. Empty endpoint defaults to DefaultEndpoint.
func New(endpoint, apiKey string) *Tracker {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Tracker{
		Endpoint:   endpoint,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Name implements issuetrackers.Tracker.
func (Tracker) Name() string { return "linear" }

// FindLinked implements issuetrackers.Tracker.
func (t *Tracker) FindLinked(ctx context.Context, prTitle, prBody string) ([]issuetrackers.Issue, error) {
	keys := issuetrackers.ExtractKeys(prTitle + "\n" + prBody)
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]issuetrackers.Issue, 0, len(keys))
	for _, k := range keys {
		iss, err := t.fetchOne(ctx, k)
		if err != nil || iss == nil {
			continue
		}
		out = append(out, *iss)
	}
	return out, nil
}

const issueQuery = `query($id: String!) {
  issue(id: $id) {
    identifier
    title
    description
    state { name }
    assignee { displayName }
    labels { nodes { name } }
    url
    updatedAt
  }
}`

type gqlResponse struct {
	Data struct {
		Issue *struct {
			Identifier  string
			Title       string
			Description string
			State       struct{ Name string }
			Assignee    *struct{ DisplayName string }
			Labels      struct {
				Nodes []struct{ Name string }
			}
			URL       string
			UpdatedAt string
		} `json:"issue"`
	} `json:"data"`
	Errors []struct{ Message string } `json:"errors"`
}

func (t *Tracker) fetchOne(ctx context.Context, identifier string) (*issuetrackers.Issue, error) {
	if t.APIKey == "" {
		return nil, fmt.Errorf("linear: missing api key")
	}
	body, _ := json.Marshal(map[string]any{
		"query":     issueQuery,
		"variables": map[string]string{"id": strings.ToUpper(identifier)},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", t.APIKey)

	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linear fetch %s: %w", identifier, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("linear %s: status %d: %s", identifier, resp.StatusCode, string(raw))
	}
	var parsed gqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode linear response: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("linear: %s", parsed.Errors[0].Message)
	}
	iss := parsed.Data.Issue
	if iss == nil {
		return nil, nil
	}
	out := &issuetrackers.Issue{
		Tracker: "linear",
		Key:     iss.Identifier,
		Title:   iss.Title,
		Body:    iss.Description,
		Status:  iss.State.Name,
		URL:     iss.URL,
	}
	if iss.Assignee != nil {
		out.Assignee = iss.Assignee.DisplayName
	}
	for _, l := range iss.Labels.Nodes {
		out.Labels = append(out.Labels, l.Name)
	}
	if iss.UpdatedAt != "" {
		if ts, err := time.Parse(time.RFC3339, iss.UpdatedAt); err == nil {
			out.UpdatedAt = ts
		}
	}
	return out, nil
}

var _ issuetrackers.Tracker = (*Tracker)(nil)
