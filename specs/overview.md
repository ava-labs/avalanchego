# AvalancheGo — Architecture Overview

> Top-level map of the AvalancheGo node. Start here, then drill into the
> component specs linked throughout. Every spec is written to stand alone, but
> this document explains how the pieces fit together and how a unit of work
> (a connection, a message, a block) flows across them.

---

## 1. What this repository is

AvalancheGo is the reference **node implementation** for the Avalanche network: a
proof-of-stake, multi-chain platform. A single node binary simultaneously:

- Speaks an authenticated **P2P protocol** to discover and talk to other nodes.
- Runs an **independent instance of consensus** for every blockchain it tracks.
- Hosts **Virtual Machines (VMs)** that define each blockchain's rules and state.
- Persists everything through a layered **database** abstraction.
- Exposes an **HTTP/JSON-RPC API** for wallets, tooling, and operators.

The network is organized into **subnets** (validator sets) that each validate one
or more **blockchains**. The mandatory **Primary Network** is a special subnet
that validates three built-in chains:

| Chain | Name | VM | Model | Purpose | Spec |
|-------|------|----|-------|---------|------|
| **P** | Platform Chain | PlatformVM | UTXO + metadata | Validators, subnets/L1s, staking — **source of truth for all validator sets** | [platformvm.md](platformvm.md) |
| **X** | Exchange Chain | AVM | UTXO | Asset creation & transfer (fungible, NFTs) | [avm.md](avm.md) |
| **C** | Contract Chain | Coreth / SAE-EVM | account-based | Ethereum-compatible smart contracts | [evm.md](evm.md) |

All three run the **Snowman** (linear-chain) consensus engine today; the X-Chain
migrated from DAG-based Avalanche consensus to Snowman in the *Cortina* upgrade.

---

## 2. High-level component map

```
                          ┌──────────────────────────────────────────────────┐
                          │                node.Node  (node/node.go)          │
                          │  Composition root: builds & orders every subsystem │
                          └───────┬───────────────┬───────────────┬───────────┘
       config / genesis /         │               │               │
       upgrade schedule  ─────────┘               │               │
       (node.md)                                  │               │
            ┌───────────────────┬─────────────────┼───────────────┬──────────────────┐
            ▼                   ▼                 ▼                ▼                  ▼
   ┌────────────────┐  ┌────────────────┐  ┌────────────────┐  ┌──────────────┐  ┌──────────────┐
   │  P2P Network   │  │ Chain Manager  │  │  API Server    │  │   Database   │  │   Indexer    │
   │  network/      │  │  chains/       │  │  api/server/   │  │  database/   │  │  indexer/    │
   │  (networking)  │  │  (chains)      │  │  (api)         │  │  (database)  │  │  (api)       │
   └───────┬────────┘  └───────┬────────┘  └───────┬────────┘  └──────────────┘  └──────────────┘
           │                   │  builds one of these per blockchain:            ▲
           │                   ▼                                                 │
           │   ┌──────────────────────────────────────────────────────────────┐ │
           │   │  Per-chain stack  (one independent instance per blockchain)    │ │
           │   │                                                                │ │
   inbound │   │   Handler ──► Consensus Engine ──► Consensus (Snowman/Simplex) │ │
   msgs ───┼──►│  (snow/networking/handler)   (snow/engine)   (snow/consensus)  │ │
           │   │      │              │  ▲                                       │ │
   outbound│   │      │              ▼  │ Accept/Reject                         │ │
   msgs ◄──┼───┤   Sender ◄──────────┴──┴─────────────► VM (wrapped)            │─┘
           │   │  (snow/networking/sender)              ▼                       │  accepted
           │   │                              ProposerVM►MeterVM►TracedVM►inner  │  containers
           │   └──────────────────────────────────────────┬───────────────────┘
           │                                               ▼
           │                       ┌──────────────────────────────────────────┐
           ▼                       │  Concrete VM: PlatformVM / AVM / EVM       │
   ChainRouter routes              │  (in-process, or out-of-process via gRPC   │
   by ChainID                      │   = rpcchainvm)                            │
   (snow/networking/router)        └──────────────────────────────────────────┘

   Cross-cutting, used everywhere:  IDs · Codec · secp256k1/BLS/TLS crypto  (primitives.md)
   Cross-chain UTXO transfers:      Atomic Shared Memory                    (chains.md)
```

**Per spec:** [Networking](networking.md) · [Chains & Subnets](chains.md) ·
[Consensus](consensus.md) · [Simplex](simplex.md) · [VM Framework](vm-framework.md) ·
[Database](database.md) · [API](api.md) · [Node](node.md) · [Primitives](primitives.md).

---

## 3. The layered model

Read the specs in roughly this order — each layer builds on the ones below it.

```
┌─────────────────────────────────────────────────────────────────────────┐
│  L7  Applications (VMs)        PlatformVM · AVM · EVM        platformvm/   │
│                                                              avm/ · evm/   │
├─────────────────────────────────────────────────────────────────────────┤
│  L6  VM Framework & wrappers   ChainVM/Block contract,       vm-framework  │
│                                ProposerVM, MeterVM, gRPC plugin            │
├─────────────────────────────────────────────────────────────────────────┤
│  L5  Consensus                 Snowman/Avalanche engines     consensus     │
│                                Snowball decisions; Simplex BFT   simplex    │
├─────────────────────────────────────────────────────────────────────────┤
│  L4  Per-chain messaging       Handler · Router · Sender     consensus     │
│                                (sync/async queues, timeouts)  (snow/net)   │
├─────────────────────────────────────────────────────────────────────────┤
│  L3  Orchestration             Chain Manager · Subnets ·     chains        │
│                                Atomic shared memory                        │
├─────────────────────────────────────────────────────────────────────────┤
│  L2  P2P transport             TLS peers, handshake,         networking    │
│                                discovery, throttling, p2p SDK              │
├─────────────────────────────────────────────────────────────────────────┤
│  L1  Storage                   Database interface + decorators  database   │
│                                merkledb / blockdb / archivedb              │
├─────────────────────────────────────────────────────────────────────────┤
│  L0  Primitives                IDs · Codec · Crypto (secp256k1/BLS/TLS)    │
│                                                              primitives     │
└─────────────────────────────────────────────────────────────────────────┘
       Node (L*) wires all of the above together at process startup → node.md
```

---

## 4. End-to-end data flows

### 4.1 A consensus message, from wire to decision

This is the central path of the system and spans four specs.

```
peer TCP ──► network.Peer ──► (handshake done?) ──► router.ChainRouter
(networking)   decode p2p.Message              route by ChainID
                                                      │
                                                      ▼
                                          handler.Handler  (per chain)
                                          sync/async queue, single lock
                                                      │
                                                      ▼
                                          common.Engine (Snowman)
                                          PushQuery/PullQuery/Chits/Get/Put
                                                      │
                         ┌────────────────────────────┼───────────────────────────┐
                         ▼                            ▼                            ▼
              VM.ParseBlock / GetBlock      Consensus.RecordPoll          Sender.Send/Gossip
              VM.Verify (vm-framework)      (snow/consensus → Accept)     queries K validators
                         │                            │                  (back out via network)
                         ▼                            ▼
                  block uniquified          Block.Accept / Block.Reject
                                            VM commits state to DB (versiondb)
                                            AcceptorGroup fires → indexer, etc.
```

- The [Networking](networking.md) layer authenticates the peer and forwards every
  non-handshake message verbatim to the consensus router via
  `router.ExternalHandler.HandleInbound`.
- The [Consensus](consensus.md) layer's `ChainRouter` owns **request IDs**,
  **adaptive timeouts**, and **benchlisting**; the per-chain `Handler` holds a
  single lock and dispatches to the `Engine`.
- The `Engine` samples **K** validators, collects votes (Chits), and pushes poll
  results into the `snowball`/`snowman` decision logic. Once a block's Snowball
  instance crosses the **Beta** confidence threshold and its parent is accepted,
  the engine calls `Block.Accept`.
- The [VM Framework](vm-framework.md) mediates every engine↔VM call; the concrete
  VM ([PlatformVM](platformvm.md) / [AVM](avm.md) / [EVM](evm.md)) defines what a
  block *means* and commits state through the [Database](database.md).

### 4.2 Building & proposing a block

```
VM has pending txs ──► VM.WaitForEvent returns PendingTxs
                          │
                          ▼
                   ProposerVM gate (Snowman++): is it this node's window?
                   soft leader sampled from P-Chain validators (vm-framework.md)
                          │ yes
                          ▼
                   Engine calls VM.BuildBlock on the *preferred* tip
                          │
                          ▼
                   block wrapped (header + signature) ──► Sender.Gossip / PushQuery
```

### 4.3 A cross-chain (atomic) transfer — X ⇄ P ⇄ C

UTXOs move between chains through **atomic shared memory** ([chains.md](chains.md)),
not direct calls:

```
ExportTx on chain A ──► writes atomic.Element(s) into B's shared-memory partition
                        (committed atomically with A's block via versiondb batch)
ImportTx on chain B ──► SharedMemory.Get + fx.VerifyTransfer + RemoveRequests
                        (consumes the element, mints the local UTXO/balance)
```

The EVM bridges the UTXO world to its account model here (import = mint AVAX,
export = burn). Imports/exports are restricted to chains in the **same subnet**.

---

## 5. Cross-cutting concepts

These appear in nearly every spec; understanding them early pays off.

- **Identifiers** ([primitives.md](primitives.md)) — `ids.ID` (32B) names chains,
  blocks, txs, UTXOs, subnets; `ids.NodeID` is derived from a node's staking TLS
  certificate and is the unit of identity across networking, consensus, and the
  P-Chain validator set.

- **Codec / determinism** ([primitives.md](primitives.md)) — a versioned, byte-stable
  binary serialization. *The same value always produces the same bytes on every
  node*, which is what makes IDs, signatures, and consensus reproducible.

- **Cryptography** ([primitives.md](primitives.md)) — **secp256k1** (recoverable)
  signs UTXO transactions; **BLS12-381** signs validator attestations and is
  aggregated for Warp/ICM and Simplex; **TLS** leaf certificates establish peer
  identity with no certificate authority.

- **Validator sets** ([platformvm.md](platformvm.md)) — the P-Chain is the *single
  source of truth*. The rest of the node reads them only through the
  `validators.State` interface (`GetValidatorSet(height, subnetID)`), which
  consensus sampling, Snowman++ proposer selection, peer gating, and Warp
  verification all depend on.

- **Atomic shared memory** ([chains.md](chains.md)) — the only sanctioned channel
  for moving value between chains; writes are committed atomically with the
  producing chain's block.

- **Network upgrades** ([node.md](node.md)) — activation is purely timestamp-based.
  Subsystems branch on `IsXActivated(t)`; the schedule differs per network
  (Mainnet/Fuji/Local).

- **The wrapper/decorator pattern** — appears in three layers: VM wrappers
  (`ProposerVM → MeterVM → TracedVM → inner`, [vm-framework.md](vm-framework.md)),
  database decorators (`corruptabledb → meterdb → prefixdb → engine`,
  [database.md](database.md)), and handler middleware ([api.md](api.md)).

---

## 6. Two consensus families

AvalancheGo ships two distinct consensus mechanisms; a chain uses one or the other.

| | **Snow / Snowman** ([consensus.md](consensus.md)) | **Simplex** ([simplex.md](simplex.md)) |
|---|---|---|
| Finality | Probabilistic (tunable safety) | Deterministic, irreversible |
| Mechanism | Repeated random sampling of K validators; confidence counters (Snowball) | Leader-based rounds, BLS quorum certificates (notarize → finalize) |
| Quorum | K=20, Alpha=15, Beta=20 (defaults) | `(n+f)/2 + 1`, BFT `n ≥ 3f+1` |
| Status | Production — all Primary Network chains | Implemented; full chain-manager wiring in progress |

Both plug into the same `common.Engine` / `Handler` machinery, so the rest of the
stack (networking, VM, storage) is agnostic to which is in use.

---

## 7. Node lifecycle (startup) — see [node.md](node.md)

`node.Node` is the composition root. Abbreviated startup order:

```
staking cert → NodeID → BLS signer → VM manager → beacons → metrics → NAT
  → API server → database (verify genesis hash) → shared memory → message creator
  → validator manager → resource trackers → networking → health (BEFORE chains)
  → chain manager → initChains: create P-Chain synchronously (CriticalChain)
  → P-Chain bootstraps → unblock queued X/C and subnet chains
  → Dispatch: serve HTTP + P2P until a fatal error or signal → idempotent Shutdown
```

Key invariants: the persisted **genesis hash** must match (guards against pointing
a data dir at the wrong network); **P/X/C are critical chains** whose failure shuts
the node down; the P-Chain is created first because every other chain needs its
validator sets.

---

## 8. How to read the specs

| Spec | Covers | Read it when you need to understand… |
|------|--------|--------------------------------------|
| [overview.md](overview.md) | This document | …how everything fits together |
| [node.md](node.md) | `node/`, `config/`, `genesis/`, `upgrade/`, `app/`, `main/` | startup ordering, configuration, genesis, upgrades |
| [networking.md](networking.md) | `network/`, `network/peer/`, `network/p2p/`, `message/` | peers, handshake, discovery, throttling, wire messages, the p2p SDK |
| [consensus.md](consensus.md) | `snow/consensus/`, `snow/engine/`, `snow/networking/` | Snowball/Snowman, the engine loop, bootstrap, state sync, router/handler/sender |
| [simplex.md](simplex.md) | `simplex/`, `snow/consensus/simplex/` | the deterministic BFT alternative to Snowman |
| [chains.md](chains.md) | `chains/`, `subnets/`, `chains/atomic/` | how a blockchain is wired up; subnets; cross-chain atomic memory |
| [vm-framework.md](vm-framework.md) | `snow/engine/snowman/block/`, `vms/proposervm/`, `metervm/`, `tracedvm/`, `rpcchainvm/`, `fx/` | the ChainVM contract, VM wrappers, Snowman++, the gRPC plugin model, the Fx system |
| [platformvm.md](platformvm.md) | `vms/platformvm/` | P-Chain: staking, subnets/L1s, validator sets, oracle blocks |
| [avm.md](avm.md) | `vms/avm/`, `vms/components/avax/` | X-Chain: the UTXO model, asset creation, import/export |
| [evm.md](evm.md) | `vms/saevm/`, `graft/coreth/`, `graft/evm/`, `vms/evm/` | C-Chain: Coreth vs SAE-EVM, EVM↔atomic-memory bridge, gas/fees |
| [coreth.md](coreth.md) | `graft/coreth/`, `graft/evm/` (deep dive) | Coreth (production C-Chain VM): two-layer VM, synchronous execution, libevm extras, upgrades, dynamic fees, atomic txs, Warp/ICM, state sync, trie backends |
| [saevm.md](saevm.md) | `vms/saevm/` (deep dive) | SAE-EVM (`strevm`): ACP-194 Accept→Execute→Settle, async executor, gas-time fees, hooks, atomic txs, JSON-RPC |
| [database.md](database.md) | `database/`, `x/merkledb/`, `x/blockdb/`, `x/archivedb/`, `cache/` | the Database interface, decorator stack, merkledb proofs, state-sync storage |
| [api.md](api.md) | `api/`, `indexer/`, `wallet/` | the HTTP server, handler registration, health model, indexer, wallet SDK |
| [primitives.md](primitives.md) | `ids/`, `codec/`, `staking/`, `utils/crypto/`, `utils/` | the shared vocabulary: IDs, serialization, signatures |

---

## 9. Glossary

- **Subnet** — a set of validators (with weights) that together reach consensus on
  one or more blockchains. The **Primary Network** is the mandatory subnet
  validating P/X/C.
- **L1** — a subnet converted (via ACP-77) into a sovereign, pay-as-you-go network
  with its own validator manager; see [platformvm.md](platformvm.md).
- **Validator / Delegator** — a node staking AVAX that participates in consensus /
  a stake that delegates to a validator to share rewards.
- **VM (Virtual Machine)** — the pluggable component defining a blockchain's block
  format, validity rules, state transitions, and APIs. A VM is to a blockchain
  what a class is to an instance.
- **Snowman++** — the soft-leader congestion-control layer (`ProposerVM`) wrapped
  around every Snowman VM; see [vm-framework.md](vm-framework.md).
- **Atomic memory / shared memory** — the cross-chain UTXO transfer mechanism; see
  [chains.md](chains.md).
- **Warp / ICM** — Avalanche Interchain Messaging: BLS-signed, validator-attested
  messages between chains; signing lives in the P-Chain, aggregation in the p2p
  ACP-118 protocol.
- **Bootstrapping** — the process by which a node fetches and replays historical
  blocks to reach the network tip before joining consensus; see
  [consensus.md](consensus.md).
- **graft** — vendored upstream repositories (Coreth, subnet-evm) maintained as
  separate Go modules under `graft/`; see [evm.md](evm.md).

---

*These specs describe the state of the `rahulmutt/avalanchego-spec` branch. They
were generated by reading the source directly; `path:line` references throughout
are clickable and were verified against the code at authoring time. Code evolves —
treat line numbers as starting points, not guarantees.*
