-- +goose Up
-- +goose StatementBegin

-- Each tool's summary becomes a labelled section inside one consolidated PR
-- comment. We keep the rendered section body so the dispatcher can re-render
-- the wrapper comment without re-asking the model. The wrapper comment's
-- external ID is stored in the row whose `tool` column equals the empty
-- string sentinel, sharing the table to avoid a second migration round-trip.

ALTER TABLE posted_summaries
    ADD COLUMN IF NOT EXISTS body TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE posted_summaries DROP COLUMN IF EXISTS body;

-- +goose StatementEnd
