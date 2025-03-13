This document provides details about the configuration options available for the PlatformVM.

## Standard Configurations

In order to specify a configuration for the PlatformVM, you need to define a `Config` struct and its parameters. The default values for these parameters are:

| Option                   | Type     | Default |
| ------------------------ | -------- | ------- |
| `network`                          | `Network`       | `DefaultNetwork` |
| `block-cache-size`                 | `int`          | `64 * units.MiB` |
| `tx-cache-size`                    | `int`          | `128 * units.MiB` |
| `transformed-subnet-tx-cache-size` | `int`          | `4 * units.MiB` |
| `reward-utxos-cache-size`         | `int`          | `2048` |
| `chain-cache-size`                | `int`          | `2048` |
| `chain-db-cache-size`             | `int`          | `2048` |
| `block-id-cache-size`             | `int`          | `8192` |
| `fx-owner-cache-size`             | `int`          | `4 * units.MiB` |
| `subnet-to-l1-conversion-cache-size` | `int`          | `4 * units.MiB` |
| `l1-weights-cache-size`           | `int`          | `16 * units.KiB` |
| `l1-inactive-validators-cache-size` | `int`          | `256 * units.KiB` |
| `l1-subnet-id-node-id-cache-size` | `int`          | `16 * units.KiB` |
| `checksums-enabled`               | `bool`         | `false` |
| `mempool-prune-frequency`         | `time.Duration` | `30 * time.Minute` |

Default values are overridden only if explicitly specified in the config.

## Network Configuration

The Network configuration defines parameters that control the network's gossip and validator behavior.

### Parameters

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max-validator-set-staleness` | `time.Duration` | `1 minute` | Maximum age of a validator set used for peer sampling and rate limiting |
| `target-gossip-size` | `int` | `20 * units.KiB` | Target number of bytes to send when pushing transactions or responding to transaction pull requests |
| `push-gossip-percent-stake` | `float64` | `0.9` | Percentage of total stake to target in the initial gossip round. Higher stake nodes are prioritized to minimize network messages |
| `push-gossip-num-validators` | `int` | `100` | Number of validators to push transactions to in the initial gossip round |
| `push-gossip-num-peers` | `int` | `0` | Number of peers to push transactions to in the initial gossip round |
| `push-regossip-num-validators` | `int` | `10` | Number of validators for subsequent gossip rounds after the initial push |
| `push-regossip-num-peers` | `int` | `0` | Number of peers for subsequent gossip rounds after the initial push |
| `push-gossip-discarded-cache-size` | `int` | `16384` | Size of the cache storing recently dropped transaction IDs from mempool to avoid re-pushing |
| `push-gossip-max-regossip-frequency` | `time.Duration` | `30 * time.Second` | Maximum frequency limit for re-gossiping a transaction |
| `push-gossip-frequency` | `time.Duration` | `500 * time.Millisecond` | Frequency of push gossip rounds |
| `pull-gossip-poll-size` | `int` | `1` | Number of validators to sample during pull gossip rounds |
| `pull-gossip-frequency` | `time.Duration` | `1500 * time.Millisecond` | Frequency of pull gossip rounds |
| `pull-gossip-throttling-period` | `time.Duration` | `10 * time.Second` | Time window for throttling pull requests |
| `pull-gossip-throttling-limit` | `int` | `2` | Maximum number of pull queries allowed per validator within the throttling window |
| `expected-bloom-filter-elements` | `int` | `8 * 1024` | Expected number of elements when creating a new bloom filter. Larger values increase filter size |
| `expected-bloom-filter-false-positive-probability` | `float64` | `0.01` | Target probability of false positives after inserting the expected number of elements. Lower values increase filter size |
| `max-bloom-filter-false-positive-probability` | `float64` | `0.05` | Threshold for bloom filter regeneration. Filter is refreshed when false positive probability exceeds this value |

### Details

The configuration is divided into several key areas:

- **Validator Set Management**: Controls how fresh the validator set must be for network operations. The staleness setting ensures the network operates with reasonably current validator information.
- **Gossip Size Controls**: Manages the size of gossip messages to maintain efficient network usage while ensuring reliable transaction propagation.
- **Push Gossip Configuration**: Defines how transactions are initially propagated through the network, with emphasis on reaching high-stake validators first to optimize network coverage.
- **Pull Gossip Configuration**: Controls how nodes request transactions they may have missed, including throttling mechanisms to prevent network overload.
- **Bloom Filter Settings**: Configures the trade-off between memory usage and false positive rates in transaction filtering, with automatic filter regeneration when accuracy degrades.
