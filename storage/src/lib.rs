// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![warn(missing_debug_implementations, rust_2018_idioms, missing_docs)]
#![deny(unsafe_code)]
#![cfg_attr(
    not(target_pointer_width = "64"),
    forbid(
        clippy::cast_possible_truncation,
        reason = "non-64 bit target likely to cause issues during u64 to usize conversions"
    )
)]

//! # storage implements the storage of a [Node] on top of a `LinearStore`
//!
//! Nodes are stored at a [`LinearAddress`] within a [`ReadableStorage`].
//!
//! The [`NodeStore`] maintains a free list and the [`LinearAddress`] of a root node.
//!
//! A [`NodeStore`] is backed by a [`ReadableStorage`] which is persisted storage.

use std::fmt::{Display, Formatter, LowerHex, Result};
use std::ops::Range;

mod checker;
mod hashednode;
mod hashers;
mod iter;
mod linear;
mod node;
mod nodestore;
mod trie_hash;

use crate::nodestore::AreaIndex;

/// Logger module for handling logging functionality
pub mod logger;

// re-export these so callers don't need to know where they are
pub use checker::CheckOpt;
pub use hashednode::{Hashable, Preimage, ValueDigest, hash_node, hash_preimage};
pub use linear::{FileIoError, ReadableStorage, WritableStorage};
pub use node::path::{NibblesIterator, Path};
pub use node::{
    BranchNode, Child, Children, LeafNode, Node, PathIterItem,
    branch::{HashType, IntoHashType},
};
pub use nodestore::{
    Committed, HashedNodeReader, ImmutableProposal, LinearAddress, MutableProposal, NodeReader,
    NodeStore, Parentable, RootReader, TrieReader,
};

pub use linear::filebacked::FileBacked;
pub use linear::memory::MemStore;
pub use node::persist::MaybePersistedNode;

pub use trie_hash::TrieHash;

/// A shared node, which is just a triophe Arc of a node
pub type SharedNode = triomphe::Arc<Node>;

/// The strategy for caching nodes that are read
/// from the storage layer. Generally, we only want to
/// cache write operations, but for some read-heavy workloads
/// you can enable caching of branch reads or all reads.
#[derive(Clone, Debug)]
pub enum CacheReadStrategy {
    /// Only cache writes (no reads will be cached)
    WritesOnly,

    /// Cache branch reads (reads that are not leaf nodes)
    BranchReads,

    /// Cache all reads (leaves and branches)
    All,
}

impl Display for CacheReadStrategy {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result {
        write!(f, "{self:?}")
    }
}

/// Returns the hash of an empty trie, which is the Keccak256 hash of the RLP encoding of an empty byte array.
///
/// This function is slow, so callers should cache the result
#[cfg(feature = "ethhash")]
#[must_use]
#[expect(
    clippy::missing_panics_doc,
    reason = "Found 1 occurrences after enabling the lint."
)]
pub fn empty_trie_hash() -> TrieHash {
    use sha3::Digest as _;

    sha3::Keccak256::digest(rlp::NULL_RLP)
        .as_slice()
        .try_into()
        .expect("empty trie hash is 32 bytes")
}

/// This enum encapsulates what points to the stored area.
#[derive(Debug, Clone, PartialEq, Eq, Copy)]
pub enum StoredAreaParent {
    /// The stored area is a trie node
    TrieNode(TrieNodeParent),
    /// The stored area is a free list
    FreeList(FreeListParent),
}

/// This enum encapsulates what points to the stored area allocated for a trie node.
#[derive(Debug, Clone, PartialEq, Eq, Copy)]
pub enum TrieNodeParent {
    /// The stored area is the root of the trie, so the header points to it
    Root,
    /// The stored area is not the root of the trie, so a parent trie node points to it
    Parent(LinearAddress, usize),
}

/// This enum encapsulates what points to the stored area allocated for a free list.
#[derive(Debug, Clone, PartialEq, Eq, Copy)]
pub enum FreeListParent {
    /// The stored area is the head of the free list, so the header points to it
    FreeListHead(AreaIndex),
    /// The stored area is not the head of the free list, so a previous free area points to it
    PrevFreeArea(LinearAddress),
}

impl LowerHex for StoredAreaParent {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result {
        match self {
            StoredAreaParent::TrieNode(trie_parent) => LowerHex::fmt(trie_parent, f),
            StoredAreaParent::FreeList(free_list_parent) => LowerHex::fmt(free_list_parent, f),
        }
    }
}

impl LowerHex for TrieNodeParent {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result {
        match self {
            TrieNodeParent::Root => f.write_str("Root"),
            TrieNodeParent::Parent(addr, index) => {
                f.write_str("TrieNode@")?;
                LowerHex::fmt(addr, f)?;
                f.write_fmt(format_args!("[{index}]"))
            }
        }
    }
}

impl LowerHex for FreeListParent {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result {
        match self {
            FreeListParent::FreeListHead(index) => f.write_fmt(format_args!("FreeLists[{index}]")),
            FreeListParent::PrevFreeArea(addr) => {
                f.write_str("FreeArea@")?;
                LowerHex::fmt(addr, f)
            }
        }
    }
}

use derive_where::derive_where;

/// Errors returned by the checker
#[derive(thiserror::Error, Debug)]
#[derive_where(PartialEq, Eq)]
#[non_exhaustive]
pub enum CheckerError {
    /// The file size is not valid
    #[error("Invalid DB size ({db_size}): {description}")]
    InvalidDBSize {
        /// The size of the db
        db_size: u64,
        /// The description of the error
        description: String,
    },

    /// Hash mismatch for a node
    #[error(
        "Hash mismatch for node {path:?} at address {address}: parent stored {parent_stored_hash}, computed {computed_hash}"
    )]
    HashMismatch {
        /// The path of the node
        path: Path,
        /// The address of the node
        address: LinearAddress,
        /// The parent of the node
        parent: TrieNodeParent,
        /// The hash value stored in the parent node
        parent_stored_hash: HashType,
        /// The hash value computed for the node
        computed_hash: HashType,
    },

    /// The address is out of bounds
    #[error(
        "stored area at {start:#x} with size {size} (parent: {parent:#x}) is out of bounds ({bounds:#x?})"
    )]
    AreaOutOfBounds {
        /// Start of the `StoredArea`
        start: LinearAddress,
        /// Size of the `StoredArea`
        size: u64,
        /// Valid range of addresses
        bounds: Range<LinearAddress>,
        /// The parent of the `StoredArea`
        parent: StoredAreaParent,
    },

    /// Stored areas intersect
    #[error(
        "stored area at {start:#x} with size {size} (parent: {parent:#x}) intersects with other stored areas: {intersection:#x?}"
    )]
    AreaIntersects {
        /// Start of the `StoredArea`
        start: LinearAddress,
        /// Size of the `StoredArea`
        size: u64,
        /// The intersection
        intersection: Vec<Range<LinearAddress>>,
        /// The parent of the `StoredArea`
        parent: StoredAreaParent,
    },

    /// Freelist area size does not match
    #[error(
        "Free area {address:#x} of size {size} (parent: {parent:#x}) is found in free list {actual_free_list} but it should be in freelist {expected_free_list}"
    )]
    FreelistAreaSizeMismatch {
        /// Address of the free area
        address: LinearAddress,
        /// Actual size of the free area
        size: u64,
        /// Free list on which the area is stored
        actual_free_list: AreaIndex,
        /// Expected size of the area
        expected_free_list: AreaIndex,
        /// The parent of the free area
        parent: FreeListParent,
    },

    /// The start address of a stored area is not a multiple of 16
    #[error(
        "The start address of a stored area (parent: {parent:#x}) is not a multiple of {}: {address:#x}",
        nodestore::alloc::LinearAddress::MIN_AREA_SIZE
    )]
    AreaMisaligned {
        /// The start address of the stored area
        address: LinearAddress,
        /// The parent of the `StoredArea`
        parent: StoredAreaParent,
    },

    /// Found leaked areas
    #[error("Found leaked areas: {0}")]
    #[derive_where(skip_inner)]
    AreaLeaks(checker::LinearAddressRangeSet),

    /// The root is not persisted
    #[error("The checker can only check persisted nodestores")]
    UnpersistedRoot,

    /// IO error
    #[error("IO error")]
    #[derive_where(skip_inner)]
    IO(#[from] FileIoError),
}

#[cfg(test)]
mod test_utils {
    use rand::rngs::StdRng;
    use rand::{Rng, SeedableRng, rng};

    pub fn seeded_rng() -> StdRng {
        let seed = std::env::var("FIREWOOD_STORAGE_TEST_SEED")
            .ok()
            .map_or_else(
                || rng().random(),
                |s| {
                    str::parse(&s)
                        .expect("couldn't parse FIREWOOD_STORAGE_TEST_SEED; must be a u64")
                },
            );

        eprintln!("Seed {seed}: to rerun with this data, export FIREWOOD_STORAGE_TEST_SEED={seed}");
        StdRng::seed_from_u64(seed)
    }
}
