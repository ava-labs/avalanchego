// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::logger::trace;
use arc_swap::ArcSwap;
use arc_swap::access::DynAccess;
use bincode::{DefaultOptions, Options as _};
use bytemuck_derive::{AnyBitPattern, NoUninit};
use coarsetime::Instant;
use fastrace::local::LocalSpan;
use metrics::counter;
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
use std::mem::{offset_of, take};
use std::num::NonZeroU64;
use std::ops::Deref;
use std::sync::Arc;

use crate::hashednode::hash_node;
use crate::node::{ByteCounter, Node};
use crate::{
    CacheReadStrategy, Child, FileBacked, HashType, Path, ReadableStorage, SharedNode, TrieHash,
};

use super::linear::WritableStorage;

/// [NodeStore] divides the linear store into blocks of different sizes.
/// [AREA_SIZES] is every valid block size.
const AREA_SIZES: [u64; 23] = [
    16, // Min block size
    32,
    64,
    96,
    128,
    256,
    512,
    768,
    1024,
    1024 << 1,
    1024 << 2,
    1024 << 3,
    1024 << 4,
    1024 << 5,
    1024 << 6,
    1024 << 7,
    1024 << 8,
    1024 << 9,
    1024 << 10,
    1024 << 11,
    1024 << 12,
    1024 << 13,
    1024 << 14,
];

fn serializer() -> impl bincode::Options {
    DefaultOptions::new().with_varint_encoding()
}

// TODO: automate this, must stay in sync with above
fn index_name(index: AreaIndex) -> &'static str {
    match index {
        0 => "16",
        1 => "32",
        2 => "64",
        3 => "96",
        4 => "128",
        5 => "256",
        6 => "512",
        7 => "768",
        8 => "1024",
        9 => "2048",
        10 => "4096",
        11 => "8192",
        12 => "16384",
        13 => "32768",
        14 => "65536",
        15 => "131072",
        16 => "262144",
        17 => "524288",
        18 => "1048576",
        19 => "2097152",
        20 => "4194304",
        21 => "8388608",
        22 => "16777216",
        _ => "unknown",
    }
}

/// The type of an index into the [AREA_SIZES] array
/// This is not usize because we can store this as a single byte
pub type AreaIndex = u8;

// TODO danlaine: have type for index in AREA_SIZES
// Implement try_into() for it.
const NUM_AREA_SIZES: usize = AREA_SIZES.len();
const MIN_AREA_SIZE: u64 = AREA_SIZES[0];
const MAX_AREA_SIZE: u64 = AREA_SIZES[NUM_AREA_SIZES - 1];

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

    AREA_SIZES
        .iter()
        .position(|&size| size >= n)
        .map(|index| index as AreaIndex)
        .ok_or_else(|| {
            Error::new(
                ErrorKind::InvalidData,
                format!("Node size {} is too large", n),
            )
        })
}

/// Objects cannot be stored at the zero address, so a [LinearAddress] is guaranteed not
/// to be zero. This reserved zero can be used as a [None] value for some use cases. In particular,
/// branches can use `Option<LinearAddress>` which is the same size as a [LinearAddress]
pub type LinearAddress = NonZeroU64;

/// Each [StoredArea] contains an [Area] which is either a [Node] or a [FreeArea].
#[repr(u8)]
#[derive(PartialEq, Eq, Clone, Debug, Deserialize, Serialize)]
enum Area<T, U> {
    Node(T),
    Free(U) = 255, // this is magic: no node starts with a byte of 255
}

/// Every item stored in the [NodeStore]'s ReadableStorage  after the
/// [NodeStoreHeader] is a [StoredArea].
///
/// As an overview of what this looks like stored, we get something like this:
///  - Byte 0: The index of the area size
///  - Byte 1: 0x255 if free, otherwise the low-order bit indicates Branch or Leaf
///  - Bytes 2..n: The actual data
#[derive(PartialEq, Eq, Clone, Debug, Deserialize, Serialize)]
struct StoredArea<T> {
    /// Index in [AREA_SIZES] of this area's size
    area_size_index: AreaIndex,
    area: T,
}

impl<T: ReadInMemoryNode, S: ReadableStorage> NodeStore<T, S> {
    /// Returns (index, area_size) for the [StoredArea] at `addr`.
    /// `index` is the index of `area_size` in [AREA_SIZES].
    fn area_index_and_size(&self, addr: LinearAddress) -> Result<(AreaIndex, u64), Error> {
        let mut area_stream = self.storage.stream_from(addr.get())?;

        let index: AreaIndex = serializer()
            .deserialize_from(&mut area_stream)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

        let size = *AREA_SIZES.get(index as usize).ok_or(Error::new(
            ErrorKind::InvalidData,
            format!("Invalid area size index {}", index),
        ))?;

        Ok((index, size))
    }

    /// Read a [Node] from the provided [LinearAddress].
    /// `addr` is the address of a StoredArea in the ReadableStorage.
    pub fn read_node_from_disk(
        &self,
        addr: LinearAddress,
        mode: &'static str,
    ) -> Result<SharedNode, Error> {
        if let Some(node) = self.storage.read_cached_node(addr, mode) {
            return Ok(node);
        }

        debug_assert!(addr.get() % 8 == 0);

        let actual_addr = addr.get() + 1; // skip the length byte

        let _span = LocalSpan::enter_with_local_parent("read_and_deserialize");

        let area_stream = self.storage.stream_from(actual_addr)?;
        let node: SharedNode = Node::from_reader(area_stream)?.into();
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
}

impl<S: ReadableStorage> NodeStore<Committed, S> {
    /// Open an existing [NodeStore]
    /// Assumes the header is written in the [ReadableStorage].
    pub fn open(storage: Arc<S>) -> Result<Self, Error> {
        let mut stream = storage.stream_from(0)?;
        let mut header = NodeStoreHeader::new();
        let header_bytes = bytemuck::bytes_of_mut(&mut header);
        stream.read_exact(header_bytes)?;

        drop(stream);

        if header.version != Version::new() {
            return Err(Error::new(
                ErrorKind::InvalidData,
                "Incompatible firewood version",
            ));
        }
        if header.endian_test != 1 {
            return Err(Error::new(
                ErrorKind::InvalidData,
                "Database cannot be opened due to difference in endianness",
            ));
        }

        let mut nodestore = Self {
            header,
            kind: Committed {
                deleted: Default::default(),
                root_hash: None,
            },
            storage,
        };

        if let Some(root_address) = nodestore.header.root_address {
            let node = nodestore.read_node_from_disk(root_address, "open");
            let root_hash = node.map(|n| hash_node(&n, &Path(Default::default())))?;
            nodestore.kind.root_hash = Some(root_hash.into_triehash());
        }

        Ok(nodestore)
    }

    /// Create a new, empty, Committed [NodeStore] and clobber
    /// the underlying store with an empty freelist and no root node
    pub fn new_empty_committed(storage: Arc<S>) -> Result<Self, Error> {
        let header = NodeStoreHeader::new();

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
            header: parent.header,
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

impl<S: WritableStorage> NodeStore<MutableProposal, S> {
    /// Creates a new, empty, [NodeStore] and clobbers the underlying `storage` with an empty header.
    /// This is used during testing and during the creation of an in-memory merkle for proofs
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
        let index_wanted = area_size_to_index(n)?;

        if let Some((index, free_stored_area_addr)) = self
            .header
            .free_lists
            .iter_mut()
            .enumerate()
            .skip(index_wanted as usize)
            .find(|item| item.1.is_some())
        {
            let address = free_stored_area_addr
                .take()
                .expect("impossible due to find earlier");
            // Get the first free block of sufficient size.
            if let Some(free_head) = self.storage.free_list_cache(address) {
                trace!("free_head@{address}(cached): {free_head:?} size:{index}");
                *free_stored_area_addr = free_head;
            } else {
                let free_area_addr = address.get();
                let free_head_stream = self.storage.stream_from(free_area_addr)?;
                let free_head: StoredArea<Area<Node, FreeArea>> = serializer()
                    .deserialize_from(free_head_stream)
                    .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;
                let StoredArea {
                    area: Area::Free(free_head),
                    area_size_index: read_index,
                } = free_head
                else {
                    return Err(Error::new(
                        ErrorKind::InvalidData,
                        "Attempted to read a non-free area",
                    ));
                };
                debug_assert_eq!(read_index as usize, index);

                // Update the free list to point to the next free block.
                *free_stored_area_addr = free_head.next_free_block;
            }

            counter!("firewood.space.reused", "index" => index_name(index as u8))
                .increment(AREA_SIZES[index]);
            counter!("firewood.space.wasted", "index" => index_name(index as u8))
                .increment(AREA_SIZES[index] - n);

            // Return the address of the newly allocated block.
            trace!(
                "Allocating from free list: addr: {address:?}, size: {}",
                index
            );
            return Ok(Some((address, index as AreaIndex)));
        }

        trace!("No free blocks of sufficient size {index_wanted} found");
        counter!("firewood.space.from_end", "index" => index_name(index_wanted as u8))
            .increment(AREA_SIZES[index_wanted as usize]);
        Ok(None)
    }

    fn allocate_from_end(&mut self, n: u64) -> Result<(LinearAddress, AreaIndex), Error> {
        let index = area_size_to_index(n)?;
        let area_size = AREA_SIZES[index as usize];
        let addr = LinearAddress::new(self.header.size).expect("node store size can't be 0");
        self.header.size += area_size;
        debug_assert!(addr.get() % 8 == 0);
        trace!("Allocating from end: addr: {:?}, size: {}", addr, index);
        Ok((addr, index))
    }

    /// Returns the length of the serialized area for a node.
    fn stored_len(node: &Node) -> u64 {
        let mut bytecounter = ByteCounter::new();
        node.as_bytes(0, &mut bytecounter);
        bytecounter.count()
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
        trace!("Deleting node at {addr:?} of size {}", area_size_index);
        counter!("firewood.delete_node", "index" => index_name(area_size_index)).increment(1);
        counter!("firewood.space.freed", "index" => index_name(area_size_index))
            .increment(AREA_SIZES[area_size_index as usize]);

        // The area that contained the node is now free.
        let area: Area<Node, FreeArea> = Area::Free(FreeArea {
            next_free_block: self.header.free_lists[area_size_index as usize],
        });

        let stored_area = StoredArea {
            area_size_index,
            area,
        };

        let stored_area_bytes = serializer()
            .serialize(&stored_area)
            .map_err(|e| Error::new(ErrorKind::InvalidData, e))?;

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
#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize, NoUninit, AnyBitPattern)]
#[repr(transparent)]
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
#[derive(Copy, Debug, PartialEq, Eq, Serialize, Deserialize, Clone, NoUninit, AnyBitPattern)]
#[repr(C)]
struct NodeStoreHeader {
    /// Identifies the version of firewood used to create this [NodeStore].
    version: Version,
    /// always "1"; verifies endianness
    endian_test: u64,
    size: u64,
    /// Element i is the pointer to the first free block of size `BLOCK_SIZES[i]`.
    free_lists: FreeLists,
    root_address: Option<LinearAddress>,
}

impl NodeStoreHeader {
    /// The first SIZE bytes of the ReadableStorage are reserved for the
    /// [NodeStoreHeader].
    /// We also want it aligned to a disk block
    const SIZE: u64 = 2048;

    /// Number of extra bytes to write on the first creation of the NodeStoreHeader
    /// (zero-padded)
    /// also a compile time check to prevent setting SIZE too small
    const EXTRA_BYTES: usize = Self::SIZE as usize - std::mem::size_of::<NodeStoreHeader>();

    fn new() -> Self {
        Self {
            // The store just contains the header at this point
            size: Self::SIZE,
            endian_test: 1,
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
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, Error>;
}

impl<T> NodeReader for T
where
    T: Deref,
    T::Target: NodeReader,
{
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, Error> {
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
}

/// Reads the root of a merkle trie.
pub trait RootReader {
    /// Returns the root of the trie.
    fn root_node(&self) -> Option<SharedNode>;
}

/// A committed revision of a merkle trie.
#[derive(Clone, Debug)]
pub struct Committed {
    deleted: Box<[LinearAddress]>,
    root_hash: Option<TrieHash>,
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

/// Proposed [NodeStore] types keep some nodes in memory. These nodes are new nodes that were allocated from
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
    fn read_in_memory_node(&self, addr: LinearAddress) -> Option<SharedNode> {
        match self {
            NodeStoreParent::Proposed(proposed) => proposed.read_in_memory_node(addr),
            NodeStoreParent::Committed(_) => None,
        }
    }
}

impl ReadInMemoryNode for MutableProposal {
    /// [MutableProposal] types do not have any nodes in memory, but their parent proposal might, so we check there.
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
        new_nodes: &mut HashMap<LinearAddress, (u8, SharedNode)>,
        #[cfg(feature = "ethhash")] fake_root_extra_nibble: Option<u8>,
    ) -> Result<(LinearAddress, HashType), Error> {
        // If this is a branch, find all unhashed children and recursively call hash_helper on them.
        trace!("hashing {node:?} at {path_prefix:?}");
        if let Node::Branch(ref mut b) = node {
            // special case code for ethereum hashes at the account level
            #[cfg(feature = "ethhash")]
            let make_fake_root = if path_prefix.0.len() + b.partial_path.0.len() == 64 {
                // looks like we're at an account branch
                // tally up how many hashes we need to deal with
                let (unhashed, mut hashed) = b.children.iter_mut().enumerate().fold(
                    (Vec::new(), Vec::new()),
                    |(mut unhashed, mut hashed), (idx, child)| {
                        match child {
                            None => {}
                            Some(Child::AddressWithHash(a, h)) => hashed.push((idx, (a, h))),
                            Some(Child::Node(node)) => unhashed.push((idx, node)),
                        }
                        (unhashed, hashed)
                    },
                );
                trace!("hashed {hashed:?} unhashed {unhashed:?}");
                if hashed.len() == 1 {
                    // we were left with one hashed node that must be rehashed
                    let invalidated_node = hashed.first_mut().expect("hashed is not empty");
                    let mut hashable_node = self.read_node(*invalidated_node.1.0)?.deref().clone();
                    let original_length = path_prefix.len();
                    path_prefix.0.extend(b.partial_path.0.iter().copied());
                    if !unhashed.is_empty() {
                        path_prefix.0.push(invalidated_node.0 as u8);
                    } else {
                        hashable_node.update_partial_path(Path::from_nibbles_iterator(
                            std::iter::once(invalidated_node.0 as u8)
                                .chain(hashable_node.partial_path().0.iter().copied()),
                        ));
                    }
                    let hash = hash_node(&hashable_node, path_prefix);
                    path_prefix.0.truncate(original_length);
                    *invalidated_node.1.1 = hash;
                }
                // handle the single-child case for an account special below
                if hashed.is_empty() && unhashed.len() == 1 {
                    Some(unhashed.last().expect("only one").0 as u8)
                } else {
                    None
                }
            } else {
                // not a single child
                None
            };

            // branch children -- 1. 1 child, already hashed, 2. >1 child, already hashed,
            // 3. 1 hashed child, 1 unhashed child
            // 4. 0 hashed, 1 unhashed <-- handle child special
            // 5. 1 hashed, >0 unhashed <-- rehash case
            // everything already hashed

            for (nibble, child) in b.children.iter_mut().enumerate() {
                // if this is already hashed, we're done
                if matches!(child, Some(Child::AddressWithHash(_, _))) {
                    // We already know the hash of this child.
                    continue;
                }

                // If there was no child, we're done. Otherwise, remove the child from
                // the branch and hash it. This has the side effect of dropping the [Child::Node]
                // that was allocated. This is fine because we're about to replace it with a
                // [Child::AddressWithHash].
                let Some(Child::Node(child_node)) = std::mem::take(child) else {
                    continue;
                };

                // Hash this child and update
                // we extend and truncate path_prefix to reduce memory allocations
                let original_length = path_prefix.len();
                path_prefix.0.extend(b.partial_path.0.iter().copied());
                #[cfg(feature = "ethhash")]
                if make_fake_root.is_none() {
                    // we don't push the nibble there is only one unhashed child and
                    // we're on an account
                    path_prefix.0.push(nibble as u8);
                }
                #[cfg(not(feature = "ethhash"))]
                path_prefix.0.push(nibble as u8);

                #[cfg(feature = "ethhash")]
                let (child_addr, child_hash) =
                    self.hash_helper(child_node, path_prefix, new_nodes, make_fake_root)?;
                #[cfg(not(feature = "ethhash"))]
                let (child_addr, child_hash) =
                    self.hash_helper(child_node, path_prefix, new_nodes)?;

                *child = Some(Child::AddressWithHash(child_addr, child_hash));
                path_prefix.0.truncate(original_length);
            }
        }
        // At this point, we either have a leaf or a branch with all children hashed.
        // if the encoded child hash <32 bytes then we use that RLP

        #[cfg(feature = "ethhash")]
        // if we have a child that is the only child of an account branch, we will hash this child as if it
        // is a root node. This means we have to take the nibble from the parent and prefix it to the partial path
        let hash = if let Some(nibble) = fake_root_extra_nibble {
            let mut fake_root = node.clone();
            trace!("old node: {:?}", fake_root);
            fake_root.update_partial_path(Path::from_nibbles_iterator(
                std::iter::once(nibble).chain(fake_root.partial_path().0.iter().copied()),
            ));
            trace!("new node: {:?}", fake_root);
            hash_node(&fake_root, path_prefix)
        } else {
            hash_node(&node, path_prefix)
        };

        #[cfg(not(feature = "ethhash"))]
        let hash = hash_node(&node, path_prefix);

        let (addr, size) = self.allocate_node(&node)?;

        new_nodes.insert(addr, (size, node.into()));

        Ok((addr, hash))
    }
}

impl<T, S: WritableStorage> NodeStore<T, S> {
    /// Persist the header from this proposal to storage.
    pub fn flush_header(&self) -> Result<(), Error> {
        let header_bytes = bytemuck::bytes_of(&self.header);
        self.storage.write(0, header_bytes)?;
        Ok(())
    }

    /// Persist the header, including all the padding
    /// This is only done the first time we write the header
    pub fn flush_header_with_padding(&self) -> Result<(), Error> {
        let header_bytes = bytemuck::bytes_of(&self.header)
            .iter()
            .copied()
            .chain(std::iter::repeat_n(0u8, NodeStoreHeader::EXTRA_BYTES))
            .collect::<Box<[u8]>>();
        debug_assert_eq!(header_bytes.len(), NodeStoreHeader::SIZE as usize);

        self.storage.write(0, &header_bytes)?;
        Ok(())
    }
}

impl NodeStore<Arc<ImmutableProposal>, FileBacked> {
    /// Persist the freelist from this proposal to storage.
    #[fastrace::trace(short_name = true)]
    pub fn flush_freelist(&self) -> Result<(), Error> {
        // Write the free lists to storage
        let free_list_bytes = bytemuck::bytes_of(&self.header.free_lists);
        let free_list_offset = offset_of!(NodeStoreHeader, free_lists) as u64;
        self.storage.write(free_list_offset, free_list_bytes)?;
        Ok(())
    }

    /// Persist all the nodes of a proposal to storage.
    #[fastrace::trace(short_name = true)]
    #[cfg(not(feature = "io-uring"))]
    pub fn flush_nodes(&self) -> Result<(), Error> {
        let flush_start = Instant::now();

        for (addr, (area_size_index, node)) in self.kind.new.iter() {
            let mut stored_area_bytes = Vec::new();
            node.as_bytes(*area_size_index, &mut stored_area_bytes);
            self.storage
                .write(addr.get(), stored_area_bytes.as_slice())?;
        }

        self.storage
            .write_cached_nodes(self.kind.new.iter().map(|(addr, (_, node))| (addr, node)))?;

        let flush_time = flush_start.elapsed().as_millis();
        counter!("firewood.flush_nodes").increment(flush_time);

        Ok(())
    }

    /// Persist all the nodes of a proposal to storage.
    #[fastrace::trace(short_name = true)]
    #[cfg(feature = "io-uring")]
    pub fn flush_nodes(&self) -> Result<(), Error> {
        const RINGSIZE: usize = FileBacked::RINGSIZE as usize;

        let flush_start = Instant::now();

        let mut ring = self.storage.ring.lock().expect("poisoned lock");
        let mut saved_pinned_buffers = vec![(false, std::pin::Pin::new(Box::default())); RINGSIZE];
        for (&addr, &(area_size_index, ref node)) in self.kind.new.iter() {
            let mut serialized = Vec::with_capacity(100); // TODO: better size? we can guess branches are larger
            node.as_bytes(area_size_index, &mut serialized);
            let mut serialized = serialized.into_boxed_slice();
            loop {
                // Find the first available write buffer, enumerate to get the position for marking it completed
                if let Some((pos, (busy, found))) = saved_pinned_buffers
                    .iter_mut()
                    .enumerate()
                    .find(|(_, (busy, _))| !*busy)
                {
                    *found = std::pin::Pin::new(std::mem::take(&mut serialized));
                    let submission_queue_entry = self
                        .storage
                        .make_op(found)
                        .offset(addr.get())
                        .build()
                        .user_data(pos as u64);

                    *busy = true;
                    // SAFETY: the submission_queue_entry's found buffer must not move or go out of scope
                    // until the operation has been completed. This is ensured by marking the slot busy,
                    // and not marking it !busy until the kernel has said it's done below.
                    #[expect(unsafe_code)]
                    while unsafe { ring.submission().push(&submission_queue_entry) }.is_err() {
                        ring.submitter().squeue_wait()?;
                        trace!("submission queue is full");
                        counter!("ring.full").increment(1);
                    }
                    break;
                }
                // if we get here, that means we couldn't find a place to queue the request, so wait for at least one operation
                // to complete, then handle the completion queue
                counter!("ring.full").increment(1);
                ring.submit_and_wait(1)?;
                let completion_queue = ring.completion();
                trace!("competion queue length: {}", completion_queue.len());
                for entry in completion_queue {
                    let item = entry.user_data() as usize;
                    saved_pinned_buffers
                        .get_mut(item)
                        .expect("should be an index into the array")
                        .0 = false;
                }
            }
        }
        let pending = saved_pinned_buffers
            .iter()
            .filter(|(busy, _)| *busy)
            .count();
        ring.submit_and_wait(pending)?;

        for entry in ring.completion() {
            let item = entry.user_data() as usize;
            saved_pinned_buffers
                .get_mut(item)
                .expect("should be an index into the array")
                .0 = false;
        }

        debug_assert_eq!(saved_pinned_buffers.iter().find(|(busy, _)| *busy), None);

        self.storage
            .write_cached_nodes(self.kind.new.iter().map(|(addr, (_, node))| (addr, node)))?;
        debug_assert!(ring.completion().is_empty());

        let flush_time = flush_start.elapsed().as_millis();
        counter!("firewood.flush_nodes").increment(flush_time);

        Ok(())
    }
}

impl NodeStore<Arc<ImmutableProposal>, FileBacked> {
    /// Return a Committed version of this proposal, which doesn't have any modified nodes.
    /// This function is used during commit.
    pub fn as_committed(&self) -> NodeStore<Committed, FileBacked> {
        NodeStore {
            header: self.header,
            kind: Committed {
                deleted: self.kind.deleted.clone(),
                root_hash: self.kind.root_hash.clone(),
            },
            storage: self.storage.clone(),
        }
    }
}

impl<S: ReadableStorage> TryFrom<NodeStore<MutableProposal, S>>
    for NodeStore<Arc<ImmutableProposal>, S>
{
    type Error = std::io::Error;

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
            }),
            storage,
        };

        let Some(root) = kind.root else {
            // This trie is now empty.
            nodestore.header.root_address = None;
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

        nodestore.header.root_address = Some(root_addr);
        let immutable_proposal =
            Arc::into_inner(nodestore.kind).expect("no other references to the proposal");
        nodestore.kind = Arc::new(ImmutableProposal {
            new: new_nodes,
            deleted: immutable_proposal.deleted,
            parent: immutable_proposal.parent,
            root_hash: Some(root_hash.into_triehash()),
        });

        Ok(nodestore)
    }
}

impl<S: ReadableStorage> NodeReader for NodeStore<MutableProposal, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, Error> {
        if let Some(node) = self.kind.read_in_memory_node(addr) {
            return Ok(node);
        }

        self.read_node_from_disk(addr, "write")
    }
}

impl<T: Parentable + ReadInMemoryNode, S: ReadableStorage> NodeReader for NodeStore<T, S> {
    fn read_node(&self, addr: LinearAddress) -> Result<SharedNode, Error> {
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
}

impl<T: ReadInMemoryNode + Parentable, S: ReadableStorage> RootReader for NodeStore<T, S> {
    fn root_node(&self) -> Option<SharedNode> {
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
            Ok(Some((root_addr, root_hash.into_triehash())))
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
            Ok(Some((root_addr, root_hash.into_triehash())))
        } else {
            Ok(None)
        }
    }
}

impl<S: WritableStorage> NodeStore<Committed, S> {
    /// adjust the freelist of this proposal to reflect the freed nodes in the oldest proposal
    pub fn reap_deleted(mut self, proposal: &mut NodeStore<Committed, S>) -> Result<(), Error> {
        self.storage
            .invalidate_cached_nodes(self.kind.deleted.iter());
        trace!("There are {} nodes to reap", self.kind.deleted.len());
        for addr in take(&mut self.kind.deleted) {
            proposal.delete_node(addr)?;
        }
        Ok(())
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used)]
mod tests {
    use std::array::from_fn;

    use crate::linear::memory::MemStore;
    use crate::{BranchNode, LeafNode};
    use arc_swap::access::DynGuard;
    use test_case::test_case;

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
        let r1: Arc<NodeStore<Arc<ImmutableProposal>, _>> = Arc::new(r1.try_into().unwrap());
        let parent: DynGuard<Arc<NodeStoreParent>> = r1.kind.parent.load();
        assert!(matches!(**parent, NodeStoreParent::Committed(None)));

        // create an empty r2, check that it's parent is the proposed version r1
        let r2: NodeStore<MutableProposal, _> = NodeStore::new(r1.clone()).unwrap();
        let r2: Arc<NodeStore<Arc<ImmutableProposal>, _>> = Arc::new(r2.try_into().unwrap());
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

    #[test]
    fn test_node_store_new() {
        let memstore = MemStore::new(vec![]);
        let node_store = NodeStore::new_empty_proposal(memstore.into());

        // Check the empty header is written at the start of the ReadableStorage.
        let mut header = NodeStoreHeader::new();
        let mut header_stream = node_store.storage.stream_from(0).unwrap();
        let header_bytes = bytemuck::bytes_of_mut(&mut header);
        header_stream.read_exact(header_bytes).unwrap();
        assert_eq!(header.version, Version::new());
        let empty_free_list: FreeLists = Default::default();
        assert_eq!(header.free_lists, empty_free_list);
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
        println!("{:?}", immutable); // should not be reached, but need to consume immutable to avoid optimization removal
    }
}
