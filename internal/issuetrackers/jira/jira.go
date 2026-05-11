// Package jira implements issuetrackers.Tracker against Jira Cloud / Server.
// Auth is either basic (email + API token) or PAT bearer.
package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/issuetrackers"
)

// Tracker is the Jira issuetrackers.Tracker implementation.
type Tracker struct {
	BaseURL    string // e.g. "https://your-org.atlassian.net"
	Email      string // empty == bearer auth using Token
	Token      string // API token (basic) or PAT (bearer)
	HTTPClient *http.Client
}

// New configures a Tracker. baseURL is required; (email, token) is required
// when using Cloud basic auth, otherwise pass token-only for PAT bearer.
func New(baseURL, email, token string) *Tracker {
	return &Tracker{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Email:      email,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Name implements issuetrackers.Tracker.
func (Tracker) Name() string { return "jira" }

// FindLinked implements issuetrackers.Tracker.
func (t *Tracker) FindLinked(ctx context.Context, prTitle, prBody string) ([]issuetrackers.Issue, error) {
	keys := issuetrackers.ExtractKeys(prTitle + "\n" + prBody)
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]issuetrackers.Issue, 0, len(keys))
	for _, k := range keys {
		iss, err := t.fetchOne(ctx, k)
		if err != nil {
			// Skip individual misses (404 / wrong tracker) silently.
			continue
		}
		out = append(out, *iss)
	}
	return out, nil
}

type jiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary  string `json:"summary"`
		Status   struct{ Name string } `json:"status"`
		Assignee *struct {
			DisplayName string `json:"displayName"`
		} `json:"assignee"`
		Labels  []string `json:"labels"`
		Updated string   `json:"updated"`
	} `json:"fields"`
}

func (t *Tracker) fetchOne(ctx context.Context, key string) (*issuetrackers.Issue, error) {
	if t.BaseURL == "" || t.Token == "" {
		return nil, fmt.Errorf("jira: missing base URL or token")
	}
	url := t.BaseURL + "/rest/api/3/issue/" + key + "?fields=summary,status,assignee,labels,updated"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if t.Email != "" {
		req.SetBasicAuth(t.Email, t.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+t.Token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira fetch %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("jira: %s not found", key)
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jira %s: status %d: %s", key, resp.StatusCode, string(body))
	}
	var raw jiraIssue
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode jira issue: %w", err)
	}
	iss := &issuetrackers.Issue{
		Tracker: "jira",
		Key:     raw.Key,
		Title:   raw.Fields.Summary,
		Status:  raw.Fields.Status.Name,
		Labels:  raw.Fields.Labels,
		URL:     t.BaseURL + "/browse/" + raw.Key,
	}
	if raw.Fields.Assignee != nil {
		iss.Assignee = raw.Fields.Assignee.DisplayName
	}
	if raw.Fields.Updated != "" {
		if ts, err := time.Parse("2006-01-02T15:04:05.000-0700", raw.Fields.Updated); err == nil {
			iss.UpdatedAt = ts
		}
	}
	return iss, nil
}

var _ issuetrackers.Tracker = (*Tracker)(nil)
