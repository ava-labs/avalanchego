# PlatformVM (P-Chain)

## 1. Purpose

The **PlatformVM** runs the **P-Chain**, the metadata and coordination chain of
the Avalanche Primary Network. It is the network's *source of truth for who
validates what*. Every other chain in the node — the X-Chain (AVM), the C-Chain
(EVM), and every Subnet/L1 blockchain — derives its validator set, and therefore
its consensus sampling and Warp/ICM signature aggregation, from state stored on
the P-Chain.

The P-Chain is itself a linear (Snowman) chain. It is special in two ways that
no other VM is:

1. It implements `validators.State` (`vms/platformvm/vm.go:59`,
   `vms/platformvm/vm.go:66`), the boundary the rest of the node calls to learn
   validator sets at historical heights. This is consumed by consensus, the
   networking layer, Snowman++ proposer selection, and Warp verification.
2. It is the only VM that still uses **oracle blocks** (proposal / commit /
   abort) to make a non-deterministic decision: whether a finished validator
   earned its staking reward, based on measured uptime.

This document covers the P-Chain's business logic: the UTXO model, the full
transaction taxonomy, the block types and the proposal/commit/abort flow, the
staking lifecycle, subnets vs. ACP-77 L1s, reward math, the validator-set
provider boundary, and Warp/ICM signing. The generic VM framework
(`ChainVM`, mempool plumbing, RPC chain VM) is described in
[vm-framework.md](./vm-framework.md) and is not repeated here.

## 2. Responsibilities & scope

- **Staking & validator management** for the Primary Network: add/remove
  validators and delegators, track current vs. pending sets, compute potential
  rewards, and issue rewards (or not) based on uptime.
- **Subnet management**: create subnets (`CreateSubnetTx`), create blockchains
  on subnets (`CreateChainTx`), manage subnet ownership and permissioned subnet
  validators.
- **L1 management (ACP-77)**: convert a permissioned subnet into a
  pay-as-you-go L1 (`ConvertSubnetToL1Tx`), register/weight/balance/disable L1
  validators driven by Warp messages.
- **The UTXO ledger** for AVAX on the P-Chain, including cross-chain
  import/export via shared atomic memory with the X-Chain and C-Chain.
- **Serving validator sets** at arbitrary historical heights to the rest of the
  node (`validators.State`).
- **Warp/ICM signing & verification** of P-Chain-authored messages
  (subnet conversion, L1 validator registration).

Out of scope: snowman consensus mechanics ([consensus.md](./consensus.md)),
P2P/gossip transport ([networking.md](./networking.md)), the AVAX asset model
shared with the X-Chain ([avm.md](./avm.md), [primitives.md](./primitives.md)).

## 3. Package / file layout

| Path | Role |
|------|------|
| `vms/platformvm/vm.go` | `VM` type; wires together state, manager, builder, network, validators; implements `ChainVM` + `validators.State` |
| `vms/platformvm/factory.go` | `Factory.New` — constructs a `VM` from `config.Internal` |
| `vms/platformvm/service.go` | JSON-RPC API (`platform.*`): `GetCurrentValidators`, `GetStake`, `IssueTx`, `GetBlock`, ... |
| `vms/platformvm/client.go` | Go client for the above API |
| `vms/platformvm/config/` | `Internal` (node-supplied config: stake bounds, reward config, upgrade times, tracked subnets) and `Config` (execution/cache tuning) |
| `vms/platformvm/txs/` | All unsigned transaction types + `Tx` wrapper, codec, visitor, priorities |
| `vms/platformvm/txs/executor/` | Transaction execution & verification (standard / proposal / atomic), state-change helpers, Warp verification |
| `vms/platformvm/txs/fee/` | Static + dynamic (Etna) fee calculators, tx complexity |
| `vms/platformvm/txs/mempool/` | P-Chain mempool |
| `vms/platformvm/block/` | Stateless block types (Apricot/Banff × proposal/commit/abort/standard/atomic), codec, visitor |
| `vms/platformvm/block/builder/` | Block production: when to build, what to pack, proposal vs. standard |
| `vms/platformvm/block/executor/` | Stateful block verify/accept/reject, oracle option generation, block-state cache |
| `vms/platformvm/state/` | On-disk state, staker sets (current/pending), diffs, L1 validators, expiries, weight/pubkey diffs |
| `vms/platformvm/reward/` | Staking reward calculator + reward split |
| `vms/platformvm/validators/` | `Manager` implementing `validators.State` (historical validator-set reconstruction) |
| `vms/platformvm/validators/fee/` | ACP-77 continuous validator-fee accounting |
| `vms/platformvm/network/` | Tx gossip + ACP-118 Warp signature request handling |
| `vms/platformvm/warp/`, `signer/` | Warp message codec/anycast constant, BLS proof-of-possession signer |
| `vms/platformvm/stakeable/` | `LockIn`/`LockOut` time-locked transfer wrappers |
| `vms/platformvm/utxo/` | UTXO flow-checker (`VerifySpend`) |
| `vms/platformvm/genesis/` | Genesis parsing/codec |
| `vms/platformvm/uptime` (via `snow/uptime`) | Uptime tracking used for reward decisions |

## 4. Core types

### 4.1 The VM

`VM` (`vms/platformvm/vm.go:62`) embeds `config.Internal`, the block
`Builder`, the `*network.Network`, and a `validators.State`. `Initialize`
(`vms/platformvm/vm.go:97`) constructs the dependency graph:

- `state.New(...)` — the on-disk state (`vms/platformvm/vm.go:138`)
- `pvalidators.NewManager(...)` — the validator-set provider, assigned to
  `vm.State` (`vms/platformvm/vm.go:153`)
- `txexecutor.Backend` — shared inputs for all tx execution
  (`vms/platformvm/vm.go:159`)
- `blockexecutor.NewManager(...)` — verify/accept/reject + block-state cache
  (`vms/platformvm/vm.go:181`)
- `network.New(...)` — gossip + Warp signing (`vms/platformvm/vm.go:190`)
- `blockbuilder.New(...)` — block production (`vms/platformvm/vm.go:218`)

The VM asserts at compile time that it satisfies `validators.State`
(`vms/platformvm/vm.go:59`). It also runs `initBlockchains`
(`vms/platformvm/vm.go:300`) on startup to instantiate every chain this node
should validate, plus background goroutines for gossip, mempool pruning, and
block reindexing.

### 4.2 Transactions

Every concrete tx is an `UnsignedTx` wrapped in a `txs.Tx` (signed envelope with
credentials). Dispatch is via the `txs.Visitor` interface
(`vms/platformvm/txs/visitor.go:7`), grouped by the upgrade that introduced
them:

| Tx | Introduced | What it does |
|----|-----------|--------------|
| `AddValidatorTx` | Apricot | Add a Primary Network validator (legacy; pre-Banff proposal tx) |
| `AddSubnetValidatorTx` | Apricot | Add a validator to a permissioned subnet |
| `AddDelegatorTx` | Apricot | Delegate stake to a Primary Network validator (legacy) |
| `CreateChainTx` | Apricot | Create a blockchain inside a subnet |
| `CreateSubnetTx` | Apricot | Create a permissioned subnet with an owner |
| `ImportTx` / `ExportTx` | Apricot | Move AVAX to/from the X- or C-Chain via shared memory |
| `AdvanceTimeTx` | Apricot | (Pre-Banff only) advance chain time; proposal tx |
| `RewardValidatorTx` | Apricot | Proposal tx that removes a finished staker and (if committed) pays its reward |
| `RemoveSubnetValidatorTx` | Banff | Remove a permissioned subnet validator |
| `TransformSubnetTx` | Banff | Turn a subnet into an elastic (permissionless) subnet; **forbidden post-Etna** |
| `AddPermissionlessValidatorTx` | Banff | Add a validator (Primary Network or elastic subnet) with a BLS key & reward owners |
| `AddPermissionlessDelegatorTx` | Banff | Delegate to a permissionless validator |
| `TransferSubnetOwnershipTx` | Durango | Change a subnet's owner |
| `BaseTx` | Durango | Pure UTXO transfer / fee burn, no side effects |
| `ConvertSubnetToL1Tx` | Etna (ACP-77) | Convert a subnet to an L1 with an on-chain manager + initial validator set |
| `RegisterL1ValidatorTx` | Etna | Add an L1 validator, authorized by a Warp message from the subnet manager |
| `SetL1ValidatorWeightTx` | Etna | Change (or zero-out/remove) an L1 validator's weight via Warp |
| `IncreaseL1ValidatorBalanceTx` | Etna | Top up an L1 validator's continuous-fee balance |
| `DisableL1ValidatorTx` | Etna | Deactivate an L1 validator (authorized by its deactivation owner) and refund balance |

Staking transactions implement layered interfaces in
`vms/platformvm/txs/staker_tx.go`: `Staker` (subnet, nodeID, BLS key, weight,
priority), `BoundedStaker` (+ `EndTime`), `ScheduledStaker` (+ `StartTime`,
pending priority), `PermissionlessStaker` (+ stake outputs), `ValidatorTx`
(+ reward owners and delegation `Shares`), and `DelegatorTx`. A key invariant
(`vms/platformvm/txs/staker_tx.go`) is that a `DelegatorTx` never also satisfies
`ValidatorTx`.

`AddPermissionlessValidatorTx` (`vms/platformvm/txs/add_permissionless_validator_tx.go:35`)
is the canonical modern staker. Its `SyntacticVerify`
(`...:121`) enforces the rule **"has BLS key ⇔ is Primary Network"** — Primary
Network validators must register a BLS key with a proof of possession; subnet
validators must use the empty signer.

`ConvertSubnetToL1Tx` (`vms/platformvm/txs/convert_subnet_to_l1_tx.go:34`)
carries the manager `ChainID`/`Address` and an initial sorted list of
`ConvertSubnetToL1Validator` (`...:86`), each with a NodeID, weight, initial
balance, BLS proof of possession, and `RemainingBalanceOwner` /
`DeactivationOwner` (`message.PChainOwner`s).

### 4.3 Blocks

Stateless block types live in `vms/platformvm/block/`. The common interface is
`Block` (`vms/platformvm/block/block.go:16`); `BanffBlock` adds `Timestamp()`
(`...:34`). Dispatch is via `block.Visitor`
(`vms/platformvm/block/visitor.go:6`). Each block type has an Apricot and a
Banff variant; Banff variants prepend a `Time uint64`:

- **Standard** (`standard_block.go`) — holds decision transactions; the normal
  block type since Banff. `BanffStandardBlock` also advances chain time.
- **Proposal** (`proposal_block.go`) — an **oracle block** holding exactly one
  proposal tx (post-Banff, always a `RewardValidatorTx`). `BanffProposalBlock`
  may also carry decision txs and a timestamp
  (`vms/platformvm/block/proposal_block.go:20`).
- **Commit / Abort** (`commit_block.go`, `abort_block.go`) — the two children
  of a proposal block; exactly one is accepted, encoding the
  reward/no-reward decision.
- **Atomic** (`atomic_block.go`) — Apricot-only; wrapped a single import/export
  tx before ApricotPhase5. Post-Phase5 atomic txs go into standard blocks.

`CommonBlock` (`common_block.go:12`) holds parent ID, height, and the computed
`BlockID` (sha256 of the serialized block). The block codec registers types per
upgrade (`vms/platformvm/block/codec.go`), reusing the tx codec version.

### 4.4 State

`state.Chain` (`vms/platformvm/state/state.go:113`) is the read/write interface
that both the persistent `State` and in-memory `Diff`s satisfy. It exposes:
timestamp, the Etna fee state (`gas.State`), the L1-validator excess and accrued
fees, per-subnet current supply, UTXOs and reward UTXOs, subnets and subnet
owners, subnet→L1 conversions, subnet transformations, chains, and txs. Staker
operations are split across `Stakers`/`CurrentStakers`/`PendingStakers`
(`vms/platformvm/state/stakers.go:33`) and `L1Validators`
(`vms/platformvm/state/l1_validator.go:34`).

A **`Staker`** (`vms/platformvm/state/staker.go:22`) is the in-memory record for
a current or pending validator/delegator: `TxID`, `NodeID`, `PublicKey`,
`SubnetID`, `Weight`, `StartTime`, `EndTime`, `PotentialReward`, and the
sorting fields `NextTime` + `Priority`. Stakers are ordered by `Less`
(`...:79`): by `NextTime`, then `Priority`, then `TxID`. `NextTime` is the
`StartTime` for pending stakers and the `EndTime` for current ones, so a single
sorted iterator yields the next state change.

An **`L1Validator`** (`vms/platformvm/state/l1_validator.go:81`) is the ACP-77
record keyed by a `ValidationID`. Its constant fields (subnet, node, BLS key,
balance/deactivation owners, start time) never change; its mutable fields are
`Weight`, `MinNonce` (Warp replay protection), and `EndAccumulatedFee`. A
validator is **active** iff `EndAccumulatedFee != 0`; `EndAccumulatedFee` is the
total accrued network fee at which the validator's prepaid balance runs out, at
which point it is auto-deactivated.

**Diffs.** `state.Diff` (`vms/platformvm/state/diff.go:30`) layers mutations on
top of a parent `Chain` and flattens them into the parent via `Apply`
(`...:612`). `NewDiff` builds a diff over a block's parent state; `NewDiffOn`
chains a diff on an arbitrary parent. Diffs are how block verification computes
"the state if this block is accepted" without mutating disk.

**Priorities.** `txs.Priority` (`vms/platformvm/txs/priorities.go:46`) and the
`PendingToCurrentPriorities` map (`...:37`) encode the strict ordering in which
stakers move between sets. The documented invariant is that **permissioned
subnet validators are removed by time advancement, while permissionless stakers
are removed by a `RewardValidatorTx` after time has advanced** — this is why
those two removal paths are distinct.

## 5. Execution flow & staking lifecycle

### 5.1 Transaction execution

Three executors implement `txs.Visitor`:

- **`StandardTx`** (`vms/platformvm/txs/executor/standard_tx_executor.go:77`) —
  for decision txs in standard (and Banff proposal) blocks. Each visitor method
  verifies the tx, runs the UTXO flow-check
  (`backend.FlowChecker.VerifySpend`), then `avax.Consume`/`avax.Produce`s
  UTXOs and applies side effects to the `Diff`. Returns consumed import inputs,
  atomic shared-memory requests, and an optional `onAccept` callback (e.g.
  `CreateChainTx` defers `Config.CreateChain` to acceptance,
  `...:251`).
- **`ProposalTx`** (`vms/platformvm/txs/executor/proposal_tx_executor.go:55`) —
  for the single proposal tx. It is given **two** diffs, `onCommitState` and
  `onAbortState`, assumed to start equal, and mutates each to represent the
  committed vs. aborted outcome.
- **`AtomicTx`** (`atomic_tx_executor.go`) — Apricot-only atomic block path.

`putStaker` (`standard_tx_executor.go:1374`) is the shared add-staker routine.
Pre-Durango it creates a *pending* staker with a future `StartTime`; post-Durango
it computes `PotentialReward` immediately, bumps `CurrentSupply`, and creates a
*current* staker starting at the current chain time. Permissioned subnet
validators never get a potential reward.

### 5.2 Block verification

`verifier` (`vms/platformvm/block/executor/verifier.go:39`) handles each block
type:

- **`BanffStandardBlock`** (`...:114`): create a diff on the parent, advance
  time to the block timestamp (`executor.AdvanceTimeTo`), then process all
  decision txs. The block must make a state change or it is rejected with
  `ErrStandardBlockWithoutChanges` (`...:495`).
- **`BanffProposalBlock`** (`...:59`): advance time on `onDecisionState`,
  process the embedded decision txs, then fork `onDecisionState` into
  `onCommitState` and `onAbortState` and run `ProposalTx` against both
  (`...:413`).

`processStandardTxs` (`...:524`) enforces the Etna block gas limit (computing
complexity, consuming gas from the fee state), executes each tx, rejects
duplicate inputs (`ErrConflictingBlockTxs`), merges atomic requests, verifies
inputs are unique against ancestors, and finally deactivates any L1 validators
whose balance can't cover the next second
(`deactivateLowBalanceL1Validators`, `...:676`). All verified state is cached
in `blkIDToState[blkID]` for later acceptance — verification never touches disk.

### 5.3 The proposal / commit / abort oracle flow

This is the P-Chain's distinguishing mechanism. When a validator's staking
period ends, the network must decide whether it earned its reward, and the
deciding input — measured uptime — is **node-local and non-deterministic**, so
it cannot live in a normal deterministic block. The flow:

```
                 BanffProposalBlock { RewardValidatorTx(stakerTxID) }
                 /                                                  \
       prefers commit?                                       prefers commit?
       (uptime >= req)                                       (uptime >= req)
         /                                                          \
  BanffCommitBlock  ── reward paid, staker removed,        BanffAbortBlock ── no reward,
                       supply unchanged                                       staker removed,
                                                                              supply decremented
```

1. The builder issues a `BanffProposalBlock` whose proposal tx is a
   `RewardValidatorTx` naming the staker at the front of the current-staker
   iterator (`buildBlock`, `vms/platformvm/block/builder/builder.go:321`).
2. `ProposalTx` runs `RewardValidatorTx`
   (`proposal_tx_executor.go:331`): it verifies the chain time equals the
   staker's `EndTime`, refunds the staked UTXOs to **both** diffs, and on the
   **commit** diff adds the reward UTXO(s) (`rewardValidatorTx`/`rewardDelegatorTx`).
   On the **abort** diff it decrements `CurrentSupply` by the potential reward
   (`...:414`). The staker is deleted from both diffs.
3. The engine asks the VM for the proposal block's options. `options`
   (`vms/platformvm/block/executor/options.go:55`) builds a commit and an abort
   block and orders them by `prefersCommit` (`...:143`): it fetches the staker's
   Primary Network validator, computes uptime via the uptime calculator, and
   compares it against the required uptime percentage (Primary Network's
   `UptimePercentage`, or the subnet's `UptimeRequirement`). On any error it
   defaults to *prefer commit* (over-reward rather than under-reward) — and must
   not propagate the error, since errors here would be treated as fatal.
4. Consensus votes between the two children. Whichever is accepted applies its
   diff. `acceptor.optionBlock` (`acceptor.go:117`) first accepts the parent
   proposal block (applying its `onDecisionState`), then the chosen option's
   `onAcceptState`.

Acceptance ordering note: the proposal block itself is *not* written to disk
when accepted (`acceptor.proposalBlock`, `acceptor.go:187`) — only its `blkID`
is recorded as last-accepted in memory. It is persisted together with its
accepted child, so a crash never leaves a proposal block on disk without a
decision (Snowman requires the last committed block to be a decision block).

### 5.4 Time advancement & the staking lifecycle

`advanceTimeTo` (`vms/platformvm/txs/executor/state_changes.go:107`) is the
engine of the lifecycle. Given a `newChainTime`, on a fresh diff it:

1. **Promotes pending → current**: iterates pending stakers whose `StartTime <=
   newChainTime`, computes their `PotentialReward` (permissionless only),
   bumps subnet supply, and moves them to the current set. Validators are
   promoted before delegators (a current delegator requires its validator to
   already exist).
2. **Removes finished permissioned validators**: current stakers with `EndTime
   <= newChainTime` *and* `SubnetPermissionedValidatorCurrentPriority` are
   deleted here. Permissionless stakers are **not** removed here — they stop at
   the first non-permissioned staker, because they are removed by
   `RewardValidatorTx` (see §5.3).
3. **(Etna+)** removes stale Warp expiries, advances the dynamic-fee state and
   the continuous validator-fee state, and auto-deactivates L1 validators whose
   accrued fee exceeds their `EndAccumulatedFee` (`advanceValidatorFeeState`,
   `...:335`).

`VerifyNewChainTime` (`...:35`) bounds time advancement: monotonic
(`>= currentChainTime`), within `SyncBound` (10s) of local time, and never past
the next staker-change time, so no set change is ever skipped.

Full lifecycle of a Primary Network validator:

```
AddPermissionlessValidatorTx
   │  (pre-Durango)            (post-Durango)
   ▼                              │
PENDING set ── time reaches ──►   │ immediately
StartTime, supply bumped ─────► CURRENT set (counted in validator set, sampled by consensus)
                                   │  uptime tracked while connected
                                   ▼  time reaches EndTime
                          RewardValidatorTx proposal block
                                  / commit            \ abort
                       stake refunded +            stake refunded,
                       reward minted               no reward, supply decremented
                                  \                /
                                   ▼
                              removed from CURRENT set
```

### 5.5 Block production

`builder.WaitForEvent` (`builder/builder.go:97`) sleeps until either the next
staker-change time is reached or the mempool signals pending txs.
`buildBlock` (`...:272`) packs txs (Etna vs. Durango packers, honoring gas/size
limits), then **prioritizes rewards**: if a staker's `EndTime` equals the new
chain time it builds a `BanffProposalBlock` with a `RewardValidatorTx`
(`...:325`); otherwise it builds a `BanffStandardBlock` with the packed txs, or
declines (`ErrNoPendingBlocks`) if there's nothing to do.

## 6. Component boundaries & relationships

### 6.1 The validator-set provider boundary (most important)

The P-Chain `VM` *is* the node's `validators.State`. The actual implementation
is `validators.Manager` (`vms/platformvm/validators/manager.go:48`), assigned to
`vm.State`. The rest of the node never reads P-Chain state directly; it calls
this interface. Key methods:

- **`GetValidatorSet(ctx, height, subnetID)`** (`manager.go:165`) — returns the
  validator set (nodeID → weight + BLS key) for a subnet **at a specific
  historical P-Chain height**. It starts from the *current* set
  (`cfg.Validators.GetMap(subnetID)`) and replays weight diffs and public-key
  diffs backward from the current height down to `targetHeight+1`
  (`ApplyValidatorWeightDiffs` / `ApplyValidatorPublicKeyDiffs`). Results are
  cached per tracked subnet.
- **`GetCurrentHeight` / `GetMinimumHeight`** (`manager.go:129`, `:106`) — the
  current accepted height and a "safely final" lagging height (parent of the
  oldest block in a 30s sliding window). `GetMinimumHeight` is what Snowman++
  uses as the recommended P-Chain height for proposer selection.
- **`GetSubnetID(chainID)`** (`manager.go:316`) — maps a blockchain ID to its
  subnet, used to know *which* validator set secures a given chain.
- **`GetCurrentValidatorSet(ctx, subnetID)`** (`manager.go:342`) and
  **`GetWarpValidatorSets`** (`manager.go:143`) — current sets including ACP-77
  L1 validators, used by the API and Warp verification respectively.

**Who consumes this boundary:**

- **Consensus** ([consensus.md](./consensus.md)) samples each chain's validator
  set by weight; the weights come from `GetValidatorSet`.
- **Snowman++** ([consensus.md](./consensus.md)) selects block proposers from
  the validator set at a recommended P-Chain height (`GetMinimumHeight`).
- **Networking** ([networking.md](./networking.md)) gates peer connections and
  message handling on validator membership; `p2p.NewValidators` wraps this
  `validators.State` (`vms/platformvm/network/network.go:57`).
- **Warp / ICM** verifies aggregated BLS signatures against the signing
  subnet's validator set at the message's P-Chain height.

The `validators.Manager` is fed by the **block acceptor**: `commonAccept`
(`acceptor.go:266`) calls `validators.OnAcceptedBlockID(blkID)` on every accept,
which advances the sliding window. The current in-memory set
(`cfg.Validators`) is mutated as staker state changes are applied, and the
historical diffs are written to disk so any past height can be reconstructed.

Because most of these calls require the P-Chain context lock, the VM wraps the
manager in `validators.NewLockedState` before handing it to the network
(`vms/platformvm/vm.go:194`).

### 6.2 Atomic-memory boundary with X-Chain / C-Chain

`ImportTx` / `ExportTx` move AVAX across chains through **shared atomic memory**
(see [chains.md](./chains.md), [primitives.md](./primitives.md)).
`standardTxExecutor.ImportTx` (`standard_tx_executor.go:312`) reads the imported
UTXOs from `Ctx.SharedMemory.Get(SourceChain, ...)`, flow-checks them, and emits
a `RemoveRequests` atomic request; `ExportTx` (`...:410`) emits `PutRequests`
with the exported UTXOs marshaled for the destination chain. These requests are
returned up to the block verifier and applied **atomically with the database
commit** at acceptance time via `SharedMemory.Apply(atomicRequests, batch)`
(`acceptor.go:246`). The P-Chain's secp256k1fx codec registration order is
deliberately matched to the AVM's (`vms/platformvm/txs/codec.go:67`) so UTXO
type IDs line up across the shared memory boundary. Import verification is
skipped while bootstrapping or partial-syncing.

### 6.3 Warp / ICM boundary

The P-Chain both **signs** and **consumes** Warp messages:

- **Signing**: the node's `WarpSigner` (BLS) is exposed over gRPC by
  `gwarp.Server` (`vms/platformvm/warp/gwarp/server.go`) and used by the network
  layer's ACP-118 handler.
- **Consuming**: ACP-77 L1 transactions are driven by Warp messages.
  `RegisterL1ValidatorTx` (`standard_tx_executor.go:874`) parses the Warp
  message → addressed call → `RegisterL1Validator` payload, verifies it was sent
  from the converted subnet's manager chain/address (`verifyL1Conversion`,
  `...:1449`), checks the expiry window (`RegisterL1ValidatorTxExpiryWindow` =
  1 day), guards against replay via the `expiry` set, verifies the BLS proof of
  possession, and creates the `L1Validator`. `SetL1ValidatorWeightTx`
  (`...:1032`) similarly parses an `L1ValidatorWeight` payload, enforces a
  monotonic `MinNonce`, refuses to remove the last validator
  (`errRemovingLastValidator`), and refunds remaining balance when weight is set
  to 0.
- **Verification**: the network's `signatureRequestVerifier`
  (`vms/platformvm/network/warp.go:50`) answers ACP-118 signature requests for
  P-Chain-addressed-call messages, and `manager.VerifyTx`
  (`block/executor/manager.go:151`) verifies inbound Warp messages on mempool
  txs against the validator state at the recommended P-Chain height.

### 6.4 Uptime boundary

The `uptime.Manager` (`vms/platformvm/vm.go:156`) tracks per-node connectivity.
`VM.Connected`/`Disconnected` (`vms/platformvm/vm.go:472`) feed it, and the
proposal-block option logic (`options.prefersCommit`) reads
`CalculateUptimePercentFrom` to decide reward eligibility. This is the only
place node-local, non-consensus data influences the chain — and it does so only
through the commit/abort vote, never directly in deterministic state.

## 7. Key behaviors, invariants & edge cases

- **Reward math.** `calculator.Calculate`
  (`vms/platformvm/reward/calculator.go:42`) computes
  `Reward = RemainingSupply × (StakedAmount/CurrentSupply) × MintingRate ×
  (StakingDuration/MintingPeriod)`, where the minting rate interpolates between
  `MinConsumptionRate` and `MaxConsumptionRate` by stake duration. It is
  asymptotic to `SupplyCap` and clamped to `RemainingSupply`. All arithmetic is
  big-int to avoid overflow; the invariant is that `supply + reward` never
  exceeds the cap.
- **Reward split (delegation).** `reward.Split`
  (`vms/platformvm/reward/calculator.go:72`) divides a delegator's potential
  reward between delegator and delegatee using the validator's `Shares`
  (out of `PercentDenominator` = 1,000,000). For validators that started after
  CortinaTime, the delegatee's cut is *accrued* on the validator's staking info
  and paid out when the validator itself leaves
  (`rewardDelegatorTx`, `proposal_tx_executor.go:611`); pre-Cortina it was paid
  immediately.
- **Potential reward is locked in at promotion.** A staker's reward is computed
  from the supply at the moment it enters the current set (post-Durango: at
  add; pre-Durango: at pending→current promotion). Aborting the reward later
  *decrements* supply by exactly that amount, keeping supply consistent
  (`proposal_tx_executor.go:414`).
- **Commit-on-error.** If uptime can't be computed, the option logic defaults to
  commit and must swallow the error (a returned error would be fatal)
  (`options.go:76`).
- **Standard blocks must change state** (`ErrStandardBlockWithoutChanges`).
- **Conflicting/duplicate inputs** within a block or against pinned ancestors
  are rejected (`ErrConflictingBlockTxs`, `verifyUniqueInputs`).
- **`TransformSubnetTx` is forbidden post-Etna** (`errTransformSubnetTxPostEtna`);
  ACP-77 `ConvertSubnetToL1Tx` is the modern path.
- **Stakeable locks**: `stakeable.LockOut`/`LockIn`
  (`vms/platformvm/stakeable/stakeable_lock.go`) wrap a transfer with a
  `Locktime`; they may not be nested and require non-zero locktime.
- **L1 active-validator capacity.** Activating an L1 validator fails with
  `errMaxNumActiveValidators` if the active count reaches
  `ValidatorFeeConfig.Capacity`. Low-balance active validators are
  auto-deactivated each block and each time advancement.
- **Partial sync.** With `PartialSyncPrimaryNetwork`, import txs are disallowed
  (`ErrImportTxWhilePartialSyncing`) and a tx that would make this node a
  primary validator logs a health warning.
- **BLS keys.** Primary Network validators must supply a `signer.ProofOfPossession`
  (`vms/platformvm/signer/proof_of_possession.go`) proving ownership of the BLS
  public key; the empty signer is used for subnet validators. L1 validators
  carry their BLS key (uncompressed) in state for Warp aggregation.

## 8. Configuration & parameters

`config.Internal` (`vms/platformvm/config/internal.go`) carries the
node-supplied, consensus-relevant parameters:

| Field | Meaning |
|-------|---------|
| `Validators` | The live `validators.Manager` (in-memory current sets) |
| `MinValidatorStake` / `MaxValidatorStake` | Bounds on Primary Network validator stake |
| `MinDelegatorStake` | Minimum delegation amount |
| `MinDelegationFee` | Minimum delegation `Shares` a validator may charge |
| `MinStakeDuration` / `MaxStakeDuration` | Staking period bounds |
| `UptimePercentage` | Minimum uptime to earn a Primary Network reward |
| `RewardConfig` | `MaxConsumptionRate`, `MinConsumptionRate`, `MintingPeriod`, `SupplyCap` (`reward/config.go`) |
| `UpgradeConfig` | Activation times for Apricot/Banff/Cortina/Durango/Etna/Helicon |
| `DynamicFeeConfig` | Etna dynamic gas-fee config (`gas.Config`) |
| `ValidatorFeeConfig` | ACP-77 continuous validator-fee config (target, capacity, price) |
| `SybilProtectionEnabled`, `PartialSyncPrimaryNetwork`, `TrackedSubnets` | Validation scope |

`config.Config` (`vms/platformvm/config/config.go:34`) holds execution/cache
tuning (cache sizes, `MempoolGasCapacity` = 1,000,000, `MempoolPruneFrequency` =
30m, checksum toggle) parsed from the chain's config bytes.

Reward constant: `reward.PercentDenominator` = 1,000,000
(`vms/platformvm/reward/config.go:12`). Execution constants:
`MaxFutureStartTime` = 2 weeks, `SyncBound` = 10s,
`RegisterL1ValidatorTxExpiryWindow` = 1 day
(`txs/executor/proposal_tx_executor.go:23`, `standard_tx_executor.go:42`).

## 9. Cross-references

- [overview.md](./overview.md) — where the P-Chain sits in the node
- [consensus.md](./consensus.md) — Snowman + Snowman++ consuming validator sets
- [simplex.md](./simplex.md) — alternative consensus engine
- [networking.md](./networking.md) — gossip transport, validator-gated peering
- [node.md](./node.md) — node bootstrap, chain manager
- [vm-framework.md](./vm-framework.md) — generic `ChainVM`, mempool, RPC plumbing
- [avm.md](./avm.md) — X-Chain, shared secp256k1fx asset model & shared memory
- [evm.md](./evm.md) — C-Chain
- [chains.md](./chains.md) — chain manager & atomic shared memory
- [database.md](./database.md) — versioned database layer under P-Chain state
- [api.md](./api.md) — JSON-RPC conventions for `platform.*`
- [primitives.md](./primitives.md) — UTXOs, transferable in/out, codecs, BLS
