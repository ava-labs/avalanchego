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
Subnet, like its validators and the nodes in `allowedNodes`, so admitting a
node needs no change on any other node. Membership is what `validatorOnly`
admits, and what
`--network-require-validator-to-connect` keeps in addition to validators and
beacons. Listing several roots is how a root rotation is staged.

We recommend a root per Subnet. Nothing in a certificate names a Subnet, so it
is a member of every Subnet whose `membershipCA` holds a CA in its chain. Every
certificate a member sends, the intermediate included, must hold an ECDSA
P-256, RSA-2048 or RSA-4096 key; a chain with any other key verifies nothing.

Validators need no certificate; the CA is for nodes with no on-chain identity.
A certificate stops counting when anything in its chain expires or is revoked
(`membershipCRLPath`): the node's messages for this Subnet are dropped, and under
`--network-require-validator-to-connect` the next Ping closes its connection.
The node ID is a hash of the leaf, so a reissued certificate is a new node ID.

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem"
}
```

The file is read like any CA file: whatever is not a certificate is skipped,
so verify it with OpenSSL before installing it. A file with no certificate, or
one that was cut short, fails the start.

At startup the node logs `membership CA loaded` for the Subnet, then checks its own
certificate unless it is self-signed: `staking certificate chains to membership CA`
with the Subnet, the leaf's `serial` and the expiry, or a warning when it
expires within 30 days, is revoked, or does not chain (usually a missing
intermediate or the wrong root). Design: [network/membership.md](../network/membership.md).
Running the CA: [network/membership-ca.md](../network/membership-ca.md).

#### `membershipCRLPath` (string) and `membershipCRL` (string list)

Revocation lists (X.509 CRLs), as a PEM file or inline PEM text; set at most
one, and only with a membership CA. A chain through a certificate on a list counts
as unverified. The file holds `X509 CRL` blocks as the issuer's tooling writes
them. The node does not check their signatures, so verify a list with OpenSSL
before installing it.

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem",
  "membershipCRLPath": "/path/to/membership-crl.pem"
}
```

- Lists are read at startup. A list that does not parse fails the start, as
  does a file that holds none or was cut short.
- A list revokes the serials it names among the certificates issued under its
  issuer's name, so re-encoding a certificate does not escape it.
- Several lists under one issuer revoke what any of them names. A list with a
  critical extension (a partition or a delta) fails the start. `nextUpdate` is
  not enforced.

The node logs `membership CRL loaded` for each list with its `number`. Producing and
rolling out a list: [network/membership-ca.md](../network/membership-ca.md#7-revoke-a-certificate).

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
