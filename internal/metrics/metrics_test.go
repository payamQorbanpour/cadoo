package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestDispatchCounterIncrements(t *testing.T) {
	before := testutil.ToFloat64(DispatchTotal.WithLabelValues("review", "github", "success"))
	DispatchTotal.WithLabelValues("review", "github", "success").Inc()
	after := testutil.ToFloat64(DispatchTotal.WithLabelValues("review", "github", "success"))
	if after-before != 1 {
		t.Errorf("expected delta 1, got %f", after-before)
	}
}
