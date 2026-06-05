// Package riverq is the Postgres-backed job queue. Cadoo uses an in-memory
// queue for single-process dev (internal/jobs) and switches to River when a
// DATABASE_URL is set so cadoo-webhook and cadoo-worker can run as
// independent deployments.
package riverq

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/payamqorbanpour/cadoo/internal/orchestrator"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// ToolArgs is River's typed payload for orchestrator.ToolJob.
type ToolArgs struct {
	Provider     string `json:"provider"`
	Tool         string `json:"tool"`
	InstallID    string `json:"install_id"`
	RepoFullName string `json:"repo_full_name"`
	PRNumber     int64  `json:"pr_number"`
	HeadSHA      string `json:"head_sha"`
	Trigger      string `json:"trigger"`
	Args         string `json:"args,omitempty"`
}

// Kind identifies this job type to River.
func (ToolArgs) Kind() string { return "tool" }

type toolWorker struct {
	river.WorkerDefaults[ToolArgs]
	dispatcher *orchestrator.Dispatcher
}

func (w *toolWorker) Work(ctx context.Context, j *river.Job[ToolArgs]) error {
	return w.dispatcher.Run(ctx, orchestrator.ToolJob{
		Provider:     vcs.Kind(j.Args.Provider),
		Tool:         j.Args.Tool,
		InstallID:    j.Args.InstallID,
		RepoFullName: j.Args.RepoFullName,
		PRNumber:     j.Args.PRNumber,
		HeadSHA:      j.Args.HeadSHA,
		Trigger:      j.Args.Trigger,
		Args:         j.Args.Args,
	})
}

// ReleaseArgs is River's typed payload for releasedocs.ReleaseJob.
// It mirrors the ToolArgs/toolWorker pattern for the release-docs subsystem.
type ReleaseArgs struct {
	// Provider identifies the VCS kind (github, github_enterprise, gitlab).
	Provider string `json:"provider"`
	// Repo is the full repository name (e.g. "owner/repo").
	Repo string `json:"repo"`
	// Org is the Cadoo organisation ID for multi-tenancy.
	Org string `json:"org"`
	// FromRef is the prior release tag or commit SHA used as the range start.
	// May be empty; the releasedocs dispatcher resolves it via LatestTagBefore.
	FromRef string `json:"from_ref"`
	// ToRef is the new release tag that triggered this job.
	ToRef string `json:"to_ref"`
}

// Kind identifies this job type to River.
func (ReleaseArgs) Kind() string { return "release_docs" }

type releaseWorker struct {
	river.WorkerDefaults[ReleaseArgs]
	dispatcher *releasedocs.Dispatcher
}

func (w *releaseWorker) Work(ctx context.Context, j *river.Job[ReleaseArgs]) error {
	return w.dispatcher.Run(ctx, releasedocs.ReleaseJob{
		Provider: vcs.Kind(j.Args.Provider),
		Repo:     j.Args.Repo,
		Org:      j.Args.Org,
		FromRef:  j.Args.FromRef,
		ToRef:    j.Args.ToRef,
	})
}

// Queue wraps a *river.Client.
type Queue struct {
	pool   *pgxpool.Pool
	client *river.Client[pgx.Tx]
}

// New constructs a Queue against pool. Pass dispatcher != nil on the worker
// process to register tool job handlers; pass releaseDispatcher != nil to also
// register the release-docs worker. Both may be nil on a webhook-only process
// that only enqueues. Both workers must be registered before the River client
// is created (Pitfall 4 in 02-RESEARCH.md).
func New(pool *pgxpool.Pool, dispatcher *orchestrator.Dispatcher, releaseDispatcher *releasedocs.Dispatcher) (*Queue, error) {
	cfg := &river.Config{}
	if dispatcher != nil || releaseDispatcher != nil {
		workers := river.NewWorkers()
		if dispatcher != nil {
			river.AddWorker(workers, &toolWorker{dispatcher: dispatcher})
		}
		if releaseDispatcher != nil {
			river.AddWorker(workers, &releaseWorker{dispatcher: releaseDispatcher})
		}
		cfg.Workers = workers
		cfg.Queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 4},
		}
	}
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	return &Queue{pool: pool, client: client}, nil
}

// Migrate applies River's schema migrations to the supplied pool.
// Safe to call repeatedly; River's migrator is idempotent.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migrate up: %w", err)
	}
	return nil
}

// EnqueueTool inserts a tool job.
func (q *Queue) EnqueueTool(ctx context.Context, args ToolArgs) error {
	_, err := q.client.Insert(ctx, args, nil)
	return err
}

// EnqueueRelease inserts a release-docs job. Mirrors EnqueueTool for the
// release-docs subsystem.
func (q *Queue) EnqueueRelease(ctx context.Context, args ReleaseArgs) error {
	_, err := q.client.Insert(ctx, args, nil)
	return err
}

// Start runs the consumer loop (only meaningful if dispatcher was non-nil at
// construction). Blocks until ctx cancels or an unrecoverable error occurs.
func (q *Queue) Start(ctx context.Context) error {
	return q.client.Start(ctx)
}

// Stop drains in-flight jobs cleanly.
func (q *Queue) Stop(ctx context.Context) error {
	return q.client.Stop(ctx)
}
