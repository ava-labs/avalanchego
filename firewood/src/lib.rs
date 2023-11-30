// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Firewood: Compaction-Less Database Optimized for Efficiently Storing Recent Merkleized Blockchain State
//!
//! Firewood is an embedded key-value store, optimized to store recent Merkleized blockchain
//! state with minimal overhead. Firewood is implemented from the ground up to directly
//! store trie nodes on-disk. Unlike most of state management approaches in the field,
//! it is not built on top of a generic KV store such as LevelDB/RocksDB. Firewood, like a
//! B+-tree based database, directly uses the trie structure as the index on-disk. Thus,
//! there is no additional “emulation” of the logical trie to flatten out the data structure
//! to feed into the underlying database that is unaware of the data being stored. The convenient
//! byproduct of this approach is that iteration is still fast (for serving state sync queries)
//! but compaction is not required to maintain the index. Firewood was first conceived to provide
//! a very fast storage layer for the EVM but could be used on any blockchain that
//! requires authenticated state.
//!
//! Firewood only attempts to store the latest state on-disk and will actively clean up
//! unused state when state diffs are committed. To avoid reference counting trie nodes,
//! Firewood does not copy-on-write (COW) the state trie and instead keeps
//! one latest version of the trie index on disk and applies in-place updates to it.
//! Firewood keeps some configurable number of previous states in memory to power
//! state sync (which may occur at a few roots behind the current state).
//!
//! Firewood provides OS-level crash recovery via a write-ahead log (WAL). The WAL
//! guarantees atomicity and durability in the database, but also offers
//! “reversibility”: some portion of the old WAL can be optionally kept around to
//! allow a fast in-memory rollback to recover some past versions of the entire
//! store back in memory. While running the store, new changes will also contribute
//! to the configured window of changes (at batch granularity) to access any past
//! versions with no additional cost at all.
//!
//! # Design Philosophy & Overview
//!
//! With some on-going academic research efforts and increasing demand of faster local storage
//! solutions for the chain state, we realized there are mainly two different regimes of designs.
//!
//! - "Archival" Storage: this style of design emphasizes on the ability to hold all historical
//!   data and retrieve a revision of any wold state at a reasonable performance. To economically
//!   store all historical certified data, usually copy-on-write merkle tries are used to just
//!   capture the changes made by a committed block. The entire storage consists of a forest of these
//!   "delta" tries. The total size of the storage will keep growing over the chain length and an ideal,
//!   well-executed plan for this is to make sure the performance degradation is reasonable or
//!   well-contained with respect to the ever-increasing size of the index. This design is useful
//!   for nodes which serve as the backend for some indexing service (e.g., chain explorer) or as a
//!   query portal to some user agent (e.g., wallet apps). Blockchains with delayed finality may also
//!   need this because the "canonical" branch of the chain could switch (but not necessarily a
//!   practical concern nowadays) to a different fork at times.
//!
//! - "Validation" Storage: this regime optimizes for the storage footprint and the performance of
//!   operations upon the latest/recent states. With the assumption that the chain's total state
//!   size is relatively stable over ever-coming blocks, one can just make the latest state
//!   persisted and available to the blockchain system as that's what matters for most of the time.
//!   While one can still keep some volatile state versions in memory for mutation and VM
//!   execution, the final commit to some state works on a singleton so the indexed merkle tries
//!   may be typically updated in place. It is also possible (e.g., Firewood) to allow some
//!   infrequent access to historical versions with higher cost, and/or allow fast access to
//!   versions of the store within certain limited recency. This style of storage is useful for
//!   the blockchain systems where only (or mostly) the latest state is required and data footprint
//!   should remain constant or grow slowly if possible for sustainability. Validators who
//!   directly participate in the consensus and vote for the blocks, for example, can largely
//!   benefit from such a design.
//!
//! In Firewood, we take a closer look at the second regime and have come up with a simple but
//! robust architecture that fulfills the need for such blockchain storage.
//!
//! ## Storage Model
//!
//! Firewood is built by three layers of abstractions that totally decouple the
//! layout/representation of the data on disk from the actual logical data structure it retains:
//!
//! - Linear, memory-like space: the `shale` crate offers a `CachedStore` abstraction for a
//!   (64-bit) byte-addressable space that abstracts away the intricate method that actually persists
//!   the in-memory data on the secondary storage medium (e.g., hard drive). The implementor of `CachedStore`
//!   provides the functions to give the user of `CachedStore` an illusion that the user is operating upon a
//!   byte-addressable memory space. It is just a "magical" array of bytes one can view and change
//!   that is mirrored to the disk. In reality, the linear space will be chunked into files under a
//!   directory, but the user does not have to even know about this.
//!
//! - Persistent item storage stash: `ShaleStore` trait from `shale` defines a pool of typed
//!   objects that are persisted on disk but also made accessible in memory transparently. It is
//!   built on top of `CachedStore` by defining how "items" of the given type are laid out, allocated
//!   and recycled throughout their life cycles (there is a disk-friendly, malloc-style kind of
//!   basic implementation in `shale` crate, but one can always define their own `ShaleStore`).
//!
//! - Data structure: in Firewood, one trie is maintained by invoking `ShaleStore` (see `src/merkle.rs`).
//!   The data structure code is totally unaware of how its objects (i.e., nodes) are organized or
//!   persisted on disk. It is as if they're just in memory, which makes it much easier to write
//!   and maintain the code.
//!
//! Given the abstraction, one can easily realize the fact that the actual data that affect the
//! state of the data structure (trie) is what the linear space (`CachedStore`) keeps track of, that is,
//! a flat but conceptually large byte vector. In other words, given a valid byte vector as the
//! content of the linear space, the higher level data structure can be *uniquely* determined, there
//! is nothing more (except for some auxiliary data that are kept for performance reasons, such as caching)
//! or less than that, like a way to interpret the bytes. This nice property allows us to completely
//! separate the logical data from its physical representation, greatly simplifies the storage
//! management, and allows reusing the code. It is still a very versatile abstraction, as in theory
//! any persistent data could be stored this way -- sometimes you need to swap in a different
//! `ShaleStore` or `CachedStore` implementation, but without having to touch the code for the persisted
//! data structure.
//!
//! ## Page-based Shadowing and Revisions
//!
//! Following the idea that the tries are just a view of a linear byte space, all writes made to the
//! tries inside Firewood will eventually be consolidated into some interval writes to the linear
//! space. The writes may overlap and some frequent writes are even done to the same spot in the
//! space. To reduce the overhead and be friendly to the disk, we partition the entire 64-bit
//! virtual space into pages (yeah it appears to be more and more like an OS) and keep track of the
//! dirty pages in some `CachedStore` instantiation (see `storage::StoreRevMut`). When a
//! [`db::Proposal`] commits, both the recorded interval writes and the aggregated in-memory
//! dirty pages induced by this write batch are taken out from the linear space. Although they are
//! mathematically equivalent, interval writes are more compact than pages (which are 4K in size,
//! become dirty even if a single byte is touched upon) . So interval writes are fed into the WAL
//! subsystem (supported by growthring). After the WAL record is written (one record per write batch),
//! the dirty pages are then pushed to the on-disk linear space to mirror the change by some
//! asynchronous, out-of-order file writes. See the `BufferCmd::WriteBatch` part of `DiskBuffer::process`
//! for the detailed logic.
//!
//! In short, a Read-Modify-Write (RMW) style normal operation flow is as follows in Firewood:
//!
//! - Traverse the trie, and that induces the access to some nodes. Suppose the nodes are not already in
//!   memory, then:
//!
//! - Bring the necessary pages that contain the accessed nodes into the memory and cache them
//!   (`storage::CachedSpace`).
//!
//! - Make changes to the trie, and that induces the writes to some nodes. The nodes are either
//!   already cached in memory (its pages are cached, or its handle `ObjRef<Node>` is still in
//!   `shale::ObjCache`) or need to be brought into the memory (if that's the case, go back to the
//!   second step for it).
//!
//! - Writes to nodes are converted into interval writes to the stagging `StoreRevMut` space that
//!   overlays atop `CachedSpace`, so all dirty pages during the current write batch will be
//!   exactly captured in `StoreRevMut` (see `StoreRevMut::delta`).
//!
//! - Finally:
//!
//!   - Abort: when the write batch is dropped without invoking `db::Proposal::commit`, all in-memory
//!     changes will be discarded, the dirty pages from `StoreRevMut` will be dropped and the merkle
//!     will "revert" back to its original state without actually having to rollback anything.
//!
//!   - Commit: otherwise, the write batch is committed, the interval writes (`storage::Ash`) will be bundled
//!     into a single WAL record (`storage::AshRecord`) and sent to WAL subsystem, before dirty pages
//!     are scheduled to be written to the space files. Also the dirty pages are applied to the
//!     underlying `CachedSpace`. `StoreRevMut` becomes empty again for further write batches.
//!
//! Parts of the following diagram show this normal flow, the "staging" space (implemented by
//! `StoreRevMut`) concept is a bit similar to the staging area in Git, which enables the handling
//! of (resuming from) write errors, clean abortion of an on-going write batch so the entire store
//! state remains intact, and also reduces unnecessary premature disk writes. Essentially, we
//! copy-on-write pages in the space that are touched upon, without directly mutating the
//! underlying "master" space. The staging space is just a collection of these "shadowing" pages
//! and a reference to the its base (master) so any reads could partially hit those dirty pages
//! and/or fall through to the base, whereas all writes are captured. Finally, when things go well,
//! we "push down" these changes to the base and clear up the staging space.
//!
//! <p align="center">
//!     <img src="https://ava-labs.github.io/firewood/assets/architecture.svg" width="100%">
//! </p>
//!
//! Thanks to the shadow pages, we can both revive some historical versions of the store and
//! maintain a rolling window of past revisions on-the-fly. The right hand side of the diagram
//! shows previously logged write batch records could be kept even though they are no longer needed
//! for the purpose of crash recovery. The interval writes from a record can be aggregated into
//! pages (see `storage::StoreDelta::new`) and used to reconstruct a "ghost" image of past
//! revision of the linear space (just like how staging space works, except that the ghost space is
//! essentially read-only once constructed). The shadow pages there will function as some
//! "rewinding" changes to patch the necessary locations in the linear space, while the rest of the
//! linear space is very likely untouched by that historical write batch.
//!
//! Then, with the three-layer abstraction we previously talked about, a historical trie could be
//! derived. In fact, because there is no mandatory traversal or scanning in the process, the
//! only cost to revive a historical state from the log is to just playback the records and create
//! those shadow pages. There is very little additional cost because the ghost space is summoned on an
//! on-demand manner while one accesses the historical trie.
//!
//! In the other direction, when new write batches are committed, the system moves forward, we can
//! therefore maintain a rolling window of past revisions in memory with *zero* cost. The
//! mid-bottom of the diagram shows when a write batch is committed, the persisted (master) space goes one
//! step forward, the staging space is cleared, and an extra ghost space (colored in purple) can be
//! created to hold the version of the store before the commit. The backward delta is applied to
//! counteract the change that has been made to the persisted store, which is also a set of shadow pages.
//! No change is required for other historical ghost space instances. Finally, we can phase out
//! some very old ghost space to keep the size of the rolling window invariant.
//!
pub mod db;
pub(crate) mod file;
pub mod merkle;
pub mod merkle_util;
pub mod storage;

pub mod config;
pub mod nibbles;
// TODO: shale should not be pub, but there are integration test dependencies :(
pub mod shale;

pub mod v2;
