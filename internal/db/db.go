// Package db is the thin Postgres connection wrapper. sqlc-generated query
// code will live in internal/db/sqlc_gen and is not imported until Phase 1
// adds queries.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

// Pool is the application's shared connection pool.
type Pool = pgxpool.Pool

// Vector re-exports pgvector.Vector for callers that want to avoid importing
// the pgvector package directly.
type Vector = pgvector.Vector

// Open returns a configured Pool. Caller is responsible for Close().
func Open(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse pg dsn: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MaxConnIdleTime = 5 * time.Minute
	// Register pgvector OID/codecs on every fresh connection so kb.Store can
	// pass pgvector.Vector values directly. Tolerant of databases that lack
	// the extension installed (returns no error from Register).
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if err := pgxvec.RegisterTypes(ctx, conn); err != nil {
			// Best-effort: a Postgres without pgvector is still usable for
			// the queue, audit log, etc.
			_ = err
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pg pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping pg: %w", err)
	}
	return pool, nil
}
