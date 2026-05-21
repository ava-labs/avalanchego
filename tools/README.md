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
