// Package contextengine packs a PR's diff into a token-bounded payload for
// the LLM. The strategy is "PR Compression": filter by
// path globs, drop binaries, sort small files first, allocate a per-file
// budget, truncate overflow.
package contextengine

import (
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// EstimateTokens returns a conservative token estimate using ~4 chars/token.
func EstimateTokens(s string) int { return (len(s) + 3) / 4 }

// CompressOptions tunes the pack algorithm.
type CompressOptions struct {
	MaxTokens    int      // total token budget for all packed files
	PerFileMax   int      // hard cap per file's patch (0 = unlimited within budget)
	IncludePaths []string // empty == include all
	ExcludePaths []string // applied first; an exclude beats an include
}

// Compressed is the packed result.
type Compressed struct {
	Files     []vcs.FileChange
	Truncated []string // paths whose patch was truncated
	Skipped   []string // paths dropped entirely (filtered out, binary, or out of budget)
	EstTokens int      // total estimated tokens used
}

// Compress applies the PR-compression strategy.
func Compress(files []vcs.FileChange, opts CompressOptions) Compressed {
	var kept []vcs.FileChange
	skipped := []string{}

	for _, f := range files {
		if f.IsBinary {
			skipped = append(skipped, f.Path)
			continue
		}
		if !pathMatches(f.Path, opts.IncludePaths, opts.ExcludePaths) {
			skipped = append(skipped, f.Path)
			continue
		}
		kept = append(kept, f)
	}

	sort.SliceStable(kept, func(i, j int) bool {
		return EstimateTokens(kept[i].Patch) < EstimateTokens(kept[j].Patch)
	})

	budget := opts.MaxTokens
	if budget <= 0 {
		budget = 50_000
	}

	packed := make([]vcs.FileChange, 0, len(kept))
	truncated := []string{}

	for _, f := range kept {
		if budget <= 0 {
			skipped = append(skipped, f.Path)
			continue
		}
		tokens := EstimateTokens(f.Patch)
		fileCap := opts.PerFileMax
		if fileCap == 0 || fileCap > budget {
			fileCap = budget
		}
		if tokens <= fileCap {
			packed = append(packed, f)
			budget -= tokens
			continue
		}
		f2 := f
		f2.Patch = truncatePatch(f.Patch, fileCap)
		packed = append(packed, f2)
		budget -= EstimateTokens(f2.Patch)
		truncated = append(truncated, f.Path)
	}

	return Compressed{
		Files:     packed,
		Truncated: truncated,
		Skipped:   skipped,
		EstTokens: opts.MaxTokens - budget,
	}
}

func pathMatches(path string, include, exclude []string) bool {
	for _, glob := range exclude {
		if ok, _ := doublestar.Match(glob, path); ok {
			return false
		}
	}
	if len(include) == 0 {
		return true
	}
	for _, glob := range include {
		if ok, _ := doublestar.Match(glob, path); ok {
			return true
		}
	}
	return false
}

func truncatePatch(patch string, tokenCap int) string {
	charCap := tokenCap * 4
	if len(patch) <= charCap {
		return patch
	}
	// Leave room for the truncation marker.
	const marker = "\n... (truncated by Cadoo)"
	if charCap <= len(marker) {
		return marker
	}
	return strings.TrimRight(patch[:charCap-len(marker)], "\n") + marker
}
