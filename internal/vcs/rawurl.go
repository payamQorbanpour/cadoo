package vcs

import "fmt"

// RawContentURL returns the provider-specific URL for fetching a raw file
// from a repository at a given ref. For KindGitHub, baseURL is ignored.
// For KindGitHubEnterprise and KindGitLab, baseURL is the scheme+host of the
// instance with no trailing slash (e.g. "https://ghe.example.com").
// Returns "" for unrecognised kinds.
func RawContentURL(kind Kind, baseURL, repoFullName, ref, path string) string {
	switch kind {
	case KindGitHub:
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repoFullName, ref, path)
	case KindGitHubEnterprise:
		return fmt.Sprintf("%s/%s/raw/%s/%s", baseURL, repoFullName, ref, path)
	case KindGitLab:
		return fmt.Sprintf("%s/%s/-/raw/%s/%s", baseURL, repoFullName, ref, path)
	default:
		return ""
	}
}
