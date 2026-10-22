# Avalanche e2e test suites

- Works with fixture-managed temporary networks.
- Compiles to a single binary with customizable configurations.

## Running tests

```bash
./scripts/build.sh        # Builds avalanchego for use in deploying a test network
./scripts/build_xsvm.sh   # Builds xsvm for use in deploying a test network with a subnet
./bin/ginkgo -v ./tests/e2e -- --avalanchego-path=$PWD/build/avalanchego # Note that the path given for --avalanchego-path must be an absolute and not a relative path.
```

See [`tests.e2e.sh`](../../scripts/tests.e2e.sh) for an example.

### Simplifying usage with direnv

For repo-level `direnv` setup and behavior, see [CONTRIBUTING.md](../../CONTRIBUTING.md#direnv).
In this test workflow, the repo's [`.envrc`](../../.envrc) makes
`ginkgo` available without a `./bin/` prefix and avoids having to
specify the `--avalanchego-path` or `--plugin-dir` flags.

### Filtering test execution with labels

In cases where a change can be verified against only a subset of
tests, it is possible to filter the tests that will be executed by the
declarative labels that have been applied to them. Available labels
are defined as constants in [`describe.go`](../fixture/e2e/describe.go) with names
of the form `*Label`. The following example runs only those tests that
primarily target the X-Chain:


```bash
./bin/ginkgo -v --label-filter=x ./tests/e2e -- --avalanchego-path=$PWD/build/avalanchego
```

The ginkgo docs provide further detail on [how to compose label
queries](https://onsi.github.io/ginkgo/#spec-labels).

## Adding tests

Define any flags/configurations in [`flags.go`](../fixture/e2e/flags.go).

Create a new package to implement feature-specific tests, or add tests to an existing package. For example:

```
tests
└── e2e
    ├── README.md
    ├── e2e_test.go
    └── x
        └── transfer.go
            └── virtuous.go
```

`x/transfer/virtuous.go` defines X-Chain transfer tests,
labeled with `x`, which can be selected by `--label-filter=x`.

## Reusing temporary networks

By default, a new temporary test network will be started before each
test run and stopped at the end of the run. When developing e2e tests,
it may be helpful to reuse temporary networks across multiple test
runs. This can increase the speed of iteration by removing the
requirement to start a new network for every invocation of the test
under development.

To enable network reuse across test runs, pass `--reuse-network` as an
argument to the test suite:

```bash
./bin/gingko -v ./tests/e2e -- --avalanchego-path=/path/to/avalanchego --reuse-network
```

If a network is not already running the first time the suite runs with
`--reuse-network`, one will be started automatically and configured
for reuse by subsequent test runs also supplying `--reuse-network`.

### Restarting temporary networks

When iterating on a change to avalanchego and/or a VM, it may be
useful to restart a running network to ensure the network is using the
latest binary state. Supplying `--restart-network` in addition to
`--reuse-network` will ensure that all nodes are restarted before
tests are run. `--restart-network` is ignored if a network is not
running or if `--stop-network` is supplied.

### Stopping temporary networks

To stop a network configured for reuse, invoke the test suite with the
`--stop-network` argument. This will stop the network and exit
immediately without executing any tests:

```bash
./bin/gingko -v ./tests/e2e -- --stop-network
```

## Bootstrap checks

The suite starts a shared network for most tests. Some tests start a private
network to avoid being affected by changes made by other tests.

In a serial run, the suite checks whether a new node can bootstrap from the
shared network after each successful test. Tests can opt out with
`ginkgo.Label(e2e.SkipSharedNetworkBootstrapCheckLabel)`. For example, a test that
uses only a private network does not need a shared-network check. Tests with
private networks can check those networks before they shut down.

The first failed shared-network check narrows the investigation to changes
since the last passing check. No other test changes the shared network during
a serial check. An opted-out test can fall within this interval.

In a parallel run, another test can change the shared network during a
per-test check. A failure cannot identify which test changed the network.
Concurrent checks can also compete for resources on CI runners. Parallel
runs therefore do not check the shared network after each test.

After a parallel run, the suite checks the shared network once. It runs this
check only if an eligible test passed and no test failed. If all selected tests
opt out, the suite does not check the shared network. Serial runs do not
perform a post-suite check because eligible tests already perform per-test
checks. Private-network checks still run within their tests in either mode.

If the final check fails, a serial run can help identify the cause. Test order
and accumulated network changes can affect whether the failure occurs again.

To disable all bootstrap checks, set the `E2E_SKIP_BOOTSTRAP_CHECKS` environment
variable to a non-empty value. This also disables checks for private networks:

```bash
E2E_SKIP_BOOTSTRAP_CHECKS=1 ./bin/ginkgo -v ./tests/e2e ...
```

## Monitoring

It is possible to enable collection of logs and metrics from the
temporary networks used for e2e testing by:

 - Supplying `--start-metrics-collector` and `--start-logs-collector`
   as arguments to the test suite
 - Starting collectors in advance of a test run with `tmpnetctl
   start-metrics-collector` and ` tmpnetctl start-logs-collector`

Both methods require:

 - Collector config to be supplied as env vars:
   - `PROMETHEUS_URL`
   - `PROMETHEUS_PUSH_URL`
   - `PROMETHEUS_USERNAME`
   - `PROMETHEUS_PASSWORD`
   - `LOKI_URL`
   - `LOKI_PUSH_URL`
   - `LOKI_USERNAME`
   - `LOKI_PASSWORD`
 - The availability in the path of binaries for promtail and prometheus
   - Starting a development shell with `nix develop` is one way to
     ensure this and requires the installation of nix
     (e.g. `./scripts/run_task.sh install-nix`).

Once started, the collectors will continue to run in the background
until stopped by `tmpnetctl stop-metrics-collector` and `tmpnetctl stop-logs-collector`.

The results of collection will be viewable at
https://avalabs.grafana.net.

For more detail, see the [tmpnet docs](../fixture/tmpnet/README.md##monitoring).
