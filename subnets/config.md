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

`allowedNodes` and `memberCAPath` / `memberCA` are complementary: both mark
members, one by node ID and one by certificate. Use whichever suits a given
node. Marking a node by ID requires editing every other node's config, which is
why the CA scales better for a fleet, but the two work side by side and
`allowedNodes` remains supported.

:::tip

This is a node-specific configuration. Every validator of this Subnet has to use
this configuration in order to properly allow a node in the private Subnet.

:::

#### `memberCAPath` (string) and `memberCA` (string list)

A peer is a **member** of this Subnet if it validates the Subnet, or its TLS
staking certificate chain verifies against one of these root certificates, or it
is listed in `allowedNodes`. Membership is what `validatorOnly` admits, what
selects the elevated message stack (see `largeMessages`), and what
`--network-require-validator-to-connect` keeps in addition to validators and
beacons.

`memberCAPath` points at a PEM file; `memberCA` inlines the same PEM text.
Exactly one of the two may be set. Either may hold more than one root, which is
how a root rotation is staged: add the new root everywhere, reissue leaves over
time, then drop the old root.

Validators need no certificate — their credential is their P-Chain entry — so
the CA exists for the nodes with no on-chain identity: RPC, archival and
stateful nodes. The whole chain's validity window is checked. There is no
revocation check; a certificate is valid until it expires, at which point the
node stops being a member and its connection is re-established on the default
stack. The P-Chain is what actually removes a validator.

The node ID is a hash of the leaf certificate, so reissuing a certificate gives
the node a new ID. That costs a non-validator a restart and nothing else.

```json
{
  "validatorOnly": true,
  "memberCAPath": "/etc/settl/member-ca.pem"
}
```

:::tip

Unlike `allowedNodes`, this does not have to be edited when a node is added.
Issuing that node a certificate from the CA is enough; no other node is touched.

:::

### Large messages

#### `largeMessages` (object)

Declares that members of this Subnet exchange P2P frames larger than the default
2 MiB, and fixes their size. The frame size is the one value that must match
between two peers, so it lives here — where every node of the Subnet reads it
from the same file — rather than in per-node flags. A node builds a single
elevated message stack, so it refuses to start if more than one tracked Subnet
declares `largeMessages`. `validatorOnly` must be `true`; a public Subnet would
admit a non-member on one node while the peer selects the elevated stack for its
validator, producing incompatible frame sizes.

| Field | Type | Description |
| :--- | :--- | :--- |
| `maxMessageSize` | uint32 | The elevated frame and codec size, in bytes. Must be greater than 2 MiB. |
| `throttlerConfig` | object | Overrides individual elevated-stack throttler limits. Every field left unset is derived from `maxMessageSize`. |

:::warning

Every node of the Subnet must configure the same `maxMessageSize`. Nothing
detects a disagreement at startup, because the frame size is never negotiated
between peers — each node decides from its own config and its own view of
membership. A node configured lower simply drops any frame above its own limit
and the connection is re-established, so a mismatch shows up as a reconnect loop
that only begins once a message large enough to cross the smaller limit is sent.

A node that is missing the `largeMessages` block entirely does not log
`large message config enabled` at startup, which is the cheapest way to catch
one that was missed during a rollout.

:::

```json
{
  "validatorOnly": true,
  "memberCAPath": "/etc/settl/member-ca.pem",
  "largeMessages": {
    "maxMessageSize": 167772160
  }
}
```

`throttlerConfig` reuses the node's own inbound and outbound throttler
configuration types, so its keys are nested the way those types are. It applies
only to the elevated stack; the default 2 MiB stack keeps its node-level
configuration. A key left out, or set to `0`, keeps the value derived from
`maxMessageSize`; a key spelled differently from the ones below is ignored, not
rejected, so check the derived values in the `large message config enabled`
log line after a change.

```json
{
  "largeMessages": {
    "maxMessageSize": 167772160,
    "throttlerConfig": {
      "inboundMsgThrottlerConfig": {
        "byteThrottlerConfig": {
          "atLargeAllocSize": 1073741824,
          "vdrAllocSize": 1073741824,
          "nodeMaxAtLargeBytes": 167772160
        },
        "bandwidthThrottlerConfig": {
          "bandwidthRefillRate": 536870912,
          "bandwidthMaxBurstRate": 167772160
        },
        "cpuThrottlerConfig": { "maxRecheckDelay": 5000000000 },
        "diskThrottlerConfig": { "maxRecheckDelay": 5000000000 },
        "maxProcessingMsgsPerNode": 1024
      },
      "outboundMsgThrottlerConfig": {
        "atLargeAllocSize": 4294967296,
        "vdrAllocSize": 2147483648,
        "nodeMaxAtLargeBytes": 167772160
      }
    }
  }
}
```

Each key corresponds to a node flag and takes the same unit: bytes, or bytes
per second for the refill rate, except `maxRecheckDelay`, which is a duration in
nanoseconds.

| Key | Node flag |
| :--- | :--- |
| `inboundMsgThrottlerConfig.byteThrottlerConfig.atLargeAllocSize` | `throttler-inbound-at-large-alloc-size` |
| `inboundMsgThrottlerConfig.byteThrottlerConfig.vdrAllocSize` | `throttler-inbound-validator-alloc-size` |
| `inboundMsgThrottlerConfig.byteThrottlerConfig.nodeMaxAtLargeBytes` | `throttler-inbound-node-max-at-large-bytes` |
| `inboundMsgThrottlerConfig.bandwidthThrottlerConfig.bandwidthRefillRate` | `throttler-inbound-bandwidth-refill-rate` |
| `inboundMsgThrottlerConfig.bandwidthThrottlerConfig.bandwidthMaxBurstRate` | `throttler-inbound-bandwidth-max-burst-size` |
| `inboundMsgThrottlerConfig.cpuThrottlerConfig.maxRecheckDelay` | `throttler-inbound-cpu-max-recheck-delay` |
| `inboundMsgThrottlerConfig.diskThrottlerConfig.maxRecheckDelay` | `throttler-inbound-disk-max-recheck-delay` |
| `inboundMsgThrottlerConfig.maxProcessingMsgsPerNode` | `throttler-inbound-node-max-processing-msgs` |
| `outboundMsgThrottlerConfig.atLargeAllocSize` | `throttler-outbound-at-large-alloc-size` |
| `outboundMsgThrottlerConfig.vdrAllocSize` | `throttler-outbound-validator-alloc-size` |
| `outboundMsgThrottlerConfig.nodeMaxAtLargeBytes` | `throttler-outbound-node-max-at-large-bytes` |

The node refuses to start if `nodeMaxAtLargeBytes` or `bandwidthMaxBurstRate`
is set below `maxMessageSize`, since a peer that cannot be granted a whole frame
would stall on its first large message, or if a `maxRecheckDelay` is set below
one millisecond.

A connection's frame size is fixed for its lifetime, but membership is not: a
peer that joins or leaves a validator set, or whose continuous-fee balance runs
dry, is on the wrong stack. Every peer is re-checked when a Ping is sent, and a
connection on the wrong stack is closed and re-established on the right one, so
a newly registered validator gets the elevated frame within a ping tick.

The larger `GetAncestors` byte budget applies only to chains in the Subnet that
declares `largeMessages`. Membership in that Subnet still selects the elevated
connection-wide frame stack, so a peer can carry large messages for that
Subnet without enlarging responses from unrelated chains.

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
