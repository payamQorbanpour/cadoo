// Package state persists release-docs publication state so repeated dispatcher
// runs edit existing artifacts in place rather than re-publishing. The store is
// keyed on (provider, repo_full_name, to_tag, artifact_kind) and backed by the
// release_docs_state table (migration 0006).
//
// A nil *Store is safe to call: Record returns nil and Lookup returns a zero
// result and nil error, matching the stateless / no-DB mode used in cadoo-cli
// and single-binary dev deployments.
//
// ISOLATION NOTE: this package deliberately imports nothing from
// internal/releasedocs so the dispatcher (package releasedocs) can import it
// without a cycle. Artifact kinds are plain strings here.
package state

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps the release_docs_state table. A nil Store is safe to call
// against; methods become no-ops returning zero values so callers do not need
// nil-guards in their hot paths.
type Store struct {
	pool *pgxpool.Pool
}

// New constructs a Postgres-backed Store from the given connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Record upserts a publication record for the given composite key. If a row
// already exists for (provider, repoFullName, toTag, kind) the external_id and
// updated_at columns are refreshed in place — no duplicate rows are inserted.
//
// org is stored for multi-tenancy but is not part of the uniqueness key.
//
// kind must be a plain string value such as "changelog", "release_notes", or
// "blog" — this package does not import the releasedocs package so there is
// zero coupling and no import cycle risk.
func (s *Store) Record(ctx context.Context, org, provider, repoFullName, toTag, kind, externalID string) error {
	if s == nil || s.pool == nil {
		return nil
	}
	const q = `
INSERT INTO release_docs_state (org_id, provider, repo_full_name, to_tag, artifact_kind, external_id)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, repo_full_name, to_tag, artifact_kind) DO UPDATE
  SET external_id = EXCLUDED.external_id,
      updated_at  = now()`
	_, err := s.pool.Exec(ctx, q, org, provider, repoFullName, toTag, kind, externalID)
	if err != nil {
		return fmt.Errorf("release_docs_state record: %w", err)
	}
	return nil
}

// Lookup retrieves the external_id stored for the given composite key.
// Returns ("", false, nil) when no row is found, matching the findings.Store
// nil-tolerance contract so callers can safely check the bool before using
// the ID.
func (s *Store) Lookup(ctx context.Context, provider, repoFullName, toTag, kind string) (externalID string, found bool, err error) {
	if s == nil || s.pool == nil {
		return "", false, nil
	}
	const q = `
SELECT coalesce(external_id, '')
  FROM release_docs_state
 WHERE provider       = $1
   AND repo_full_name = $2
   AND to_tag         = $3
   AND artifact_kind  = $4`
	err = s.pool.QueryRow(ctx, q, provider, repoFullName, toTag, kind).Scan(&externalID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("release_docs_state lookup: %w", err)
	}
	return externalID, true, nil
}
