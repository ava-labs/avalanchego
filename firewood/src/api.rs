use std::io::Write;

#[cfg(feature = "eth")]
use crate::account::Account;
#[cfg(feature = "eth")]
use primitive_types::U256;

use crate::db::{DBError, DBRevConfig};
use crate::merkle::Hash;
#[cfg(feature = "proof")]
use crate::{merkle::MerkleError, proof::Proof};

use async_trait::async_trait;

pub type Nonce = u64;

#[async_trait]
pub trait DB<B: WriteBatch, R: Revision> {
    async fn new_writebatch(&self) -> B;
    async fn get_revision(&self, nback: usize, cfg: Option<DBRevConfig>) -> Option<R>;
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
    ) -> Result<Self, DBError>;
    /// Remove an item from the generic key-value storage. `val` will be set to the value that is
    /// removed from the storage if it exists.
    async fn kv_remove<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
    ) -> Result<(Self, Option<Vec<u8>>), DBError>;

    /// Set balance of the account
    #[cfg(feature = "eth")]
    async fn set_balance<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        balance: U256,
    ) -> Result<Self, DBError>;
    /// Set code of the account
    #[cfg(feature = "eth")]
    async fn set_code<K: AsRef<[u8]> + Send + Sync, V: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        code: V,
    ) -> Result<Self, DBError>;
    /// Set nonce of the account.
    #[cfg(feature = "eth")]
    async fn set_nonce<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        nonce: u64,
    ) -> Result<Self, DBError>;
    /// Set the state value indexed by `sub_key` in the account indexed by `key`.
    async fn set_state<
        K: AsRef<[u8]> + Send + Sync,
        SK: AsRef<[u8]> + Send + Sync,
        V: AsRef<[u8]> + Send + Sync,
    >(
        self,
        key: K,
        sub_key: SK,
        val: V,
    ) -> Result<Self, DBError>;
    /// Create an account.
    #[cfg(feature = "eth")]
    async fn create_account<K: AsRef<[u8]> + Send + Sync>(self, key: K) -> Result<Self, DBError>;
    /// Delete an account.
    #[cfg(feature = "eth")]
    async fn delete_account<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        acc: &mut Option<Account>,
    ) -> Result<Self, DBError>;
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
    async fn kv_root_hash(&self) -> Result<Hash, DBError>;
    async fn kv_get<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DBError>;

    async fn kv_dump<W: Write + Send + Sync>(&self, writer: W) -> Result<(), DBError>;
    async fn root_hash(&self) -> Result<Hash, DBError>;
    async fn dump<W: Write + Send + Sync>(&self, writer: W) -> Result<(), DBError>;

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
    async fn get_balance<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<U256, DBError>;
    #[cfg(feature = "eth")]
    async fn get_code<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DBError>;
    #[cfg(feature = "eth")]
    async fn get_nonce<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Nonce, DBError>;
    #[cfg(feature = "eth")]
    async fn get_state<K: AsRef<[u8]> + Send + Sync>(
        &self,
        key: K,
        sub_key: K,
    ) -> Result<Vec<u8>, DBError>;
    #[cfg(feature = "eth")]
    async fn dump_account<W: Write + Send + Sync, K: AsRef<[u8]> + Send + Sync>(
        &self,
        key: K,
        writer: W,
    ) -> Result<(), DBError>;
}
