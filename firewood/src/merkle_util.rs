// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::merkle::{BinarySerde, Bincode, Merkle, Node, Proof, ProofError, Ref, RefMut, TrieHash};
use crate::shale::{
    self, cached::DynamicMem, compact::CompactSpace, disk_address::DiskAddress, CachedStore,
    ShaleStore, StoredView,
};
use std::num::NonZeroUsize;
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

pub struct MerkleSetup<S, T> {
    root: DiskAddress,
    merkle: Merkle<S, T>,
}

impl<S: ShaleStore<Node> + Send + Sync, T: BinarySerde> MerkleSetup<S, T> {
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

    pub fn get_mut<K: AsRef<[u8]>>(
        &mut self,
        key: K,
    ) -> Result<Option<RefMut<S, T>>, DataStoreError> {
        self.merkle
            .get_mut(key, self.root)
            .map_err(|_err| DataStoreError::GetError)
    }

    pub const fn get_sentinel_address(&self) -> DiskAddress {
        self.root
    }

    pub fn get_merkle_mut(&mut self) -> &mut Merkle<S, T> {
        &mut self.merkle
    }

    pub fn root_hash(&self) -> Result<TrieHash, DataStoreError> {
        self.merkle
            .root_hash(self.root)
            .map_err(|_err| DataStoreError::RootHashError)
    }

    pub fn dump(&self) -> Result<String, DataStoreError> {
        let mut s = Vec::new();
        self.merkle
            .dump(self.root, &mut s)
            .map_err(|_err| DataStoreError::DumpError)?;
        String::from_utf8(s).map_err(|_err| DataStoreError::UTF8Error)
    }

    pub fn prove<K: AsRef<[u8]>>(&self, key: K) -> Result<Proof<Vec<u8>>, DataStoreError> {
        self.merkle
            .prove(key, self.root)
            .map_err(|_err| DataStoreError::ProofError)
    }

    pub fn verify_proof<N: AsRef<[u8]> + Send, K: AsRef<[u8]>>(
        &self,
        key: K,
        proof: &Proof<N>,
    ) -> Result<Option<Vec<u8>>, DataStoreError> {
        let hash: [u8; 32] = *self.root_hash()?;
        proof
            .verify(key, hash)
            .map_err(|_err| DataStoreError::ProofVerificationError)
    }

    pub fn verify_range_proof<N: AsRef<[u8]> + Send, K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self,
        proof: &Proof<N>,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        vals: Vec<V>,
    ) -> Result<bool, ProofError> {
        let hash: [u8; 32] = *self.root_hash()?;
        proof.verify_range_proof(hash, first_key, last_key, keys, vals)
    }
}

pub fn new_merkle(
    meta_size: u64,
    compact_size: u64,
) -> MerkleSetup<CompactSpace<Node, DynamicMem>, Bincode> {
    const RESERVED: usize = 0x1000;
    assert!(meta_size as usize > RESERVED);
    assert!(compact_size as usize > RESERVED);
    let mut dm = DynamicMem::new(meta_size, 0);
    let compact_header = DiskAddress::null();
    #[allow(clippy::unwrap_used)]
    dm.write(
        compact_header.into(),
        &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
            NonZeroUsize::new(RESERVED).unwrap(),
            #[allow(clippy::unwrap_used)]
            NonZeroUsize::new(RESERVED).unwrap(),
        ))
        .unwrap(),
    );
    #[allow(clippy::unwrap_used)]
    let compact_header =
        StoredView::ptr_to_obj(&dm, compact_header, shale::compact::CompactHeader::MSIZE).unwrap();
    let mem_meta = dm;
    let mem_payload = DynamicMem::new(compact_size, 0x1);

    let cache = shale::ObjCache::new(1);
    let space =
        shale::compact::CompactSpace::new(mem_meta, mem_payload, compact_header, cache, 10, 16)
            .expect("CompactSpace init fail");

    let merkle = Merkle::new(Box::new(space));
    #[allow(clippy::unwrap_used)]
    let root = merkle.init_root().unwrap();

    MerkleSetup { root, merkle }
}
