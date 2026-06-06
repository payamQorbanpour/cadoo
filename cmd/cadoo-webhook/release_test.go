package main

import (
	"context"
	"testing"

	gogithub "github.com/google/go-github/v66/github"
	glab "gitlab.com/gitlab-org/api/client-go"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// recordingEnqueue captures every ReleaseJob passed to it.
type recordingEnqueue struct {
	jobs []releasedocs.ReleaseJob
}

func (r *recordingEnqueue) enqueue(_ context.Context, job releasedocs.ReleaseJob) error {
	r.jobs = append(r.jobs, job)
	return nil
}

// --- GitHub release --------------------------------------------------------

func TestHandleGithubRelease_Published(t *testing.T) {
	rec := &recordingEnqueue{}
	action := "published"
	tagName := "v1.2.3"
	fullName := "owner/repo"
	e := &gogithub.ReleaseEvent{
		Action: &action,
		Release: &gogithub.RepositoryRelease{
			TagName: &tagName,
		},
		Repo: &gogithub.Repository{FullName: &fullName},
	}

	handleGithubRelease(context.Background(), e, "release", "v*", rec.enqueue)

	if len(rec.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(rec.jobs))
	}
	job := rec.jobs[0]
	if job.Provider != vcs.KindGitHub {
		t.Errorf("Provider = %q; want %q", job.Provider, vcs.KindGitHub)
	}
	if job.Repo != fullName {
		t.Errorf("Repo = %q; want %q", job.Repo, fullName)
	}
	if job.ToRef != tagName {
		t.Errorf("ToRef = %q; want %q", job.ToRef, tagName)
	}
}

func TestHandleGithubRelease_NonPublished(t *testing.T) {
	rec := &recordingEnqueue{}
	for _, action := range []string{"draft", "edited", "deleted", "created", "prereleased"} {
		action := action
		tagName := "v1.0.0"
		fullName := "owner/repo"
		e := &gogithub.ReleaseEvent{
			Action:  &action,
			Release: &gogithub.RepositoryRelease{TagName: &tagName},
			Repo:    &gogithub.Repository{FullName: &fullName},
		}
		handleGithubRelease(context.Background(), e, "release", "v*", rec.enqueue)
	}
	if len(rec.jobs) != 0 {
		t.Errorf("expected 0 enqueued jobs for non-published actions, got %d", len(rec.jobs))
	}
}

// Trigger early-exit: trigger=="tag" + release event → no enqueue.
func TestHandleGithubRelease_TriggerTagExcludesRelease(t *testing.T) {
	rec := &recordingEnqueue{}
	action := "published"
	tagName := "v1.0.0"
	fullName := "owner/repo"
	e := &gogithub.ReleaseEvent{
		Action:  &action,
		Release: &gogithub.RepositoryRelease{TagName: &tagName},
		Repo:    &gogithub.Repository{FullName: &fullName},
	}
	handleGithubRelease(context.Background(), e, "tag", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("trigger=tag should exclude release event; got %d jobs", len(rec.jobs))
	}
}

// --- GitHub tag push -------------------------------------------------------

func TestHandleGithubTagPush_Created(t *testing.T) {
	rec := &recordingEnqueue{}
	ref := "refs/tags/v2.0.0"
	created := true
	deleted := false
	fullName := "owner/repo"
	e := &gogithub.PushEvent{
		Ref:     &ref,
		Created: &created,
		Deleted: &deleted,
		Repo:    &gogithub.PushEventRepository{FullName: &fullName},
	}

	handleGithubTagPush(context.Background(), e, "tag", "v*", rec.enqueue)

	if len(rec.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(rec.jobs))
	}
	job := rec.jobs[0]
	if job.Provider != vcs.KindGitHub {
		t.Errorf("Provider = %q; want %q", job.Provider, vcs.KindGitHub)
	}
	if job.ToRef != "v2.0.0" {
		t.Errorf("ToRef = %q; want stripped tag %q", job.ToRef, "v2.0.0")
	}
	if job.Repo != fullName {
		t.Errorf("Repo = %q; want %q", job.Repo, fullName)
	}
}

func TestHandleGithubTagPush_Deletion(t *testing.T) {
	rec := &recordingEnqueue{}
	ref := "refs/tags/v1.0.0"
	created := false
	deleted := true
	fullName := "owner/repo"
	e := &gogithub.PushEvent{
		Ref:     &ref,
		Created: &created,
		Deleted: &deleted,
		Repo:    &gogithub.PushEventRepository{FullName: &fullName},
	}
	handleGithubTagPush(context.Background(), e, "tag", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("tag deletion should not enqueue; got %d jobs", len(rec.jobs))
	}
}

func TestHandleGithubTagPush_NonTagRef(t *testing.T) {
	rec := &recordingEnqueue{}
	ref := "refs/heads/main"
	created := true
	deleted := false
	fullName := "owner/repo"
	e := &gogithub.PushEvent{
		Ref:     &ref,
		Created: &created,
		Deleted: &deleted,
		Repo:    &gogithub.PushEventRepository{FullName: &fullName},
	}
	handleGithubTagPush(context.Background(), e, "tag", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("non-tag push should not enqueue; got %d jobs", len(rec.jobs))
	}
}

// Trigger early-exit: trigger=="release" (default) + tag push → no enqueue.
func TestHandleGithubTagPush_TriggerReleaseExcludesTag(t *testing.T) {
	rec := &recordingEnqueue{}
	ref := "refs/tags/v1.0.0"
	created := true
	deleted := false
	fullName := "owner/repo"
	e := &gogithub.PushEvent{
		Ref:     &ref,
		Created: &created,
		Deleted: &deleted,
		Repo:    &gogithub.PushEventRepository{FullName: &fullName},
	}
	handleGithubTagPush(context.Background(), e, "release", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("trigger=release should exclude tag push; got %d jobs", len(rec.jobs))
	}
}

func TestHandleGithubTagPush_TagPatternMismatch(t *testing.T) {
	rec := &recordingEnqueue{}
	ref := "refs/tags/release-1.0.0"
	created := true
	deleted := false
	fullName := "owner/repo"
	e := &gogithub.PushEvent{
		Ref:     &ref,
		Created: &created,
		Deleted: &deleted,
		Repo:    &gogithub.PushEventRepository{FullName: &fullName},
	}
	// pattern "v*" should not match "release-1.0.0"
	handleGithubTagPush(context.Background(), e, "tag", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("tagPattern mismatch should not enqueue; got %d jobs", len(rec.jobs))
	}
}

// --- GitLab release --------------------------------------------------------

func TestHandleGitlabRelease_Create(t *testing.T) {
	rec := &recordingEnqueue{}
	e := &glab.ReleaseEvent{
		Action: "create",
		Tag:    "v3.0.0",
		Project: glab.ReleaseEventProject{
			PathWithNamespace: "group/repo",
		},
	}

	handleGitlabRelease(context.Background(), e, "release", "v*", rec.enqueue)

	if len(rec.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(rec.jobs))
	}
	job := rec.jobs[0]
	if job.Provider != vcs.KindGitLab {
		t.Errorf("Provider = %q; want %q", job.Provider, vcs.KindGitLab)
	}
	if job.ToRef != "v3.0.0" {
		t.Errorf("ToRef = %q; want %q", job.ToRef, "v3.0.0")
	}
	if job.Repo != "group/repo" {
		t.Errorf("Repo = %q; want %q", job.Repo, "group/repo")
	}
}

func TestHandleGitlabRelease_NonCreate(t *testing.T) {
	rec := &recordingEnqueue{}
	for _, action := range []string{"update", "delete"} {
		e := &glab.ReleaseEvent{
			Action: action,
			Tag:    "v1.0.0",
			Project: glab.ReleaseEventProject{
				PathWithNamespace: "group/repo",
			},
		}
		handleGitlabRelease(context.Background(), e, "release", "v*", rec.enqueue)
	}
	if len(rec.jobs) != 0 {
		t.Errorf("expected 0 jobs for non-create actions, got %d", len(rec.jobs))
	}
}

// Trigger early-exit: trigger=="tag" + GitLab release event → no enqueue.
func TestHandleGitlabRelease_TriggerTagExcludesRelease(t *testing.T) {
	rec := &recordingEnqueue{}
	e := &glab.ReleaseEvent{
		Action: "create",
		Tag:    "v1.0.0",
		Project: glab.ReleaseEventProject{
			PathWithNamespace: "group/repo",
		},
	}
	handleGitlabRelease(context.Background(), e, "tag", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("trigger=tag should exclude release event; got %d jobs", len(rec.jobs))
	}
}

// --- GitLab tag push -------------------------------------------------------

func TestHandleGitlabTagPush_Created(t *testing.T) {
	rec := &recordingEnqueue{}
	e := &glab.TagEvent{
		Ref:   "refs/tags/v4.0.0",
		After: "abc123def456abc123def456abc123def456abc1",
		Project: glab.TagEventProject{
			PathWithNamespace: "group/repo",
		},
	}

	handleGitlabTagPush(context.Background(), e, "tag", "v*", rec.enqueue)

	if len(rec.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(rec.jobs))
	}
	job := rec.jobs[0]
	if job.Provider != vcs.KindGitLab {
		t.Errorf("Provider = %q; want %q", job.Provider, vcs.KindGitLab)
	}
	if job.ToRef != "v4.0.0" {
		t.Errorf("ToRef = %q; want stripped tag %q", job.ToRef, "v4.0.0")
	}
	if job.Repo != "group/repo" {
		t.Errorf("Repo = %q; want %q", job.Repo, "group/repo")
	}
}

func TestHandleGitlabTagPush_Deletion(t *testing.T) {
	rec := &recordingEnqueue{}
	e := &glab.TagEvent{
		Ref:   "refs/tags/v1.0.0",
		After: "0000000000000000000000000000000000000000",
		Project: glab.TagEventProject{
			PathWithNamespace: "group/repo",
		},
	}
	handleGitlabTagPush(context.Background(), e, "tag", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("tag deletion should not enqueue; got %d jobs", len(rec.jobs))
	}
}

func TestHandleGitlabTagPush_EmptyAfter(t *testing.T) {
	rec := &recordingEnqueue{}
	e := &glab.TagEvent{
		Ref:   "refs/tags/v1.0.0",
		After: "",
		Project: glab.TagEventProject{
			PathWithNamespace: "group/repo",
		},
	}
	handleGitlabTagPush(context.Background(), e, "tag", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("empty After should not enqueue; got %d jobs", len(rec.jobs))
	}
}

// Trigger early-exit: trigger=="release" + GitLab tag push → no enqueue.
func TestHandleGitlabTagPush_TriggerReleaseExcludesTag(t *testing.T) {
	rec := &recordingEnqueue{}
	e := &glab.TagEvent{
		Ref:   "refs/tags/v1.0.0",
		After: "abc123def456abc123def456abc123def456abc1",
		Project: glab.TagEventProject{
			PathWithNamespace: "group/repo",
		},
	}
	handleGitlabTagPush(context.Background(), e, "release", "v*", rec.enqueue)
	if len(rec.jobs) != 0 {
		t.Errorf("trigger=release should exclude tag push; got %d jobs", len(rec.jobs))
	}
}

// Consolidated trigger early-exit test across all four handlers.
func TestTriggerEarlyExit(t *testing.T) {
	t.Run("github release with trigger=tag", func(t *testing.T) {
		rec := &recordingEnqueue{}
		action := "published"
		tagName := "v1.0.0"
		fullName := "owner/repo"
		e := &gogithub.ReleaseEvent{
			Action:  &action,
			Release: &gogithub.RepositoryRelease{TagName: &tagName},
			Repo:    &gogithub.Repository{FullName: &fullName},
		}
		handleGithubRelease(context.Background(), e, "tag", "v*", rec.enqueue)
		if len(rec.jobs) != 0 {
			t.Errorf("trigger=tag: github release event should not enqueue; got %d", len(rec.jobs))
		}
	})
	t.Run("github tag push with trigger=release", func(t *testing.T) {
		rec := &recordingEnqueue{}
		ref := "refs/tags/v1.0.0"
		created := true
		deleted := false
		fullName := "owner/repo"
		e := &gogithub.PushEvent{
			Ref:     &ref,
			Created: &created,
			Deleted: &deleted,
			Repo:    &gogithub.PushEventRepository{FullName: &fullName},
		}
		handleGithubTagPush(context.Background(), e, "release", "v*", rec.enqueue)
		if len(rec.jobs) != 0 {
			t.Errorf("trigger=release: github tag push should not enqueue; got %d", len(rec.jobs))
		}
	})
	t.Run("gitlab release with trigger=tag", func(t *testing.T) {
		rec := &recordingEnqueue{}
		e := &glab.ReleaseEvent{
			Action:  "create",
			Tag:     "v1.0.0",
			Project: glab.ReleaseEventProject{PathWithNamespace: "g/r"},
		}
		handleGitlabRelease(context.Background(), e, "tag", "v*", rec.enqueue)
		if len(rec.jobs) != 0 {
			t.Errorf("trigger=tag: gitlab release event should not enqueue; got %d", len(rec.jobs))
		}
	})
	t.Run("gitlab tag push with trigger=release", func(t *testing.T) {
		rec := &recordingEnqueue{}
		e := &glab.TagEvent{
			Ref:     "refs/tags/v1.0.0",
			After:   "abc123",
			Project: glab.TagEventProject{PathWithNamespace: "g/r"},
		}
		handleGitlabTagPush(context.Background(), e, "release", "v*", rec.enqueue)
		if len(rec.jobs) != 0 {
			t.Errorf("trigger=release: gitlab tag push should not enqueue; got %d", len(rec.jobs))
		}
	})
}

// TestHandleGitlabTagPush_NonTagRef verifies that a GitLab push event whose
// Ref does not start with "refs/tags/" is silently dropped (WR-02 guard).
func TestHandleGitlabTagPush_NonTagRef(t *testing.T) {
	cases := []struct {
		name string
		ref  string
	}{
		{"branch ref", "refs/heads/main"},
		{"bare branch name", "main"},
		{"empty ref", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingEnqueue{}
			e := &glab.TagEvent{
				Ref:     tc.ref,
				After:   "abc123def456",
				Project: glab.TagEventProject{PathWithNamespace: "g/r"},
			}
			handleGitlabTagPush(context.Background(), e, "tag", "v*", rec.enqueue)
			if len(rec.jobs) != 0 {
				t.Errorf("ref=%q: expected no enqueue for non-tag ref, got %d", tc.ref, len(rec.jobs))
			}
		})
	}
}
