// cadoo-api serves the public REST API.
//
// When DATABASE_URL is set it also exposes:
//   - /v1/audit       — admin-gated audit log query
//
// /metrics is always served (Prometheus exposition; safe to leave open or
// gate at the ingress).
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/payamqorbanpour/cadoo/internal/audit"
	"github.com/payamqorbanpour/cadoo/internal/auth"
	"github.com/payamqorbanpour/cadoo/internal/db"
	"github.com/payamqorbanpour/cadoo/internal/httpx"
	"github.com/payamqorbanpour/cadoo/internal/settings"
	"github.com/payamqorbanpour/cadoo/internal/version"
)

func main() {
	s, err := settings.FromEnv()
	if err != nil {
		slog.Error("settings", "err", err)
		os.Exit(1)
	}
	addr := s.HTTPAddr
	if addr == ":8081" { // settings default targets webhook; use API default here
		addr = envOr("HTTP_ADDR", ":8080")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var pool *pgxpool.Pool
	if s.DatabaseURL != "" {
		pool, err = db.Open(ctx, s.DatabaseURL)
		if err != nil {
			slog.Error("db open", "err", err)
			os.Exit(1)
		}
		defer pool.Close()
	}
	auditLog := audit.New(pool)

	var verifier *auth.Verifier
	if s.OIDCIssuer != "" && s.OIDCClientID != "" {
		verifier, err = auth.NewVerifier(ctx, s.OIDCIssuer, s.OIDCClientID)
		if err != nil {
			slog.Error("oidc verifier", "err", err)
			os.Exit(1)
		}
		slog.Info("OIDC verifier configured", "issuer", s.OIDCIssuer)
	} else {
		slog.Warn("OIDC not configured (OIDC_ISSUER + OIDC_CLIENT_ID); /v1/* will return 503")
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain")
		_, _ = w.Write([]byte(version.Version))
	})
	r.Handle("/metrics", promhttp.Handler())

	if verifier != nil {
		r.Route("/v1", func(r chi.Router) {
			r.Use(auth.Required(verifier))
			r.With(auth.RequireRole(auth.RoleAdmin)).Get("/audit", auditHandler(auditLog))
			r.Get("/me", meHandler())
		})
	} else {
		r.Get("/v1/*", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "OIDC not configured", http.StatusServiceUnavailable)
		})
	}

	if err := httpx.ListenAndServe(addr, r); err != nil {
		slog.Error("api shutdown", "err", err)
		os.Exit(1)
	}
}

func auditHandler(a *audit.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := auth.ClaimsFrom(r.Context())
		events, err := a.Query(r.Context(), claims.Org, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(events)
	}
}

func meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, _ := auth.ClaimsFrom(r.Context())
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(claims)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
