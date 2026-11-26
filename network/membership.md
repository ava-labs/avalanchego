# Subnet Membership

A `validatorOnly` subnet exchanges its blockchain messages only with its
members: its validators, the node IDs in its `allowedNodes`, and the peers its
**membership CA** certified. The membership CA admits non-validators (RPC,
archival or stateful nodes) by certificate, so admitting one takes no change
on any other node.

Configuration reference: [subnets/config.md](../subnets/config.md).

## Design

A typical CA hierarchy:

```text
Member Root CA     offline · trusted by every node of the subnet
└── Issuing CA     the online intermediate
    ├── rpc-01
    └── archival-01
```

How a member such as rpc-01 gets its certificate and node ID:

1. **Root CA.** Creates the root key and a self-signed certificate
   (`root.crt`) offline. Every node of the subnet sets `membershipCAPath` to
   `root.crt` in its subnet config. Use a root per subnet: a root trusted by
   several subnets admits each one's members to all of them.
2. **Issuing CA → Root CA, once.** The Issuing CA creates its key (ECDSA
   P-256, RSA-2048 or RSA-4096: peers ignore a chain through any other) and a
   certificate signing request (CSR) and sends the CSR. The root signs it and
   sends back `issuing-ca.crt`.
3. **rpc-01 → Issuing CA.** rpc-01 creates its staking key (`staker.key`:
   ECDSA P-256, RSA-2048 or RSA-4096, as for any staking certificate) and a
   CSR, and sends the CSR.
4. **Issuing CA → rpc-01.** The Issuing CA signs a leaf certificate of at most
   2 KiB and sends back `staker.crt`, a PEM bundle of the leaf followed by
   `issuing-ca.crt`.
5. **rpc-01 starts** with `--staking-tls-cert-file=staker.crt` and
   `--staking-tls-key-file=staker.key`. Like every other node, its subnet
   config sets `membershipCAPath`, and `membershipCRLPath` once a list exists.
6. **rpc-01 → peer.** In the TLS handshake, rpc-01 sends the bundle and signs
   the handshake with `staker.key`.
7. **Peer.** Checks the handshake signature against the leaf, verifies the
   chain to its membership CA through the intermediate in the bundle, so it
   needs no intermediate configured, and derives the node ID from the leaf as
   today: `RIPEMD-160(SHA-256(leaf DER))`. A copied certificate fails without
   its key and a self-signed one fails the chain. rpc-01 reads its own node ID
   from `info.getNodeID`.

Validators keep their own self-signed certificates, since a validator's node ID is its
P-Chain identity. If the intermediate leaks, the root revokes it and the
non-validators are reissued from a new one, with no change to any node's
membership CA.

The subnet config sets the roots and revocation lists, each as a PEM file path
or inline PEM text:

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem",
  "membershipCRLPath": "/path/to/membership-crl.pem"
}
```

- `membershipCAPath` / `membershipCAContent`: the roots. Listing several
  stages a root rotation.
- `membershipCRLPath` / `membershipCRLContent`: optional revocation lists
  (X.509 CRLs).

The node does not check list signatures; the operator verifies both files
before installing them. A file with no certificate or list, a list that does
not parse, or a file cut short fails the start. Both files are read at
startup, so changing either takes a restart.

- **The CA file should hold only CA certificates.** Every certificate in it is
  a trust anchor, so a leaf put there admits its holder even after the
  intermediate above it is revoked.
- **The CRL file must hold every issuer's current list.** A list left out
  stops revoking, so replacing one issuer's list must keep the others.

A peer is a member of a tracked subnet if it validates the subnet, is listed in
`allowedNodes`, or its certificate chain verifies against the subnet's
membership CA and no revocation list names a certificate in it. Each node
decides this from its own config.

- **`validatorOnly: true` is required** with a membership CA, or the node
  refuses to start.
- **Membership is recorded after the TLS handshake**, which still accepts any
  certificate, as today. A peer presents its certificate chain only in the
  handshake, so the node verifies it right after, once against every tracked
  subnet's membership CA, and records which subnets certified it, and until
  when, for the life of the connection. Message handling reads that record
  instead of checking signatures, and every read checks the expiry.
- **Membership ends** when the member's certificate or a CA certificate above
  it expires or is revoked by a list. Revocation never affects a validator.
- **Members stay connected.** Under `network-require-validator-to-connect`, a
  node that does not validate the primary network keeps a peer only if it
  validates the primary network or a tracked subnet, is manually tracked (a
  bootstrapper or state-sync peer), or is a member of a tracked subnet,
  `allowedNodes` included. This is checked at the handshake and at every Ping,
  so a member whose certificate expires is disconnected.
- **Members dial the validators by default.** Membership alone does not make a
  member's IP gossipable or cause validators to dial it.
- **`allowedNodes` is deprecated** in favor of a membership CA. It still
  admits and keeps the listed nodes, and the node warns at startup when it is
  set.
- **Lists revoke by issuer and serial.** Both are signed by the CA, so a member
  cannot escape a list by re-encoding its certificate into a new node ID. Lists
  are read at startup, so evicting a member before its certificate expires
  means rolling a new list out to every node and restarting them. A partial
  list (a critical extension such as a delta) fails the start; `nextUpdate` is
  not enforced, since a stale list is still correct.
- **Verification cost is bounded.** A certificate chain is capped at 8
  certificates, and every certificate the peer sends must hold a staking key
  (ECDSA P-256, RSA-2048 or RSA-4096), since the peer chooses the keys that
  path building checks signatures with. The worst case is about 1.5 ms per
  membership CA per handshake with P-256 and 2.5 ms with RSA-4096. Inbound
  upgrades are rate limited per IP and in total.
- **The node counts what it drops.** The chain router's `dropped_unallowed`
  counter counts messages dropped because the sender is not allowed on the
  subnet.

## Deployment

- **Membership needs no other client support.** A node without a membership
  CA, of any version, derives the node ID from the leaf and ignores the rest of
  the certificate chain, so a CA-issued certificate looks like any other to it.
  Nothing is added to the wire protocol, the P-Chain or consensus.
- **Every validator of the subnet needs the membership CA.** Each node decides
  membership from its own config, so a node without it drops a member's
  blockchain messages. If that node does not validate the primary network and
  runs `network-require-validator-to-connect`, it also drops the connection.
- **Moving a node from `allowedNodes` to a certificate changes its node ID**,
  and so does every renewal after that. Anything keyed by it, such as
  `--bootstrap-ids`, `allowedNodes` or monitoring, needs updating.
