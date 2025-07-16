# tmpnet - temporary network orchestration

This package implements a simple orchestrator for the avalanchego
nodes of a temporary network. Configuration is stored on disk, and
nodes run as independent processes whose process details are also
written to disk. Using the filesystem to store configuration and
process details allows for the `tmpnetctl` cli and e2e test fixture to
orchestrate the same temporary networks without the use of an rpc daemon.

## Table of Contents

- [What's in a name?](#whats-in-a-name)
- [Package details](#package-details)
- [Usage](#usage)
  - [Via tmpnetctl](#via-tmpnetctl)
  - [Simplifying usage with direnv](#simplifying-usage-with-direnv)
    - [Deprecated usage with e2e suite](#deprecated-usage-with-e2e-suite)
  - [Via code](#via-code)
- [Networking configuration](#networking-configuration)
- [Configuration on disk](#configuration-on-disk)
  - [Common networking configuration](#common-networking-configuration)
  - [Genesis](#genesis)
  - [Subnet and Chain configuration](#subnet-and-chain-configuration)
  - [Network env](#network-env)
  - [Node configuration](#node-configuration)
    - [Runtime config](#runtime-config)
    - [Flags](#flags)
    - [Process details](#process-details)
- [Monitoring](#monitoring)
  - [Example usage](#example-usage)
  - [Running collectors](#running-collectors)
  - [Metric collection configuration](#metric-collection-configuration)
  - [Log collection configuration](#log-collection-configuration)
  - [Labels](#labels)
  - [CI Collection](#ci-collection)
  - [Viewing](#viewing)
    - [Local networks](#local-networks)
    - [CI](#ci)

## What's in a name?
[Top](#table-of-contents)

The name of this package was originally `testnet` and its cli was
`testnetctl`. This name was chosen in ignorance that `testnet`
commonly refers to a persistent blockchain network used for testing.

To avoid confusion, the name was changed to `tmpnet` and its cli
`tmpnetctl`. `tmpnet` is short for `temporary network` since the
networks it deploys are likely to live for a limited duration in
support of the development and testing of avalanchego and its related
repositories.

## Package details
[Top](#table-of-contents)

The functionality in this package is grouped by logical purpose into
the following non-test files:

| Filename                    | Types          | Purpose                                                                |
|:----------------------------|:---------------|:-----------------------------------------------------------------------|
| flags/                      |                | Directory defining flags usable with both stdlib flags and spf13/pflag |
| flags/collector.go          |                | Defines flags configuring collection of logs and metrics               |
| flags/common.go             |                | Defines type definitions common across other files                     |
| flags/process_runtime.go    |                | Defines flags configuring the process node runtime                     |
| flags/runtime.go            |                | Defines flags configuring node runtime                                 |
| flags/start_network.go      |                | Defines flags configuring network start                                |
| tmpnetctl/                  |                | Directory containing main entrypoint for tmpnetctl command             |
| yaml/                       |                | Directory defining kubernetes resources in yaml format                 |
| check_monitoring.go         |                | Enables checking if logs and metrics were collected                    |
| defaults.go                 |                | Defines common default configuration                                   |
| detached_process_default.go |                | Configures detached processes for darwin and linux                     |
| detached_process_windows.go |                | No-op detached process configuration for windows                       |
| flagsmap.go                 | FlagsMap       | Simplifies configuration of avalanchego flags                          |
| genesis.go                  |                | Creates test genesis                                                   |
| kube.go                     |                | Library for Kubernetes interaction                                     |
| local_network.go            |                | Defines configuration for the default local network                    |
| monitor_kube.go             |                | Enables collection of logs and metrics from kube pods                  |
| monitor_processes.go        |                | Enables collection of logs and metrics from local processes            |
| network.go                  | Network        | Orchestrates and configures temporary networks                         |
| network_config.go           | Network        | Reads and writes network configuration                                 |
| network_test.go             |                | Simple test round-tripping Network serialization                       |
| node.go                     | Node           | Orchestrates and configures nodes                                      |
| node_config.go              | Node           | Reads and writes node configuration                                    |
| process_runtime.go          | ProcessRuntime | Orchestrates node processes                                            |
| start_kind_cluster.go       |                | Starts a local kind cluster                                            |
| subnet.go                   | Subnet         | Orchestrates subnets                                                   |
| utils.go                    |                | Defines shared utility functions                                       |

## Usage

### Via tmpnetctl
[Top](#table-of-contents)

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

#### Deprecated usage with e2e suite
[Top](#table-of-contents)

`tmpnetctl` was previously used to create temporary networks for use
across multiple e2e test runs. As the usage of temporary networks has
expanded to require subnets, that usage has been supplanted by the
`--reuse-network` flag defined for the e2e suite. It was easier to
support defining subnet configuration in the e2e suite in code than to
extend a cli tool like `tmpnetctl` to support similar capabilities.

### Simplifying usage with direnv
[Top](#table-of-contents)

The repo includes a [.envrc](../../../.envrc) that can be applied by
[direnv](https://direnv.net/) when in a shell. This will enable
`tmpnetctl` to be invoked directly (without a `./bin/` prefix ) and
without having to specify the `--avalanchego-path` or `--plugin-dir`
flags.

### Via code
[Top](#table-of-contents)

A temporary network can be managed in code:

```golang
network := &tmpnet.Network{                         // Configure non-default values for the new network
    DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
        Process: &tmpnet.ProcessRuntimeConfig{
            ReuseDynamicPorts: true,                // Configure process-based nodes to reuse a dynamically allocated API port when restarting
        },
    }
    DefaultFlags: tmpnet.FlagsMap{
        config.LogLevelKey: "INFO",                 // Change one of the network's defaults
    },
    Nodes: tmpnet.NewNodesOrPanic(5),               // Number of initial validating nodes
    Subnets: []*tmpnet.Subnet{                      // Subnets to create on the new network once it is running
        {
            Name: "xsvm-a",                         // User-defined name used to reference subnet in code and on disk
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
[Top](#table-of-contents)

By default, nodes in a temporary network will be started with staking and
API ports set to `0` to ensure that ports will be dynamically
chosen. The tmpnet fixture discovers the ports used by a given node
by reading the `[base-data-dir]/process.json` file written by
avalanchego on node start. The use of dynamic ports supports testing
with many temporary networks without having to manually select compatible
port ranges.

## Configuration on disk
[Top](#table-of-contents)

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
            ├── config.json                              // tmpnet configuration for the network
            ├── genesis.json                             // Genesis for all nodes
            ├── metrics.txt                              // Link for metrics and logs collected from the network (see: Monitoring)
            ├── network.env                              // Sets network dir env var to simplify network usage
            └── subnets                                  // Directory containing tmpnet subnet configuration
                ├── subnet-a.json                        // tmpnet configuration for subnet-a and its chain(s)
                └── subnet-b.json                        // tmpnet configuration for subnet-b and its chain(s)
```

### Common networking configuration
[Top](#table-of-contents)

Network configuration such as default flags (e.g. `--log-level=`),
runtime defaults (e.g. avalanchego path) and pre-funded private keys
are stored at `[network-dir]/config.json`. A default for a given flag
will only be applied to a node if that node does not itself set a
value for that flag.

### Genesis
[Top](#table-of-contents)

The genesis file is stored at `[network-dir]/genesis.json`. The
genesis file content will be generated with reasonable defaults if
not supplied. The content of the file is provided to each node via
the `--genesis-file-content` flag if a node does not set a value for
the flag.

### Subnet and chain configuration
[Top](#table-of-contents)

tmpnet configuration for a given subnet and its chain(s) is stored at
`[network-dir]/subnets/[subnet name].json`. Subnet configuration for
all subnets is provided to each node via the
`--subnet-config-content` flag if a node does not set a value for the
flag. Chain configuration for all chains is provided to each node via
the `--chain-config-content` flag where a node does not set a value
for the flag.

### Network env
[Top](#table-of-contents)

A shell script that sets the `TMPNET_NETWORK_DIR` env var to the
path of the network is stored at `[network-dir]/network.env`. Sourcing
this file (i.e. `source network.env`) in a shell will configure ginkgo
e2e and the `tmpnetctl` cli to target the network path specified in
the env var.

Set `TMPNET_ROOT_NETWORK_DIR` to specify the root network directory in
which to create the configuration directory of new networks
(e.g. `TMPNET_ROOT_NETWORK_DIR/[network-dir]`). The default network
root directory is `~/.tmpdir/networks`. Configuring the network root
directory is only relevant when creating new networks as the path of
existing networks will already have been set.

### Node configuration
[Top](#table-of-contents)

The data dir for a node is set by default to
`[network-path]/[node-id]`. A node can be configured to use a
non-default path by explicitly setting the `--data-dir`
flag.

#### Runtime config
[Top](#table-of-contents)

The details required to configure a node's execution are written to
`[network-path]/[node-id]/config.json`. This file contains the
runtime-specific details like the path of the avalanchego binary to
start the node with.

#### Flags
[Top](#table-of-contents)

All flags used to configure a node are written to
`[network-path]/[node-id]/flags.json` so that a node can be
configured with only a single argument:
`--config-file=/path/to/flags.json`. This simplifies node launch and
ensures all parameters used to launch a node can be modified by
editing the config file.

#### Process details
[Top](#table-of-contents)

The process details of a node are written by avalanchego to
`[base-data-dir]/process.json`. The file contains the PID of the node
process, the URI of the node's API, and the address other nodes can
use to bootstrap themselves (aka staking address).

## Monitoring
[Top](#table-of-contents)

Monitoring is an essential part of understanding the workings of a
distributed system such as avalanchego. The tmpnet fixture enables
collection of logs and metrics from temporary networks to a monitoring
stack (prometheus+loki+grafana) to enable results to be analyzed and
shared.

### Example usage
[Top](#table-of-contents)

```bash
# Start a nix shell to ensure the availability of promtail and prometheus.
nix develop

# Enable collection of logs and metrics
PROMETHEUS_USERNAME=<username> \
PROMETHEUS_PASSWORD=<password> \
LOKI_USERNAME=<username> \
LOKI_PASSWORD=<password> \
./bin/tmpnetctl start-metrics-collector
./bin/tmpnetctl start-logs-collector

# Network start emits link to grafana displaying collected logs and metrics
./bin/tmpnetctl start-network

# When done with the network, stop the collectors
./bin/tmpnetctl stop-metrics-collector
./bin/tmpnetctl stop-logs-collector
```

### Running collectors
[Top](#table-of-contents)

 - `tmpnetctl start-metrics-collector` starts `prometheus` in agent mode
   configured to scrape metrics from configured nodes and forward
   them to https://prometheus-poc.avax-dev.network.
   - Requires:
     - Credentials supplied as env vars:
       - `PROMETHEUS_USERNAME`
       - `PROMETHEUS_PASSWORD`
     - A `prometheus` binary available in the path
   - Once started, `prometheus` can be stopped by `tmpnetctl stop-metrics-collector`
 - `tmpnetctl start-logs-collector` starts `promtail` configured to collect logs
   from configured nodes and forward them to
   https://loki-poc.avax-dev.network.
   - Requires:
     - Credentials supplied as env vars:
       - `LOKI_USERNAME`
       - `LOKI_PASSWORD`
     - A `promtail` binary available in the path
   - Once started, `promtail` can be stopped by `tmpnetctl stop-logs-collector`
 - Starting a development shell with `nix develop` is one way to
   ensure availability of the necessary binaries and requires the
   installation of nix (e.g. `./scripts/run_task.sh install-nix`).

### Metric collection configuration
[Top](#table-of-contents)

When a node is started, configuration enabling collection of metrics
from the node is written to
`~/.tmpnet/prometheus/file_sd_configs/[network uuid]-[node id].json`.

### Log collection configuration
[Top](#table-of-contents)

Nodes log are stored at `~/.tmpnet/networks/[network id]/[node
id]/logs` by default, and can optionally be forwarded to loki with
promtail.

When a node is started, promtail configuration enabling
collection of logs for the node is written to
`~/.tmpnet/promtail/file_sd_configs/[network
uuid]-[node id].json`.

### Labels
[Top](#table-of-contents)

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
[Top](#table-of-contents)

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

### Viewing

#### Local networks
[Top](#table-of-contents)

When a network is started with tmpnet, a link to the [default grafana
instance](https://grafana-poc.avax-dev.network) will be
emitted. The dashboards will only be populated if prometheus and
promtail are running locally (as per previous sections) to collect
metrics and logs.

#### CI
[Top](#table-of-contents)

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
