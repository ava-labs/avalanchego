use crate::dynamic_mem::DynamicMem;
use crate::merkle::*;
use crate::proof::Proof;
use shale::{compact::CompactSpaceHeader, MemStore, MummyObj, ObjPtr};
use std::rc::Rc;

pub struct MerkleSetup {
    root: ObjPtr<Node>,
    merkle: Merkle,
}

impl MerkleSetup {
    pub fn insert<K: AsRef<[u8]>>(&mut self, key: K, val: Vec<u8>) {
        self.merkle.insert(key, val, self.root).unwrap()
    }

    pub fn remove<K: AsRef<[u8]>>(&mut self, key: K) {
        self.merkle.remove(key, self.root).unwrap();
    }

    pub fn get<K: AsRef<[u8]>>(&self, key: K) -> Option<Ref> {
        self.merkle.get(key, self.root).unwrap()
    }

    pub fn get_mut<K: AsRef<[u8]>>(&mut self, key: K) -> Option<RefMut> {
        self.merkle.get_mut(key, self.root).unwrap()
    }

    pub fn get_root(&self) -> ObjPtr<Node> {
        self.root
    }

    pub fn get_merkle_mut(&mut self) -> &mut Merkle {
        &mut self.merkle
    }

    pub fn root_hash(&self) -> Hash {
        self.merkle.root_hash::<IdTrans>(self.root).unwrap()
    }

    pub fn dump(&self) -> String {
        let mut s = Vec::new();
        self.merkle.dump(self.root, &mut s).unwrap();
        String::from_utf8(s).unwrap()
    }

    pub fn prove<K: AsRef<[u8]>>(&self, key: K) -> Proof {
        self.merkle.prove::<K, IdTrans>(key, self.root).unwrap()
    }

    pub fn verify_proof<K: AsRef<[u8]>>(&self, key: K, proof: &Proof) -> Option<Vec<u8>> {
        let hash: [u8; 32] = self.root_hash().0;
        proof.verify_proof(key, hash).unwrap()
    }

    pub fn verify_range_proof<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self, proof: &Proof, first_key: K, last_key: K, keys: Vec<K>, vals: Vec<V>,
    ) -> bool {
        let hash: [u8; 32] = self.root_hash().0;
        proof.verify_range_proof(hash, first_key, last_key, keys, vals).unwrap()
    }
}

pub fn new_merkle(meta_size: u64, compact_size: u64) -> MerkleSetup {
    const RESERVED: u64 = 0x1000;
    assert!(meta_size > RESERVED);
    assert!(compact_size > RESERVED);
    let mem_meta = Rc::new(DynamicMem::new(meta_size, 0x0)) as Rc<dyn MemStore>;
    let mem_payload = Rc::new(DynamicMem::new(compact_size, 0x1));
    let compact_header: ObjPtr<CompactSpaceHeader> = unsafe { ObjPtr::new_from_addr(0x0) };

    mem_meta.write(
        compact_header.addr(),
        &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(RESERVED, RESERVED)),
    );

    let compact_header = unsafe {
        MummyObj::ptr_to_obj(mem_meta.as_ref(), compact_header, shale::compact::CompactHeader::MSIZE).unwrap()
    };

    let cache = shale::ObjCache::new(1);
    let space = shale::compact::CompactSpace::new(mem_meta, mem_payload, compact_header, cache, 10, 16)
        .expect("CompactSpace init fail");
    let mut root = ObjPtr::null();
    Merkle::init_root(&mut root, &space).unwrap();
    MerkleSetup {
        root,
        merkle: Merkle::new(Box::new(space)),
    }
}
