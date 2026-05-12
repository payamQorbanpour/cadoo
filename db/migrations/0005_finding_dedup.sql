-- +goose Up
-- +goose StatementBegin

-- The old `fingerprint` column hashes the full body, so an LLM that rephrases
-- the same finding across re-runs produces a different fingerprint and
-- bypasses dedup. We add a coarser `structural_key` (tool|file|line range|
-- severity) plus a `normalized_title` so the application can match near-
-- duplicates by title similarity instead of byte equality.

ALTER TABLE posted_findings
    ADD COLUMN IF NOT EXISTS structural_key   TEXT,
    ADD COLUMN IF NOT EXISTS normalized_title TEXT;

CREATE INDEX IF NOT EXISTS posted_findings_structural_idx
    ON posted_findings (provider, repo_full_name, pr_number, structural_key);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS posted_findings_structural_idx;
ALTER TABLE posted_findings
    DROP COLUMN IF EXISTS normalized_title,
    DROP COLUMN IF EXISTS structural_key;

-- +goose StatementEnd
