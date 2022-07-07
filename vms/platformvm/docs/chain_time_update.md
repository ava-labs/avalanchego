# Blueberry fork: removing Advance Time transactions

The activation of Blueberry fork changes the way P-chain tracks its `ChainTime`. In this brief document we detail these changes.

## About `ChainTime`

One of P-chain main responsibilities is to record staking periods of any staker, i.e. any validator or delegator, on any subnet, so to duly reward their activity.

P-chain tracks a network agreed timestamp called `ChainTime`, that allows nodes to reach agreement about when a staker starts and stops its staking time. These start/stop times are basic inputs to determine whether the staker should be rewarded, based what percentage of `ChainTime` time it was perceived as active from other validators.

Note that this `ChainTime` has nothing to do with `Snowman++` timestamp as reported in block header. `Snowman++` timestamps are local times used to reduce network congestion and have no role in rewarding of any staker.

## Pre Blueberry fork context

Before Blueberry fork activation, `ChainTime` was incremented by an `AdvanceTimeTx` transaction, being included into a `ProposalBlock` block type. Validators voted on `ChainTime` advance by accepting either the `CommitBlock` or the `AbortBlock` following the `ProposalBlock`. `ChainTime` was moved ahead only if the `CommitBlock` was accepted.

`AdvanceTimeTx` transactions are zero-fee transactions subject to three main validations:

1. *Strict Monotonicity*: proposed time must be *strictly* greater than current `ChainTime`
2. *Synchronicity*: proposed time must not be later than node’s current time plus a synchronicity bound (currently set to 10 seconds).
3. *No Skipping*: proposed time must be smaller or equal to the next staking event, that is start/end of any staker.

Note that *Synchronicity* makes sure that `ChainTime` approximate “real” time flow. If we dropped synchronicity requirement, a staker could declare any staking time and immediately push `ChainTime` to the end, so as to pocket a reward without having actually carried out any activity in the “real” time.

## Post Blueberry fork context

Following Blueberry fork activation, `AdvanceTimeTxs` cannot be included anymore in any block. Instead each P-chain block type explicitly serializes a timestamp, so that `ChainTime` is set to the block timestamp once the block is accepted.

Validation rules for block timestamps varies slightly depending on block types:

* `CommitBlock`s and `AbortBlock`s timestamp must be equal to the timestamp of the `ProposalBlock` they depend upon
* `StandardBlock`s and `ProposalBlock`s share `AdvanceTimeTx`s validation rules with the exception of the *strict monotonicity*:
  1. *Monotonicity*: block timestamp must be *greater or equal* to current `ChainTime` (which is also its parent timestamp if parent was accepted)
  2. *Synchronicity*: block timestamp must not be later than node’s current time plus a synchronicity bound (currently set to 10 seconds).
  3. *No Skipping*: proposed time must be less than or equal to the next staking event, that is start/end of any staker.

## Serialization changes

Other than forbidding `AdvanceTimeTx`s to be included in a block, Blueberry fork activation does not require any transactions format change. This means that transactions byte representation is fully backward compatible.

On the contrary, Blueberry fork activation does change block formatting, which is why an explicit hard fork is necessary to activate these changes. Technically blocks change in two ways:

1. A new codec version is used for blocks, which is explicitly serialized in blocks byte representation.
2. Transactions included in both `ProposalBlock`s and `StandardBlock`s are serialized as byte slices rather than directly as `txs.SignedTx`s. This measure is required to guarantee transactions backward compatibility face the blocks version change.
