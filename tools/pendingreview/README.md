# GitHub Pending Review

`tools/pendingreview` contains the repo-local implementation behind
`gh-pending-review`, a narrowly scoped CLI for manipulating the authenticated
user's pending GitHub review on a pull request.

The tool exists to support an agent-assisted, human-submitted review workflow:

- the agent may create, inspect, update, or delete a pending review
- the human may edit that pending review in the GitHub UI between agent runs
- the tool must detect those edits before overwriting them
- the tool must never submit the review
- for any live mutation testing or API validation, the PR under test must be
  explicitly chosen by the user; the agent must never pick a PR autonomously

This README documents the maintained contract of the tool. It is not a spike
log or implementation plan.

For the GraphQL migration research that motivated the next pending-review-session
design, see [graphql-pending-review-design.md](./graphql-pending-review-design.md).

## Scope

The supported surface is intentionally small and pending-review-only:

- create a pending review with a top-level body
- fetch the authenticated user's current pending review, including inline
  comments and replies to existing threads
- update the top-level review body
- replace the managed inline comment set
- delete the pending review
- inspect or delete the local state used for conflict detection

Intentionally out of scope:

- submitting a review
- approve or request-changes flows
- generic GitHub write operations outside pending review endpoints
- patch-in-place mutation of individual inline comments

## Auth Boundary

Normal `gh` usage on this machine may be read-only through ambient token
environment variables. `gh-pending-review` must not reuse that path for writes.

Instead, the tool shells out to:

```bash
gh auth token --hostname github.com
```

using an isolated `GH_CONFIG_DIR`, while removing these ambient overrides from
the child environment:

- `GH_TOKEN`
- `GITHUB_TOKEN`
- `GH_ENTERPRISE_TOKEN`
- `GITHUB_ENTERPRISE_TOKEN`

That gives the tool a separate authenticated `gh` context for pending review
operations without changing how normal shell usage of `gh` behaves.

Defaults:

- config dir: `${XDG_CONFIG_HOME}/gh-pending-review` or
  `~/.config/gh-pending-review`
- override env vars:
  - `GH_PENDING_REVIEW_CONFIG_DIR`
  - `GH_DRAFT_REVIEW_CONFIG_DIR` for backward compatibility

Typical one-time setup:

```bash
GH_CONFIG_DIR="$HOME/.config/gh-pending-review" \
  gh auth login --hostname github.com --web
```

Use a GitHub account with the repo access needed to create pending reviews.

## Command Surface

```text
gh-pending-review create --pr NUMBER [--repo OWNER/REPO] (--body TEXT | --body-file PATH) [--config-dir DIR] [--state-dir DIR]
gh-pending-review delete --pr NUMBER [--repo OWNER/REPO] [--config-dir DIR] [--state-dir DIR]
gh-pending-review get --pr NUMBER [--repo OWNER/REPO] [--config-dir DIR] [--state-dir DIR]
gh-pending-review get-state --pr NUMBER [--repo OWNER/REPO] --user LOGIN [--state-dir DIR]
gh-pending-review replace-comments --pr NUMBER [--repo OWNER/REPO] --comments-file PATH [--config-dir DIR] [--state-dir DIR] [--force]
gh-pending-review delete-state --pr NUMBER [--repo OWNER/REPO] --user LOGIN [--state-dir DIR]
gh-pending-review update-body --pr NUMBER [--repo OWNER/REPO] (--body TEXT | --body-file PATH) [--config-dir DIR] [--state-dir DIR] [--force]
gh-pending-review version
```

Notes:

- the tool only operates on pending reviews owned by the authenticated user
- `create` and `update-body` require exactly one of `--body` or `--body-file`
- `replace-comments` requires `--comments-file`
- `--repo` defaults to `ava-labs/avalanchego`
- `get-state` and `delete-state` are local-only and do not talk to GitHub

## Workflow

### Create

Create a pending review body:

```bash
./bin/gh-pending-review create \
  --pr 5168 \
  --body-file /tmp/review.md \
  --config-dir "$HOME/.config/gh-pending-review" \
  --state-dir "$HOME/.local/state/gh-pending-review"
```

On success the command prints the created review ID and, when available, the
GitHub review URL.

### Read Back

Fetch the current pending review for the authenticated user:

```bash
./bin/gh-pending-review get \
  --pr 5168 \
  --config-dir "$HOME/.config/gh-pending-review" \
  --state-dir "$HOME/.local/state/gh-pending-review"
```

The output is JSON for the live pending review, including:

- review metadata
- top-level body
- attached inline comments for that review

`get` is read-only. It does not advance local state.

### Update Body

Replace only the top-level review body:

```bash
./bin/gh-pending-review update-body \
  --pr 5168 \
  --body-file /tmp/review.md \
  --config-dir "$HOME/.config/gh-pending-review" \
  --state-dir "$HOME/.local/state/gh-pending-review"
```

This updates the body in place when the live pending review still matches the
tool's stored state.

### Replace Inline Comments

Replace the managed inline comment set:

```bash
./bin/gh-pending-review replace-comments \
  --pr 5168 \
  --comments-file /tmp/comments.json \
  --config-dir "$HOME/.config/gh-pending-review" \
  --state-dir "$HOME/.local/state/gh-pending-review"
```

This is a replace operation, not an edit-in-place operation. The current
implementation:

1. loads and validates the desired comment set from JSON
2. finds the authenticated user's current pending review
3. verifies the live comment set still matches stored state unless `--force` is
   used
4. deletes managed draft comments that are no longer desired
5. adds new draft threads and draft replies needed to reach the desired set
6. reads the pending review back and saves the resulting body and entries in
   local state

The pending review itself is updated in place. Its inline comment set may
change, but the command does not intentionally recreate the top-level review.

### Delete

Delete the authenticated user's pending review and remove matching local state:

```bash
./bin/gh-pending-review delete \
  --pr 5168 \
  --config-dir "$HOME/.config/gh-pending-review" \
  --state-dir "$HOME/.local/state/gh-pending-review"
```

### Local State

Inspect stored state without talking to GitHub:

```bash
./bin/gh-pending-review get-state --pr 5168 --user YOUR_GITHUB_LOGIN
./bin/gh-pending-review delete-state --pr 5168 --user YOUR_GITHUB_LOGIN
```

These commands are for recovery, inspection, and deliberate cleanup when local
state has become stale.

## Local State Model

Conflict detection is state-based. The tool persists the last pending review state
that it successfully published under:

```text
<state-dir>/<owner>/<repo>/<user>/<pr>.json
```

Defaults:

- state dir: `${XDG_STATE_HOME}/gh-pending-review` or
  `~/.local/state/gh-pending-review`
- override env vars:
  - `GH_PENDING_REVIEW_STATE_DIR`
  - `GH_DRAFT_REVIEW_STATE_DIR` for backward compatibility

Stored fields:

- `repo`
- `pr`
- `user`
- `review_id`
- `last_published_body`
- `last_published_comments`
- `html_url`

The stored state represents what the tool last wrote, not what it last read.
That distinction is intentional and is the basis for conflict detection.

## Conflict Rules

The tool protects human GitHub edits with optimistic concurrency checks.

### Body Updates

`update-body` refuses to overwrite when either of these is true:

- the stored `review_id` points to a different pending review than the live one
- the live review body differs from `last_published_body`

The failure is deliberate. The intended recovery flow is:

1. run `get`
2. inspect the live review body and comments
3. reconcile the human edits into the desired new state
4. retry with `--force` only if overwriting is intentional

### Comment Replacements

`replace-comments` follows the same model for inline comments:

- it normalizes both the stored managed comment set and the live managed comment
  set
- it compares those normalized sets
- it refuses to continue if they differ, unless `--force` is supplied

Managed comments are compared by normalized content, not by the original input
file order.

## Comments File Format

`replace-comments` reads a JSON array of objects. The default entry kind is a
new draft thread:

```json
[
  {
    "path": "snow/engine/avalanche/bootstrap/bootstrapper.go",
    "line": 123,
    "side": "RIGHT",
    "body": "This path still assumes the old state transition."
  }
]
```

To reply to an existing review thread instead of opening a new one, set
`kind` to `thread_reply` and provide the GitHub review thread node ID:

```json
[
  {
    "kind": "thread_reply",
    "thread_id": "PRRT_kwDODq-TvM52SHRS",
    "body": "This should stay attached to the existing thread."
  }
]
```

Rules:

- `body` is required
- for `new_thread` entries:
  - `path` is required
  - `line` must be a positive integer
  - `side` must be `LEFT` or `RIGHT`
- for `thread_reply` entries:
  - `thread_id` is required
  - file anchor fields must be omitted
- unknown fields are rejected

The file must contain exactly one JSON value. Parsed comments are normalized
before comparison and persistence.

Normalization rule:

- `thread_reply` entries are compared by reply kind, thread ID, and body
- GitHub may read replies back with inherited anchor fields such as `path`,
  `line`, or `start_line`; those fields are ignored for reply comparison and
  stored-state reconciliation

## Output Contract

This tool follows the repo-local tool conventions documented in
[`../README.md`](../README.md), including structured read output, concise
human-readable write output, structured logging, and stack-bearing error
wrapping.

Pending-review-specific output behavior:

- `get` prints the live pending review as JSON
- `get-state` prints stored review state as JSON
- `create` and `replace-comments` also print the review URL when GitHub returns
  one

## Operational Constraints

- The tool never submits reviews. Human submission happens in the GitHub UI.
- The tool only manipulates the authenticated user's own `PENDING` review.
- Inline comment writes are replace-only at the managed-set level.
- The managed set may contain both new draft threads and replies to existing
  review threads.

## Maintenance Notes

Follow the shared tooling conventions in [`../README.md`](../README.md).

For this package specifically, keep the boundary narrow: pending review state
only, with explicit conflict protection around any mutation path that could
overwrite human edits.
