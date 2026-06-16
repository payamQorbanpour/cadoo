package vcs

import "testing"

func TestRawContentURL(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		base string
		repo string
		ref  string
		path string
		want string
	}{
		{
			name: "github.com ignores baseURL",
			kind: KindGitHub,
			base: "https://wrong.example.com", // must be ignored for KindGitHub
			repo: "owner/repo",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://raw.githubusercontent.com/owner/repo/main/docs/assets/AI.png",
		},
		{
			name: "ghes",
			kind: KindGitHubEnterprise,
			base: "https://ghe.example.com",
			repo: "owner/repo",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://ghe.example.com/owner/repo/raw/main/docs/assets/AI.png",
		},
		{
			name: "ghes trailing slash tolerated",
			kind: KindGitHubEnterprise,
			base: "https://ghe.example.com/",
			repo: "owner/repo",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://ghe.example.com/owner/repo/raw/main/docs/assets/AI.png",
		},
		{
			name: "gitlab trailing slash tolerated",
			kind: KindGitLab,
			base: "https://gitlab.example.com/",
			repo: "group/project",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://gitlab.example.com/group/project/-/raw/main/docs/assets/AI.png",
		},
		{
			name: "gitlab.com",
			kind: KindGitLab,
			base: "https://gitlab.com",
			repo: "group/project",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://gitlab.com/group/project/-/raw/main/docs/assets/AI.png",
		},
		{
			name: "gitlab self-managed",
			kind: KindGitLab,
			base: "https://gitlab.example.com",
			repo: "group/subgroup/project",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://gitlab.example.com/group/subgroup/project/-/raw/main/docs/assets/AI.png",
		},
		{
			name: "unknown kind returns empty",
			kind: Kind("unknown"),
			base: "https://example.com",
			repo: "owner/repo",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RawContentURL(tc.kind, tc.base, tc.repo, tc.ref, tc.path)
			if got != tc.want {
				t.Errorf("RawContentURL = %q; want %q", got, tc.want)
			}
		})
	}
}
