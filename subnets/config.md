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

#### `allowedNodes` (string list, deprecated)

If `validatorOnly=true` this allows explicitly specified NodeIDs to be allowed
to sync the Subnet regardless of validator status. Defaults to be empty.

Deprecated: use `membershipCAPath` / `membershipCA` instead. Listing a node by
ID means editing every other node's config, while a membership CA admits it
with one certificate. `allowedNodes` is still honoured, and the node logs a
warning at startup when it is set.

Under `--network-require-validator-to-connect`, these nodes also stay
connected. A node that does not validate the Primary Network, such as an L1
validator, used to refuse them at the handshake even though this Subnet would
have answered them.

:::tip

This is a node-specific configuration. Every validator of this Subnet has to use
this configuration in order to properly allow a node in the private Subnet.

:::

#### `membershipCAPath` (string) and `membershipCA` (string list)

Root certificates, as a PEM file (`membershipCAPath`) or inline PEM text
(`membershipCA`); set at most one, and only with `validatorOnly`. A peer whose
staking certificate chain verifies against one of them is a **member** of this
Subnet, like its validators and the nodes in `allowedNodes`. Listing several
roots stages a root rotation. A file with no certificate, or one that was cut
short, fails the start.

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem"
}
```

Design: [network/membership.md](../network/membership.md). Running the CA:
[network/membership-ca.md](../network/membership-ca.md).

#### `membershipCRLPath` (string) and `membershipCRL` (string list)

Revocation lists (X.509 CRLs), as a PEM file or inline PEM text; set at most
one, and only with a membership CA. A chain through a certificate on any list
does not verify. Lists are read at startup and their signatures are not
checked, so verify a list with OpenSSL before installing it. A list that does
not parse or has a critical extension (a partition or a delta) fails the start.

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem",
  "membershipCRLPath": "/path/to/membership-crl.pem"
}
```

Producing and rolling out a list:
[network/membership-ca.md](../network/membership-ca.md#7-revoke-a-certificate).

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
