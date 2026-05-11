// CI-mode for cadoo-cli: a one-shot review pass intended to run inside a
// GitLab CI pipeline on merge_request_event. Reads $GITLAB_TOKEN +
// $LLM_GATEWAY_* from the environment, parses the MR URL, builds a
// stateless Dispatcher (no KB / learnings / audit / sandbox), and
// dispatches the requested tools sequentially.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm/litellm"
	"github.com/payamqorbanpour/cadoo/internal/orchestrator"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
	cadoogl "github.com/payamqorbanpour/cadoo/internal/vcs/gitlab"
)

// mrTarget is everything ci needs to talk to one merge request.
type mrTarget struct {
	BaseURL     string // e.g. "https://gitlab.example.com" (no trailing slash, no /api/v4)
	APIBaseURL  string // BaseURL + "/api/v4"
	ProjectPath string // e.g. "group/subgroup/project"
	IID         int64
}

// parseMRURL accepts the standard MR URL forms and returns the pieces the
// GitLab adapter + dispatcher need.
//
// Supported:
//   - https://gitlab.com/group/project/-/merge_requests/42
//   - https://gitlab.example.com/group/subgroup/project/-/merge_requests/42
//   - https://gitlab.example.com/group/project/merge_requests/42  (legacy)
func parseMRURL(raw string) (mrTarget, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return mrTarget{}, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return mrTarget{}, fmt.Errorf("missing scheme or host in %q", raw)
	}
	path := strings.Trim(u.Path, "/")
	// Try the modern "/-/merge_requests/" form first.
	project, iidStr, ok := strings.Cut(path, "/-/merge_requests/")
	if !ok {
		// Legacy form: "/merge_requests/" without the "/-/" separator.
		// We only treat it as legacy if "/-/merge_requests/" wasn't found.
		project, iidStr, ok = strings.Cut(path, "/merge_requests/")
	}
	if !ok || project == "" || iidStr == "" {
		return mrTarget{}, fmt.Errorf("not a merge-request URL: %q", raw)
	}
	// Strip any trailing path (e.g. "/diffs", "/commits").
	if i := strings.IndexByte(iidStr, '/'); i >= 0 {
		iidStr = iidStr[:i]
	}
	iid, err := strconv.ParseInt(iidStr, 10, 64)
	if err != nil || iid <= 0 {
		return mrTarget{}, fmt.Errorf("merge-request iid %q is not a positive integer", iidStr)
	}
	base := strings.TrimRight(u.Scheme+"://"+u.Host, "/")
	return mrTarget{
		BaseURL:     base,
		APIBaseURL:  base + "/api/v4",
		ProjectPath: project,
		IID:         iid,
	}, nil
}

// ciCmd is the entry point for `cadoo ci`.
func ciCmd(args []string) {
	fs := flag.NewFlagSet("ci", flag.ExitOnError)
	mrURL := fs.String("mr", "", "GitLab merge-request URL (required)")
	toolsCSV := fs.String("tools", "describe,review,improve",
		"comma-separated tools to run in order (e.g. describe,review,improve)")
	cfgPath := fs.String("config", "", "path to .cadoo.yaml (default: <repo>/.cadoo.yaml)")
	repoDir := fs.String("repo", envOr("CI_PROJECT_DIR", "."),
		"path to the checked-out repo root (used to locate .cadoo.yaml)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *mrURL == "" {
		fmt.Fprintln(os.Stderr, "ci: --mr is required (e.g. --mr \"$CI_MERGE_REQUEST_PROJECT_URL/-/merge_requests/$CI_MERGE_REQUEST_IID\")")
		os.Exit(2)
	}

	target, err := parseMRURL(*mrURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ci:", err)
		os.Exit(2)
	}

	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "ci: GITLAB_TOKEN env is required")
		os.Exit(2)
	}

	llmURL := envOr("LLM_GATEWAY_URL", os.Getenv("OPENAI_API_BASE"))
	llmKey := envOr("LLM_GATEWAY_API_KEY", os.Getenv("OPENAI_API_KEY"))
	model := envOr("CADOO_DEFAULT_MODEL", "claude-sonnet-4-6")
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

	// One-shot GitLab adapter.
	gl, err := cadoogl.New(cadoogl.Config{
		BaseURL: target.BaseURL,
		Token:   token,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ci: gitlab client:", err)
		os.Exit(1)
	}

	// Stateless dispatcher: no DB, no audit, no KB.
	d := &orchestrator.Dispatcher{
		LLM:      litellm.New(llmURL, llmKey),
		VCSPool:  map[vcs.Kind]vcs.Provider{vcs.KindGitLab: gl},
		Model:    model,
		BaseCfg:  repoCfg,
		Registry: orchestrator.DefaultRegistry(),
	}

	ctx := context.Background()
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

	var firstErr error
	for _, name := range toolList {
		fmt.Fprintf(os.Stderr, "ci: dispatching %s on %s!%d\n", name, target.ProjectPath, target.IID)
		job := orchestrator.ToolJob{
			Provider:     vcs.KindGitLab,
			Tool:         name,
			RepoFullName: target.ProjectPath,
			PRNumber:     target.IID,
			Trigger:      "ci",
		}
		if err := d.Run(ctx, job); err != nil {
			fmt.Fprintf(os.Stderr, "ci: %s failed: %v\n", name, err)
			if firstErr == nil {
				firstErr = err
			}
			// Keep going scripts run describe→review→improve
			// even if one stage errors. The pipeline can decide via
			// allow_failure: true whether to fail the build.
			continue
		}
	}
	if firstErr != nil {
		os.Exit(1)
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
