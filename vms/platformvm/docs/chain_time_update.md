# Chain time update mechanism

The activation of the Banff fork changes the way P-chain tracks its `ChainTime`. In this brief document we detail these changes.

## About `ChainTime`

One of the P-chain's main responsibilities is to record staking periods of any staker (i.e. any validator or delegator) on any subnet to duly reward their activity.

The P-chain tracks a network agreed timestamp called `ChainTime` that allows nodes to reach agreement about when a staker starts and stops staking. These start/stop times are basic inputs to determine whether the staker should be rewarded based on what percentage of `ChainTime` it was perceived as active from other validators.

Note that this `ChainTime` has nothing to do with the `Snowman++` timestamp. `Snowman++` timestamps are local times used to reduce network congestion and have no role in rewarding of any staker.

## Pre Banff fork context

Before the Banff fork activation, `ChainTime` was incremented by an `AdvanceTimeTx` transaction, being included into an `ApricotProposalBlock` block type. Validators voted on `ChainTime` advance by accepting either the `ApricotCommitBlock` or the `ApricotAbortBlock` following the `ApricotProposalBlock`. `ChainTime` was moved ahead only if the `CommitBlock` was accepted.

`AdvanceTimeTx` transactions are subject to three main validations:

1. *Strict Monotonicity*: proposed time must be *strictly* greater than current `ChainTime`.
2. *Synchronicity*: proposed time must not be greater than node’s current time plus a synchronicity bound (currently set to 10 seconds).
3. *No Skipping*: proposed time must be less than or equal to the next staking event, that is start/end of any staker.

Note that *Synchronicity* makes sure that `ChainTime` approximates “real” time flow. If we dropped synchronicity requirement, a staker could declare any staking time and immediately push `ChainTime` to the end, so as to pocket a reward without having actually carried out any activity in the “real” time.

## Post Banff fork context

Following the Banff fork activation, `AdvanceTimeTx`s cannot be included anymore in any block. Instead, each P-chain block type explicitly serializes a timestamp so that `ChainTime` is set to the block timestamp once the block is accepted.

Validation rules for block timestamps varies slightly depending on block types:

* `BanffCommitBlock`s and `BanffAbortBlock`s timestamp must be equal to the timestamp of the `BanffProposalBlock` they depend upon.
* `BanffStandardBlock`s and `BanffProposalBlock`s share `AdvanceTimeTx`s validation rules with the exception of the *strict monotonicity*:
  1. *Monotonicity*: block timestamp must be *greater than or equal to* the current `ChainTime` (which is also its parent's timestamp if the parent was accepted).
  2. *Synchronicity*: block timestamp must not be greater than node’s current time plus a synchronicity bound (currently set to 10 seconds).
  3. *No Skipping*: proposed time must be less than or equal to the next staking event (a staker starting or stopping).
