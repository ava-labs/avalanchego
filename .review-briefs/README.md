# PR review briefs

This directory holds **PR review briefs**: lightweight, PR-scoped documents in
`.review-briefs/` that give reviewers a holistic overview of what a PR is trying to do and why.

They are the versioned, in-tree long-form review companion for a PR.

## At a glance

A PR review brief is the versioned, in-tree long-form review companion for a PR.

Use one when the diff and PR description alone are not enough for a reviewer to quickly form the
right mental model.

A good review brief should:
- explain what the PR is doing and why
- make scope and non-goals explicit
- capture the key concepts, assumptions, or invariants needed for review
- say how the PR should be validated
- point reviewers at the parts of the diff that deserve the most attention

If a PR has a review brief, link it from the PR description.

Think of these as:
- the evolving review companion for a PR
- a versioned long-form review overview that can change with the code
- more focused than long-lived design docs in `docs/`
- useful to the author, reviewers, and agent-assisted review

They may be authored and maintained by humans, agents, or both.

## Relation to the PR description

The PR description is usually the entry point for review.

The review brief is the preferred versioned long-form review companion when a PR benefits from one.

The PR description can stay relatively static and high-level, for example:
- a short summary of the PR
- links to issues, related PRs, or external context
- any GitHub-specific review metadata

Once pointed to it, reviewers should be able to rely on the review brief for the holistic
explanation of the PR. The code should still be readable on its own.

For readers familiar with Gerrit's commit-message-file workflow, a PR review brief serves a
similar purpose: it provides an in-tree, versioned place for reviewer-facing context that evolves
with the change. The difference is that a review brief is intentionally longer-form and can cover
scope, non-goals, validation, and review focus, not just the change summary.

## Reviewer expectations

Authors should aim to keep the review brief aligned with the current state of the PR.

Reviewers should be able to read the brief first and then use it to evaluate whether the diff
matches the stated intent.

Reviewers should be able to use the brief to answer:
- what is this PR trying to do and why?
- what is in scope, and what is intentionally not?
- what assumptions or invariants matter when reading the diff?
- how was this validated?
- where should review attention go?

The brief should match the code with no important omissions or contradictions. If part of the brief
has become stable project guidance, consider promoting it into durable docs.

## Why this exists

Some context is important during implementation and review, but doesn't belong in code comments or
commit messages:
- what problem the PR is solving
- what high-level ideas or invariants matter
- what is intentionally out of scope
- what reviewers should pay special attention to
- how the change should be validated

A PR review brief gives that context a place in the tree where it can evolve alongside the code.

This also avoids relying on the PR description as the main home for evolving review context.
PR descriptions are useful entry points, but they are not versioned and reviewed like files in the
tree, so review context drift is easier to miss.

## Default convention

Commit review briefs in `.review-briefs/` as part of the same PR as the code they describe.

That keeps the review overview versioned, reviewable, and editable as the implementation changes,
without adding special pre-merge or post-merge workflows.

This directory may accumulate stale docs over time. That's okay. Periodic cleanup is cheaper than
requiring everyone to manage a separate lifecycle for every PR.

## How this differs from a design doc

A review brief may contain some design-doc-like material, but it serves a different purpose.

Compared with a design doc, a PR review brief is:
- more review-oriented than design-oriented
- usually narrower in scope and tied to a single PR
- allowed to be written before, during, or after implementation starts
- more like an evolved PR description than a standalone architecture document
- not expected to be durable project documentation by default

If content becomes stable, reusable guidance or durable architectural context, promote it into
`docs/`, this directory's `README.md`, or a package `README.md`.

## Agent-assisted workflow

A PR review brief can be agent-maintained.

A useful default workflow is:
1. Ask an agent to generate a review brief that captures the important high-level details, scope,
   invariants, validation, and review focus of the PR.
2. In another session or subagent, review the brief against the code and PR description for
   consistency and completeness.
3. Resolve any gaps or inconsistencies by updating the brief, the code, or both.
4. Repeat as the PR evolves.

The goal is not to have the agent invent rationale after the fact. The goal is to keep the brief
aligned with the code as the PR evolves.

## What belongs here

Create a PR review brief when a reviewer would benefit from a more explicit review companion,
especially when:
- the intent is not obvious from the code alone
- there are meaningful tradeoffs or rejected alternatives
- the work spans multiple commits or PRs
- you want a checklist for self-review or agent-assisted review
- you want reviewers to evaluate the change against explicit scope and validation criteria

Good review-brief content is specific to the PR under review, such as:
- the problem this PR is solving
- the key concepts, invariants, or constraints a reviewer should keep in mind
- the scope and non-goals of this PR
- the high-level shape of the approach
- how the PR should be validated
- where reviewers should focus attention

## What does not belong here

Do not use this directory for:
- agent configuration or prompts (`.agents/` is for that)
- durable user/developer documentation that should remain after the PR is merged (`docs/`,
  package `README.md`, etc.)
- stable usage guidance for a mechanism or convention once it stops being PR-specific
- changelog-style narration of everything that changed in the diff
- details that should instead be made clear in the code, commit structure, or durable docs
- issue tracking or task management already captured elsewhere
- temporary scratch notes that are not useful for review

## Lifecycle

### During PR development

Use the review brief to:
1. capture the reviewer-facing overview of the PR
2. record important scope boundaries, concepts, and invariants
3. document how the PR should be validated
4. guide self-review and peer review
5. update the brief as understanding evolves
6. periodically review it against the code for consistency and completeness

A review brief may be written early, but it can also be synthesized later from an implemented PR
if that is the best way to prepare for review.

### Before merge

Trim obviously stale sections so the brief reflects the final reviewable shape of the PR, not
every abandoned thought.

### After merge

Use judgment:
- If the brief still has value as historical context for the PR, leave it here.
- If some parts describe stable convention or durable behavior, promote those parts into `docs/`,
  this directory's `README.md`, or a package `README.md`.
- If it no longer adds value, it can be removed in a later cleanup sweep.

The default is **no special cleanup step required**.

## Naming

Use one file in `.review-briefs/` per PR under review.

Preferred names:
- `<issue-id>-short-title.md`
- `<short-title>.md`

Examples:
- `3333-firewood-commit-path.md`
- `subnet-bootstrap-validation.md`

Choose names that will still make sense after the branch is gone and the PR has merged.

## Suggested structure

A review brief should read more like an evolved PR description than a reduced design doc.
Not every section is required; include only what materially helps review.

```md
# <title>

## Overview
A concise, holistic explanation of what the PR is doing and why.

## Why now
Why this PR exists now. What problem or pressure is it addressing?

## What reviewers should know
The key concepts, invariants, assumptions, or mental model needed to read the diff well.

## Scope
What this PR is doing, and what it is intentionally not doing.

## Approach
The high-level shape of the implementation, only as much as needed to orient review.

## Validation
How this was or should be validated.

## Review focus
Where reviewers should spend attention.

## Follow-ups
What is left for later.
```

Optional sections when useful:
- `## Alternatives considered`
- `## References`
- `## Risks`

See `.review-briefs/introduce-review-briefs.md` for a worked example.

