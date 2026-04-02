# GraphQL Pending Review Session Design

This document captures the `xb1.9` research outcome for migrating
`gh-pending-review` from the current REST-backed pending-review model to a
GraphQL-backed draft review session model that can represent both:

- new review comments that start new threads
- replies attached to existing review threads

Validation date: 2026-04-01.

## Why Change

The current implementation is REST-backed and models pending review comments as
replace-only inline comments. That works for authoring new comments, but it
cannot represent "reply on an existing thread as part of my pending review"
without falling out of the current abstraction.

GitHub's current GraphQL schema exposes draft-review-thread and draft-reply
mutations directly. GitHub's GraphQL breaking-change documentation also marks
the older comment-oriented mutation path as deprecated in favor of thread-based
operations.

## Live Findings

### Confirmed from live schema introspection

The GraphQL schema currently exposes the mutation and input types needed for a
unified draft review session:

- `addPullRequestReview(input: AddPullRequestReviewInput!)`
- `addPullRequestReviewThread(input: AddPullRequestReviewThreadInput!)`
- `addPullRequestReviewThreadReply(input: AddPullRequestReviewThreadReplyInput!)`
- `updatePullRequestReview(input: UpdatePullRequestReviewInput!)`
- `deletePullRequestReview(input: DeletePullRequestReviewInput!)`

Relevant input-shape findings from live introspection:

- `AddPullRequestReviewInput` accepts `body`, `event`, and `threads`.
- `AddPullRequestReviewInput.comments` still exists, but is documented in the
  live schema as deprecated in favor of `threads`.
- `DraftPullRequestReviewThread` uses file-line coordinates:
  `path`, `line`, `side`, `startLine`, `startSide`, `body`.
- `AddPullRequestReviewThreadInput` can target either a pull request
  (`pullRequestId`) or an existing pending review (`pullRequestReviewId`).
- `AddPullRequestReviewThreadReplyInput` requires
  `pullRequestReviewThreadId` and `body`, and also accepts an optional
  `pullRequestReviewId`.

The optional `pullRequestReviewId` on
`AddPullRequestReviewThreadReplyInput` is the strongest schema signal that a
reply can be attached to an existing pending review instead of being published
outside that draft session.

### Confirmed from live read queries

A live query against `ava-labs/avalanchego#5168` on 2026-04-01 confirmed:

- `pullRequest.reviewThreads` returns existing review threads
- each thread returns `comments`
- each comment exposes:
  - `replyTo`
  - `pullRequestReview`
  - `path`
  - `line`
  - `originalLine`
  - `startLine`
  - `originalStartLine`
  - `state`

Those fields are sufficient to reconstruct:

- which comments begin a thread (`replyTo == null`)
- which comments are replies (`replyTo != null`)
- which review a draft or submitted comment belongs to
- the file/line anchor associated with the thread

### Confirmed from live write validation

Live write validation succeeded on `ava-labs/avalanchego#5122` on 2026-04-01
using isolated auth from `GH_CONFIG_DIR=$HOME/.config/gh-pending-review`.

Validated mutation sequence:

1. `addPullRequestReview`
   - created pending review `PRR_kwDODq-TvM7xO-8o`
2. `addPullRequestReviewThread`
   - added a new thread on `.github/workflows/bazel-ci.yml:43`
3. `addPullRequestReviewThreadReply`
   - added a reply on existing thread `PRRT_kwDODq-TvM52SHRS`
   - reply was attached to pending review `PRR_kwDODq-TvM7xO-8o`
4. `updatePullRequestReview`
   - updated the top-level draft body in place
5. `deletePullRequestReview`
   - removed the temporary draft review cleanly

Observed result:

- one pending review session can contain both:
  - a new thread created during the draft session
  - a reply attached to an already-existing review thread
- both comments were readable back through
  `pullRequest.reviews(states:[PENDING])`
- the reply was also readable through `pullRequest.reviewThreads`, where it
  remained attached to the existing thread while still pointing back to the
  pending review via `pullRequestReview`
- deleting the pending review removed the temporary validation artifacts and
  left the PR with no pending review for the authenticated user

## Recommended Model

Use a single local draft-review session model that owns:

- the top-level pending review body
- new draft threads to be opened on diff lines
- draft replies to already-existing review threads

Recommended shape:

```go
type DraftReviewSession struct {
    Repo         string
    PRNumber     int
    UserLogin    string
    ReviewNodeID string
    ReviewURL    string
    Body         string
    Entries      []DraftReviewEntry
}

type DraftReviewEntry struct {
    Kind                 DraftReviewEntryKind
    LocalID              string
    Body                 string
    ThreadNodeID         string
    ReplyToCommentNodeID string
    Path                 string
    Line                 int
    Side                 string
    StartLine            int
    StartSide            string
}
```

With:

- `Kind == new_thread` for agent-authored findings that start new threads
- `Kind == thread_reply` for replies to already-existing review threads

Notes:

- `ThreadNodeID` is required for `thread_reply` entries.
- `ReplyToCommentNodeID` is optional local bookkeeping. GitHub replies are
  anchored to the thread mutation, not directly to a specific parent comment.
- file/line fields are required for `new_thread` entries and should be empty for
  `thread_reply` entries.

## Required GraphQL Surface

### Read

Use one pull-request query that fetches both pending reviews and existing review
threads:

- `pullRequest.reviews(states: [PENDING])`
- `pullRequest.reviewThreads`

The read path should identify the authenticated viewer's pending review from
`reviews(states: [PENDING])`, then reconcile session entries from two sources:

- `PullRequestReview.comments`
  - authoritative for comments already attached to the viewer's pending review
- `PullRequest.reviewThreads.comments`
  - authoritative for thread identity and for linking replies back to existing
    threads via `replyTo` and `pullRequestReview`

Recommended rule:

- treat `pullRequestReview.id == currentPendingReview.id` as membership in the
  local draft session
- classify entries with `replyTo == null` as `new_thread`
- classify entries with `replyTo != null` as `thread_reply`

### Write

Use these mutations:

1. `addPullRequestReview`
   - create the pending review
   - include `body`
   - optionally include initial `threads`

2. `addPullRequestReviewThread`
   - add a new thread to an existing pending review
   - prefer `pullRequestReviewId` once the pending review exists

3. `addPullRequestReviewThreadReply`
   - add a reply to an existing thread
   - pass both `pullRequestReviewThreadId` and `pullRequestReviewId`

4. `updatePullRequestReview`
   - update only the top-level review body

5. `deletePullRequestReview`
   - delete the pending review for cleanup or full replacement flows

## Can One Pending Review Hold Both New Threads And Replies?

Yes.

Reasoning:

- GraphQL exposes `addPullRequestReviewThread` for new draft threads on a
  pending review.
- GraphQL exposes `addPullRequestReviewThreadReply` with an optional
  `pullRequestReviewId`, which strongly implies the reply can be attached to an
  existing pending review.
- both mutations operate on the same `PullRequestReview` node type.
- both surfaces can be read back through `pullRequest.reviews(states:[PENDING])`
  plus `pullRequest.reviewThreads`.

This was confirmed by live mutation testing on `ava-labs/avalanchego#5122` on
2026-04-01. GitHub kept both operations inside one pending review in practice.

## Migration Recommendation

Prefer a full GraphQL migration for pending-review write operations, not a
long-term hybrid write model.

Why:

- the existing REST comment flow is replace-only and recreates the review
- replies on existing threads are a GraphQL-first concept in the current schema
- GitHub's documented direction is thread-based GraphQL mutations, not the older
  comment-based path
- a hybrid write model would preserve the worst current behavior: destructive
  review recreation for new comments while needing GraphQL for replies

A temporary hybrid read path is acceptable during migration if it simplifies the
cutover, but the end state should use GraphQL for:

- create
- get
- update-body
- replace-comments or its successor
- delete

## Existing Abstractions Worth Preserving

These current abstractions are still useful and should survive the migration:

- CLI boundary and command names in `gh-pending-review`
- isolated auth flow from `tools/pendingreview/auth.go`
- one-pending-review-per-viewer lookup semantics
- local state as an optimistic-concurrency guard
- narrow repo-local scope: pending review manipulation only, never submission

These current abstractions should change:

- `DraftReviewComment` is too narrow because it only models new threads
- `ReviewState.LastPublishedComments` should become a richer entry list
- `replace-comments` should stop deleting and recreating the review once the
  GraphQL session model exists
- readback should stop depending on REST review-comment listing alone

## Live Validation Outcome

The required write-path validation has been completed successfully.

Implementation for `xb1.10` can proceed on the unified session model described
above:

- create the pending review with `addPullRequestReview`
- add new draft threads with `addPullRequestReviewThread`
- add replies to existing threads with `addPullRequestReviewThreadReply`
- read back draft-session state from `reviews(states:[PENDING])` plus
  `reviewThreads`
- update the draft body with `updatePullRequestReview`
- delete the draft with `deletePullRequestReview`
