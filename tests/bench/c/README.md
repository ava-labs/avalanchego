# C-Chain Benchmark

The C-Chain benchmarks support re-executing a range of mainnet C-Chain blocks against a provided snapshot of the current state as of some initial state.

AvalancheGo provides a [Taskfile](https://taskfile.dev/) with commands to manage the import/export of data required for re-execution (block range and current state) and triggering a benchmark run.

## Metrics

The C-Chain benchmarks export VM metrics to the same Grafana instance as AvalancheGo CI: https://grafana-poc.avax-dev.network/.

You can view granular C-Chain processing metrics with the label attached to this job (job="c-chain-reexecution") [here](https://grafana-poc.avax-dev.network/d/c-chain-processing-custom-v8/c-chain-processing?orgId=1&from=now-5m&to=now&timezone=browser&var-datasource=P1809F7CD0C75ACF3&var-filter=job%7C%3D%7Cc-chain-reexecution&var-chain=C&refresh=10s).

Note: to ensure Prometheus gets a final scrape at the end of a run, the test will sleep for 2s greater than the 10s Prometheus scrape interval, which will cause short-running tests to appear to take much longer than expected. Additionally, the linked dashboard displays most metrics using a 1min rate, which means that very short running tests will not produce a very useful visualization.

For a realistic view, run the default C-Chain benchmark in the [final step](#run-default-c-chain-benchmark) or view the preview URL printed by the [c-chain-benchmark](../../../.github/workflows/c-chain-benchmark.yml) job, which executes the block range [101, 250k].

## Configure Dev Environment

To set up your dev environment to run C-Chain benchmarks, run:

```bash
nix develop
```

If using AWS to push/pull S3 buckets, configure your AWS profile with the required access. The instructions here utilize the S3 bucket `s3://avalanchego-bootstrap-testing` in `us-east-2` under the Ava Labs Experimental AWS account.

To authenticate metrics collection (enabled by default), provide the Prometheus credentials referenced in the e2e [README](../../e2e/README.md#monitoring).

## Import Blocks

To import the first 200 blocks for re-execution, you can fetch the following ZIP from S3: `s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb.zip`:

```bash
task import-s3-to-dir S3_SRC=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb.zip LOCAL_DST=$HOME/exec-data/blocks
```

## Create C-Chain State Snapshot

To execute a range of blocks [N, N+K], we need an initial current state with the last accepted block of N-1. To generate this from scratch, simply execute the range of blocks [1, N-1] (genesis is "executed" vacuously, so do not provide 0 as the start block) locally starting from an empty state:

```bash
task reexecute-cchain-range EXECUTION_DATA_DIR=$HOME/exec-data SOURCE_BLOCK_DIR=$HOME/exec-data/blocks START_BLOCK=1 END_BLOCK=100
```

This initailizes a `current-state` subdirectory inside of `$HOME/exec-data`. This will contain two subdirectories `chain-data-dir` and `db`. The `chain-data-dir` is the path passed in via `*snow.Context` to the VM as `snowContext.ChainDataDir`. If the VM does not populate it, it may remain empty after a run. The `db` directory is used to initialize the leveldb instance used to create two nested PrefixDBs the database passed into `vm.Initialize(...)` and the database used by shared memory. These two databases must be built on top of the same base database as documented in the shared memory [README](../../../chains/atomic/README.md#shared-database).

For reference, the expected directory structure is:

```
$HOME/exec-data
├── blocks
│   ├── 000001.log
│   ├── CURRENT
│   ├── LOCK
│   ├── LOG
│   └── MANIFEST-000000
└── current-state
    ├── chain-data-dir
    └── db
        ├── 000002.ldb
        ├── 000003.log
        ├── CURRENT
        ├── CURRENT.bak
        ├── LOCK
        ├── LOG
        └── MANIFEST-000004
```

After generating the `$HOME/exec-data/current-state` directory from executing the first segment of blocks, we can take a snapshot of the current state and push to S3 (or copy to another location locally if preferred) for re-use.

Run the export task:

```bash
task export-dir-to-s3 LOCAL_SRC=$HOME/exec-data/current-state/ S3_DST=s3://avalanchego-bootstrap-testing/cchain-current-state-test/
```

## Run C-Chain Benchmark

Now that we've pushed the current-state back to S3, we can run the target range of blocks [101, 200] either re-using the data we already have locally or copying all of the data including both the blocks and current state for a completely fresh run.

First, to run the block range using our locally available data, run:

```bash
task reexecute-cchain-range EXECUTION_DATA_DIR=$HOME/exec-data SOURCE_BLOCK_DIR=$HOME/exec-data/blocks START_BLOCK=101 END_BLOCK=200
```

Note: if you attempt to re-execute a second time on the same data set, it will fail because the current state has been updated to block 200.

Provide the parameters explicitly that we have just used locally:

```bash
task reexecute-cchain-range-with-copied-data EXECUTION_DATA_DIR=$HOME/reexec-data-params SOURCE_BLOCK_DIR=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb.zip CURRENT_STATE_DIR=s3://avalanchego-bootstrap-testing/cchain-current-state-test/** START_BLOCK=101 END_BLOCK=10000
```

## Run Default C-Chain Benchmark

To re-execute with an fresh copy, use the defaults provided in [Taskfile.yaml](../../../Taskfile.yml) to execute the range [101, 250k]:

```bash
task reexecute-cchain-range-with-copied-data EXECUTION_DATA_DIR=$HOME/reexec-data-defaults
```
