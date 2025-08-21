# C-Chain Re-Execution Benchmark

The C-Chain benchmarks support re-executing a range of mainnet C-Chain blocks against a provided snapshot of the current state as of some initial state.

AvalancheGo provides a [Taskfile](https://taskfile.dev/) with commands to manage the import/export of data required for re-execution (block range and current state) and triggering a benchmark run.

## Metrics

The C-Chain benchmarks export VM metrics to the same Grafana instance as AvalancheGo CI: https://grafana-poc.avax-dev.network/.

You can view granular C-Chain processing metrics with the label attached to this job (job="c-chain-reexecution") [here](https://grafana-poc.avax-dev.network/d/Gl1I20mnk/c-chain?orgId=1&from=now-5m&to=now&timezone=browser&var-datasource=P1809F7CD0C75ACF3&var-filter=job%7C%3D%7Cc-chain-reexecution&var-chain=C&refresh=10s).

Note: to ensure Prometheus gets a final scrape at the end of a run, the test will sleep for 2s greater than the 10s Prometheus scrape interval, which will cause short-running tests to appear to take much longer than expected. Additionally, the linked dashboard displays most metrics using a 1min rate, which means that very short running tests will not produce a very useful visualization.

For a realistic view, run the default C-Chain benchmark in the [final step](#run-default-c-chain-benchmark) or view the preview URL printed by the [c-chain-benchmark](../../../.github/workflows/c-chain-reexecution-benchmark.yml) job, which executes the block range [101, 250k].

## Configure Dev Environment

To set up your dev environment to run C-Chain benchmarks, run:

```bash
nix develop
```

If using AWS to push/pull S3 buckets, configure your AWS profile with the required access. The instructions here utilize the S3 bucket `s3://avalanchego-bootstrap-testing` in `us-east-2` under the Ava Labs Experimental AWS account.

To authenticate metrics collection (enabled by default), provide the Prometheus credentials referenced in the e2e [README](../../e2e/README.md#monitoring).

## Import Blocks

To import the first 200 blocks for re-execution, you can fetch the following directory from S3: `s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb/`:

```bash
task import-s3-to-dir SRC=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb/** DST=$HOME/exec-data/blocks
```

## Create C-Chain State Snapshot

To execute a range of blocks [N, N+K], we need an initial current state with the last accepted block of N-1. To generate this from scratch, simply execute the range of blocks [1, N-1] (genesis is "executed" vacuously, so do not provide 0 as the start block) locally starting from an empty state:

```bash
task reexecute-cchain-range CURRENT_STATE_DIR=$HOME/exec-data/current-state SOURCE_BLOCK_DIR=$HOME/exec-data/blocks START_BLOCK=1 END_BLOCK=100
```

This initializes a `current-state` subdirectory inside of `$HOME/exec-data`, which will contain two subdirectories `chain-data-dir` and `db`.

The `chain-data-dir` is the path passed in via `*snow.Context` to the VM as `snowContext.ChainDataDir`.
If the VM does not populate it, it may remain empty after a run.

The `db` directory is used to initialize the leveldb instance used to create two nested PrefixDBs: the database passed into `vm.Initialize(...)` and the database used by shared memory.
These two databases must be built on top of the same base database as documented in the shared memory [README](../../../chains/atomic/README.md#shared-database).

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
task reexecute-cchain-range CURRENT_STATE_DIR=$HOME/exec-data/current-state SOURCE_BLOCK_DIR=$HOME/exec-data/blocks START_BLOCK=101 END_BLOCK=200
```

Note: if you attempt to re-execute a second time on the same data set, it will fail because the current state has been updated to block 200.

Provide the parameters explicitly that we have just used locally:

```bash
task reexecute-cchain-range-with-copied-data EXECUTION_DATA_DIR=$HOME/reexec-data-params SOURCE_BLOCK_DIR=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-10k-ldb/** CURRENT_STATE_DIR=s3://avalanchego-bootstrap-testing/cchain-current-state-test/** START_BLOCK=101 END_BLOCK=10000
```

## Predefined Configs

To support testing the VM in multiple configurations, the benchmark supports a set of pre-defined configs passed via the Task variable ex. `CONFIG=archive`.

The currently supported options are: "default", "archive", and "firewood".

Note: to execute a benchmark with any of these options, double check to ensure you are using a compatible database via `CURRENT_STATE_DIR`. For example, attempting to execute the VM with Firewood on a database using the default configuration will refuse to startup.

This currently only supports pre-defined configs and not passing a full JSON blob in, so that we have a clear config name to use in the name of the sub-benchmark (used by GitHub Action Benchmark to separate historical results) and added as a label to exported Prometheus metrics.

## Run Default C-Chain Benchmark

To re-execute with an fresh copy, use the defaults provided in [Taskfile.yaml](../../../Taskfile.yml) to execute the range [101, 250k]:

```bash
task reexecute-cchain-range-with-copied-data EXECUTION_DATA_DIR=$HOME/reexec-data-defaults
```

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
    "source-block-dir": "s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-50m-ldb/**",
    "current-state-dir": "s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-30m/**"
}
```

## GitHub CLI

### Trigger a Single Job

To triggers runs conveniently, you can use the [GitHub CLI](https://cli.github.com/manual/gh_workflow_run) to trigger workflows.

To run the same workflow as above via the GitHub CLI write the desired input to a JSON file `input.json`

```json
{
    "runner": "blacksmith-4vcpu-ubuntu-2404",
    "config": "firewood",
    "start-block": 30000001,
    "end-block": 40000000,
    "source-block-dir": "s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-50m-ldb/**",
    "current-state-dir": "s3://avalanchego-bootstrap-testing/cchain-current-state-firewood-30m/**"
}
```

Then pass it to the GitHub CLI:

```bash
cat input.json | gh workflow run .github/workflows/c-chain-reexecution-benchmark-gh-native.yml --json
```

### Trigger Parallel Jobs

To execute multiple runs in parallel, you can re-run this with multiple inputs or define a single array in a JSON file `input_array.json`:

```json
[
    {
        "start-block": "101",
        "end-block": "200",
        "source-block-dir": "s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-1m-ldb/**",
        "current-state-dir": "s3://avalanchego-bootstrap-testing/cchain-current-state-hashdb-full-100/**"
    },
    {
        "start-block": "101",
        "end-block": "300",
        "source-block-dir": "s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-1m-ldb/**",
        "current-state-dir": "s3://avalanchego-bootstrap-testing/cchain-current-state-hashdb-full-100/**"
    }
]
```

Then pass the slice to the GitHub CLI, processing the array with `jq` and `xargs`:

```bash
cat input_array.json | jq -c '.[]' | xargs -p -n 1 gh workflow run .github/workflows/c-chain-reexecution-benchmark-gh-native.yml --json
```

Here the `-n 1` flag ensures each entry is processed individually and `-p` will prompt you to authorize each individual run. To execute without the prompt, simply remove the `-p` flag from the command above.
