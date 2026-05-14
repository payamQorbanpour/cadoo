// Package findings tracks comments Cadoo has posted on PRs so re-dispatches
// (e.g. on every `synchronize` event) edit existing comments instead of
// spamming new ones.
package findings

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	Tool              string
	File              string
	LineStart         int
	LineEnd           int
	Severity          string
	Title             string
	ExternalCommentID string
}

// SimilarTitleThreshold is the Jaccard score above which two normalized
// titles are treated as the same finding. Exported so tests (and future
// per-tool tuning) can override.
const SimilarTitleThreshold = 0.5

// Store wraps the posted_findings + posted_summaries tables. A nil Store is
// safe to call against; methods become no-ops returning zero values so the
// dispatcher's hot path doesn't need nil-checks. When `pool` is nil and
// `mem` is set, the same surface is backed by an in-memory map — used by
// deployments without a database (e.g. single-container docker setups).
type Store struct {
	pool *pgxpool.Pool
	mem  *memoryStore
}

// New builds a Postgres-backed Store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// NewMemory builds an in-memory Store. If persistPath is non-empty, the
// store hydrates from that JSON file on startup and rewrites it on every
// mutation, so dedup state survives container restarts. Hydration errors
// are logged-soft (start empty) rather than failing construction, so the
// process never refuses to boot just because the cache file is corrupt.
func NewMemory(persistPath string) *Store {
	return &Store{mem: newMemoryStore(persistPath)}
}

// DefaultCachePath returns a sensible persistence path for the in-memory
// findings store when no DB is configured. Uses os.UserCacheDir() so the
// file lands under ~/.cache/cadoo on Linux, ~/Library/Caches/cadoo on
// macOS, or %LocalAppData%\cadoo on Windows. Returns "" if no cache
// directory can be determined — callers should treat that as "stay
// process-local".
func DefaultCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "cadoo", "findings.json")
}

// Enabled reports whether this Store can read/write — either backend is
// fine. Callers use this to branch into a no-dedup fallback when neither
// is set (e.g. a literal nil *Store).
func (s *Store) Enabled() bool { return s != nil && (s.pool != nil || s.mem != nil) }

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

// StructuralKey hashes the stable identity of a finding: tool, file,
// severity, and normalized title. Line numbers are intentionally NOT in
// the key — when a later commit shifts a finding's anchor, the key stays
// the same so HasFinding still recognises it as a duplicate.
func StructuralKey(tool string, c vcs.InlineComment) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s",
		strings.ToLower(tool), strings.ToLower(c.File),
		strings.ToLower(string(c.Severity)), normalizeTitle(c.Body))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// HasFinding reports whether a near-duplicate of this comment has already
// been posted on the PR. Two-stage match:
//   - Exact match on StructuralKey (tool, file, severity, normalized title)
//     — catches re-runs that produce identical titles.
//   - Jaccard ≥ SimilarTitleThreshold over the normalized title against
//     every prior comment in the same (tool, file, severity) bucket —
//     catches rephrasings that change a word or two.
func (s *Store) HasFinding(ctx context.Context, key PRKey, tool string, c vcs.InlineComment) (bool, error) {
	if s == nil {
		return false, nil
	}
	if s.mem != nil {
		return s.mem.has(key, tool, c), nil
	}
	if s.pool == nil {
		return false, nil
	}
	const q = `SELECT structural_key,
	                  coalesce(normalized_title, lower(coalesce(title, '')))
		FROM posted_findings
		WHERE provider = $1 AND repo_full_name = $2 AND pr_number = $3
		  AND tool = $4 AND file = $5 AND severity = $6`
	rows, err := s.pool.Query(ctx, q,
		key.Provider, key.RepoFullName, key.PRNumber,
		tool, c.File, string(c.Severity))
	if err != nil {
		return false, fmt.Errorf("has finding: %w", err)
	}
	defer rows.Close()
	wantKey := StructuralKey(tool, c)
	wantTokens := titleTokens(c.Body)
	for rows.Next() {
		var storedKey, storedTitle string
		if err := rows.Scan(&storedKey, &storedTitle); err != nil {
			return false, fmt.Errorf("scan finding: %w", err)
		}
		if storedKey == wantKey {
			return true, nil
		}
		if jaccard(wantTokens, tokenize(storedTitle)) >= SimilarTitleThreshold {
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
	if s == nil {
		return nil
	}
	if s.mem != nil {
		s.mem.record(key, tool, externalCommentID, c)
		return nil
	}
	if s.pool == nil {
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
	if s == nil {
		return nil, nil
	}
	if s.mem != nil {
		return s.mem.list(key), nil
	}
	if s.pool == nil {
		return nil, nil
	}
	const q = `SELECT tool, coalesce(file, ''), coalesce(line_start, 0),
	                  coalesce(line_end, 0), coalesce(severity, ''),
	                  coalesce(title, ''), coalesce(external_comment_id, '')
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
		if err := rows.Scan(&f.Tool, &f.File, &f.LineStart, &f.LineEnd,
			&f.Severity, &f.Title, &f.ExternalCommentID); err != nil {
			return nil, fmt.Errorf("scan posted finding: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SummaryID returns the prior summary-comment external ID for (PR, tool), or
// empty if none.
func (s *Store) SummaryID(ctx context.Context, key PRKey, tool string) (string, error) {
	if s == nil {
		return "", nil
	}
	if s.mem != nil {
		return s.mem.summaryID(key, tool), nil
	}
	if s.pool == nil {
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
	if s == nil {
		return nil
	}
	if s.mem != nil {
		s.mem.putSummaryID(key, tool, externalCommentID)
		return nil
	}
	if s.pool == nil {
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
	if s == nil {
		return nil
	}
	if s.mem != nil {
		s.mem.putSection(key, tool, body)
		return nil
	}
	if s.pool == nil {
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
	if s == nil {
		return nil, nil
	}
	if s.mem != nil {
		return s.mem.allSections(key), nil
	}
	if s.pool == nil {
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

// normalizeTitle returns a lowercased fingerprint of the comment's
// user-facing content, used by both StructuralKey (for exact match) and
// titleTokens (for the Jaccard rephrase check). It walks up to ~200
// characters of body, drops fenced code blocks, and strips markdown
// bullets, bold wrappers, and a leading `[severity]` tag — so the same
// finding looks the same regardless of which tool added the decoration,
// and tools like `improve` whose every body shares a static
// `**Suggestions:**` header still produce a key that varies with the
// actual suggestion content.
func normalizeTitle(body string) string {
	const budget = 200
	var sb strings.Builder
	inFence := false
	for raw := range strings.SplitSeq(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" {
			continue
		}
		// Strip markdown bullet markers, bold wrappers, and a leading
		// [tag] (e.g. "[WARN]") so rephrasings still share tokens.
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "**")
		line = strings.TrimSuffix(line, "**")
		if strings.HasPrefix(line, "[") {
			if end := strings.IndexByte(line, ']'); end > 0 {
				line = strings.TrimSpace(line[end+1:])
			}
		}
		if line == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(line)
		if sb.Len() >= budget {
			break
		}
	}
	s := sb.String()
	if len(s) > budget {
		s = s[:budget]
	}
	return strings.ToLower(s)
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

// memoryStore is the no-DB backend for Store. It is intentionally
// process-local; a docker container that restarts will start with an empty
// cache (and may re-post one batch of comments) unless a persistPath was
// supplied. The mutex covers every map access — webhook events for the
// same PR can land concurrently on a single worker.
type memoryStore struct {
	mu        sync.Mutex
	findings  map[PRKey][]findingRec
	summaries map[summaryRefKey]string
	sections  map[PRKey]map[string]string
	path      string
}

type findingRec struct {
	Tool            string `json:"tool"`
	File            string `json:"file"`
	Severity        string `json:"severity"`
	StructuralKey   string `json:"sk"`
	Fingerprint     string `json:"fp"`
	NormalizedTitle string `json:"nt"`
	Title           string `json:"title"`
	LineStart       int    `json:"ls"`
	LineEnd         int    `json:"le"`
	ExternalID      string `json:"eid,omitempty"`
}

type summaryRefKey struct {
	PR   PRKey
	Tool string
}

// persistedState is the on-disk JSON shape. Maps with struct keys can't be
// JSON-encoded directly, so PRKey is flattened into a string of the form
// "provider|repo|number".
type persistedState struct {
	Findings  map[string][]findingRec      `json:"findings"`
	Summaries map[string]string            `json:"summaries"`
	Sections  map[string]map[string]string `json:"sections"`
}

func newMemoryStore(path string) *memoryStore {
	m := &memoryStore{
		findings:  map[PRKey][]findingRec{},
		summaries: map[summaryRefKey]string{},
		sections:  map[PRKey]map[string]string{},
		path:      path,
	}
	m.load()
	return m
}

func (m *memoryStore) has(key PRKey, tool string, c vcs.InlineComment) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	wantKey := StructuralKey(tool, c)
	wantTokens := titleTokens(c.Body)
	for _, r := range m.findings[key] {
		if r.Tool != tool || r.File != c.File || r.Severity != string(c.Severity) {
			continue
		}
		if r.StructuralKey == wantKey {
			return true
		}
		if jaccard(wantTokens, tokenize(r.NormalizedTitle)) >= SimilarTitleThreshold {
			return true
		}
	}
	return false
}

func (m *memoryStore) record(key PRKey, tool, externalID string, c vcs.InlineComment) {
	rec := findingRec{
		Tool:            tool,
		File:            c.File,
		Severity:        string(c.Severity),
		StructuralKey:   StructuralKey(tool, c),
		Fingerprint:     Fingerprint(tool, c),
		NormalizedTitle: normalizeTitle(c.Body),
		Title:           firstLine(c.Body),
		LineStart:       c.LineStart,
		LineEnd:         c.LineEnd,
		ExternalID:      externalID,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.findings[key] {
		if existing.Fingerprint == rec.Fingerprint {
			// Idempotent: same exact comment already recorded. If the
			// caller has now learned the external ID (earlier record
			// happened with empty), backfill it so future resolves work.
			if existing.ExternalID == "" && externalID != "" {
				m.findings[key][i].ExternalID = externalID
				m.persist()
			}
			return
		}
	}
	m.findings[key] = append(m.findings[key], rec)
	m.persist()
}

func (m *memoryStore) list(key PRKey) []PostedFinding {
	m.mu.Lock()
	defer m.mu.Unlock()
	recs := m.findings[key]
	if len(recs) == 0 {
		return nil
	}
	out := make([]PostedFinding, 0, len(recs))
	for _, r := range recs {
		out = append(out, PostedFinding{
			Tool:              r.Tool,
			File:              r.File,
			LineStart:         r.LineStart,
			LineEnd:           r.LineEnd,
			Severity:          r.Severity,
			Title:             r.Title,
			ExternalCommentID: r.ExternalID,
		})
	}
	return out
}

func (m *memoryStore) summaryID(key PRKey, tool string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.summaries[summaryRefKey{PR: key, Tool: tool}]
}

func (m *memoryStore) putSummaryID(key PRKey, tool, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summaries[summaryRefKey{PR: key, Tool: tool}] = id
	m.persist()
}

func (m *memoryStore) putSection(key PRKey, tool, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sections[key] == nil {
		m.sections[key] = map[string]string{}
	}
	m.sections[key][tool] = body
	m.persist()
}

func (m *memoryStore) allSections(key PRKey) []Section {
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.sections[key]
	if len(bucket) == 0 {
		return nil
	}
	// Stable order: sort by tool name to match the DB ORDER BY tool.
	tools := make([]string, 0, len(bucket))
	for t, body := range bucket {
		if t == WrapperToolKey || body == "" {
			continue
		}
		tools = append(tools, t)
	}
	// insertion-sort: bucket is tiny in practice (a handful of tools).
	for i := 1; i < len(tools); i++ {
		for j := i; j > 0 && tools[j] < tools[j-1]; j-- {
			tools[j], tools[j-1] = tools[j-1], tools[j]
		}
	}
	out := make([]Section, 0, len(tools))
	for _, t := range tools {
		out = append(out, Section{Tool: t, Body: bucket[t]})
	}
	return out
}

// load hydrates the in-memory maps from the JSON file at m.path. Missing
// file is fine; malformed file is logged-soft by leaving the store empty so
// the process can still boot.
func (m *memoryStore) load() {
	if m.path == "" {
		return
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		return // missing or unreadable — start empty
	}
	var p persistedState
	if err := json.Unmarshal(data, &p); err != nil {
		return // corrupt file — start empty rather than refusing to boot
	}
	for encoded, recs := range p.Findings {
		if k, ok := decodePRKey(encoded); ok {
			m.findings[k] = recs
		}
	}
	for encoded, id := range p.Summaries {
		k, tool, ok := decodeSummaryKey(encoded)
		if !ok {
			continue
		}
		m.summaries[summaryRefKey{PR: k, Tool: tool}] = id
	}
	for encoded, body := range p.Sections {
		if k, ok := decodePRKey(encoded); ok {
			m.sections[k] = body
		}
	}
}

// persist writes the maps back to m.path. Best-effort: errors are ignored
// because the next mutation will retry, and an unwritable cache file
// shouldn't crash the dispatcher.
func (m *memoryStore) persist() {
	if m.path == "" {
		return
	}
	p := persistedState{
		Findings:  map[string][]findingRec{},
		Summaries: map[string]string{},
		Sections:  map[string]map[string]string{},
	}
	for k, recs := range m.findings {
		p.Findings[encodePRKey(k)] = recs
	}
	for k, id := range m.summaries {
		p.Summaries[encodeSummaryKey(k.PR, k.Tool)] = id
	}
	for k, body := range m.sections {
		p.Sections[encodePRKey(k)] = body
	}
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, m.path) // atomic-ish replace
}

func encodePRKey(k PRKey) string {
	return fmt.Sprintf("%s|%s|%d", k.Provider, k.RepoFullName, k.PRNumber)
}

func decodePRKey(s string) (PRKey, bool) {
	parts := strings.SplitN(s, "|", 3)
	if len(parts) != 3 {
		return PRKey{}, false
	}
	var n int64
	if _, err := fmt.Sscanf(parts[2], "%d", &n); err != nil {
		return PRKey{}, false
	}
	return PRKey{Provider: parts[0], RepoFullName: parts[1], PRNumber: n}, true
}

func encodeSummaryKey(k PRKey, tool string) string {
	return encodePRKey(k) + "|" + tool
}

func decodeSummaryKey(s string) (PRKey, string, bool) {
	parts := strings.SplitN(s, "|", 4)
	if len(parts) != 4 {
		return PRKey{}, "", false
	}
	var n int64
	if _, err := fmt.Sscanf(parts[2], "%d", &n); err != nil {
		return PRKey{}, "", false
	}
	return PRKey{Provider: parts[0], RepoFullName: parts[1], PRNumber: n}, parts[3], true
}
