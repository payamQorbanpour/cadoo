// Package github — release capability methods on *Adapter.
//
// This file adds the three optional release-docs capability interfaces to the
// GitHub adapter:
//   - vcs.ReleaseRangeReader   (ListCommits, ListMergedPRs, LatestTagBefore)
//   - vcs.ReleasePublisher     (GetReleaseByTag, UpdateReleaseBody)
//   - vcs.BranchCommitter      (UpsertFile, OpenOrUpdatePR)
//
// All calls go through the same authenticated *gogithub.Client as the core
// adapter (no GraphQL — Pitfall 5). Tokens and credentials stay inside
// *Adapter.cfg; they are never surfaced through these interfaces (T-04-01).
package github

import (
	"context"
	"fmt"
	"path"

	gogithub "github.com/google/go-github/v66/github"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// ListCommits returns all commits reachable from toRef but not fromRef, in
// reverse-chronological order. It uses Repositories.CompareCommits which
// performs a two-dot (non-symmetric) diff: toRef relative to fromRef.
func (a *Adapter) ListCommits(ctx context.Context, repo, fromRef, toRef string) ([]vcs.Commit, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	// CompareCommits accepts branch names, tags, or SHAs for both sides.
	cmp, _, err := a.client.Repositories.CompareCommits(ctx, owner, name, fromRef, toRef, nil)
	if err != nil {
		return nil, fmt.Errorf("compare commits %s %s..%s: %w", repo, fromRef, toRef, err)
	}
	out := make([]vcs.Commit, 0, len(cmp.Commits))
	for _, rc := range cmp.Commits {
		c := rc.GetCommit()
		author := ""
		if u := rc.GetAuthor(); u != nil {
			author = u.GetLogin()
		}
		if author == "" && c != nil {
			if ca := c.GetAuthor(); ca != nil {
				author = ca.GetName()
			}
		}
		date := c.GetAuthor().GetDate()
		out = append(out, vcs.Commit{
			SHA:     rc.GetSHA(),
			Message: c.GetMessage(),
			Author:  author,
			Date:    date.Time,
		})
	}
	return out, nil
}

// ListMergedPRs returns all pull-requests that were merged in the commit range
// defined by fromRef..toRef. It uses PullRequests.ListPullRequestsWithCommit
// on each commit returned by CompareCommits to find PRs whose merge SHA is
// present in the range.
func (a *Adapter) ListMergedPRs(ctx context.Context, repo, fromRef, toRef string) ([]vcs.MergedPR, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	// Collect commit SHAs in the range first.
	cmp, _, err := a.client.Repositories.CompareCommits(ctx, owner, name, fromRef, toRef, nil)
	if err != nil {
		return nil, fmt.Errorf("compare commits for PRs %s %s..%s: %w", repo, fromRef, toRef, err)
	}
	// Deduplicate PRs across commits (a merge commit appears for every commit
	// in the squashed PR).
	seen := make(map[int]bool)
	var out []vcs.MergedPR
	for _, rc := range cmp.Commits {
		sha := rc.GetSHA()
		if sha == "" {
			continue
		}
		prs, _, listErr := a.client.PullRequests.ListPullRequestsWithCommit(ctx, owner, name, sha,
			&gogithub.ListOptions{PerPage: 10})
		if listErr != nil {
			// Non-fatal: skip this commit's PRs if the API call fails.
			continue
		}
		for _, pr := range prs {
			num := pr.GetNumber()
			if seen[num] {
				continue
			}
			if pr.GetMerged() || (pr.GetState() == "closed" && !pr.GetMergedAt().IsZero()) {
				seen[num] = true
				labels := make([]string, 0, len(pr.Labels))
				for _, l := range pr.Labels {
					labels = append(labels, l.GetName())
				}
				out = append(out, vcs.MergedPR{
					Number:   int64(num),
					Title:    pr.GetTitle(),
					Body:     pr.GetBody(),
					Author:   pr.GetUser().GetLogin(),
					Labels:   labels,
					MergedAt: pr.GetMergedAt().Time,
					MergeSHA: pr.GetMergeCommitSHA(),
				})
			}
		}
	}
	return out, nil
}

// LatestTagBefore returns the most recent tag whose name matches tagPattern
// (a glob like "v*") and that precedes toRef in the repository's tag history.
// Tags are listed in reverse-chronological order by the API; we return the
// first match. Returns "" when no matching tag exists.
func (a *Adapter) LatestTagBefore(ctx context.Context, repo, toRef, tagPattern string) (string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return "", err
	}
	opts := &gogithub.ListOptions{PerPage: 100}
	for {
		tags, resp, err := a.client.Repositories.ListTags(ctx, owner, name, opts)
		if err != nil {
			return "", fmt.Errorf("list tags %s: %w", repo, err)
		}
		for _, t := range tags {
			tagName := t.GetName()
			if tagName == toRef {
				// Skip the toRef tag itself.
				continue
			}
			matched, matchErr := path.Match(tagPattern, tagName)
			if matchErr != nil {
				return "", fmt.Errorf("invalid tag pattern %q: %w", tagPattern, matchErr)
			}
			if matched {
				return tagName, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return "", nil
}

// GetReleaseByTag fetches a release by its tag name. Returns an error when
// the tag does not have an associated release.
func (a *Adapter) GetReleaseByTag(ctx context.Context, repo, tag string) (*vcs.Release, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	r, _, err := a.client.Repositories.GetReleaseByTag(ctx, owner, name, tag)
	if err != nil {
		return nil, fmt.Errorf("get release by tag %s@%s: %w", repo, tag, err)
	}
	return &vcs.Release{
		ID:         r.GetID(),
		TagName:    r.GetTagName(),
		Body:       r.GetBody(),
		Draft:      r.GetDraft(),
		Prerelease: r.GetPrerelease(),
	}, nil
}

// UpdateReleaseBody replaces the body field of the identified release. Other
// release fields (draft, prerelease, tag) are left unchanged.
func (a *Adapter) UpdateReleaseBody(ctx context.Context, repo string, releaseID int64, body string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	_, _, err = a.client.Repositories.EditRelease(ctx, owner, name, releaseID,
		&gogithub.RepositoryRelease{Body: ptr(body)})
	if err != nil {
		return fmt.Errorf("edit release %s#%d: %w", repo, releaseID, err)
	}
	return nil
}

// UpsertFile creates or updates a single file on the named branch. If the
// branch does not exist it is created from the default branch HEAD. commitMessage
// is used as the VCS commit message.
func (a *Adapter) UpsertFile(ctx context.Context, repo, branch, commitMessage string, f vcs.FileWrite) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	// Ensure the branch exists — try to get its ref; create if absent.
	refName := "refs/heads/" + branch
	_, _, getErr := a.client.Git.GetRef(ctx, owner, name, refName)
	if getErr != nil {
		// Branch missing — resolve the default branch HEAD to use as base.
		repoData, _, err := a.client.Repositories.Get(ctx, owner, name)
		if err != nil {
			return fmt.Errorf("get repo %s to find default branch: %w", repo, err)
		}
		defBranch := repoData.GetDefaultBranch()
		if defBranch == "" {
			defBranch = "main"
		}
		headRef, _, err := a.client.Git.GetRef(ctx, owner, name, "refs/heads/"+defBranch)
		if err != nil {
			return fmt.Errorf("get head ref %s: %w", defBranch, err)
		}
		baseSHA := headRef.GetObject().GetSHA()
		_, _, err = a.client.Git.CreateRef(ctx, owner, name, &gogithub.Reference{
			Ref:    ptr(refName),
			Object: &gogithub.GitObject{SHA: ptr(baseSHA)},
		})
		if err != nil {
			return fmt.Errorf("create branch %s: %w", branch, err)
		}
	}

	// Check if the file already exists so we can supply its blob SHA
	// (required by UpdateFile to prevent conflicts).
	existing, _, _, getFileErr := a.client.Repositories.GetContents(ctx, owner, name, f.Path,
		&gogithub.RepositoryContentGetOptions{Ref: branch})

	opts := &gogithub.RepositoryContentFileOptions{
		Message: ptr(commitMessage),
		Content: f.Content,
		Branch:  ptr(branch),
	}

	if getFileErr == nil && existing != nil {
		// File exists: supply blob SHA for UpdateFile.
		opts.SHA = ptr(existing.GetSHA())
		_, _, err = a.client.Repositories.UpdateFile(ctx, owner, name, f.Path, opts)
		if err != nil {
			return fmt.Errorf("update file %s on %s: %w", f.Path, branch, err)
		}
	} else {
		_, _, err = a.client.Repositories.CreateFile(ctx, owner, name, f.Path, opts)
		if err != nil {
			return fmt.Errorf("create file %s on %s: %w", f.Path, branch, err)
		}
	}
	return nil
}

// OpenOrUpdatePR ensures exactly one open pull-request exists for the given
// branch. If a PR is already open it is updated (title + body); otherwise a
// new PR is created from branch → base. Returns the PR number.
func (a *Adapter) OpenOrUpdatePR(ctx context.Context, repo, branch, base, title, body string) (int64, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return 0, err
	}

	// Look for an existing open PR from this branch to base.
	opts := &gogithub.PullRequestListOptions{
		State: "open",
		Head:  owner + ":" + branch,
		Base:  base,
	}
	opts.PerPage = 10
	existing, _, err := a.client.PullRequests.List(ctx, owner, name, opts)
	if err != nil {
		return 0, fmt.Errorf("list prs %s: %w", repo, err)
	}
	if len(existing) > 0 {
		pr := existing[0]
		// Update title + body only if something changed to keep the call idempotent.
		if pr.GetTitle() != title || pr.GetBody() != body {
			updated, _, err := a.client.PullRequests.Edit(ctx, owner, name, pr.GetNumber(),
				&gogithub.PullRequest{Title: ptr(title), Body: ptr(body)})
			if err != nil {
				return 0, fmt.Errorf("update pr %s#%d: %w", repo, pr.GetNumber(), err)
			}
			return int64(updated.GetNumber()), nil
		}
		return int64(pr.GetNumber()), nil
	}

	// No existing open PR — create one.
	created, _, err := a.client.PullRequests.Create(ctx, owner, name, &gogithub.NewPullRequest{
		Title: ptr(title),
		Head:  ptr(branch),
		Base:  ptr(base),
		Body:  ptr(body),
	})
	if err != nil {
		return 0, fmt.Errorf("create pr %s: %w", repo, err)
	}
	return int64(created.GetNumber()), nil
}

// Compile-time assertions: *Adapter must satisfy the three optional capability
// interfaces declared in internal/vcs/vcs.go.
var _ vcs.ReleaseRangeReader = (*Adapter)(nil)
var _ vcs.ReleasePublisher = (*Adapter)(nil)
var _ vcs.BranchCommitter = (*Adapter)(nil)
