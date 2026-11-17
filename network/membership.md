# Subnet Membership

A `validatorOnly` subnet exchanges its blockchain messages only with its
members: its validators, the node IDs in its `allowedNodes`, and the peers its
**membership CA** certified. The membership CA admits non-validators (RPC,
archival or stateful nodes) by certificate, so admitting one takes no change
on any other node.

Running the CA: [membership-ca.md](membership-ca.md). Configuration reference:
[subnets/config.md](../subnets/config.md).

## Configuration

Each setting takes a PEM file path or inline PEM text:

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

Both require `validatorOnly: true`, or the node refuses to start.
`allowedNodes` is deprecated in favor of a membership CA: it still admits and
keeps the listed nodes, and the node warns at startup when it is set.

The files are read with the standard library: roots through
`x509.CertPool.AppendCertsFromPEM`, lists through `x509.ParseRevocationList`,
and anything else in the files is skipped. The node does not check list
signatures; the operator verifies both files with OpenSSL before installing
them. It refuses only a file with no certificate or list, a list that does not
parse, or a file cut off mid-block.

## Certificates

```text
Member Root CA       offline · trusted by this subnet's nodes only (membershipCA)
└── Nodes CA         the online issuer
    ├── rpc-01       key generated on the node, never leaves it
    └── archival-01
```

A member sends its leaf and the intermediate as its staking certificate. The
node ID is a hash of the leaf, so every renewal is a new node ID. Validators
keep their own certificates, since a validator's node ID is its P-Chain
identity.

We recommend a root per subnet. Nothing in a certificate names a subnet, so a
certificate is a member of every subnet whose `membershipCA` holds a CA in its
certificate chain. The root stays offline: if the intermediate leaks, the root
revokes it and non-validators are reissued from a new one, with no change to
any node's `membershipCA`.

## Membership

A peer is a member of a tracked subnet if it validates the subnet, is listed in
`allowedNodes`, or its certificate chain verifies against the subnet's
membership CA and no revocation list names a certificate in it. Each node
decides this from its own config.

- **Membership is decided at the handshake.** A peer presents its certificate
  chain only in the TLS handshake, so the node verifies it there, once against
  every tracked subnet's membership CA, and records which subnets certified it,
  and until when, for the life of the connection. Message handling reads that
  record instead of checking signatures, and every read checks the expiry.
- **Membership ends** when the member's certificate or a CA certificate above
  it expires or is revoked by a list. Revocation never affects a validator.
- **Members stay connected.** Under `network-require-validator-to-connect`, a
  node that does not validate the primary network keeps a peer only if it
  validates the primary network or a tracked subnet, is a bootstrapper, or is a
  member of a tracked subnet, `allowedNodes` included. This is checked at the
  handshake and at every Ping, so a member whose certificate expires is
  disconnected.
- **Members dial the validators.** Only validators' IPs are gossiped, so a
  validator never dials a member.

## Revocation

A list revokes by issuer and serial. Both are signed by the CA, so a member
cannot escape a list by re-encoding its certificate into a new node ID. Lists
are read at startup, so evicting a member before its certificate expires means
rolling a new list out to every node and restarting them. A partial list (a
critical extension such as a delta) fails the start; `nextUpdate` is not
enforced, since a stale list is still correct.

## Verification cost

A certificate chain is capped at 8 certificates, and every certificate the peer
sends must hold a staking key (ECDSA P-256, RSA-2048 or RSA-4096), since the
peer chooses the keys that path building checks signatures with. The worst case
is about 1.5 ms per membership CA per handshake with P-256 and 2.5 ms with
RSA-4096. Inbound upgrades are rate limited per IP and in total.

## Observability

At startup the node logs each loaded membership CA and list, and checks its own
certificate chain, warning if it does not verify, is revoked, or expires within
30 days. Each certified peer is logged at debug level with the serial a list
would have to name. The chain router's `dropped_unallowed` counter counts
messages dropped because the sender is not allowed on the subnet.

## Deployment

- **Membership needs no other client support.** A node without a membership
  CA, of any version, derives the node ID from the leaf and ignores the rest of
  the certificate chain, so a CA-issued certificate looks like any other to it.
  Nothing is added to the wire protocol, the P-Chain or consensus.
- **Every validator of the subnet needs the membership CA.** Each node decides
  membership from its own config, so a node without it drops a member's
  blockchain messages, and under `network-require-validator-to-connect` its
  connection.
- **Moving a node from `allowedNodes` to a certificate changes its node ID.**
  Anything keyed by it, such as `--bootstrap-ids`, `allowedNodes` or
  monitoring, needs updating.
