// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::io::Write;

use crate::db::DbError;
use crate::merkle::MerkleError;
use crate::merkle::TrieHash;
use crate::v2::api::Proof;

use async_trait::async_trait;

pub type Nonce = u64;

#[async_trait]
pub trait Db<R: Revision<N>, N: Send> {
    async fn get_revision(&self, root_hash: TrieHash) -> Option<R>;
}

#[async_trait]
pub trait Revision<N>
where
    Self: Sized,
{
    async fn kv_root_hash(&self) -> Result<TrieHash, DbError>;
    async fn kv_get<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DbError>;

    async fn kv_dump<W: Write + Send + Sync>(&self, writer: W) -> Result<(), DbError>;
    async fn root_hash(&self) -> Result<TrieHash, DbError>;
    async fn dump<W: Write + Send + Sync>(&self, writer: W) -> Result<(), DbError>;

    async fn prove<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Proof<N>, MerkleError>;
    async fn verify_range_proof<K: AsRef<[u8]> + Send + Sync>(
        &self,
        proof: Proof<N>,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        values: Vec<K>,
    );
}
