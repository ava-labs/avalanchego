# tmpnet - temporary network orchestration

This package implements a simple orchestrator for the avalanchego
nodes of a temporary network. Configuration is stored on disk, and
nodes run as independent processes whose process details are also
written to disk. Using the filesystem to store configuration and
process details allows for the `tmpnetctl` cli and e2e test fixture to
orchestrate the same temporary networks without the use of an rpc daemon.

## What's in a name?

The name of this package was originally `testnet` and its cli was
`testnetctl`. This name was chosen in ignorance that `testnet`
commonly refers to a persistent blockchain network used for testing.

To avoid confusion, the name was changed to `tmpnet` and its cli
`tmpnetctl`. `tmpnet` is short for `temporary network` since the
networks it deploys are likely to live for a limited duration in
support of the development and testing of avalanchego and its related
repositories.

## Package details

The functionality in this package is grouped by logical purpose into
the following non-test files:

| Filename          | Types       | Purpose                                        |
|:------------------|:------------|:-----------------------------------------------|
| defaults.go       |             | Defines common default configuration           |
| flags.go          | FlagsMap    | Simplifies configuration of avalanchego flags  |
| genesis.go        |             | Creates test genesis                           |
| network.go        | Network     | Orchestrates and configures temporary networks |
| network_config.go | Network     | Reads and writes network configuration         |
| node.go           | Node        | Orchestrates and configures nodes              |
| node_config.go    | Node        | Reads and writes node configuration            |
| node_process.go   | NodeProcess | Orchestrates node processes                    |
| subnet.go         | Subnet      | Orchestrates subnets                           |
| utils.go          |             | Defines shared utility functions               |

## Usage

### Via tmpnetctl

A temporary network can be managed by the `tmpnetctl` cli tool:

```bash
# From the root of the avalanchego repo

# Build the tmpnetctl binary
$ ./scripts/build_tmpnetctl.sh

# Start a new network
$ ./build/tmpnetctl start-network --avalanchego-path=/path/to/avalanchego
...
Started network /home/me/.tmpnet/networks/20240306-152305.924531 (UUID: abaab590-b375-44f6-9ca5-f8a6dc061725)

Configure tmpnetctl to target this network by default with one of the following statements:
 - source /home/me/.tmpnet/networks/20240306-152305.924531/network.env
 - export TMPNET_NETWORK_DIR=/home/me/.tmpnet/networks/20240306-152305.924531
 - export TMPNET_NETWORK_DIR=/home/me/.tmpnet/networks/latest

# Stop the network
$ ./build/tmpnetctl stop-network --network-dir=/path/to/network
```

Note the export of the path ending in `latest`. This is a symlink that
is set to the last network created by `tmpnetctl start-network`. Setting
the `TMPNET_NETWORK_DIR` env var to this symlink ensures that
`tmpnetctl` commands and e2e execution with
`--use-existing-network` will target the most recently deployed temporary
network.

### Via code

A temporary network can be managed in code:

```golang
network := &tmpnet.Network{                   // Configure non-default values for the new network
    DefaultFlags: tmpnet.FlagsMap{
        config.LogLevelKey: "INFO",           // Change one of the network's defaults
    },
    Subnets: []*tmpnet.Subnet{                // Subnets to create on the new network once it is running
        {
            Name: "xsvm-a",                   // User-defined name used to reference subnet in code and on disk
            Chains: []*tmpnet.Chain{
                {
                    VMName: "xsvm",           // Name of the VM the chain will run, will be used to derive the name of the VM binary
                    Genesis: <genesis bytes>, // Genesis bytes used to initialize the custom chain
                    PreFundedKey: <key>,      // (Optional) A private key that is funded in the genesis bytes
                },
            },
        },
    },
}

_ := tmpnet.StartNewNetwork(              // Start the network
    ctx,                                  // Context used to limit duration of waiting for network health
    ginkgo.GinkgoWriter,                  // Writer to report progress of initialization
    network,
    "",                                   // Empty string uses the default network path (~/tmpnet/networks)
    "/path/to/avalanchego",               // The path to the binary that nodes will execute
    "/path/to/plugins",                   // The path nodes will use for plugin binaries (suggested value ~/.avalanchego/plugins)
    5,                                    // Number of initial validating nodes
)

uris := network.GetNodeURIs()

// Use URIs to interact with the network

// Stop all nodes in the network
network.Stop(context.Background())
```

## Networking configuration

By default, nodes in a temporary network will be started with staking and
API ports set to `0` to ensure that ports will be dynamically
chosen. The tmpnet fixture discovers the ports used by a given node
by reading the `[base-data-dir]/process.json` file written by
avalanchego on node start. The use of dynamic ports supports testing
with many temporary networks without having to manually select compatible
port ranges.

## Configuration on disk

A temporary network relies on configuration written to disk in the following structure:

```
HOME
└── .tmpnet                                              // Root path for the temporary network fixture
    ├── prometheus                                       // Working directory for a metrics-scraping prometheus instance
    │   └── file_sd_configs                              // Directory containing file-based service discovery config for prometheus
    ├── promtail                                         // Working directory for a log-collecting promtail instance
    │   └── file_sd_configs                              // Directory containing file-based service discovery config for promtail
    └── networks                                         // Default parent directory for temporary networks
        └── 20240306-152305.924531                       // The timestamp of creation is the name of a network's directory
            ├── NodeID-37E8UK3x2YFsHE3RdALmfWcppcZ1eTuj9 // The ID of a node is the name of its data dir
            │   ├── chainData
            │   │   └── ...
            │   ├── config.json                          // Node runtime configuration
            │   ├── db
            │   │   └── ...
            │   ├── flags.json                           // Node flags
            │   ├── logs
            │   │   └── ...
            │   ├── plugins
            │   │   └── ...
            │   └── process.json                         // Node process details (PID, API URI, staking address)
            ├── chains
            │   ├── C
            │   │   └── config.json                      // C-Chain config for all nodes
            │   └── raZ51bwfepaSaZ1MNSRNYNs3ZPfj...U7pa3
            │       └── config.json                      // Custom chain configuration for all nodes
            ├── config.json                              // Common configuration (including defaults and pre-funded keys)
            ├── genesis.json                             // Genesis for all nodes
            ├── network.env                              // Sets network dir env var to simplify network usage
            └── subnets                                  // Parent directory for subnet definitions
                ├─ subnet-a.json                         // Configuration for subnet-a and its chain(s)
                └─ subnet-b.json                         // Configuration for subnet-b and its chain(s)
```

### Common networking configuration

Network configuration such as default flags (e.g. `--log-level=`),
runtime defaults (e.g. avalanchego path) and pre-funded private keys
are stored at `[network-dir]/config.json`. A given default will only
be applied to a new node on its addition to the network if the node
does not explicitly set a given value.

### Genesis

The genesis file is stored at `[network-dir]/genesis.json` and
referenced by default by all nodes in the network. The genesis file
content will be generated with reasonable defaults if not
supplied. Each node in the network can override the default by setting
an explicit value for `--genesis-file` or `--genesis-file-content`.

### Chain configuration

The chain configuration for a temporary network is stored at
`[network-dir]/chains/[chain alias or ID]/config.json` and referenced
by all nodes in the network. The C-Chain config will be generated with
reasonable defaults if not supplied. X-Chain and P-Chain will use
implicit defaults. The configuration for custom chains can be provided
with subnet configuration and will be writen to the appropriate path.

Each node in the network can override network-level chain
configuration by setting `--chain-config-dir` to an explicit value and
ensuring that configuration files for all chains exist at
`[custom-chain-config-dir]/[chain alias or ID]/config.json`.

### Network env

A shell script that sets the `TMPNET_NETWORK_DIR` env var to the
path of the network is stored at `[network-dir]/network.env`. Sourcing
this file (i.e. `source network.env`) in a shell will configure ginkgo
e2e and the `tmpnetctl` cli to target the network path specified in
the env var.

Set `TMPNET_ROOT_DIR` to specify the root directory in which to create
the configuration directory of new networks
(e.g. `$TMPNET_ROOT_DIR/[network-dir]`). The default root directory is
`~/.tmpdir/networks`. Configuring the root directory is only relevant
when creating new networks as the path of existing networks will
already have been set.

### Node configuration

The data dir for a node is set by default to
`[network-path]/[node-id]`. A node can be configured to use a
non-default path by explicitly setting the `--data-dir`
flag.

#### Runtime config

The details required to configure a node's execution are written to
`[network-path]/[node-id]/config.json`. This file contains the
runtime-specific details like the path of the avalanchego binary to
start the node with.

#### Flags

All flags used to configure a node are written to
`[network-path]/[node-id]/flags.json` so that a node can be
configured with only a single argument:
`--config-file=/path/to/flags.json`. This simplifies node launch and
ensures all parameters used to launch a node can be modified by
editing the config file.

#### Process details

The process details of a node are written by avalanchego to
`[base-data-dir]/process.json`. The file contains the PID of the node
process, the URI of the node's API, and the address other nodes can
use to bootstrap themselves (aka staking address).

## Metrics

### Prometheus configuration

When nodes are started, prometheus configuration for each node is
written to `~/.tmpnet/prometheus/file_sd_configs/` with a filename of
`[network uuid]-[node id].json`. Prometheus can be configured to
scrape the nodes as per the following example:

```yaml
scrape_configs:
  - job_name: "avalanchego"
    metrics_path: "/ext/metrics"
    file_sd_configs:
      - files:
        - '/home/me/.tmpnet/prometheus/file_sd_configs/*.yaml'
```

### Viewing metrics

When  a network  is  started with  `tmpnet`, a  grafana  link for  the
network's metrics will be emitted.

The metrics emitted by temporary networks configured with tmpnet will
have the following labels applied:

 - `network_uuid`
 - `node_id`
 - `is_ephemeral_node`
 - `network_owner`

When a tmpnet network runs as part of github CI, the following
additional labels will be applied:

 - `gh_repo`
 - `gh_workflow`
 - `gh_run_id`
 - `gh_run_number`
 - `gh_run_attempt`
 - `gh_job_id`
