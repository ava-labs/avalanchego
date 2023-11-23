// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::shale::{disk_address::DiskAddress, CachedStore, ShaleError, ShaleStore, Storable};
use bincode::{Error, Options};
use bitflags::bitflags;
use enum_as_inner::EnumAsInner;
use serde::{de::DeserializeOwned, Deserialize, Serialize};
use sha3::{Digest, Keccak256};
use std::{
    fmt::Debug,
    io::{Cursor, Read, Write},
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

pub use branch::{BranchNode, MAX_CHILDREN, SIZE as BRANCH_NODE_SIZE};
pub use extension::ExtNode;
pub use leaf::{LeafNode, SIZE as LEAF_NODE_SIZE};
pub use partial_path::PartialPath;

use crate::merkle::to_nibble_array;
use crate::nibbles::Nibbles;

use super::{from_nibbles, TrieHash, TRIE_HASH_LEN};

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

                let decoded_key: Vec<u8> = items.next().unwrap().decode()?;

                let decoded_key_nibbles = Nibbles::<0>::new(&decoded_key);

                let (cur_key_path, term) =
                    PartialPath::from_nibbles(decoded_key_nibbles.into_iter());

                let cur_key = cur_key_path.into_inner();
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
            BRANCH_NODE_SIZE => Ok(NodeType::Branch(BranchNode::decode(buf)?.into())),
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
    is_encoded_longer_than_hash_len: OnceLock<bool>,
    encoded: OnceLock<Vec<u8>>,
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
            is_encoded_longer_than_hash_len,
            encoded,
            lazy_dirty: _,
            inner,
        } = self;
        *root_hash == other.root_hash
            && *is_encoded_longer_than_hash_len == other.is_encoded_longer_than_hash_len
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
            is_encoded_longer_than_hash_len: OnceLock::new(),
            encoded: OnceLock::new(),
            inner,
            lazy_dirty: AtomicBool::new(false),
        };
        s.rehash();
        s
    }
}

bitflags! {
    struct NodeAttributes: u8 {
        const ROOT_HASH_VALID      = 0b001;
        const IS_ENCODED_BIG_VALID = 0b010;
        const LONG                 = 0b100;
    }
}

impl Node {
    pub(super) fn max_branch_node_size() -> u64 {
        let max_size: OnceLock<u64> = OnceLock::new();
        *max_size.get_or_init(|| {
            Self {
                root_hash: OnceLock::new(),
                is_encoded_longer_than_hash_len: OnceLock::new(),
                encoded: OnceLock::new(),
                inner: NodeType::Branch(
                    BranchNode {
                        // path: vec![].into(),
                        children: [Some(DiskAddress::null()); MAX_CHILDREN],
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
        *self.is_encoded_longer_than_hash_len.get_or_init(|| {
            self.set_dirty(true);
            self.get_encoded(store).len() >= TRIE_HASH_LEN
        })
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

    pub fn inner(&self) -> &NodeType {
        &self.inner
    }

    pub fn inner_mut(&mut self) -> &mut NodeType {
        &mut self.inner
    }

    pub(super) fn new_from_hash(
        root_hash: Option<TrieHash>,
        encoded_longer_than_hash_len: Option<bool>,
        inner: NodeType,
    ) -> Self {
        Self {
            root_hash: match root_hash {
                Some(h) => OnceLock::from(h),
                None => OnceLock::new(),
            },
            is_encoded_longer_than_hash_len: match encoded_longer_than_hash_len {
                Some(b) => OnceLock::from(b),
                None => OnceLock::new(),
            },
            encoded: OnceLock::new(),
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

#[repr(C)]
struct Meta {
    root_hash: [u8; TRIE_HASH_LEN],
    attrs: NodeAttributes,
    is_encoded_longer_than_hash_len: Option<bool>,
}

impl Meta {
    const SIZE: usize = size_of::<Self>();
}

mod type_id {
    use crate::shale::ShaleError;

    const BRANCH: u8 = 0;
    const LEAF: u8 = 1;
    const EXTENSION: u8 = 2;

    #[repr(u8)]
    pub enum NodeTypeId {
        Branch = BRANCH,
        Leaf = LEAF,
        Extension = EXTENSION,
    }

    impl TryFrom<u8> for NodeTypeId {
        type Error = ShaleError;

        fn try_from(value: u8) -> Result<Self, Self::Error> {
            match value {
                BRANCH => Ok(Self::Branch),
                LEAF => Ok(Self::Leaf),
                EXTENSION => Ok(Self::Extension),
                _ => Err(ShaleError::InvalidNodeType),
            }
        }
    }
}

use type_id::NodeTypeId;

impl Storable for Node {
    fn deserialize<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let meta_raw =
            mem.get_view(addr, Meta::SIZE as u64)
                .ok_or(ShaleError::InvalidCacheView {
                    offset: addr,
                    size: Meta::SIZE as u64,
                })?;

        let attrs = NodeAttributes::from_bits_retain(meta_raw.as_deref()[TRIE_HASH_LEN]);

        let root_hash = if attrs.contains(NodeAttributes::ROOT_HASH_VALID) {
            Some(TrieHash(
                meta_raw.as_deref()[..TRIE_HASH_LEN]
                    .try_into()
                    .expect("invalid slice"),
            ))
        } else {
            None
        };

        let is_encoded_longer_than_hash_len =
            if attrs.contains(NodeAttributes::IS_ENCODED_BIG_VALID) {
                Some(attrs.contains(NodeAttributes::LONG))
            } else {
                None
            };

        match meta_raw.as_deref()[TRIE_HASH_LEN + 1].try_into()? {
            NodeTypeId::Branch => {
                // TODO: add path
                // TODO: figure out what this size is?
                let branch_header_size = MAX_CHILDREN as u64 * 8 + 4;
                let node_raw = mem.get_view(addr + Meta::SIZE, branch_header_size).ok_or(
                    ShaleError::InvalidCacheView {
                        offset: addr + Meta::SIZE,
                        size: branch_header_size,
                    },
                )?;

                let mut cur = Cursor::new(node_raw.as_deref());
                let mut chd = [None; MAX_CHILDREN];
                let mut buff = [0; 8];

                for chd in chd.iter_mut() {
                    cur.read_exact(&mut buff)?;
                    let addr = usize::from_le_bytes(buff);
                    if addr != 0 {
                        *chd = Some(DiskAddress::from(addr))
                    }
                }

                cur.read_exact(&mut buff[..4])?;

                let raw_len =
                    u32::from_le_bytes(buff[..4].try_into().expect("invalid slice")) as u64;

                let value = if raw_len == u32::MAX as u64 {
                    None
                } else {
                    Some(Data(
                        mem.get_view(addr + Meta::SIZE + branch_header_size as usize, raw_len)
                            .ok_or(ShaleError::InvalidCacheView {
                                offset: addr + Meta::SIZE + branch_header_size as usize,
                                size: raw_len,
                            })?
                            .as_deref(),
                    ))
                };

                let mut chd_encoded: [Option<Vec<u8>>; MAX_CHILDREN] = Default::default();

                let offset = if raw_len == u32::MAX as u64 {
                    addr + Meta::SIZE + branch_header_size as usize
                } else {
                    addr + Meta::SIZE + branch_header_size as usize + raw_len as usize
                };

                let mut cur_encoded_len = 0;

                for chd_encoded in chd_encoded.iter_mut() {
                    let mut buff = [0_u8; 1];
                    let len_raw = mem.get_view(offset + cur_encoded_len, 1).ok_or(
                        ShaleError::InvalidCacheView {
                            offset: offset + cur_encoded_len,
                            size: 1,
                        },
                    )?;

                    cur = Cursor::new(len_raw.as_deref());
                    cur.read_exact(&mut buff)?;

                    let len = buff[0] as u64;
                    cur_encoded_len += 1;

                    if len != 0 {
                        let encoded_raw = mem.get_view(offset + cur_encoded_len, len).ok_or(
                            ShaleError::InvalidCacheView {
                                offset: offset + cur_encoded_len,
                                size: len,
                            },
                        )?;

                        let encoded: Vec<u8> = encoded_raw.as_deref()[0..].to_vec();
                        *chd_encoded = Some(encoded);
                        cur_encoded_len += len as usize
                    }
                }

                let inner = NodeType::Branch(
                    BranchNode {
                        // path: vec![].into(),
                        children: chd,
                        value,
                        children_encoded: chd_encoded,
                    }
                    .into(),
                );

                Ok(Self::new_from_hash(
                    root_hash,
                    is_encoded_longer_than_hash_len,
                    inner,
                ))
            }

            NodeTypeId::Extension => {
                let ext_header_size = 1 + 8;

                let node_raw = mem.get_view(addr + Meta::SIZE, ext_header_size).ok_or(
                    ShaleError::InvalidCacheView {
                        offset: addr + Meta::SIZE,
                        size: ext_header_size,
                    },
                )?;

                let mut cur = Cursor::new(node_raw.as_deref());
                let mut buff = [0; 8];

                cur.read_exact(&mut buff[..1])?;
                let path_len = buff[0] as u64;

                cur.read_exact(&mut buff)?;
                let ptr = u64::from_le_bytes(buff);

                let nibbles: Vec<u8> = mem
                    .get_view(addr + Meta::SIZE + ext_header_size as usize, path_len)
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr + Meta::SIZE + ext_header_size as usize,
                        size: path_len,
                    })?
                    .as_deref()
                    .into_iter()
                    .flat_map(to_nibble_array)
                    .collect();

                let (path, _) = PartialPath::decode(&nibbles);

                let mut buff = [0_u8; 1];

                let encoded_len_raw = mem
                    .get_view(
                        addr + Meta::SIZE + ext_header_size as usize + path_len as usize,
                        1,
                    )
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr + Meta::SIZE + ext_header_size as usize + path_len as usize,
                        size: 1,
                    })?;

                cur = Cursor::new(encoded_len_raw.as_deref());
                cur.read_exact(&mut buff)?;

                let encoded_len = buff[0] as u64;

                let encoded: Option<Vec<u8>> = if encoded_len != 0 {
                    let emcoded_raw = mem
                        .get_view(
                            addr + Meta::SIZE + ext_header_size as usize + path_len as usize + 1,
                            encoded_len,
                        )
                        .ok_or(ShaleError::InvalidCacheView {
                            offset: addr
                                + Meta::SIZE
                                + ext_header_size as usize
                                + path_len as usize
                                + 1,
                            size: encoded_len,
                        })?;

                    Some(emcoded_raw.as_deref()[0..].to_vec())
                } else {
                    None
                };

                let inner = NodeType::Extension(ExtNode {
                    path,
                    child: DiskAddress::from(ptr as usize),
                    child_encoded: encoded,
                });

                let node = Self::new_from_hash(root_hash, is_encoded_longer_than_hash_len, inner);

                Ok(node)
            }

            NodeTypeId::Leaf => {
                let leaf_header_size = 1 + 4;
                let node_raw = mem.get_view(addr + Meta::SIZE, leaf_header_size).ok_or(
                    ShaleError::InvalidCacheView {
                        offset: addr + Meta::SIZE,
                        size: leaf_header_size,
                    },
                )?;

                let mut cur = Cursor::new(node_raw.as_deref());
                let mut buff = [0; 4];
                cur.read_exact(&mut buff[..1])?;

                let path_len = buff[0] as u64;

                cur.read_exact(&mut buff)?;

                let data_len = u32::from_le_bytes(buff) as u64;
                let remainder = mem
                    .get_view(
                        addr + Meta::SIZE + leaf_header_size as usize,
                        path_len + data_len,
                    )
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr + Meta::SIZE + leaf_header_size as usize,
                        size: path_len + data_len,
                    })?;

                let nibbles: Vec<_> = remainder
                    .as_deref()
                    .into_iter()
                    .take(path_len as usize)
                    .flat_map(to_nibble_array)
                    .collect();

                let (path, _) = PartialPath::decode(&nibbles);
                let data = Data(remainder.as_deref()[path_len as usize..].to_vec());

                let node = Self::new_from_hash(
                    root_hash,
                    is_encoded_longer_than_hash_len,
                    NodeType::Leaf(LeafNode { path, data }),
                );

                Ok(node)
            }
        }
    }

    fn serialized_len(&self) -> u64 {
        Meta::SIZE as u64
            + match &self.inner {
                NodeType::Branch(n) => {
                    // TODO: add path
                    let mut encoded_len = 0;
                    for emcoded in n.children_encoded.iter() {
                        encoded_len += match emcoded {
                            Some(v) => 1 + v.len() as u64,
                            None => 1,
                        }
                    }
                    MAX_CHILDREN as u64 * 8
                        + 4
                        + match &n.value {
                            Some(val) => val.len() as u64,
                            None => 0,
                        }
                        + encoded_len
                }
                NodeType::Extension(n) => {
                    1 + 8
                        + n.path.serialized_len()
                        + match n.chd_encoded() {
                            Some(v) => 1 + v.len() as u64,
                            None => 1,
                        }
                }
                NodeType::Leaf(n) => 1 + 4 + n.path.serialized_len() + n.data.len() as u64,
            }
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);

        let mut attrs = match self.root_hash.get() {
            Some(h) => {
                cur.write_all(&h.0)?;
                NodeAttributes::ROOT_HASH_VALID
            }
            None => {
                cur.write_all(&[0; 32])?;
                NodeAttributes::empty()
            }
        };

        if let Some(&b) = self.is_encoded_longer_than_hash_len.get() {
            attrs.insert(if b {
                NodeAttributes::LONG
            } else {
                NodeAttributes::IS_ENCODED_BIG_VALID
            });
        }

        cur.write_all(&[attrs.bits()]).unwrap();

        match &self.inner {
            NodeType::Branch(n) => {
                // TODO: add path
                cur.write_all(&[type_id::NodeTypeId::Branch as u8]).unwrap();

                for c in n.children.iter() {
                    cur.write_all(&match c {
                        Some(p) => p.to_le_bytes(),
                        None => 0u64.to_le_bytes(),
                    })?;
                }

                match &n.value {
                    Some(val) => {
                        cur.write_all(&(val.len() as u32).to_le_bytes())?;
                        cur.write_all(val)?
                    }
                    None => {
                        cur.write_all(&u32::MAX.to_le_bytes())?;
                    }
                }

                // Since child encoding will only be unset after initialization (only used for range proof),
                // it is fine to encode its value adjacent to other fields. Same for extention node.
                for encoded in n.children_encoded.iter() {
                    match encoded {
                        Some(v) => {
                            cur.write_all(&[v.len() as u8])?;
                            cur.write_all(v)?
                        }
                        None => cur.write_all(&0u8.to_le_bytes())?,
                    }
                }

                Ok(())
            }

            NodeType::Extension(n) => {
                cur.write_all(&[type_id::NodeTypeId::Extension as u8])?;

                let path: Vec<u8> = from_nibbles(&n.path.encode(false)).collect();

                cur.write_all(&[path.len() as u8])?;
                cur.write_all(&n.child.to_le_bytes())?;
                cur.write_all(&path)?;

                if let Some(encoded) = n.chd_encoded() {
                    cur.write_all(&[encoded.len() as u8])?;
                    cur.write_all(encoded)?;
                }

                Ok(())
            }

            NodeType::Leaf(n) => {
                cur.write_all(&[type_id::NodeTypeId::Leaf as u8])?;

                let path: Vec<u8> = from_nibbles(&n.path.encode(true)).collect();

                cur.write_all(&[path.len() as u8])?;
                cur.write_all(&(n.data.len() as u32).to_le_bytes())?;
                cur.write_all(&path)?;
                cur.write_all(&n.data).map_err(ShaleError::Io)
            }
        }
    }
}

#[cfg(test)]
pub(super) mod tests {
    use std::array::from_fn;

    use super::*;
    use crate::shale::cached::PlainMem;
    use test_case::test_case;

    pub fn leaf(path: Vec<u8>, data: Vec<u8>) -> Node {
        Node::from_leaf(LeafNode::new(PartialPath(path), Data(data)))
    }

    pub fn branch(
        repeated_disk_address: usize,
        value: Option<Vec<u8>>,
        repeated_encoded_child: Option<Vec<u8>>,
    ) -> Node {
        let children: [Option<DiskAddress>; MAX_CHILDREN] = from_fn(|i| {
            if i < MAX_CHILDREN / 2 {
                DiskAddress::from(repeated_disk_address).into()
            } else {
                None
            }
        });

        let children_encoded = repeated_encoded_child
            .map(|child| {
                from_fn(|i| {
                    if i < MAX_CHILDREN / 2 {
                        child.clone().into()
                    } else {
                        None
                    }
                })
            })
            .unwrap_or_default();

        Node::from_branch(BranchNode {
            // path: vec![].into(),
            children,
            value: value.map(Data),
            children_encoded,
        })
    }

    pub fn extension(
        path: Vec<u8>,
        child_address: DiskAddress,
        child_encoded: Option<Vec<u8>>,
    ) -> Node {
        Node::from(NodeType::Extension(ExtNode {
            path: PartialPath(path),
            child: child_address,
            child_encoded,
        }))
    }

    #[test_case(leaf(vec![0x01, 0x02, 0x03], vec![0x04, 0x05]); "leaf_node")]
    #[test_case(extension(vec![0x01, 0x02, 0x03], DiskAddress::from(0x42), None); "extension with child address")]
    #[test_case(extension(vec![0x01, 0x02, 0x03], DiskAddress::null(), vec![0x01, 0x02, 0x03].into()) ; "extension without child address")]
    #[test_case(branch(0x0a, b"hello world".to_vec().into(), None); "branch with data")]
    #[test_case(branch(0x0a, None, vec![0x01, 0x02, 0x03].into()); "branch without data")]
    fn test_encoding(node: Node) {
        let mut bytes = vec![0; node.serialized_len() as usize];

        node.serialize(&mut bytes).unwrap();

        let mut mem = PlainMem::new(node.serialized_len(), 0x00);
        mem.write(0, &bytes);

        let hydrated_node = Node::deserialize(0, &mem).unwrap();

        assert_eq!(node, hydrated_node);
    }
}
