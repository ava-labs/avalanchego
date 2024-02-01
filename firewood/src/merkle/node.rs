// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{
    logger::trace,
    merkle::from_nibbles,
    shale::{disk_address::DiskAddress, CachedStore, ShaleError, ShaleStore, Storable},
};
use bincode::{Error, Options};
use bitflags::bitflags;
use bytemuck::{CheckedBitPattern, NoUninit, Pod, Zeroable};
use enum_as_inner::EnumAsInner;
use serde::{
    de::DeserializeOwned,
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
};

mod branch;
mod extension;
mod leaf;
mod partial_path;

pub use branch::BranchNode;
pub use extension::ExtNode;
pub use leaf::{LeafNode, SIZE as LEAF_NODE_SIZE};
pub use partial_path::PartialPath;

use crate::nibbles::Nibbles;

use super::{TrieHash, TRIE_HASH_LEN};

bitflags! {
    // should only ever be the size of a nibble
    struct Flags: u8 {
        const TERMINAL = 0b0010;
        const ODD_LEN  = 0b0001;
    }
}

#[derive(Debug, PartialEq, Eq, Clone)]
pub struct Data(pub(super) Vec<u8>);

impl std::ops::Deref for Data {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

impl From<Vec<u8>> for Data {
    fn from(v: Vec<u8>) -> Self {
        Self(v)
    }
}

impl Data {
    pub fn into_inner(self) -> Vec<u8> {
        self.0
    }
}

#[derive(Serialize, Deserialize, Debug)]
enum Encoded<T> {
    Raw(T),
    Data(T),
}

impl Default for Encoded<Vec<u8>> {
    fn default() -> Self {
        // This is the default serialized empty vector
        Encoded::Data(vec![0])
    }
}

impl<T: DeserializeOwned + AsRef<[u8]>> Encoded<T> {
    pub fn decode(self) -> Result<T, bincode::Error> {
        match self {
            Encoded::Raw(raw) => Ok(raw),
            Encoded::Data(data) => bincode::DefaultOptions::new().deserialize(data.as_ref()),
        }
    }
}

#[derive(PartialEq, Eq, Clone, Debug, EnumAsInner)]
pub enum NodeType {
    Branch(Box<BranchNode>),
    Leaf(LeafNode),
    Extension(ExtNode),
}

impl NodeType {
    pub fn decode(buf: &[u8]) -> Result<NodeType, Error> {
        let items: Vec<Encoded<Vec<u8>>> = bincode::DefaultOptions::new().deserialize(buf)?;

        match items.len() {
            LEAF_NODE_SIZE => {
                let mut items = items.into_iter();

                #[allow(clippy::unwrap_used)]
                let decoded_key: Vec<u8> = items.next().unwrap().decode()?;

                let decoded_key_nibbles = Nibbles::<0>::new(&decoded_key);

                let (cur_key_path, term) =
                    PartialPath::from_nibbles(decoded_key_nibbles.into_iter());

                let cur_key = cur_key_path.into_inner();
                #[allow(clippy::unwrap_used)]
                let data: Vec<u8> = items.next().unwrap().decode()?;

                if term {
                    Ok(NodeType::Leaf(LeafNode::new(cur_key, data)))
                } else {
                    Ok(NodeType::Extension(ExtNode {
                        path: PartialPath(cur_key),
                        child: DiskAddress::null(),
                        child_encoded: Some(data),
                    }))
                }
            }
            // TODO: add path
            BranchNode::MSIZE => Ok(NodeType::Branch(BranchNode::decode(buf)?.into())),
            size => Err(Box::new(bincode::ErrorKind::Custom(format!(
                "invalid size: {size}"
            )))),
        }
    }

    pub fn encode<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        match &self {
            NodeType::Leaf(n) => n.encode(),
            NodeType::Extension(n) => n.encode(store),
            NodeType::Branch(n) => n.encode(store),
        }
    }

    pub fn path_mut(&mut self) -> &mut PartialPath {
        match self {
            NodeType::Branch(_u) => todo!(),
            NodeType::Leaf(node) => &mut node.path,
            NodeType::Extension(node) => &mut node.path,
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
                        // path: vec![].into(),
                        children: [Some(DiskAddress::null()); BranchNode::MAX_CHILDREN],
                        value: Some(Data(Vec::new())),
                        children_encoded: Default::default(),
                    }
                    .into(),
                ),
                lazy_dirty: AtomicBool::new(false),
            }
            .serialized_len()
        })
    }

    pub(super) fn get_encoded<S: ShaleStore<Node>>(&self, store: &S) -> &[u8] {
        self.encoded.get_or_init(|| self.inner.encode::<S>(store))
    }

    pub(super) fn get_root_hash<S: ShaleStore<Node>>(&self, store: &S) -> &TrieHash {
        self.root_hash.get_or_init(|| {
            self.set_dirty(true);
            TrieHash(Keccak256::digest(self.get_encoded::<S>(store)).into())
        })
    }

    fn is_encoded_longer_than_hash_len<S: ShaleStore<Node>>(&self, store: &S) -> bool {
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
        Extension = 2,
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
                NodeType::Extension(_) => NodeTypeId::Extension,
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

            NodeTypeId::Extension => {
                let inner = NodeType::Extension(ExtNode::deserialize(offset, mem)?);
                let node =
                    Self::new_from_hash(root_hash, encoded, is_encoded_longer_than_hash_len, inner);

                Ok(node)
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
                NodeType::Extension(n) => n.serialized_len(),
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

            NodeType::Extension(n) => {
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

pub struct EncodedNode<T> {
    pub(crate) node: EncodedNodeType,
    pub(crate) phantom: PhantomData<T>,
}

impl<T> EncodedNode<T> {
    pub const fn new(node: EncodedNodeType) -> Self {
        Self {
            node,
            phantom: PhantomData,
        }
    }
}

pub enum EncodedNodeType {
    Leaf(LeafNode),
    Branch {
        children: Box<[Option<Vec<u8>>; BranchNode::MAX_CHILDREN]>,
        value: Option<Data>,
    },
}

// TODO: probably can merge with `EncodedNodeType`.
#[derive(Debug, Deserialize)]
struct EncodedBranchNode {
    chd: Vec<(u64, Vec<u8>)>,
    data: Option<Vec<u8>>,
    path: Vec<u8>,
}

// Note that the serializer passed in should always be the same type as T in EncodedNode<T>.
impl Serialize for EncodedNode<PlainCodec> {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        let n = match &self.node {
            EncodedNodeType::Leaf(n) => {
                let data = Some(n.data.to_vec());
                let chd: Vec<(u64, Vec<u8>)> = Default::default();
                let path = from_nibbles(&n.path.encode(true)).collect();
                EncodedBranchNode { chd, data, path }
            }
            EncodedNodeType::Branch { children, value } => {
                let chd: Vec<(u64, Vec<u8>)> = children
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

                let data = value.as_ref().map(|v| v.0.to_vec());
                EncodedBranchNode {
                    chd,
                    data,
                    path: Vec::new(),
                }
            }
        };

        let mut s = serializer.serialize_tuple(3)?;
        s.serialize_element(&n.chd)?;
        s.serialize_element(&n.data)?;
        s.serialize_element(&n.path)?;
        s.end()
    }
}

impl<'de> Deserialize<'de> for EncodedNode<PlainCodec> {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        let node: EncodedBranchNode = Deserialize::deserialize(deserializer)?;
        if node.chd.is_empty() {
            let data = if let Some(d) = node.data {
                Data(d)
            } else {
                Data(Vec::new())
            };

            let path = PartialPath::from_nibbles(Nibbles::<0>::new(&node.path).into_iter()).0;
            let node = EncodedNodeType::Leaf(LeafNode { path, data });
            Ok(Self::new(node))
        } else {
            let mut children: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN] = Default::default();
            let value = node.data.map(Data);

            for (i, chd) in node.chd {
                #[allow(clippy::indexing_slicing)]
                (children[i as usize] = Some(chd));
            }

            let node = EncodedNodeType::Branch {
                children: children.into(),
                value,
            };
            Ok(Self::new(node))
        }
    }
}

// Note that the serializer passed in should always be the same type as T in EncodedNode<T>.
impl Serialize for EncodedNode<Bincode> {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        use serde::ser::Error;

        match &self.node {
            EncodedNodeType::Leaf(n) => {
                let list = [
                    Encoded::Raw(from_nibbles(&n.path.encode(true)).collect()),
                    Encoded::Raw(n.data.to_vec()),
                ];
                let mut seq = serializer.serialize_seq(Some(list.len()))?;
                for e in list {
                    seq.serialize_element(&e)?;
                }
                seq.end()
            }
            EncodedNodeType::Branch { children, value } => {
                let mut list = <[Encoded<Vec<u8>>; BranchNode::MAX_CHILDREN + 1]>::default();

                for (i, c) in children
                    .iter()
                    .enumerate()
                    .filter_map(|(i, c)| c.as_ref().map(|c| (i, c)))
                {
                    if c.len() >= TRIE_HASH_LEN {
                        let serialized_hash = Bincode::serialize(&Keccak256::digest(c).to_vec())
                            .map_err(|e| S::Error::custom(format!("bincode error: {e}")))?;
                        #[allow(clippy::indexing_slicing)]
                        (list[i] = Encoded::Data(serialized_hash));
                    } else {
                        #[allow(clippy::indexing_slicing)]
                        (list[i] = Encoded::Raw(c.to_vec()));
                    }
                }
                if let Some(Data(val)) = &value {
                    let serialized_val = Bincode::serialize(val)
                        .map_err(|e| S::Error::custom(format!("bincode error: {e}")))?;
                    list[BranchNode::MAX_CHILDREN] = Encoded::Data(serialized_val);
                }

                let mut seq = serializer.serialize_seq(Some(list.len()))?;
                for e in list {
                    seq.serialize_element(&e)?;
                }
                seq.end()
            }
        }
    }
}

impl<'de> Deserialize<'de> for EncodedNode<Bincode> {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        use serde::de::Error;

        let items: Vec<Encoded<Vec<u8>>> = Deserialize::deserialize(deserializer)?;
        let len = items.len();
        match len {
            LEAF_NODE_SIZE => {
                let mut items = items.into_iter();
                let Some(Encoded::Raw(path)) = items.next() else {
                    return Err(D::Error::custom(
                        "incorrect encoded type for leaf node path",
                    ));
                };
                let Some(Encoded::Raw(data)) = items.next() else {
                    return Err(D::Error::custom(
                        "incorrect encoded type for leaf node data",
                    ));
                };
                let path = PartialPath::from_nibbles(Nibbles::<0>::new(&path).into_iter()).0;
                let node = EncodedNodeType::Leaf(LeafNode {
                    path,
                    data: Data(data),
                });
                Ok(Self::new(node))
            }
            BranchNode::MSIZE => {
                let mut children: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN] = Default::default();
                let mut value: Option<Data> = Default::default();
                let len = items.len();

                for (i, chd) in items.into_iter().enumerate() {
                    if i == len - 1 {
                        let data = match chd {
                            Encoded::Raw(data) => Err(D::Error::custom(format!(
                                "incorrect encoded type for branch node value {:?}",
                                data
                            )))?,
                            Encoded::Data(data) => Bincode::deserialize(data.as_ref())
                                .map_err(|e| D::Error::custom(format!("bincode error: {e}")))?,
                        };
                        // Extract the value of the branch node and set to None if it's an empty Vec
                        value = Some(Data(data)).filter(|data| !data.is_empty());
                    } else {
                        let chd = match chd {
                            Encoded::Raw(chd) => chd,
                            Encoded::Data(chd) => Bincode::deserialize(chd.as_ref())
                                .map_err(|e| D::Error::custom(format!("bincode error: {e}")))?,
                        };
                        #[allow(clippy::indexing_slicing)]
                        (children[i] = Some(chd).filter(|chd| !chd.is_empty()));
                    }
                }
                let node = EncodedNodeType::Branch {
                    children: children.into(),
                    value,
                };
                Ok(Self::new(node))
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
    use crate::shale::cached::PlainMem;
    use std::iter::repeat;
    use test_case::test_matrix;

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
        let leaf = NodeType::Leaf(LeafNode::new(PartialPath(vec![1, 2, 3]), Data(vec![4, 5])));
        let branch = NodeType::Branch(Box::new(BranchNode {
            // path: vec![].into(),
            children: [Some(DiskAddress::from(1)); BranchNode::MAX_CHILDREN],
            value: Some(Data(vec![1, 2, 3])),
            children_encoded: std::array::from_fn(|_| Some(vec![1])),
        }));
        let extension = NodeType::Extension(ExtNode {
            path: PartialPath(vec![1, 2, 3]),
            child: DiskAddress::from(1),
            child_encoded: Some(vec![1, 2, 3]),
        });

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

        let node = Node::new_from_hash(
            root_hash,
            encoded.clone(),
            is_encoded_longer_than_hash_len,
            extension,
        );

        check_node_encoding(node);
    }

    #[test_matrix(
        (0..0, 0..15, 0..16, 0..31, 0..32),
        [0..0, 0..16, 0..32]
    )]
    fn leaf_node<Iter: Iterator<Item = u8>>(path: Iter, data: Iter) {
        let node = Node::from_leaf(LeafNode::new(
            PartialPath(path.map(|x| x & 0xf).collect()),
            Data(data.collect()),
        ));

        check_node_encoding(node);
    }

    #[test_matrix(
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
        children: Vec<usize>,
        value: impl Into<Option<u8>>,
        children_encoded: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN],
    ) {
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
            .map(|x| Data(std::iter::repeat(x).take(x as usize).collect()));

        let node = Node::from_branch(BranchNode {
            // path: vec![].into(),
            children,
            value,
            children_encoded,
        });

        check_node_encoding(node);
    }

    #[test_matrix(
        [0..1, 0..16],
        [DiskAddress::null(), DiskAddress::from(1)],
        [Nil, 1, 32, 33]
    )]
    fn extension_encoding<Iter: Iterator<Item = u8>>(
        path: Iter,
        child: DiskAddress,
        child_encoded: impl Into<Option<usize>>,
    ) {
        let node = Node::from(NodeType::Extension(ExtNode {
            path: PartialPath(path.map(|x| x & 0xf).collect()),
            child,
            child_encoded: child_encoded
                .into()
                .map(|x| repeat(x as u8).take(x).collect()),
        }));

        check_node_encoding(node);
    }

    fn check_node_encoding(node: Node) {
        let serialized_len = node.serialized_len();

        let mut bytes = vec![0; serialized_len as usize];
        node.serialize(&mut bytes).expect("node should serialize");

        let mut mem = PlainMem::new(serialized_len, 0);
        mem.write(0, &bytes);

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
