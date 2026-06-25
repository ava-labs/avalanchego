xt, read all review comments from PR fifty three seventy-three. I want to make sure they're all addressed in preparation for getting this mergeable.
# Repository documentation guidelines

This note describes how to think about documentation in this repository.

The goal is to keep important knowledge out of any one person's head, out of
one agent session, and out of scattered PR discussion.

These guidelines apply to documentation for any repository content whose
correct use, review, or evolution requires more context than code comments and
tests alone can provide.

## Motivation

Historically, repository documentation has skewed toward *what something is*
and *how to use it*. Maintenance rationale - the reasoning, constraints, and
trade-offs that should inform future changes - has either been missing or
scattered across PR threads, design docs, and people's heads. Modifying code
without that context carries the same kind of risk as modifying code without
tests: the change can look correct in isolation and still break something the
original author had reason to preserve.

Maintenance docs have been tolerated as optional because keeping them current
was prohibitively expensive. They drifted, became misleading, and were
rationally ignored. That cost equation has shifted. LLM-based tooling makes it
cheap to detect divergence between code and documentation, and makes it
practical to extract maintenance content from the reasoning trail of an
implementation session and potentially include it in the documentation for
what was implemented. Maintenance documentation that was previously hard to
justify is now practical to produce and keep current.

Making divergence easier to detect does not make repository documentation
infallible. When code, tests, and documentation diverge, treat that divergence
as a signal that the area needs investigation. The purpose of keeping
repository documentation current is not to make it the sole source of truth,
but to make important disagreements visible so they can be resolved with human
judgment.

The expectation that follows is parallel to tests: code should be adequately
documented before it is modified for the same reasons it should be adequately
tested before it is modified. Documentation of the maintenance layer is no
longer optional infrastructure. It is a precondition for safe change.

That expectation should be applied proportionally. It does not mean every
mechanical edit, obvious rename, formatting-only change, or tightly local fix
requires a documentation pass first. It does mean that when a safe change
depends on non-obvious assumptions, preserved invariants, design rationale,
constraints, or validation expectations that are not recoverable from code and
tests alone, that context should be documented before or as part of the change.
When work introduces a new area, or touches an existing area that lacks
adequate repository documentation, add repository documentation once the
surface area, complexity, or maintenance risk is high enough that future
readers would otherwise need to reconstruct important reasoning from PRs,
issue threads, or oral history.

## Definition

Repository documentation is the subset of information a future reader needs to
correctly understand, review, use, or evolve something in the repository, but
which is not reliably recoverable from code and tests alone.

Repository documentation may be embedded in code-adjacent material such as
READMEs or comments, or collected in a dedicated document. A dedicated
repository document is preferable when the context spans multiple files, tests,
workflows, or audiences, or when readers need a discoverable entrypoint that
is not tied to one code location.

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
   - who the document is for
   - examples, limits, entrypoints, and common paths
3. **How does it work?**
   - the right mental model
   - the important concepts, boundaries, states, or lifecycle
   - what code alone may fail to make clear
4. **How do I change it safely?**
   - why it is implemented this way
   - what constraints and trade-offs shaped it
   - what alternatives were considered
   - why those alternatives were not chosen
   - what invariants matter
   - how changes should be validated
   - what assumptions would justify revisiting the design
   - what open questions are worth revisiting later

Not every document needs all four layers equally. The point is to make sure
these questions are answered somewhere discoverable when they matter.

These four layers are author-facing, but they correspond directly to the
questions a reader arrives with: *what is this*, *how do I use it*, *how
should I think about it*, *how do I change it safely*. The first two
typically serve readers trying to understand or use something; the latter
two serve readers trying to review, maintain, or evolve it.

Layer 4 - *how do I change it safely?* - is the layer this guide most wants
to elevate. Historically, layers 1 and 2 have been documented
inconsistently, layer 3 occasionally, and layer 4 rarely. Layer 4 is also
the layer for which LLM tooling provides the most leverage: an agent
working with you has the reasoning trail that produces this content as a
byproduct of the implementation session itself.

Layer 4 should not be interpreted narrowly as only "validation steps" or
"current invariants." It also includes enough preserved decision context that a
future maintainer can understand why a different change may be wrong, or why it
might only become appropriate under changed assumptions. Repository
documentation should usually preserve enough context around meaningful
alternatives that a future reader can understand why the current choice was made.

### What this looks like in practice

A concrete example of a document written in the style this guide recommends is
[`.github/packaging/README.md`](../.github/packaging/README.md). It documents the RPM
packaging flow, its design constraints, and context that may prove helpful in
maintaining it.

The document maps cleanly to the four layers:

- **Why it exists** - the overview explains that the directory exists to ship
  signed RPMs for RHEL 9.x-compatible systems *without making future maintainers
  reconstruct the packaging model from CI YAML and shell scripts alone.* That is
  exactly the sort of durable motivation repository documentation should
  preserve.

- **How to use it** - the usage sections identify the produced artifacts, the
  main entrypoints, local build/validation commands, and CI behavior.  A reader
  who just needs to build or validate RPMs can stop there and still succeed.

- **How does it work?** - the conceptual model section explains that the RPM
  packaging code is kept separate from the source tree being packaged rather
  than treated as something that must already exist in each historical release
  tag. It also calls out the architecture-name mapping between RPM conventions
  and Docker/Go conventions. Those are important details that are not obvious
  from individual scripts alone.

- **How to change it safely** - the maintainer guidance preserves why builds run in
  Rocky Linux 9, why manual tag builds copy only `.github/packaging` into the tagged
  source tree, why the signing path is always exercised, why dynamic glibc is the
  current linking choice, what alternatives were considered, which invariants should
  be preserved, and what assumptions would justify revisiting the design.

The document is not exhaustive in any layer. It records the parts that would be
expensive to rediscover from code and CI configuration alone, and leaves the rest
to the implementation.

## Scope

Repository documentation is the in-repo record that helps future readers
understand the implemented system and the reasoning that still matters after
implementation.

It is not about transient artifacts like PR descriptions, review threads, or
session notes, except to say that important information should not live only
there.

It is also not a replacement for design docs, architecture decision records
(ADRs, as popularized by Michael Nygard), Requests for Comments (RFCs, from the
IETF), or Requests for Discussion (RFDs, in the Oxide Computer sense) when a
change needs pre-implementation alignment, and does not remove the need for
stakeholder review, requirement clarification, architectural approval, or
up-front comparison of competing approaches.

Some changes need both: a design-time document to align on the intended
direction before implementation, and repository documentation near the code
to preserve the reasoning behind the implemented result. Design documents are
for deciding. Repository documentation is for learning, using, remembering,
and evolving.

## Non-goals

This guide deliberately does not prescribe:

- **length or depth.** How long a document should be follows from the scope
  and criticality of what is being documented, not from a separate
  heuristic. The answer depends on how much non-obvious, durable context is
  needed to answer the four questions above.
- **agent-specific authoring rules.** LLM tooling is what makes the
  maintenance-doc expectation practical, but this guide does not prescribe
  how agents should produce documentation content. The criteria for what
  belongs apply regardless of author. How agent-assisted authoring works in
  practice - drafting, divergence detection, synthesis from session
  history - is left to evolve as a working pattern rather than codified
  here.

## When to add or expand documentation

Use this section to decide whether something deserves a dedicated document.
Use the later "What belongs in repository documentation" section to decide
whether a particular detail belongs in one.

Something likely deserves dedicated documentation when two or more of the
following are true:

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

To apply that criterion to a specific detail, ask:

- If this were lost, would a future maintainer or reviewer be more likely to
  make a bad change?
- If a thoughtful reader asked "why is it like this?", would this detail
  help answer the question?
- Does this help someone use, validate, or evolve the code correctly?
- Is this part of the current reasoning model, rather than just part of the
  path taken to get here?

If the answer is yes, it likely belongs. Typical examples:

- why an abstraction boundary exists where it does
- why a runtime-specific implementation was chosen instead of a generic one
- why a validation strategy uses end-to-end testing instead of only unit tests
- why a permissions split, cleanup strategy, or lifecycle hook exists
- what assumptions justify the current design
- what alternatives remain plausible enough that future readers would naturally
  ask about them
- what meaningful alternatives were considered, and why they were not chosen
- what conditions would justify revisiting the design later
- what entrypoints a new reader should use depending on whether they are a user,
  reviewer, or maintainer

## What does not belong

Do not use repository documentation as:

- a duplicate of code-level details already obvious from the implementation
- a session transcript
- a PR summary
- a debugging log
- an implementation diary
- a dumping ground for every explored dead end

Some explored paths are worth preserving, but only after they are translated
into a stable explanation such as a trade-off, rejected alternative, or
important discovered constraint.

When deciding whether to keep rationale about an explored path, ask: if a future
maintainer proposed an obvious alternative here, would the repository docs
explain why that might be wrong? If not, the rationale may still be missing.

Preserve durable reasoning, not step-by-step history.

## Location and naming

Place documentation as close to the thing it describes as possible:

- package- or feature-specific docs live next to the code they describe
- cross-cutting documentation that spans multiple areas lives under `docs/`

Closeness to code makes documentation easier to find while reading the code,
and easier to keep current as the code evolves.

For area-specific documents, choose names based on discoverability near the
code. Common shapes include:

- `README.md` for the package-level index
- `<feature>.md` for a single combined feature document
- `<feature>-architecture.md`, `<feature>-concepts.md`, or
  `<feature>-maintainers.md` when user-facing and maintainer-facing material
  are split
- `validation.md` or similar for cross-cutting topics within an area

The exact filename matters less than making the document easy to find and easy
to interpret.

## Discoverability

This repository is not a wiki, but documentation still needs wiki-like
reachability. The convention is:

- the **nearest README is the index** for documentation in that area
- every dedicated document should be reachable by following links from that
  README (directly or transitively)
- if a document is not linked from any README, it is effectively orphaned

In practice, when adding a new document, also add a link to it from the
appropriate README. For cross-cutting documentation under `docs/`,
`docs/README.md` is the index for that subtree. Every top-level document in
`docs/` should be linked from `docs/README.md`, and subdirectories under
`docs/` that contain multiple documents should usually have their own
`README.md` linking the documents in that area. Particularly important
cross-cutting documents may also be linked from the repository root
`README.md`, but `docs/README.md` is the minimum required index.

When moving or renaming a document, update inbound links. Treat broken or
missing inbound links as maintenance issues that should be fixed when found.

When exploring or modifying an unfamiliar area of the repository, readers and
agents should start with the nearest README, then follow its links to the more
specific documentation and code entrypoints for that area.

READMEs themselves may carry some durable context, but they should not be
forced to carry all of it. Their primary job is to orient and point.

## Recommended structure

Start simple. A single document is often enough.

One useful default is to structure a document around the
[four-layer framework](#a-four-layer-framework) above:

1. **Overview**
2. **Usage**
3. **Conceptual model**
4. **Maintainer guidance**

Use the earlier framework for the detailed questions each section should
answer. This is a recommendation, not a rigid template.

A trailing **References** section pointing to the relevant code, tests, and
related docs is a useful close. It makes the cross-references between doc
and code explicit, supports the discoverability conventions above, and gives
a reviewer a quick map of the surface the doc covers.

This is a recommendation, not a rigid template. The important thing is to make
it easy for the next reader to find the right level of detail.

A code-adjacent README is not merely a usage guide. When it serves as
repository documentation, it must also preserve the non-obvious reasoning a
future maintainer would otherwise need to reconstruct from design docs, PRs,
or oral history.

In some cases, these map naturally to audience-oriented sections such as:

- **For users or consumers**
  - why it exists, how to use it, examples, limits
- **For maintainers**
  - conceptual model, rationale, constraints, validation, invariants

Either framing is fine. The important part is that repository documentation
should help readers understand, use, and safely evolve what it describes.

## Single document or multiple

A single document is often enough. Start with one when the scope is moderate,
a single entrypoint improves discoverability, and user and maintainer content
can coexist with clear sectioning. Split into multiple documents when
maintainer material overwhelms user-facing material, the document becomes
difficult to skim, different parts evolve independently, or separate
audiences consistently need separate entrypoints. The repository should
optimize for clarity and discoverability, not a fixed file count.

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
or issue boundaries. The document becomes a shared record for the code or
workflow itself, rather than a per-task handoff note.

Documentation should be maintained like tests, and treated like tests:
kept current with the code it describes, and a precondition for safe
modification. Modifying code whose maintenance rationale is missing or
stale carries the same kind of risk as modifying untested code, and
should prompt the same response: document first, then change.

Apply that rule proportionally here as well. If a change is purely mechanical
and does not alter behavior, constraints, rationale, or validation
expectations, a documentation update may not be needed. If the change relies
on or changes durable context that a future maintainer would need in order to
review or evolve the area safely, updating the repository documentation is part
of doing the change correctly.

When moving, consolidating, or shortening existing documentation, do not treat
that refactor as permission to discard durable maintenance rationale. The live
repository document does not need to preserve every detail of the original, but
it should retain the reasoning that would materially influence future changes,
including important constraints, alternatives that help explain the current
design, and the reasons those alternatives were not chosen.

## Rule of thumb

For significant parts of the repository, maintain code-adjacent documentation
that helps future readers understand why something exists, use it correctly,
form the right mental model, and change it safely. If the information that
supports those goals would otherwise live only in someone's head, in scattered
PR comments, or in agent session history, it probably belongs in repository
documentation.
