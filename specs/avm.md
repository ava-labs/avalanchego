# AVM (X-Chain) — Asset / Exchange VM

The X-Chain (`vms/avm/`) is AvalancheGo's UTXO-based VM for **creating and exchanging
assets**. It implements the native AVAX token plus arbitrary fungible assets, NFTs,
and mintable/variable-cap assets, and handles cross-chain transfers to/from the
P-Chain and C-Chain via **atomic shared memory**.

The X-Chain is historically significant: it originally ran on **DAG-based Avalanche
consensus** and was migrated to **linear Snowman** consensus in the **Cortina** upgrade.
The codebase still carries both code paths (`Linearize`, `ParseTx` for the DAG engine,
plus block builder/executor for Snowman), and this split shapes its initialization
flow heavily.

> Sibling specs: [overview](overview.md) · [consensus](consensus.md) · [simplex](simplex.md) · [networking](networking.md) · [node](node.md) · [vm-framework](vm-framework.md) · [platformvm](platformvm.md) · [evm](evm.md) · [chains](chains.md) · [database](database.md) · [api](api.md) · [primitives](primitives.md)

---

## 1. Responsibilities & Scope

The AVM is responsible for:

- **Asset creation** — `CreateAssetTx` defines a new asset with name, symbol,
  denomination, and one or more *initial states* (per-Fx outputs).
- **Asset transfer** — `BaseTx` consumes UTXOs and produces new UTXOs, burning a fee.
- **Multi-output operations** — `OperationTx` performs Fx-specific operations: minting
  more of a variable-cap asset, minting/transferring NFTs, minting/burning property-fx
  assets.
- **Cross-chain transfer** — `ExportTx` / `ImportTx` move assets to/from other chains
  in the same subnet (P-Chain, C-Chain) through atomic shared memory.
- **UTXO bookkeeping** — maintaining the set of unspent transaction outputs, asset
  definitions, blocks, and chain metadata.
- **APIs** — an `avm` JSON-RPC service (balances, UTXOs, tx submission/status) and a
  deprecated `wallet` service.

**In scope for this doc:** all of `vms/avm/`, the shared `vms/components/avax/` UTXO
primitives (used by both AVM and P-Chain), and a brief treatment of `vms/secp256k1fx`,
`vms/nftfx`, `vms/propertyfx` as they relate to assets.

**Out of scope:** the generic VM framework (see [vm-framework](vm-framework.md)),
P-Chain semantics (see [platformvm](platformvm.md)), and the generic atomic-memory
substrate (see [database](database.md) and [platformvm](platformvm.md#shared-memory)).

---

## 2. Package / File Layout

```
vms/avm/
├── vm.go                  # VM struct; Initialize, Linearize, ParseTx, ChainVM impl
├── factory.go             # Factory.New() -> &VM{Config}
├── config.go              # ParseConfig (JSON) -> avmConfig
├── config/config.go       # Config: Upgrades, TxFee, CreateAssetTxFee
├── genesis.go             # Genesis, GenesisAsset, NewGenesis, genesis codec
├── tx.go / tx_init.go     # VM-level Tx wrapper (snowstorm.Tx) for the DAG engine
├── service.go             # "avm" JSON-RPC API service
├── wallet_service.go      # deprecated "wallet" API service
├── client.go              # Go RPC client
├── health.go              # HealthCheck
├── txs/
│   ├── tx.go              # Tx (Unsigned + Creds), UnsignedTx interface
│   ├── base_tx.go         # BaseTx (wraps avax.BaseTx)
│   ├── create_asset_tx.go # CreateAssetTx
│   ├── operation_tx.go    # OperationTx
│   ├── operation.go       # Operation (Fx op + consumed UTXOIDs)
│   ├── import_tx.go       # ImportTx
│   ├── export_tx.go       # ExportTx
│   ├── initial_state.go   # InitialState (per-Fx initial outputs)
│   ├── codec.go / parser.go  # codec + Fx type registration
│   ├── visitor.go         # Visitor interface (dispatch over tx types)
│   ├── executor/          # syntactic + semantic verification + state mutation
│   ├── mempool/           # AVM mempool wrapper (generic vms/txs/mempool)
│   └── txstest/           # test tx builder
├── block/
│   ├── block.go           # Block interface (stateless)
│   ├── standard_block.go  # StandardBlock (the only block type)
│   ├── parser.go          # block codec/parser
│   ├── builder/builder.go # builds blocks from the mempool
│   └── executor/          # stateful Block (Verify/Accept/Reject) + Manager
├── state/
│   ├── state.go           # State / Chain / ReadOnlyChain interfaces + impl
│   ├── diff.go            # in-memory state diff layered over a parent
│   └── versions.go        # Versions interface (block -> Chain lookup)
├── fxs/fx.go              # Fx, FxOperation, FxCredential, ParsedFx
├── utxo/spender.go        # Spender: select UTXOs to fund amounts/fees (API helper)
├── network/               # P2P tx gossip, mempool bloom filter, atomic handler swap
├── config/, metrics/      # config params, metrics

vms/components/avax/       # shared UTXO/asset primitives (AVM + P-Chain)
├── utxo.go                # UTXO
├── utxo_id.go             # UTXOID (TxID + OutputIndex -> InputID)
├── asset.go               # Asset (asset ID wrapper)
├── transferables.go       # TransferableInput/Output, VerifyTx (flow check)
├── base_tx.go             # avax.BaseTx (NetworkID, BlockchainID, Ins, Outs, Memo)
├── flow_checker.go        # FlowChecker: consumed >= produced per asset
├── atomic_utxos.go        # GetAtomicUTXOs (read shared memory)
├── utxo_state.go, state.go, utxo_handler.go, addresses.go, ...
```

---

## 3. The UTXO Model (shared `avax` components)

The X-Chain (and P-Chain) use an **unspent-transaction-output** model. The core types
live in `vms/components/avax/` and are reused across VMs.

### 3.1 Asset & UTXO identity

- **`Asset`** — `vms/components/avax/asset.go:20` — a single `ID ids.ID` naming the
  asset. The asset ID equals the **TxID of the `CreateAssetTx` that created it**
  (`Executor.CreateAssetTx`, `vms/avm/txs/executor/executor.go:49`). `Verify` rejects
  the empty ID.
- **`UTXOID`** — `vms/components/avax/utxo_id.go:29` — `{TxID, OutputIndex}`. The unique
  key of a UTXO is `InputID() = TxID.Prefix(OutputIndex)`
  (`utxo_id.go:46`). The unexported `Symbol bool` (`utxo_id.go:35`) flags an *imported*
  (cross-chain) input that lives in shared memory rather than the local UTXO DB; it is
  set `true` by `ImportTx.InputUTXOs` (`import_tx.go:33`).
- **`UTXO`** — `vms/components/avax/utxo.go:19` — `{UTXOID, Asset, Out verify.State}`.
  `Out` is an Fx-specific output (e.g. `secp256k1fx.TransferOutput`).

### 3.2 Transferable inputs/outputs

- **`TransferableOutput`** — `transferables.go:64` — `{Asset, FxID, Out TransferableOut}`.
  `FxID` is `serialize:"false"` — it is derived from the concrete `Out` type via the
  codec, not encoded.
- **`TransferableInput`** — `transferables.go:140` — `{UTXOID, Asset, FxID, In TransferableIn}`.
  Inputs reference the UTXO they spend by `UTXOID`.
- `TransferableIn` is `Verifiable + Amounter + Coster`; `TransferableOut` is
  `Amounter + verify.State` (`transferables.go:48-62`).

### 3.3 `avax.BaseTx` and flow checking

- **`avax.BaseTx`** — `base_tx.go:26` — `{NetworkID, BlockchainID, Outs, Ins, Memo}`.
  `Verify` enforces the correct network/chain ID (replay protection) and
  `len(Memo) <= MaxMemoSize` (256, `base_tx.go:16`). Post-Durango the memo must be
  empty (`VerifyMemoFieldLength`, `base_tx.go:69`).
- **`FlowChecker`** — `flow_checker.go:16` — accumulates `consumed`/`produced` per asset
  ID. `Verify` (`flow_checker.go:42`) fails with `ErrInsufficientFunds` if, for any
  asset, `produced > consumed`. Note: it does **not** require exact equality — excess
  consumed value is simply burned.
- **`VerifyTx`** — `transferables.go:204` — the central conservation check. It
  `Produce`s the fee (`feeAmount` of `feeAssetID`) plus every output, `Consume`s every
  input, verifies inputs are **sorted & unique** and outputs are **sorted**, and runs
  the flow check. Inputs/outputs are passed as `[][]` groups so import/export can add
  their extra side (imported inputs / exported outputs) to the same balance.

---

## 4. Transaction Types

All AVM txs embed `txs.BaseTx` (which embeds `avax.BaseTx`). The signed wrapper is
`txs.Tx` (`txs/tx.go:46`): `{Unsigned UnsignedTx, Creds []*FxCredential}`. The
`UnsignedTx` interface (`txs/tx.go:25`) exposes `InputIDs`, `NumCredentials`,
`InputUTXOs`, and `Visit(Visitor)`. Codec version is `0` and types are registered in a
fixed order (`txs/codec.go:64`): `BaseTx, CreateAssetTx, OperationTx, ImportTx, ExportTx`.

| Tx | File / line | Adds | Fee | Purpose |
|----|-------------|------|-----|---------|
| **BaseTx** | `txs/base_tx.go:20` | — | `TxFee` | Plain transfer / burn |
| **CreateAssetTx** | `txs/create_asset_tx.go:17` | `Name, Symbol, Denomination, States []*InitialState` | `CreateAssetTxFee` | Define a new asset |
| **OperationTx** | `txs/operation_tx.go:20` | `Ops []*Operation` | `TxFee` | Fx operations (mint/NFT/burn) |
| **ImportTx** | `txs/import_tx.go:19` | `SourceChain, ImportedIns []*TransferableInput` | `TxFee` | Pull funds from another chain's shared memory |
| **ExportTx** | `txs/export_tx.go:19` | `DestinationChain, ExportedOuts []*TransferableOutput` | `TxFee` | Push funds to another chain's shared memory |

### 4.1 CreateAssetTx & InitialState

- **`InitialState`** — `txs/initial_state.go:28` — `{FxIndex uint32, FxID ids.ID,
  Outs []verify.State}`. Each initial state targets one Fx (by index) and lists the
  asset's starting outputs for that Fx. Outputs must be sorted and non-nil
  (`initial_state.go:40`), and `FxIndex` must reference a registered Fx.
- **Fixed-cap vs variable-cap** (see `NewGenesis`, `genesis.go:108-146`): a *fixed-cap*
  asset's initial state contains `secp256k1fx.TransferOutput`s (the full supply, no
  minting authority). A *variable-cap* asset's initial state contains
  `secp256k1fx.MintOutput`s (mint authorities); more units are minted later via an
  `OperationTx` + `MintOperation`.
- On execution, `Executor.CreateAssetTx` (`executor.go:35`) runs the base
  consume/produce, then for each initial-state output creates a new UTXO whose
  `Asset.ID = txID` (the new asset ID), with `OutputIndex` continuing after the base
  `Outs`.

### 4.2 OperationTx & Operation

- **`Operation`** — `txs/operation.go:26` — `{Asset, UTXOIDs []*UTXOID, FxID,
  Op fxs.FxOperation}`. It consumes the listed UTXOs and asks an Fx to produce new
  outputs. UTXOIDs must be sorted & unique (`operation.go:39`).
- `OperationTx.NumCredentials = len(Ins) + len(Ops)` (`operation_tx.go:58`): one
  credential per base input **plus one per operation**.
- `Executor.OperationTx` (`executor.go:60`) consumes base inputs, then for each op
  deletes the consumed UTXOs and adds the op's `Op.Outs()` as new UTXOs under the op's
  asset ID.
- Fx operations include `secp256k1fx.MintOperation` (mint variable-cap fungible:
  produces a `MintOutput` + `TransferOutput`, `vms/secp256k1fx/mint_operation.go:30`),
  `nftfx.MintOperation`/`TransferOperation`, and `propertyfx.MintOperation`/`BurnOperation`.

### 4.3 ImportTx / ExportTx

- **ImportTx** consumes `Ins` (local UTXOs, e.g. for the fee) plus `ImportedIns`
  (UTXOs sitting in the **source chain's** shared-memory partition). Imported inputs are
  marked symbolic (`import_tx.go:33`).
- **ExportTx** produces `Outs` (local change) plus `ExportedOuts` (UTXOs written into
  the **destination chain's** shared-memory partition).
- See [§5](#5-txutxo-flow--importexport-atomic-memory) for the atomic-memory mechanics.

---

## 5. Tx/UTXO Flow + Import/Export (Atomic Memory)

### 5.1 Local tx verification & execution

Three visitors operate over every tx (`txs/visitor.go` dispatches by concrete type):

1. **`SyntacticVerifier`** (`txs/executor/syntactic_verifier.go:48`) — stateless. Checks
   network/chain ID, runs `avax.VerifyTx` (fee + flow + sorting), verifies each
   credential, and asserts `numCreds == numInputs` (`+len(Ops)` for OperationTx,
   `+len(ImportedIns)` for ImportTx). For `CreateAssetTx` it also enforces name length
   (1–128, ASCII letters/numbers/space, no leading/trailing whitespace), symbol length
   (1–4, uppercase), denomination ≤ 32, ≥1 initial state, and sorted-unique states.
2. **`SemanticVerifier`** (`txs/executor/semantic_verifier.go:28`) — stateful. For each
   input it loads the referenced UTXO from state, checks the input's asset ID matches
   the UTXO's, resolves the Fx by the credential's Go type
   (`getFx`/`TypeToFxIndex`), checks the asset actually *supports* that Fx
   (`verifyFxUsage` — reads the asset's `CreateAssetTx.States`,
   `semantic_verifier.go:223`), and finally calls `fx.VerifyTransfer` /
   `fx.VerifyOperation`.
3. **`Executor`** (`txs/executor/executor.go:20`) — applies state changes:
   `avax.Consume`/`avax.Produce` for the base layer, plus the per-tx-type UTXO
   add/delete and atomic-memory request construction.

### 5.2 Import/export over shared memory

Cross-chain transfers within a subnet use the **atomic shared memory** substrate
(`chains/atomic`). Each ordered pair of chains shares a memory partition; an export on
chain A writes UTXO bytes there, and an import on chain B removes and consumes them.
Crucially the *write to the destination* and the *state commit on the source* happen in
a **single atomic batch** when the block is accepted.

```
   X-Chain ExportTx (Accept)                       C-Chain / P-Chain ImportTx (Accept)
   ─────────────────────────                       ──────────────────────────────────
  Executor.ExportTx (executor.go:107)
   • create UTXO {TxID, idx, asset, Out}
   • marshal -> atomic.Element{Key=InputID,
                  Value=bytes, Traits=addrs}
   • AtomicRequests[DestChain].PutRequests   ┐
                                             │
  Block.Accept (block/executor/block.go:212)│
   • onAcceptState.Apply(state)              │     SemanticVerifier.ImportTx
   • batch = state.CommitBatch()             │      (semantic_verifier.go:84)
   • SharedMemory.Apply(atomicRequests,batch)┘      • SharedMemory.Get(SourceChain, utxoIDs)
        └── atomically: commit X state  ───────┐     • unmarshal each UTXO
            AND Put UTXOs into DestChain mem    │     • fx.VerifyTransfer(in, cred, utxo.Out)
                                                │
                          ┌─────────────────────┘
                          ▼
            ┌──────────────────────────────┐    Executor.ImportTx (executor.go:87)
            │   Shared Memory partition     │     • AtomicRequests[SrcChain].RemoveRequests
            │   (X <-> Dest)                │     • Inputs.Add(utxoID)  (for conflict check)
            │   key=InputID -> UTXO bytes   │
            └──────────────────────────────┘    Block.Accept on Dest:
                                                  SharedMemory.Apply(removeReqs, destBatch)
```

Key references:
- Export write: `Executor.ExportTx`, `executor.go:107` — builds `atomic.Element`s,
  attaching `Addresses()` as **Traits** so the destination can index by recipient.
- Import read: `SemanticVerifier.ImportTx`, `semantic_verifier.go:103` —
  `v.Ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)`.
- Import remove: `Executor.ImportTx`, `executor.go:99` — `RemoveRequests`.
- Atomic apply on accept: `Block.Accept`, `block/executor/block.go:243` —
  `SharedMemory.Apply(blkState.atomicRequests, batch)` writes state and memory together.
- `verify.SameSubnet` (`semantic_verifier.go:93,131`) restricts import/export to chains
  in the **same subnet**.
- Reading exported UTXOs for an address (used by the API): `avax.GetAtomicUTXOs`,
  `atomic_utxos.go:25`, via `SharedMemory.Indexed`.

---

## 6. Component Boundaries & Relationships

### 6.1 Consensus boundary — DAG → Snowman (Cortina linearization)

The VM implements `vertex.LinearizableVMWithEngine` (`vm.go:53`). Lifecycle:

- **`Initialize`** (`vm.go:136`) sets up the parser, state, fxs, and `txBackend`, but
  **does not** create the block manager, builder, or network — those fields are
  "only initialized after the chain has been linearized" (`vm.go:104`).
- **`ParseTx`** (`vm.go:438`) is the DAG-engine entrypoint: it parses a tx, runs
  syntactic verification, and returns a `*Tx` (`snowstorm.Tx`).
- **`Linearize(ctx, stopVertexID)`** (`vm.go:361`) is called once, at the Cortina
  upgrade time (`vm.Config.Upgrades.CortinaTime`). It:
  1. `state.InitializeChainState(stopVertexID, cortinaTime)` — seeds the linear chain
     from the last DAG vertex.
  2. Creates the mempool, `blockexecutor.Manager`, `blockbuilder.Builder`, and
     `network.Network` (Snowman machinery).
  3. Swaps the live `AppHandler` via `vm.Atomic.Set(vm.network)` (`vm.go:419`) — see
     §6.4 — and starts push/pull gossip goroutines.
- After linearization the VM behaves as a standard `snowman.ChainVM`
  (`GetBlock`/`ParseBlock`/`BuildBlock`/`SetPreference`/`LastAccepted`, `vm.go:330-353`).

There is exactly **one block type**, `StandardBlock` (`block/standard_block.go:19`):
`{PrntID, Hght, Time, Root, Transactions}`. The `Root` (merkle root) field is currently
unused — `Block.Verify` rejects any non-empty root (`block/executor/block.go:50`).

### 6.2 Atomic-memory boundary with P/C chains

The X-Chain only touches other chains through `ctx.SharedMemory` and only for chains in
the same subnet. It never reads P-/C-chain state directly. The boundary is entirely the
shared-memory key/value partitions described in [§5](#52-importexport-over-shared-memory).
See [platformvm](platformvm.md) for the symmetric P-Chain side and
[evm](evm.md) for the C-Chain side. The substrate itself is documented in
[database](database.md).

### 6.3 Fx boundary (feature extensions)

The AVM is asset-type-agnostic; all asset semantics are delegated to **Fxs**
(`fxs/fx.go:29`). Three are registered by the chain manager in fixed order
(`chains/manager.go:112`): `secp256k1fx` (index 0), `nftfx` (1), `propertyfx` (2). The
VM builds `typeToFxIndex` from the registered codec types (`vm.go:195`,
`txs/codec.go`), so verification can map a concrete credential/output/operation type to
its owning Fx. The semantic verifier enforces that an asset only uses Fxs declared in
its `CreateAssetTx.States` (`verifyFxUsage`, `semantic_verifier.go:223`).

- **secp256k1fx** — fungible value: `TransferInput/Output`, `MintInput/Output`,
  `MintOperation`, `Credential`, `OutputOwners` (threshold + addresses).
- **nftfx** — non-fungible tokens: `MintOutput`/`MintOperation`,
  `TransferOutput`/`TransferOperation`.
- **propertyfx** — `MintOutput`, `OwnedOutput`, `MintOperation`, `BurnOperation`.

See [vm-framework](vm-framework.md) for the generic Fx mechanism.

### 6.4 Networking boundary — the `network.Atomic` handler swap

Because the AppHandler must exist from `Initialize` (peers connect before
linearization) but the real gossip stack only exists after `Linearize`, the VM embeds
`network.Atomic` (`network/atomic.go:17`), an atomically-swappable `common.AppHandler`.
It starts as a no-op handler (`vm.go:147`) and is replaced by the real `*network.Network`
during `Linearize` (`vm.go:419`). Likewise, `Connected`/`Disconnected` buffer peers in
`connectedPeers` until the network exists (`vm.go:110-128`).

### 6.5 State layering boundary

`state.State` (`state/state.go:68`) is the persisted store (UTXOs, txs, blocks,
singletons — schema diagram at `state/state.go:99`). During verification the executor
writes into a `state.Diff` (`state/diff.go`) layered over a parent block's state, looked
up through the `state.Versions` interface (`block/executor/manager.go:100`,
`GetState`). On `Accept`, the diff is `Apply`-ed to the base state and committed
together with atomic requests.

---

## 7. Key Behaviors, Invariants & Edge Cases

- **Asset ID = creating TxID.** A new asset's UTXOs and the asset's own ID are both the
  `CreateAssetTx` ID (`executor.go:49`).
- **Fee is burned, not paid out.** `VerifyTx` `Produce`s the fee with no matching
  output, so the flow check forces inputs to cover it; the value simply disappears
  (`transferables.go:213`). The fee asset is the **AVAX asset** (`vm.feeAssetID`,
  defaulted in `initGenesis`, `vm.go:500-527`).
- **Conservation is per-asset and one-directional.** `produced > consumed` fails, but
  over-consuming (burning) is allowed (`flow_checker.go:46`). Different assets cannot
  cross-fund each other.
- **Credential counting.** `numCreds` must equal `numInputs`, where operations and
  imported inputs each count as an input (`syntactic_verifier.go`, OperationTx/ImportTx).
- **Sorting requirements.** Inputs sorted+unique, outputs sorted, initial states
  sorted+unique, operations sorted+unique — all enforced syntactically.
- **No double-spend within a tx or block.** OperationTx checks its op UTXOIDs don't
  collide with base inputs (`errDoubleSpend`, `syntactic_verifier.go:191`). Within a
  block, `Block.Verify` tracks `importedInputs` and rejects conflicting imports
  (`ErrConflictingBlockTxs`, `block/executor/block.go:167`); `VerifyUniqueInputs`
  (`manager.go:179`) walks the in-memory ancestry to reject imports that conflict with
  an as-yet-undecided ancestor block.
- **Timestamp rules.** A block's timestamp must be ≥ parent's and ≤ now + `SyncBound`
  (10s) (`block/executor/block.go:22,57`).
- **Empty blocks rejected.** `ErrEmptyBlock` (`block/executor/block.go:69`); the builder
  returns `ErrNoTransactions` rather than producing one (`builder.go:163`).
- **Genesis assets.** Each `GenesisAsset` is a `CreateAssetTx` with `Alias`; the **first**
  genesis asset becomes the fee asset (AVAX) (`vm.go:522`). Genesis assets must have
  empty `Outs` — only initial states (`errGenesisAssetMustHaveState`, `vm.go:503`).
- **Block accept ordering.** `onAccept(tx)` (which notifies the wallet service) runs
  *before* state changes are applied (`block/executor/block.go:218`; invariant noted at
  `vm.go:561`).
- **Reject re-queues txs.** On `Reject`, still-valid txs are re-added to the mempool
  (`block/executor/block.go:272`).
- **Historical hack:** one specific mainnet OperationTx ID is hard-coded to skip
  operation semantic verification (`semantic_verifier.go:68`) — a legacy compatibility
  carve-out.

---

## 8. Configuration / Parameters

`config.Config` (`config/config.go:9`):

| Field | Meaning |
|-------|---------|
| `Upgrades` | `upgrade.Config` — supplies `CortinaTime` (linearization), `DurangoTime` (memo restriction), etc. |
| `TxFee` | AVAX burned by every non-asset-creating tx (BaseTx, OperationTx, ImportTx, ExportTx) |
| `CreateAssetTxFee` | AVAX burned by a `CreateAssetTx` |

Default fees (`genesis/genesis_*.go`):

| Network | `TxFee` | `CreateAssetTxFee` |
|---------|---------|--------------------|
| Mainnet / Fuji | 1 milliAVAX | 10 milliAVAX |
| Local | 1 milliAVAX | 1 milliAVAX |

Runtime VM config is JSON-parsed from `configBytes` (`config.go`, `ParseConfig`) and
includes `ChecksumsEnabled` and the `network.Config` gossip parameters (bloom-filter
sizing, push/pull gossip frequencies and branching factors, `network/config.go`).
Block builder target size is `128 KiB` (`builder.go:26`).

---

## 9. APIs

### 9.1 `avm` service (`service.go`, client `client.go`)

Registered at the chain's base endpoint (`vm.go:302`). Notable methods:

| Method | `service.go` | Purpose |
|--------|--------------|---------|
| `IssueTx` | `:184` | Submit a signed tx (→ `issueTxFromRPC` → mempool + gossip) |
| `GetTx` / `GetTxStatus` | `:244` / `:217` | Fetch tx bytes / status |
| `GetUTXOs` | `:285` | Page through local UTXOs (and atomic UTXOs from a source chain) |
| `GetBalance` / `GetAllBalances` | `:453` / `:530` | Address balances per asset |
| `GetAssetDescription` | `:403` | Name/symbol/denomination of an asset |
| `GetBlock` / `GetBlockByHeight` / `GetHeight` | `:54` / `:101` / `:156` | Block queries |
| `GetTxFee` | `:594` | Current `TxFee` / `CreateAssetTxFee` |

`issueTxFromRPC` (`vm.go:468`) is the submission path: it calls
`network.IssueTxFromRPC` (verify → mempool → push-gossip, `network/network.go:143`) and
tolerates `ErrDuplicateTx`. Asset references in API args may be an **alias** or an ID
(`lookupAssetID`, `vm.go:551`).

### 9.2 `wallet` service (deprecated)

Registered at `/wallet` (`vm.go:312`). `WalletService` (`wallet_service.go`) tracks
locally-issued pending txs (`pendingTxs`, a linked hashmap) so a client can chain
dependent txs before the first is accepted; `decided` (`wallet_service.go:27`) clears
them as blocks accept. This path is being removed alongside `onAccept` (`vm.go:563`).

The Go `Client` (`client.go:24`) wraps all of the above plus `GetAtomicUTXOs`
(`client.go:126`) and `AwaitTxAccepted` polling (`client.go:221`).

---

## 10. Cross-References

- [overview](overview.md) — where the X-Chain sits among P/X/C chains.
- [platformvm](platformvm.md) — P-Chain; symmetric atomic-memory peer; reuses the same
  `vms/components/avax` UTXO primitives.
- [evm](evm.md) — C-Chain import/export counterpart.
- [vm-framework](vm-framework.md) — the generic Fx mechanism and VM lifecycle.
- [consensus](consensus.md) — Snowman consensus the linearized X-Chain runs on; the
  DAG-Avalanche engine it migrated from.
- [networking](networking.md) — the P2P gossip substrate behind `network/`.
- [database](database.md) — the atomic shared-memory substrate (`chains/atomic`).
- [primitives](primitives.md) — `ids`, codec, and `verify` helpers used throughout.
