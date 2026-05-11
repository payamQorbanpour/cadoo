package linear

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindLinked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct{ ID string } `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Variables.ID != "ENG-1" {
			t.Errorf("id: %q", req.Variables.ID)
		}
		_, _ = w.Write([]byte(`{"data":{"issue":{"identifier":"ENG-1","title":"first","state":{"name":"Started"},"url":"https://linear.app/x/ENG-1"}}}`))
	}))
	defer srv.Close()
	tr := New(srv.URL, "k")
	got, err := tr.FindLinked(context.Background(), "fix ENG-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "ENG-1" || got[0].Status != "Started" {
		t.Errorf("got %+v", got)
	}
}
