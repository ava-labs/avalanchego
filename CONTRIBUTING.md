# How to Contribute to Avalanche

## Setup

To start developing on AvalancheGo, you'll need a few things installed.

- Golang version >= 1.25.8
- gcc
- g++

On MacOS, a modern version of bash is required (e.g. via [homebrew](https://brew.sh/) with `brew install bash`). The version installed by default is not compatible with AvalancheGo's [shell scripts](scripts).

## Go Workspace

This repository uses a [Go workspace](https://go.dev/doc/tutorial/workspaces) (`go.work`)
to unify the main module with grafted modules (coreth, subnet-evm, evm) under `graft/`.
This provides IDE support for navigating and refactoring across all modules seamlessly.

### Behavioral changes from workspace mode

When `go.work` is present at the repository root, some go command flags are restricted:

| Flag | Workspace behavior |
|------|-------------|
| `-mod=readonly` | Implicit default and only allowed value |
| `-mod=mod` | Not allowed (would modify go.mod) |
| `-mod=vendor` | Not allowed (use `go work vendor` instead) |
| `-modfile=path` | Not allowed (workspace manages module resolution) |

These restrictions exist because the workspace manages dependencies across all member
modules. Use `GOWORK=off` to disable workspace mode when needed:

```bash
GOWORK=off go <command>
```

Other behavioral changes:

| Command | Behavior Change |
|---------|-----------------|
| `go list -m` | Lists all workspace modules (use `head -1` or specify module path for single result) |
| `go mod tidy` | Only affects current module; run `task go-mod-tidy` to tidy all modules |
| `go work sync` | Updates `go.work.sum`; prefer `task sync-go-work` after dependency changes |

### Using a custom workspace

The repo's `go.work` takes precedence over any workspace file in a parent directory.
To use your own workspace (e.g., with local checkouts of repos not yet in the monorepo):

```bash
export GOWORK=~/src/my-go.work
```

See the [Go Modules Reference](https://go.dev/ref/mod#workspaces) for full workspace documentation.

## Nix

This repository uses Nix to provide the pinned development toolchain used by many tasks and scripts.

### Installation

Install Nix with:

```bash
task install-nix
```

If `task` is not yet available, use:

```bash
./scripts/run_task.sh install-nix
```

Platform-specific behavior:

- macOS: this uses the [Lix installer](https://lix.systems/install/). We prefer it on macOS
  because it has a cleaner install path, documented uninstall support, and documented support
  for surviving macOS upgrades. The [Lix installer
  documentation](https://git.lix.systems/lix-project/lix-installer) also says it installs Lix
  with flakes enabled by default. We prefer Lix over vendor-backed alternatives because Lix is
  maintained as an [independent community project](https://lix.systems/about), while
  Determinate's installer sits within a broader vendor ecosystem that includes
  [FlakeHub](https://determinate.systems/flakehub), Determinate's platform for publishing
  flakes.  The Determinate installer it is based on also documents a built-in single-command
  uninstall: [`/nix/nix-installer
  uninstall`](https://docs.determinate.systems/guides/migrating-from-upstream-nix/). As of
  February 27, 2026, upstream Nix's Rust installer also documents uninstall support, but
  upstream still described that installer as beta in the [Nix 2.34 release
  notes](https://nix.dev/manual/nix/2.34/release-notes/rl-2.34).
- Linux: this uses the upstream Nix daemon installer and enables `nix-command flakes` in
  `~/.config/nix/nix.conf`.

### Using the dev shell

Start the repo's dev shell with:

```bash
nix develop
```

This is explicit and works well when you want the full pinned environment for a session.

You do not need to run `nix develop` for every task. Named tasks in the root `Taskfile.yml` use
`./scripts/nix_run.sh`, which enters the repo's nix dev shell automatically when needed. The
main exception is bare `task`, which only lists available tasks.

For zsh users, `nix develop` is explicit but disruptive because it starts a `bash` shell rather
than preserving the normal interactive zsh session.

## direnv

This repository includes a [`.envrc`](./.envrc) for use with [direnv](https://direnv.net/). When
loaded, it:

- adds repo-local commands from [`bin/`](./bin) to `PATH`
- sets `AVALANCHEGO_PATH` to the repo-local `avalanchego` binary path
- creates and sets `AVAGO_PLUGIN_DIR`
- sets `TMPNET_NETWORK_DIR` to the latest tmpnet deployment by default
- supports per-user overrides via `.envrc.local`
- supports shared personal overrides via `GLOBAL_ENVRC`
- optionally activates the repo's nix flake when `AVALANCHEGO_DIRENV_USE_FLAKE=1` is set

### Setup

To use it:

1. Install [direnv](https://direnv.net/docs/installation.html).
1. Hook it into your shell:
   - bash: [`eval "$(direnv hook bash)"`](https://direnv.net/docs/hook.html)
   - zsh: [`eval "$(direnv hook zsh)"`](https://direnv.net/docs/hook.html)
1. Allow this repo's `.envrc`:

```bash
direnv allow
```

If you trust this repository and want `direnv` to auto-allow `.envrc` files under a directory
hierarchy, configure a trusted prefix in
[`direnv.toml`](https://direnv.net/man/direnv.toml.1.html). The `whitelist.prefix` setting marks
matching directories as trusted.

### Flake activation

By default, the repo's `.envrc` applies the repo-local environment without activating the nix dev
shell.

If you want `direnv` to activate the repo flake as well, Nix must be installed first. See
[Nix](#nix).

To enable flake activation, export `AVALANCHEGO_DIRENV_USE_FLAKE=1` before entering the repo, or
reload `direnv` after setting it.

If you want quieter `direnv` output when using flake activation, set:

```bash
export DIRENV_LOG_FORMAT=
```

```bash
export AVALANCHEGO_DIRENV_USE_FLAKE=1
direnv reload
```

For bash users, this is typically the most integrated workflow. For zsh users, it is often less
desirable; see [Nix](#nix).

## Running tasks

This repo uses the [Task](https://taskfile.dev/) task runner to simplify usage and discoverability
of development tasks. To list available tasks:

```bash
task
```

### Choosing a task execution mode

There are two separate concerns when running tasks in this repository:

- How the `task` runner itself is made available
- How individual task commands get the repo's pinned toolchain

The `task` command is typically made available in one of these ways:

- From the nix dev shell, where `task` is on `PATH`
- From repo-local `bin/task`, which is added to `PATH` by `direnv`
- From the pinned Go tool in `tools/external/go.mod`, which `./scripts/run_task.sh` and `bin/task`
  use as a fallback when a real `task` binary is not already available

Many task commands also use `./scripts/nix_run.sh` internally so they can enter the repo's nix dev
shell when required, without requiring you to manually run `nix develop` first.

### Common configurations

#### Bash

- Best: `direnv` with `AVALANCHEGO_DIRENV_USE_FLAKE=1`
  - Loads the repo flake automatically when you enter the repo.
  - Best when you want the pinned toolchain available throughout a shell session.
- Good: `direnv` without `AVALANCHEGO_DIRENV_USE_FLAKE`
  - Keeps your normal shell environment while letting named tasks enter nix automatically when
    needed.
- Explicit: `nix develop`
  - Best when you want to work inside the full dev shell for a whole shell session.

#### Zsh

- Best: `direnv` without `AVALANCHEGO_DIRENV_USE_FLAKE`
  - Keeps your normal interactive zsh session while letting named tasks enter nix automatically
    when needed.
- Explicit: `nix develop`
  - Use this when you want the full dev shell for a whole shell session and are fine with the
    tradeoffs described in [Nix](#nix).
- Not recommended: `direnv` with `AVALANCHEGO_DIRENV_USE_FLAKE=1`
  - This activates the repo flake automatically, but it brings the same shell tradeoffs described
    in [Nix](#nix).

### How task invocation works

- Plain `task` lists available tasks.
- Named tasks in the root `Taskfile.yml` use `./scripts/nix_run.sh`, so they enter the nix dev
  shell automatically when they need tools from the pinned flake environment.
- `./scripts/run_task.sh <task>` is still a valid entrypoint and falls back to the pinned Go `task`
  tool if a `task` binary is not already available on `PATH`.
- If you do not use `direnv`, you can still run tasks with either `./scripts/run_task.sh <task>` or
  `nix develop --command task <task>`.

## Issues

### Security

- Do not open up a GitHub issue if it relates to a security vulnerability in AvalancheGo, and instead refer to our [security policy](./SECURITY.md).

### Did you fix whitespace, format code, or make a purely cosmetic patch?

- Changes from the community that are cosmetic in nature and do not add anything substantial to the stability, functionality, or testability of `avalanchego` will generally not be accepted.

### Making an Issue

- Check that the issue you're filing doesn't already exist by searching under [issues](https://github.com/ava-labs/avalanchego/issues).
- If you're unable to find an open issue addressing the problem, [open a new one](https://github.com/ava-labs/avalanchego/issues/new/choose). Be sure to include a *title and clear description* with as much relevant information as possible.

## Features

- If you want to start a discussion about the development of a new feature or the modification of an existing one, start a thread under GitHub [discussions](https://github.com/ava-labs/avalanchego/discussions/categories/ideas).
- Post a thread about your idea and why it should be added to AvalancheGo.
- Don't start working on a pull request until you've received positive feedback from the maintainers.

## Pull Request Guidelines

- Open a new GitHub pull request containing your changes.
- Ensure the PR description clearly describes the problem and solution. Include the relevant issue number if applicable.
- The PR should be opened against the `master` branch.
- If your PR isn't ready to be reviewed just yet, you can open it as a draft to collect early feedback on your changes.
- Once the PR is ready for review, mark it as ready-for-review and request review from one of the maintainers.

### Autogenerated code

- Any changes to protobuf message types require that protobuf files are regenerated.

```sh
./scripts/run_task.sh generate-protobuf
```

#### Autogenerated mocks

💁 The general direction is to **reduce** usage of mocks, so use the following with moderation.

Mocks are auto-generated using [mockgen](https://pkg.go.dev/go.uber.org/mock/mockgen) and `//go:generate` commands in the code.

- To **re-generate all mocks**, use the command below from the root of the project:

    ```sh
    ./scripts/run_task.sh generate-mocks
    ```

- To **add** an interface that needs a corresponding mock generated:
  - if the file `mocks_generate_test.go` exists in the package where the interface is located, either:
    - modify its `//go:generate go run go.uber.org/mock/mockgen` to generate a mock for your interface (preferred); or
    - add another `//go:generate go run go.uber.org/mock/mockgen` to generate a mock for your interface according to specific mock generation settings
  - if the file `mocks_generate_test.go` does not exist in the package where the interface is located, create it with content (adapt as needed):

    ```go
    // Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
    // See the file LICENSE for licensing terms.

    package mypackage

    //go:generate go run go.uber.org/mock/mockgen -package=${GOPACKAGE} -destination=mocks_test.go . YourInterface
    ```

    Notes:
    1. Ideally generate all mocks to `mocks_test.go` for the package you need to use the mocks for and do not export mocks to other packages. This reduces package dependencies, reduces production code pollution and forces to have locally defined narrow interfaces.
    1. Prefer using reflect mode to generate mocks than source mode, unless you need a mock for an unexported interface, which should be rare.
- To **remove** an interface from having a corresponding mock generated:
  1. Edit the `mocks_generate_test.go` file in the directory where the interface is defined
  1. If the `//go:generate` mockgen command line:
      - generates a mock file for multiple interfaces, remove your interface from the line
      - generates a mock file only for the interface, remove the entire line. If the file is empty, remove `mocks_generate_test.go` as well.

### Testing

#### Local

- Build the avalanchego binary

```sh
./scripts/run_task.sh build
```

- Run unit tests

```sh
./scripts/run_task.sh test-unit
```

- Run the linter

```sh
./scripts/run_task.sh lint
```

### Continuous Integration (CI)

- Pull requests will generally not be approved or merged unless they pass CI.

## Other

### Do you have questions about the source code?

- Ask any question about AvalancheGo under GitHub [discussions](https://github.com/ava-labs/avalanchego/discussions/categories/q-a).

### Do you want to contribute to the Avalanche documentation?

- Please check out the `avalanche-docs` repository [here](https://github.com/ava-labs/avalanche-docs).
