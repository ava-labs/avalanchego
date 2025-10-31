# AvalancheGo Setup Action

## Overview
This action provides composable CI capabilities for setting up AvalancheGo with custom dependency versions.

### Why this exists?
Solves CI composability problems by enabling repositories to setup and build AvalancheGo with specific dependency versions without complex setup or cross-repository coordination.

### Why is it needed?
Dependencies need AvalancheGo as their integration context for realistic testing.
Without this action, setting up AvalancheGo with custom dependency versions requires build knowledge and manual `go mod` manipulation.
Teams either skip proper testing or dump tests in AvalancheGo (wrong ownership).
This action makes AvalancheGo composable - any repository can pull it in as integration context with one line.

## Security Model
- Repository Restriction: Only allows dependencies from `github.com/ava-labs/*` repositories
- No Custom Forks: Prevents supply chain attacks from malicious forks
- Commit Validation: Validates dependency versions reference ava-labs repositories only

## Inputs

| Input | Description | Required | Default               |
|-------|-------------|----------|-----------------------|
| `checkout-path` | Directory path where AvalancheGo will be checked out | No | `'.'`                 |
| `avalanchego` | AvalancheGo version (commit SHA, branch, tag) | No | `'${{ github.sha }}'` |
| `firewood` | Firewood version. Consumer should run Firewood shared workflow first | No | `''`                  |
| `coreth` | Coreth version (commit SHA, branch, tag) | No | `''`                  |
| `libevm` | LibEVM version (commit SHA, branch, tag) | No | `''`                  |

## Usage Examples

```yaml
- name: Setup AvalancheGo with Firewood             # This will setup go.mod
  uses: ./.github/actions/avalanchego-setup-action
  with:
    checkout-path: "build/avalanchego"
    coreth: "my-feature-branch"
    libevm: "experimental-branch"
    firewood: "ffi/v0.0.12"
- name: Load test                                   # This will compile and run the load test
  uses: ./.github/actions/run-monitored-tmpnet-cmd
  with:
      run: ./scripts/run_task.sh test-load -- --load-timeout=30m --firewood
```
