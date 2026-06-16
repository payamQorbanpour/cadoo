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

	glab "gitlab.com/gitlab-org/api/client-go"

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

// BaseURL returns the scheme+host of this GitLab instance with no trailing
// slash. For GitLab.com it returns "https://gitlab.com"; for self-managed
// instances it strips any trailing slash from cfg.BaseURL.
func (a *Adapter) BaseURL() string {
	if a.cfg.BaseURL == "" {
		return "https://gitlab.com"
	}
	return strings.TrimRight(a.cfg.BaseURL, "/")
}

// FetchPullRequest implements vcs.Provider. The PR number is the MR IID.
func (a *Adapter) FetchPullRequest(ctx context.Context, repo string, number int64) (*vcs.PullRequest, error) {
	mr, _, err := a.client.MergeRequests.GetMergeRequest(repo, number,
		&glab.GetMergeRequestsOptions{}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get mr %s!%d: %w", repo, number, err)
	}
	return convertMR(mr, repo), nil
}

// ListChangedFiles implements vcs.Provider via the MR diffs endpoint.
func (a *Adapter) ListChangedFiles(ctx context.Context, pr *vcs.PullRequest) ([]vcs.FileChange, error) {
	diffs, _, err := a.client.MergeRequests.ListMergeRequestDiffs(pr.RepoFullName, pr.Number,
		&glab.ListMergeRequestDiffsOptions{}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("list mr diffs %s!%d: %w", pr.RepoFullName, pr.Number, err)
	}
	out := make([]vcs.FileChange, 0, len(diffs))
	for _, c := range diffs {
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
	note, _, err := a.client.Notes.CreateMergeRequestNote(pr.RepoFullName, pr.Number,
		&glab.CreateMergeRequestNoteOptions{Body: ptr(body)}, glab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("create mr note: %w", err)
	}
	return strconv.FormatInt(note.ID, 10), nil
}

// UpdateSummaryComment edits an existing MR note.
func (a *Adapter) UpdateSummaryComment(ctx context.Context, pr *vcs.PullRequest, id, body string) error {
	nid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid note id %q: %w", id, err)
	}
	_, _, err = a.client.Notes.UpdateMergeRequestNote(pr.RepoFullName, pr.Number, nid,
		&glab.UpdateMergeRequestNoteOptions{Body: ptr(body)}, glab.WithContext(ctx))
	return err
}

// EditPullRequestBody replaces the MR description.
func (a *Adapter) EditPullRequestBody(ctx context.Context, pr *vcs.PullRequest, body string) error {
	_, _, err := a.client.MergeRequests.UpdateMergeRequest(pr.RepoFullName, pr.Number,
		&glab.UpdateMergeRequestOptions{Description: ptr(body)}, glab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("update mr description: %w", err)
	}
	return nil
}

// PostInlineComments creates one MR discussion per inline comment, anchored
// to a position object built from the MR's diff_refs. Comments whose target
// line falls outside any diff hunk are downgraded to top-level MR notes (with
// the file:line in the body) so they aren't silently lost — GitLab rejects
// positions it can't compute a line_code for. Failures on individual comments
// are returned as the first error after a best-effort attempt at the rest.
func (a *Adapter) PostInlineComments(ctx context.Context, pr *vcs.PullRequest, comments []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	if len(comments) == 0 {
		return nil, nil
	}
	mr, _, err := a.client.MergeRequests.GetMergeRequest(pr.RepoFullName, pr.Number,
		&glab.GetMergeRequestsOptions{}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("get mr for diff refs: %w", err)
	}
	if mr.DiffRefs.HeadSha == "" {
		return nil, fmt.Errorf("mr %s!%d has no diff_refs", pr.RepoFullName, pr.Number)
	}

	diffs, _, err := a.client.MergeRequests.ListMergeRequestDiffs(pr.RepoFullName, pr.Number,
		&glab.ListMergeRequestDiffsOptions{}, glab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("list mr diffs for positions: %w", err)
	}
	idx := indexDiffs(diffs)

	refs := make([]vcs.PostedInlineRef, 0, len(comments))
	var firstErr error
	for _, c := range comments {
		body := formatSeverity(c.Severity) + c.Body
		line := c.LineEnd
		if line == 0 {
			line = c.LineStart
		}

		anchor, ok := idx.lookup(c.File, line)
		if !ok {
			// Target line isn't in any hunk for this file — GitLab would
			// reject the position. Post a non-positional note instead so the
			// finding still reaches the MR. Unanchored notes have no
			// "resolve" concept, so we return an empty ExternalID and let
			// the caller skip them on cleanup.
			if err := a.postUnanchoredNote(ctx, pr, c.File, line, body); err != nil && firstErr == nil {
				firstErr = err
			}
			refs = append(refs, vcs.PostedInlineRef{Comment: c})
			continue
		}

		pos := &glab.PositionOptions{
			BaseSHA:      ptr(mr.DiffRefs.BaseSha),
			StartSHA:     ptr(mr.DiffRefs.StartSha),
			HeadSHA:      ptr(mr.DiffRefs.HeadSha),
			PositionType: ptr("text"),
			NewPath:      ptr(anchor.newPath),
			OldPath:      ptr(anchor.oldPath),
		}
		if anchor.newLine > 0 {
			pos.NewLine = ptr(int64(anchor.newLine))
		}
		// Context lines need both new_line and old_line so GitLab can compute
		// a stable line_code; deleted lines only carry old_line.
		if anchor.oldLine > 0 && (anchor.newLine == 0 || anchor.context) {
			pos.OldLine = ptr(int64(anchor.oldLine))
		}
		opts := &glab.CreateMergeRequestDiscussionOptions{
			Body:     ptr(body),
			Position: pos,
		}
		disc, _, err := a.client.Discussions.CreateMergeRequestDiscussion(
			pr.RepoFullName, pr.Number, opts, glab.WithContext(ctx),
		)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("create discussion at %s:%d: %w", c.File, line, err)
			}
			refs = append(refs, vcs.PostedInlineRef{Comment: c})
			continue
		}
		id := ""
		if disc != nil {
			id = disc.ID
		}
		refs = append(refs, vcs.PostedInlineRef{Comment: c, ExternalID: id})
	}
	return refs, firstErr
}

// postUnanchoredNote posts a regular MR note for a finding whose target line
// is outside the diff, so the feedback isn't dropped when GitLab refuses the
// position.
func (a *Adapter) postUnanchoredNote(ctx context.Context, pr *vcs.PullRequest, file string, line int, body string) error {
	loc := file
	if line > 0 {
		loc = fmt.Sprintf("%s:%d", file, line)
	}
	text := fmt.Sprintf("`%s` (outside diff)\n\n%s", loc, body)
	_, _, err := a.client.Notes.CreateMergeRequestNote(pr.RepoFullName, pr.Number,
		&glab.CreateMergeRequestNoteOptions{Body: ptr(text)}, glab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("create note for %s: %w", loc, err)
	}
	return nil
}

// ListCadooArtifacts implements vcs.PriorReviewReader. It walks the MR's
// discussions, recognising Cadoo's own comments by the hidden marker (inline
// findings) and by SummaryWrapperBegin (the overview note), so stateless
// CI-mode can rebuild dedup state from the MR itself.
func (a *Adapter) ListCadooArtifacts(ctx context.Context, pr *vcs.PullRequest) (vcs.PriorReview, error) {
	var out vcs.PriorReview
	opt := &glab.ListMergeRequestDiscussionsOptions{}
	opt.ListOptions = glab.ListOptions{PerPage: 100, Page: 1}
	for {
		discs, resp, err := a.client.Discussions.ListMergeRequestDiscussions(
			pr.RepoFullName, pr.Number, opt, glab.WithContext(ctx))
		if err != nil {
			return vcs.PriorReview{}, fmt.Errorf("list mr discussions: %w", err)
		}
		for _, d := range discs {
			for _, n := range d.Notes {
				if n == nil || n.System {
					continue
				}
				md, stripped, ok := vcs.ParseInlineMarker(n.Body)
				if ok && n.Position != nil {
					file := n.Position.NewPath
					if file == "" {
						file = n.Position.OldPath
					}
					orig := strings.TrimPrefix(stripped, formatSeverity(vcs.Severity(md.Sev)))
					out.Inline = append(out.Inline, vcs.PriorInline{
						Tool:            md.Tool,
						File:            file,
						Severity:        md.Sev,
						StructuralKey:   md.SK,
						Title:           vcs.FirstLine(strings.TrimSpace(orig)),
						NormalizedTitle: md.NT,
						ExternalID:      d.ID,
						Resolved:        n.Resolved,
						Line:            int(n.Position.NewLine),
						EndLine:         int(n.Position.NewLine),
					})
					continue
				}
				if ok && n.Position == nil {
					// Unanchored note: posted for a line outside the diff at the
					// time. The note body starts with "`file:line` (outside diff)"
					// — extract the file path so dedup suppresses re-posts on
					// subsequent runs. ExternalID is empty because unanchored notes
					// have no resolvable discussion thread in GitLab.
					file := parseUnanchoredFile(vcs.FirstLine(n.Body))
					orig := strings.TrimPrefix(stripped, formatSeverity(vcs.Severity(md.Sev)))
					out.Inline = append(out.Inline, vcs.PriorInline{
						Tool:            md.Tool,
						File:            file,
						Severity:        md.Sev,
						StructuralKey:   md.SK,
						Title:           vcs.FirstLine(strings.TrimSpace(orig)),
						NormalizedTitle: md.NT,
						// ExternalID intentionally empty: unanchored notes have no
						// resolvable discussion thread.
					})
					continue
				}
				if n.Position == nil && strings.Contains(n.Body, vcs.SummaryWrapperBegin) {
					out.SummaryCommentID = strconv.FormatInt(n.ID, 10)
					out.LastReviewedSHA = vcs.ParseReviewedSHA(n.Body)
				}
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}

// ResolveThread marks the given MR discussion as resolved. threadID is the
// discussion ID returned by PostInlineComments. Unanchored notes (which
// have no discussion ID and no "resolved" concept in GitLab) are silently
// skipped — callers pass an empty threadID for them.
func (a *Adapter) ResolveThread(ctx context.Context, pr *vcs.PullRequest, threadID string) error {
	if threadID == "" {
		return nil
	}
	_, _, err := a.client.Discussions.ResolveMergeRequestDiscussion(
		pr.RepoFullName, pr.Number, threadID,
		&glab.ResolveMergeRequestDiscussionOptions{Resolved: ptr(true)},
		glab.WithContext(ctx),
	)
	if err != nil {
		return fmt.Errorf("resolve discussion %s: %w", threadID, err)
	}
	return nil
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

// diffAnchor describes a line of an MR diff that an inline comment can be
// anchored to. context=true means the line is unchanged in the hunk, which
// requires both new_line and old_line in the position payload.
type diffAnchor struct {
	newPath string
	oldPath string
	newLine int
	oldLine int
	context bool
}

// diffIndex maps (file, new_line) -> anchor for every line present in the MR
// diff. Built once per PostInlineComments call so each comment can decide
// whether GitLab will accept a position for it.
type diffIndex struct {
	byFile map[string]map[int]diffAnchor
}

func (i *diffIndex) lookup(file string, newLine int) (diffAnchor, bool) {
	if i == nil || file == "" || newLine <= 0 {
		return diffAnchor{}, false
	}
	lines, ok := i.byFile[file]
	if !ok {
		return diffAnchor{}, false
	}
	a, ok := lines[newLine]
	return a, ok
}

// indexDiffs walks every MR diff and indexes each hunk line by its new-file
// line number so callers can answer "is this line in the diff, and how should
// I anchor a comment to it?".
func indexDiffs(diffs []*glab.MergeRequestDiff) *diffIndex {
	idx := &diffIndex{byFile: make(map[string]map[int]diffAnchor, len(diffs))}
	for _, d := range diffs {
		if d == nil || d.Diff == "" {
			continue
		}
		newPath := d.NewPath
		if newPath == "" {
			newPath = d.OldPath
		}
		oldPath := d.OldPath
		if oldPath == "" {
			oldPath = newPath
		}
		key := newPath
		if key == "" {
			continue
		}
		lines := idx.byFile[key]
		if lines == nil {
			lines = make(map[int]diffAnchor)
			idx.byFile[key] = lines
		}
		var newNo, oldNo int
		inHunk := false
		for _, raw := range strings.Split(d.Diff, "\n") {
			if strings.HasPrefix(raw, "@@") {
				o, n, ok := parseHunkHeader(raw)
				if !ok {
					inHunk = false
					continue
				}
				oldNo, newNo = o, n
				inHunk = true
				continue
			}
			if !inHunk || strings.HasPrefix(raw, "+++") || strings.HasPrefix(raw, "---") || strings.HasPrefix(raw, "\\") {
				continue
			}
			switch {
			case strings.HasPrefix(raw, "+"):
				lines[newNo] = diffAnchor{newPath: newPath, oldPath: oldPath, newLine: newNo}
				newNo++
			case strings.HasPrefix(raw, "-"):
				// Deleted lines have no new_line slot, so the orchestrator's
				// new-file-line addressing can't reach them — skip.
				oldNo++
			default:
				lines[newNo] = diffAnchor{newPath: newPath, oldPath: oldPath, newLine: newNo, oldLine: oldNo, context: true}
				newNo++
				oldNo++
			}
		}
	}
	return idx
}

// parseHunkHeader extracts (oldStart, newStart) from a unified-diff hunk
// header like "@@ -12,3 +14,5 @@ optional section".
func parseHunkHeader(h string) (oldStart, newStart int, ok bool) {
	rest := strings.TrimPrefix(h, "@@")
	if i := strings.Index(rest, "@@"); i >= 0 {
		rest = rest[:i]
	}
	var sawOld, sawNew bool
	for _, p := range strings.Fields(rest) {
		switch {
		case strings.HasPrefix(p, "-"):
			n := strings.TrimPrefix(p, "-")
			if c := strings.IndexByte(n, ','); c >= 0 {
				n = n[:c]
			}
			v, err := strconv.Atoi(n)
			if err != nil {
				return 0, 0, false
			}
			oldStart = v
			sawOld = true
		case strings.HasPrefix(p, "+"):
			n := strings.TrimPrefix(p, "+")
			if c := strings.IndexByte(n, ','); c >= 0 {
				n = n[:c]
			}
			v, err := strconv.Atoi(n)
			if err != nil {
				return 0, 0, false
			}
			newStart = v
			sawNew = true
		}
	}
	return oldStart, newStart, sawOld && sawNew
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

// parseUnanchoredFile extracts the file path from the first line of an
// unanchored note body, which has the format "`file:line` (outside diff)" or
// "`file` (outside diff)". Returns "" if the format doesn't match.
func parseUnanchoredFile(firstLine string) string {
	if !strings.HasPrefix(firstLine, "`") {
		return ""
	}
	rest := firstLine[1:]
	end := strings.IndexByte(rest, '`')
	if end < 0 {
		return ""
	}
	loc := rest[:end]
	// Strip optional ":line" suffix.
	if i := strings.LastIndexByte(loc, ':'); i > 0 {
		loc = loc[:i]
	}
	return loc
}

// ptr is a generic pointer helper. Mirrors the pattern used in the github
// adapter so call sites stay uniform regardless of element type.
func ptr[T any](v T) *T { return &v }

var _ vcs.Provider = (*Adapter)(nil)
