// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::match_same_arms,
    reason = "Found 1 occurrences after enabling the lint."
)]

use crate::node::ExtendableBytes;
use crate::node::children::Children;
use crate::{LeafNode, LinearAddress, MaybePersistedNode, Node, Path, PathComponent, SharedNode};
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

    #[derive(Clone)]
    pub enum HashOrRlp {
        Hash(TrieHash),
        // TODO: this slice is never larger than 32 bytes so smallvec is probably not our best container
        // the length is stored in a `usize` but it could be in a `u8` and it will never overflow
        Rlp(SmallVec<[u8; 32]>),
    }

    /// Manual implementation of [`Debug`](std::fmt::Debug) so that the RLP bytes
    /// are displayed as hex rather than raw bytes, which is more useful for
    /// debugging purposes.
    impl std::fmt::Debug for HashOrRlp {
        fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
            match self {
                HashOrRlp::Hash(h) => write!(f, "Hash({h})"),
                HashOrRlp::Rlp(r) => write!(f, "Rlp({})", hex::encode(r)),
            }
        }
    }

    impl HashOrRlp {
        /// Creates a new `TrieHash` from the default value, which is the all zeros.
        ///
        /// ```
        /// assert_eq!(
        ///     firewood_storage::HashType::empty(),
        ///     firewood_storage::HashType::from([0; 32]),
        /// )
        /// ```
        #[must_use]
        pub fn empty() -> Self {
            TrieHash::empty().into()
        }

        pub fn as_slice(&self) -> &[u8] {
            self
        }

        pub fn into_triehash(self) -> TrieHash {
            self.into()
        }

        // used in PartialEq and Hash impls and is to trick clippy into not caring
        // about creating an owned instance for comparison
        fn as_triehash(&self) -> TrieHash {
            self.into()
        }
    }

    impl PartialEq<TrieHash> for HashOrRlp {
        fn eq(&self, other: &TrieHash) -> bool {
            match self {
                HashOrRlp::Hash(h) => h == other,
                #[expect(deprecated, reason = "transitive dependency on generic-array")]
                HashOrRlp::Rlp(r) => Keccak256::digest(r.as_ref()).as_slice() == other.as_ref(),
            }
        }
    }

    impl PartialEq<HashOrRlp> for TrieHash {
        fn eq(&self, other: &HashOrRlp) -> bool {
            match other {
                HashOrRlp::Hash(h) => h == self,
                #[expect(deprecated, reason = "transitive dependency on generic-array")]
                HashOrRlp::Rlp(r) => Keccak256::digest(r.as_ref()).as_slice() == self.as_ref(),
            }
        }
    }

    impl PartialEq for HashOrRlp {
        fn eq(&self, other: &Self) -> bool {
            match (self, other) {
                // if both are hash or rlp, we can skip hashing
                (HashOrRlp::Hash(h1), HashOrRlp::Hash(h2)) => h1 == h2,
                (HashOrRlp::Rlp(r1), HashOrRlp::Rlp(r2)) => r1 == r2,
                // otherwise, one is a hash and the other isn't, so convert both
                // to hash and compare
                _ => self.as_triehash() == other.as_triehash(),
            }
        }
    }

    impl Eq for HashOrRlp {}

    impl std::hash::Hash for HashOrRlp {
        fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
            // contract on `Hash` and `PartialEq` requires that if `a == b` then `hash(a) == hash(b)`
            // and since `PartialEq` may require hashing, we must always convert to `TrieHash` here
            // and use it's hash implementation
            self.as_triehash().hash(state);
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
                    #[expect(clippy::indexing_slicing)]
                    {
                        reader.read_exact(&mut bytes[0..len as usize])?;
                    }
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

    impl From<&HashOrRlp> for TrieHash {
        fn from(val: &HashOrRlp) -> Self {
            match val {
                HashOrRlp::Hash(h) => h.clone(),
                HashOrRlp::Rlp(r) => Keccak256::digest(r).into(),
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

    impl TryFrom<&[u8]> for HashOrRlp {
        type Error = crate::InvalidTrieHashLength;

        fn try_from(value: &[u8]) -> Result<Self, Self::Error> {
            value.try_into().map(HashOrRlp::Hash)
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
                HashOrRlp::Hash(h) => h.as_slice(),
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
