package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindLinkedFetchesEachKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/JIRA-1":
			_ = json.NewEncoder(w).Encode(jiraIssue{
				Key: "JIRA-1",
				Fields: struct {
					Summary  string                            `json:"summary"`
					Status   struct{ Name string }              `json:"status"`
					Assignee *struct{ DisplayName string `json:"displayName"` } `json:"assignee"`
					Labels   []string                          `json:"labels"`
					Updated  string                            `json:"updated"`
				}{Summary: "first", Status: struct{ Name string }{Name: "Open"}},
			})
		case "/rest/api/3/issue/JIRA-2":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	tracker := New(srv.URL, "x@example.com", "tok")
	got, err := tracker.FindLinked(context.Background(), "fix JIRA-1 and JIRA-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "JIRA-1" || got[0].Status != "Open" {
		t.Errorf("got %+v", got)
	}
}

func TestFindLinkedNoKeys(t *testing.T) {
	tracker := New("http://nope", "", "tok")
	got, err := tracker.FindLinked(context.Background(), "no keys here", "just prose")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected none, got %+v", got)
	}
}
