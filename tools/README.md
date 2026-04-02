# Tools

This directory is for repo-local tooling.

## `./external/go.mod`

The `external` subdirectory contains a separate Go module for
managing external CLI tools intended for invocation with `go tool`. By
keeping these dependencies in a separate module, the main module's
dependencies remain unpolluted.

See `external/go.mod` for usage instructions.

## Adding Repo-Local Commands

Custom commands specific to this repository should be implemented as
subdirectories under `./tools`. Each command can have its own
`main.go` and be invoked via `go run ./tools/<command>` or built as
needed.

These tools are for development-time and test-time workflows, not
production service runtime. Optimize them for debuggability and clear
operator behavior.

Preferred conventions for repo-local tools:

- use structured logging at command entry points and external call boundaries
- log important mutations and persistence operations at info level
- return structured machine-readable output for read paths when callers may need
  to inspect results programmatically
- keep mutating command success output short and human-readable
- create or wrap errors through
  `github.com/ava-labs/avalanchego/tests/fixture/stacktrace` so failures retain
  stack-bearing context during development and test debugging

`tools/pendingreview` is the current reference example for these conventions.

## Import Boundary

Packages under `./tools` are repo-local command and helper implementations for
development-time and test-time workflows. They are not part of the runtime
application surface and should not become dependencies of production packages.

In practice:

- runtime packages should not import `github.com/ava-labs/avalanchego/tools/...`
- if logic is needed in both runtime code and a tool, move that logic into a
  non-`tools` package and have both call into it
- lint should enforce this boundary just as `scripts/lint.sh` already enforces
  the boundary against importing test-only packages into non-test code
