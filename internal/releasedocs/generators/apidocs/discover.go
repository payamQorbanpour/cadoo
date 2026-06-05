// Package apidocs (discover.go) implements spec discovery for the API-docs
// generator. discoverSpec locates the committed OpenAPI/Swagger spec using the
// configured path or the ordered fallback list (D-02).
package apidocs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
)

// fallbackPaths is the ordered list of repository-relative paths tried when
// apiDocs.specPath is not configured (D-02). The first path that returns a
// successful fetch wins; 404s are skipped; non-404 errors are logged and
// skipped (Pitfall 5 — never abort discovery on a transient auth error).
var fallbackPaths = []string{
	"openapi.yaml",
	"openapi.yml",
	"openapi.json",
	"docs/openapi.yaml",
	"api/openapi.yaml",
}

// isMissingFile reports whether err represents a file-not-found condition.
// It mirrors internal/releasedocs/template/template.go:isMissingFile verbatim:
// checks fs.ErrNotExist and matches VCS-client 404 strings heuristically so
// the apidocs package never imports VCS adapter packages directly.
func isMissingFile(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

// discoverSpec locates and fetches the committed OpenAPI/Swagger spec bytes.
//
// If rc.Config.Artifacts.APIDocs.SpecPath is non-empty, it fetches EXACTLY
// that path at rc.ToRef with no fallback (D-02). If it is empty, discoverSpec
// iterates fallbackPaths in order and returns the first successful fetch.
//
// 404 errors are silently skipped (isMissingFile). Non-404 errors are logged
// via slog.Warn and skipped — never aborting discovery (Pitfall 5 / D-10).
//
// Returns an error if rc.Provider does not implement releasedocs.FileFetcher,
// if the explicit specPath fetch fails, or if all fallback paths are exhausted.
func discoverSpec(ctx context.Context, rc releasedocs.ReleaseContext) ([]byte, error) {
	ff, ok := rc.Provider.(releasedocs.FileFetcher)
	if !ok {
		return nil, fmt.Errorf("apidocs: provider does not implement FileFetcher")
	}

	specPath := rc.Config.Artifacts.APIDocs.SpecPath
	if specPath != "" {
		// Explicit path configured: fetch exactly that path, no fallback (D-02).
		return ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, specPath)
	}

	// No explicit path — try fallbackPaths in order.
	for _, p := range fallbackPaths {
		b, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, p)
		if err == nil {
			return b, nil
		}
		if isMissingFile(err) {
			// 404 / not found → try next path.
			continue
		}
		// Non-404 error (transient auth, rate-limit, network): log and skip
		// rather than abort so sibling fallback paths still get a chance (Pitfall 5).
		slog.Warn("apidocs: spec fetch attempt failed; trying next fallback",
			"path", p, "err", err)
	}
	return nil, fmt.Errorf("apidocs: no spec found at any fallback path")
}
