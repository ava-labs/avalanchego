# Subnet Membership

## Motivation

A `validatorOnly` subnet admits its validators and the node IDs in its
`allowedNodes`, so admitting a non-validator (an RPC, archival or stateful node)
means editing and restarting every validator.

## Proposed design

This proposes that a subnet can instead trust a **membership CA** and admit any
peer whose staking certificate was issued under it. Admitting a node then takes
one certificate and no change on the other nodes. Two new settings in the subnet
config set it, each as a PEM file path or inline PEM text:

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem",
  "membershipCRLPath": "/path/to/membership-crl.pem"
}
```

- `membershipCAPath` / `membershipCA`: the roots. Listing several stages a root
  rotation.
- `membershipCRLPath` / `membershipCRL`: optional revocation lists (X.509 CRLs).

A typical CA hierarchy:

```text
Member Root CA       offline · trusted by this subnet's nodes only (membershipCA)
└── Nodes CA         the online issuer
    ├── rpc-01       key generated on the node, never leaves it
    └── archival-01
```

A peer would be a member of a tracked subnet if it validates the subnet, is
listed in `allowedNodes`, or its certificate chain verifies against the subnet's
membership CA and no revocation list names a certificate in it. Each node
decides this on its own:

| | Today | Proposed |
| --- | --- | --- |
| May the peer exchange the subnet's blockchain messages? | validator or `allowedNodes` | member |
| May it stay connected under `network-require-validator-to-connect`? | validator or bootstrapper | validator, bootstrapper or member |
| Add a non-validator to an N-node subnet | edit `allowedNodes` and restart N nodes | issue one certificate |
| Remove it before it expires | edit `allowedNodes` and restart N nodes | revoke it, roll the list out and restart N nodes |

- **`validatorOnly: true` is required** with a membership CA, or the node
  refuses to start.
- **Membership is decided at the handshake.** A peer presents its certificate
  chain only in the TLS handshake, so the node verifies it there, once against
  every tracked subnet's membership CA, and records which subnets certified it,
  and until when, for the life of the connection. Message handling reads that
  record instead of checking signatures, and every read checks the expiry.
- **Membership ends** when the member's certificate or a CA certificate above
  it expires or is revoked by a list. Revocation never affects a validator.
- **Members would stay connected.** Under
  `network-require-validator-to-connect`, a node that does not validate the
  primary network drops its own subnet's `allowedNodes` at the handshake today.
  The flag would keep every member, `allowedNodes` included.
- **Members dial the validators.** Only validators' IPs are gossiped, so a
  validator never dials a member.
- **`allowedNodes` would be deprecated** in favor of a membership CA. It would
  still admit and keep the listed nodes, with a warning at startup when it is
  set.

## Backwards compatibility

This is not a breaking change:

- **No wire, P-Chain or consensus change.** A node without a membership CA, of
  any version, derives the node ID from the leaf and ignores the rest of the
  certificate chain, so a CA-issued certificate looks like any other to it.
- **Subnets without the new settings behave as today.** `allowedNodes` keeps
  working. The one behavior change is that
  `network-require-validator-to-connect` keeps a subnet's `allowedNodes`
  connected instead of dropping them, which is a fix.
- **Validators keep their certificates.** A validator's node ID is its P-Chain
  identity, so it needs no CA-issued certificate.

Rollout:

- **Every validator of the subnet needs the new version and the config.** Each
  node decides membership from its own config, so a node without it drops a
  member's subnet messages, and under `network-require-validator-to-connect` its
  connection.
- **A CA-issued certificate is a new node ID.** The node ID is a hash of the
  leaf, so moving a non-validator from `allowedNodes` to a certificate, and
  every renewal after that, changes its node ID. Anything keyed by it, such as
  `--bootstrap-ids`, `allowedNodes` or monitoring, needs updating.
