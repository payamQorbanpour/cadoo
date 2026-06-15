// Package github implements vcs.Provider against GitHub.com and GitHub
// Enterprise Server. Authentication is either a GitHub App installation
// (bradleyfalzon/ghinstallation, used by cadoo-webhook) or a bearer token
// such as the Actions-injected GITHUB_TOKEN / a PAT (used by `cadoo ci`).
package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gogithub "github.com/google/go-github/v66/github"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// Config configures the adapter. Exactly one auth mode must be set: either
// Token (bearer-token / PAT / Actions GITHUB_TOKEN) or the AppID +
// InstallationID + PrivateKeyPEM triple. Token wins if both are present.
type Config struct {
	BaseURL   string // empty for github.com; e.g. "https://ghe.example.com/api/v3" for GHES
	UploadURL string // optional GHES upload URL

	// Token-based auth. Used by `cadoo ci` inside GitHub Actions where the
	// runner injects GITHUB_TOKEN. Mutually exclusive with App auth.
	Token string

	AppID          int64
	InstallationID int64
	PrivateKeyPEM  []byte
}

// Adapter is the GitHub vcs.Provider implementation.
type Adapter struct {
	cfg    Config
	client *gogithub.Client

	// GraphQL seams. Default to the authenticated go-github http client and
	// the endpoint derived from its REST base URL; overridable in tests.
	gqlClient   *http.Client
	gqlEndpoint string
}

// New returns a ready Adapter authenticated either by bearer token or as a
// GitHub App installation, depending on which fields of cfg are set.
func New(cfg Config) (*Adapter, error) {
	var (
		httpClient *http.Client
		client     *gogithub.Client
		err        error
	)

	if cfg.Token != "" {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	} else {
		tr, err := ghinstallation.New(http.DefaultTransport, cfg.AppID, cfg.InstallationID, cfg.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("github app auth: %w", err)
		}
		if cfg.BaseURL != "" {
			tr.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
		}
		httpClient = &http.Client{Transport: tr, Timeout: 30 * time.Second}
	}

	if cfg.BaseURL == "" {
		client = gogithub.NewClient(httpClient)
	} else {
		client, err = gogithub.NewClient(httpClient).WithEnterpriseURLs(cfg.BaseURL, cfg.UploadURL)
		if err != nil {
			return nil, fmt.Errorf("ghes urls: %w", err)
		}
	}
	if cfg.Token != "" {
		client = client.WithAuthToken(cfg.Token)
	}
	ad := &Adapter{cfg: cfg, client: client}
	ad.gqlClient = client.Client()
	ad.gqlEndpoint = graphqlEndpoint(client.BaseURL)
	return ad, nil
}

// Kind reports github.com vs GHES.
func (a *Adapter) Kind() vcs.Kind {
	if a.cfg.BaseURL == "" {
		return vcs.KindGitHub
	}
	return vcs.KindGitHubEnterprise
}

// FetchPullRequest implements vcs.Provider.
func (a *Adapter) FetchPullRequest(ctx context.Context, repo string, number int64) (*vcs.PullRequest, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	pr, _, err := a.client.PullRequests.Get(ctx, owner, name, int(number))
	if err != nil {
		return nil, fmt.Errorf("get pr %s#%d: %w", repo, number, err)
	}
	return convertPR(pr, repo, a.Kind()), nil
}

// ListChangedFiles implements vcs.Provider with paginated retrieval.
func (a *Adapter) ListChangedFiles(ctx context.Context, pr *vcs.PullRequest) ([]vcs.FileChange, error) {
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return nil, err
	}
	var out []vcs.FileChange
	opts := &gogithub.ListOptions{PerPage: 100}
	for {
		files, resp, err := a.client.PullRequests.ListFiles(ctx, owner, name, int(pr.Number), opts)
		if err != nil {
			return nil, fmt.Errorf("list files %s#%d: %w", pr.RepoFullName, pr.Number, err)
		}
		for _, f := range files {
			out = append(out, vcs.FileChange{
				Path:      f.GetFilename(),
				PrevPath:  f.GetPreviousFilename(),
				Status:    f.GetStatus(),
				Patch:     f.GetPatch(),
				Additions: f.GetAdditions(),
				Deletions: f.GetDeletions(),
				IsBinary:  f.GetPatch() == "" && f.GetChanges() > 0,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return out, nil
}

// PostSummaryComment creates an issue comment and returns its numeric ID
// as a decimal string.
func (a *Adapter) PostSummaryComment(ctx context.Context, pr *vcs.PullRequest, body string) (string, error) {
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return "", err
	}
	c, _, err := a.client.Issues.CreateComment(ctx, owner, name, int(pr.Number),
		&gogithub.IssueComment{Body: ptr(body)})
	if err != nil {
		return "", fmt.Errorf("create issue comment: %w", err)
	}
	return strconv.FormatInt(c.GetID(), 10), nil
}

// UpdateSummaryComment edits an existing issue comment.
func (a *Adapter) UpdateSummaryComment(ctx context.Context, pr *vcs.PullRequest, id, body string) error {
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return err
	}
	cid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid comment id %q: %w", id, err)
	}
	_, _, err = a.client.Issues.EditComment(ctx, owner, name, cid,
		&gogithub.IssueComment{Body: ptr(body)})
	return err
}

// EditPullRequestBody replaces the PR description.
func (a *Adapter) EditPullRequestBody(ctx context.Context, pr *vcs.PullRequest, body string) error {
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return err
	}
	_, _, err = a.client.PullRequests.Edit(ctx, owner, name, int(pr.Number),
		&gogithub.PullRequest{Body: ptr(body)})
	if err != nil {
		return fmt.Errorf("edit pr body: %w", err)
	}
	return nil
}

// PostInlineComments creates a single PR review with all inline comments.
// Multi-line comments use start_line/line on the RIGHT side of the diff.
func (a *Adapter) PostInlineComments(ctx context.Context, pr *vcs.PullRequest, comments []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	if len(comments) == 0 {
		return nil, nil
	}
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return nil, err
	}
	drafts := make([]*gogithub.DraftReviewComment, 0, len(comments))
	for _, c := range comments {
		body := formatSeverity(c.Severity) + c.Body
		dc := &gogithub.DraftReviewComment{
			Path: ptr(c.File),
			Body: ptr(body),
			Side: ptr("RIGHT"),
		}
		switch {
		case c.LineStart > 0 && c.LineEnd > c.LineStart:
			dc.StartLine = ptr(c.LineStart)
			dc.Line = ptr(c.LineEnd)
			dc.StartSide = ptr("RIGHT")
		case c.LineStart > 0:
			dc.Line = ptr(c.LineStart)
		default:
			// File-level comment falls back to position 1.
			dc.Line = ptr(1)
		}
		drafts = append(drafts, dc)
	}
	_, _, err = a.client.PullRequests.CreateReview(ctx, owner, name, int(pr.Number),
		&gogithub.PullRequestReviewRequest{
			Event:    ptr("COMMENT"),
			Comments: drafts,
		})
	if err != nil {
		return nil, err
	}
	// GitHub's review-create API returns the review, not per-comment
	// IDs, so the refs we surface here carry empty IDs. Thread
	// resolution does not rely on these: ListCadooArtifacts recovers
	// review-thread node IDs via GraphQL, and ResolveThread resolves
	// them through the resolveReviewThread mutation.
	refs := make([]vcs.PostedInlineRef, len(comments))
	for i, c := range comments {
		refs[i] = vcs.PostedInlineRef{Comment: c}
	}
	return refs, nil
}

// ListCadooArtifacts implements vcs.PriorReviewReader using a single
// paginated GraphQL query over the PR's issue comments (overview, matched
// by SummaryWrapperBegin) and review threads (inline findings, matched by
// the hidden marker on the first comment).
func (a *Adapter) ListCadooArtifacts(ctx context.Context, pr *vcs.PullRequest) (vcs.PriorReview, error) {
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return vcs.PriorReview{}, err
	}
	const q = `query($owner:String!,$name:String!,$num:Int!,$tc:String,$rc:String){
	  repository(owner:$owner,name:$name){ pullRequest(number:$num){
	    comments(first:100,after:$tc){ nodes{ databaseId body }
	      pageInfo{ hasNextPage endCursor } }
	    reviewThreads(first:100,after:$rc){ nodes{ id isResolved line originalLine
	      comments(first:1){ nodes{ path body } } }
	      pageInfo{ hasNextPage endCursor } }
	  }}}`

	type gqlResp struct {
		Repository struct {
			PullRequest struct {
				Comments struct {
					Nodes []struct {
						DatabaseID int64  `json:"databaseId"`
						Body       string `json:"body"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"comments"`
				ReviewThreads struct {
					Nodes []struct {
						ID         string `json:"id"`
						IsResolved bool   `json:"isResolved"`
						// Line is the current-side line number for the thread. Nullable
						// per GitHub GraphQL schema (threads on deleted lines return null).
						Line *int `json:"line"`
						// OrigLine is the original line number before any rebase. Nullable
						// for the same reason as Line. Currently unused for suppression but
						// captured for future reference.
						OrigLine *int `json:"originalLine"`
						Comments struct {
							Nodes []struct {
								Path string `json:"path"`
								Body string `json:"body"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	var out vcs.PriorReview
	var tc, rc string
	// Per-connection done flags: stop processing a connection once it has no
	// next page, so an exhausted connection is never re-scanned.
	var doneC, doneT bool
	for {
		var r gqlResp
		vars := map[string]any{"owner": owner, "name": name, "num": int(pr.Number)}
		if tc != "" {
			vars["tc"] = tc
		}
		if rc != "" {
			vars["rc"] = rc
		}
		if err := doGraphQL(ctx, a.gqlClient, a.gqlEndpoint, q, vars, &r); err != nil {
			return vcs.PriorReview{}, err
		}
		p := r.Repository.PullRequest
		if !doneC {
			for _, c := range p.Comments.Nodes {
				if out.SummaryCommentID == "" && strings.Contains(c.Body, vcs.SummaryWrapperBegin) {
					out.SummaryCommentID = strconv.FormatInt(c.DatabaseID, 10)
					if out.LastReviewedSHA == "" {
						out.LastReviewedSHA = vcs.ParseReviewedSHA(c.Body)
					}
				}
			}
			if p.Comments.PageInfo.HasNextPage {
				tc = p.Comments.PageInfo.EndCursor
			} else {
				doneC = true
			}
		}
		if !doneT {
			for _, th := range p.ReviewThreads.Nodes {
				if len(th.Comments.Nodes) == 0 {
					continue
				}
				first := th.Comments.Nodes[0]
				md, stripped, ok := vcs.ParseInlineMarker(first.Body)
				if !ok {
					continue
				}
				orig := strings.TrimPrefix(stripped, formatSeverity(vcs.Severity(md.Sev)))
				// Nil-safe deref for line: GitHub returns null for threads on
				// deleted lines or threads created on older API versions (Pitfall 6).
				// Zero means "unknown anchor" — memoryStore.has guards r.Line > 0.
				var anchorLine int
				if th.Line != nil {
					anchorLine = *th.Line
				}
				out.Inline = append(out.Inline, vcs.PriorInline{
					Tool:            md.Tool,
					File:            first.Path,
					Severity:        md.Sev,
					StructuralKey:   md.SK,
					Title:           vcs.FirstLine(strings.TrimSpace(orig)),
					NormalizedTitle: md.NT,
					ExternalID:      th.ID,
					Resolved:        th.IsResolved,
					Line:            anchorLine,
					EndLine:         anchorLine,
				})
			}
			if p.ReviewThreads.PageInfo.HasNextPage {
				rc = p.ReviewThreads.PageInfo.EndCursor
			} else {
				doneT = true
			}
		}
		if doneC && doneT {
			break
		}
	}
	return out, nil
}

// ResolveThread resolves a GitHub review thread via the GraphQL
// resolveReviewThread mutation. threadID is the GraphQL node ID captured by
// ListCadooArtifacts. Empty id is a no-op (e.g. unrecoverable thread).
func (a *Adapter) ResolveThread(ctx context.Context, _ *vcs.PullRequest, threadID string) error {
	if threadID == "" {
		return nil
	}
	const m = `mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{id}}}`
	return doGraphQL(ctx, a.gqlClient, a.gqlEndpoint, m,
		map[string]any{"id": threadID}, nil)
}

// FetchArchive returns a gzipped tarball of the repo at ref. Used by the
// orchestrator to materialize a workspace for sandboxed linters. The
// archive URL is presigned by GitHub for ~5 minutes; we follow it with the
// default HTTP client (no auth on the redirect target).
func (a *Adapter) FetchArchive(ctx context.Context, repo, ref string) (io.ReadCloser, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	link, _, err := a.client.Repositories.GetArchiveLink(ctx, owner, name, gogithub.Tarball,
		&gogithub.RepositoryContentGetOptions{Ref: ref}, 5)
	if err != nil {
		return nil, fmt.Errorf("get archive link %s@%s: %w", repo, ref, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch archive %s@%s: %w", repo, ref, err)
	}
	if resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("fetch archive %s@%s: status %d", repo, ref, resp.StatusCode)
	}
	return resp.Body, nil
}

// FetchFileFromRef returns the raw contents of a file at a specific git ref
// (commit, branch, or tag). Used by the orchestrator to read .cadoo.yaml
// from the PR's head SHA. Not part of vcs.Provider — orchestrator
// type-asserts on *Adapter.
func (a *Adapter) FetchFileFromRef(ctx context.Context, repo, ref, path string) ([]byte, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	file, _, _, err := a.client.Repositories.GetContents(ctx, owner, name, path,
		&gogithub.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		return nil, fmt.Errorf("get contents %s@%s:%s: %w", repo, ref, path, err)
	}
	if file == nil {
		return nil, fmt.Errorf("%s@%s:%s is not a regular file", repo, ref, path)
	}
	content, err := file.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decode content: %w", err)
	}
	return []byte(content), nil
}

// UpsertCheckRun creates or updates the named check run on the head SHA.
func (a *Adapter) UpsertCheckRun(ctx context.Context, pr *vcs.PullRequest, run vcs.CheckRun) error {
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return err
	}
	output := &gogithub.CheckRunOutput{
		Title:   ptr(run.Title),
		Summary: ptr(run.Summary),
	}
	status, conclusion := mapCheckStatus(run.Status)

	existing, _, err := a.client.Checks.ListCheckRunsForRef(ctx, owner, name, pr.HeadSHA,
		&gogithub.ListCheckRunsOptions{CheckName: ptr(run.Name)})
	if err != nil {
		return fmt.Errorf("list check runs: %w", err)
	}

	if existing.GetTotal() > 0 {
		opts := gogithub.UpdateCheckRunOptions{
			Name:   run.Name,
			Status: ptr(status),
			Output: output,
		}
		if conclusion != "" {
			opts.Conclusion = ptr(conclusion)
		}
		if run.URL != "" {
			opts.DetailsURL = ptr(run.URL)
		}
		_, _, err = a.client.Checks.UpdateCheckRun(ctx, owner, name, existing.CheckRuns[0].GetID(), opts)
		return err
	}

	create := gogithub.CreateCheckRunOptions{
		Name:    run.Name,
		HeadSHA: pr.HeadSHA,
		Status:  ptr(status),
		Output:  output,
	}
	if conclusion != "" {
		create.Conclusion = ptr(conclusion)
	}
	if run.URL != "" {
		create.DetailsURL = ptr(run.URL)
	}
	_, _, err = a.client.Checks.CreateCheckRun(ctx, owner, name, create)
	return err
}

func splitRepo(full string) (string, string, error) {
	i := strings.IndexByte(full, '/')
	if i <= 0 || i == len(full)-1 {
		return "", "", fmt.Errorf("invalid repo %q (want owner/name)", full)
	}
	return full[:i], full[i+1:], nil
}

func convertPR(pr *gogithub.PullRequest, repo string, kind vcs.Kind) *vcs.PullRequest {
	return &vcs.PullRequest{
		Provider:     kind,
		RepoFullName: repo,
		Number:       int64(pr.GetNumber()),
		Title:        pr.GetTitle(),
		Body:         pr.GetBody(),
		Author:       pr.GetUser().GetLogin(),
		BaseSHA:      pr.GetBase().GetSHA(),
		HeadSHA:      pr.GetHead().GetSHA(),
		BaseRef:      pr.GetBase().GetRef(),
		HeadRef:      pr.GetHead().GetRef(),
		State:        pr.GetState(),
		URL:          pr.GetHTMLURL(),
		UpdatedAt:    pr.GetUpdatedAt().Time,
	}
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

func mapCheckStatus(s vcs.CheckRunStatus) (status, conclusion string) {
	switch s {
	case vcs.CheckQueued:
		return "queued", ""
	case vcs.CheckRunning:
		return "in_progress", ""
	case vcs.CheckSucceeded:
		return "completed", "success"
	case vcs.CheckFailed:
		return "completed", "failure"
	case vcs.CheckNeutral:
		return "completed", "neutral"
	}
	return "queued", ""
}

// ptr is a tiny generic pointer helper. go-github v66 ships the traditional
// per-type helpers (String, Int, Int64, Bool); we use this to keep call sites
// uniform regardless of element type.
func ptr[T any](v T) *T { return &v }

var _ vcs.Provider = (*Adapter)(nil)
