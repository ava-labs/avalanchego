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
pub(crate) mod header;
pub(crate) mod persist;

use crate::firewood_gauge;
use crate::logger::trace;
use crate::node::branch::ReadSerializable as _;
use arc_swap::ArcSwap;
use arc_swap::access::DynAccess;
use smallvec::SmallVec;
use std::fmt::Debug;
use std::io::{Error, ErrorKind};
use std::sync::atomic::AtomicUsize;

// Re-export types from alloc module
pub use alloc::{AreaIndex, LinearAddress, NodeAllocator};

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
use crate::nodestore::alloc::AREA_SIZES;
use crate::{CacheReadStrategy, FileIoError, Path, ReadableStorage, SharedNode, TrieHash};

use super::linear::WritableStorage;

impl<S: ReadableStorage> NodeStore<Committed, S> {
    /// Open an existing [`NodeStore`]
    /// Assumes the header is written in the [`ReadableStorage`].
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the header cannot be read or validated.
    pub fn open(storage: Arc<S>) -> Result<Self, FileIoError> {
        let mut stream = storage.stream_from(0)?;
        let mut header_bytes = vec![0u8; std::mem::size_of::<NodeStoreHeader>()];
        if let Err(e) = stream.read_exact(&mut header_bytes) {
            if e.kind() == std::io::ErrorKind::UnexpectedEof {
                return Self::new_empty_committed(storage.clone());
            }
            return Err(storage.file_io_error(e, 0, Some("header read".to_string())));
        }

        drop(stream);

        let header = *NodeStoreHeader::from_bytes(&header_bytes);
        header
            .validate()
            .map_err(|e| storage.file_io_error(e, 0, Some("header read".to_string())))?;

        let mut nodestore = Self {
            header,
            kind: Committed {
                deleted: Box::default(),
                root_hash: None,
                root: header.root_address().map(Into::into),
                unwritten_nodes: AtomicUsize::new(0),
            },
            storage,
        };

        if let Some(root_address) = nodestore.header.root_address() {
            let node = nodestore.read_node_from_disk(root_address, "open");
            let root_hash = node.map(|n| hash_node(&n, &Path(SmallVec::default())))?;
            nodestore.kind.root_hash = Some(root_hash.into_triehash());
        }

        Ok(nodestore)
    }

    /// Create a new, empty, Committed [`NodeStore`] and clobber
    /// the underlying store with an empty freelist and no root node
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the storage cannot be accessed.
    pub fn new_empty_committed(storage: Arc<S>) -> Result<Self, FileIoError> {
        let header = NodeStoreHeader::new();

        Ok(Self {
            header,
            storage,
            kind: Committed {
                deleted: Box::default(),
                root_hash: None,
                root: None,
                unwritten_nodes: AtomicUsize::new(0),
            },
        })
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
}

impl Parentable for Arc<ImmutableProposal> {
    fn as_nodestore_parent(&self) -> NodeStoreParent {
        NodeStoreParent::Proposed(Arc::clone(self))
    }
    fn root_hash(&self) -> Option<TrieHash> {
        self.root_hash.clone()
    }
    fn root(&self) -> Option<MaybePersistedNode> {
        self.root.clone()
    }
}

impl<S> NodeStore<Arc<ImmutableProposal>, S> {
    /// When an immutable proposal commits, we need to reparent any proposal that
    /// has the committed proposal as it's parent
    pub fn commit_reparent(&self, other: &NodeStore<Arc<ImmutableProposal>, S>) {
        match *other.kind.parent.load() {
            NodeStoreParent::Proposed(ref parent) => {
                if Arc::ptr_eq(&self.kind, parent) {
                    other
                        .kind
                        .parent
                        .store(NodeStoreParent::Committed(self.kind.root_hash()).into());
                }
            }
            NodeStoreParent::Committed(_) => {}
        }
    }
}

impl Parentable for Committed {
    fn as_nodestore_parent(&self) -> NodeStoreParent {
        NodeStoreParent::Committed(self.root_hash.clone())
    }
    fn root_hash(&self) -> Option<TrieHash> {
        self.root_hash.clone()
    }
    fn root(&self) -> Option<MaybePersistedNode> {
        self.root.clone()
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
            header: parent.header,
            kind,
            storage: parent.storage.clone(),
        })
    }

    /// Marks the node at `addr` as deleted in this proposal.
    pub fn delete_node(&mut self, node: MaybePersistedNode) {
        trace!("Pending delete at {node:?}");
        self.kind.deleted.push(node);
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
    pub const fn mut_root(&mut self) -> &mut Option<Node> {
        &mut self.kind.root
    }
}

impl<S: WritableStorage> NodeStore<MutableProposal, S> {
    /// Creates a new, empty, [`NodeStore`] and clobbers the underlying `storage` with an empty header.
    /// This is used during testing and during the creation of an in-memory merkle for proofs
    ///
    /// # Panics
    ///
    /// Panics if the header cannot be written.
    pub fn new_empty_proposal(storage: Arc<S>) -> Self {
        let header = NodeStoreHeader::new();
        let header_bytes = bytemuck::bytes_of(&header);
        storage
            .write(0, header_bytes)
            .expect("failed to write header");
        NodeStore {
            header,
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
#[derive(Debug)]
pub struct Committed {
    deleted: Box<[MaybePersistedNode]>,
    root_hash: Option<TrieHash>,
    root: Option<MaybePersistedNode>,
    /// TODO: No readers of this variable yet - will be used for tracking unwritten nodes in committed revisions
    unwritten_nodes: AtomicUsize,
}

impl Clone for Committed {
    fn clone(&self) -> Self {
        Self {
            deleted: self.deleted.clone(),
            root_hash: self.root_hash.clone(),
            root: self.root.clone(),
            unwritten_nodes: AtomicUsize::new(
                self.unwritten_nodes
                    .load(std::sync::atomic::Ordering::Relaxed),
            ),
        }
    }
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
    parent: Arc<ArcSwap<NodeStoreParent>>,
    /// The hash of the root node for this proposal
    root_hash: Option<TrieHash>,
    /// The root node, either in memory or on disk
    root: Option<MaybePersistedNode>,
    /// The number of unwritten nodes in this proposal
    unwritten_nodes: usize,
}

impl ImmutableProposal {
    /// Returns true if the parent of this proposal is committed and has the given hash.
    #[must_use]
    fn parent_hash_is(&self, hash: Option<TrieHash>) -> bool {
        match <Arc<ArcSwap<NodeStoreParent>> as arc_swap::access::DynAccess<Arc<_>>>::load(
            &self.parent,
        )
        .as_ref()
        {
            NodeStoreParent::Committed(root_hash) => *root_hash == hash,
            NodeStoreParent::Proposed(_) => false,
        }
    }
}

impl Drop for ImmutableProposal {
    fn drop(&mut self) {
        // When an immutable proposal is dropped without being committed,
        // decrement the gauge to reflect that these nodes will never be written
        if self.unwritten_nodes > 0 {
            #[allow(clippy::cast_precision_loss)]
            firewood_gauge!(
                "firewood.nodes.unwritten",
                "current number of unwritten nodes"
            )
            .decrement(self.unwritten_nodes as f64);
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
    // Metadata for this revision.
    header: NodeStoreHeader,
    /// This is one of [Committed], [`ImmutableProposal`], or [`MutableProposal`].
    kind: T,
    /// Persisted storage to read nodes from.
    storage: Arc<S>,
}

impl<T, S> NodeStore<T, S> {
    pub(crate) const fn freelists(&self) -> &alloc::FreeLists {
        self.header.free_lists()
    }
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
            header: val.header,
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
        let NodeStore {
            header,
            kind,
            storage,
        } = val;
        // Use ManuallyDrop to prevent the Drop impl from running since we're committing
        let kind = std::mem::ManuallyDrop::new(kind);

        NodeStore {
            header,
            kind: Committed {
                deleted: kind.deleted.clone(),
                root_hash: kind.root_hash.clone(),
                root: kind.root.clone(),
                unwritten_nodes: AtomicUsize::new(kind.unwritten_nodes),
            },
            storage,
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
    pub fn as_committed(
        &self,
        current_revision: &NodeStore<Committed, S>,
    ) -> NodeStore<Committed, S> {
        NodeStore {
            header: current_revision.header,
            kind: Committed {
                deleted: self.kind.deleted.clone(),
                root_hash: self.kind.root_hash.clone(),
                root: self.kind.root.clone(),
                unwritten_nodes: AtomicUsize::new(self.kind.unwritten_nodes),
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
        let NodeStore {
            header,
            kind,
            storage,
        } = val;

        let mut nodestore = NodeStore {
            header,
            kind: Arc::new(ImmutableProposal {
                deleted: kind.deleted.into(),
                parent: Arc::new(ArcSwap::new(Arc::new(kind.parent))),
                root_hash: None,
                root: None,
                unwritten_nodes: 0,
            }),
            storage,
        };

        let Some(root) = kind.root else {
            // This trie is now empty.
            nodestore.header.set_root_address(None);
            return Ok(nodestore);
        };

        // Hashes the trie and returns the address of the new root.
        #[cfg(feature = "ethhash")]
        let (root, root_hash, unwritten_count) =
            nodestore.hash_helper(root, &mut Path::new(), None)?;
        #[cfg(not(feature = "ethhash"))]
        let (root, root_hash, unwritten_count) =
            NodeStore::<MutableProposal, S>::hash_helper(root, &mut Path::new())?;

        let immutable_proposal =
            Arc::into_inner(nodestore.kind).expect("no other references to the proposal");
        // Use ManuallyDrop to prevent Drop from running since we're replacing the proposal
        let immutable_proposal = std::mem::ManuallyDrop::new(immutable_proposal);
        nodestore.kind = Arc::new(ImmutableProposal {
            deleted: immutable_proposal.deleted.clone(),
            parent: immutable_proposal.parent.clone(),
            root_hash: Some(root_hash.into_triehash()),
            root: Some(root),
            unwritten_nodes: unwritten_count,
        });

        // Track unwritten nodes in metrics
        #[allow(clippy::cast_precision_loss)]
        firewood_gauge!(
            "firewood.nodes.unwritten",
            "current number of unwritten nodes"
        )
        .increment(unwritten_count as f64);

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
        self.kind.root.as_ref()?.as_shared_node(self).ok()
    }
    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode> {
        self.kind.root.clone()
    }
}

impl<S: ReadableStorage> RootReader for NodeStore<Arc<ImmutableProposal>, S> {
    fn root_node(&self) -> Option<SharedNode> {
        // Use the MaybePersistedNode's as_shared_node method to get the root
        self.kind.root.as_ref()?.as_shared_node(self).ok()
    }
    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode> {
        self.kind.root.clone()
    }
}

impl<T, S> HashedNodeReader for NodeStore<T, S>
where
    NodeStore<T, S>: TrieReader,
    T: Parentable,
    S: ReadableStorage,
{
    fn root_address(&self) -> Option<LinearAddress> {
        self.header.root_address()
    }

    fn root_hash(&self) -> Option<TrieHash> {
        self.kind.root_hash()
    }
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
                self.file_io_error(
                    Error::other("Reader offset went backwards"),
                    actual_addr,
                    Some("read_node_with_num_bytes_from_disk".to_string()),
                )
            })?;
        Ok((node, length))
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
        let mut area_stream = self.storage.stream_from(addr.get())?;

        let index: AreaIndex = area_stream.read_byte().map_err(|e| {
            self.storage.file_io_error(
                Error::new(ErrorKind::InvalidData, e),
                addr.get(),
                Some("area_index_and_size".to_string()),
            )
        })?;

        let size = *AREA_SIZES
            .get(index as usize)
            .ok_or(self.storage.file_io_error(
                Error::other(format!("Invalid area size index {index}")),
                addr.get(),
                Some("area_index_and_size".to_string()),
            ))?;

        Ok((index, size))
    }

    pub(crate) fn physical_size(&self) -> Result<u64, FileIoError> {
        self.storage.size()
    }

    #[cold]
    pub(crate) fn file_io_error(
        &self,
        error: Error,
        addr: u64,
        context: Option<String>,
    ) -> FileIoError {
        self.storage.file_io_error(error, addr, context)
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

impl<S: WritableStorage> NodeStore<Committed, S> {
    /// adjust the freelist of this proposal to reflect the freed nodes in the oldest proposal
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if a node cannot be deleted.
    pub fn reap_deleted(
        mut self,
        proposal: &mut NodeStore<Committed, S>,
    ) -> Result<(), FileIoError> {
        self.storage
            .invalidate_cached_nodes(self.kind.deleted.iter());
        trace!("There are {} nodes to reap", self.kind.deleted.len());
        let mut allocator = NodeAllocator::new(self.storage.as_ref(), &mut proposal.header);
        for node in take(&mut self.kind.deleted) {
            allocator.delete_node(node)?;
        }
        Ok(())
    }
}

// Helper functions for the checker
impl<S: ReadableStorage> NodeStore<Committed, S> {
    pub(crate) const fn size(&self) -> u64 {
        self.header.size()
    }

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

    use crate::LeafNode;
    use crate::linear::memory::MemStore;
    use arc_swap::access::DynGuard;

    use super::*;
    use alloc::{AREA_SIZES, area_size_to_index};

    #[test]
    fn area_sizes_aligned() {
        for area_size in &AREA_SIZES {
            assert_eq!(area_size % LinearAddress::MIN_AREA_SIZE, 0);
        }
    }

    #[test]
    fn test_area_size_to_index() {
        // TODO: rustify using: for size in AREA_SIZES
        for (i, &area_size) in AREA_SIZES.iter().enumerate() {
            // area size is at top of range
            assert_eq!(area_size_to_index(area_size).unwrap(), i as AreaIndex);

            if i > 0 {
                // 1 less than top of range stays in range
                assert_eq!(area_size_to_index(area_size - 1).unwrap(), i as AreaIndex);
            }

            if i < LinearAddress::num_area_sizes() - 1 {
                // 1 more than top of range goes to next range
                assert_eq!(
                    area_size_to_index(area_size + 1).unwrap(),
                    (i + 1) as AreaIndex
                );
            }
        }

        for i in 0..=LinearAddress::MIN_AREA_SIZE {
            assert_eq!(area_size_to_index(i).unwrap(), 0);
        }

        assert!(area_size_to_index(LinearAddress::MAX_AREA_SIZE + 1).is_err());
    }

    #[test]
    fn test_reparent() {
        // create an empty base revision
        let memstore = MemStore::new(vec![]);
        let base = NodeStore::new_empty_committed(memstore.into()).unwrap();

        // create an empty r1, check that it's parent is the empty committed version
        let r1 = NodeStore::new(&base).unwrap();
        let r1: NodeStore<Arc<ImmutableProposal>, _> = r1.try_into().unwrap();
        let parent: DynGuard<Arc<NodeStoreParent>> = r1.kind.parent.load();
        assert!(matches!(**parent, NodeStoreParent::Committed(None)));

        // create an empty r2, check that it's parent is the proposed version r1
        let r2: NodeStore<MutableProposal, _> = NodeStore::new(&r1).unwrap();
        let r2: NodeStore<Arc<ImmutableProposal>, _> = r2.try_into().unwrap();
        let parent: DynGuard<Arc<NodeStoreParent>> = r2.kind.parent.load();
        assert!(matches!(**parent, NodeStoreParent::Proposed(_)));

        // reparent r2
        r1.commit_reparent(&r2);

        // now check r2's parent, should match the hash of r1 (which is still None)
        let parent: DynGuard<Arc<NodeStoreParent>> = r2.kind.parent.load();
        if let NodeStoreParent::Committed(hash) = &**parent {
            assert_eq!(*hash, r1.root_hash());
            assert_eq!(*hash, None);
        } else {
            panic!("expected committed parent");
        }
    }

    #[test]
    #[ignore = "https://github.com/ava-labs/firewood/issues/1054"]
    #[should_panic(expected = "Node size 16777225 is too large")]
    fn giant_node() {
        let memstore = MemStore::new(vec![]);
        let mut node_store = NodeStore::new_empty_proposal(memstore.into());

        let huge_value = vec![0u8; *AREA_SIZES.last().unwrap() as usize];

        let giant_leaf = Node::Leaf(LeafNode {
            partial_path: Path::from([0, 1, 2]),
            value: huge_value.into_boxed_slice(),
        });

        node_store.mut_root().replace(giant_leaf);

        let immutable = NodeStore::<Arc<ImmutableProposal>, _>::try_from(node_store).unwrap();
        println!("{immutable:?}"); // should not be reached, but need to consume immutable to avoid optimization removal
    }
}
