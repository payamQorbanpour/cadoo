package check_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	checkTool "github.com/payamqorbanpour/cadoo/internal/tools/check"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

type slowLLM struct {
	delay         time.Duration
	maxConcurrent atomic.Int64
	current       atomic.Int64
}

func (s *slowLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	cur := s.current.Add(1)
	for {
		old := s.maxConcurrent.Load()
		if cur <= old || s.maxConcurrent.CompareAndSwap(old, cur) {
			break
		}
	}
	time.Sleep(s.delay)
	s.current.Add(-1)
	return &llm.ChatResponse{Content: `{"findings":[]}`, FinishReason: "stop"}, nil
}

func TestCheckRunsRulesInParallel(t *testing.T) {
	slow := &slowLLM{delay: 50 * time.Millisecond}
	in := tools.Input{
		PR:    &vcs.PullRequest{Title: "t", Author: "a"},
		LLM:   slow,
		Model: "test",
		Config: config.Repo{
			Checks: []config.Check{
				{Name: "c1", Prompt: "check 1"},
				{Name: "c2", Prompt: "check 2"},
				{Name: "c3", Prompt: "check 3"},
			},
		},
	}

	start := time.Now()
	_, err := checkTool.Tool{}.Run(context.Background(), in)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 3 rules × 50ms sequential = 150ms; parallel should finish in ~70ms
	if elapsed > 120*time.Millisecond {
		t.Errorf("rules appear to run sequentially: elapsed=%v, want <120ms for 3×50ms rules", elapsed)
	}
	if slow.maxConcurrent.Load() < 2 {
		t.Errorf("expected at least 2 concurrent LLM calls; max was %d", slow.maxConcurrent.Load())
	}
}
