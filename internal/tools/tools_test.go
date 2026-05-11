package tools

import (
	"context"
	"testing"
)

type stub struct{ name string }

func (s stub) Name() string                                  { return s.name }
func (s stub) Run(_ context.Context, _ Input) (*Result, error) { return &Result{}, nil }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(stub{name: "review"})
	r.Register(stub{name: "describe"})
	if _, ok := r.Get("review"); !ok {
		t.Error("review not found")
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("nope should not be found")
	}
	got := r.Names()
	if len(got) != 2 {
		t.Errorf("names: %v", got)
	}
}

func TestRegistryLastWriterWins(t *testing.T) {
	r := NewRegistry()
	r.Register(stub{name: "x"})
	r.Register(stub{name: "x"})
	if len(r.Names()) != 1 {
		t.Errorf("expected 1 entry after re-register, got %d", len(r.Names()))
	}
}

func TestExtractJSONHandlesProseAndFences(t *testing.T) {
	cases := []string{
		`{"a":1}`,
		"```json\n{\"a\":1}\n```",
		"sure: {\"a\":1} hope this helps",
	}
	type x struct{ A int }
	for _, in := range cases {
		var got x
		if err := ExtractJSON(in, &got); err != nil {
			t.Errorf("ExtractJSON(%q): %v", in, err)
		}
		if got.A != 1 {
			t.Errorf("ExtractJSON(%q) = %+v", in, got)
		}
	}
}

func TestExtractJSONNoObject(t *testing.T) {
	var got struct{}
	if err := ExtractJSON("nothing here", &got); err == nil {
		t.Fatal("expected error")
	}
}
