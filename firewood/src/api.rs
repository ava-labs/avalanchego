// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::io::Write;

#[cfg(feature = "eth")]
use crate::account::Account;
#[cfg(feature = "eth")]
use primitive_types::U256;

use crate::db::{DbError, DbRevConfig};
use crate::merkle::Hash;
#[cfg(feature = "proof")]
use crate::{merkle::MerkleError, proof::Proof};

use async_trait::async_trait;

pub type Nonce = u64;

#[async_trait]
pub trait Db<B: WriteBatch, R: Revision> {
    async fn new_writebatch(&self) -> B;
    async fn get_revision(&self, nback: usize, cfg: Option<DbRevConfig>) -> Option<R>;
}

#[async_trait]
pub trait WriteBatch
where
    Self: Sized,
{
    async fn kv_insert<K: AsRef<[u8]> + Send + Sync, V: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        val: V,
    ) -> Result<Self, DbError>;
    /// Remove an item from the generic key-value storage. `val` will be set to the value that is
    /// removed from the storage if it exists.
    async fn kv_remove<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
    ) -> Result<(Self, Option<Vec<u8>>), DbError>;

    /// Set balance of the account
    #[cfg(feature = "eth")]
    async fn set_balance<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        balance: U256,
    ) -> Result<Self, DbError>;
    /// Set code of the account
    #[cfg(feature = "eth")]
    async fn set_code<K: AsRef<[u8]> + Send + Sync, V: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        code: V,
    ) -> Result<Self, DbError>;
    /// Set nonce of the account.
    #[cfg(feature = "eth")]
    async fn set_nonce<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        nonce: u64,
    ) -> Result<Self, DbError>;
    /// Set the state value indexed by `sub_key` in the account indexed by `key`.
    #[cfg(feature = "eth")]
    async fn set_state<
        K: AsRef<[u8]> + Send + Sync,
        SK: AsRef<[u8]> + Send + Sync,
        V: AsRef<[u8]> + Send + Sync,
    >(
        self,
        key: K,
        sub_key: SK,
        val: V,
    ) -> Result<Self, DbError>;
    /// Create an account.
    #[cfg(feature = "eth")]
    async fn create_account<K: AsRef<[u8]> + Send + Sync>(self, key: K) -> Result<Self, DbError>;
    /// Delete an account.
    #[cfg(feature = "eth")]
    async fn delete_account<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        acc: &mut Option<Account>,
    ) -> Result<Self, DbError>;
    /// Do not rehash merkle roots upon commit. This will leave the recalculation of the dirty root
    /// hashes to future invocation of `root_hash`, `kv_root_hash` or batch commits.
    async fn no_root_hash(self) -> Self;

    /// Persist all changes to the DB. The atomicity of the [WriteBatch] guarantees all changes are
    /// either retained on disk or lost together during a crash.
    async fn commit(self);
}

#[async_trait]
pub trait Revision
where
    Self: Sized,
{
    async fn kv_root_hash(&self) -> Result<Hash, DbError>;
    async fn kv_get<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DbError>;

    async fn kv_dump<W: Write + Send + Sync>(&self, writer: W) -> Result<(), DbError>;
    async fn root_hash(&self) -> Result<Hash, DbError>;
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
    #[cfg(feature = "eth")]
    async fn get_balance<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<U256, DbError>;
    #[cfg(feature = "eth")]
    async fn get_code<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DbError>;
    #[cfg(feature = "eth")]
    async fn get_nonce<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Nonce, DbError>;
    #[cfg(feature = "eth")]
    async fn get_state<K: AsRef<[u8]> + Send + Sync>(
        &self,
        key: K,
        sub_key: K,
    ) -> Result<Vec<u8>, DbError>;
    #[cfg(feature = "eth")]
    async fn dump_account<W: Write + Send + Sync, K: AsRef<[u8]> + Send + Sync>(
        &self,
        key: K,
        writer: W,
    ) -> Result<(), DbError>;
}
