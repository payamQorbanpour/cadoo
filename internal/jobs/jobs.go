// Package jobs is Cadoo's background-work abstraction. Phase 0 ships an
// in-memory queue so the worker binary has something concrete to drive;
// Phase 1 swaps in a Postgres-backed River queue behind the same interface.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Job is one unit of background work. Implementations are normal Go structs
// that JSON-marshal to a stable payload.
type Job interface {
	Kind() string
}

// Handler runs jobs of a particular kind.
type Handler interface {
	Handle(ctx context.Context, payload json.RawMessage) error
}

// HandlerFunc adapts a plain function to Handler.
type HandlerFunc func(ctx context.Context, payload json.RawMessage) error

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, payload json.RawMessage) error {
	return f(ctx, payload)
}

// Queue enqueues jobs and runs registered handlers.
type Queue interface {
	Register(kind string, h Handler)
	Enqueue(ctx context.Context, j Job) error
	Run(ctx context.Context) error
}

// NewMemory returns an in-memory queue. Replace with the River-backed
// implementation in Phase 1.
func NewMemory() Queue {
	return &memQueue{
		handlers: map[string]Handler{},
		ch:       make(chan envelope, 1024),
	}
}

type envelope struct {
	kind    string
	payload json.RawMessage
}

type memQueue struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	ch       chan envelope
}

func (q *memQueue) Register(kind string, h Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[kind] = h
}

func (q *memQueue) Enqueue(ctx context.Context, j Job) error {
	payload, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	select {
	case q.ch <- envelope{kind: j.Kind(), payload: payload}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *memQueue) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-q.ch:
			q.mu.RLock()
			h := q.handlers[e.kind]
			q.mu.RUnlock()
			if h == nil {
				continue
			}
			// Handler errors are intentionally swallowed at the queue layer;
			// real implementations record them to llm_calls / pr_jobs rows.
			_ = h.Handle(ctx, e.payload)
		}
	}
}
