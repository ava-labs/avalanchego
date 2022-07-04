# Blueberry fork: changing block formation logic

The activation of Blueberry fork changes slightly (IS IT TRUE???) the way P-chain selects transactions to be included in next block. In this brief document we detail these changes.

## Apricot block formation logic

Transactions to be included into an Apricot blocks can originate from mempool or can be created just in time to duly update the stakers set. Block formation logic upon Apricot fork can be broken up into two high-level logical steps:

* First we try and select standard or proposal transactions which can be included in a block *without advancing current chain time*;
* If no such transactions exist, we try and select transactions which *may require advancing chain time*. If chain time change is required a proposal block with an advance time tx is build first; selected transactions may be included in a subsequent block.

In more details, blocks which do not change chain time are build as follows:

* If mempool contains any decision transactions, a Standard block is issued with all of the transactions the default block size can accommodate. Note that Apricot standard blocks do not change in any way current chain time.
* If current chain time matches the end of any staker's staking time, a reward transaction is issued into a Proposal Block to initiate network voting on whether the specified staker should be rewarded. Note that there could be multiple stakers ending their staking period at the same chain time, hence a Proposal block must be issued for any of them before chain time is moved ahead. Any attempt to move chain time ahead before rewarding all stakers would fail block verification.

While steps above could be execute in any order, we picked decisions transactions first to maximize throughput.

Once all possibilities of create a block not advancing chain time are exhausted, we attempt to build a block which may advance chain time as follows:

* If local clock is later than or at the same time as the earliest stakers' set change event, an advance time transactions is issued into a Proposal Block to move current chain time to the earliest stakers' set change time. Upon this Proposal block acceptance, chain time will be move ahead and all scheduled changes (e.g. promoting a staker from pending to current) will be carried out.
* If the mempool contains any proposal transactions, the one with earlier start time is selected and included into a Proposal Block[^1]. A proposal transactions as is won't cause any change in the current chain time. However there is an edge case to consider: on low activity chains (e.g. Fuji P-chain) chain time may be pretty far behind local clock. If a proposal transaction is finally issued, it is better not to readily included into a Proposal Block. In fact a proposal transactions is deemed invalid if its start time it too far in the future with respect to current chain time. To prevent generating an invalid Proposal block, in this case *an advance time transactions is issued first*, moving chain time to local time.

Note that the order in which these steps are executed matters. A block updating chain time would be deemed invalid if it would advance time beyond next stakers' set change event, skipping the associated changes. The order above ensures this never happens because if checks first if chain time should be moved to next stakers' set change event. It can also be verified by inspection that timestamp selected for the advance time transactions always respect the synchrony bound.

Block formation terminates as soon as any of the steps executed manage to select transactions to be included into a block. An error is raised otherwise to signal that there are not transactions to be issued. Finally a timer is kicked off to schedule the next block formation attempt.

[^1]: Proposal transactions whose start time is too close to local time are dropped first and won't be included in any block. TODO: I am not sure why is this, but it's coherent with P-chain API dropping any AddValidator/Delegator/SubnetValidator request whose start time is too close.
