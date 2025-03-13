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

| Filename                    | Types       | Purpose                                                     |
|:----------------------------|:------------|:------------------------------------------------------------|
| check_monitoring.go         |             | Enables checking if logs and metrics were collected         |
| defaults.go                 |             | Defines common default configuration                        |
| detached_process_default.go |             | Configures detached processes for darwin and linux          |
| detached_process_windows.go |             | No-op detached process configuration for windows            |
| flags.go                    | FlagsMap    | Simplifies configuration of avalanchego flags               |
| genesis.go                  |             | Creates test genesis                                        |
| kube.go                     |             | Library for Kubernetes interaction                          |
| local_network.go            |             | Defines configuration for the default local network         |
| monitor_processes.go        |             | Enables collection of logs and metrics from local processes |
| network.go                  | Network     | Orchestrates and configures temporary networks              |
| network_config.go           | Network     | Reads and writes network configuration                      |
| network_test.go             |             | Simple test round-tripping Network serialization            |
| node.go                     | Node        | Orchestrates and configures nodes                           |
| node_config.go              | Node        | Reads and writes node configuration                         |
| node_process.go             | NodeProcess | Orchestrates node processes                                 |
| start_kind_cluster.go       |             | Starts a local kind cluster                                 |
| subnet.go                   | Subnet      | Orchestrates subnets                                        |
| utils.go                    |             | Defines shared utility functions                            |

## Usage

### Via tmpnetctl

A temporary network can be managed by the `tmpnetctl` cli tool:

```bash
# From the root of the avalanchego repo

# Start a new network. Possible to specify the number of nodes (> 1) with --node-count.
$ ./bin/tmpnetctl start-network --avalanchego-path=/path/to/avalanchego
...
Started network /home/me/.tmpnet/networks/20240306-152305.924531 (UUID: abaab590-b375-44f6-9ca5-f8a6dc061725)

Configure tmpnetctl to target this network by default with one of the following statements:
 - source /home/me/.tmpnet/networks/20240306-152305.924531/network.env
 - export TMPNET_NETWORK_DIR=/home/me/.tmpnet/networks/20240306-152305.924531
 - export TMPNET_NETWORK_DIR=/home/me/.tmpnet/networks/latest

# Stop the network
$ ./bin/tmpnetctl stop-network --network-dir=/path/to/network
```

Note the export of the path ending in `latest`. This is a symlink that
is set to the last network created by `tmpnetctl start-network`. Setting
the `TMPNET_NETWORK_DIR` env var to this symlink ensures that
`tmpnetctl` commands target the most recently deployed temporary
network.

### Simplifying usage with direnv

The repo includes a [.envrc](../../../.envrc) that can be applied by
[direnv](https://direnv.net/) when in a shell. This will enable
`tmpnetctl` to be invoked directly (without a `./bin/` prefix ) and
without having to specify the `--avalanchego-path` or `--plugin-dir`
flags.

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
    DefaultRuntimeConfig{
        ReuseDynamicPorts: true,              // Configure process-based nodes to reuse a dynamically allocated API port when restarting
    },
    DefaultFlags: tmpnet.FlagsMap{
        config.LogLevelKey: "INFO",           // Change one of the network's defaults
    },
    Nodes: tmpnet.NewNodesOrPanic(5),         // Number of initial validating nodes
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
            ├── metrics.txt                              // Link for metrics and logs collected from the network (see: Monitoring)
            ├── network.env                              // Sets network dir env var to simplify network usage
            └── subnets                                  // Directory containing subnet config for both avalanchego and tmpnet
                ├── subnet-a.json                        // tmpnet configuration for subnet-a and its chain(s)
                ├── subnet-b.json                        // tmpnet configuration for subnet-b and its chain(s)
                └── 2jRbWtaonb2RP8DEM5DBsd7...RqNs9.json // avalanchego configuration for subnet with ID 2jRbWtao...RqNs9
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

### Subnet configuration

The subnet configuration for a temporary network is stored at
`[network-dir]/subnets/[subnet ID].json` and referenced by all
nodes in the network.

Each node in the network can override network-level subnet
configuration by setting `--subnet-config-dir` to an explicit value
and ensuring that configuration files for all chains exist at
`[custom-subnet-config-dir]/[subnet ID].json`.

### Chain configuration

The chain configuration for a temporary network is stored at
`[network-dir]/chains/[chain alias or ID]/config.json` and referenced
by all nodes in the network. The C-Chain config will be generated with
reasonable defaults if not supplied. X-Chain and P-Chain will use
implicit defaults. The configuration for custom chains can be provided
with subnet configuration and will be written to the appropriate path.

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
# Start a nix shell to ensure the availability of promtail and prometheus.
nix develop

# Enable collection of logs and metrics
PROMETHEUS_USERNAME=<username> \
PROMETHEUS_PASSWORD=<password> \
LOKI_USERNAME=<username> \
LOKI_PASSWORD=<password> \
./bin/tmpnetctl start-collectors

# Network start emits link to grafana displaying collected logs and metrics
./bin/tmpnetctl start-network

# When done with the network, stop the collectors
./bin/tmpnetctl stop-collectors
```

### Starting collectors

Collectors for logs and metrics can be started by `tmpnetctl
start-collectors`:

 - Requires that the following env vars be set
   - `PROMETHEUS_USERNAME`
   - `PROMETHEUS_PASSWORD`
   - `LOKI_USERNAME`
   - `LOKI_PASSWORD`
 - Requires that binaries for promtail and prometheus be available in the path
   - Starting a development shell with `nix develop` is one way to
     ensure this and requires the [installation of
     nix](https://github.com/DeterminateSystems/nix-installer?tab=readme-ov-file#install-nix).
 - Starts prometheus in agent mode configured to scrape metrics from
   configured nodes and forward them to
   https://prometheus-poc.avax-dev.network.
 - Starts promtail configured to collect logs from configured nodes
   and forward them to https://loki-poc.avax-dev.network.

### Stopping collectors

Collectors for logs and metrics can be stopped by `tmpnetctl
stop-collectors`:

### Metrics collection

When a node is started, configuration enabling collection of metrics
from the node is written to
`~/.tmpnet/prometheus/file_sd_configs/[network uuid]-[node id].json`.

### Log collection

Nodes log are stored at `~/.tmpnet/networks/[network id]/[node
id]/logs` by default, and can optionally be forwarded to loki with
promtail.

When a node is started, promtail configuration enabling
collection of logs for the node is written to
`~/.tmpnet/promtail/file_sd_configs/[network
uuid]-[node id].json`.

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

### CI Collection

A [custom github
action](../../../.github/actions/run-monitored-tmpnet-cmd/action.yml)
exists to simplify collection of logs and metrics from CI. The action
takes care of invoking a nix shell to ensure the availability of
binary dependencies, configures tmpnet to collect metrics and ensures
that the tmpnet path is collected as a github artifact to aid in troubleshooting.

Example usage:

```yaml
- name: Run e2e tests

  # A qualified path is required for use outside of avalanchego
  # e.g. `ava-labs/avalanchego/.github/actions/run-monitored-tmpnet-cmd@[sha or tag]`
  uses: ./.github/actions/run-monitored-tmpnet-cmd #

  with:
    # This needs to be the path to a bash script
    run: ./scripts/tests.e2e.sh

    # Env vars for the script need to be provided via run_env as a space-separated string
    # e.g. `MY_VAR1=foo MY_VAR2=bar`
    run_env: E2E_SERIAL=1

    # Sets the prefix of the artifact containing the tmpnet network dir for this job.
    # Only required if a workflow uses this action more than once so that each artifact
    # will have a unique name.
    artifact_prefix: e2e

    # These credentials are mandatory
    prometheus_username: ${{ secrets.PROMETHEUS_ID || '' }}
    prometheus_password: ${{ secrets.PROMETHEUS_PASSWORD || '' }}
    loki_username: ${{ secrets.LOKI_ID || '' }}
    loki_password: ${{ secrets.LOKI_PASSWORD || '' }}
```

The action requires a flake.nix file in the repo root that enables
availability of promtail and prometheus. The following is a minimal
flake that inherits from the avalanchego flake:

```nix
{
  # To use:
  #  - install nix: https://github.com/DeterminateSystems/nix-installer?tab=readme-ov-file#install-nix
  #  - run `nix develop` or use direnv (https://direnv.net/)
  #    - for quieter direnv output, set `export DIRENV_LOG_FORMAT=`

  description = "VM development environment";

  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2405.*.tar.gz";
    # Make sure to set a SHA or tag to the desired version
    avalanchego.url = "github:ava-labs/avalanchego?ref=[sha or tag]";
  };

  outputs = { self, nixpkgs, avalanchego, ... }:
    let
      allSystems = builtins.attrNames avalanchego.devShells;
      forAllSystems = nixpkgs.lib.genAttrs allSystems;
    in {
      devShells = forAllSystems (system: {
        default = avalanchego.devShells.${system}.default;
      });
    };
}
```

The action expects to be able to run bin/tmpnetctl from the root of
the repository. A suggested version of this script:

```bash
#!/usr/bin/env bash

set -euo pipefail

# Ensure the go command is run from the root of the repository
REPO_ROOT=$(cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
cd "${REPO_ROOT}"

# Set AVALANCHE_VERSION
source ./scripts/versions.sh

# Install if not already available
if command -v tmpnetctl &2> /dev/null; then
  # An explicit version is required since a main package can't be included as a dependency of the go module.
  go install github.com/ava-labs/avalanchego/tests/fixture/tmpnet/tmpnetctl@${AVALANCHE_VERSION}
fi
tmpnetctl "${@}"
```

### Viewing

#### Local networks

When a network is started with tmpnet, a link to the [default grafana
instance](https://grafana-poc.avax-dev.network) will be
emitted. The dashboards will only be populated if prometheus and
promtail are running locally (as per previous sections) to collect
metrics and logs.

#### CI

Collection of logs and metrics is enabled for CI jobs that use
tmpnet. Each job will execute a step including the script
`notify-metrics-availability.sh` that emits a link to grafana
parameterized to show results for the job.

Additional links to grafana parameterized to show results for
individual network will appear in the logs displaying the start of
those networks.

In cases where a given job uses private networks in addition to the
usual shared network, it may be useful to parameterize the
[run_monitored_tmpnet_action](../../../.github/actions/run-monitored-tmpnet-cmd/action.yml)
github action with `filter_by_owner` set to the owner string for the
shared network. This ensures that the link emitted by the annotation
displays results for only the shared network of the job rather than
mixing results from all the networks started for the job.
