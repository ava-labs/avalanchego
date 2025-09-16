// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Firewood: Compaction-Less Database Optimized for Efficiently Storing Recent Merkleized Blockchain State
//!
//! Firewood is an embedded key-value store, optimized to store recent Merkleized blockchain
//! state with minimal overhead. Firewood is implemented from the ground up to directly
//! store trie nodes on-disk. Unlike most of state management approaches in the field,
//! it is not built on top of a generic KV store such as LevelDB/RocksDB. Firewood, like a
//! B+-tree based database, directly uses the trie structure as the index on-disk. Thus,
//! there is no additional "emulation" of the logical trie to flatten out the data structure
//! to feed into the underlying database that is unaware of the data being stored. The convenient
//! byproduct of this approach is that iteration is still fast (for serving state sync queries)
//! but compaction is not required to maintain the index. Firewood was first conceived to provide
//! a very fast storage layer for the EVM but could be used on any blockchain that
//! requires authenticated state.
//!
//! Firewood only attempts to support queries from recent revisions and will actively clean up
//! unused older revisions when state diffs are committed. The number of revisions is
//! configured when the database is opened.
//!
//! Firewood provides OS-level crash recovery, but not machine-level crash recovery. That is,
//! if the firewood process crashes, the OS will flush the cache leave the system in a valid state.
//! No protection is (currently) offered to handle machine failures.
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
//! robust architecture that fulfills the need for such blockchain storage. However, firewood
//! can also efficiently handle the first regime.
//!
//! ## Storage Model
//!
//! Firewood is built by layers of abstractions that totally decouple the layout/representation
//! of the data on disk from the actual logical data structure it retains:
//!
//! - The storage module has a [`firewood_storage::NodeStore`] which has a generic parameter identifying
//!   the state of the nodestore, and a storage type.
//!
//!   There are three states for a nodestore:
//!    - [`firewood_storage::Committed`] for revisions that are committed
//!    - [`firewood_storage::ImmutableProposal`] for revisions that are proposals against committed versions
//!    - [`firewood_storage::MutableProposal`] for revisions where nodes are still being added.
//!
//!  For more information on these node states, see their associated documentation.
//!
//!   The storage type is either a file or memory. Memory storage is used for creating temporary
//!   merkle tries for proofs as well as testing. Nodes are identified by their offset within the
//!   storage medium (a memory array or a disk file).
//!
//! ## Node caching
//!
//! Once committed, nodes never change until they expire for re-use. This means that a node cache
//! can reduce the amount of serialization and deserialization of nodes. The size of the cache, in
//! nodes, is specified when the database is opened.
//!
//! In short, a Read-Modify-Write (RMW) style normal operation flow is as follows in Firewood:
//!
//! - Create a [`firewood_storage::MutableProposal`] [`firewood_storage::NodeStore`] from the most recent [`firewood_storage::Committed`] one.
//! - Traverse the trie, starting at the root. Make a new root node by duplicating the existing
//!   root from the committed one and save that in memory. As you continue traversing, make copies
//!   of each node accessed if they are not already in memory.
//!
//! - Make changes to the trie, in memory. Each node you've accessed is currently in memory and is
//!   owned by the [`firewood_storage::MutableProposal`]. Adding a node simply means adding a reference to it.
//!
//! - If you delete a node, mark it as deleted in the proposal and remove the child reference to it.
//!
//! - After making all mutations, convert the [`firewood_storage::MutableProposal`] to an [`firewood_storage::ImmutableProposal`]. This
//!   involves walking the in-memory trie and converting them to a [`firewood_storage::SharedNode`].
//!
//! - Since the root is guaranteed to be new, the new root will reference all of the new revision.
//!
//! A commit involves allocating space for the nmodes and writing them as well as the freelist to disk.
//!
// Instead of using a `compile_error!`, cause clippy to hard fail if the target is not 64-bit. This
// is a workaround for the fact that the `clippy::cast_possible_truncation` lint does not delineate
// between 64-bit and non-64-bit targets with respect to `usize -> u64` casts and vice versa which
// leads to a lot of unecessary `TryInto` casts. This also allows 32-bit builds to compile as long
// as `clippy` is not part of the build process albeit with the risk of truncation errors.
#![cfg_attr(
    not(target_pointer_width = "64"),
    forbid(
        clippy::cast_possible_truncation,
        reason = "non-64 bit target likely to cause issues during u64 to usize conversions"
    )
)]

#[cfg(all(feature = "ethhash", feature = "branch_factor_256"))]
compile_error!(
    "feature \"ethhash\" and feature \"branch_factor_256\" cannot be enabled at the same time"
);

/// Database module for Firewood.
pub mod db;

/// Iterator module, for both node and key-value streams
pub mod iter;

/// Database manager module
pub mod manager;

/// Merkle module, containing merkle operations
pub mod merkle;

/// Proof module
pub mod proof;

/// Change, Range, and Key proofs for the Merkle Trie
// TODO: push `proof` and `range_proof` modules into this module
pub mod proofs;

// Re-export the proc macro from firewood-macros
pub use firewood_macros::metrics;

/// Range proof module
pub mod range_proof;

/// Version 2 API
pub mod v2;

/// Expose the storage logger
pub use firewood_storage::logger;

#[cfg(all(test, feature = "logger"))]
#[ctor::ctor]
/// `ctor` will ensure this function is invoked before any tests are run so we
/// can initialize the logger consistently across all tests without having to
/// manually call it in each test.
///
/// This is technically black magic as it runs before `main` is invoked, which
/// violates some of the Rust guarantees. But, it is convenient to ensure the
/// logger is initialized for all tests.
///
/// In the event of unexpected behavior in testing, disable this first before
/// looking elsewhere.
fn init_logger() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("trace"))
        .is_test(true)
        .try_init()
        .ok();
}
