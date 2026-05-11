-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS vector;

-- Tenants. Self-host = a degenerate single-org tenant.
CREATE TABLE orgs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    name            TEXT,
    oidc_subject    TEXT UNIQUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE org_members (
    org_id      UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member', 'viewer')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

-- VCS provider installations (one per provider+org+target tenant).
CREATE TABLE installations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL CHECK (provider IN ('github', 'github_enterprise', 'gitlab')),
    external_id     TEXT NOT NULL,
    api_base_url    TEXT,
    auth_secret_ref TEXT NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, external_id)
);

CREATE TABLE repos (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id              UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    installation_id     UUID NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL,
    external_id         TEXT NOT NULL,
    full_name           TEXT NOT NULL,
    default_branch      TEXT,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, external_id)
);
CREATE INDEX repos_org_id_idx ON repos (org_id);

-- One row per pull/merge request Cadoo has seen.
CREATE TABLE pull_requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_id         UUID NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    external_number BIGINT NOT NULL,
    title           TEXT,
    author          TEXT,
    base_sha        TEXT,
    head_sha        TEXT,
    state           TEXT NOT NULL DEFAULT 'open',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo_id, external_number)
);

-- A single review run for a PR (e.g. opened, synchronize, /review command).
CREATE TABLE pr_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pr_id           UUID NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    tool            TEXT NOT NULL,
    trigger         TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled')),
    error           TEXT,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX pr_jobs_pr_id_idx ON pr_jobs (pr_id);
CREATE INDEX pr_jobs_status_idx ON pr_jobs (status);

-- Findings posted to a PR (used for idempotent edit-on-resync).
CREATE TABLE findings (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pr_id           UUID NOT NULL REFERENCES pull_requests(id) ON DELETE CASCADE,
    job_id          UUID NOT NULL REFERENCES pr_jobs(id) ON DELETE CASCADE,
    fingerprint     TEXT NOT NULL,
    severity        TEXT NOT NULL CHECK (severity IN ('block', 'warn', 'nit')),
    file            TEXT,
    line_start      INT,
    line_end        INT,
    title           TEXT,
    body            TEXT,
    external_comment_id TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pr_id, fingerprint)
);

-- LLM call ledger for cost tracking and audit.
CREATE TABLE llm_calls (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id          UUID REFERENCES pr_jobs(id) ON DELETE SET NULL,
    provider        TEXT NOT NULL,
    model           TEXT NOT NULL,
    prompt_tokens   INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens    INT NOT NULL DEFAULT 0,
    cost_usd        NUMERIC(12, 6) NOT NULL DEFAULT 0,
    latency_ms      INT NOT NULL DEFAULT 0,
    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX llm_calls_job_id_idx ON llm_calls (job_id);

-- Audit log (append-only).
CREATE TABLE audit_events (
    id          BIGSERIAL PRIMARY KEY,
    org_id      UUID REFERENCES orgs(id) ON DELETE SET NULL,
    actor       TEXT,
    action      TEXT NOT NULL,
    target      TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_org_id_idx ON audit_events (org_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS llm_calls;
DROP TABLE IF EXISTS findings;
DROP TABLE IF EXISTS pr_jobs;
DROP TABLE IF EXISTS pull_requests;
DROP TABLE IF EXISTS repos;
DROP TABLE IF EXISTS installations;
DROP TABLE IF EXISTS org_members;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS orgs;
-- +goose StatementEnd
