// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::shale::{disk_address::DiskAddress, CachedStore, ShaleError, ShaleStore, Storable};
use bincode::{Error, Options};
use enum_as_inner::EnumAsInner;
use serde::{de::DeserializeOwned, Deserialize, Serialize};
use sha3::{Digest, Keccak256};
use std::{
    fmt::{self, Debug},
    io::{Cursor, Read, Write},
    sync::{
        atomic::{AtomicBool, Ordering},
        OnceLock,
    },
};

use crate::merkle::to_nibble_array;
use crate::nibbles::Nibbles;

use super::{from_nibbles, PartialPath, TrieHash, TRIE_HASH_LEN};

pub const NBRANCH: usize = 16;

const EXT_NODE_SIZE: usize = 2;
const BRANCH_NODE_SIZE: usize = 17;

#[derive(Debug, PartialEq, Eq, Clone)]
pub struct Data(pub(super) Vec<u8>);

impl std::ops::Deref for Data {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

#[derive(Serialize, Deserialize, Debug)]
pub(crate) enum Encoded<T> {
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

#[derive(PartialEq, Eq, Clone)]
pub struct BranchNode {
    pub(super) children: [Option<DiskAddress>; NBRANCH],
    pub(super) value: Option<Data>,
    pub(super) children_encoded: [Option<Vec<u8>>; NBRANCH],
}

impl Debug for BranchNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Branch")?;
        for (i, c) in self.children.iter().enumerate() {
            if let Some(c) = c {
                write!(f, " ({i:x} {c:?})")?;
            }
        }
        for (i, c) in self.children_encoded.iter().enumerate() {
            if let Some(c) = c {
                write!(f, " ({i:x} {:?})", c)?;
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
    pub(super) fn single_child(&self) -> (Option<(DiskAddress, u8)>, bool) {
        let mut has_chd = false;
        let mut only_chd = None;
        for (i, c) in self.children.iter().enumerate() {
            if c.is_some() {
                has_chd = true;
                if only_chd.is_some() {
                    only_chd = None;
                    break;
                }
                only_chd = (*c).map(|e| (e, i as u8))
            }
        }
        (only_chd, has_chd)
    }

    pub fn decode(buf: &[u8]) -> Result<Self, Error> {
        let mut items: Vec<Encoded<Vec<u8>>> = bincode::DefaultOptions::new().deserialize(buf)?;

        // we've already validated the size, that's why we can safely unwrap
        let data = items.pop().unwrap().decode()?;
        // Extract the value of the branch node and set to None if it's an empty Vec
        let value = Some(data).filter(|data| !data.is_empty());

        // encode all children.
        let mut chd_encoded: [Option<Vec<u8>>; NBRANCH] = Default::default();

        // we popped the last element, so their should only be NBRANCH items left
        for (i, chd) in items.into_iter().enumerate() {
            let data = chd.decode()?;
            chd_encoded[i] = Some(data).filter(|data| !data.is_empty());
        }

        Ok(BranchNode::new([None; NBRANCH], value, chd_encoded))
    }

    fn encode<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        let mut list = <[Encoded<Vec<u8>>; NBRANCH + 1]>::default();

        for (i, c) in self.children.iter().enumerate() {
            match c {
                Some(c) => {
                    let mut c_ref = store.get_item(*c).unwrap();

                    if c_ref.is_encoded_longer_than_hash_len::<S>(store) {
                        list[i] = Encoded::Data(
                            bincode::DefaultOptions::new()
                                .serialize(&&(*c_ref.get_root_hash::<S>(store))[..])
                                .unwrap(),
                        );

                        // See struct docs for ordering requirements
                        if c_ref.lazy_dirty.load(Ordering::Relaxed) {
                            c_ref.write(|_| {}).unwrap();
                            c_ref.lazy_dirty.store(false, Ordering::Relaxed)
                        }
                    } else {
                        let child_encoded = &c_ref.get_encoded::<S>(store);
                        list[i] = Encoded::Raw(child_encoded.to_vec());
                    }
                }
                None => {
                    // Check if there is already a calculated encoded value for the child, which
                    // can happen when manually constructing a trie from proof.
                    if let Some(v) = &self.children_encoded[i] {
                        if v.len() == TRIE_HASH_LEN {
                            list[i] =
                                Encoded::Data(bincode::DefaultOptions::new().serialize(v).unwrap());
                        } else {
                            list[i] = Encoded::Raw(v.clone());
                        }
                    }
                }
            };
        }

        if let Some(Data(val)) = &self.value {
            list[NBRANCH] = Encoded::Data(bincode::DefaultOptions::new().serialize(val).unwrap());
        }

        bincode::DefaultOptions::new()
            .serialize(list.as_slice())
            .unwrap()
    }

    pub fn new(
        chd: [Option<DiskAddress>; NBRANCH],
        value: Option<Vec<u8>>,
        chd_encoded: [Option<Vec<u8>>; NBRANCH],
    ) -> Self {
        BranchNode {
            children: chd,
            value: value.map(Data),
            children_encoded: chd_encoded,
        }
    }

    pub fn value(&self) -> &Option<Data> {
        &self.value
    }

    pub fn chd(&self) -> &[Option<DiskAddress>; NBRANCH] {
        &self.children
    }

    pub fn chd_mut(&mut self) -> &mut [Option<DiskAddress>; NBRANCH] {
        &mut self.children
    }

    pub fn chd_encode(&self) -> &[Option<Vec<u8>>; NBRANCH] {
        &self.children_encoded
    }

    pub fn chd_encoded_mut(&mut self) -> &mut [Option<Vec<u8>>; NBRANCH] {
        &mut self.children_encoded
    }
}

#[derive(PartialEq, Eq, Clone)]
pub struct LeafNode(pub(super) PartialPath, pub(super) Data);

impl Debug for LeafNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Leaf {:?} {}]", self.0, hex::encode(&*self.1))
    }
}

impl LeafNode {
    fn encode(&self) -> Vec<u8> {
        bincode::DefaultOptions::new()
            .serialize(
                [
                    Encoded::Raw(from_nibbles(&self.0.encode(true)).collect()),
                    Encoded::Raw(self.1.to_vec()),
                ]
                .as_slice(),
            )
            .unwrap()
    }

    pub fn new(path: Vec<u8>, data: Vec<u8>) -> Self {
        LeafNode(PartialPath(path), Data(data))
    }

    pub fn path(&self) -> &PartialPath {
        &self.0
    }

    pub fn data(&self) -> &Data {
        &self.1
    }
}

#[derive(PartialEq, Eq, Clone)]
pub struct ExtNode {
    pub(crate) path: PartialPath,
    pub(crate) child: DiskAddress,
    pub(crate) child_encoded: Option<Vec<u8>>,
}

impl Debug for ExtNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        let Self {
            path,
            child,
            child_encoded,
        } = self;
        write!(f, "[Extension {path:?} {child:?} {child_encoded:?}]",)
    }
}

impl ExtNode {
    fn encode<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        let mut list = <[Encoded<Vec<u8>>; 2]>::default();
        list[0] = Encoded::Data(
            bincode::DefaultOptions::new()
                .serialize(&from_nibbles(&self.path.encode(false)).collect::<Vec<_>>())
                .unwrap(),
        );

        if !self.child.is_null() {
            let mut r = store.get_item(self.child).unwrap();

            if r.is_encoded_longer_than_hash_len(store) {
                list[1] = Encoded::Data(
                    bincode::DefaultOptions::new()
                        .serialize(&&(*r.get_root_hash(store))[..])
                        .unwrap(),
                );

                if r.lazy_dirty.load(Ordering::Relaxed) {
                    r.write(|_| {}).unwrap();
                    r.lazy_dirty.store(false, Ordering::Relaxed);
                }
            } else {
                list[1] = Encoded::Raw(r.get_encoded(store).to_vec());
            }
        } else {
            // Check if there is already a caclucated encoded value for the child, which
            // can happen when manually constructing a trie from proof.
            if let Some(v) = &self.child_encoded {
                if v.len() == TRIE_HASH_LEN {
                    list[1] = Encoded::Data(bincode::DefaultOptions::new().serialize(v).unwrap());
                } else {
                    list[1] = Encoded::Raw(v.clone());
                }
            }
        }

        bincode::DefaultOptions::new()
            .serialize(list.as_slice())
            .unwrap()
    }

    pub fn chd(&self) -> DiskAddress {
        self.child
    }

    pub fn chd_encoded(&self) -> Option<&[u8]> {
        self.child_encoded.as_deref()
    }

    pub fn chd_mut(&mut self) -> &mut DiskAddress {
        &mut self.child
    }

    pub fn chd_encoded_mut(&mut self) -> &mut Option<Vec<u8>> {
        &mut self.child_encoded
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
    pub(super) lazy_dirty: AtomicBool,
    pub(super) inner: NodeType,
}

impl Eq for Node {}
impl PartialEq for Node {
    fn eq(&self, other: &Self) -> bool {
        let Node {
            root_hash,
            is_encoded_longer_than_hash_len,
            encoded,
            lazy_dirty,
            inner,
        } = self;
        *root_hash == other.root_hash
            && *is_encoded_longer_than_hash_len == other.is_encoded_longer_than_hash_len
            && *encoded == other.encoded
            && (*lazy_dirty).load(Ordering::Relaxed) == other.lazy_dirty.load(Ordering::Relaxed)
            && *inner == other.inner
    }
}
impl Clone for Node {
    fn clone(&self) -> Self {
        Self {
            root_hash: self.root_hash.clone(),
            is_encoded_longer_than_hash_len: self.is_encoded_longer_than_hash_len.clone(),
            encoded: self.encoded.clone(),
            lazy_dirty: AtomicBool::new(self.lazy_dirty.load(Ordering::Relaxed)),
            inner: self.inner.clone(),
        }
    }
}

#[derive(PartialEq, Eq, Clone, Debug, EnumAsInner)]
#[allow(clippy::large_enum_variant)]
pub enum NodeType {
    Branch(BranchNode),
    Leaf(LeafNode),
    Extension(ExtNode),
}

impl NodeType {
    pub fn decode(buf: &[u8]) -> Result<NodeType, Error> {
        let items: Vec<Encoded<Vec<u8>>> = bincode::DefaultOptions::new().deserialize(buf)?;

        match items.len() {
            EXT_NODE_SIZE => {
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
            BRANCH_NODE_SIZE => Ok(NodeType::Branch(BranchNode::decode(buf)?)),
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

    pub fn path_mut(&mut self) -> Option<&mut PartialPath> {
        let path = match self {
            NodeType::Branch(_) => return None,
            NodeType::Leaf(node) => &mut node.0,
            NodeType::Extension(node) => &mut node.path,
        };

        path.into()
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

impl Node {
    const BRANCH_NODE: u8 = 0x0;
    const EXT_NODE: u8 = 0x1;
    const LEAF_NODE: u8 = 0x2;

    pub(super) fn max_branch_node_size() -> u64 {
        let max_size: OnceLock<u64> = OnceLock::new();
        *max_size.get_or_init(|| {
            Self {
                root_hash: OnceLock::new(),
                is_encoded_longer_than_hash_len: OnceLock::new(),
                encoded: OnceLock::new(),
                inner: NodeType::Branch(BranchNode {
                    children: [Some(DiskAddress::null()); NBRANCH],
                    value: Some(Data(Vec::new())),
                    children_encoded: Default::default(),
                }),
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
            self.lazy_dirty.store(true, Ordering::Relaxed);
            TrieHash(Keccak256::digest(self.get_encoded::<S>(store)).into())
        })
    }

    fn is_encoded_longer_than_hash_len<S: ShaleStore<Node>>(&self, store: &S) -> bool {
        *self.is_encoded_longer_than_hash_len.get_or_init(|| {
            self.lazy_dirty.store(true, Ordering::Relaxed);
            self.get_encoded(store).len() >= TRIE_HASH_LEN
        })
    }

    pub(super) fn rehash(&mut self) {
        self.encoded = OnceLock::new();
        self.is_encoded_longer_than_hash_len = OnceLock::new();
        self.root_hash = OnceLock::new();
    }

    pub fn branch(node: BranchNode) -> Self {
        Self::from(NodeType::Branch(node))
    }

    pub fn leaf(path: PartialPath, data: Data) -> Self {
        Self::from(NodeType::Leaf(LeafNode(path, data)))
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

    const ROOT_HASH_VALID_BIT: u8 = 1 << 0;
    // TODO: why are these different?
    const IS_ENCODED_BIG_VALID: u8 = 1 << 1;
    const LONG_BIT: u8 = 1 << 2;
}

impl Storable for Node {
    fn deserialize<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        const META_SIZE: usize = 32 + 1 + 1;
        let meta_raw =
            mem.get_view(addr, META_SIZE as u64)
                .ok_or(ShaleError::InvalidCacheView {
                    offset: addr,
                    size: META_SIZE as u64,
                })?;
        let attrs = meta_raw.as_deref()[32];
        let root_hash = if attrs & Node::ROOT_HASH_VALID_BIT == 0 {
            None
        } else {
            Some(TrieHash(
                meta_raw.as_deref()[0..32]
                    .try_into()
                    .expect("invalid slice"),
            ))
        };
        let is_encoded_longer_than_hash_len = if attrs & Node::IS_ENCODED_BIG_VALID == 0 {
            None
        } else {
            Some(attrs & Node::LONG_BIT != 0)
        };
        match meta_raw.as_deref()[33] {
            Self::BRANCH_NODE => {
                let branch_header_size = NBRANCH as u64 * 8 + 4;
                let node_raw = mem.get_view(addr + META_SIZE, branch_header_size).ok_or(
                    ShaleError::InvalidCacheView {
                        offset: addr + META_SIZE,
                        size: branch_header_size,
                    },
                )?;
                let mut cur = Cursor::new(node_raw.as_deref());
                let mut chd = [None; NBRANCH];
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
                        mem.get_view(addr + META_SIZE + branch_header_size as usize, raw_len)
                            .ok_or(ShaleError::InvalidCacheView {
                                offset: addr + META_SIZE + branch_header_size as usize,
                                size: raw_len,
                            })?
                            .as_deref(),
                    ))
                };
                let mut chd_encoded: [Option<Vec<u8>>; NBRANCH] = Default::default();
                let offset = if raw_len == u32::MAX as u64 {
                    addr + META_SIZE + branch_header_size as usize
                } else {
                    addr + META_SIZE + branch_header_size as usize + raw_len as usize
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

                Ok(Self::new_from_hash(
                    root_hash,
                    is_encoded_longer_than_hash_len,
                    NodeType::Branch(BranchNode {
                        children: chd,
                        value,
                        children_encoded: chd_encoded,
                    }),
                ))
            }
            Self::EXT_NODE => {
                let ext_header_size = 1 + 8;
                let node_raw = mem.get_view(addr + META_SIZE, ext_header_size).ok_or(
                    ShaleError::InvalidCacheView {
                        offset: addr + META_SIZE,
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
                    .get_view(addr + META_SIZE + ext_header_size as usize, path_len)
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr + META_SIZE + ext_header_size as usize,
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
                        addr + META_SIZE + ext_header_size as usize + path_len as usize,
                        1,
                    )
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr + META_SIZE + ext_header_size as usize + path_len as usize,
                        size: 1,
                    })?;
                cur = Cursor::new(encoded_len_raw.as_deref());
                cur.read_exact(&mut buff)?;
                let encoded_len = buff[0] as u64;
                let encoded: Option<Vec<u8>> = if encoded_len != 0 {
                    let emcoded_raw = mem
                        .get_view(
                            addr + META_SIZE + ext_header_size as usize + path_len as usize + 1,
                            encoded_len,
                        )
                        .ok_or(ShaleError::InvalidCacheView {
                            offset: addr
                                + META_SIZE
                                + ext_header_size as usize
                                + path_len as usize
                                + 1,
                            size: encoded_len,
                        })?;

                    Some(emcoded_raw.as_deref()[0..].to_vec())
                } else {
                    None
                };

                Ok(Self::new_from_hash(
                    root_hash,
                    is_encoded_longer_than_hash_len,
                    NodeType::Extension(ExtNode {
                        path,
                        child: DiskAddress::from(ptr as usize),
                        child_encoded: encoded,
                    }),
                ))
            }
            Self::LEAF_NODE => {
                let leaf_header_size = 1 + 4;
                let node_raw = mem.get_view(addr + META_SIZE, leaf_header_size).ok_or(
                    ShaleError::InvalidCacheView {
                        offset: addr + META_SIZE,
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
                        addr + META_SIZE + leaf_header_size as usize,
                        path_len + data_len,
                    )
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr + META_SIZE + leaf_header_size as usize,
                        size: path_len + data_len,
                    })?;

                let nibbles: Vec<_> = remainder
                    .as_deref()
                    .into_iter()
                    .take(path_len as usize)
                    .flat_map(to_nibble_array)
                    .collect();

                let (path, _) = PartialPath::decode(&nibbles);
                let value = Data(remainder.as_deref()[path_len as usize..].to_vec());
                Ok(Self::new_from_hash(
                    root_hash,
                    is_encoded_longer_than_hash_len,
                    NodeType::Leaf(LeafNode(path, value)),
                ))
            }
            _ => Err(ShaleError::InvalidNodeType),
        }
    }

    fn serialized_len(&self) -> u64 {
        32 + 1
            + 1
            + match &self.inner {
                NodeType::Branch(n) => {
                    let mut encoded_len = 0;
                    for emcoded in n.children_encoded.iter() {
                        encoded_len += match emcoded {
                            Some(v) => 1 + v.len() as u64,
                            None => 1,
                        }
                    }
                    NBRANCH as u64 * 8
                        + 4
                        + match &n.value {
                            Some(val) => val.len() as u64,
                            None => 0,
                        }
                        + encoded_len
                }
                NodeType::Extension(n) => {
                    1 + 8
                        + n.path.dehydrated_len()
                        + match n.chd_encoded() {
                            Some(v) => 1 + v.len() as u64,
                            None => 1,
                        }
                }
                NodeType::Leaf(n) => 1 + 4 + n.0.dehydrated_len() + n.1.len() as u64,
            }
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        let mut cur = Cursor::new(to);

        let mut attrs = 0;
        attrs |= match self.root_hash.get() {
            Some(h) => {
                cur.write_all(&h.0)?;
                Node::ROOT_HASH_VALID_BIT
            }
            None => {
                cur.write_all(&[0; 32])?;
                0
            }
        };
        attrs |= match self.is_encoded_longer_than_hash_len.get() {
            Some(b) => (if *b { Node::LONG_BIT } else { 0 } | Node::IS_ENCODED_BIG_VALID),
            None => 0,
        };
        cur.write_all(&[attrs]).unwrap();

        match &self.inner {
            NodeType::Branch(n) => {
                cur.write_all(&[Self::BRANCH_NODE]).unwrap();
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
                cur.write_all(&[Self::EXT_NODE])?;
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
                cur.write_all(&[Self::LEAF_NODE])?;
                let path: Vec<u8> = from_nibbles(&n.0.encode(true)).collect();
                cur.write_all(&[path.len() as u8])?;
                cur.write_all(&(n.1.len() as u32).to_le_bytes())?;
                cur.write_all(&path)?;
                cur.write_all(&n.1).map_err(ShaleError::Io)
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
        Node::leaf(PartialPath(path), Data(data))
    }

    pub fn branch(
        repeated_disk_address: usize,
        value: Option<Vec<u8>>,
        repeated_encoded_child: Option<Vec<u8>>,
    ) -> Node {
        let children: [Option<DiskAddress>; NBRANCH] = from_fn(|i| {
            if i < NBRANCH / 2 {
                DiskAddress::from(repeated_disk_address).into()
            } else {
                None
            }
        });

        let children_encoded = repeated_encoded_child
            .map(|child| {
                from_fn(|i| {
                    if i < NBRANCH / 2 {
                        child.clone().into()
                    } else {
                        None
                    }
                })
            })
            .unwrap_or_default();

        Node::branch(BranchNode {
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
