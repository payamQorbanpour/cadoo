// Package findings tracks comments Cadoo has posted on PRs so re-dispatches
// (e.g. on every `synchronize` event) edit existing comments instead of
// spamming new ones.
package findings

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// PRKey identifies a pull request without leaning on a normalized DB row.
type PRKey struct {
	Provider     string // matches vcs.Kind string form
	RepoFullName string
	PRNumber     int64
}

// Store wraps the posted_findings + posted_summaries tables. A nil Store is
// safe to call against; methods become no-ops returning zero values so the
// dispatcher's hot path doesn't need nil-checks.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Enabled reports whether this Store can read/write — i.e. it has a non-nil
// pgx pool. Callers use this to branch into a no-DB fallback path.
func (s *Store) Enabled() bool { return s != nil && s.pool != nil }

// Fingerprint hashes the stable fields of an inline comment so re-dispatches
// can recognize "already posted". Body is included so a tool that legitimately
// rephrases a finding will surface the rephrasing as a fresh signal.
func Fingerprint(tool string, c vcs.InlineComment) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%s\x00%s",
		tool, c.File, c.LineStart, c.LineEnd, c.Severity, c.Body)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// HasFinding reports whether a finding with this fingerprint has already been
// posted for the PR. nil Store returns false.
func (s *Store) HasFinding(ctx context.Context, key PRKey, fingerprint string) (bool, error) {
	if s == nil || s.pool == nil {
		return false, nil
	}
	const q = `SELECT EXISTS (
		SELECT 1 FROM posted_findings
		WHERE provider = $1 AND repo_full_name = $2 AND pr_number = $3 AND fingerprint = $4
	)`
	var exists bool
	if err := s.pool.QueryRow(ctx, q, key.Provider, key.RepoFullName, key.PRNumber, fingerprint).Scan(&exists); err != nil {
		return false, fmt.Errorf("has finding: %w", err)
	}
	return exists, nil
}

// RecordFinding stores the finding so future dispatches can dedup against it.
// External comment ID may be empty when the VCS API doesn't surface per-comment
// IDs (e.g. GitHub's Reviews API returns one review for many comments).
func (s *Store) RecordFinding(ctx context.Context, key PRKey, tool, fingerprint, externalCommentID string, c vcs.InlineComment) error {
	if s == nil || s.pool == nil {
		return nil
	}
	const q = `
INSERT INTO posted_findings(provider, repo_full_name, pr_number, tool, fingerprint,
                            severity, file, line_start, line_end, title, external_comment_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''))
ON CONFLICT (provider, repo_full_name, pr_number, fingerprint) DO NOTHING`
	title := firstLine(c.Body)
	_, err := s.pool.Exec(ctx, q,
		key.Provider, key.RepoFullName, key.PRNumber, tool, fingerprint,
		string(c.Severity), c.File, c.LineStart, c.LineEnd, title, externalCommentID,
	)
	if err != nil {
		return fmt.Errorf("record finding: %w", err)
	}
	return nil
}

// SummaryID returns the prior summary-comment external ID for (PR, tool), or
// empty if none.
func (s *Store) SummaryID(ctx context.Context, key PRKey, tool string) (string, error) {
	if s == nil || s.pool == nil {
		return "", nil
	}
	const q = `SELECT external_comment_id FROM posted_summaries
		WHERE provider = $1 AND repo_full_name = $2 AND pr_number = $3 AND tool = $4`
	var id string
	if err := s.pool.QueryRow(ctx, q, key.Provider, key.RepoFullName, key.PRNumber, tool).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("summary id: %w", err)
	}
	return id, nil
}

// PutSummaryID upserts the (PR, tool) → comment ID mapping.
func (s *Store) PutSummaryID(ctx context.Context, key PRKey, tool, externalCommentID string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	const q = `
INSERT INTO posted_summaries(provider, repo_full_name, pr_number, tool, external_comment_id, body)
VALUES ($1, $2, $3, $4, $5, '')
ON CONFLICT (provider, repo_full_name, pr_number, tool) DO UPDATE
  SET external_comment_id = EXCLUDED.external_comment_id, updated_at = now()`
	_, err := s.pool.Exec(ctx, q, key.Provider, key.RepoFullName, key.PRNumber, tool, externalCommentID)
	if err != nil {
		return fmt.Errorf("put summary id: %w", err)
	}
	return nil
}

// WrapperToolKey is the sentinel `tool` value used to track the consolidated
// summary comment that wraps every tool's section. Tool names never collide
// with the empty string.
const WrapperToolKey = ""

// Section is one tool's rendered fragment inside the consolidated comment.
type Section struct {
	Tool string
	Body string
}

// PutSection upserts the rendered body for a single tool's section. The
// wrapper comment is identified by tool == WrapperToolKey, and stored without
// a body (its external ID points at the comment that contains every section).
func (s *Store) PutSection(ctx context.Context, key PRKey, tool, body string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	const q = `
INSERT INTO posted_summaries(provider, repo_full_name, pr_number, tool, external_comment_id, body)
VALUES ($1, $2, $3, $4, '', $5)
ON CONFLICT (provider, repo_full_name, pr_number, tool) DO UPDATE
  SET body = EXCLUDED.body, updated_at = now()`
	_, err := s.pool.Exec(ctx, q, key.Provider, key.RepoFullName, key.PRNumber, tool, body)
	if err != nil {
		return fmt.Errorf("put section: %w", err)
	}
	return nil
}

// AllSections returns every tool section recorded for this PR, ordered by
// tool name. Sections whose body is empty (e.g. wrapper bookkeeping rows)
// are skipped.
func (s *Store) AllSections(ctx context.Context, key PRKey) ([]Section, error) {
	if s == nil || s.pool == nil {
		return nil, nil
	}
	const q = `SELECT tool, body FROM posted_summaries
		WHERE provider = $1 AND repo_full_name = $2 AND pr_number = $3
		  AND tool <> '' AND body <> ''
		ORDER BY tool`
	rows, err := s.pool.Query(ctx, q, key.Provider, key.RepoFullName, key.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("all sections: %w", err)
	}
	defer rows.Close()
	var out []Section
	for rows.Next() {
		var sec Section
		if err := rows.Scan(&sec.Tool, &sec.Body); err != nil {
			return nil, fmt.Errorf("scan section: %w", err)
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
