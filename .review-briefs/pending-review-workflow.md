# Pending Review Workflow

## Overview

This branch adds a repo-local GitHub pending-review workflow for `avalanchego`.
The core addition is `gh-pending-review`, a narrow CLI for manipulating only the
authenticated user's `PENDING` GitHub review without ever submitting it.

Around that tool, the branch also adds:

- a `pending-review` agent skill that teaches Claude/Codex how to use the tool
  in a human-in-the-loop review-authoring workflow
- a reusable `tools/skilltest` harness for running skill-backed agent tests from
  Go tests
- end-to-end pending-review skill tests that exercise real agent behavior
  against a fake GitHub backend

Taken together, the change is not just "add a CLI" and not just "add a skill".
It introduces a review-authoring model where:

- the agent may draft and revise a pending review body
- the agent may manage draft inline comments, including replies on existing
  review threads
- the human may edit the same pending review in GitHub between agent runs
- the tool must detect and protect those human edits before overwriting them
- the human remains responsible for actually submitting the review in GitHub

## Why now

This branch is addressing a gap in the current agent-assisted review workflow.

The repo needed a supported way for an agent to help author GitHub review
content without crossing the line into autonomous review submission and without
blindly overwriting edits the human made in the GitHub UI.

The older draft-review model was also too limited for the intended workflow
because it treated pending review comments primarily as replace-only inline
comments. That was not enough to support a realistic draft session that can mix:

- new review comments that open new threads
- replies on already-existing review threads
- iterative body/comment updates after human edits

This branch moves the workflow to a GraphQL-backed pending-review-session model
and then builds the skill and tests around that maintained contract.

## What reviewers should know

The most important mental model is that this branch is implementing an
optimistic-concurrency pending-review tool, not a generic GitHub write layer.

The stored local state is intentionally "what the tool last published", not
"what the tool last read". Mutation commands compare live GitHub state against
that published state before making changes. That is the main protection against
silently clobbering human edits made in GitHub between agent runs.

The managed review entry model now has two distinct kinds:

- `new_thread` for agent-authored draft comments that start new review threads
- `thread_reply` for agent-authored draft replies attached to existing review
  threads

That distinction matters throughout the implementation:

- GraphQL read mapping needs to reconstruct the current draft session from both
  pending reviews and review threads
- normalization needs to compare replies by thread identity and body rather than
  by inherited anchor fields GitHub may attach on readback
- reconciliation logic needs to protect unrelated managed entries while still
  allowing a narrow one-comment rewrite path

The skill contract is also deliberately narrow. The skill is for operating the
repo-local tool in a human-submitted review flow. It is explicitly not for
review submission, approval/request-changes flows, or generic GitHub API access.

## Scope

This branch:

- adds `tools/pendingreview`, including the `gh-pending-review` CLI and its
  docs
- adds isolated `gh` auth handling so pending-review writes do not reuse ambient
  read-only token environment
- adds pending-review commands for create/get/update-body/delete,
  replace-comments, upsert-comment, get-state, and delete-state
- adds support for both new draft threads and replies on existing review
  threads
- adds explicit conflict protection around body updates and comment mutations
- adds `--create-if-missing` for comment-only draft flows
- adds idempotent `delete --ensure-absent` behavior for recovery/cleanup
- adds a repo-local `pending-review` skill that teaches agents how to use this
  workflow
- adds `tools/skilltest` for exercising skills end to end from Go tests
- adds a gated pending-review skill test suite plus deterministic tool tests and
  task wiring
- adds design and maintained-contract documentation for the pending-review
  workflow

This branch does not:

- submit reviews
- automate approve/request-changes flows
- provide a generic GitHub mutation framework
- try to replace human review judgment with agent policy
- make the slow agent-driven skill suite part of ordinary ungated `go test`
  usage

## Approach

The implementation is split across three layers.

First, `tools/pendingreview` owns the actual GitHub pending-review operations.
It uses GraphQL-backed review-session reads and writes, persists local published
state, and enforces the mutation safety rules. The CLI surface is intentionally
small and aligned to the supported review-authoring operations.

Second, `agents/skills/pending-review/SKILL.md` defines the agent-facing
workflow contract. The skill tells the agent when to use `gh-pending-review`,
how to stage body/comment payloads, when to read back live state, how to handle
`!!` instructions from human edits, and when `--force` is or is not appropriate.

Third, the branch validates the skill through a dedicated harness instead of
only unit-testing the docs or command parser. `tools/skilltest` launches a real
agent CLI, injects the skill, captures the transcript, and lets tests shadow
external commands. The pending-review skill tests then run that real agent flow
against a fake GitHub backend that implements the subset of GraphQL operations
the tool currently depends on.

The result is a narrow but end-to-end tested workflow:

- real skill instructions
- real agent process
- real repo-local CLI
- fake but stateful GitHub backend

## Validation

Validation is intentionally split into cheap deterministic coverage and slow
agent-driven coverage.

Fast validation:

- `task pendingreview:test-tool`
- deterministic tests under `./tools/pendingreview/...`
- unit tests for `./tools/skilltest/...`

These tests cover command parsing, auth/config behavior, GraphQL request/response
mapping, state persistence, conflict detection, deletion semantics, logging, and
single-comment/full-set comment workflows.

Separate live API validation also exists under
`tools/pendingreview/integration_test.go`, but it is explicitly gated behind
`GH_PENDING_REVIEW_LIVE_TEST=1` plus user-selected PR/repo inputs. That path is
for deliberate live GitHub validation, not ordinary local test execution.

Slow validation:

- `task pendingreview:test-skill`
- `task pendingreview:test`
- `RUN_PENDING_REVIEW_SKILL_TESTS=1 go test ./agents/skills/pending-review/tests -count=1`

The skill suite is gated on purpose so normal local `go test` runs do not
unexpectedly trigger multi-minute agent sessions. The suite exercises the skill
against the fake backend across the main authoring/recovery flows, including:

- body creation and update workflows
- reading back the live pending review
- full managed comment replacement
- single-comment upserts
- creating a draft review implicitly for comment-only flows
- replying on an existing thread
- external-edit reconciliation
- conflict detection and recovery
- local-state inspection and deletion semantics

Reviewers should treat the split validation model itself as part of the design:
the branch is intentionally choosing broad behavioral coverage without making
all of it an always-on unit-test path.

## Review focus

Focus review on whether the three layers stay aligned with each other.

- `tools/pendingreview/github.go`: verify the GraphQL read/write mapping,
  especially how pending-review comments are correlated with existing review
  threads and how reply-vs-thread behavior is reconstructed.
- `tools/pendingreview/comments.go` and `tools/pendingreview/state.go`: verify
  normalization, identity, and conflict-detection rules, since these define when
  the tool protects human edits versus allowing a rewrite.
- `tools/pendingreview/app.go`: verify the command-level safety boundaries,
  including `--force`, `--create-if-missing`, and `delete --ensure-absent`.
- `tools/pendingreview/README.md`: verify that the maintained tool contract
  still matches the implementation. This README is substantive, not incidental.
- `agents/skills/pending-review/SKILL.md`: verify that the agent instructions
  accurately reflect the CLI contract and do not imply unsupported behavior.
- `agents/skills/pending-review/tests/skill_test.go` and `tools/skilltest`:
  verify that the test harness is realistic enough to catch skill/tool contract
  drift without becoming misleadingly overfit to the fake backend.

## Follow-ups

Likely follow-up areas, intentionally not completed here:

- broader live GitHub validation beyond the current explicitly gated paths
- future updates if GitHub's GraphQL draft-review surface changes again
- any further evolution of the review-authoring workflow beyond pending-review
  operations

## References

- `tools/pendingreview/README.md`
- `tools/pendingreview/graphql-pending-review-design.md`
- `agents/skills/pending-review/SKILL.md`
- `agents/skills/pending-review/testing-plan.md`
- `tools/skilltest/README.md`
