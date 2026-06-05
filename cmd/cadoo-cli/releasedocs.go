// release-docs subcommand for cadoo-cli. Stateless, one-entry-pool CLI that
// resolves the VCS provider, builds a releasedocs.Dispatcher with the default
// generators and publishers, and runs it against the supplied tag range.
// Idempotency is marker-based (stateless, no DB — Phase-1 only).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/llm/litellm"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/defaults"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// releaseDocsCmd is the entry point for `cadoo release-docs`.
//
// It parses flags, validates inputs (Security V5 — reuse parseTargetURL for
// URL-form inputs), builds a stateless releasedocs.Dispatcher backed by a
// one-entry VCSPool, and calls Dispatcher.Run. The env contract is identical
// to `cadoo ci` — no new secret names are introduced (T-07-04).
func releaseDocsCmd(args []string) {
	rdFS := flag.NewFlagSet("release-docs", flag.ExitOnError)

	// --repo accepts "owner/repo" form. --mr / --pr accept a full PR/MR URL
	// which is parsed via parseTargetURL for URL validation (Security V5).
	// Flag→ReleaseJob mapping is Claude's Discretion per D-16.
	var repoFlag string
	var prURL string
	rdFS.StringVar(&repoFlag, "repo", "", "repository in owner/repo form (e.g. payamqorbanpour/cadoo)")
	rdFS.StringVar(&prURL, "mr", "", "pull-request or merge-request URL (alternate form; derives repo + provider)")
	rdFS.StringVar(&prURL, "pr", "", "pull-request or merge-request URL (alias of --mr)")

	fromRef := rdFS.String("from", "", "prior release tag or commit SHA (range start, exclusive)")
	toRef := rdFS.String("to", "", "new release tag (range end, inclusive)")
	prHost := rdFS.String("pr-host", "", "VCS host for --repo form (defaults to github.com); GHES/GitLab hosts override provider detection")

	if err := rdFS.Parse(args); err != nil {
		os.Exit(2)
	}

	// Derive ciTarget from flags, applying URL validation (Security V5, T-07-01).
	var target ciTarget
	switch {
	case prURL != "":
		// URL form: parse provider + repo from the PR/MR URL.
		var err error
		target, err = parseTargetURL(prURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, "release-docs:", err)
			os.Exit(2)
		}
	case repoFlag != "":
		// Repo form: owner/repo + optional --pr-host for provider detection.
		host := *prHost
		if host == "" {
			host = "github.com"
		}
		// Synthesise a PR URL so parseTargetURL can validate the host and extract
		// the vcs.Kind; the PR number is irrelevant for release-docs.
		var err error
		target, err = parseTargetURL("https://" + host + "/" + repoFlag + "/pull/1")
		if err != nil {
			fmt.Fprintln(os.Stderr, "release-docs: --repo:", err)
			os.Exit(2)
		}
	default:
		fmt.Fprintln(os.Stderr, "release-docs: --repo or --mr/--pr URL is required")
		rdFS.Usage()
		os.Exit(2)
	}

	if *toRef == "" {
		fmt.Fprintln(os.Stderr, "release-docs: --to (new release tag) is required")
		os.Exit(2)
	}

	// Build the ReleaseJob from the resolved target + flags (D-16).
	job := releasedocs.ReleaseJob{
		Provider: target.Provider,
		Repo:     target.ProjectPath,
		FromRef:  *fromRef,
		ToRef:    *toRef,
	}

	// Build the VCS provider — one-entry pool, mirrors ci.go:buildProvider.
	provider, err := buildProvider(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release-docs:", err)
		os.Exit(1)
	}

	// LLM is nil-tolerant: when env is absent we keep it nil so generators
	// fall back to deterministic output (D-10, D-11). Env contract is identical
	// to cadoo ci — no new secret names (T-07-04).
	llmURL := envOr("LLM_GATEWAY_URL", os.Getenv("OPENAI_API_BASE"))
	llmKey := envOr("LLM_GATEWAY_API_KEY", os.Getenv("OPENAI_API_KEY"))
	model := os.Getenv("CADOO_DEFAULT_MODEL")

	var llmProvider llm.Provider
	if llmURL != "" {
		llmProvider = litellm.New(llmURL, llmKey)
	}

	// Build the stateless dispatcher (no DB, no audit, no KB — Phase-1 only).
	d := &releasedocs.Dispatcher{
		VCSPool:    map[vcs.Kind]vcs.Provider{target.Provider: provider},
		LLM:        llmProvider,
		Model:      model,
		Generators: defaults.DefaultGenerators(),
		Publishers: defaults.DefaultPublishers(),
	}

	ctx := context.Background()
	fmt.Fprintf(os.Stderr, "release-docs: running %s..%s on %s\n", job.FromRef, job.ToRef, job.Repo)
	if err := d.Run(ctx, job); err != nil {
		fmt.Fprintln(os.Stderr, "release-docs:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "release-docs: done")
}
