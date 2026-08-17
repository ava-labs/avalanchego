# Generate Bazel unit-test suites

This tool generates the root Bazel `test_suite` targets that group runnable Go
unit tests into CI shards.

## Table of contents

- [Workflow](#workflow)

## Workflow

The tool reads [`.bazel/test_shards.json`](../../.bazel/test_shards.json) and
writes [`.bazel/generated_test_suites.bzl`](../../.bazel/generated_test_suites.bzl).
Each shard definition needs a name, description, and Bazel query. The generated
file includes the description as a comment. Run it with:

```bash
task bazel-generate-unit-test-suites
```

For shard definitions, generator checks, and validation steps, see [Generated
Unit-Test Suites](../../docs/bazel.md#generated-unit-test-suites).

[`generator.go`](./generator.go) generates suite membership.
[`main.go`](./main.go) reads shard definitions and writes the generated file.
[`BUILD.bazel`](./BUILD.bazel) defines the Bazel executable and its tests.
