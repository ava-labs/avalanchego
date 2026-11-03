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

## `./netviz`

`netviz` shows how txs and blocks move between nodes, from the
`events.log` files that nodes write with `--log-events`. A fuzz run's
root dir (`tests/fuzz`) also holds the fuzzer's `events.log`, and netviz
shows each op in a `fuzzer` column beside its effects on the nodes. It is a
separate module, like `external`, so its TUI dependencies stay out of
the main module. Build it with `GOWORK=off`, since the repo's
`go.work` does not list it:

```sh
GOWORK=off go build -C tools/netviz -o "$PWD/build/netviz" .
./build/netviz path/to/network            # TUI: pick a tx or block ([b]), follow it; [/] follows any ID, tab completes
./build/netviz -print path/to/network     # the tx list
./build/netviz -print -blocks path/to/network
./build/netviz -print -follow <id> path/to/network
```
