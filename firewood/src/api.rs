// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::io::Write;

use crate::db::DbError;
use crate::merkle::TrieHash;
#[cfg(feature = "proof")]
use crate::{merkle::MerkleError, proof::Proof};

use async_trait::async_trait;

pub type Nonce = u64;

#[async_trait]
pub trait Db<R: Revision> {
    async fn get_revision(&self, root_hash: TrieHash) -> Option<R>;
}

#[async_trait]
pub trait Revision
where
    Self: Sized,
{
    async fn kv_root_hash(&self) -> Result<TrieHash, DbError>;
    async fn kv_get<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DbError>;

    async fn kv_dump<W: Write + Send + Sync>(&self, writer: W) -> Result<(), DbError>;
    async fn root_hash(&self) -> Result<TrieHash, DbError>;
    async fn dump<W: Write + Send + Sync>(&self, writer: W) -> Result<(), DbError>;

    #[cfg(feature = "proof")]
    async fn prove<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Proof, MerkleError>;
    #[cfg(feature = "proof")]
    async fn verify_range_proof<K: AsRef<[u8]> + Send + Sync>(
        &self,
        proof: Proof,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        values: Vec<K>,
    );
}
