// cadoo-webhook receives VCS webhooks, verifies signatures, and enqueues
// tool jobs.
//
// Routes:
//   - POST /webhook/github   — GitHub.com and GitHub Enterprise Server
//   - POST /webhook/gitlab   — GitLab.com and self-managed GitLab
//
// When DATABASE_URL is set the webhook also attaches a Postgres-backed
// knowledge base and learnings store to the dispatcher; otherwise those
// features are simply disabled.
//
// Queue selection mirrors the same toggle:
//   - DATABASE_URL set → enqueue to River (Postgres). cadoo-worker consumes.
//   - DATABASE_URL empty → enqueue to in-process memory queue and consume in
//     a sibling goroutine. Single-binary dev mode.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	gogithub "github.com/google/go-github/v66/github"
	"github.com/jackc/pgx/v5/pgxpool"
	glab "gitlab.com/gitlab-org/api/client-go"

	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
	"github.com/payamqorbanpour/cadoo/internal/audit"
	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/db"
	"github.com/payamqorbanpour/cadoo/internal/findings"
	"github.com/payamqorbanpour/cadoo/internal/httpx"
	"github.com/payamqorbanpour/cadoo/internal/issuetrackers"
	"github.com/payamqorbanpour/cadoo/internal/issuetrackers/jira"
	"github.com/payamqorbanpour/cadoo/internal/issuetrackers/linear"
	"github.com/payamqorbanpour/cadoo/internal/jobs"
	"github.com/payamqorbanpour/cadoo/internal/kb"
	"github.com/payamqorbanpour/cadoo/internal/kb/querydistill"
	"github.com/payamqorbanpour/cadoo/internal/learnings"
	"github.com/payamqorbanpour/cadoo/internal/llm/embed"
	"github.com/payamqorbanpour/cadoo/internal/llm/litellm"
	"github.com/payamqorbanpour/cadoo/internal/notifiers/slack"
	"github.com/payamqorbanpour/cadoo/internal/orchestrator"
	"github.com/payamqorbanpour/cadoo/internal/riverq"
	"github.com/payamqorbanpour/cadoo/internal/settings"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
	cadoogh "github.com/payamqorbanpour/cadoo/internal/vcs/github"
	cadoogl "github.com/payamqorbanpour/cadoo/internal/vcs/gitlab"
)

type enqueueFn func(ctx context.Context, job orchestrator.ToolJob) error

func main() {
	s, err := settings.FromEnv()
	if err != nil {
		fatal("settings", err)
	}
	if !s.HasGitHub() && !s.HasGitLab() {
		slog.Warn("no VCS configured; webhook will accept events but reject signatures")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var pool *pgxpool.Pool
	if s.DatabaseURL != "" {
		pool, err = db.Open(ctx, s.DatabaseURL)
		if err != nil {
			fatal("db open", err)
		}
		defer pool.Close()
		if err := riverq.Migrate(ctx, pool); err != nil {
			fatal("river migrate", err)
		}
	}

	dispatcher, err := buildDispatcher(s, pool)
	if err != nil {
		fatal("dispatcher", err)
	}

	enqueue, queueCleanup := buildEnqueue(ctx, s, pool, dispatcher)
	defer queueCleanup()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Post("/webhook/github", githubWebhookHandler(s, enqueue))
	r.Post("/webhook/gitlab", gitlabWebhookHandler(s, enqueue))

	if err := httpx.ListenAndServe(s.HTTPAddr, r); err != nil {
		slog.Error("webhook shutdown", "err", err)
		os.Exit(1)
	}
}

// buildEnqueue picks River (when pool != nil) or an in-process memory queue.
func buildEnqueue(ctx context.Context, s *settings.Settings, pool *pgxpool.Pool, dispatcher *orchestrator.Dispatcher) (enqueueFn, func()) {
	if pool != nil {
		rq, err := riverq.New(pool, nil)
		if err != nil {
			fatal("river queue", err)
		}
		slog.Info("queue: river (postgres-backed); cadoo-worker consumes")
		enqueue := func(ctx context.Context, job orchestrator.ToolJob) error {
			return rq.EnqueueTool(ctx, riverq.ToolArgs{
				Provider:     string(job.Provider),
				Tool:         job.Tool,
				InstallID:    job.InstallID,
				RepoFullName: job.RepoFullName,
				PRNumber:     job.PRNumber,
				HeadSHA:      job.HeadSHA,
				Trigger:      job.Trigger,
				Args:         job.Args,
			})
		}
		return enqueue, func() {}
	}

	q := jobs.NewMemory()
	if dispatcher != nil {
		q.Register(orchestrator.ToolJob{}.Kind(), dispatcher)
	} else {
		q.Register("noop", jobs.HandlerFunc(func(_ context.Context, payload json.RawMessage) error {
			slog.Info("noop job (no VCS configured)", "payload", string(payload))
			return nil
		}))
	}
	go func() {
		if err := q.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("memory queue run", "err", err)
		}
	}()
	slog.Info("queue: in-memory (single-process dev mode)")
	enqueue := func(ctx context.Context, job orchestrator.ToolJob) error {
		return q.Enqueue(ctx, job)
	}
	return enqueue, func() {}
}

func buildDispatcher(s *settings.Settings, pool *pgxpool.Pool) (*orchestrator.Dispatcher, error) {
	pool2 := map[vcs.Kind]vcs.Provider{}
	if s.HasGitHub() {
		gh, err := cadoogh.New(cadoogh.Config{
			BaseURL:        s.GitHubBaseURL,
			UploadURL:      s.GitHubUploadURL,
			AppID:          s.GitHubAppID,
			InstallationID: s.GitHubDefaultInstallationID,
			PrivateKeyPEM:  s.GitHubAppPrivateKeyPEM,
		})
		if err != nil {
			return nil, err
		}
		pool2[vcs.KindGitHub] = gh
	}
	if s.HasGitLab() {
		gl, err := cadoogl.New(cadoogl.Config{
			BaseURL: s.GitLabBaseURL,
			Token:   s.GitLabToken,
		})
		if err != nil {
			return nil, err
		}
		pool2[vcs.KindGitLab] = gl
	}
	if len(pool2) == 0 {
		return nil, nil //nolint:nilnil
	}
	d := &orchestrator.Dispatcher{
		LLM:          litellm.New(s.LLMGatewayURL, s.LLMGatewayAPIKey),
		VCSPool:      pool2,
		Model:        s.DefaultModel,
		BaseCfg:      config.Default(),
		Registry:     orchestrator.DefaultRegistry(),
		ReportStatus: true,
	}
	if pool != nil {
		embedder := embed.New(s.LLMGatewayURL, s.LLMGatewayAPIKey, "")
		d.KB = kb.New(pool, embedder)
		d.Learnings = learnings.New(pool)
		d.Audit = audit.New(pool)
		d.Posted = findings.New(pool)
		d.KBDistiller = &querydistill.Distiller{LLM: d.LLM, Model: s.DefaultModel}
		slog.Info("knowledge layer enabled (kb + learnings + audit + posted-state + distiller)")
	} else {
		// No DB configured — fall back to an in-memory findings store so
		// dedup still works for the lifetime of this process. Optional
		// JSON file persists state across container restarts.
		d.Posted = findings.NewMemory(s.FindingsCacheFile)
		slog.Info("findings store: in-memory (no DB configured)", "persist_path", s.FindingsCacheFile)
	}
	d.Trackers = buildTrackers(s)
	if s.SlackWebhookURL != "" {
		d.Notifier = slack.New(s.SlackWebhookURL)
		slog.Info("slack notifier enabled")
	}
	if s.SandboxImage != "" {
		d.LinterRegistry = orchestrator.DefaultLintRegistry(s.SandboxImage)
		d.SandboxRunner = &sandbox.DockerRunner{DockerBin: s.SandboxDockerBin}
		slog.Info("sandboxed analysis enabled", "image", s.SandboxImage)
	}
	return d, nil
}

func buildTrackers(s *settings.Settings) []issuetrackers.Tracker {
	var out []issuetrackers.Tracker
	if s.JiraBaseURL != "" && s.JiraToken != "" {
		out = append(out, jira.New(s.JiraBaseURL, s.JiraEmail, s.JiraToken))
		slog.Info("jira tracker enabled")
	}
	if s.LinearAPIKey != "" {
		out = append(out, linear.New("", s.LinearAPIKey))
		slog.Info("linear tracker enabled")
	}
	return out
}

// --- GitHub webhook --------------------------------------------------------

func githubWebhookHandler(s *settings.Settings, enqueue enqueueFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if err := cadoogh.VerifySignature(s.GitHubWebhookSecret,
			r.Header.Get("X-Hub-Signature-256"), body); err != nil {
			slog.Warn("github signature rejected", "err", err)
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		eventType := r.Header.Get("X-GitHub-Event")
		event, err := cadoogh.ParseEvent(eventType, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch e := event.(type) {
		case *gogithub.PullRequestEvent:
			handleGithubPR(r.Context(), e, enqueue)
		case *gogithub.IssueCommentEvent:
			handleGithubComment(r.Context(), e, enqueue)
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

func handleGithubPR(ctx context.Context, e *gogithub.PullRequestEvent, enqueue enqueueFn) {
	action := e.GetAction()
	if action != "opened" && action != "synchronize" && action != "reopened" {
		return
	}
	cfg := config.Default() // TODO(phase-2.x): load .cadoo.yaml from PR head here
	repo := e.GetRepo().GetFullName()
	prNum := int64(e.GetPullRequest().GetNumber())
	head := e.GetPullRequest().GetHead().GetSHA()

	for tool, trigger := range cfg.Auto {
		if !triggerMatches(trigger, action) {
			continue
		}
		job := orchestrator.ToolJob{
			Provider:     vcs.KindGitHub,
			Tool:         tool,
			RepoFullName: repo,
			PRNumber:     prNum,
			HeadSHA:      head,
			Trigger:      action,
		}
		if err := enqueue(ctx, job); err != nil {
			slog.Error("enqueue auto-trigger", "err", err, "tool", tool)
			continue
		}
		slog.Info("enqueued auto-trigger", "provider", "github", "tool", tool, "trigger", action, "repo", repo, "pr", prNum)
	}
}

func handleGithubComment(ctx context.Context, e *gogithub.IssueCommentEvent, enqueue enqueueFn) {
	if e.GetIssue().GetPullRequestLinks() == nil || e.GetAction() != "created" {
		return
	}
	cmd, args := parseSlash(e.GetComment().GetBody())
	if cmd == "" {
		return
	}
	job := orchestrator.ToolJob{
		Provider:     vcs.KindGitHub,
		Tool:         cmd,
		RepoFullName: e.GetRepo().GetFullName(),
		PRNumber:     int64(e.GetIssue().GetNumber()),
		Trigger:      "command",
		Args:         args,
	}
	if err := enqueue(ctx, job); err != nil {
		slog.Error("enqueue command", "err", err, "tool", cmd)
		return
	}
	slog.Info("enqueued command", "provider", "github", "tool", cmd, "repo", job.RepoFullName, "pr", job.PRNumber)
}

// --- GitLab webhook --------------------------------------------------------

func gitlabWebhookHandler(s *settings.Settings, enqueue enqueueFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if err := cadoogl.VerifyToken(s.GitLabWebhookSecret, r.Header.Get("X-Gitlab-Token")); err != nil {
			slog.Warn("gitlab token rejected", "err", err)
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		eventType := r.Header.Get("X-Gitlab-Event")
		event, err := cadoogl.ParseEvent(eventType, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch e := event.(type) {
		case *glab.MergeEvent:
			handleGitlabMR(r.Context(), e, enqueue)
		case *glab.MergeCommentEvent:
			handleGitlabNote(r.Context(), e, enqueue)
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

func handleGitlabMR(ctx context.Context, e *glab.MergeEvent, enqueue enqueueFn) {
	action := mapGitlabMRAction(e.ObjectAttributes.Action)
	if action == "" {
		return
	}
	cfg := config.Default() // TODO(phase-2.x): load .cadoo.yaml from PR head here
	repo := e.Project.PathWithNamespace
	prNum := int64(e.ObjectAttributes.IID)
	head := e.ObjectAttributes.LastCommit.ID

	for tool, trigger := range cfg.Auto {
		if !triggerMatches(trigger, action) {
			continue
		}
		job := orchestrator.ToolJob{
			Provider:     vcs.KindGitLab,
			Tool:         tool,
			RepoFullName: repo,
			PRNumber:     prNum,
			HeadSHA:      head,
			Trigger:      action,
		}
		if err := enqueue(ctx, job); err != nil {
			slog.Error("enqueue auto-trigger", "err", err, "tool", tool)
			continue
		}
		slog.Info("enqueued auto-trigger", "provider", "gitlab", "tool", tool, "trigger", action, "repo", repo, "mr", prNum)
	}
}

func handleGitlabNote(ctx context.Context, e *glab.MergeCommentEvent, enqueue enqueueFn) {
	if e.MergeRequest.IID == 0 {
		return
	}
	cmd, args := parseSlash(e.ObjectAttributes.Note)
	if cmd == "" {
		return
	}
	job := orchestrator.ToolJob{
		Provider:     vcs.KindGitLab,
		Tool:         cmd,
		RepoFullName: e.Project.PathWithNamespace,
		PRNumber:     int64(e.MergeRequest.IID),
		Trigger:      "command",
		Args:         args,
	}
	if err := enqueue(ctx, job); err != nil {
		slog.Error("enqueue command", "err", err, "tool", cmd)
		return
	}
	slog.Info("enqueued command", "provider", "gitlab", "tool", cmd, "repo", job.RepoFullName, "mr", job.PRNumber)
}

func mapGitlabMRAction(glAction string) string {
	switch glAction {
	case "open":
		return "opened"
	case "reopen":
		return "reopened"
	case "update":
		return "synchronize"
	}
	return ""
}

// --- Shared ----------------------------------------------------------------

func triggerMatches(trigger, action string) bool {
	switch trigger {
	case "on_open":
		return action == "opened" || action == "reopened"
	case "on_sync":
		return action == "synchronize"
	case "always":
		return action == "opened" || action == "synchronize" || action == "reopened"
	case "never", "":
		return false
	}
	return false
}

func parseSlash(body string) (cmd, args string) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "/") {
		return "", ""
	}
	rest := body[1:]
	if i := strings.IndexAny(rest, " \t\n"); i >= 0 {
		return rest[:i], strings.TrimSpace(rest[i:])
	}
	return rest, ""
}

func fatal(stage string, err error) {
	slog.Error("fatal", "stage", stage, "err", err)
	os.Exit(1)
}
