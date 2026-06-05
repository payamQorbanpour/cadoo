// Package gitlab — release capability methods on *Adapter.
//
// This file adds the three optional release-docs capability interfaces to the
// GitLab adapter:
//   - vcs.ReleaseRangeReader   (ListCommits, ListMergedPRs, LatestTagBefore)
//   - vcs.ReleasePublisher     (GetReleaseByTag, UpdateReleaseBody)
//   - vcs.BranchCommitter      (UpsertFile, OpenOrUpdatePR)
//
// CRITICAL: import path is gitlab.com/gitlab-org/api/client-go aliased as glab
// (Pitfall 1). Every client call takes glab.WithContext(ctx) as the trailing
// argument (house style). Tokens stay inside *Adapter.cfg; never surfaced
// through these interfaces (T-04-01).
package gitlab

import (
	"context"
	"encoding/base64"
	"fmt"
	"path"
	"time"

	glab "gitlab.com/gitlab-org/api/client-go"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// ListCommits returns all commits reachable from toRef but not fromRef, in
// reverse-chronological order. It uses Repositories.Compare to obtain the
// commit range, then extracts the commits from the comparison result.
func (a *Adapter) ListCommits(ctx context.Context, repo, fromRef, toRef string) ([]vcs.Commit, error) {
	cmp, _, err := a.client.Repositories.Compare(repo, &glab.CompareOptions{
		From: ptr(fromRef),
		To:   ptr(toRef),
	}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("compare commits %s %s..%s: %w", repo, fromRef, toRef, err)
	}
	if cmp == nil {
		return nil, nil
	}
	out := make([]vcs.Commit, 0, len(cmp.Commits))
	for _, c := range cmp.Commits {
		if c == nil {
			continue
		}
		author := c.AuthorName
		date := c.AuthoredDate
		var t vcs.Commit
		t.SHA = c.ID
		t.Message = c.Message
		t.Author = author
		if date != nil {
			t.Date = *date
		}
		out = append(out, t)
	}
	return out, nil
}

// ListMergedPRs returns all merge requests that were merged in the commit range
// defined by fromRef..toRef. It lists project MRs with state=merged. Because
// GitLab's Compare API returns the commits but not which MR they belong to, we
// collect all merged MRs and return them; the caller is responsible for
// filtering by merge SHA if needed.
func (a *Adapter) ListMergedPRs(ctx context.Context, repo, fromRef, toRef string) ([]vcs.MergedPR, error) {
	// Collect commit IDs in the range to cross-reference MR merge SHAs.
	cmp, _, err := a.client.Repositories.Compare(repo, &glab.CompareOptions{
		From: ptr(fromRef),
		To:   ptr(toRef),
	}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("compare for MRs %s %s..%s: %w", repo, fromRef, toRef, err)
	}
	rangeSHAs := make(map[string]bool)
	if cmp != nil {
		for _, c := range cmp.Commits {
			if c != nil {
				rangeSHAs[c.ID] = true
			}
		}
	}

	var out []vcs.MergedPR
	opts := &glab.ListProjectMergeRequestsOptions{
		State: ptr("merged"),
	}
	opts.PerPage = 100
	opts.Page = 1
	for {
		mrs, resp, err := a.client.MergeRequests.ListProjectMergeRequests(repo, opts, glab.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("list merged mrs %s: %w", repo, err)
		}
		for _, mr := range mrs {
			if mr == nil {
				continue
			}
			// Include the MR if its merge commit SHA or squash commit SHA is in range,
			// or if rangeSHAs is empty (no compare result, include all merged MRs).
			inRange := len(rangeSHAs) == 0 || rangeSHAs[mr.MergeCommitSHA] || rangeSHAs[mr.SquashCommitSHA]
			if !inRange {
				continue
			}
			labels := make([]string, 0, len(mr.Labels))
			for _, l := range mr.Labels {
				labels = append(labels, string(l))
			}
			author := ""
			if mr.Author != nil {
				author = mr.Author.Username
			}
			var mergedAt time.Time
			if mr.MergedAt != nil {
				mergedAt = *mr.MergedAt
			}
			out = append(out, vcs.MergedPR{
				Number:   mr.IID,
				Title:    mr.Title,
				Body:     mr.Description,
				Author:   author,
				Labels:   labels,
				MergedAt: mergedAt,
				MergeSHA: mr.MergeCommitSHA,
			})
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// LatestTagBefore returns the most recent tag whose name matches tagPattern
// (a glob like "v*") and that precedes toRef in the repository's tag history.
// Tags are listed with the API default sort (created_at desc); we return the
// first matching tag that is not toRef. Returns "" when no matching tag exists.
func (a *Adapter) LatestTagBefore(ctx context.Context, repo, toRef, tagPattern string) (string, error) {
	opts := &glab.ListTagsOptions{
		OrderBy: ptr("version"),
		Sort:    ptr("desc"),
	}
	opts.PerPage = 100
	opts.Page = 1
	for {
		tags, resp, err := a.client.Tags.ListTags(repo, opts, glab.WithContext(ctx))
		if err != nil {
			return "", fmt.Errorf("list tags %s: %w", repo, err)
		}
		for _, t := range tags {
			if t == nil {
				continue
			}
			if t.Name == toRef {
				continue
			}
			matched, matchErr := path.Match(tagPattern, t.Name)
			if matchErr != nil {
				return "", fmt.Errorf("invalid tag pattern %q: %w", tagPattern, matchErr)
			}
			if matched {
				return t.Name, nil
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return "", nil
}

// GetReleaseByTag fetches a release by its tag name. Returns an error when
// the tag does not have an associated release.
func (a *Adapter) GetReleaseByTag(ctx context.Context, repo, tag string) (*vcs.Release, error) {
	r, _, err := a.client.Releases.GetRelease(repo, tag, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get release by tag %s@%s: %w", repo, tag, err)
	}
	return &vcs.Release{
		// GitLab releases don't have a numeric ID; use 0. The tag name is the
		// stable identifier used by UpdateReleaseBody.
		ID:      0,
		TagName: r.TagName,
		Body:    r.Description,
	}, nil
}

// UpdateReleaseBody replaces the description field of the identified release.
// For GitLab, releases are identified by their tag name rather than a numeric
// ID. Because the vcs.ReleasePublisher interface carries only an int64 releaseID,
// and GetReleaseByTag returns vcs.Release.ID=0 for GitLab, publishers that need
// to update a GitLab release should type-assert for [*Adapter] and call
// UpdateReleaseBodyByTag directly. This interface implementation returns an
// error directing callers to the type-safe helper.
func (a *Adapter) UpdateReleaseBody(_ context.Context, repo string, _ int64, _ string) error {
	return fmt.Errorf("gitlab.UpdateReleaseBody: GitLab releases have no numeric ID; " +
		"type-assert *gitlab.Adapter and call UpdateReleaseBodyByTag(ctx, %q, tag, body) instead", repo)
}

// UpdateReleaseBodyByTag replaces the description of the release identified by
// tag. This is the GitLab-specific method that publishers should prefer over the
// generic UpdateReleaseBody interface method.
func (a *Adapter) UpdateReleaseBodyByTag(ctx context.Context, repo, tag, body string) error {
	_, _, err := a.client.Releases.UpdateRelease(repo, tag, &glab.UpdateReleaseOptions{
		Description: ptr(body),
	}, glab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("update release %s@%s: %w", repo, tag, err)
	}
	return nil
}

// UpsertFile creates or updates a single file on the named branch. If the
// branch does not exist it is created from the default branch via GitLab's
// start_branch option on CreateFile. commitMessage is used as the VCS commit
// message.
func (a *Adapter) UpsertFile(ctx context.Context, repo, branch, commitMessage string, f vcs.FileWrite) error {
	// Try to get the existing file to determine whether to create or update.
	existing, _, getErr := a.client.RepositoryFiles.GetFile(repo, f.Path,
		&glab.GetFileOptions{Ref: ptr(branch)}, glab.WithContext(ctx))

	content := base64.StdEncoding.EncodeToString(f.Content)

	if getErr != nil {
		// File doesn't exist (or branch doesn't exist) — create it.
		// Use start_branch="main" so GitLab creates the branch if needed.
		_, _, err := a.client.RepositoryFiles.CreateFile(repo, f.Path, &glab.CreateFileOptions{
			Branch:        ptr(branch),
			StartBranch:   ptr("main"),
			Content:       ptr(content),
			Encoding:      ptr("base64"),
			CommitMessage: ptr(commitMessage),
		}, glab.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("create file %s on %s: %w", f.Path, branch, err)
		}
		return nil
	}

	// File exists — update it, supplying the last commit ID for safety.
	lastCommitID := ""
	if existing != nil {
		lastCommitID = existing.LastCommitID
	}
	updateOpts := &glab.UpdateFileOptions{
		Branch:        ptr(branch),
		Content:       ptr(content),
		Encoding:      ptr("base64"),
		CommitMessage: ptr(commitMessage),
	}
	if lastCommitID != "" {
		updateOpts.LastCommitID = ptr(lastCommitID)
	}
	_, _, err := a.client.RepositoryFiles.UpdateFile(repo, f.Path, updateOpts, glab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("update file %s on %s: %w", f.Path, branch, err)
	}
	return nil
}

// OpenOrUpdatePR ensures exactly one open merge request exists for the given
// source branch. If an MR is already open it is updated (title + body);
// otherwise a new MR is created from branch → base. Returns the MR IID.
func (a *Adapter) OpenOrUpdatePR(ctx context.Context, repo, branch, base, title, body string) (int64, error) {
	// Look for an existing open MR from this branch to base.
	opts := &glab.ListProjectMergeRequestsOptions{
		State:        ptr("opened"),
		SourceBranch: ptr(branch),
		TargetBranch: ptr(base),
	}
	opts.PerPage = 10
	opts.Page = 1
	existing, _, err := a.client.MergeRequests.ListProjectMergeRequests(repo, opts, glab.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("list mrs %s: %w", repo, err)
	}
	if len(existing) > 0 {
		mr := existing[0]
		if mr.Title != title || mr.Description != body {
			updated, _, err := a.client.MergeRequests.UpdateMergeRequest(repo, mr.IID,
				&glab.UpdateMergeRequestOptions{
					Title:       ptr(title),
					Description: ptr(body),
				}, glab.WithContext(ctx))
			if err != nil {
				return 0, fmt.Errorf("update mr %s!%d: %w", repo, mr.IID, err)
			}
			return int64(updated.IID), nil
		}
		return int64(mr.IID), nil
	}

	// No existing open MR — create one.
	created, _, err := a.client.MergeRequests.CreateMergeRequest(repo, &glab.CreateMergeRequestOptions{
		Title:        ptr(title),
		Description:  ptr(body),
		SourceBranch: ptr(branch),
		TargetBranch: ptr(base),
	}, glab.WithContext(ctx))
	if err != nil {
		return 0, fmt.Errorf("create mr %s: %w", repo, err)
	}
	return int64(created.IID), nil
}

// Compile-time assertions: *Adapter must satisfy the three optional capability
// interfaces declared in internal/vcs/vcs.go.
var _ vcs.ReleaseRangeReader = (*Adapter)(nil)
var _ vcs.ReleasePublisher = (*Adapter)(nil)
var _ vcs.BranchCommitter = (*Adapter)(nil)
