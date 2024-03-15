// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{
    logger::trace,
    merkle::nibbles_to_bytes_iter,
    shale::{compact::CompactSpace, disk_address::DiskAddress, CachedStore, ShaleError, Storable},
};
use bincode::{Error, Options};
use bitflags::bitflags;
use bytemuck::{CheckedBitPattern, NoUninit, Pod, Zeroable};
use enum_as_inner::EnumAsInner;
use serde::{
    ser::{SerializeSeq, SerializeTuple},
    Deserialize, Serialize,
};
use sha3::{Digest, Keccak256};
use std::{
    fmt::Debug,
    io::{Cursor, Write},
    marker::PhantomData,
    mem::size_of,
    sync::{
        atomic::{AtomicBool, Ordering},
        OnceLock,
    },
    vec,
};

mod branch;
mod leaf;
mod path;

pub use branch::BranchNode;
pub use leaf::{LeafNode, SIZE as LEAF_NODE_SIZE};
pub use path::Path;

use crate::nibbles::Nibbles;

use super::{TrieHash, TRIE_HASH_LEN};

bitflags! {
    // should only ever be the size of a nibble
    struct Flags: u8 {
        const ODD_LEN  = 0b0001;
    }
}

#[derive(PartialEq, Eq, Clone, Debug, EnumAsInner)]
pub enum NodeType {
    Branch(Box<BranchNode>),
    Leaf(LeafNode),
}

impl NodeType {
    pub fn decode(buf: &[u8]) -> Result<NodeType, Error> {
        let items: Vec<Vec<u8>> = bincode::DefaultOptions::new().deserialize(buf)?;

        match items.len() {
            LEAF_NODE_SIZE => {
                let mut items = items.into_iter();

                #[allow(clippy::unwrap_used)]
                let decoded_key: Vec<u8> = items.next().unwrap();

                let decoded_key_nibbles = Nibbles::<0>::new(&decoded_key);

                let cur_key_path = Path::from_nibbles(decoded_key_nibbles.into_iter());

                let cur_key = cur_key_path.into_inner();
                #[allow(clippy::unwrap_used)]
                let value: Vec<u8> = items.next().unwrap();

                Ok(NodeType::Leaf(LeafNode::new(cur_key, value)))
            }
            // TODO: add path
            BranchNode::MSIZE => Ok(NodeType::Branch(BranchNode::decode(buf)?.into())),
            size => Err(Box::new(bincode::ErrorKind::Custom(format!(
                "invalid size: {size}"
            )))),
        }
    }

    pub fn encode<S: CachedStore>(&self, store: &CompactSpace<Node, S>) -> Vec<u8> {
        match &self {
            NodeType::Leaf(n) => n.encode(),
            NodeType::Branch(n) => n.encode(store),
        }
    }

    pub fn path_mut(&mut self) -> &mut Path {
        match self {
            NodeType::Branch(u) => &mut u.partial_path,
            NodeType::Leaf(node) => &mut node.partial_path,
        }
    }

    pub fn set_path(&mut self, path: Path) {
        match self {
            NodeType::Branch(u) => u.partial_path = path,
            NodeType::Leaf(node) => node.partial_path = path,
        }
    }

    pub fn set_value(&mut self, value: Vec<u8>) {
        match self {
            NodeType::Branch(u) => u.value = Some(value),
            NodeType::Leaf(node) => node.value = value,
        }
    }
}

#[derive(Debug)]
pub struct Node {
    pub(super) root_hash: OnceLock<TrieHash>,
    encoded: OnceLock<Vec<u8>>,
    is_encoded_longer_than_hash_len: OnceLock<bool>,
    // lazy_dirty is an atomicbool, but only writers ever set it
    // Therefore, we can always use Relaxed ordering. It's atomic
    // just to ensure Sync + Send.
    lazy_dirty: AtomicBool,
    pub(super) inner: NodeType,
}

impl Eq for Node {}

impl PartialEq for Node {
    fn eq(&self, other: &Self) -> bool {
        let is_dirty = self.is_dirty();

        let Node {
            root_hash,
            encoded,
            is_encoded_longer_than_hash_len: _,
            lazy_dirty: _,
            inner,
        } = self;
        *root_hash == other.root_hash
            && *encoded == other.encoded
            && is_dirty == other.is_dirty()
            && *inner == other.inner
    }
}

impl Clone for Node {
    fn clone(&self) -> Self {
        Self {
            root_hash: self.root_hash.clone(),
            is_encoded_longer_than_hash_len: self.is_encoded_longer_than_hash_len.clone(),
            encoded: self.encoded.clone(),
            lazy_dirty: AtomicBool::new(self.is_dirty()),
            inner: self.inner.clone(),
        }
    }
}

impl From<NodeType> for Node {
    fn from(inner: NodeType) -> Self {
        let mut s = Self {
            root_hash: OnceLock::new(),
            encoded: OnceLock::new(),
            is_encoded_longer_than_hash_len: OnceLock::new(),
            inner,
            lazy_dirty: AtomicBool::new(false),
        };
        s.rehash();
        s
    }
}

bitflags! {
    #[derive(Debug, Default, Clone, Copy, Pod, Zeroable)]
    #[repr(transparent)]
    struct NodeAttributes: u8 {
        const HAS_ROOT_HASH           = 0b001;
        const ENCODED_LENGTH_IS_KNOWN = 0b010;
        const ENCODED_IS_LONG         = 0b110;
    }
}

impl Node {
    pub(super) fn max_branch_node_size() -> u64 {
        let max_size: OnceLock<u64> = OnceLock::new();
        *max_size.get_or_init(|| {
            Self {
                root_hash: OnceLock::new(),
                encoded: OnceLock::new(),
                is_encoded_longer_than_hash_len: OnceLock::new(),
                inner: NodeType::Branch(
                    BranchNode {
                        partial_path: vec![].into(),
                        children: [Some(DiskAddress::null()); BranchNode::MAX_CHILDREN],
                        value: Some(Vec::new()),
                        children_encoded: Default::default(),
                    }
                    .into(),
                ),
                lazy_dirty: AtomicBool::new(false),
            }
            .serialized_len()
        })
    }

    pub(super) fn get_encoded<S: CachedStore>(&self, store: &CompactSpace<Node, S>) -> &[u8] {
        self.encoded.get_or_init(|| self.inner.encode(store))
    }

    pub(super) fn get_root_hash<S: CachedStore>(&self, store: &CompactSpace<Node, S>) -> &TrieHash {
        self.root_hash.get_or_init(|| {
            self.set_dirty(true);
            TrieHash(Keccak256::digest(self.get_encoded(store)).into())
        })
    }

    fn is_encoded_longer_than_hash_len<S: CachedStore>(
        &self,
        store: &CompactSpace<Node, S>,
    ) -> bool {
        *self
            .is_encoded_longer_than_hash_len
            .get_or_init(|| self.get_encoded(store).len() >= TRIE_HASH_LEN)
    }

    pub(super) fn rehash(&mut self) {
        self.encoded = OnceLock::new();
        self.is_encoded_longer_than_hash_len = OnceLock::new();
        self.root_hash = OnceLock::new();
    }

    pub fn from_branch<B: Into<Box<BranchNode>>>(node: B) -> Self {
        Self::from(NodeType::Branch(node.into()))
    }

    pub fn from_leaf(leaf: LeafNode) -> Self {
        Self::from(NodeType::Leaf(leaf))
    }

    pub const fn inner(&self) -> &NodeType {
        &self.inner
    }

    pub fn inner_mut(&mut self) -> &mut NodeType {
        &mut self.inner
    }

    pub(super) fn new_from_hash(
        root_hash: Option<TrieHash>,
        encoded: Option<Vec<u8>>,
        is_encoded_longer_than_hash_len: Option<bool>,
        inner: NodeType,
    ) -> Self {
        Self {
            root_hash: match root_hash {
                Some(h) => OnceLock::from(h),
                None => OnceLock::new(),
            },
            encoded: match encoded.filter(|encoded| !encoded.is_empty()) {
                Some(e) => OnceLock::from(e),
                None => OnceLock::new(),
            },
            is_encoded_longer_than_hash_len: match is_encoded_longer_than_hash_len {
                Some(v) => OnceLock::from(v),
                None => OnceLock::new(),
            },
            inner,
            lazy_dirty: AtomicBool::new(false),
        }
    }

    pub(super) fn is_dirty(&self) -> bool {
        self.lazy_dirty.load(Ordering::Relaxed)
    }

    pub(super) fn set_dirty(&self, is_dirty: bool) {
        self.lazy_dirty.store(is_dirty, Ordering::Relaxed)
    }

    pub(crate) fn as_branch_mut(&mut self) -> &mut Box<BranchNode> {
        self.inner_mut()
            .as_branch_mut()
            .expect("must be a branch node")
    }
}

#[derive(Clone, Copy, CheckedBitPattern, NoUninit)]
#[repr(C, packed)]
struct Meta {
    root_hash: [u8; TRIE_HASH_LEN],
    attrs: NodeAttributes,
    encoded_len: u64,
    encoded: [u8; TRIE_HASH_LEN],
    type_id: NodeTypeId,
}

impl Meta {
    const SIZE: usize = size_of::<Self>();
}

mod type_id {
    use super::{CheckedBitPattern, NoUninit, NodeType};
    use crate::shale::ShaleError;

    #[derive(Clone, Copy, CheckedBitPattern, NoUninit)]
    #[repr(u8)]
    pub enum NodeTypeId {
        Branch = 0,
        Leaf = 1,
    }

    impl TryFrom<u8> for NodeTypeId {
        type Error = ShaleError;

        fn try_from(value: u8) -> Result<Self, Self::Error> {
            bytemuck::checked::try_cast::<_, Self>(value).map_err(|_| ShaleError::InvalidNodeType)
        }
    }

    impl From<&NodeType> for NodeTypeId {
        fn from(node_type: &NodeType) -> Self {
            match node_type {
                NodeType::Branch(_) => NodeTypeId::Branch,
                NodeType::Leaf(_) => NodeTypeId::Leaf,
            }
        }
    }
}

use type_id::NodeTypeId;

impl Storable for Node {
    fn deserialize<T: CachedStore>(offset: usize, mem: &T) -> Result<Self, ShaleError> {
        let meta_raw =
            mem.get_view(offset, Meta::SIZE as u64)
                .ok_or(ShaleError::InvalidCacheView {
                    offset,
                    size: Meta::SIZE as u64,
                })?;
        let meta_raw = meta_raw.as_deref();

        let meta = bytemuck::checked::try_from_bytes::<Meta>(&meta_raw)
            .map_err(|_| ShaleError::InvalidNodeMeta)?;

        let Meta {
            root_hash,
            attrs,
            encoded_len,
            encoded,
            type_id,
        } = *meta;

        trace!("[{mem:p}] Deserializing node at {offset}");

        let offset = offset + Meta::SIZE;

        let root_hash = if attrs.contains(NodeAttributes::HAS_ROOT_HASH) {
            Some(TrieHash(root_hash))
        } else {
            None
        };

        let encoded = if encoded_len > 0 {
            Some(encoded.iter().take(encoded_len as usize).copied().collect())
        } else {
            None
        };

        let is_encoded_longer_than_hash_len =
            if attrs.contains(NodeAttributes::ENCODED_LENGTH_IS_KNOWN) {
                attrs.contains(NodeAttributes::ENCODED_IS_LONG).into()
            } else {
                None
            };

        match type_id {
            NodeTypeId::Branch => {
                let inner = NodeType::Branch(Box::new(BranchNode::deserialize(offset, mem)?));

                Ok(Self::new_from_hash(
                    root_hash,
                    encoded,
                    is_encoded_longer_than_hash_len,
                    inner,
                ))
            }

            NodeTypeId::Leaf => {
                let inner = NodeType::Leaf(LeafNode::deserialize(offset, mem)?);
                let node =
                    Self::new_from_hash(root_hash, encoded, is_encoded_longer_than_hash_len, inner);

                Ok(node)
            }
        }
    }

    fn serialized_len(&self) -> u64 {
        Meta::SIZE as u64
            + match &self.inner {
                NodeType::Branch(n) => n.serialized_len(),
                NodeType::Leaf(n) => n.serialized_len(),
            }
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        trace!("[{self:p}] Serializing node");
        let mut cursor = Cursor::new(to);

        let (mut attrs, root_hash) = match self.root_hash.get() {
            Some(hash) => (NodeAttributes::HAS_ROOT_HASH, hash.0),
            None => Default::default(),
        };

        let encoded = self
            .encoded
            .get()
            .filter(|encoded| encoded.len() < TRIE_HASH_LEN);

        let encoded_len = encoded.map(Vec::len).unwrap_or(0) as u64;

        if let Some(&is_encoded_longer_than_hash_len) = self.is_encoded_longer_than_hash_len.get() {
            attrs.insert(if is_encoded_longer_than_hash_len {
                NodeAttributes::ENCODED_IS_LONG
            } else {
                NodeAttributes::ENCODED_LENGTH_IS_KNOWN
            });
        }

        let encoded = std::array::from_fn({
            let mut encoded = encoded.into_iter().flatten().copied();
            move |_| encoded.next().unwrap_or(0)
        });

        let type_id = NodeTypeId::from(&self.inner);

        let meta = Meta {
            root_hash,
            attrs,
            encoded_len,
            encoded,
            type_id,
        };

        cursor.write_all(bytemuck::bytes_of(&meta))?;

        match &self.inner {
            NodeType::Branch(n) => {
                let pos = cursor.position() as usize;

                #[allow(clippy::indexing_slicing)]
                n.serialize(&mut cursor.get_mut()[pos..])
            }

            NodeType::Leaf(n) => {
                let pos = cursor.position() as usize;

                #[allow(clippy::indexing_slicing)]
                n.serialize(&mut cursor.get_mut()[pos..])
            }
        }
    }
}

/// Contains the fields that we include in a node's hash.
/// If this is a leaf node, `children` is empty and `value` is Some.
/// If this is a branch node, `children` is non-empty.
#[derive(Debug)]
pub struct EncodedNode<T> {
    pub(crate) partial_path: Path,
    /// If a child is None, it doesn't exist.
    /// If it's Some, it's the value or value hash of the child.
    pub(crate) children: Box<[Option<Vec<u8>>; BranchNode::MAX_CHILDREN]>,
    pub(crate) value: Option<Vec<u8>>,
    pub(crate) phantom: PhantomData<T>,
}

impl<T> PartialEq for EncodedNode<T> {
    fn eq(&self, other: &Self) -> bool {
        self.partial_path == other.partial_path
            && self.children == other.children
            && self.value == other.value
    }
}

// Note that the serializer passed in should always be the same type as T in EncodedNode<T>.
impl Serialize for EncodedNode<PlainCodec> {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        let chd: Vec<(u64, Vec<u8>)> = self
            .children
            .iter()
            .enumerate()
            .filter_map(|(i, c)| c.as_ref().map(|c| (i as u64, c)))
            .map(|(i, c)| {
                if c.len() >= TRIE_HASH_LEN {
                    (i, Keccak256::digest(c).to_vec())
                } else {
                    (i, c.to_vec())
                }
            })
            .collect();

        let value = self.value.as_deref();

        let path: Vec<u8> = nibbles_to_bytes_iter(&self.partial_path.encode()).collect();

        let mut s = serializer.serialize_tuple(3)?;

        s.serialize_element(&chd)?;
        s.serialize_element(&value)?;
        s.serialize_element(&path)?;

        s.end()
    }
}

impl<'de> Deserialize<'de> for EncodedNode<PlainCodec> {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let chd: Vec<(u64, Vec<u8>)>;
        let value: Option<Vec<u8>>;
        let path: Vec<u8>;

        (chd, value, path) = Deserialize::deserialize(deserializer)?;

        let path = Path::from_nibbles(Nibbles::<0>::new(&path).into_iter());

        let mut children: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN] = Default::default();
        #[allow(clippy::indexing_slicing)]
        for (i, chd) in chd {
            children[i as usize] = Some(chd);
        }

        Ok(Self {
            partial_path: path,
            children: children.into(),
            value,
            phantom: PhantomData,
        })
    }
}

// Note that the serializer passed in should always be the same type as T in EncodedNode<T>.
impl Serialize for EncodedNode<Bincode> {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        let mut list = <[Vec<u8>; BranchNode::MAX_CHILDREN + 2]>::default();
        let children = self
            .children
            .iter()
            .enumerate()
            .filter_map(|(i, c)| c.as_ref().map(|c| (i, c)));

        #[allow(clippy::indexing_slicing)]
        for (i, child) in children {
            if child.len() >= TRIE_HASH_LEN {
                let serialized_hash = Keccak256::digest(child).to_vec();
                list[i] = serialized_hash;
            } else {
                list[i] = child.to_vec();
            }
        }

        if let Some(val) = &self.value {
            list[BranchNode::MAX_CHILDREN] = val.clone();
        }

        let serialized_path = nibbles_to_bytes_iter(&self.partial_path.encode()).collect();
        list[BranchNode::MAX_CHILDREN + 1] = serialized_path;

        let mut seq = serializer.serialize_seq(Some(list.len()))?;

        for e in list {
            seq.serialize_element(&e)?;
        }

        seq.end()
    }
}

impl<'de> Deserialize<'de> for EncodedNode<Bincode> {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        use serde::de::Error;

        let mut items: Vec<Vec<u8>> = Deserialize::deserialize(deserializer)?;
        let len = items.len();

        match len {
            LEAF_NODE_SIZE => {
                let mut items = items.into_iter();
                let Some(path) = items.next() else {
                    return Err(D::Error::custom(
                        "incorrect encoded type for leaf node path",
                    ));
                };
                let Some(value) = items.next() else {
                    return Err(D::Error::custom(
                        "incorrect encoded type for leaf node value",
                    ));
                };
                let path = Path::from_nibbles(Nibbles::<0>::new(&path).into_iter());
                let children: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN] = Default::default();
                Ok(Self {
                    partial_path: path,
                    children: children.into(),
                    value: Some(value),
                    phantom: PhantomData,
                })
            }

            BranchNode::MSIZE => {
                let path = items.pop().expect("length was checked above");
                let path = Path::from_nibbles(Nibbles::<0>::new(&path).into_iter());

                let value = items.pop().expect("length was checked above");
                let value = if value.is_empty() { None } else { Some(value) };

                let mut children: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN] = Default::default();

                for (i, chd) in items.into_iter().enumerate() {
                    #[allow(clippy::indexing_slicing)]
                    (children[i] = Some(chd).filter(|chd| !chd.is_empty()));
                }

                Ok(Self {
                    partial_path: path,
                    children: children.into(),
                    value,
                    phantom: PhantomData,
                })
            }
            size => Err(D::Error::custom(format!("invalid size: {size}"))),
        }
    }
}

pub trait BinarySerde {
    type SerializeError: serde::ser::Error;
    type DeserializeError: serde::de::Error;

    fn new() -> Self;

    fn serialize<T: Serialize>(t: &T) -> Result<Vec<u8>, Self::SerializeError>
    where
        Self: Sized,
    {
        Self::new().serialize_impl(t)
    }

    fn deserialize<'de, T: Deserialize<'de>>(bytes: &'de [u8]) -> Result<T, Self::DeserializeError>
    where
        Self: Sized,
    {
        Self::new().deserialize_impl(bytes)
    }

    fn serialize_impl<T: Serialize>(&self, t: &T) -> Result<Vec<u8>, Self::SerializeError>;
    fn deserialize_impl<'de, T: Deserialize<'de>>(
        &self,
        bytes: &'de [u8],
    ) -> Result<T, Self::DeserializeError>;
}

#[derive(Default)]
pub struct Bincode(pub bincode::DefaultOptions);

impl Debug for Bincode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> Result<(), std::fmt::Error> {
        write!(f, "[bincode::DefaultOptions]")
    }
}

impl BinarySerde for Bincode {
    type SerializeError = bincode::Error;
    type DeserializeError = Self::SerializeError;

    fn new() -> Self {
        Self(bincode::DefaultOptions::new())
    }

    fn serialize_impl<T: Serialize>(&self, t: &T) -> Result<Vec<u8>, Self::SerializeError> {
        self.0.serialize(t)
    }

    fn deserialize_impl<'de, T: Deserialize<'de>>(
        &self,
        bytes: &'de [u8],
    ) -> Result<T, Self::DeserializeError> {
        self.0.deserialize(bytes)
    }
}

#[derive(Default)]
pub struct PlainCodec(pub bincode::DefaultOptions);

impl Debug for PlainCodec {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> Result<(), std::fmt::Error> {
        write!(f, "PlainCodec")
    }
}

impl BinarySerde for PlainCodec {
    type SerializeError = bincode::Error;
    type DeserializeError = Self::SerializeError;

    fn new() -> Self {
        Self(bincode::DefaultOptions::new())
    }

    fn serialize_impl<T: Serialize>(&self, t: &T) -> Result<Vec<u8>, Self::SerializeError> {
        // Serializes the object directly into a Writer without include the length.
        let mut writer = Vec::new();
        self.0.serialize_into(&mut writer, t)?;
        Ok(writer)
    }

    fn deserialize_impl<'de, T: Deserialize<'de>>(
        &self,
        bytes: &'de [u8],
    ) -> Result<T, Self::DeserializeError> {
        self.0.deserialize(bytes)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::shale::cached::InMemLinearStore;
    use std::iter::repeat;
    use test_case::{test_case, test_matrix};

    #[test_matrix(
        [Nil, [0x00; TRIE_HASH_LEN]],
        [Nil, vec![], vec![0x01], (0..TRIE_HASH_LEN as u8).collect::<Vec<_>>(), (0..33).collect::<Vec<_>>()],
        [Nil, false, true]
    )]
    fn cached_node_data(
        root_hash: impl Into<Option<[u8; TRIE_HASH_LEN]>>,
        encoded: impl Into<Option<Vec<u8>>>,
        is_encoded_longer_than_hash_len: impl Into<Option<bool>>,
    ) {
        let leaf = NodeType::Leaf(LeafNode::new(Path(vec![1, 2, 3]), vec![4, 5]));
        let branch = NodeType::Branch(Box::new(BranchNode {
            partial_path: vec![].into(),
            children: [Some(DiskAddress::from(1)); BranchNode::MAX_CHILDREN],
            value: Some(vec![1, 2, 3]),
            children_encoded: std::array::from_fn(|_| Some(vec![1])),
        }));

        let root_hash = root_hash.into().map(TrieHash);
        let encoded = encoded.into();
        let is_encoded_longer_than_hash_len = is_encoded_longer_than_hash_len.into();

        let node = Node::new_from_hash(
            root_hash,
            encoded.clone(),
            is_encoded_longer_than_hash_len,
            leaf,
        );

        check_node_encoding(node);

        let node = Node::new_from_hash(
            root_hash,
            encoded.clone(),
            is_encoded_longer_than_hash_len,
            branch,
        );

        check_node_encoding(node);
    }

    #[test_matrix(
        (0..0, 0..15, 0..16, 0..31, 0..32),
        [0..0, 0..16, 0..32]
    )]
    fn leaf_node<Iter: Iterator<Item = u8>>(path: Iter, value: Iter) {
        let node = Node::from_leaf(LeafNode::new(
            Path(path.map(|x| x & 0xf).collect()),
            value.collect::<Vec<u8>>(),
        ));

        check_node_encoding(node);
    }

    #[test_case(&[])]
    #[test_case(&[0x00])]
    #[test_case(&[0x0F])]
    #[test_case(&[0x00, 0x00])]
    #[test_case(&[0x01, 0x02])]
    #[test_case(&[0x00,0x0F])]
    #[test_case(&[0x0F,0x0F])]
    #[test_case(&[0x0F,0x01,0x0F])]
    fn encoded_branch_node_bincode_serialize(path_nibbles: &[u8]) -> Result<(), Error> {
        let node = EncodedNode::<Bincode> {
            partial_path: Path(path_nibbles.to_vec()),
            children: Default::default(),
            value: Some(vec![1, 2, 3, 4]),
            phantom: PhantomData,
        };

        let node_bytes = Bincode::serialize(&node)?;

        let deserialized_node: EncodedNode<Bincode> = Bincode::deserialize(&node_bytes)?;

        assert_eq!(&node, &deserialized_node);

        Ok(())
    }

    #[test_matrix(
        [&[], &[0xf], &[0xf, 0xf]],
        [vec![], vec![1,0,0,0,0,0,0,1], vec![1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,1], repeat(1).take(16).collect()],
        [Nil, 0, 15],
        [
            std::array::from_fn(|_| None),
            std::array::from_fn(|_| Some(vec![1])),
            [Some(vec![1]), None, None, None, None, None, None, None, None, None, None, None, None, None, None, Some(vec![1])],
            std::array::from_fn(|_| Some(vec![1; 32])),
            std::array::from_fn(|_| Some(vec![1; 33]))
        ]
    )]
    fn branch_encoding(
        path: &[u8],
        children: Vec<usize>,
        value: impl Into<Option<u8>>,
        children_encoded: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN],
    ) {
        let partial_path = Path(path.iter().copied().map(|x| x & 0xf).collect());

        let mut children = children.into_iter().map(|x| {
            if x == 0 {
                None
            } else {
                Some(DiskAddress::from(x))
            }
        });

        let children = std::array::from_fn(|_| children.next().flatten());

        let value = value
            .into()
            .map(|x| std::iter::repeat(x).take(x as usize).collect());

        let node = Node::from_branch(BranchNode {
            partial_path,
            children,
            value,
            children_encoded,
        });

        check_node_encoding(node);
    }

    fn check_node_encoding(node: Node) {
        let serialized_len = node.serialized_len();

        let mut bytes = vec![0; serialized_len as usize];
        node.serialize(&mut bytes).expect("node should serialize");

        let mut mem = InMemLinearStore::new(serialized_len, 0);
        mem.write(0, &bytes).expect("write should succed");

        let mut hydrated_node = Node::deserialize(0, &mem).expect("node should deserialize");

        let encoded = node
            .encoded
            .get()
            .filter(|encoded| encoded.len() >= TRIE_HASH_LEN);

        match encoded {
            // long-encoded won't be serialized
            Some(encoded) if hydrated_node.encoded.get().is_none() => {
                hydrated_node.encoded = OnceLock::from(encoded.clone());
            }
            _ => (),
        }

        assert_eq!(node, hydrated_node);
    }

    struct Nil;

    macro_rules! impl_nil_for {
        // match a comma separated list of types
        ($($t:ty),* $(,)?) => {
            $(
                impl From<Nil> for Option<$t> {
                    fn from(_val: Nil) -> Self {
                        None
                    }
                }
            )*
        };
    }

    impl_nil_for!([u8; 32], Vec<u8>, usize, u8, bool);
}
