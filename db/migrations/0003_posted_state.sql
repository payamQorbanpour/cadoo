-- +goose Up
-- +goose StatementBegin

-- Idempotency tables. Both keyed by (provider, repo_full_name, pr_number)
-- so dispatch can run before any pull_requests row exists; we deliberately
-- skip the FK to keep the dispatcher's hot path simple.

-- One row per inline finding Cadoo has ever posted on a PR. The fingerprint
-- collapses (file, line range, severity, title, body) to a stable hash so a
-- re-dispatch can dedupe without re-asking the model.
CREATE TABLE posted_findings (
    id                 BIGSERIAL PRIMARY KEY,
    provider           TEXT NOT NULL,
    repo_full_name     TEXT NOT NULL,
    pr_number          BIGINT NOT NULL,
    tool               TEXT NOT NULL,
    fingerprint        TEXT NOT NULL,
    severity           TEXT,
    file               TEXT,
    line_start         INT,
    line_end           INT,
    title              TEXT,
    external_comment_id TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, repo_full_name, pr_number, fingerprint)
);
CREATE INDEX posted_findings_pr_idx
    ON posted_findings (provider, repo_full_name, pr_number);

-- One row per (PR, tool) summary comment Cadoo has posted. The dispatcher
-- looks this up before posting so subsequent runs edit in place instead of
-- spawning a new comment per resync.
CREATE TABLE posted_summaries (
    id                 BIGSERIAL PRIMARY KEY,
    provider           TEXT NOT NULL,
    repo_full_name     TEXT NOT NULL,
    pr_number          BIGINT NOT NULL,
    tool               TEXT NOT NULL,
    external_comment_id TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, repo_full_name, pr_number, tool)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS posted_summaries;
DROP TABLE IF EXISTS posted_findings;
-- +goose StatementEnd
