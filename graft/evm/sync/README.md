# State sync

## Overview

Normally, a node joins the network through bootstrapping: First it fetches all blocks from genesis to the chain's last accepted block from peers, then it applies the state transition specified in each block to reach the state necessary to join consensus.

State sync is an alternative in which a node downloads the state of the chain from its peers at a specific _syncable_ block height. Then, the node processes the rest of the chain's blocks (from syncable block to tip) via normal bootstrapping.
Blocks at heights divisible by `defaultSyncableInterval` (= 16,384 or 2**14) are considered syncable.
_Note: `defaultSyncableInterval` must be divisible by `CommitInterval` (= 4096). This is so the state corresponding to syncable blocks is available on nodes with pruning enabled._

State sync is faster than bootstrapping and uses less bandwidth and computation:

- Nodes joining the network do not process all the state transitions.
- The amount of data sent over the network is proportionate to the amount of state not the chain's length

_Note: nodes joining the network through state sync will not have historical state prior to the syncable block._

## What is the chain state?

The node needs the following data from its peers to continue processing blocks from a syncable block:

- Accounts trie & storage tries for all accounts (at the state root corresponding to the syncable block),
- State of the cross-chain shared memory (coreth only: this data is fetched from peers as a merkelized trie containing add/remove operations from Import and Export Txs, known as the _atomic trie_),
- Contract code referenced in the account trie,
- 256 parents of the syncable block (required for the BLOCKHASH opcode)

## Code structure

This package (`evm/sync`) contains the shared state sync implementation used by both coreth (C-Chain) and subnet-evm. VM-specific concerns (atomic trie sync, VM plugin wiring) remain in their respective modules.

- `types`: Core interfaces shared across sync packages: `Syncer`, `Finalizer`, `LeafClient`, `Extender`.
- `handlers`: Nodes that have joined the network are expected to respond to valid requests for the chain state:
  - `LeafsRequestHandler`: handles requests for trie data (leafs)
  - `CodeRequestHandler`: handles requests for contract code
  - `BlockRequestHandler`: handles requests for blocks
  - _Note: There are response size and time limits in place so peers joining the network do not overload peers providing data. Additionally, the engine tracks the CPU usage of each peer for such messages and throttles inbound requests accordingly._
- `client`: Validates responses from peers and provides support for syncing tries.
- `evmstate`: Syncs EVM state (accounts trie, storage tries, and contract code) using `client`. Supports both hash-based tries and Firewood.
- `leaf`: Generic leaf syncer (`CallbackSyncer`) that iterates trie ranges and invokes callbacks on each batch of received leafs.
- `block`: Syncs parent blocks of the syncable block.
- `code`: Syncs contract code referenced in the account trie, with concurrent workers and a persistent queue.
- `engine`: `SyncerRegistry` orchestrates execution of multiple `Syncer` implementations (EVM state, blocks, code, and VM-specific syncers like the atomic trie).
- `synctest`: Shared test utilities for trie and block generation.
- `syncutils`: Iterator utilities for trie traversal.

VM-specific code that uses this package:

- `coreth/plugin/evm/`: Implements `StateSyncableVM`. `StateSyncClient` orchestrates the top-level sync steps. `coreth/plugin/evm/atomic/sync/` syncs the atomic trie via the `Extender` interface.
- `subnet-evm/plugin/evm/`: Implements `StateSyncableVM` for subnet-evm VMs (no atomic trie).
- `message`: Contains structs that are serialized and sent over the network during state sync. Supports both coreth and subnet-evm wire formats via `LeafsRequestType`.

## Sync summaries & engine involvement

When a new node wants to join the network via state sync, it will need a few pieces of information as a starting point so it can make valid requests to its peers:

- Number (height) and hash of the latest available syncable block,
- Root of the account trie,
- Root of the atomic trie (coreth only)

The above information is called a _state summary_, and each syncable block corresponds to one such summary (see `message.BlockSyncSummary`). The engine and VM interact as follows to find a syncable state summary:

1. The engine calls `StateSyncEnabled`. The VM returns `true` to initiate state sync, or `false` to start bootstrapping. This is controlled by the `state-sync-enabled` flag.
1. The engine calls `GetOngoingSyncStateSummary`. If the VM has a previously interrupted sync to resume it returns that summary. Otherwise, it returns `ErrNotFound`. By default, the VM will resume an interrupted sync.
1. The engine samples peers for their latest available summaries, then verifies the correctness and availability of each sampled summary with validators. The messaging flow is documented in the [block engine README](https://github.com/ava-labs/avalanchego/blob/master/snow/engine/snowman/block/README.md).
1. The engine calls `Accept` on the chosen summary. The VM may return `false` to skip syncing to this summary (e.g., coreth skips state sync for less than `defaultStateSyncMinBlocks = 300_000` blocks). If the VM decides to perform the sync, it must return `true` without blocking and fetch the state from its peers asynchronously.
1. The VM sends `common.StateSyncDone` on the `toEngine` channel on completion.
1. The engine calls `VM.SetState(Bootstrapping)`. Then, blocks after the syncable block are processed one by one.

## Syncing state

The following steps are executed by the VM to sync its state from peers:

1. Wipe snapshot data
1. Sync 256 parents of the syncable block (see `BlockRequest`),
1. Sync VM-specific state (e.g., the atomic trie in coreth, via the `Extender` interface),
1. Sync the EVM state: account trie, code, and storage tries,
1. Update in-memory and on-disk pointers.

Steps 3 and 4 involve syncing tries. To sync trie data, the VM will send a series of `LeafRequests` to its peers. Each request specifies:

- Type of trie (`NodeType`):
  - `StateTrieNode` (account trie and storage tries share the same database)
  - VM-specific node types (e.g., `atomic.TrieNode` for the atomic trie in coreth)
- `Root` of the trie to sync,
- `Start` and `End` specify a range of keys.

Peers responding to these requests send back trie leafs (key/value pairs) beginning at `Start` and up to `End` (or a maximum number of leafs). The response must also contain a merkle proof for the range of leafs it contains. Nodes serving state sync data are responsible for constructing these proofs (see `handlers/leafs_request.go`).

`client.GetLeafs` handles sending a single request and validating the response. This method will retry the request from a different peer up to `maxRetryAttempts` (= 32) times if the peer's response is:

- malformed,
- does not contain a valid merkle proof,
- or is not received in time.

If there are more leafs in a trie than can be returned in a single response, the client will make successive requests to continue fetching data (with `Start` set to the last key received) until the trie is complete. `leaf.CallbackSyncer` manages this process and invokes a callback on each batch of received leafs.

### EVM state: Account trie, code, and storage tries

`evmstate.stateSync` uses `leaf.CallbackSyncer` to sync the account trie. When the leaf callback is invoked, each leaf represents an account:

- If the account has contract code, it is queued for download via `code.Queue`
- If the account has a storage root, it is added to the list of trie roots returned from the callback. The syncer has `defaultNumThreads` (= 4) goroutines to fetch these tries concurrently.

If the account trie encounters a new storage trie task and there are already 4 in-progress trie tasks (1 for the account trie and 3 for in-progress storage trie tasks), then the account trie worker will block until one of the storage trie tasks finishes and it can create a new task.

When an account leaf is received, it is converted to `SlimRLP` format and written to the snapshot.
To reconstruct the trie, the syncer inserts leafs as they arrive in a `StackTrie`. Since leafs arrive sorted by increasing key order, the `StackTrie` can create intermediary trie nodes as soon as all possible children for a given path are known (by hashing the children). This allows the sync process to recreate the trie locally, without the need to transmit non-leaf nodes over the network.
When the trie is complete, an `OnFinish` callback is called and any remaining nodes are hashed (resulting in the trie root).

When a storage trie leaf is received, it is stored in the account's storage snapshot. A `StackTrie` is used here to reconstruct intermediary trie nodes & root as well.

### Atomic trie (coreth only)

Coreth extends the sync process via the `Extender` interface (`coreth/plugin/evm/atomic/sync/`). The atomic trie contains cross-chain shared memory operations:

- Key: block height + peer blockchain ID
- Value: codec serialized `atomic.Requests` (includes `PutRequests` and `RemoveRequests`)

For each 4096 blocks (`commitHeightInterval`) inserted in the atomic trie, a root is constructed and the trie is persisted.

### Updating in-memory and on-disk pointers

Once the tries have been synced, the VM:

- Verifies the block the engine has received matches the expected block hash and block number in the summary,
- Adds a checkpoint to the `core.ChainIndexer` (to avoid indexing missing blocks),
- Resets in-memory and on-disk pointers on the `core.BlockChain` struct,
- Updates VM's last accepted block,
- Applies VM-specific post-sync operations (e.g., coreth applies atomic operations from the atomic trie to shared memory).

## Resuming a partial sync operation

While state sync is faster than normal bootstrapping, the process may take several hours to complete. In case the node is shut down in the middle of a state sync, progress on syncing the account trie and storage tries is preserved:

- When starting a sync, the VM persists the state summary to disk. This is so if the node is shut down while the sync is ongoing, this summary can be found and returned to the engine from `GetOngoingSyncStateSummary` upon node restart.
- If enough validators indicate this summary is available and valid, the engine will prefer it and the VM will use this summary for the rest of the sync.
- `evmstate.stateSync` maintains a set of in-progress tries (see `evmstate/state_syncer_progress.go`), and will resume syncing these tries.
- For each in-progress trie, leafs are restored by iterating keys from the snapshot (account or storage) to the `StackTrie`, and syncing continues from the next key.
- When the sync is complete, the ongoing state summary is removed from disk.

## Configuration flags

| flag | type | description | default |
|------|------|-------------|---------|
| `state-sync-enabled` | `bool` | set to true to enable state sync | `false` |
| `state-sync-skip-resume` | `bool` | set to true to avoid resuming an ongoing sync | `false` |
| `state-sync-min-blocks` | `uint64` | Minimum number of blocks the chain must be ahead of local state to prefer state sync over bootstrapping | `300,000` |
| `state-sync-server-trie-cache` | `int` | Size of trie cache to serve state sync data in MB. Should be set to multiples of `64`. | `64` |
| `state-sync-ids` | `string` | a comma separated list of `NodeID-` prefixed node IDs to sync data from. If not provided, peers are randomly selected. | |
