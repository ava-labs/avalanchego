// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::match_same_arms,
    reason = "Found 1 occurrences after enabling the lint."
)]

use crate::node::ExtendableBytes;
use crate::node::children::Children;
use crate::{
    FileIoError, HashType, LeafNode, LinearAddress, MaybePersistedNode, Node, NodeReader, Path,
    PathComponent, SharedNode,
};
use std::fmt::{Debug, Formatter};
use std::io::Read;

pub(crate) trait Serializable {
    fn write_to<W: ExtendableBytes>(&self, vec: &mut W);

    fn from_reader<R: Read>(reader: R) -> Result<Self, std::io::Error>
    where
        Self: Sized;
}

/// An extension trait for [`Read`] for convenience methods when
/// reading serialized data.
pub(crate) trait ReadSerializable: Read {
    /// Read a single byte from the reader.
    fn read_byte(&mut self) -> Result<u8, std::io::Error> {
        let mut this = 0;
        self.read_exact(std::slice::from_mut(&mut this))?;
        Ok(this)
    }

    /// Reads a fixed amount of bytes from the reader into a vector
    fn read_fixed_len(&mut self, len: usize) -> Result<Vec<u8>, std::io::Error> {
        let mut buf = Vec::with_capacity(len);
        self.take(len as u64).read_to_end(&mut buf)?;
        if buf.len() != len {
            return Err(std::io::Error::new(
                std::io::ErrorKind::UnexpectedEof,
                "not enough bytes read",
            ));
        }
        Ok(buf)
    }

    /// Read a value of type `T` from the reader.
    fn next_value<T: Serializable>(&mut self) -> Result<T, std::io::Error> {
        T::from_reader(self)
    }
}

impl<T: Read> ReadSerializable for T {}

#[derive(PartialEq, Eq, Clone, Debug)]
/// A child of a branch node.
pub enum Child {
    /// There is a child at this index, but we haven't hashed it
    /// or allocated space in storage for it yet.
    Node(Node),

    /// We know the child's persisted address and hash.
    AddressWithHash(LinearAddress, HashType),

    /// A `MaybePersisted` child
    MaybePersisted(MaybePersistedNode, HashType),
}

impl lru_mem::HeapSize for Child {
    fn heap_size(&self) -> usize {
        match self {
            Child::Node(node) => node.heap_size(),
            Child::AddressWithHash(_, _) => 0,
            // MaybePersisted contains Arc<Mutex>, we don't count shared data
            Child::MaybePersisted(_, _) => 0,
        }
    }
}

impl Child {
    /// Return a mutable reference to the underlying Node if the child
    /// is a [`Child::Node`] variant, otherwise None.
    #[must_use]
    pub const fn as_mut_node(&mut self) -> Option<&mut Node> {
        match self {
            Child::Node(node) => Some(node),
            _ => None,
        }
    }

    /// Return the persisted address of the child if it is a [`Child::AddressWithHash`] or [`Child::MaybePersisted`] variant, otherwise None.
    #[must_use]
    pub fn persisted_address(&self) -> Option<LinearAddress> {
        match self {
            Child::AddressWithHash(addr, _) => Some(*addr),
            Child::MaybePersisted(maybe_persisted, _) => maybe_persisted.as_linear_address(),
            Child::Node(_) => None,
        }
    }

    /// Return the unpersisted node if the child is an unpersisted [`Child::MaybePersisted`]
    /// variant, otherwise None.
    #[must_use]
    pub fn unpersisted(&self) -> Option<&MaybePersistedNode> {
        if let Child::MaybePersisted(maybe_persisted, _) = self {
            maybe_persisted.unpersisted()
        } else {
            None
        }
    }

    /// Return the hash of the child if it is a [`Child::AddressWithHash`] or [`Child::MaybePersisted`] variant, otherwise None.
    #[must_use]
    pub const fn hash(&self) -> Option<&HashType> {
        match self {
            Child::AddressWithHash(_, hash) => Some(hash),
            Child::MaybePersisted(_, hash) => Some(hash),
            Child::Node(_) => None,
        }
    }

    /// Return the persistence information (address and hash) of the child if it is persisted.
    ///
    /// This method returns `Some((address, hash))` for:
    /// - [`Child::AddressWithHash`] variants (already persisted)
    /// - [`Child::MaybePersisted`] variants that have been persisted
    ///
    /// Returns `None` for:
    /// - [`Child::Node`] variants (unpersisted nodes)
    /// - [`Child::MaybePersisted`] variants that are not yet persisted
    #[must_use]
    pub fn persist_info(&self) -> Option<(LinearAddress, &HashType)> {
        match self {
            Child::AddressWithHash(addr, hash) => Some((*addr, hash)),
            Child::MaybePersisted(maybe_persisted, hash) => {
                maybe_persisted.as_linear_address().map(|addr| (addr, hash))
            }
            Child::Node(_) => None,
        }
    }

    /// Return a `MaybePersistedNode` from a child
    ///
    /// This is used in the dump utility, but otherwise should be avoided,
    /// as it may create an unnecessary `MaybePersistedNode`
    #[must_use]
    pub fn as_maybe_persisted_node(&self) -> MaybePersistedNode {
        match self {
            Child::Node(node) => MaybePersistedNode::from(SharedNode::from(node.clone())),
            Child::AddressWithHash(addr, _) => MaybePersistedNode::from(*addr),
            Child::MaybePersisted(maybe_persisted, _) => maybe_persisted.clone(),
        }
    }

    /// Converts this `Child` to a `SharedNode` by reading from a `NodeReader`.
    ///
    /// # Arguments
    ///
    /// * `storage` - A reference to a `NodeReader` implementation that can read nodes from storage.
    ///
    /// # Returns
    ///
    /// Returns a `Result<SharedNode, FileIoError>` where:
    /// - `Ok(SharedNode)` contains the node if successfully read
    /// - `Err(FileIoError)` if there was an error reading from storage
    pub fn as_shared_node<S: NodeReader>(&self, storage: &S) -> Result<SharedNode, FileIoError> {
        match self {
            Child::Node(node) => Ok(node.clone().into()),
            Child::AddressWithHash(addr, _) => storage.read_node(*addr),
            Child::MaybePersisted(maybe_persisted, _) => maybe_persisted.as_shared_node(storage),
        }
    }
}

#[derive(PartialEq, Eq, Clone)]
/// A branch node
pub struct BranchNode {
    /// The partial path for this branch
    pub partial_path: Path,

    /// The value of the data for this branch, if any
    pub value: Option<Box<[u8]>>,

    /// The children of this branch.
    /// Element i is the child at index i, or None if there is no child at that index.
    /// Each element is (`child_hash`, `child_address`).
    /// `child_address` is None if we don't know the child's hash.
    pub children: Children<Option<Child>>,
}

impl lru_mem::HeapSize for BranchNode {
    fn heap_size(&self) -> usize {
        let value_size = self.value.as_ref().map_or(0, |v| v.len());
        let children_size: usize = self
            .children
            .iter()
            .filter_map(|(_, child)| child.as_ref())
            .map(lru_mem::HeapSize::heap_size)
            .sum();
        self.partial_path
            .heap_size()
            .wrapping_add(value_size)
            .wrapping_add(children_size)
    }
}

impl Debug for BranchNode {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "[BranchNode")?;
        write!(f, r#" path="{:?}""#, self.partial_path)?;

        for (i, c) in &self.children {
            match c {
                None => {}
                Some(Child::Node(_)) => {} //TODO
                Some(Child::AddressWithHash(addr, hash)) => {
                    write!(f, "({i:?}: address={addr:?} hash={hash})")?;
                }
                Some(Child::MaybePersisted(maybe_persisted, hash)) => {
                    // For MaybePersisted, show the address if persisted, otherwise show as unpersisted
                    match maybe_persisted.as_linear_address() {
                        Some(addr) => write!(f, "({i:?}: address={addr:?} hash={hash})")?,
                        None => write!(f, "({i:?}: unpersisted hash={hash})")?,
                    }
                }
            }
        }

        let value: &dyn std::fmt::Display = match self.value.as_deref() {
            Some(v) => &super::DisplayTruncatedHex(v),
            None => &"nil",
        };
        write!(f, "v={value}]")
    }
}

impl BranchNode {
    /// The maximum number of children a branch node can have.
    pub const MAX_CHILDREN: usize = PathComponent::LEN;

    /// Returns a set of persistence information (address and hash) for each child that
    /// is persisted.
    ///
    /// This will skip any child that is a [`Child::Node`] variant (not yet hashed)
    /// or a [`Child::MaybePersisted`] variant that does not have an address (not
    /// yet persisted).
    #[must_use]
    pub fn persist_info(&self) -> Children<Option<(LinearAddress, &HashType)>> {
        self.children
            .each_ref()
            .map(|_, c| c.as_ref().and_then(Child::persist_info))
    }

    /// Returns a set of hashes for each child that has a hash set.
    ///
    /// The index of the hash in the returned array corresponds to the index of the child
    /// in the branch node.
    ///
    /// Note: This function will skip any child is a [`Child::Node`] variant
    /// as it is still mutable and has not been hashed yet.
    ///
    /// This is an unintentional side effect of the current implementation. Future
    /// changes will have this check implemented structurally to prevent such cases.
    #[must_use]
    pub fn children_hashes(&self) -> Children<Option<HashType>> {
        self.children
            .each_ref()
            .map(|_, c| c.as_ref().and_then(Child::hash).cloned())
    }

    /// Returns a set of addresses for each child that has an address set.
    ///
    /// The index of the address in the returned array corresponds to the index of the child
    /// in the branch node.
    ///
    /// Note: This function will skip:
    ///   - Any child is a [`Child::Node`] variant as it does not have an address.
    ///   - Any child is a [`Child::MaybePersisted`] variant that is not yet
    ///     persisted, as we do not yet know its address.
    ///
    /// This is an unintentional side effect of the current implementation. Future
    /// changes will have this check implemented structurally to prevent such cases.
    #[must_use]
    pub fn children_addresses(&self) -> Children<Option<LinearAddress>> {
        self.children
            .each_ref()
            .map(|_, c| c.as_ref().and_then(Child::persisted_address))
    }
}

impl From<&LeafNode> for BranchNode {
    fn from(leaf: &LeafNode) -> Self {
        BranchNode {
            partial_path: leaf.partial_path.clone(),
            value: Some(Box::from(&leaf.value[..])),
            children: Children::new(),
        }
    }
}
