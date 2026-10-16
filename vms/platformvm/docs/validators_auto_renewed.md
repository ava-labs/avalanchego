# Auto-Renewed Validators

[ACP-236](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/236-auto-renewed-staking/README.md) introduces auto-renewed staking for primary network validators on the P-Chain. Instead of requiring a validator operator to manually re-stake after each fixed term, an auto-renewed validator can immediately start another validation cycle when its current cycle ends, as long as it is eligible for rewards and `NextPeriod` is non-zero.

The implementation adds three Helicon-gated transaction types:

- `AddAutoRenewedValidatorTx` creates a primary network validator that can automatically renew.
- `SetAutoRenewedValidatorConfigTx` updates the validator's auto-renew configuration for the next cycle.
- `RewardAutoRenewedValidatorTx` is issued by block builders at each cycle boundary to settle rewards and either start the next cycle or remove the validator.

The Igloo upgrade adds three more transaction types:

- `IncreaseAutoRenewedValidatorStakeTx` locks additional stake for the validator, which is added to the validator's weight at the start of its next cycle.
- `CancelAutoRenewedValidatorStakeIncreaseTx` cancels a stake increase that has not been applied yet, and returns its stake.
- `DecreaseAutoRenewedValidatorStakeTx` withdraws stake from the validator at the start of its next cycle.

A cycle is one active validation interval for an auto-renewed validator. The
first cycle duration comes from `AddAutoRenewedValidatorTx.Period`; later cycle
durations come from `StakingInfo.NextPeriod`. A `NextPeriod` of 0 means the
validator exits at the next cycle boundary instead of renewing.

## StakingInfo Metadata

Mutable auto-renewed validator state is stored separately from the core `Staker` record in `state.StakingInfo`:

- `DelegateeReward` - pending validator commission from delegator rewards accumulated during the current validator cycle.
- `AccruedValidationRewards` - validation rewards restaked by previous auto-renewal commits.
- `AccruedDelegateeRewards` - delegatee rewards restaked by previous auto-renewal commits.
- `AutoCompoundRewardShares` - percentage of current cycle rewards to restake at cycle end, expressed in millionths.
- `NextPeriod` - next validation cycle duration in seconds. A value of 0 means the validator stops at the end of the current cycle.
- `PendingStakeIncrease` - stake locked by `IncreaseAutoRenewedValidatorStakeTx` during the current cycle, which is added to the validator's weight at the start of the next cycle.
- `AccruedStakeIncrease` - stake increases added to the validator's weight by previous auto-renewal commits.
- `PendingStakeIncreaseTxIDs` - IDs of the `IncreaseAutoRenewedValidatorStakeTx`s that locked `PendingStakeIncrease`.
- `AccruedStakeIncreaseTxIDs` - IDs of the `IncreaseAutoRenewedValidatorStakeTx`s that locked `AccruedStakeIncrease`.
- `PendingStakeDecrease` - stake to withdraw at the start of the next cycle.
- `WithdrawnStake` - stake withdrawn from the validator's stake outputs in previous cycles.

The validator's effective weight is the original stake plus `AccruedValidationRewards`, `AccruedDelegateeRewards`, and `AccruedStakeIncrease`, minus `WithdrawnStake`; the implementation persists this weight on the renewed `Staker`. The current cycle's `StartTime`, `EndTime`, `PotentialReward`, and weight remain on the `Staker` record itself.

`StakingInfo` is initialized from `AddAutoRenewedValidatorTx`, updated by `SetAutoRenewedValidatorConfigTx`, `IncreaseAutoRenewedValidatorStakeTx`, `CancelAutoRenewedValidatorStakeIncreaseTx` and `DecreaseAutoRenewedValidatorStakeTx`, and updated again at cycle end when rewards are restaked or withdrawn.

## Transaction Types

### AddAutoRenewedValidatorTx

Creates a new primary network auto-renewed validator.

Verification rules:

- Weight must be between `minValidatorStake` and `maxValidatorStake`.
- Delegation fee must be >= `minDelegationFee`.
- `Period` must be between `minStakeDuration` and `maxStakeDuration`. On execution, it is stored as `StakingInfo.NextPeriod`.
- NodeID must not already be validating on the primary network.
- `AutoCompoundRewardShares` must be <= `reward.PercentDenominator` (`1_000_000`).
- `Signer` must verify and the staked asset must be AVAX.
- The BaseTx spend must be valid.

On execution:

- Potential reward is calculated for the first cycle.
- Current supply is increased by that potential reward.
- A staker is added with `StartTime = chainTimestamp` and `EndTime = StartTime + Period`.
- `StakingInfo` is initialized with `AutoCompoundRewardShares` and `NextPeriod`.
- BaseTx inputs are consumed and BaseTx outputs are produced.

### SetAutoRenewedValidatorConfigTx

Updates the auto-renew configuration.

Verification rules:

- Referenced TxID must be an `AddAutoRenewedValidatorTx`.
- Validator must be currently active.
- TxID must match the active validator's latest transaction ID, so stale configuration transactions cannot update a later validator that reuses the same NodeID.
- `AutoCompoundRewardShares` must be <= `reward.PercentDenominator` (`1_000_000`).
- If `Period > 0`, it must be >= `minStakeDuration`. A zero period is allowed because it requests removal at the next cycle boundary.
- Must be authorized by the validator's `ValidatorAuthority`.
- The BaseTx spend must be valid.

On execution:

- `StakingInfo.AutoCompoundRewardShares` is updated, and `Period` is stored as the new `StakingInfo.NextPeriod`.
- BaseTx inputs are consumed and BaseTx outputs are produced.

### IncreaseAutoRenewedValidatorStakeTx

Locks additional stake for an auto-renewed validator without changing its current cycle. The stake is locked when the transaction is accepted, but it only increases the validator's weight at the start of the next cycle, so the current cycle's weight, `PotentialReward`, and delegation capacity are unaffected.

Verification rules:

- Igloo must be active.
- Referenced TxID must be an `AddAutoRenewedValidatorTx`.
- Validator must be currently active.
- TxID must match the active validator's latest transaction ID.
- `StakingInfo.NextPeriod` must be non-zero. Otherwise the validator stops at the end of the current cycle and the stake would never be applied.
- The validator must have fewer than 64 `PendingStakeIncreaseTxIDs` and `AccruedStakeIncreaseTxIDs` combined, which bounds the size of its metadata.
- The validator's weight plus `PendingStakeIncrease` plus the new stake must not exceed `maxValidatorStake`. The stake increase is reserved against `maxValidatorStake` when it is locked, so it is guaranteed to fit when it is applied.
- The staked asset must be AVAX.
- Must be authorized by the validator's `ValidatorAuthority`.
- The BaseTx spend, including the stake outputs, must be valid.

On execution:

- The stake is added to `StakingInfo.PendingStakeIncrease`, and the transaction ID is appended to `StakingInfo.PendingStakeIncreaseTxIDs`.
- BaseTx inputs are consumed and BaseTx outputs are produced. The stake outputs are not produced: like the stake of `AddAutoRenewedValidatorTx`, they are locked until the validator stops.

### CancelAutoRenewedValidatorStakeIncreaseTx

Cancels a pending stake increase and returns its stake. A stake increase is cancelled as a whole, by the ID of the `IncreaseAutoRenewedValidatorStakeTx` that locked it, so that its stake outputs are returned exactly as they were locked.

Verification rules:

- Igloo must be active.
- Referenced TxID must be an `AddAutoRenewedValidatorTx`.
- Validator must be currently active.
- TxID must match the active validator's latest transaction ID.
- `StakeIncreaseTxID` must be in `StakingInfo.PendingStakeIncreaseTxIDs`. A stake increase that was already applied to the validator's weight can't be cancelled; its stake is returned when the validator stops. Cancellation is allowed even if the validator stops at the end of its current cycle.
- Must be authorized by the validator's `ValidatorAuthority`.
- The BaseTx spend must be valid.

On execution:

- The amount of the stake increase is removed from `StakingInfo.PendingStakeIncrease`, and its transaction ID is removed from `StakingInfo.PendingStakeIncreaseTxIDs`. This frees its share of `maxValidatorStake` and of the 64 stake increase transactions.
- The stake outputs of the `IncreaseAutoRenewedValidatorStakeTx` are returned as UTXOs that reference that transaction, to the owners of the outputs rather than to the signer of the cancellation.
- BaseTx inputs are consumed and BaseTx outputs are produced.

### DecreaseAutoRenewedValidatorStakeTx

Asks to withdraw an amount of stake from an auto-renewed validator at the start of its next cycle. The current cycle is not affected.

Verification rules:

- Igloo must be active.
- `Amount` must be greater than 0.
- Referenced TxID must be an `AddAutoRenewedValidatorTx`.
- Validator must be currently active.
- TxID must match the active validator's latest transaction ID.
- `StakingInfo.NextPeriod` must be non-zero. Otherwise the validator stops at the end of the current cycle and all of its stake is returned anyway.
- The validator's weight minus `PendingStakeDecrease` and `Amount` must be at least `minValidatorStake`. Pending stake increases are not counted, as they can still be cancelled.
- Must be authorized by the validator's `ValidatorAuthority`.
- The BaseTx spend must be valid.

On execution:

- `Amount` is added to `StakingInfo.PendingStakeDecrease`.
- BaseTx inputs are consumed and BaseTx outputs are produced. No stake is returned until the next cycle starts.

### RewardAutoRenewedValidatorTx

Settles an auto-renewed validator at the end of a cycle.

Auto-renewed validators keep the original validator transaction ID across cycles. `RewardAutoRenewedValidatorTx` includes the current chain timestamp so reward transactions for different cycles have distinct transaction IDs, and proposal verification requires that timestamp to match the cycle end time.

This is a proposal transaction with commit and abort paths. It has no inputs or outputs; reward and stake-return UTXOs are created by proposal execution. The standard executor rejects it, and `RewardValidatorTx` is rejected for auto-renewed validators.

## Cycle End Processing

When a cycle ends, the block builder issues a `RewardAutoRenewedValidatorTx` as a proposal block. The builder selects this reward transaction type when the next staker to reward was created by `AddAutoRenewedValidatorTx`; other permissionless stakers continue to use `RewardValidatorTx`.

### Commit Path

Taken when the validator has sufficient uptime and is eligible for rewards.

**If `NextPeriod > 0` (continue validating):**

1. `PendingStakeDecrease` is withdrawn. It draws from the validator's stake outputs first: the outputs of the `AddAutoRenewedValidatorTx`, then the outputs of the transactions in `AccruedStakeIncreaseTxIDs`, in that order, skipping the first `WithdrawnStake`. Each drawn amount is returned as a UTXO of the `RewardAutoRenewedValidatorTx`, with the owners and lock time of the output it is drawn from, so a partly drawn output is split. Whatever the stake outputs can't cover is drawn from `AccruedValidationRewards`, then from `AccruedDelegateeRewards`, and paid with the reward UTXOs. The amount drawn from stake outputs is added to `WithdrawnStake`.

2. The current cycle's `PotentialReward` and `DelegateeReward` are each split according to `AutoCompoundRewardShares`. Accrued values (`AccruedValidationRewards`, `AccruedDelegateeRewards`) are already part of the validator's weight and are not split again.

3. `PendingStakeIncrease` is added to the validator's weight and moved to `AccruedStakeIncrease`, and `PendingStakeIncreaseTxIDs` are moved to `AccruedStakeIncreaseTxIDs`. It is applied before the restaked rewards, because it was already reserved against `MaxValidatorStake`.

4. The restaked portions increase validator weight. If the new weight would exceed `MaxValidatorStake`, only the remaining capacity is restaked and the excess is withdrawn.

5. Withdrawn rewards are paid as reward UTXOs on the commit path. Withdrawn validation rewards and withdrawn delegatee rewards are each paid as a separate reward UTXO when non-zero. Each amount includes both the configured withdrawal from `AutoCompoundRewardShares` and any additional overflow from the `MaxValidatorStake` cap.

6. A new cycle begins immediately with `StartTime` set to the previous `EndTime`, `EndTime = StartTime + NextPeriod`, a new `PotentialReward` calculated from the new weight and current supply, and `DelegateeReward` reset. Current supply is increased by the new `PotentialReward`.

**If `NextPeriod == 0` (graceful exit):**

On commit, all rewards (`PotentialReward` + `AccruedValidationRewards` + `DelegateeReward` + `AccruedDelegateeRewards`) are withdrawn as reward UTXOs. Principal is returned as stake UTXOs that reference the original `AddAutoRenewedValidatorTx`. The stake of each transaction in `AccruedStakeIncreaseTxIDs` and `PendingStakeIncreaseTxIDs` is returned as stake UTXOs that reference that `IncreaseAutoRenewedValidatorStakeTx`. Stake that was already withdrawn is not returned again: an output that was fully drawn from is skipped, and the rest of a partly drawn output is returned as a UTXO of the `RewardAutoRenewedValidatorTx`. Validator is removed.

### Abort Path

Taken when the validator did not meet uptime requirements, regardless of `NextPeriod`. Auto-renewal is conditioned on reward eligibility; failing uptime forces exit.

- Principal is returned as stake UTXOs that reference the original `AddAutoRenewedValidatorTx`.
- The stake of each transaction in `AccruedStakeIncreaseTxIDs` and `PendingStakeIncreaseTxIDs` is returned as stake UTXOs that reference that `IncreaseAutoRenewedValidatorStakeTx`.
- Stake that was already withdrawn is not returned again, as on the graceful exit path. A pending stake decrease is not withdrawn, as all of the remaining stake is returned.
- `AccruedValidationRewards`, `AccruedDelegateeRewards`, and `DelegateeReward` are returned as reward UTXOs.
- Current cycle's `PotentialReward` is forfeited.
- Current supply is decreased by the forfeited `PotentialReward`, which was added when the cycle started.

`DelegateeReward` is paid on abort because it is validator commission from
delegator periods that already completed during this validator cycle. It is
separate from the validator's own current-cycle `PotentialReward`, which is
conditioned on meeting uptime requirements and is forfeited on abort.

## API

The `GetCurrentValidators` endpoint embeds an `AutoRenewedConfig` only for auto-renewed validators. The config contains `validatorAuthority`, `nextPeriod`, `autoCompoundRewardShares`, `restakedValidationRewards`, `restakedDelegateeRewards`, `pendingStakeIncrease`, `pendingStakeIncreases`, `appliedStakeIncrease`, `pendingStakeDecrease`, and `withdrawnStake`. `pendingStakeIncreases` lists the transaction ID and amount of each pending stake increase, which are the stake increases that can be cancelled.

RPC consumers need this metadata to distinguish auto-renewed validators from fixed-term validators and to show the validator's pending next-cycle behavior. Wallets, staking dashboards, and operators can use it to display whether the validator will renew or exit at the next cycle boundary, what percentage of rewards will be restaked in the next cycle, and which authority can update the configuration.
