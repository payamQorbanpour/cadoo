## Conflict Detection Report

Mode: new
Docs ingested: 1 (SPEC: 1)
Cross-ref cycle check: run (no cycles; cross_refs point only to code modules/files, not to other
ingested docs, so the cross-doc graph is empty).
Precedence: ADR > SPEC > PRD > DOC (no per-doc overrides; the one doc carries `precedence: null`).

### BLOCKERS (0)

None.

No LOCKED ADRs are present (the only doc is a SPEC with `locked: false`), so no LOCKED-vs-LOCKED
contradiction is possible. No UNKNOWN/low-confidence docs (the single doc is `type: SPEC`,
`confidence: high`). No cross-ref cycles. Mode is `new`, so no existing-CONTEXT.md conflicts apply.

### WARNINGS (0)

None.

Competing acceptance variants require two or more PRDs defining requirements on the same scope with
divergent acceptance criteria. No PRDs are present and only one source exists, so no competing variants
can arise.

### INFO (0)

None.

Auto-resolution requires a lower-precedence source contradicting a higher-precedence one (e.g. SPEC vs
ADR). With a single source there is nothing to resolve; all extracted intel was carried through verbatim
with provenance.
