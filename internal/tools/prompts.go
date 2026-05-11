package tools

import (
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/config"
)

// EffectivePrompt returns the system prompt a tool should actually send to
// the LLM, after applying the user's per-repo customization for tool.
//
// Resolution order:
//  1. cfg.Prompts[tool].Override — full replace (user takes responsibility)
//  2. defaultPrompt + addendum    — append-only customization (recommended)
//  3. defaultPrompt               — bare default
//
// In all cases the strictness section is appended last so it always wins
// over per-tool prose, mirroring the way `--strict` flags work in CLIs.
func EffectivePrompt(tool, defaultPrompt string, cfg config.Repo) string {
	p := cfg.Prompts[tool]
	var base string
	switch {
	case strings.TrimSpace(p.Override) != "":
		base = p.Override
	case strings.TrimSpace(p.Addendum) != "":
		base = defaultPrompt + "\n\n## Additional team-specific guidance\n\n" + p.Addendum
	default:
		base = defaultPrompt
	}
	if s := StrictnessSection(cfg.Strictness); s != "" {
		base += "\n\n" + s
	}
	return base
}

// StrictnessSection returns the system-prompt block describing the
// configured strictness level. Empty for unrecognized values.
func StrictnessSection(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "minimal":
		return `## Strictness: MINIMAL
Only flag bugs that will demonstrably break production. Skip everything else — including most security/performance hints — unless the change clearly introduces a real vulnerability or a measurable regression. When in doubt, do not flag.`
	case "strict":
		return `## Strictness: STRICT
Flag everything a senior reviewer would have a meaningful objection to: bugs, security, performance, brittle invariants, hidden coupling, missing tests on branchy code. Skip pure style nits unless they actively obscure intent.`
	case "pedantic":
		return `## Strictness: PEDANTIC
Flag every deviation, including style nits, missing comments, and minor maintainability concerns. The team has explicitly opted into maximum signal — do not self-censor.`
	case "balanced", "":
		return `## Strictness: BALANCED
Flag bugs, security issues, performance regressions, and clear maintainability problems. Skip nits, style preferences, and "consider X" speculation unless they hide real risk.`
	}
	return ""
}
