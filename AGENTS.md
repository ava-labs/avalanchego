# AGENTS

## Environment (Required)
Use the Nix dev shell for a reproducible toolchain.

- Install Nix (once): `./scripts/run_task.sh install-nix`
- Enter dev shell: `nix develop`
- Optional direnv: `direnv allow` (set `DIRENV_LOG_FORMAT=` to quiet logs)

## Tasks
The task runner is available in the Nix shell.

- List tasks: `./scripts/run_task.sh --list` or `task --list`
- Task definitions: `Taskfile.yml`
- If `task` is not installed: `./scripts/run_task.sh <task>`

## Verification (Scoped First)
Prefer the smallest relevant check before running full-suite tasks.

- Scoped unit tests: `go test -tags test -shuffle=on -race ./path/to/pkg/...`
- Faster scoped tests (no race): `NO_RACE=1 go test -tags test -shuffle=on ./path/to/pkg/...`
- Scoped linting: `./scripts/lint.sh ./path/to/pkg/...`

For full checks, consider `task test-unit`, `task lint`, or `task lint-all`.
