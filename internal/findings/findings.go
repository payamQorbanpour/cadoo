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
	"strings"
	"unicode"

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

// PostedFinding is the slim, prompt-friendly view of a previously-posted
// inline comment. The dispatcher feeds these back into the next tool run so
// the model knows not to restate them.
type PostedFinding struct {
	Tool      string
	File      string
	LineStart int
	LineEnd   int
	Severity  string
	Title     string
}

// SimilarTitleThreshold is the Jaccard score above which two normalized
// titles are treated as the same finding. Exported so tests (and future
// per-tool tuning) can override.
const SimilarTitleThreshold = 0.6

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

// Fingerprint hashes every field of an inline comment, body included. It is
// kept as the table's idempotence guard: identical re-runs of the same tool
// produce the same fingerprint and the INSERT becomes a no-op. Near-dup
// matching is the job of structuralKey + normalizeTitle below.
func Fingerprint(tool string, c vcs.InlineComment) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%s\x00%s",
		strings.ToLower(tool), strings.ToLower(c.File),
		c.LineStart, c.LineEnd,
		strings.ToLower(string(c.Severity)), c.Body)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// StructuralKey hashes only the stable location of a finding (tool, file,
// line range, severity) — deliberately excluding the body. Two LLM
// re-runs that rephrase the same finding at the same spot share a key, so
// the title-similarity check below can recognize them as duplicates.
func StructuralKey(tool string, c vcs.InlineComment) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%d\x00%d\x00%s",
		strings.ToLower(tool), strings.ToLower(c.File),
		c.LineStart, c.LineEnd,
		strings.ToLower(string(c.Severity)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// HasFinding reports whether a near-duplicate of this comment has already
// been posted on the PR. The lookup is two-stage: rows are filtered by the
// structural key (same tool/file/line/severity), then any candidate whose
// normalized title is sufficiently similar (Jaccard ≥ SimilarTitleThreshold)
// is treated as the same finding.
func (s *Store) HasFinding(ctx context.Context, key PRKey, tool string, c vcs.InlineComment) (bool, error) {
	if s == nil || s.pool == nil {
		return false, nil
	}
	sk := StructuralKey(tool, c)
	const q = `SELECT coalesce(normalized_title, lower(coalesce(title, '')))
		FROM posted_findings
		WHERE provider = $1 AND repo_full_name = $2 AND pr_number = $3
		  AND structural_key = $4`
	rows, err := s.pool.Query(ctx, q, key.Provider, key.RepoFullName, key.PRNumber, sk)
	if err != nil {
		return false, fmt.Errorf("has finding: %w", err)
	}
	defer rows.Close()
	wantTokens := titleTokens(c.Body)
	for rows.Next() {
		var stored string
		if err := rows.Scan(&stored); err != nil {
			return false, fmt.Errorf("scan finding: %w", err)
		}
		if jaccard(wantTokens, tokenize(stored)) >= SimilarTitleThreshold {
			return true, nil
		}
	}
	return false, rows.Err()
}

// RecordFinding stores the finding so future dispatches can dedup against it.
// External comment ID may be empty when the VCS API doesn't surface per-
// comment IDs (e.g. GitHub's Reviews API returns one review for many
// comments). The exact-fingerprint UNIQUE constraint absorbs accidental
// double-records on the same dispatch.
func (s *Store) RecordFinding(ctx context.Context, key PRKey, tool, externalCommentID string, c vcs.InlineComment) error {
	if s == nil || s.pool == nil {
		return nil
	}
	const q = `
INSERT INTO posted_findings(provider, repo_full_name, pr_number, tool, fingerprint,
                            structural_key, normalized_title,
                            severity, file, line_start, line_end, title, external_comment_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NULLIF($13, ''))
ON CONFLICT (provider, repo_full_name, pr_number, fingerprint) DO NOTHING`
	title := firstLine(c.Body)
	_, err := s.pool.Exec(ctx, q,
		key.Provider, key.RepoFullName, key.PRNumber, tool, Fingerprint(tool, c),
		StructuralKey(tool, c), normalizeTitle(c.Body),
		string(c.Severity), c.File, c.LineStart, c.LineEnd, title, externalCommentID,
	)
	if err != nil {
		return fmt.Errorf("record finding: %w", err)
	}
	return nil
}

// ListPostedFindings returns every inline finding Cadoo has posted on this
// PR. The dispatcher passes this into the next tool run so the model can
// avoid restating known issues.
func (s *Store) ListPostedFindings(ctx context.Context, key PRKey) ([]PostedFinding, error) {
	if s == nil || s.pool == nil {
		return nil, nil
	}
	const q = `SELECT tool, coalesce(file, ''), coalesce(line_start, 0),
	                  coalesce(line_end, 0), coalesce(severity, ''),
	                  coalesce(title, '')
		FROM posted_findings
		WHERE provider = $1 AND repo_full_name = $2 AND pr_number = $3
		ORDER BY file, line_start, line_end`
	rows, err := s.pool.Query(ctx, q, key.Provider, key.RepoFullName, key.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("list posted findings: %w", err)
	}
	defer rows.Close()
	var out []PostedFinding
	for rows.Next() {
		var f PostedFinding
		if err := rows.Scan(&f.Tool, &f.File, &f.LineStart, &f.LineEnd, &f.Severity, &f.Title); err != nil {
			return nil, fmt.Errorf("scan posted finding: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
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

// titleTokens extracts the title (first line of the comment body, stripped
// of common markdown emphasis and the leading `[SEVERITY]` tag) and
// tokenizes it.
func titleTokens(body string) []string {
	return tokenize(normalizeTitle(body))
}

// normalizeTitle returns a stable, lowercase, punctuation-stripped form of
// the comment's first line. Markdown bold (`**…**`) wrapping the title and
// a leading `[severity]` tag are removed so the same finding looks the same
// regardless of which tool added the decoration.
func normalizeTitle(body string) string {
	t := firstLine(body)
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "**")
	t = strings.TrimSuffix(t, "**")
	// Strip a leading "[whatever]" tag (e.g. "[WARN]", "[block]").
	if strings.HasPrefix(t, "[") {
		if end := strings.IndexByte(t, ']'); end > 0 {
			t = strings.TrimSpace(t[end+1:])
		}
	}
	return strings.ToLower(t)
}

// stopwords are filler that adds noise to title comparison without changing
// meaning. Tuned for code-review titles; intentionally small.
var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {},
	"be": {}, "by": {}, "for": {}, "from": {}, "has": {}, "have": {},
	"in": {}, "is": {}, "it": {}, "of": {}, "on": {}, "or": {},
	"that": {}, "the": {}, "this": {}, "to": {}, "was": {}, "with": {},
}

// tokenize splits on non-letter/digit runes, drops stopwords, returns a
// deduped set as a slice. Order is not stable but Jaccard doesn't care.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 8)
	field := func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}
	for _, tok := range strings.FieldsFunc(s, field) {
		if len(tok) < 2 {
			continue
		}
		if _, drop := stopwords[tok]; drop {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// jaccard is |A∩B| / |A∪B| over two token sets. Empty inputs yield 0 so a
// missing title never silently dedupes.
func jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(a))
	for _, t := range a {
		set[t] = struct{}{}
	}
	inter := 0
	bSet := make(map[string]struct{}, len(b))
	for _, t := range b {
		bSet[t] = struct{}{}
		if _, ok := set[t]; ok {
			inter++
		}
	}
	union := len(set) + len(bSet) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
