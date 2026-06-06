// cadoo-worker subscribes to the job queue and runs the dispatcher.
//
// Queue selection mirrors cadoo-webhook:
//   - DATABASE_URL set → consume from River (Postgres). KB + learnings stores
//     are also constructed and attached to the dispatcher.
//   - DATABASE_URL empty → degenerate noop loop; for dev use cadoo-webhook
//     which runs an in-process consumer instead.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
	"github.com/payamqorbanpour/cadoo/internal/audit"
	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/db"
	"github.com/payamqorbanpour/cadoo/internal/findings"
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
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/defaults"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/state"
	"github.com/payamqorbanpour/cadoo/internal/reports"
	"github.com/payamqorbanpour/cadoo/internal/riverq"
	"github.com/payamqorbanpour/cadoo/internal/settings"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
	cadoogh "github.com/payamqorbanpour/cadoo/internal/vcs/github"
	cadoogl "github.com/payamqorbanpour/cadoo/internal/vcs/gitlab"
)

func main() {
	s, err := settings.FromEnv()
	if err != nil {
		slog.Error("settings", "err", err)
		os.Exit(1)
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
		if err := riverq.Migrate(ctx, pool); err != nil {
			slog.Error("river migrate", "err", err)
			os.Exit(1)
		}
	}

	dispatcher, err := buildDispatcher(s, pool)
	if err != nil {
		slog.Error("dispatcher", "err", err)
		os.Exit(1)
	}

	releaseDispatcher := buildReleaseDispatcher(s, pool)

	if pool != nil {
		go startReporter(ctx, s, pool, dispatcher)
		runRiver(ctx, pool, dispatcher, releaseDispatcher)
		return
	}
	runMemory(ctx, dispatcher, releaseDispatcher)
}

func startReporter(ctx context.Context, s *settings.Settings, pool *pgxpool.Pool, dispatcher *orchestrator.Dispatcher) {
	if s.ReportsInterval == "" {
		return
	}
	d, err := time.ParseDuration(s.ReportsInterval)
	if err != nil {
		slog.Warn("reports interval parse", "err", err)
		return
	}
	r := &reports.Reporter{
		Audit:    audit.New(pool),
		Notifier: dispatcher.Notifier,
		Interval: d,
	}
	slog.Info("scheduled reporter started", "interval", d)
	if err := r.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("reporter exited", "err", err)
	}
}

func runRiver(ctx context.Context, pool *pgxpool.Pool, dispatcher *orchestrator.Dispatcher, releaseDispatcher *releasedocs.Dispatcher) {
	if dispatcher == nil {
		slog.Error("worker requires at least one VCS configured (GitHub App or GitLab token) when DATABASE_URL is set")
		os.Exit(1)
	}
	q, err := riverq.New(pool, dispatcher, releaseDispatcher)
	if err != nil {
		slog.Error("river queue", "err", err)
		os.Exit(1)
	}
	slog.Info("worker started (river)")
	if err := q.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("worker exited", "err", err)
		os.Exit(1)
	}
}

func runMemory(ctx context.Context, dispatcher *orchestrator.Dispatcher, releaseDispatcher *releasedocs.Dispatcher) {
	q := jobs.NewMemory()
	if dispatcher != nil {
		q.Register(orchestrator.ToolJob{}.Kind(), dispatcher)
	} else {
		q.Register("noop", jobs.HandlerFunc(func(_ context.Context, payload json.RawMessage) error {
			slog.Info("noop job (no VCS configured)", "payload", string(payload))
			return nil
		}))
	}
	// Register release-docs handler when a release dispatcher is available.
	// When no VCS is configured, releaseDispatcher is nil so we leave the kind
	// unregistered (single-process dev with no VCS yields noop behaviour).
	if releaseDispatcher != nil {
		q.Register(releasedocs.ReleaseJob{}.Kind(), jobs.HandlerFunc(func(ctx context.Context, payload json.RawMessage) error {
			var j releasedocs.ReleaseJob
			if err := json.Unmarshal(payload, &j); err != nil {
				return fmt.Errorf("decode release job: %w", err)
			}
			return releaseDispatcher.Run(ctx, j)
		}))
	}
	slog.Info("worker started (in-memory; cross-process queue requires DATABASE_URL)")
	if err := q.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("worker exited", "err", err)
		os.Exit(1)
	}
}

func buildDispatcher(s *settings.Settings, pool *pgxpool.Pool) (*orchestrator.Dispatcher, error) {
	vcspool := map[vcs.Kind]vcs.Provider{}
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
		vcspool[vcs.KindGitHub] = gh
	}
	if s.HasGitLab() {
		gl, err := cadoogl.New(cadoogl.Config{
			BaseURL: s.GitLabBaseURL,
			Token:   s.GitLabToken,
		})
		if err != nil {
			return nil, err
		}
		vcspool[vcs.KindGitLab] = gl
	}
	if len(vcspool) == 0 {
		return nil, nil //nolint:nilnil
	}
	d := &orchestrator.Dispatcher{
		LLM:          litellm.New(s.LLMGatewayURL, s.LLMGatewayAPIKey),
		VCSPool:      vcspool,
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
		// dedup still works for the lifetime of this process. We default
		// to a JSON file under the user's cache dir so the store survives
		// container restarts even when the operator hasn't set
		// CADOO_FINDINGS_CACHE_FILE explicitly; without persistence each
		// restart re-posts everything.
		path := s.FindingsCacheFile
		if path == "" {
			path = findings.DefaultCachePath()
		}
		d.Posted = findings.NewMemory(path)
		slog.Info("findings store: in-memory (no DB configured)", "persist_path", path)
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

// buildReleaseDispatcher constructs a releasedocs.Dispatcher using the same
// VCS pool as buildDispatcher. Returns nil when no VCS adapter is configured
// so that callers can treat a nil dispatcher as "feature not available".
// When pool is non-nil a DB-backed state.Store is attached for cross-restart
// idempotency (REQ-release-docs-idempotency); otherwise Store is left nil
// (stateless marker mode, D-14).
func buildReleaseDispatcher(s *settings.Settings, pool *pgxpool.Pool) *releasedocs.Dispatcher {
	vcspool := map[vcs.Kind]vcs.Provider{}
	if s.HasGitHub() {
		gh, err := cadoogh.New(cadoogh.Config{
			BaseURL:        s.GitHubBaseURL,
			UploadURL:      s.GitHubUploadURL,
			AppID:          s.GitHubAppID,
			InstallationID: s.GitHubDefaultInstallationID,
			PrivateKeyPEM:  s.GitHubAppPrivateKeyPEM,
		})
		if err != nil {
			slog.Warn("releasedocs: github adapter failed; skipping", "err", err)
		} else {
			vcspool[vcs.KindGitHub] = gh
		}
	}
	if s.HasGitLab() {
		gl, err := cadoogl.New(cadoogl.Config{
			BaseURL: s.GitLabBaseURL,
			Token:   s.GitLabToken,
		})
		if err != nil {
			slog.Warn("releasedocs: gitlab adapter failed; skipping", "err", err)
		} else {
			vcspool[vcs.KindGitLab] = gl
		}
	}
	if len(vcspool) == 0 {
		return nil
	}
	d := &releasedocs.Dispatcher{
		VCSPool:    vcspool,
		LLM:        litellm.New(s.LLMGatewayURL, s.LLMGatewayAPIKey),
		Model:      s.DefaultModel,
		BaseCfg:    config.Default(),
		Generators: defaults.DefaultGenerators(),
		Publishers: defaults.DefaultPublishers(),
	}
	if pool != nil {
		d.Store = state.New(pool)
		slog.Info("releasedocs dispatcher enabled (DB-backed state)")
	} else {
		slog.Info("releasedocs dispatcher enabled (stateless marker mode)")
	}
	return d
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
