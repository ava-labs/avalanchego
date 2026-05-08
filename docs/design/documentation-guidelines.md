# Repository documentation guidelines

This note describes a lightweight way to think about documentation in this
repository.

The goal is to keep important knowledge out of any one person's head, out of
one agent session, and out of scattered PR discussion. The topology example at
[`tests/fixture/tmpnet/topology.md`](../../tests/fixture/tmpnet/topology.md)
is one use of this pattern. Read it alongside
[`tests/fixture/tmpnet/README.md`](../../tests/fixture/tmpnet/README.md) for a
concrete example of how package documentation and maintainer-oriented context
can complement each other.

This document is intentionally broader than "feature context." The same ideas
apply to features, packages, test fixtures, integrations, workflows, and other
parts of the repository that need more than code comments and tests to be
understood well.

## Definition

Repository documentation is the subset of information a future reader needs to
correctly understand, review, use, or evolve something in the repository, but
which is not reliably recoverable from code and tests alone.

It usually includes:

- what exists and what problem it solves
- how it is meant to be used or reasoned about
- why it is shaped this way today
- what constraints materially influenced the design
- what alternatives were considered and why they were not chosen
- how it is validated and why that validation strategy was chosen
- what assumptions, non-goals, invariants, or reevaluation triggers future
  maintainers should know

A detail belongs in repository documentation when it remains useful after the
current task or PR is finished.

## A four-layer framework

A useful default is to think about repository documentation in four layers:

1. **Why does this exist?**
   - what problem does it solve
   - why it matters
2. **How do I use it?**
   - what it does
   - how to interact with it
   - examples, limits, and common paths
3. **How should I think about it?**
   - the right mental model
   - the important concepts, boundaries, states, or lifecycle
   - what a smart reader might misunderstand from code alone
4. **How do I change it safely?**
   - why it is implemented this way
   - what constraints and trade-offs shaped it
   - what invariants matter
   - how changes should be validated
   - what assumptions would justify revisiting the design

Not every document needs all four layers equally. The point is to make sure
these questions are answered somewhere discoverable when they matter.

## What good repository documentation should do

Good documentation should help a future reader answer some combination of:

- what is this
- where should I start
- how do I use it
- how should I think about it
- how does it work
- why is it this way
- what constraints or trade-offs matter
- what should be preserved versus revisited

In practice, the first two questions often serve readers trying to understand
or use something, while the latter questions become more important for readers
trying to review, maintain, or evolve it.

## What this is not

This guidance is not about transient artifacts like PR descriptions, review
threads, or session notes, except to say that important information should not
live only there.

It is also not a replacement for design docs, ADRs, RFCs, or RFDs when a
change needs pre-implementation alignment.

It does not remove the need for:

- stakeholder review
- requirement clarification
- architectural approval
- up-front comparison of competing approaches

Repository documentation is best understood as the in-repo record that helps
future readers understand the implemented system and the reasoning that still
matters after implementation.

## When both are needed

Some changes need both:

- a design-time document to align on the intended direction before
  implementation
- repository documentation near the code to preserve the reasoning behind the
  implemented result

Design documents are for deciding. Repository documentation is for
remembering, using, and evolving.

## When to add or expand documentation

Something likely deserves dedicated documentation when two or more of these are
true:

- it spans multiple code areas, tests, CI, or integrations
- important review questions are architectural rather than syntactic
- important trade-offs are not obvious from code alone
- constraints materially shape the design
- future changes would be risky without additional context
- the area is likely to evolve over time
- meaningful exploration produced durable conclusions worth preserving
- users and maintainers need different kinds of guidance
- the same explanations are likely to be repeated in PRs or handoff notes

The purpose is not to document everything equally. The purpose is to document
the places where context loss would be expensive.

## What belongs in repository documentation

Include information that is:

- **non-obvious**
- **durable**
- **relevant to future use or reasoning**
- **not already clear from code and tests alone**

Typical examples:

- why an abstraction boundary exists where it does
- why a runtime-specific implementation was chosen instead of a generic one
- why a validation strategy uses end-to-end testing instead of only unit tests
- why a permissions split, cleanup strategy, or lifecycle hook exists
- what assumptions justify the current design
- what alternatives remain plausible enough that future readers would naturally
  ask about them
- what conditions would justify revisiting the design later
- what entrypoints a new reader should use depending on whether they are a user,
  reviewer, or maintainer

## What does not belong

Do not use repository documentation as:

- a session transcript
- a PR summary
- a debugging log
- an implementation diary
- a dumping ground for every explored dead end
- a duplicate of code-level details already obvious from the implementation

Some explored paths are worth preserving, but only after they are translated
into a stable explanation such as a trade-off, rejected alternative, or
important discovered constraint.

Preserve durable reasoning, not process exhaust.

## Recommended structure

Start simple. A single document is often enough.

A useful shape is:

1. **Overview**
   - what this thing is
   - why it exists
   - who the document is for
2. **Usage**
   - behavior, usage, limits, examples, entrypoints
3. **Conceptual model**
   - the right way to think about the thing
   - key concepts, boundaries, states, or lifecycle
4. **Maintenance notes**
   - design overview
   - current rationale
   - alternatives considered
   - constraints shaping the implementation
   - validation strategy
   - invariants / maintenance notes
   - revisit if assumptions change

This is a recommendation, not a rigid template. The important thing is to make
it easy for the next reader to find the right level of detail.

In some cases, these map naturally to audience-oriented sections such as:

- **For users or consumers**
  - why it exists, how to use it, examples, limits
- **For maintainers**
  - conceptual model, rationale, constraints, validation, invariants

Either framing is fine. The important part is that repository documentation
should help readers understand, use, and safely evolve what it describes.

## Single document vs multiple documents

Either is acceptable.

Start with one document when:

- the scope is moderate
- a single entrypoint improves discoverability
- user and maintainer content can coexist with clear sectioning

Split into multiple documents when:

- maintainer material overwhelms user-facing material
- the document becomes difficult to skim
- different parts evolve independently
- separate audiences consistently need separate entrypoints

The repository should optimize for clarity and discoverability, not a fixed
file count.

## How to decide whether a detail belongs

Ask:

- If this detail were lost, would a future maintainer or reviewer be more
  likely to make a bad change?
- If a thoughtful reader asked "why is it like this?" would this detail help
  answer the question?
- Does this help someone use, validate, or evolve the code correctly?
- Is this part of the current reasoning model, rather than just part of the
  path taken to get here?

If the answer is yes, it may belong.

## Updating documentation during work

Repository documentation should be updated as part of normal implementation,
not only after the fact.

Update it when work changes:

- externally meaningful behavior
- major constraints or assumptions
- rationale for important design choices
- validation strategy
- known non-goals or extension boundaries
- the best entrypoint for future readers

This is especially important when work is split across multiple agent sessions
or issue boundaries. The document becomes a stable memory surface for the code
or workflow itself, rather than a per-task handoff note.

## Relationship to other artifacts

### READMEs
READMEs should orient the reader and point to deeper material. They may contain
some durable context, but they should not be forced to carry all of it.

### Feature- or area-specific docs
These are the primary home for richer documentation when a capability, package,
or workflow deserves it.

### ADRs / RFCs / RFDs
Use these when a broader or more formal decision record is needed. Repository
documentation is lighter-weight and more code-adjacent than a full design
process artifact.

### PR descriptions or review briefs
These should stay lightweight. They may point to repository docs, but they
should not be the long-term home for important rationale.

## Naming

This document is broader than a single document category like "feature
context." It is meant to guide how we write repository documentation more
generally.

For area-specific documents, choose names based on discoverability near the
code, for example:

- `README.md`
- `topology.md`
- `topology-design.md`
- `topology-maintainers.md`
- `validation.md`

The exact filename matters less than making the document easy to find and easy
to interpret.

## Rule of thumb

For significant parts of the repository, maintain code-adjacent documentation
that helps future readers:

- understand why something exists
- use it correctly
- form the right mental model
- change it safely

In practice, that often means recording:

- current behavior
- intended usage
- key concepts and boundaries
- current rationale
- implementation constraints and trade-offs
- validation strategy
- assumptions and triggers for reconsideration

If that information would otherwise live only in someone's head, in scattered PR
comments, or in agent session history, it probably belongs in repository
documentation.
