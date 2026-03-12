# Auto-Renewed Validators

This document describes the implementation of auto-renewed staking (ACP-236) for primary network validators on the P-Chain. For the full specification, see the ACP-236 proposal.

## Transaction Types

Three new transaction types are introduced, all gated behind the Helicon upgrade.

### AddAutoRenewedValidatorTx

Creates a new auto-renewed validator. Defined in `txs/add_auto_renewed_validator_tx.go`.

Verification rules (in `txs/executor/staker_tx_verification.go`):
- Weight must be between `minValidatorStake` and `maxValidatorStake`.
- Delegation fee must be >= `minDelegationFee`.
- Period must be between `minStakeDuration` and `maxStakeDuration`.
- NodeID must not already be validating on the primary network.
- UTXO flow check must pass.

On execution (in `txs/executor/standard_tx_executor.go`):
- Potential reward is calculated for the first cycle.
- Current supply is updated.
- A staker is added with `StartTime = chainTimestamp` and `EndTime = StartTime + Period`.
- StakingInfo metadata is initialized with the auto-renew configuration.

### SetAutoRenewedValidatorConfigTx

Updates the auto-renew configuration. Defined in `txs/set_auto_renewed_validator_config_tx.go`.

Verification rules (in `txs/executor/staker_tx_verification.go`):
- Referenced TxID must be an `AddAutoRenewedValidatorTx`.
- Validator must be currently active.
- TxID must match the validator's latest transaction ID.
- If Period > 0, it must be >= `minStakeDuration`.
- Must be authorized by the validator's `Owner`.

On execution (in `txs/executor/standard_tx_executor.go`):
- StakingInfo is updated with new `AutoCompoundRewardShares` and `Period`. Changes take effect at the next cycle boundary.

### RewardAutoRenewedValidatorTx

Issued by the block builder at the end of each cycle. Defined in `txs/reward_auto_renewed_validator_tx.go`. Includes a `Timestamp` field to ensure unique transaction IDs across cycles.

This is a proposal transaction with commit and abort paths, handled in `txs/executor/proposal_tx_executor.go`.

## Cycle End Processing

When a cycle ends, the block builder (in `block/builder/builder.go`) issues a `RewardAutoRenewedValidatorTx` as a proposal block.

### Commit Path

Taken when the validator has sufficient uptime and is eligible for rewards.

**If Period > 0 (continue validating):**

1. The current cycle's `PotentialReward` and `DelegateeReward` are each split according to `AutoCompoundRewardShares`. The restake portion increases validator weight; the withdrawal portion creates UTXOs. Accrued values (`AccruedRewards`, `AccruedDelegateeRewards`) are not split — they carry forward and are added to with the restake portion.

2. If the new weight would exceed `MaxValidatorStake`, excess rewards are withdrawn proportionally between validation and delegatee streams (see `createOverflowUTXOs` in `txs/executor/proposal_tx_executor.go`).

3. A new cycle begins immediately with `StartTime` = previous `EndTime`, new `PotentialReward` recalculated, and `DelegateeReward` reset. `AccruedRewards` and `AccruedDelegateeRewards` accumulate across cycles.

**If Period == 0 (graceful exit):**

All rewards (`PotentialReward` + `AccruedRewards` + `DelegateeReward` + `AccruedDelegateeRewards`) are withdrawn. Principal is returned. Validator is removed.

### Abort Path

Taken when the validator did not meet uptime requirements, regardless of Period. Auto-renewal is conditioned on reward eligibility; failing uptime forces exit.
- Principal is returned.
- `AccruedRewards`, `AccruedDelegateeRewards`, and `DelegateeReward` are returned.
- Current cycle's `PotentialReward` is forfeited.

## StakingInfo Metadata

Mutable state is stored separately from the core `Staker` record in a `StakingInfo` structure (see `state/metadata_validator.go`):

- `DelegateeReward` - pending delegatee rewards from the current cycle.
- `AccruedRewards` - validation rewards carried forward from previous cycles.
- `AccruedDelegateeRewards` - delegatee rewards carried forward from previous cycles.
- `AutoCompoundRewardShares` - current restake ratio.
- `Period` - current cycle duration (0 means stop at cycle end).
- `StakerEndTime` - Unix timestamp of the current cycle's end.

## UTXO Creation

Attached to `AddAutoRenewedValidatorTx`:
- Initial stake outputs (returned when validator stops).

Attached to `RewardAutoRenewedValidatorTx`:
- Withdrawal portion of rewards (based on `AutoCompoundRewardShares`).
- Overflow rewards when restaking would exceed `MaxValidatorStake`.
- All accrued rewards when validator stops (graceful exit or forced).

## API

The `GetStakers` endpoint (in `service.go`) returns `Period`, `AutoCompoundRewardShares`, and `ConfigOwner` for auto-renewed validators.
