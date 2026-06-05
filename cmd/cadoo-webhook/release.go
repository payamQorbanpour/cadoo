package main

import (
	"context"
	"log/slog"
	"path"
	"strings"

	gogithub "github.com/google/go-github/v66/github"
	glab "gitlab.com/gitlab-org/api/client-go"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// enqueueReleaseFn is the typed enqueue function for releasedocs.ReleaseJob.
// It is separate from enqueueFn (which handles orchestrator.ToolJob) and must
// never be merged into a single interface{} function (Pitfall 3, 02-RESEARCH.md).
type enqueueReleaseFn func(ctx context.Context, job releasedocs.ReleaseJob) error

// defaultTagPattern is the default tag glob used when tagPattern is empty.
const defaultTagPattern = "v*"

// handleGithubRelease processes a GitHub release event. It enqueues a
// ReleaseJob only when the action is "published" and the releaseTrigger allows
// release events (i.e. trigger is "release" or empty). Tag events are routed
// separately via handleGithubTagPush.
//
// Per Pitfall 2 (02-RESEARCH.md), .cadoo.yaml is NOT loaded here synchronously.
// The trigger/tagPattern values are webhook-side defaults; the dispatcher
// performs the authoritative enabled/config check when it runs the job.
func handleGithubRelease(ctx context.Context, e *gogithub.ReleaseEvent, releaseTrigger, tagPattern string, enqueue enqueueReleaseFn) {
	// Trigger early-exit: release events are only routed when trigger is
	// "release" or empty (the default). If the customer configured trigger=tag,
	// release events are silently skipped.
	if releaseTrigger != "" && releaseTrigger != "release" {
		slog.Debug("releasedocs: trigger excludes release event; skipping",
			"trigger", releaseTrigger, "provider", "github")
		return
	}

	if e.GetAction() != "published" {
		slog.Debug("releasedocs: github release action is not published; skipping",
			"action", e.GetAction())
		return
	}

	toRef := e.GetRelease().GetTagName()
	repo := e.GetRepo().GetFullName()

	if toRef == "" || repo == "" {
		slog.Warn("releasedocs: github release event missing tag or repo; skipping")
		return
	}

	job := releasedocs.ReleaseJob{
		Provider: vcs.KindGitHub,
		Repo:     repo,
		ToRef:    toRef,
		// TODO: populate Org from installation→org resolution (same gap as ToolJob.InstallID)
	}
	if err := enqueue(ctx, job); err != nil {
		slog.Error("releasedocs: enqueue github release job", "err", err, "repo", repo, "tag", toRef)
		return
	}
	slog.Info("releasedocs: enqueued github release job", "repo", repo, "tag", toRef)
}

// handleGithubTagPush processes a GitHub push event for tag creation. It
// enqueues a ReleaseJob only when:
//   - releaseTrigger is "tag" (not "release" / empty),
//   - the push is a tag creation (created=true, deleted=false),
//   - the ref starts with "refs/tags/", and
//   - the stripped tag name matches tagPattern (path.Match).
//
// The "refs/tags/" prefix is stripped per Pitfall 1 (02-RESEARCH.md) before
// storing the tag in ToRef.
func handleGithubTagPush(ctx context.Context, e *gogithub.PushEvent, releaseTrigger, tagPattern string, enqueue enqueueReleaseFn) {
	// Trigger early-exit: tag push events only enqueue when trigger=="tag".
	if releaseTrigger != "tag" {
		slog.Debug("releasedocs: trigger excludes tag push event; skipping",
			"trigger", releaseTrigger, "provider", "github")
		return
	}

	ref := e.GetRef()
	if !strings.HasPrefix(ref, "refs/tags/") {
		return
	}

	// Only act on tag creation, not deletion or update.
	if !e.GetCreated() || e.GetDeleted() {
		slog.Debug("releasedocs: github tag push is not a creation; skipping",
			"ref", ref, "created", e.GetCreated(), "deleted", e.GetDeleted())
		return
	}

	tagName := strings.TrimPrefix(ref, "refs/tags/")
	repo := e.GetRepo().GetFullName()

	// Apply tagPattern glob filter. Default to "v*" when empty.
	pattern := tagPattern
	if pattern == "" {
		pattern = defaultTagPattern
	}
	matched, err := path.Match(pattern, tagName)
	if err != nil {
		slog.Warn("releasedocs: tagPattern invalid; skipping", "pattern", pattern, "err", err)
		return
	}
	if !matched {
		slog.Debug("releasedocs: tag does not match pattern; skipping",
			"tag", tagName, "pattern", pattern)
		return
	}

	job := releasedocs.ReleaseJob{
		Provider: vcs.KindGitHub,
		Repo:     repo,
		ToRef:    tagName,
		// TODO: populate Org from installation→org resolution (same gap as ToolJob.InstallID)
	}
	if err := enqueue(ctx, job); err != nil {
		slog.Error("releasedocs: enqueue github tag push job", "err", err, "repo", repo, "tag", tagName)
		return
	}
	slog.Info("releasedocs: enqueued github tag push job", "repo", repo, "tag", tagName)
}

// handleGitlabRelease processes a GitLab release event. It enqueues a
// ReleaseJob only when the action is "create" and the releaseTrigger allows
// release events.
func handleGitlabRelease(ctx context.Context, e *glab.ReleaseEvent, releaseTrigger, tagPattern string, enqueue enqueueReleaseFn) {
	// Trigger early-exit: release events are only routed when trigger is
	// "release" or empty.
	if releaseTrigger != "" && releaseTrigger != "release" {
		slog.Debug("releasedocs: trigger excludes release event; skipping",
			"trigger", releaseTrigger, "provider", "gitlab")
		return
	}

	if e.Action != "create" {
		slog.Debug("releasedocs: gitlab release action is not create; skipping",
			"action", e.Action)
		return
	}

	toRef := e.Tag
	repo := e.Project.PathWithNamespace

	if toRef == "" || repo == "" {
		slog.Warn("releasedocs: gitlab release event missing tag or repo; skipping")
		return
	}

	job := releasedocs.ReleaseJob{
		Provider: vcs.KindGitLab,
		Repo:     repo,
		ToRef:    toRef,
		// TODO: populate Org from installation→org resolution (same gap as ToolJob.InstallID)
	}
	if err := enqueue(ctx, job); err != nil {
		slog.Error("releasedocs: enqueue gitlab release job", "err", err, "repo", repo, "tag", toRef)
		return
	}
	slog.Info("releasedocs: enqueued gitlab release job", "repo", repo, "tag", toRef)
}

// handleGitlabTagPush processes a GitLab tag push event. It enqueues a
// ReleaseJob only when:
//   - releaseTrigger is "tag",
//   - the After field is non-empty and not the zero SHA (tag creation, not deletion),
//   - the stripped tag name matches tagPattern.
//
// The "refs/tags/" prefix is stripped per Pitfall 1 (02-RESEARCH.md).
func handleGitlabTagPush(ctx context.Context, e *glab.TagEvent, releaseTrigger, tagPattern string, enqueue enqueueReleaseFn) {
	// Trigger early-exit: tag push events only enqueue when trigger=="tag".
	if releaseTrigger != "tag" {
		slog.Debug("releasedocs: trigger excludes tag push event; skipping",
			"trigger", releaseTrigger, "provider", "gitlab")
		return
	}

	// Zero SHA or empty After means tag deletion.
	const zeroSHA = "0000000000000000000000000000000000000000"
	if e.After == "" || e.After == zeroSHA {
		slog.Debug("releasedocs: gitlab tag push is a deletion; skipping",
			"ref", e.Ref, "after", e.After)
		return
	}

	if !strings.HasPrefix(e.Ref, "refs/tags/") {
		slog.Debug("releasedocs: gitlab tag push ref is not a tag ref; skipping",
			"ref", e.Ref)
		return
	}

	tagName := strings.TrimPrefix(e.Ref, "refs/tags/")
	repo := e.Project.PathWithNamespace

	// Apply tagPattern glob filter.
	pattern := tagPattern
	if pattern == "" {
		pattern = defaultTagPattern
	}
	matched, err := path.Match(pattern, tagName)
	if err != nil {
		slog.Warn("releasedocs: tagPattern invalid; skipping", "pattern", pattern, "err", err)
		return
	}
	if !matched {
		slog.Debug("releasedocs: tag does not match pattern; skipping",
			"tag", tagName, "pattern", pattern)
		return
	}

	job := releasedocs.ReleaseJob{
		Provider: vcs.KindGitLab,
		Repo:     repo,
		ToRef:    tagName,
		// TODO: populate Org from installation→org resolution (same gap as ToolJob.InstallID)
	}
	if err := enqueue(ctx, job); err != nil {
		slog.Error("releasedocs: enqueue gitlab tag push job", "err", err, "repo", repo, "tag", tagName)
		return
	}
	slog.Info("releasedocs: enqueued gitlab tag push job", "repo", repo, "tag", tagName)
}
