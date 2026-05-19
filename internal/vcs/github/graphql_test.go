package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGraphQLEndpoint(t *testing.T) {
	cases := []struct{ rest, want string }{
		{"https://api.github.com/", "https://api.github.com/graphql"},
		{"https://ghe.example.com/api/v3/", "https://ghe.example.com/api/graphql"},
	}
	for _, tc := range cases {
		u, _ := url.Parse(tc.rest)
		if got := graphqlEndpoint(u); got != tc.want {
			t.Errorf("graphqlEndpoint(%s) = %s; want %s", tc.rest, got, tc.want)
		}
	}
}

func TestDoGraphQLDecodesDataAndErrors(t *testing.T) {
	ctx := context.Background()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"x":{"n":7}}}`))
	}))
	defer ok.Close()
	var out struct {
		X struct{ N int } `json:"x"`
	}
	if err := doGraphQL(ctx, ok.Client(), ok.URL, "query{}", nil, &out); err != nil {
		t.Fatalf("doGraphQL ok: %v", err)
	}
	if out.X.N != 7 {
		t.Errorf("decoded N = %d; want 7", out.X.N)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	}))
	defer bad.Close()
	if err := doGraphQL(ctx, bad.Client(), bad.URL, "query{}", nil, &out); err == nil {
		t.Error("doGraphQL with GraphQL errors: err=nil; want error")
	}
}
