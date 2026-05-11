package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestNotifyResultPosts(t *testing.T) {
	var got slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	n := New(srv.URL)
	err := n.NotifyResult(context.Background(),
		&vcs.PullRequest{RepoFullName: "o/r", Number: 7, URL: "https://example/pr/7"},
		"review",
		&tools.Result{Summary: "looks fine", CheckRun: &vcs.CheckRun{Status: vcs.CheckSucceeded, Title: "0 findings"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text == "" || len(got.Blocks) < 1 {
		t.Errorf("got %+v", got)
	}
}

func TestNotifyResultEmptyWebhookIsNoop(t *testing.T) {
	n := New("")
	if err := n.NotifyResult(context.Background(), &vcs.PullRequest{}, "review", &tools.Result{}); err != nil {
		t.Fatal(err)
	}
}
