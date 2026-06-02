# API, Wallet SDK, Staking, IDs, and Cryptography

## 1. API Server (`api/server/`)

### 1.1 Architecture

The API server is a standard Go `net/http` server with HTTP/2 support (via h2c):

```go
type Server interface {
    Dispatch() error
    DispatchTLS(certFile, keyFile string) error
    RegisterChain(chainName string, vm common.VM)
    AddRoute(handler http.Handler, base, endpoint string) error
    AddAliases(base string, aliases ...string) error
    // ... authentication helpers
}
```

**Middleware stack (inside-out):**
1. CORS (`rs/cors`)
2. Node ID header attachment (`X-Avalanche-NodeID`)
3. Host filter (allowed hosts validation)
4. Tracing (OpenTelemetry, optional)
5. Bootstrap state guard (reject calls until chain bootstrapped)
6. Prometheus metrics

**HTTP/2 (h2c):** Supports up to 64 concurrent streams per connection.

### 1.2 URL Structure

```
/ext/bc/<chainID or alias>/<endpoint>   — chain-specific APIs
/ext/P/                                 — P-Chain API
/ext/X/                                 — X-Chain API
/ext/C/                                 — C-Chain API
/ext/health                             — health API
/ext/metrics                            — Prometheus metrics
/ext/admin                              — admin API
/ext/info                               — node info API
```

### 1.3 Router (`api/server/router.go`)

Gorilla mux with two routing modes:

1. **URL-based**: Standard path matching.
2. **Header-based**: Route via `Avalanche-Api-Route` HTTP header.

Alias management: multiple names can map to one canonical route. Reserved routes prevent conflicts.

---

## 2. Admin API (`api/admin/`)

JSON-RPC 2.0 over HTTP at `/ext/admin`.

| Method | Description |
|--------|-------------|
| `StartCPUProfiler()` | Begin CPU profiling to file |
| `StopCPUProfiler()` | Stop and flush CPU profile |
| `MemoryProfile()` | Snapshot heap profile |
| `LockProfile()` | Mutex contention profile |
| `Alias(endpoint, alias)` | Add alias for API endpoint |
| `AliasChain(chainID, alias)` | Add alias for chain |
| `GetChainAliases(chainID)` | List all aliases for chain |
| `SetLoggerLevel(loggerName, level)` | Adjust log level at runtime |
| `GetLoggerLevel(loggerName)` | Query current log levels |
| `LoadVMs()` | Scan plugin dir and load new VMs |
| `GetConfig()` | Return node startup config |
| `DbGet(key hex-string)` | Read raw database value |
| `Stacktrace()` | Dump goroutine stacks to file |

---

## 3. Health API (`api/health/`)

HTTP GET and POST at `/ext/health`, `/ext/health/readiness`, `/ext/health/liveness`.

```go
type APIReply struct {
    Checks  map[string]Result
    Healthy bool
}

type Result struct {
    Message interface{}
    Error   *string
    Duration Duration
    Timestamp time.Time
    ContiguousFailures uint32
    TimeOfFirstFailure *time.Time
}
```

- HTTP 200 if healthy; 503 if not.
- Optional tag filtering: `?tags=foo,bar` to query only specific check groups.
- Subsystems register checks via `health.Registerer`.

**Registered checks include:** network connectivity, chain bootstrapping, disk space, memory, CPU.

---

## 4. Info API (`api/info/`)

JSON-RPC 2.0 at `/ext/info`.

| Method | Returns |
|--------|---------|
| `GetNodeVersion()` | Version string, database version, git commit |
| `GetNodeID()` | NodeID, proof of possession |
| `GetNodeIP()` | Public IP |
| `Uptime()` | `{rewardingStakePercentage, weightedAveragePercentage}` |
| `GetNetworkID()` | Network ID number |
| `GetNetworkName()` | "mainnet", "fuji", "local" |
| `Peers(nodeIDs)` | Peer list with IP, version, last contact, bench status |
| `GetBlockchainID(alias)` | Chain ID from alias |
| `IsBootstrapped(chainID)` | bool |
| `GetVMs()` | All registered VMs and aliases |
| `GetTxFee()` | Current fee schedule |
| `Acps()` | Avalanche Consensus Proposals — support/objection percentages |
| `Upgrades()` | Full upgrade schedule |

**`GetTxFee()` response:**
```go
GetTxFeeResponse {
    TxFee                         uint64
    CreateAssetTxFee              uint64
    CreateSubnetTxFee             uint64
    TransformSubnetTxFee          uint64
    CreateBlockchainTxFee         uint64
    AddPrimaryNetworkValidatorFee uint64
    AddPrimaryNetworkDelegatorFee uint64
    AddSubnetValidatorFee         uint64
    AddSubnetDelegatorFee         uint64
}
```

---

## 5. Metrics API (`api/metrics/`)

Prometheus scrape endpoint at `/ext/metrics`.

**MultiGatherer:** Dynamic registration/deregistration of metric sources:
```go
type MultiGatherer interface {
    Register(name string, gatherer prometheus.Gatherer) error
    Deregister(name string)
    prometheus.Gatherer  // Gather() []*dto.MetricFamily
}
```

**PrefixGatherer:** Prefixes all metric names with a namespace string.
**LabelGatherer:** Adds static Prometheus labels to all metrics.

---

## 6. Wallet SDK (`wallet/`)

### 6.1 Primary Network Wallet

```go
// wallet/subnet/primary/
type Wallet interface {
    P() pwallet.Wallet
    X() xwallet.Wallet
    C() cwallet.Wallet
}
```

**Construction:**
```go
wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
    URI:              uri,       // node URI
    AVAXKeychain:     avaxKC,
    EthKeychain:      ethKC,
    SubnetIDs:        subnetIDs,
    ValidationIDs:    validationIDs,
})
```

Fetches UTXOs and chain context from the remote node.

### 6.2 P-Chain Wallet

```go
// wallet/chain/p/wallet/wallet.go
type Wallet interface {
    Builder() builder.Builder
    Signer()  signer.Signer

    // Staking
    IssueAddValidatorTx(vdr, rewardsOwner, shares, ...) (*txs.Tx, error)
    IssueAddDelegatorTx(vdr, rewardsOwner, ...) (*txs.Tx, error)
    IssueAddSubnetValidatorTx(vdr, ...) (*txs.Tx, error)
    IssueRemoveSubnetValidatorTx(nodeID, subnetID, ...) (*txs.Tx, error)
    IssueAddPermissionlessValidatorTx(vdr, signer, assetID, ...) (*txs.Tx, error)
    IssueAddPermissionlessDelegatorTx(vdr, assetID, ...) (*txs.Tx, error)

    // Subnet management
    IssueCreateSubnetTx(owner, ...) (*txs.Tx, error)
    IssueCreateChainTx(subnetID, genesis, vmID, fxIDs, name, ...) (*txs.Tx, error)
    IssueTransformSubnetTx(subnetID, assetID, ...) (*txs.Tx, error)
    IssueTransferSubnetOwnershipTx(subnetID, owner, ...) (*txs.Tx, error)
    IssueConvertSubnetToL1Tx(subnetID, chainID, addr, validators, ...) (*txs.Tx, error)

    // L1 validators
    IssueRegisterL1ValidatorTx(balance, pop, warpMsg, ...) (*txs.Tx, error)
    IssueSetL1ValidatorWeightTx(warpMsg, ...) (*txs.Tx, error)
    IssueIncreaseL1ValidatorBalanceTx(validationID, balance, ...) (*txs.Tx, error)
    IssueDisableL1ValidatorTx(validationID, ...) (*txs.Tx, error)

    // Value transfer
    IssueBaseTx(outputs, ...) (*txs.Tx, error)
    IssueImportTx(chainID, to, ...) (*txs.Tx, error)
    IssueExportTx(chainID, outputs, ...) (*txs.Tx, error)
}
```

**Builder context:**
```go
type Context struct {
    NetworkID        uint32
    AVAXAssetID      ids.ID
    ComplexityWeights map[string]uint16  // Etna dynamic fee weights
    GasPrice         uint64             // base gas price
}
```

### 6.3 X-Chain Wallet

```go
type Wallet interface {
    Builder() builder.Builder
    Signer()  signer.Signer

    IssueBaseTx(outputs, ...) (*txs.Tx, error)
    IssueCreateAssetTx(name, symbol, denomination, initialState, ...) (*txs.Tx, error)
    IssueOperationTx(ops, ...) (*txs.Tx, error)
    IssueOperationTxMintFT(outputs, ...) (*txs.Tx, error)
    IssueOperationTxMintNFT(assetID, payload, owners, ...) (*txs.Tx, error)
    IssueOperationTxMintProperty(assetID, owner, ...) (*txs.Tx, error)
    IssueOperationTxBurnProperty(assetID, ...) (*txs.Tx, error)
    IssueImportTx(chainID, to, ...) (*txs.Tx, error)
    IssueExportTx(chainID, outputs, ...) (*txs.Tx, error)
}
```

### 6.4 C-Chain Wallet

```go
type Wallet interface {
    IssueImportTx(chainID, to common.Address, ...) (*evm.Tx, error)
    IssueExportTx(chainID, outputs, ...) (*evm.Tx, error)
}
```

Handles both AVAX-side (UTXO) and EVM-side (account) representations.

### 6.5 Options Pattern

All `Issue*` methods accept variadic options:
```go
type Option interface{ Apply(*options) }

// Common options:
WithContext(ctx context.Context)
WithChangeOwner(owner *secp256k1fx.OutputOwners)
WithBaseFee(*big.Int)
WithPollFrequency(time.Duration)
WithAssumeDecided()  // skip confirmation polling
```

### 6.6 Transaction Flow

```
1. wallet.P().IssueAddValidatorTx(...)
2. Builder.NewAddValidatorTx() → calculates fees, selects UTXOs, computes change
3. Signer.Sign(ctx, tx)         → adds secp256k1 + BLS signatures
4. client.IssueTx(tx)          → HTTP POST to platformvm RPC
5. Confirmation polling         → polls until tx accepted (if not WithAssumeDecided)
```

---

## 7. Staking Certificate Management (`staking/`)

### 7.1 TLS Certificate

Node identity is a self-signed ECDSA P-256 certificate:

```go
func NewCertAndKeyBytes() (certBytes, keyBytes []byte, err error)
// Generates:
// - ECDSA key on P-256 curve
// - Self-signed X.509 cert:
//   - Validity: 2000-01-01 to ~2100
//   - Serial number: 0
//   - Key usage: Digital Signature
//   - No CA constraints

func InitNodeStakingKeyPair(keyPath, certPath string) error
// Idempotent: generates only if files don't exist
// Key: PKCS8 PEM; Cert: X.509 PEM
```

**NodeID derivation:**
```
SHA256(cert.Raw) → RIPEMD160(result) → 20-byte NodeID
```

### 7.2 BLS Key

Used for Warp signing and validator weight aggregation:

```go
type Signer interface {
    PublicKey() *bls.PublicKey
    Sign(msg []byte) (*bls.Signature, error)
    SignProofOfPossession(msg []byte) (*bls.Signature, error)
}
```

**Proof of Possession (PoP):** A BLS signature that proves the node controls the private key, preventing key substitution attacks. Uses a separate cipher suite from ordinary message signing.

**Key sizes:**
- Public key: 48 bytes (compressed G1 point)
- Signature: 96 bytes (compressed G2 point)

### 7.3 Certificate Verification (`staking/verify.go`)

```go
func CheckSignature(cert *Certificate, msg []byte, sig []byte) error
// Supports RSA-PSS (PKCS1v15 + SHA256) and ECDSA (ASN1 + SHA256)
```

---

## 8. ID Types (`ids/`)

### 8.1 ID (32 bytes)

```go
type ID [32]byte  // IDLen = 32

// Creation
IDs.FromString(str string) (ID, error)  // CB58 decode
IDs.ToID(bytes []byte) (ID, error)       // Hash256 of arbitrary bytes

// Encoding
id.String() string   // CB58
id.Hex() string      // hex

// Operations
id.Prefix(prefixes ...uint64) ID   // Hash with uint64 prefixes
id.Append(suffixes ...uint32) ID   // Hash with uint32 suffixes
id.XOR(other ID) ID                // bitwise XOR
id.Bit(i uint) int                 // extract bit at position i
id.Compare(other ID) int           // lexicographic compare
```

**CB58 encoding:** Crockford's Base58 with 4-byte SHA256 checksum. All 32-byte IDs displayed as CB58 strings (e.g., `2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM`).

### 8.2 NodeID (20 bytes)

```go
type NodeID ShortID  // 20-byte alias

// Derivation
ids.NodeIDFromCert(cert *staking.Certificate) NodeID
// = RIPEMD160(SHA256(cert.DER))

// Display
nodeID.String() → "NodeID-<CB58>"
ids.NodeIDFromString("NodeID-<CB58>") (NodeID, error)
```

JSON marshal/unmarshal uses the `"NodeID-"` prefix.

### 8.3 ShortID (20 bytes)

```go
type ShortID [20]byte  // ShortIDLen = 20

ids.ToShortID(bytes []byte) (ShortID, error)  // Hash160
ids.ShortFromPrefixedString(str, prefix) (ShortID, error)

shortID.String() → CB58
shortID.PrefixedString(prefix) → prefix + CB58
// Common: "P-avax...", "X-avax...", "C-0x..."
```

Used for addresses (RIPEMD160 of public key), chain aliases (first 20 bytes of chain ID), and display addresses with chain prefix.

### 8.4 Aliaser

```go
type Aliaser interface {
    Alias(id ID) (string, error)           // ID → primary alias
    AliasOrDefault(id ID) string
    Aliases(id ID) ([]string, error)       // all aliases
    PrimaryAliasOrDefault(id ID) string
}

type AliasWriter interface {
    Aliaser
    Link(id ID, alias string) error
    RemoveAlias(alias string)
}
```

Chains register their human-readable names (e.g., `"P"`, `"X"`, `"C"`) via the Aliaser.

---

## 9. Cryptographic Utilities (`utils/crypto/`)

### 9.1 SECP256K1 (`utils/crypto/secp256k1/`)

ECDSA over secp256k1 (Bitcoin curve), using `decred/dcrd`.

**Key sizes:**
- Private key: 32 bytes
- Compressed public key: 33 bytes
- Signature: 65 bytes (`r[32] || s[32] || v[1]` recovery byte)

```go
// Generation
secp256k1.NewPrivateKey() (*PrivateKey, error)

// Public key
priv.PublicKey() *PublicKey
pub.Address() ids.ShortID          // RIPEMD160(SHA256(pubkey))
pub.EthAddress() common.Address    // Ethereum keccak160

// Signing
priv.Sign(msg []byte) ([]byte, error)      // signs SHA256(msg)
priv.SignHash(hash []byte) ([]byte, error) // signs raw 32-byte hash

// Verification
pub.Verify(msg, sig []byte) bool
pub.VerifyHash(hash, sig []byte) bool
secp256k1.RecoverPublicKey(msg, sig) (*PublicKey, error)
```

**Recovery cache:** `RecoverCache` (LRU) maps `(hash, sig) → NodeID`. Avoids expensive ECDSA recovery on repeated verification.

**Canonical signature check:** `verifySECP256K1RSignatureFormat` rejects non-canonical signatures (high-S malleability prevention).

### 9.2 BLS (`utils/crypto/bls/`)

Boneh-Lynn-Shacham using `supranational/blst` (constant-time, BLST reference implementation).

```go
// Public key: 48 bytes (compressed G1 point)
bls.PublicKeyToCompressedBytes(pk) []byte
bls.PublicKeyFromCompressedBytes(b) (*PublicKey, error)

// Signature: 96 bytes (compressed G2 point)
bls.SignatureToBytes(sig) []byte
bls.SignatureFromBytes(b) (*Signature, error)

// Aggregation
bls.AggregatePublicKeys(pks []*PublicKey) (*PublicKey, error)
bls.AggregateSignatures(sigs []*Signature) (*Signature, error)

// Verification
bls.Verify(pk, sig, msg) bool
bls.VerifyProofOfPossession(pk, sig, msg) bool

// Aggregated batch verification
bls.VerifyAggregate(pks []*PublicKey, sig *Signature, msgs [][]byte) bool
```

**Cipher suites:**
- `CiphersuiteSignature`: For regular BLS message signing.
- `CiphersuiteProofOfPossession`: Distinct hash-to-curve; prevents PoP/signature confusion.

**Warp message signing:** All Warp messages signed with `CiphersuiteSignature`. PoP uses `CiphersuiteProofOfPossession`. This prevents a validator's regular message signature from being reused as a PoP.

### 9.3 Keychain Abstraction (`utils/crypto/keychain/`)

```go
type Signer interface {
    Sign(msg []byte) ([]byte, error)
    Address() ids.ShortID
}

type Keychain interface {
    Get(addr ids.ShortID) (Signer, bool)
    Addresses() set.Set[ids.ShortID]
}
```

Unifies different signing backends (secp256k1, BLS, HSM) for wallet operations.

---

## 10. Compression (`utils/compression/`)

Two compression algorithms supported:

| Algorithm | Usage |
|-----------|-------|
| Zstandard (zstd) | P2P messages, BlockDB, snapshot files |
| Gzip | Legacy; some API responses |

```go
type Compressor interface {
    Compress([]byte) ([]byte, error)
    Decompress([]byte) ([]byte, error)
}
```

P2P message compression: If compressed size is smaller than the original, messages are wrapped in a `p2p.Message{CompressedZstd: ...}` envelope. Both ends must detect and decompress.

---

## 11. Hashing (`utils/hashing/`)

```go
func ComputeHash256(data []byte) []byte        // SHA256
func ComputeHash256Array(data []byte) [32]byte // SHA256 as array
func ComputeHash160(data []byte) []byte        // RIPEMD160(SHA256(data))
func ComputeHashOf(a, b []byte) []byte         // SHA256(a||b)
func PrefixHashID(prefix uint64, id ID) ID     // used for DB prefix keys
```

All cryptographic hashes use SHA-256. Addresses use RIPEMD-160 of SHA-256 (matches Bitcoin / Ethereum address derivation).

---

## 12. Formatting and Encoding (`utils/formatting/`)

```go
// Encoding enum
const (
    Hex    Encoding = "hex"
    CB58   Encoding = "cb58"
    JSON   Encoding = "json"
)

func Encode(encoding Encoding, bytes []byte) (string, error)
func Decode(encoding Encoding, str string) ([]byte, error)
```

API responses use CB58 for IDs and hex for raw bytes by default. Callers may request either via the `encoding` parameter.
