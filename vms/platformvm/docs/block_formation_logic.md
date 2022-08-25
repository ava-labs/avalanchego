# Blueberry fork: minor changes to block formation logic

The activation of Blueberry fork slightly changes the way the P-chain selects transactions to be included in next block and deals with block timestamps. In this brief document we detail the process and the changes.

## Apricot block formation logic

Transactions to be included into an Apricot block can originate from the mempool or can be created just in time to duly update the staker set. Block formation logic in the Apricot fork can be broken up into two high-level logical steps:

* First, we try selecting any candidate decision or proposal transactions which could be included in a block *without advancing the current chain time*;
* If no such transactions are found, we evaluate candidate transactions which *may require advancing chain time*. If a chain time advancement is required to include these transactions in a block, a proposal block with an advance time transaction is built first; selected transactions may be included in a subsequent block.

In more details, blocks which do not change chain time are build as follows:

1. If mempool contains any decision transactions, a Standard block is issued with all of the transactions the default block size can accommodate. Note that Apricot standard blocks do not change the current chain time.
2. If the current chain time matches the end of any staker's staking time, a reward transaction is issued into a Proposal Block to initiate network voting on whether the specified staker should be rewarded. Note that there could be multiple stakers ending their staking period at the same chain time, hence a Proposal block must be issued for all of them before chain time is moved ahead. Any attempt to move chain time ahead before rewarding all stakers would fail block verification.

While steps above could be executed in any order, we picked decisions transactions first to maximize throughput.

Once all possibilities of create a block not advancing chain time are exhausted, we attempt to build a block which *may* advance chain time as follows:

1. If the local clock's time is greater than or equal to the earliest staker set change event timestamp, an advance time transaction is issued into a Proposal Block to move current chain time to the earliest staker set change timestamp. Upon this Proposal block's acceptance, chain time will be move ahead and all scheduled changes (e.g. promoting a staker from pending to current) will be carried out.
2. If the mempool contains any proposal transactions, the mempool proposal transaction with the earliest start time is selected and included into a Proposal Block[^1]. A mempool proposal transaction as is won't change the current chain time[^2]. However there is an edge case to consider: on low activity chains (e.g. Fuji P-chain) chain time may fall significantly behind the local clock. If a proposal transaction is finally issued, its start time is likely to be quite far in the future relative to the current chain time. This would cause the proposal transaction to be considered invalid and rejected, since a staker added by a proposal transaction's start time must be at most 366 hours (two weeks) after current chain time. To avoid this edge case on low-activity chains, an advance time transaction is issued first to move chain time to the local clock's time. As soon as chain time is advanced, the mempool proposal transaction will be issued and accepted.

Note that the order in which these steps are executed matters. A block updating chain time would be deemed invalid if it would advance time beyond the staker set's next change event, skipping the associated changes. The order above ensures this never happens because if checks first if chain time should be moved to the time of the next staker set change. It can also be verified by inspection that the timestamp selected for the advance time transactions always respect the synchrony bound.

Block formation terminates as soon as any of the steps executed manage to select transactions to be included into a block. An error is raised otherwise to signal that there are not transactions to be issued. Finally a timer is kicked off to schedule the next block formation attempt.

## Blueberry block formation logic

The activation of the Blueberry fork only makes minor changes to the way the P-chain selects transactions to be included in next block, such as block timestamp calculation. In this section we detail the process and the changes.

We carry out operations in the following order:

* We try to move chain time ahead to the current local time or the earliest staker set change event. Unlike Apricot, here we issue either a Standard block or a Proposal block to advance the time.
* We try to fill a Standard block with mempool decision transactions.
* We check if any staker needs to be rewarded, issuing as many Proposal blocks as needed, as above.
* We try to build a Proposal block with one mempool proposal transaction, if any.

[^1]: Proposal transactions whose start time is too close to local time are dropped first and won't be included in any block.
[^2]: Advance time transactions are proposal transactions and they do change chain time. But advance time transactions are generated just in time and never stored in the mempool. Here mempool proposal transactions refer to AddValidator, AddDelegator and AddSubnetValidator transactions. Reward validator transactions are proposal transactions which do not change chain time but which never in mempool (they are generated just in time).
