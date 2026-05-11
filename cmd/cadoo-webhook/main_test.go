package main

import "testing"

func TestParseSlash(t *testing.T) {
	cases := []struct {
		body string
		cmd  string
		args string
	}{
		{"/review", "review", ""},
		{"/ask why is X done this way?", "ask", "why is X done this way?"},
		{"  /describe   ", "describe", ""},
		{"/improve   please   ", "improve", "please"},
		{"no command", "", ""},
		{"", "", ""},
		{"/", "", ""},
	}
	for _, tc := range cases {
		gotCmd, gotArgs := parseSlash(tc.body)
		if gotCmd != tc.cmd || gotArgs != tc.args {
			t.Errorf("parseSlash(%q) = (%q, %q); want (%q, %q)",
				tc.body, gotCmd, gotArgs, tc.cmd, tc.args)
		}
	}
}

func TestTriggerMatches(t *testing.T) {
	cases := []struct {
		trigger string
		action  string
		want    bool
	}{
		{"on_open", "opened", true},
		{"on_open", "reopened", true},
		{"on_open", "synchronize", false},
		{"on_sync", "synchronize", true},
		{"on_sync", "opened", false},
		{"always", "opened", true},
		{"always", "synchronize", true},
		{"never", "opened", false},
		{"", "opened", false},
		{"unknown_trigger", "opened", false},
	}
	for _, tc := range cases {
		if got := triggerMatches(tc.trigger, tc.action); got != tc.want {
			t.Errorf("triggerMatches(%q, %q) = %v; want %v", tc.trigger, tc.action, got, tc.want)
		}
	}
}
