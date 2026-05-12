// Package settings holds process-level runtime configuration loaded from
// environment variables. This is distinct from internal/config (which is the
// per-repo .cadoo.yaml).
package settings

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Settings captures everything a Cadoo process needs at startup.
type Settings struct {
	HTTPAddr         string
	DatabaseURL      string
	LLMGatewayURL    string
	LLMGatewayAPIKey string
	DefaultModel     string

	// GitHub App credentials. PrivateKeyPEM is the contents of the .pem file.
	// GitHubBaseURL is empty for github.com; set for GHES
	// (e.g. "https://ghe.example.com/api/v3").
	GitHubBaseURL          string
	GitHubUploadURL        string
	GitHubAppID            int64
	GitHubAppPrivateKeyPEM []byte
	GitHubWebhookSecret    string

	// For SaaS, InstallationID is looked up per repo. For dev/single-tenant,
	// a default install ID can be set so the webhook works without a DB.
	GitHubDefaultInstallationID int64

	// GitLab credentials. Empty BaseURL targets gitlab.com.
	GitLabBaseURL       string
	GitLabToken         string
	GitLabWebhookSecret string

	// Issue tracker credentials. All optional.
	JiraBaseURL  string
	JiraEmail    string // for Cloud basic auth; leave empty for PAT bearer
	JiraToken    string
	LinearAPIKey string

	// Notifier targets. All optional.
	SlackWebhookURL string

	// OIDC (for cadoo-api). Both required to enable /v1/* routes; otherwise
	// the API serves only public endpoints (/healthz, /version, /metrics).
	OIDCIssuer   string
	OIDCClientID string

	// Reports. Empty == disabled. Parsed as Go duration (e.g. "24h", "168h").
	ReportsInterval string

	// Sandboxed analysis. When SandboxImage is non-empty, the dispatcher
	// runs the bundled linters via Docker against that image (typically the
	// pre-baked cadoo/sandbox-polyglot). DockerBin overrides the docker CLI
	// path; defaults to "docker".
	SandboxImage     string
	SandboxDockerBin string

	// FindingsCacheFile is an optional JSON file path used by the in-memory
	// findings store to persist dedup state across container restarts. Only
	// consulted when DATABASE_URL is unset (i.e. no Postgres backend); when
	// empty, the in-memory store is purely process-local.
	FindingsCacheFile string
}

// FromEnv reads settings from process environment.
func FromEnv() (*Settings, error) {
	s := &Settings{
		HTTPAddr:         envOr("HTTP_ADDR", ":8081"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		LLMGatewayURL:    envOr("LLM_GATEWAY_URL", "http://litellm:4000/v1"),
		LLMGatewayAPIKey: os.Getenv("LLM_GATEWAY_API_KEY"),
		DefaultModel:     os.Getenv("CADOO_DEFAULT_MODEL"),

		GitHubBaseURL:       os.Getenv("GITHUB_BASE_URL"),
		GitHubUploadURL:     os.Getenv("GITHUB_UPLOAD_URL"),
		GitHubWebhookSecret: os.Getenv("GITHUB_WEBHOOK_SECRET"),

		GitLabBaseURL:       os.Getenv("GITLAB_BASE_URL"),
		GitLabToken:         os.Getenv("GITLAB_TOKEN"),
		GitLabWebhookSecret: os.Getenv("GITLAB_WEBHOOK_SECRET"),

		JiraBaseURL:  os.Getenv("JIRA_BASE_URL"),
		JiraEmail:    os.Getenv("JIRA_EMAIL"),
		JiraToken:    os.Getenv("JIRA_TOKEN"),
		LinearAPIKey: os.Getenv("LINEAR_API_KEY"),

		SlackWebhookURL: os.Getenv("SLACK_WEBHOOK_URL"),

		OIDCIssuer:      os.Getenv("OIDC_ISSUER"),
		OIDCClientID:    os.Getenv("OIDC_CLIENT_ID"),
		ReportsInterval: os.Getenv("REPORTS_INTERVAL"),

		SandboxImage:      os.Getenv("CADOO_SANDBOX_IMAGE"),
		SandboxDockerBin:  os.Getenv("CADOO_SANDBOX_DOCKER_BIN"),
		FindingsCacheFile: os.Getenv("CADOO_FINDINGS_CACHE_FILE"),
	}

	if v := os.Getenv("GITHUB_APP_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("GITHUB_APP_ID: %w", err)
		}
		s.GitHubAppID = id
	}
	if v := os.Getenv("GITHUB_INSTALLATION_ID"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("GITHUB_INSTALLATION_ID: %w", err)
		}
		s.GitHubDefaultInstallationID = id
	}
	if path := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"); path != "" {
		key, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read GITHUB_APP_PRIVATE_KEY_PATH=%s: %w", path, err)
		}
		s.GitHubAppPrivateKeyPEM = key
	} else if pem := os.Getenv("GITHUB_APP_PRIVATE_KEY"); pem != "" {
		s.GitHubAppPrivateKeyPEM = []byte(pem)
	}
	return s, nil
}

// HasGitHub reports whether the env supplied a complete GitHub App config.
func (s *Settings) HasGitHub() bool {
	return s.GitHubAppID != 0 &&
		len(s.GitHubAppPrivateKeyPEM) > 0 &&
		s.GitHubDefaultInstallationID != 0
}

// HasGitLab reports whether the env supplied a complete GitLab config.
func (s *Settings) HasGitLab() bool {
	return s.GitLabToken != "" && s.GitLabWebhookSecret != ""
}

// RequireGitHub returns an error if any GitHub App field is missing.
func (s *Settings) RequireGitHub() error {
	var missing []string
	if s.GitHubAppID == 0 {
		missing = append(missing, "GITHUB_APP_ID")
	}
	if len(s.GitHubAppPrivateKeyPEM) == 0 {
		missing = append(missing, "GITHUB_APP_PRIVATE_KEY(_PATH)")
	}
	if s.GitHubWebhookSecret == "" {
		missing = append(missing, "GITHUB_WEBHOOK_SECRET")
	}
	if len(missing) > 0 {
		return errors.New("missing required env: " + strings.Join(missing, ", "))
	}
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
