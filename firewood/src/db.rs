// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::missing_errors_doc,
    reason = "Found 12 occurrences after enabling the lint."
)]

use crate::merkle::Merkle;
use crate::proof::{Proof, ProofNode};
use crate::range_proof::RangeProof;
use crate::stream::MerkleKeyValueStream;
use crate::v2::api::{self, KeyType, ValueType};
pub use crate::v2::api::{Batch, BatchOp};

use crate::manager::{RevisionManager, RevisionManagerConfig};
use async_trait::async_trait;
use firewood_storage::{
    Committed, FileBacked, FileIoError, HashedNodeReader, ImmutableProposal, NodeStore, TrieHash,
};
use metrics::{counter, describe_counter};
use std::io::Write;
use std::path::Path;
use std::sync::Arc;
use std::sync::atomic::AtomicBool;
use thiserror::Error;
use typed_builder::TypedBuilder;

#[derive(Error, Debug)]
/// Represents the different types of errors that can occur in the database.
pub enum DbError {
    /// I/O error
    #[error("I/O error: {0:?}")]
    FileIo(#[from] FileIoError),
}

type HistoricalRev = NodeStore<Committed, FileBacked>;

/// Metrics for the database.
/// TODO: Add more metrics
pub struct DbMetrics {
    proposals: metrics::Counter,
}

impl std::fmt::Debug for DbMetrics {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("DbMetrics").finish()
    }
}

/// A synchronous view of the database.
pub trait DbViewSync {
    /// find a value synchronously
    fn val_sync<K: KeyType>(&self, key: K) -> Result<Option<Box<[u8]>>, DbError>;
}

/// A synchronous view of the database with raw byte keys (object-safe version).
pub trait DbViewSyncBytes: std::fmt::Debug {
    /// find a value synchronously using raw bytes
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Box<[u8]>>, DbError>;
}

// Provide blanket implementation for DbViewSync using DbViewSyncBytes
impl<T: DbViewSyncBytes> DbViewSync for T {
    fn val_sync<K: KeyType>(&self, key: K) -> Result<Option<Box<[u8]>>, DbError> {
        self.val_sync_bytes(key.as_ref())
    }
}

impl DbViewSyncBytes for Arc<HistoricalRev> {
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Box<[u8]>>, DbError> {
        let merkle = Merkle::from(self);
        let value = merkle.get_value(key)?;
        Ok(value)
    }
}

impl DbViewSyncBytes for Proposal<'_> {
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Box<[u8]>>, DbError> {
        let merkle = Merkle::from(self.nodestore.clone());
        let value = merkle.get_value(key)?;
        Ok(value)
    }
}

impl DbViewSyncBytes for Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>> {
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Box<[u8]>>, DbError> {
        let merkle = Merkle::from(self.clone());
        let value = merkle.get_value(key)?;
        Ok(value)
    }
}

#[async_trait]
impl api::DbView for HistoricalRev {
    type Stream<'a>
        = MerkleKeyValueStream<'a, Self>
    where
        Self: 'a;

    async fn root_hash(&self) -> Result<Option<api::HashKey>, api::Error> {
        Ok(HashedNodeReader::root_hash(self))
    }

    async fn val<K: api::KeyType>(&self, key: K) -> Result<Option<Box<[u8]>>, api::Error> {
        let merkle = Merkle::from(self);
        Ok(merkle.get_value(key.as_ref())?)
    }

    async fn single_key_proof<K: api::KeyType>(
        &self,
        key: K,
    ) -> Result<Proof<ProofNode>, api::Error> {
        let merkle = Merkle::from(self);
        merkle.prove(key.as_ref()).map_err(api::Error::from)
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

/// Database configuration.
#[derive(Clone, TypedBuilder, Debug)]
pub struct DbConfig {
    /// Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    /// existing contents will be lost.
    #[builder(default = false)]
    pub truncate: bool,
    /// Revision manager configuration.
    #[builder(default = RevisionManagerConfig::builder().build())]
    pub manager: RevisionManagerConfig,
}

#[derive(Debug)]
/// A database instance.
pub struct Db {
    metrics: Arc<DbMetrics>,
    manager: RevisionManager,
}

#[async_trait]
impl api::Db for Db
where
    for<'p> Proposal<'p>: api::Proposal,
{
    type Historical = NodeStore<Committed, FileBacked>;

    type Proposal<'p>
        = Proposal<'p>
    where
        Self: 'p;

    async fn revision(&self, root_hash: TrieHash) -> Result<Arc<Self::Historical>, api::Error> {
        let nodestore = self.manager.revision(root_hash)?;
        Ok(nodestore)
    }

    async fn root_hash(&self) -> Result<Option<TrieHash>, api::Error> {
        self.root_hash_sync()
    }

    async fn all_hashes(&self) -> Result<Vec<TrieHash>, api::Error> {
        Ok(self.manager.all_hashes())
    }

    #[fastrace::trace(short_name = true)]
    async fn propose<'p, K: KeyType, V: ValueType>(
        &'p self,
        batch: api::Batch<K, V>,
    ) -> Result<Arc<Self::Proposal<'p>>, api::Error>
    where
        Self: 'p,
    {
        let parent = self.manager.current_revision();
        let proposal = NodeStore::new(&parent)?;
        let mut merkle = Merkle::from(proposal);
        let span = fastrace::Span::enter_with_local_parent("merkleops");
        for op in batch {
            match op {
                BatchOp::Put { key, value } => {
                    merkle.insert(key.as_ref(), value.as_ref().into())?;
                }
                BatchOp::Delete { key } => {
                    merkle.remove(key.as_ref())?;
                }
                BatchOp::DeleteRange { prefix } => {
                    merkle.remove_prefix(prefix.as_ref())?;
                }
            }
        }

        drop(span);
        let span = fastrace::Span::enter_with_local_parent("freeze");

        let nodestore = merkle.into_inner();
        let immutable: Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>> =
            Arc::new(nodestore.try_into()?);

        drop(span);
        self.manager.add_proposal(immutable.clone());

        self.metrics.proposals.increment(1);

        Ok(Self::Proposal {
            nodestore: immutable,
            db: self,
            committed: AtomicBool::new(false),
        }
        .into())
    }
}

impl Db {
    /// Create a new database instance.
    pub async fn new<P: AsRef<Path>>(db_path: P, cfg: DbConfig) -> Result<Self, api::Error> {
        let metrics = Arc::new(DbMetrics {
            proposals: counter!("firewood.proposals"),
        });
        describe_counter!("firewood.proposals", "Number of proposals created");
        let manager = RevisionManager::new(
            db_path.as_ref().to_path_buf(),
            cfg.truncate,
            cfg.manager.clone(),
        )?;
        let db = Self { metrics, manager };
        Ok(db)
    }

    /// Create a new database instance with synchronous I/O.
    pub fn new_sync<P: AsRef<Path>>(db_path: P, cfg: DbConfig) -> Result<Self, api::Error> {
        let metrics = Arc::new(DbMetrics {
            proposals: counter!("firewood.proposals"),
        });
        describe_counter!("firewood.proposals", "Number of proposals created");
        let manager = RevisionManager::new(
            db_path.as_ref().to_path_buf(),
            cfg.truncate,
            cfg.manager.clone(),
        )?;
        let db = Self { metrics, manager };
        Ok(db)
    }

    /// Synchronously get the root hash of the latest revision.
    pub fn root_hash_sync(&self) -> Result<Option<TrieHash>, api::Error> {
        let hash = self.manager.root_hash()?;
        #[cfg(not(feature = "ethhash"))]
        return Ok(hash);
        #[cfg(feature = "ethhash")]
        return Ok(Some(hash.unwrap_or_else(firewood_storage::empty_trie_hash)));
    }

    /// Synchronously get a revision from a root hash
    pub fn revision_sync(&self, root_hash: TrieHash) -> Result<Arc<HistoricalRev>, api::Error> {
        let nodestore = self.manager.revision(root_hash)?;
        Ok(nodestore)
    }

    /// Synchronously get a view, either committed or proposed
    pub fn view_sync(&self, root_hash: TrieHash) -> Result<Box<dyn DbViewSyncBytes>, api::Error> {
        let nodestore = self.manager.view(root_hash)?;
        Ok(nodestore)
    }

    /// propose a new batch synchronously
    pub fn propose_sync<K: KeyType, V: ValueType>(
        &'_ self,
        batch: Batch<K, V>,
    ) -> Result<Arc<Proposal<'_>>, api::Error> {
        let parent = self.manager.current_revision();
        let proposal = NodeStore::new(&parent)?;
        let mut merkle = Merkle::from(proposal);
        for op in batch {
            match op {
                BatchOp::Put { key, value } => {
                    merkle.insert(key.as_ref(), value.as_ref().into())?;
                }
                BatchOp::Delete { key } => {
                    merkle.remove(key.as_ref())?;
                }
                BatchOp::DeleteRange { prefix } => {
                    merkle.remove_prefix(prefix.as_ref())?;
                }
            }
        }
        let nodestore = merkle.into_inner();
        let immutable: Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>> =
            Arc::new(nodestore.try_into()?);
        self.manager.add_proposal(immutable.clone());

        self.metrics.proposals.increment(1);

        Ok(Arc::new(Proposal {
            nodestore: immutable,
            db: self,
            committed: AtomicBool::new(false),
        }))
    }

    /// Dump the Trie of the latest revision.
    pub async fn dump(&self, w: &mut dyn Write) -> Result<(), std::io::Error> {
        self.dump_sync(w)
    }

    /// Dump the Trie of the latest revision, synchronously.
    pub fn dump_sync(&self, w: &mut dyn Write) -> Result<(), std::io::Error> {
        let latest_rev_nodestore = self.manager.current_revision();
        let merkle = Merkle::from(latest_rev_nodestore);
        // TODO: This should be a stream
        let output = merkle.dump()?;
        write!(w, "{output}")
    }

    /// Get a copy of the database metrics
    pub fn metrics(&self) -> Arc<DbMetrics> {
        self.metrics.clone()
    }
}

#[derive(Debug)]
/// A user-visible database proposal
pub struct Proposal<'p> {
    nodestore: Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>>,
    db: &'p Db,
    committed: AtomicBool,
}

impl Proposal<'_> {
    /// Get the root hash of the proposal synchronously
    pub fn start_commit(&self) -> Result<(), api::Error> {
        if self
            .committed
            .swap(true, std::sync::atomic::Ordering::Relaxed)
        {
            return Err(api::Error::AlreadyCommitted);
        }
        Ok(())
    }

    /// Get the root hash of the proposal synchronously
    pub fn root_hash_sync(&self) -> Result<Option<api::HashKey>, api::Error> {
        #[cfg(not(feature = "ethhash"))]
        return Ok(self.nodestore.root_hash());
        #[cfg(feature = "ethhash")]
        return Ok(Some(
            self.nodestore
                .root_hash()
                .unwrap_or_else(firewood_storage::empty_trie_hash),
        ));
    }
}

#[async_trait]
impl api::DbView for Proposal<'_> {
    type Stream<'b>
        = MerkleKeyValueStream<'b, NodeStore<Arc<ImmutableProposal>, FileBacked>>
    where
        Self: 'b;

    async fn root_hash(&self) -> Result<Option<api::HashKey>, api::Error> {
        Ok(self.nodestore.root_hash())
    }

    async fn val<K: KeyType>(&self, key: K) -> Result<Option<Box<[u8]>>, api::Error> {
        let merkle = Merkle::from(self.nodestore.clone());
        merkle.get_value(key.as_ref()).map_err(api::Error::from)
    }

    async fn single_key_proof<K: KeyType>(&self, key: K) -> Result<Proof<ProofNode>, api::Error> {
        let merkle = Merkle::from(self.nodestore.clone());
        merkle.prove(key.as_ref()).map_err(api::Error::from)
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

    #[fastrace::trace(short_name = true)]
    async fn propose<K: KeyType, V: ValueType>(
        self: Arc<Self>,
        batch: api::Batch<K, V>,
    ) -> Result<Arc<Self::Proposal>, api::Error> {
        Ok(self.create_proposal(batch)?.into())
    }

    async fn commit(self: Arc<Self>) -> Result<(), api::Error> {
        self.start_commit()?;
        Ok(self.db.manager.commit(self.nodestore.clone())?)
    }
}

impl Proposal<'_> {
    /// Commit a proposal synchronously
    pub fn commit_sync(self: Arc<Self>) -> Result<(), api::Error> {
        self.start_commit()?;
        Ok(self.db.manager.commit(self.nodestore.clone())?)
    }

    /// Create a new proposal from the current one synchronously
    pub fn propose_sync<K: KeyType, V: ValueType>(
        &self,
        batch: api::Batch<K, V>,
    ) -> Result<Arc<Self>, api::Error> {
        Ok(self.create_proposal(batch)?.into())
    }

    #[crate::metrics("firewood.proposal.create", "database proposal creation")]
    fn create_proposal<K: KeyType, V: ValueType>(
        &self,
        batch: api::Batch<K, V>,
    ) -> Result<Self, api::Error> {
        let parent = self.nodestore.clone();
        let proposal = NodeStore::new(&parent)?;
        let mut merkle = Merkle::from(proposal);
        for op in batch {
            match op {
                BatchOp::Put { key, value } => {
                    merkle.insert(key.as_ref(), value.as_ref().into())?;
                }
                BatchOp::Delete { key } => {
                    merkle.remove(key.as_ref())?;
                }
                BatchOp::DeleteRange { prefix } => {
                    merkle.remove_prefix(prefix.as_ref())?;
                }
            }
        }
        let nodestore = merkle.into_inner();
        let immutable: Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>> =
            Arc::new(nodestore.try_into()?);
        self.db.manager.add_proposal(immutable.clone());

        Ok(Self {
            nodestore: immutable,
            db: self.db,
            committed: AtomicBool::new(false),
        })
    }
}

#[cfg(test)]
mod test {
    #![expect(clippy::unwrap_used)]
    #![expect(
        clippy::default_trait_access,
        reason = "Found 1 occurrences after enabling the lint."
    )]

    use std::ops::{Deref, DerefMut};
    use std::path::PathBuf;

    use crate::db::Db;
    use crate::v2::api::{Db as _, DbView as _, Error, Proposal as _};

    use super::{BatchOp, DbConfig};

    #[tokio::test]
    async fn test_cloned_proposal_error() {
        let db = testdb().await;
        let proposal = db
            .propose::<Vec<u8>, Vec<u8>>(Default::default())
            .await
            .unwrap();
        let cloned = proposal.clone();

        // attempt to commit the clone; this should fail
        let result = cloned.commit().await;
        assert!(result.is_ok());

        let result = proposal.commit().await;
        assert!(matches!(result, Err(Error::AlreadyCommitted)), "{result:?}");
    }

    #[tokio::test]
    async fn test_proposal_reads() {
        let db = testdb().await;
        let batch = vec![BatchOp::Put {
            key: b"k",
            value: b"v",
        }];
        let proposal = db.propose(batch).await.unwrap();
        assert_eq!(&*proposal.val(b"k").await.unwrap().unwrap(), b"v");

        assert_eq!(proposal.val(b"notfound").await.unwrap(), None);
        proposal.commit().await.unwrap();

        let batch = vec![BatchOp::Put {
            key: b"k",
            value: b"v2",
        }];
        let proposal = db.propose(batch).await.unwrap();
        assert_eq!(&*proposal.val(b"k").await.unwrap().unwrap(), b"v2");

        let committed = db.root_hash().await.unwrap().unwrap();
        let historical = db.revision(committed).await.unwrap();
        assert_eq!(&*historical.val(b"k").await.unwrap().unwrap(), b"v");
    }

    #[tokio::test]
    async fn reopen_test() {
        let db = testdb().await;
        let initial_root = db.root_hash().await.unwrap();
        let batch = vec![
            BatchOp::Put {
                key: b"a",
                value: b"1",
            },
            BatchOp::Put {
                key: b"b",
                value: b"2",
            },
        ];
        let proposal = db.propose(batch).await.unwrap();
        proposal.commit().await.unwrap();
        println!("{:?}", db.root_hash().await.unwrap().unwrap());

        let db = db.reopen().await;
        println!("{:?}", db.root_hash().await.unwrap().unwrap());
        let committed = db.root_hash().await.unwrap().unwrap();
        let historical = db.revision(committed).await.unwrap();
        assert_eq!(&*historical.val(b"a").await.unwrap().unwrap(), b"1");

        let db = db.replace().await;
        println!("{:?}", db.root_hash().await.unwrap());
        assert!(db.root_hash().await.unwrap() == initial_root);
    }

    #[tokio::test]
    // test that dropping a proposal removes it from the list of known proposals
    //    /-> P1 - will get committed
    // R1 --> P2 - will get dropped
    //    \-> P3 - will get orphaned, but it's still known
    async fn test_proposal_scope_historic() {
        let db = testdb().await;
        let batch1 = vec![BatchOp::Put {
            key: b"k1",
            value: b"v1",
        }];
        let proposal1 = db.propose(batch1).await.unwrap();
        assert_eq!(&*proposal1.val(b"k1").await.unwrap().unwrap(), b"v1");

        let batch2 = vec![BatchOp::Put {
            key: b"k2",
            value: b"v2",
        }];
        let proposal2 = db.propose(batch2).await.unwrap();
        assert_eq!(&*proposal2.val(b"k2").await.unwrap().unwrap(), b"v2");

        let batch3 = vec![BatchOp::Put {
            key: b"k3",
            value: b"v3",
        }];
        let proposal3 = db.propose(batch3).await.unwrap();
        assert_eq!(&*proposal3.val(b"k3").await.unwrap().unwrap(), b"v3");

        // the proposal is dropped here, but the underlying
        // nodestore is still accessible because it's referenced by the revision manager
        // The third proposal remains referenced
        let p2hash = proposal2.root_hash().await.unwrap().unwrap();
        assert!(db.all_hashes().await.unwrap().contains(&p2hash));
        drop(proposal2);

        // commit the first proposal
        proposal1.commit().await.unwrap();
        // Ensure we committed the first proposal's data
        let committed = db.root_hash().await.unwrap().unwrap();
        let historical = db.revision(committed).await.unwrap();
        assert_eq!(&*historical.val(b"k1").await.unwrap().unwrap(), b"v1");

        // the second proposal shouldn't be available to commit anymore
        assert!(!db.all_hashes().await.unwrap().contains(&p2hash));

        // the third proposal should still be contained within the all_hashes list
        // would be deleted if another proposal was committed and proposal3 was dropped here
        let hash3 = proposal3.root_hash().await.unwrap().unwrap();
        assert!(db.manager.all_hashes().contains(&hash3));
    }

    #[tokio::test]
    // test that dropping a proposal removes it from the list of known proposals
    // R1 - base revision
    //  \-> P1 - will get committed
    //   \-> P2 - will get dropped
    //    \-> P3 - will get orphaned, but it's still known
    async fn test_proposal_scope_orphan() {
        let db = testdb().await;
        let batch1 = vec![BatchOp::Put {
            key: b"k1",
            value: b"v1",
        }];
        let proposal1 = db.propose(batch1).await.unwrap();
        assert_eq!(&*proposal1.val(b"k1").await.unwrap().unwrap(), b"v1");

        let batch2 = vec![BatchOp::Put {
            key: b"k2",
            value: b"v2",
        }];
        let proposal2 = proposal1.clone().propose(batch2).await.unwrap();
        assert_eq!(&*proposal2.val(b"k2").await.unwrap().unwrap(), b"v2");

        let batch3 = vec![BatchOp::Put {
            key: b"k3",
            value: b"v3",
        }];
        let proposal3 = proposal2.clone().propose(batch3).await.unwrap();
        assert_eq!(&*proposal3.val(b"k3").await.unwrap().unwrap(), b"v3");

        // the proposal is dropped here, but the underlying
        // nodestore is still accessible because it's referenced by the revision manager
        // The third proposal remains referenced
        let p2hash = proposal2.root_hash().await.unwrap().unwrap();
        assert!(db.all_hashes().await.unwrap().contains(&p2hash));
        drop(proposal2);

        // commit the first proposal
        proposal1.commit().await.unwrap();
        // Ensure we committed the first proposal's data
        let committed = db.root_hash().await.unwrap().unwrap();
        let historical = db.revision(committed).await.unwrap();
        assert_eq!(&*historical.val(b"k1").await.unwrap().unwrap(), b"v1");

        // the second proposal shouldn't be available to commit anymore
        assert!(!db.all_hashes().await.unwrap().contains(&p2hash));

        // the third proposal should still be contained within the all_hashes list
        let hash3 = proposal3.root_hash().await.unwrap().unwrap();
        assert!(db.manager.all_hashes().contains(&hash3));

        // moreover, the data from the second and third proposals should still be available
        // through proposal3
        assert_eq!(&*proposal3.val(b"k2").await.unwrap().unwrap(), b"v2");
        assert_eq!(&*proposal3.val(b"k3").await.unwrap().unwrap(), b"v3");
    }

    #[tokio::test]
    async fn test_view_sync() {
        let db = testdb().await;

        // Create and commit some data to get a historical revision
        let batch = vec![BatchOp::Put {
            key: b"historical_key",
            value: b"historical_value",
        }];
        let proposal = db.propose(batch).await.unwrap();
        let historical_hash = proposal.root_hash().await.unwrap().unwrap();
        proposal.commit().await.unwrap();

        // Create a new proposal (uncommitted)
        let batch = vec![BatchOp::Put {
            key: b"proposal_key",
            value: b"proposal_value",
        }];
        let proposal = db.propose(batch).await.unwrap();
        let proposal_hash = proposal.root_hash().await.unwrap().unwrap();

        // Test that view_sync can find the historical revision
        let historical_view = db.view_sync(historical_hash).unwrap();
        let value = historical_view
            .val_sync_bytes(b"historical_key")
            .unwrap()
            .unwrap();
        assert_eq!(&*value, b"historical_value");

        // Test that view_sync can find the proposal
        let proposal_view = db.view_sync(proposal_hash).unwrap();
        let value = proposal_view
            .val_sync_bytes(b"proposal_key")
            .unwrap()
            .unwrap();
        assert_eq!(&*value, b"proposal_value");
    }

    // Testdb is a helper struct for testing the Db. Once it's dropped, the directory and file disappear
    struct TestDb {
        db: Db,
        tmpdir: tempfile::TempDir,
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
        let dbconfig = DbConfig::builder().build();
        let db = Db::new(dbpath, dbconfig).await.unwrap();
        TestDb { db, tmpdir }
    }

    impl TestDb {
        fn path(&self) -> PathBuf {
            [self.tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
                .iter()
                .collect()
        }
        async fn reopen(self) -> Self {
            let path = self.path();
            drop(self.db);
            let dbconfig = DbConfig::builder().truncate(false).build();

            let db = Db::new(path, dbconfig).await.unwrap();
            TestDb {
                db,
                tmpdir: self.tmpdir,
            }
        }
        async fn replace(self) -> Self {
            let path = self.path();
            drop(self.db);
            let dbconfig = DbConfig::builder().truncate(true).build();

            let db = Db::new(path, dbconfig).await.unwrap();
            TestDb {
                db,
                tmpdir: self.tmpdir,
            }
        }
    }
}
