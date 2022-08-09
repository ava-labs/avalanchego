# Blueberry fork: removing Advance Time transactions

The activation of the Blueberry fork changes the way P-chain tracks its `ChainTime`. In this brief document we detail these changes.

## About `ChainTime`

One of the P-chain's main responsibilities is to record staking periods of any staker (i.e. any validator or delegator) on any subnet to duly reward their activity.

The P-chain tracks a network agreed timestamp called `ChainTime` that allows nodes to reach agreement about when a staker starts and stops its staking time. These start/stop times are basic inputs to determine whether the staker should be rewarded based on what percentage of `ChainTime` it was perceived as active from other validators.

Note that this `ChainTime` has nothing to do with the `Snowman++` timestamp. `Snowman++` timestamps are local times used to reduce network congestion and have no role in rewarding of any staker.

## Pre Blueberry fork context

Before Blueberry fork activation, `ChainTime` was incremented by an `AdvanceTimeTx` transaction, being included into a `ProposalBlock` block type. Validators voted on `ChainTime` advance by accepting either the `CommitBlock` or the `AbortBlock` following the `ProposalBlock`. `ChainTime` was moved ahead only if the `CommitBlock` was accepted.

`AdvanceTimeTx` transactions are zero-fee transactions subject to three main validations:

1. *Strict Monotonicity*: proposed time must be *strictly* greater than current `ChainTime`.
2. *Synchronicity*: proposed time must not be greater than node’s current time plus a synchronicity bound (currently set to 10 seconds).
3. *No Skipping*: proposed time must be less than or equal to the next staking event, that is start/end of any staker.

Note that *Synchronicity* makes sure that `ChainTime` approximate “real” time flow. If we dropped synchronicity requirement, a staker could declare any staking time and immediately push `ChainTime` to the end, so as to pocket a reward without having actually carried out any activity in the “real” time.

## Post Blueberry fork context

Following the Blueberry fork activation, `AdvanceTimeTxs` cannot be included anymore in any block. Instead, each P-chain block type explicitly serializes a timestamp so that `ChainTime` is set to the block timestamp once the block is accepted.

Validation rules for block timestamps varies slightly depending on block types:

* `CommitBlock`s and `AbortBlock`s timestamp must be equal to the timestamp of the `ProposalBlock` they depend upon.
* `StandardBlock`s and `ProposalBlock`s share `AdvanceTimeTx`s validation rules with the exception of the *strict monotonicity*:
  1. *Monotonicity*: block timestamp must be *greater than or equal to* the current `ChainTime` (which is also its parent's timestamp if the parent was accepted).
  2. *Synchronicity*: block timestamp must not be greater than node’s current time plus a synchronicity bound (currently set to 10 seconds).
  3. *No Skipping*: proposed time must be less than or equal to the next staking event (a staker starting or stopping).

## Serialization changes

However upon Blueberry activation some transactions and blocks type will be forbidden, while some new block types will be allowed. Specifically:

* The following block types will be forbidden [^1]:
  * `blocks.ApricotAbortBlock`
  * `blocks.ApricotCommitBlock`
  * `blocks.ApricotProposalBlock`
  * `blocks.ApricotStandardtBlock`
* The following tx types will be forbidden:
  * `AdvanceTimeTx`
* The following block types will be allowed:
  * `blocks.BlueberryAbortBlock`
  * `blocks.BlueberryCommitBlock`
  * `blocks.BlueberryProposalBlock`
  * `blocks.BlueberryStandardtBlock`

Note that unlike `blocks.Apricot*` blocks, `blocks.Blueberry*` blocks will serialize block timestamp.

Note Bluberry fork won't change any transactions format, so transactions byte representation is fully backward compatible. Also Blueberry won't change any codec version

[^1]: note that avalanchego codebase includes `blocks.ApricotAtomicBlock`, which has been forbidden on Apricot Phase 5 fork. This type is kept just to allow boostrapping from genesis and it is forbidden on Blueberry as well as subsequent Apricot forks.