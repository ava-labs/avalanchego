---
name: pending-review
description: Use when the user wants to create, read, update, or delete their GitHub pending review through the repo-local `gh-pending-review` tool as part of a human-in-the-loop PR review workflow. This skill covers top-level review body iteration, inline comment replacement, and local state inspection, but never review submission.
---

# Pending Review

Use the repo-local `gh-pending-review` tool to manipulate the authenticated
user's pending GitHub review on `ava-labs/avalanchego`.

## When To Use

Use this skill when the user wants to:

- create a pending review body
- fetch the current pending review body and inline comments
- update the current pending review body
- replace the current managed inline comment set, including replies on existing
  review threads
- delete the current pending review
- inspect or delete the local pending review state
- iterate on a pending review after the user edits it in GitHub with `!!`
  instructions

Do not use this skill for:

- submitting a review
- approving or requesting changes
- generic GitHub API access outside pending review operations

## Tool Boundary

Use the repo-local launcher:

```bash
./bin/gh-pending-review
```

This tool is only the GitHub pending review interface. Keep instruction
interpretation, review drafting, and reconciliation in the agent, not in the
tool.

For routine pending-review work, stay at the CLI boundary:

- use `./bin/gh-pending-review`
- write any temporary body or comments payloads you need
- do not inspect or modify `tools/pendingreview`, tests, or other repo source
  just to complete a normal pending-review task
- if a command fails, correct the command usage or payload and retry; do not
  pivot into debugging the tool implementation unless the user explicitly asks

Shell note:

- commands in this environment may run under `zsh`
- avoid bash-only cleanup snippets or assigning to `status`, which is read-only
  in `zsh`
- if you need to preserve an exit code while cleaning up a temp file, use a
  variable such as `rc`

The tool currently supports:

- `create --pr <number> (--body <text> | --body-file <path>)`
- `get --pr <number>`
- `update-body --pr <number> (--body <text> | --body-file <path>) [--force]`
- `replace-comments --pr <number> --comments-file <path> [--force]`
- `delete --pr <number> [--ensure-absent]`
- `get-state --pr <number> --user <login>`
- `delete-state --pr <number> --user <login>`
- `version`

Unless there is a reason to do otherwise, use:

```bash
--config-dir "$HOME/.config/gh-pending-review"
--state-dir "$HOME/.local/state/gh-pending-review"
```

The tool stores the last review body and comment set it published in the local
state directory and uses that state as an optimistic-concurrency guard before
overwriting GitHub edits.

## Workflow

### Create

When the user asks to post a pending review body:

```bash
./bin/gh-pending-review create --pr <number> --body-file <path> --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

Use `--body` instead of `--body-file` only when the body is short enough to be
safe and readable inline.

Return the created review ID and URL.

### Read Back

When the user says they edited the review in GitHub or wants the current draft
state:

```bash
./bin/gh-pending-review get --pr <number> --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

Read the returned JSON and inspect:

- the top-level review body
- any inline comments attached to that pending review
- `!!` instructions in either place

`get` returns JSON for the live pending review. The fields that matter for
reconciliation are:

- top-level `body`
- `comments[]` entries with:
  - `kind`
  - `thread_id` for replies
  - `path`, `line`, and `side` for new threads
  - `body`

Treat `id`, `reply_to_comment_id`, and `user` as read-only metadata. Do not copy
those fields into a `replace-comments` input file unless the write format
explicitly requires them.

### Update Body

If the user wants you to revise only the top-level review body:

```bash
./bin/gh-pending-review update-body --pr <number> --body-file <path> --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

Use `--force` only after reading the current pending review and intentionally
reconciling the live edits.

### Replace Inline Comments

If the user wants to replace the current managed inline comment set, write the
desired comments to a JSON file and apply them with:

```bash
./bin/gh-pending-review replace-comments --pr <number> --comments-file <path> --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

Prefer a simple temp file write such as a heredoc over complex shell quoting or
helper scripts when producing the JSON payload for `--comments-file`.

Safe pattern:

```bash
tmp=$(mktemp)
cat >"$tmp" <<'EOF'
[
  {
    "path": "path/to/file.go",
    "line": 123,
    "side": "RIGHT",
    "body": "Review comment text."
  }
]
EOF
./bin/gh-pending-review replace-comments --pr <number> --comments-file "$tmp" --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
rc=$?
rm -f "$tmp"
exit $rc
```

When using a heredoc for JSON, write raw JSON object syntax inside the heredoc.
Do not shell-escape the JSON quotes.

For reconciliation-sensitive comment updates, do not treat the task as complete
until `replace-comments` succeeds. If a write fails, fix the command or payload
and retry. When helpful, run `get` again to confirm the live comment body now
matches the reconciled text.

Important behavior:

- this is replace-only, not patch-in-place comment editing
- the tool updates the existing pending review session in place rather than
  intentionally recreating the top-level review
- the managed set may contain both new draft threads and replies to existing
  review threads
- use `--force` only after reading and reconciling live changes intentionally

Comments file shape:

```json
[
  {
    "path": "path/to/file.go",
    "line": 123,
    "side": "RIGHT",
    "body": "Review comment text."
  }
]
```

For ordinary new-thread replacements, prefer that default shape and omit
`kind`. Only include `kind` when you actually need a non-default entry type such
as `thread_reply`.

Replies to existing review threads use `kind: "thread_reply"` and a GitHub
review thread node ID:

```json
[
  {
    "kind": "thread_reply",
    "thread_id": "PRRT_kwDODq-TvM52SHRS",
    "body": "This should stay on the existing thread."
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

Normalization rule:

- replies are compared by reply kind, thread ID, and body
- GitHub may read replies back with inherited anchor fields such as `path`,
  `line`, or `start_line`; those fields are ignored for reply comparison and
  conflict reconciliation

When reconciling comments after a human GitHub edit:

1. run `get`
2. read the live pending review JSON
3. identify the managed comment entries in `comments`
4. apply any `!!` instruction to the live comment text
5. remove the `!!` instruction text from the final comment body
6. preserve the human note, shared context, and anchor fields that should stay
7. write the desired managed set to a JSON file using the `replace-comments`
   input format
8. rerun `replace-comments` with `--force` only after that reconciliation

For new-thread entries, the replacement file normally keeps `path`, `line`,
`side`, and the reconciled `body`, while omitting `kind` unless a non-default
entry type is needed. For thread replies, keep `kind:
"thread_reply"`, `thread_id`, and the reconciled `body`, while omitting
read-only fields such as reply comment IDs inherited from `get`.

### Local State

Use local-only state commands for inspection or recovery:

```bash
./bin/gh-pending-review get-state --pr <number> --user <github-login> --state-dir "$HOME/.local/state/gh-pending-review"
./bin/gh-pending-review delete-state --pr <number> --user <github-login> --state-dir "$HOME/.local/state/gh-pending-review"
```

These commands do not talk to GitHub and do not mutate pending reviews.

### Delete

If the instruction is to remove the pending review entirely:

```bash
./bin/gh-pending-review delete --pr <number> --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

If the goal is "make sure no pending review is left behind" in one command, use
`--ensure-absent`. That makes delete idempotent: it succeeds when no live draft
exists and still clears any stored local state for that PR.

## Conflict Handling

The local state reflects what the tool last published, not what it last read.
That is the basis for overwrite protection.

If `update-body` or `replace-comments` reports a conflict:

1. run `get`
2. inspect the current live review body and comments
3. reconcile the user's edits into the desired new state
4. retry with `--force` only if overwriting is intentional

Do not retry blindly after a conflict.

If the request already says the draft was edited in GitHub, assume the safe flow
is still: `get`, reconcile, then mutate. Do not skip straight to overwriting
based only on local state.

## Interpretation Rules

- Treat `!!` in the review body or inline comments as instructions to the
  agent.
- If the user asks you to read the review back first, do not mutate it until
  they confirm.
- Prefer `--body-file` for nontrivial review bodies.
- Preserve the user's current draft intent when replacing comments; the tool
  keeps the existing top-level review and updates its managed entry set.
- When `!!` appears inside a live inline comment, edit that live comment text
  into the requested final comment body rather than appending a second
  instruction response.
- For write requests, completion means the pending-review write command
  succeeded. Do not stop after a failed mutation or after only reading the
  draft state.
- If there is no pending review for the authenticated user, say so plainly.
- Preserve safety: do not submit the review.

## Current Scope

This skill covers pending review state only:

- top-level body create/read/update/delete
- inline comment and existing-thread reply read/replace
- local state inspection and cleanup

It does not cover submission.
