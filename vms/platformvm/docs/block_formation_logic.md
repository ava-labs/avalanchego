# Blueberry fork: minor changes to block formation logic

The activation of Blueberry fork changes only slightly the way P-chain selects transactions to be included in next block, if not in minor details like block timestamp calculation. In this brief document we detail the process and the changes.

## Apricot block formation logic

Transactions to be included into an Apricot block can originate from the mempool or can be created just in time to duly update the stakers set. Block formation logic in the Apricot fork can be broken up into two high-level logical steps:

* First, we try selecting any candidate standard or proposal transactions which could be included in a block *without advancing the current chain time*;
* If no such transactions are found, we evaluate candidate transactions which *may require advancing chain time*. If a chain time advancement is required to include these transactions in a block, a proposal block with an advance time transaction is built first; selected transactions may be included in a subsequent block.

In more details, blocks which do not change chain time are build as follows:

1. If mempool contains any decision transactions, a Standard block is issued with all of the transactions the default block size can accommodate. Note that Apricot standard blocks do not change in any way current chain time.
2. If current chain time matches the end of any staker's staking time, a reward transaction is issued into a Proposal Block to initiate network voting on whether the specified staker should be rewarded. Note that there could be multiple stakers ending their staking period at the same chain time, hence a Proposal block must be issued for any of them before chain time is moved ahead. Any attempt to move chain time ahead before rewarding all stakers would fail block verification.

While steps above could be execute in any order, we picked decisions transactions first to maximize throughput.

Once all possibilities of create a block not advancing chain time are exhausted, we attempt to build a block which *may* advance chain time as follows:

1. If the local clock's time is greater than or equal to the earliest stakers' set change event timestamp, an advance time transaction is issued into a Proposal Block to move current chain time to the earliest stakers' set change timestamp. Upon this Proposal block's acceptance, chain time will be move ahead and all scheduled changes (e.g. promoting a staker from pending to current) will be carried out.
2. If the mempool contains any proposal transactions, the mempool proposal transaction with the earliest start time is selected and included into a Proposal Block[^1]. A mempool proposal transaction as is won't cause any change in the current chain time[^2]. However there is an edge case to consider: on low activity chains (e.g. Fuji P-chain) chain time may fall significantly behind the local clock. If a proposal transaction is finally issued, its start time it likely to be quite far in the future with respect to the current chain time. This would cause the proposal transaction to be considered invalid and rejected, since proposal transactions start time must be at most 366 hours (or two weeks) after current chain time. So instead to readily issuing the mempool proposal transaction, an advance time transaction is issued first, moving chain time to local clock's time. As soon as chain time is advanced the mempool proposal transaction will be issued and accepted.

Note that the order in which these steps are executed matters. A block updating chain time would be deemed invalid if it would advance time beyond next stakers' set change event, skipping the associated changes. The order above ensures this never happens because if checks first if chain time should be moved to next stakers' set change event. It can also be verified by inspection that timestamp selected for the advance time transactions always respect the synchrony bound.

Block formation terminates as soon as any of the steps executed manage to select transactions to be included into a block. An error is raised otherwise to signal that there are not transactions to be issued. Finally a timer is kicked off to schedule the next block formation attempt.

## Blueberry block formation logic

The activation of the Blueberry fork only makes minor changes to the way the P-chain selects transactions to be included in next block, such as block timestamp calculation. In this brief document we detail the process and the changes.

We carry out operations in the following order:

* We try to fill a Standard blocks with mempool standard transactions
* We check if any staker needs to be rewarded, issuing as many Proposal blocks as needed, as above
* We try and move chain time ahead to earliest stakers' set change event. Unlike Apricot, here we issue a Standard block with no transactions, whose timestamp is the proposed chain time. Standard block does not require any voting, and will be either accepted or rejected, hence this solution is marginally faster.
* We try and build a Proposal block with one mempool proposal transaction, if any. No changes to chain time are proposed here.

[^1]: Proposal transactions whose start time is too close to local time are dropped first and won't be included in any block. TODO: I am not sure why is this, but it's coherent with P-chain API dropping any AddValidator/Delegator/SubnetValidator request whose start time is too close.
[^2]: Of course advance time transactions are proposal ones and they do change chain time. But advance time transactions are generated just in time and never stored in a mempool. Here I refer to mempool proposal transactions which are AddValidator, AddDelegator and AddSubnetValidator. Reward delegator transaction is a proposal transaction which does not change chain time but which is never in mempool (it's generated just in time).
