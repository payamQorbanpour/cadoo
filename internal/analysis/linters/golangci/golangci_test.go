package golangci

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/analysis/sandbox"
)

const sampleJSON = `{
  "Issues": [
    {
      "FromLinter": "errcheck",
      "Text": "Error return value not checked",
      "Severity": "",
      "Pos": {"Filename": "main.go", "Line": 5, "Column": 2}
    },
    {
      "FromLinter": "gosec",
      "Text": "G104: Errors unhandled",
      "Severity": "error",
      "Pos": {"Filename": "auth.go", "Line": 42, "Column": 8}
    }
  ]
}`

func TestParseJSON(t *testing.T) {
	got, err := ParseJSON([]byte(sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d findings", len(got))
	}
	if got[0].Rule != "errcheck" || got[0].File != "main.go" || got[0].LineStart != 5 {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Severity != analysis.SeverityError {
		t.Errorf("second severity: %v", got[1].Severity)
	}
}

func TestRunInvokesContainer(t *testing.T) {
	mock := &sandbox.MockRunner{
		Responses: map[string]*sandbox.Result{Image: {Stdout: []byte(sampleJSON)}},
	}
	got, err := Linter{}.Run(context.Background(), mock, analysis.Workspace{HostPath: "/tmp/ws"}, []string{"main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("findings: %d", len(got))
	}
	if len(mock.Calls) != 1 || mock.Calls[0].Image != Image {
		t.Errorf("calls: %+v", mock.Calls)
	}
}
