# Testing Patterns
_Last updated: 2026-05-19_

## Test Framework

**Runner:** Go stdlib `testing` package — no third-party test framework.
**Assertion style:** Manual `t.Fatal` / `t.Fatalf` / `t.Error` / `t.Errorf` — no assertion libraries (testify, etc.).
**HTTP mocking:** `net/http/httptest` (stdlib).
**Run commands:**
```bash
make test                           # go test -race -count=1 ./...
go test -race -run TestFooBar ./internal/orchestrator/...  # single test
```

---

## Test File Organization

- **Co-located:** every `*_test.go` file lives in the same directory as the code it tests.
- **Naming:** `<package>_test.go` for the primary test file, additional files allowed (`prior_test.go` alongside `findings_test.go` in `internal/findings/`).
- **Package:** tests use the same package as the source (`package orchestrator`, `package vcs`, `package findings`) — white-box testing is the norm. No `_test` package suffix is used.
- **Helpers:** small helper functions in the same file, or in a `helpers_test.go` file visible only to the test binary (example: `internal/config/helpers_test.go` exports `writeFile`).

---

## Test Coverage by Package

| Package | Source files | Test files | Notes |
|---------|-------------|------------|-------|
| `internal/orchestrator` | 4 | 2 | `reviewer_test.go`, `consolidate_test.go` — core dispatch + idempotency |
| `internal/findings` | 2 | 2 | `findings_test.go`, `prior_test.go` — dedup, structural keys, Jaccard |
| `internal/vcs` | 2 | 1 | `marker_test.go` — marker round-trip |
| `internal/vcs/github` | 3 | 3 | GraphQL pagination, `ListCadooArtifacts`, resolve thread |
| `internal/vcs/gitlab` | 2 | 2 | Hunk parsing, diff indexing, webhook |
| `internal/llm` | 2 | 1 | HTTP retry behavior |
| `internal/llm/litellm` | 1 | 1 | Chat round-trip via `httptest` |
| `internal/llm/embed` | 1 | 1 | Embedding round-trip |
| `internal/tools` | 4 | 3 | Registry, `ExtractJSON`, prompt helpers |
| `internal/tools/review` | 1 | 1 | Full tool run with fake LLM |
| `internal/tools/describe` | 1 | 1 | |
| `internal/tools/resolveconflicts` | 1 | 1 | |
| `internal/contextengine` | 1 | 1 | Compress filter+pack |
| `internal/config` | 1 | 2 | YAML load, helpers |
| `internal/slop` | 1 | 1 | Score detection |
| `internal/issuetrackers` | 1 | 1 | Key extraction |
| `internal/analysis/workspace` | 1 | 1 | Archive extraction + path traversal guard |
| `internal/analysis/sandbox` | 1 | 1 | |
| `internal/auth` | 2 | 1 | |
| `internal/mcp` | 1 | 1 | |
| `internal/metrics` | 1 | 1 | |
| `internal/audit` | 1 | 1 | |
| `internal/learnings` | 1 | 1 | |
| `internal/jobs` | 1 | 1 | |
| `internal/kb` | 2 | 1 | |
| `internal/notifiers/slack` | 1 | 1 | |
| `internal/issuetrackers/jira` | 1 | 1 | |
| `internal/issuetrackers/linear` | 1 | 1 | |
| `internal/reports` | 1 | 1 | |
| `cmd/cadoo-cli` | — | 1 | URL parsing, prior-store seeding, `splitCSV` |
| `cmd/cadoo-webhook` | — | 1 | |
| `internal/analysis/eslint` etc. | 1 each | 1 each | Static-analysis runners |

**No test files:**
`internal/db`, `internal/httpx`, `internal/riverq`, `internal/settings`, `internal/tools/adddocs`, `internal/tools/addtests`, `internal/tools/ask`, `internal/tools/changelog`, `internal/tools/check`, `internal/tools/deepreview`, `internal/tools/improve`, `internal/tools/learn`, `internal/tools/plan`, `internal/tools/unlearn`, `internal/kb/querydistill`, `internal/version`

---

## Test Patterns

### Table-Driven Tests

The dominant pattern for functions with many input/output cases:

```go
cases := []struct {
    name         string
    in           string
    wantProvider vcs.Kind
    wantErr      bool
}{
    {"gitlab.com modern", "https://gitlab.com/...", vcs.KindGitLab, false},
    {"missing scheme", "gitlab.com/...", vcs.KindGitHub, true},
}
for _, tc := range cases {
    t.Run(tc.name, func(t *testing.T) {
        got, err := parseTargetURL(tc.in)
        if tc.wantErr {
            if err == nil { t.Fatalf("expected error, got %+v", got) }
            return
        }
        ...
    })
}
```

Examples: `cmd/cadoo-cli/ci_test.go:TestParseTargetURL`, `internal/vcs/gitlab/gitlab_test.go:TestParseHunkHeader`, `internal/issuetrackers/issues_test.go`.

### Scenario / Multi-Run Tests

For testing stateful idempotency across multiple simulated runs:

```go
// Run 1: post two findings
d1 := &Dispatcher{Posted: findings.NewFromPrior(key, vcs.PriorReview{})}
d1.postInline(ctx, sv, pr, key, "review", []vcs.InlineComment{c1, c2})

// Capture state
prior := sv.replay()
sv.inline = nil

// Run 2: one finding gone, one new
d2 := &Dispatcher{Posted: findings.NewFromPrior(key, prior)}
d2.postInline(ctx, sv, pr, key, "review", []vcs.InlineComment{c1, c3})

// Assert: c2 resolved, c3 posted, c1 deduped
```

Examples: `internal/orchestrator/reviewer_test.go:TestCIModeTwoRunIdempotency`, `TestCIModeSuppressesRephrasedImproveOnPush2`.

### Fake/Stub Implementations

Tests define local fake implementations of interfaces rather than using a mock library:

```go
// Fake VCS provider — satisfies vcs.Provider
type fakeVCS struct {
    kind      vcs.Kind
    pr        *vcs.PullRequest
    files     []vcs.FileChange
    mu        sync.Mutex
    inlineCnt int
    summary   string
}
func (f *fakeVCS) Kind() vcs.Kind { return f.kind }
func (f *fakeVCS) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, body string) (string, error) {
    f.mu.Lock(); defer f.mu.Unlock()
    f.summary = body; return "1", nil
}
// ... all interface methods implemented

// Fake LLM provider
type fakeLLM struct{ body string }
func (f *fakeLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
    return &llm.ChatResponse{Content: f.body}, nil
}

// Capturing tool — records what Input was passed
type capturingTool struct {
    name string
    res  *tools.Result
    got  *tools.Input
}
```

Examples: `internal/orchestrator/reviewer_test.go`, `internal/tools/review/review_test.go`.

Fakes are composed using embedding when additional behavior is needed:

```go
type idVCS struct {
    fakeVCS       // embed base fake
    nextID int    // add new state
}
func (f *idVCS) PostInlineComments(...) { ... } // override one method
```

### HTTP Fake Servers (`httptest`)

For packages that call external HTTP APIs (LLM, VCS GraphQL, auth endpoints):

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Inspect request, return canned response
    _, _ = w.Write([]byte(`{"data":{...}}`))
}))
defer srv.Close()

adapter, _ := New(Config{Token: "t"})
adapter.gqlEndpoint = srv.URL   // override endpoint
adapter.gqlClient = srv.Client()
```

Examples: `internal/vcs/github/github_test.go`, `internal/llm/retry_test.go`, `internal/llm/litellm/client_test.go`.

### Nil-Tolerant Tests

Testing that nil-receiver methods are no-ops:

```go
func TestNilStoreIsNoop(t *testing.T) {
    var s *Store
    if got, err := s.HasFinding(ctx, PRKey{}, "review", c); got || err != nil {
        t.Errorf("nil HasFinding: %v %v", got, err)
    }
}
```

Example: `internal/findings/findings_test.go:TestNilStoreIsNoop`.

### Regression Comment Pattern

Tests for previously-seen bugs include a prose comment describing the bug, the fix, and what would break if the fix were removed:

```go
// TestCIModeSuppressesRephrasedImproveOnPush2 is the regression test for the
// CI-mode runaway duplication bug: "improve" suggestions were re-posted on
// every push because the seeded in-memory store only stored the first-line
// normalised title ("suggestions:"), which made the Jaccard fallback useless...
func TestCIModeSuppressesRephrasedImproveOnPush2(t *testing.T) { ... }
```

---

## Test Data / Fixtures

- No fixtures directory. Test data is **inline** in the test function as struct literals or string constants.
- Files that need temp-dir–based fixtures use `t.TempDir()` (auto-cleaned):
  ```go
  dir := t.TempDir()
  path := filepath.Join(dir, ".cadoo.yaml")
  _ = writeFile(path, `model: claude-opus-4-7\n...`)
  ```
  Examples: `internal/config/config_test.go`, `internal/analysis/workspace/workspace_test.go`.
- Canned archive data (tar.gz) is built in-process via helper functions marked `t.Helper()`.

---

## Database Testing

**No DB integration tests in the test suite.** The `internal/findings` package (which owns the Postgres-backed `Store`) is tested entirely with in-memory implementations (`findings.NewMemory`). The `internal/db` and `internal/riverq` packages have **no test files**.

The CI pipeline has a separate **`migrations` job** that spins up a real `pgvector/pgvector:pg16` Postgres container and runs `goose up → down → up` to verify round-trip correctness of migration files in `db/migrations/`. This is schema validation only — no application-level queries are tested.

---

## CI Test Stages

Defined in `.github/workflows/ci.yml`. Three parallel jobs on `ubuntu-latest`:

| Job | Command | What it tests |
|-----|---------|--------------|
| `build` | `go vet ./...` → `make build` → `go test -race -count=1 ./...` | Correctness, race detector, all packages |
| `lint` | `golangci-lint run` v2.12.2 | Style, unused code, error handling |
| `migrations` | `goose up → down → up` against pgvector/pg16 | Migration round-trip |

All three must pass for a PR to merge. The `-race` flag is mandatory; `-count=1` disables test caching.

---

## What Is Well-Tested

- **Dedup/idempotency logic** (`internal/findings/`, `internal/orchestrator/`) — extensive scenario tests cover exact match, line-shift dedup, Jaccard fuzzy match, intra-batch dedup, legacy marker compatibility.
- **Dispatch routing** (`internal/orchestrator/reviewer_test.go`) — all routing branches covered (unknown tool, unknown provider, default provider, multi-provider).
- **VCS adapter internals** — hunk parsing, diff indexing, GraphQL pagination, `ListCadooArtifacts` marker parsing, `ResolveThread`.
- **HTTP retry logic** (`internal/llm/retry_test.go`) — 5xx retry, give-up after 3, no-retry on 4xx.
- **URL parsing** (`cmd/cadoo-cli/ci_test.go`) — all URL shapes: github.com, GHES, gitlab.com, self-managed, legacy paths, error cases.
- **Wrapper marker round-trip** (`internal/vcs/marker_test.go`) — with/without NT field, legacy format backward compatibility.

## Coverage Gaps

| Area | Gap | Risk |
|------|-----|------|
| `internal/tools/improve` | No tests | LLM response parsing / suggestion formatting untested |
| `internal/tools/deepreview` | No tests | Agentic pipeline untested |
| `internal/tools/changelog`, `ask`, `plan`, `check`, `adddocs`, `addtests` | No tests | Output shaping, severity filtering |
| `internal/db` | No tests | SQL query correctness only verified at migration level |
| `internal/riverq` | No tests | River job enqueueing untested in isolation |
| `internal/settings` | No tests | `FromEnv()` environment parsing untested |
| `internal/httpx` | No tests | HTTP middleware behavior untested |
| `internal/kb/querydistill` | No tests | LLM distillation fallback path |
