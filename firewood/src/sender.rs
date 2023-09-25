// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::api::DB;

pub struct Sender {}

impl<K: AsRef<[u8]>, V: AsRef<[u8]>> DB<K, V> for Sender {
    fn kv_root_hash(&self) -> Result<crate::merkle::Hash, crate::db::DbError> {
        todo!()
    }

    fn kv_get(&self, key: K) -> Option<Vec<u8>> {
        todo!()
    }

    fn kv_dump<W: std::io::Write>(&self, writer: W) -> Result<(), crate::db::DbError> {
        todo!()
    }

    fn root_hash(&self) -> Result<crate::merkle::Hash, crate::db::DbError> {
        todo!()
    }

    fn dump<W: std::io::Write>(&self, writer: W) -> Result<(), crate::db::DbError> {
        todo!()
    }

    fn prove(&self, key: K) -> Result<crate::proof::Proof, crate::merkle::MerkleError> {
        todo!()
    }

    fn verify_range_proof(
        &self,
        proof: crate::proof::Proof,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        values: Vec<V>,
    ) {
        todo!()
    }

    fn exist(&self, key: K) -> Result<bool, crate::db::DbError> {
        todo!()
    }
}
