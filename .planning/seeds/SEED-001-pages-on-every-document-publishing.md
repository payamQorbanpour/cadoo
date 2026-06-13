---
id: SEED-001
status: dormant
planted: 2026-06-13
planted_during: v1.1 — Phase 7 (Release-Docs Engineering Diagrams)
trigger_when: when scoping release-docs / GitHub Pages publishing work, or any milestone that touches document publishing or the pages publisher
scope: medium
---

# SEED-001: Create or update GitHub Pages on every document publishing

## Why This Matters

Today the release-docs pages publisher (`internal/releasedocs/publishers/pages/pages.go`)
emits artifacts (changelog, release-notes, blog, api-docs, and the upcoming diagrams)
to deterministic paths **at release time**, as part of a release run. The idea here is
to broaden that: whenever **any** document is published — not only on a tagged release —
Cadoo should ensure the corresponding GitHub Pages site is **created if absent or updated
in place** so the published docs are always live and current.

Value:
- Docs stay continuously in sync with what was published, instead of only refreshing on
  a release boundary.
- Removes the manual/implicit assumption that a GitHub Pages site already exists and is
  configured — "create or update" makes the publish path self-healing.
- Natural extension of the idempotent marker + deterministic-path model already used by
  the pages publisher; keeps publishing reproducible.

## When to Surface

**Trigger:** When scoping release-docs / GitHub Pages publishing work, or any milestone
that touches document publishing or the pages publisher.

This seed will surface during `/gsd:new-milestone` when the milestone scope matches —
especially a milestone framed around release-docs publishing, GitHub Pages, or
continuous documentation delivery. It deliberately overlaps with v1.1 (diagrams), which
adds a new artifact to the same pages publisher; this seed addresses **when/how the Pages
site itself is created and refreshed**, which is orthogonal to which artifacts exist.

## Scope Estimate

**Medium** — a phase or two. Likely touches:
- The pages publisher's create-vs-update logic (Pages site existence/enablement, branch
  vs. Actions source) rather than just writing files to a path.
- A `ReleasePublisher`-style optional `vcs` capability for Pages site provisioning
  (GitHub Pages API: enable Pages, configure source), mirroring the existing
  capability-interface pattern (`ReleasePublisher`, `BranchCommitter`) — degrade
  gracefully with a logged reason when the capability or permission is missing.
- Defining what "every document publishing" means operationally (every artifact write?
  every release-docs dispatch? a non-release publish trigger?) — an open design question
  for discuss-phase.
- Idempotency across re-runs (create-once, update-in-place), consistent with the existing
  marker + stored-state model.

Open questions for discuss/plan time:
- Does "every document publishing" introduce a **non-release** publish trigger, or does it
  only harden the existing release-time path to create-or-update the Pages site?
- GHES + GitLab Pages parity, or GitHub.com Pages first?
- Permissions/auth needed to enable Pages (the install/PAT must have Pages admin scope).

## Breadcrumbs

- `internal/releasedocs/publishers/pages/pages.go` — current pages publisher (writes
  artifacts to deterministic paths; idempotent across re-runs).
- `internal/releasedocs/publishers/pages/pages_test.go`,
  `pages_apidocs_test.go` — publisher test coverage / golden patterns to extend.
- `internal/vcs/vcs.go` — `ReleasePublisher` / optional capability interfaces; the place a
  Pages-provisioning capability (`PagesPublisher`?) would be declared.
- `internal/vcs/github/release.go`, `internal/vcs/gitlab/release.go` — per-provider
  release/publish implementations; Pages create-or-update would extend the GitHub one.
- `internal/releasedocs/dispatcher.go` + `registry.go` — where publishers are wired and
  run; a publish-time hook would live here.
- `internal/config/config.go` — `releaseDocs` config block; a `pages` create/enable option
  would be added here.
- Related decisions (PROJECT.md → Key Decisions): "Release/range/branch ops as optional
  `vcs` capability interfaces" and "Marker + stored-state idempotency" directly inform the
  create-or-update approach.

## Notes

Captured via `/gsd-new-milestone "Create or update github pages on every document
publishing"` on 2026-06-13. The active milestone v1.1 (Release-Docs Engineering Diagrams,
Phase 7) was discussed but not yet planned at capture time, so rather than reset STATE/
phases the idea was planted as a seed to surface for the next milestone after v1.1 ships.
Enriched at capture (trigger / why / scope filled).
