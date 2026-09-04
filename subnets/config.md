# Subnet Configs

It is possible to provide parameters for a Subnet. Parameters here apply to all
chains in the specified Subnet.

AvalancheGo looks for files specified with `{subnetID}.json` under
`--subnet-config-dir` as documented
[here](https://build.avax.network/docs/nodes/configure/configs-flags#subnet-configs).

Here is an example of Subnet config file:

```json
{
  "validatorOnly": false,
  "snowParameters": {
    "k": 25,
    "alpha": 18
  }
}
```

## Parameters

### Private Subnet

#### `validatorOnly` (bool)

If `true` this node does not expose Subnet blockchain contents to non-validators
via P2P messages. Defaults to `false`.

Avalanche Subnets are public by default. It means that every node can sync and
listen ongoing transactions/blocks in Subnets, even they're not validating the
listened Subnet.

Subnet validators can choose not to publish contents of blockchains via this
configuration. If a node sets `validatorOnly` to true, the node exchanges
messages only with this Subnet's validators. Other peers will not be able to
learn contents of this Subnet from this node.

:::tip

This is a node-specific configuration. Every validator of this Subnet has to use
this configuration in order to create a full private Subnet.

:::

#### `allowedNodes` (string list)

If `validatorOnly=true` this allows explicitly specified NodeIDs to be allowed
to sync the Subnet regardless of validator status. Defaults to be empty.

:::tip

This is a node-specific configuration. Every validator of this Subnet has to use
this configuration in order to properly allow a node in the private Subnet.

:::

### ProposerVM Config

#### `proposerWindowMilliseconds` (uint)

The length of a single proposerVM proposer slot for the chains in this Subnet, in
milliseconds. Defaults to 5s (`5000`) when unset or `0`. When set, must be
between `1000` (1s) and `5000` (5s) inclusive.

```json
{ "proposerWindowMilliseconds": 1000 }
```

Most Subnets do not need this. It exists for one narrow case: small
proof-of-authority L1s where a single offline validator stalls a large share of
proposer slots and failover time matters more than extra rejected blocks.

Slots are `proposerWindowMilliseconds` apart. When a scheduled proposer is
offline, the chain stalls until the next live proposer's slot opens, so a smaller
window speeds failover recovery at the cost of more rejected blocks. The floor is 1s because the proposerVM block timestamp is
whole-second granular: the slot clock only ticks once per second, so a sub-second
window gains nothing without millisecond-granular timestamps.

:::caution

This is a network-wide consensus parameter, not a per-node tuning knob. Every
validator of this Subnet's chains must use the same `proposerWindowMilliseconds`.
Validators with different windows disagree about which proposer is expected for a
slot, reject each other's blocks, and break liveness. Roll it out to every
validator at once, the same way as an upgrade time. The primary network (P/C/X) is
unaffected and always uses the default.

:::

### Consensus Config

Subnet configs supports loading new consensus parameters or even consensus engines(Snowman or Simplex).
JSON keys are different from their matching `CLI` keys. The snow parameters of a Subnet default to the
same values used for the Primary Network, which are given [CLI Snow Parameters](https://build.avax.network/docs/nodes/configure/configs-flags#snow-parameters).

| CLI Key                           | JSON Key                                   |
| :-------------------------------- | :----------------------------------------- |
| --snow-sample-size                | `snowParameters.k`                         |
| --snow-quorum-size                | `snowParameters.alpha`                     |
| --snow-commit-threshold           | `snowParameters.beta`                      |
| --snow-concurrent-repolls         | `snowParameters.concurrentRepolls`         |
| --snow-optimal-processing         | `snowParameters.optimalProcessing`         |
| --snow-max-processing             | `snowParameters.maxOutstandingItems`       |
| --snow-max-time-processing        | `snowParameters.maxItemProcessingTime`     |
| --snow-avalanche-batch-size       | `snowParameters.batchSize`                 |
| --snow-avalanche-num-parents      | `snowParameters.parentSize`                |
| --simplex-max-network-delay       | `simplexParameters.maxNetworkDelay`        |
| --simplex-max-rebroadcast-wait    | `simplexParameters.maxRebroadcastWait`     |

### Gossip Configs

It's possible to define different Gossip configurations for each Subnet without
changing values for Primary Network. JSON keys of these
parameters are different from their matching `CLI` keys. These parameters
default to the same values used for the Primary Network. For more information
see [CLI Gossip Configs](https://build.avax.network/docs/nodes/configure/configs-flags#gossiping).

| CLI Key                                                 | JSON Key                               |
| :------------------------------------------------------ | :------------------------------------- |
| --consensus-accepted-frontier-gossip-validator-size     | gossipAcceptedFrontierValidatorSize    |
| --consensus-accepted-frontier-gossip-non-validator-size | gossipAcceptedFrontierNonValidatorSize |
| --consensus-accepted-frontier-gossip-peer-size          | gossipAcceptedFrontierPeerSize         |
| --consensus-on-accept-gossip-validator-size             | gossipOnAcceptValidatorSize            |
| --consensus-on-accept-gossip-non-validator-size         | gossipOnAcceptNonValidatorSize         |
| --consensus-on-accept-gossip-peer-size                  | gossipOnAcceptPeerSize                 |
