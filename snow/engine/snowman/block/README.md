# State Sync: Engine role and workflow

State sync promises to dramatically cut down a VM bootstrapping time by allowing to directly download and verify a VM state, rather than rebuilding it by re-processing all blocks history.

Avalanche's approach to state sync leaves most of state sync specifics to each VM, to allow maximal flexibility. However Avalanche engine plays a well defined role in selecting and validating `state summaries`, which are the state sync *seeds* used by a VM to kickstart its syncing process. Also Avalanche engine supports completion of state syncing by safely downloading some data needed to seamlessly integrate state syncing with normal engine and VM operations.

In this brief document, we clarify Avalanche engine role in state sync, its promises and requirements imposed to any VM implementing state sync.

## State syncable VM interface walkthrough

`StateSyncableVM` interface collects all the features a VM needs to offer in order to support state sync. In the following we will clarify the meaning of each `StateSyncableVM` methods, along with its role in the whole flow.

Avalanche engine can start its operation by state syncing or bootstrapping a VM. State syncing is the preferred choice, but it is selected only if the following conditions applies:

- `StateSyncEnabled` is return true, i.e. the VM explicitly indicated that it is available for state syncing;
- in case VM implements Snowman++ congestion control, its block height index is *not* rebuilding. The index could be complete or empty (e.g. for freshly created VMs) but it must not currently be under reconstruction.

If conditions above apply, Avalanche engine initiates state sync for the target VM; otherwise it falls back to bootstrapping the VM.

At a high level, Avalanche engine takes care of the following three state syncing phases:

- Frontier retrieval: Avalanche engine retrieves from the network the most recent state summaries from a random subset of network nodes.
- Frontier validation: Avalanche engine validates these summaries by requesting all connected node whether they support these state summaries.
- State sync completion: Following state sync completion on VM side, Avalanche engine downloads data to ensure normal operations can be smoothly resumed.  

These phases are devised to stop a malicious actor from poisoning a VM with a crafted state summary or DoSing it with an unavailable one. Similar to Avalanche's bootstrapping process, security is achieved by feeding state summaries to the VM only if a sufficiently high fraction of network stake has validated them.

In the following we detail these phases.

### Frontier retrieval

At first Avalanche engine checks whether enough stake is connected; if not, state syncing is stalled till enough nodes are connected.

Before polling the network for state summaries frontier, Avalanche Engine retrieves the (possibly) locally available state summary from VM, via `StateSyncGetOngoingSummary` method; if available, this state summary is added to the frontier to be validated.

A state summary may be locally available if the VM was previously shut down while state syncing. By revalidating this local summary, Avalanche engine helps the VM understand whether its local state summary is still widely supported by the network, so that VM can resume state syncing from it, or whether VM should drop it in favour of a fresher, more available one.

State summary frontier is collected as follows:

1. Avalanche engine samples a random subset of validators and sends each of them a `GetStateSummaryFrontier` message to retrieve the latest state summaries.
2. Each target validator pulls its latest state sync from VM via `StateSyncGetLastSummary` method and respond with a `StateSummaryFrontier` message.
3. `StateSummaryFrontier` responses are parsed via `StateSyncParseSummary` method and added to the state summary frontier to be then validated.

Avalanche engine does not pose major constraints on the state summary structure as its parsing is left to the VM to implement. However Avalanche engine does require each state summary to be uniquely described by two identifiers, a `Height` and a `ID`. The reason for this double identification will be clearer as we describe the state summary validation phase.

`Height` is a `uint64` type and represents the block height state summaries refers to. `Height` offers the succinct way to address a state summary.

`ID` is a `ids.ID` type and in our intention is the verifiable way to address a state summary. In our C-chain implementation, a state summary `ID` it's the hash of state summary bytes.

### Frontier validation

Once frontier has been retrieved, a network wide voting round is initiated to validate it. Avalanche engine addresses each connected validator with a `GetAcceptedStateSummary` message, listing state summary frontier `Height`s. `Height`s provide a uniquely yet succinct way to identify state summaries and they help reducing `GetAcceptedStateSummary` size.

Target validators reached by `GetAcceptedStateSummary` message try pulling from VM all the listed state summaries via `StateSyncGetSummary(summaryHeight uint64)` method. Validators prepare a `AcceptedStateSummary` message response by appending those state summaries `ID`s in the same order as  `GetAcceptedStateSummary`; unknown or unsupported state summaries are simply skipped. We chose to respond with state summary `ID`s to hinder potential DoSser and ease up votes verification.

`AcceptedStateSummary` messages returned to state syncing node are validated by comparing responded state summaries `ID`s with the `ID`s calculated from state summary frontier previously retrieved. Valid responses are stored along with validator stake.

Once all validators respond or timeout, Avalanche engine will select from the frontier all state summaries with a sufficient stake backing them.

If at least a valid state summary is available, it is passed down to the VM via `StateSync([]Summary)` method and operations on VM side are started. Note that Avalanche engine will hand on `StateSync([]Summary)` response, hence VM should perform actual state syncing asynchronously if it foresees a long processing.

If no state summary is backed by sufficient stake, the whole process of collecting a state frontier and validating it is restarted again, up to a configurable number of times.

### State sync completion

Once the possible long running processing on VM side is done, a final operation is needed to ensure that both Avalanche engine and the VM can start their operations. Specifically while the whole VM state corresponding to the selected state summary has been rebuilt, the block originally generating that state may not be available. Since this block availability is necessary for the smooth working of Avalanche engine, state sync is completed by downloading it.

Specifically, once VM signal that state sync is done on its side, Avalanche engine calls `StateSyncGetResult()` to the VM. `StateSyncGetResult()` returns:

1- an error of VM state sync processing has failed. This is considered a fatal error.
2- the blockID and height of the block corresponding to state summary, which needs to be downloaded. Note that while height is not strictly needed, it helps coordinating Snowman++ operations.

If `StateSyncGetResult()` returns no errors, Avalanche engine downloads the block from the network via a `Get` message. Once it receives it, it passes the full block down to the VM by calling `StateSyncSetLastSummaryBlock`.

After `StateSyncSetLastSummaryBlock`, state sync is complete and Avalanche engine moves ahead to bootstrapping.
