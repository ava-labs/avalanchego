# On the structure of StateSummaries and Keys

This brief document outlines the requirements and structure of StateSummaries and StateKeys for Snowman and Snowman++ VM supporting state syncing.

State syncing allows safe rebuilding the VM state at a given height without the need to download and re-execute the whole block history from that height down to genesis.

## Supported operations and requirements

StateSummaries contains enough information to allow a VM to download, verify and rebuild its state. StateKeys must uniquely identify a StateSummary.
The whole StateSummaries and StateKeys system **must** support the following operations:

1. Retrieval of last accepted StateSummary. This enables to disseminate around StateSummaries frontiers upon request.
2. Retrieval of StateKey from a StateSummary. This enables requesting votes by passing around a StateKey, rather than the full StateSummary.
3. Retrieval of StateSummary from its StateKey. This enables to verify availability of a StateSummary by passing around StateKey only.

Moreover we impose the following requirements on StateSummaries and StateKeys:

1. StateKey retrieval from its StateSummary must be allowed on any node, even a freshly create one with no previous state or index.
2. A StateSummary is uniquely associated with a block. We require the StateSummary to contain the BlockID, so that the full block may be downloaded at will.
3. A StateKeys must be as small as possible and smaller than StateSummary.
4. The StateSummaries/StateKeys system should work seamlessly on Snowman and Snowman++ VMs.
5. We want StateKeys to contain its StateSummary hash so to readily verify matching.

## StateSummaries and StateKeys structure

The requirements above bring us to the following structure for StateSummaries and StateKeys:

|            | StateKey                                | StateSummary                   |
|:----------:|:---------------------------------------:|:------------------------------:|
| CoreVM     | BlkID + SummaryID                       | BlkID + SummaryContent         |
| ProposerVM | ProBlkID + ProSummaryID + CoreSummaryID | ProBlkID + CoreVM StateSummary |

It can easily verify by inspection that the structure above allows support the required operations. Below we content ourselves with some observations:

1. Both CoreVM and ProposerVM StateKeys can directly be derived from their StateSummary via hashing and concatenation operations, without accessing any state or index. This
2. Note also that BlkID must be included in CoreVM StateSummary and StateKey because we required seamless operability for Snowman and Snowman++ VMs. Should we drop this requirement, and allow State Syncing only for Snowman++ VMs, CoreVM StateSummary and StateKey could be shortened by dropping BlkID and using a simpler hash.
3. Note that both ProSummaryID and CoreSummaryID must be included into ProposerVM StateKey to allow full matching verification with CoreVM and ProposerVM StateSummaries. Should we drop this requirement, CoreVM StateSummary and StateKey could be shortened by dropping ProSummaryID and CoreSummaryID.
4. Note that StateKeys are smaller then StateSummaries as long as SummaryHashes are smaller than SummaryContent.
