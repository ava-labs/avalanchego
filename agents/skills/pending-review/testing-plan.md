# Pending Review Skill Testing Plan

This document captures the intended direction for end-to-end testing of the
`pending-review` skill so a later implementation session can proceed without
reconstructing the design discussion.

## Goal

Validate that an agent can use the `pending-review` skill and the repo-local
`gh-pending-review` tool to iteratively manage a pending GitHub review with a
human in the loop.

This is **not** a test of review quality. The content of the review body or
comments can be minimal. The point is to verify interactive draft-management
behavior:

- can the agent read the current pending review state?
- can it mutate the right part of the draft when asked?
- can it preserve human edits?
- can it respond to `!!` feedback correctly?
- can it avoid clobbering live changes?
- can it stay within the pending-review tool boundary?

## What Is Under Test

The thing under test is not whether the agent produces a good review. The thing
under test is whether the agent can correctly use the skill and tool to
interactively refine review state with the human in the loop.

The test suite should focus on:

- workflow control
- state awareness
- human-loop responsiveness
- mutation safety

The suite should not initially focus on:

- quality of review prose
- quality of review judgment
- whether comments are insightful or complete

## Test Oracle

The authoritative test oracle is **fake backend state**, not agent narration.

The assertions should be prioritized as follows:

1. **Primary**: final fake backend live review state
2. **Secondary**: command sequence, flags, and payloads sent through the real
   tool
3. **Tertiary**: agent output transcript, only when useful for debugging or
   explicit user-facing guarantees

Example: for a create-body scenario, the main assertion is that the fake live
review body equals the user-requested text. It is not enough to assert that the
agent claimed success.

## Harness Shape

The preferred harness is:

- the real skill
- the real `./bin/gh-pending-review` CLI
- the real pending-review app logic and local state logic
- a fake local server replacing the GitHub backend

This is intentionally **not** a fake CLI wrapper. The stateful end-to-end tests
should execute the real repo-local launcher and redirect its network traffic to
the fake backend.

Why:

- it exercises the real CLI surface
- it exercises real argument parsing
- it exercises real app/state/conflict logic
- it keeps the fake limited to the GitHub-facing dependency surface

### Existing Seams To Reuse

The current implementation already provides useful test seams:

- `tools/pendingreview.App` already has a `baseURL` field used by package tests
- `tools/pendingreview.GitHubClient` already talks to a configurable GraphQL
  base URL rather than hardcoding request construction at each callsite
- package tests already include GraphQL fake-server helpers that validate the
  current request/response shape

This means the testing work should prefer **reusing and extending existing
seams** rather than inventing a second fake transport layer.

## Backend Injection

The pending-review tool should be configurable in tests to target a local fake
server instead of real GitHub.

Preferred shape:

- the CLI/app supports a base URL override that reaches `pendingreview.App`
- the skill test configures the tool via environment variable or similar
- the real tool then talks to the fake server for all GitHub operations

The exact mechanism can be decided during implementation, but the desired
effect is:

- agent -> skill -> real `gh-pending-review` -> fake local server

The verification model for these tests should be:

- fake backend request log
- fake backend final state
- local pending-review state on disk

Do **not** rely on wrapper-based command interception for the stateful workflow
suite.

### Constraint: Skill Harness Environment

The current `tools/skilltest` helper supports command wrappers on `PATH`, but
it does **not** currently support per-test environment injection.

That creates a concrete decision for implementation:

1. add environment override support to `skilltest.Config`, or
2. choose a CLI-visible override mechanism that can be driven without custom
   environment injection

Do not leave this implicit. The first vertical slice should pick one path and
validate it end to end.

## Fake Server Rule

The fake server should implement **only the subset of GitHub behavior that
`gh-pending-review` actually uses**.

It should not try to emulate general GitHub behavior.

Add support incrementally as scenarios require it.

## Fake State Model

The fake backend should be backed by a coherent domain model aligned with the
`pendingreview` tool's semantics.

It should support round-tripping:

- fake state -> GraphQL responses consumed by `pendingreview`
- requests sent by `pendingreview` -> state mutations in the fake

This is important because `pendingreview` currently:

- reads GitHub data
- converts that data into internal structs
- acts on those structs
- sends mutations back to GitHub

The fake should let tests exercise that same loop locally.

### Round-Trip Principle

The fake should not just return canned payloads. It should maintain a small
state machine:

- canonical fake review state
- projection helpers from domain state to API response shapes
- request handlers that parse incoming mutations and apply them back onto the
  domain state

This allows:

- realistic create/read/update flows
- realistic conflict simulation
- realistic comment normalization/reply behavior later

### Recommended Fake Shape

Prefer a fake with two layers:

- a small canonical in-memory review state used for assertions
- thin GraphQL handlers that project that state into the exact API responses
  the current client expects

That keeps the test oracle on domain state while still exercising the real
wire protocol.

## Initial Scope

Start with **body-only** workflow validation.

The simplest first scenario is:

- initial state: no pending review
- user asks to create a pending review on PR `123` with body text `foo`
- agent uses the skill
- real tool talks to the fake backend
- fake live review state ends with body `foo`

This is the first real vertical slice because it validates an actual state
change, not just command selection.

### Validation Criteria For Phase 1

Before implementation, treat the first phase as successful only if a test can
assert all of the following mechanically:

- the agent exits successfully
- the fake backend's final live review body matches the requested body
- the fake backend shows exactly one pending review for the expected viewer
- the local state store records the created review ID and last-published body
- the command path went through the real repo-local `gh-pending-review` binary

If any of those require transcript inspection rather than state or invocation
inspection, the harness is still too weak.

## Current Status

There is already a smoke-style skill test in place that validates basic command
selection and repo-local tool usage. Treat that as harness plumbing, not as the
real end-to-end workflow validation target.

The next meaningful step is to add the first stateful workflow scenario using
the fake backend.

## Scenario Philosophy

Tests should be scenario-based rather than monolithic.

The goal is not to exhaustively test every conversation permutation. The goal
is to cover each meaningful workflow branch in the skill contract.

Scenarios should be driven by:

- operation type
- state relationship
- user intent mode
- safety expectation

Not by superficial prompt wording differences.

## Minimum Scenario Set

This is the target workflow inventory to grow into over time.

### Phase 1: Body Workflows

1. Create body from user-provided text
   - no existing draft
   - assert created live body matches requested text
   - assert local state is created consistently

2. Read body back
   - existing draft present
   - user asks to inspect current draft
   - assert no mutation occurs
   - assert returned content matches fake backend state

3. Update body
   - existing draft present
   - user asks to replace body text
   - assert final live body changes accordingly
   - assert stored last-published body advances with the live update

### Phase 2: Comment Workflows

4. Replace managed inline comments
   - existing draft present
   - assert comment set changes and top-level body does not

5. Existing-thread reply support
   - validate reply flow once comment replacement harness is in place

### Phase 3: Human-in-the-Loop Reconciliation

6. Human edits body and leaves `!!` instruction
   - external edit changes live body
   - user asks agent to continue iteration
   - assert agent reads live state first and reconciles rather than clobbering

7. Human edits inline comments
   - external edit changes live comments
   - assert reconciliation path behaves correctly

8. Explicit read-first workflow
   - user asks to inspect before mutating
   - assert no mutation happens until explicitly requested

### Phase 4: Conflict Handling

9. Conflict on body update
   - fake backend/tool reports body conflict
   - assert no blind retry
   - assert read/reconcile flow before any forced overwrite

10. Conflict on comment replacement
   - same pattern for comments

### Phase 5: Recovery and Safety

11. Local state inspection/recovery
   - `get-state` / `delete-state`
   - no live GitHub mutation

12. Delete pending review
   - assert pending review removal and local cleanup behavior

13. Safety boundary
   - no review submission
   - no unrelated GitHub operations
   - no leaving the tool boundary

## Vertical Slice Ordering

Implement in this order:

1. Choose and implement the CLI-to-app backend override path
2. Extend `skilltest` if needed so the skill can activate that override in test
3. Add a minimal fake backend server backed by canonical review state
4. Implement first real body-create scenario
5. Add read-body scenario
6. Add update-body scenario
7. Expand to comments
8. Expand to external edits and conflicts

This sequence proves the harness before adding complexity.

## First Vertical Slice

The first realistic transcript should remain intentionally simple.

Suggested scenario:

- fake backend starts with no pending review
- user prompt says to create a pending review on PR `123` with body `foo`
- agent runs the skill
- real tool creates the draft through the fake backend
- test asserts:
- live review exists
- live body is `foo`
- stored state was updated consistently
- fake server observed the expected GraphQL operations in the expected order

This is the first slice because it proves:

- skill context is sufficient
- the real tool can be driven end to end
- state mutation can be asserted mechanically
- the backend override wiring is actually live

## Multi-Turn Transcript Tests

After the first body-only slices work, move toward transcript-style scenarios
with multiple turns and external edits.

Those scenarios should model:

- user requests
- agent action
- external human edits between turns
- `!!` instructions embedded in live review content
- follow-up user requests
- final reconciled review state

These tests should validate collaboration mechanics, not content quality.

## Data Simplicity

Use minimal sentinel content in tests.

Examples:

- `foo`
- `bar`
- `draft v1`
- `draft v2`
- `user-edited body`
- `!! revise intro`

The point is observability of state transitions, not realism of prose.

## Implementation Notes

- Prefer a fake backend built around the actual subset of GraphQL calls the tool
  currently issues.
- Do not build a generic GitHub emulator.
- Keep the fake domain model small and aligned to the tool's logic.
- Grow the fake surface only when a new scenario needs it.
- Existing pending-review package fake-server helpers currently live in `_test.go`
  files, so the skill tests cannot import them directly. Either promote the
  minimum needed helper code into shared test support or copy the minimum helper
  code into the skill test package deliberately.

## Concrete First Tasks

The next implementation session should start with a short exploration pass and
finish only after one stateful skill test is green.

1. Inspect how `./bin/gh-pending-review` constructs `pendingreview.App` and
   identify the narrowest way to plumb a test-only base URL override through the
   real CLI.
2. Decide whether the skill test will pass that override through environment or
   whether `skilltest` itself needs a small API extension.
3. Factor or copy the smallest existing GraphQL fake-server helper needed from
   `tools/pendingreview` package tests.
4. Write the first failing skill test for create-body with assertions on:
   - fake backend live state
   - local state on disk
   - fake backend request sequence
5. Make the minimum production changes needed to make that test pass.

## Test Execution Assumption

Because the skill invokes `./bin/gh-pending-review` as a repo-local relative
path, the stateful skill tests should run from the actual repo root or another
working directory that contains the real launcher and its build script.

## Out Of Scope For The First Slice

- review submission behavior
- review quality evaluation
- broad transcript snapshot testing
- generic GitHub emulation
- comment-thread parity beyond what body-only scenarios need

## Session Handoff

The next implementation session should start by:

1. Inspecting how `tools/pendingreview` chooses its GitHub base URL and client
   boundary
2. Resolving the `skilltest` environment/override gap explicitly
3. Adding a test-only backend override path for the real CLI
4. Building the smallest fake backend needed for create/read/update-body
5. Replacing or supplementing the current smoke-style scenario with the first
   real stateful create-body scenario
