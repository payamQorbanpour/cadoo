package orchestrator

import (
	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/linters/eslint"
	"github.com/payamqorbanpour/cadoo/internal/analysis/linters/golangci"
	"github.com/payamqorbanpour/cadoo/internal/analysis/linters/ruff"
	"github.com/payamqorbanpour/cadoo/internal/analysis/linters/semgrep"
	"github.com/payamqorbanpour/cadoo/internal/analysis/linters/shellcheck"
)

// DefaultLintRegistry returns an analysis.Registry pre-loaded with every
// built-in linter wrapper. cmd binaries call this when sandboxed analysis is
// enabled. Override individual linters by re-registering.
//
// Each linter's Image field defaults to its language-specific upstream
// image; setting a single sandbox-polyglot image at construction time avoids
// pulling N base images.
func DefaultLintRegistry(polyglotImage string) *analysis.Registry {
	r := analysis.NewRegistry()
	r.Register(golangci.Linter{Image: polyglotImage})
	r.Register(ruff.Linter{Image: polyglotImage})
	r.Register(eslint.Linter{Image: polyglotImage})
	r.Register(semgrep.Linter{Image: polyglotImage})
	r.Register(shellcheck.Linter{Image: polyglotImage})
	return r
}
