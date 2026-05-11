package audit

import (
	"context"
	"testing"
)

func TestNilLoggerIsNoop(t *testing.T) {
	var l *Logger
	if err := l.Record(context.Background(), "", "x", "y", "z", nil); err != nil {
		t.Errorf("nil Record: %v", err)
	}
	got, err := l.Query(context.Background(), "", 10)
	if err != nil || got != nil {
		t.Errorf("nil Query: got %+v err=%v", got, err)
	}
}

func TestOrgArg(t *testing.T) {
	if v := orgArg(""); v != nil {
		t.Errorf("empty org should map to nil, got %v", v)
	}
	if v := orgArg("uuid"); v != "uuid" {
		t.Errorf("non-empty org should pass through, got %v", v)
	}
}
