---
name: review-brief
description: Use when the user wants to create, update, or reconcile a PR review brief in `.review-briefs/` for the current change.
---

# Review Brief

Use this skill when the user wants to create, update, or review a PR review brief in `.review-briefs/`.

## Goal

Produce or refine a PR-scoped review brief that helps reviewers understand the current change without inventing rationale that is not supported by the diff, the code, or durable documentation.

## Required first step

Always read `.review-briefs/README.md` completely before writing or editing a review brief.

Treat paths like `.review-briefs/...` as relative to the current working repository, not the skill source location.

Treat that README as the repository convention for:
- what a review brief is for
- what belongs in it
- naming guidance
- suggested structure
- agent-assisted workflow expectations

Do not skip this read, even if you have worked with review briefs before.

## When to use

Use this skill when the user asks to:
- create a review brief
- draft a first-pass review brief from the current diff
- update an existing review brief to match the latest code
- reconcile a review brief with the code and durable docs
- review whether a review brief is complete, accurate, and aligned with the PR

Do not use this skill for:
- durable project documentation that belongs in `docs/` or package `README.md`
- PR description editing, unless the user explicitly asks for that too
- inventing motivation, validation, or follow-ups that are not supported by the repo state or user instructions

## Validation criteria

A successful review-brief task means:
1. `.review-briefs/README.md` was read first.
2. The brief lives in `.review-briefs/` and uses a stable descriptive filename.
3. The brief matches the current change scope.
4. The brief includes reviewer-useful context such as overview, scope, validation, and review focus when those sections materially help.
5. Claims in the brief are supported by the diff, code, durable docs, or explicit user-provided context.
6. The brief has been checked back against the changed files and corrected for drift.

## Workflow

### 1. Read the convention

Read `.review-briefs/README.md` in full from the current working repository.

If the current working repository does not contain `.review-briefs/README.md`, stop and explain that this repo does not provide the review-brief convention needed by this skill.

### 2. Identify the target brief path

If the user gave a specific review-brief filename or path, use it.

Otherwise:
- inspect `.review-briefs/` for an obvious existing PR-specific brief to update
- if creating a new brief, choose a stable descriptive filename that follows the README guidance
- avoid using transient branch-only names unless they would still make sense after merge

If there is not enough context to pick a sensible filename, make a reasonable choice instead of blocking on a question, unless the user explicitly asked to choose the name themselves.

### 3. Inspect the current change before writing

Understand the actual review scope before drafting.

Useful commands:
- `git status --short`
- `git diff --stat`
- `git diff --cached --stat`
- `git diff`
- `git diff --cached`

If the worktree is clean, inspect committed branch work instead. Prefer a diff against the branch base when available. If there is no clear base available, fall back to inspecting the relevant recent commit(s) and say so in your reasoning.

Also read the key changed files and any relevant durable docs needed to explain the change correctly.

### 4. Draft the brief from evidence

Write the review brief in `.review-briefs/`.

Use the README's suggested structure as a guide, not a checklist. Include only sections that materially help review. Common useful sections are:
- `## Overview`
- `## Why now`
- `## What reviewers should know`
- `## Scope`
- `## Approach`
- `## Validation`
- `## Review focus`
- `## Follow-ups`

Guidelines:
- write for reviewers, not as internal scratch notes
- prefer concise, high-signal prose over exhaustive change narration
- explain the diff's intent, scope boundaries, invariants, and validation
- explicitly call out non-goals when they matter for review
- if validation was not run, say that plainly rather than implying it was
- if something is uncertain, either verify it or label the uncertainty clearly

### 5. Reconcile the brief with the code

After drafting, re-read:
- the brief you wrote or updated
- the key changed files
- any relevant durable docs

Then tighten the brief so it is consistent with the actual implementation.

Look specifically for:
- unsupported rationale
- stale scope statements
- validation claims not backed by what you observed
- missing reviewer focus areas
- details that belong in durable docs instead of the brief

Update the brief until it accurately reflects the current change.

### 6. Report completion clearly

When done, report:
- the review-brief path
- whether you created or updated it
- any notable assumptions or gaps you left explicit in the brief

## Default behavior

When the user says something like "let's create a review brief":
1. read `.review-briefs/README.md`
2. inspect the current diff and changed files
3. choose a stable filename in `.review-briefs/`
4. draft the brief
5. reconcile it against the code
6. summarize the result

If there is no meaningful diff or review scope to describe, do not fabricate a brief. Explain what is missing and what you need to proceed.

Do not read or write `.review-briefs/...` relative to the skill source checkout unless the user explicitly asks you to work there.
