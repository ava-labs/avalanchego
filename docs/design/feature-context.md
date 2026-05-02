# Feature context

This note describes a lightweight way to capture durable feature context in this
repository.

The goal is to keep important feature knowledge out of any one person's head,
out of one agent session, and out of scattered PR discussion. The topology
example at `tests/fixture/tmpnet/topology.md` is the first use of this pattern.

## Definition

Feature context is the subset of information a future reader needs to correctly
understand, review, or evolve a feature, but which is not reliably recoverable
from code and tests alone.

It usually includes:

- what problem the feature solves
- why the feature is shaped this way today
- what constraints materially influenced the design
- what alternatives were considered and why they were not chosen
- how the feature is validated and why that validation strategy was chosen
- what assumptions, non-goals, or reevaluation triggers future maintainers
  should know

A detail is durable only if it remains useful after the current task or PR is
finished.

## How feature context differs from ordinary documentation

Ordinary durable documentation usually focuses on:

- what exists
- how it works
- how to use it
- what the current behavior is

Feature context adds the reasoning needed to think well about the feature:

- why it is this way
- what constraints matter
- what trade-offs were chosen
- what should be preserved versus revisited

In practice, one feature document may contain both ordinary documentation and
feature context. The distinction is by purpose, not necessarily by file.

## What feature context is not

Feature context is not a replacement for design docs, ADRs, RFCs, or RFDs when
a change needs pre-implementation alignment.

It does not remove the need for:

- stakeholder review
- requirement clarification
- architectural approval
- up-front comparison of competing approaches

Feature context is best understood as a durable record of the implemented
feature's current behavior, rationale, and important implementation learnings,
especially when those learnings emerge during execution.

## When both are needed

Some changes need both:

- a design-time document to align on the intended direction before
  implementation
- a feature-context document to preserve the durable reasoning behind the
  implemented result

Design documents are for deciding. Feature-context documents are for
remembering.

## When to create a feature-context document

A feature likely deserves its own context document when two or more of these are
true:

- the feature spans multiple code areas, tests, CI, or integrations
- the important review questions are architectural rather than syntactic
- important trade-offs are not obvious from code alone
- constraints materially shape the design
- future changes would be risky without additional context
- the feature is likely to evolve over time
- meaningful exploration produced durable conclusions worth preserving

The purpose is not to document every feature. The purpose is to document the
features where context loss would be expensive.

## What belongs in feature context

Include information that is:

- **non-obvious**
- **durable**
- **relevant to future reasoning**
- **not already clear from code and tests alone**

Typical examples:

- why a feature is modeled at one abstraction boundary rather than another
- why a runtime-specific implementation was chosen instead of a generic one
- why a validation strategy uses end-to-end testing instead of only unit tests
- why a permissions split, cleanup strategy, or lifecycle hook exists
- what alternatives remain plausible enough that future readers would naturally
  ask about them
- what assumptions would justify revisiting the design later

## What does not belong in feature context

Do not use a feature-context document as:

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
   - what the feature is and why it exists
2. **For users**
   - behavior, usage, limits, examples
3. **For maintainers**
   - design overview
   - current rationale
   - alternatives considered
   - constraints shaping the implementation
   - validation strategy
   - invariants / maintenance notes
   - revisit if assumptions change

This is a recommendation, not a rigid template. The important thing is to keep
user-facing behavior distinct from maintainer reasoning when both are present.

## Single document vs multiple documents

Either is acceptable.

Start with one document when:

- the feature is moderate in scope
- a single entrypoint improves discoverability
- user and maintainer content can coexist with clear sectioning

Split into multiple documents when:

- maintainer material overwhelms user-facing material
- the document becomes difficult to skim
- different parts evolve independently
- separate audiences consistently need separate entrypoints

The repository should optimize for clarity and discoverability, not a fixed file
count.

## How to decide whether a detail belongs

Ask:

- If this detail were lost, would a future maintainer be more likely to make a
  bad change?
- If a thoughtful reviewer asked "why is it like this?" would this detail help
  answer the question?
- Is this detail part of the feature's current reasoning model, rather than just
  part of the path taken to get there?

If the answer is yes, it may belong.

## Updating feature context during work

Feature-context documents should be updated as part of normal implementation,
not only after the fact.

Update feature context when work changes:

- externally meaningful behavior
- major constraints or assumptions
- rationale for important design choices
- validation strategy
- known non-goals or extension boundaries

Feature context is also a good fit for session finalization. If a session
materially improved understanding of a feature, it should leave behind any
future-facing conclusions that later work will depend on.

This is especially important when work is split across multiple agent sessions
or issue boundaries. The feature-context document acts as a stable memory
surface for the feature itself, rather than a per-task handoff note.

## Relationship to other artifacts

### READMEs
READMEs should orient the reader and point to deeper material. They may contain
some feature context, but they should not be forced to carry all of it.

### Feature-specific docs
These are the primary home for rich feature context when a capability deserves
it.

### ADRs / RFCs / RFDs
Use these when a broader or more formal decision record is needed. Feature
context documents are lighter-weight and more code-adjacent than a full design
process artifact.

### PR descriptions or review briefs
These should stay lightweight. They may point to durable feature-context docs,
but they should not be the long-term home for important rationale.

## Naming

`feature-context.md` is a reasonable name for a guidance document like this one.

For feature-specific documents, choose names based on discoverability near the
code, for example:

- `topology.md`
- `topology-design.md`
- `topology-maintainers.md`

The exact filename matters less than making the document easy to find and easy
to interpret.

## Rule of thumb

For significant features, maintain a code-adjacent document that records:

- current behavior
- current rationale
- durable implementation learnings
- validation strategy
- assumptions and triggers for reconsideration

If that information would otherwise live only in someone's head, in scattered PR
comments, or in agent session history, it likely belongs in feature context.
