-- +goose Up
-- +goose StatementBegin

-- Knowledge base. repo_key is provider-qualified (e.g. "github:owner/name")
-- so we don't depend on a normalized repos row existing yet.
CREATE TABLE kb_documents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_key    TEXT NOT NULL,
    source      TEXT NOT NULL,    -- "repo_doc", "adr", "confluence", "notion", ...
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo_key, source, title)
);
CREATE INDEX kb_documents_repo_idx ON kb_documents (repo_key);

CREATE TABLE kb_chunks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id  UUID NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    repo_key     TEXT NOT NULL,
    ordinal      INT NOT NULL,
    text         TEXT NOT NULL,
    embedding    vector(1536),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, ordinal)
);
CREATE INDEX kb_chunks_repo_idx ON kb_chunks (repo_key);
-- IVFFlat index speeds up nearest-neighbour search; tune lists per data size.
CREATE INDEX kb_chunks_embedding_idx ON kb_chunks
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- Learnings: per-repo rules accumulated from accept/reject reactions on
-- previous Cadoo comments. Higher weight = more strongly preferred guidance.
CREATE TABLE learnings (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_key      TEXT NOT NULL,
    rule          TEXT NOT NULL,
    accepted      INT NOT NULL DEFAULT 0,
    rejected      INT NOT NULL DEFAULT 0,
    weight        REAL NOT NULL DEFAULT 0.5 CHECK (weight >= 0 AND weight <= 1),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (repo_key, rule)
);
CREATE INDEX learnings_repo_idx ON learnings (repo_key, weight DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS learnings;
DROP TABLE IF EXISTS kb_chunks;
DROP TABLE IF EXISTS kb_documents;
-- +goose StatementEnd
