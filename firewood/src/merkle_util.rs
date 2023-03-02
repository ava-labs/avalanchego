use crate::dynamic_mem::DynamicMem;
use crate::merkle::*;
use crate::proof::Proof;
use shale::{compact::CompactSpaceHeader, MemStore, MummyObj, ObjPtr};
use std::rc::Rc;

use thiserror::Error;

#[derive(Error, Debug, PartialEq)]
pub enum DataStoreError {
    #[error("failed to insert data")]
    InsertionError,
    #[error("failed to remove data")]
    RemovalError,
    #[error("failed to get data")]
    GetError,
    #[error("failed to generate root hash")]
    RootHashError,
    #[error("failed to dump data")]
    DumpError,
    #[error("invalid utf8")]
    UTF8Error,
    #[error("bad proof")]
    ProofError,
    #[error("failed to verify proof")]
    ProofVerificationError,
    #[error("no keys or values found in proof")]
    ProofEmptyKeyValuesError,
}

pub struct MerkleSetup {
    root: ObjPtr<Node>,
    merkle: Merkle,
}

impl MerkleSetup {
    pub fn insert<K: AsRef<[u8]>>(&mut self, key: K, val: Vec<u8>) -> Result<(), DataStoreError> {
        self.merkle
            .insert(key, val, self.root)
            .map_err(|_err| DataStoreError::InsertionError)
    }

    pub fn remove<K: AsRef<[u8]>>(&mut self, key: K) -> Result<Option<Vec<u8>>, DataStoreError> {
        self.merkle
            .remove(key, self.root)
            .map_err(|_err| DataStoreError::RemovalError)
    }

    pub fn get<K: AsRef<[u8]>>(&self, key: K) -> Result<Option<Ref>, DataStoreError> {
        self.merkle
            .get(key, self.root)
            .map_err(|_err| DataStoreError::GetError)
    }

    pub fn get_mut<K: AsRef<[u8]>>(&mut self, key: K) -> Result<Option<RefMut>, DataStoreError> {
        self.merkle
            .get_mut(key, self.root)
            .map_err(|_err| DataStoreError::GetError)
    }

    pub fn get_root(&self) -> ObjPtr<Node> {
        self.root
    }

    pub fn get_merkle_mut(&mut self) -> &mut Merkle {
        &mut self.merkle
    }

    pub fn root_hash(&self) -> Result<Hash, DataStoreError> {
        self.merkle
            .root_hash::<IdTrans>(self.root)
            .map_err(|_err| DataStoreError::RootHashError)
    }

    pub fn dump(&self) -> Result<String, DataStoreError> {
        let mut s = Vec::new();
        self.merkle
            .dump(self.root, &mut s)
            .map_err(|_err| DataStoreError::DumpError)?;
        String::from_utf8(s).map_err(|_err| DataStoreError::UTF8Error)
    }

    pub fn prove<K: AsRef<[u8]>>(&self, key: K) -> Result<Proof, DataStoreError> {
        self.merkle
            .prove::<K, IdTrans>(key, self.root)
            .map_err(|_err| DataStoreError::ProofError)
    }

    pub fn verify_proof<K: AsRef<[u8]>>(
        &self,
        key: K,
        proof: &Proof,
    ) -> Result<Option<Vec<u8>>, DataStoreError> {
        let hash: [u8; 32] = *self.root_hash()?;
        proof
            .verify_proof(key, hash)
            .map_err(|_err| DataStoreError::ProofVerificationError)
    }

    pub fn verify_range_proof<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self,
        proof: &Proof,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        vals: Vec<V>,
    ) -> Result<bool, DataStoreError> {
        let hash: [u8; 32] = *self.root_hash()?;
        proof
            .verify_range_proof(hash, first_key, last_key, keys, vals)
            .map_err(|_err| DataStoreError::ProofVerificationError)
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
        MummyObj::ptr_to_obj(
            mem_meta.as_ref(),
            compact_header,
            shale::compact::CompactHeader::MSIZE,
        )
        .unwrap()
    };

    let cache = shale::ObjCache::new(1);
    let space =
        shale::compact::CompactSpace::new(mem_meta, mem_payload, compact_header, cache, 10, 16)
            .expect("CompactSpace init fail");
    let mut root = ObjPtr::null();
    Merkle::init_root(&mut root, &space).unwrap();
    MerkleSetup {
        root,
        merkle: Merkle::new(Box::new(space)),
    }
}
