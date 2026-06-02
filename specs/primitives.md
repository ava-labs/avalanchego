# AvalancheGo — Cross-Cutting Primitives (IDs, Codec, Crypto, Utilities)

## 1. Purpose

This document specifies the **shared vocabulary** of AvalancheGo: the small set of
data types and utilities that nearly every other subsystem depends on. These are
not features in themselves — they are the building blocks out of which
transactions, blocks, wire messages, validator records, and peer identities are
constructed.

Four pillars are covered:

1. **Identifiers** (`ids/`) — fixed-width hashes (`ID`, `ShortID`, `NodeID`) that
   name chains, transactions, blocks, UTXOs, subnets, and peers.
2. **Codec / serialization** (`codec/`, `utils/wrappers/`) — the canonical,
   deterministic binary encoding used to serialize everything that must be
   hashed, signed, gossiped, or persisted.
3. **Cryptography** (`utils/crypto/`, `staking/`) — secp256k1 (transaction
   signatures, addresses), BLS12-381 (validator keys, proof of possession,
   aggregation), and TLS staking certificates (peer identity).
4. **Key utilities** (`utils/`) — the most widely depended-upon helpers:
   `set`, `wrappers` (Packer), `hashing`, `units`, `constants`, `formatting`
   (CB58 / bech32), `bloom`, and friends.

The defining property of all of these is **determinism**: the same logical value
must always produce the same bytes, on every node, forever — because those bytes
are hashed into IDs and signed by validators.

---

## 2. Responsibilities & Scope

| Concern | In scope | Out of scope |
|---|---|---|
| Identifier types and their hashing | `ID`, `ShortID`, `NodeID`, `RequestID`, aliasing | Chain/VM-specific ID derivation rules (see [platformvm.md](./platformvm.md), [avm.md](./avm.md)) |
| Binary serialization | codec versions, type registration, linear/hierarchy codecs, the `Packer`, length limits | Per-VM tx/block field layouts (each VM owns its codec registry) |
| secp256k1 | key/address derivation, recoverable signatures, signature format | Wallet/keystore policy ([api.md](./api.md)) |
| BLS | key gen, sign/verify, aggregation, proof of possession, ciphersuites | Warp message semantics ([networking.md](./networking.md)), Simplex protocol ([simplex.md](./simplex.md)) |
| Staking certs | TLS cert generation, parsing, verification, NodeID derivation | TLS handshake / peer auth flow ([networking.md](./networking.md)) |
| Address formatting | CB58, bech32 HRP-prefixed addresses, hex encodings | — |

---

## 3. Package / File Layout

```
ids/
  id.go             ID — 32-byte hash (chains, txs, blocks, UTXOs, subnets)
  short.go          ShortID — 20-byte hash (addresses, node id payload)
  node_id.go        NodeID — 20-byte peer identity derived from staking cert
  request_id.go     RequestID — (NodeID, ChainID, RequestID, Op) tuple
  aliases.go        Aliaser — string<->ID bidirectional alias registry
  bits.go           bit-level helpers over ID (used by Avalanche/patricia code)
  galiasreader/     gRPC client/server exposing an AliaserReader to plugin VMs

codec/
  codec.go          Codec interface (MarshalInto / UnmarshalFrom / Size)
  manager.go        Manager — versioned codec registry; prepends 2-byte version
  general_codec.go  GeneralCodec = Codec + Registry
  registry.go       Registry — RegisterType(interface{})
  linearcodec/      linear codec: 4-byte type ID prefix for interfaces
  hierarchycodec/   hierarchy codec: (2-byte group, 2-byte type) prefix
  reflectcodec/     genericCodec — reflection-driven (un)marshal engine

staking/
  certificate.go    Certificate{Raw, PublicKey}
  tls.go            cert/key generation (P-256 ECDSA self-signed)
  parse.go          ParseCertificate — bespoke ASN.1 parser; RSA/ECDSA only
  verify.go         CheckSignature — RSA-PKCS1v15 / ECDSA-ASN1 over SHA-256

utils/crypto/
  secp256k1/secp256k1.go   ECDSA keys, recoverable sigs, address derivation
  bls/public.go            BLS public keys, aggregation, Verify
  bls/signature.go         BLS signatures, aggregation
  bls/signer.go            Signer interface (Sign / SignProofOfPossession)
  bls/ciphersuite.go       domain-separation tags (signature vs PoP)
  bls/signer/localsigner/  in-process BLS signer (blst-backed)
  bls/signer/rpcsigner/    remote signer over gRPC (HSM-style)
  keychain/keychain.go     Signer + Keychain interfaces (addr -> signer)

utils/
  wrappers/packing.go      Packer — primitive big-endian read/write engine
  hashing/hashing.go       SHA-256, RIPEMD-160, checksum, pubkey->addr
  cb58/cb58.go             base58 + 4-byte checksum string encoding
  formatting/encoding.go   Hex/HexNC/HexC/JSON byte<->string encodings
  formatting/address/      bech32 chain-prefixed address Format/Parse
  set/set.go               Set[T] generic set
  units/{avax,bytes}.go    denomination + byte-size constants
  constants/network_ids.go MainnetID, FujiID, PrimaryNetworkID, HRPs
  bloom/                    rotating bloom filter (gossip dedup)
```

---

## 4. Core Types

### 4.1 `ID` — 32-byte hash — `ids/id.go:33`

```go
const IDLen = 32
type ID [IDLen]byte
```

The universal 256-bit identifier. It names **chains**, **transaction IDs**,
**block IDs**, **UTXO IDs**, **subnet IDs**, and any other content-addressed
object. An `ID` is almost always a SHA-256 hash of serialized bytes
(`ids.ToID` → `hashing.ToHash256`, `ids/id.go:36`).

Key methods:

- `ToID([]byte) (ID, error)` — wraps a 32-byte slice (`ids/id.go:36`).
- `FromString` / `String` — CB58 round-trip (`ids/id.go:41`, `ids/id.go:165`).
- `Prefix(...uint64) ID` — re-hashes `[prefixes... || id]`; lets one ID address
  multiple derived keys in a database (`ids/id.go:97`).
- `Append(...uint32) ID` — re-hashes `[id || suffixes...]`; used to derive
  ACP-77 `validationID`s (`ids/id.go:116`).
- `XOR`, `Bit`, `Compare` — bit/ordering helpers (`ids/id.go:132`, `:140`,
  `:176`). `Compare` makes `ID` satisfy `utils.Sortable[ID]`.

JSON / text marshaling uses CB58 (`ids/id.go:58`).

### 4.2 `ShortID` — 20-byte hash — `ids/short.go:27`

```go
const ShortIDLen = 20
type ShortID [ShortIDLen]byte
```

The 160-bit identifier, primarily used for **addresses** (RIPEMD-160 of SHA-256
of a public key). It is also the byte payload of a `NodeID`. CB58-encoded for
strings, with an optional prefix via `PrefixedString` (`ids/short.go:115`).

### 4.3 `NodeID` — peer identity — `ids/node_id.go:29`

```go
const NodeIDLen = ShortIDLen          // 20
type NodeID ShortID
```

A `NodeID` is a `ShortID` with a `"NodeID-"` string prefix. Critically, it is
**derived from the node's staking TLS certificate**:

```go
// ids/node_id.go:79
func NodeIDFromCert(cert *staking.Certificate) NodeID {
    return hashing.ComputeHash160Array(
        hashing.ComputeHash256(cert.Raw),
    )
}
```

So `NodeID = RIPEMD160(SHA256(DER-encoded cert))`. This binds a node's network
identity to possession of the cert's private key — the foundation of P2P peer
authentication (see [networking.md](./networking.md)) and of validator identity
in the P-Chain (see [platformvm.md](./platformvm.md)).

### 4.4 `RequestID` — in-flight request key — `ids/request_id.go:7`

```go
type RequestID struct {
    NodeID    NodeID  // who the request came from
    ChainID   ID      // which chain should answer
    RequestID uint32  // unique per (node, chain)
    Op        byte    // message opcode
}
```

Used by the message router and consensus engines to correlate responses with
outstanding requests (see [networking.md](./networking.md), [consensus.md](./consensus.md)).

### 4.5 `Aliaser` — name registry — `ids/aliases.go:42`

A thread-safe bidirectional map between human-readable aliases (e.g. `"X"`,
`"P"`, `"C"`) and chain `ID`s. An ID may have many aliases; an alias maps to one
ID. `galiasreader/` exposes the read side over gRPC so out-of-process plugin VMs
can resolve aliases (`ids/galiasreader/alias_reader_client.go`).

### 4.6 secp256k1 keys & signatures — `utils/crypto/secp256k1/secp256k1.go`

```go
const (
    SignatureLen  = 65  // [r || s || v]
    PrivateKeyLen = 32
    PublicKeyLen  = 33  // compressed
)
```

- `PrivateKey.Sign(msg)` hashes with SHA-256 then signs:
  `SignHash` → `ecdsa.SignCompact` produces `[v || r || s]`, which is rotated to
  the Avalanche on-wire order `[r || s || v]` by `rawSigToSig`
  (`secp256k1.go:215`, `:294`).
- `PublicKey.Address() ids.ShortID` = `RIPEMD160(SHA256(compressedPubKey))` via
  `hashing.PubkeyBytesToAddress` (`secp256k1.go:172`, `utils/hashing/hashing.go:99`).
  `EthAddress()` instead returns the Keccak-based C-Chain address
  (`secp256k1.go:183`).
- **Recoverable signatures**: there is no separate stored public key on a
  transaction. `RecoverPublicKey(msg, sig)` recovers the signer's pubkey from the
  signature (`secp256k1.go:86`); verification compares recovered addresses
  (`VerifyHash`, `secp256k1.go:159`). `RecoverCache` memoizes recoveries keyed by
  `SHA256(hash||sig)` (`secp256k1.go:112`).
- **Malleability guard**: `verifySECP256K1RSignatureFormat` rejects any signature
  whose `s` is over half the curve order (`errMutatedSig`, `secp256k1.go:316`),
  enforcing canonical low-S signatures.
- Private keys serialize as `"PrivateKey-" + CB58(bytes)` (`secp256k1.go:236`).

### 4.7 BLS keys, signatures, proof of possession — `utils/crypto/bls/`

Backed by `supranational/blst` (BLS12-381), public keys on **G1**, signatures on
**G2**:

```go
const PublicKeyLen = blst.BLST_P1_COMPRESS_BYTES   // 48
const SignatureLen = blst.BLST_P2_COMPRESS_BYTES   // 96
type PublicKey = blst.P1Affine
type Signature = blst.P2Affine
```

- **Signer interface** (`bls/signer.go:6`): `PublicKey()`, `Sign(msg)`,
  `SignProofOfPossession(msg)`, `Shutdown()`. Implementations:
  - `localsigner` — holds the secret key in-process; key gen via
    `blst.KeyGen(ikm)` from 32 bytes of OS randomness, secret zeroized after use
    and finalized on GC (`localsigner.go:35`, `:55`).
  - `rpcsigner` — delegates signing to a remote service over gRPC (HSM / KMS).
- **Two ciphersuites for domain separation** (`bls/ciphersuite.go`):
  - `CiphersuiteSignature` → `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_`
  - `CiphersuiteProofOfPossession` → `BLS_POP_..._POP_`

  This separation ensures a signature produced for one purpose can never be
  replayed as the other.
- **Verify** (`bls/public.go:76`) and **VerifyProofOfPossession**
  (`bls/public.go:84`) call `sig.Verify(...)` with the appropriate ciphersuite.
- **Aggregation**: `AggregatePublicKeys` (`bls/public.go:61`) and
  `AggregateSignatures` (`bls/signature.go:47`) combine many keys/sigs into one.
  This is what lets thousands of validator signatures over the same message
  collapse into a single 96-byte signature — the core mechanism behind Warp/ICM
  and Simplex finality certificates.

**Proof of Possession** (`vms/platformvm/signer/proof_of_possession.go:20`) is the
on-chain artifact registered when a validator joins:

```go
type ProofOfPossession struct {
    PublicKey         [bls.PublicKeyLen]byte  `serialize:"true"` // 48 bytes
    ProofOfPossession [bls.SignatureLen]byte  `serialize:"true"` // 96 bytes
}
```

It is a BLS signature **over the public key bytes**, using the PoP ciphersuite,
proving the registrant controls the secret key (defends against rogue-key
attacks in aggregation). Constructed by `NewProofOfPossession` and checked by
`Verify()` (`proof_of_possession.go:31`, `:49`).

### 4.8 Staking Certificate — `staking/certificate.go:8`

```go
type Certificate struct {
    Raw       []byte            // DER bytes (these are what NodeID hashes)
    PublicKey crypto.PublicKey  // *rsa.PublicKey or *ecdsa.PublicKey
}
```

- **Generation** (`staking/tls.go:117`): a self-signed X.509 cert with a P-256
  ECDSA key, `NotBefore` year 2000, `NotAfter` +100 years, PEM-encoded.
- **Parsing** (`staking/parse.go:61`): AvalancheGo uses a **bespoke ASN.1 parser**
  rather than `crypto/x509` so that certificate bytes parse identically across Go
  versions (consensus-critical — the bytes feed into `NodeID`). It enforces
  `MaxCertificateLen = 2 KiB`, accepts only RSA (2048/4096-bit modulus,
  exponent 65537) or ECDSA P-256 keys, and rejects malformed RSA moduli.
- **Verification** (`staking/verify.go:23`): `CheckSignature` hashes the message
  with SHA-256 and verifies with RSA-PKCS1v15 or ECDSA-ASN1 depending on key type.

### 4.9 The `Packer` — `utils/wrappers/packing.go:42`

The lowest layer of serialization. A `Packer` wraps a `[]byte` plus an `Offset`
and an accumulated error, and reads/writes fixed-width primitives in
**big-endian**:

```go
type Packer struct {
    Errs
    MaxSize int
    Bytes   []byte
    Offset  int
}
```

| Width | const | Pack/Unpack |
|---|---|---|
| 1 | `ByteLen` | `PackByte` / `UnpackByte`, `PackBool` |
| 2 | `ShortLen` | `PackShort` / `UnpackShort` |
| 4 | `IntLen` | `PackInt` / `UnpackInt` |
| 8 | `LongLen` | `PackLong` / `UnpackLong` |

Variable-length:
- `PackBytes` = 4-byte length prefix + raw bytes (`packing.go:194`).
- `PackStr` = 2-byte length prefix + UTF-8, capped at `MaxStringLen = 65535`
  (`packing.go:217`).
- `UnpackLimitedBytes` / `UnpackLimitedStr` enforce caller-supplied caps and add
  `errOversized` on overflow (`packing.go:207`, `:235`).

Errors are *sticky*: once the packer errors, every subsequent op is a no-op, so
callers can do a long sequence of packs and check `Errored()` once.

---

## 5. Serialization Flow — How a Transaction Becomes Bytes

The `codec` package layers on top of the `Packer`:

```
                Manager.Marshal(version, value)            codec/manager.go:113
                       │
                       ▼
   ┌───────────────────────────────────────────────────────────┐
   │ Packer.PackShort(version)   ← 2-byte codec version prefix   │
   └───────────────────────────────────────────────────────────┘
                       │
                       ▼
        Codec.MarshalInto(value, &packer)            (linear / hierarchy)
                       │
                       ▼
        reflectcodec.genericCodec.marshal(...)       codec/reflectcodec/type_codec.go:304
                       │
        reflect over the value, field by field, emitting:
          uint8/16/32/64  → big-endian fixed width
          bool            → 1 byte
          string          → 2-byte len + bytes
          slice           → 4-byte count + elements
          array           → elements (no count)
          struct          → only fields tagged `serialize:"true"`, in declaration order
          map             → 4-byte count + entries sorted by serialized key bytes
          interface       → type-ID prefix (from registry) + concrete value
```

### 5.1 Versioned codecs — `codec/manager.go`

A `Manager` is a registry of `uint16 version -> Codec` (`manager.go:74`).
`Marshal` always prepends the 2-byte version (`VersionSize = 2`,
`manager.go:129`); `Unmarshal` reads it back, selects the matching codec, and
**requires that all bytes are consumed** — trailing bytes are an error
(`ErrExtraSpace`, `manager.go:165`). It also enforces a `maxSize`
(default `defaultMaxSize = 256 KiB`, `manager.go:19`) on the input.

This versioning lets a VM evolve its wire format: register a new codec under a
new version while still decoding old persisted/gossiped bytes under the old one.

### 5.2 Type registration & interfaces — linear vs hierarchy codec

The reflection engine can't know which concrete type to instantiate when it hits
an interface field. So interfaces are encoded with a **type-ID prefix** resolved
through a registry. Two registry shapes exist:

- **linearcodec** (`codec/linearcodec/codec.go`): each `RegisterType` assigns the
  next sequential `uint32` ID. The prefix is a single 4-byte type ID
  (`PackPrefix`, `linearcodec/codec.go:84`). `SkipRegistrations(n)` reserves IDs
  to keep numbering stable across releases. This is the common case (AVM,
  PlatformVM, Warp all use it).
- **hierarchycodec** (`codec/hierarchycodec/codec.go`): the prefix is a
  `(uint16 groupID, uint16 typeID)` pair (`PackPrefix`, `hierarchycodec/codec.go:104`).
  `NextGroup()` starts a new group with type IDs reset to 0, letting a VM
  partition its type space (e.g. one group per feature extension/"fx").

On unmarshal, `UnpackPrefix` looks up the type, instantiates it via reflection,
and verifies it actually implements the target interface
(`ErrDoesNotImplementInterface`).

### 5.3 Determinism guarantees (the whole point)

The reflectcodec enforces several rules so encoding is **canonical**:

- **Struct fields** are emitted strictly in declaration order, and *only* fields
  tagged `serialize:"true"` (`reflectcodec/struct_fielder.go`; default tag name
  `"serialize"`, `reflectcodec/type_codec.go:21`). Unexported tagged fields are an
  error.
- **Maps** are emitted with keys sorted by their serialized byte representation
  (`marshal`, `type_codec.go:463`); on decode, keys must arrive strictly
  increasing or it's an error (`type_codec.go:721`). This removes Go map iteration
  nondeterminism.
- **nil slices encode as empty** (length 0). Zero-length values inside
  slices/maps are rejected (`ErrMarshalZeroLength`) so encoding is injective.
- **Slice/map counts** are bounded by `math.MaxInt32` (`type_codec.go:367`).
- A recursive-interface guard (`typeStack`) prevents infinite type cycles.

### 5.4 Concrete usage in VMs

Each VM owns its codec(s) and registers its types in a fixed order. For example
the X-Chain (AVM) defines `txs.CodecVersion` and a parser that registers all tx
and fx types (`vms/avm/txs/parser.go`, exposed to fxs via the `codecRegistry`
shim in `vms/avm/txs/codec.go:21`). A transaction's signed body is exactly
`Manager.Marshal(CodecVersion, &unsignedTx)`; the resulting bytes are hashed to
produce the tx `ID`, and secp256k1 signatures over those bytes go into
`secp256k1fx.Credential` (`vms/secp256k1fx/credential.go:17`, a `[][65]byte`).
See [avm.md](./avm.md) and [platformvm.md](./platformvm.md) for the field layouts.

---

## 6. Component Boundaries & Relationships

These primitives are consumed by essentially every subsystem. Concrete examples:

```
                         ┌────────────────────────────────────────────┐
                         │                ids / codec / crypto         │
                         └────────────────────────────────────────────┘
        ┌──────────────┬───────────────┬───────────────┬───────────────┐
        ▼              ▼               ▼               ▼               ▼
   networking/     consensus/      platformvm/        avm/           evm/
   - NodeID from   - ID for        - NodeID = staker  - secp256k1    - secp256k1
     staking cert    vertices/       identity           tx sigs        EthAddress
   - RequestID       blocks        - BLS PoP on       - codec for    - shares ID
   - TLS handshake - codec for       validator add      tx/block       types
     (peer auth)     wire msgs     - BLS aggregation    layout
                                     for L1/Warp
                         ▼               ▼
                      Warp / ICM      Simplex
                      - BLS agg sig   - BLS agg sig over
                        over chain      epoch finality
                        messages        certificates
```

- **Networking** ([networking.md](./networking.md)): a peer's `NodeID` is
  `NodeIDFromCert` of the TLS cert it presents during the handshake; messages are
  serialized via codec; in-flight requests are tracked by `RequestID`.
- **Consensus** ([consensus.md](./consensus.md), [simplex.md](./simplex.md)):
  vertices/blocks are content-addressed by `ID`; Simplex finality certificates
  are **aggregated BLS signatures** over a quorum of validators.
- **PlatformVM** ([platformvm.md](./platformvm.md)): validator identity is a
  `NodeID`; each validator registers a BLS public key plus a `ProofOfPossession`;
  the P-Chain aggregates validator BLS keys to authorize L1/subnet and Warp
  operations.
- **AVM (X-Chain)** ([avm.md](./avm.md)) and **EVM (C-Chain)** ([evm.md](./evm.md)):
  transactions are authorized by **secp256k1** recoverable signatures; addresses
  are `ShortID` (bech32) on X/P and Keccak `EthAddress` on C.
- **Warp / ICM**: cross-chain messages are signed with BLS and aggregated across
  the source subnet's validator set, then verified against the aggregated public
  key — the same `bls.Aggregate*` / `bls.Verify` primitives.
- **Database** ([database.md](./database.md)): keys are frequently `ID`/`ID.Prefix(...)`
  values; stored records are codec-marshaled.

---

## 7. Key Behaviors, Invariants & Edge Cases

- **Encoding is deterministic and canonical.** Same value → same bytes on every
  node, every time. Map key ordering, struct field ordering, and low-S signature
  normalization all exist to guarantee this. Violations would fork the network.
- **NodeID binds to cert bytes, not the public key.** Because the *raw DER* is
  hashed, the custom ASN.1 parser must produce stable bytes across Go versions;
  this is why `staking/parse.go` does not delegate to `crypto/x509`.
- **secp256k1 signatures are recoverable** — transactions store only signatures,
  not public keys; the signer's address is recovered and compared. Non-canonical
  (high-S) signatures are rejected (`errMutatedSig`).
- **BLS ciphersuite separation** prevents a normal signature from being accepted
  as a proof of possession and vice versa.
- **Proof of Possession** is mandatory at validator registration to prevent
  rogue-key attacks against signature aggregation.
- **Codec version + exact-consumption.** `Unmarshal` fails if the version is
  unknown, if input exceeds `maxSize`, or if any trailing bytes remain.
- **Length limits everywhere**: `MaxStringLen = 65535`, slice/map counts
  ≤ `MaxInt32`, manager `defaultMaxSize = 256 KiB`, cert ≤ `2 KiB`. These bound
  memory allocation on untrusted input.
- **CB58 vs bech32 vs hex** are distinct, non-interchangeable string encodings —
  see §8.
- **Packer errors are sticky** — check once after a batch of operations.

---

## 8. Constants & Parameters

| Constant | Value | Location |
|---|---|---|
| `ids.IDLen` | 32 | `ids/id.go:19` |
| `ids.ShortIDLen` / `NodeIDLen` | 20 | `ids/short.go:17`, `ids/node_id.go:18` |
| `ids.NodeIDPrefix` | `"NodeID-"` | `ids/node_id.go:17` |
| `codec.VersionSize` | 2 | `codec/manager.go:16` |
| `codec` default max size | 256 KiB | `codec/manager.go:19` |
| `reflectcodec.DefaultTagName` | `"serialize"` | `codec/reflectcodec/type_codec.go:21` |
| `wrappers.MaxStringLen` | 65535 | `utils/wrappers/packing.go:13` |
| `wrappers.{Byte,Short,Int,Long}Len` | 1,2,4,8 | `utils/wrappers/packing.go:17` |
| `secp256k1.SignatureLen` | 65 (`[r\|\|s\|\|v]`) | `secp256k1.go:27` |
| `secp256k1.PrivateKeyLen` / `PublicKeyLen` | 32 / 33 | `secp256k1.go:31` |
| `secp256k1.PrivateKeyPrefix` | `"PrivateKey-"` | `secp256k1.go:44` |
| `bls.PublicKeyLen` / `SignatureLen` | 48 / 96 | `bls/public.go:12`, `bls/signature.go:12` |
| BLS signature ciphersuite | `BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` | `bls/ciphersuite.go:14` |
| BLS PoP ciphersuite | `BLS_POP_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_` | `bls/ciphersuite.go:15` |
| `staking.MaxCertificateLen` | 2 KiB | `staking/parse.go:24` |
| allowed RSA modulus bits | 2048 / 4096, exp 65537 | `staking/parse.go:26` |
| `hashing.HashLen` / `AddrLen` | 32 / 20 | `utils/hashing/hashing.go:22` |
| cb58 checksum length | 4 bytes | `utils/cb58/cb58.go:16` |
| `units` denominations | `NanoAvax`=1 .. `Avax`=1e9 NanoAvax .. `MegaAvax` | `utils/units/avax.go:8` |
| `units` bytes | `KiB`/`MiB`/`GiB` | `utils/units/bytes.go:7` |
| `constants.MainnetID` / `FujiID` | 1 / 5 | `utils/constants/network_ids.go:18` |
| `constants.PrimaryNetworkID` / `PlatformChainID` | `ids.Empty` | `utils/constants/network_ids.go:49` |

### Encoding cheat-sheet

| Encoding | Where | Used for |
|---|---|---|
| **CB58** = base58(bytes ‖ first-4-of-SHA256) | `utils/cb58/cb58.go` | `ID`, `ShortID`, `NodeID` payload, private keys |
| **bech32** (HRP-prefixed, chain-prefixed) | `utils/formatting/address/address.go` | X/P-chain addresses, e.g. `X-avax1...` |
| **Hex / HexNC / HexC** | `utils/formatting/encoding.go:34` | API byte blobs; `HexNC` = hex no-checksum (BLS keys, sigs) |
| big-endian fixed-width | `utils/wrappers/packing.go` | all codec-serialized binary |

---

## 9. Cross-References

- [overview.md](./overview.md) — where these primitives sit in the whole node.
- [networking.md](./networking.md) — `NodeID` from staking certs, `RequestID`,
  Warp BLS aggregation, codec for wire messages.
- [consensus.md](./consensus.md) / [simplex.md](./simplex.md) — `ID` for
  vertices/blocks; Simplex BLS aggregate finality certificates.
- [platformvm.md](./platformvm.md) — validator `NodeID`, BLS keys +
  `ProofOfPossession`, key aggregation for L1/Warp.
- [avm.md](./avm.md) / [evm.md](./evm.md) — secp256k1 tx signatures and
  addresses; per-VM codec registries.
- [vm-framework.md](./vm-framework.md) — how VMs own and version their codecs.
- [database.md](./database.md) — `ID`-keyed, codec-marshaled storage.
- [node.md](./node.md) — where the staking cert and BLS signing key are loaded.
- [api.md](./api.md) — CB58/bech32/hex formatting at the JSON-RPC boundary.
