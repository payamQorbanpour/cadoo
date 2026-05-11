# Pre-merge gates

Cadoo posts a check run named `cadoo/review` on every `/review` dispatch.
Wire it into your branch protection to gate merges on Cadoo's verdict.

## How the verdict is computed

For `/review`:

- The check run's `conclusion` is **failure** if any finding has a severity
  that appears in `review.request_changes_on` (default: `["block"]`).
- Otherwise, **success** — even when warns/nits were posted.
- If Cadoo crashed or the model timed out, the check run's `conclusion` is
  **failure** with the error in `output.summary`.

## GitHub branch protection

1. Open **Settings → Branches → Branch protection rules → Edit/Add**.
2. Under **Require status checks to pass before merging**, add `cadoo/review`.
3. Optionally enable **Require branches to be up to date before merging** so
   the gate re-evaluates on every push.

## GitLab merge requests

GitLab maps Cadoo's check run to a [commit status](https://docs.gitlab.com/ee/api/commits.html#post-the-build-status-to-a-commit) named `cadoo/review`.

1. Project → Settings → Merge requests → **Pipelines must succeed**.
2. Project → Settings → CI/CD → Variables, then add a custom rule that
   requires the `cadoo/review` external status to be `success`.

## Tuning per repo

`.cadoo.yaml`:

```yaml
review:
  severity_threshold: warn        # block | warn | nit
  request_changes_on: ["block"]   # which severities flip the gate to failure
```

To make warns also block merging, set `request_changes_on: ["block", "warn"]`.

## Custom checks as gates

The `/check` tool runs every entry in `.cadoo.yaml` `checks:`. Today its check
run uses the per-tool default name (`cadoo`); if you want a separate gate per
check, run `/check` on a label and configure branch protection accordingly.
A dedicated per-check check-run name is on the Phase 7.x roadmap.
