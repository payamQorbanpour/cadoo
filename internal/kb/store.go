// Package kb is Cadoo's per-repo knowledge base. Documents are chunked,
// embedded, and stored in pgvector; tools query for top-K chunks relevant to
// the current PR.
package kb

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/payamqorbanpour/cadoo/internal/llm/embed"
)

// Hit is one search result.
type Hit struct {
	DocumentID string
	ChunkID    string
	Title      string
	Source     string
	Text       string
	Distance   float32 // 0..1 cosine distance; lower == more similar
}

// Store is the KB façade. Construct via New.
type Store struct {
	pool     *pgxpool.Pool
	embedder embed.Embedder
}

// New builds a Store.
func New(pool *pgxpool.Pool, embedder embed.Embedder) *Store {
	return &Store{pool: pool, embedder: embedder}
}

// IngestDocument upserts a document for repoKey, chunks it, embeds the
// chunks, and stores them. Returns the document ID.
func (s *Store) IngestDocument(ctx context.Context, repoKey, source, title, body string) (string, error) {
	chunks := Chunk(body, DefaultChunkSize, DefaultOverlap)
	if len(chunks) == 0 {
		return "", fmt.Errorf("empty body")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var docID string
	const upsertDoc = `
INSERT INTO kb_documents(repo_key, source, title, body)
VALUES ($1, $2, $3, $4)
ON CONFLICT (repo_key, source, title) DO UPDATE
  SET body = EXCLUDED.body, updated_at = now()
RETURNING id::text`
	if err := tx.QueryRow(ctx, upsertDoc, repoKey, source, title, body).Scan(&docID); err != nil {
		return "", fmt.Errorf("upsert document: %w", err)
	}
	// Replace chunks in full — simpler than diffing.
	if _, err := tx.Exec(ctx, `DELETE FROM kb_chunks WHERE document_id = $1`, docID); err != nil {
		return "", fmt.Errorf("delete prior chunks: %w", err)
	}

	embeddings, err := s.embedder.Embed(ctx, chunks)
	if err != nil {
		return "", fmt.Errorf("embed: %w", err)
	}
	if len(embeddings) != len(chunks) {
		return "", fmt.Errorf("embedder returned %d vectors for %d chunks", len(embeddings), len(chunks))
	}

	const insertChunk = `
INSERT INTO kb_chunks(document_id, repo_key, ordinal, text, embedding)
VALUES ($1, $2, $3, $4, $5)`
	for i, text := range chunks {
		if _, err := tx.Exec(ctx, insertChunk,
			docID, repoKey, i, text, pgvector.NewVector(embeddings[i]),
		); err != nil {
			return "", fmt.Errorf("insert chunk %d: %w", i, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return docID, nil
}

// Search returns the top-K chunks (cosine-nearest) for repoKey + query.
// Empty query returns nil with no error.
func (s *Store) Search(ctx context.Context, repoKey, query string, topK int) ([]Hit, error) {
	if query == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = 5
	}
	embeddings, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(embeddings) != 1 {
		return nil, fmt.Errorf("embedder returned %d vectors", len(embeddings))
	}

	const q = `
SELECT
  c.id::text, c.document_id::text, d.title, d.source, c.text,
  c.embedding <=> $1 AS distance
FROM kb_chunks c
JOIN kb_documents d ON d.id = c.document_id
WHERE c.repo_key = $2 AND c.embedding IS NOT NULL
ORDER BY c.embedding <=> $1
LIMIT $3`
	rows, err := s.pool.Query(ctx, q, pgvector.NewVector(embeddings[0]), repoKey, topK)
	if err != nil {
		return nil, fmt.Errorf("kb search: %w", err)
	}
	defer rows.Close()

	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.ChunkID, &h.DocumentID, &h.Title, &h.Source, &h.Text, &h.Distance); err != nil {
			return nil, fmt.Errorf("scan hit: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
