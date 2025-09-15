# Load Testing

The `load` package is a framework for performing load testing 
against an instance of the C-Chain. It allows for simulation of various
transaction scenarios to test network performance, transaction throughput, and system
resilience under different workloads.

This package also comes with `main`, a subpackage executable which runs a load test against
an instance of the C-Chain. For information on how to run the executable, refer to
the `main` [README.md](./main/README.md).

## Prerequisites

Using the `load` package has so far been coupled with `tmpnet`, a framework that
enables orchestration of temporary AvalancheGo networks. For more details as to
its capabilities and configuration, refer to the `tmpnet` [README.md](../fixture/tmpnet/README.md).

## Components

### Worker

Workers represent accounts which will be used for load testing. Each worker consists of a private key, nonce, and a network client - these values are used to create a wallet for load testing.

### Wallet

A wallet manages account state and transaction lifecycle for a single account.

The main function of wallets is to send transactions; wallets take signed transactions
and send them to the network while monitoring block headers to detect transaction inclusion. A wallet
considers a transaction successful if its execution succeeded, and returns an error otherwise. 
Upon confirming a transaction was successful, a wallet increments its nonce by one, ensuring its account
state matches that of the network.

Wallets are not thread-safe and each account should have at most one wallet
instance associated with it. Having multiple wallets associated with a single 
account, or using an account outside of a wallet, can cause synchronization 
issues. This risks a wallet's state becoming inconsistent with the network, 
potentially leading to failed transactions.

Wallets are not thread-safe and each account should have at most one wallet instance associated with it.

### Generator

The generator executes multiple tests concurrently against the network. The key features of the 
generator are as follows:

- **Parallel Execution**: Runs a goroutine per wallet for concurrent test execution.
- **Timeout Management**: Supports both overall load test timeout and a timeout per-test.
- **Error Recovery**: Automatically recovers from test failures to ensure continuous load generation.
- **Metrics**: Creates metrics during wallet initialization and tracks performance throughout execution.

The generator starts a goroutine for each wallet to execute tests asynchronously, maximizing throughput while maintaining isolation between accounts.

### Tests

The `load` package performs load testing by continuously running a series of tests. This approach provides the following benefits:

- **Correctness**: Each test validates transaction execution and state changes, ensuring the network behaves as expected under load
- **Reproducibility**: Standardized test scenarios with consistent parameters enable repeatable performance measurements and regression detection

Each test must satisfy the following interface for compatibility with the load generator:

```go
type Test interface {
    // Run should create a signed transaction and broadcast it to the network via wallet.
    Run(tc tests.TestContext, wallet *Wallet)
}
```

#### Available Test Types

The `load` package provides a comprehensive suite of test types designed to stress different aspects of EVM execution. Each test targets specific performance characteristics and resource usage patterns.

| Test Type      | Description                                            |
| -------------- | ------------------------------------------------------ |
| Transfer       | Transfers 1 nAVAX to a random account                  |
| Read           | Reads from a set of storage slots                      |
| Write          | Writes to a set of storage slots                       |
| Modify         | Modifies a set of non-empty storage slots              |
| Hash           | Hash a value for N iterations                          |
| Deploy         | Deploy an instance of the Dummy contract               |
| Large Calldata | Compute the sum of a large calldata tx                 |
| TrieStress     | Performs insert operations on TrieDB                   |
| ERC20          | Transfers 1 unit of an ERC20 token to a random account |

Additionally, there is a `RandomTest` which executes one of the aforementioned tests 
at random, with certain tests having a higher probability of being executed than others.
This test is particular useful for mimicking the diverse transactions patterns seen on blockchains like the C-Chain.

### Metrics

The package exposes Prometheus metrics for comprehensive load test monitoring:

- **`txs_issued`** (Counter): Total number of transactions submitted to the network
- **`tx_issuance_latency`** (Histogram): Time from transaction creation to network submission
- **`tx_confirmation_latency`** (Histogram): Time from network submission to block confirmation  
- **`tx_total_latency`** (Histogram): End-to-end time from creation to confirmation

These metrics are registered with the registry passed into the load generator during initialization and are updated via the wallets during test execution.

