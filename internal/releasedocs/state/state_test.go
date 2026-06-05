package state_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs/state"
)

// TestNilStoreRecord verifies the nil-tolerance contract: a nil *Store calling
// Record returns nil without panicking.
func TestNilStoreRecord(t *testing.T) {
	t.Parallel()
	var s *state.Store
	if err := s.Record(context.Background(), "org1", "github", "owner/repo", "v1.0.0", "changelog", "ext-123"); err != nil {
		t.Fatalf("nil Store.Record must return nil, got %v", err)
	}
}

// TestNilStoreLookup verifies the nil-tolerance contract: a nil *Store calling
// Lookup returns ("", false, nil) without panicking.
func TestNilStoreLookup(t *testing.T) {
	t.Parallel()
	var s *state.Store
	id, found, err := s.Lookup(context.Background(), "github", "owner/repo", "v1.0.0", "changelog")
	if err != nil {
		t.Fatalf("nil Store.Lookup must return nil error, got %v", err)
	}
	if found {
		t.Fatal("nil Store.Lookup must return found=false")
	}
	if id != "" {
		t.Fatalf("nil Store.Lookup must return empty externalID, got %q", id)
	}
}

// TestNilPoolStoreRecord verifies a Store constructed with a nil pool behaves
// identically to a nil *Store.
func TestNilPoolStoreRecord(t *testing.T) {
	t.Parallel()
	s := state.New(nil)
	if err := s.Record(context.Background(), "org1", "github", "owner/repo", "v1.0.0", "release_notes", ""); err != nil {
		t.Fatalf("nil-pool Store.Record must return nil, got %v", err)
	}
}

// TestNilPoolStoreLookup verifies a Store constructed with a nil pool returns
// ("", false, nil).
func TestNilPoolStoreLookup(t *testing.T) {
	t.Parallel()
	s := state.New(nil)
	id, found, err := s.Lookup(context.Background(), "github", "owner/repo", "v1.0.0", "release_notes")
	if err != nil {
		t.Fatalf("nil-pool Store.Lookup must return nil error, got %v", err)
	}
	if found {
		t.Fatal("nil-pool Store.Lookup must return found=false")
	}
	if id != "" {
		t.Fatalf("nil-pool Store.Lookup must return empty externalID, got %q", id)
	}
}

// TestDBRoundTrip performs a live DB round-trip: record twice on the same
// composite key and assert no duplicate rows exist (ON CONFLICT DO UPDATE).
// The test is skipped when DATABASE_URL is unset.
func TestDBRoundTrip(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping DB round-trip test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()

	s := state.New(pool)

	const (
		org      = "test-org"
		provider = "github"
		repo     = "owner/cadoo-test-repo"
		toTag    = "v9.99.0-testonly"
		kind     = "changelog"
	)

	// Clean up before and after so the test is idempotent.
	cleanup := func() {
		_, _ = pool.Exec(ctx,
			`DELETE FROM release_docs_state WHERE provider=$1 AND repo_full_name=$2 AND to_tag=$3`,
			provider, repo, toTag)
	}
	cleanup()
	defer cleanup()

	// First record.
	if err := s.Record(ctx, org, provider, repo, toTag, kind, "ext-001"); err != nil {
		t.Fatalf("first Record: %v", err)
	}

	// Lookup: should find it.
	id, found, err := s.Lookup(ctx, provider, repo, toTag, kind)
	if err != nil {
		t.Fatalf("Lookup after first Record: %v", err)
	}
	if !found {
		t.Fatal("expected found=true after Record, got false")
	}
	if id != "ext-001" {
		t.Fatalf("expected externalID %q, got %q", "ext-001", id)
	}

	// Second record with a different external_id — should update in place.
	if err := s.Record(ctx, org, provider, repo, toTag, kind, "ext-002"); err != nil {
		t.Fatalf("second Record: %v", err)
	}

	// Row count must still be exactly 1 (ON CONFLICT DO UPDATE, not INSERT).
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM release_docs_state WHERE provider=$1 AND repo_full_name=$2 AND to_tag=$3 AND artifact_kind=$4`,
		provider, repo, toTag, kind).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row after two Records with same key, got %d", count)
	}

	// Lookup after update: should reflect new external_id.
	id, found, err = s.Lookup(ctx, provider, repo, toTag, kind)
	if err != nil {
		t.Fatalf("Lookup after second Record: %v", err)
	}
	if !found {
		t.Fatal("expected found=true after second Record, got false")
	}
	if id != "ext-002" {
		t.Fatalf("expected updated externalID %q, got %q", "ext-002", id)
	}

	// Lookup a non-existent key: must return ("", false, nil).
	id, found, err = s.Lookup(ctx, provider, repo, toTag, "blog")
	if err != nil {
		t.Fatalf("Lookup for non-existent key: %v", err)
	}
	if found {
		t.Fatalf("expected found=false for missing key, got found=true with id=%q", id)
	}
}
