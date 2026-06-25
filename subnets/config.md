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

#### `proposerWindowDuration` (duration, nanoseconds)

The length of a single proposerVM proposer slot for the chains in this Subnet.
Defaults to 5s (`5000000000`) when unset or `0`.

This is a Go `time.Duration` decoded from JSON as an **integer number of
nanoseconds** (e.g. `1000000000` for 1s), the same encoding as the other Subnet
duration fields such as `snowParameters.maxItemProcessingTime` and
`simplexParameters.maxNetworkDelay`. When set, it must be between 50ms
(`50000000`) and 5s (`5000000000`) inclusive; `0` means use the default.

The proposerVM schedules each validator into a proposer slot, and slots are
`proposerWindowDuration` apart. When a scheduled proposer is offline, the chain
cannot accept a block until the next live proposer's slot opens, so a smaller
window shortens the stall on validator failover (faster recovery for CFT/PoA
L1s), at the cost of more rejected blocks when a slot closes before a block
propagates.

:::warning

The proposerVM block timestamp is currently **whole-second granular**, so today a
sub-second window gives no real failover benefit (the stall quantizes up to the
next second) and only raises the block-rejection rate. **~1s is the practical
floor.** Smaller values down to the 50ms hard floor are accepted to leave room for
a future proposerVM with millisecond-precision timestamps, but on current versions
they are the operator's responsibility.

:::

:::warning

This is a network-wide consensus parameter, not a per-node tuning knob. Every
validator of this Subnet's chains MUST use the same `proposerWindowDuration`.
Validators with different windows disagree about which proposer is expected for a
slot, reject each other's blocks, and break liveness. Roll it out identically to
every validator, the same way as an upgrade time. The primary network (P/C/X) is
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
