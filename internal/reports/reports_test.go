package reports

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/audit"
)

func TestSummarizeCountsAndOrders(t *testing.T) {
	events := []audit.Event{
		{Action: "tool.dispatch"},
		{Action: "tool.dispatch"},
		{Action: "tool.dispatch"},
		{Action: "user.role.change"},
	}
	got := Summarize(events)
	if !strings.Contains(got, "tool.dispatch: 3") {
		t.Errorf("missing dispatch count; got %q", got)
	}
	if !strings.Contains(got, "user.role.change: 1") {
		t.Errorf("missing role-change count; got %q", got)
	}
	if i, j := strings.Index(got, "tool.dispatch"), strings.Index(got, "user.role.change"); i > j {
		t.Errorf("expected tool.dispatch before role.change in descending count order")
	}
}

func TestSummarizeEmpty(t *testing.T) {
	got := Summarize(nil)
	if !strings.Contains(got, "0 events") {
		t.Errorf("expected zero-event summary, got %q", got)
	}
}
