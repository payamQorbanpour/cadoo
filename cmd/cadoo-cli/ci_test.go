package main

import "testing"

func TestParseMRURL(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantBase    string
		wantProject string
		wantIID     int64
		wantErr     bool
	}{
		{
			name:        "gitlab.com modern",
			in:          "https://gitlab.com/group/project/-/merge_requests/42",
			wantBase:    "https://gitlab.com",
			wantProject: "group/project",
			wantIID:     42,
		},
		{
			name:        "self-managed nested groups",
			in:          "https://gitlab.example.com/group/subgroup/project/-/merge_requests/7",
			wantBase:    "https://gitlab.example.com",
			wantProject: "group/subgroup/project",
			wantIID:     7,
		},
		{
			name:        "legacy without /-/",
			in:          "https://gitlab.example.com/group/project/merge_requests/3",
			wantBase:    "https://gitlab.example.com",
			wantProject: "group/project",
			wantIID:     3,
		},
		{
			name:        "trailing /diffs",
			in:          "https://gitlab.com/g/p/-/merge_requests/12/diffs",
			wantBase:    "https://gitlab.com",
			wantProject: "g/p",
			wantIID:     12,
		},
		{
			name:    "missing iid",
			in:      "https://gitlab.com/g/p/-/merge_requests/",
			wantErr: true,
		},
		{
			name:    "not an mr url",
			in:      "https://gitlab.com/g/p/-/issues/1",
			wantErr: true,
		},
		{
			name:    "non-numeric iid",
			in:      "https://gitlab.com/g/p/-/merge_requests/abc",
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
			got, err := parseMRURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.BaseURL != tc.wantBase {
				t.Errorf("BaseURL = %q, want %q", got.BaseURL, tc.wantBase)
			}
			if got.APIBaseURL != tc.wantBase+"/api/v4" {
				t.Errorf("APIBaseURL = %q, want %q", got.APIBaseURL, tc.wantBase+"/api/v4")
			}
			if got.ProjectPath != tc.wantProject {
				t.Errorf("ProjectPath = %q, want %q", got.ProjectPath, tc.wantProject)
			}
			if got.IID != tc.wantIID {
				t.Errorf("IID = %d, want %d", got.IID, tc.wantIID)
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
