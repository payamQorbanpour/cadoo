package sandbox

import (
	"context"
	"testing"
)

func TestMockRunnerReturnsCanned(t *testing.T) {
	m := &MockRunner{
		Responses: map[string]*Result{
			"linter:latest": {Stdout: []byte("hello"), ExitCode: 0},
		},
	}
	res, err := m.Run(context.Background(), Spec{Image: "linter:latest", Cmd: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "hello" {
		t.Errorf("stdout: %q", string(res.Stdout))
	}
	if len(m.Calls) != 1 {
		t.Errorf("calls: %d", len(m.Calls))
	}
}

func TestMockRunnerErrorsOnUnknownImage(t *testing.T) {
	m := &MockRunner{}
	if _, err := m.Run(context.Background(), Spec{Image: "x"}); err == nil {
		t.Fatal("expected error")
	}
}
