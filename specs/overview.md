# AvalancheGo — Architecture Overview

## What This Repository Is

AvalancheGo is the reference node implementation for the Avalanche blockchain network. It implements:

- Two consensus families: **Snow** (probabilistic, DAG/chain) and **Simplex** (BFT, epoch-based)
- A multi-chain architecture with three built-in chains: P-Chain, X-Chain, C-Chain
- A plug-in VM system (gRPC-based) for running arbitrary blockchains on subnets
- A full P2P networking stack with cryptographic peer identity
- An HTTP/JSON-RPC API surface for wallets and tooling

---

## High-Level Component Map

```
                          ┌─────────────────────────────────────────┐
                          │              Node (node/node.go)         │
                          │  Wires all subsystems; handles lifecycle  │
                          └────────────────┬────────────────────────┘
            ┌─────────────────────┬────────┴──────────┬─────────────────────┐
            ▼                     ▼                    ▼                     ▼
   ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐  ┌────────────────┐
   │  P2P Network     │  │  Chain Manager   │  │  API Server      │  │  Database      │
   │  (network/)      │  │  (chains/)       │  │  (api/server/)   │  │  (database/)   │
   └──────────┬───────┘  └───────┬──────────┘  └──────────────────┘  └────────────────┘
              │                  │
              ▼                  ▼
   ┌──────────────────┐  ┌──────────────────────────────────────────────┐
   │  Message Router  │  │  Per-Chain Handler (snow/networking/handler)  │
   │  (snow/          │  │  ┌─────────────┐  ┌────────────────────────┐ │
   │  networking/     │  │  │BootstrapEng │  │ Consensus Engine       │ │
   │  router/)        │  │  │             │  │ Snowman / Avalanche /  │ │
   └──────────────────┘  │  └─────────────┘  │ Simplex               │ │
                         │                   └──────────┬─────────────┘ │
                         └──────────────────────────────┼───────────────┘
                                                        ▼
                         ┌──────────────────────────────────────────────┐
                         │  Virtual Machine (VM)                         │
                         │  ┌──────────┐ ┌──────────┐ ┌─────────────┐  │
                         │  │ProposerVM│ │MeterVM   │ │TracedVM     │  │
                         │  └────┬─────┘ └──────────┘ └─────────────┘  │
                         │       ▼                                        │
                         │  ┌──────────┐ ┌──────────┐ ┌─────────────┐  │
                         │  │PlatformVM│ │AVM       │ │RPCChainVM / │  │
                         │  │(P-Chain) │ │(X-Chain) │ │SAEVM (EVM)  │  │
                         │  └──────────┘ └──────────┘ └─────────────┘  │
                         └──────────────────────────────────────────────┘
```

---

## The Three Built-in Chains

| Chain | Name | VM | Purpose |
|-------|------|----|---------|
| P | Platform Chain | PlatformVM | Validator/subnet management, staking |
| X | Exchange Chain | AVM (Asset VM) | UTXO-based asset transfers |
| C | Contract Chain | EVM (RPCChainVM or SAEVM) | Ethereum-compatible execution |

All three are created at genesis. The P-Chain creates X and C by emitting `CreateChainTx` transactions embedded in the P-Chain genesis block.

---

## Consensus Architecture

Avalanche supports two consensus families, selectable per subnet:

### Snow Consensus (default)
A family of probabilistic protocols inspired by the Avalanche whitepaper:
- **Snowflake / Snowball** — binary/N-ary voting primitives with confidence counters
- **Snowman** — linear chain consensus; used by P-Chain and C-Chain
- **Avalanche (DAG)** — directed acyclic graph consensus; used by X-Chain (then linearized)
- Parameters: K (sample size), AlphaPreference, AlphaConfidence, Beta (consecutive polls needed)

See [consensus.md](consensus.md) for full details on the Snow and Simplex algorithms.

### Simplex Consensus (new, BFT)
A semi-synchronous BFT protocol with explicit finality:
- Single leader per round, epoch-scoped to a validator set
- Notarization (quorum on block) → Finalization (quorum on notarization)
- Tolerates f < n/3 Byzantine faults
- Integrates with P-Chain for epoch/validator set changes

---

## VM Plugin System

Any chain VM can be implemented externally as a gRPC subprocess ([RPCChainVM](vms.md#part-3-rpcchainvm)). The engine:
1. Spawns the subprocess
2. Wraps it with `VMClient` that implements `block.ChainVM` via gRPC
3. Applies the same `ProposerVM → Consensus Engine` stack

Internal VMs (PlatformVM, AVM) are statically linked.

---

## Data Flow: Receiving a Block

```
Network peer
  → TLS connection (peer/upgrader.go)          [networking.md §6.2]
  → Inbound message throttling                 [networking.md §5.1]
  → network.Network.HandleInbound()            [networking.md §1]
  → snow/networking/router.Router.HandleInbound()
  → Per-chain Handler message queue            [consensus.md §1.7]
  → Consensus Engine (Snowman / Simplex)       [consensus.md §1.5, §2.6]
  → VM.ParseBlock() + Block.Verify()           [vms.md §5.4]
  → Consensus votes / finalization
  → Block.Accept()  →  VM applies state        [platformvm.md §4]
```

---

## Data Flow: Cross-Chain Transfer

```
Chain A (e.g., X-Chain)
  → ExportTx issued                            [vms.md §1.1]
  → Atomic request written to chains/atomic.Memory (sharedDB)   [chains.md §2]
Chain B (e.g., P-Chain or C-Chain)
  → ImportTx issued, reads from atomic.Memory  [platformvm.md §2.1]
  → Atomic request applied atomically with block acceptance      [chains.md §2.4]
```

The atomic shared memory (`chains/atomic/Memory`) uses [PrefixDB](database.md#25-prefixdb-databaseprefixdb) to namespace each chain-pair's entries, and writes are applied in the same database batch as block acceptance to guarantee atomicity.

---

## Key Packages and Their Roles

| Package | Role | Spec |
|---------|------|------|
| `node/` | Node struct; wires all subsystems together | [chains.md §4.3](chains.md#43-node-initialization-sequence-nodenode-go) |
| `snow/` | Consensus algorithms, engines, context, validator sets | [consensus.md](consensus.md) |
| `network/` | P2P: peer lifecycle, TLS, gossip, throttling | [networking.md](networking.md) |
| `chains/` | Chain manager: creation, VM association, atomic memory | [chains.md](chains.md) |
| `vms/platformvm/` | P-Chain VM: staking, subnet management | [platformvm.md](platformvm.md) |
| `vms/avm/` | X-Chain VM: UTXO assets | [vms.md §Part 1](vms.md#part-1-avm--asset-vm-x-chain) |
| `vms/proposervm/` | ProposerVM: Snowman++ proposer selection | [vms.md §Part 2](vms.md#part-2-proposervm) |
| `vms/rpcchainvm/` | gRPC plugin host for external VMs | [vms.md §Part 3](vms.md#part-3-rpcchainvm) |
| `vms/saevm/` | Streaming Asynchronous Execution VM (C-Chain) | [vms.md §Part 4](vms.md#part-4-saevm--streaming-asynchronous-execution-vm) |
| `simplex/` | Simplex BFT consensus engine | [consensus.md §Part 2](consensus.md#part-2-simplex-consensus) |
| `database/` | Database interface, LevelDB/PebbleDB/VersionDB, etc. | [database.md §1–2](database.md#1-core-interface-database) |
| `x/merkledb/` | PATRICIA trie with range/change proofs | [database.md §4](database.md#4-merkledb--patricia-trie-xmerkledb) |
| `x/archivedb/` | Append-only historical state storage | [database.md §5](database.md#5-archivedb-xarchivedb) |
| `x/blockdb/` | Height-indexed block file storage | [database.md §6](database.md#6-blockdb-xblockdb) |
| `genesis/` | Genesis state generation per network | [chains.md §3](chains.md#3-genesis-genesis) |
| `upgrade/` | Protocol upgrade schedules | [chains.md §6](chains.md#6-upgrade-management-upgrade) |
| `config/` | Node configuration flags | [chains.md §5](chains.md#5-configuration-config) |
| `api/` | HTTP server, admin/health/info/metrics endpoints | [api.md §1–5](api.md#1-api-server-apiserver) |
| `wallet/` | SDK for building and signing transactions | [api.md §6](api.md#6-wallet-sdk-wallet) |
| `staking/` | TLS cert + BLS key management | [api.md §7](api.md#7-staking-certificate-management-staking) |
| `ids/` | ID, NodeID, ShortID types | [api.md §8](api.md#8-id-types-ids) |
| `utils/crypto/` | secp256k1, BLS, keychain abstractions | [api.md §9](api.md#9-cryptographic-utilities-utilscrypto) |
| `message/` | P2P message types and codec | [networking.md §3](networking.md#3-message-protocol) |
| `proto/` | Protobuf definitions for VM and DB gRPC services | [vms.md §3.3](vms.md#33-vm-grpc-service-from-protovmvmproto) |

---

## Spec Files in This Directory

| File | Coverage | Status |
|------|----------|--------|
| `overview.md` | This file — architecture overview | Current |
| `consensus.md` | Snow consensus algorithms and Simplex BFT | Current |
| `networking.md` | P2P layer: peers, messages, throttling | Current |
| `platformvm.md` | P-Chain: transactions, staking, state | Current |
| `vms.md` | AVM (X-Chain), ProposerVM, RPCChainVM, SAEVM | Current |
| `chains.md` | Chain manager, genesis, node init, config, upgrades | Current |
| `database.md` | Database implementations, MerkleDB, ArchiveDB, BlockDB | Current |
| `api.md` | API server, wallet SDK, staking certs, IDs, crypto | Current |
