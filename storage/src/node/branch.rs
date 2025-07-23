// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::match_same_arms,
    reason = "Found 1 occurrences after enabling the lint."
)]
#![expect(
    clippy::missing_panics_doc,
    reason = "Found 2 occurrences after enabling the lint."
)]

use crate::node::ExtendableBytes;
use crate::{LeafNode, LinearAddress, MaybePersistedNode, Node, Path, SharedNode};
use std::fmt::{Debug, Formatter};
use std::io::Read;

/// The type of a hash. For ethereum compatible hashes, this might be a RLP encoded
/// value if it's small enough to fit in less than 32 bytes. For merkledb compatible
/// hashes, it's always a `TrieHash`.
#[cfg(feature = "ethhash")]
pub type HashType = ethhash::HashOrRlp;

#[cfg(not(feature = "ethhash"))]
/// The type of a hash. For non-ethereum compatible hashes, this is always a `TrieHash`.
pub type HashType = crate::TrieHash;

/// A trait to convert a value into a [`HashType`].
///
/// This is used to allow different hash types to be conditionally used, e.g., when the
/// `ethhash` feature is enabled. When not enabled, this suppresses the clippy warnings
/// about useless `.into()` calls.
pub trait IntoHashType {
    /// Converts the value into a `HashType`.
    #[must_use]
    fn into_hash_type(self) -> HashType;
}

#[cfg(feature = "ethhash")]
impl IntoHashType for crate::TrieHash {
    #[inline]
    fn into_hash_type(self) -> HashType {
        self.into()
    }
}

#[cfg(not(feature = "ethhash"))]
impl IntoHashType for crate::TrieHash {
    #[inline]
    fn into_hash_type(self) -> HashType {
        self
    }
}

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
#[repr(C)]
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
}

#[cfg(feature = "ethhash")]
mod ethhash {
    use sha2::Digest as _;
    use sha3::Keccak256;
    use smallvec::SmallVec;
    use std::{
        fmt::{Display, Formatter},
        io::Read,
    };

    use crate::TrieHash;
    use crate::node::ExtendableBytes;

    use super::Serializable;

    #[derive(Clone, Debug, PartialEq, Eq)]
    pub enum HashOrRlp {
        Hash(TrieHash),
        // TODO: this slice is never larger than 32 bytes so smallvec is probably not our best container
        // the length is stored in a `usize` but it could be in a `u8` and it will never overflow
        Rlp(SmallVec<[u8; 32]>),
    }

    impl HashOrRlp {
        pub fn as_slice(&self) -> &[u8] {
            self
        }

        pub(crate) fn into_triehash(self) -> TrieHash {
            self.into()
        }
    }

    impl Serializable for HashOrRlp {
        fn write_to<W: ExtendableBytes>(&self, vec: &mut W) {
            match self {
                HashOrRlp::Hash(h) => {
                    vec.push(0);
                    vec.extend_from_slice(h.as_ref());
                }
                HashOrRlp::Rlp(r) => {
                    debug_assert!(!r.is_empty());
                    debug_assert!(r.len() < 32);
                    vec.push(r.len() as u8);
                    vec.extend_from_slice(r.as_ref());
                }
            }
        }

        fn from_reader<R: Read>(mut reader: R) -> Result<Self, std::io::Error> {
            let mut bytes = [0; 32];
            reader.read_exact(&mut bytes[0..1])?;
            match bytes[0] {
                0 => {
                    reader.read_exact(&mut bytes)?;
                    Ok(HashOrRlp::Hash(TrieHash::from(bytes)))
                }
                len if len < 32 => {
                    reader.read_exact(&mut bytes[0..len as usize])?;
                    Ok(HashOrRlp::Rlp(SmallVec::from_buf_and_len(
                        bytes,
                        len as usize,
                    )))
                }
                _ => Err(std::io::Error::new(
                    std::io::ErrorKind::InvalidData,
                    "invalid RLP length",
                )),
            }
        }
    }

    impl From<HashOrRlp> for TrieHash {
        fn from(val: HashOrRlp) -> Self {
            match val {
                HashOrRlp::Hash(h) => h,
                HashOrRlp::Rlp(r) => Keccak256::digest(&r).into(),
            }
        }
    }

    impl From<TrieHash> for HashOrRlp {
        fn from(val: TrieHash) -> Self {
            HashOrRlp::Hash(val)
        }
    }

    impl From<[u8; 32]> for HashOrRlp {
        fn from(value: [u8; 32]) -> Self {
            HashOrRlp::Hash(TrieHash::into(value.into()))
        }
    }

    impl AsRef<[u8]> for HashOrRlp {
        fn as_ref(&self) -> &[u8] {
            match self {
                HashOrRlp::Hash(h) => h.as_ref(),
                HashOrRlp::Rlp(r) => r.as_ref(),
            }
        }
    }

    impl std::ops::Deref for HashOrRlp {
        type Target = [u8];
        fn deref(&self) -> &Self::Target {
            match self {
                HashOrRlp::Hash(h) => h,
                HashOrRlp::Rlp(r) => r,
            }
        }
    }

    impl Display for HashOrRlp {
        fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
            match self {
                HashOrRlp::Hash(h) => write!(f, "{h}"),
                HashOrRlp::Rlp(r) => {
                    let width = f.precision().unwrap_or(32);
                    write!(f, "{:.*}", width, hex::encode(r))
                }
            }
        }
    }

    impl Default for HashOrRlp {
        fn default() -> Self {
            HashOrRlp::Hash(TrieHash::default())
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
    pub children: [Option<Child>; Self::MAX_CHILDREN],
}

impl Debug for BranchNode {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "[BranchNode")?;
        write!(f, r#" path="{:?}""#, self.partial_path)?;

        for (i, c) in self.children.iter().enumerate() {
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

        write!(
            f,
            " v={}]",
            match &self.value {
                Some(v) => hex::encode(&**v),
                None => "nil".to_string(),
            }
        )
    }
}

impl BranchNode {
    /// The maximum number of children in a [`BranchNode`]
    #[cfg(feature = "branch_factor_256")]
    pub const MAX_CHILDREN: usize = 256;

    /// The maximum number of children in a [`BranchNode`]
    #[cfg(not(feature = "branch_factor_256"))]
    pub const MAX_CHILDREN: usize = 16;

    /// Returns the address of the child at the given index.
    /// Panics if `child_index` >= [`BranchNode::MAX_CHILDREN`].
    #[must_use]
    pub fn child(&self, child_index: u8) -> &Option<Child> {
        self.children
            .get(child_index as usize)
            .expect("child_index is in bounds")
    }

    /// Update the child at `child_index` to be `new_child_addr`.
    /// If `new_child_addr` is None, the child is removed.
    pub fn update_child(&mut self, child_index: u8, new_child: Option<Child>) {
        let child = self
            .children
            .get_mut(child_index as usize)
            .expect("child_index is in bounds");

        *child = new_child;
    }

    // Helper to iterate over only valid children
    pub(crate) fn children_iter(
        &self,
    ) -> impl Iterator<Item = (usize, (LinearAddress, &HashType))> + Clone {
        self.children
            .iter()
            .enumerate()
            .filter_map(|(i, child)| match child {
                None => None,
                Some(Child::Node(_)) => unreachable!("TODO make unreachable"),
                Some(Child::AddressWithHash(address, hash)) => Some((i, (*address, hash))),
                Some(Child::MaybePersisted(maybe_persisted, hash)) => {
                    // For MaybePersisted, we need the address if it's persisted
                    maybe_persisted
                        .as_linear_address()
                        .map(|addr| (i, (addr, hash)))
                }
            })
    }

    /// Returns (index, hash) for each child that has a hash set.
    pub fn children_hashes(&self) -> impl Iterator<Item = (usize, HashType)> + Clone {
        self.children.iter().enumerate().filter_map(|(i, child)| {
            child
                .as_ref()
                .and_then(|child| child.hash().cloned())
                .map(|hash| (i, hash))
        })
    }

    /// Returns (index, address) for each child that has a hash set.
    pub fn children_addresses(&self) -> impl Iterator<Item = (usize, LinearAddress)> + Clone {
        self.children_iter()
            .map(|(idx, (address, _))| (idx, address))
    }
}

impl From<&LeafNode> for BranchNode {
    fn from(leaf: &LeafNode) -> Self {
        BranchNode {
            partial_path: leaf.partial_path.clone(),
            value: Some(Box::from(&leaf.value[..])),
            children: [const { None }; BranchNode::MAX_CHILDREN],
        }
    }
}
