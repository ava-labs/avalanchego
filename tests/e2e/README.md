# Avalanche e2e test suites

- Works with fixture-managed temporary networks.
- Compiles to a single binary with customizable configurations.

## Running tests

```bash
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.0.0
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

./tests/e2e/e2e.test \
--avalanchego-path=./build/avalanchego
```

See [`tests.e2e.sh`](../../scripts/tests.e2e.sh) for an example.

### Filtering test execution with labels

In cases where a change can be verified against only a subset of
tests, it is possible to filter the tests that will be executed by the
declarative labels that have been applied to them. Available labels
are defined as constants in [`describe.go`](./describe.go) with names
of the form `*Label`. The following example runs only those tests that
primarily target the X-Chain:


```bash
./tests/e2e/e2e.test \
  --avalanchego-path=./build/avalanchego \
  --ginkgo.label-filter=x
```

The ginkgo docs provide further detail on [how to compose label
queries](https://onsi.github.io/ginkgo/#spec-labels).

## Adding tests

Define any flags/configurations in [`e2e.go`](./e2e.go).

Create a new package to implement feature-specific tests, or add tests to an existing package. For example:

```
tests
└── e2e
    ├── README.md
    ├── e2e.go
    ├── e2e_test.go
    └── x
        └── transfer.go
            └── virtuous.go
```

`e2e.go` defines common configuration for other test
packages. `x/transfer/virtuous.go` defines X-Chain transfer tests,
labeled with `x`, which can be selected by `./tests/e2e/e2e.test
--ginkgo.label-filter "x"`.

## Testing against an existing network

By default, a new temporary test network will be started before each
test run and stopped at the end of the run. When developing e2e tests,
it may be helpful to create a temporary network that can be used
across multiple test runs. This can increase the speed of iteration by
removing the requirement to start a new network for every invocation
of the test under development.

To create a temporary network for use across test runs:

```bash
# From the root of the avalanchego repo

# Build the tmpnetctl binary
$ ./scripts/build_tmpnetctl.sh

# Start a new network
$ ./build/tmpnetctl start-network --avalanchego-path=/path/to/avalanchego
...
Started network /home/me/.tmpnet/networks/20240306-152305.924531 (UUID: abaab590-b375-44f6-9ca5-f8a6dc061725)

Configure tmpnetctl and the test suite to target this network by default
with one of the following statements:
 - source /home/me/.tmpnet/networks/20240306-152305.924531/network.env
 - export TMPNET_NETWORK_DIR=/home/me/.tmpnet/networks/20240306-152305.924531
 - export TMPNET_NETWORK_DIR=/home/me/.tmpnet/networks/latest

# Start a new test run using the existing network
ginkgo -v ./tests/e2e -- \
    --avalanchego-path=/path/to/avalanchego \
    --ginkgo.focus-file=[name of file containing test] \
    --use-existing-network \
    --network-dir=/path/to/network

# It is also possible to set the AVALANCHEGO_PATH env var instead of supplying --avalanchego-path
# and to set TMPNET_NETWORK_DIR instead of supplying --network-dir.
```

See the tmpnet fixture [README](../fixture/tmpnet/README.md) for more details.

## Skipping bootstrap checks

By default many tests will attempt to bootstrap a new node with the
post-test network state. While this is a valuable activity to perform
in CI, it can add considerable latency to test development. To disable
these bootstrap checks during development, set the
`E2E_SKIP_BOOTSTRAP_CHECKS` env var to a non-empty value:

```bash
E2E_SKIP_BOOTSTRAP_CHECKS=1 ginkgo -v ./tests/e2e ...
```
