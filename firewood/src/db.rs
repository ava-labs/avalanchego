// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::merkle::{Merkle, MerkleError};
use crate::proof::{Proof, ProofNode};
use crate::range_proof::RangeProof;
use crate::stream::MerkleKeyValueStream;
use crate::v2::api::{self, KeyType, ValueType};
pub use crate::v2::api::{Batch, BatchOp};

use crate::manager::{RevisionManager, RevisionManagerConfig};
use async_trait::async_trait;
use metered::metered;
use std::error::Error;
use std::fmt;
use std::io::Write;
use std::path::Path;
use std::sync::{Arc, RwLock};
use storage::{Committed, FileBacked, HashedNodeReader, ImmutableProposal, NodeStore, TrieHash};
use typed_builder::TypedBuilder;

// TODO use or remove
const _VERSION_STR: &[u8; 16] = b"firewood v0.1\0\0\0";

#[derive(Debug)]
#[non_exhaustive]
pub enum DbError {
    InvalidParams,
    Merkle(MerkleError),
    CreateError,
    IO(std::io::Error),
    InvalidProposal,
}

impl fmt::Display for DbError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            DbError::InvalidParams => write!(f, "invalid parameters provided"),
            DbError::Merkle(e) => write!(f, "merkle error: {e:?}"),
            DbError::CreateError => write!(f, "database create error"),
            DbError::IO(e) => write!(f, "I/O error: {e:?}"),
            DbError::InvalidProposal => write!(f, "invalid proposal"),
        }
    }
}

impl From<std::io::Error> for DbError {
    fn from(e: std::io::Error) -> Self {
        DbError::IO(e)
    }
}

impl Error for DbError {}

type HistoricalRev = NodeStore<Committed, FileBacked>;

#[async_trait]
impl api::DbView for HistoricalRev {
    type Stream<'a> = MerkleKeyValueStream<'a, Self> where Self: 'a;

    async fn root_hash(&self) -> Result<Option<api::HashKey>, api::Error> {
        HashedNodeReader::root_hash(self).map_err(api::Error::IO)
    }

    async fn val<K: api::KeyType>(&self, key: K) -> Result<Option<Box<[u8]>>, api::Error> {
        let merkle = Merkle::from(self);
        Ok(merkle.get_value(key.as_ref())?)
    }

    async fn single_key_proof<K: api::KeyType>(
        &self,
        _key: K,
    ) -> Result<Option<Proof<ProofNode>>, api::Error> {
        todo!()
    }

    async fn range_proof<K: api::KeyType, V>(
        &self,
        _first_key: Option<K>,
        _last_key: Option<K>,
        _limit: Option<usize>,
    ) -> Result<Option<RangeProof<Box<[u8]>, Box<[u8]>, ProofNode>>, api::Error> {
        todo!()
    }

    fn iter_option<K: KeyType>(
        &self,
        _first_key: Option<K>,
    ) -> Result<Self::Stream<'_>, api::Error> {
        todo!()
    }
}

// impl<T: NodeReader> HistoricalRev<T> {
//     pub fn stream(&self) -> MerkleKeyValueStream<'_, T> {
//         todo!()
//     }

//     pub fn stream_from(&self, _start_key: &[u8]) -> MerkleKeyValueStream<'_, T> {
//         todo!()
//     }

//     /// Get root hash of the generic key-value storage.
//     pub fn kv_root_hash(&self) -> Result<TrieHash, DbError> {
//         todo!()
//     }

//     /// Get a value associated with a key.
//     pub fn get(&self, _key: &[u8]) -> Option<Vec<u8>> {
//         todo!()
//     }

//     /// Dump the Trie of the generic key-value storage.
//     pub fn dump(&self, _w: &mut dyn Write) -> Result<(), DbError> {
//         todo!()
//     }

//     pub fn prove(&self, _key: &[u8]) -> Result<Proof<ProofNode>, MerkleError> {
//         todo!()
//     }

//     /// Verifies a range proof is valid for a set of keys.
//     pub fn verify_range_proof<V: AsRef<[u8]>>(
//         &self,
//         _proof: &Proof<impl Hashable>,
//         _first_key: &[u8],
//         _last_key: &[u8],
//         _keys: Vec<&[u8]>,
//         _values: Vec<V>,
//     ) -> Result<bool, ProofError> {
//         todo!()
//     }
// }

/// TODO danlaine: implement
// pub struct Proposal<T> {
//     _proposal: T,
// }

// #[async_trait]
// impl<T: NodeWriter> api::Proposal for Proposal<T> {
//     type Proposal = Proposal<T>;

//     async fn commit(self: Arc<Self>) -> Result<(), api::Error> {
//         todo!()
//     }

//     async fn propose<K: api::KeyType, V: api::ValueType>(
//         self: Arc<Self>,
//         _data: api::Batch<K, V>,
//     ) -> Result<Arc<Self::Proposal>, api::Error> {
//         todo!()
//     }
// }

// #[async_trait]
// impl<T: NodeReader> api::DbView for Proposal<T> {
//     type Stream<'a> = MerkleKeyValueStream<'a, T> where T: 'a;

//     async fn root_hash(&self) -> Result<api::HashKey, api::Error> {
//         todo!()
//     }

//     async fn val<K>(&self, _key: K) -> Result<Option<Vec<u8>>, api::Error>
//     where
//         K: api::KeyType,
//     {
//         todo!()
//     }

// async fn single_key_proof<K>(&self, _key: K) -> Result<Option<Proof<ProofNode>>, api::Error>
// where
//     K: api::KeyType,
// {
//     todo!()
// }

// async fn range_proof<K, V>(
//     &self,
//     _first_key: Option<K>,
//     _last_key: Option<K>,
//     _limit: Option<usize>,
// ) -> Result<Option<api::RangeProof<Vec<u8>, Vec<u8>, ProofNode>>, api::Error>
// where
//     K: api::KeyType,
// {
//     todo!();
// }

//     fn iter_option<K: KeyType>(
//         &self,
//         _first_key: Option<K>,
//     ) -> Result<Self::Stream<'_>, api::Error> {
//         todo!()
//     }
// }

/// Database configuration.
#[derive(Clone, TypedBuilder, Debug)]
pub struct DbConfig {
    /// Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    /// existing contents will be lost.
    #[builder(default = false)]
    pub truncate: bool,
    #[builder(default = RevisionManagerConfig::builder().build())]
    pub manager: RevisionManagerConfig,
}

/// TODO danlaine: implement
#[derive(Debug)]
pub struct Db {
    metrics: Arc<DbMetrics>,
    // TODO: consider using https://docs.rs/lock_api/latest/lock_api/struct.RwLock.html#method.upgradable_read
    // TODO: This should probably use an async RwLock
    manager: RwLock<RevisionManager>,
}

#[async_trait]
impl api::Db for Db
where
    for<'p> Proposal<'p>: api::Proposal,
{
    type Historical = NodeStore<Committed, FileBacked>;

    type Proposal<'p> = Proposal<'p> where Self: 'p;

    async fn revision(&self, root_hash: TrieHash) -> Result<Arc<Self::Historical>, api::Error> {
        let nodestore = self
            .manager
            .read()
            .expect("poisoned lock")
            .revision(root_hash)?;
        Ok(nodestore)
    }

    async fn root_hash(&self) -> Result<Option<TrieHash>, api::Error> {
        Ok(self.manager.read().expect("poisoned lock").root_hash()?)
    }

    async fn propose<'p, K: KeyType, V: ValueType>(
        &'p mut self,
        batch: api::Batch<K, V>,
    ) -> Result<Arc<Self::Proposal<'p>>, api::Error>
    where
        Self: 'p,
    {
        let parent = self
            .manager
            .read()
            .expect("poisoned lock")
            .current_revision();
        let proposal = NodeStore::new(parent)?;
        let mut merkle = Merkle::from(proposal);
        for op in batch {
            match op {
                BatchOp::Put { key, value } => {
                    merkle.insert(key.as_ref(), value.as_ref().into())?;
                }
                BatchOp::Delete { key } => {
                    merkle.remove(key.as_ref())?;
                }
            }
        }
        let nodestore = merkle.into_inner();
        let immutable: Arc<NodeStore<ImmutableProposal, FileBacked>> = Arc::new(nodestore.into());
        self.manager
            .write()
            .expect("poisoned lock")
            .add_proposal(immutable.clone());

        Ok(Self::Proposal {
            nodestore: immutable,
            db: self,
        }
        .into())
    }
}

#[metered(registry = DbMetrics, visibility = pub)]
impl Db {
    pub async fn new<P: AsRef<Path>>(db_path: P, cfg: DbConfig) -> Result<Self, api::Error> {
        let metrics = DbMetrics::default().into();
        let manager = RevisionManager::new(
            db_path.as_ref().to_path_buf(),
            cfg.truncate,
            cfg.manager.clone(),
        )?;
        let db = Self {
            metrics,
            manager: manager.into(),
        };
        Ok(db)
    }

    /// Create a proposal.
    // pub fn new_proposal<K: KeyType, V: ValueType>(
    //     &self,
    //     _data: Batch<K, V>,
    // ) -> Result<Proposal<ProposedMutable>, DbError> {
    //     todo!()
    // }

    /// Dump the Trie of the latest revision.
    pub fn dump(&self, _w: &mut dyn Write) -> Result<(), DbError> {
        todo!()
    }

    pub fn metrics(&self) -> Arc<DbMetrics> {
        self.metrics.clone()
    }
}

#[derive(Debug)]
pub struct Proposal<'p> {
    nodestore: Arc<NodeStore<ImmutableProposal, FileBacked>>,
    db: &'p Db,
}

#[async_trait]
impl<'a> api::DbView for Proposal<'a> {
    type Stream<'b> = MerkleKeyValueStream<'b, NodeStore<ImmutableProposal, FileBacked>> where Self: 'b;

    async fn root_hash(&self) -> Result<Option<api::HashKey>, api::Error> {
        todo!()
    }

    async fn val<K: KeyType>(&self, _key: K) -> Result<Option<Box<[u8]>>, api::Error> {
        todo!()
    }

    async fn single_key_proof<K: KeyType>(
        &self,
        _key: K,
    ) -> Result<Option<Proof<ProofNode>>, api::Error> {
        todo!()
    }

    async fn range_proof<K: KeyType, V>(
        &self,
        _first_key: Option<K>,
        _last_key: Option<K>,
        _limit: Option<usize>,
    ) -> Result<Option<api::RangeProof<Box<[u8]>, Box<[u8]>, ProofNode>>, api::Error> {
        todo!()
    }

    fn iter_option<K: KeyType>(
        &self,
        _first_key: Option<K>,
    ) -> Result<Self::Stream<'_>, api::Error> {
        todo!()
    }
}

#[async_trait]
impl<'a> api::Proposal for Proposal<'a> {
    type Proposal = Proposal<'a>;

    async fn propose<K: KeyType, V: ValueType>(
        self: Arc<Self>,
        _data: api::Batch<K, V>,
    ) -> Result<Arc<Self::Proposal>, api::Error> {
        todo!()
    }

    // When committing a proposal, refuse to commit if there are any cloned proposals.
    async fn commit(self: Arc<Self>) -> Result<(), api::Error> {
        match Arc::into_inner(self) {
            Some(proposal) => {
                let mut manager = proposal.db.manager.write().expect("poisoned lock");
                Ok(manager.commit(proposal.nodestore.clone())?)
            }
            None => Err(api::Error::CannotCommitClonedProposal),
        }
    }
}
#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod test {
    use std::{
        ops::{Deref, DerefMut},
        path::PathBuf,
    };

    use crate::{
        db::Db,
        v2::api::{Db as _, Error, Proposal},
    };

    use super::DbConfig;

    #[tokio::test]
    async fn test_cloned_proposal_error() {
        let mut db = testdb().await;
        let proposal = db
            .propose::<Vec<u8>, Vec<u8>>(Default::default())
            .await
            .unwrap();
        let cloned = proposal.clone();

        // attempt to commit the clone; this should fail
        let result = cloned.commit().await;
        assert!(
            matches!(result, Err(Error::CannotCommitClonedProposal)),
            "{result:?}"
        );

        // the prior attempt consumed the Arc though, so cloned is no longer valid
        // that means the actual proposal can be committed
        let result = proposal.commit().await;
        assert!(matches!(result, Ok(())), "{result:?}");
    }

    // Testdb is a helper struct for testing the Db. Once it's dropped, the directory and file disappear
    struct TestDb {
        db: Db,
        _tmpdir: tempfile::TempDir,
    }
    impl Deref for TestDb {
        type Target = Db;
        fn deref(&self) -> &Self::Target {
            &self.db
        }
    }
    impl DerefMut for TestDb {
        fn deref_mut(&mut self) -> &mut Self::Target {
            &mut self.db
        }
    }

    async fn testdb() -> TestDb {
        let tmpdir = tempfile::tempdir().unwrap();
        let dbpath: PathBuf = [tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
            .iter()
            .collect();
        let dbconfig = DbConfig::builder().truncate(true).build();
        let db = Db::new(dbpath, dbconfig).await.unwrap();
        TestDb {
            db,
            _tmpdir: tmpdir,
        }
    }
}
