// Package learnings tracks per-repo guidance derived from user reactions on
// Cadoo's prior comments. Accepted comments raise a rule's weight; rejected
// comments lower it. High-weight rules are injected into future-prompt
// guidance so Cadoo's voice converges to each team's preferences.
package learnings

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reaction is one user signal on a Cadoo finding.
type Reaction string

// Reaction kinds.
const (
	Accept Reaction = "accept"
	Reject Reaction = "reject"
)

// Rule is one piece of accumulated team guidance.
type Rule struct {
	ID       string
	RepoKey  string
	Text     string
	Accepted int
	Rejected int
	Weight   float32
}

// Store wraps the learnings table.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Record applies a reaction to a rule, upserting the row and recomputing
// weight as accepted / (accepted + rejected) clamped to [0.05, 0.95]. The
// clamp prevents either signal from completely silencing the other.
func (s *Store) Record(ctx context.Context, repoKey, rule string, reaction Reaction) error {
	if rule == "" {
		return fmt.Errorf("rule must be non-empty")
	}
	dAcc, dRej := 0, 0
	switch reaction {
	case Accept:
		dAcc = 1
	case Reject:
		dRej = 1
	default:
		return fmt.Errorf("unknown reaction %q", reaction)
	}
	const q = `
INSERT INTO learnings(repo_key, rule, accepted, rejected, weight, last_seen_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (repo_key, rule) DO UPDATE SET
  accepted = learnings.accepted + EXCLUDED.accepted,
  rejected = learnings.rejected + EXCLUDED.rejected,
  weight = LEAST(0.95, GREATEST(0.05,
    (learnings.accepted + EXCLUDED.accepted)::REAL /
    NULLIF(learnings.accepted + EXCLUDED.accepted +
           learnings.rejected + EXCLUDED.rejected, 0))),
  last_seen_at = now()`
	initialWeight := float32(0.5)
	if reaction == Accept {
		initialWeight = 0.95
	} else {
		initialWeight = 0.05
	}
	_, err := s.pool.Exec(ctx, q, repoKey, rule, dAcc, dRej, initialWeight)
	if err != nil {
		return fmt.Errorf("record reaction: %w", err)
	}
	return nil
}

// Active returns the top-N rules for repoKey ordered by weight desc.
// minWeight filters out rules that haven't accumulated meaningful signal.
func (s *Store) Active(ctx context.Context, repoKey string, limit int, minWeight float32) ([]Rule, error) {
	if limit <= 0 {
		limit = 20
	}
	if minWeight <= 0 {
		minWeight = 0.6
	}
	const q = `
SELECT id::text, repo_key, rule, accepted, rejected, weight
FROM learnings
WHERE repo_key = $1 AND weight >= $2
ORDER BY weight DESC, last_seen_at DESC
LIMIT $3`
	rows, err := s.pool.Query(ctx, q, repoKey, minWeight, limit)
	if err != nil {
		return nil, fmt.Errorf("query learnings: %w", err)
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		if err := rows.Scan(&r.ID, &r.RepoKey, &r.Text, &r.Accepted, &r.Rejected, &r.Weight); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
