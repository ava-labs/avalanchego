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

# Start a new network. Possible to specify the number of nodes (> 1) with --node-count.
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
`tmpnetctl` commands target the most recently deployed temporary
network.

#### Deprecated usage with e2e suite

`tmpnetctl` was previously used to create temporary networks for use
across multiple e2e test runs. As the usage of temporary networks has
expanded to require subnets, that usage has been supplanted by the
`--reuse-network` flag defined for the e2e suite. It was easier to
support defining subnet configuration in the e2e suite in code than to
extend a cli tool like `tmpnetctl` to support similar capabilities.

### Via code

A temporary network can be managed in code:

```golang
network := &tmpnet.Network{                   // Configure non-default values for the new network
    DefaultFlags: tmpnet.FlagsMap{
        config.LogLevelKey: "INFO",           // Change one of the network's defaults
    },
    Nodes: tmpnet.NewNodesOrPanic(5),           // Number of initial validating nodes
    Subnets: []*tmpnet.Subnet{                // Subnets to create on the new network once it is running
        {
            Name: "xsvm-a",                   // User-defined name used to reference subnet in code and on disk
            Chains: []*tmpnet.Chain{
                {
                    VMName: "xsvm",              // Name of the VM the chain will run, will be used to derive the name of the VM binary
                    Genesis: <genesis bytes>,    // Genesis bytes used to initialize the custom chain
                    PreFundedKey: <key>,         // (Optional) A private key that is funded in the genesis bytes
                    VersionArgs: "version-json", // (Optional) Arguments that prompt the VM binary to output version details in json format.
                                                 // If one or more arguments are provided, the resulting json output should include a field
                                                 // named `rpcchainvm` of type uint64 containing the rpc version supported by the VM binary.
                                                 // The version will be checked against the version reported by the configured avalanchego
                                                 // binary before network and node start.
                },
            },
            ValidatorIDs: <node ids>,         // The IDs of nodes that validate the subnet
        },
    },
}

_ := tmpnet.BootstrapNewNetwork(          // Bootstrap the network
    ctx,                                  // Context used to limit duration of waiting for network health
    ginkgo.GinkgoWriter,                  // Writer to report progress of initialization
    network,
    "",                                   // Empty string uses the default network path (~/tmpnet/networks)
    "/path/to/avalanchego",               // The path to the binary that nodes will execute
    "/path/to/plugins",                   // The path nodes will use for plugin binaries (suggested value ~/.avalanchego/plugins)
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
            └── subnets                                  // Directory containing subnet config for both avalanchego and tmpnet
                ├── subnet-a.json                        // tmpnet configuration for subnet-a and its chain(s)
                ├── subnet-b.json                        // tmpnet configuration for subnet-b and its chain(s)
                └── 2jRbWtaonb2RP8DEM5DBsd7o2o8d...RqNs9 // The ID of a subnet is the name of its configuration dir
                    └── config.json                      // avalanchego configuration for subnet
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

## Monitoring

Monitoring is an essential part of understanding the workings of a
distributed system such as avalanchego. The tmpnet fixture enables
collection of logs and metrics from temporary networks to a monitoring
stack (prometheus+loki+grafana) to enable results to be analyzed and
shared.

### Example usage

```bash
# Start prometheus to collect metrics
PROMETHEUS_ID=<id> PROMETHEUS_PASSWORD=<password> ./scripts/run_prometheus.sh

# Start promtail to collect logs
LOKI_ID=<id> LOKI_PASSWORD=<password> ./scripts/run_promtail.sh

# Network start emits link to grafana displaying collected logs and metrics
./build/tmpnetctl start-network
```

### Metrics collection

When a node is started, configuration enabling collection of metrics
from the node is written to
`~/.tmpnet/prometheus/file_sd_configs/[network uuid]-[node id].json`.

The `scripts/run_prometheus.sh` script starts prometheus in agent mode
configured to scrape metrics from configured nodes and forward the
metrics to a persistent prometheus instance. The script requires that
the `PROMETHEUS_ID` and `PROMETHEUS_PASSWORD` env vars be set. By
default the prometheus instance at
https://prometheus-poc.avax-dev.network will be targeted and
this can be overridden via the `PROMETHEUS_URL` env var.

### Log collection

Nodes log are stored at `~/.tmpnet/networks/[network id]/[node
id]/logs` by default, and can optionally be forwarded to loki with
promtail.

When a node is started, promtail configuration enabling
collection of logs for the node is written to
`~/.tmpnet/promtail/file_sd_configs/[network
uuid]-[node id].json`.

The `scripts/run_promtail.sh` script starts promtail configured to
collect logs from configured nodes and forward the results to loki. The
script requires that the `LOKI_ID` and `LOKI_PASSWORD` env vars be
set. By default the loki instance at
https://loki-poc.avax-dev.network will be targeted and this
can be overridden via the `LOKI_URL` env var.

### Labels

The logs and metrics collected for temporary networks will have the
following labels applied:

 - `network_uuid`
   - uniquely identifies a network across hosts
 - `node_id`
 - `is_ephemeral_node`
   - 'ephemeral' nodes are expected to run for only a fraction of the
     life of a network
 - `network_owner`
   - an arbitrary string that can be used to differentiate results
     when a CI job runs more than one network

When a network runs as part of a github CI job, the following
additional labels will be applied:

 - `gh_repo`
 - `gh_workflow`
 - `gh_run_id`
 - `gh_run_number`
 - `gh_run_attempt`
 - `gh_job_id`

These labels are sourced from Github Actions' `github` context as per
https://docs.github.com/en/actions/learn-github-actions/contexts#github-context.

### Viewing

#### Local networks

When a network is started with tmpnet, a link to the [default grafana
instance](https://grafana-poc.avax-dev.network) will be
emitted. The dashboards will only be populated if prometheus and
promtail are running locally (as per previous sections) to collect
metrics and logs.

#### CI

Collection of logs and metrics is enabled for CI jobs that use
tmpnet. Each job will execute a step titled `Notify of metrics
availability` that emits a link to grafana parametized to show results
for the job. Additional links to grafana parametized to show results
for individual network will appear in the logs displaying the start of
those networks.
