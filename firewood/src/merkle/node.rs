// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use enum_as_inner::EnumAsInner;
use sha3::{Digest, Keccak256};
use shale::{disk_address::DiskAddress, CachedStore, ShaleError, ShaleStore, Storable};
use std::{
    fmt::{self, Debug},
    io::{Cursor, Read, Write},
    sync::{
        atomic::{AtomicBool, Ordering},
        OnceLock,
    },
};

use crate::merkle::to_nibble_array;

use super::{from_nibbles, PartialPath, TrieHash, TRIE_HASH_LEN};

pub const NBRANCH: usize = 16;

#[derive(Debug, PartialEq, Eq, Clone)]
pub struct Data(pub(super) Vec<u8>);

impl std::ops::Deref for Data {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

#[derive(PartialEq, Eq, Clone)]
pub struct BranchNode {
    pub(super) chd: [Option<DiskAddress>; NBRANCH],
    pub(super) value: Option<Data>,
    pub(super) chd_eth_rlp: [Option<Vec<u8>>; NBRANCH],
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
    pub(super) fn single_child(&self) -> (Option<(DiskAddress, u8)>, bool) {
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

    fn calc_eth_rlp<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        let mut stream = rlp::RlpStream::new_list(17);
        for (i, c) in self.chd.iter().enumerate() {
            match c {
                Some(c) => {
                    let mut c_ref = store.get_item(*c).unwrap();
                    if c_ref.get_eth_rlp_long::<S>(store) {
                        stream.append(&&(*c_ref.get_root_hash::<S>(store))[..]);
                        // See struct docs for ordering requirements
                        if c_ref.lazy_dirty.load(Ordering::Relaxed) {
                            c_ref.write(|_| {}).unwrap();
                            c_ref.lazy_dirty.store(false, Ordering::Relaxed)
                        }
                    } else {
                        let c_rlp = &c_ref.get_eth_rlp::<S>(store);
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
pub struct LeafNode(pub(super) PartialPath, pub(super) Data);

impl Debug for LeafNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Leaf {:?} {}]", self.0, hex::encode(&*self.1))
    }
}

impl LeafNode {
    fn calc_eth_rlp(&self) -> Vec<u8> {
        rlp::encode_list::<Vec<u8>, _>(&[
            from_nibbles(&self.0.encode(true)).collect(),
            self.1.to_vec(),
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
pub struct ExtNode(
    pub(super) PartialPath,
    pub(super) DiskAddress,
    pub(super) Option<Vec<u8>>,
);

impl Debug for ExtNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Extension {:?} {:?} {:?}]", self.0, self.1, self.2)
    }
}

impl ExtNode {
    fn calc_eth_rlp<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        let mut stream = rlp::RlpStream::new_list(2);
        if !self.1.is_null() {
            let mut r = store.get_item(self.1).unwrap();
            stream.append(&from_nibbles(&self.0.encode(false)).collect::<Vec<_>>());
            if r.get_eth_rlp_long(store) {
                stream.append(&&(*r.get_root_hash(store))[..]);
                if r.lazy_dirty.load(Ordering::Relaxed) {
                    r.write(|_| {}).unwrap();
                    r.lazy_dirty.store(false, Ordering::Relaxed);
                }
            } else {
                stream.append_raw(r.get_eth_rlp(store), 1);
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
    pub(super) root_hash: OnceLock<TrieHash>,
    eth_rlp_long: OnceLock<bool>,
    eth_rlp: OnceLock<Vec<u8>>,
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
    fn calc_eth_rlp<S: ShaleStore<Node>>(&self, store: &S) -> Vec<u8> {
        match &self {
            NodeType::Leaf(n) => n.calc_eth_rlp(),
            NodeType::Extension(n) => n.calc_eth_rlp(store),
            NodeType::Branch(n) => n.calc_eth_rlp(store),
        }
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

    pub(super) fn get_eth_rlp<S: ShaleStore<Node>>(&self, store: &S) -> &[u8] {
        self.eth_rlp
            .get_or_init(|| self.inner.calc_eth_rlp::<S>(store))
    }

    pub(super) fn get_root_hash<S: ShaleStore<Node>>(&self, store: &S) -> &TrieHash {
        self.root_hash.get_or_init(|| {
            self.lazy_dirty.store(true, Ordering::Relaxed);
            TrieHash(Keccak256::digest(self.get_eth_rlp::<S>(store)).into())
        })
    }

    fn get_eth_rlp_long<S: ShaleStore<Node>>(&self, store: &S) -> bool {
        *self.eth_rlp_long.get_or_init(|| {
            self.lazy_dirty.store(true, Ordering::Relaxed);
            self.get_eth_rlp(store).len() >= TRIE_HASH_LEN
        })
    }

    pub(super) fn rehash(&mut self) {
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

    pub(super) fn new_from_hash(
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
