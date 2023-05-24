# State Sync: Engine role and workflow

State sync promises to dramatically cut down the time it takes for a validator to join a chain by directly downloading and verifying the state of the chain, rather than rebuilding the state by processing all historical blocks.

Avalanche's approach to state sync leaves most of specifics to each VM, to allow maximal flexibility. However, the Avalanche engine plays a well defined role in selecting and validating `state summaries`, which are the state sync *seeds* used by a VM to kickstart its syncing process.

In this document, we outline the engine's role in state sync and the requirements for VMs implementing state sync.

## `StateSyncableVM` Interface

The [`StateSyncableVM`](./state_syncable_vm.go) interface enumerates all the features a VM must implement to support state sync.

The Avalanche engine begins bootstrapping a VM through state-sync if `StateSyncEnabled()` returns true. Otherwise, the engine falls back to processing all historical blocks.

The Avalanche engine performs two state-syncing phases:

- Frontier retrieval: retrieve the most recent state summaries from a random subset of validators
- Frontier validation: the retrieved state summaries are voted on by all connected validators

Security is guaranteed by accepting the state summary if and only if a sufficient fraction of stake has validated them. This prevents malicious actors from poisoning a VM with a corrupt or unavailable state summary.

### Frontier retrieval

The Avalanche engine waits to begin frontier retrieval until enough stake is connected.

The engine first calls `GetOngoingSyncStateSummary()` to retrieve a local state summary from the VM. If available, this state summary is added to the frontier.

A state summary may be locally available if the VM was previously shut down while state syncing. By revalidating this local summary, Avalanche engine helps the VM understand whether its local state summary is still widely supported by the network. If the local state summary is widely supported, the engine will allow the VM to resume state syncing from it. Otherwise the VM will be requested to drop it in favour of a fresher, more available state summary.

State summary frontier is collected as follows:

1. The Avalanche engine samples a random subset of validators and sends each of them a `GetStateSummaryFrontier` message to retrieve the latest state summaries.
2. Each target validator pulls its latest state summary from the VM via the `GetLastStateSummary` method, and responds with a `StateSummaryFrontier` message.
3. `StateSummaryFrontier` responses are parsed via the `ParseStateSummary` method and added to the state summary frontier to be validated.

The Avalanche engine does not pose major constraints on the state summary structure; as its parsing is left to the VM to implement. However, the Avalanche engine does require each state summary to be uniquely described by two identifiers, `Height` and `ID`. The reason for this double identification will be clearer as we describe the state summary validation phase.

`Height` is a `uint64` and represents the block height that the state summaries refers to. `Height` offers the most succinct way to address an accepted state summary.

`ID` is an `ids.ID` type and is the verifiable way to address a state summary, regardless of its status. In most implementations, a state summary `ID` is the hash of the state summary's bytes.

### Frontier validation

Once the frontiers have been retrieved, a network wide voting round is initiated to validate them. Avalanche sends a `GetAcceptedStateSummary` message to each connected validator, containing a list of the state summary frontier's `Height`s. `Height`s provide a unique yet succinct way to identify state summaries, and they help reduce the size of the  `GetAcceptedStateSummary` message.

When a validator receives a `GetAcceptedStateSummary` message, it will respond with `ID`s corresponding to the `Height`s sent in the message. This is done by calling `GetStateSummary(summaryHeight uint64)` on the VM for each `Height`. Unknown or unsupported state summary `Height`s are skipped. Responding with the summary `ID`s allows the client to verify votes easily and ensures the integrity of the state sync starting point.

`AcceptedStateSummary` messages returned to the state syncing node are validated by comparing responded state summaries `ID`s with the `ID`s calculated from state summary frontier previously retrieved. Valid responses are stored along with cumulative validator stake.

Once all validators respond or timeout, Avalanche will select all the state summaries with a sufficient stake verifying them.

If no state summary is backed by sufficient stake, the process of collecting a state frontier and validating it is restarted again, up to a configurable number of times.

Of all the valid state summaries, one is selected and passed to the VM by calling `Summary.Accept()`. The preferred state summary is selected as follows: if the locally available one is still valid and supported by the network it will be accepted to allow the VM to resume the previously interrupted state sync. Otherwise, the most recent state summary (with the highest `Height` value) is picked. Note that Avalanche engine will block upon `Summary.Accept()`'s response, hence the VM should perform actual state syncing asynchronously.

The Avalanche engine declares state syncing complete in the following cases:

1. `Summary.Accept()` returns `(StateSyncSkipped, nil)` signalling that the VM considered the summary valid but skips the whole syncing process. This may happen if the VM estimates that bootstrapping would be faster than state syncing with the provided summary.
2. The VM sends `StateSyncDone` via the `Notify` channel.

Note that any error returned from `Summary.Accept()` is considered fatal and causes the engine to shutdown.

If `Summary.Accept()` returns `(StateSyncStatic, nil)`, Avalanche will wait until state synced is complete before continuing the bootstrapping process. If `Summary.Accept()` returns `(StateSyncDynamic, nil)`, Avalanche will immediately continue the bootstrapping process. If bootstrapping finishes before the state sync has been completed, `Chits` messages will include the `LastAcceptedID` rather than the `PreferredID`.
