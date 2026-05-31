# Releasing

Cadoo ships releases by pushing an **annotated git tag** named `vMAJOR.MINOR.PATCH`
to `origin`. The tag push is the only manual step — two GitHub Actions workflows do
the rest.

## Getting changes onto `main`

Releases tag `main`, so every change must land there through a pull request first.
Never commit feature or fix work directly to `main`.

1. Branch off an up-to-date `main` using a type prefix:

   ```sh
   git checkout main && git pull origin main
   git checkout -b feat/short-description   # new feature
   # or: git checkout -b fix/short-description   # bug fix
   ```

   Prefixes follow the project's history: `feat/` for features, `fix/` for bug
   fixes (e.g. `feat/test-gh-actions-review`, `fix/add-diff-anchor`).

2. Commit your work, run the same checks CI runs, and push the branch:

   ```sh
   make ci   # vet + test + build
   git push -u origin feat/short-description
   ```

3. Open a pull request **into `main`** and get it green:

   ```sh
   gh pr create --base main --fill
   ```

   CI (`ci.yml`) and the `cadoo/review` check-run must pass — see
   [`PRE_MERGE_GATES.md`](PRE_MERGE_GATES.md) for the branch-protection gate.

4. Merge the PR. Pushing `main` (and merges) also builds fresh `latest`/SHA images
   via [`docker-publish.yml`](../.github/workflows/docker-publish.yml), but does **not**
   cut a release — only a `v*` tag does that (below).

## What a tag push triggers

| Workflow | Trigger | Result |
| --- | --- | --- |
| [`release.yml`](../.github/workflows/release.yml) | push of any `v*` tag | GoReleaser (`--clean`) builds the five `cmd/*` binaries for linux/darwin (+ windows for `cadoo-cli`) across amd64/arm64, then publishes a **GitHub Release** with the archives. Version is stamped into `internal/version.Version` via ldflags. |
| [`docker-publish.yml`](../.github/workflows/docker-publish.yml) | push of any `v*` tag (also `main`) | Builds and pushes the five service images to `ghcr.io/payamqorbanpour/<cmd>` for linux/amd64+arm64, tagged with the version, the short SHA, and (on `main`) `latest`. |

The release artifact set is defined in [`.goreleaser.yaml`](../.goreleaser.yaml).

## Cutting a release

1. Make sure `main` is green and up to date locally:

   ```sh
   git checkout main
   git pull origin main
   make ci   # vet + test + build, the same checks CI runs
   ```

2. Pick the next version. Tags are forward-only and follow SemVer (`vMAJOR.MINOR.PATCH`).
   Check the latest with:

   ```sh
   git tag --sort=-creatordate | head -1
   ```

3. Create an **annotated** tag on the current `main` HEAD and push it:

   ```sh
   git tag -a v0.2.4 -m "v0.2.4"
   git push origin v0.2.4
   ```

   > Use `git tag -a` (annotated), not a lightweight tag — GoReleaser relies on the
   > annotated tag and a full-depth checkout to generate the changelog.

4. Watch the workflows finish:

   ```sh
   gh run watch          # or: gh run list --workflow=release.yml
   ```

   When both succeed you'll have a GitHub Release with binaries and fresh `ghcr.io`
   images tagged `v0.2.4`.

## Notes & recovery

- **Tag the merge commit on `main`** you want to ship. The tag, not the branch, is
  what gets built — pushing `main` afterward does not re-release an existing tag.
- **Mistyped/abandoned tag.** Delete it locally and remotely *before* re-tagging.
  Re-pushing the same tag name will not re-run a release cleanly:

  ```sh
  git tag -d v0.2.4
  git push origin :refs/tags/v0.2.4
  ```

- **Re-running a failed release.** Fix the cause on `main`, then bump to the next
  patch version rather than reusing the tag.
- GoReleaser version is pinned to `~> v2` in the workflow; keep `.goreleaser.yaml`
  on `version: 2`.
