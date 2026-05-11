package orchestrator

import (
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/tools/adddocs"
	"github.com/payamqorbanpour/cadoo/internal/tools/addtests"
	"github.com/payamqorbanpour/cadoo/internal/tools/ask"
	"github.com/payamqorbanpour/cadoo/internal/tools/changelog"
	"github.com/payamqorbanpour/cadoo/internal/tools/check"
	"github.com/payamqorbanpour/cadoo/internal/tools/deepreview"
	"github.com/payamqorbanpour/cadoo/internal/tools/describe"
	"github.com/payamqorbanpour/cadoo/internal/tools/improve"
	"github.com/payamqorbanpour/cadoo/internal/tools/learn"
	"github.com/payamqorbanpour/cadoo/internal/tools/plan"
	"github.com/payamqorbanpour/cadoo/internal/tools/resolveconflicts"
	"github.com/payamqorbanpour/cadoo/internal/tools/review"
	"github.com/payamqorbanpour/cadoo/internal/tools/unlearn"
)

// DefaultRegistry returns a Registry with every Cadoo built-in tool
// registered. Cmd binaries call this at startup; tests can build their own.
func DefaultRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(review.Tool{})
	r.Register(describe.Tool{})
	r.Register(improve.Tool{})
	r.Register(ask.Tool{})
	r.Register(changelog.Tool{})
	r.Register(adddocs.Tool{})
	r.Register(deepreview.Tool{})
	r.Register(resolveconflicts.Tool{})
	r.Register(addtests.Tool{})
	r.Register(plan.Tool{})
	r.Register(check.Tool{})
	r.Register(learn.Tool{})
	r.Register(unlearn.Tool{})
	return r
}
