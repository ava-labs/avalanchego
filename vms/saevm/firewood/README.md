# Firewood for SAE

## Overview

This package integrates [Firewood](https://github.com/ava-labs/firewood), a Rust-based key-value store optimized for Ethereum state, with the SAE EVM. It provides a `state.Database` backed by Firewood via CGo FFI, replacing the standard Merkle Patricia Trie with Firewood's internal path-based storage.

## Use

`TrieDB` implements `triedb.DBOverride` (acting as `triedb.HashDB` for compatibility) and is passed to `state.NewDatabaseWithConfig` via `TrieDBConfig.BackendConstructor`. The `state.RegisterDatabaseInterceptor` registered in `init()` ensures that any `state.Database` backed by this `TrieDB` uses Firewood's custom trie implementations instead of the standard ones.

```go
cfg := firewood.DefaultConfig(dataDir, log)
db := state.NewDatabaseWithConfig(rawdb.NewMemoryDatabase(), &triedb.Config{
    DBOverride: cfg.BackendConstructor,
})
```

## Rust Memory Management

Firewood's CGo FFI exposes two types of Rust-owned heap objects: `ffi.Revision` and `ffi.Proposal`. Both should be explicitly freed to avoid leaking Rust memory - otherwise we must rely on Go's garbage collector to eventually call `runtime.AddCleanup`.

All proposals tracked by the `TrieDB` are either explicitly freed (via `Drop`) or committed. Proposals are dropped in the following functions:

- `TrieDB.Close`: all pending and committable proposals are dropped before the database is closed.
- `TrieDB.Commit`: proposals are committed in ancestor-first order; if any commit fails, the remaining proposals are dropped rather than leaked.
- `accountTrie.hash`: the previous reader is dropped when a new proposal replaces it.

Revisions are freed when possible (e.g. in `accountTrie.hash` when unused), but in general, they will be freed via `runtime.AddCleanup`, because the `state.Trie` implementation does not have a `Close` method or anything similar. Any proposal or revision remaining within a trie can only be garbage collected. The account trie is only ever created by the `state.StateDB`, so one must allow these objects to be garbage collected prior to calling `TrieDB.Close()`.

## Operation Model

### Linear proposal chain only

Unlike `graft/evm/firewood`, which supports an arbitrary DAG of proposals (multiple concurrent children per parent), this implementation **requires a strictly linear chain**: each proposal has at most one child, and that child must eventually be committed. This maps onto the SAE EVM's single-chain, sequential block execution model.

The linear invariant means:

- Any proposal created via `accountTrie.Hash()` will be tracked in `accountTrie.Commit()`, and **must** then be moved to the committable map in `TrieDB.Update`.
- The parent of each new proposal must be either the current committed tip of the Firewood database or the most recent uncommitted proposal in the chain.
- Branching — creating two proposals from the same parent — is not supported.

## SELFDESTRUCT Handling

Reads never reflect pending writes (see [Why reads aren't safe](#why-reads-arent-safe)); this has particular implications for `SELFDESTRUCT`. The `SELFDESTRUCT` opcode must delete an account's entire storage trie, but `state.StateDB` does not iterate over and individually delete each storage slot — it relies on the trie to handle bulk deletion. Firewood handles this via prefix deletion: `DeleteAccount` issues an `ffi.PrefixDelete(accountKey)`, which atomically removes the account leaf and all storage slots that share the same key prefix. This means:

- No explicit storage-trie iteration is required for self-destructed accounts.
- After a `SELFDESTRUCT`, storage reads return nothing correctly, because the prefix-deleted state is reflected in the proposal produced by the next `Hash()`.
- If storage for a self-destructed account is read *before* the next `Hash()`, the old reader would still show the account's storage — but `state.StateDB` never reads storage for a self-destructed account before committing, so this edge case does not arise.

## Design Decisions

### Why `Commit` commits multiple proposals at once

`TrieDB.Commit(root)` does not commit only the proposal for `root`. It walks back the entire chain of uncommitted ancestor proposals and commits them all in ancestor-first order before committing `root`.
This is required because Firewood enforces strict ancestor ordering: a child proposal cannot be committed before its parent.
The `DeferredCommitInterval` configuration deliberately delays disk commits to batch I/O, allowing multiple blocks' worth of proposals to accumulate before persisting to disk.
This differs from `HashDB`, which synchronously writes all nodes on `Commit()` for the provided state root.
This allows Firewood a performance improvement, but it is recommended that users frequently call `Commit`, as it will not result in faster state growth.

The `TrieDB` will never return an empty proposal, so there will never be ambiguity about which proposal represents `root`, unlike in the historical Firewood TrieDB. There will never be two proposals with the same root, since at minimum, a nonce is incremented.

As a consequence, `Commit` for a single root may queue many proposals to be committed, and thus could be written to disk at any time. Any error from `Commit` should be treated as fatal. `Commit` is not expected to be an expensive operation, since all disk commits are handled asynchronously.

### Why `Reader` is unimplemented (custom trie reader)

`TrieDB.Reader(root)` always returns an error. The `triedb.Backend` interface expects `Reader` to supply node bytes to a generic `trie.Trie`, which then reassembles the Merkle Trie from encoded node data. Firewood does not store state in that format, and the resulting `trie.Trie` created would be nonsensical.

Instead, this package provides custom `accountTrie` and `storageTrie` implementations that read directly from `ffi.Revision` and `ffi.Proposal` objects via flat key-value `Get` calls. This avoids another implementation of a reader-like object, and allows re-using the Firewood API.

### Why reads aren't safe

Unexpectedly perhaps, calling `UpdateAccount` immediately followed by `GetAccount` on the account trie will return the old version of the account. This is because we cannot safely determine where a storage node should be deleted or not quickly. Imagine the following case:

```go
accountTrie := db.OpenTrie(root)
storageTrie := db.OpenStorageTrie(root, addr, accountRoot, accountTrie)
storageTrie.UpdateStorage(addr, key, putVal)
accountTrie.DeleteAccount(addr)

val := storageTrie.GetStorage(addr, key)
```

What is the expected value of `val`? Should it be `nil`, since the account was deleted? Should it be `putVal`, since the storage trie in go-ethereum is a separate struct? Or should it return an error, because an invariant was violated?
The important detail here is that the `state.StateDB` will never read after writing. Maintaining a map of pending writes would cover most cases, but storage reads would also need to check whether the parent account has been prefix-deleted — which is difficult to track without the full trie context. Additionally, if an account is recreated after deletion, a naive pending-writes map could not determine whether storage should be considered deleted when read later. Even worse, if an account is deleted by the `state.StateDB`, the corresponding storages will never be explicitly deleted from the storage trie, so we have to rely on prefix deletions.

For all these caveats, the only logical conclusion for compatibility is that the implementation of `state.Trie` must only guarantee that the `state.StateDB` functions correctly. Since it will not read after writing until `state.StateDB.IntermediateRoot` is called, we can rely on reading from an actual proposal/revision.

### Relation to the snapshot

The go-ethereum `snapshot.Tree` allows fast reads for HashDB by avoiding loading all intermediate merkle nodes into the trie by storing a flattened K/V layer. The `state.StateDB` allows configuring parallel prefetchers to load the data in asynchronously for faster hashing later. In Firewood, there is little benefit to this optimization due to the path-based storage scheme, and avoids the need to have a concurrent-safe trie, an iterator, and divorcing the storage trie from the account trie. Because these all complicate the implementation with no performance benefit, the snapshot is NOT supported with Firewood.
