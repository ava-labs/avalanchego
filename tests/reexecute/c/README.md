# C-Chain Re-Execution Benchmark

The C-Chain benchmarks support re-executing a range of mainnet C-Chain blocks against a provided snapshot of the current state as of some initial state.

AvalancheGo provides a [Taskfile](https://taskfile.dev/) with commands to manage the import/export of data required for re-execution (block range and current state) and triggering a benchmark run.

## Prerequisites

Configuring your AvalancheGo dev environment requires:

- Nix
- AWS Credentials
- Prometheus Credentials

### Nix Shell
To install Nix, refer to the AvalancheGo [flake.nix](../../../flake.nix) file for installation instructions.

To set up your shell environment, run:

```bash
nix develop
```

### AWS Access
This walkthrough assumes AWS access to the S3 bucket `s3://avalanchego-bootstrap-testing` in the Ava Labs Experimental Account in `us-east-2`.

To authenticate metrics collection (disabled by default), provide the Prometheus credentials referenced in the e2e [README](../../e2e/README.md#monitoring).

Okta -> AWS Access Portal -> Experimental -> Access Keys

```bash
export AWS_ACCESS_KEY_ID=<AWS_ACCESS_KEY_ID>
export AWS_SECRET_ACCESS_KEY=<AWS_SECRET_ACCESS_KEY>
export AWS_SESSION_TOKEN=<AWS_SESSION_TOKEN>
```

Note: the AWS Access Keys page provides the above credentials, but does not include setting the region parameter. To access the bucket, set the region via:

```bash
export AWS_REGION=us-east-2
```

### Metrics Collection

If running locally, there are three options for metrics collection:

- `METRICS_MODE=disabled`: no metrics are available.
- `METRICS_MODE=server-only`: starts a Prometheus server exporting VM metrics. A
  link to the metrics endpoint is logged during execution.
- `METRICS_MODE=full`: starts both a Prometheus server exporting VM metrics and
  a Prometheus collector. A link to the corresponding Grafana dashboard is
  logged during execution.

When utilizing the `full` options, follow the instructions in the e2e [README](../../e2e/README.md#monitoring) to set the required Prometheus environment variables.

Running the re-execution test in CI will always set `METRICS_MODE=full`.

## Quick Start

Let's run the default benchmark to get started. Make sure that you have completed the [Prerequisites](#prerequisites) section because it is required to copy the data from S3.

Decide what directory you want to use as a working directory and set the parameter `EXECUTION_DATA_DIR`. To re-execute a range of blocks, we need to copy the blocks themselves and the initial state of the chain, so these will be copied into `EXECUTION_DATA_DIR`.

[Taskfile](https://taskfile.dev/) supports reading arguments via both environment variables and named arguments on the command line, so we'll set `EXECUTION_DATA_DIR` and use the defaults for the remainder of the parameters:

```bash
export EXECUTION_DATA_DIR=$HOME/.reexecute-cchain/default
task reexecute-cchain-range-with-copied-data
```

This performs the following steps:

1. Copy a block database into `$EXECUTION_DATA_DIR/blocks` (first 1m blocks)
2. Copy the current state as of the default task's initial height into `$EXECUTION_DATA_DIR/current-state` (state as of height 100) using the VM's default config (hashdb full)
3. Build and execute a Golang Benchmark a single time to execute the default block range (block range [101, 250k])

The final output displays the top-level metrics from the benchmark run, which is simply mgas/s for the C-Chain:

```
[09-02|16:25:21.481] INFO vm-executor c/vm_reexecute_test.go:383 executing block {"height": 250000, "eta": "0s"}
[09-02|16:25:21.483] INFO vm-executor c/vm_reexecute_test.go:401 finished executing sequence
[09-02|16:25:21.485] INFO c-chain-reexecution c/vm_reexecute_test.go:194 shutting down VM
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/txpool/legacypool/legacypool.go:449 Transaction pool stopped
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:966 Closing quit channel
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:969 Stopping Acceptor
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:972 Acceptor queue drained                   t="4.359µs"
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:975 Shutting down sender cacher
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:979 Closing scope
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:983 Waiting for background processes to complete
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:1002 Shutting down state manager
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:1007 State manager shut down                  t=198ns
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> core/blockchain.go:1012 Blockchain stopped
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> eth/backend.go:404 Stopped shutdownTracker
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> eth/backend.go:407 Closed chaindb
[09-02|16:25:21.485] INFO <2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5 Chain> eth/backend.go:409 Stopped EventMux
[09-02|16:25:21.485] INFO c-chain-reexecution c/vm_reexecute_test.go:181 shutting down DB
BenchmarkReexecuteRange/[101,250000]-Config-archive-6         	       1	        85.29 mgas/s
PASS
ok  	github.com/ava-labs/avalanchego/tests/reexecute/c	313.560s
```

## How to Create and Use a Re-Execution Snapshot

Let's walk through how to create a state snapshot for re-execution from scratch and how to use it. To do so, we'll walk through:

- Importing the required block range
- Executing the initial range [1, N]
- Exporting the state snapshot to S3
- Re-executing from where we left off on the current state
- Re-executing by importing the current state from S3

### Generate Initial State Snapshot

To generate our initial state snapshot locally, we will:

- Import the block range including at least blocks in [1, N]
- Execute the block range [1, N]

First, we will import the first 10k blocks. To see what block databases are available, you can check the contents of the S3 bucket with:

```bash
s5cmd ls s3://avalanchego-bootstrap-testing | grep blocks
```

In this case, we will use the directory: `s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb/`. To import it, run the import task:

```bash
export EXECUTION_DATA_DIR=$HOME/.reexecute-cchain/walkthrough
task import-s3-to-dir SRC=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb/** DST=$EXECUTION_DATA_DIR/blocks
```

Next, we need to want to execute the range of blocks [1, N]. We use 1 as the initial start block, since VM initialization vacuously executes the genesis block.

We can execute an arbitrary range [N, N+K] provided we have the required inputs:

- `CURRENT_STATE_DIR` - the directory with the current state as of last accepted block N-1
- `BLOCK_DIR` - the directory containing a LevelDB instance with the required block range [N, N+K]
- `START_BLOCK` - the first block to execute in the range (inclusive)
- `END_BLOCK` - the last block to execute in the range (inclusive)

To generate this from scratch, we can use a directory path for `CURRENT_STATE_DIR` that is initially empty:

```bash
export CURRENT_STATE_DIR=$EXECUTION_DATA_DIR/current-state
export BLOCK_DIR=$EXECUTION_DATA_DIR/blocks
task reexecute-cchain-range START_BLOCK=1 END_BLOCK=100
```

This initializes the contents of `$EXECUTION_DATA_DIR/current-state` to include two subdirectories:

- `chain-data-dir`
- `db`

The `chain-data-dir` is the path passed in via `*snow.Context` to the VM as `snowContext.ChainDataDir`. If the VM does not populate it, it may remain empty after a run.

The `db` directory is used to initialize the leveldb instance used to create two nested PrefixDBs: the database passed into `vm.Initialize(...)` and the database used by shared memory.
These two databases must be built on top of the same base database as documented in the shared memory [README](../../../chains/atomic/README.md#shared-database).

The expected directory structure will look something like:

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

### Export Current State to S3

After generating the `$EXECUTION_DATA_DIR/current-state` directory from executing the first segment of blocks, we can take a snapshot of the current state and push to S3 (or copy to another location including a local directory if preferred) for re-use.

Since we are already using the S3 bucket `s3://avalanchego-bootstrap-testing`, we'll re-use it here. We'll use the `export-dir-to-s3` task, which takes in two parameters:

- `SRC` - local path to recursive copy contents from
- `DST` - S3 bucket destination path

To avoid clobbering useful data, `export-dir-to-s3` will first attempt to check that the destination does not yet exist and that the path does not have any nesting. For example, `s3://avalanchego-bootstrap-testing/target-dir/` is valid, but `s3://avalanchego-bootstrap-testing/nested/target-dir/` is invalid because it contains two levels of nesting.

As a result, if you run into a warning using this command, check the current contents using either the AWS Console or `s5cmd ls` and pick a valid, unused prefix.

```bash
task export-dir-to-s3 SRC=$EXECUTION_DATA_DIR/current-state/ DST=s3://avalanchego-bootstrap-testing/cchain-current-state-test/
```

### Re-Execute C-Chain Range

Now that we've pushed the current-state back to S3, we can run the target range of blocks [101, 200] either re-using the data we already have locally or copying the data in fresh using the `reexecute-cchain-range-with-copied-data` task.

First, let's continue executing using the already available `CURRENT_STATE_DIR` and `BLOCK_DIR`. Since we have already exported these values, the task will pick them up and we can set only the `START_BLOCK` and `END_BLOCK` parameters:

```bash
task reexecute-cchain-range START_BLOCK=101 END_BLOCK=200
```

Note: if you attempt to re-execute a second time on the same data set, it will fail because the current state has been updated to block 200.

### Re-Execute C-Chain Range with Copied Data

Next, we can re-execute the same range using the `CURRENT_STATE_DIR` that we exported to S3 using `reexecute-cchain-with-copied-data`.

This time we will copy the same block directory from S3 and copy the state snapshot that we just exported into S3 to pick up re-execution from where we left off.

Specify the following parameters:

- `EXECUTION_DATA_DIR` - local path to copy the block and current state directories
- `BLOCK_DIR_SRC` - source path to copy the blocks from (supports both local directory and S3 URI)
- `CURRENT_STATE_DIR_SRC` - source path to copy the current state directory from (supports both local directory and S3 URI)
- `START_BLOCK` - first block to execute (inclusive)
- `END_BLOCK` - final block to execute (inclusive)

We'll use a new `EXECUTION_DATA_DIR` for this run to avoid conflicts with previous runs from this walkthrough:

```bash
task reexecute-cchain-range-with-copied-data EXECUTION_DATA_DIR=$HOME/.reexecute-cchain/reexecute-with-copied-data BLOCK_DIR_SRC=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb/** CURRENT_STATE_DIR_SRC=s3://avalanchego-bootstrap-testing/cchain-current-state-test/** START_BLOCK=101 END_BLOCK=10000
```

## Predefined Configs

To support testing the VM in multiple configurations, the benchmark supports a set of pre-defined configs passed via the Task variable ex. `CONFIG=archive`.

The currently supported options are: "default", "archive", and "firewood".

To execute a benchmark with any of these options, you must use a compatible `CURRENT_STATE_DIR` or `CURRENT_STATE_DIR_SRC` or the VM will refuse to start with an incompatible existing database and newly provided config.

The `CONFIG` parameter currently only supports pre-defined configs and not passing a full JSON blob in, so that we can define corresponding names for each config option. The config name is attached as a label to the exported metrics and included in the name of the sub-benchmark (used by GitHub Action Benchmark to separate historical results with different configs).

## Metrics

The C-Chain benchmarks export VM metrics to the same Grafana instance as AvalancheGo CI: https://grafana-poc.avax-dev.network/.

To export metrics for a local run, simply set the Taskfile variable `METRICS_MODE=full` either via environment variable or passing it at the command line.

You can view granular C-Chain processing metrics with the label attached to this job (job="c-chain-reexecution") [here](https://grafana-poc.avax-dev.network/d/Gl1I20mnk/c-chain?orgId=1&from=now-5m&to=now&timezone=browser&var-datasource=P1809F7CD0C75ACF3&var-filter=job%7C%3D%7Cc-chain-reexecution&var-chain=C&refresh=10s).

To attach additional labels to the metrics from a local run, set the Taskfile variable `LABELS` to a comma separated list of key value pairs (ex. `LABELS=user=alice,os=ubuntu`).

Note: to ensure Prometheus gets a final scrape at the end of a run, the test will sleep for 2s greater than the 10s Prometheus scrape interval, which will cause short-running tests to appear to take much longer than expected. Additionally, the linked dashboard displays most metrics using a 1min rate, which means that very short running tests will not produce a very useful visualization.

For a realistic view, run the default C-Chain benchmark in the [Quick Start](#quick-start) or view the preview URL printed by the [c-chain-benchmark](../../../.github/workflows/c-chain-reexecution-benchmark-gh-native.yml) job, which executes the block range [101, 250k].

## CI

Benchmarks are run via [c-chain-benchmark-gh-native](../../../.github/workflows/c-chain-reexecution-benchmark-gh-native.yml) and [c-chain-reexecution-benchmark-container](../../../.github/workflows/c-chain-reexecution-benchmark-container.yml).

To run on our GitHub [Actions Runner Controller](https://github.com/actions/actions-runner-controller) installation, we need to specify a container and add an extra installation step. Unfortunately, once the YAML has been updated to include this field at all, there is no way to populate it dynamically to skip the extra layer of containerization. To support both ARC and GH Native jobs (including Blacksmith runners) without this unnecessary layer, we separate this into two separate workflows with their own triggers.

The runner options are:
- native GH runner options (ex. `ubuntu-latest`, more information [here](https://docs.github.com/en/actions/concepts/runners/github-hosted-runners))
- Actions Runner Controller labels (ex. `avalanche-avalanchego-runner-2tier`)
- Blacksmith labels (ex. `blacksmith-4vcpu-ubuntu-2404`, more information [here](https://docs.blacksmith.sh/blacksmith-runners/overview#runner-tags))

Both workflows provide three triggers:

- `manual_workflow`
- `pull_request`
- `schedule`

The manual workflow takes in all parameters specified by the user. To more easily specify a CI matrix and avoid GitHub's pain inducing matrix syntax, we define simple JSON files with the exact set of configs to run for each `pull_request` and `schedule` trigger. To add a new job for either of these triggers, simply define the entry in JSON and add it to run on the desired workflow.

For example, to add a new Firewood benchmark to execute the block range [30m, 40m] on a daily basis, follow the instructions above to generate the Firewood state as of block height 30m, export it to S3, and add the following entry under the `schedule` include array in the [GH Native JSON file](../../../.github/workflows/c-chain-reexecution-benchmark-gh-native.json).

```json
{
    "runner": "blacksmith-4vcpu-ubuntu-2404",
    "config": "firewood",
    "start-block": 30000001,
    "end-block": 40000000,
    "block-dir-src": "s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-50m-ldb/**",
    "current-state-dir-src": "s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-30m/**",
    "timeout-minutes": 1440
}
```

## Trigger Workflow Dispatch with GitHub CLI

To triggers runs conveniently, you can use the [GitHub CLI](https://cli.github.com/manual/gh_workflow_run) to trigger workflows.

Note: passing JSON to the GitHub CLI requires all key/value pairs as strings, so ensure that any number parameters are quoted as strings or you will see the error:

```bash
could not parse provided JSON: json: cannot unmarshal number into Go value of type string
```

Copy your desired parameters as JSON into a file or write it out on the command line:

```json
{
    "runner": "blacksmith-4vcpu-ubuntu-2404",
    "config": "firewood",
    "start-block": "101",
    "end-block": "200",
    "block-dir-src": "s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb/**",
    "current-state-dir-src": "s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-100/**",
    "timeout-minutes": "5"
}
```

Then pass it to the GitHub CLI:

```bash
cat input.json | gh workflow run .github/workflows/c-chain-reexecution-benchmark-gh-native.yml --json
```
