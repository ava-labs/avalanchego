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
between `50` and `5000` (5s) inclusive.

```json
{ "proposerWindowMilliseconds": 1000 }
```

Most Subnets do not need this. It exists for one narrow case: small
proof-of-authority L1s where a single offline validator stalls a large share of
proposer slots and failover time matters more than extra rejected blocks.

Slots are `proposerWindowMilliseconds` apart. When a scheduled proposer is
offline, the chain stalls until the next live proposer's slot opens, so a smaller
window speeds failover recovery at the cost of more rejected blocks. A value of
`1000` (1s) is enough to make validator restarts and maintenance largely
invisible and needs no other settings. Going below 1s only helps if the Subnet
also sets
[`proposerMillisecondTimestamps`](#proposermillisecondtimestamps-bool): with the
default whole-second timestamps the slot clock only ticks once per second, so a
sub-second window stays quantized to ~1s and gains nothing.

:::caution

This is a network-wide consensus parameter, not a per-node tuning knob. Every
validator of this Subnet's chains must use the same `proposerWindowMilliseconds`.
Validators with different windows disagree about which proposer is expected for a
slot, reject each other's blocks, and break liveness. Roll it out to every
validator at once, the same way as an upgrade time. The primary network (P/C/X) is
unaffected and always uses the default.

:::

#### `proposerMillisecondTimestamps` (bool)

Interprets the proposerVM wrapper block's timestamp as unix-milliseconds
instead of unix-seconds. Defaults to `false` (seconds). Existing networks are
unaffected when this is unset.

The proposerVM proposer-slot clock advances at the resolution of the wrapper
block timestamp. With the default whole-second timestamps the clock can only
advance in whole-second steps, so proposer rotation (and therefore how fast the
chain recovers when a scheduled proposer is offline) is quantized to ~1s no
matter how short the proposer window is. Setting this to `true` makes the
timestamp millisecond-granular so that a sub-second proposer window can actually
advance.

Use it together with a sub-second [`proposerWindowMilliseconds`](#proposerwindowmilliseconds-uint):
neither has much effect without the other. A sub-second window is still quantized
to ~1s while timestamps are whole-second, and millisecond timestamps do nothing
while the window stays at its 5s default.

```json
{ "proposerMillisecondTimestamps": true }
```

:::caution

This is a Subnet-wide consensus parameter, not a per-node tuning knob, and it
must be fixed for the life of the chain:

- Every validator of this Subnet's chains must use the same value. Validators
  that disagree decode each other's block timestamps differently, expect
  different proposers per slot, reject each other's blocks, and break liveness.
- It must be set from genesis. Enabling it on a chain that already has
  whole-second history misreads every previously-accepted block (a unix-seconds
  value read as unix-millis is off by a factor of 1000), corrupting the node on
  restart. Only enable it on a fresh chain that is millisecond-granular from its
  first block.
- With a sub-second window, proposer rotation is only advisory against
  misbehaving validators. Block verification tolerates timestamps up to 10s in
  the future as a clock-drift allowance, which at a 50ms window is ~200 slots,
  so a modified node can timestamp ahead and claim a future slot immediately.
  Safety is unaffected. Use sub-second windows only on chains where all
  validators are trusted, which is the intended CFT/PoA setting.

The primary network (P/C/X) is unaffected and always uses whole-second
timestamps.

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
