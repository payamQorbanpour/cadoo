package main

import (
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestParseTargetURL(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantProvider vcs.Kind
		wantBase     string
		wantAPIBase  string
		wantProject  string
		wantNumber   int64
		wantErr      bool
	}{
		{
			name:         "gitlab.com modern",
			in:           "https://gitlab.com/group/project/-/merge_requests/42",
			wantProvider: vcs.KindGitLab,
			wantBase:     "https://gitlab.com",
			wantAPIBase:  "https://gitlab.com/api/v4",
			wantProject:  "group/project",
			wantNumber:   42,
		},
		{
			name:         "self-managed nested groups",
			in:           "https://gitlab.example.com/group/subgroup/project/-/merge_requests/7",
			wantProvider: vcs.KindGitLab,
			wantBase:     "https://gitlab.example.com",
			wantAPIBase:  "https://gitlab.example.com/api/v4",
			wantProject:  "group/subgroup/project",
			wantNumber:   7,
		},
		{
			name:         "legacy without /-/",
			in:           "https://gitlab.example.com/group/project/merge_requests/3",
			wantProvider: vcs.KindGitLab,
			wantBase:     "https://gitlab.example.com",
			wantAPIBase:  "https://gitlab.example.com/api/v4",
			wantProject:  "group/project",
			wantNumber:   3,
		},
		{
			name:         "gitlab trailing /diffs",
			in:           "https://gitlab.com/g/p/-/merge_requests/12/diffs",
			wantProvider: vcs.KindGitLab,
			wantBase:     "https://gitlab.com",
			wantAPIBase:  "https://gitlab.com/api/v4",
			wantProject:  "g/p",
			wantNumber:   12,
		},
		{
			name:         "github.com pull",
			in:           "https://github.com/owner/repo/pull/42",
			wantProvider: vcs.KindGitHub,
			wantBase:     "https://github.com",
			wantAPIBase:  "",
			wantProject:  "owner/repo",
			wantNumber:   42,
		},
		{
			name:         "github.com trailing /files",
			in:           "https://github.com/owner/repo/pull/12/files",
			wantProvider: vcs.KindGitHub,
			wantBase:     "https://github.com",
			wantAPIBase:  "",
			wantProject:  "owner/repo",
			wantNumber:   12,
		},
		{
			name:         "GHES pull",
			in:           "https://ghe.example.com/owner/repo/pull/7",
			wantProvider: vcs.KindGitHubEnterprise,
			wantBase:     "https://ghe.example.com",
			wantAPIBase:  "https://ghe.example.com/api/v3",
			wantProject:  "owner/repo",
			wantNumber:   7,
		},
		{
			name:    "github too many path segments",
			in:      "https://github.com/owner/group/repo/pull/1",
			wantErr: true,
		},
		{
			name:    "missing iid",
			in:      "https://gitlab.com/g/p/-/merge_requests/",
			wantErr: true,
		},
		{
			name:    "not an mr or pr url",
			in:      "https://gitlab.com/g/p/-/issues/1",
			wantErr: true,
		},
		{
			name:    "non-numeric iid",
			in:      "https://gitlab.com/g/p/-/merge_requests/abc",
			wantErr: true,
		},
		{
			name:    "non-numeric pr number",
			in:      "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
		{
			name:    "missing scheme",
			in:      "gitlab.com/g/p/-/merge_requests/1",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTargetURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Provider != tc.wantProvider {
				t.Errorf("Provider = %q, want %q", got.Provider, tc.wantProvider)
			}
			if got.BaseURL != tc.wantBase {
				t.Errorf("BaseURL = %q, want %q", got.BaseURL, tc.wantBase)
			}
			if got.APIBaseURL != tc.wantAPIBase {
				t.Errorf("APIBaseURL = %q, want %q", got.APIBaseURL, tc.wantAPIBase)
			}
			if got.ProjectPath != tc.wantProject {
				t.Errorf("ProjectPath = %q, want %q", got.ProjectPath, tc.wantProject)
			}
			if got.Number != tc.wantNumber {
				t.Errorf("Number = %d, want %d", got.Number, tc.wantNumber)
			}
		})
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{" describe , review ,improve", []string{"describe", "review", "improve"}},
		{",,review,,", []string{"review"}},
		{"", []string{}},
	}
	for _, tc := range cases {
		got := splitCSV(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
