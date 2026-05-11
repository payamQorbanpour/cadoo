// Package reports periodically aggregates audit events and posts a summary
// to the configured notifier. Phase 8 v1 ships the loop + summary writer;
// per-org filtering and email delivery are Phase 8.x.
package reports

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/audit"
	"github.com/payamqorbanpour/cadoo/internal/orchestrator"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// Reporter periodically posts a summary of recent audit activity.
type Reporter struct {
	Audit    *audit.Logger
	Notifier orchestrator.ResultNotifier
	Interval time.Duration // 0 == 24h default
	Lookback int           // events to inspect each tick; 0 == 1000 default
}

// Run blocks until ctx cancels. Safe to call as a goroutine.
func (r *Reporter) Run(ctx context.Context) error {
	if r.Audit == nil {
		return errors.New("reports: Audit is nil")
	}
	interval := r.Interval
	if interval == 0 {
		interval = 24 * time.Hour
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := r.runOnce(ctx); err != nil {
				slog.Error("report tick", "err", err)
			}
		}
	}
}

func (r *Reporter) runOnce(ctx context.Context) error {
	limit := r.Lookback
	if limit <= 0 {
		limit = 1000
	}
	events, err := r.Audit.Query(ctx, "", limit)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}
	body := Summarize(events)
	if r.Notifier == nil {
		slog.Info("scheduled report (no notifier configured)", "events", len(events))
		return nil
	}
	return r.Notifier.NotifyResult(ctx,
		&vcs.PullRequest{RepoFullName: "cadoo/system", URL: "#"},
		"scheduled-report",
		&tools.Result{Summary: body},
	)
}

// Summarize collapses events into a per-action count table. Exposed for
// tests and direct use.
func Summarize(events []audit.Event) string {
	counts := map[string]int{}
	for _, e := range events {
		counts[e.Action]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })

	var b strings.Builder
	fmt.Fprintf(&b, "*Cadoo activity report* — %d events in window\n\n", len(events))
	for _, k := range keys {
		fmt.Fprintf(&b, "• %s: %d\n", k, counts[k])
	}
	return b.String()
}
