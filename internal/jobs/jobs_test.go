package jobs

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type pingJob struct {
	Msg string `json:"msg"`
}

func (pingJob) Kind() string { return "ping" }

func TestMemoryQueueRoundtrip(t *testing.T) {
	q := NewMemory()
	got := make(chan string, 1)
	q.Register("ping", HandlerFunc(func(_ context.Context, payload json.RawMessage) error {
		var p pingJob
		if err := json.Unmarshal(payload, &p); err != nil {
			return err
		}
		got <- p.Msg
		return nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = q.Run(ctx)
	}()

	if err := q.Enqueue(ctx, pingJob{Msg: "hello"}); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-got:
		if msg != "hello" {
			t.Fatalf("got %q", msg)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for handler")
	}
	cancel()
	wg.Wait()
}
