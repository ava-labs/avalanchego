# Generate Bazel unit-test suites

This tool converts query-based unit-test shard definitions into checked-in
lists of Bazel test labels. The repository uses those lists to declare reusable
root `test_suite` targets for CI, local tasks, and dependency-cache setup. For
the reason this tool generates static suites, see [Generated Unit-Test
Suites](../../docs/bazel.md#generated-unit-test-suites).

## Table of contents

- [Workflow](#workflow)
- [Files](#files)

## Workflow

The tool reads [`.bazel/test_shards.json`](../../.bazel/test_shards.json) and
writes [`.bazel/generated_test_suites.bzl`](../../.bazel/generated_test_suites.bzl).
Each shard definition has a name, description, and Bazel query. The generator
excludes manual tests. It verifies that every non-manual Go test belongs to
exactly one shard. The generated file records each description and the explicit
test labels selected by its query.

After a change that can affect BUILD metadata or shard membership, use the
normal metadata task:

```bash
task bazel-generate-metadata
```

It runs Gazelle first. Gazelle updates BUILD rules for new test files and
packages before this tool queries their `go_test` targets. To regenerate only
the suites when BUILD metadata is already current, run:

```bash
task bazel-generate-unit-test-suites
```

For shard maintenance requirements, see [Generated Unit-Test
Suites](../../docs/bazel.md#generated-unit-test-suites).

To verify that the checked-in output is current, commit the generated file.
Then run:

```bash
task bazel-check-metadata
```

This command runs all Bazel metadata checks. It fails if a generator changes
the working tree.

## Files

- [`generator.go`](./generator.go): Generates suite membership.
- [`main.go`](./main.go): Reads shard definitions and writes the generated file.
- [`BUILD.bazel`](./BUILD.bazel): Defines the Bazel executable and its tests.
