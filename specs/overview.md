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

### Simplex Consensus (new, BFT)
A semi-synchronous BFT protocol with explicit finality:
- Single leader per round, epoch-scoped to a validator set
- Notarization (quorum on block) → Finalization (quorum on notarization)
- Tolerates f < n/3 Byzantine faults
- Integrates with P-Chain for epoch/validator set changes

---

## VM Plugin System

Any chain VM can be implemented externally as a gRPC subprocess (RPCChainVM). The engine:
1. Spawns the subprocess
2. Wraps it with `VMClient` that implements `block.ChainVM` via gRPC
3. Applies the same `ProposerVM → Consensus Engine` stack

Internal VMs (PlatformVM, AVM) are statically linked.

---

## Data Flow: Receiving a Block

```
Network peer
  → TLS connection (peer/upgrader.go)
  → Inbound message throttling (network/throttling/)
  → network.Network.HandleInbound()
  → snow/networking/router.Router.HandleInbound()
  → Per-chain Handler message queue
  → Consensus Engine (Snowman / Simplex)
  → VM.ParseBlock() + Block.Verify()
  → Consensus votes / finalization
  → Block.Accept()  →  VM applies state
```

---

## Data Flow: Cross-Chain Transfer

```
Chain A (e.g., X-Chain)
  → ExportTx issued
  → Atomic request written to chains/atomic.Memory (sharedDB)
Chain B (e.g., P-Chain or C-Chain)
  → ImportTx issued, reads from atomic.Memory
  → Atomic request applied atomically with block acceptance
```

---

## Key Packages and Their Roles

| Package | Role |
|---------|------|
| `node/` | Node struct; wires all subsystems together |
| `snow/` | Consensus algorithms, engines, context, validator sets |
| `network/` | P2P: peer lifecycle, TLS, gossip, throttling |
| `chains/` | Chain manager: creation, VM association, atomic memory |
| `vms/platformvm/` | P-Chain VM: staking, subnet management |
| `vms/avm/` | X-Chain VM: UTXO assets |
| `vms/proposervm/` | ProposerVM: Snowman++ proposer selection |
| `vms/rpcchainvm/` | gRPC plugin host for external VMs |
| `vms/saevm/` | Streaming Asynchronous Execution VM (C-Chain) |
| `simplex/` | Simplex BFT consensus engine |
| `database/` | Database interface, LevelDB/PebbleDB/VersionDB, etc. |
| `x/merkledb/` | PATRICIA trie with range/change proofs |
| `genesis/` | Genesis state generation per network |
| `upgrade/` | Protocol upgrade schedules |
| `config/` | Node configuration flags |
| `api/` | HTTP server, admin/health/info/metrics endpoints |
| `wallet/` | SDK for building and signing transactions |
| `staking/` | TLS cert + BLS key management |
| `ids/` | ID, NodeID, ShortID types |
| `utils/crypto/` | secp256k1, BLS, keychain abstractions |
| `message/` | P2P message types and codec |
| `proto/` | Protobuf definitions for VM and DB gRPC services |

---

## Spec Files in This Directory

| File | Coverage |
|------|----------|
| `overview.md` | This file — architecture overview |
| `consensus.md` | Snow consensus algorithms and Simplex BFT |
| `networking.md` | P2P layer: peers, messages, throttling |
| `platformvm.md` | P-Chain: transactions, staking, state |
| `vms.md` | AVM (X-Chain), ProposerVM, RPCChainVM, SAEVM |
| `chains.md` | Chain manager, genesis, node init, config, upgrades |
| `database.md` | Database implementations, MerkleDB, ArchiveDB, BlockDB |
| `api.md` | API server, wallet SDK, staking certs, IDs, crypto |
