-- +goose Up
-- +goose StatementBegin

-- Release-docs published state. Keyed by (provider, repo_full_name, to_tag, artifact_kind)
-- so repeated dispatcher runs edit in place rather than re-publishing.
-- Deliberately no FK to repos/pull_requests to keep the dispatcher's hot path decoupled.
-- org_id is TEXT (not FK) for multi-tenancy, matching the posted_findings pattern.
CREATE TABLE release_docs_state (
    id             BIGSERIAL PRIMARY KEY,
    org_id         TEXT,                              -- Cadoo org for multi-tenancy
    provider       TEXT NOT NULL,
    repo_full_name TEXT NOT NULL,
    to_tag         TEXT NOT NULL,
    artifact_kind  TEXT NOT NULL,                    -- changelog | release_notes | blog
    external_id    TEXT,                             -- release body ID, PR number, pages commit SHA, etc.
    published_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, repo_full_name, to_tag, artifact_kind)
);
CREATE INDEX release_docs_state_repo_idx
    ON release_docs_state (provider, repo_full_name, to_tag);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS release_docs_state;
-- +goose StatementEnd
