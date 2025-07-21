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
//! - **`ReadInMemoryNode`** - Interface for accessing in-memory nodes
//!

pub(crate) mod alloc;
pub(crate) mod hash;
pub(crate) mod header;
pub(crate) mod persist;

use crate::logger::trace;
use arc_swap::ArcSwap;
use arc_swap::access::DynAccess;
use smallvec::SmallVec;
use std::collections::HashMap;
use std::fmt::Debug;

// Re-export types from alloc module
pub use alloc::{AreaIndex, LinearAddress};

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
use crate::{FileBacked, FileIoError, Path, ReadableStorage, SharedNode, TrieHash};

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
    pub fn commit_reparent(&self, other: &Arc<NodeStore<Arc<ImmutableProposal>, S>>) {
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
    pub fn new<F: Parentable + ReadInMemoryNode>(
        parent: &Arc<NodeStore<F, S>>,
    ) -> Result<Self, FileIoError> {
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
#[derive(Clone, Debug)]
pub struct Committed {
    deleted: Box<[MaybePersistedNode]>,
    root_hash: Option<TrieHash>,
    root: Option<MaybePersistedNode>,
}

impl ReadInMemoryNode for Committed {
    // A committed revision has no in-memory changes. All its nodes are in storage.
    fn read_in_memory_node(&self, _addr: LinearAddress) -> Option<SharedNode> {
        None
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
    /// Address --> Node for nodes created in this proposal.
    new: HashMap<LinearAddress, (u8, SharedNode)>,
    /// Nodes that have been deleted in this proposal.
    deleted: Box<[MaybePersistedNode]>,
    /// The parent of this proposal.
    parent: Arc<ArcSwap<NodeStoreParent>>,
    /// The hash of the root node for this proposal
    root_hash: Option<TrieHash>,
    /// The root node, either in memory or on disk
    root: Option<MaybePersistedNode>,
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

impl ReadInMemoryNode for ImmutableProposal {
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<SharedNode> {
        // Check if the node being requested was created in this proposal.
        if let Some((_, node)) = self.new.get(&addr) {
            return Some(node.clone());
        }

        // It wasn't. Try our parent, and its parent, and so on until we find it or find
        // a committed revision.
        match *self.parent.load() {
            NodeStoreParent::Proposed(ref parent) => parent.read_in_memory_node(addr),
            NodeStoreParent::Committed(_) => None,
        }
    }
}

/// Proposed [`NodeStore`] types keep some nodes in memory. These nodes are new nodes that were allocated from
/// the free list, but are not yet on disk. This trait checks to see if a node is in memory and returns it if
/// it's there. If it's not there, it will be read from disk.
///
/// This trait does not know anything about the underlying storage, so it just returns None if the node isn't in memory.
pub trait ReadInMemoryNode {
    /// Returns the node at `addr` if it is in memory.
    /// Returns None if it isn't.
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<SharedNode>;
}

impl<T> ReadInMemoryNode for T
where
    T: Deref,
    T::Target: ReadInMemoryNode,
{
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<SharedNode> {
        self.deref().read_in_memory_node(addr)
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

impl ReadInMemoryNode for NodeStoreParent {
    /// Returns the node at `addr` if it is in memory from a parent proposal.
    /// If the base revision is committed, there are no in-memory nodes, so we return None
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<SharedNode> {
        match self {
            NodeStoreParent::Proposed(proposed) => proposed.read_in_memory_node(addr),
            NodeStoreParent::Committed(_) => None,
        }
    }
}

impl ReadInMemoryNode for MutableProposal {
    /// [`MutableProposal`] types do not have any nodes in memory, but their parent proposal might, so we check there.
    /// This might be recursive: a grandparent might also have that node in memory.
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<SharedNode> {
        self.parent.read_in_memory_node(addr)
    }
}

impl<T: ReadInMemoryNode + Into<NodeStoreParent>, S: ReadableStorage> From<NodeStore<T, S>>
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
        NodeStore {
            header: val.header,
            kind: Committed {
                deleted: val.kind.deleted,
                root_hash: val.kind.root_hash,
                root: val.kind.root,
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

impl NodeStore<Arc<ImmutableProposal>, FileBacked> {
    /// Return a Committed version of this proposal, which doesn't have any modified nodes.
    /// This function is used during commit.
    #[must_use]
    pub fn as_committed(&self) -> NodeStore<Committed, FileBacked> {
        NodeStore {
            header: self.header,
            kind: Committed {
                deleted: self.kind.deleted.clone(),
                root_hash: self.kind.root_hash.clone(),
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
        let NodeStore {
            header,
            kind,
            storage,
        } = val;

        let mut nodestore = NodeStore {
            header,
            kind: Arc::new(ImmutableProposal {
                new: HashMap::new(),
                deleted: kind.deleted.into(),
                parent: Arc::new(ArcSwap::new(Arc::new(kind.parent))),
                root_hash: None,
                root: None,
            }),
            storage,
        };

        let Some(root) = kind.root else {
            // This trie is now empty.
            nodestore.header.set_root_address(None);
            return Ok(nodestore);
        };

        // Hashes the trie and returns the address of the new root.
        let mut new_nodes = HashMap::new();
        #[cfg(feature = "ethhash")]
        let (root_addr, root_hash) =
            nodestore.hash_helper(root, &mut Path::new(), &mut new_nodes, None)?;
        #[cfg(not(feature = "ethhash"))]
        let (root_addr, root_hash) =
            nodestore.hash_helper(root, &mut Path::new(), &mut new_nodes)?;

        nodestore.header.set_root_address(Some(root_addr));
        let immutable_proposal =
            Arc::into_inner(nodestore.kind).expect("no other references to the proposal");
        nodestore.kind = Arc::new(ImmutableProposal {
            new: new_nodes,
            deleted: immutable_proposal.deleted,
            parent: immutable_proposal.parent,
            root_hash: Some(root_hash.into_triehash()),
            root: Some(root_addr.into()),
        });

        Ok(nodestore)
    }
}

impl<S: ReadableStorage> NodeReader for NodeStore<MutableProposal, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        if let Some(node) = self.kind.read_in_memory_node(addr) {
            return Ok(node);
        }

        self.read_node_from_disk(addr, "write")
    }
}

impl<T: Parentable + ReadInMemoryNode, S: ReadableStorage> NodeReader for NodeStore<T, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        if let Some(node) = self.kind.read_in_memory_node(addr) {
            return Ok(node);
        }
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
        for node in take(&mut self.kind.deleted) {
            proposal.delete_node(node)?;
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
    use std::array::from_fn;

    use crate::linear::memory::MemStore;
    use crate::{BranchNode, Child, LeafNode};
    use arc_swap::access::DynGuard;

    use test_case::test_case;

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
        let base = NodeStore::new_empty_committed(memstore.into())
            .unwrap()
            .into();

        // create an empty r1, check that it's parent is the empty committed version
        let r1 = NodeStore::new(&base).unwrap();
        let r1: Arc<NodeStore<Arc<ImmutableProposal>, _>> = Arc::new(r1.try_into().unwrap());
        let parent: DynGuard<Arc<NodeStoreParent>> = r1.kind.parent.load();
        assert!(matches!(**parent, NodeStoreParent::Committed(None)));

        // create an empty r2, check that it's parent is the proposed version r1
        let r2: NodeStore<MutableProposal, _> = NodeStore::new(&r1.clone()).unwrap();
        let r2: Arc<NodeStore<Arc<ImmutableProposal>, _>> = Arc::new(r2.try_into().unwrap());
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

    #[test_case(BranchNode {
        partial_path: Path::from([6, 7, 8]),
        value: Some(vec![9, 10, 11].into_boxed_slice()),
        children: from_fn(|i| {
            if i == 15 {
                Some(Child::AddressWithHash(LinearAddress::new(1).unwrap(), std::array::from_fn::<u8, 32, _>(|i| i as u8).into()))
            } else {
                None
            }
        }),
    }; "branch node with 1 child")]
    #[test_case(BranchNode {
        partial_path: Path::from([6, 7, 8]),
        value: Some(vec![9, 10, 11].into_boxed_slice()),
        children: from_fn(|_|
            Some(Child::AddressWithHash(LinearAddress::new(1).unwrap(), std::array::from_fn::<u8, 32, _>(|i| i as u8).into()))
        ),
    }; "branch node with all child")]
    #[test_case(
    Node::Leaf(LeafNode {
        partial_path: Path::from([0, 1, 2]),
        value: Box::new([3, 4, 5]),
    }); "leaf node")]

    fn test_serialized_len<N: Into<Node>>(node: N) {
        let node = node.into();

        let computed_length =
            NodeStore::<std::sync::Arc<ImmutableProposal>, MemStore>::stored_len(&node);

        let mut serialized = Vec::new();
        node.as_bytes(0, &mut serialized);
        assert_eq!(serialized.len() as u64, computed_length);
    }
    #[test]
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
