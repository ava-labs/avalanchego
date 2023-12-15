# tmpnet (temporary network fixture)

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

| Filename    | Types   | Purpose                                       |
|:------------|:--------|:----------------------------------------------|
| defaults.go | <none>  | Default configuration                         |
| network.go  | Network | Network-level orchestration and configuration |
| node.go     | Node    | Node-level orchestration and configuration    |
| util.go     | <none>  | Shared utility functions                      |

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
Started network 1000 @ /home/me/.tmpnet/networks/1000

Configure tmpnetctl to target this network by default with one of the following statements:
 - source /home/me/.tmpnet/networks/1000/network.env
 - export TMPNET_NETWORK_DIR=/home/me/.tmpnet/networks/1000
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
network, _ := tmpnet.StartNetwork(
    ctx,                                           // Context used to limit duration of waiting for network health
    ginkgo.GinkgoWriter,                           // Writer to report progress of network start
    "",                                            // Use default root dir (~/.tmpnet)
    &tmpnet.Network{
        DefaultRuntime: tmpnet.NodeRuntimeConfig{
            ExecPath: "/path/to/avalanchego",      // Defining the avalanchego exec path is required
        },
    },
    5,                                             // Number of initial validating nodes
    50,                                            // Number of pre-funded keys to create
)

uris := network.GetURIs()

// Use URIs to interact with the network

// Stop all nodes in the network
network.Stop()
```

If non-default node behavior is required, the `Network` instance
supplied to `StartNetwork()` can be initialized with explicit node
configuration and by supplying a nodeCount argument of `0`:

```golang
network, _ := tmpnet.StartNetwork(
    ctx,
    ginkgo.GinkgoWriter,
    "",
    &tmpnet.Network{
        DefaultRuntime: tmpnet.NodeRuntimeConfig{
            ExecPath: "/path/to/avalanchego",
        },
        Nodes: []*Node{
            {                                                       // node1 configuration is customized
                Flags: FlagsMap{                                    // Any and all node flags can be configured here
                    config.DataDirKey: "/custom/path/to/node/data",
                }
            },
        },
        {},                                                         // node2 uses default configuration
        {},                                                         // node3 uses default configuration
        {},                                                         // node4 uses default configuration
        {},                                                         // node5 uses default configuration
    },
    0,                                                              // Node count must be zero when setting node config
    50,
)
```

Further examples of code-based usage are located in the [e2e
tests](../../e2e/e2e_test.go).

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
    └── networks                                         // Default parent directory for temporary networks
        └── 1000                                         // The networkID is used to name the network dir and starts at 1000
            ├── NodeID-37E8UK3x2YFsHE3RdALmfWcppcZ1eTuj9 // The ID of a node is the name of its data dir
            │   ├── chainData
            │   │   └── ...
            │   ├── config.json                          // Node flags
            │   ├── db
            │   │   └── ...
            │   ├── logs
            │   │   └── ...
            │   ├── plugins
            │   │   └── ...
            │   └── process.json                         // Node process details (PID, API URI, staking address)
            ├── chains
            │   └── C
            │       └── config.json                      // C-Chain config for all nodes
            ├── defaults.json                            // Default flags and configuration for network
            ├── genesis.json                             // Genesis for all nodes
            ├── network.env                              // Sets network dir env to simplify use of network
            └── ephemeral                                // Parent directory for ephemeral nodes (e.g. created by tests)
                └─ NodeID-FdxnAvr4jK9XXAwsYZPgWAHW2QnwSZ // Data dir for an ephemeral node
                   └── ...

```

### Default flags and configuration

The default avalanchego node flags (e.g. `--staking-port=`) and
default configuration like the avalanchego path are stored at
`[network-dir]/defaults.json`. The value for a given defaulted flag
will be set on initial and subsequently added nodes that do not supply
values for a given defaulted flag.

### Genesis

The genesis file is stored at `[network-dir]/genesis.json` and
referenced by default by all nodes in the network. The genesis file
content will be generated with reasonable defaults if not
supplied. Each node in the network can override the default by setting
an explicit value for `--genesis-file` or `--genesis-file-content`.

### C-Chain config

The C-Chain config for a temporary network is stored at
`[network-dir]/chains/C/config.json` and referenced by default by all
nodes in the network. The C-Chain config will be generated with
reasonable defaults if not supplied. Each node in the network can
override the default by setting an explicit value for
`--chain-config-dir` and ensuring the C-Chain config file exists at
`[chain-config-dir]/C/config.json`.

TODO(marun) Enable configuration of X-Chain and P-Chain.

### Network env

A shell script that sets the `TMPNET_NETWORK_DIR` env var to the
path of the network is stored at `[network-dir]/network.env`. Sourcing
this file (i.e. `source network.env`) in a shell will configure ginkgo
e2e and the `tmpnetctl` cli to target the network path specified in
the env var.

### Node configuration

The data dir for a node is set by default to
`[network-path]/[node-id]`. A node can be configured to use a
non-default path by explicitly setting the `--data-dir`
flag.

#### Flags

All flags used to configure a node are written to
`[network-path]/[node-id]/config.json` so that a node can be
configured with only a single argument:
`--config-file=/path/to/config.json`. This simplifies node launch and
ensures all parameters used to launch a node can be modified by
editing the config file.

#### Process details

The process details of a node are written by avalanchego to
`[base-data-dir]/process.json`. The file contains the PID of the node
process, the URI of the node's API, and the address other nodes can
use to bootstrap themselves (aka staking address).
