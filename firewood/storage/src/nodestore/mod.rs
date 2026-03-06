// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # `NodeStore` Module
//!
//! The main module for nodestore functionality, containing core types, traits, and operations
//! for managing merkle trie data in Firewood.
//!
//! ## Module Structure
//!
//! The nodestore module is organized into several specialized submodules:
//!
//! - [`alloc`] - Memory allocation and area management for nodes in the linear store
//! - [`hash`] - Node hashing functionality, including specialized ethereum hash processing
//! - [`header`] - `NodeStore` header structure and validation logic
//! - [`persist`] - Persistence operations for writing nodes and metadata to storage
//!
//! ## Types
//!
//! This module defines the primary types for nodestore operations:
//!
//! - [`NodeStore<T, S>`] - The main nodestore container parameterized by state type and storage
//!
//! `T` is one of the following state types:
//! - [`Committed`] - For a committed revision with no in-memory changes
//! - [`MutableProposal`] - For a proposal being actively modified with in-memory nodes
//! - [`ImmutableProposal`] - For a proposal that has been hashed and assigned addresses
//!
//! The nodestore follows a lifecycle pattern:
//! ```text
//! Committed -> MutableProposal -> ImmutableProposal -> Committed
//! ```
//!
//! ## Traits
//!
//! - **`NodeReader`** - Interface for reading nodes by address
//! - **`RootReader`** - Interface for accessing the root node
//! - **`HashedNodeReader`** - Interface for immutable merkle trie access
//! - **`Parentable`** - Trait for nodestores that can have children

pub(crate) mod alloc;
pub(crate) mod hash;
pub(crate) mod hash_algo;
pub(crate) mod header;
pub(crate) mod persist;
pub(crate) mod primitives;

use crate::IntoHashType;
use crate::linear::OffsetReader;
use crate::logger::{debug, trace};
use crate::node::branch::ReadSerializable as _;
use firewood_metrics::firewood_increment;
use smallvec::SmallVec;
use std::fmt::Debug;
use std::io::{Error, ErrorKind};

// Re-export types from alloc module
pub use alloc::NodeAllocator;
pub use hash_algo::{NodeHashAlgorithm, NodeHashAlgorithmTryFromIntError};
pub use primitives::{AreaIndex, LinearAddress};
// Re-export types from header module
pub use header::NodeStoreHeader;

/// The [`NodeStore`] handles the serialization of nodes and
/// free space management of nodes in the page store. It lays out the format
/// of the [`PageStore`]. More specifically, it places a [`FileIdentifyingMagic`]
/// and a [`FreeSpaceHeader`] at the beginning
///
/// Nodestores represent a revision of the trie. There are three types of nodestores:
/// - Committed: A committed revision of the trie. It has no in-memory changes.
/// - `MutableProposal`: A proposal that is still being modified. It has some nodes in memory.
/// - `ImmutableProposal`: A proposal that has been hashed and assigned addresses. It has no in-memory changes.
///
/// The general lifecycle of nodestores is as follows:
/// ```mermaid
/// flowchart TD
/// subgraph subgraph["Committed Revisions"]
/// L("Latest Nodestore&lt;Committed, S&gt;") --- |...|O("Oldest NodeStore&lt;Committed, S&gt;")
/// end
/// O --> E("Expire")
/// L --> |start propose|M("NodeStore&lt;ProposedMutable, S&gt;")
/// M --> |finish propose + hash|I("NodeStore&lt;ProposedImmutable, S&gt;")
/// I --> |commit|N("New commit NodeStore&lt;Committed, S&gt;")
/// style E color:#FFFFFF, fill:#AA00FF, stroke:#AA00FF
/// ```
use std::mem::take;
use std::ops::Deref;
use std::sync::Arc;

use crate::hashednode::hash_node;
use crate::node::Node;
use crate::node::persist::MaybePersistedNode;
use crate::{
    CacheReadStrategy, Child, FileIoError, HashType, Path, ReadableStorage, SharedNode, TrieHash,
};

use super::linear::WritableStorage;

/// Initial size for the bump allocator used in node serialization batches.
/// Set to the maximum area size to minimize allocations for large nodes.
const INITIAL_BUMP_SIZE: usize = AreaIndex::MAX_AREA_SIZE as usize;

impl<S: ReadableStorage> NodeStore<Committed, S> {
    /// Creates a new `NodeStore` using the provided header and storage.
    ///
    /// The header should be read using [`NodeStoreHeader::read_from_storage`] before calling this.
    /// This separation allows callers to manage the header lifecycle independently.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the root node cannot be read from storage.
    pub fn open(header: &NodeStoreHeader, storage: Arc<S>) -> Result<Self, FileIoError> {
        let mut nodestore = Self {
            kind: Committed {
                deleted: Box::default(),
                root: None,
            },
            storage,
        };

        if let Some(root_address) = header.root_address() {
            let root_hash = if let Some(hash) = header.root_hash() {
                hash.into_hash_type()
            } else {
                debug!("No root hash in header; computing from disk");
                nodestore
                    .read_node_from_disk(root_address, "open")
                    .map(|n| hash_node(&n, &Path(SmallVec::default())))?
            };

            nodestore.kind.root = Some(Child::AddressWithHash(root_address, root_hash));
        }

        Ok(nodestore)
    }

    /// Create a new, empty, Committed [`NodeStore`].
    #[must_use]
    pub fn new_empty_committed(storage: Arc<S>) -> Self {
        Self {
            storage,
            kind: Committed {
                deleted: Box::default(),
                root: None,
            },
        }
    }

    /// Create a new Committed [`NodeStore`] with a specified root node.
    ///
    /// This constructor is used when you have an existing root node at a known
    /// address and hash, typically when reconstructing a [`NodeStore`] from
    /// a committed state.
    ///
    /// ## Panics
    ///
    /// Panics in debug builds if the hash of the node at `root_address` does
    /// not equal `root_hash`.
    #[must_use]
    pub fn with_root(root_hash: HashType, root_address: LinearAddress, storage: Arc<S>) -> Self {
        let nodestore = NodeStore {
            kind: Committed {
                deleted: Box::default(),
                root: Some(Child::AddressWithHash(root_address, root_hash)),
            },
            storage,
        };

        debug_assert_eq!(
            nodestore
                .root_hash()
                .expect("Nodestore should have root hash"),
            hash_node(
                &nodestore
                    .read_node(root_address)
                    .expect("Root node read should succeed"),
                &Path(SmallVec::default())
            )
        );

        nodestore
    }
}

impl<S: ReadableStorage> NodeStore<Committed, S> {
    /// Get the underlying storage for a `NodeStore`.
    #[cfg(any(test, feature = "test_utils"))]
    #[must_use]
    pub fn get_storage(&self) -> Arc<S> {
        self.storage.clone()
    }
}

/// Some nodestore kinds implement Parentable.
///
/// This means that the nodestore can have children.
/// Only [`ImmutableProposal`] and [Committed] implement this trait.
/// [`MutableProposal`] does not implement this trait because it is not a valid parent.
/// TODO: Maybe this can be renamed to `ImmutableNodestore`
pub trait Parentable {
    /// Returns the parent of this nodestore.
    fn as_nodestore_parent(&self) -> NodeStoreParent;
    /// Returns the root hash of this nodestore. This works because all parentable nodestores have a hash
    fn root_hash(&self) -> Option<TrieHash>;
    /// Returns the root node
    fn root(&self) -> Option<MaybePersistedNode>;
    /// Returns the persisted address of the root node, if any.
    fn root_address(&self) -> Option<LinearAddress>;
}

impl Parentable for Arc<ImmutableProposal> {
    fn as_nodestore_parent(&self) -> NodeStoreParent {
        NodeStoreParent::Proposed(Arc::clone(self))
    }
    fn root_hash(&self) -> Option<TrieHash> {
        self.root
            .as_ref()
            .and_then(|root| root.hash().cloned().map(HashType::into_triehash))
    }
    fn root(&self) -> Option<MaybePersistedNode> {
        self.root.as_ref().map(Child::as_maybe_persisted_node)
    }
    fn root_address(&self) -> Option<LinearAddress> {
        self.root.as_ref().and_then(Child::persisted_address)
    }
}

impl<S> NodeStore<Arc<ImmutableProposal>, S> {
    /// When an immutable proposal commits, we need to reparent any proposal that
    /// has the committed proposal as it's parent
    pub fn commit_reparent(&self, other: &NodeStore<Arc<ImmutableProposal>, S>) {
        other.kind.commit_reparent(&self.kind);
    }
}

impl Parentable for Committed {
    fn as_nodestore_parent(&self) -> NodeStoreParent {
        NodeStoreParent::Committed(
            self.root
                .as_ref()
                .and_then(|root| root.hash().cloned().map(HashType::into_triehash)),
        )
    }
    fn root_hash(&self) -> Option<TrieHash> {
        self.root
            .as_ref()
            .and_then(|root| root.hash().cloned().map(HashType::into_triehash))
    }
    fn root(&self) -> Option<MaybePersistedNode> {
        self.root.as_ref().map(Child::as_maybe_persisted_node)
    }
    fn root_address(&self) -> Option<LinearAddress> {
        self.root.as_ref().and_then(Child::persisted_address)
    }
}

impl<S: ReadableStorage> NodeStore<MutableProposal, S> {
    /// Create a new `MutableProposal` [`NodeStore`] from a parent [`NodeStore`]
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the parent root cannot be read.
    pub fn new<F: Parentable>(parent: &NodeStore<F, S>) -> Result<Self, FileIoError> {
        let mut deleted = Vec::default();
        let root = if let Some(ref root) = parent.kind.root() {
            deleted.push(root.clone());
            let root = root.as_shared_node(parent)?.deref().clone();
            Some(root)
        } else {
            None
        };
        let kind = MutableProposal {
            root,
            deleted,
            parent: parent.kind.as_nodestore_parent(),
        };
        Ok(NodeStore {
            kind,
            storage: parent.storage.clone(),
        })
    }

    /// Marks the node at `addr` as deleted in this proposal.
    pub fn delete_node(&mut self, node: MaybePersistedNode) {
        trace!("Pending delete at {node:?}");
        self.kind.deleted.push(node);
    }

    /// Take the nodes that have been marked as deleted in this proposal.
    pub fn take_deleted_nodes(&mut self) -> Vec<MaybePersistedNode> {
        take(&mut self.kind.deleted)
    }

    /// Adds to the nodes deleted in this proposal.
    pub fn delete_nodes(&mut self, nodes: &[MaybePersistedNode]) {
        self.kind.deleted.extend_from_slice(nodes);
    }

    /// Reads a node for update, marking it as deleted in this proposal.
    /// We get an arc from cache (reading it from disk if necessary) then
    /// copy/clone the node and return it.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the node cannot be read.
    pub fn read_for_update(&mut self, node: MaybePersistedNode) -> Result<Node, FileIoError> {
        let arc_wrapped_node = node.as_shared_node(self)?;
        self.delete_node(node);
        Ok((*arc_wrapped_node).clone())
    }

    /// Returns the root of this proposal.
    pub const fn root_mut(&mut self) -> &mut Option<Node> {
        &mut self.kind.root
    }
}

impl<S: ReadableStorage> NodeStore<MutableProposal, S> {
    /// Creates a new [`NodeStore`] from a root node.
    #[must_use]
    pub fn from_root(parent: &NodeStore<MutableProposal, S>, root: Option<Node>) -> Self {
        NodeStore {
            kind: MutableProposal {
                root,
                deleted: Vec::default(),
                parent: parent.kind.parent.clone(),
            },
            storage: parent.storage.clone(),
        }
    }

    /// Consumes the `NodeStore` and returns the root of the trie
    #[must_use]
    pub fn into_root(self) -> Option<Node> {
        self.kind.root
    }
}

impl<S: WritableStorage> NodeStore<MutableProposal, S> {
    /// Creates a new, empty, [`NodeStore`].
    /// This is used during testing and during the creation of an in-memory merkle for proofs.
    #[cfg(any(test, feature = "test_utils"))]
    pub fn new_empty_proposal(storage: Arc<S>) -> Self {
        NodeStore {
            kind: MutableProposal {
                root: None,
                deleted: Vec::default(),
                parent: NodeStoreParent::Committed(None),
            },
            storage,
        }
    }
}

/// Reads from an immutable (i.e. already hashed) merkle trie.
pub trait HashedNodeReader: TrieReader {
    /// Gets the address of the root node of an immutable merkle trie.
    fn root_address(&self) -> Option<LinearAddress>;

    /// Gets the hash of the root node of an immutable merkle trie.
    fn root_hash(&self) -> Option<TrieHash>;
}

/// Reads nodes and the root address from a merkle trie.
pub trait TrieReader: NodeReader + RootReader {}
impl<T> TrieReader for T where T: NodeReader + RootReader {}

/// Reads nodes from a merkle trie.
pub trait NodeReader {
    /// Returns the node at `addr`.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the node cannot be read.
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError>;
}

impl<T> NodeReader for T
where
    T: Deref,
    T::Target: NodeReader,
{
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        self.deref().read_node(addr)
    }
}

impl<T> RootReader for T
where
    T: Deref,
    T::Target: RootReader,
{
    fn root_node(&self) -> Option<SharedNode> {
        self.deref().root_node()
    }
    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode> {
        self.deref().root_as_maybe_persisted_node()
    }
}

/// Reads the root of a merkle trie.
///
/// The root may be None if the trie is empty.
pub trait RootReader {
    /// Returns the root of the trie.
    /// Callers that just need the node at the root should use this function.
    fn root_node(&self) -> Option<SharedNode>;

    /// Returns the root of the trie as a `MaybePersistedNode`.
    /// Callers that might want to modify the root or know how it is stored
    /// should use this function.
    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode>;
}

/// A committed revision of a merkle trie.
#[derive(Debug, Clone)]
pub struct Committed {
    deleted: Box<[MaybePersistedNode]>,
    root: Option<Child>,
}

#[derive(Clone, Debug)]
pub enum NodeStoreParent {
    Proposed(Arc<ImmutableProposal>),
    Committed(Option<TrieHash>),
}

impl PartialEq for NodeStoreParent {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (NodeStoreParent::Proposed(a), NodeStoreParent::Proposed(b)) => Arc::ptr_eq(a, b),
            (NodeStoreParent::Committed(a), NodeStoreParent::Committed(b)) => a == b,
            _ => false,
        }
    }
}

impl Eq for NodeStoreParent {}

#[derive(Debug)]
/// Contains state for a proposed revision of the trie.
pub struct ImmutableProposal {
    /// Nodes that have been deleted in this proposal.
    deleted: Box<[MaybePersistedNode]>,
    /// The parent of this proposal.
    parent: Arc<parking_lot::Mutex<NodeStoreParent>>,
    /// The root of the trie in this proposal.
    root: Option<Child>,
}

impl ImmutableProposal {
    /// Returns true if the parent of this proposal is committed and has the given hash.
    #[must_use]
    fn parent_hash_is(&self, hash: Option<TrieHash>) -> bool {
        match &*self.parent.lock() {
            NodeStoreParent::Committed(root_hash) => *root_hash == hash,
            NodeStoreParent::Proposed(_) => false,
        }
    }

    fn commit_reparent(self: &Arc<Self>, committing: &Arc<Self>) {
        let mut guard = self.parent.lock();
        if let NodeStoreParent::Proposed(ref parent) = *guard
            && Arc::ptr_eq(parent, committing)
        {
            *guard = NodeStoreParent::Committed(committing.root_hash());
            // Track reparenting events
            firewood_increment!(crate::registry::REPARENTED_PROPOSAL_COUNT, 1);
        }
    }
}

/// Contains the state of a revision of a merkle trie.
///
/// The first generic parameter is the type of the revision, which supports reading nodes from parent proposals.
/// The second generic parameter is the type of the storage used, either
/// in-memory or on-disk.
///
/// The lifecycle of a [`NodeStore`] is as follows:
/// 1. Create a new, empty, [Committed] [`NodeStore`] using [`NodeStore::new_empty_committed`].
/// 2. Create a [`NodeStore`] from disk using [`NodeStore::open`].
/// 3. Create a new mutable proposal from either a [Committed] or [`ImmutableProposal`] [`NodeStore`] using [`NodeStore::new`].
/// 4. Convert a mutable proposal to an immutable proposal using [`std::convert::TryInto`], which hashes the nodes and assigns addresses
/// 5. Convert an immutable proposal to a committed revision using [`std::convert::TryInto`], which writes the nodes to disk.

#[derive(Debug)]
pub struct NodeStore<T, S> {
    /// This is one of [Committed], [`ImmutableProposal`], or [`MutableProposal`].
    kind: T,
    /// Persisted storage to read nodes from.
    storage: Arc<S>,
}

/// Contains the state of a proposal that is still being modified.
#[derive(Debug)]
pub struct MutableProposal {
    /// The root of the trie in this proposal.
    root: Option<Node>,
    /// Nodes that have been deleted in this proposal.
    deleted: Vec<MaybePersistedNode>,
    parent: NodeStoreParent,
}

impl<T: Into<NodeStoreParent>, S: ReadableStorage> From<NodeStore<T, S>>
    for NodeStore<MutableProposal, S>
{
    fn from(val: NodeStore<T, S>) -> Self {
        NodeStore {
            kind: MutableProposal {
                root: None,
                deleted: Vec::default(),
                parent: val.kind.into(),
            },
            storage: val.storage,
        }
    }
}

/// Commit a proposal to a new revision of the trie
impl<S: WritableStorage> From<NodeStore<ImmutableProposal, S>> for NodeStore<Committed, S> {
    fn from(val: NodeStore<ImmutableProposal, S>) -> Self {
        NodeStore {
            kind: Committed {
                deleted: val.kind.deleted.clone(),
                root: val.kind.root.clone(),
            },
            storage: val.storage,
        }
    }
}

impl<S: ReadableStorage> NodeStore<Arc<ImmutableProposal>, S> {
    /// Re-export the `parent_hash_is` function of [`ImmutableProposal`].
    #[must_use]
    pub fn parent_hash_is(&self, hash: Option<TrieHash>) -> bool {
        self.kind.parent_hash_is(hash)
    }
}

impl<S: WritableStorage> NodeStore<Arc<ImmutableProposal>, S> {
    /// Return a Committed version of this proposal, which doesn't have any modified nodes.
    /// This function is used during commit.
    #[must_use]
    pub fn as_committed(&self) -> NodeStore<Committed, S> {
        NodeStore {
            kind: Committed {
                deleted: self.kind.deleted.clone(),
                root: self.kind.root.clone(),
            },
            storage: self.storage.clone(),
        }
    }
}

impl<S: ReadableStorage> TryFrom<NodeStore<MutableProposal, S>>
    for NodeStore<Arc<ImmutableProposal>, S>
{
    type Error = FileIoError;

    fn try_from(val: NodeStore<MutableProposal, S>) -> Result<Self, Self::Error> {
        let NodeStore { kind, storage } = val;

        let mut nodestore = NodeStore {
            kind: Arc::new(ImmutableProposal {
                deleted: kind.deleted.into(),
                parent: Arc::new(parking_lot::Mutex::new(kind.parent)),
                root: None,
            }),
            storage,
        };

        let Some(root) = kind.root else {
            // This trie is now empty. Root address will be set to None during persist.
            return Ok(nodestore);
        };

        // Hashes the trie with an empty path and returns the address of the new root.
        #[cfg(feature = "ethhash")]
        let (root, root_hash) = nodestore.hash_helper(root, Path::new())?;
        #[cfg(not(feature = "ethhash"))]
        let (root, root_hash) = NodeStore::<MutableProposal, S>::hash_helper(root, Path::new())?;

        let immutable_proposal =
            Arc::into_inner(nodestore.kind).expect("no other references to the proposal");
        nodestore.kind = Arc::new(ImmutableProposal {
            deleted: immutable_proposal.deleted.clone(),
            parent: immutable_proposal.parent.clone(),
            root: Some(Child::MaybePersisted(root, root_hash)),
        });

        Ok(nodestore)
    }
}

impl<S: ReadableStorage> NodeReader for NodeStore<MutableProposal, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        self.read_node_from_disk(addr, "write")
    }
}

impl<T: Parentable, S: ReadableStorage> NodeReader for NodeStore<T, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        self.read_node_from_disk(addr, "read")
    }
}

impl<S: ReadableStorage> RootReader for NodeStore<MutableProposal, S> {
    fn root_node(&self) -> Option<SharedNode> {
        self.kind.root.as_ref().map(|node| node.clone().into())
    }
    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode> {
        self.kind
            .root
            .as_ref()
            .map(|node| SharedNode::new(node.clone()).into())
    }
}

impl<S: ReadableStorage> RootReader for NodeStore<Committed, S> {
    fn root_node(&self) -> Option<SharedNode> {
        // TODO: If the read_node fails, we just say there is no root; this is incorrect
        self.kind
            .root
            .as_ref()
            .map(Child::as_maybe_persisted_node)
            .and_then(|node| node.as_shared_node(self).ok())
    }
    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode> {
        self.kind.root.as_ref().map(Child::as_maybe_persisted_node)
    }
}

impl<S: ReadableStorage> RootReader for NodeStore<Arc<ImmutableProposal>, S> {
    fn root_node(&self) -> Option<SharedNode> {
        // Use the MaybePersistedNode's as_shared_node method to get the root
        self.kind
            .root
            .as_ref()
            .map(Child::as_maybe_persisted_node)
            .and_then(|node| node.as_shared_node(self).ok())
    }
    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode> {
        self.kind.root.as_ref().map(Child::as_maybe_persisted_node)
    }
}

impl<T, S> HashedNodeReader for NodeStore<T, S>
where
    NodeStore<T, S>: TrieReader,
    T: Parentable,
    S: ReadableStorage,
{
    fn root_address(&self) -> Option<LinearAddress> {
        self.kind.root_address()
    }

    fn root_hash(&self) -> Option<TrieHash> {
        self.kind.root_hash()
    }
}

impl<T: HashedNodeReader> HashedNodeReader for &T {
    fn root_address(&self) -> Option<LinearAddress> {
        (**self).root_address()
    }

    fn root_hash(&self) -> Option<TrieHash> {
        (**self).root_hash()
    }
}

// TODO: return only the index since we can easily get the size from the index
fn area_index_and_size<S: ReadableStorage>(
    storage: &S,
    addr: LinearAddress,
) -> Result<(AreaIndex, u64), FileIoError> {
    let mut area_stream = storage.stream_from(addr.get())?;

    let index: AreaIndex = AreaIndex::new(area_stream.read_byte().map_err(|e| {
        storage.file_io_error(
            Error::new(ErrorKind::InvalidData, e),
            addr.get(),
            Some("area_index_and_size".to_string()),
        )
    })?)
    .ok_or_else(|| {
        storage.file_io_error(
            Error::new(ErrorKind::InvalidData, "invalid area index"),
            addr.get(),
            Some("area_index_and_size".to_string()),
        )
    })?;

    let size = index.size();

    Ok((index, size))
}

impl<T, S: ReadableStorage> NodeStore<T, S> {
    /// Read a [Node] from the provided [`LinearAddress`].
    /// `addr` is the address of a `StoredArea` in the `ReadableStorage`.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the node cannot be read.
    pub fn read_node_from_disk(
        &self,
        addr: LinearAddress,
        mode: &'static str,
    ) -> Result<SharedNode, FileIoError> {
        if let Some(node) = self.storage.read_cached_node(addr, mode) {
            return Ok(node);
        }

        let (node, _) = self.read_node_with_num_bytes_from_disk(addr)?;

        match self.storage.cache_read_strategy() {
            CacheReadStrategy::All => {
                self.storage.cache_node(addr, node.clone());
            }
            CacheReadStrategy::BranchReads => {
                if !node.is_leaf() {
                    self.storage.cache_node(addr, node.clone());
                }
            }
            CacheReadStrategy::WritesOnly => {}
        }

        Ok(node)
    }

    pub(crate) fn read_node_with_num_bytes_from_disk(
        &self,
        addr: LinearAddress,
    ) -> Result<(SharedNode, u64), FileIoError> {
        debug_assert!(addr.is_aligned());

        // saturating because there is no way we can be reading at u64::MAX
        // and this will fail very soon afterwards
        let actual_addr = addr.get().saturating_add(1); // skip the length byte

        let _span = fastrace::local::LocalSpan::enter_with_local_parent("read_and_deserialize");

        let mut area_stream = self.storage.stream_from(actual_addr)?;
        let offset_before = area_stream.offset();
        let node: SharedNode = Node::from_reader(&mut area_stream)
            .map_err(|e| {
                self.storage
                    .file_io_error(e, actual_addr, Some("read_node_from_disk".to_string()))
            })?
            .into();
        let length = area_stream
            .offset()
            .checked_sub(offset_before)
            .ok_or_else(|| {
                self.storage.file_io_error(
                    Error::other("Reader offset went backwards"),
                    actual_addr,
                    Some("read_node_with_num_bytes_from_disk".to_string()),
                )
            })?;
        Ok((node, length.saturating_add(1))) // add 1 for the area size index byte
    }

    /// Returns (index, `area_size`) for the stored area at `addr`.
    /// `index` is the index of `area_size` in the array of valid block sizes.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the area cannot be read.
    pub fn area_index_and_size(
        &self,
        addr: LinearAddress,
    ) -> Result<(AreaIndex, u64), FileIoError> {
        area_index_and_size(self.storage.as_ref(), addr)
    }
}

impl<N> HashedNodeReader for Arc<N>
where
    N: HashedNodeReader,
{
    fn root_address(&self) -> Option<LinearAddress> {
        self.as_ref().root_address()
    }

    fn root_hash(&self) -> Option<TrieHash> {
        self.as_ref().root_hash()
    }
}

impl<S: WritableStorage> NodeStore<Arc<ImmutableProposal>, S> {
    /// Returns the slice of deleted nodes in this proposal (test only).
    #[cfg(any(test, feature = "test_utils"))]
    #[must_use]
    pub fn deleted(&self) -> &[MaybePersistedNode] {
        self.kind.deleted.as_ref()
    }
}
impl<S: WritableStorage> NodeStore<Committed, S> {
    /// Adjust the freelist to reflect the freed nodes in the oldest revision.
    ///
    /// This method takes ownership of `self` and adds its deleted nodes to the free list
    /// managed by the given header.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if a node cannot be deleted.
    pub fn reap_deleted(mut self, header: &mut NodeStoreHeader) -> Result<(), FileIoError> {
        self.storage
            .invalidate_cached_nodes(self.kind.deleted.iter());
        trace!("There are {} nodes to reap", self.kind.deleted.len());
        let mut allocator = NodeAllocator::new(self.storage.as_ref(), header);
        for node in take(&mut self.kind.deleted) {
            allocator.delete_node(node)?;
        }
        Ok(())
    }
}

// Helper functions for the checker
impl<T, S: ReadableStorage> NodeStore<T, S>
where
    NodeStore<T, S>: NodeReader,
{
    // Find the area index and size of the stored area at the given address if the area is valid.
    // TODO: there should be a way to read stored area directly instead of try reading as a free area then as a node
    pub(crate) fn read_leaked_area(
        &self,
        address: LinearAddress,
    ) -> Result<(AreaIndex, u64), FileIoError> {
        if alloc::FreeArea::from_storage(self.storage.as_ref(), address).is_err() {
            self.read_node(address)?;
        }

        let area_index_and_size = self.area_index_and_size(address)?;
        Ok(area_index_and_size)
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used)]
#[expect(clippy::cast_possible_truncation)]
mod tests {

    use crate::BranchNode;
    use crate::Children;
    use crate::FileBacked;
    use crate::LeafNode;
    use crate::NibblesIterator;
    use crate::PathComponent;
    use crate::linear::memory::MemStore;

    use super::*;
    use nonzero_ext::nonzero;
    use primitives::area_size_iter;
    use std::error::Error;

    #[test]
    fn area_sizes_aligned() {
        for (_, area_size) in area_size_iter() {
            assert_eq!(area_size % AreaIndex::MIN_AREA_SIZE, 0);
        }
    }

    #[test]
    fn test_area_size_to_index() {
        // TODO: rustify using: for size in AREA_SIZES
        for (i, area_size) in area_size_iter() {
            // area size is at top of range
            assert_eq!(AreaIndex::from_size(area_size).unwrap(), i);

            if i > AreaIndex::MIN {
                // 1 less than top of range stays in range
                assert_eq!(AreaIndex::from_size(area_size - 1).unwrap(), i);
            }

            if i < AreaIndex::MAX {
                // 1 more than top of range goes to next range
                assert_eq!(
                    AreaIndex::from_size(area_size + 1).unwrap(),
                    AreaIndex::try_from(i.as_usize() + 1).unwrap()
                );
            }
        }

        for i in 0..=AreaIndex::MIN_AREA_SIZE {
            assert_eq!(AreaIndex::from_size(i).unwrap(), AreaIndex::MIN);
        }

        assert!(AreaIndex::from_size(AreaIndex::MAX_AREA_SIZE + 1).is_err());
    }

    #[test]
    fn test_reparent() {
        // create an empty base revision
        let memstore = MemStore::default();
        let base = NodeStore::new_empty_committed(memstore.into());

        // create an empty r1, check that it's parent is the empty committed version
        let r1 = NodeStore::new(&base).unwrap();
        let r1: NodeStore<Arc<ImmutableProposal>, _> = r1.try_into().unwrap();
        {
            let parent = r1.kind.parent.lock();
            assert!(matches!(*parent, NodeStoreParent::Committed(None)));
        }

        // create an empty r2, check that it's parent is the proposed version r1
        let r2: NodeStore<MutableProposal, _> = NodeStore::new(&r1).unwrap();
        let r2: NodeStore<Arc<ImmutableProposal>, _> = r2.try_into().unwrap();
        {
            let parent = r2.kind.parent.lock();
            assert!(matches!(*parent, NodeStoreParent::Proposed(_)));
        }

        // reparent r2
        r1.commit_reparent(&r2);

        // now check r2's parent, should match the hash of r1 (which is still None)
        let parent = r2.kind.parent.lock();
        if let NodeStoreParent::Committed(hash) = &*parent {
            assert_eq!(*hash, r1.root_hash());
            assert_eq!(*hash, None);
        } else {
            panic!("expected committed parent");
        }
    }

    #[test]
    fn test_slow_giant_node() {
        let memstore = Arc::new(MemStore::default());
        let mut header = NodeStoreHeader::new(NodeHashAlgorithm::compile_option());
        let empty_root = NodeStore::new_empty_committed(Arc::clone(&memstore));

        let mut node_store = NodeStore::new(&empty_root).unwrap();

        let huge_value = vec![0u8; AreaIndex::MAX_AREA_SIZE as usize];

        let giant_leaf = Node::Leaf(LeafNode {
            partial_path: Path::from([0, 1, 2]),
            value: huge_value.into_boxed_slice(),
        });

        node_store.root_mut().replace(giant_leaf);

        let node_store = NodeStore::<Arc<ImmutableProposal>, _>::try_from(node_store).unwrap();

        let node_store = node_store.as_committed();

        let err = node_store.persist(&mut header).unwrap_err();
        let err_ctx = err.context();
        assert!(err_ctx == Some("allocate_node"));

        let io_err = err
            .source()
            .unwrap()
            .downcast_ref::<std::io::Error>()
            .unwrap();
        assert_eq!(io_err.kind(), std::io::ErrorKind::OutOfMemory);

        let io_err_source = io_err
            .get_ref()
            .unwrap()
            .downcast_ref::<primitives::AreaSizeError>()
            .unwrap();
        assert_eq!(io_err_source.0, 16_777_225);
    }

    /// Test that persisting a branch node with children succeeds.
    ///
    /// This test verifies the fix for issue #1488 where the panic
    /// "child must be hashed when serializing" occurred during persist.
    ///
    /// The bug was caused by batching nodes before writing but only calling
    /// `allocate_at` after writing. This meant when serializing a parent node,
    /// its children didn't have addresses yet (children are serialized first
    /// in depth-first order, but without `allocate_at()` being called before
    /// batching, the parent couldn't get the child's address during serialization).
    ///
    /// The fix calls `allocate_at` immediately after allocating storage for a node
    /// but before adding it to the batch, ensuring children have addresses when
    /// their parents are serialized.
    #[test]
    fn persist_branch_with_children() -> Result<(), Box<dyn Error>> {
        let tmpdir = tempfile::tempdir()?;
        let dbfile = tmpdir.path().join("nodestore_branch_persist_test.db");

        let storage = Arc::new(FileBacked::new(
            dbfile,
            nonzero!(10usize),
            nonzero!(10usize),
            false,
            true,
            CacheReadStrategy::WritesOnly,
            NodeHashAlgorithm::compile_option(),
        )?);
        let mut header = NodeStoreHeader::new(NodeHashAlgorithm::compile_option());
        let nodestore = NodeStore::open(&header, storage)?;

        let mut proposal = NodeStore::new(&nodestore)?;

        {
            let root = proposal.root_mut();
            assert!(root.is_none());

            let mut children = Children::new();
            children[PathComponent::ALL[0x0]] = Some(Child::Node(Node::Leaf(LeafNode {
                partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"123")),
                value: b"\x00123".to_vec().into_boxed_slice(),
            })));
            children[PathComponent::ALL[0xF]] = Some(Child::Node(Node::Leaf(LeafNode {
                partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"abc")),
                value: b"\x0Fabc".to_vec().into_boxed_slice(),
            })));
            let branch1 = Node::Branch(Box::new(BranchNode {
                partial_path: Path::new(),
                value: None,
                children,
            }));

            let mut children = Children::new();
            children[PathComponent::ALL[0x0]] = Some(Child::Node(Node::Leaf(LeafNode {
                partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"123")),
                value: b"\xF0123".to_vec().into_boxed_slice(),
            })));
            children[PathComponent::ALL[0xF]] = Some(Child::Node(Node::Leaf(LeafNode {
                partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"abc")),
                value: b"\xFFabc".to_vec().into_boxed_slice(),
            })));
            let branch2 = Node::Branch(Box::new(BranchNode {
                partial_path: Path::new(),
                value: None,
                children,
            }));

            let mut children = Children::new();
            children[PathComponent::ALL[0x0]] = Some(Child::Node(branch1));
            children[PathComponent::ALL[0xF]] = Some(Child::Node(branch2));

            *root = Some(Node::Branch(Box::new(BranchNode {
                partial_path: Path::new(),
                value: None,
                children,
            })));
        }

        let proposal = NodeStore::<Arc<ImmutableProposal>, _>::try_from(proposal)?;

        let nodestore = proposal.as_committed();
        nodestore.persist(&mut header)?;

        let mut proposal = NodeStore::new(&nodestore)?;

        {
            let root = proposal.root_mut();
            assert!(root.is_some());
        }

        Ok(())
    }
}
