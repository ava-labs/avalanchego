# GitHub Pending Review

This directory contains a repo-local tool for manipulating GitHub pending
reviews as part of an agent-assisted review workflow.

## Goal

Enable an agent to manipulate a GitHub pull request pending review as part of a
human-in-the-loop review workflow, while keeping normal `gh` usage on the
machine read-only via the existing PAT-based environment.

The required boundary is:

- normal `gh` usage continues to use the ambient read-only token path
- pending review operations use a separate authenticated `gh` context
- the write-capable path is narrowly scoped to draft review manipulation only
- the tool must never submit the review; a human submits it in the GitHub UI

## Logging Rationale

This package intentionally uses zap-backed structured logging through the repo's
`utils/logging` abstraction even though the workflow is small.

The point is not that `gh-pending-review` is operationally complex. The point is
that agents need legible execution traces when something goes wrong. A failed
test, a mismatched local state file, or an unexpected GitHub API response should
leave behind enough structured context to reconstruct what happened without
re-running the workflow blindly.

The logging model follows `tests/fixture/tmpnet`:

- log command entry points and external call boundaries at debug
- log review creation, update, deletion, and persisted state changes at info
- prefer stable structured fields over interpolated strings

That makes this package a small but concrete example of the observability
standard expected for future agent-developed tooling.

## Stack Trace Errors

This package also follows `tests/fixture/tmpnet` in opting into stack-bearing
errors via `tests/fixture/stacktrace`.

Set `STACK_TRACE_ERRORS=1` when running `gh-pending-review` to include a stack
trace in emitted errors. This tool is agent-facing and not performance
critical, so the usual Go tradeoff of omitting stack traces for overhead does
not apply here.

The point is better agent debugging: when a command fails because of a bad local
state file, an auth problem, or an unexpected GitHub API response, the stack
trace shows where the failure was constructed instead of forcing a blind
reproduction loop.

For future maintenance, treat this as a package-level rule:

- create all new errors originating in this package with
  `stacktrace.New` or `stacktrace.Errorf`
- wrap errors returned from other packages or libraries with
  `stacktrace.Wrap` before returning them

This is the only way to keep stack traces attached consistently as the tool
evolves. If a new error path bypasses `tests/fixture/stacktrace`, that failure
will fall back to a plain Go error and lose the debugging context this tool is
supposed to preserve.

## What Was Learned

The key design question was whether GitHub CLI authentication could be used for
pending reviews at all, given that:

- `gh pr review` does not expose a draft/pending review mode
- the user does not have a PAT with PR write access
- the user does have access to the `gh` browser/SSO auth flow

The answer is yes:

- `gh pr review` is insufficient, but `gh api` is not
- GitHub's REST API supports creating a pending review by calling
  `POST /repos/{owner}/{repo}/pulls/{pull_number}/reviews` without an `event`
- `gh` can authenticate through the browser/SSO flow and then use that auth for
  API requests
- on macOS, the `gh` OAuth token can be stored in the system keychain-backed
  credential store rather than plaintext

The crucial operational detail is:

- ambient `GH_TOKEN` / `GITHUB_TOKEN` override stored `gh` auth
- the pending-review tool must clear those variables before invoking `gh` or the
  read-only PAT path will win

## Validated Behavior

This is no longer just a theoretical spike. The following was validated live on
2026-03-31 against `ava-labs/avalanchego` PR `5168`.

### Auth Flow

The user ran:

```bash
GH_CONFIG_DIR="$HOME/.config/gh-pending-review" gh auth login --hostname github.com --web
```

During the interactive prompts:

- protocol: `HTTPS`
- authenticate Git with GitHub credentials: `No`

After login, `gh auth status` showed two accounts:

- `maru-ava (GH_TOKEN)` as the active account when ambient `GH_TOKEN` was
  present
- `maru-ava (keyring)` as an inactive account, with token scopes:
  - `gist`
  - `read:org`
  - `repo`

After clearing the ambient token override, the keyring-backed account became the
active account:

```bash
GH_TOKEN= GH_CONFIG_DIR="$HOME/.config/gh-pending-review" \
  gh auth status --hostname github.com
```

This confirmed:

- separate `GH_CONFIG_DIR` does not imply insecure storage
- `gh` stored the OAuth token in keyring-backed secure storage
- the isolated auth path works as intended once ambient token overrides are
  removed

### Read Validation

The isolated auth was able to read the target PR:

```bash
GH_TOKEN= \
GH_CONFIG_DIR="$HOME/.config/gh-pending-review" \
gh api repos/ava-labs/avalanchego/pulls/5168 \
  --jq '{number: .number, state: .state, title: .title, user: .user.login}'
```

This returned PR metadata for:

- number: `5168`
- state: `open`
- title: `[ci] Update rpm builder script for podman compatibility`
- user: `maru-ava`

### Pending Review Creation Validation

The isolated auth was able to create a pending review:

```bash
GH_TOKEN= \
GH_CONFIG_DIR="$HOME/.config/gh-pending-review" \
gh api repos/ava-labs/avalanchego/pulls/5168/reviews \
  --method POST \
  -f body='test'
```

This returned:

- review ID: `4041220791`
- state: `PENDING`
- html URL:
  - `https://github.com/ava-labs/avalanchego/pull/5168#pullrequestreview-4041220791`

This is the core proof that the intended auth and API model works end to end
without a PAT that has PR write access.

### Pending Review Retrieval Validation

The created pending review was retrievable through both list and get endpoints:

```bash
GH_TOKEN= GH_CONFIG_DIR="$HOME/.config/gh-pending-review" \
  gh api repos/ava-labs/avalanchego/pulls/5168/reviews

GH_TOKEN= GH_CONFIG_DIR="$HOME/.config/gh-pending-review" \
  gh api repos/ava-labs/avalanchego/pulls/5168/reviews/4041220791
```

Both returned the pending review with:

- author: `maru-ava`
- state: `PENDING`
- body: `test`

That matters because the desired workflow requires the agent to pull down the
current pending review after the human edits it in the GitHub UI.

## Confirmed API Capabilities

The current understanding from docs plus live validation is:

- create pending review: supported
- get a specific review: supported
- list reviews on a PR: supported
- update a review body: supported
- delete a pending review: supported
- list comments for a review: supported
- submit a review: supported by GitHub, but must not be implemented here

The previous assumption that pending reviews could not be updated was incorrect.
GitHub documents `PUT /repos/{owner}/{repo}/pulls/{pull_number}/reviews/{review_id}`.

The likely practical limitation is not review-body updates, but granular
mutation of individual draft inline comments. Because of that, a higher-level
`replace` operation is likely preferable for comment-heavy workflows.

## Desired Workflow

The intended human-in-the-loop loop is:

1. agent creates or replaces a pending review
2. human opens the pending review in GitHub
3. human edits the top-level review body or inline review comments
4. human can add `!!` markers or other instructions directed at the agent
5. agent fetches the current pending review and its comments
6. agent updates or replaces the pending review based on the human edits
7. human manually submits the review in GitHub when satisfied

The tool must assist with pending-review state only. It must never submit.

## Recommended Maintained Design

Do not keep evolving this as a nontrivial shell script. The shell version was a
useful spike to prove the auth model. The maintained implementation should be a
small Go CLI.

### Why Go

Go is the recommended language because:

- the target repo uses Go heavily
- the desired API surface is larger than a comfortable shell wrapper
- maintainability and automated testability are explicit requirements
- `net/http` plus `httptest` make the GitHub API integration straightforward to
  test

### Architecture

Use `gh` only as the auth provider and token bridge. Do not use `gh` as the
main implementation substrate.

Recommended shape:

- Go package for command parsing and workflow logic
- direct HTTPS calls to the GitHub review endpoints
- token acquisition by invoking `gh auth token` under the isolated
  `GH_CONFIG_DIR`
- explicit clearing of `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`,
  `GITHUB_ENTERPRISE_TOKEN` before invoking `gh`

This preserves the proven SSO/browser/keychain flow while moving the actual tool
into a typed and testable implementation.

### Package Layout

The maintained layout should keep `main` minimal and put all behavior in an
importable package:

```text
tools/draftreview/
  README.md
  app.go
  cli.go
  config.go
  auth.go
  github.go
  reviews.go
  types.go
  integration_test.go
  auth_test.go
  reviews_test.go
  cmd/
    main.go
```

Conventions:

- `tools/draftreview/*.go` uses package `draftreview`
- `tools/draftreview/cmd/main.go` uses package `main`
- `main.go` should do as little as possible beyond calling into
  `draftreview.Run(...)`
- command parsing belongs in the package, not in `main`
- review logic, auth handling, and GitHub API access must all be importable and
  testable without going through a `main` package

The source tree does not need an extra `draftreview/` subdirectory, and it does
not need a `cmd/gh-pending-review/` layer for a single internal command.

### Repo Integration

The command should be exposed using the same repo-local pattern as `tmpnetctl`:

- add `gh` to the root development environment so it is always available
- add a build script, for example `scripts/build_gh_pending_review.sh`
- add a thin launcher at `bin/gh-pending-review`
- have the launcher build `./build/gh-pending-review` before execution and then
  run it

This keeps developer ergonomics simple while avoiding repeated `go run`
overhead.

## Recommended API Surface

The maintained tool should support only pending-review manipulation.

Recommended commands:

- `get`
  - fetch the current authenticated user's pending review for a PR
  - include top-level body and attached review comments
- `create`
  - create a pending review with a body and optional inline comments
- `update-body`
  - update only the top-level review body
  - refuse to overwrite when the live review body no longer matches the last
    body published by the tool, unless `--force` is explicitly used
- `delete`
  - delete a pending review
- `replace`
  - find the current pending review for the authenticated user
  - delete it if present
  - recreate it from supplied body/comments

Intentionally excluded:

- `submit`
- `approve`
- `request-changes`
- any generic GitHub write operations outside review endpoints

## Body Workflow

The currently implemented body-only workflow is:

1. create a pending review with a top-level body
2. store the exact body that was published
3. fetch the current pending review body with `get`
4. update the body only if the live body still matches the stored last
   published body
5. refuse to overwrite if the human has edited the body in GitHub
6. allow an explicit `--force` update only after the agent has read and
   reconciled the current live body

This is the baseline safety model. Inline comment support must preserve the
same overwrite protection guarantees.

## First Milestone

The first milestone is intentionally narrow:

- go from zero to creating a pending review whose body is exactly `test`
- do this through the Go implementation, not the shell spike
- validate it end to end against live GitHub

The initial command surface can be as small as a single create path. The rest
of the API surface can come after this is proven.

The first completed flow should be:

1. invoke the Go CLI for a target PR in `ava-labs/avalanchego`
2. acquire an OAuth token through isolated `gh` auth
3. create a pending review with body `test`
4. fetch the created review back from GitHub
5. validate that:
   - the author is the authenticated user
   - the review state is `PENDING`
   - the body is `test`

No inline comments are required for the first milestone.

## Local State And Conflict Detection

The tool keeps a local record of the last review body it published so it can
detect when a human has edited the pending review in GitHub.

Default state location:

- `${XDG_STATE_HOME}/gh-pending-review` when `XDG_STATE_HOME` is set
- otherwise `~/.local/state/gh-pending-review`

It can be overridden with:

```bash
--state-dir <dir>
```

State is keyed by:

- repository
- pull request number
- authenticated GitHub user

The minimal stored fields are:

- review ID
- last published body
- review URL

### Update Safety Rule

`update-body` uses optimistic concurrency:

1. load the stored last-published body
2. fetch the current pending review from GitHub
3. compare the live body to the stored body
4. if they differ, refuse to update and return a conflict error

This is the guardrail that prevents the tool from blindly overwriting manual
GitHub edits.

If the agent has already fetched the current review, reconciled the human's
changes, and intentionally wants to overwrite the draft review body, it must
use:

```bash
--force
```

`get` does not advance stored state. Only successful writes do. That preserves
the distinction between "what the agent last published" and "what the agent has
most recently read."

## Inline Comment Model

Inline comments are the main remaining capability after the body-only workflow.
The README is the source of truth for the intended first implementation.

### Scope

The first inline-comment implementation should stay narrow:

- modern line-based review comments only
- comments owned by the authenticated user only
- comments attached to the authenticated user's `PENDING` review only
- no review submission
- no approve/request-changes flows
- no generic GitHub write surface outside pending review endpoints

### Read Shape

The first useful `get` result for comment-aware workflows should include both:

- the top-level pending review body
- the authenticated user's pending inline review comments

The intent is that the agent sees the entire current pending-review state in a
single read operation before deciding how to update it.

### Write Shape

The first comment write path should favor replace semantics over granular
mutation.

Recommended initial operation:

- replace the authenticated user's pending review comments as a set

Reasoning:

- GitHub's granular pending-comment mutation surface is more awkward and easier
  to get wrong
- the agent workflow naturally wants to regenerate the intended set of pending
  comments after reading the current review
- replace semantics align better with optimistic concurrency and reduce partial
  update edge cases

That means the likely first implementation is:

- keep `update-body` for top-level body-only changes
- add a comment-aware replace operation for body plus comments, or comments
  alone, based on the authenticated user's current pending review

### Input Shape

The first durable inline-comment input should be structured and file-based.

Suggested initial comment shape:

```json
[
  {
    "path": "file.go",
    "line": 123,
    "side": "RIGHT",
    "body": "!! re-check this error path"
  }
]
```

The first version should:

- validate the JSON schema strictly
- reject unsupported legacy positioning fields
- avoid mixing multiple incompatible comment-position models in one command

## Inline Comment Conflict Model

Inline comments need the same overwrite protection as the review body.

### Principle

The tool must not blindly overwrite manual user edits to pending inline
comments. A normal comment update should fail if the live comment set no longer
matches what the tool last published.

### State Requirement

Once inline comments are supported, local state should be extended to store:

- the last published review body
- the last published pending comment set
- the pending review ID

The key distinction remains:

- stored state means what the agent last published
- reading current GitHub state does not silently update stored state

### Conflict Rule

Normal comment writes should:

1. load the stored last-published comment set
2. fetch the current live pending comment set
3. compare them
4. fail with a conflict if they differ

That conflict should instruct the caller to:

1. run `get`
2. reconcile the live review body and comments
3. retry intentionally, using an explicit override only when overwrite is
   desired

### Override Path

An explicit override path is acceptable, but it must not be the default.

The design goal is not to make overwrites impossible. It is to make them
intentional and visible to the agent so that manual GitHub edits are not
silently clobbered.

## Testing Strategy

The primary risk is live behavior, not local parsing. Because of that, the
first meaningful validation should be an opt-in integration test against
GitHub.

The body-only live test already validates create, external edit, conflict, and
forced reconcile for the top-level review body.

The next live test target for inline comments should:

- accept a target PR number through explicit configuration
- allow hard-coding `ava-labs/avalanchego` for the initial implementation
- create a pending review with body `test`
- create or replace pending inline comments for that review
- fetch the full pending review state and verify body plus comments

Safety expectations for the live test:

- it must require explicit opt-in through environment variables
- it should verify whether the authenticated user already has a pending review
  on the target PR before mutating anything
- it should avoid touching unrelated reviews
- if practical for the initial flow, it should delete the test-created pending
  review during cleanup
- it should verify conflict detection after a simulated external manual change
  to the pending inline comments

Unit tests are still useful, but secondary. They should focus on narrow,
deterministic logic such as:

- PR reference parsing
- auth environment scrubbing before invoking `gh`
- selecting the authenticated user's pending review from a review list

## Safety Constraints

The implementation should enforce these constraints in code:

- only operate on reviews authored by the authenticated user
- only operate on reviews whose state is `PENDING`
- only call the review-related GitHub API endpoints needed for:
  - get/list reviews
  - create review
  - update review body
  - delete pending review
  - list review comments
  - resolve authenticated viewer identity
- never expose a generic GitHub API passthrough
- never implement review submission

This is the main mechanism for narrowing the privilege surface of the OAuth
token.

## Testing Requirements

Automated testing is a requirement. The tool should not depend on manual
validation for core behavior.

### Core Automated Tests

The maintained implementation should include:

- unit tests for argument validation and safety checks
- tests for auth environment isolation
- tests for token acquisition through a stubbed `gh`
- API tests using `httptest.Server`
- tests for:
  - create
  - get
  - update-body
  - delete
  - replace
  - author/state guardrails
  - error mapping
  - state persistence for both body and inline comments
  - conflict detection for both body and inline comments

### Live Smoke Tests

Add optional live smoke tests against a dedicated test repo/PR, gated behind
environment variables.

These should exercise:

- create pending review
- fetch pending review
- update review body
- delete pending review
- replace pending review
- create or replace pending inline comments
- fetch pending inline comments
- conflict on user-edited pending inline comments

Live smoke tests should still never submit the review.

## Current Implementation

The current Go implementation provides:

- package `github.com/ava-labs/avalanchego/tools/draftreview`
- minimal binary entrypoint at `tools/draftreview/cmd/main.go`
- repo-local launcher at `bin/gh-pending-review`
- body-only pending review commands: `create`, `get`, `update-body`, `delete`
- local state tracking for last-published review bodies
- live integration test coverage for create, fetch, conflict, forced reconcile,
  and cleanup
- build-time version metadata via embedded git commit

Current command shape:

```bash
./bin/gh-pending-review create --pr 5167 --body test
./bin/gh-pending-review delete --pr 5167
./bin/gh-pending-review get --pr 5167
./bin/gh-pending-review update-body --pr 5167 --body "revised text"
./bin/gh-pending-review version
```

The repo defaults to `ava-labs/avalanchego`. The isolated auth config defaults
to `${XDG_CONFIG_HOME:-$HOME/.config}/gh-pending-review`.

### Live Integration Test

The live test is opt-in and requires an isolated authenticated `gh` config:

```bash
GH_PENDING_REVIEW_LIVE_TEST=1 \
GH_PENDING_REVIEW_TEST_REPO=ava-labs/avalanchego \
GH_PENDING_REVIEW_TEST_PR=5167 \
GH_PENDING_REVIEW_CONFIG_DIR="$HOME/.config/gh-pending-review" \
go test ./tools/draftreview -run TestCreatePendingReviewLive -count=1 -v
```

The test will:

- resolve the authenticated viewer
- refuse to run if that viewer already has a pending review on the target PR
- optionally delete that existing pending review first when
  `GH_PENDING_REVIEW_TEST_DELETE_EXISTING=1` is set
- create a pending review with body `test`
- fetch the created review and verify author, state, and body
- simulate an external manual edit to the review body
- verify that `update-body` refuses to overwrite that external change
- verify that an explicit `--force` update succeeds after that conflict
- delete the created pending review during cleanup

## Remaining Work

The main remaining implementation work is:

- define and document the first inline-comment replace model
- extend `get` to return pending body plus inline comments together
- implement pending inline comment write support
- extend local state and conflict protection to inline comments
- add live integration coverage for inline comments
- improve file-based input and state ergonomics for day-to-day agent use

## Key Facts Not To Lose

- `gh pr review` is not enough because it submits immediately
- `gh api` plus the GitHub review endpoints is sufficient
- browser/SSO login via `gh auth login --web` works for this use case
- macOS keychain-backed storage works with a separate `GH_CONFIG_DIR`
- ambient `GH_TOKEN` overrides stored auth and must be cleared
- pending reviews are retrievable through normal review list/get endpoints
- review body update and pending review deletion are supported
- the human must remain the only actor who submits the review
