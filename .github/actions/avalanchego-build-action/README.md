# AvalancheGo Build Action

## Overview
This action provides composable CI capabilities for building AvalancheGo with custom dependency versions.

### Why this exists?
Solves CI composability problems by enabling repositories to build AvalancheGo with specific dependency versions without complex setup or cross-repository coordination.

## Modes

**BUILD Mode** (target specified): Produces a ready-to-use binary with custom dependencies - for teams that need the executable.

When `target` parameter is provided, the action:
1. Checks out AvalancheGo at specified version
2. Replaces dependencies with custom versions
3. Builds the specified binary
4. Makes binary available via both output parameter AND artifact upload
5. Optionally executes binary with provided args

**SETUP Mode** (no target): Prepares the build environment with custom dependencies - for teams that need custom build workflows.

When `target` parameter is empty, the action:
1. Checks out AvalancheGo at specified version
2. Replaces dependencies with custom versions
3. Sets up build environment for consumer's custom workflow

## Security Model
- Repository Restriction: Only allows dependencies from `github.com/ava-labs/*` repositories
- No Custom Forks: Prevents supply chain attacks from malicious forks
- Commit Validation: Validates dependency versions reference ava-labs repositories only

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `target` | Which binary to build (`avalanchego`, `reexecution`). Determines BUILD vs SETUP mode | No | `''` |
| `args` | Command-line arguments for target executable (BUILD mode only) | No | `''` |
| `checkout-path` | Directory path where AvalancheGo will be checked out | No | `'avalanchego'` |
| `avalanchego` | AvalancheGo version (commit SHA, branch, tag) | No | `'main'` |
| `firewood` | Firewood version. Consumer should run Firewood shared workflow first | No | `''` |
| `coreth` | Coreth version (commit SHA, branch, tag) | No | `'main'` |
| `libevm` | LibEVM version (commit SHA, branch, tag) | No | `'main'` |

## Outputs

| Output | Description |
|--------|-------------|
| `binary-path` | Absolute path to built binary (BUILD mode only) |

## Usage Examples

### BUILD Mode - Binary Available for Consumer

```yaml
- name: Build AvalancheGo
  id: build
  uses: ./.github/actions/avalanchego-build-action
  with:
    target: "avalanchego"
    coreth: "v0.12.5"
    libevm: "v1.0.0"

- name: Use binary via output parameter
  run: ${{ steps.build.outputs.binary-path }} --network-id=local

- name: Or download as artifact
  uses: actions/download-artifact@v4
  with:
    name: avalanchego-avalanchego_main-coreth_v0.12.5-libevm_v1.0.0

- name: Use downloaded artifact
  run: ./avalanchego --network-id=local
```

### SETUP Mode - Custom Workflow

```yaml
- name: Setup AvalancheGo with custom dependencies
  uses: ./.github/actions/avalanchego-build-action
  with:
    checkout-path: "build/avalanchego"
    coreth: "my-feature-branch"
    libevm: "experimental-branch"

- name: Run custom build commands
  run: |
    cd build/avalanchego
    ./scripts/run_task.sh reexecute-cchain-range
    ./scripts/run_task.sh my-custom-task
```

## Artifact Naming

Artifacts are named with the complete dependency matrix for full traceability:

**Format:** `{target}-avalanchego_{version}-coreth_{version}-libevm_{version}[-firewood_{version}]`

**Examples:**
- `avalanchego-avalanchego_main-coreth_main-libevm_main` (default versions)
- `avalanchego-avalanchego_v1.11.0-coreth_v0.12.5-libevm_v1.0.0-firewood_ffi%2Fv0.0.13` (with firewood)
- `reexecution-avalanchego_my-branch-coreth_main-libevm_experimental-firewood_abc123` (mixed versions)
