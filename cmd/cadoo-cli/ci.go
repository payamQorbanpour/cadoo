// CI-mode for cadoo-cli: a one-shot review pass intended to run inside a
// GitLab CI pipeline on merge_request_event or a GitHub Actions workflow on
// pull_request. Reads the provider-specific token env (GITLAB_TOKEN /
// GITHUB_TOKEN) and $LLM_GATEWAY_* from the environment, parses the PR/MR
// URL, builds a stateless Dispatcher (no KB / learnings / audit / sandbox),
// and dispatches the requested tools concurrently (one goroutine per tool).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/findings"
	"github.com/payamqorbanpour/cadoo/internal/llm/litellm"
	"github.com/payamqorbanpour/cadoo/internal/orchestrator"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
	cadoogh "github.com/payamqorbanpour/cadoo/internal/vcs/github"
	cadoogl "github.com/payamqorbanpour/cadoo/internal/vcs/gitlab"
)

// ciTarget is everything ci needs to talk to one pull-request / merge-request.
type ciTarget struct {
	Provider    vcs.Kind // KindGitHub / KindGitHubEnterprise / KindGitLab
	BaseURL     string   // e.g. "https://gitlab.example.com" or "https://github.com" (no trailing slash, no /api)
	APIBaseURL  string   // GitLab: BaseURL+"/api/v4"; GHES: BaseURL+"/api/v3"; github.com: ""
	ProjectPath string   // "group/subgroup/project" or "owner/repo"
	Number      int64    // MR IID or PR number
}

// parseTargetURL accepts the standard PR/MR URL forms for GitLab and GitHub.
//
// Supported:
//
//	GitLab:
//	  - https://gitlab.com/group/project/-/merge_requests/42
//	  - https://gitlab.example.com/group/subgroup/project/-/merge_requests/42
//	  - https://gitlab.example.com/group/project/merge_requests/42  (legacy)
//	GitHub:
//	  - https://github.com/owner/repo/pull/42
//	  - https://ghe.example.com/owner/repo/pull/42  (GHES → KindGitHubEnterprise)
func parseTargetURL(raw string) (ciTarget, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ciTarget{}, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return ciTarget{}, fmt.Errorf("missing scheme or host in %q", raw)
	}
	path := strings.Trim(u.Path, "/")
	base := strings.TrimRight(u.Scheme+"://"+u.Host, "/")

	// GitLab MR — modern "/-/merge_requests/" then legacy "/merge_requests/".
	for _, sep := range []string{"/-/merge_requests/", "/merge_requests/"} {
		project, tail, ok := strings.Cut(path, sep)
		if !ok {
			continue
		}
		n, err := parseTrailingNumber(tail)
		if err != nil || project == "" {
			return ciTarget{}, fmt.Errorf("not a valid merge-request URL: %q", raw)
		}
		return ciTarget{
			Provider:    vcs.KindGitLab,
			BaseURL:     base,
			APIBaseURL:  base + "/api/v4",
			ProjectPath: project,
			Number:      n,
		}, nil
	}

	// GitHub PR — "<owner>/<repo>/pull/<N>".
	if project, tail, ok := strings.Cut(path, "/pull/"); ok {
		n, err := parseTrailingNumber(tail)
		if err != nil {
			return ciTarget{}, fmt.Errorf("not a valid pull-request URL: %q", raw)
		}
		if strings.Count(project, "/") != 1 || project == "" {
			return ciTarget{}, fmt.Errorf("not a valid pull-request URL: %q", raw)
		}
		kind := vcs.KindGitHub
		apiBase := ""
		if u.Host != "github.com" {
			kind = vcs.KindGitHubEnterprise
			apiBase = base + "/api/v3"
		}
		return ciTarget{
			Provider:    kind,
			BaseURL:     base,
			APIBaseURL:  apiBase,
			ProjectPath: project,
			Number:      n,
		}, nil
	}

	return ciTarget{}, fmt.Errorf("not a pull-request or merge-request URL: %q", raw)
}

// parseTrailingNumber strips any path after the first slash and parses the
// leading positive integer (e.g. "42/files" → 42).
func parseTrailingNumber(s string) (int64, error) {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("not a positive integer: %q", s)
	}
	return n, nil
}

// ciCmd is the entry point for `cadoo ci`.
func ciCmd(args []string) {
	fs := flag.NewFlagSet("ci", flag.ExitOnError)
	var targetURL string
	fs.StringVar(&targetURL, "mr", "", "GitLab merge-request URL")
	fs.StringVar(&targetURL, "pr", "", "GitHub pull-request URL (alias of --mr)")
	toolsCSV := fs.String("tools", "describe,review,improve",
		"comma-separated tools to run in order (e.g. describe,review,improve)")
	cfgPath := fs.String("config", "", "path to .cadoo.yaml (default: <repo>/.cadoo.yaml)")
	defaultRepo := envOr("CI_PROJECT_DIR", envOr("GITHUB_WORKSPACE", "."))
	repoDir := fs.String("repo", defaultRepo,
		"path to the checked-out repo root (used to locate .cadoo.yaml)")
	reportStatus := fs.Bool("status", parseBoolEnv("CADOO_REPORT_STATUS", false),
		"post a commit status / check-run on the head SHA (default: false; can also be set via CADOO_REPORT_STATUS)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if targetURL == "" {
		fmt.Fprintln(os.Stderr, "ci: --mr (GitLab) or --pr (GitHub) URL is required")
		os.Exit(2)
	}

	target, err := parseTargetURL(targetURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ci:", err)
		os.Exit(2)
	}

	provider, err := buildProvider(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ci:", err)
		os.Exit(2)
	}

	llmURL := envOr("LLM_GATEWAY_URL", os.Getenv("OPENAI_API_BASE"))
	llmKey := envOr("LLM_GATEWAY_API_KEY", os.Getenv("OPENAI_API_KEY"))
	model := os.Getenv("CADOO_DEFAULT_MODEL")
	if llmURL == "" {
		fmt.Fprintln(os.Stderr, "ci: LLM_GATEWAY_URL (or OPENAI_API_BASE) env is required")
		os.Exit(2)
	}

	// Per-repo .cadoo.yaml (optional — missing file → defaults).
	cfgFile := *cfgPath
	if cfgFile == "" {
		cfgFile = filepath.Join(*repoDir, ".cadoo.yaml")
	}
	repoCfg, err := config.LoadFile(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ci: load %s: %v\n", cfgFile, err)
		os.Exit(1)
	}
	// Optional sibling free-form review brief (.cadoo.md), next to the YAML.
	mdPath := filepath.Join(filepath.Dir(cfgFile), config.MarkdownFilename)
	if repoCfg.Markdown, err = config.LoadMarkdown(mdPath); err != nil {
		fmt.Fprintf(os.Stderr, "ci: load %s: %v\n", mdPath, err)
		os.Exit(1)
	}

	// Stateless dispatcher: no DB, no audit, no KB.
	d := &orchestrator.Dispatcher{
		LLM:          litellm.New(llmURL, llmKey),
		VCSPool:      map[vcs.Kind]vcs.Provider{target.Provider: provider},
		Model:        model,
		BaseCfg:      repoCfg,
		Registry:     orchestrator.DefaultRegistry(),
		ReportStatus: *reportStatus,
	}

	ctx := context.Background()

	if rr, ok := provider.(vcs.PriorReviewReader); ok {
		if st := priorStore(ctx, rr, target.ProjectPath, target.Number, target.Provider); st != nil {
			d.Posted = st
		}
	}

	toolList := splitCSV(*toolsCSV)
	if len(toolList) == 0 {
		fmt.Fprintln(os.Stderr, "ci: --tools is empty")
		os.Exit(2)
	}

	// Validate tool names up-front so a typo in the YAML/job fails fast
	// instead of after the first model call has already burned tokens.
	for _, name := range toolList {
		if _, ok := d.Registry.Get(name); !ok {
			fmt.Fprintf(os.Stderr, "ci: unknown tool %q (registered: %v)\n", name, d.Registry.Names())
			os.Exit(2)
		}
	}

	sep := "#"
	if target.Provider == vcs.KindGitLab {
		sep = "!"
	}

	var (
		firstErr   error
		firstErrMu sync.Mutex
	)
	g, gctx := errgroup.WithContext(ctx)
	for _, name := range toolList {
		g.Go(func() error {
			fmt.Fprintf(os.Stderr, "ci: dispatching %s on %s%s%d\n", name, target.ProjectPath, sep, target.Number)
			job := orchestrator.ToolJob{
				Provider:     target.Provider,
				Tool:         name,
				RepoFullName: target.ProjectPath,
				PRNumber:     target.Number,
				Trigger:      "ci",
			}
			if err := d.Run(gctx, job); err != nil {
				fmt.Fprintf(os.Stderr, "ci: %s failed: %v\n", name, err)
				firstErrMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				firstErrMu.Unlock()
			}
			return nil // never cancel siblings on single-tool failure
		})
	}
	_ = g.Wait()
	if firstErr != nil {
		os.Exit(1)
	}
}

// priorStore reconstructs an in-memory findings.Store from the PR's own
// prior Cadoo artifacts so stateless CI-mode is idempotent across pushes.
// Returns nil when the read fails — callers then degrade to the
// non-idempotent legacy behaviour.
func priorStore(ctx context.Context, r vcs.PriorReviewReader, repo string, number int64, provider vcs.Kind) *findings.Store {
	pr := &vcs.PullRequest{Provider: provider, RepoFullName: repo, Number: number}
	snap, err := r.ListCadooArtifacts(ctx, pr)
	if err != nil {
		slog.Warn("ci: prior-artifact read-back failed; comments may duplicate this run",
			"err", err, "repo", repo, "number", number)
		return nil
	}
	key := findings.PRKey{Provider: string(provider), RepoFullName: repo, PRNumber: number}
	return findings.NewFromPrior(key, snap)
}

// buildProvider wires the right VCS adapter for the parsed target, reading
// the token from the provider-specific env var.
func buildProvider(target ciTarget) (vcs.Provider, error) {
	switch target.Provider {
	case vcs.KindGitLab:
		token := os.Getenv("GITLAB_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("GITLAB_TOKEN env is required for GitLab MRs")
		}
		gl, err := cadoogl.New(cadoogl.Config{
			BaseURL: target.BaseURL,
			Token:   token,
		})
		if err != nil {
			return nil, fmt.Errorf("gitlab client: %w", err)
		}
		return gl, nil
	case vcs.KindGitHub, vcs.KindGitHubEnterprise:
		token := os.Getenv("GITHUB_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("GITHUB_TOKEN env is required for GitHub PRs")
		}
		ghCfg := cadoogh.Config{Token: token}
		if target.Provider == vcs.KindGitHubEnterprise {
			ghCfg.BaseURL = target.APIBaseURL
		}
		gh, err := cadoogh.New(ghCfg)
		if err != nil {
			return nil, fmt.Errorf("github client: %w", err)
		}
		return gh, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", target.Provider)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseBoolEnv reads a boolean env var. Accepts "1/0", "true/false", "yes/no",
// "on/off" (case-insensitive). Empty or unparseable falls back to the default.
func parseBoolEnv(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}
