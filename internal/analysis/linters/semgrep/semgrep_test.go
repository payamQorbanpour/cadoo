package semgrep

import (
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
)

const sample = `{
  "results": [
    {
      "check_id": "javascript.lang.security.audit.dangerously-set-inner-html",
      "path": "src/foo.js",
      "start": {"line": 10, "col": 5},
      "end": {"line": 10},
      "extra": {"message": "Detected user-controlled HTML", "severity": "ERROR"}
    },
    {
      "check_id": "python.lang.maintainability.unused-import",
      "path": "app.py",
      "start": {"line": 1, "col": 1},
      "end": {"line": 1},
      "extra": {"message": "Unused import", "severity": "INFO"}
    }
  ]
}`

func TestParseJSON(t *testing.T) {
	got, err := ParseJSON([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Severity != analysis.SeverityError {
		t.Errorf("first severity: %v", got[0].Severity)
	}
	if got[1].Severity != analysis.SeverityNote {
		t.Errorf("second severity: %v", got[1].Severity)
	}
}
