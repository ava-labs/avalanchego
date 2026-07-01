# Introduce PR review briefs

## Overview

This PR introduces `.review-briefs/` as a place for a versioned, in-tree long-form review
companion that would otherwise be reconstructed from the PR description, review comments, and the
diff.

The goal is to make that reviewer-facing overview explicit enough to guide self-review, peer
review, and agent-assisted review.

## Why now

Important PR-level context is currently scattered across:
- the PR description
- review comments
- commit messages
- the code itself

That makes it harder to review a PR against a clear statement of:
- what problem the PR is solving
- which high-level ideas and constraints matter
- what is intentionally out of scope
- how the reviewer should evaluate success

This PR introduces the review brief as a versioned long-form review companion, so reviewers do not
need to reconstruct that model from the PR description and diff alone.

A secondary motivation is to avoid relying on the PR description as the main home for evolving
review context. PR descriptions are useful entry points, but they are not versioned and reviewed
like files in the tree, so changes in emphasis or intent are easier to miss.

## What reviewers should know

This PR is intentionally defining review briefs as something closer to an evolved PR description
than a reduced design doc.

The intended mental model is:
- the PR description is the entry point
- the review brief is the versioned long-form review companion when a PR benefits from one
- the brief should help reviewers form the right high-level model without reconstructing it from
  the diff alone

That means the brief is:
- primarily for reviewers
- allowed to be written before, during, or after implementation
- PR-scoped rather than durable by default
- meant to guide review rather than narrate the diff
- meant to supplement, not replace, a concise PR description

## Scope

This PR:
- adds a top-level `.review-briefs/` directory
- adds `.review-briefs/README.md` defining the convention
- adds an early "at a glance" framing so readers quickly see what a review brief is and when to
  use one
- positions the review brief as the preferred versioned long-form review companion for a PR
- defines an agent-assisted workflow for generating and maintaining the brief
- adds guidance to link the brief from the PR description for discovery
- adds reviewer-expectations guidance near the top of the README
- adds a brief Gerrit commit-message-file analogy under the PR-description relationship
- changes the suggested format to a reviewer-oriented, evolved-PR-description structure

This PR does not:
- require a review brief for every trivial or mechanical PR
- add tooling, automation, or CI enforcement
- make review briefs durable project documentation by default
- replace concise PR descriptions as the normal entry point for review

## Approach

Keep the convention lightweight:
- commit review briefs in `.review-briefs/` as part of the same PR as the code they describe
- use the PR description to point reviewers to the brief
- let the README carry the stable usage guidance
- allow briefs to be created up front or synthesized later for review
- tolerate some stale briefs instead of requiring merge-time cleanup

## Validation

Success for this PR is:
- the repository contains `.review-briefs/README.md`
- the README clearly explains what review briefs are for
- the README gives readers an early elevator pitch for what a review brief is and when to use one
- the README distinguishes review briefs from design docs and durable documentation
- the README is explicit that review briefs are for PR review
- the README explains how reviewers discover a brief during review
- the README makes reviewer expectations easy to find near the top
- the README explicitly captures agent-assisted generation and maintenance
- the suggested format reads like an evolved PR description that guides review

## Review focus

Focus review on:
- whether `.review-briefs/` is the right scope and level of explicitness
- whether the README now gives readers the right elevator pitch early enough
- whether the README clearly separates review briefs from design docs and durable docs
- whether the PR-description framing is concrete enough to be useful in practice
- whether the Gerrit analogy is clarifying without becoming the main framing
- whether the discoverability guidance is enough to make briefs useful
- whether the agent-assisted workflow is concrete enough to use
- whether the suggested format matches the reviewer-oriented role of the document
- whether the convention feels lightweight and selective enough to adopt in practice

## Follow-ups

Possible future follow-ups, intentionally out of scope for this PR:
- add a `TEMPLATE.md`
- mention the convention in `CONTRIBUTING.md`
- add more example briefs after the pattern has been used on more PRs
