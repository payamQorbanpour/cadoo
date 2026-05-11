package shellcheck

import "testing"

const sample = `[
  {"file": "deploy.sh", "line": 3, "endLine": 3, "column": 5, "level": "warning", "code": 2034, "message": "x appears unused"},
  {"file": "deploy.sh", "line": 7, "column": 1, "level": "error", "code": 1009, "message": "Mismatched 'fi'"}
]`

func TestParseJSON(t *testing.T) {
	got, err := ParseJSON([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Rule != "SC2034" {
		t.Errorf("rule: %q", got[0].Rule)
	}
	if got[1].LineEnd != 7 {
		t.Errorf("end line should default to line, got %d", got[1].LineEnd)
	}
}
