---
name: land-prs
description: Triage your open PRs and walk them to "auto-merge enabled" one at a time. Use when the user says things like "land my PRs", "let's work on landing PRs", "what PRs are ready", "next action on my PRs", or otherwise wants to make progress on their open pull requests. Designed for a recurring (daily/weekly) review-and-land workflow.
---

# land-prs

A guided workflow for triaging the user's open PRs and landing the ones that are ready. Run from inside the relevant repo. This skill covers the recurring "let's land what we can" session that happens every few days.

Throughout this document, `<default-branch>` is a placeholder for the repository's default branch — `main`, `master`, `develop`, or whatever the repo uses — and `<OWNER>/<REPO>` for the GitHub slug. Substitute the real values when running the commands.

The version-control commands here assume [Jujutsu](https://jj-vcs.github.io/jj/) (`jj`) as the front-end over git; see the `jj` skill for the full command mapping, the signing flow, and the merge-commit conflict resolution this skill refers to. On a plain-git repo, substitute the git equivalents.

## Phase 1 — Triage

List the user's open PRs and their state. Use `gh pr list --author "@me"` with JSON fields that reveal blockers.

```bash
gh pr list --author "@me" --json number,title,isDraft,reviewDecision,mergeStateStatus,headRefName,updatedAt --limit 50
```

**Skip drafts.** They're work-in-progress, not landing candidates.

For each non-draft PR, check three things:

1. **`reviewDecision`** — `APPROVED`, `CHANGES_REQUESTED`, or `REVIEW_REQUIRED`.
2. **`mergeStateStatus`** — `CLEAN`, `BEHIND`, `BLOCKED` (e.g. waiting on CI), `DIRTY` (conflicts), `UNKNOWN`.
3. **Unresolved review threads** — *a PR can be approved and still have unresponded comments that must be addressed before auto-merge.* Always check.

```bash
gh api graphql -f query='query($pr: Int!) {
  repository(owner:"<OWNER>", name:"<REPO>") {
    pullRequest(number:$pr) {
      reviewThreads(first:100) {
        nodes {
          id isResolved isOutdated path line
          comments(first:5) { nodes { databaseId author { login } body createdAt } }
        }
      }
    }
  }
}' -F pr=<NUMBER> --jq '.data.repository.pullRequest.reviewThreads.nodes | map(select(.isResolved == false))'
```

Bucket the PRs:

- **Truly ready** — APPROVED, 0 unresolved threads, mergeStateStatus CLEAN or BLOCKED-by-CI only.
- **Approved but has unresponded comments** — APPROVED, but at least one `isResolved == false` thread. Often a quick fix.
- **Needs work** — CHANGES_REQUESTED or many unresolved threads. Big chunk; flag and don't dive in unless the user asks.

Present the triage as a short table, then suggest a sensible order (typically easiest first). Ask before diving in.

## Phase 2 — Per-PR landing flow

For each PR the user wants to address, follow this loop. **The order matters.**

### 1. Fetch and re-check mergeStateStatus, every time

```bash
jj git fetch
gh pr view <N> --repo <OWNER>/<REPO> --json mergeStateStatus,reviewDecision,headRefName
```

Run this at the start of each PR, **before every new commit on an existing PR branch**, before any push, and at any natural break. Auto-merge or other PR landings can flip a CLEAN PR to BEHIND mid-session, and CI will start failing with merge-conflict errors that look like real bugs. **Stale state is the #1 cause of botched landings.**

**`mergeStateStatus: BEHIND` is authoritative.** Don't reason about it. If the branch has an old `merge: merge <default-branch> into <branch>` commit on it, that does not make it current — the default branch moves continuously. Trust the field, run the merge (step 6).

### 2. Branch to a new commit

```bash
jj new <pr-branch> -m "<conventional-commit subject>"
```

The subject becomes the PR-tail commit message. **Always use Conventional Commits format** (`feat(scope): ...`, `fix(scope): ...`, `refactor(scope): ...`, etc.) regardless of whether the target repo enforces it on PR titles — it's a hard rule for this workflow.

Never edit an already-pushed commit. Pushed revisions are immutable; stack a new commit on top so reviewers can see exactly what changed since their last pass.

### 3. Make the change

- Address every unresponded comment, not just the obvious ones.
- **Fix nearby instances of the same issue** if the change is localized or test-only. Call it out in the reply so the reviewer knows the fix went beyond their literal ask.
- For test-assertion strengthening, prefer a concrete equality assertion against a computed expected value over a weak "not empty" / "not equal" check.
- **For substantive changes, present the design before editing.** A bug description + concrete plan (what to change, what to keep, what's beyond the reviewer's literal ask) gives the user a chance to redirect cheaply. Push back on reviewer suggestions you disagree with — they're suggestions, not orders.
- **After a rename or signature change, audit for stale references.** This catches what the compiler/tests miss:
  - `grep` the old identifier across all touched files (and adjacent ones — a doc comment on a sibling type may reference a renamed method)
  - check doc-comment cross-references (e.g. godoc `[oldName]` links, rustdoc intra-doc links) — those don't error, they just rot
  - check field/method docs that describe initialization/lifecycle when those changed
  - check the type's main doc paragraph for terminology drift (e.g. it still says "WaitGroup" after you replaced the waitgroup with a counter)

### 4. Local validation

Follow the repo's own authority for which checks to run — typically `CLAUDE.md`, `CONTRIBUTING.md`, the task runner's task list, and any language-specific READMEs. This skill deliberately does not duplicate those command lists; the repo is canonical and changes with the codebase.

Everything the repo specifies must be clean, **including the formatter** — formatting issues sneak past tests and linters locally and then break CI. **For concurrency-touching changes** (locking, channels, goroutines, shared-state bookkeeping) also run the language's race detector — race conditions are exactly the class of bug these changes risk introducing.

### 5. Stop for the user's review of the local changes

This is a hard checkpoint. Summarize: what changed, what's beyond the reviewer's literal ask, and that local checks are green. Wait for the user to say "looks good" / "continue" / "push it" before proceeding.

**For substantive commits, write a commit body**, not just a subject. Subject summarizes; body explains *why* — the bug being fixed, the design rationale, what it intentionally doesn't do. Don't just describe the diff (the diff already does that). A reader six months from now should be able to reconstruct your thinking from the body alone. Skip the body only for genuinely trivial fixes (one-line typo, dependency bump, etc.).

**Granularity:** substantive review items get their own commit, one at a time — pushing tougher fixes individually keeps them reviewable in isolation. Small bug fixes and doc/nit cleanups can be batched into a single commit (the user will indicate when to batch).

### 6. Merge the default branch into the branch (whenever BEHIND)

Re-check `mergeStateStatus` right before pushing — it can flip during a session as other PRs auto-merge. If `BEHIND`:

```bash
jj git fetch                       # always re-fetch right before this
jj new <branch> <default-branch> -m "merge: merge <default-branch> into <branch>"
```

**Parent order matters** — the PR branch first, the default branch second. Reversed, GitHub renders the PR as if the branch were merging into itself. See the `jj` skill for the full merge-commit flow.

**Trust mergeStateStatus over branch history.** A prior `merge: merge <default-branch> into <branch>` commit on the branch does not make it current — the default branch moves continuously. If the field says BEHIND, run the merge. Don't reason your way out of it.

**Do the merge after the user approves the fix but before the first push.** Folding the default-branch merge into the same push avoids two CI cycles.

If conflicts arise, resolve them as a real merge commit and describe the resolution in the commit body. Rebuild and re-run tests after resolving.

**`jj` can auto-resolve textually clean merges that are semantically broken.** When the default branch adds new code that calls into APIs your branch renamed (e.g. it adds `F() { x.OldName() }` and your branch renamed `OldName` → `NewName`), jj's merge produces no conflict markers but the result won't build. Always rebuild after a merge, even if jj reports zero conflicts.

### 7. Sign, move bookmark, and push

```bash
jj sign                            # sign the unpushed stack — tell the user a hardware-key touch is coming
jj bookmark move <branch> --to @
jj git push --bookmark <branch>
```

**Sign as an explicit step before pushing.** Signing during the push blocks on a hardware-key touch the user isn't expecting, and the prompt times out. See the `jj` skill's *Signing before push* section.

If a push still fails with `gpg: signing failed: Timeout`, **just retry the same push command**. The retry surfaces the unlock dialog the user can act on. Don't try to disable signing or work around it. Wait for the unlock, then the push completes (you'll see `Updated signatures of N commits`).

Record the new sha — you'll need it for the comment replies. Then run `jj new` so no further edits land on an already-pushed revision.

### 8. Reply to threads

For each unresolved thread you've addressed, post a reply:

```bash
gh api repos/<OWNER>/<REPO>/pulls/<PR>/comments/<commentDbId>/replies \
  -f body="Fixed in <short-sha>. <optional kudo or note>."
```

Kudo phrasing rules:

- For human review comments, sprinkle in kudos: "Nice catch", "Good point", "Great find", "Thanks for catching this", "Makes sense".
- **Max 5 kudos per PR**, and **don't repeat the same phrase on the same PR**. Track which you've used.
- Not every reply needs a kudo — the bare `"Fixed in <sha>."` is fine for terse confirmations.
- For multi-instance fixes, use: `"Fixed in <sha>. Good point, there was another case of this I also fixed. ..."`

Keep replies terse. If it isn't obvious from the diff, explain how it was fixed; the reviewer can read the commit body for more. Don't restate an obvious change.

Don't mark threads resolved yourself — that's the user's call (and often the reviewer's).

**Reply-only threads.** Some threads need only a typed response, not a code change ("I agree, leaving as-is" / "low priority, will follow up"). The user may prefer to handle those manually offline rather than have you draft replies. Surface them clearly in the remaining-threads list and wait for direction.

### 9. Ask to enable auto-merge

Once the fix is pushed and every unresolved thread has a reply, and the PR is APPROVED, ask the user:

> "Ready to enable auto-merge on `#<N>`?"

If yes:

```bash
gh pr merge <N> --repo <OWNER>/<REPO> --auto --squash
```

Then verify:

```bash
gh pr view <N> --repo <OWNER>/<REPO> --json autoMergeRequest,mergeStateStatus
```

## Phase 3 — Session wrap-up

**Only run Phase 3 if at least one PR was actually pushed or merged this session.** If the triage produced nothing actionable (everything is drafts or stalled on large multi-thread reviews), there's nothing in the default branch that wasn't already there — skip Phase 3 entirely. Just summarize what's blocking the remaining PRs and stop.

If the user keeps a fork or mirror that tracks this repo's default branch, **offer to update it** after the public PRs land:

> "Want me to update the fork's default branch with the latest from upstream?"

Don't do it unprompted — wait for the go-ahead. Some branches must not accidentally move.

## Things that go wrong

- **Signing timeout on push** → retry the same command once; the retry surfaces the unlock dialog. Better: pre-sign with `jj sign`.
- **Conflicts when merging the default branch** → real merge commit, describe the resolution; see the `jj` skill.
- **PR is APPROVED but unresolved threads still exist** → those are blocking. Don't enable auto-merge until they all have replies.
- **PR was approved on an earlier sha** → if you push new commits, GitHub keeps the approval valid unless the reviewer dismissed it. Check `reviewDecision` again before enabling auto-merge.
- **Another PR auto-merged during your session** → `jj git fetch` again. Conflicts and BEHIND states change.
- **Wrong remote** → in a repo with more than one remote (e.g. a public upstream and a private fork), confirm which remote a branch belongs to before pushing. Never push work that isn't cleared for public release to the public remote, and only with the user's explicit go-ahead.
- **CI suddenly red on a long-running PR after weeks of green** → the default branch has almost certainly moved and added code that depends on (or conflicts with) something on the branch. Diagnose with `jj log -r '<branch>..<default-branch>'` and `jj diff --git --from 'fork_point(<default-branch> | <branch>)' --to <default-branch> --stat` over the touched paths *before* reading the failure as a real bug. Don't debug code first.
- **"We merged the default branch already" reasoning** → the branch having an old `merge:` commit does not mean it's current. `mergeStateStatus` is authoritative. If it says BEHIND, merge again.
- **Textually clean merge that doesn't build** → jj reports zero conflicts but the build fails because the default branch added callers of APIs your branch renamed. Always rebuild after a merge. Fix the callers, fold the fix into the merge commit body.

## Useful one-liners

List my open PRs with their state:

```bash
gh pr list --author "@me" --json number,title,isDraft,reviewDecision,mergeStateStatus,headRefName --limit 50
```

Get unresolved threads on a PR (with comment DB IDs for replying):

```bash
gh api graphql -f query='query($pr:Int!){repository(owner:"<OWNER>",name:"<REPO>"){pullRequest(number:$pr){reviewThreads(first:100){nodes{isResolved isOutdated path line comments(first:1){nodes{databaseId author{login} body}}}}}}}' -F pr=<N> --jq '.data.repository.pullRequest.reviewThreads.nodes|map(select(.isResolved==false))'
```

Check checks status on a PR:

```bash
gh pr view <N> --repo <OWNER>/<REPO> --json statusCheckRollup,reviewDecision,mergeStateStatus
```

Reply to a review comment:

```bash
gh api repos/<OWNER>/<REPO>/pulls/<PR>/comments/<commentDbId>/replies -f body="Fixed in <sha>. ..."
```
