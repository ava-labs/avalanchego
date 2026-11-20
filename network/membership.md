# Subnet Membership

## Motivation

A `validatorOnly` subnet admits its validators and the node IDs in its
`allowedNodes`, so admitting a non-validator (an RPC, archival or stateful node)
means editing and restarting every validator.

## Proposed design

This proposes that a subnet can instead trust a **membership CA** and admit any
peer whose staking certificate was issued under it. Admitting a node then takes
one certificate and no change on the other nodes.

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
2. **Issuing CA → Root CA, once.** The Issuing CA creates its key and a
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

Two new settings in the subnet config set the roots and revocation lists, each
as a PEM file path or inline PEM text:

```json
{
  "validatorOnly": true,
  "membershipCAPath": "/path/to/membership-ca.pem",
  "membershipCRLPath": "/path/to/membership-crl.pem"
}
```

- `membershipCAPath` / `membershipCA`: the trust anchors (step 1). Listing
  several stages a rotation.
- `membershipCRLPath` / `membershipCRL`: optional revocation lists (X.509 CRLs).

A peer would be a member of a tracked subnet if it validates the subnet, is
listed in `allowedNodes`, or its certificate chain verifies against the subnet's
membership CA and no revocation list names a certificate in it. Each node
decides this on its own:

| | Today | Proposed |
| --- | --- | --- |
| May the peer exchange the subnet's blockchain messages? | validator or `allowedNodes` | member |
| May it stay connected under `network-require-validator-to-connect`| validator or manually tracked peer | validator, manually tracked peer or member |
| Add a non-validator to an N-node subnet | edit `allowedNodes` and restart N nodes | issue one certificate |
| Remove it before it expires | edit `allowedNodes` and restart N nodes | revoke it, roll the list out and restart N nodes |

- **`validatorOnly: true` is required** with a membership CA, or the node
  refuses to start.
- **Membership is recorded after the TLS handshake**, which still accepts any
  certificate, as today. A peer presents its certificate chain only in the
  handshake, so the node verifies it right after, once against every tracked
  subnet's membership CA, and records which subnets certified it,
  and until when, for the life of the connection. Message handling reads that
  record instead of checking signatures, and every read checks the expiry.
- **Membership ends** when the member's certificate or a CA certificate above
  it expires or is revoked by a list. Revocation never affects a validator.
- **Members would stay connected.** Under
  `network-require-validator-to-connect`, a node that does not validate the
  primary network drops its own subnet's `allowedNodes` at the handshake today.
  The flag would keep every member, `allowedNodes` included.
- **Members dial the validators by default.** Membership alone does not make a
  member's IP gossipable or cause validators to dial it.
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
  member's subnet messages. If that node does not validate the primary network
  and runs `network-require-validator-to-connect`, it also drops the connection.
- **A CA-issued certificate is a new node ID.** The node ID is a hash of the
  leaf, so moving a non-validator from `allowedNodes` to a certificate, and
  every renewal after that, changes its node ID. Anything keyed by it, such as
  `--bootstrap-ids`, `allowedNodes` or monitoring, needs updating.
