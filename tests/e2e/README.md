# Avalanche e2e test suites

- Works for any environments (e.g., local, test network).
- Compiles to a single binary with customizable configurations.

## Running tests

```bash
go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.0.0
ACK_GINKGO_RC=true ginkgo build ./tests/e2e
./tests/e2e/e2e.test --help

./tests/e2e/e2e.test \
--network-runner-grpc-endpoint="0.0.0.0:12340" \
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
    └── ping
        └── suites.go
```

`e2e.go` defines common configurations (e.g., network-runner client) for other test packages. `ping/suites.go` defines ping tests, annotated by `[Ping]`, which can be selected by `./tests/e2e/e2e.test --ginkgo.focus "\[Local\] \[Ping\]"`.
