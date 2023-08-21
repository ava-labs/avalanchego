// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::proof::Proof;
use enum_as_inner::EnumAsInner;
use sha3::Digest;
use shale::{disk_address::DiskAddress, CachedStore, ObjRef, ShaleError, ShaleStore, Storable};
use std::{
    collections::HashMap,
    error::Error,
    fmt::{self, Debug},
    io::{Cursor, Read, Write},
    iter,
    sync::{
        atomic::{AtomicBool, Ordering},
        OnceLock,
    },
};

pub const NBRANCH: usize = 16;
pub const TRIE_HASH_LEN: usize = 32;

#[derive(Debug)]
pub enum MerkleError {
    Shale(ShaleError),
    ReadOnly,
    NotBranchNode,
    Format(std::io::Error),
    ParentLeafBranch,
    UnsetInternal,
}

impl fmt::Display for MerkleError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            MerkleError::Shale(e) => write!(f, "merkle datastore error: {e:?}"),
            MerkleError::ReadOnly => write!(f, "error: read only"),
            MerkleError::NotBranchNode => write!(f, "error: node is not a branch node"),
            MerkleError::Format(e) => write!(f, "format error: {e:?}"),
            MerkleError::ParentLeafBranch => write!(f, "parent should not be a leaf branch"),
            MerkleError::UnsetInternal => write!(f, "removing internal node references failed"),
        }
    }
}

impl Error for MerkleError {}

#[derive(PartialEq, Eq, Clone)]
pub struct TrieHash(pub [u8; TRIE_HASH_LEN]);

impl TrieHash {
    const MSIZE: u64 = 32;
}

impl std::ops::Deref for TrieHash {
    type Target = [u8; TRIE_HASH_LEN];
    fn deref(&self) -> &[u8; TRIE_HASH_LEN] {
        &self.0
    }
}

impl Storable for TrieHash {
    fn hydrate<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        Ok(Self(
            raw.as_deref()[..Self::MSIZE as usize].try_into().unwrap(),
        ))
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        Cursor::new(to).write_all(&self.0).map_err(ShaleError::Io)
    }
}

impl Debug for TrieHash {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "{}", hex::encode(self.0))
    }
}

/// PartialPath keeps a list of nibbles to represent a path on the Trie.
#[derive(PartialEq, Eq, Clone)]
pub struct PartialPath(Vec<u8>);

impl Debug for PartialPath {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        for nib in self.0.iter() {
            write!(f, "{:x}", *nib & 0xf)?;
        }
        Ok(())
    }
}

impl std::ops::Deref for PartialPath {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

impl PartialPath {
    pub fn into_inner(self) -> Vec<u8> {
        self.0
    }

    fn encode(&self, term: bool) -> Vec<u8> {
        let odd_len = (self.0.len() & 1) as u8;
        let flags = if term { 2 } else { 0 } + odd_len;
        let mut res = if odd_len == 1 {
            vec![flags]
        } else {
            vec![flags, 0x0]
        };
        res.extend(&self.0);
        res
    }

    pub fn decode<R: AsRef<[u8]>>(raw: R) -> (Self, bool) {
        let raw = raw.as_ref();
        let term = raw[0] > 1;
        let odd_len = raw[0] & 1;
        (
            Self(if odd_len == 1 {
                raw[1..].to_vec()
            } else {
                raw[2..].to_vec()
            }),
            term,
        )
    }

    fn dehydrated_len(&self) -> u64 {
        let len = self.0.len() as u64;
        if len & 1 == 1 {
            (len + 1) >> 1
        } else {
            (len >> 1) + 1
        }
    }
}

#[derive(Debug, PartialEq, Eq, Clone)]
pub struct Data(Vec<u8>);

impl std::ops::Deref for Data {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

#[derive(PartialEq, Eq, Clone)]
pub struct BranchNode {
    chd: [Option<DiskAddress>; NBRANCH],
    value: Option<Data>,
    chd_eth_rlp: [Option<Vec<u8>>; NBRANCH],
}

impl Debug for BranchNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Branch")?;
        for (i, c) in self.chd.iter().enumerate() {
            if let Some(c) = c {
                write!(f, " ({i:x} {c:?})")?;
            }
        }
        for (i, c) in self.chd_eth_rlp.iter().enumerate() {
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
    fn single_child(&self) -> (Option<(DiskAddress, u8)>, bool) {
        let mut has_chd = false;
        let mut only_chd = None;
        for (i, c) in self.chd.iter().enumerate() {
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

    fn calc_eth_rlp<T: ValueTransformer, S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        let mut stream = rlp::RlpStream::new_list(17);
        for (i, c) in self.chd.iter().enumerate() {
            match c {
                Some(c) => {
                    let mut c_ref = store.get_item(*c).unwrap();
                    if c_ref.get_eth_rlp_long::<T, S>(store) {
                        stream.append(&&(*c_ref.get_root_hash::<T, S>(store))[..]);
                        // See struct docs for ordering requirements
                        if c_ref.lazy_dirty.load(Ordering::Relaxed) {
                            c_ref.write(|_| {}).unwrap();
                            c_ref.lazy_dirty.store(false, Ordering::Relaxed)
                        }
                    } else {
                        let c_rlp = &c_ref.get_eth_rlp::<T, S>(store);
                        stream.append_raw(c_rlp, 1);
                    }
                }
                None => {
                    // Check if there is already a calculated rlp for the child, which
                    // can happen when manually constructing a trie from proof.
                    if self.chd_eth_rlp[i].is_none() {
                        stream.append_empty_data();
                    } else {
                        let v = self.chd_eth_rlp[i].clone().unwrap();
                        if v.len() == TRIE_HASH_LEN {
                            stream.append(&v);
                        } else {
                            stream.append_raw(&v, 1);
                        }
                    }
                }
            };
        }
        match &self.value {
            Some(val) => stream.append(&val.to_vec()),
            None => stream.append_empty_data(),
        };
        stream.out().into()
    }

    pub fn new(
        chd: [Option<DiskAddress>; NBRANCH],
        value: Option<Vec<u8>>,
        chd_eth_rlp: [Option<Vec<u8>>; NBRANCH],
    ) -> Self {
        BranchNode {
            chd,
            value: value.map(Data),
            chd_eth_rlp,
        }
    }

    pub fn value(&self) -> &Option<Data> {
        &self.value
    }

    pub fn chd(&self) -> &[Option<DiskAddress>; NBRANCH] {
        &self.chd
    }

    pub fn chd_mut(&mut self) -> &mut [Option<DiskAddress>; NBRANCH] {
        &mut self.chd
    }

    pub fn chd_eth_rlp(&self) -> &[Option<Vec<u8>>; NBRANCH] {
        &self.chd_eth_rlp
    }

    pub fn chd_eth_rlp_mut(&mut self) -> &mut [Option<Vec<u8>>; NBRANCH] {
        &mut self.chd_eth_rlp
    }
}

#[derive(PartialEq, Eq, Clone)]
pub struct LeafNode(PartialPath, Data);

impl Debug for LeafNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Leaf {:?} {}]", self.0, hex::encode(&*self.1))
    }
}

impl LeafNode {
    fn calc_eth_rlp<T: ValueTransformer>(&self) -> Vec<u8> {
        rlp::encode_list::<Vec<u8>, _>(&[
            from_nibbles(&self.0.encode(true)).collect(),
            T::transform(&self.1),
        ])
        .into()
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
pub struct ExtNode(PartialPath, DiskAddress, Option<Vec<u8>>);

impl Debug for ExtNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Extension {:?} {:?} {:?}]", self.0, self.1, self.2)
    }
}

impl ExtNode {
    fn calc_eth_rlp<T: ValueTransformer, S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        let mut stream = rlp::RlpStream::new_list(2);
        if !self.1.is_null() {
            let mut r = store.get_item(self.1).unwrap();
            stream.append(&from_nibbles(&self.0.encode(false)).collect::<Vec<_>>());
            if r.get_eth_rlp_long::<T, S>(store) {
                stream.append(&&(*r.get_root_hash::<T, S>(store))[..]);
                if r.lazy_dirty.load(Ordering::Relaxed) {
                    r.write(|_| {}).unwrap();
                    r.lazy_dirty.store(false, Ordering::Relaxed);
                }
            } else {
                stream.append_raw(r.get_eth_rlp::<T, S>(store), 1);
            }
        } else {
            // Check if there is already a caclucated rlp for the child, which
            // can happen when manually constructing a trie from proof.
            if self.2.is_none() {
                stream.append_empty_data();
            } else {
                let v = self.2.clone().unwrap();
                if v.len() == TRIE_HASH_LEN {
                    stream.append(&v);
                } else {
                    stream.append_raw(&v, 1);
                }
            }
        }
        stream.out().into()
    }

    pub fn new(path: Vec<u8>, chd: DiskAddress, chd_eth_rlp: Option<Vec<u8>>) -> Self {
        ExtNode(PartialPath(path), chd, chd_eth_rlp)
    }

    pub fn path(&self) -> &PartialPath {
        &self.0
    }

    pub fn chd(&self) -> DiskAddress {
        self.1
    }

    pub fn chd_mut(&mut self) -> &mut DiskAddress {
        &mut self.1
    }

    pub fn chd_eth_rlp_mut(&mut self) -> &mut Option<Vec<u8>> {
        &mut self.2
    }
}

#[derive(Debug)]
pub struct Node {
    root_hash: OnceLock<TrieHash>,
    eth_rlp_long: OnceLock<bool>,
    eth_rlp: OnceLock<Vec<u8>>,
    // lazy_dirty is an atomicbool, but only writers ever set it
    // Therefore, we can always use Relaxed ordering. It's atomic
    // just to ensure Sync + Send.
    lazy_dirty: AtomicBool,
    inner: NodeType,
}

impl Eq for Node {}
impl PartialEq for Node {
    fn eq(&self, other: &Self) -> bool {
        let Node {
            root_hash,
            eth_rlp_long,
            eth_rlp,
            lazy_dirty,
            inner,
        } = self;
        *root_hash == other.root_hash
            && *eth_rlp_long == other.eth_rlp_long
            && *eth_rlp == other.eth_rlp
            && (*lazy_dirty).load(Ordering::Relaxed) == other.lazy_dirty.load(Ordering::Relaxed)
            && *inner == other.inner
    }
}
impl Clone for Node {
    fn clone(&self) -> Self {
        Self {
            root_hash: self.root_hash.clone(),
            eth_rlp_long: self.eth_rlp_long.clone(),
            eth_rlp: self.eth_rlp.clone(),
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
    fn calc_eth_rlp<T: ValueTransformer, S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        match &self {
            NodeType::Leaf(n) => n.calc_eth_rlp::<T>(),
            NodeType::Extension(n) => n.calc_eth_rlp::<T, S>(store),
            NodeType::Branch(n) => n.calc_eth_rlp::<T, S>(store),
        }
    }
}

impl Node {
    const BRANCH_NODE: u8 = 0x0;
    const EXT_NODE: u8 = 0x1;
    const LEAF_NODE: u8 = 0x2;

    fn max_branch_node_size() -> u64 {
        let max_size: OnceLock<u64> = OnceLock::new();
        *max_size.get_or_init(|| {
            Self {
                root_hash: OnceLock::new(),
                eth_rlp_long: OnceLock::new(),
                eth_rlp: OnceLock::new(),
                inner: NodeType::Branch(BranchNode {
                    chd: [Some(DiskAddress::null()); NBRANCH],
                    value: Some(Data(Vec::new())),
                    chd_eth_rlp: Default::default(),
                }),
                lazy_dirty: AtomicBool::new(false),
            }
            .dehydrated_len()
        })
    }

    fn get_eth_rlp<T: ValueTransformer, S: ShaleStore<Node>>(&self, store: &S) -> &[u8] {
        self.eth_rlp
            .get_or_init(|| self.inner.calc_eth_rlp::<T, S>(store))
    }

    fn get_root_hash<T: ValueTransformer, S: ShaleStore<Node>>(&self, store: &S) -> &TrieHash {
        self.root_hash.get_or_init(|| {
            self.lazy_dirty.store(true, Ordering::Relaxed);
            TrieHash(sha3::Keccak256::digest(self.get_eth_rlp::<T, S>(store)).into())
        })
    }

    fn get_eth_rlp_long<T: ValueTransformer, S: ShaleStore<Node>>(&self, store: &S) -> bool {
        *self.eth_rlp_long.get_or_init(|| {
            self.lazy_dirty.store(true, Ordering::Relaxed);
            self.get_eth_rlp::<T, S>(store).len() >= TRIE_HASH_LEN
        })
    }

    fn rehash(&mut self) {
        self.eth_rlp = OnceLock::new();
        self.eth_rlp_long = OnceLock::new();
        self.root_hash = OnceLock::new();
    }

    pub fn new(inner: NodeType) -> Self {
        let mut s = Self {
            root_hash: OnceLock::new(),
            eth_rlp_long: OnceLock::new(),
            eth_rlp: OnceLock::new(),
            inner,
            lazy_dirty: AtomicBool::new(false),
        };
        s.rehash();
        s
    }

    pub fn inner(&self) -> &NodeType {
        &self.inner
    }

    pub fn inner_mut(&mut self) -> &mut NodeType {
        &mut self.inner
    }

    fn new_from_hash(
        root_hash: Option<TrieHash>,
        eth_rlp_long: Option<bool>,
        inner: NodeType,
    ) -> Self {
        Self {
            root_hash: match root_hash {
                Some(h) => OnceLock::from(h),
                None => OnceLock::new(),
            },
            eth_rlp_long: match eth_rlp_long {
                Some(b) => OnceLock::from(b),
                None => OnceLock::new(),
            },
            eth_rlp: OnceLock::new(),
            inner,
            lazy_dirty: AtomicBool::new(false),
        }
    }

    const ROOT_HASH_VALID_BIT: u8 = 1 << 0;
    const ETH_RLP_LONG_VALID_BIT: u8 = 1 << 1;
    const ETH_RLP_LONG_BIT: u8 = 1 << 2;
}

impl Storable for Node {
    fn hydrate<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
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
        let eth_rlp_long = if attrs & Node::ETH_RLP_LONG_VALID_BIT == 0 {
            None
        } else {
            Some(attrs & Node::ETH_RLP_LONG_BIT != 0)
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
                let mut chd_eth_rlp: [Option<Vec<u8>>; NBRANCH] = Default::default();
                let offset = if raw_len == u32::MAX as u64 {
                    addr + META_SIZE + branch_header_size as usize
                } else {
                    addr + META_SIZE + branch_header_size as usize + raw_len as usize
                };
                let mut cur_rlp_len = 0;
                for chd_rlp in chd_eth_rlp.iter_mut() {
                    let mut buff = [0_u8; 1];
                    let rlp_len_raw = mem.get_view(offset + cur_rlp_len, 1).ok_or(
                        ShaleError::InvalidCacheView {
                            offset: offset + cur_rlp_len,
                            size: 1,
                        },
                    )?;
                    cur = Cursor::new(rlp_len_raw.as_deref());
                    cur.read_exact(&mut buff)?;
                    let rlp_len = buff[0] as u64;
                    cur_rlp_len += 1;
                    if rlp_len != 0 {
                        let rlp_raw = mem.get_view(offset + cur_rlp_len, rlp_len).ok_or(
                            ShaleError::InvalidCacheView {
                                offset: offset + cur_rlp_len,
                                size: rlp_len,
                            },
                        )?;
                        let rlp: Vec<u8> = rlp_raw.as_deref()[0..].to_vec();
                        *chd_rlp = Some(rlp);
                        cur_rlp_len += rlp_len as usize
                    }
                }

                Ok(Self::new_from_hash(
                    root_hash,
                    eth_rlp_long,
                    NodeType::Branch(BranchNode {
                        chd,
                        value,
                        chd_eth_rlp,
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

                let (path, _) = PartialPath::decode(nibbles);

                let mut buff = [0_u8; 1];
                let rlp_len_raw = mem
                    .get_view(
                        addr + META_SIZE + ext_header_size as usize + path_len as usize,
                        1,
                    )
                    .ok_or(ShaleError::InvalidCacheView {
                        offset: addr + META_SIZE + ext_header_size as usize + path_len as usize,
                        size: 1,
                    })?;
                cur = Cursor::new(rlp_len_raw.as_deref());
                cur.read_exact(&mut buff)?;
                let rlp_len = buff[0] as u64;
                let rlp: Option<Vec<u8>> = if rlp_len != 0 {
                    let rlp_raw = mem
                        .get_view(
                            addr + META_SIZE + ext_header_size as usize + path_len as usize + 1,
                            rlp_len,
                        )
                        .ok_or(ShaleError::InvalidCacheView {
                            offset: addr
                                + META_SIZE
                                + ext_header_size as usize
                                + path_len as usize
                                + 1,
                            size: rlp_len,
                        })?;

                    Some(rlp_raw.as_deref()[0..].to_vec())
                } else {
                    None
                };

                Ok(Self::new_from_hash(
                    root_hash,
                    eth_rlp_long,
                    NodeType::Extension(ExtNode(path, DiskAddress::from(ptr as usize), rlp)),
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

                let (path, _) = PartialPath::decode(nibbles);
                let value = Data(remainder.as_deref()[path_len as usize..].to_vec());
                Ok(Self::new_from_hash(
                    root_hash,
                    eth_rlp_long,
                    NodeType::Leaf(LeafNode(path, value)),
                ))
            }
            _ => Err(ShaleError::InvalidNodeType),
        }
    }

    fn dehydrated_len(&self) -> u64 {
        32 + 1
            + 1
            + match &self.inner {
                NodeType::Branch(n) => {
                    let mut rlp_len = 0;
                    for rlp in n.chd_eth_rlp.iter() {
                        rlp_len += match rlp {
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
                        + rlp_len
                }
                NodeType::Extension(n) => {
                    1 + 8
                        + n.0.dehydrated_len()
                        + match &n.2 {
                            Some(v) => 1 + v.len() as u64,
                            None => 1,
                        }
                }
                NodeType::Leaf(n) => 1 + 4 + n.0.dehydrated_len() + n.1.len() as u64,
            }
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
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
        attrs |= match self.eth_rlp_long.get() {
            Some(b) => (if *b { Node::ETH_RLP_LONG_BIT } else { 0 } | Node::ETH_RLP_LONG_VALID_BIT),
            None => 0,
        };
        cur.write_all(&[attrs]).unwrap();

        match &self.inner {
            NodeType::Branch(n) => {
                cur.write_all(&[Self::BRANCH_NODE]).unwrap();
                for c in n.chd.iter() {
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
                // Since child eth rlp will only be unset after initialization (only used for range proof),
                // it is fine to encode its value adjacent to other fields. Same for extention node.
                for rlp in n.chd_eth_rlp.iter() {
                    match rlp {
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
                let path: Vec<u8> = from_nibbles(&n.0.encode(false)).collect();
                cur.write_all(&[path.len() as u8])?;
                cur.write_all(&n.1.to_le_bytes())?;
                cur.write_all(&path)?;
                if n.2.is_some() {
                    let rlp = n.2.as_ref().unwrap();
                    cur.write_all(&[rlp.len() as u8])?;
                    cur.write_all(rlp)?;
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

macro_rules! write_node {
    ($self: expr, $r: expr, $modify: expr, $parents: expr, $deleted: expr) => {
        if let Err(_) = $r.write($modify) {
            let ptr = $self.new_node($r.clone())?.as_ptr();
            $self.set_parent(ptr, $parents);
            $deleted.push($r.as_ptr());
            true
        } else {
            false
        }
    };
}

#[derive(Debug)]
pub struct Merkle<S> {
    store: Box<S>,
}

impl<S: ShaleStore<Node> + Send + Sync> Merkle<S> {
    pub fn get_node(&self, ptr: DiskAddress) -> Result<ObjRef<Node>, MerkleError> {
        self.store.get_item(ptr).map_err(MerkleError::Shale)
    }
    pub fn new_node(&self, item: Node) -> Result<ObjRef<Node>, MerkleError> {
        self.store.put_item(item, 0).map_err(MerkleError::Shale)
    }
    fn free_node(&mut self, ptr: DiskAddress) -> Result<(), MerkleError> {
        self.store.free_item(ptr).map_err(MerkleError::Shale)
    }
}

impl<S: ShaleStore<Node> + Send + Sync> Merkle<S> {
    pub fn new(store: Box<S>) -> Self {
        Self { store }
    }

    pub fn init_root(&self, root: &mut DiskAddress) -> Result<(), MerkleError> {
        *root = self
            .store
            .put_item(
                Node::new(NodeType::Branch(BranchNode {
                    chd: [None; NBRANCH],
                    value: None,
                    chd_eth_rlp: Default::default(),
                })),
                Node::max_branch_node_size(),
            )
            .map_err(MerkleError::Shale)?
            .as_ptr();
        Ok(())
    }

    pub fn get_store(&self) -> &dyn ShaleStore<Node> {
        self.store.as_ref()
    }

    pub fn empty_root() -> &'static TrieHash {
        static V: OnceLock<TrieHash> = OnceLock::new();
        V.get_or_init(|| {
            TrieHash(
                hex::decode("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
                    .unwrap()
                    .try_into()
                    .unwrap(),
            )
        })
    }

    pub fn root_hash<T: ValueTransformer>(
        &self,
        root: DiskAddress,
    ) -> Result<TrieHash, MerkleError> {
        let root = self
            .get_node(root)?
            .inner
            .as_branch()
            .ok_or(MerkleError::NotBranchNode)?
            .chd[0];
        Ok(if let Some(root) = root {
            let mut node = self.get_node(root)?;
            let res = node.get_root_hash::<T, S>(self.store.as_ref()).clone();
            if node.lazy_dirty.load(Ordering::Relaxed) {
                node.write(|_| {}).unwrap();
                node.lazy_dirty.store(false, Ordering::Relaxed);
            }
            res
        } else {
            Self::empty_root().clone()
        })
    }

    fn dump_(&self, u: DiskAddress, w: &mut dyn Write) -> Result<(), MerkleError> {
        let u_ref = self.get_node(u)?;
        write!(
            w,
            "{u:?} => {}: ",
            match u_ref.root_hash.get() {
                Some(h) => hex::encode(**h),
                None => "<lazy>".to_string(),
            }
        )
        .map_err(MerkleError::Format)?;
        match &u_ref.inner {
            NodeType::Branch(n) => {
                writeln!(w, "{n:?}").map_err(MerkleError::Format)?;
                for c in n.chd.iter().flatten() {
                    self.dump_(*c, w)?
                }
            }
            NodeType::Leaf(n) => writeln!(w, "{n:?}").unwrap(),
            NodeType::Extension(n) => {
                writeln!(w, "{n:?}").map_err(MerkleError::Format)?;
                self.dump_(n.1, w)?
            }
        }
        Ok(())
    }

    pub fn dump(&self, root: DiskAddress, w: &mut dyn Write) -> Result<(), MerkleError> {
        if root.is_null() {
            write!(w, "<Empty>").map_err(MerkleError::Format)?;
        } else {
            self.dump_(root, w)?;
        };
        Ok(())
    }

    fn set_parent(&self, new_chd: DiskAddress, parents: &mut [(ObjRef<'_, Node>, u8)]) {
        let (p_ref, idx) = parents.last_mut().unwrap();
        p_ref
            .write(|p| {
                match &mut p.inner {
                    NodeType::Branch(pp) => pp.chd[*idx as usize] = Some(new_chd),
                    NodeType::Extension(pp) => pp.1 = new_chd,
                    _ => unreachable!(),
                }
                p.rehash();
            })
            .unwrap();
    }

    #[allow(clippy::too_many_arguments)]
    fn split<'b>(
        &self,
        mut u_ref: ObjRef<'b, Node>,
        parents: &mut [(ObjRef<'b, Node>, u8)],
        rem_path: &[u8],
        n_path: Vec<u8>,
        n_value: Option<Data>,
        val: Vec<u8>,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<Option<Vec<u8>>, MerkleError> {
        let u_ptr = u_ref.as_ptr();
        let new_chd = match rem_path.iter().zip(n_path.iter()).position(|(a, b)| a != b) {
            Some(idx) => {
                //                                                      _ [u (new path)]
                //                                                     /
                //  [parent] (-> [ExtNode (common prefix)]) -> [branch]*
                //                                                     \_ [leaf (with val)]
                u_ref
                    .write(|u| {
                        (*match &mut u.inner {
                            NodeType::Leaf(u) => &mut u.0,
                            NodeType::Extension(u) => &mut u.0,
                            _ => unreachable!(),
                        }) = PartialPath(n_path[idx + 1..].to_vec());
                        u.rehash();
                    })
                    .unwrap();
                let leaf_ptr = self
                    .new_node(Node::new(NodeType::Leaf(LeafNode(
                        PartialPath(rem_path[idx + 1..].to_vec()),
                        Data(val),
                    ))))?
                    .as_ptr();
                let mut chd = [None; NBRANCH];
                chd[rem_path[idx] as usize] = Some(leaf_ptr);
                chd[n_path[idx] as usize] = Some(match &u_ref.inner {
                    NodeType::Extension(u) => {
                        if u.0.len() == 0 {
                            deleted.push(u_ptr);
                            u.1
                        } else {
                            u_ptr
                        }
                    }
                    _ => u_ptr,
                });
                drop(u_ref);
                let t = NodeType::Branch(BranchNode {
                    chd,
                    value: None,
                    chd_eth_rlp: Default::default(),
                });
                let branch_ptr = self.new_node(Node::new(t))?.as_ptr();
                if idx > 0 {
                    self.new_node(Node::new(NodeType::Extension(ExtNode(
                        PartialPath(rem_path[..idx].to_vec()),
                        branch_ptr,
                        None,
                    ))))?
                    .as_ptr()
                } else {
                    branch_ptr
                }
            }
            None => {
                if rem_path.len() == n_path.len() {
                    let mut result = Ok(None);

                    write_node!(
                        self,
                        u_ref,
                        |u| {
                            match &mut u.inner {
                                NodeType::Leaf(u) => u.1 = Data(val),
                                NodeType::Extension(u) => {
                                    let write_result = self.get_node(u.1).and_then(|mut b_ref| {
                                        let write_result = b_ref.write(|b| {
                                            b.inner.as_branch_mut().unwrap().value =
                                                Some(Data(val));
                                            b.rehash()
                                        });

                                        if write_result.is_err() {
                                            u.1 = self.new_node(b_ref.clone())?.as_ptr();
                                            deleted.push(b_ref.as_ptr());
                                        }

                                        Ok(())
                                    });

                                    if let Err(e) = write_result {
                                        result = Err(e);
                                    }
                                }
                                _ => unreachable!(),
                            }
                            u.rehash();
                        },
                        parents,
                        deleted
                    );

                    return result;
                }

                let (leaf_ptr, prefix, idx, v) = if rem_path.len() < n_path.len() {
                    // key path is a prefix of the path to u
                    u_ref
                        .write(|u| {
                            (*match &mut u.inner {
                                NodeType::Leaf(u) => &mut u.0,
                                NodeType::Extension(u) => &mut u.0,
                                _ => unreachable!(),
                            }) = PartialPath(n_path[rem_path.len() + 1..].to_vec());
                            u.rehash();
                        })
                        .unwrap();
                    (
                        match &u_ref.inner {
                            NodeType::Extension(u) => {
                                if u.0.len() == 0 {
                                    deleted.push(u_ptr);
                                    u.1
                                } else {
                                    u_ptr
                                }
                            }
                            _ => u_ptr,
                        },
                        rem_path,
                        n_path[rem_path.len()],
                        Some(Data(val)),
                    )
                } else {
                    // key path extends the path to u
                    if n_value.is_none() {
                        // this case does not apply to an extension node, resume the tree walk
                        return Ok(Some(val));
                    }
                    let leaf = self.new_node(Node::new(NodeType::Leaf(LeafNode(
                        PartialPath(rem_path[n_path.len() + 1..].to_vec()),
                        Data(val),
                    ))))?;
                    deleted.push(u_ptr);
                    (leaf.as_ptr(), &n_path[..], rem_path[n_path.len()], n_value)
                };
                drop(u_ref);
                // [parent] (-> [ExtNode]) -> [branch with v] -> [Leaf]
                let mut chd = [None; NBRANCH];
                chd[idx as usize] = Some(leaf_ptr);
                let branch_ptr = self
                    .new_node(Node::new(NodeType::Branch(BranchNode {
                        chd,
                        value: v,
                        chd_eth_rlp: Default::default(),
                    })))?
                    .as_ptr();
                if !prefix.is_empty() {
                    self.new_node(Node::new(NodeType::Extension(ExtNode(
                        PartialPath(prefix.to_vec()),
                        branch_ptr,
                        None,
                    ))))?
                    .as_ptr()
                } else {
                    branch_ptr
                }
            }
        };
        // observation:
        // - leaf/extension node can only be the child of a branch node
        // - branch node can only be the child of a branch/extension node
        self.set_parent(new_chd, parents);
        Ok(None)
    }

    pub fn insert<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        val: Vec<u8>,
        root: DiskAddress,
    ) -> Result<(), MerkleError> {
        // as we split a node, we need to track deleted nodes and parents
        let mut deleted = Vec::new();
        let mut parents = Vec::new();

        // TODO: Explain why this always starts with a 0 chunk
        // I think this may have to do with avoiding moving the root
        let mut chunked_key = vec![0];
        chunked_key.extend(key.as_ref().iter().copied().flat_map(to_nibble_array));

        let mut next_node = Some(self.get_node(root)?);
        let mut nskip = 0;

        // wrap the current value into an Option to indicate whether it has been
        // inserted yet. If we haven't inserted it after we traverse the tree, we
        // have to do some splitting
        let mut val = Some(val);

        // walk down the merkle tree starting from next_node, currently the root
        for (key_nib_offset, key_nib) in chunked_key.iter().enumerate() {
            // special handling for extension nodes
            if nskip > 0 {
                nskip -= 1;
                continue;
            }
            // move the current node into node; next_node becomes None
            // unwrap() is okay here since we are certain we have something
            // in next_node at this point
            let mut node = next_node.take().unwrap();
            let node_ptr = node.as_ptr();

            let next_node_ptr = match &node.inner {
                // For a Branch node, we look at the child pointer. If it points
                // to another node, we walk down that. Otherwise, we can store our
                // value as a leaf and we're done
                NodeType::Branch(n) => match n.chd[*key_nib as usize] {
                    Some(c) => c,
                    None => {
                        // insert the leaf to the empty slot
                        // create a new leaf
                        let leaf_ptr = self
                            .new_node(Node::new(NodeType::Leaf(LeafNode(
                                PartialPath(chunked_key[key_nib_offset + 1..].to_vec()),
                                Data(val.take().unwrap()),
                            ))))?
                            .as_ptr();
                        // set the current child to point to this leaf
                        node.write(|u| {
                            let uu = u.inner.as_branch_mut().unwrap();
                            uu.chd[*key_nib as usize] = Some(leaf_ptr);
                            u.rehash();
                        })
                        .unwrap();
                        break;
                    }
                },
                NodeType::Leaf(n) => {
                    // we collided with another key; make a copy
                    // of the stored key to pass into split
                    let n_path = n.0.to_vec();
                    let n_value = Some(n.1.clone());
                    self.split(
                        node,
                        &mut parents,
                        &chunked_key[key_nib_offset..],
                        n_path,
                        n_value,
                        val.take().unwrap(),
                        &mut deleted,
                    )?;
                    break;
                }
                NodeType::Extension(n) => {
                    let n_path = n.0.to_vec();
                    let n_ptr = n.1;
                    nskip = n_path.len() - 1;
                    if let Some(v) = self.split(
                        node,
                        &mut parents,
                        &chunked_key[key_nib_offset..],
                        n_path,
                        None,
                        val.take().unwrap(),
                        &mut deleted,
                    )? {
                        // we couldn't split this, so we
                        // skip n_path items and follow the
                        // extension node's next pointer
                        val = Some(v);
                        node = self.get_node(node_ptr)?;
                        n_ptr
                    } else {
                        // successfully inserted
                        break;
                    }
                }
            };
            // push another parent, and follow the next pointer
            parents.push((node, *key_nib));
            next_node = Some(self.get_node(next_node_ptr)?);
        }
        if val.is_some() {
            // we walked down the tree and reached the end of the key,
            // but haven't inserted the value yet
            let mut info = None;
            let u_ptr = {
                let mut u = next_node.take().unwrap();
                write_node!(
                    self,
                    u,
                    |u| {
                        info = match &mut u.inner {
                            NodeType::Branch(n) => {
                                n.value = Some(Data(val.take().unwrap()));
                                None
                            }
                            NodeType::Leaf(n) => {
                                if n.0.len() == 0 {
                                    n.1 = Data(val.take().unwrap());
                                    None
                                } else {
                                    let idx = n.0[0];
                                    n.0 = PartialPath(n.0[1..].to_vec());
                                    u.rehash();
                                    Some((idx, true, None))
                                }
                            }
                            NodeType::Extension(n) => {
                                let idx = n.0[0];
                                let more = if n.0.len() > 1 {
                                    n.0 = PartialPath(n.0[1..].to_vec());
                                    true
                                } else {
                                    false
                                };
                                Some((idx, more, Some(n.1)))
                            }
                        };
                        u.rehash()
                    },
                    &mut parents,
                    &mut deleted
                );
                u.as_ptr()
            };

            if let Some((idx, more, ext)) = info {
                let mut chd = [None; NBRANCH];
                let c_ptr = if more {
                    u_ptr
                } else {
                    deleted.push(u_ptr);
                    ext.unwrap()
                };
                chd[idx as usize] = Some(c_ptr);
                let branch = self
                    .new_node(Node::new(NodeType::Branch(BranchNode {
                        chd,
                        value: Some(Data(val.take().unwrap())),
                        chd_eth_rlp: Default::default(),
                    })))?
                    .as_ptr();
                self.set_parent(branch, &mut parents);
            }
        }

        drop(next_node);

        for (mut r, _) in parents.into_iter().rev() {
            r.write(|u| u.rehash()).unwrap();
        }

        for ptr in deleted.into_iter() {
            self.free_node(ptr)?
        }
        Ok(())
    }

    fn after_remove_leaf(
        &self,
        parents: &mut Vec<(ObjRef<'_, Node>, u8)>,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<(), MerkleError> {
        let (b_chd, val) = {
            let (mut b_ref, b_idx) = parents.pop().unwrap();
            // the immediate parent of a leaf must be a branch
            b_ref
                .write(|b| {
                    b.inner.as_branch_mut().unwrap().chd[b_idx as usize] = None;
                    b.rehash()
                })
                .unwrap();
            let b_inner = b_ref.inner.as_branch().unwrap();
            let (b_chd, has_chd) = b_inner.single_child();
            if (has_chd && (b_chd.is_none() || b_inner.value.is_some())) || parents.is_empty() {
                return Ok(());
            }
            deleted.push(b_ref.as_ptr());
            (b_chd, b_inner.value.clone())
        };
        let (mut p_ref, p_idx) = parents.pop().unwrap();
        let p_ptr = p_ref.as_ptr();
        if let Some(val) = val {
            match &p_ref.inner {
                NodeType::Branch(_) => {
                    // from: [p: Branch] -> [b (v)]x -> [Leaf]x
                    // to: [p: Branch] -> [Leaf (v)]
                    let leaf = self
                        .new_node(Node::new(NodeType::Leaf(LeafNode(
                            PartialPath(Vec::new()),
                            val,
                        ))))?
                        .as_ptr();
                    p_ref
                        .write(|p| {
                            p.inner.as_branch_mut().unwrap().chd[p_idx as usize] = Some(leaf);
                            p.rehash()
                        })
                        .unwrap();
                }
                NodeType::Extension(n) => {
                    // from: P -> [p: Ext]x -> [b (v)]x -> [leaf]x
                    // to: P -> [Leaf (v)]
                    let leaf = self
                        .new_node(Node::new(NodeType::Leaf(LeafNode(
                            PartialPath(n.0.clone().into_inner()),
                            val,
                        ))))?
                        .as_ptr();
                    deleted.push(p_ptr);
                    self.set_parent(leaf, parents);
                }
                _ => unreachable!(),
            }
        } else {
            let (c_ptr, idx) = b_chd.unwrap();
            let mut c_ref = self.get_node(c_ptr)?;
            match &c_ref.inner {
                NodeType::Branch(_) => {
                    drop(c_ref);
                    match &p_ref.inner {
                        NodeType::Branch(_) => {
                            //                            ____[Branch]
                            //                           /
                            // from: [p: Branch] -> [b]x*
                            //                           \____[Leaf]x
                            // to: [p: Branch] -> [Ext] -> [Branch]
                            let ext = self
                                .new_node(Node::new(NodeType::Extension(ExtNode(
                                    PartialPath(vec![idx]),
                                    c_ptr,
                                    None,
                                ))))?
                                .as_ptr();
                            self.set_parent(ext, &mut [(p_ref, p_idx)]);
                        }
                        NodeType::Extension(_) => {
                            //                         ____[Branch]
                            //                        /
                            // from: [p: Ext] -> [b]x*
                            //                        \____[Leaf]x
                            // to: [p: Ext] -> [Branch]
                            write_node!(
                                self,
                                p_ref,
                                |p| {
                                    let pp = p.inner.as_extension_mut().unwrap();
                                    pp.0 .0.push(idx);
                                    pp.1 = c_ptr;
                                    p.rehash();
                                },
                                parents,
                                deleted
                            );
                        }
                        _ => unreachable!(),
                    }
                }
                NodeType::Leaf(_) | NodeType::Extension(_) => {
                    match &p_ref.inner {
                        NodeType::Branch(_) => {
                            //                            ____[Leaf/Ext]
                            //                           /
                            // from: [p: Branch] -> [b]x*
                            //                           \____[Leaf]x
                            // to: [p: Branch] -> [Leaf/Ext]
                            let write_result = c_ref.write(|c| {
                                let partial_path = match &mut c.inner {
                                    NodeType::Leaf(n) => &mut n.0,
                                    NodeType::Extension(n) => &mut n.0,
                                    _ => unreachable!(),
                                };

                                partial_path.0.insert(0, idx);
                                c.rehash()
                            });

                            let c_ptr = if write_result.is_err() {
                                deleted.push(c_ptr);
                                self.new_node(c_ref.clone())?.as_ptr()
                            } else {
                                c_ptr
                            };

                            drop(c_ref);

                            p_ref
                                .write(|p| {
                                    p.inner.as_branch_mut().unwrap().chd[p_idx as usize] =
                                        Some(c_ptr);
                                    p.rehash()
                                })
                                .unwrap();
                        }
                        NodeType::Extension(n) => {
                            //                               ____[Leaf/Ext]
                            //                              /
                            // from: P -> [p: Ext]x -> [b]x*
                            //                              \____[Leaf]x
                            // to: P -> [p: Leaf/Ext]
                            deleted.push(p_ptr);

                            let write_failed = write_node!(
                                self,
                                c_ref,
                                |c| {
                                    let mut path = n.0.clone().into_inner();
                                    path.push(idx);
                                    let path0 = match &mut c.inner {
                                        NodeType::Leaf(n) => &mut n.0,
                                        NodeType::Extension(n) => &mut n.0,
                                        _ => unreachable!(),
                                    };
                                    path.extend(&**path0);
                                    *path0 = PartialPath(path);
                                    c.rehash()
                                },
                                parents,
                                deleted
                            );

                            if !write_failed {
                                drop(c_ref);
                                self.set_parent(c_ptr, parents);
                            }
                        }
                        _ => unreachable!(),
                    }
                }
            }
        }
        Ok(())
    }

    fn after_remove_branch(
        &self,
        (c_ptr, idx): (DiskAddress, u8),
        parents: &mut Vec<(ObjRef<'_, Node>, u8)>,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<(), MerkleError> {
        // [b] -> [u] -> [c]
        let (mut b_ref, b_idx) = parents.pop().unwrap();
        let mut c_ref = self.get_node(c_ptr).unwrap();
        match &c_ref.inner {
            NodeType::Branch(_) => {
                drop(c_ref);
                let mut err = None;
                write_node!(
                    self,
                    b_ref,
                    |b| {
                        if let Err(e) = (|| {
                            match &mut b.inner {
                                NodeType::Branch(n) => {
                                    // from: [Branch] -> [Branch]x -> [Branch]
                                    // to: [Branch] -> [Ext] -> [Branch]
                                    n.chd[b_idx as usize] =
                                        Some(
                                            self.new_node(Node::new(NodeType::Extension(
                                                ExtNode(PartialPath(vec![idx]), c_ptr, None),
                                            )))?
                                            .as_ptr(),
                                        );
                                }
                                NodeType::Extension(n) => {
                                    // from: [Ext] -> [Branch]x -> [Branch]
                                    // to: [Ext] -> [Branch]
                                    n.0 .0.push(idx);
                                    n.1 = c_ptr
                                }
                                _ => unreachable!(),
                            }
                            b.rehash();
                            Ok(())
                        })() {
                            err = Some(Err(e))
                        }
                    },
                    parents,
                    deleted
                );
                if let Some(e) = err {
                    return e;
                }
            }
            NodeType::Leaf(_) | NodeType::Extension(_) => match &b_ref.inner {
                NodeType::Branch(_) => {
                    // from: [Branch] -> [Branch]x -> [Leaf/Ext]
                    // to: [Branch] -> [Leaf/Ext]
                    let write_result = c_ref.write(|c| {
                        match &mut c.inner {
                            NodeType::Leaf(n) => &mut n.0,
                            NodeType::Extension(n) => &mut n.0,
                            _ => unreachable!(),
                        }
                        .0
                        .insert(0, idx);
                        c.rehash()
                    });
                    if write_result.is_err() {
                        deleted.push(c_ptr);
                        self.new_node(c_ref.clone())?.as_ptr()
                    } else {
                        c_ptr
                    };
                    drop(c_ref);
                    b_ref
                        .write(|b| {
                            b.inner.as_branch_mut().unwrap().chd[b_idx as usize] = Some(c_ptr);
                            b.rehash()
                        })
                        .unwrap();
                }
                NodeType::Extension(n) => {
                    // from: P -> [Ext] -> [Branch]x -> [Leaf/Ext]
                    // to: P -> [Leaf/Ext]
                    let write_result = c_ref.write(|c| {
                        let mut path = n.0.clone().into_inner();
                        path.push(idx);
                        let path0 = match &mut c.inner {
                            NodeType::Leaf(n) => &mut n.0,
                            NodeType::Extension(n) => &mut n.0,
                            _ => unreachable!(),
                        };
                        path.extend(&**path0);
                        *path0 = PartialPath(path);
                        c.rehash()
                    });

                    let c_ptr = if write_result.is_err() {
                        deleted.push(c_ptr);
                        self.new_node(c_ref.clone())?.as_ptr()
                    } else {
                        c_ptr
                    };

                    deleted.push(b_ref.as_ptr());
                    drop(c_ref);
                    self.set_parent(c_ptr, parents);
                }
                _ => unreachable!(),
            },
        }
        Ok(())
    }

    pub fn remove<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<Vec<u8>>, MerkleError> {
        let mut chunks = vec![0];
        chunks.extend(key.as_ref().iter().copied().flat_map(to_nibble_array));

        if root.is_null() {
            return Ok(None);
        }

        let mut deleted = Vec::new();
        let mut parents: Vec<(ObjRef<Node>, _)> = Vec::new();
        let mut u_ref = self.get_node(root)?;
        let mut nskip = 0;
        let mut found = None;

        for (i, nib) in chunks.iter().enumerate() {
            if nskip > 0 {
                nskip -= 1;
                continue;
            }
            let next_ptr = match &u_ref.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => return Ok(None),
                },
                NodeType::Leaf(n) => {
                    if chunks[i..] != *n.0 {
                        return Ok(None);
                    }
                    found = Some(n.1.clone());
                    deleted.push(u_ref.as_ptr());
                    self.after_remove_leaf(&mut parents, &mut deleted)?;
                    break;
                }
                NodeType::Extension(n) => {
                    let n_path = &*n.0;
                    let rem_path = &chunks[i..];
                    if rem_path < n_path || &rem_path[..n_path.len()] != n_path {
                        return Ok(None);
                    }
                    nskip = n_path.len() - 1;
                    n.1
                }
            };

            parents.push((u_ref, *nib));
            u_ref = self.get_node(next_ptr)?;
        }
        if found.is_none() {
            match &u_ref.inner {
                NodeType::Branch(n) => {
                    if n.value.is_none() {
                        return Ok(None);
                    }
                    let (c_chd, _) = n.single_child();
                    u_ref
                        .write(|u| {
                            found = u.inner.as_branch_mut().unwrap().value.take();
                            u.rehash()
                        })
                        .unwrap();
                    if let Some((c_ptr, idx)) = c_chd {
                        deleted.push(u_ref.as_ptr());
                        self.after_remove_branch((c_ptr, idx), &mut parents, &mut deleted)?
                    }
                }
                NodeType::Leaf(n) => {
                    if n.0.len() > 0 {
                        return Ok(None);
                    }
                    found = Some(n.1.clone());
                    deleted.push(u_ref.as_ptr());
                    self.after_remove_leaf(&mut parents, &mut deleted)?
                }
                _ => (),
            }
        }

        drop(u_ref);

        for (mut r, _) in parents.into_iter().rev() {
            r.write(|u| u.rehash()).unwrap();
        }

        for ptr in deleted.into_iter() {
            self.free_node(ptr)?;
        }
        Ok(found.map(|e| e.0))
    }

    fn remove_tree_(
        &self,
        u: DiskAddress,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<(), MerkleError> {
        let u_ref = self.get_node(u)?;
        match &u_ref.inner {
            NodeType::Branch(n) => {
                for c in n.chd.iter().flatten() {
                    self.remove_tree_(*c, deleted)?
                }
            }
            NodeType::Leaf(_) => (),
            NodeType::Extension(n) => self.remove_tree_(n.1, deleted)?,
        }
        deleted.push(u);
        Ok(())
    }

    pub fn remove_tree(&mut self, root: DiskAddress) -> Result<(), MerkleError> {
        let mut deleted = Vec::new();
        if root.is_null() {
            return Ok(());
        }
        self.remove_tree_(root, &mut deleted)?;
        for ptr in deleted.into_iter() {
            self.free_node(ptr)?;
        }
        Ok(())
    }

    pub fn get_mut<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<RefMut<S>>, MerkleError> {
        let mut chunks = vec![0];
        chunks.extend(key.as_ref().iter().copied().flat_map(to_nibble_array));
        let mut parents = Vec::new();

        if root.is_null() {
            return Ok(None);
        }

        let mut u_ref = self.get_node(root)?;
        let mut nskip = 0;

        for (i, nib) in chunks.iter().enumerate() {
            let u_ptr = u_ref.as_ptr();
            if nskip > 0 {
                nskip -= 1;
                continue;
            }
            let next_ptr = match &u_ref.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => return Ok(None),
                },
                NodeType::Leaf(n) => {
                    if chunks[i..] != *n.0 {
                        return Ok(None);
                    }
                    drop(u_ref);
                    return Ok(Some(RefMut::new(u_ptr, parents, self)));
                }
                NodeType::Extension(n) => {
                    let n_path = &*n.0;
                    let rem_path = &chunks[i..];
                    if rem_path.len() < n_path.len() || &rem_path[..n_path.len()] != n_path {
                        return Ok(None);
                    }
                    nskip = n_path.len() - 1;
                    n.1
                }
            };
            parents.push((u_ptr, *nib));
            u_ref = self.get_node(next_ptr)?;
        }

        let u_ptr = u_ref.as_ptr();
        match &u_ref.inner {
            NodeType::Branch(n) => {
                if n.value.as_ref().is_some() {
                    drop(u_ref);
                    return Ok(Some(RefMut::new(u_ptr, parents, self)));
                }
            }
            NodeType::Leaf(n) => {
                if n.0.len() == 0 {
                    drop(u_ref);
                    return Ok(Some(RefMut::new(u_ptr, parents, self)));
                }
            }
            _ => (),
        }

        Ok(None)
    }

    /// Constructs a merkle proof for key. The result contains all encoded nodes
    /// on the path to the value at key. The value itself is also included in the
    /// last node and can be retrieved by verifying the proof.
    ///
    /// If the trie does not contain a value for key, the returned proof contains
    /// all nodes of the longest existing prefix of the key, ending with the node
    /// that proves the absence of the key (at least the root node).
    pub fn prove<K, T>(&self, key: K, root: DiskAddress) -> Result<Proof, MerkleError>
    where
        K: AsRef<[u8]>,
        T: ValueTransformer,
    {
        let mut chunks = Vec::new();
        chunks.extend(key.as_ref().iter().copied().flat_map(to_nibble_array));

        let mut proofs: HashMap<[u8; TRIE_HASH_LEN], Vec<u8>> = HashMap::new();
        if root.is_null() {
            return Ok(Proof(proofs));
        }

        // Skip the sentinel root
        let root = self
            .get_node(root)?
            .inner
            .as_branch()
            .ok_or(MerkleError::NotBranchNode)?
            .chd[0];
        let mut u_ref = match root {
            Some(root) => self.get_node(root)?,
            None => return Ok(Proof(proofs)),
        };

        let mut nskip = 0;
        let mut nodes: Vec<DiskAddress> = Vec::new();
        for (i, nib) in chunks.iter().enumerate() {
            if nskip > 0 {
                nskip -= 1;
                continue;
            }
            nodes.push(u_ref.as_ptr());
            let next_ptr: DiskAddress = match &u_ref.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => break,
                },
                NodeType::Leaf(_) => break,
                NodeType::Extension(n) => {
                    let n_path = &*n.0;
                    let remaining_path = &chunks[i..];
                    if remaining_path.len() < n_path.len()
                        || &remaining_path[..n_path.len()] != n_path
                    {
                        break;
                    } else {
                        nskip = n_path.len() - 1;
                        n.1
                    }
                }
            };
            u_ref = self.get_node(next_ptr)?;
        }

        match &u_ref.inner {
            NodeType::Branch(n) => {
                if n.value.as_ref().is_some() {
                    nodes.push(u_ref.as_ptr());
                }
            }
            NodeType::Leaf(n) => {
                if n.0.len() == 0 {
                    nodes.push(u_ref.as_ptr());
                }
            }
            _ => (),
        }

        drop(u_ref);
        // Get the hashes of the nodes.
        for node in nodes {
            let node = self.get_node(node)?;
            let rlp = <&[u8]>::clone(&node.get_eth_rlp::<T, S>(self.store.as_ref()));
            let hash: [u8; TRIE_HASH_LEN] = sha3::Keccak256::digest(rlp).into();
            proofs.insert(hash, rlp.to_vec());
        }
        Ok(Proof(proofs))
    }

    pub fn get<K: AsRef<[u8]>>(
        &self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<Ref>, MerkleError> {
        // TODO: Make this NonNull<DiskAddress> or something similar
        if root.is_null() {
            return Ok(None);
        }

        let chunks: Vec<u8> = iter::once(0)
            .chain(key.as_ref().iter().copied().flat_map(to_nibble_array))
            .collect();

        let mut u_ref = self.get_node(root)?;
        let mut nskip = 0;

        for (i, nib) in chunks.iter().enumerate() {
            if nskip > 0 {
                nskip -= 1;
                continue;
            }
            let next_ptr = match &u_ref.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => return Ok(None),
                },
                NodeType::Leaf(n) => {
                    if chunks[i..] != *n.0 {
                        return Ok(None);
                    }
                    return Ok(Some(Ref(u_ref)));
                }
                NodeType::Extension(n) => {
                    let n_path = &*n.0;
                    let rem_path = &chunks[i..];
                    if rem_path.len() < n_path.len() || &rem_path[..n_path.len()] != n_path {
                        return Ok(None);
                    }
                    nskip = n_path.len() - 1;
                    n.1
                }
            };
            u_ref = self.get_node(next_ptr)?;
        }

        match &u_ref.inner {
            NodeType::Branch(n) => {
                if n.value.as_ref().is_some() {
                    return Ok(Some(Ref(u_ref)));
                }
            }
            NodeType::Leaf(n) => {
                if n.0.len() == 0 {
                    return Ok(Some(Ref(u_ref)));
                }
            }
            _ => (),
        }

        Ok(None)
    }

    pub fn flush_dirty(&self) -> Option<()> {
        self.store.flush_dirty()
    }
}

pub struct Ref<'a>(ObjRef<'a, Node>);

pub struct RefMut<'a, S> {
    ptr: DiskAddress,
    parents: Vec<(DiskAddress, u8)>,
    merkle: &'a mut Merkle<S>,
}

impl<'a> std::ops::Deref for Ref<'a> {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        match &self.0.inner {
            NodeType::Branch(n) => n.value.as_ref().unwrap(),
            NodeType::Leaf(n) => &n.1,
            _ => unreachable!(),
        }
    }
}

impl<'a, S: ShaleStore<Node> + Send + Sync> RefMut<'a, S> {
    fn new(ptr: DiskAddress, parents: Vec<(DiskAddress, u8)>, merkle: &'a mut Merkle<S>) -> Self {
        Self {
            ptr,
            parents,
            merkle,
        }
    }

    pub fn get(&self) -> Ref {
        Ref(self.merkle.get_node(self.ptr).unwrap())
    }

    pub fn write(&mut self, modify: impl FnOnce(&mut Vec<u8>)) -> Result<(), MerkleError> {
        let mut deleted = Vec::new();
        {
            let mut u_ref = self.merkle.get_node(self.ptr).unwrap();
            let mut parents: Vec<_> = self
                .parents
                .iter()
                .map(|(ptr, nib)| (self.merkle.get_node(*ptr).unwrap(), *nib))
                .collect();
            write_node!(
                self.merkle,
                u_ref,
                |u| {
                    modify(match &mut u.inner {
                        NodeType::Branch(n) => &mut n.value.as_mut().unwrap().0,
                        NodeType::Leaf(n) => &mut n.1 .0,
                        _ => unreachable!(),
                    });
                    u.rehash()
                },
                &mut parents,
                &mut deleted
            );
        }
        for ptr in deleted.into_iter() {
            self.merkle.free_node(ptr)?;
        }
        Ok(())
    }
}

pub trait ValueTransformer {
    fn transform(bytes: &[u8]) -> Vec<u8>;
}

pub struct IdTrans;

impl ValueTransformer for IdTrans {
    fn transform(bytes: &[u8]) -> Vec<u8> {
        bytes.to_vec()
    }
}

// nibbles, high bits first, then low bits
pub fn to_nibble_array(x: u8) -> [u8; 2] {
    [x >> 4, x & 0b_0000_1111]
}

// given a set of nibbles, take each pair and convert this back into bytes
// if an odd number of nibbles, in debug mode it panics. In release mode,
// the final nibble is dropped
pub fn from_nibbles(nibbles: &[u8]) -> impl Iterator<Item = u8> + '_ {
    debug_assert_eq!(nibbles.len() & 1, 0);
    nibbles.chunks_exact(2).map(|p| (p[0] << 4) | p[1])
}

#[cfg(test)]
mod test {
    use super::*;
    use shale::cached::PlainMem;
    use std::ops::Deref;
    use test_case::test_case;

    #[test_case(vec![0x12, 0x34, 0x56], vec![0x1, 0x2, 0x3, 0x4, 0x5, 0x6])]
    #[test_case(vec![0xc0, 0xff], vec![0xc, 0x0, 0xf, 0xf])]
    fn test_to_nibbles(bytes: Vec<u8>, nibbles: Vec<u8>) {
        let n: Vec<_> = bytes.into_iter().flat_map(to_nibble_array).collect();
        assert_eq!(n, nibbles);
    }

    const ZERO_HASH: TrieHash = TrieHash([0u8; TRIE_HASH_LEN]);

    #[test]
    fn test_hash_len() {
        assert_eq!(TRIE_HASH_LEN, ZERO_HASH.dehydrated_len() as usize);
    }
    #[test]
    fn test_dehydrate() {
        let mut to = [1u8; TRIE_HASH_LEN];
        assert_eq!(
            {
                ZERO_HASH.dehydrate(&mut to).unwrap();
                &to
            },
            ZERO_HASH.deref()
        );
    }

    #[test]
    fn test_hydrate() {
        let mut store = PlainMem::new(TRIE_HASH_LEN as u64, 0u8);
        store.write(0, ZERO_HASH.deref());
        assert_eq!(TrieHash::hydrate(0, &store).unwrap(), ZERO_HASH);
    }
    #[test]
    fn test_partial_path_encoding() {
        let check = |steps: &[u8], term| {
            let (d, t) = PartialPath::decode(PartialPath(steps.to_vec()).encode(term));
            assert_eq!(d.0, steps);
            assert_eq!(t, term);
        };
        for steps in [
            vec![0x1, 0x2, 0x3, 0x4],
            vec![0x1, 0x2, 0x3],
            vec![0x0, 0x1, 0x2],
            vec![0x1, 0x2],
            vec![0x1],
        ] {
            for term in [true, false] {
                check(&steps, term)
            }
        }
    }
    #[test]
    fn test_merkle_node_encoding() {
        let check = |node: Node| {
            let mut bytes = Vec::new();
            bytes.resize(node.dehydrated_len() as usize, 0);
            node.dehydrate(&mut bytes).unwrap();

            let mut mem = PlainMem::new(bytes.len() as u64, 0x0);
            mem.write(0, &bytes);
            println!("{bytes:?}");
            let node_ = Node::hydrate(0, &mem).unwrap();
            assert!(node == node_);
        };
        let chd0 = [None; NBRANCH];
        let mut chd1 = chd0;
        for node in chd1.iter_mut().take(NBRANCH / 2) {
            *node = Some(DiskAddress::from(0xa));
        }
        let mut chd_eth_rlp: [Option<Vec<u8>>; NBRANCH] = Default::default();
        for rlp in chd_eth_rlp.iter_mut().take(NBRANCH / 2) {
            *rlp = Some(vec![0x1, 0x2, 0x3]);
        }
        for node in [
            Node::new_from_hash(
                None,
                None,
                NodeType::Leaf(LeafNode(
                    PartialPath(vec![0x1, 0x2, 0x3]),
                    Data(vec![0x4, 0x5]),
                )),
            ),
            Node::new_from_hash(
                None,
                None,
                NodeType::Extension(ExtNode(
                    PartialPath(vec![0x1, 0x2, 0x3]),
                    DiskAddress::from(0x42),
                    None,
                )),
            ),
            Node::new_from_hash(
                None,
                None,
                NodeType::Extension(ExtNode(
                    PartialPath(vec![0x1, 0x2, 0x3]),
                    DiskAddress::null(),
                    Some(vec![0x1, 0x2, 0x3]),
                )),
            ),
            Node::new_from_hash(
                None,
                None,
                NodeType::Branch(BranchNode {
                    chd: chd0,
                    value: Some(Data("hello, world!".as_bytes().to_vec())),
                    chd_eth_rlp: Default::default(),
                }),
            ),
            Node::new_from_hash(
                None,
                None,
                NodeType::Branch(BranchNode {
                    chd: chd1,
                    value: None,
                    chd_eth_rlp,
                }),
            ),
        ] {
            check(node);
        }
    }
}
