---
name: firewood-review
description: Use when reviewing ava-labs/firewood code changes — pull request or local workspace. Invoke with phrases like "review PR", "review my changes", "review this branch", "firewood review". Handles PR checkout, staged/unstaged changes, Rust toolchain, Go FFI, CI workflow, and multi-agent parallel review.
compatibility: Requires Claude Code with git, cargo/rustup, golangci-lint, and the gh CLI. Designed for use in the ava-labs/firewood workspace.
allowed-tools: Read Write Bash Agent AskUserQuestion EnterWorktree ExitWorktree mcp__github__pull_request_read mcp__github__list_pull_requests mcp__github__get_commit mcp__github__list_commits
---

# Firewood Code Review

## Overview

Firewood-specific local code review. Dispatches parallel agents covering Rust toolchain validation, Go FFI checks, CI workflow analysis, and deep code quality. Never publishes review output to GitHub unless explicitly instructed.

Code quality review rules are defined in [`AGENTS.md` — Code Review Guidelines](../../../AGENTS.md#code-review-guidelines). Agents should read that section in full before beginning scrutiny.

## Phase 0: Setup

### Scratch Directory

Create a session-scoped scratch directory at the start. All fetched API data is written here so subagents can read it without re-fetching or needing extra permissions:

```bash
SCRATCH=/tmp/firewood-review/$$
mkdir -p "$SCRATCH"
```

Pass `$SCRATCH` to every subagent as part of its prompt.

### Determine Review Mode

**PR review** — prompt names a PR number, branch, or URL:

1. If worktrees are available and no worktree is already checked out for this PR, create a dedicated worktree via `EnterWorktree` for the PR branch. Otherwise fall back to `gh pr co <PR_NUMBER>`.
2. Skip checkout if the prompt says the workspace is already on the PR branch.
3. Fetch PR metadata — prefer the `github` MCP if available, otherwise fall back to `gh` CLI:

   **MCP (preferred):**

   ```
   mcp__github__pull_request_read → write JSON to $SCRATCH/pr.json
   ```

   **CLI fallback:**

   ```bash
   gh pr view <PR_NUMBER> --json title,body,comments,reviews \
     > "$SCRATCH/pr.json"
   gh api repos/ava-labs/firewood/pulls/<PR_NUMBER>/comments \
     > "$SCRATCH/pr-comments.json"
   ```

4. Record the base and head SHAs from the fetched data for use in diffs.

**Local changes** — prompt says "local changes", "unpublished", or similar:

1. Run `git status --porcelain` to detect staged vs unstaged files.
2. If both staged and unstaged changes exist and the prompt didn't clarify, use `AskUserQuestion`: "Review workspace as-is, or stash unstaged changes first?"
3. If stash: `git stash push --include-untracked -m "firewood-review-stash"`. After synthesis completes, `git stash pop` unconditionally.

### Detect Changed File Types

From the diff (`git diff --name-only <base>..<head>` for PR, or `git diff --name-only HEAD` for local), determine which agents to activate:

- Any `*.rs` or `Cargo.*` changes → activate **rust-toolchain**
- Any `ffi/**` Go files → activate **go-toolchain**
- Any `.github/**` changes → activate **ci-review**
- Always activate **code-quality**

Write the changed file list to `$SCRATCH/changed-files.txt`.

## Phase 1: Parallel Agents

Dispatch all applicable agents **in a single message** (parallel execution). Each agent receives: `$SCRATCH` path, diff output, list of changed files, and the instructions from its section below. PR metadata is read from files in `$SCRATCH`.

---

### Agent: rust-toolchain

**Precondition:** Any `*.rs` or `Cargo.*` files changed.

```bash
uname -s
cargo fmt --all --check
cargo fetch --locked
# All subsequent cargo commands: add --frozen --target-dir target/review
```

Use `uname -s` to determine which feature sets to test (e.g., skip `io-uring` features on macOS/Darwin).

Use `--frozen` after the initial fetch to ensure we're not accidentally pulling new dependencies that aren't covered by the lockfile.

Use `--target-dir target/review` for ALL cargo commands to avoid conflicting with the workspace target dir.

Run `cargo clippy --workspace --all-targets` and `cargo nextest run --workspace --all-targets` for each feature set:

| Feature flags               | macOS               | Linux |
| --------------------------- | ------------------- | ----- |
| (none)                      | ✓                   | ✓     |
| `--no-default-features`     | ✓                   | ✓     |
| `--features ethhash,logger` | ✓                   | ✓     |
| `--all-features`            | ✗ (skip `io-uring`) | ✓     |

If the PR adds any new Cargo feature, test with and without it across the applicable matrix rows.

**Header drift check:**

```bash
cargo build -p firewood-ffi --frozen --target-dir target/review
git diff --exit-code ffi/firewood.h
```

Non-zero exit = stale header → blocking finding. Write results to `$SCRATCH/rust-toolchain.txt`.

---

### Agent: go-toolchain

**Precondition:** Any Go files in `ffi/` changed.

Go bindings require the FFI build to use the workspace `target/` dir (the `LDFLAGS` in `ffi/firewood.go` reference it directly — do not use `--target-dir` here):

```bash
cargo build -p firewood-ffi -F ethhash,logger --frozen
cd ffi
golangci-lint run
go test ./... -race
```

Write results to `$SCRATCH/go-toolchain.txt`. Report any lint violations or test failures as blocking.

---

### Agent: code-quality

**Always activated.** Read all changed files in full. Do not skim diffs — read the actual source for context. Read PR metadata from `$SCRATCH/pr.json` and `$SCRATCH/pr-comments.json` if present.

Read the **Code Review Guidelines** section of `AGENTS.md` in full and apply every rule to the changed code.

Additionally:

- Read `CLAUDE.md` and `CONTRIBUTING.md` in full and check whether this PR's changes create any gaps or inconsistencies in those documents. Suggest specific wording updates if helpful.
- If this review session itself introduces new conventions (e.g., a new skill, new workflow), note what should be added to `AGENTS.md` or `CONTRIBUTING.md`.

Write findings to `$SCRATCH/code-quality.txt`.

---

### Agent: ci-review

**Precondition:** Any `.github/**` files changed.

1. Read all changed workflow YAML files in full.
2. **Efficiency:** redundant steps, matrix entries that duplicate coverage, jobs that could parallelize, missing `cache` steps.
3. **Correctness:** wrong trigger events, missing `permissions`, broken `if:` conditionals, misuse of `continue-on-error`, pinned action SHA vs tag inconsistencies.
4. **`act` validation** (if available):
   ```bash
   act --list
   act -n -j <modified-job>
   ```
   Report output. If `act` is unavailable, note it.
5. **Realized CI results** — prefer the `github` MCP if available, otherwise fall back to `gh`:

   **MCP (preferred):**

   ```
   mcp__github__pull_request_read → extract check_runs/status fields → write to $SCRATCH/ci-checks.json
   ```

   **CLI fallback:**

   ```bash
   gh pr checks <PR_NUMBER> --json name,state,conclusion \
     > "$SCRATCH/ci-checks.json"
   ```

   Compare expected jobs vs actual. Flag failures, unexpected skips, or jobs that didn't run.

Write findings to `$SCRATCH/ci-review.txt`.

---

## Phase 2: Synthesis

After all agents complete, read `$SCRATCH/*.txt` and aggregate into a single report:

1. Three severity tiers:
   - **Blocking** — must fix before merge (test failures, clippy errors, stale header, `unwrap` in prod, unsafe soundness issues)
   - **Warning** — should fix, non-blocking (style, missing edge case tests, minor error context gaps)
   - **Suggestion** — optional improvements (doc wording, AGENTS.md updates, CI efficiency)

2. Present the report in the current conversation. Do not publish to GitHub unless the user explicitly asks.

3. If a worktree was created: offer to exit and clean it up.

4. If changes were stashed: `git stash pop` unconditionally before finishing.

5. Clean up: `rm -rf "$SCRATCH"` after presenting the report.
