// Package audit is Cadoo's append-only audit log. Every dispatch and admin
// mutation writes a row to audit_events; queries are admin-gated at the
// API surface (cadoo-api) using internal/auth.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event is one audit record.
type Event struct {
	ID        int64
	OrgID     string // empty == system-level event
	Actor     string // user email/sub or "system" / "github-app"
	Action    string // verb: "tool.dispatch", "user.role.change", ...
	Target    string // freeform identifier of the affected thing
	Metadata  map[string]any
	CreatedAt time.Time
}

// Logger writes audit events. A nil Logger silently no-ops so callers don't
// need to nil-check on every Record call.
type Logger struct {
	pool *pgxpool.Pool
}

// New returns a Logger bound to pool.
func New(pool *pgxpool.Pool) *Logger { return &Logger{pool: pool} }

// Record appends one event. orgID may be "" for system-level events.
func (l *Logger) Record(ctx context.Context, orgID, actor, action, target string, metadata map[string]any) error {
	if l == nil || l.pool == nil {
		return nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		raw = []byte("{}")
	}
	const q = `INSERT INTO audit_events(org_id, actor, action, target, metadata) VALUES ($1, $2, $3, $4, $5)`
	if _, err := l.pool.Exec(ctx, q, orgArg(orgID), actor, action, target, raw); err != nil {
		return fmt.Errorf("audit record: %w", err)
	}
	return nil
}

// Query returns the most recent events. Empty orgID returns all (admin-only
// at the API surface).
func (l *Logger) Query(ctx context.Context, orgID string, limit int) ([]Event, error) {
	if l == nil || l.pool == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	var (
		rows interface {
			Next() bool
			Scan(...any) error
			Close()
			Err() error
		}
		err error
	)
	if orgID == "" {
		rows, err = l.pool.Query(ctx,
			`SELECT id, COALESCE(org_id::text, ''), COALESCE(actor, ''), action, COALESCE(target, ''), metadata, created_at
			 FROM audit_events ORDER BY id DESC LIMIT $1`, limit)
	} else {
		rows, err = l.pool.Query(ctx,
			`SELECT id, COALESCE(org_id::text, ''), COALESCE(actor, ''), action, COALESCE(target, ''), metadata, created_at
			 FROM audit_events WHERE org_id = $1 ORDER BY id DESC LIMIT $2`, orgID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("audit query: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var raw []byte
		if err := rows.Scan(&e.ID, &e.OrgID, &e.Actor, &e.Action, &e.Target, &raw, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit row: %w", err)
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &e.Metadata)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func orgArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}
