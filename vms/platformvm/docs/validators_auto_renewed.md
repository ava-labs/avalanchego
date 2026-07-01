# Auto-Renewed Validators

[ACP-236](https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/236-auto-renewed-staking/README.md) introduces auto-renewed staking for primary network validators on the P-Chain. Instead of requiring a validator operator to manually re-stake after each fixed term, an auto-renewed validator can immediately start another validation cycle when its current cycle ends, as long as it is eligible for rewards and `NextPeriod` is non-zero.

The implementation adds three Helicon-gated transaction types:

- `AddAutoRenewedValidatorTx` creates a primary network validator that can automatically renew.
- `SetAutoRenewedValidatorConfigTx` updates the validator's auto-renew configuration for the next cycle.
- `RewardAutoRenewedValidatorTx` is issued by block builders at each cycle boundary to settle rewards and either start the next cycle or remove the validator.

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

The validator's effective weight is the original stake plus `AccruedValidationRewards` and `AccruedDelegateeRewards`; the implementation persists this weight on the renewed `Staker`. The current cycle's `StartTime`, `EndTime`, `PotentialReward`, and weight remain on the `Staker` record itself.

`StakingInfo` is initialized from `AddAutoRenewedValidatorTx`, updated by `SetAutoRenewedValidatorConfigTx`, and updated again at cycle end when rewards are restaked or withdrawn.

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

### RewardAutoRenewedValidatorTx

Settles an auto-renewed validator at the end of a cycle.

Auto-renewed validators keep the original validator transaction ID across cycles. `RewardAutoRenewedValidatorTx` includes the current chain timestamp so reward transactions for different cycles have distinct transaction IDs, and proposal verification requires that timestamp to match the cycle end time.

This is a proposal transaction with commit and abort paths. It has no inputs or outputs; reward and stake-return UTXOs are created by proposal execution. The standard executor rejects it, and `RewardValidatorTx` is rejected for auto-renewed validators.

## Cycle End Processing

When a cycle ends, the block builder issues a `RewardAutoRenewedValidatorTx` as a proposal block. The builder selects this reward transaction type when the next staker to reward was created by `AddAutoRenewedValidatorTx`; other permissionless stakers continue to use `RewardValidatorTx`.

### Commit Path

Taken when the validator has sufficient uptime and is eligible for rewards.

**If `NextPeriod > 0` (continue validating):**

1. The current cycle's `PotentialReward` and `DelegateeReward` are each split according to `AutoCompoundRewardShares`. Accrued values (`AccruedValidationRewards`, `AccruedDelegateeRewards`) are already part of the validator's weight and are not split again.

2. The restaked portions increase validator weight. If the new weight would exceed `MaxValidatorStake`, only the remaining capacity is restaked and the excess is withdrawn.

3. Withdrawn rewards are paid as reward UTXOs on the commit path. Withdrawn validation rewards and withdrawn delegatee rewards are each paid as a separate reward UTXO when non-zero. Each amount includes both the configured withdrawal from `AutoCompoundRewardShares` and any additional overflow from the `MaxValidatorStake` cap.

4. A new cycle begins immediately with `StartTime` set to the previous `EndTime`, `EndTime = StartTime + NextPeriod`, a new `PotentialReward` calculated from the new weight and current supply, and `DelegateeReward` reset. Current supply is increased by the new `PotentialReward`.

**If `NextPeriod == 0` (graceful exit):**

On commit, all rewards (`PotentialReward` + `AccruedValidationRewards` + `DelegateeReward` + `AccruedDelegateeRewards`) are withdrawn as reward UTXOs. Principal is returned as stake UTXOs that reference the original `AddAutoRenewedValidatorTx`. Validator is removed.

### Abort Path

Taken when the validator did not meet uptime requirements, regardless of `NextPeriod`. Auto-renewal is conditioned on reward eligibility; failing uptime forces exit.

- Principal is returned as stake UTXOs that reference the original `AddAutoRenewedValidatorTx`.
- `AccruedValidationRewards`, `AccruedDelegateeRewards`, and `DelegateeReward` are returned as reward UTXOs.
- Current cycle's `PotentialReward` is forfeited.
- Current supply is decreased by the forfeited `PotentialReward`, which was added when the cycle started.

`DelegateeReward` is paid on abort because it is validator commission from
delegator periods that already completed during this validator cycle. It is
separate from the validator's own current-cycle `PotentialReward`, which is
conditioned on meeting uptime requirements and is forfeited on abort.

## API

The `GetStakers` endpoint embeds an `AutoRenewedConfig` only for auto-renewed validators. The config contains `validatorAuthority`, `nextPeriod`, and `autoCompoundRewardShares`.

RPC consumers need this metadata to distinguish auto-renewed validators from fixed-term validators and to show the validator's pending next-cycle behavior. Wallets, staking dashboards, and operators can use it to display whether the validator will renew or exit at the next cycle boundary, what percentage of rewards will be restaked in the next cycle, and which authority can update the configuration.
