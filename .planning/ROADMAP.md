# Roadmap: Cadoo — Release Docs

## Overview

This milestone adds Release Docs to Cadoo: a parallel `internal/releasedocs` subsystem that, after a release, generates and publishes documentation artifacts for the customer's repo. Delivery follows the SPEC's three-phase order. Phase 1 ships a stateless, dogfoodable `cadoo release-docs` CLI that generates changelog + release notes and publishes them to the release body and a `CHANGELOG.md` PR — proven on Cadoo's own repo. Phase 2 adds the webhook auto-trigger, DB-backed state, the pages publisher, and the blog generator. Phase 3 adds code-derived API docs / OpenAPI. Each phase is idempotent across re-runs and honors per-artifact toggles.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: Generators + Publishers + CLI** - Stateless `cadoo release-docs` generates changelog + release notes and publishes to release body + CHANGELOG.md PR; dogfooded on Cadoo's own repo
- [ ] **Phase 2: Webhook Auto-Trigger + State** - Release/tag webhook ingestion, ReleaseJob enqueue, worker consumer, DB state table, pages publisher, blog generator
- [ ] **Phase 3: API Docs / OpenAPI** - Code-derived extraction (narrow framework set), apidocs generator, pages output

## Phase Details

### Phase 1: Generators + Publishers + CLI
**Goal**: A maintainer can run `cadoo release-docs --repo … --from vX --to vY` and have Cadoo generate a grouped changelog section and polished release notes for that range and publish them — to the release body and to a single `CHANGELOG.md` PR — idempotently, with no DB, dogfooded on Cadoo's own repo.
**Depends on**: Nothing (first phase)
**Requirements**: REQ-release-artifact-generation (changelog + release-notes), REQ-per-artifact-toggles, REQ-configurable-templates, REQ-release-docs-idempotency (stateless/marker mode), REQ-configurable-trigger (manual CLI/CI entry point), REQ-publish-destinations (releasebody + changelogpr)
**Success Criteria** (what must be TRUE):
  1. Running `cadoo release-docs --repo … --from vX --to vY` produces a grouped changelog section (Features/Fixes/Breaking/…) and polished release notes for the commits/PRs in that range.
  2. The release body is updated inside Cadoo markers (user content preserved) and a single `CHANGELOG.md` PR is opened or updated for the release tag.
  3. Re-running the same command edits the release body in place and updates the same changelog PR/branch — no duplicates — with no database (stateless marker reconstruction).
  4. A disabled artifact (`enabled:false`) or one whose `when:` excludes the bump is never generated; the changelog runs with LLM off and produces reproducible output.
  5. An artifact's `template:` override (loaded from the tag tree) replaces the preset; with no override, embedded preset templates are used.
  6. The flow is dogfooded end-to-end on Cadoo's own repository.
**Plans**: TBD

Plans:
- [ ] 01-01: TBD

### Phase 2: Webhook Auto-Trigger + State
**Goal**: A published release (or configured tag push) on a customer repo automatically triggers release-docs generation through the dual-mode queue and worker, with DB-backed state so re-syncs edit in place, plus the pages publisher and blog generator.
**Depends on**: Phase 1
**Requirements**: REQ-configurable-trigger (release/tag webhook ingestion), REQ-release-docs-idempotency (DB-backed state table + migration), REQ-publish-destinations (pages), REQ-release-artifact-generation (blog generator)
**Success Criteria** (what must be TRUE):
  1. A GitHub `release: published` / GitLab release webhook (and, when configured, a `v*` tag push filtered by `tagPattern`) builds a `ReleaseJob` and enqueues it via River when `DATABASE_URL` is set or the in-memory queue otherwise; the worker consumes it and runs the dispatcher.
  2. If `releaseDocs.trigger` excludes the event kind, the webhook no-ops early.
  3. Published state is recorded in a new `(provider, repo, to_tag, artifact_kind)` table (migration round-trips `up → down → up`) so re-runs edit the release body and changelog PR in place when DB-backed.
  4. The pages publisher commits rendered artifacts to the configured `branch`/`dir` at deterministic paths (`docs/releases/vX.Y.Z/…`); re-runs overwrite the same paths.
  5. The blog generator produces a long-form announcement on minor/major releases (per its `when:` condition) and is routed to pages.
**Plans**: TBD

Plans:
- [ ] 02-01: TBD

### Phase 3: API Docs / OpenAPI
**Goal**: For a supported framework, Cadoo derives an OpenAPI spec and a rendered API reference from the repo's code at release time and publishes them to pages.
**Depends on**: Phase 2
**Requirements**: REQ-release-artifact-generation (api-docs + openapi)
**Success Criteria** (what must be TRUE):
  1. For a repo in the supported (narrow, well-supported) framework set, the apidocs generator produces an OpenAPI YAML spec and a rendered API reference from the code.
  2. The generated API docs and OpenAPI are published to pages at deterministic paths and are idempotent across re-runs.
  3. A repo outside the supported framework set degrades gracefully (apidocs skipped with a logged reason), without failing the rest of the release-docs run.
**Plans**: TBD

Plans:
- [ ] 03-01: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Generators + Publishers + CLI | 0/TBD | Not started | - |
| 2. Webhook Auto-Trigger + State | 0/TBD | Not started | - |
| 3. API Docs / OpenAPI | 0/TBD | Not started | - |
