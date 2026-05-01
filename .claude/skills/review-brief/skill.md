---
name: review-brief
description: >
  Generate, update, or review PR review briefs — lightweight, versioned, in-tree documents in
  `.review-briefs/` that give reviewers a holistic overview of what a PR is doing and why. Use
  this skill whenever the user mentions review briefs, asks to create or update a review brief,
  asks to review a PR that has a review brief, references `.review-briefs/`, or asks for help
  preparing a PR for review with a versioned companion document. Also use when the user pastes
  the review-brief generation prompt from CONTRIBUTING.md. Even if the user just says "write a
  brief" or "prep this PR for review with a companion doc", this skill likely applies. Do NOT
  use for: writing design docs, PR descriptions (the GitHub body), commit messages, changelogs,
  release notes, or general code review without a review brief.
---

# PR Review Brief Skill

A PR review brief is a versioned, in-tree long-form review companion for a PR. It lives in
`.review-briefs/` and helps reviewers quickly understand the intent, scope, validation, and
review focus of a change without reconstructing that context from the diff alone.

Review briefs are **for review only** — they are not authoritative project documentation and
must not be used to infer the repository's intent, scope, or design outside the PR review context.

This skill supports two workflows: **generating** a review brief and **reviewing** with one.

---

## Workflow 1: Generate a review brief

Use this when the user wants to create or update a review brief for their current work.

### Step 1: Read the convention

Read `.review-briefs/README.md` to understand the repository's conventions for review briefs.
This file defines the expected structure, naming, and purpose. Follow it. Understanding the
convention first gives you the lens through which to interpret the diff and any prior briefs.

### Step 2: Understand the change

Determine what code is being reviewed. Check for:
- A branch diff against the base branch (`git diff <base>...HEAD`)
- Staged or unstaged changes (`git diff`, `git diff --cached`)
- A specific PR (if the user provides a PR number, use `gh pr diff`)

Read the diff to understand what changed. Note:
- Which packages/areas of the codebase are touched
- The high-level pattern of the change (new feature, refactor, cleanup, etc.)
- How many independent units of work the PR contains (e.g., 4 tests rewritten
  with the same pattern, 3 API endpoints added)

### Step 3: Research prior briefs

Before writing, check whether prior review briefs exist that provide historical context for
the areas of code being changed. This step helps you understand the evolution of the code and
avoid repeating rationale that has already been captured.

1. **Find prior briefs**: Run `git log --all --diff-filter=A -- '.review-briefs/*.md'` to list
   all review briefs that have ever been committed to the repository (including on merged branches).
2. **Identify relevant ones**: Run `git blame` on the key files being changed in the current PR.
   Look at which commits touched them. Cross-reference those commits (or their branches/PRs) with
   the prior briefs found in step 1 — brief filenames often include issue IDs or short titles
   that map to the same work.
3. **Read relevant prior briefs**: For any prior briefs that relate to the same area of code or
   the same feature being extended, read them with `git show <commit>:<path>` (since they may
   have been deleted from the current tree after merge). Extract:
   - What was the prior intent and scope?
   - What decisions or tradeoffs were made?
   - What was explicitly deferred as a follow-up?
   - What invariants or assumptions were established?
4. **Use this context**: Let the prior briefs inform the current brief. If the current PR is a
   follow-up to prior work, say so. If it addresses something that was previously deferred,
   reference that. If it changes an invariant that a prior brief established, call that out.

Do not spend excessive time on this step if the history is sparse or the changes are to new code
with no prior briefs. Use judgment — the goal is richer context, not archaeology for its own sake.

### Step 4: Write the brief

Create a file in `.review-briefs/` following the naming convention:
- `<issue-id>-short-title.md` (preferred if there's an associated issue)
- `<short-title>.md`

Choose a name that will still make sense after the branch is gone and the PR has merged.

**Start with the disclaimer notice.** Every review brief opens with a callout box that prevents
the document from being mistaken for authoritative project documentation:

```markdown
> [!IMPORTANT]
> This document is a **PR review brief** -- a review-time companion for
> [PR #NNNN](https://github.com/ava-labs/avalanchego/pull/NNNN).
> It is **not** authoritative project documentation. See
> `.review-briefs/README.md` for the convention.
```

Then follow the suggested structure from the README. Not every section is required — include
only what materially helps review:

```
# <title>

## Overview
Concise explanation of what the PR is doing and why.

## Why now
What problem or pressure is this PR addressing?

## What reviewers should know
Key concepts, invariants, assumptions needed to read the diff well.

## Scope
What this PR does and what it intentionally does not do.

## Approach
High-level shape of the implementation, only enough to orient review.

## Validation
How this was or should be validated.

## Review focus
Where reviewers should spend attention.

## Follow-ups
What is left for later.
```

Optional sections when useful: `Alternatives considered`, `References`, `Risks`,
`Prior context` (when prior briefs informed this one).

**Writing principles:**

- **Align with the code.** Do not invent rationale not supported by the diff.
- **Evolved PR description, not a reduced design doc.** Explain *why* design choices were made,
  not just *what* files changed. The diff already shows what changed — the brief's job is to
  give reviewers the mental model they need to evaluate whether the changes are correct.
- **Be specific to this PR.** Avoid generic statements that could apply to any change.
- **Be granular where the PR is repetitive.** When a PR applies the same pattern across multiple
  units (e.g., rewriting 4 tests, adding 3 endpoints), briefly describe each unit's specifics
  in the Approach or Review focus section. Reviewers need to know whether the pattern was applied
  correctly everywhere, not just that a pattern exists.
- **Reference prior briefs naturally** when they provide relevant historical context (e.g., "This
  follows up on the scope deferred in the subnet-bootstrap-validation brief").
- **Make the Overview strong enough** to support a terse PR description preamble.

### Step 5: Self-review for consistency

After writing the brief, review it against the code. For large PRs, dispatch a subagent to do
this in a fresh context — it will catch things you've become blind to after writing. For smaller
PRs (under ~200 lines of diff), inline review is fine.

Check:
- Does the brief match the code with no important omissions or contradictions?
- Is anything claimed in the brief that the diff doesn't support?
- Is anything important in the diff that the brief doesn't mention?
- Are scope boundaries accurate — does the code stay within the stated scope?
- Is the Scope section consistent with Follow-ups? (Nothing should appear in both.)
- Is the validation section actionable?

Resolve any gaps by updating the brief, or flag issues in the code or durable docs that the
review uncovered.

### Step 6: Suggest PR description

After the brief is written, suggest a short PR description. Example format:

```markdown
## Why this should be merged

<1-3 sentence summary from the brief's Overview section>

See `.review-briefs/<filename>` for the full review companion.
```

---

## Workflow 2: Review a PR using its review brief

Use this when the user asks to review a PR that has a review brief, or when you discover a
review brief exists for the PR under review.

### Step 1: Read the convention and the brief

1. Read `.review-briefs/README.md` to understand what review briefs are for.
2. Read the PR-specific review brief.
3. Research prior briefs as described in Workflow 1, Step 3 — understanding the historical
   context helps you evaluate whether the current brief and code are consistent with prior
   decisions and whether deferred work is being addressed correctly.

### Step 2: Review code against the brief

Read the relevant code and durable documentation. Evaluate:

- Does the code match the stated intent in the brief?
- Is the scope accurate — does the code do what the brief says and not what it says is out of scope?
- Are the invariants and assumptions described in the brief honored in the code?
- Is the validation approach described in the brief sufficient?
- Does durable documentation need updating based on the changes?
- Is the Scope section consistent with Follow-ups? (A common drift: something listed as a
  follow-up was actually implemented, or vice versa.)

Also check the brief and related files for mechanical issues: typos, broken links, stale
references, formatting problems. These are easy to miss in review and easy to fix.

### Step 3: Report findings

For each deficiency found, indicate where the fix belongs:
- **Code**: if the implementation is wrong or unclear
- **Durable documentation**: if stable maintenance/usage context is missing
- **Review brief**: if PR-specific review context is missing or misleading
- **PR description**: if the PR description is missing a link to the brief or is inconsistent

Structure your review as numbered findings, each with a clear "Where the fix belongs" label,
so the user can act on them directly without re-reading the analysis.

At the end, summarize: how many items need attention before merge, how many are minor/optional.

---

## Discovery

If a PR has a review brief, it should be linked from the PR description. When reviewing any PR,
check for `.review-briefs/` files in the diff. If one exists and the user hasn't mentioned it,
note its presence and offer to use it for the review.
