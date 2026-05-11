package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRoleAllows(t *testing.T) {
	cases := []struct {
		have Role
		min  Role
		want bool
	}{
		{RoleOwner, RoleAdmin, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleMember, RoleAdmin, false},
		{RoleViewer, RoleMember, false},
		{RoleMember, RoleViewer, true},
		{Role("unknown"), RoleViewer, false},
	}
	for _, tc := range cases {
		if got := tc.have.Allows(tc.min); got != tc.want {
			t.Errorf("%s.Allows(%s) = %v want %v", tc.have, tc.min, got, tc.want)
		}
	}
}

func TestClaimsHasRole(t *testing.T) {
	c := &Claims{Roles: []string{"member", "viewer"}}
	if !c.HasRole(RoleMember) {
		t.Error("expected member to satisfy member")
	}
	if c.HasRole(RoleAdmin) {
		t.Error("member should not satisfy admin")
	}
}

func TestRequireRoleMiddleware(t *testing.T) {
	called := false
	handler := RequireRole(RoleAdmin)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))

	// no claims → 401
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusUnauthorized || called {
		t.Errorf("no claims: code=%d called=%v", rec.Code, called)
	}

	// member → 403
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(WithClaims(req.Context(), &Claims{Roles: []string{"member"}}))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || called {
		t.Errorf("member: code=%d called=%v", rec.Code, called)
	}

	// admin → 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(WithClaims(req.Context(), &Claims{Roles: []string{"admin"}}))
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 || !called {
		t.Errorf("admin: code=%d called=%v", rec.Code, called)
	}
}

func TestBearerTokenExtraction(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer abc.def.ghi")
	if got := bearerToken(r); got != "abc.def.ghi" {
		t.Errorf("got %q", got)
	}
	r.Header.Set("Authorization", "Token wrong")
	if got := bearerToken(r); got != "" {
		t.Errorf("expected empty for non-Bearer, got %q", got)
	}
}
