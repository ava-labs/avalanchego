---
name: pending-review
description: Use when the user wants to create, read, update, or delete a GitHub pending review body through the repo-local `gh-pending-review` tool as part of a human-in-the-loop PR review workflow. This skill is for top-level review body iteration only; do not use it for inline comment editing or for submitting reviews.
---

# Draft Review

Use the repo-local `gh-pending-review` tool to manipulate the authenticated
user's pending GitHub review on `ava-labs/avalanchego`.

## When To Use

Use this skill when the user wants to:

- create a pending draft review body
- fetch the current pending draft review body
- update the current pending draft review body
- delete the current pending draft review
- iterate on a review body after the user edits it in GitHub with `!!`
  instructions

Do not use this skill for:

- submitting a review
- approving or requesting changes
- editing inline review comments
- generic GitHub API access outside pending review operations

## Tool Boundary

Use the repo-local launcher:

```bash
./bin/gh-pending-review
```

This tool is only the GitHub interface. Keep instruction interpretation in the
agent, not in the tool.

The tool currently supports:

- `create --pr <number> --body <text>`
- `get --pr <number>`
- `update-body --pr <number> --body <text> [--force]`
- `delete --pr <number>`
- `version`

Unless there is a reason to do otherwise, use:

```bash
--config-dir "$HOME/.config/gh-pending-review"
--state-dir "$HOME/.local/state/gh-pending-review"
```

The tool stores the last review body it published in the local state directory
and uses that as an optimistic-concurrency guard before updates.

## Workflow

### Create

When the user asks to post a draft review body:

```bash
./bin/gh-pending-review create --pr <number> --body '<text>' --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

Return the created review ID and URL.

### Read Back

When the user says they edited the review in GitHub:

```bash
./bin/gh-pending-review get --pr <number> --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

Read the review body and inspect it for `!!` instructions.

### Update

If the user wants you to act on the edited review body, convert their
instruction into the new desired top-level review body and apply it with:

```bash
./bin/gh-pending-review update-body --pr <number> --body '<new text>' --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

If the tool returns a conflict error, that means the live review body no longer
matches the last body the agent published. In that case:

1. read the current body with `get`
2. reconcile the user's edits into the desired new body
3. only then retry with `--force` if overwriting is intentional

### Delete

If the instruction is to remove the pending review entirely:

```bash
./bin/gh-pending-review delete --pr <number> --config-dir "$HOME/.config/gh-pending-review" --state-dir "$HOME/.local/state/gh-pending-review"
```

## Interpretation Rules

- Treat `!!` in the review body as instructions to the agent.
- If the user explicitly asks you to act directly on those instructions, do so.
- If the user asks you to read the review back first, do not mutate it until
  they confirm.
- If `update-body` reports a conflict, do not immediately retry blindly. Read
  the current review, reconcile it, and only then consider `--force`.
- Preserve safety: do not submit the review.
- If there is no pending review for the authenticated user, say so plainly.

## Current Scope

This skill is intentionally limited to the top-level review body. If the user
needs inline review comment workflows, that is a later extension.
