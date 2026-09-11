# `statesync`

To enable nodes to join the network without executing every block, snowman consensus provides the option to "state sync". Rather than bootstrapping from genesis, the node skips ahead to a relatively recent state that the network agrees on, and bootstraps from there. The consensus engine consumes this functionality through [`block.StateSyncableVM`](../../../snow/engine/snowman/block/state_syncable_vm.go); see [state_syncer.go](../../../snow/engine/snowman/syncer/state_syncer.go) for the engine's side of the protocol. This package implements [`adaptor.SyncableVM`](../adaptor/sync.go), a simplified form of that interface, which `adaptor.ConvertStateSync` converts into a `block.StateSyncableVM`.

## Usage

A VM can simply embed the `SummaryHandler` in their VM and ensure that, based on the state provided to `common.VM.SetState`, all methods are routed to either the summary handler or some more efficient lookup (e.g. the VM's in-memory chain state once the node is fully running). This applies both to the summary-providing methods and to the consensus block getters (`GetBlock`, `LastAccepted`, `GetBlockIDAtHeight`), all of which the summary handler implements.

Whether this node performs a state sync itself is controlled by `Config.Enabled`, reported to the engine via `StateSyncEnabled`. Summaries are served to peers regardless of this setting.

### Expected invariants

The disk layout is critical to the correctness of the summary handler. It is expected for ALL of the following invariants to hold at construction:

- `rawdb.ReadHeadFastBlockHash` will always return the last accepted block, implying that the state it settles was fully executed. If no block has been accepted, it should return the genesis block.
- `rawdb.ReadCanonicalHash` will always provide the genesis block hash at height 0, and at any other height will return the accepted block hash, if there is one.
- For any block hash available from the above methods, `rawdb.ReadBlock` will correctly return the `*types.Block` if the block was explicitly accepted by the VM or is the genesis block.
- `rawdb.ReadHeaderNumber` will return the corresponding height for any header recorded on disk.

Note that these invariants are followed by the SAE VM (see [invariants.md](../docs/invariants.md)).

Additionally, summaries can only be served at heights where the state was committed to disk: heights that are a multiple of the configured trie commit interval (`Config.DBConfig.CommitInterval`). Thus, the each node should agree on the commit interval provided here.

## Serving State Summaries

When a state sync starts, the syncing node queries a sample of validators, asking "What state should I sync to?". Each responds with the `Summary` returned by `SummaryHandler.GetLastStateSummary`. A node should only return a summary if it is willing to provide all corresponding state at that height. Now that the syncing node has a collection of candidate summaries, it sends the full list of candidate heights to the state sync validators, each of which responds with the summaries it can vouch for at those heights (served by `SummaryHandler.GetStateSummary`). Any summary backed by a sufficient (alpha) fraction of stake is viable; the engine selects a preferred one and calls its `Accept` method, which the adaptor routes to `SummaryHandler.AcceptSummary`. If no summary gathers enough stake, the engine skips state sync and proceeds directly to bootstrapping.

`AcceptSummary` may also decline the sync by returning `block.StateSyncSkipped` — for example, if this node has already accepted blocks past genesis. Otherwise, it starts the sync in the background and signals completion by returning `common.StateSyncDone` from `WaitForEvent`, at which point the engine resumes bootstrapping from the synced height.

These functions MUST work even when the node has not yet started up fully. Imagine the case where some sort of widespread outage causes a majority of validators to crash. When starting back up, these validators must be able to serve each other blocks and summaries before reaching normal operation, or the network would fully stall. Because of this, all the summary-providing methods and the consensus block getters are available immediately in the summary handler, allowing any consuming VM to simply direct any calls to the summary handler until the node starts up fully.
