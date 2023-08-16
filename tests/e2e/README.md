# Avalanche e2e test suites

- Works with fixture-managed networks.
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

## Adding tests

Define any flags/configurations in [`e2e.go`](./e2e.go).

Create a new package to implement feature-specific tests, or add tests to an existing package. For example:

```
.
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

## Testing against a persistent network

By default, a new ephemeral test network will be started before each
test run. When developing e2e tests, it may be helpful to create a
persistent test network to test against. This can increase the speed
of iteration by removing the requirement to start a new network for
every invocation of the test under development.

To use a persistent network:

```bash
# From the root of the avalanchego repo

# Build the testnetctl binary
$ ./scripts/build_testnetctl.sh

# Start a new network
$ ./build/testnetctl start-network --avalanchego-path=/path/to/avalanchego
...
Started network 1000 @ /home/me/.testnetctl/networks/1000

Configure testnetctl to target this network by default with one of the following statements:
 - source /home/me/.testnetctl/networks/1000/network.env
 - export TESTNETCTL_NETWORK_DIR=/home/me/.testnetctl/networks/1000
 - export TESTNETCTL_NETWORK_DIR=/home/me/.testnetctl/networks/latest

# Start a new test run using the persistent network
ginkgo -v ./tests/e2e -- \
    --avalanchego-path=/path/to/avalanchego \
    --ginkgo-focus-file=[name of file containing test] \
    --use-persistent-network \
    --network-dir=/path/to/network

# It is also possible to set the AVALANCHEGO_PATH env var instead of supplying --avalanchego-path
# and to set TESTNETCTL_NETWORK_DIR instead of supplying --network-dir.
```

See the testnet fixture [README](../fixture/testnet/README.md) for more details.
