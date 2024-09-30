// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::logger::trace;
use arc_swap::access::DynAccess;
use arc_swap::ArcSwap;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fmt::Debug;
/// The [NodeStore] handles the serialization of nodes and
/// free space management of nodes in the page store. It lays out the format
/// of the [PageStore]. More specifically, it places a [FileIdentifyingMagic]
/// and a [FreeSpaceHeader] at the beginning
///
/// Nodestores represent a revision of the trie. There are three types of nodestores:
/// - Committed: A committed revision of the trie. It has no in-memory changes.
/// - MutableProposal: A proposal that is still being modified. It has some nodes in memory.
/// - ImmutableProposal: A proposal that has been hashed and assigned addresses. It has no in-memory changes.
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
///
/// Nodestores represent a revision of the trie. There are three types of nodestores:
/// - Committed: A committed revision of the trie. It has no in-memory changes.
/// - MutableProposal: A proposal that is still being modified. It has some nodes in memory.
/// - ImmutableProposal: A proposal that has been hashed and assigned addresses. It has no in-memory changes.
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
use std::io::{Error, ErrorKind, Write};
use std::iter::once;
use std::mem::offset_of;
use std::num::NonZeroU64;
use std::ops::Deref;
use std::sync::Arc;

use crate::hashednode::hash_node;
use crate::node::Node;
use crate::{Child, FileBacked, Path, ReadableStorage, TrieHash};

use super::linear::WritableStorage;

/// [NodeStore] divides the linear store into blocks of different sizes.
/// [AREA_SIZES] is every valid block size.
const AREA_SIZES: [u64; 21] = [
    1 << MIN_AREA_SIZE_LOG, // Min block size is 8
    1 << 4,
    1 << 5,
    1 << 6,
    1 << 7,
    1 << 8,
    1 << 9,
    1 << 10,
    1 << 11,
    1 << 12,
    1 << 13,
    1 << 14,
    1 << 15,
    1 << 16,
    1 << 17,
    1 << 18,
    1 << 19,
    1 << 20,
    1 << 21,
    1 << 22,
    1 << 23, // 16 MiB
];

/// The type of an index into the [AREA_SIZES] array
/// This is not usize because we can store this as a single byte
pub type AreaIndex = u8;

// TODO danlaine: have type for index in AREA_SIZES
// Implement try_into() for it.
const MIN_AREA_SIZE_LOG: AreaIndex = 3;
const NUM_AREA_SIZES: usize = AREA_SIZES.len();
const MIN_AREA_SIZE: u64 = AREA_SIZES[0];
const MAX_AREA_SIZE: u64 = AREA_SIZES[NUM_AREA_SIZES - 1];

const SOME_FREE_LIST_ELT_SIZE: u64 = 1 + std::mem::size_of::<LinearAddress>() as u64;
const FREE_LIST_MAX_SIZE: u64 = NUM_AREA_SIZES as u64 * SOME_FREE_LIST_ELT_SIZE;

/// Returns the index in `BLOCK_SIZES` of the smallest block size >= `n`.
fn area_size_to_index(n: u64) -> Result<AreaIndex, Error> {
    if n > MAX_AREA_SIZE {
        return Err(Error::new(
            ErrorKind::InvalidData,
            format!("Node size {} is too large", n),
        ));
    }

    if n <= MIN_AREA_SIZE {
        return Ok(0);
    }

    let mut log = n.ilog2();
    // If n is not a power of 2, we need to round up to the next power of 2.
    if n != 1 << log {
        log += 1;
    }

    Ok(log as AreaIndex - MIN_AREA_SIZE_LOG)
}

/// Objects cannot be stored at the zero address, so a [LinearAddress] is guaranteed not
/// to be zero. This reserved zero can be used as a [None] value for some use cases. In particular,
/// branches can use `Option<LinearAddress>` which is the same size as a [LinearAddress]
pub type LinearAddress = NonZeroU64;

/// Each [StoredArea] contains an [Area] which is either a [Node] or a [FreeArea].
#[derive(PartialEq, Eq, Clone, Debug, Deserialize, Serialize)]
enum Area<T, U> {
    Node(T),
    Free(U),
}

/// Every item stored in the [NodeStore]'s ReadableStorage  after the
/// [NodeStoreHeader] is a [StoredArea].
#[derive(PartialEq, Eq, Clone, Debug, Deserialize, Serialize)]
struct StoredArea<T> {
    /// Index in [AREA_SIZES] of this area's size
    area_size_index: AreaIndex,
    area: T,
}

impl<T: ReadInMemoryNode, S: ReadableStorage> NodeStore<T, S> {
    /// Returns (index, area_size) for the [StoredArea] at `addr`.
    /// `index` is the index of `area_size` in [AREA_SIZES].
    #[allow(dead_code)]
    fn area_index_and_size(&self, addr: LinearAddress) -> Result<(AreaIndex, u64), Error> {
        let mut area_stream = self.storage.stream_from(addr.get())?;

        let index: AreaIndex = bincode::deserialize_from(&mut area_stream)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

        let size = *AREA_SIZES.get(index as usize).ok_or(Error::new(
            ErrorKind::InvalidData,
            format!("Invalid area size index {}", index),
        ))?;

        Ok((index, size))
    }

    /// Read a [Node] from the provided [LinearAddress].
    /// `addr` is the address of a StoredArea in the ReadableStorage.
    pub fn read_node_from_disk(&self, addr: LinearAddress) -> Result<Arc<Node>, Error> {
        if let Some(node) = self.storage.read_cached_node(addr) {
            return Ok(node);
        }

        debug_assert!(addr.get() % 8 == 0);

        let addr = addr.get() + 1; // Skip the index byte

        let area_stream = self.storage.stream_from(addr)?;
        let area: Area<Node, FreeArea> = bincode::deserialize_from(area_stream)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

        match area {
            Area::Node(node) => Ok(node.into()),
            Area::Free(_) => Err(Error::new(
                ErrorKind::InvalidData,
                "Attempted to read a freed area",
            )),
        }
    }
}

impl<S: ReadableStorage> NodeStore<Committed, S> {
    /// Open an existing [NodeStore]
    /// Assumes the header is written in the [ReadableStorage].
    pub fn open(storage: Arc<S>) -> Result<Self, Error> {
        let mut stream = storage.stream_from(0)?;

        let header: NodeStoreHeader = bincode::deserialize_from(&mut stream)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

        drop(stream);

        let mut nodestore = Self {
            header,
            kind: Committed {
                deleted: Default::default(),
                root_hash: None,
            },
            storage,
        };

        if let Some(root_address) = nodestore.header.root_address {
            let node = nodestore.read_node_from_disk(root_address);
            let root_hash = node.map(|n| hash_node(&n, &Path(Default::default())))?;
            nodestore.kind.root_hash = Some(root_hash);
        }

        Ok(nodestore)
    }

    /// Create a new, empty, Committed [NodeStore] and clobber
    /// the underlying store with an empty freelist and no root node
    pub fn new_empty_committed(storage: Arc<S>) -> Result<Self, Error> {
        let header = NodeStoreHeader::new();

        // let header_bytes = bincode::serialize(&header).map_err(|e| {
        //     Error::new(
        //         ErrorKind::InvalidData,
        //         format!("Failed to serialize header: {}", e),
        //     )
        // })?;

        // storage.write(0, header_bytes.as_slice())?;

        Ok(Self {
            header,
            storage,
            kind: Committed {
                deleted: Default::default(),
                root_hash: None,
            },
        })
    }
}

/// Some nodestore kinds implement Parentable.
///
/// This means that the nodestore can have children.
/// Only [ImmutableProposal] and [Committed] implement this trait.
/// [MutableProposal] does not implement this trait because it is not a valid parent.
/// TODO: Maybe this can be renamed to ImmutableNodestore
pub trait Parentable {
    /// Returns the parent of this nodestore.
    fn as_nodestore_parent(&self) -> NodeStoreParent;
    /// Returns the root hash of this nodestore. This works because all parentable nodestores have a hash
    fn root_hash(&self) -> Option<TrieHash>;
}

impl Parentable for Arc<ImmutableProposal> {
    fn as_nodestore_parent(&self) -> NodeStoreParent {
        NodeStoreParent::Proposed(Arc::clone(self))
    }
    fn root_hash(&self) -> Option<TrieHash> {
        self.root_hash.clone()
    }
}

impl<S> NodeStore<Arc<ImmutableProposal>, S> {
    /// When an immutable proposal commits, we need to reparent any proposal that
    /// has the committed proposal as it's parent
    pub fn commit_reparent(&self, other: &Arc<NodeStore<Arc<ImmutableProposal>, S>>) -> bool {
        match *other.kind.parent.load() {
            NodeStoreParent::Proposed(ref parent) => {
                if Arc::ptr_eq(&self.kind, parent) {
                    other
                        .kind
                        .parent
                        .store(NodeStoreParent::Committed(self.kind.root_hash()).into());
                    true
                } else {
                    false
                }
            }
            NodeStoreParent::Committed(_) => false,
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
}

impl<S: ReadableStorage> NodeStore<MutableProposal, S> {
    /// Create a new MutableProposal [NodeStore] from a parent [NodeStore]
    pub fn new<F: Parentable + ReadInMemoryNode>(
        parent: Arc<NodeStore<F, S>>,
    ) -> Result<Self, Error> {
        let mut deleted: Vec<_> = Default::default();
        let root = if let Some(root_addr) = parent.header.root_address {
            deleted.push(root_addr);
            let root = parent.read_node(root_addr)?;
            Some((*root).clone())
        } else {
            None
        };
        let kind = MutableProposal {
            root,
            deleted,
            parent: parent.kind.as_nodestore_parent(),
        };
        Ok(NodeStore {
            header: parent.header.clone(),
            kind,
            storage: parent.storage.clone(),
        })
    }

    /// Marks the node at `addr` as deleted in this proposal.
    pub fn delete_node(&mut self, addr: LinearAddress) {
        trace!("Pending delete at {addr:?}");
        self.kind.deleted.push(addr);
    }

    /// Reads a node for update, marking it as deleted in this proposal.
    /// We get an arc from cache (reading it from disk if necessary) then
    /// copy/clone the node and return it.
    pub fn read_for_update(&mut self, addr: LinearAddress) -> Result<Node, Error> {
        self.delete_node(addr);
        let arc_wrapped_node = self.read_node(addr)?;
        Ok((*arc_wrapped_node).clone())
    }

    /// Returns the root of this proposal.
    pub fn mut_root(&mut self) -> &mut Option<Node> {
        &mut self.kind.root
    }
}

impl<S: WritableStorage> NodeStore<ImmutableProposal, S> {
    // TODO danlaine: Use this code in the revision management code.
    // TODO danlaine: Write only the parts of the header that have changed instead of the whole thing
    // fn write_header(&mut self) -> Result<(), Error> {
    //     let header_bytes = bincode::serialize(&self.header).map_err(|e| {
    //         Error::new(
    //             ErrorKind::InvalidData,
    //             format!("Failed to serialize free lists: {}", e),
    //         )
    //     })?;

    //     self.storage.write(0, header_bytes.as_slice())?;

    //     Ok(())
    // }
}

impl<S: WritableStorage> NodeStore<MutableProposal, S> {
    /// Creates a new, empty, [NodeStore] and clobbers the underlying `storage` with an empty header.
    pub fn new_empty_proposal(storage: Arc<S>) -> Self {
        let header = NodeStoreHeader::new();
        let header_bytes = bincode::serialize(&header).expect("failed to serialize header");
        storage
            .write(0, header_bytes.as_slice())
            .expect("failed to write header");
        NodeStore {
            header,
            kind: MutableProposal {
                root: None,
                deleted: Default::default(),
                parent: NodeStoreParent::Committed(None),
            },
            storage,
        }
    }
}

impl<S: ReadableStorage> NodeStore<Arc<ImmutableProposal>, S> {
    /// Attempts to allocate `n` bytes from the free lists.
    /// If successful returns the address of the newly allocated area
    /// and the index of the free list that was used.
    /// If there are no free areas big enough for `n` bytes, returns None.
    /// TODO danlaine: If we return a larger area than requested, we should split it.
    fn allocate_from_freed(&mut self, n: u64) -> Result<Option<(LinearAddress, AreaIndex)>, Error> {
        // Find the smallest free list that can fit this size.
        let index = area_size_to_index(n)?;

        // rustify: rewrite using self.header.free_lists.iter_mut().find(...)
        for index in index as usize..NUM_AREA_SIZES {
            // Get the first free block of sufficient size.
            if let Some(free_stored_area_addr) = self.header.free_lists[index] {
                // Update the free list head.
                // Skip the index byte and Area discriminant byte
                if let Some(free_head) = self.storage.free_list_cache(free_stored_area_addr) {
                    self.header.free_lists[index] = free_head;
                } else {
                    let free_area_addr = free_stored_area_addr.get() + 2;
                    let free_head_stream = self.storage.stream_from(free_area_addr)?;
                    let free_head: FreeArea = bincode::deserialize_from(free_head_stream)
                        .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

                    // Update the free list to point to the next free block.
                    self.header.free_lists[index] = free_head.next_free_block;
                }

                // Return the address of the newly allocated block.
                trace!(
                    "Allocating from free list: addr: {free_stored_area_addr:?}, size: {}",
                    AREA_SIZES[index]
                );
                return Ok(Some((free_stored_area_addr, index as AreaIndex)));
            }
            // No free blocks in this list, try the next size up.
        }

        trace!("No free blocks of sufficient size {index} found");
        Ok(None)
    }

    fn allocate_from_end(&mut self, n: u64) -> Result<(LinearAddress, AreaIndex), Error> {
        let index = area_size_to_index(n)?;
        let area_size = AREA_SIZES[index as usize];
        let addr = LinearAddress::new(self.header.size).expect("node store size can't be 0");
        self.header.size += area_size;
        debug_assert!(addr.get() % 8 == 0);
        trace!("Allocating from end: addr: {:?}, size: {}", addr, area_size);
        Ok((addr, index))
    }

    /// Returns the length of the serialized area for a node.
    fn stored_len(node: &Node) -> u64 {
        // TODO: calculate length without serializing!
        let area: Area<&Node, FreeArea> = Area::Node(node);
        let area_bytes = bincode::serialize(&area)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))
            .expect("fixme");

        // +1 for the size index byte
        // TODO: do a better job packing the boolean (freed) with the possible node sizes
        // A reasonable option is use a i8 and negative values indicate it's freed whereas positive values are non-free
        // This would still allow for 127 different sizes
        area_bytes.len() as u64 + 1
    }

    /// Returns an address that can be used to store the given `node` and updates
    /// `self.header` to reflect the allocation. Doesn't actually write the node to storage.
    /// Also returns the index of the free list the node was allocated from.
    pub fn allocate_node(&mut self, node: &Node) -> Result<(LinearAddress, AreaIndex), Error> {
        let stored_area_size = Self::stored_len(node);

        // Attempt to allocate from a free list.
        // If we can't allocate from a free list, allocate past the existing
        // of the ReadableStorage.
        let (addr, index) = match self.allocate_from_freed(stored_area_size)? {
            Some((addr, index)) => (addr, index),
            None => self.allocate_from_end(stored_area_size)?,
        };

        Ok((addr, index))
    }
}

impl<S: WritableStorage> NodeStore<Committed, S> {
    /// Deletes the [Node] at the given address, updating the next pointer at
    /// the given addr, and changing the header of this committed nodestore to
    /// have the address on the freelist
    pub fn delete_node(&mut self, addr: LinearAddress) -> Result<(), Error> {
        debug_assert!(addr.get() % 8 == 0);

        let (area_size_index, _) = self.area_index_and_size(addr)?;
        trace!(
            "Deleting node at {addr:?} of size {}",
            AREA_SIZES[area_size_index as usize]
        );

        // The area that contained the node is now free.
        let area: Area<Node, FreeArea> = Area::Free(FreeArea {
            next_free_block: self.header.free_lists[area_size_index as usize],
        });

        let stored_area = StoredArea {
            area_size_index,
            area,
        };

        let stored_area_bytes =
            bincode::serialize(&stored_area).map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

        self.storage.write(addr.into(), &stored_area_bytes)?;

        self.storage
            .add_to_free_list_cache(addr, self.header.free_lists[area_size_index as usize]);

        // The newly freed block is now the head of the free list.
        self.header.free_lists[area_size_index as usize] = Some(addr);

        Ok(())
    }
}

/// An error from doing an update
#[derive(Debug)]
pub enum UpdateError {
    /// An IO error occurred during the update
    Io(Error),
}

impl From<Error> for UpdateError {
    fn from(value: Error) -> Self {
        UpdateError::Io(value)
    }
}

/// Can be used by filesystem tooling such as "file" to identify
/// the version of firewood used to create this [NodeStore] file.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize)]
struct Version {
    bytes: [u8; 16],
}

impl Version {
    const SIZE: u64 = std::mem::size_of::<Self>() as u64;

    /// construct a [Version] header from the firewood version
    fn new() -> Self {
        let mut version_bytes: [u8; Self::SIZE as usize] = Default::default();
        let version = env!("CARGO_PKG_VERSION");
        let _ = version_bytes
            .as_mut_slice()
            .write_all(format!("firewood {}", version).as_bytes());
        Self {
            bytes: version_bytes,
        }
    }
}

pub type FreeLists = [Option<LinearAddress>; NUM_AREA_SIZES];

/// Persisted metadata for a [NodeStore].
/// The [NodeStoreHeader] is at the start of the ReadableStorage.
#[derive(Debug, PartialEq, Eq, Serialize, Deserialize, Clone)]
struct NodeStoreHeader {
    /// Identifies the version of firewood used to create this [NodeStore].
    version: Version,
    size: u64,
    /// Element i is the pointer to the first free block of size `BLOCK_SIZES[i]`.
    free_lists: FreeLists,
    root_address: Option<LinearAddress>,
}

impl NodeStoreHeader {
    /// The first SIZE bytes of the ReadableStorage are the [NodeStoreHeader].
    /// The serialized NodeStoreHeader may be less than SIZE bytes but we
    /// reserve this much space for it since it can grow and it must always be
    /// at the start of the ReadableStorage so it can't be moved in a resize.
    const SIZE: u64 = {
        // 8 and 9 for `size` and `root_address` respectively
        let max_size = Version::SIZE + 8 + 9 + FREE_LIST_MAX_SIZE;
        // Round up to the nearest multiple of MIN_AREA_SIZE
        let remainder = max_size % MIN_AREA_SIZE;
        if remainder == 0 {
            max_size
        } else {
            max_size + MIN_AREA_SIZE - remainder
        }
    };

    fn new() -> Self {
        Self {
            // The store just contains the header at this point
            size: Self::SIZE,
            root_address: None,
            version: Version::new(),
            free_lists: Default::default(),
        }
    }
}

/// A [FreeArea] is stored at the start of the area that contained a node that
/// has been freed.
#[derive(Debug, PartialEq, Eq, Clone, Copy, Serialize, Deserialize)]
struct FreeArea {
    next_free_block: Option<LinearAddress>,
}

/// Reads from an immutable (i.e. already hashed) merkle trie.
pub trait HashedNodeReader: TrieReader {
    /// Gets the address and hash of the root node of an immutable merkle trie.
    fn root_address_and_hash(&self) -> Result<Option<(LinearAddress, TrieHash)>, Error>;

    /// Gets the hash of the root node of an immutable merkle trie.
    fn root_hash(&self) -> Result<Option<TrieHash>, Error> {
        Ok(self.root_address_and_hash()?.map(|(_, hash)| hash))
    }
}

/// Reads nodes and the root address from a merkle trie.
pub trait TrieReader: NodeReader + RootReader {}
impl<T> TrieReader for T where T: NodeReader + RootReader {}

/// Reads nodes from a merkle trie.
pub trait NodeReader {
    /// Returns the node at `addr`.
    fn read_node(&self, addr: LinearAddress) -> Result<Arc<Node>, Error>;
}

impl<T> NodeReader for T
where
    T: Deref,
    T::Target: NodeReader,
{
    fn read_node(&self, addr: LinearAddress) -> Result<Arc<Node>, Error> {
        self.deref().read_node(addr)
    }
}

impl<T> RootReader for T
where
    T: Deref,
    T::Target: RootReader,
{
    fn root_node(&self) -> Option<Arc<Node>> {
        self.deref().root_node()
    }
}

/// Reads the root of a merkle trie.
pub trait RootReader {
    /// Returns the root of the trie.
    fn root_node(&self) -> Option<Arc<Node>>;
}

/// A committed revision of a merkle trie.
#[derive(Clone, Debug)]
pub struct Committed {
    #[allow(dead_code)]
    deleted: Box<[LinearAddress]>,
    root_hash: Option<TrieHash>,
}

impl ReadInMemoryNode for Committed {
    // A committed revision has no in-memory changes. All its nodes are in storage.
    fn read_in_memory_node(&self, _addr: LinearAddress) -> Option<Arc<Node>> {
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

#[derive(Clone, Debug)]
/// Contains state for a proposed revision of the trie.
pub struct ImmutableProposal {
    /// Address --> Node for nodes created in this proposal.
    new: HashMap<LinearAddress, (u8, Arc<Node>)>,
    /// Nodes that have been deleted in this proposal.
    deleted: Box<[LinearAddress]>,
    /// The parent of this proposal.
    parent: Arc<ArcSwap<NodeStoreParent>>,
    /// The hash of the root node for this proposal
    root_hash: Option<TrieHash>,
}

impl ImmutableProposal {
    /// Returns true if the parent of this proposal is committed and has the given hash.
    pub fn parent_hash_is(&self, hash: Option<TrieHash>) -> bool {
        match <Arc<ArcSwap<NodeStoreParent>> as arc_swap::access::DynAccess<Arc<_>>>::load(
            &self.parent,
        )
        .as_ref()
        {
            NodeStoreParent::Committed(root_hash) => *root_hash == hash,
            _ => false,
        }
    }
}

impl ReadInMemoryNode for ImmutableProposal {
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<Arc<Node>> {
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

/// Proposed [NodeStore] types keep some nodes in memory. These nodes are new nodes that were allocated from
/// the free list, but are not yet on disk. This trait checks to see if a node is in memory and returns it if
/// it's there. If it's not there, it will be read from disk.
///
/// This trait does not know anything about the underlying storage, so it just returns None if the node isn't in memory.
pub trait ReadInMemoryNode {
    /// Returns the node at `addr` if it is in memory.
    /// Returns None if it isn't.
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<Arc<Node>>;
}

impl<T> ReadInMemoryNode for T
where
    T: Deref,
    T::Target: ReadInMemoryNode,
{
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<Arc<Node>> {
        self.deref().read_in_memory_node(addr)
    }
}

/// Contains the state of a revision of a merkle trie.
///
/// The first generic parameter is the type of the revision, which supports reading nodes from parent proposals.
/// The second generic parameter is the type of the storage used, either
/// in-memory or on-disk.
///
/// The lifecycle of a [NodeStore] is as follows:
/// 1. Create a new, empty, [Committed] [NodeStore] using [NodeStore::new_empty_committed].
/// 2. Create a [NodeStore] from disk using [NodeStore::open].
/// 3. Create a new mutable proposal from either a [Committed] or [ImmutableProposal] [NodeStore] using [NodeStore::new].
/// 4. Convert a mutable proposal to an immutable proposal using [std::convert::TryInto], which hashes the nodes and assigns addresses
/// 5. Convert an immutable proposal to a committed revision using [std::convert::TryInto], which writes the nodes to disk.

#[derive(Debug)]
pub struct NodeStore<T, S> {
    // Metadata for this revision.
    header: NodeStoreHeader,
    /// This is one of [Committed], [ImmutableProposal], or [MutableProposal].
    pub kind: T,
    /// Persisted storage to read nodes from.
    pub storage: Arc<S>,
}

/// Contains the state of a proposal that is still being modified.
#[derive(Debug)]
pub struct MutableProposal {
    /// The root of the trie in this proposal.
    root: Option<Node>,
    /// Nodes that have been deleted in this proposal.
    deleted: Vec<LinearAddress>,
    parent: NodeStoreParent,
}

impl ReadInMemoryNode for NodeStoreParent {
    /// Returns the node at `addr` if it is in memory from a parent proposal.
    /// If the base revision is committed, there are no in-memory nodes, so we return None
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<Arc<Node>> {
        match self {
            NodeStoreParent::Proposed(proposed) => proposed.read_in_memory_node(addr),
            NodeStoreParent::Committed(_) => None,
        }
    }
}

impl ReadInMemoryNode for MutableProposal {
    /// [MutableProposal] types do not have any nodes in memory, but their parent proposal might, so we check there.
    /// This might be recursive: a grandparent might also have that node in memory.
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<Arc<Node>> {
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
                deleted: Default::default(),
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
            },
            storage: val.storage,
        }
    }
}

impl<S: ReadableStorage> NodeStore<Arc<ImmutableProposal>, S> {
    /// Hashes `node`, which is at the given `path_prefix`, and its children recursively.
    /// Returns the hashed node and its hash.
    fn hash_helper(
        &mut self,
        mut node: Node,
        path_prefix: &mut Path,
        new_nodes: &mut HashMap<LinearAddress, (u8, Arc<Node>)>,
    ) -> (LinearAddress, TrieHash) {
        // Allocate addresses and calculate hashes for all new nodes
        match node {
            Node::Branch(ref mut b) => {
                for (nibble, child) in b.children.iter_mut().enumerate() {
                    // if this is already hashed, we're done
                    if matches!(child, Some(Child::AddressWithHash(_, _))) {
                        // We already know the hash of this child.
                        continue;
                    }

                    // If this child is a node, hash it and update the child.
                    let Some(Child::Node(child_node)) = std::mem::take(child) else {
                        continue;
                    };

                    // Hash this child and update
                    // we extend and truncate path_prefix to reduce memory allocations
                    let original_length = path_prefix.len();
                    path_prefix
                        .0
                        .extend(b.partial_path.0.iter().copied().chain(once(nibble as u8)));

                    let (child_addr, child_hash) =
                        self.hash_helper(child_node, path_prefix, new_nodes);
                    *child = Some(Child::AddressWithHash(child_addr, child_hash));
                    path_prefix.0.truncate(original_length);
                }
            }
            Node::Leaf(_) => {}
        }

        let hash = hash_node(&node, path_prefix);
        let (addr, size) = self.allocate_node(&node).expect("TODO handle error");

        new_nodes.insert(addr, (size, Arc::new(node)));

        (addr, hash)
    }
}

impl<T, S: WritableStorage> NodeStore<T, S> {
    /// Persist the header from this proposal to storage.
    pub fn flush_header(&self) -> Result<(), Error> {
        let header_bytes = bincode::serialize(&self.header).map_err(|e| {
            Error::new(
                ErrorKind::InvalidData,
                format!("Failed to serialize header: {}", e),
            )
        })?;

        self.storage.write(0, header_bytes.as_slice())?;

        Ok(())
    }
}

impl<S: WritableStorage> NodeStore<ImmutableProposal, S> {
    /// Persist the freelist from this proposal to storage.
    pub fn flush_freelist(&self) -> Result<(), Error> {
        // Write the free lists to storage
        let free_list_bytes = bincode::serialize(&self.header.free_lists)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;
        let free_list_offset = offset_of!(NodeStoreHeader, free_lists) as u64;
        self.storage
            .write(free_list_offset, free_list_bytes.as_slice())?;
        Ok(())
    }

    /// Persist all the nodes of a proposal to storage.
    pub fn flush_nodes(&self) -> Result<(), Error> {
        for (addr, (area_size_index, node)) in self.kind.new.iter() {
            let stored_area = StoredArea {
                area_size_index: *area_size_index,
                area: Area::<_, FreeArea>::Node(node.as_ref()),
            };

            let stored_area_bytes = bincode::serialize(&stored_area)
                .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

            self.storage
                .write(addr.get(), stored_area_bytes.as_slice())?;
        }
        self.storage
            .write_cached_nodes(self.kind.new.iter().map(|(addr, (_, node))| (addr, node)))?;

        Ok(())
    }
}

impl NodeStore<ImmutableProposal, FileBacked> {
    /// Return a Committed version of this proposal, which doesn't have any modified nodes.
    /// This function is used during commit.
    pub fn as_committed(&self) -> NodeStore<Committed, FileBacked> {
        NodeStore {
            header: self.header.clone(),
            kind: Committed {
                deleted: self.kind.deleted.clone(),
                root_hash: self.kind.root_hash.clone(),
            },
            storage: self.storage.clone(),
        }
    }
}

impl<S: WritableStorage> NodeStore<Arc<ImmutableProposal>, S> {
    /// Persist the freelist from this proposal to storage.
    pub fn flush_freelist(&self) -> Result<(), Error> {
        // Write the free lists to storage
        let free_list_bytes = bincode::serialize(&self.header.free_lists)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;
        let free_list_offset = offset_of!(NodeStoreHeader, free_lists) as u64;
        self.storage
            .write(free_list_offset, free_list_bytes.as_slice())?;
        Ok(())
    }

    /// Persist all the nodes of a proposal to storage.
    pub fn flush_nodes(&self) -> Result<(), Error> {
        for (addr, (area_size_index, node)) in self.kind.new.iter() {
            let stored_area = StoredArea {
                area_size_index: *area_size_index,
                area: Area::<_, FreeArea>::Node(node.as_ref()),
            };

            let stored_area_bytes = bincode::serialize(&stored_area)
                .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

            self.storage
                .write(addr.get(), stored_area_bytes.as_slice())?;
        }
        self.storage
            .write_cached_nodes(self.kind.new.iter().map(|(addr, (_, node))| (addr, node)))?;

        Ok(())
    }
}

impl NodeStore<Arc<ImmutableProposal>, FileBacked> {
    /// Return a Committed version of this proposal, which doesn't have any modified nodes.
    /// This function is used during commit.
    pub fn as_committed(&self) -> NodeStore<Committed, FileBacked> {
        NodeStore {
            header: self.header.clone(),
            kind: Committed {
                deleted: self.kind.deleted.clone(),
                root_hash: self.kind.root_hash.clone(),
            },
            storage: self.storage.clone(),
        }
    }
}

impl<S: ReadableStorage> From<NodeStore<MutableProposal, S>>
    for NodeStore<Arc<ImmutableProposal>, S>
{
    fn from(val: NodeStore<MutableProposal, S>) -> Self {
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
            }),
            storage,
        };

        let Some(root) = kind.root else {
            // This trie is now empty.
            nodestore.header.root_address = None;
            return nodestore;
        };

        // Hashes the trie and returns the address of the new root.
        let mut new_nodes = HashMap::new();
        let (root_addr, root_hash) = nodestore.hash_helper(root, &mut Path::new(), &mut new_nodes);

        nodestore.header.root_address = Some(root_addr);
        let immutable_proposal =
            Arc::into_inner(nodestore.kind).expect("no other references to the proposal");
        nodestore.kind = Arc::new(ImmutableProposal {
            new: new_nodes,
            deleted: immutable_proposal.deleted,
            parent: immutable_proposal.parent,
            root_hash: Some(root_hash),
        });

        nodestore
    }
}

impl<T: ReadInMemoryNode, S: ReadableStorage> NodeReader for NodeStore<T, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<Arc<Node>, Error> {
        if let Some(node) = self.kind.read_in_memory_node(addr) {
            return Ok(node);
        }

        self.read_node_from_disk(addr)
    }
}

impl<S: ReadableStorage> RootReader for NodeStore<MutableProposal, S> {
    fn root_node(&self) -> Option<Arc<Node>> {
        self.kind.root.as_ref().map(|node| Arc::new(node.clone()))
    }
}

trait Hashed {}
impl Hashed for Committed {}
impl Hashed for Arc<ImmutableProposal> {}

impl<T: ReadInMemoryNode + Hashed, S: ReadableStorage> RootReader for NodeStore<T, S> {
    fn root_node(&self) -> Option<Arc<Node>> {
        // TODO: If the read_node fails, we just say there is no root; this is incorrect
        self.header
            .root_address
            .and_then(|addr| self.read_node(addr).ok())
    }
}

impl<T, S> HashedNodeReader for NodeStore<T, S>
where
    NodeStore<T, S>: TrieReader,
    T: ReadInMemoryNode,
    S: ReadableStorage,
{
    fn root_address_and_hash(&self) -> Result<Option<(LinearAddress, TrieHash)>, Error> {
        if let Some(root_addr) = self.header.root_address {
            let root_node = self.read_node(root_addr)?;
            let root_hash = hash_node(&root_node, &Path::new());
            Ok(Some((root_addr, root_hash)))
        } else {
            Ok(None)
        }
    }
}

impl<T, S> HashedNodeReader for Arc<NodeStore<T, S>>
where
    NodeStore<T, S>: TrieReader,
    T: ReadInMemoryNode,
    S: ReadableStorage,
{
    fn root_address_and_hash(&self) -> Result<Option<(LinearAddress, TrieHash)>, Error> {
        if let Some(root_addr) = self.header.root_address {
            let root_node = self.read_node(root_addr)?;
            let root_hash = hash_node(&root_node, &Path::new());
            Ok(Some((root_addr, root_hash)))
        } else {
            Ok(None)
        }
    }
}

impl<S: WritableStorage> NodeStore<Committed, S> {
    /// adjust the freelist of this proposal to reflect the freed nodes in the oldest proposal
    pub fn reap_deleted(&mut self, oldest: &NodeStore<Committed, S>) -> Result<(), Error> {
        self.storage
            .invalidate_cached_nodes(oldest.kind.deleted.iter());
        trace!("There are {} nodes to reap", oldest.kind.deleted.len());
        for addr in oldest.kind.deleted.iter() {
            trace!("reap {addr}");
            self.delete_node(*addr)?;
        }
        Ok(())
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use crate::linear::memory::MemStore;
    use arc_swap::access::DynGuard;

    use super::*;

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

            if i < NUM_AREA_SIZES - 1 {
                // 1 more than top of range goes to next range
                assert_eq!(
                    area_size_to_index(area_size + 1).unwrap(),
                    (i + 1) as AreaIndex
                );
            }
        }

        for i in 0..=MIN_AREA_SIZE {
            assert_eq!(area_size_to_index(i).unwrap(), 0);
        }

        assert!(area_size_to_index(MAX_AREA_SIZE + 1).is_err());
    }

    #[test]
    fn test_reparent() {
        // create an empty base revision
        let memstore = MemStore::new(vec![]);
        let base = NodeStore::new_empty_committed(memstore.into())
            .unwrap()
            .into();

        // create an empty r1, check that it's parent is the empty committed version
        let r1 = NodeStore::new(base).unwrap();
        let r1: Arc<NodeStore<Arc<ImmutableProposal>, _>> = Arc::new(r1.into());
        let parent: DynGuard<Arc<NodeStoreParent>> = r1.kind.parent.load();
        assert!(matches!(**parent, NodeStoreParent::Committed(None)));

        // create an empty r2, check that it's parent is the proposed version r1
        let r2: NodeStore<MutableProposal, _> = NodeStore::new(r1.clone()).unwrap();
        let r2: Arc<NodeStore<Arc<ImmutableProposal>, _>> = Arc::new(r2.into());
        let parent: DynGuard<Arc<NodeStoreParent>> = r2.kind.parent.load();
        assert!(matches!(**parent, NodeStoreParent::Proposed(_)));

        // reparent r2
        r1.commit_reparent(&r2);

        // now check r2's parent, should match the hash of r1 (which is still None)
        let parent: DynGuard<Arc<NodeStoreParent>> = r2.kind.parent.load();
        if let NodeStoreParent::Committed(hash) = &**parent {
            assert_eq!(*hash, r1.root_hash().unwrap());
            assert_eq!(*hash, None);
        } else {
            panic!("expected committed parent");
        }
    }

    // TODO add new tests
    // #[test]
    // fn test_create() {
    //     let memstore = Arc::new(MemStore::new(vec![]));
    //     let mut node_store = NodeStore::new_empty_proposal(memstore);

    //     let leaf = Node::Leaf(LeafNode {
    //         partial_path: Path::from([0, 1, 2]),
    //         value: Box::new([3, 4, 5]),
    //     });

    //     let leaf_addr = node_store.create_node(leaf.clone()).unwrap();
    //     let got_leaf = node_store.kind.new.get(&leaf_addr).unwrap();
    //     assert_eq!(**got_leaf, leaf);
    //     let got_leaf = node_store.read_node(leaf_addr).unwrap();
    //     assert_eq!(*got_leaf, leaf);

    //     // The header should be unchanged in storage
    //     {
    //         let mut header_bytes = node_store.storage.stream_from(0).unwrap();
    //         let header: NodeStoreHeader = bincode::deserialize_from(&mut header_bytes).unwrap();
    //         assert_eq!(header.version, Version::new());
    //         let empty_free_lists: FreeLists = Default::default();
    //         assert_eq!(header.free_lists, empty_free_lists);
    //         assert_eq!(header.root_address, None);
    //     }

    //     // Leaf should go right after the header
    //     assert_eq!(leaf_addr.get(), NodeStoreHeader::SIZE);

    //     // Create another node
    //     let branch = Node::Branch(Box::new(BranchNode {
    //         partial_path: Path::from([6, 7, 8]),
    //         value: Some(vec![9, 10, 11].into_boxed_slice()),
    //         children: Default::default(),
    //     }));

    //     let old_size = node_store.header.size;
    //     let branch_addr = node_store.create_node(branch.clone()).unwrap();
    //     assert!(node_store.header.size > old_size);

    //     // branch should go after leaf
    //     assert!(branch_addr.get() > leaf_addr.get());

    //     // The header should be unchanged in storage
    //     {
    //         let mut header_bytes = node_store.storage.stream_from(0).unwrap();
    //         let header: NodeStoreHeader = bincode::deserialize_from(&mut header_bytes).unwrap();
    //         assert_eq!(header.version, Version::new());
    //         let empty_free_lists: FreeLists = Default::default();
    //         assert_eq!(header.free_lists, empty_free_lists);
    //         assert_eq!(header.root_address, None);
    //     }
    // }

    // #[test]
    // fn test_delete() {
    //     let memstore = Arc::new(MemStore::new(vec![]));
    //     let mut node_store = NodeStore::new_empty_proposal(memstore);

    //     // Create a leaf
    //     let leaf = Node::Leaf(LeafNode {
    //         partial_path: Path::new(),
    //         value: Box::new([1]),
    //     });
    //     let leaf_addr = node_store.create_node(leaf.clone()).unwrap();

    //     // Delete the node
    //     node_store.delete_node(leaf_addr).unwrap();
    //     assert!(node_store.kind.deleted.contains(&leaf_addr));

    //     // Create a new node with the same size
    //     let new_leaf_addr = node_store.create_node(leaf).unwrap();

    //     // The new node shouldn't be at the same address
    //     assert_ne!(new_leaf_addr, leaf_addr);
    // }

    #[test]
    fn test_node_store_new() {
        let memstore = MemStore::new(vec![]);
        let node_store = NodeStore::new_empty_proposal(memstore.into());

        // Check the empty header is written at the start of the ReadableStorage.
        let mut header_bytes = node_store.storage.stream_from(0).unwrap();
        let header: NodeStoreHeader = bincode::deserialize_from(&mut header_bytes).unwrap();
        assert_eq!(header.version, Version::new());
        let empty_free_list: FreeLists = Default::default();
        assert_eq!(header.free_lists, empty_free_list);
    }
}
