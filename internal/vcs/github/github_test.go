package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestGitHubListCadooArtifactsAndResolve(t *testing.T) {
	var resolvedWith string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var q struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &q)
		switch {
		case strings.Contains(q.Query, "resolveReviewThread"):
			resolvedWith, _ = q.Variables["id"].(string)
			_, _ = w.Write([]byte(`{"data":{"resolveReviewThread":{"thread":{"id":"T1"}}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{
			  "comments":{"nodes":[{"databaseId":55,"body":"` + vcs.SummaryWrapperBegin + ` overview"}],
			    "pageInfo":{"hasNextPage":false,"endCursor":null}},
			  "reviewThreads":{"nodes":[{"id":"T1","isResolved":false,
			    "comments":{"nodes":[{"path":"a.go","body":"**[WARN]** Fix the leak.\n\n<!-- cadoo:fp v=1 tool=review sk=deadbeefdeadbeef sev=warn -->"}]}}],
			    "pageInfo":{"hasNextPage":false,"endCursor":null}}
			}}}}`))
		}
	}))
	defer srv.Close()

	a, err := New(Config{Token: "t"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.gqlEndpoint = srv.URL
	a.gqlClient = srv.Client()

	ctx := context.Background()
	pr := &vcs.PullRequest{RepoFullName: "o/r", Number: 3}
	got, err := a.ListCadooArtifacts(ctx, pr)
	if err != nil {
		t.Fatalf("ListCadooArtifacts: %v", err)
	}
	if got.SummaryCommentID != "55" {
		t.Errorf("SummaryCommentID = %q; want 55", got.SummaryCommentID)
	}
	if len(got.Inline) != 1 || got.Inline[0].ExternalID != "T1" ||
		got.Inline[0].StructuralKey != "deadbeefdeadbeef" ||
		got.Inline[0].File != "a.go" || got.Inline[0].Title != "Fix the leak." {
		t.Fatalf("inline = %+v; unexpected", got.Inline)
	}

	if err := a.ResolveThread(ctx, pr, "T1"); err != nil {
		t.Fatalf("ResolveThread: %v", err)
	}
	if resolvedWith != "T1" {
		t.Errorf("resolveReviewThread called with %q; want T1", resolvedWith)
	}
}

func TestGitHubListCadooArtifactsPaginationNoDuplicate(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var q struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &q)
		call++
		// reviewThreads: one page only (hasNextPage:false), one marked thread.
		threads := `"reviewThreads":{"nodes":[{"id":"T1","isResolved":false,` +
			`"comments":{"nodes":[{"path":"a.go","body":"**[WARN]** Fix the leak.\n\n<!-- cadoo:fp v=1 tool=review sk=deadbeefdeadbeef sev=warn -->"}]}}],` +
			`"pageInfo":{"hasNextPage":false,"endCursor":null}}`
		var comments string
		if _, ok := q.Variables["tc"]; !ok {
			// first page of comments: hasNextPage true, no overview yet
			comments = `"comments":{"nodes":[{"databaseId":1,"body":"unrelated"}],` +
				`"pageInfo":{"hasNextPage":true,"endCursor":"C1"}}`
		} else {
			// second page: contains the overview, no more pages
			comments = `"comments":{"nodes":[{"databaseId":55,"body":"` + vcs.SummaryWrapperBegin + ` overview"}],` +
				`"pageInfo":{"hasNextPage":false,"endCursor":null}}`
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{` + comments + `,` + threads + `}}}}`))
	}))
	defer srv.Close()

	a, err := New(Config{Token: "t"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.gqlEndpoint = srv.URL
	a.gqlClient = srv.Client()

	got, err := a.ListCadooArtifacts(context.Background(), &vcs.PullRequest{RepoFullName: "o/r", Number: 5})
	if err != nil {
		t.Fatalf("ListCadooArtifacts: %v", err)
	}
	if len(got.Inline) != 1 {
		t.Fatalf("inline count = %d; want 1 (no duplicate from re-fetched exhausted reviewThreads page)", len(got.Inline))
	}
	if got.Inline[0].StructuralKey != "deadbeefdeadbeef" {
		t.Errorf("inline sk = %q; want deadbeefdeadbeef", got.Inline[0].StructuralKey)
	}
	if got.SummaryCommentID != "55" {
		t.Errorf("SummaryCommentID = %q; want 55 (found on comments page 2)", got.SummaryCommentID)
	}
	if call < 2 {
		t.Errorf("server calls = %d; want >=2 (comments paginated)", call)
	}
}
