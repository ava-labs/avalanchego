# Project-first naming for tasks, CI jobs, and Bazel targets

## Overview

This repository is converging on a project-first naming convention so that the
main execution surfaces - Taskfile entrypoints, CI job names, and Bazel target
names - describe the same ownership model and are easier to extend without
inventing one-off naming patterns.

The immediate goal is coherence. The longer-term goal is to make continued
Bazelification of CI less confusing by keeping local workflows, CI workflows,
and Bazel target groups aligned enough that a reader can move between them
without re-learning the naming scheme each time.

This matters because the repository now spans multiple buildable and testable
projects (`avalanchego`, `coreth`, `evm`, and `subnet-evm`) plus repo-owned
subsystems such as Bazel metadata maintenance. Without an explicit convention,
new tasks and CI jobs tend to accumulate as a flat list of verbs, backend
prefixes, or historical exceptions whose ownership is only obvious if you
already know the repo.

## Usage

### Canonical task form

Project-owned tasks use:

- `<project>:<action>`

Examples:

- `avalanchego:test-unit-bazel`
- `avalanchego:test-unit-gomod`
- `avalanchego:build-bazel`
- `avalanchego:build-gomod`
- `avalanchego:build-opt-bazel`
- `avalanchego:build-race-gomod`
- `avalanchego:lint`
- `coreth:test-unit-gomod`
- `coreth:test-unit-bazel`
- `subnet-evm:test-unit-bazel`
- `evm:test-unit-gomod`

Backend qualifiers are used when the backend matters operationally or when more
than one backend exists for the same action.

Common shapes:

- backend-neutral test: `test-<kind>`
- backend-qualified test: `test-<kind>-<backend>`
- backend-qualified build: `build-<backend>`
- backend-qualified build variant: `build-<variant>-<backend>`

Examples:

- `test-unit-bazel`
- `test-unit-gomod`
- `test-e2e-bazel`
- `test-e2e-gomod`
- `test-load`
- `build-bazel`
- `build-gomod`
- `build-opt-bazel`
- `build-race-gomod`

The ordering is intentional so related variants sort together while keeping the
backend as the suffix.

### Canonical CI job form

Ordinary task-like CI jobs use kebab-case and mirror task semantics:

- `<project>-<action>`

Examples:

- `avalanchego-test-unit-bazel`
- `avalanchego-test-unit-gomod`
- `coreth-test-e2e-warp`
- `subnet-evm-test-unit-bazel`
- `evm-lint`

When a workflow already provides the namespace, redundant namespace text in
each job name may be omitted. For example, inside `bazel-ci.yml`, job names
such as `avalanchego-test-unit`, `subnet-evm-test-unit`, `setup`, and
`required` are preferred over repeating `bazel` in every job name. Likewise,
inside project-specific workflows such as `coreth-ci.yml`, `evm-ci.yml`, and
`subnet-evm-ci.yml`, workflow-local job names such as `lint`,
`test-unit-gomod`, `test-e2e-warp`, and `required` are preferred over
repeating the project name in every job.

This rule is for CI jobs that directly represent task/action entrypoints. It is
not intended to rename every workflow job in the repository. Release,
packaging, and artifact-bundling workflows may use other names when the job is
not itself a task-like project surface.

Required aggregate jobs such as `avalanchego-required` or workflow-local names
such as `required` are allowed to be special cases because they exist to
provide one stable required check for a workflow, not to represent a user-facing
project workflow.

### Canonical Bazel target form

When Bazel exposes explicit named project/group entrypoints, they should use
noun-oriented underscore names:

- `<project>_<thing>`

Examples:

- `//:avalanchego_unit_tests`
- `//graft/coreth:coreth_unit_tests`
- `//graft/evm:evm_unit_tests`
- `//graft/subnet-evm:subnet_evm_unit_tests`
- `//:go_unit_tests`

This differs intentionally from task and CI names:

- tasks are human-oriented action entrypoints
- CI jobs are kebab-case action names
- Bazel targets are noun-oriented target groups

## Conceptual model

### Projects vs subsystems

A **project** is an independently owned, buildable, or testable component with
its own lifecycle boundary. In this repository that currently includes:

- `avalanchego`
- `coreth`
- `evm`
- `subnet-evm`

A **subsystem** is repo-owned infrastructure that is not itself a product-like
project surface. Examples:

- `bazel:*` for Bazel metadata maintenance
- `go:*` for true cross-project Go aggregates
- `git:*` for repository tag lifecycle operations
- `packaging:*` for packaging automation
- `tools:*` for repo tooling and environment setup

New work should default to either a project namespace or a subsystem namespace.
Do not add a vague root/global bucket just because a task is convenient from the
repo root.

### Taskfile ownership model

The root `Taskfile.yml` is an include/orchestration entrypoint only. It should
make namespaces available, not accumulate project-owned workflows.

Ownership is split as follows:

- `Taskfile.yml` - namespace wiring and minimal orchestration
- `.tasks/avalanchego.yml` - avalanchego-owned workflows
- `graft/coreth/Taskfile.yml` - coreth-owned workflows
- `graft/evm/Taskfile.yml` - evm-owned workflows
- `graft/subnet-evm/Taskfile.yml` - subnet-evm-owned workflows
- `.bazel/Taskfile.yml` - Bazel subsystem workflows
- `.tasks/go.yml` - true Go-family aggregate workflows
- `.tasks/git.yml` - git/tag lifecycle workflows
- `tools/Taskfile.yml` - repo tooling workflows

This lets each real project own its workflow surface near its code while still
presenting one canonical root entrypoint.

### Aggregates

Use aggregates only for real aggregates.

Valid examples:

- project-specific: `avalanchego:test-unit-bazel`
- language aggregate: `go:test-unit`
- Bazel aggregate target: `//:go_unit_tests`

Invalid pattern:

- `go:avalanchego-test-unit-bazel`

The `go:` namespace is for cross-project aggregates, not for smuggling a
project name under a language prefix.

### Current Bazel state vs intended Bazel naming

This branch defines the intended Bazel naming model, but it does not introduce
new Bazel suite-generation machinery.

Today, Bazel task entrypoints still use target patterns such as:

- `//... -- -//graft/...`
- `//graft/coreth/...`
- `//graft/evm/...`
- `//graft/subnet-evm/...`

Those patterns should eventually map to explicit project-first noun targets such
as `avalanchego_unit_tests` and `go_unit_tests`, but that implementation should
land in the dedicated Bazel suite/sharding work rather than being duplicated in
this naming branch.

## Maintainer guidance

### Extending the scheme for a new project

When adding a new real project:

1. Choose a namespace that matches the durable project name and ownership
   boundary.
2. Add or keep a project-owned Taskfile near the project root.
3. Include that Taskfile from the root `Taskfile.yml` under the project
   namespace.
4. When explicit Bazel entrypoints are added for the project, name them using
   the `<project>_<thing>` noun form.
5. Decide explicitly whether the project belongs in aggregate surfaces such as
   `go:test-unit` and `//:go_unit_tests`.

Prefer names that match user expectations and stable repository terminology.
Do not choose names based on transient implementation details such as the
current build backend.

### Adding a new test kind

For a new test kind, keep the action shape consistent:

- neutral form: `test-<kind>`
- qualified form when needed: `test-<kind>-<backend>`

Examples:

- `avalanchego:test-load`
- `subnet-evm:test-e2e-warp`
- `avalanchego:test-unit-bazel`
- `avalanchego:test-unit-gomod`

If the same project/action exists through more than one backend, prefer explicit
backend qualification rather than relying on historical defaults.

### Adding a non-default backend

When introducing another backend for an existing action:

1. Keep the project prefix unchanged.
2. Keep the action stem unchanged.
3. Add the backend suffix at the end.

For example, adding a new backend to `test-unit` should produce names parallel
to `test-unit-bazel` and `test-unit-gomod`, not a new prefix-based scheme. The
same applies to builds: once more than one backend exists, use names parallel
to `build-bazel` and `build-gomod`, with variants such as `build-opt-bazel` or
`build-race-gomod`.

This keeps related variants adjacent in task listings, CI UIs, and grep output.

### Changing the scheme safely

Preserve these invariants:

- project-owned workflows stay under project namespaces
- subsystem/tooling workflows stay under explicit subsystem namespaces
- CI job names stay action-oriented and kebab-case
- Bazel target names stay noun-oriented and underscore-based
- aggregate namespaces remain true aggregates, not project-specific wrappers
- the root `Taskfile.yml` remains an include/orchestration entrypoint

Validate changes proportionally:

- `task --list-all` should show the expected namespaced surface
- changed Taskfile entrypoints should run through their normal wrappers
- Bazel task entrypoints should still resolve with `bazel query` or
  `bazel test`
- CI workflow renames should pass `yamlfmt`/workflow linting and still invoke
  the intended task entrypoints
- if this branch is later combined with explicit Bazel suite generation, ensure
  the generated suite names follow the project-first noun form documented here

### Non-goals

This convention does not try to make the exact same string appear everywhere.
The mapping is intentionally surface-specific:

- `subnet-evm:test-unit-bazel`
- `subnet-evm-test-unit`
- `subnet_evm_unit_tests`

That distinction is part of the design, not a mismatch to eliminate.

## References

- [Documentation guidelines](./documentation-guidelines.md)
- [Bazel](./bazel.md)
- [`Taskfile.yml`](../Taskfile.yml)
- [`.tasks/avalanchego.yml`](../.tasks/avalanchego.yml)
- [`.bazel/Taskfile.yml`](../.bazel/Taskfile.yml)
