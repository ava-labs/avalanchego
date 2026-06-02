# Platform VM (P-Chain)

The P-Chain (`vms/platformvm/`) is the Snowman-based VM that manages:
- Primary network validators and delegators
- Subnets and their validators
- L1 (pay-as-you-go) validators post-Etna
- Cross-chain imports/exports (via atomic memory)
- Staking rewards and fee collection

## Directory Layout

```
vms/platformvm/
├── vm.go              # VM initialization and interface implementation
├── block/
│   ├── builder/       # Block building from mempool
│   └── executor/      # Block execution (verify/accept/reject)
├── txs/
│   ├── executor/      # Transaction verification and state mutation
│   ├── fee/           # Fee calculation
│   ├── mempool/       # Pending transaction pool
│   └── *.go           # All transaction type definitions
├── state/             # State management, diff, persistence
├── validators/        # Validator set management helpers
├── network/           # P2P gossip for P-chain transactions
├── reward/            # Reward calculator
├── warp/              # Warp message signing/verification
└── config/            # P-chain configuration parameters
```

---

## 1. VM Interface Implementation

The VM struct implements `snowman.ChainVM` and several optional extensions:
- `snowman.BuildBlockWithContextChainVM` (uses P-chain height in block building)
- `snowman.SetPreferenceWithContextChainVM`
- `secp256k1fx.VM`
- `validators.State` (P-chain validator lookups for other VMs)

### 1.1 Initialize Sequence

```go
func (vm *VM) Initialize(ctx, chainCtx, db, genesisBytes, _, configBytes, _, appSender) error
```

1. Load `ExecutionConfig` from `configBytes`.
2. Create codec (linearcodec) and register all types.
3. Initialize `secp256k1fx.Fx`.
4. Create `RewardCalculator` with `RewardConfig`.
5. Initialize `State` from genesis bytes.
6. Create `ValidatorManager` (wraps snow's validators.Manager, syncs to state).
7. Create `UTXOVerifier` for secp256k1fx verification.
8. Create `UptimeManager` using state's uptime storage.
9. Build `TxExecutorBackend` (carries all verification dependencies).
10. Create `Mempool` with gas-based ordering.
11. Create `BlockExecutorManager`.
12. Initialize `Network` (push/pull gossip for txs).
13. Create `BlockBuilder` for proposing blocks.
14. Initialize all tracked subnets/chains from state.
15. Set preference to last accepted block.
16. Start periodic mempool pruning goroutine.
17. Start async block height index goroutine.

### 1.2 Key Lifecycle Methods

| Method | Action |
|--------|--------|
| `ParseBlock(bytes)` | Deserialize → `block.Parse()` → wrap in stateful `manager.NewBlock()` |
| `GetBlock(id)` | Retrieve from state by block ID |
| `LastAccepted()` | Return state's last accepted block ID |
| `SetPreference(id)` | Update the preferred tip; recalculate reorg depth |
| `BuildBlock(ctx)` | Pull from builder; uses P-chain height for proposer context |
| `SetState(state)` | Transition between Bootstrapping and NormalOp |
| `Connected(nodeID, version)` | Notify uptime manager |
| `Disconnected(nodeID)` | Notify uptime manager |

---

## 2. Transaction Types

All transactions implement the `Visitor` interface for double-dispatch execution:

```go
type Visitor interface {
    AddValidatorTx(*AddValidatorTx) error
    AddSubnetValidatorTx(*AddSubnetValidatorTx) error
    AddDelegatorTx(*AddDelegatorTx) error
    CreateChainTx(*CreateChainTx) error
    CreateSubnetTx(*CreateSubnetTx) error
    ImportTx(*ImportTx) error
    ExportTx(*ExportTx) error
    AdvanceTimeTx(*AdvanceTimeTx) error
    RewardValidatorTx(*RewardValidatorTx) error
    RemoveSubnetValidatorTx(*RemoveSubnetValidatorTx) error
    TransformSubnetTx(*TransformSubnetTx) error
    AddPermissionlessValidatorTx(*AddPermissionlessValidatorTx) error
    AddPermissionlessDelegatorTx(*AddPermissionlessDelegatorTx) error
    TransferSubnetOwnershipTx(*TransferSubnetOwnershipTx) error
    BaseTx(*BaseTx) error
    ConvertSubnetToL1Tx(*ConvertSubnetToL1Tx) error
    RegisterL1ValidatorTx(*RegisterL1ValidatorTx) error
    SetL1ValidatorWeightTx(*SetL1ValidatorWeightTx) error
    IncreaseL1ValidatorBalanceTx(*IncreaseL1ValidatorBalanceTx) error
    DisableL1ValidatorTx(*DisableL1ValidatorTx) error
}
```

### 2.1 Apricot Era

**AddValidatorTx** — Join primary network validator set.
- Fields: `NodeID`, `Start/End time`, `Stake amount`, `DelegationShares` (basis points), `StakeOuts`, `RewardsOwner`.
- Locked for entire duration; delegation allowed.

**AddDelegatorTx** — Delegate to primary network validator.
- Fields: `Validator NodeID`, `Start/End time`, `Stake amount`, `DelegatorRewardsOwner`.
- Must end before the validator it delegates to.

**AddSubnetValidatorTx** — Join a permissioned subnet.
- Fields: `SubnetValidator{NodeID, SubnetID, Weight, Start/End}`, `SubnetAuth` (subnet owner signature).

**CreateSubnetTx** — Create a new subnet.
- Fields: `Owner fx.Owner` (controls who can add validators).
- SubnetID = hash of this transaction.

**CreateChainTx** — Create a blockchain within a subnet.
- Fields: `SubnetID`, `ChainName` (ASCII ≤128 chars), `VMID`, `FxIDs`, `GenesisData` (≤1 MiB).
- Requires subnet authorization.

**ImportTx** — Import UTXOs from another chain via atomic memory.
- Fields: `SourceChain ids.ID`, `ImportedInputs []*TransferableInput`.

**ExportTx** — Export UTXOs to another chain via atomic memory.
- Fields: `DestinationChain ids.ID`, `ExportedOutputs []*TransferableOutput`.
- Cannot export locked/stakeable outputs.

**AdvanceTimeTx** — Advance chain clock (staker activation).
- Fields: `Time uint64`.
- Constraint: `Time > current`, `Time ≤ next staker set change`.
- When accepted + committed: pending stakers with `StartTime ≤ Time` move to current set.

**RewardValidatorTx** — Remove staker and distribute rewards.
- Fields: `TxID ids.ID` (the staker's creation tx).
- `CommitBlock`: rewards distributed; `AbortBlock`: staker removed, no reward.

### 2.2 Banff Era

**RemoveSubnetValidatorTx** — Remove a permissioned subnet validator.

**TransformSubnetTx** (deprecated post-Etna) — Convert to custom-token staking.
- Specifies: `AssetID`, supply limits, consumption rates, stake duration bounds, delegation fee floor.

**AddPermissionlessValidatorTx** — Join primary or permissionless subnet without owner permission.
- Fields: `Signer` (BLS key for primary network), `ValidationRewardsOwner`, `DelegatorRewardsOwner`, `DelegationShares`.

**AddPermissionlessDelegatorTx** — Delegate to permissionless validator.

### 2.3 Durango Era

**TransferSubnetOwnershipTx** — Change subnet owner.
- Requires current owner authorization.

**BaseTx** — Simple UTXO value transfer (no staking).

### 2.4 Etna Era (L1 Validators)

**ConvertSubnetToL1Tx** — Irreversible conversion to pay-as-you-go L1.
- Fields: `ChainID`, `Address` (subnet manager contract), `Validators []L1ValidatorInit`, `SubnetAuth`.

**RegisterL1ValidatorTx** — Register L1 validator.
- Fields: `Balance` (initial fee balance), `ProofOfPossession` (BLS PoP), `Message` (warp message from subnet manager).

**SetL1ValidatorWeightTx** — Adjust L1 validator weight via warp message from subnet manager.

**IncreaseL1ValidatorBalanceTx** — Add to L1 validator's accrued fee balance.

**DisableL1ValidatorTx** — Deactivate L1 validator; remaining balance returned to `RemainingBalanceOwner`.

---

## 3. Block Types

### 3.1 Hierarchy

```
Block (interface)
├── ApricotStandardBlock      — list of txs, no timestamp
│   └── BanffStandardBlock   — adds timestamp
├── ApricotProposalBlock      — single proposal tx (staking)
│   └── BanffProposalBlock   — adds timestamp + extra txs
├── ApricotAtomicBlock        — single atomic (cross-chain) tx
├── ApricotAbortBlock         — decision: abort proposal
│   └── BanffAbortBlock
└── ApricotCommitBlock        — decision: commit proposal
    └── BanffCommitBlock
```

### 3.2 Block Semantics

**StandardBlock**: Execute all txs as regular state mutations. Accepted or rejected atomically.

**ProposalBlock**: Single staking proposal. Always followed by a CommitBlock or AbortBlock child.
- `onCommitState` diff: applied if CommitBlock accepted.
- `onAbortState` diff: applied if AbortBlock accepted.

**AtomicBlock**: Single cross-chain tx (Apricot only).

**CommitBlock / AbortBlock**: Decision blocks; no txs. Finalise their parent proposal.

### 3.3 Block ID

`BlockID = SHA256(serialized_block_bytes)`.

---

## 4. Execution Engine

### 4.1 Two-Phase Verification

Every transaction goes through:

1. **SyntacticVerify** — format, field constraints, codec version. No state access.
2. **SemanticVerify** — signature validation, UTXO availability, fee sufficiency. Reads state.

### 4.2 StandardTxExecutor

Handles: `BaseTx`, `AddSubnetValidatorTx`, `CreateSubnetTx`, `CreateChainTx`, `ImportTx`, `ExportTx`, `RemoveSubnetValidatorTx`, `AddPermissionlessValidatorTx/Delegator`, `TransformSubnetTx`, `ConvertSubnetToL1Tx`, `RegisterL1ValidatorTx`, `SetL1ValidatorWeightTx`, `IncreaseL1ValidatorBalanceTx`, `DisableL1ValidatorTx`, `TransferSubnetOwnershipTx`.

Returns: `(consumedUTXOIDs, atomicRequests, onAcceptCallback, error)`.

Key actions per type:
- **ImportTx**: Issue atomic remove-request on source chain; verify imported inputs.
- **ExportTx**: Issue atomic put-request on destination chain; consume local inputs.
- **CreateSubnetTx**: Write subnet owner to state.
- **CreateChainTx**: Validate subnet exists + authorized; store chain tx; queue `chainManager.QueueChainCreation()`.

### 4.3 ProposalTxExecutor

Handles: `AddValidatorTx`, `AddDelegatorTx`, `AdvanceTimeTx`, `RewardValidatorTx`.

Creates two state diffs:
```go
onCommitState state.Diff  // applied if CommitBlock wins
onAbortState  state.Diff  // applied if AbortBlock wins
```

**AdvanceTimeTx.Execute**:
- Set `onCommitState.timestamp = tx.Time`.
- For all pending stakers with `StartTime ≤ tx.Time`: move to current set (onCommit only).
- Emit reward UTXOs for stakers ending at `tx.Time`.

**RewardValidatorTx.Execute**:
- Identify staker by `tx.TxID`.
- Calculate `potentialReward` via `RewardCalculator`.
- `onCommit`: transfer rewards to `RewardsOwner` and `DelegatorRewardsOwner`.
- `onAbort`: just remove staker, no reward.

### 4.4 Fee Calculation (`txs/fee/`)

**Pre-Etna (static):**
- Fixed fee per transaction type: `config.TxFee`, `config.CreateSubnetTxFee`, etc.

**Etna era (dynamic gas):**
```go
type Complexity struct {
    NumOperations uint64
    NumSignatures uint64
    DataLength    uint64
    MintOutput    bool
    ExportOutput  bool
}
// Gas = Complexity.ToGas(Weights)
// Fee = Gas × GasPrice (nAVAX/gas)
```

Mempool orders by `gasPrice = totalFee / totalGas` (higher = earlier inclusion).

---

## 5. State Management

### 5.1 State Interface

The `state.State` manages all persistent data for the P-Chain:

- **UTXO set**: by UTXO ID, with indexed lookup by address
- **Stakers**: current + pending validator and delegator sets
- **Subnets**: owners, configurations, L1 conversions
- **Chains**: creation txs, indexed by subnet
- **Transactions**: by TxID with acceptance status
- **Reward UTXOs**: by staker TxID
- **Timestamps and fee state**

### 5.2 State Diff (Copy-on-Write)

```go
type Diff struct {
    parentID           ids.ID
    timestamp          time.Time
    currentSupply      map[ids.ID]uint64
    modifiedUTXOs      map[ids.ID]*avax.UTXO
    currentStakerDiffs diffStakers  // additions/removals from current set
    pendingStakerDiffs diffStakers  // additions/removals from pending set
    subnetOwners       map[ids.ID]fx.Owner
    addedChains        map[ids.ID][]*txs.Tx
}
```

On `diff.Apply(parentState)`:
- Write all changes to parent.
- Misses fall through to parent's state (reads not cached in diff go to parent).

On rejection: diff discarded, parent unchanged.

### 5.3 Database Prefixes

```
block:{blockID}
validator/current/{nodeID}
validator/pending/{nodeID}
delegator/current/{txID}
delegator/pending/{txID}
subnet:{subnetID}
subnetOwner:{subnetID}
chain:{subnetID}:{chainID}
tx:{txID}
rewardUTXOs:{txID}
utxo:{utxoID}
utxoIndex:{address}:{utxoID}
expiryReplayProtection:{txID}
l1/weights:{subnetID}
l1/active:{validationID}
l1/inactive:{validationID}
timestamp
feeState
lastAccepted
cleanShutdown
```

### 5.4 Staker Ordering

Priority determines ordering when multiple stakers have the same `NextTime`:

**Pending (activation order):**
1. PrimaryNetworkDelegatorApricotPending
2. PrimaryNetworkValidatorPending
3. PrimaryNetworkDelegatorBanffPending
4. SubnetPermissionlessValidatorPending
5. SubnetPermissionlessDelegatorPending
6. SubnetPermissionedValidatorPending

**Current (removal order, lowest removed first):**
1. SubnetPermissionedValidatorCurrent
2. SubnetPermissionlessDelegatorCurrent
3. SubnetPermissionlessValidatorCurrent
4. PrimaryNetworkDelegatorCurrent
5. PrimaryNetworkValidatorCurrent (last)

---

## 6. Staking Mechanics

### 6.1 Staker Struct

```go
type Staker struct {
    TxID        ids.ID
    NodeID      ids.NodeID
    PublicKey   *bls.PublicKey  // primary network only
    SubnetID    ids.ID
    Weight      uint64
    StartTime   time.Time
    EndTime     time.Time
    PotentialReward uint64
    NextTime    time.Time  // min(StartTime, EndTime)
    Priority    txs.Priority
}
```

### 6.2 Validator Lifecycle

```
AddValidatorTx accepted
  → Staker added to pendingValidators

AdvanceTimeTx(T) + CommitBlock
  → All pending stakers with StartTime ≤ T move to currentValidators
  → ValidatorManager weights updated
  → uptime tracking starts (via Connected callback)

RewardValidatorTx(txID) + CommitBlock
  → Staker removed from currentValidators
  → Reward UTXOs created for validator and delegators
  → ValidatorManager weights updated

RewardValidatorTx(txID) + AbortBlock
  → Staker removed, no reward
```

### 6.3 Reward Formula

```go
func (c *calculator) Calculate(
    stakedDuration time.Duration,
    stakedAmount, currentSupply uint64,
) uint64

// remainingSupply = SupplyCap - currentSupply
// portionOfExisting = stakedAmount / currentSupply
// portionOfDuration = stakedDuration / MaxStakingDuration
// mintingRate = MinRate + (MaxRate - MinRate) * portionOfDuration
// reward = remainingSupply * portionOfExisting * mintingRate * portionOfDuration
```

Configured by `RewardConfig`: `MinConsumptionRate`, `MaxConsumptionRate`, `MintingPeriod`, `SupplyCap`.

### 6.4 Delegator Reward Split

```
delegatorReward = validatorReward × (1 - delegationFee%) × (delegatorWeight / totalDelegatedWeight)
```

`delegationFee` is set by the validator in `AddValidatorTx.DelegationShares` (basis points, max 10000 = 100%).

### 6.5 L1 Validator Accrued Fee Model (Etna)

```go
type L1Validator struct {
    ValidationID    ids.ID
    SubnetID        ids.ID
    NodeID          ids.NodeID
    PublicKey        []byte
    Weight           uint64
    RemainingBalance uint64
    AccumulatedFee   uint64
    EndAccumulatedFee uint64   // fee total when validator will be deactivated
    StartTime        time.Time
    RemainingBalanceOwner []byte
    DeactivationOwner     []byte
}
```

- `RemainingBalance` decreases each block.
- When `RemainingBalance == 0`, validator is removed.
- Leftover balance returned to `RemainingBalanceOwner`.

---

## 7. Mempool

```go
type Mempool struct {
    tree          *btree.BTreeG[meteredTx]           // ordered by gasPrice desc
    txs           map[ids.ID]meteredTx
    consumedUTXOs *setmap.SetMap[ids.ID, ids.ID]      // input UTXO tracking
    droppedTxIDs  *lru.Cache[ids.ID, error]
    gasAvailable  gas.Gas
    weights       gas.Dimensions
}
```

**Add(tx):**
1. Check no consumed UTXO overlap with existing txs.
2. Calculate gas usage.
3. Add to btree if `gas.Used ≤ gas.Available`.
4. Track consumed UTXOs.

**Periodic pruning:** Re-validate all txs against current preferred state; remove invalid ones.

**Gas capacity:** `gasAvailable = totalCapacity - sumOfTxGases`.

---

## 8. Subnet Management

### 8.1 Permissioned Subnet Lifecycle

1. `CreateSubnetTx` — creates subnet with owner.
2. `AddSubnetValidatorTx` — owner authorizes each validator.
3. `CreateChainTx` — creates blockchain on subnet (requires owner auth).
4. `RemoveSubnetValidatorTx` — owner removes validator.
5. `TransferSubnetOwnershipTx` — ownership transferred.

### 8.2 Conversion to L1

`ConvertSubnetToL1Tx`:
- One-way conversion (irreversible).
- Specifies subnet manager contract (`ChainID` + `Address`).
- Provides initial validators with BLS keys and opening balances.
- After conversion: `RegisterL1ValidatorTx` / `SetL1ValidatorWeightTx` drive validator set via warp messages from the manager contract.

### 8.3 Warp Messaging (`vms/platformvm/warp/`)

The P-Chain can:
- **Sign** warp messages to other chains (proves P-Chain state to other VMs).
- **Verify** warp messages from other chains (subnet manager contracts drive L1 validators).

Verification requires ≥80% of validator weight to have signed.

---

## 9. Block Building

### 9.1 Builder (`block/builder/`)

```go
type Builder interface {
    BuildBlock(context.Context) (snowman.Block, error)
}
```

**Algorithm:**
1. Check if there are stakers whose `EndTime ≤ now` — if so, must emit `RewardValidatorTx`.
2. Check if `AdvanceTimeTx` is needed (pending staker activation or timestamp advancement).
3. Otherwise, pack regular txs from mempool up to `TargetBlockSize` (128 KiB).
4. Return block (StandardBlock or ProposalBlock with AdvanceTimeTx).

**`WaitForEvent()`:** Waits for `PendingTxs` message from the mempool/network, or for a timer when stakers are about to expire.

### 9.2 Tx Packing

Transactions sorted by `gasPrice` (highest first). Each tx verified during packing; invalid txs skipped and dropped from mempool.

---

## 10. API (`api/platformvm/`)

All methods are JSON-RPC 2.0. Key endpoints:

**Validator queries:**
- `getCurrentValidators(subnetID)` — active validators
- `getPendingValidators(subnetID)` — pending validators
- `getStakingAssetID(subnetID)` — staking asset

**UTXO queries:**
- `getUTXOs(addresses, sourceChain)` — paginated UTXO listing

**Transaction operations:**
- `issueTx(tx)` — submit signed tx
- `getTx(txID)` — retrieve by ID
- `getTxStatus(txID)` — acceptance status

**Chain/subnet queries:**
- `getBlockchains()` — all chains
- `getSubnets(subnetIDs)` — subnet details

**Supply/economics:**
- `getCurrentSupply(subnetID)` — minted supply
- `getRewardUTXOs(txID)` — reward UTXOs for a staker
- `getMinStake(subnetID)` — minimum stake amounts
