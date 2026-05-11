// Package vcs defines the provider-agnostic interface every Git-host adapter
// implements (github, github_enterprise, gitlab, ...). The orchestrator and
// review tools depend only on this interface.
package vcs

import (
	"context"
	"time"
)

// Kind identifies a VCS provider.
type Kind string

// Recognized provider kinds.
const (
	KindGitHub           Kind = "github"
	KindGitHubEnterprise Kind = "github_enterprise"
	KindGitLab           Kind = "gitlab"
)

// PullRequest is the normalized model Cadoo operates on (covers GH PRs and
// GL MRs).
type PullRequest struct {
	Provider     Kind
	InstallID    string
	RepoFullName string
	Number       int64
	Title        string
	Body         string
	Author       string
	BaseSHA      string
	HeadSHA      string
	BaseRef      string
	HeadRef      string
	State        string
	URL          string
	UpdatedAt    time.Time
}

// FileChange is one file's diff in a PR.
type FileChange struct {
	Path      string
	PrevPath  string
	Status    string // added | modified | removed | renamed
	Patch     string
	Additions int
	Deletions int
	IsBinary  bool
}

// Severity for inline findings.
type Severity string

// Severity levels — keep in sync with the CHECK constraint on findings.
const (
	SeverityBlock Severity = "block"
	SeverityWarn  Severity = "warn"
	SeverityNit   Severity = "nit"
)

// InlineComment is anchored to a specific line range in a file.
type InlineComment struct {
	File      string
	LineStart int
	LineEnd   int
	Body      string
	Severity  Severity
}

// CheckRunStatus is the high-level outcome posted to the VCS as a check.
type CheckRunStatus string

// Check run statuses.
const (
	CheckQueued    CheckRunStatus = "queued"
	CheckRunning   CheckRunStatus = "in_progress"
	CheckSucceeded CheckRunStatus = "success"
	CheckFailed    CheckRunStatus = "failure"
	CheckNeutral   CheckRunStatus = "neutral"
)

// CheckRun is the status reported back to the VCS.
type CheckRun struct {
	Name    string
	Status  CheckRunStatus
	Title   string
	Summary string
	URL     string
}

// Provider is the surface every VCS adapter implements.
type Provider interface {
	Kind() Kind

	FetchPullRequest(ctx context.Context, repo string, number int64) (*PullRequest, error)
	ListChangedFiles(ctx context.Context, pr *PullRequest) ([]FileChange, error)

	PostSummaryComment(ctx context.Context, pr *PullRequest, body string) (id string, err error)
	UpdateSummaryComment(ctx context.Context, pr *PullRequest, id, body string) error
	PostInlineComments(ctx context.Context, pr *PullRequest, comments []InlineComment) error

	// EditPullRequestBody replaces the PR/MR description. The orchestrator
	// uses this to inject Cadoo-managed sections (e.g. /describe) while
	// preserving the user's original text inside an explicit section block.
	EditPullRequestBody(ctx context.Context, pr *PullRequest, body string) error

	UpsertCheckRun(ctx context.Context, pr *PullRequest, run CheckRun) error
}
