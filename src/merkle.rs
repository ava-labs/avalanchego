use enum_as_inner::EnumAsInner;
use once_cell::unsync::OnceCell;
use sha3::Digest;
use shale::{MemStore, MummyItem, ObjPtr, ObjRef, ShaleError, ShaleStore};

use std::cell::Cell;
use std::fmt::{self, Debug};
use std::io::{Cursor, Read, Write};

const NBRANCH: usize = 16;

#[derive(Debug)]
pub enum MerkleError {
    Shale(ShaleError),
    ReadOnly,
    NotBranchNode,
    Format(std::io::Error),
}

#[derive(PartialEq, Eq, Clone)]
pub struct Hash(pub [u8; 32]);

impl Hash {
    const MSIZE: u64 = 32;
}

impl std::ops::Deref for Hash {
    type Target = [u8; 32];
    fn deref(&self) -> &[u8; 32] {
        &self.0
    }
}

impl MummyItem for Hash {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let raw = mem.get_view(addr, Self::MSIZE).ok_or(ShaleError::LinearMemStoreError)?;
        Ok(Self(raw[..Self::MSIZE as usize].try_into().unwrap()))
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) {
        Cursor::new(to).write_all(&self.0).unwrap()
    }
}

/// PartialPath keeps a list of nibbles to represent a path on the MPT.
#[derive(PartialEq, Eq, Clone)]
struct PartialPath(Vec<u8>);

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
    fn into_inner(self) -> Vec<u8> {
        self.0
    }

    fn encode(&self, term: bool) -> Vec<u8> {
        let odd_len = (self.0.len() & 1) as u8;
        let flags = if term { 2 } else { 0 } + odd_len;
        let mut res = if odd_len == 1 { vec![flags] } else { vec![flags, 0x0] };
        res.extend(&self.0);
        res
    }

    fn decode<R: AsRef<[u8]>>(raw: R) -> (Self, bool) {
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

#[test]
fn test_partial_path_encoding() {
    let check = |steps: &[u8], term| {
        let (d, t) = PartialPath::decode(&PartialPath(steps.to_vec()).encode(term));
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

#[derive(PartialEq, Eq, Clone)]
struct Data(Vec<u8>);

impl std::ops::Deref for Data {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

#[derive(PartialEq, Eq, Clone)]
struct BranchNode {
    chd: [Option<ObjPtr<Node>>; NBRANCH],
    value: Option<Data>,
}

impl Debug for BranchNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Branch")?;
        for (i, c) in self.chd.iter().enumerate() {
            if let Some(c) = c {
                write!(f, " ({:x} {})", i, c)?;
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
    fn single_child(&self) -> (Option<(ObjPtr<Node>, u8)>, bool) {
        let mut has_chd = false;
        let mut only_chd = None;
        for (i, c) in self.chd.iter().enumerate() {
            if c.is_some() {
                has_chd = true;
                if only_chd.is_some() {
                    only_chd = None;
                    break
                }
                only_chd = (*c).map(|e| (e, i as u8))
            }
        }
        (only_chd, has_chd)
    }

    fn calc_eth_rlp<T: ValueTransformer>(&self, store: &dyn ShaleStore<Node>) -> Vec<u8> {
        let mut stream = rlp::RlpStream::new_list(17);
        for c in self.chd.iter() {
            match c {
                Some(c) => {
                    let mut c_ref = store.get_item(*c).unwrap();
                    if c_ref.get_eth_rlp_long::<T>(store) {
                        let s = stream.append(&&(*c_ref.get_root_hash::<T>(store))[..]);
                        if c_ref.lazy_dirty.get() {
                            c_ref.write(|_| {}).unwrap();
                            c_ref.lazy_dirty.set(false)
                        }
                        s
                    } else {
                        let c_rlp = &c_ref.get_eth_rlp::<T>(store);
                        stream.append_raw(c_rlp, 1)
                    }
                }
                None => stream.append_empty_data(),
            };
        }
        match &self.value {
            Some(val) => stream.append(&val.to_vec()),
            None => stream.append_empty_data(),
        };
        stream.out().into()
    }
}

#[derive(PartialEq, Eq, Clone)]
struct LeafNode(PartialPath, Data);

impl Debug for LeafNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Leaf {:?} {}]", self.0, hex::encode(&*self.1))
    }
}

impl LeafNode {
    fn calc_eth_rlp<T: ValueTransformer>(&self) -> Vec<u8> {
        rlp::encode_list::<Vec<u8>, _>(&[from_nibbles(&self.0.encode(true)).collect(), T::transform(&self.1)]).into()
    }
}

#[derive(PartialEq, Eq, Clone)]
struct ExtNode(PartialPath, ObjPtr<Node>);

impl Debug for ExtNode {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "[Extension {:?} {}]", self.0, self.1)
    }
}

impl ExtNode {
    fn calc_eth_rlp<T: ValueTransformer>(&self, store: &dyn ShaleStore<Node>) -> Vec<u8> {
        let mut r = store.get_item(self.1).unwrap();
        let mut stream = rlp::RlpStream::new_list(2);
        stream.append(&from_nibbles(&self.0.encode(false)).collect::<Vec<_>>());
        if r.get_eth_rlp_long::<T>(store) {
            stream.append(&&(*r.get_root_hash::<T>(store))[..]);
            if r.lazy_dirty.get() {
                r.write(|_| {}).unwrap();
                r.lazy_dirty.set(false)
            }
        } else {
            stream.append_raw(r.get_eth_rlp::<T>(store), 1);
        }
        stream.out().into()
    }
}

#[derive(PartialEq, Eq, Clone)]
pub struct Node {
    root_hash: OnceCell<Hash>,
    eth_rlp_long: OnceCell<bool>,
    eth_rlp: OnceCell<Vec<u8>>,
    lazy_dirty: Cell<bool>,
    inner: NodeType,
}

#[derive(PartialEq, Eq, Clone, Debug, EnumAsInner)]
enum NodeType {
    Branch(BranchNode),
    Leaf(LeafNode),
    Extension(ExtNode),
}

impl NodeType {
    fn calc_eth_rlp<T: ValueTransformer>(&self, store: &dyn ShaleStore<Node>) -> Vec<u8> {
        let eth_rlp = match &self {
            NodeType::Leaf(n) => n.calc_eth_rlp::<T>(),
            NodeType::Extension(n) => n.calc_eth_rlp::<T>(store),
            NodeType::Branch(n) => n.calc_eth_rlp::<T>(store),
        };
        eth_rlp
    }
}

impl Node {
    const BRANCH_NODE: u8 = 0x0;
    const EXT_NODE: u8 = 0x1;
    const LEAF_NODE: u8 = 0x2;

    fn max_branch_node_size() -> u64 {
        const MAX_SIZE: OnceCell<u64> = OnceCell::new();
        *MAX_SIZE.get_or_init(|| {
            Self {
                root_hash: OnceCell::new(),
                eth_rlp_long: OnceCell::new(),
                eth_rlp: OnceCell::new(),
                inner: NodeType::Branch(BranchNode {
                    chd: [Some(ObjPtr::null()); NBRANCH],
                    value: Some(Data(Vec::new())),
                }),
                lazy_dirty: Cell::new(false),
            }
            .dehydrated_len()
        })
    }

    fn get_eth_rlp<T: ValueTransformer>(&self, store: &dyn ShaleStore<Node>) -> &[u8] {
        self.eth_rlp.get_or_init(|| self.inner.calc_eth_rlp::<T>(store))
    }

    fn get_root_hash<T: ValueTransformer>(&self, store: &dyn ShaleStore<Node>) -> &Hash {
        self.root_hash.get_or_init(|| {
            self.lazy_dirty.set(true);
            Hash(sha3::Keccak256::digest(self.get_eth_rlp::<T>(store)).into())
        })
    }

    fn get_eth_rlp_long<T: ValueTransformer>(&self, store: &dyn ShaleStore<Node>) -> bool {
        *self.eth_rlp_long.get_or_init(|| {
            self.lazy_dirty.set(true);
            self.get_eth_rlp::<T>(store).len() >= 32
        })
    }

    fn rehash(&mut self) {
        self.eth_rlp = OnceCell::new();
        self.eth_rlp_long = OnceCell::new();
        self.root_hash = OnceCell::new();
    }

    fn new(inner: NodeType) -> Self {
        let mut s = Self {
            root_hash: OnceCell::new(),
            eth_rlp_long: OnceCell::new(),
            eth_rlp: OnceCell::new(),
            inner,
            lazy_dirty: Cell::new(false),
        };
        s.rehash();
        s
    }

    fn new_from_hash(root_hash: Option<Hash>, eth_rlp_long: Option<bool>, inner: NodeType) -> Self {
        Self {
            root_hash: match root_hash {
                Some(h) => OnceCell::with_value(h),
                None => OnceCell::new(),
            },
            eth_rlp_long: match eth_rlp_long {
                Some(b) => OnceCell::with_value(b),
                None => OnceCell::new(),
            },
            eth_rlp: OnceCell::new(),
            inner,
            lazy_dirty: Cell::new(false),
        }
    }

    const ROOT_HASH_VALID_BIT: u8 = 1 << 0;
    const ETH_RLP_LONG_VALID_BIT: u8 = 1 << 1;
    const ETH_RLP_LONG_BIT: u8 = 1 << 2;
}

impl MummyItem for Node {
    fn hydrate(addr: u64, mem: &dyn MemStore) -> Result<Self, ShaleError> {
        let dec_err = |_| ShaleError::DecodeError;
        const META_SIZE: u64 = 32 + 1 + 1;
        let meta_raw = mem.get_view(addr, META_SIZE).ok_or(ShaleError::LinearMemStoreError)?;
        let attrs = meta_raw[32];
        let root_hash = if attrs & Node::ROOT_HASH_VALID_BIT == 0 {
            None
        } else {
            Some(Hash(meta_raw[0..32].try_into().map_err(dec_err)?))
        };
        let eth_rlp_long = if attrs & Node::ETH_RLP_LONG_VALID_BIT == 0 {
            None
        } else {
            Some(attrs & Node::ETH_RLP_LONG_BIT != 0)
        };
        match meta_raw[33] {
            Self::BRANCH_NODE => {
                let branch_header_size = NBRANCH as u64 * 8 + 4;
                let node_raw = mem
                    .get_view(addr + META_SIZE, branch_header_size)
                    .ok_or(ShaleError::LinearMemStoreError)?;
                let mut cur = Cursor::new(node_raw.deref());
                let mut chd = [None; NBRANCH];
                let mut buff = [0; 8];
                for chd in chd.iter_mut() {
                    cur.read_exact(&mut buff).map_err(|_| ShaleError::DecodeError)?;
                    let addr = u64::from_le_bytes(buff);
                    if addr != 0 {
                        *chd = Some(unsafe { ObjPtr::new_from_addr(addr) })
                    }
                }
                cur.read_exact(&mut buff[..4]).map_err(|_| ShaleError::DecodeError)?;
                let raw_len = u32::from_le_bytes(buff[..4].try_into().map_err(dec_err)?) as u64;
                let value = if raw_len == u32::MAX as u64 {
                    None
                } else {
                    Some(Data(
                        mem.get_view(addr + META_SIZE + branch_header_size, raw_len)
                            .ok_or(ShaleError::LinearMemStoreError)?
                            .to_vec(),
                    ))
                };
                Ok(Self::new_from_hash(
                    root_hash,
                    eth_rlp_long,
                    NodeType::Branch(BranchNode { chd, value }),
                ))
            }
            Self::EXT_NODE => {
                let ext_header_size = 1 + 8;
                let node_raw = mem
                    .get_view(addr + META_SIZE, ext_header_size)
                    .ok_or(ShaleError::LinearMemStoreError)?;
                let mut cur = Cursor::new(node_raw.deref());
                let mut buff = [0; 8];
                cur.read_exact(&mut buff[..1]).map_err(|_| ShaleError::DecodeError)?;
                let len = buff[0] as u64;
                cur.read_exact(&mut buff).map_err(|_| ShaleError::DecodeError)?;
                let ptr = u64::from_le_bytes(buff);
                let nibbles: Vec<_> = to_nibbles(
                    &*mem
                        .get_view(addr + META_SIZE + ext_header_size, len)
                        .ok_or(ShaleError::LinearMemStoreError)?,
                )
                .collect();
                let (path, _) = PartialPath::decode(nibbles);
                Ok(Self::new_from_hash(
                    root_hash,
                    eth_rlp_long,
                    NodeType::Extension(ExtNode(path, unsafe { ObjPtr::new_from_addr(ptr) })),
                ))
            }
            Self::LEAF_NODE => {
                let leaf_header_size = 1 + 4;
                let node_raw = mem
                    .get_view(addr + META_SIZE, leaf_header_size)
                    .ok_or(ShaleError::LinearMemStoreError)?;
                let mut cur = Cursor::new(node_raw.deref());
                let mut buff = [0; 4];
                cur.read_exact(&mut buff[..1]).map_err(|_| ShaleError::DecodeError)?;
                let path_len = buff[0] as u64;
                cur.read_exact(&mut buff).map_err(|_| ShaleError::DecodeError)?;
                let data_len = u32::from_le_bytes(buff) as u64;
                let remainder = mem
                    .get_view(addr + META_SIZE + leaf_header_size, path_len + data_len)
                    .ok_or(ShaleError::LinearMemStoreError)?;
                let nibbles: Vec<_> = to_nibbles(&remainder[..path_len as usize]).collect();
                let (path, _) = PartialPath::decode(nibbles);
                let value = Data(remainder[path_len as usize..].to_vec());
                Ok(Self::new_from_hash(
                    root_hash,
                    eth_rlp_long,
                    NodeType::Leaf(LeafNode(path, value)),
                ))
            }
            _ => Err(ShaleError::DecodeError),
        }
    }

    fn dehydrated_len(&self) -> u64 {
        32 + 1 +
            1 +
            match &self.inner {
                NodeType::Branch(n) => {
                    NBRANCH as u64 * 8 +
                        4 +
                        match &n.value {
                            Some(val) => val.len() as u64,
                            None => 0,
                        }
                }
                NodeType::Extension(n) => 1 + 8 + n.0.dehydrated_len(),
                NodeType::Leaf(n) => 1 + 4 + n.0.dehydrated_len() + n.1.len() as u64,
            }
    }

    fn dehydrate(&self, to: &mut [u8]) {
        let mut cur = Cursor::new(to);

        let mut attrs = 0;
        attrs |= match self.root_hash.get() {
            Some(h) => {
                cur.write_all(&h.0).unwrap();
                Node::ROOT_HASH_VALID_BIT
            }
            None => {
                cur.write_all(&[0; 32]).unwrap();
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
                        Some(p) => p.addr().to_le_bytes(),
                        None => 0u64.to_le_bytes(),
                    })
                    .unwrap();
                }
                match &n.value {
                    Some(val) => {
                        cur.write_all(&(val.len() as u32).to_le_bytes()).unwrap();
                        cur.write_all(&val).unwrap();
                    }
                    None => {
                        cur.write_all(&u32::MAX.to_le_bytes()).unwrap();
                    }
                }
            }
            NodeType::Extension(n) => {
                cur.write_all(&[Self::EXT_NODE]).unwrap();
                let path: Vec<u8> = from_nibbles(&n.0.encode(false)).collect();
                cur.write_all(&[path.len() as u8]).unwrap();
                cur.write_all(&n.1.addr().to_le_bytes()).unwrap();
                cur.write_all(&path).unwrap();
            }
            NodeType::Leaf(n) => {
                cur.write_all(&[Self::LEAF_NODE]).unwrap();
                let path: Vec<u8> = from_nibbles(&n.0.encode(true)).collect();
                cur.write_all(&[path.len() as u8]).unwrap();
                cur.write_all(&(n.1.len() as u32).to_le_bytes()).unwrap();
                cur.write_all(&path).unwrap();
                cur.write_all(&n.1).unwrap();
            }
        }
    }
}

#[test]
fn test_merkle_node_encoding() {
    let check = |node: Node| {
        let mut bytes = Vec::new();
        bytes.resize(node.dehydrated_len() as usize, 0);
        node.dehydrate(&mut bytes);

        let mem = shale::PlainMem::new(bytes.len() as u64, 0x0);
        mem.write(0, &bytes);
        println!("{:?}", bytes);
        let node_ = Node::hydrate(0, &mem).unwrap();
        assert!(node == node_);
    };
    let chd0 = [None; NBRANCH];
    let mut chd1 = chd0;
    for i in 0..NBRANCH / 2 {
        chd1[i] = Some(unsafe { ObjPtr::new_from_addr(0xa) });
    }
    for node in [
        Node::new_from_hash(
            None,
            None,
            NodeType::Leaf(LeafNode(PartialPath(vec![0x1, 0x2, 0x3]), Data(vec![0x4, 0x5]))),
        ),
        Node::new_from_hash(
            None,
            None,
            NodeType::Extension(ExtNode(PartialPath(vec![0x1, 0x2, 0x3]), unsafe {
                ObjPtr::new_from_addr(0x42)
            })),
        ),
        Node::new_from_hash(
            None,
            None,
            NodeType::Branch(BranchNode {
                chd: chd0,
                value: Some(Data("hello, world!".as_bytes().to_vec())),
            }),
        ),
        Node::new_from_hash(None, None, NodeType::Branch(BranchNode { chd: chd1, value: None })),
    ] {
        check(node);
    }
}

macro_rules! write_node {
    ($self: expr, $r: expr, $modify: expr, $parents: expr, $deleted: expr) => {
        if let None = $r.write($modify) {
            let ptr = $self.new_node($r.clone())?.as_ptr();
            $self.set_parent(ptr, $parents);
            $deleted.push($r.as_ptr());
            true
        } else {
            false
        }
    };
}

pub struct Merkle {
    store: Box<dyn ShaleStore<Node>>,
}

impl Merkle {
    fn get_node(&self, ptr: ObjPtr<Node>) -> Result<ObjRef<Node>, MerkleError> {
        self.store.get_item(ptr).map_err(MerkleError::Shale)
    }
    fn new_node(&self, item: Node) -> Result<ObjRef<Node>, MerkleError> {
        self.store.put_item(item, 0).map_err(MerkleError::Shale)
    }
    fn free_node(&mut self, ptr: ObjPtr<Node>) -> Result<(), MerkleError> {
        self.store.free_item(ptr).map_err(MerkleError::Shale)
    }
}

impl Merkle {
    pub fn new(store: Box<dyn ShaleStore<Node>>) -> Self {
        Self { store }
    }

    pub fn init_root(root: &mut ObjPtr<Node>, store: &dyn ShaleStore<Node>) -> Result<(), MerkleError> {
        Ok(*root = store
            .put_item(
                Node::new(NodeType::Branch(BranchNode {
                    chd: [None; NBRANCH],
                    value: None,
                })),
                Node::max_branch_node_size(),
            )
            .map_err(MerkleError::Shale)?
            .as_ptr())
    }

    pub fn get_store(&self) -> &dyn ShaleStore<Node> {
        self.store.as_ref()
    }

    pub fn empty_root() -> &'static Hash {
        use once_cell::sync::OnceCell;
        static V: OnceCell<Hash> = OnceCell::new();
        V.get_or_init(|| {
            Hash(
                hex::decode("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
                    .unwrap()
                    .try_into()
                    .unwrap(),
            )
        })
    }

    pub fn root_hash<T: ValueTransformer>(&self, root: ObjPtr<Node>) -> Result<Hash, MerkleError> {
        let root = self
            .get_node(root)?
            .inner
            .as_branch()
            .ok_or(MerkleError::NotBranchNode)?
            .chd[0];
        Ok(if let Some(root) = root {
            let mut node = self.get_node(root)?;
            let res = node.get_root_hash::<T>(self.store.as_ref()).clone();
            if node.lazy_dirty.get() {
                node.write(|_| {}).unwrap();
                node.lazy_dirty.set(false)
            }
            res
        } else {
            Self::empty_root().clone()
        })
    }

    fn dump_(&self, u: ObjPtr<Node>, w: &mut dyn Write) -> Result<(), MerkleError> {
        let u_ref = self.get_node(u)?;
        write!(
            w,
            "{} => {}: ",
            u,
            match u_ref.root_hash.get() {
                Some(h) => hex::encode(&**h),
                None => "<lazy>".to_string(),
            }
        )
        .map_err(MerkleError::Format)?;
        match &u_ref.inner {
            NodeType::Branch(n) => {
                writeln!(w, "{:?}", n).map_err(MerkleError::Format)?;
                for c in n.chd.iter() {
                    if let Some(c) = c {
                        self.dump_(*c, w)?
                    }
                }
            }
            NodeType::Leaf(n) => writeln!(w, "{:?}", n).unwrap(),
            NodeType::Extension(n) => {
                writeln!(w, "{:?}", n).map_err(MerkleError::Format)?;
                self.dump_(n.1, w)?
            }
        }
        Ok(())
    }

    pub fn dump(&self, root: ObjPtr<Node>, w: &mut dyn Write) -> Result<(), MerkleError> {
        Ok(if root.is_null() {
            write!(w, "<Empty>").map_err(MerkleError::Format)?;
        } else {
            self.dump_(root, w)?;
        })
    }

    fn set_parent<'b>(&self, new_chd: ObjPtr<Node>, parents: &mut [(ObjRef<'b, Node>, u8)]) {
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

    fn split<'b>(
        &self, mut u_ref: ObjRef<'b, Node>, parents: &mut [(ObjRef<'b, Node>, u8)], rem_path: &[u8], n_path: Vec<u8>,
        n_value: Option<Data>, val: Vec<u8>, deleted: &mut Vec<ObjPtr<Node>>,
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
                let t = NodeType::Branch(BranchNode { chd, value: None });
                let branch_ptr = self.new_node(Node::new(t))?.as_ptr();
                if idx > 0 {
                    self.new_node(Node::new(NodeType::Extension(ExtNode(
                        PartialPath(rem_path[..idx].to_vec()),
                        branch_ptr,
                    ))))?
                    .as_ptr()
                } else {
                    branch_ptr
                }
            }
            None => {
                if rem_path.len() == n_path.len() {
                    let mut err = None;
                    write_node!(
                        self,
                        u_ref,
                        |u| {
                            match &mut u.inner {
                                NodeType::Leaf(u) => u.1 = Data(val),
                                NodeType::Extension(u) => {
                                    match (|| {
                                        let mut b_ref = self.get_node(u.1)?;
                                        if let None = b_ref.write(|b| {
                                            b.inner.as_branch_mut().unwrap().value = Some(Data(val));
                                            b.rehash()
                                        }) {
                                            u.1 = self.new_node(b_ref.clone())?.as_ptr();
                                            deleted.push(b_ref.as_ptr());
                                        }
                                        Ok(())
                                    })() {
                                        Err(e) => err = Some(Err(e)),
                                        _ => (),
                                    }
                                }
                                _ => unreachable!(),
                            }
                            u.rehash();
                        },
                        parents,
                        deleted
                    );
                    return err.unwrap_or(Ok(None))
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
                        return Ok(Some(val))
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
                    .new_node(Node::new(NodeType::Branch(BranchNode { chd, value: v })))?
                    .as_ptr();
                if prefix.len() > 0 {
                    self.new_node(Node::new(NodeType::Extension(ExtNode(
                        PartialPath(prefix.to_vec()),
                        branch_ptr,
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

    pub fn insert<K: AsRef<[u8]>>(&mut self, key: K, val: Vec<u8>, root: ObjPtr<Node>) -> Result<(), MerkleError> {
        let mut deleted = Vec::new();
        let mut chunks = vec![0];
        chunks.extend(to_nibbles(key.as_ref()));
        let mut parents = Vec::new();
        let mut u_ref = Some(self.get_node(root)?);
        let mut nskip = 0;
        let mut val = Some(val);
        for (i, nib) in chunks.iter().enumerate() {
            if nskip > 0 {
                nskip -= 1;
                continue
            }
            let mut u = u_ref.take().unwrap();
            let u_ptr = u.as_ptr();
            let next_ptr = match &u.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => {
                        // insert the leaf to the empty slot
                        let leaf_ptr = self
                            .new_node(Node::new(NodeType::Leaf(LeafNode(
                                PartialPath(chunks[i + 1..].to_vec()),
                                Data(val.take().unwrap()),
                            ))))?
                            .as_ptr();
                        u.write(|u| {
                            let uu = u.inner.as_branch_mut().unwrap();
                            uu.chd[*nib as usize] = Some(leaf_ptr);
                            u.rehash();
                        })
                        .unwrap();
                        break
                    }
                },
                NodeType::Leaf(n) => {
                    let n_path = n.0.to_vec();
                    let n_value = Some(n.1.clone());
                    self.split(
                        u,
                        &mut parents,
                        &chunks[i..],
                        n_path,
                        n_value,
                        val.take().unwrap(),
                        &mut deleted,
                    )?;
                    break
                }
                NodeType::Extension(n) => {
                    let n_path = n.0.to_vec();
                    let n_ptr = n.1;
                    nskip = n_path.len() - 1;
                    if let Some(v) = self.split(
                        u,
                        &mut parents,
                        &chunks[i..],
                        n_path,
                        None,
                        val.take().unwrap(),
                        &mut deleted,
                    )? {
                        val = Some(v);
                        u = self.get_node(u_ptr)?;
                        n_ptr
                    } else {
                        break
                    }
                }
            };

            parents.push((u, *nib));
            u_ref = Some(self.get_node(next_ptr)?);
        }
        if val.is_some() {
            let mut info = None;
            let u_ptr = {
                let mut u = u_ref.take().unwrap();
                write_node!(
                    self,
                    &mut u,
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
                    })))?
                    .as_ptr();
                self.set_parent(branch, &mut parents);
            }
        }

        drop(u_ref);

        for (mut r, _) in parents.into_iter().rev() {
            r.write(|u| u.rehash()).unwrap();
        }

        for ptr in deleted.into_iter() {
            self.free_node(ptr)?
        }
        Ok(())
    }

    fn after_remove_leaf<'b>(
        &self, parents: &mut Vec<(ObjRef<'b, Node>, u8)>, deleted: &mut Vec<ObjPtr<Node>>,
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
            if (has_chd && (b_chd.is_none() || b_inner.value.is_some())) || parents.len() < 1 {
                return Ok(())
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
                        .new_node(Node::new(NodeType::Leaf(LeafNode(PartialPath(Vec::new()), val))))?
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
                                .new_node(Node::new(NodeType::Extension(ExtNode(PartialPath(vec![idx]), c_ptr))))?
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
                                    let mut pp = p.inner.as_extension_mut().unwrap();
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
                            let c_ptr = if let None = c_ref.write(|c| {
                                (match &mut c.inner {
                                    NodeType::Leaf(n) => &mut n.0,
                                    NodeType::Extension(n) => &mut n.0,
                                    _ => unreachable!(),
                                })
                                .0
                                .insert(0, idx);
                                c.rehash()
                            }) {
                                deleted.push(c_ptr);
                                self.new_node(c_ref.clone())?.as_ptr()
                            } else {
                                c_ptr
                            };
                            drop(c_ref);
                            p_ref
                                .write(|p| {
                                    p.inner.as_branch_mut().unwrap().chd[p_idx as usize] = Some(c_ptr);
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
                            if !write_node!(
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
                            ) {
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

    fn after_remove_branch<'b>(
        &self, (c_ptr, idx): (ObjPtr<Node>, u8), parents: &mut Vec<(ObjRef<'b, Node>, u8)>,
        deleted: &mut Vec<ObjPtr<Node>>,
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
                        match (|| {
                            match &mut b.inner {
                                NodeType::Branch(n) => {
                                    // from: [Branch] -> [Branch]x -> [Branch]
                                    // to: [Branch] -> [Ext] -> [Branch]
                                    n.chd[b_idx as usize] = Some(
                                        self.new_node(Node::new(NodeType::Extension(ExtNode(
                                            PartialPath(vec![idx]),
                                            c_ptr,
                                        ))))?
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
                            Ok(b.rehash())
                        })() {
                            Err(e) => err = Some(Err(e)),
                            _ => (),
                        }
                    },
                    parents,
                    deleted
                );
                if let Some(e) = err {
                    return e
                }
            }
            NodeType::Leaf(_) | NodeType::Extension(_) => match &b_ref.inner {
                NodeType::Branch(_) => {
                    // from: [Branch] -> [Branch]x -> [Leaf/Ext]
                    // to: [Branch] -> [Leaf/Ext]
                    let c_ptr = if let None = c_ref.write(|c| {
                        match &mut c.inner {
                            NodeType::Leaf(n) => &mut n.0,
                            NodeType::Extension(n) => &mut n.0,
                            _ => unreachable!(),
                        }
                        .0
                        .insert(0, idx);
                        c.rehash()
                    }) {
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
                    let c_ptr = if let None = c_ref.write(|c| {
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
                    }) {
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

    pub fn remove<K: AsRef<[u8]>>(&mut self, key: K, root: ObjPtr<Node>) -> Result<Option<Vec<u8>>, MerkleError> {
        let mut chunks = vec![0];
        chunks.extend(to_nibbles(key.as_ref()));

        if root.is_null() {
            return Ok(None)
        }

        let mut deleted = Vec::new();
        let mut parents: Vec<(ObjRef<Node>, _)> = Vec::new();
        let mut u_ref = self.get_node(root)?;
        let mut nskip = 0;
        let mut found = None;

        for (i, nib) in chunks.iter().enumerate() {
            if nskip > 0 {
                nskip -= 1;
                continue
            }
            let next_ptr = match &u_ref.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => return Ok(None),
                },
                NodeType::Leaf(n) => {
                    if &chunks[i..] != &*n.0 {
                        return Ok(None)
                    }
                    found = Some(n.1.clone());
                    deleted.push(u_ref.as_ptr());
                    self.after_remove_leaf(&mut parents, &mut deleted)?;
                    break
                }
                NodeType::Extension(n) => {
                    let n_path = &*n.0;
                    let rem_path = &chunks[i..];
                    if rem_path < n_path || &rem_path[..n_path.len()] != n_path {
                        return Ok(None)
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
                        return Ok(None)
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
                        return Ok(None)
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

    fn remove_tree_(&self, u: ObjPtr<Node>, deleted: &mut Vec<ObjPtr<Node>>) -> Result<(), MerkleError> {
        let u_ref = self.get_node(u)?;
        match &u_ref.inner {
            NodeType::Branch(n) => {
                for c in n.chd.iter() {
                    if let Some(c) = c {
                        self.remove_tree_(*c, deleted)?
                    }
                }
            }
            NodeType::Leaf(_) => (),
            NodeType::Extension(n) => self.remove_tree_(n.1, deleted)?,
        }
        deleted.push(u);
        Ok(())
    }

    pub fn remove_tree(&mut self, root: ObjPtr<Node>) -> Result<(), MerkleError> {
        let mut deleted = Vec::new();
        if root.is_null() {
            return Ok(())
        }
        self.remove_tree_(root, &mut deleted)?;
        for ptr in deleted.into_iter() {
            self.free_node(ptr)?;
        }
        Ok(())
    }

    pub fn get_mut<K: AsRef<[u8]>>(&mut self, key: K, root: ObjPtr<Node>) -> Result<Option<RefMut>, MerkleError> {
        let mut chunks = vec![0];
        chunks.extend(to_nibbles(key.as_ref()));
        let mut parents = Vec::new();

        if root.is_null() {
            return Ok(None)
        }

        let mut u_ref = self.get_node(root)?;
        let mut nskip = 0;

        for (i, nib) in chunks.iter().enumerate() {
            let u_ptr = u_ref.as_ptr();
            if nskip > 0 {
                nskip -= 1;
                continue
            }
            let next_ptr = match &u_ref.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => return Ok(None),
                },
                NodeType::Leaf(n) => {
                    if &chunks[i..] != &*n.0 {
                        return Ok(None)
                    }
                    drop(u_ref);
                    return Ok(Some(RefMut::new(u_ptr, parents, self)))
                }
                NodeType::Extension(n) => {
                    let n_path = &*n.0;
                    let rem_path = &chunks[i..];
                    if rem_path.len() < n_path.len() || &rem_path[..n_path.len()] != n_path {
                        return Ok(None)
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
                if let Some(_) = n.value.as_ref() {
                    drop(u_ref);
                    return Ok(Some(RefMut::new(u_ptr, parents, self)))
                }
            }
            NodeType::Leaf(n) => {
                if n.0.len() == 0 {
                    drop(u_ref);
                    return Ok(Some(RefMut::new(u_ptr, parents, self)))
                }
            }
            _ => (),
        }

        Ok(None)
    }

    pub fn get<K: AsRef<[u8]>>(&self, key: K, root: ObjPtr<Node>) -> Result<Option<Ref>, MerkleError> {
        let mut chunks = vec![0];
        chunks.extend(to_nibbles(key.as_ref()));

        if root.is_null() {
            return Ok(None)
        }

        let mut u_ref = self.get_node(root)?;
        let mut nskip = 0;

        for (i, nib) in chunks.iter().enumerate() {
            if nskip > 0 {
                nskip -= 1;
                continue
            }
            let next_ptr = match &u_ref.inner {
                NodeType::Branch(n) => match n.chd[*nib as usize] {
                    Some(c) => c,
                    None => return Ok(None),
                },
                NodeType::Leaf(n) => {
                    if &chunks[i..] != &*n.0 {
                        return Ok(None)
                    }
                    return Ok(Some(Ref(u_ref)))
                }
                NodeType::Extension(n) => {
                    let n_path = &*n.0;
                    let rem_path = &chunks[i..];
                    if rem_path.len() < n_path.len() || &rem_path[..n_path.len()] != n_path {
                        return Ok(None)
                    }
                    nskip = n_path.len() - 1;
                    n.1
                }
            };
            u_ref = self.get_node(next_ptr)?;
        }

        match &u_ref.inner {
            NodeType::Branch(n) => {
                if let Some(_) = n.value.as_ref() {
                    return Ok(Some(Ref(u_ref)))
                }
            }
            NodeType::Leaf(n) => {
                if n.0.len() == 0 {
                    return Ok(Some(Ref(u_ref)))
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

pub struct RefMut<'a> {
    ptr: ObjPtr<Node>,
    parents: Vec<(ObjPtr<Node>, u8)>,
    merkle: &'a mut Merkle,
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

impl<'a> RefMut<'a> {
    fn new(ptr: ObjPtr<Node>, parents: Vec<(ObjPtr<Node>, u8)>, merkle: &'a mut Merkle) -> Self {
        Self { ptr, parents, merkle }
    }

    pub fn get<'b>(&'b self) -> Ref<'b> {
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

fn to_nibbles<'a>(bytes: &'a [u8]) -> impl Iterator<Item = u8> + 'a {
    bytes.iter().flat_map(|b| [(b >> 4) & 0xf, b & 0xf].into_iter())
}

fn from_nibbles<'a>(nibbles: &'a [u8]) -> impl Iterator<Item = u8> + 'a {
    assert!(nibbles.len() & 1 == 0);
    nibbles.chunks_exact(2).map(|p| (p[0] << 4) | p[1])
}

#[test]
fn test_to_nibbles() {
    for (bytes, nibbles) in [
        (vec![0x12, 0x34, 0x56], vec![0x1, 0x2, 0x3, 0x4, 0x5, 0x6]),
        (vec![0xc0, 0xff], vec![0xc, 0x0, 0xf, 0xf]),
    ] {
        let n: Vec<_> = to_nibbles(&bytes).collect();
        assert_eq!(n, nibbles);
    }
}
