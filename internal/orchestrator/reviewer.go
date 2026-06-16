// Package orchestrator wires the review pipeline: fetch PR → load per-PR
// config → build context → dispatch the right Tool from the Registry → post
// the Tool's Result to the VCS.
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
	"github.com/payamqorbanpour/cadoo/internal/analysis/workspace"
	"github.com/payamqorbanpour/cadoo/internal/audit"
	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/contextengine"
	"github.com/payamqorbanpour/cadoo/internal/findings"
	"github.com/payamqorbanpour/cadoo/internal/issuetrackers"
	"github.com/payamqorbanpour/cadoo/internal/kb"
	"github.com/payamqorbanpour/cadoo/internal/kb/querydistill"
	"github.com/payamqorbanpour/cadoo/internal/learnings"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/metrics"
	"github.com/payamqorbanpour/cadoo/internal/slop"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// ResultNotifier is an optional sink Cadoo posts each tool result to (e.g. a
// Slack incoming webhook). Failures are logged at Debug and never abort the
// dispatch.
type ResultNotifier interface {
	NotifyResult(ctx context.Context, pr *vcs.PullRequest, tool string, res *tools.Result) error
}

// prFileReader adapts a VCS FileFetcher into the tools.FileReader interface.
type prFileReader struct {
	ff   FileFetcher
	repo string
	ref  string
}

// ReadFile implements tools.FileReader / agent.FileReader.
func (r *prFileReader) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return r.ff.FetchFileFromRef(ctx, r.repo, r.ref, path)
}

// CheckRunName is the default check-run name. Tools may override by
// returning their own CheckRun in Result.
const CheckRunName = "cadoo"

// FileFetcher is an optional capability VCS adapters may implement so the
// orchestrator can read .cadoo.yaml from the PR head.
type FileFetcher interface {
	FetchFileFromRef(ctx context.Context, repo, ref, path string) ([]byte, error)
}

// ToolJob is the queue payload for a single tool invocation.
type ToolJob struct {
	Provider     vcs.Kind `json:"provider"` // empty defaults to KindGitHub
	Tool         string   `json:"tool"`
	InstallID    string   `json:"install_id"`
	RepoFullName string   `json:"repo_full_name"`
	PRNumber     int64    `json:"pr_number"`
	HeadSHA      string   `json:"head_sha"`
	Trigger      string   `json:"trigger"` // opened | synchronize | reopened | command
	Args         string   `json:"args,omitempty"`
}

// Kind implements jobs.Job.
func (ToolJob) Kind() string { return "tool" }

// Dispatcher runs ToolJobs by picking the right VCS adapter from VCSPool and
// the right Tool from Registry.
type Dispatcher struct {
	LLM      llm.Provider
	VCSPool  map[vcs.Kind]vcs.Provider
	Model    string
	BaseCfg  config.Repo
	Registry *tools.Registry

	// Optional knowledge layer. Both stores may be nil — the dispatcher
	// only queries them when configured.
	KB        *kb.Store
	Learnings *learnings.Store

	// Optional issue trackers (Jira, Linear, ...) consulted in order.
	Trackers []issuetrackers.Tracker

	// Optional result notifier (e.g. Slack incoming webhook).
	Notifier ResultNotifier

	// Optional audit logger; writes one row per dispatch when non-nil.
	Audit *audit.Logger

	// Optional static-analysis layer. Both must be non-nil to enable
	// sandboxed lint dispatch on every run.
	LinterRegistry *analysis.Registry
	SandboxRunner  sandbox.Runner

	// Optional posted-state store. When non-nil, the dispatcher dedupes
	// inline comments by fingerprint and edits summary comments in place
	// instead of creating fresh ones on every resync.
	Posted *findings.Store

	// Optional KB query distiller. When non-nil, the dispatcher rewrites
	// the PR title+body into a focused retrieval query before embedding,
	// trading one cheap LLM call for higher KB recall.
	KBDistiller *querydistill.Distiller

	// ReportStatus controls whether tool-emitted CheckRuns are posted to
	// the VCS as commit statuses / check-runs. Zero value (false) suppresses
	// them — the dispatcher still posts the inline review and summary, but
	// nothing lands on the head SHA that a branch-protection rule could
	// require. Server callers (webhook/worker) opt in explicitly to keep
	// today's SaaS behavior.
	ReportStatus bool

	MaxTokens     int
	PerFileTokens int

	// summaryMu serializes the read-SummaryID → write-comment → store-ID
	// sequence inside postSummary. Without this, concurrent tool goroutines
	// (CI parallel dispatch) race to create duplicate consolidated comments.
	summaryMu sync.Mutex
}

// Handle is the jobs.Handler entry point.
func (d *Dispatcher) Handle(ctx context.Context, payload json.RawMessage) error {
	var job ToolJob
	if err := json.Unmarshal(payload, &job); err != nil {
		return fmt.Errorf("decode tool job: %w", err)
	}
	return d.Run(ctx, job)
}

// Run executes one ToolJob.
func (d *Dispatcher) Run(ctx context.Context, job ToolJob) (retErr error) {
	if job.Tool == "" {
		job.Tool = "review"
	}
	if job.Provider == "" {
		job.Provider = vcs.KindGitHub
	}
	start := time.Now()
	defer func() {
		outcome := "success"
		if retErr != nil {
			outcome = "failure"
		}
		metrics.DispatchTotal.WithLabelValues(job.Tool, string(job.Provider), outcome).Inc()
		metrics.DispatchDuration.WithLabelValues(job.Tool, string(job.Provider)).Observe(time.Since(start).Seconds())
		_ = d.Audit.Record(ctx, "", "system", "tool.dispatch",
			fmt.Sprintf("%s:%s#%d", job.Provider, job.RepoFullName, job.PRNumber),
			map[string]any{
				"tool":    job.Tool,
				"trigger": job.Trigger,
				"outcome": outcome,
			})
	}()
	provider, ok := d.VCSPool[job.Provider]
	if !ok {
		return fmt.Errorf("no adapter for provider %q (configured: %v)",
			job.Provider, configuredKinds(d.VCSPool))
	}
	tool, ok := d.Registry.Get(job.Tool)
	if !ok {
		return fmt.Errorf("unknown tool %q (registered: %v)", job.Tool, d.Registry.Names())
	}

	pr, err := provider.FetchPullRequest(ctx, job.RepoFullName, job.PRNumber)
	if err != nil {
		return fmt.Errorf("fetch pr: %w", err)
	}

	cfg := d.loadCfg(ctx, provider, pr)

	files, err := provider.ListChangedFiles(ctx, pr)
	if err != nil {
		d.failCheck(ctx, provider, pr, err)
		return fmt.Errorf("list files: %w", err)
	}

	packed := contextengine.Compress(files, contextengine.CompressOptions{
		MaxTokens:    d.maxTokens(),
		PerFileMax:   d.perFileTokens(),
		IncludePaths: cfg.Review.IncludePaths,
		ExcludePaths: cfg.Review.ExcludePaths,
	})

	model := d.modelName(cfg)
	in := tools.Input{
		PR:     pr,
		Files:  files,
		Packed: packed,
		Config: cfg,
		LLM:    d.LLM,
		Model:  model,
		Args:   job.Args,
	}
	if ff, ok := provider.(FileFetcher); ok && pr.HeadSHA != "" {
		in.Reader = &prFileReader{ff: ff, repo: pr.RepoFullName, ref: pr.HeadSHA}
	}
	totalAdd, totalDel := 0, 0
	for _, f := range files {
		totalAdd += f.Additions
		totalDel += f.Deletions
	}
	report := slop.Detect(pr.Title, pr.Body, totalAdd, totalDel, len(files))
	in.Slop = &report

	in.Analysis = d.runLinters(ctx, provider, pr, files)

	for _, tracker := range d.Trackers {
		issues, err := tracker.FindLinked(ctx, pr.Title, pr.Body)
		if err != nil {
			slog.Debug("tracker lookup", "tracker", tracker.Name(), "err", err)
			continue
		}
		in.Issues = append(in.Issues, issues...)
	}

	repoKey := string(job.Provider) + ":" + pr.RepoFullName
	in.RepoKey = repoKey
	in.LearningsStore = d.Learnings
	if d.Posted != nil {
		priorKey := findings.PRKey{
			Provider:     string(job.Provider),
			RepoFullName: pr.RepoFullName,
			PRNumber:     pr.Number,
		}
		if posted, err := d.Posted.ListPostedFindings(ctx, priorKey); err == nil {
			in.PriorFindings = make([]tools.PriorFinding, 0, len(posted))
			for _, p := range posted {
				in.PriorFindings = append(in.PriorFindings, tools.PriorFinding{
					Tool:      p.Tool,
					File:      p.File,
					LineStart: p.LineStart,
					LineEnd:   p.LineEnd,
					Severity:  p.Severity,
					Title:     p.Title,
				})
			}
		} else {
			slog.Debug("list prior findings", "err", err)
		}
	}
	if d.Learnings != nil {
		if rules, err := d.Learnings.Active(ctx, repoKey, 10, 0.6); err == nil {
			in.Learnings = rules
		} else {
			slog.Debug("load learnings", "err", err, "repo", repoKey)
		}
	}
	if d.KB != nil {
		var query string
		if d.KBDistiller != nil {
			query = d.KBDistiller.Distill(ctx, pr.Title, pr.Body)
		} else {
			query = pr.Title
			if pr.Body != "" {
				query += "\n\n" + pr.Body
			}
		}
		if hits, err := d.KB.Search(ctx, repoKey, query, 5); err == nil {
			in.KBHits = hits
		} else {
			slog.Debug("kb search", "err", err, "repo", repoKey)
		}
	}

	// Incremental dispatch block (Part C, T-08-C4 / T-08-C5): when the
	// Posted store holds a LastReviewedSHA and the provider implements
	// DiffBetweener, narrow the inline review to the delta.
	//
	// Guard: sha must be non-empty AND not the current head — equal SHA
	// is the fixed-point fast-path that skips the provider call entirely
	// and produces an empty incremental set (T-08-C4).
	var inChangeSet map[string]struct{}
	isIncrementalRun := false
	if sha := d.Posted.LastReviewedSHA(); sha != "" && sha != pr.HeadSHA {
		if db, ok := provider.(vcs.DiffBetweener); ok {
			incr, err := db.DiffBetween(ctx, pr.RepoFullName, sha, pr.HeadSHA)
			switch {
			case err != nil || incr == nil:
				// Non-ancestor SHA, force-push, or provider error → full-review
				// fallback: leave Files/Packed full, IsIncrementalRun=false (T-08-C5).
			case len(incr) == 0:
				// Empty diff: unchanged head. Inline tools have nothing to review;
				// resolveStalePriors sees an empty changeSet and resolves nothing.
				in.IsIncrementalRun = true
				isIncrementalRun = true
				inChangeSet = map[string]struct{}{}
			default:
				// Non-empty incremental diff: populate incremental context fields.
				in.IncrementalFiles = incr
				in.IncrementalPacked = contextengine.Compress(incr, contextengine.CompressOptions{
					MaxTokens:    d.maxTokens(),
					PerFileMax:   d.perFileTokens(),
					IncludePaths: cfg.Review.IncludePaths,
					ExcludePaths: cfg.Review.ExcludePaths,
				})
				in.IsIncrementalRun = true
				isIncrementalRun = true
				inChangeSet = fileSet(incr)
			}
		}
	} else if sha := d.Posted.LastReviewedSHA(); sha != "" && sha == pr.HeadSHA {
		// Same-commit fast-path: sha == head → empty incremental set without
		// calling DiffBetween (T-08-C4 DoS guard).
		in.IsIncrementalRun = true
		isIncrementalRun = true
		inChangeSet = map[string]struct{}{}
	}

	res, err := tool.Run(ctx, in)
	if err != nil {
		d.failCheck(ctx, provider, pr, err)
		return fmt.Errorf("run tool %q: %w", job.Tool, err)
	}
	if err := d.applyResult(ctx, provider, pr, job.Tool, res, inChangeSet, isIncrementalRun); err != nil {
		return err
	}
	if d.Notifier != nil {
		if err := d.Notifier.NotifyResult(ctx, pr, job.Tool, res); err != nil {
			slog.Debug("notify result", "err", err)
		}
	}
	return nil
}

// applyResult posts everything the tool returned. Failures on individual
// posts are logged but don't abort — partial output is still valuable.
//
// When d.Posted is configured, every tool's summary becomes a labelled
// section inside one consolidated PR comment (instead of one comment per
// tool), and inline comments whose fingerprints have already been posted
// are skipped. Passing tool == "" disables idempotency for this call.
func (d *Dispatcher) applyResult(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, tool string, res *tools.Result, changeSet map[string]struct{}, incrementalRun bool) error {
	if res == nil {
		return nil
	}
	key := findings.PRKey{
		Provider:     string(provider.Kind()),
		RepoFullName: pr.RepoFullName,
		PRNumber:     pr.Number,
	}
	if res.Summary != "" {
		d.postSummary(ctx, provider, pr, key, tool, res.Summary)
	}
	if len(res.InlineComments) > 0 {
		d.postInline(ctx, provider, pr, key, tool, res.InlineComments, changeSet, incrementalRun)
	}
	if d.ReportStatus {
		if res.CheckRun != nil {
			if err := provider.UpsertCheckRun(ctx, pr, *res.CheckRun); err != nil {
				slog.Error("upsert check run", "err", err, "pr", pr.URL)
			}
		}
		for _, run := range res.CheckRuns {
			if err := provider.UpsertCheckRun(ctx, pr, run); err != nil {
				slog.Error("upsert extra check run", "err", err, "name", run.Name, "pr", pr.URL)
			}
		}
	}
	if res.EditPRBody != nil {
		if err := d.applyPRBody(ctx, provider, pr, *res.EditPRBody); err != nil {
			slog.Error("edit pr body", "err", err, "pr", pr.URL)
		}
	}
	return nil
}

// postSummary turns this tool's body into a section inside the consolidated
// Cadoo comment. When d.Posted is unavailable, falls back to the legacy
// per-tool comment (so the dispatcher still works for callers — e.g. cadoo
// ci — that don't carry a Postgres pool).
func (d *Dispatcher) postSummary(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, key findings.PRKey, tool, body string) {
	if tool == "" || d.Posted == nil || !d.Posted.Enabled() {
		// Legacy path: one comment per call, no consolidation.
		if _, err := provider.PostSummaryComment(ctx, pr, body); err != nil {
			slog.Error("post summary", "err", err, "pr", pr.URL)
		}
		return
	}

	if err := d.Posted.PutSection(ctx, key, tool, body); err != nil {
		slog.Debug("put section", "err", err)
	}

	d.summaryMu.Lock()
	defer d.summaryMu.Unlock()

	sections, err := d.Posted.AllSections(ctx, key)
	if err != nil {
		slog.Debug("all sections", "err", err)
		sections = []findings.Section{{Tool: tool, Body: body}}
	}
	rendered := renderConsolidated(sections, pr.HeadSHA)

	existing, err := d.Posted.SummaryID(ctx, key, findings.WrapperToolKey)
	if err == nil && existing != "" {
		if err := provider.UpdateSummaryComment(ctx, pr, existing, rendered); err == nil {
			return
		}
		// Fall through to create-new on edit failure (comment may have been
		// deleted upstream).
	}
	id, err := provider.PostSummaryComment(ctx, pr, rendered)
	if err != nil {
		slog.Error("post summary", "err", err, "pr", pr.URL)
		return
	}
	if id != "" {
		_ = d.Posted.PutSummaryID(ctx, key, findings.WrapperToolKey, id)
	}
}

// applyPRBody splices a Cadoo-managed section into the PR description while
// preserving whatever the user originally wrote. Idempotent on re-dispatch.
func (d *Dispatcher) applyPRBody(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, section string) error {
	if strings.TrimSpace(section) == "" {
		return nil
	}
	newBody := spliceCadooBody(pr.Body, section)
	if newBody == pr.Body {
		return nil
	}
	return provider.EditPullRequestBody(ctx, pr, newBody)
}

func (d *Dispatcher) postInline(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, key findings.PRKey, tool string, comments []vcs.InlineComment, changeSet map[string]struct{}, incrementalRun bool) {
	// Snapshot prior findings before we filter — we need them to compute
	// which threads went stale on this run.
	var prior []findings.PostedFinding
	if tool != "" && d.Posted != nil {
		prior, _ = d.Posted.ListPostedFindings(ctx, key)
	}

	// Two-stage filter: (a) drop comments the Posted store already knows
	// about (cross-dispatch dedup), (b) drop comments whose StructuralKey
	// already appeared earlier in this same batch (intra-dispatch dedup).
	// Stage (b) runs even when Posted is unset — the CLI path has no DB,
	// and without it two near-duplicate suggestions from one improve run
	// would both slip through HasFinding (neither has been recorded yet).
	delta := comments
	if tool != "" {
		delta = make([]vcs.InlineComment, 0, len(comments))
		seenKeys := make(map[string]struct{}, len(comments))
		for _, c := range comments {
			if d.Posted != nil {
				has, err := d.Posted.HasFinding(ctx, key, tool, c)
				if err != nil {
					slog.Debug("dedup lookup", "err", err)
				}
				if has {
					continue
				}
			}
			sk := findings.StructuralKey(tool, c)
			if _, dup := seenKeys[sk]; dup {
				continue
			}
			seenKeys[sk] = struct{}{}
			delta = append(delta, c)
		}
	}

	if len(delta) > 0 {
		// Stamp a separate wire copy with the hidden dedup marker. The
		// original delta entries stay pristine — they are what we hash for
		// keys and what we record, so the marker can never poison
		// StructuralKey/Fingerprint.
		wire := make([]vcs.InlineComment, len(delta))
		for i, c := range delta {
			wc := c
			if tool != "" {
				wc.Body = findings.StampInline(tool, c)
			}
			wire[i] = wc
		}
		refs, err := provider.PostInlineComments(ctx, pr, wire)
		if err != nil {
			slog.Error("post inline review", "err", err, "pr", pr.URL)
			// Fall through: refs may still hold the partial set the adapter
			// managed to post (especially the per-comment GitLab path).
		}
		if tool != "" && d.Posted != nil {
			// Adapters return exactly one ref per input comment, in input
			// order. Pair each ref with its PRISTINE delta entry by index.
			for i := range refs {
				if i >= len(delta) {
					break
				}
				_ = d.Posted.RecordFinding(ctx, key, tool, refs[i].ExternalID, delta[i])
			}
		}
	}

	// Auto-resolve threads whose finding the model didn't repeat this
	// run. Only acts on this tool's own priors so /describe doesn't
	// resolve /review threads (or vice versa). Skips priors with no
	// external ID — we have nothing to resolve against.
	d.resolveStalePriors(ctx, provider, pr, tool, prior, comments, changeSet, incrementalRun)
}

// resolveStalePriors walks prior findings for the given tool, computes
// whether each one is still in the current run's output (by structural
// key, which is line-agnostic), and asks the provider to resolve the
// thread for any prior that's gone missing. Best-effort: errors are logged
// and the loop continues so one flaky resolve doesn't stop the rest.
//
// When incrementalRun is true and changeSet is non-empty, only priors whose
// anchor File is in the change set are eligible for resolution — priors on
// untouched files persist as open threads (SPEC critical interaction, T-08-C6).
func (d *Dispatcher) resolveStalePriors(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, tool string, prior []findings.PostedFinding, current []vcs.InlineComment, changeSet map[string]struct{}, incrementalRun bool) {
	if tool == "" || len(prior) == 0 {
		return
	}
	currentKeys := make(map[string]struct{}, len(current))
	for _, c := range current {
		currentKeys[findings.StructuralKey(tool, c)] = struct{}{}
	}
	for _, p := range prior {
		if p.Tool != tool || p.ExternalCommentID == "" {
			continue
		}
		// Incremental change-set gate: when running in incremental mode, skip
		// priors on files that were NOT changed since the last reviewed SHA.
		// This preserves threads on untouched code instead of resolving them
		// just because the inline tool received a narrower file set (T-08-C6).
		if incrementalRun && len(changeSet) > 0 {
			if _, changed := changeSet[p.File]; !changed {
				continue
			}
		}
		// Compare the carried StructuralKey directly when available.
		// Legacy records (written before StructuralKey was threaded into
		// PostedFinding) have an empty field: fall back to the first-line
		// recompute so they are not all mass-resolved on the first run
		// after deploy (Pitfall-1 guard, T-08-A1).
		var pkey string
		if p.StructuralKey != "" {
			pkey = p.StructuralKey
		} else {
			pkey = findings.StructuralKey(p.Tool, vcs.InlineComment{
				File:     p.File,
				Severity: vcs.Severity(p.Severity),
				Body:     p.Title,
			})
		}
		if _, present := currentKeys[pkey]; present {
			continue
		}
		if err := provider.ResolveThread(ctx, pr, p.ExternalCommentID); err != nil {
			slog.Debug("resolve stale thread", "err", err, "pr", pr.URL, "thread", p.ExternalCommentID)
		}
	}
}

// loadCfg reads .cadoo.yaml from the PR head if the VCS adapter supports it,
// otherwise returns d.BaseCfg.
func (d *Dispatcher) loadCfg(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest) config.Repo {
	ff, ok := provider.(FileFetcher)
	if !ok || pr.HeadSHA == "" {
		return d.BaseCfg
	}
	raw, err := ff.FetchFileFromRef(ctx, pr.RepoFullName, pr.HeadSHA, ".cadoo.yaml")
	if err != nil {
		if !isMissingFile(err) {
			slog.Debug("load .cadoo.yaml failed; using base config", "err", err)
		}
		return d.BaseCfg
	}
	cfg, err := config.Parse(raw)
	if err != nil {
		slog.Warn("parse .cadoo.yaml; using base config", "err", err)
		return d.BaseCfg
	}
	// Load the optional sibling .cadoo.md free-form review brief from the same
	// head SHA. Missing or unreadable is non-fatal — the brief is additive.
	if mdRaw, mdErr := ff.FetchFileFromRef(ctx, pr.RepoFullName, pr.HeadSHA, config.MarkdownFilename); mdErr == nil {
		cfg.Markdown = strings.TrimSpace(string(mdRaw))
	} else if !isMissingFile(mdErr) {
		slog.Debug("load .cadoo.md failed; continuing without brief", "err", mdErr)
	}
	return cfg
}

func isMissingFile(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	// VCS clients wrap 404s in their own error types; we don't import them
	// here to keep this package VCS-agnostic. Match by string as a soft
	// heuristic.
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}

// modelName resolves which LLM model this dispatch should use. Per-repo
// `model:` in .cadoo.yaml wins; otherwise CADOO_DEFAULT_MODEL (read into
// d.Model) is used. If neither is set we pass an empty string through to
// the gateway and let it decide (LiteLLM, for example, can route by a
// default route configured on its side).
func (d *Dispatcher) modelName(cfg config.Repo) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	return d.Model
}

func (d *Dispatcher) maxTokens() int {
	if d.MaxTokens > 0 {
		return d.MaxTokens
	}
	return 50_000
}

func (d *Dispatcher) perFileTokens() int {
	if d.PerFileTokens > 0 {
		return d.PerFileTokens
	}
	return 8_000
}

func (d *Dispatcher) failCheck(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, err error) {
	if !d.ReportStatus {
		return
	}
	_ = provider.UpsertCheckRun(ctx, pr, vcs.CheckRun{
		Name:    CheckRunName,
		Status:  vcs.CheckFailed,
		Title:   "Cadoo failed",
		Summary: err.Error(),
	})
}

// runLinters materializes the PR head as a temp workspace, picks the
// linters relevant to each changed file's extension, runs them in parallel,
// and merges all findings. Returns nil (no error) when the analysis layer
// isn't configured or the VCS adapter doesn't support archive fetch.
func (d *Dispatcher) runLinters(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, files []vcs.FileChange) []analysis.Finding {
	if d.LinterRegistry == nil || d.SandboxRunner == nil {
		return nil
	}
	archiver, ok := provider.(workspace.RepoArchiver)
	if !ok || pr.HeadSHA == "" {
		return nil
	}
	ws, err := workspace.Open(ctx, archiver, pr.RepoFullName, pr.HeadSHA)
	if err != nil {
		slog.Debug("workspace open failed; skipping lint", "err", err)
		return nil
	}
	defer func() { _ = ws.Close() }()

	byExt := map[string][]string{}
	for _, f := range files {
		if f.IsBinary || f.Status == "removed" {
			continue
		}
		ext := filepath.Ext(f.Path)
		if ext == "" {
			continue
		}
		byExt[ext] = append(byExt[ext], f.Path)
	}

	wsSpec := analysis.Workspace{HostPath: ws.Dir, ContainerPath: "/workspace"}

	var (
		mu  sync.Mutex
		all []analysis.Finding
		wg  sync.WaitGroup
	)
	for ext, paths := range byExt {
		for _, linter := range d.LinterRegistry.ForExt(ext) {
			wg.Add(1)
			go func(l analysis.Linter, paths []string) {
				defer wg.Done()
				findings, err := l.Run(ctx, d.SandboxRunner, wsSpec, paths)
				if err != nil {
					slog.Debug("linter failed", "linter", l.Name(), "err", err)
					return
				}
				mu.Lock()
				all = append(all, findings...)
				mu.Unlock()
			}(linter, paths)
		}
	}
	wg.Wait()
	return all
}

// fileSet builds a map of file paths for O(1) membership tests. Used to
// determine which files are in the incremental change set so resolveStalePriors
// can skip priors on untouched files.
func fileSet(files []vcs.FileChange) map[string]struct{} {
	out := make(map[string]struct{}, len(files))
	for _, f := range files {
		out[f.Path] = struct{}{}
	}
	return out
}

func configuredKinds(pool map[vcs.Kind]vcs.Provider) []vcs.Kind {
	out := make([]vcs.Kind, 0, len(pool))
	for k := range pool {
		out = append(out, k)
	}
	return out
}
