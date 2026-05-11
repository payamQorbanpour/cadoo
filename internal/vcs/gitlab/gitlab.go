// Package gitlab implements vcs.Provider against GitLab SaaS and
// self-managed instances. Authentication is via personal/project access
// token; multi-tenant SaaS will swap in per-installation tokens later.
package gitlab

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	glab "github.com/xanzy/go-gitlab"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// Config configures the adapter.
type Config struct {
	BaseURL string // empty for gitlab.com (e.g. "https://gitlab.example.com")
	Token   string // personal or project access token
}

// Adapter is the GitLab vcs.Provider implementation.
type Adapter struct {
	cfg    Config
	client *glab.Client
}

// New constructs an Adapter authenticated as the supplied token.
func New(cfg Config) (*Adapter, error) {
	var opts []glab.ClientOptionFunc
	if cfg.BaseURL != "" {
		opts = append(opts, glab.WithBaseURL(strings.TrimRight(cfg.BaseURL, "/")))
	}
	client, err := glab.NewClient(cfg.Token, opts...)
	if err != nil {
		return nil, fmt.Errorf("gitlab client: %w", err)
	}
	return &Adapter{cfg: cfg, client: client}, nil
}

// Kind reports gitlab.com / self-managed (both map to KindGitLab).
func (a *Adapter) Kind() vcs.Kind { return vcs.KindGitLab }

// FetchPullRequest implements vcs.Provider. The PR number is the MR IID.
func (a *Adapter) FetchPullRequest(ctx context.Context, repo string, number int64) (*vcs.PullRequest, error) {
	mr, _, err := a.client.MergeRequests.GetMergeRequest(repo, int(number),
		&glab.GetMergeRequestsOptions{}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get mr %s!%d: %w", repo, number, err)
	}
	return convertMR(mr, repo), nil
}

// ListChangedFiles implements vcs.Provider via the MR changes endpoint.
func (a *Adapter) ListChangedFiles(ctx context.Context, pr *vcs.PullRequest) ([]vcs.FileChange, error) {
	changes, _, err := a.client.MergeRequests.GetMergeRequestChanges(pr.RepoFullName, int(pr.Number),
		&glab.GetMergeRequestChangesOptions{}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get mr changes %s!%d: %w", pr.RepoFullName, pr.Number, err)
	}
	out := make([]vcs.FileChange, 0, len(changes.Changes))
	for _, c := range changes.Changes {
		path := c.NewPath
		if path == "" {
			path = c.OldPath
		}
		status := "modified"
		switch {
		case c.NewFile:
			status = "added"
		case c.DeletedFile:
			status = "removed"
		case c.RenamedFile:
			status = "renamed"
		}
		add, del := countDiffLines(c.Diff)
		out = append(out, vcs.FileChange{
			Path:      path,
			PrevPath:  c.OldPath,
			Status:    status,
			Patch:     c.Diff,
			Additions: add,
			Deletions: del,
			IsBinary:  c.Diff == "" && !c.DeletedFile,
		})
	}
	return out, nil
}

// PostSummaryComment creates a top-level MR note.
func (a *Adapter) PostSummaryComment(ctx context.Context, pr *vcs.PullRequest, body string) (string, error) {
	note, _, err := a.client.Notes.CreateMergeRequestNote(pr.RepoFullName, int(pr.Number),
		&glab.CreateMergeRequestNoteOptions{Body: ptr(body)}, glab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("create mr note: %w", err)
	}
	return strconv.Itoa(note.ID), nil
}

// UpdateSummaryComment edits an existing MR note.
func (a *Adapter) UpdateSummaryComment(ctx context.Context, pr *vcs.PullRequest, id, body string) error {
	nid, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("invalid note id %q: %w", id, err)
	}
	_, _, err = a.client.Notes.UpdateMergeRequestNote(pr.RepoFullName, int(pr.Number), nid,
		&glab.UpdateMergeRequestNoteOptions{Body: ptr(body)}, glab.WithContext(ctx))
	return err
}

// EditPullRequestBody replaces the MR description.
func (a *Adapter) EditPullRequestBody(ctx context.Context, pr *vcs.PullRequest, body string) error {
	_, _, err := a.client.MergeRequests.UpdateMergeRequest(pr.RepoFullName, int(pr.Number),
		&glab.UpdateMergeRequestOptions{Description: ptr(body)}, glab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("update mr description: %w", err)
	}
	return nil
}

// PostInlineComments creates one MR discussion per inline comment, anchored
// to a position object built from the MR's diff_refs. Failures on individual
// comments are returned as the first error after a best-effort attempt at
// the rest.
func (a *Adapter) PostInlineComments(ctx context.Context, pr *vcs.PullRequest, comments []vcs.InlineComment) error {
	if len(comments) == 0 {
		return nil
	}
	mr, _, err := a.client.MergeRequests.GetMergeRequest(pr.RepoFullName, int(pr.Number),
		&glab.GetMergeRequestsOptions{}, glab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("get mr for diff refs: %w", err)
	}
	if mr.DiffRefs.HeadSha == "" {
		return fmt.Errorf("mr %s!%d has no diff_refs", pr.RepoFullName, pr.Number)
	}

	var firstErr error
	for _, c := range comments {
		body := formatSeverity(c.Severity) + c.Body
		line := c.LineEnd
		if line == 0 {
			line = c.LineStart
		}
		if line == 0 {
			line = 1
		}
		opts := &glab.CreateMergeRequestDiscussionOptions{
			Body: ptr(body),
			Position: &glab.PositionOptions{
				BaseSHA:      ptr(mr.DiffRefs.BaseSha),
				StartSHA:     ptr(mr.DiffRefs.StartSha),
				HeadSHA:      ptr(mr.DiffRefs.HeadSha),
				PositionType: ptr("text"),
				NewPath:      ptr(c.File),
				OldPath:      ptr(c.File),
				NewLine:      ptr(line),
			},
		}
		if _, _, err := a.client.Discussions.CreateMergeRequestDiscussion(
			pr.RepoFullName, int(pr.Number), opts, glab.WithContext(ctx),
		); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("create discussion at %s:%d: %w", c.File, line, err)
		}
	}
	return firstErr
}

// UpsertCheckRun maps the abstract check run to a GitLab commit status on
// the head SHA.
func (a *Adapter) UpsertCheckRun(ctx context.Context, pr *vcs.PullRequest, run vcs.CheckRun) error {
	if pr.HeadSHA == "" {
		return fmt.Errorf("no head sha to attach status to")
	}
	state := mapCheckStatus(run.Status)
	opts := &glab.SetCommitStatusOptions{
		State:       state,
		Name:        ptr(run.Name),
		Description: ptr(run.Title),
	}
	if run.URL != "" {
		opts.TargetURL = ptr(run.URL)
	}
	_, _, err := a.client.Commits.SetCommitStatus(pr.RepoFullName, pr.HeadSHA, opts, glab.WithContext(ctx))
	return err
}

// FetchArchive returns a gzipped tarball of the project at ref. Used by the
// orchestrator to materialize a workspace for sandboxed linters.
func (a *Adapter) FetchArchive(ctx context.Context, repo, ref string) (io.ReadCloser, error) {
	data, _, err := a.client.Repositories.Archive(repo,
		&glab.ArchiveOptions{SHA: ptr(ref), Format: ptr("tar.gz")},
		glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("gitlab archive %s@%s: %w", repo, ref, err)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// FetchFileFromRef returns raw file contents at a given ref. Used by the
// orchestrator to read .cadoo.yaml from the MR head SHA.
func (a *Adapter) FetchFileFromRef(ctx context.Context, repo, ref, path string) ([]byte, error) {
	data, _, err := a.client.RepositoryFiles.GetRawFile(repo, path,
		&glab.GetRawFileOptions{Ref: ptr(ref)}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get raw file %s@%s:%s: %w", repo, ref, path, err)
	}
	return data, nil
}

func convertMR(mr *glab.MergeRequest, repo string) *vcs.PullRequest {
	pr := &vcs.PullRequest{
		Provider:     vcs.KindGitLab,
		RepoFullName: repo,
		Number:       int64(mr.IID),
		Title:        mr.Title,
		Body:         mr.Description,
		BaseRef:      mr.TargetBranch,
		HeadRef:      mr.SourceBranch,
		State:        mr.State,
		URL:          mr.WebURL,
	}
	if mr.Author != nil {
		pr.Author = mr.Author.Username
	}
	if mr.UpdatedAt != nil {
		pr.UpdatedAt = *mr.UpdatedAt
	}
	pr.BaseSHA = mr.DiffRefs.BaseSha
	pr.HeadSHA = mr.DiffRefs.HeadSha
	return pr
}

func formatSeverity(s vcs.Severity) string {
	switch s {
	case vcs.SeverityBlock:
		return "**[BLOCK]** "
	case vcs.SeverityWarn:
		return "**[WARN]** "
	case vcs.SeverityNit:
		return "_[nit]_ "
	}
	return ""
}

func mapCheckStatus(s vcs.CheckRunStatus) glab.BuildStateValue {
	switch s {
	case vcs.CheckQueued:
		return glab.Pending
	case vcs.CheckRunning:
		return glab.Running
	case vcs.CheckSucceeded:
		return glab.Success
	case vcs.CheckFailed:
		return glab.Failed
	case vcs.CheckNeutral:
		return glab.Success
	}
	return glab.Pending
}

// countDiffLines returns (additions, deletions) by counting +/- lines in a
// unified diff string. Header/context lines are ignored.
func countDiffLines(diff string) (add, del int) {
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			add++
		case strings.HasPrefix(line, "-"):
			del++
		}
	}
	return
}

// ptr is a generic pointer helper. Mirrors the pattern used in the github
// adapter so call sites stay uniform regardless of element type.
func ptr[T any](v T) *T { return &v }

var _ vcs.Provider = (*Adapter)(nil)
