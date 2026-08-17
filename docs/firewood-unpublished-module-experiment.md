# Firewood unpublished-module experiment

This experiment records the expected transient state while Firewood source is
newer than its published Go module. It does not change CI policy or publish a
module.

## Setup

The root module remains pinned to the published FFI module:

```text
github.com/ava-labs/firewood-go-ethhash/ffi v0.8.0
```

The Firewood source API intentionally renamed
`ffi.WithExpensiveMetrics` to `ffi.WithExpensiveMetricsEnabled`. The local
AvalancheGo callers in `graft/evm/firewood` and `vms/saevm/firewood` use the
new name. Version `v0.8.0` does not contain it.

Bazel resolves `github.com/ava-labs/firewood-go-ethhash/ffi` to
`//firewood/ffi:ffi` through the root `BUILD.bazel` Gazelle directive. That
local target compiles the source Go FFI and the Rust static library. Native Go
commands resolve the import from `go.mod` and use the published module.

## Commands and results

All commands below ran from the repository root.

| CI/task path | Firewood resolution path | Result | First failure point | Implication |
| --- | --- | --- | --- | --- |
| `./scripts/nix_run.sh ./scripts/run_bazel_ci_command.sh build //vms/saevm/firewood:firewood` | Bazel `//firewood/ffi:ffi` source target | Pass | None | The affected AvalancheGo target builds from the checkout, including Rust FFI source. |
| `./scripts/nix_run.sh go build ./vms/saevm/firewood` | `go.mod` -> `github.com/ava-labs/firewood-go-ethhash/ffi@v0.8.0` | Fail | Compile of local `graft/evm/firewood`, a dependency of the requested package | Native Go uses the stale published API. |
| `./scripts/run_task.sh bazel-test-e2e-ci` | Bazel for AvalancheGo; native Go for `build-xsvm` and Ginkgo | Fail | Ginkgo compilation of `./tests/e2e` | The Bazel-built AvalancheGo binary and `build-xsvm` both pass. `./bin/ginkgo` compiles the E2E binary with Go modules and finds the stale FFI API. |
| `./scripts/run_task.sh test-e2e-ci-schedule-latest` | `go.mod` -> published FFI module | Fail | `build-race` | Native latest-upgrade E2E does not reach `build-xsvm` or Ginkgo. |

The first compiler error for every module-based failure was:

```text
# github.com/ava-labs/avalanchego/graft/evm/firewood
graft/evm/firewood/triedb.go:164:33: undefined: ffi.WithExpensiveMetricsEnabled
```

The Ginkgo invocation reports the same error using its relative path:

```text
../../graft/evm/firewood/triedb.go:164:33: undefined: ffi.WithExpensiveMetricsEnabled
```

## CI classification during the unpublished window

Treat every task or job that runs native `go build`, `go test`, `go run`, or
Ginkgo as go.mod-dependent when it compiles a package that reaches Firewood.
This includes the native build, unit, fuzz, upgrade, E2E, load, re-execution,
and antithesis paths. In particular, `test-e2e-ci`,
`test-e2e-ci-post-latest`, `test-e2e-ci-schedule-latest`, and
`test-e2e-existing-ci` are go.mod-dependent: each begins with native
`build-race`, and their E2E scripts invoke `./bin/ginkgo`.

`bazel-test-e2e-ci` is not a fully Bazel path. Its `bazel-build-race` step
succeeds against source Firewood. It then runs native `build-xsvm` (which did
not reach Firewood in this experiment) and `tests.e2e.existing.sh`. The latter
calls `tests.e2e.sh`, whose `./bin/ginkgo` invocation compiles `./tests/e2e`
through Go modules. Therefore this purported Bazel E2E job remains
Go-module-dependent and fails before starting the reusable network.

The Bazel E2E task runs only the reusable-network `xsvm.go` scenario. It does
not cover the full E2E suite or either latest-upgrade variant.
