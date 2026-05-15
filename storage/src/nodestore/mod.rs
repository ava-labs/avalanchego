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
//! - [`Mutable<Propose>`] - For a proposal being actively modified with in-memory nodes
//! - [`ImmutableProposal`] - For a proposal that has been hashed and assigned addresses
//! - [`Mutable<Recon>`] - For a reconstruction workspace being actively modified
//! - [`Reconstructed`] - For a reconstructed read-only view created by applying a batch linearly
//!
//! The nodestore follows two lifecycle patterns:
//! ```text
//! Proposal:       Committed -> Mutable<Propose> -> Arc<ImmutableProposal> -> Committed
//! Reconstruction: Committed/Reconstructed -> Mutable<Recon> -> Reconstructed
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
use crate::arc_swap_triomphe::TriompheArc;
use crate::linear::{OffsetReader, ReadableNodeMode};
use crate::logger::{debug, trace};
use crate::node::branch::ReadSerializable as _;
use firewood_metrics::{firewood_counter, firewood_histogram};
use smallvec::SmallVec;
use std::fmt::Debug;
use std::io::{Error, ErrorKind};
use std::num::NonZeroU64;
use std::sync::OnceLock;
use std::time::Instant;

// Re-export types from alloc module
pub use alloc::NodeAllocator;
pub use hash_algo::{NodeHashAlgorithm, NodeHashAlgorithmTryFromIntError};
pub use primitives::{AreaIndex, LinearAddress};
// Re-export types from header module
#[cfg(feature = "ethhash")]
pub use hash::fix_account_storage_root_value;
pub use header::NodeStoreHeader;

/// The [`NodeStore`] handles the serialization of nodes and
/// free space management of nodes in the page store. It lays out the format
/// of the [`PageStore`]. More specifically, it places a [`FileIdentifyingMagic`]
/// and a [`FreeSpaceHeader`] at the beginning
///
/// Nodestores represent a revision of the trie. There are four types of nodestores:
/// - `Committed`: A committed revision of the trie. It has no in-memory changes.
/// - `Mutable<Propose>`: A proposal that is still being modified. It has some nodes in memory, a delete list, and a parent.
/// - `Mutable<Recon>`: A reconstruction workspace being actively modified. It has in-memory nodes but no delete list or parent.
/// - `ImmutableProposal`: A proposal that has been hashed and assigned addresses. It has no in-memory changes.
/// - `Reconstructed`: A reconstructed view that supports read operations and linear reconstruction.
///
/// The general lifecycle of nodestores is as follows:
/// ```mermaid
/// flowchart TD
/// subgraph subgraph["Committed Revisions"]
/// L("Latest Nodestore&lt;Committed, S&gt;") --- |...|O("Oldest NodeStore&lt;Committed, S&gt;")
/// end
/// O --> E("Expire")
/// L --> |start propose|M("NodeStore&lt;Mutable&lt;Propose&gt;, S&gt;")
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

impl<S> NodeStore<Committed, S> {
    /// Returns the [`CommittedId`] that uniquely identifies this committed
    /// revision. This is the identity used to validate a proposal's parent
    /// at commit time; root hash alone would not distinguish hash-equal
    /// revisions like the two ends of an A→B→A round-trip.
    #[must_use]
    pub const fn committed_id(&self) -> CommittedId {
        self.kind.id
    }
}

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
                id: CommittedId::next(),
            },
            storage,
            must_recompute_storage_hash: header.must_recompute_storage_hash(),
        };

        if let Some(root_address) = header.root_address() {
            let root_hash = if let Some(hash) = header.root_hash() {
                hash.into_hash_type()
            } else {
                debug!("No root hash in header; computing from disk");
                nodestore
                    .read_node_from_disk(root_address, ReadableNodeMode::Open)
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
                id: CommittedId::next(),
            },
            must_recompute_storage_hash: true,
        }
    }

    /// Create a new Committed [`NodeStore`] with a specified root node.
    ///
    /// This constructor is used when you have an existing root node at a known
    /// address and hash, typically when reconstructing a [`NodeStore`] from
    /// a committed state.
    ///
    /// ## Errors
    ///
    /// This method reads the root node and verifies that it matches the expected
    /// value. If this read fails or if the hash does not match, an error is
    /// returned
    pub(crate) fn with_root(
        root_hash: HashType,
        root_address: LinearAddress,
        storage: Arc<S>,
    ) -> Result<Self, FileIoError> {
        // first construct a nodestore without a root
        let mut nodestore = NodeStore {
            kind: Committed {
                deleted: Box::default(),
                root: None,
                id: CommittedId::next(),
            },
            storage,
            must_recompute_storage_hash: true,
        };

        let node = nodestore.read_node(root_address)?;

        if hash_node(node.as_ref(), &Path::new()) == root_hash {
            nodestore.kind.root = Some(Child::AddressWithHash(root_address, root_hash));
            Ok(nodestore)
        } else {
            Err(FileIoError::new(
                std::io::Error::other("hash verification failed"),
                None,
                root_address.get(),
                None,
            ))
        }
    }

    /// Returns the length of the deleted list for this `NodeStore`.
    #[must_use]
    pub fn deleted_len(&self) -> usize {
        self.kind.deleted.len()
    }
}

impl<S: ReadableStorage> NodeStore<Committed, S> {
    /// Get the underlying storage for a `NodeStore`.
    #[must_use]
    pub const fn storage(&self) -> &Arc<S> {
        &self.storage
    }
}

/// Some nodestore kinds implement Parentable.
///
/// This means that the nodestore can have children.
/// Only [`ImmutableProposal`] and [Committed] implement this trait.
/// [`Mutable<Propose>`] and [`Mutable<Recon>`] do not implement this trait because they are not valid parents.
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
    /// has the committed proposal as its parent.
    ///
    /// `new_id` is the [`CommittedId`] of the new committed revision produced
    /// from `self` (the just-committed proposal).
    pub fn commit_reparent(
        &self,
        other: &NodeStore<Arc<ImmutableProposal>, S>,
        new_id: CommittedId,
    ) {
        other.kind.commit_reparent(&self.kind, new_id);
    }
}

impl Parentable for Committed {
    fn as_nodestore_parent(&self) -> NodeStoreParent {
        NodeStoreParent::Committed {
            hash: self
                .root
                .as_ref()
                .and_then(|root| root.hash().cloned().map(HashType::into_triehash)),
            id: self.id,
        }
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

impl<S: ReadableStorage> NodeStore<Mutable<Propose>, S> {
    /// Create a new proposal [`NodeStore`] from a parent [`NodeStore`].
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the parent root cannot be read.
    pub fn new<F: Parentable>(parent: &NodeStore<F, S>) -> Result<Self, FileIoError> {
        let mut deleted = Vec::default();
        let root = if let Some(ref root) = parent.kind.root() {
            deleted.push(root.clone());
            let root = triomphe::Arc::unwrap_or_clone(root.as_shared_node(parent)?);
            Some(root)
        } else {
            None
        };
        let kind = Mutable {
            root,
            inner: Propose {
                deleted,
                parent: parent.kind.as_nodestore_parent(),
            },
        };
        Ok(NodeStore {
            kind,
            storage: parent.storage.clone(),
            must_recompute_storage_hash: parent.must_recompute_storage_hash,
        })
    }

    /// Marks the node at `addr` as deleted in this proposal.
    pub fn delete_node(&mut self, node: MaybePersistedNode) {
        trace!("Pending delete at {node:?}");
        self.kind.inner.deleted.push(node);
    }

    /// Take the nodes that have been marked as deleted in this proposal.
    pub fn take_deleted_nodes(&mut self) -> Vec<MaybePersistedNode> {
        take(&mut self.kind.inner.deleted)
    }

    /// Adds to the nodes deleted in this proposal.
    pub fn delete_nodes(&mut self, nodes: &[MaybePersistedNode]) {
        self.kind.inner.deleted.extend_from_slice(nodes);
    }

    /// Creates a new [`NodeStore`] from a root node, inheriting the parent from another proposal.
    #[must_use]
    pub fn from_root(parent: &NodeStore<Mutable<Propose>, S>, root: Option<Node>) -> Self {
        NodeStore {
            kind: Mutable {
                root,
                inner: Propose {
                    deleted: Vec::default(),
                    parent: parent.kind.inner.parent.clone(),
                },
            },
            storage: parent.storage.clone(),
            must_recompute_storage_hash: parent.must_recompute_storage_hash,
        }
    }
}

impl<K: MutableKind, S: ReadableStorage> NodeStore<Mutable<K>, S> {
    /// Reads a node for update, marking it as replaced (logically deleted).
    ///
    /// Reads the node from cache or disk, then calls [`MutableKind::track_deleted`] on the
    /// kind-specific inner state. For [`Propose`] this records the node in the delete list;
    /// for [`Recon`] it is a no-op.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the node cannot be read.
    #[inline]
    pub fn read_for_update(&mut self, node: MaybePersistedNode) -> Result<Node, FileIoError> {
        let arc_wrapped_node = node.as_shared_node(self)?;
        self.kind.inner.track_deleted(node);
        Ok(triomphe::Arc::unwrap_or_clone(arc_wrapped_node))
    }
}

impl<S: ReadableStorage> NodeStore<Mutable<Recon>, S> {
    /// Create a new mutable nodestore for reconstruction from a read-capable parent.
    ///
    /// Unlike [`NodeStore::new`], this constructor does not require `Parentable`.
    /// It is intended for linear reconstruction flows (e.g. historical or reconstructed views)
    /// that should not participate in proposal parent/reparent semantics.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the parent root cannot be read.
    pub(crate) fn new_for_reconstruction<T>(parent: &NodeStore<T, S>) -> Result<Self, FileIoError>
    where
        NodeStore<T, S>: TrieReader,
    {
        let root = if let Some(root) = parent.root_as_maybe_persisted_node() {
            let root = triomphe::Arc::unwrap_or_clone(root.as_shared_node(parent)?);
            Some(root)
        } else {
            None
        };

        Ok(NodeStore {
            kind: Mutable { root, inner: Recon },
            storage: parent.storage.clone(),
            must_recompute_storage_hash: parent.must_recompute_storage_hash,
        })
    }
}

impl<T: ReconstructionSource, S: ReadableStorage> NodeStore<T, S>
where
    NodeStore<T, S>: TrieReader,
{
    /// Create a mutable reconstruction child from a committed or reconstructed parent.
    ///
    /// This constructor is restricted to [`Committed`] and [`Reconstructed`] nodestores
    /// via the [`ReconstructionSource`] bound, preventing reconstruction from proposals.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the parent root cannot be read.
    pub fn reconstruction_child(&self) -> Result<NodeStore<Mutable<Recon>, S>, FileIoError> {
        NodeStore::new_for_reconstruction(self)
    }
}

impl<T, S> NodeStore<Mutable<T>, S> {
    /// Returns the root of this mutable nodestore.
    pub const fn root_mut(&mut self) -> &mut Option<Node> {
        &mut self.kind.root
    }
}

impl<S: WritableStorage> NodeStore<Mutable<Propose>, S> {
    /// Creates a new, empty, [`NodeStore`].
    /// This is used during testing and during the creation of an in-memory merkle for proofs.
    pub fn new_empty_proposal(storage: Arc<S>) -> Self {
        NodeStore {
            kind: Mutable {
                root: None,
                inner: Propose {
                    deleted: Vec::default(),
                    parent: NodeStoreParent::Committed {
                        hash: None,
                        // Sentinel id used only for the empty-trie placeholder
                        // returned by `new_empty_proposal`; this proposal has
                        // no real parent revision to identify, so it can never
                        // commit through `commit_critical_section`.
                        id: CommittedId::next(),
                    },
                },
            },
            storage,
            must_recompute_storage_hash: true,
        }
    }
}

impl<S> NodeStore<Mutable<Recon>, S> {
    /// Creates a new, empty, reconstruction [`NodeStore`].
    /// This is used during testing of reconstruction workflows.
    #[cfg(any(test, feature = "test_utils"))]
    pub const fn new_empty_recon(storage: Arc<S>) -> Self {
        NodeStore {
            kind: Mutable {
                root: None,
                inner: Recon,
            },
            storage,
            must_recompute_storage_hash: true,
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

/// Marker trait for nodestore states that can serve as the source for reconstruction.
///
/// Only [`Committed`] and [`Reconstructed`] implement this trait. Proposals
/// ([`Arc<ImmutableProposal>`] and [`Mutable<Propose>`]) are excluded because
/// reconstruction chains should only branch from persisted or already-reconstructed state.
pub trait ReconstructionSource {}
impl ReconstructionSource for Committed {}
impl ReconstructionSource for Reconstructed {}

/// Reads nodes from a merkle trie.
pub trait NodeReader {
    /// Returns the node at `addr`.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the node cannot be read.
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError>;

    /// Returns `true` if account storage-root hashes must be recomputed at
    /// proof-generation time.
    ///
    /// The default returns `true` (safe for all existing database versions).
    /// [`NodeStore`] overrides this with the value read from the database header.
    fn must_recompute_storage_hash(&self) -> bool {
        true
    }
}

impl<T> NodeReader for T
where
    T: Deref,
    T::Target: NodeReader,
{
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        self.deref().read_node(addr)
    }

    fn must_recompute_storage_hash(&self) -> bool {
        self.deref().must_recompute_storage_hash()
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

/// Monotonic, process-local identifier for a committed revision. Two
/// committed revisions can share a root hash (e.g. an A→B→A round-trip) but
/// always have distinct ids, so this is what proposals capture to identify
/// their committed parent. Unlike root address, ids are assigned
/// synchronously at commit time — they don't race with the persist worker.
#[derive(Copy, Clone, Debug, PartialEq, Eq, Hash)]
pub struct CommittedId(NonZeroU64);

impl CommittedId {
    /// Allocate a fresh id from the process-global counter.
    ///
    /// # Panics
    ///
    /// Panics on overflow: the counter is a `NonZeroU64` and we never want to
    /// silently wrap and collide with an earlier id. At 1 ns per commit
    /// hitting the wrap takes ≈585 years, so a panic here means a bug, not
    /// a workload we need to handle.
    #[must_use]
    pub fn next() -> Self {
        use std::sync::atomic::{AtomicU64, Ordering};
        static NEXT: AtomicU64 = AtomicU64::new(1);
        let raw = NEXT.fetch_add(1, Ordering::Relaxed);
        Self(NonZeroU64::new(raw).expect("CommittedId counter overflowed u64"))
    }
}

/// A committed revision of a merkle trie.
#[derive(Debug, Clone)]
pub struct Committed {
    deleted: Box<[MaybePersistedNode]>,
    root: Option<Child>,
    id: CommittedId,
}

#[derive(Clone, Debug)]
pub enum NodeStoreParent {
    Proposed(Arc<ImmutableProposal>),
    /// Committed parent. `id` pins identity beyond hash equality: two committed
    /// revisions can share a `hash` (e.g. an A→B→A round-trip) but always have
    /// distinct ids, so a proposal whose parent has a stale `id` must NOT be
    /// committable against a hash-equal sibling. Address would also be unique
    /// per revision, but it is assigned asynchronously by the persist worker
    /// and races with proposal creation, so we use a synchronously-assigned
    /// monotonic id instead.
    Committed {
        hash: Option<TrieHash>,
        id: CommittedId,
    },
}

impl PartialEq for NodeStoreParent {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            (NodeStoreParent::Proposed(a), NodeStoreParent::Proposed(b)) => Arc::ptr_eq(a, b),
            (
                NodeStoreParent::Committed { hash: ah, id: ai },
                NodeStoreParent::Committed { hash: bh, id: bi },
            ) => ah == bh && ai == bi,
            _ => false,
        }
    }
}

impl Eq for NodeStoreParent {}

/// The root hash of a proposal's committed parent, or an indication that
/// the parent is another proposal (not yet committed).
#[derive(Debug, Clone)]
pub enum CommittedParentHash {
    /// Parent is committed with this root hash (`None` for an empty trie).
    Hash(Option<TrieHash>),
    /// Parent is another proposal, not yet committed.
    NotCommitted,
}

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
    /// Returns true if the parent of this proposal is the committed revision
    /// identified by `id`.
    ///
    /// `id` is fully unique per committed revision, so hash comparison would
    /// be redundant. (We still keep the hash in [`NodeStoreParent::Committed`]
    /// for the rebase path, which looks up the old parent revision by hash.)
    /// Hash equality alone is not sufficient: a database can produce two
    /// committed revisions with the same root hash (an A→B→A round trip),
    /// and a proposal built off the first "A" must not be committable against
    /// the second "A" — the deleted list still refers to the *first* A's
    /// nodes, which may already have been freed and reallocated.
    #[must_use]
    fn parent_id_is(&self, id: CommittedId) -> bool {
        match &*self.parent.lock() {
            NodeStoreParent::Committed { id: parent_id, .. } => *parent_id == id,
            NodeStoreParent::Proposed(_) => false,
        }
    }

    /// Returns the committed parent's root hash, or indicates the parent
    /// is another proposal.
    fn committed_parent_hash(&self) -> CommittedParentHash {
        match &*self.parent.lock() {
            NodeStoreParent::Committed { hash, .. } => CommittedParentHash::Hash(hash.clone()),
            NodeStoreParent::Proposed(_) => CommittedParentHash::NotCommitted,
        }
    }

    /// Reparent any of `self`'s parent pointers that still reference
    /// `committing` (a proposal that just became committed) to point at the
    /// new committed revision instead.
    ///
    /// `new_id` is the [`CommittedId`] assigned to the new committed
    /// revision; the caller must pass the same id that was used when the
    /// proposal was converted via [`NodeStore::as_committed`].
    fn commit_reparent(self: &Arc<Self>, committing: &Arc<Self>, new_id: CommittedId) {
        let mut guard = self.parent.lock();
        if let NodeStoreParent::Proposed(ref parent) = *guard
            && Arc::ptr_eq(parent, committing)
        {
            *guard = NodeStoreParent::Committed {
                hash: committing.root_hash(),
                id: new_id,
            };
            // Track reparenting events
            firewood_counter!(REPARENTED_PROPOSAL_COUNT).increment(1);
        }
    }
}

/// Contains the state of a revision of a merkle trie.
///
/// The first generic parameter `T` is the state type (see module docs for the full list).
/// The second generic parameter `S` is the storage backend (in-memory or on-disk).
///
/// The general lifecycle of nodestores is as follows.
/// Red arrows consume their source; blue arrows borrow it.
#[cfg_attr(doc, aquamarine::aquamarine)]
/// ```mermaid
/// flowchart TD
///
/// subgraph committed["Committed Revisions"]
///   L("Latest Committed")
///   O("Oldest Committed")
/// end
///
/// O -->|"PersistWorker::reap"| E(("Expired"))
///
/// L -->|"Db::propose"| MP("Mutable< Propose >")
/// MP --> IP("Arc< ImmutableProposal >")
/// IP -->|"Proposal::commit"| C2("New Committed")
/// IP -->|"Proposal::propose"| MP
///
/// committed -->|"reconstruct"| MR("Mutable< Recon >")
/// R -->|"reconstruct"| MR
/// MR --> R("Reconstructed")
///
/// linkStyle 0 stroke:red
/// linkStyle 1 stroke:blue
/// linkStyle 2 stroke:red
/// linkStyle 3 stroke:red
/// linkStyle 4 stroke:blue
/// linkStyle 5 stroke:blue
/// linkStyle 6 stroke:red
/// linkStyle 7 stroke:red
///
/// style E color:#FFFFFF, fill:#AA00FF, stroke:#AA00FF
/// style MP fill:#E8F5E9
/// style IP fill:#E8F5E9
/// style MR fill:#E3F2FD
/// style R fill:#E3F2FD
/// ```
///
/// # Proposal lifecycle
///
/// 1. Create a new, empty, [Committed] [`NodeStore`] using [`NodeStore::new_empty_committed`] or open from disk with [`NodeStore::open`].
/// 2. Create a [`Mutable<Propose>`] from a [Committed] or [`Arc<ImmutableProposal>`] parent using [`NodeStore::new`].
/// 3. Apply batch operations to the mutable proposal via `Merkle` insert/remove.
/// 4. Convert to [`Arc<ImmutableProposal>`] via [`TryFrom`], which hashes nodes and assigns addresses.
/// 5. Commit to a new [Committed] revision via [`NodeStore::as_committed`] or [`From<ImmutableProposal>`].
///
/// # Reconstruction lifecycle
///
/// 1. Create a [`Mutable<Recon>`] from a [Committed] or [`Reconstructed`] parent
///    using [`NodeStore::reconstruction_child`].
/// 2. Apply batch operations to the mutable reconstruction via `Merkle` insert/remove.
/// 3. Convert to [`Reconstructed`] via [`From`]. The resulting view supports reads and lazy hashing.
/// 4. Chain further reconstructions: convert back to [`Mutable<Recon>`] via [`From`] and repeat.
///
#[derive(Debug)]
pub struct NodeStore<T, S> {
    /// This is one of [Committed], [`ImmutableProposal`], [`Mutable<Propose>`], [`Mutable<Recon>`], or [`Reconstructed`].
    kind: T,
    /// Persisted storage to read nodes from.
    storage: Arc<S>,
    /// Whether account storage-root hashes must be recomputed at
    /// proof-generation time. Set from the database header version.
    must_recompute_storage_hash: bool,
}

/// Contains state for a reconstructed revision of the trie.
/// These are unparented, and only contain an in-memory root.
///
/// The root node is held in an [`arc_swap::ArcSwapAny`] so that
/// [`HashedNodeReader::root_hash`] can replace it atomically with the
/// fully-hashed version produced by hashing (children rewritten from
/// `Child::Node` to `Child::MaybePersisted`). Reconstruction chains that
/// never request the hash never pay for the swap.
#[derive(Debug)]
pub struct Reconstructed {
    /// The current root node. `None` for an empty trie; otherwise an
    /// atomically-swappable `SharedNode`. After the first `root_hash` call
    /// the swapped-in value's children are all `Child::MaybePersisted`.
    root: Option<arc_swap::ArcSwapAny<TriompheArc<Node>>>,
    /// Lazily computed root hash (write-once, then lock-free reads).
    hash: OnceLock<TrieHash>,
}

impl Clone for Reconstructed {
    fn clone(&self) -> Self {
        Self {
            root: self
                .root
                .as_ref()
                .map(|swap| arc_swap::ArcSwapAny::new(swap.load_full())),
            hash: self.hash.clone(),
        }
    }
}

/// Proposal-specific fields for a mutable nodestore.
#[derive(Debug)]
pub struct Propose {
    /// Nodes that have been deleted in this proposal.
    pub(crate) deleted: Vec<MaybePersistedNode>,
    pub(crate) parent: NodeStoreParent,
}

/// Reconstruction-specific marker for a mutable nodestore.
/// Zero-size: no delete list and no parent are needed for linear reconstruction chains.
#[derive(Debug)]
pub struct Recon;

/// Behaviour that differs between proposal and reconstruction mutable nodestores.
///
/// Types that implement this trait can be used as the `Kind` parameter of [`Mutable`],
/// allowing [`NodeStore`] operations such as [`NodeStore::read_for_update`] and
/// `Merkle` insert/remove to work for both workflows.
pub trait MutableKind: std::fmt::Debug {
    /// Record that `node` is being replaced (i.e. logically deleted).
    ///
    /// For [`Propose`] this appends the node to the delete list so that its
    /// storage can be reclaimed when the proposal is committed.
    /// For [`Recon`] this is a no-op: reconstruction views are ephemeral and
    /// do not participate in the future-delete log.
    fn track_deleted(&mut self, node: MaybePersistedNode);
}

impl MutableKind for Propose {
    #[inline]
    fn track_deleted(&mut self, node: MaybePersistedNode) {
        trace!("Pending delete at {node:?}");
        self.deleted.push(node);
    }
}

impl MutableKind for Recon {
    #[inline]
    fn track_deleted(&mut self, _node: MaybePersistedNode) {
        // Reconstruction views are ephemeral; no delete list to maintain.
    }
}

/// Contains the state of a nodestore that is still being modified.
///
/// The type parameter `Kind` is either [`Propose`] (for building proposals,
/// with a delete list and parent) or [`Recon`] (for linear reconstruction
/// chains, with neither).
#[derive(Debug)]
pub struct Mutable<Kind> {
    /// The root of the trie in this mutable nodestore.
    pub(crate) root: Option<Node>,
    pub(crate) inner: Kind,
}

/// Given root from either a Committed or Reconstructed,
/// create a mutable root node for a new Reconstructed
/// with this root.
/// For reconstruct on reconstruct, this avoids cloning
impl<S: ReadableStorage> From<NodeStore<Reconstructed, S>> for NodeStore<Mutable<Recon>, S> {
    fn from(val: NodeStore<Reconstructed, S>) -> Self {
        // Consume the ArcSwap to get back the SharedNode, then unwrap_or_clone to extract
        // an owned Node. In the linear M->R->M->R chain the SharedNode is uniquely held,
        // so this is a free move.
        let root = val
            .kind
            .root
            .map(|swap| triomphe::Arc::unwrap_or_clone(swap.into_inner().into_inner()));
        NodeStore {
            kind: Mutable { root, inner: Recon },
            storage: val.storage,
            must_recompute_storage_hash: val.must_recompute_storage_hash,
        }
    }
}

impl<S: ReadableStorage> From<NodeStore<Mutable<Recon>, S>> for NodeStore<Reconstructed, S> {
    fn from(val: NodeStore<Mutable<Recon>, S>) -> Self {
        NodeStore {
            kind: Reconstructed {
                root: val
                    .kind
                    .root
                    .map(|n| arc_swap::ArcSwapAny::new(TriompheArc::new(triomphe::Arc::new(n)))),
                hash: OnceLock::new(),
            },
            storage: val.storage,
            must_recompute_storage_hash: val.must_recompute_storage_hash,
        }
    }
}

impl<S: ReadableStorage> From<Arc<NodeStore<Reconstructed, S>>> for NodeStore<Mutable<Recon>, S> {
    fn from(val: Arc<NodeStore<Reconstructed, S>>) -> Self {
        // Fast path: if this Arc is uniquely owned, `try_unwrap` is O(1) and lets us move the
        // reconstructed root out without cloning.
        // Why this Arc might be shared: a caller could keep another handle alive (e.g. iterating
        // over a reconstructed view while also deriving the next reconstructed state), which makes
        // `try_unwrap` fail even though conversion is still valid.
        // Fallback cost: when shared, we must clone the `NodeStore<Reconstructed, S>`, which
        // clones the root `SharedNode` arc and then `Arc::unwrap_or_clone` extracts the `Node`.
        // This shared-Arc fallback does not match our known reconstruction use cases, where the
        // previous reconstructed handle is typically consumed before building the next one.
        // We do this to avoid forcing callers to guarantee uniqueness while still exploiting the
        // cheaper move path whenever possible.
        Self::from(Arc::unwrap_or_clone(val))
    }
}

/// Implement clone for `NodeStore<Reconstructed, S>`.
///
/// This clones the [`SharedNode`] arc (cheap ref-count bump) and the
/// [`OnceLock`] hash (cloned if already computed, empty otherwise).
impl<S> Clone for NodeStore<Reconstructed, S> {
    fn clone(&self) -> Self {
        NodeStore {
            kind: self.kind.clone(),
            storage: self.storage.clone(),
            must_recompute_storage_hash: self.must_recompute_storage_hash,
        }
    }
}

impl<S: ReadableStorage> NodeStore<Arc<ImmutableProposal>, S> {
    /// Returns true if the parent of this proposal is the committed revision
    /// identified by `id`.
    ///
    /// `id` is fully unique per committed revision, so hash comparison would
    /// be redundant. Hash equality alone is not sufficient: a database can
    /// produce two committed revisions with the same root hash (an A→B→A
    /// round trip), and a proposal built off the first "A" must not be
    /// committable against the second "A" — the deleted list still refers
    /// to the *first* A's nodes, which may already have been freed and
    /// reallocated.
    #[must_use]
    pub fn parent_id_is(&self, id: CommittedId) -> bool {
        self.kind.parent_id_is(id)
    }

    /// Returns the committed parent's root hash, or indicates the parent
    /// is another proposal.
    #[must_use]
    pub fn committed_parent_hash(&self) -> CommittedParentHash {
        self.kind.committed_parent_hash()
    }

    /// Returns a fresh, empty committed view sharing this proposal's storage.
    ///
    /// Intended for diffing against an empty parent in `commit_with_rebase`
    /// when the proposal's recorded committed parent was an empty trie.
    /// Using a synthetic empty view avoids relying on the manager's revisions
    /// queue, whose front entry is not guaranteed to be the initial empty
    /// revision once `max_revisions` is exceeded.
    #[must_use]
    pub fn empty_committed_sibling(&self) -> NodeStore<Committed, S> {
        NodeStore::new_empty_committed(self.storage.clone())
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
                id: CommittedId::next(),
            },
            storage: self.storage.clone(),
            must_recompute_storage_hash: self.must_recompute_storage_hash,
        }
    }
}

impl<S: ReadableStorage> TryFrom<NodeStore<Mutable<Propose>, S>>
    for NodeStore<Arc<ImmutableProposal>, S>
{
    type Error = FileIoError;

    fn try_from(val: NodeStore<Mutable<Propose>, S>) -> Result<Self, Self::Error> {
        let NodeStore {
            kind,
            storage,
            must_recompute_storage_hash,
        } = val;
        let Mutable {
            root,
            inner: Propose { deleted, parent },
        } = kind;

        let mut nodestore = NodeStore {
            kind: Arc::new(ImmutableProposal {
                deleted: deleted.into(),
                parent: Arc::new(parking_lot::Mutex::new(parent)),
                root: None,
            }),
            storage,
            must_recompute_storage_hash,
        };

        let Some(root) = root else {
            // This trie is now empty. Root address will be set to None during persist.
            return Ok(nodestore);
        };

        // Hashes the trie with an empty path and returns the address of the new root.
        #[cfg(feature = "ethhash")]
        let (root, root_hash) = nodestore.hash_helper(root, Path::new())?;
        #[cfg(not(feature = "ethhash"))]
        let (root, root_hash) = NodeStore::<Mutable<Propose>, S>::hash_helper(root, Path::new())?;

        let immutable_proposal =
            Arc::into_inner(nodestore.kind).expect("no other references to the proposal");
        nodestore.kind = Arc::new(ImmutableProposal {
            deleted: immutable_proposal.deleted,
            parent: immutable_proposal.parent,
            root: Some(Child::MaybePersisted(root, root_hash)),
        });

        Ok(nodestore)
    }
}

impl<T, S: ReadableStorage> NodeReader for NodeStore<Mutable<T>, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        self.read_node_from_disk(addr, ReadableNodeMode::Write)
    }

    fn must_recompute_storage_hash(&self) -> bool {
        self.must_recompute_storage_hash
    }
}

impl<S: ReadableStorage> NodeReader for NodeStore<Reconstructed, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        self.read_node_from_disk(addr, ReadableNodeMode::ReconRead)
    }

    fn must_recompute_storage_hash(&self) -> bool {
        self.must_recompute_storage_hash
    }
}

impl<T: Parentable, S: ReadableStorage> NodeReader for NodeStore<T, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, FileIoError> {
        self.read_node_from_disk(addr, ReadableNodeMode::Read)
    }

    fn must_recompute_storage_hash(&self) -> bool {
        self.must_recompute_storage_hash
    }
}

impl<T, S: ReadableStorage> RootReader for NodeStore<Mutable<T>, S> {
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

impl<S: ReadableStorage> RootReader for NodeStore<Reconstructed, S> {
    fn root_node(&self) -> Option<SharedNode> {
        self.kind
            .root
            .as_ref()
            .map(|swap| swap.load_full().into_inner())
    }

    fn root_as_maybe_persisted_node(&self) -> Option<MaybePersistedNode> {
        self.root_node().map(Into::into)
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

// This implements HashedNodeReader, can never be persisted,
// and the root_hash is computed lazily via OnceLock.
impl<S: ReadableStorage> HashedNodeReader for NodeStore<Reconstructed, S>
where
    NodeStore<Reconstructed, S>: TrieReader,
{
    fn root_address(&self) -> Option<LinearAddress> {
        // Reconstructed views are read-only overlays and are never persisted.
        None
    }

    // Lazily compute and cache the hash. The hashed root (with children rewritten
    // to `Child::MaybePersisted`) is swapped back into `self.kind.root` so any
    // subsequent fork-then-reconstruct walks Arc-shared children instead of
    // deep-cloning every `Child::Node` subtree. We don't parallelise the hash
    // because reconstructed views don't benefit much from multiple threads.
    fn root_hash(&self) -> Option<TrieHash> {
        if let Some(hash) = self.kind.hash.get() {
            return Some(hash.clone());
        }
        let swap = self.kind.root.as_ref()?;
        let current = swap.load_full();
        let node_val: Node = Node::clone(&current);
        #[cfg(feature = "ethhash")]
        let (hashed_mp, hash) = self.hash_helper(node_val, Path::new()).ok()?;
        #[cfg(not(feature = "ethhash"))]
        let (hashed_mp, hash) =
            NodeStore::<Mutable<Propose>, S>::hash_helper(node_val, Path::new()).ok()?;
        // Extract the in-memory SharedNode from the freshly-built MaybePersistedNode
        // (always the `Unpersisted` variant here, so this is a Mutex lock + Arc clone,
        // no I/O). Replace the unhashed root with the fully-hashed one. If another
        // thread raced us, the loser's swap overwrites with an equivalent tree; both
        // contain the same trie content, only the children's form differs
        // (`Child::Node` vs `Child::MaybePersisted`) and both are valid reads.
        let hashed_shared = hashed_mp.as_shared_node(self).ok()?;
        swap.store(TriompheArc::new(hashed_shared));
        let hash = hash.into_triehash();
        // If another thread raced us on the hash too, discard ours — same value.
        let _ = self.kind.hash.set(hash.clone());
        Some(hash)
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
            Some("area_index_and_size".to_owned()),
        )
    })?)
    .ok_or_else(|| {
        storage.file_io_error(
            Error::new(ErrorKind::InvalidData, "invalid area index"),
            addr.get(),
            Some("area_index_and_size".to_owned()),
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
    pub(crate) fn read_node_from_disk(
        &self,
        addr: LinearAddress,
        mode: ReadableNodeMode,
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
                    .file_io_error(e, actual_addr, Some("read_node_from_disk".to_owned()))
            })?
            .into();
        let length = area_stream
            .offset()
            .checked_sub(offset_before)
            .ok_or_else(|| {
                self.storage.file_io_error(
                    Error::other("Reader offset went backwards"),
                    actual_addr,
                    Some("read_node_with_num_bytes_from_disk".to_owned()),
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
        let reap_start = Instant::now();

        self.storage
            .invalidate_cached_nodes(self.kind.deleted.iter());
        trace!("There are {} nodes to reap", self.kind.deleted.len());
        let mut allocator = NodeAllocator::new(self.storage.as_ref(), header);
        for node in take(&mut self.kind.deleted) {
            allocator.delete_node(node)?;
        }

        firewood_histogram!(cheap: REAP_DURATION_SECONDS)
            .record(reap_start.elapsed().as_secs_f64());

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
            assert!(matches!(
                *parent,
                NodeStoreParent::Committed { hash: None, .. }
            ));
        }

        // create an empty r2, check that it's parent is the proposed version r1
        let r2: NodeStore<Mutable<Propose>, _> = NodeStore::new(&r1).unwrap();
        let r2: NodeStore<Arc<ImmutableProposal>, _> = r2.try_into().unwrap();
        {
            let parent = r2.kind.parent.lock();
            assert!(matches!(*parent, NodeStoreParent::Proposed(_)));
        }

        // reparent r2
        r1.commit_reparent(&r2, CommittedId::next());

        // now check r2's parent, should match the hash of r1 (which is still None)
        let parent = r2.kind.parent.lock();
        if let NodeStoreParent::Committed { hash, .. } = &*parent {
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

    #[test]
    fn with_root_success() -> Result<(), Box<dyn Error>> {
        let tmpdir = tempfile::tempdir()?;
        let dbfile = tmpdir.path().join("with_root_test.db");

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
        let base = NodeStore::open(&header, Arc::clone(&storage))?;

        // Create a proposal with a leaf node and persist it
        let mut proposal = NodeStore::new(&base)?;
        proposal.root_mut().replace(Node::Leaf(LeafNode {
            partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"key")),
            value: b"value".to_vec().into_boxed_slice(),
        }));
        let proposal = NodeStore::<Arc<ImmutableProposal>, _>::try_from(proposal)?;
        let committed = proposal.as_committed();
        committed.persist(&mut header)?;

        // Retrieve root address and hash from the header
        let root_address = header.root_address().unwrap();
        let root_hash = header.root_hash().unwrap().into_hash_type();

        // Reconstruct using with_root
        let restored = NodeStore::with_root(root_hash.clone(), root_address, storage)?;
        assert_eq!(restored.root_hash(), Some(root_hash.into_triehash()));
        assert_eq!(restored.root_address(), Some(root_address));

        Ok(())
    }

    #[test]
    fn with_root_wrong_hash() -> Result<(), Box<dyn Error>> {
        let tmpdir = tempfile::tempdir()?;
        let dbfile = tmpdir.path().join("with_root_bad_hash_test.db");

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
        let base = NodeStore::open(&header, Arc::clone(&storage))?;

        // Create a proposal with a leaf node and persist it
        let mut proposal = NodeStore::new(&base)?;
        proposal.root_mut().replace(Node::Leaf(LeafNode {
            partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"key")),
            value: b"value".to_vec().into_boxed_slice(),
        }));
        let proposal = NodeStore::<Arc<ImmutableProposal>, _>::try_from(proposal)?;
        let committed = proposal.as_committed();
        committed.persist(&mut header)?;

        let root_address = header.root_address().unwrap();

        // Use a bogus hash
        let bad_hash = HashType::from([0xAB; 32]);
        let result = NodeStore::with_root(bad_hash, root_address, storage);
        assert!(result.is_err());
        let err = result.unwrap_err();
        assert!(
            err.source()
                .unwrap()
                .to_string()
                .contains("hash verification failed")
        );

        Ok(())
    }

    #[test]
    fn reconstructed_root_address_is_none() {
        let storage = Arc::new(MemStore::default());
        let mut recon = NodeStore::new_empty_recon(Arc::clone(&storage));

        recon.root_mut().replace(Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: b"value".to_vec().into_boxed_slice(),
        }));

        let reconstructed: NodeStore<Reconstructed, _> = recon.into();

        assert_eq!(reconstructed.root_address(), None);
    }

    #[test]
    fn reconstructed_conversion_defers_hashing() {
        let storage = Arc::new(MemStore::default());
        let mut recon = NodeStore::new_empty_recon(Arc::clone(&storage));

        recon.root_mut().replace(Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: b"value".to_vec().into_boxed_slice(),
        }));

        let reconstructed: NodeStore<Reconstructed, _> = recon.into();

        // Conversion should not eagerly hash reconstructed roots.
        assert!(reconstructed.kind.hash.get().is_none());
    }

    #[test]
    fn reconstructed_root_hash_is_memoized() {
        let storage = Arc::new(MemStore::default());
        let mut recon = NodeStore::new_empty_recon(Arc::clone(&storage));

        recon.root_mut().replace(Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: b"value".to_vec().into_boxed_slice(),
        }));

        let reconstructed: NodeStore<Reconstructed, _> = recon.into();

        // Before hashing, the OnceLock is empty
        assert!(reconstructed.kind.hash.get().is_none());

        let first_hash = reconstructed.root_hash();
        // After hashing, the root hash is memoized in the OnceLock
        assert!(first_hash.is_some());
        assert_eq!(reconstructed.kind.hash.get().cloned(), first_hash);
        let second_hash = reconstructed.root_hash();
        assert_eq!(first_hash, second_hash);
    }

    #[test]
    fn reconstructed_empty_root_hash_is_none() {
        let storage = Arc::new(MemStore::default());
        let recon = NodeStore::new_empty_recon(Arc::clone(&storage));

        let reconstructed: NodeStore<Reconstructed, _> = recon.into();

        assert_eq!(reconstructed.root_hash(), None);
    }

    #[test]
    fn reconstructed_root_hash_rewrites_root_children() {
        // After root_hash() runs, the swapped-in root must have no Child::Node
        // children — they should all be Child::MaybePersisted. hash_helper works
        // bottom-up, so if no Child::Node remains at the root, none remains below.
        let storage = Arc::new(MemStore::default());
        let mut recon = NodeStore::new_empty_recon(Arc::clone(&storage));

        let mut children = Children::new();
        children[PathComponent::ALL[0x0]] = Some(Child::Node(Node::Leaf(LeafNode {
            partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"abc")),
            value: b"v0".to_vec().into_boxed_slice(),
        })));
        children[PathComponent::ALL[0xF]] = Some(Child::Node(Node::Leaf(LeafNode {
            partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"xyz")),
            value: b"vf".to_vec().into_boxed_slice(),
        })));
        recon.root_mut().replace(Node::Branch(Box::new(BranchNode {
            partial_path: Path::new(),
            value: None,
            children,
        })));

        let reconstructed: NodeStore<Reconstructed, _> = recon.into();

        // Sanity: pre-hash, the root branch has at least one Child::Node.
        let before = reconstructed.root_node().expect("root present");
        let Node::Branch(b) = &*before else {
            panic!("root must be a branch");
        };
        assert!(
            b.children
                .iter_present()
                .any(|(_, c)| matches!(c, Child::Node(_))),
            "fixture must start with at least one Child::Node"
        );
        drop(before);

        assert!(reconstructed.root_hash().is_some());

        // After root_hash, no Child::Node remains at the root.
        let after = reconstructed.root_node().expect("root present");
        let Node::Branch(b) = &*after else {
            panic!("root must still be a branch");
        };
        for (_, child) in b.children.iter_present() {
            assert!(
                !matches!(child, Child::Node(_)),
                "root child still Child::Node after root_hash: {child:?}"
            );
        }
    }
}
