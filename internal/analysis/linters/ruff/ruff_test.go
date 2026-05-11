package ruff

import "testing"

const sampleJSON = `[
  {
    "code": "E501",
    "message": "line too long (102 > 100)",
    "filename": "app.py",
    "location": {"row": 12, "column": 1},
    "end_location": {"row": 12, "column": 102}
  },
  {
    "code": "F401",
    "message": "'os' imported but unused",
    "filename": "util.py",
    "location": {"row": 1, "column": 1},
    "end_location": {"row": 1, "column": 9}
  }
]`

func TestParseJSON(t *testing.T) {
	got, err := ParseJSON([]byte(sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Rule != "E501" || got[0].File != "app.py" || got[0].LineStart != 12 {
		t.Errorf("first: %+v", got[0])
	}
}

func TestParseJSONEmpty(t *testing.T) {
	got, err := ParseJSON(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0, got %d", len(got))
	}
}
