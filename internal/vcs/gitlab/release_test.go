package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// newTestGitLabAdapter creates a GitLab adapter pointing at the given httptest server.
func newTestGitLabAdapter(t *testing.T, srv *httptest.Server) *Adapter {
	t.Helper()
	a, err := New(Config{BaseURL: srv.URL, Token: "test-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// TestGitLabListCommits verifies that ListCommits normalizes the Compare
// response into vcs.Commit values.
func TestGitLabListCommits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/compare") {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"commits": []map[string]any{
				{
					"id":            "abc123",
					"message":       "feat: add thing",
					"author_name":   "Alice",
					"authored_date": "2024-01-01T00:00:00Z",
				},
				{
					"id":            "def456",
					"message":       "fix: patch bug",
					"author_name":   "Bob",
					"authored_date": "2024-01-02T00:00:00Z",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	commits, err := a.ListCommits(context.Background(), "group/project", "v1.0.0", "v1.1.0")
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
	if commits[0].Author != "Alice" {
		t.Errorf("commit[0].Author = %q; want Alice", commits[0].Author)
	}
	if commits[1].SHA != "def456" {
		t.Errorf("commit[1].SHA = %q; want def456", commits[1].SHA)
	}
}

// TestGitLabLatestTagBefore verifies that LatestTagBefore returns the first
// tag matching the pattern, skipping toRef.
func TestGitLabLatestTagBefore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tags") {
			http.NotFound(w, r)
			return
		}
		tags := []map[string]any{
			{"name": "v1.2.0", "commit": map[string]any{"id": "sha1"}},
			{"name": "v1.1.0", "commit": map[string]any{"id": "sha2"}},
			{"name": "v1.0.0", "commit": map[string]any{"id": "sha3"}},
		}
		_ = json.NewEncoder(w).Encode(tags)
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	// v1.2.0 matches "v*" but is excluded (it equals toRef).
	tag, err := a.LatestTagBefore(context.Background(), "group/project", "v1.2.0", "v*")
	if err != nil {
		t.Fatalf("LatestTagBefore: %v", err)
	}
	if tag != "v1.1.0" {
		t.Errorf("LatestTagBefore = %q; want v1.1.0", tag)
	}
}

// TestGitLabLatestTagBeforeNoMatch verifies that LatestTagBefore returns ""
// when no matching tag exists before toRef.
func TestGitLabLatestTagBeforeNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tags") {
			http.NotFound(w, r)
			return
		}
		// Only a non-matching tag.
		tags := []map[string]any{
			{"name": "release-1.0", "commit": map[string]any{"id": "sha1"}},
		}
		_ = json.NewEncoder(w).Encode(tags)
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	tag, err := a.LatestTagBefore(context.Background(), "group/project", "v1.2.0", "v*")
	if err != nil {
		t.Fatalf("LatestTagBefore: %v", err)
	}
	if tag != "" {
		t.Errorf("LatestTagBefore = %q; want empty string", tag)
	}
}

// TestGitLabGetReleaseByTag verifies that GetReleaseByTag returns a normalized
// vcs.Release from the stubbed REST response.
func TestGitLabGetReleaseByTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/releases/") {
			http.NotFound(w, r)
			return
		}
		rel := map[string]any{
			"tag_name":    "v1.1.0",
			"description": "Initial release notes",
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	rel, err := a.GetReleaseByTag(context.Background(), "group/project", "v1.1.0")
	if err != nil {
		t.Fatalf("GetReleaseByTag: %v", err)
	}
	if rel.TagName != "v1.1.0" {
		t.Errorf("Release.TagName = %q; want v1.1.0", rel.TagName)
	}
	if rel.Body != "Initial release notes" {
		t.Errorf("Release.Body = %q; want 'Initial release notes'", rel.Body)
	}
	// GitLab releases have no numeric ID; ID is 0.
	if rel.ID != 0 {
		t.Errorf("Release.ID = %d; want 0 (GitLab has no numeric release ID)", rel.ID)
	}
}

// TestGitLabUpdateReleaseBodyByTag verifies that UpdateReleaseBodyByTag sends
// a PUT/PATCH request with the new description.
func TestGitLabUpdateReleaseBodyByTag(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/v1.1.0") &&
			(r.Method == http.MethodPut || r.Method == http.MethodPatch) {
			var payload struct {
				Description string `json:"description"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			capturedBody = payload.Description
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":    "v1.1.0",
				"description": payload.Description,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	err := a.UpdateReleaseBodyByTag(context.Background(), "group/project", "v1.1.0", "Updated release notes")
	if err != nil {
		t.Fatalf("UpdateReleaseBodyByTag: %v", err)
	}
	if capturedBody != "Updated release notes" {
		t.Errorf("description sent = %q; want 'Updated release notes'", capturedBody)
	}
}

// TestGitLabUpsertFileCreate verifies that UpsertFile creates a file when it
// doesn't exist yet.
func TestGitLabUpsertFileCreate(t *testing.T) {
	var commitMessage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// GET file (returns 404 to simulate new file on missing branch)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/files/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 File Not Found"}`))
		// POST to create file
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/files/"):
			var payload struct {
				CommitMessage string `json:"commit_message"`
				Branch        string `json:"branch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			commitMessage = payload.CommitMessage
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_path": "CHANGELOG.md",
				"branch":    payload.Branch,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	err := a.UpsertFile(context.Background(), "group/project", "cadoo/changelog/v1.1.0",
		"chore: update CHANGELOG.md", vcs.FileWrite{
			Path:    "CHANGELOG.md",
			Content: []byte("# Changelog\n"),
		})
	if err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}
	if commitMessage != "chore: update CHANGELOG.md" {
		t.Errorf("commit_message = %q; want 'chore: update CHANGELOG.md'", commitMessage)
	}
}

// TestGitLabUpsertFileUpdate verifies that UpsertFile updates an existing file.
func TestGitLabUpsertFileUpdate(t *testing.T) {
	var updatedContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// GET file (returns existing file)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/files/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_path":      "CHANGELOG.md",
				"last_commit_id": "oldcommitid",
				"content":        "",
			})
		// PUT to update file
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/files/"):
			var payload struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			updatedContent = payload.Content
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file_path": "CHANGELOG.md",
				"branch":    "cadoo/changelog/v1.1.0",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	err := a.UpsertFile(context.Background(), "group/project", "cadoo/changelog/v1.1.0",
		"chore: update CHANGELOG.md", vcs.FileWrite{
			Path:    "CHANGELOG.md",
			Content: []byte("# Changelog\nnew content"),
		})
	if err != nil {
		t.Fatalf("UpsertFile update: %v", err)
	}
	if updatedContent == "" {
		t.Error("expected content to be sent in update request")
	}
}

// TestGitLabOpenOrUpdatePRCreate verifies that OpenOrUpdatePR creates a new MR
// when none exists.
func TestGitLabOpenOrUpdatePRCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// List MRs: return empty list
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/merge_requests"):
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		// Create MR
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/merge_requests"):
			var payload struct {
				Title        string `json:"title"`
				SourceBranch string `json:"source_branch"`
				TargetBranch string `json:"target_branch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"iid":           int64(42),
				"title":         payload.Title,
				"source_branch": payload.SourceBranch,
				"target_branch": payload.TargetBranch,
				"state":         "opened",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	num, err := a.OpenOrUpdatePR(context.Background(), "group/project",
		"cadoo/changelog/v1.1.0", "main",
		"chore: CHANGELOG.md for v1.1.0", "<!-- cadoo:changelog:v1.1.0 -->\n...")
	if err != nil {
		t.Fatalf("OpenOrUpdatePR: %v", err)
	}
	if num != 42 {
		t.Errorf("MR IID = %d; want 42", num)
	}
}

// TestGitLabOpenOrUpdatePRUpdate verifies that OpenOrUpdatePR updates an
// existing open MR when one is found.
func TestGitLabOpenOrUpdatePRUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// List MRs: return one open MR with old title/body
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/merge_requests"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"iid":           int64(77),
					"title":         "old title",
					"description":   "old body",
					"state":         "opened",
					"source_branch": "cadoo/changelog/v1.1.0",
					"target_branch": "main",
				},
			})
		// PATCH/PUT to update MR
		case (r.Method == http.MethodPut || r.Method == http.MethodPatch) && strings.Contains(r.URL.Path, "/merge_requests/"):
			var payload struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"iid":         int64(77),
				"title":       payload.Title,
				"description": payload.Description,
				"state":       "opened",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a := newTestGitLabAdapter(t, srv)
	num, err := a.OpenOrUpdatePR(context.Background(), "group/project",
		"cadoo/changelog/v1.1.0", "main",
		"new title", "new body")
	if err != nil {
		t.Fatalf("OpenOrUpdatePR: %v", err)
	}
	if num != 77 {
		t.Errorf("MR IID = %d; want 77", num)
	}
}
