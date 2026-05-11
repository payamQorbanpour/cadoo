package issuetrackers

import (
	"reflect"
	"testing"
)

func TestExtractKeys(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Fixes JIRA-123 and ENG-4 too", []string{"JIRA-123", "ENG-4"}},
		{"refs eng-12", nil}, // lowercase isn't extracted
		{"closes ABC-1, ABC-1 dup", []string{"ABC-1"}},
		{"plain prose", nil},
	}
	for _, tc := range cases {
		got := ExtractKeys(tc.in)
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ExtractKeys(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}
