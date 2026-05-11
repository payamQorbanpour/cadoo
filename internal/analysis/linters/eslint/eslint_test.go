package eslint

import "testing"

const sample = `[
  {
    "filePath": "src/foo.js",
    "messages": [
      {"ruleId": "no-unused-vars", "severity": 2, "message": "x is defined but never used", "line": 3, "endLine": 3, "column": 7},
      {"ruleId": "prefer-const", "severity": 1, "message": "x is never reassigned", "line": 3, "column": 7}
    ]
  },
  {"filePath": "src/bar.js", "messages": []}
]`

func TestParseJSON(t *testing.T) {
	got, err := ParseJSON([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d findings", len(got))
	}
	if got[0].File != "src/foo.js" || got[0].Rule != "no-unused-vars" {
		t.Errorf("first: %+v", got[0])
	}
	if got[0].LineEnd != 3 {
		t.Errorf("end line should default to line, got %d", got[0].LineEnd)
	}
}
