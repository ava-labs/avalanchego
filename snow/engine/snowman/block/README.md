# On the structure of StateSummaries and Keys

This brief document outlines the requirements and structure of StateSummaries and StateKeys for Snowman and Snowman++ VM supporting state syncing.

State syncing allows safe rebuilding the VM state at a given height without the need to download and re-execute the whole block history from that height down to genesis.

## Supported operations and requirements

StateSummaries contains enough information to allow a VM to download, verify and rebuild its state. StateKeys must uniquely identify a StateSummary.
The whole StateSummaries and StateKeys system **must** support the following operations:

1. Retrieval of last accepted StateSummary. This enables disseminating around StateSummaries frontiers upon request.
2. Retrieval of StateKey from a StateSummary. This enables requesting votes by passing around a StateKey, rather than the full StateSummary.
3. Verify StateKey validity and availability. This enables verifying and voting on a StateSummary by passing around StateKey only.

Moreover we impose the following requirements on StateSummaries and StateKeys:

0. State syncing requires availability of a block height index.  
1. StateKey retrieval from its StateSummary must be allowed on any node, even a freshly create one with no previous state or index.
2. A StateSummary is uniquely associated with a block. We require the StateSummary to contain the BlockID, so that the full block may be downloaded at will.
3. A StateKeys must be as small as possible and smaller than StateSummary.
4. The StateSummaries/StateKeys system should work seamlessly on Snowman and Snowman++ VMs.
5. We want StateKeys to contain its StateSummary hash. This protects freshly created nodes from poisoned state summaries as, upon voting round, honest nodes can verify that the summaries hash would not match and down-vote the summary.

## StateSummaries and StateKeys structure

The requirements above bring us to the following structure for StateSummaries and StateKeys:

|            | StateKey                                | StateSummary                    |
|:----------:|:---------------------------------------:|:-------------------------------:|
| CoreVM     | height + SummaryHash                    | BlkID + height + SummaryContent |
| ProposerVM | height + ProSummaryHash                 | ProBlkID + CoreVM_StateSummary  |

It can easily verify by inspection that the structure above allows support the required operations. Below we content ourselves with some observations:

1. Both CoreVM and ProposerVM StateKeys can directly be derived from their StateSummary via hashing and concatenation operations, without accessing any state or index.
2. StateKey carries summary height rather than summary block ID to make it shorter. This comes at the cost of including both blkID and height into CoreVM StateSummary, which is optimal since StateKey are send around the network whenever they can substitute StateSummaries.
3. Note that StateKeys are smaller then StateSummaries as long as SummaryHashes are smaller than SummaryContent.

## Detailing StateSyncableVM interface

State sync is implemented internally via the `StateSyncableVM` interface. `StateSyncableVM` supports the following operations:

1. `StateSyncGetLastSummary() -> Summary` to retrieve StateSummary frontier
2. `StateSyncGetKey(Summary) -> Key` to extract StateKey from StateSummary
3. `StateSyncGetSummary(Key.height) -> Summary` helper to retrieve StateSummary associated with a given StateKey. Note that height is enough of an index
4. `IsKeyValid(Key, Summary) -> bool`, to be used along `StateSyncGetSummary` to verify key availability and hash matching and vote on it.
