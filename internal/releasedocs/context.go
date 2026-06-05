package releasedocs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// BuildContext constructs a ReleaseContext that is passed to every Generator
// and Publisher. It performs the following steps in order:
//
//  1. Type-assert vcs.ReleaseRangeReader from provider — returns an error with
//     a clear reason if absent (a range read is required for generation; D-15).
//  2. Resolve FromRef via LatestTagBefore when job.FromRef is empty.
//  3. List commits and merged PRs in the range.
//  4. Compute the semver bump between FromRef and ToRef.
//  5. Build the shared GroupedModel once (conventional or labels grouping).
//
// llmProvider may be nil; when nil, generators must produce deterministic
// output without any LLM call (D-10, D-11).
func BuildContext(
	ctx context.Context,
	provider vcs.Provider,
	job ReleaseJob,
	cfg config.ReleaseDocs,
	llmProvider llm.Provider,
	model string,
) (ReleaseContext, error) {
	// Step 1: type-assert the required capability.
	rr, ok := provider.(vcs.ReleaseRangeReader)
	if !ok {
		return ReleaseContext{}, fmt.Errorf(
			"releasedocs: provider %q does not implement vcs.ReleaseRangeReader; "+
				"range read is required for release-docs generation (D-15). "+
				"Upgrade the VCS adapter or use a provider that implements this capability",
			provider.Kind(),
		)
	}

	// Step 2: resolve FromRef when empty.
	fromRef := job.FromRef
	if fromRef == "" {
		tagPattern := cfg.TagPattern
		if tagPattern == "" {
			tagPattern = "v*"
		}
		resolved, err := rr.LatestTagBefore(ctx, job.Repo, job.ToRef, tagPattern)
		if err != nil {
			slog.Warn("releasedocs: LatestTagBefore failed; proceeding with empty fromRef (first-release)",
				"repo", job.Repo, "toRef", job.ToRef, "err", err)
		} else {
			fromRef = resolved
		}
	}

	// Step 3: list commits and merged PRs in the range.
	commits, err := rr.ListCommits(ctx, job.Repo, fromRef, job.ToRef)
	if err != nil {
		return ReleaseContext{}, fmt.Errorf("releasedocs: list commits %s..%s: %w", fromRef, job.ToRef, err)
	}

	prs, err := rr.ListMergedPRs(ctx, job.Repo, fromRef, job.ToRef)
	if err != nil {
		return ReleaseContext{}, fmt.Errorf("releasedocs: list merged PRs %s..%s: %w", fromRef, job.ToRef, err)
	}

	// Step 4: compute semver bump.
	bump := ComputeBump(fromRef, job.ToRef)

	// Step 5: build the grouped model once.
	grouped := BuildGroupedModel(commits, prs, cfg)

	return ReleaseContext{
		Repo:         job.Repo,
		Org:          job.Org,
		FromRef:      fromRef,
		ToRef:        job.ToRef,
		Bump:         bump,
		Commits:      commits,
		MergedPRs:    prs,
		Config:       cfg,
		Provider:     provider,
		LLM:          llmProvider,
		Model:        model,
		GroupedModel: grouped,
	}, nil
}

// Enabled reports whether the artifact described by artifactCfg should be
// generated for the given semver bump. This implements the per-artifact
// enabled+when: gate (D-08) used by every Generator's Enabled method:
//
//   - artifactCfg.Enabled == false ⇒ always false, regardless of bump.
//   - artifactCfg.When == "" or "always" ⇒ true for any bump when enabled.
//   - artifactCfg.When == "major" ⇒ true only for BumpMajor.
//   - artifactCfg.When == "minor" ⇒ true only for BumpMinor.
//   - artifactCfg.When == "patch" ⇒ true only for BumpPatch.
//   - artifactCfg.When == "minor_or_above" ⇒ true for BumpMajor or BumpMinor.
//   - artifactCfg.When == "patch_or_above" ⇒ true for BumpMajor, BumpMinor, or BumpPatch.
func Enabled(artifactCfg config.ArtifactConfig, bump SemverBump) bool {
	if !artifactCfg.Enabled {
		return false
	}
	switch artifactCfg.When {
	case "", "always":
		return true
	case "major":
		return bump == BumpMajor
	case "minor":
		return bump == BumpMinor
	case "patch":
		return bump == BumpPatch
	case "minor_or_above":
		return bump == BumpMajor || bump == BumpMinor
	case "patch_or_above":
		return bump == BumpMajor || bump == BumpMinor || bump == BumpPatch
	default:
		slog.Warn("releasedocs: unknown when: value; treating as 'always'",
			"when", artifactCfg.When)
		return true
	}
}
