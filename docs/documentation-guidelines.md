# Repository documentation guidelines

This guide explains what belongs in repository documentation and how to
structure it.

The goal is to keep important knowledge out of any one person's head, out of one
agent session, and out of scattered PR discussion.

These guidelines apply to documentation for any repository content whose correct
use, review, or evolution requires more context than code comments and tests
alone can provide.

## Motivation

Historically, repository documentation has skewed toward *what something is* and
*how to use it*. Maintenance rationale - the reasoning, constraints, and
trade-offs that should inform future changes - has either been missing or
scattered across PR threads, design docs, and people's heads. Modifying code
without that context carries the same kind of risk as modifying code without
tests: the change can look correct in isolation and still break something the
original author intended.

Maintenance docs were often treated as optional because keeping them current was
too expensive. They drifted, became misleading, and were ignored. That cost
equation has shifted. LLMs make it cheaper to spot drift between code and
documentation and to turn what was learned during implementation into
maintainable documentation. Documentation that was once hard to justify is now
practical to produce and keep current.

Making divergence easier to detect does not make repository documentation
infallible. When code, tests, and documentation diverge, treat that divergence
as a signal that the mismatch needs investigation. The purpose of keeping
repository documentation current is not to make it the sole source of truth, but
to make important disagreements visible so they can be resolved with human
judgment.

Think about repository documentation the way you think about tests: before you
change code, make sure the documentation needed to change it safely exists and
is current. Apply that proportionally. Mechanical or local changes usually do
not need a documentation pass. But if a safe change depends on non-obvious
assumptions, preserved invariants, design rationale, constraints, or validation
expectations that are not recoverable from code and tests alone, that context
should be documented before or as part of the change.


## Definition

Repository documentation is information a future reader needs to correctly
understand, review, use, or evolve something in the repository, but cannot
reliably recover from code and tests alone.

Repository documentation may live in comments and other inline material, or in
dedicated documents such as READMEs or feature docs. Inline documentation is
often the better fit for local, point-of-use details that are easiest to
understand where they appear. A dedicated document is preferable when readers
would benefit from having the relevant context collected in one place instead of
having to assemble it from code, tests, scripts, and CI configuration.

A detail belongs in repository documentation when it will still be useful
after the current task or PR is finished.

## A four-layer framework

A useful default is to think about repository documentation in four layers:

1. **Why does this exist?**
   - what problem does it solve
   - why it matters
2. **How do I use it?**
   - who the document is for
   - behavior, usage, and limits
   - examples, entrypoints, and common paths
3. **How does it work?**
   - the right mental model
   - the important concepts, boundaries, states, or lifecycle
   - what code alone may fail to make clear
4. **How do I change it safely?**
   - why it is implemented this way
   - what constraints and trade-offs shaped it
   - what alternatives were considered and why they were not chosen
   - what invariants matter
   - how changes should be validated
   - assumptions that would justify revisiting the design
   - open questions worth revisiting later

Not every document needs all four layers equally. The point is to ask questions
like these where relevant, and ensure that their answers are easy to find when
needed.

These four layers come from the writer's point of view, but they match the
questions different readers are likely to bring: *what is this*, *how do I use
it*, *how does it work*, *how do I change it safely*. The first two mostly help
readers understand or use something; the latter two mostly help them review,
maintain, or evolve it.

Layer 4 - *how do I change it safely?* - is the part this guide most wants to
emphasize. Historically, layers 1 and 2 have been documented inconsistently,
layer 3 occasionally, and layer 4 rarely. It is also the layer where LLMs can be
the most helpful because an LLM working with you already has much of the context
needed to write it.

Layer 4 should not be interpreted narrowly as only "validation steps" or
"current invariants." It also includes enough preserved decision context that a
future maintainer can understand why a different change may be wrong, or why it
might only become appropriate under changed assumptions. Repository
documentation should usually preserve enough context around meaningful
alternatives that a future reader can understand why the current choice was
made.

### What this looks like in practice

For example, see
[`.github/packaging/README.md`](../.github/packaging/README.md). It maps to
the four layers:

- **Why it exists** - the overview indicates that the directory exists to ship
  signed RPMs for RHEL 9.x-compatible systems.

- **How to use it** - the usage sections identify the produced artifacts, the
  main entrypoints, local build and validation commands, and CI behavior. A
  reader who only needs to build or validate RPMs can stop there.

- **How does it work?** - the conceptual model section explains that the RPM
  packaging code is kept separate from the source tree being packaged rather
  than treated as something that must already exist in each historical release
  tag. It also explains the architecture-name mapping between RPM conventions
  and Docker/Go conventions, which is not obvious from the scripts alone.

- **How to change it safely** - the maintainer guidance explains the current
  build environment, how manual tag builds are assembled, why RPM signing is
  always exercised, why the current linking approach was chosen, what
  alternatives were considered, which invariants matter, and what assumptions
  would justify revisiting the design.

The document does not try to cover everything. It records the parts that would
be hard to reconstruct from the code and CI configuration alone, and leaves the
rest to the implementation.

## Scope

Repository documentation is the in-repo record that helps future readers
understand what has been built and the context that still matters after
implementation.

It is not about transient artifacts like PR descriptions, review threads, or
session notes, except to say that important information should not live only
there.

It is also not a replacement for design docs or architecture decision records
(ADRs) when a change needs alignment before implementation. Stakeholder review,
requirement clarification, architectural approval, and up-front comparison of
competing approaches are still required.

Some changes need both: a design-time document to align on the intended
direction before implementation, and repository documentation near the code to
preserve the reasoning behind the implemented result. Design documents are for
deciding. Repository documentation is for learning, using, remembering, and
evolving the implemented system.

## Non-goals

This guide deliberately does not prescribe:

- **how long or detailed documents should be.** How long a document should be
  follows from the scope and criticality of what is being documented, not from a
  separate heuristic. The answer depends on how much non-obvious and durable
  context is needed to answer the four questions above.
- **a fixed authoring process.** LLMs are what make this level of maintenance
  documentation practical, but this guide does not prescribe a single way to
  produce it. The criteria for what belongs apply regardless of who writes it.
  The details of drafting, checking for drift, and using session history can
  evolve over time.

## When to add or expand documentation

The definition above explains what repository documentation is. This section
addresses a narrower question: when an area deserves a dedicated document rather
than relying only on inline or code-adjacent material. The later "What belongs
in repository documentation" section then helps decide what details belong in
that documentation.

Something likely deserves dedicated documentation when two or more of the
following are true:

- it spans multiple code areas, tests, CI, or integrations
- important review questions are about design rather than code style or
  low-level details
- important trade-offs are not obvious from code alone
- important constraints shape the design
- future changes would be risky without additional context
- the area is likely to evolve over time
- the work produced conclusions that may inform future changes
- users and maintainers need different kinds of guidance
- the same explanations are likely to be repeated in PRs or handoff notes

The goal is not to document everything equally, but to document the places where
insufficient context would make future changes harder or less safe.

## What belongs in repository documentation

Include information that is:

- **non-obvious**
- **durable**
- **relevant to future use or reasoning**
- **not already clear from code and tests alone**

To apply that criterion to a specific detail, ask:

- If this were lost, would a future maintainer or reviewer be more likely to
  make an incorrect change?
- If a thoughtful reader asked "why is it like this?", would this detail help
  answer the question?
- Does this help someone use, validate, or evolve the code correctly?
- Is this part of the current reasoning model, rather than just part of the path
  taken to get here?

If the answer is yes, it likely belongs. Typical examples:

- why an abstraction boundary exists where it does
- why a runtime-specific implementation was chosen instead of a generic one
- why a validation strategy uses end-to-end testing instead of only unit tests
- why a permissions split, cleanup strategy, or lifecycle hook exists
- what assumptions justify the current design
- what alternatives were considered, which ones might be worth revisiting, and
  what would justify changing the design
- where a new reader should start depending on whether they are a user,
  reviewer, or maintainer

## What does not belong

Do not use repository documentation as:

- a duplicate of code-level details already obvious from the implementation
- a session transcript
- a PR summary
- a debugging log
- an implementation diary
- a record of every explored dead end

Some explored paths are worth preserving, but only after they are translated
into a stable explanation such as a trade-off, rejected alternative, or
important discovered constraint.

When deciding whether to keep rationale from an explored path, ask whether a
future maintainer would understand the trade-off, rejected alternative, or
constraint it revealed. If not, that explanation should remain in the docs.

Preserve durable reasoning, not step-by-step history.

## Location and naming

Place documentation as close to the thing it describes as possible:

- docs about code in one package, feature, or component should usually live next
  to that code
- cross-cutting documentation that spans multiple parts of the repository should
  live under `docs/`

Keeping documentation close to the code makes it easier to find while reading
the code, and easier to keep current as the code evolves.

Choose names that make the document easy to find and understand. Common examples
include:

- `README.md` for the local index
- `<feature>.md` for a single combined feature document
- `<feature>-architecture.md`, `<feature>-concepts.md`, or
  `<feature>-maintainers.md` when user and maintainer material are split
- `validation.md` or similar for focused topics within that part of the
  repository

The exact filename matters less than making the document easy to find and
interpret.

## Discoverability

Documentation still needs to be discoverable. In this repository, that means
readers should be able to navigate to it by following links. The convention is:

- the **nearest README is the index** for documentation in that area
- every dedicated document should be reachable by following links from that
  README (directly or transitively)
- if a document is not linked from any README, it is effectively orphaned

In practice, when adding a new document, also add a link to it from the
appropriate README. For cross-cutting documentation under `docs/`,
`docs/README.md` is the index for that subtree. Every top-level document in
`docs/` should be linked from `docs/README.md`, and subdirectories under `docs/`
that contain multiple documents should usually have their own `README.md`
linking the documents in that area. Particularly important cross-cutting
documents may also be linked from the repository root `README.md`, but
`docs/README.md` is the minimum required index.

When moving or renaming a document, update inbound links. Treat broken or
missing inbound links as maintenance issues that should be fixed when found.

When exploring or modifying an unfamiliar area of the repository, readers and
agents should start with the nearest README, then follow its links to the more
specific documentation and code entrypoints for that area.

READMEs themselves may carry some durable context, but they should not be forced
to carry all of it. Their primary job is to help readers find what they are
looking for.

## Recommended structure

Start simple. A single document is often enough.

One useful default is to structure a document around the [four-layer
framework](#a-four-layer-framework) above, often with sections such as:

1. **Overview**
2. **Usage**
3. **Conceptual model**
4. **Maintainer guidance**

Use the earlier framework for the detailed questions each section may want to
answer. This is a recommendation, not a rigid template. The important thing is
to make it easy for a reader to find the right level of detail.

A trailing **References** section pointing to the relevant code, tests, and
related docs is often useful. It makes the links between the document and the
code explicit, supports the discoverability conventions above, and helps
reviewers see what parts of the repository the document covers.

A README near the code can do more than explain usage. It can also preserve
non-obvious context a future maintainer would otherwise need to reconstruct
from design docs, PRs, or oral history.

In some cases, it is clearer to organize the document by audience, for
example:

- **For users or consumers**
  - why it exists, how to use it
- **For maintainers**
  - how it works, why it is designed this way

Either approach is fine. The important part is that repository documentation
should help readers understand, use, and safely evolve what it describes.

## Single document or multiple

A single document is often enough. Start with one when it stays easy to
navigate. Split into multiple documents when one document becomes hard to use,
when different audiences need different entrypoints, or when different parts
evolve separately. Optimize for clarity and discoverability, not a fixed file
count.

## Updating documentation during work

Repository documentation should be updated as part of the work, not only
after the fact.

Update it when work changes:

- externally meaningful behavior
- major constraints or assumptions
- rationale for important design choices
- validation strategy
- known non-goals or extension boundaries
- the best place for future readers to start

This is especially important when work is split across multiple agent sessions
or issue boundaries. The document becomes a shared record for the code or
workflow itself, rather than a per-task handoff note.

Apply that proportionally. Purely mechanical changes may not need documentation
updates. If a change relies on or changes durable context that future
maintainers will need to review or evolve what is being changed safely, updating
the repository documentation should be part of that change.

When modifying existing documentation, don't throw away context future
maintainers will still need. The updated document does not need every detail
from the original, but it should preserve the reasoning that still matters for
future changes, including important constraints, relevant alternatives, and why
those alternatives were not chosen.
