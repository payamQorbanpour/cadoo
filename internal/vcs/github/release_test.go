package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v66/github"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// newTestAdapter creates a GitHub adapter pointing at the given httptest server.
func newTestAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	a, err := New(Config{Token: "test-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Point the REST client at the test server. go-github's WithEnterpriseURLs
	// is the canonical way to override the base URL.
	client, err := gogithub.NewClient(srv.Client()).WithEnterpriseURLs(srv.URL+"/", srv.URL+"/")
	if err != nil {
		t.Fatalf("WithEnterpriseURLs: %v", err)
	}
	a.client = client.WithAuthToken("test-token")
	return a
}

// TestGitHubListCommits checks that ListCommits normalizes the CompareCommits
// response into vcs.Commit values.
func TestGitHubListCommits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/compare/") {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"commits": []map[string]any{
				{
					"sha": "abc123",
					"commit": map[string]any{
						"message": "feat: add thing",
						"author":  map[string]any{"name": "Alice", "date": "2024-01-01T00:00:00Z"},
					},
					"author": map[string]any{"login": "alice"},
				},
				{
					"sha": "def456",
					"commit": map[string]any{
						"message": "fix: patch bug",
						"author":  map[string]any{"name": "Bob", "date": "2024-01-02T00:00:00Z"},
					},
					"author": map[string]any{"login": "bob"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	commits, err := a.ListCommits(context.Background(), "owner/repo", "v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatalf("ListCommits: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("want 2 commits, got %d", len(commits))
	}
	if commits[0].SHA != "abc123" {
		t.Errorf("commit[0].SHA = %q; want abc123", commits[0].SHA)
	}
	if commits[0].Message != "feat: add thing" {
		t.Errorf("commit[0].Message = %q; want 'feat: add thing'", commits[0].Message)
	}
	if commits[0].Author != "alice" {
		t.Errorf("commit[0].Author = %q; want alice", commits[0].Author)
	}
	if commits[1].SHA != "def456" {
		t.Errorf("commit[1].SHA = %q; want def456", commits[1].SHA)
	}
}

// TestGitHubLatestTagBefore verifies that LatestTagBefore returns the first
// tag matching the pattern that is not equal to toRef.
func TestGitHubLatestTagBefore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tags") {
			http.NotFound(w, r)
			return
		}
		tags := []map[string]any{
			{"name": "v1.2.0", "commit": map[string]any{"sha": "sha1"}},
			{"name": "v1.1.0", "commit": map[string]any{"sha": "sha2"}},
			{"name": "v1.0.0", "commit": map[string]any{"sha": "sha3"}},
		}
		_ = json.NewEncoder(w).Encode(tags)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	// v1.2.0 matches "v*" but is excluded because it equals toRef.
	tag, err := a.LatestTagBefore(context.Background(), "owner/repo", "v1.2.0", "v*")
	if err != nil {
		t.Fatalf("LatestTagBefore: %v", err)
	}
	if tag != "v1.1.0" {
		t.Errorf("LatestTagBefore = %q; want v1.1.0", tag)
	}
}

// TestGitHubLatestTagBeforeNoMatch verifies that LatestTagBefore returns ""
// when no matching tag exists before toRef.
func TestGitHubLatestTagBeforeNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tags") {
			http.NotFound(w, r)
			return
		}
		// Only a non-matching tag exists.
		tags := []map[string]any{
			{"name": "release-1.0", "commit": map[string]any{"sha": "sha1"}},
		}
		_ = json.NewEncoder(w).Encode(tags)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	tag, err := a.LatestTagBefore(context.Background(), "owner/repo", "v1.2.0", "v*")
	if err != nil {
		t.Fatalf("LatestTagBefore: %v", err)
	}
	if tag != "" {
		t.Errorf("LatestTagBefore = %q; want empty string", tag)
	}
}

// TestGitHubGetReleaseByTag verifies that GetReleaseByTag returns a normalized
// vcs.Release from the stubbed REST response.
func TestGitHubGetReleaseByTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/releases/tags/") {
			http.NotFound(w, r)
			return
		}
		rel := map[string]any{
			"id":          int64(42),
			"tag_name":    "v1.1.0",
			"body":        "Initial release notes",
			"draft":       false,
			"prerelease":  false,
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	rel, err := a.GetReleaseByTag(context.Background(), "owner/repo", "v1.1.0")
	if err != nil {
		t.Fatalf("GetReleaseByTag: %v", err)
	}
	if rel.ID != 42 {
		t.Errorf("Release.ID = %d; want 42", rel.ID)
	}
	if rel.TagName != "v1.1.0" {
		t.Errorf("Release.TagName = %q; want v1.1.0", rel.TagName)
	}
	if rel.Body != "Initial release notes" {
		t.Errorf("Release.Body = %q; want 'Initial release notes'", rel.Body)
	}
}

// TestGitHubUpdateReleaseBody verifies that UpdateReleaseBody sends an EDIT
// request with the new body.
func TestGitHubUpdateReleaseBody(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/releases/") {
			var payload struct {
				Body string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			capturedBody = payload.Body
			// Return the updated release.
			rel := map[string]any{
				"id":       int64(42),
				"tag_name": "v1.1.0",
				"body":     payload.Body,
			}
			_ = json.NewEncoder(w).Encode(rel)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	err := a.UpdateReleaseBody(context.Background(), "owner/repo", 42, "Updated release notes")
	if err != nil {
		t.Fatalf("UpdateReleaseBody: %v", err)
	}
	if capturedBody != "Updated release notes" {
		t.Errorf("body sent = %q; want 'Updated release notes'", capturedBody)
	}
}

// TestGitHubUpsertFileCreate verifies that UpsertFile creates a file when the
// branch does not exist yet (branch creation + file create path).
func TestGitHubUpsertFileCreate(t *testing.T) {
	var createdFile string
	var createdBranch bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// GET repo to find default branch
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repos/owner/repo"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_branch": "main",
				"name":           "repo",
				"full_name":      "owner/repo",
			})
		// GET ref for existing branch (returns 404 to simulate missing branch)
		// go-github uses /git/ref/ (singular) for GetRef
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/cadoo"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		// GET ref for main branch (returns a ref so we can use its SHA)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/main"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ref": "refs/heads/main",
				"object": map[string]any{
					"sha":  "mainsha123",
					"type": "commit",
				},
			})
		// POST to create the new branch ref
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/git/refs"):
			createdBranch = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ref":    "refs/heads/cadoo/changelog/v1.1.0",
				"object": map[string]any{"sha": "mainsha123"},
			})
		// GET file contents (returns 404 to simulate new file)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/CHANGELOG.md"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		// PUT to create/update file (go-github uses PUT for both CreateFile and UpdateFile)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			var payload struct {
				Message string `json:"message"`
				Content []byte `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			createdFile = payload.Message
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]any{"name": "CHANGELOG.md"},
				"commit":  map[string]any{"sha": "newcommitsha"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	err := a.UpsertFile(context.Background(), "owner/repo", "cadoo/changelog/v1.1.0",
		"chore: update CHANGELOG.md", vcs.FileWrite{
			Path:    "CHANGELOG.md",
			Content: []byte("# Changelog\n"),
		})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if !createdBranch {
		t.Error("expected branch to be created, but it was not")
	}
	if createdFile != "chore: update CHANGELOG.md" {
		t.Errorf("commit message = %q; want 'chore: update CHANGELOG.md'", createdFile)
	}
}

// TestGitHubOpenOrUpdatePRCreate verifies that OpenOrUpdatePR creates a new PR
// when none exists.
func TestGitHubOpenOrUpdatePRCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// List PRs: return empty list (no open PR yet)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		// Create PR
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/pulls"):
			var payload struct {
				Title string `json:"title"`
				Head  string `json:"head"`
				Base  string `json:"base"`
				Body  string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 99,
				"title":  payload.Title,
				"head":   map[string]any{"ref": payload.Head},
				"base":   map[string]any{"ref": payload.Base},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	num, err := a.OpenOrUpdatePR(context.Background(), "owner/repo",
		"cadoo/changelog/v1.1.0", "main",
		"chore: CHANGELOG.md for v1.1.0", "<!-- cadoo:changelog:v1.1.0 -->\n...")
	if err != nil {
		t.Fatalf("OpenOrUpdatePR: %v", err)
	}
	if num != 99 {
		t.Errorf("PR number = %d; want 99", num)
	}
}

// TestGitHubOpenOrUpdatePRUpdate verifies that OpenOrUpdatePR updates an
// existing open PR when one is found.
func TestGitHubOpenOrUpdatePRUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// List PRs: return one open PR with old body
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"number": 77,
					"title":  "old title",
					"body":   "old body",
					"state":  "open",
					"head":   map[string]any{"ref": "cadoo/changelog/v1.1.0"},
					"base":   map[string]any{"ref": "main"},
				},
			})
		// PATCH to edit PR
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/pulls/"):
			var payload struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"number": 77,
				"title":  payload.Title,
				"body":   payload.Body,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newTestAdapter(t, srv)
	num, err := a.OpenOrUpdatePR(context.Background(), "owner/repo",
		"cadoo/changelog/v1.1.0", "main",
		"new title", "new body")
	if err != nil {
		t.Fatalf("OpenOrUpdatePR: %v", err)
	}
	if num != 77 {
		t.Errorf("PR number = %d; want 77", num)
	}
}
