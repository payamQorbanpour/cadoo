---
status: approved
phase: 01-generators-publishers-cli
source: [01-VERIFICATION.md]
started: 2026-06-05T17:25:00Z
updated: 2026-06-05T17:30:00Z
---

## Current Test

[operator approved 2026-06-05]

## Tests

### 1. Dogfood end-to-end idempotency (SC-6)
expected: Run `cadoo release-docs --repo payamqorbanpour/cadoo --from <prevTag> --to <tag>` twice with a valid GITHUB_TOKEN. First run opens exactly one CHANGELOG.md PR on branch `cadoo/changelog/<tag>` with grouped sections; release body updated inside markers. Second run edits the same PR/branch and release body in place — no duplicate PR, no duplicate marker block. User content outside markers preserved.
result: skipped — no GITHUB_TOKEN available at phase close; operator approved skip, will verify on first live run

### 2. GitLab release-body degradation (CR-01)
expected: Decide and document: either fix `gitlab.UpdateReleaseBody` to use `TagName` instead of numeric ID (removing the unconditional error), or accept as a known limitation and open a follow-up issue. GitLab repos with `publish.releaseBody.enabled: true` currently hard-fail.
result: accepted as known limitation — GitLab users must not enable `publish.releaseBody.enabled: true` until CR-01 is fixed in a follow-up; GitHub/GHES unaffected

## Summary

total: 2
passed: 0
issues: 0
pending: 0
skipped: 1
blocked: 0
accepted: 1

## Gaps
