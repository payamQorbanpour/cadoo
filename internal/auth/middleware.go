package auth

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey int

const claimsKey ctxKey = iota

// Required is a chi middleware that verifies a Bearer token against verifier
// and stores the resulting Claims in the request context.
func Required(verifier *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := bearerToken(r)
			if tok == "" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			c, err := verifier.Verify(r.Context(), tok)
			if err != nil {
				http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, c)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole is a chi middleware that returns 403 unless the request's
// claims include any role at or above minRole.
func RequireRole(minRole Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := ClaimsFrom(r.Context())
			if !ok {
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
				return
			}
			if !c.HasRole(minRole) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClaimsFrom extracts the Claims that Required attached to ctx.
func ClaimsFrom(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// WithClaims attaches claims to ctx (exposed for tests).
func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, c)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}
