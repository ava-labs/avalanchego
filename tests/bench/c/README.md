# C-Chain Benchmark

The C-Chain benchmarks support re-executing a range of mainnet C-Chain blocks against a provided snapshot of the current state as of some starting point.

AvalancheGo provides a [Taskfile](https://taskfile.dev/) with commands to manage the import/export of data required for re-execution (block range and current state) and triggering a benchmark run.

The C-Chain benchmarks export VM metrics to the same Grafana instance as AvalancheGo CI: https://grafana-poc.avax-dev.network/.

## Configure Dev Environment

To set up your dev environment to run C-Chain benchmarks, run:

```bash
nix develop
```

## Import Blocks

To import the first 200 blocks for re-execution, you can fetch the following ZIP from S3: s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-200.zip:

```bash
task import-s3-to-dir S3_SRC=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-200.zip LOCAL_DST=$HOME/exec-data/blocks
```

## Create C-Chain State Snapshot

To execute a range of blocks [N, N+K], we need an initial current state with the last accepted block of N-1. To generate this from scratch, simply execute the range of blocks [0, N-1] locally starting from an empty state:

```bash
task reexecute-cchain-range EXECUTION_DATA_DIR=$HOME/exec-data START_BLOCK=1 END_BLOCK=100
```

This will initialize a `current-state` directory containing a base database for AvalancheGo provided as the VM's database and the database used for shared memory and a `chain-data-dir` subdirectory provided via `*snow.Context` as the `ChainDataDir` used by the VM.

This should produce the following directory structure:

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

If `chain-data-dir` is empty or non-existent, this indicates that the VM did not initialize or populate it, which will be the case when running with the default config and using `hashdb`.

After generating the `$HOME/exec-data/current-state` directory from executing the first segment of blocks, we can take a snapshot of the current state and push to S3 (or copy to another location locally if preferred) for re-use.

To export, run:

```bash
task export-dir-to-s3 LOCAL_SRC=$HOME/exec-data/current-state S3_DST=s3://avalanchego-bootstrap-testing/cchain-current-state-test
```

## Run C-Chain Benchmark

Now that we've pushed the current-state back to S3, we can run the target range of blocks [101, 200] either re-using the data we already have locally or copying all of the data including both the blocks and current state for a completely fresh run.

First, to run the block range using our locally available data, run:

```bash
task reexecute-cchain-range EXECUTION_DATA_DIR=$HOME/exec-data START_BLOCK=101 END_BLOCK=200
```

To re-execute with an entirely fresh copy, either use the defaults provided in [Taskfile.yaml](../../../Taskfile.yml) or provide the parameters we've already used here.

Use the defaults to execute the range [101, 200]:

```bash
task reexecute-cchain-range-with-copied-data EXECUTION_DATA_DIR=$HOME/reexec-data-defaults
```

Provide the parameters explicitly that we have just used locally:

```bash
task reexecute-cchain-range-with-copied-data EXECUTION_DATA_DIR=$HOME/reexec-data-params SOURCE_BLOCK_DIR=s3://avalanchego-bootstrap-testing/cchain-mainnet-blocks-200.zip CURRENT_STATE_DIR=s3://avalanchego-bootstrap-testing/cchain-current-state-test START_BLOCK=101 END_BLOCK=200
```

## Future Additions

TODO:
- update C-Chain dashboards
- provide task to copy block range from 50m blocks to smaller db
- provide task to pull blocks from the network