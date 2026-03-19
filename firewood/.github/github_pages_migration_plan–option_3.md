# GitHub Pages Migration Plan — Option 3: Cross-Repo Deployment

**Goal:** Move all CI workflows (including GitHub Pages deployment) from
`ava-labs/firewood` to `ava-labs/avalanchego`, while keeping the published
content available at the **original URL**: `https://ava-labs.github.io/firewood/`.

**Approach:** The `avalanchego` CI builds the site and pushes it to the
`firewood` repo's `gh-pages` branch via a deploy key. GitHub Pages on
`firewood` serves that branch.

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Create deploy key for cross-repo push](#2-create-deploy-key-for-cross-repo-push)
3. [Benchmark-data branch location](#3-benchmark-data-branch-location)
4. [Create the GitHub Pages workflow in avalanchego](#4-create-the-github-pages-workflow-in-avalanchego)
5. [Track-performance workflow (already migrated)](#5-track-performance-workflow-already-migrated)
6. [Copy supporting files to avalanchego](#6-copy-supporting-files-to-avalanchego)
7. [Configure GitHub Pages source on firewood](#7-configure-github-pages-source-on-firewood)
8. [Test end-to-end](#8-test-end-to-end)
9. [Cutover](#9-cutover)
10. [Post-migration cleanup](#10-post-migration-cleanup)
11. [Rollback plan](#11-rollback-plan)

---

## 1. Prerequisites

| Item | Detail |
|------|--------|
| GitHub admin access | Required on **both** `ava-labs/firewood` and `ava-labs/avalanchego` repos |
| `gh` CLI authenticated | With a token that has `repo` and `admin:public_key` scopes |
| Firewood source in avalanchego | The Firewood Rust workspace must already be present in the `avalanchego` monorepo (or accessible as a path dependency) so `cargo doc` can run there |

> **Important:** The workflows below assume the Firewood Rust crates live under
> a known path inside `avalanchego` (e.g., `firewood/`). Adjust `working-directory`
> and paths accordingly.

---

## 2. Create deploy key for cross-repo push

A deploy key lets `avalanchego` CI push to the `firewood` repo without a PAT.

### 2.1 Generate an SSH keypair (locally, once)

```bash
ssh-keygen -t ed25519 -C "avalanchego-to-firewood-pages" -f /tmp/firewood-pages-deploy-key -N ""
```

This produces two files:

- `/tmp/firewood-pages-deploy-key` — **private** key
- `/tmp/firewood-pages-deploy-key.pub` — **public** key

### 2.2 Add the public key to `firewood` as a deploy key (write access)

```bash
gh repo deploy-key add /tmp/firewood-pages-deploy-key.pub \
  --repo ava-labs/firewood \
  --title "avalanchego gh-pages deploy" \
  --allow-write
```

Or via the GitHub UI: `firewood` → Settings → Deploy keys → Add deploy key →
paste the `.pub` content → check **Allow write access**.

### 2.3 Add the private key to `avalanchego` as a secret

```bash
gh secret set FIREWOOD_PAGES_DEPLOY_KEY \
  --repo ava-labs/avalanchego \
  < /tmp/firewood-pages-deploy-key
```

### 2.4 Delete the local keypair

```bash
rm /tmp/firewood-pages-deploy-key /tmp/firewood-pages-deploy-key.pub
```

---

## 3. Benchmark-data branch location

The `track-performance` workflow has already been migrated to avalanchego as
`firewood-track-performance.yml`. It uses `auto-push: true` with `GITHUB_TOKEN`,
which pushes benchmark results to **avalanchego's** own `benchmark-data` branch
(not firewood's). Historical data lives in `bench/` and `dev/bench/` directories
on that branch.

> **Note:** The `benchmark-data` branch in avalanchego will be created
> automatically on the first successful `track-performance` run. No manual
> branch migration is needed.

---

## 4. Create the GitHub Pages workflow in avalanchego

Create the file at `.github/workflows/firewood-gh-pages.yaml` in `avalanchego`:

```yaml
name: firewood-gh-pages

on:
  push:
    branches:
      - "master"  # avalanchego's main branch
    paths:
      - "firewood/**"  # Only rebuild when firewood code changes
      - ".github/workflows/firewood-gh-pages.yaml"
  # Rebuild after benchmark workflow completes
  workflow_run:
    workflows: ["C-Chain Reexecution Performance Tracking"]
    types: [completed]
  # Allow manual trigger
  workflow_dispatch:

env:
  CARGO_TERM_COLOR: always

jobs:
  build:
    if: github.event_name != 'workflow_run' || github.event.workflow_run.conclusion == 'success'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          ref: master  # Always build docs from master

      - uses: dtolnay/rust-toolchain@stable

      - uses: Swatinem/rust-cache@v2
        with:
          save-if: "false"
          # Adjust workspaces if firewood is under a subdirectory
          workspaces: "firewood -> target"

      - name: Build firewood docs
        # Adjust working-directory to wherever the firewood workspace lives
        working-directory: firewood
        run: cargo doc --document-private-items --no-deps

      - name: Assemble _site with redirect
        run: |
          rm -fr _site
          mkdir _site
          echo '<meta http-equiv="refresh" content="0; url=firewood">' > _site/index.html

      - name: Copy doc files to _site
        run: |
          # Adjust the source path to match the actual target/doc location
          cp -rv firewood/target/doc/* ./_site
          cp -rv firewood/docs/assets ./_site

      # Fetch benchmark data from avalanchego's own benchmark-data branch
      - name: Include benchmark data
        run: |
          if ! git fetch origin benchmark-data 2>/dev/null; then
            echo "No benchmark-data branch yet, skipping"
            exit 0
          fi
          git checkout origin/benchmark-data -- dev bench 2>/dev/null || true
          [[ -d dev ]]   && cp -rv dev   _site/
          [[ -d bench ]] && cp -rv bench _site/
          [[ -d _site/dev || -d _site/bench ]] || {
            echo "::warning::No benchmark data (dev/ or bench/) found"
          }

      - uses: actions/upload-artifact@v4
        with:
          name: firewood-pages
          path: _site
          if-no-files-found: error
          overwrite: true
          include-hidden-files: true

  deploy:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - name: Download pages artifact
        uses: actions/download-artifact@v4
        with:
          name: firewood-pages
          path: _site

      - name: Deploy to firewood repo via deploy key
        uses: peaceiris/actions-gh-pages@v4
        with:
          deploy_key: ${{ secrets.FIREWOOD_PAGES_DEPLOY_KEY }}
          external_repository: ava-labs/firewood
          publish_branch: gh-pages
          publish_dir: ./_site
          # Keep existing files not in _site (e.g., CNAME) — disable if not needed
          keep_files: false
```

### Key differences from the original workflow

| Aspect | Original (firewood) | New (avalanchego) |
|--------|---------------------|-------------------|
| Trigger branch | `main` | `master` (avalanchego default) |
| Trigger paths | all pushes to main | only `firewood/**` changes |
| Cargo doc location | repo root | `firewood/` subdirectory |
| Benchmark data source | `git fetch origin benchmark-data` (same repo) | `git fetch origin benchmark-data` (avalanchego's own branch) |
| Deploy method | `actions/deploy-pages` (same repo) | `peaceiris/actions-gh-pages` (cross-repo via deploy key) |
| Pages source | GitHub Actions artifact | `gh-pages` branch |

---

## 5. Track-performance workflow (already migrated)

The `track-performance` workflow has already been migrated to avalanchego as
`.github/workflows/firewood-track-performance.yml`. It uses `auto-push: true`
with `GITHUB_TOKEN` to push benchmark data to avalanchego's own `benchmark-data`
branch. No further changes are needed for this step.

---

## 6. Copy supporting files to avalanchego

These files need to exist in the `avalanchego` repo (under `firewood/` or at
the repo root, as appropriate):

| File | Purpose | Destination in avalanchego |
|------|---------|---------------------------|
| `docs/assets/architecture.svg` | Copied into Pages site | `firewood/docs/assets/architecture.svg` |
| `scripts/bench-cchain-reexecution.sh` | Triggers benchmark workflow | `firewood/scripts/bench-cchain-reexecution.sh` |
| `BENCHMARKS.md` | Documentation | `firewood/BENCHMARKS.md` |
| `justfile` (benchmark targets) | Developer convenience | Adapt into avalanchego's task runner |

> Update all URLs in `BENCHMARKS.md` that reference
> `github.com/ava-labs/firewood/actions` to point to
> `github.com/ava-labs/avalanchego/actions` where applicable. The GitHub Pages
> URLs (`ava-labs.github.io/firewood/...`) remain unchanged.

---

## 7. Configure GitHub Pages source on firewood

After the first successful cross-repo deploy creates the `gh-pages` branch:

1. Go to `firewood` → Settings → Pages
2. Set **Source** to **Deploy from a branch**
3. Set **Branch** to `gh-pages` / `/ (root)`
4. Save

> The original workflow uses the "GitHub Actions" source with
> `actions/deploy-pages`. The cross-repo approach uses the branch-based source
> instead, since `peaceiris/actions-gh-pages` pushes to a branch.

---

## 8. Test end-to-end

### 8.1 Parallel run (both workflows active)

Before disabling the original workflow, run both in parallel to verify:

| Check | How to verify |
|-------|---------------|
| Docs build correctly | Visit `https://ava-labs.github.io/firewood/` — verify redirect and Rust docs load |
| Benchmark data present | Visit `https://ava-labs.github.io/firewood/bench/` — verify charts render |
| Deep links work | Visit a specific doc page, e.g., `https://ava-labs.github.io/firewood/firewood/db/struct.Db.html` |
| Deploy key auth works | Check the `deploy` job logs in avalanchego — no auth errors |
| Benchmark publish works | Trigger a manual `track-performance` run and verify data appears on the `benchmark-data` branch in avalanchego |

### 8.2 Smoke test the deploy

```bash
# Trigger the new workflow manually
gh workflow run firewood-gh-pages.yaml --repo ava-labs/avalanchego

# Watch the run
gh run list --workflow=firewood-gh-pages.yaml --repo ava-labs/avalanchego --limit 1
gh run watch <run-id> --repo ava-labs/avalanchego

# Verify the gh-pages branch was updated in firewood
gh api repos/ava-labs/firewood/branches/gh-pages --jq '.commit.sha'

# Verify the site
curl -sI https://ava-labs.github.io/firewood/ | head -5
```

---

## 9. Cutover

Once testing passes:

1. **Disable** the remaining original workflow in `firewood`:
   - `.github/workflows/gh-pages.yaml` — delete or add `if: false` to all jobs
   - ~~`.github/workflows/track-performance.yml`~~ — already removed as part
     of CI migration

2. **Verify** the site still works after disabling (the `gh-pages` branch in
   firewood is independent of the workflow files on `main`).

3. **Update** any CI status badges or links in README files that point to the
   old workflow runs.

4. **If archiving firewood later:**
   - Ensure GitHub Pages is set to deploy from `gh-pages` branch (not Actions)
   - Verify Pages is enabled and serving before archiving
   - **Important:** After archiving, the `gh-pages` branch becomes read-only.
     Cross-repo deploys from `avalanchego` will fail. You must either:
     - Keep `firewood` unarchived (recommended — it costs nothing), or
     - Temporarily unarchive → deploy → re-archive each time (not practical)
     - Consider using GitHub's `actions/deploy-pages` with a custom OIDC token
       setup if archiving becomes necessary

---

## 10. Post-migration cleanup

- [ ] Remove `gh-pages.yaml` from firewood's `main` branch
- [x] ~~Remove `track-performance.yml` from firewood's `main` branch~~ (already done as part of CI migration)
- [ ] Remove the `FIREWOOD_AVALANCHEGO_GITHUB_TOKEN` secret from firewood
      (was used for cross-repo benchmark triggering — no longer needed if
      the workflow lives in avalanchego)
- [ ] Update `BENCHMARKS.md` action/workflow URLs
- [ ] Update `CODEOWNERS` if the workflows moved directories
- [ ] Verify scheduled benchmark runs work (wait for next weekday 05:00 UTC
      cron trigger)
- [ ] Verify `benchmark-data` branch is created in avalanchego after first
      successful track-performance run

---

## 11. Rollback plan

If the migration fails or causes issues:

1. **Re-enable** the original workflows in `firewood` (revert the disable
   commit)
2. **Switch** GitHub Pages source back to "GitHub Actions" in firewood's
   settings
3. **Push** to `main` in `firewood` to trigger the original `gh-pages.yaml`
4. Site is restored within minutes

The deploy key and secrets can remain in place — they're harmless if unused.

---

## Summary of secrets and keys

| Secret/Key | Repository | Purpose |
|------------|-----------|---------|
| `FIREWOOD_PAGES_DEPLOY_KEY` (private) | `avalanchego` (secret) | Push to firewood's `gh-pages` branch (benchmark data stays in avalanchego) |
| Deploy key (public) | `firewood` (deploy key, write) | Authorize pushes from avalanchego |
| `FIREWOOD_AVALANCHEGO_GITHUB_TOKEN` | `firewood` (existing) | Can be removed post-migration |

---

## Estimated sequence

```text
Day 1:  Steps 1–2  (prerequisites, deploy key)
        Step 3     (no action — benchmark data already in avalanchego)
        Step 5     (no action — track-performance already migrated)
Day 2:  Steps 4, 6 (create gh-pages workflow, copy supporting files)
Day 2:  Step 7     (configure Pages source)
Day 2:  Step 8     (test with parallel runs)
Day 3+: Step 9     (cutover after confidence period)
Day 7+: Step 10    (cleanup)
```
