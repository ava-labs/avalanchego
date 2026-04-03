# avalanchego repo

## Worktree Locations

- **Primary development**: `~/src/avalanchego`
- **Gerrit PR review**: `~/src/avalanchego_gerrit-review`

The gerrit-review worktree is dedicated to importing GitHub PRs into Gerrit for collaborative review. Do NOT use `~/src/avalanchego-beads` - that's a separate beads-related repo.

### Remote Configuration

Both worktrees share the same remotes:
- `origin` → Gerrit (`ssh://pika-svc-gerrit/avalanchego`)
- `upstream` → GitHub (`git@github.com-work:ava-labs/avalanchego`)

### GitHub Access

GitHub git operations against `upstream` use SSH and require YubiKey interaction.
For PR metadata and branch checkout in review flows, prefer `gh` over raw git commands
when possible, because `gh` can use `GH_TOKEN` without needing the YubiKey.

Practical guidance:
- Use `gh pr view`, `gh pr diff`, and `gh pr checkout` first for GitHub PR review work
- Avoid `git fetch upstream ...` unless SSH/YubiKey interaction is explicitly intended
- Keep using `origin` for Gerrit operations per the normal workflow

## Gerrit PR Review Workflow

To import a GitHub PR for review in the gerrit-review worktree:

```bash
cd ~/src/avalanchego_gerrit-review
gerrit-review sync-pr <PR-number>
```

This fetches the PR, squashes commits into one, and pushes to Gerrit for review.

## Development Workflow

The `Taskfile.yml` at the repo root provides entrypoints to common functionality. Run `task` to see available tasks.

### After Making Changes

Always run linting before committing:

```bash
task lint-all
```

This executes golangci-lint, shellcheck, and actionlint in parallel to catch issues early.

### Error Assertion Guidance

In avalanchego, forbidigo rules banning `require.Error` and `require.ErrorContains`
in favor of `ErrorIs` are intentional. Do not work around those lint failures by
weakening assertions or switching to manual string checks when the code under test
controls the error being returned.

When a test wants `ErrorIs` but the implementation only returns a formatted string
for a domain condition we control, treat that lint failure as a prompt to improve
the error API. Introduce or wrap a stable sentinel or typed error so the test can
assert the behavior directly.

Only fall back to string-based error inspection when the failure comes from a
genuinely opaque external interface that cannot reasonably be normalized into a
stable local error value.

### Key Tasks

- `task build` - Build avalanchego
- `task test-unit` - Run unit tests
- `task test-unit-fast` - Run unit tests without race detection (faster)
- `task test-e2e` - Run e2e tests
- `task lint` - Run golangci-lint only
- `task lint-all` - Run all linters (golangci-lint, shellcheck, actionlint)
