// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::missing_errors_doc,
    reason = "Found 12 occurrences after enabling the lint."
)]

use crate::merkle::{Merkle, Value};
use crate::stream::MerkleKeyValueStream;
pub use crate::v2::api::BatchOp;
use crate::v2::api::{
    self, FrozenProof, FrozenRangeProof, HashKey, KeyType, KeyValuePair, KeyValuePairIter,
    OptionalHashKeyExt,
};

use crate::manager::{ConfigManager, RevisionManager, RevisionManagerConfig};
use async_trait::async_trait;
use firewood_storage::{
    CheckOpt, CheckerReport, Committed, FileBacked, FileIoError, HashedNodeReader,
    ImmutableProposal, NodeStore,
};
use metrics::{counter, describe_counter};
use std::io::Write;
use std::num::NonZeroUsize;
use std::path::Path;
use std::sync::Arc;
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
    fn val_sync<K: KeyType>(&self, key: K) -> Result<Option<Value>, DbError>;
}

/// A synchronous view of the database with raw byte keys (object-safe version).
pub trait DbViewSyncBytes: std::fmt::Debug + Send + Sync {
    /// find a value synchronously using raw bytes
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Value>, DbError>;
}

// Provide blanket implementation for DbViewSync using DbViewSyncBytes
impl<T: DbViewSyncBytes> DbViewSync for T {
    fn val_sync<K: KeyType>(&self, key: K) -> Result<Option<Value>, DbError> {
        self.val_sync_bytes(key.as_ref())
    }
}

impl DbViewSyncBytes for Arc<HistoricalRev> {
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Value>, DbError> {
        let merkle = Merkle::from(self);
        let value = merkle.get_value(key)?;
        Ok(value)
    }
}

impl DbViewSyncBytes for Proposal<'_> {
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Value>, DbError> {
        let merkle = Merkle::from(self.nodestore.clone());
        let value = merkle.get_value(key)?;
        Ok(value)
    }
}

impl DbViewSyncBytes for Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>> {
    fn val_sync_bytes(&self, key: &[u8]) -> Result<Option<Value>, DbError> {
        let merkle = Merkle::from(self.clone());
        let value = merkle.get_value(key)?;
        Ok(value)
    }
}

#[async_trait]
impl api::DbView for HistoricalRev {
    type Stream<'view>
        = MerkleKeyValueStream<'view, Self>
    where
        Self: 'view;

    async fn root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        Ok(HashedNodeReader::root_hash(self))
    }

    async fn val<K: api::KeyType>(&self, key: K) -> Result<Option<Value>, api::Error> {
        let merkle = Merkle::from(self);
        Ok(merkle.get_value(key.as_ref())?)
    }

    async fn single_key_proof<K: api::KeyType>(&self, key: K) -> Result<FrozenProof, api::Error> {
        let merkle = Merkle::from(self);
        merkle.prove(key.as_ref()).map_err(api::Error::from)
    }

    async fn range_proof<K: api::KeyType>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, api::Error> {
        Merkle::from(self)
            .range_proof(
                first_key.as_ref().map(AsRef::as_ref),
                last_key.as_ref().map(AsRef::as_ref),
                limit,
            )
            .await
    }

    fn iter_option<K: KeyType>(
        &self,
        first_key: Option<K>,
    ) -> Result<Self::Stream<'_>, api::Error> {
        match first_key {
            Some(key) => Ok(MerkleKeyValueStream::from_key(self, key)),
            None => Ok(MerkleKeyValueStream::from(self)),
        }
    }
}

/// Database configuration.
#[derive(Clone, TypedBuilder, Debug)]
pub struct DbConfig {
    /// Whether to create the DB if it doesn't exist.
    #[builder(default = true)]
    pub create_if_missing: bool,
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
impl api::Db for Db {
    type Historical = NodeStore<Committed, FileBacked>;

    type Proposal<'db>
        = Proposal<'db>
    where
        Self: 'db;

    async fn revision(&self, root_hash: HashKey) -> Result<Arc<Self::Historical>, api::Error> {
        let nodestore = self.manager.revision(root_hash)?;
        Ok(nodestore)
    }

    async fn root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        self.root_hash_sync()
    }

    async fn all_hashes(&self) -> Result<Vec<HashKey>, api::Error> {
        Ok(self.manager.all_hashes())
    }

    #[fastrace::trace(short_name = true)]
    async fn propose<'db>(
        &'db self,
        batch: (impl IntoIterator<IntoIter: KeyValuePairIter> + Send),
    ) -> Result<Self::Proposal<'db>, api::Error> {
        let parent = self.manager.current_revision();
        let proposal = NodeStore::new(&parent)?;
        let mut merkle = Merkle::from(proposal);
        let span = fastrace::Span::enter_with_local_parent("merkleops");
        for op in batch.into_iter().map_into_batch() {
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
        })
    }
}

impl Db {
    /// Create a new database instance.
    pub fn new<P: AsRef<Path>>(db_path: P, cfg: DbConfig) -> Result<Self, api::Error> {
        let metrics = Arc::new(DbMetrics {
            proposals: counter!("firewood.proposals"),
        });
        describe_counter!("firewood.proposals", "Number of proposals created");
        let config_manager = ConfigManager::builder()
            .create(cfg.create_if_missing)
            .truncate(cfg.truncate)
            .manager(cfg.manager)
            .build();
        let manager = RevisionManager::new(db_path.as_ref().to_path_buf(), config_manager)?;
        let db = Self { metrics, manager };
        Ok(db)
    }

    /// Synchronously get the root hash of the latest revision.
    pub fn root_hash_sync(&self) -> Result<Option<HashKey>, api::Error> {
        Ok(self.manager.root_hash()?.or_default_root_hash())
    }

    /// Synchronously get a revision from a root hash
    pub fn revision_sync(&self, root_hash: HashKey) -> Result<Arc<HistoricalRev>, api::Error> {
        let nodestore = self.manager.revision(root_hash)?;
        Ok(nodestore)
    }

    /// Synchronously get a view, either committed or proposed
    pub fn view_sync(&self, root_hash: HashKey) -> Result<Box<dyn DbViewSyncBytes>, api::Error> {
        let nodestore = self.manager.view(root_hash)?;
        Ok(nodestore)
    }

    /// propose a new batch synchronously
    pub fn propose_sync(
        &self,
        batch: impl IntoIterator<IntoIter: KeyValuePairIter>,
    ) -> Result<Proposal<'_>, api::Error> {
        let parent = self.manager.current_revision();
        let proposal = NodeStore::new(&parent)?;
        let mut merkle = Merkle::from(proposal);
        for op in batch {
            match op.into_batch() {
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

        Ok(Proposal {
            nodestore: immutable,
            db: self,
        })
    }

    /// Dump the Trie of the latest revision.
    pub fn dump(&self, w: &mut dyn Write) -> Result<(), std::io::Error> {
        let latest_rev_nodestore = self.manager.current_revision();
        let merkle = Merkle::from(latest_rev_nodestore);
        merkle.dump(w).map_err(std::io::Error::other)
    }

    /// Get a copy of the database metrics
    pub fn metrics(&self) -> Arc<DbMetrics> {
        self.metrics.clone()
    }

    /// Check the database for consistency
    pub fn check(&self, opt: CheckOpt) -> CheckerReport {
        let latest_rev_nodestore = self.manager.current_revision();
        latest_rev_nodestore.check(opt)
    }
}

#[derive(Debug)]
/// A user-visible database proposal
pub struct Proposal<'db> {
    nodestore: Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>>,
    db: &'db Db,
}

impl Proposal<'_> {
    /// Synchronously get the root hash of the latest revision.
    pub fn root_hash_sync(&self) -> Result<Option<HashKey>, api::Error> {
        Ok(self.nodestore.root_hash().or_default_root_hash())
    }
}

#[async_trait]
impl api::DbView for Proposal<'_> {
    type Stream<'view>
        = MerkleKeyValueStream<'view, NodeStore<Arc<ImmutableProposal>, FileBacked>>
    where
        Self: 'view;

    async fn root_hash(&self) -> Result<Option<api::HashKey>, api::Error> {
        self.root_hash_sync()
    }

    async fn val<K: KeyType>(&self, key: K) -> Result<Option<Value>, api::Error> {
        let merkle = Merkle::from(self.nodestore.clone());
        merkle.get_value(key.as_ref()).map_err(api::Error::from)
    }

    async fn single_key_proof<K: KeyType>(&self, key: K) -> Result<FrozenProof, api::Error> {
        let merkle = Merkle::from(self.nodestore.clone());
        merkle.prove(key.as_ref()).map_err(api::Error::from)
    }

    async fn range_proof<K: KeyType>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, api::Error> {
        Merkle::from(&self.nodestore)
            .range_proof(
                first_key.as_ref().map(AsRef::as_ref),
                last_key.as_ref().map(AsRef::as_ref),
                limit,
            )
            .await
    }

    fn iter_option<K: KeyType>(
        &self,
        _first_key: Option<K>,
    ) -> Result<Self::Stream<'_>, api::Error> {
        todo!()
    }
}

impl<'db> api::Proposal for Proposal<'db> {
    type Proposal = Proposal<'db>;

    #[fastrace::trace(short_name = true)]
    fn propose(
        &self,
        batch: impl IntoIterator<IntoIter: KeyValuePairIter>,
    ) -> Result<Self::Proposal, api::Error> {
        self.create_proposal(batch)
    }

    fn commit(self) -> Result<(), api::Error> {
        Ok(self.db.manager.commit(self.nodestore.clone())?)
    }
}

impl Proposal<'_> {
    /// Commit a proposal synchronously
    pub fn commit_sync(self) -> Result<(), api::Error> {
        Ok(self.db.manager.commit(self.nodestore.clone())?)
    }

    /// Create a new proposal from the current one synchronously
    pub fn propose_sync(
        &self,
        batch: impl IntoIterator<IntoIter: KeyValuePairIter>,
    ) -> Result<Self, api::Error> {
        self.create_proposal(batch)
    }

    #[crate::metrics("firewood.proposal.create", "database proposal creation")]
    fn create_proposal(
        &self,
        batch: impl IntoIterator<IntoIter: KeyValuePairIter>,
    ) -> Result<Self, api::Error> {
        let parent = self.nodestore.clone();
        let proposal = NodeStore::new(&parent)?;
        let mut merkle = Merkle::from(proposal);
        for op in batch {
            match op.into_batch() {
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
        })
    }
}

#[cfg(test)]
mod test {
    #![expect(clippy::unwrap_used)]

    use core::iter::Take;
    use std::iter::Peekable;
    use std::num::NonZeroUsize;
    use std::ops::{Deref, DerefMut};
    use std::path::PathBuf;

    use firewood_storage::{CheckOpt, CheckerError};
    use tokio::sync::mpsc::{Receiver, Sender};

    use crate::db::{Db, Proposal};
    use crate::v2::api::{Db as _, DbView as _, KeyValuePairIter, Proposal as _};

    use super::{BatchOp, DbConfig};

    /// A chunk of an iterator, provided by [`IterExt::async_chunk_fold`] to the folding
    /// function.
    type Chunk<'chunk, 'base, T> = &'chunk mut Take<&'base mut Peekable<T>>;

    trait IterExt: Iterator {
        /// Asynchronously folds the iterator with chunks of a specified size. The last
        /// chunk may be smaller than the specified size.
        ///
        /// The folding function is an async closure that takes an accumulator and a
        /// chunk of the underlying iterator, and returns a new accumulator.
        ///
        /// # Panics
        ///
        /// If the folding function does not consume the entire chunk, it will panic.
        ///
        /// If the folding function panics, the iterator will be dropped (because this
        /// method consumes `self`).
        async fn async_chunk_fold<B, F>(self, chunk_size: NonZeroUsize, init: B, mut f: F) -> B
        where
            Self: Sized,
            F: for<'a, 'b> AsyncFnMut(B, Chunk<'a, 'b, Self>) -> B,
        {
            let chunk_size = chunk_size.get();
            let mut iter = self.peekable();
            let mut acc = init;
            while iter.peek().is_some() {
                let mut chunk = iter.by_ref().take(chunk_size);
                acc = f(acc, chunk.by_ref()).await;
                assert!(chunk.next().is_none(), "entire chunk was not consumed");
            }
            acc
        }
    }

    impl<T: Iterator> IterExt for T {}

    #[tokio::test]
    async fn test_proposal_reads() {
        let db = testdb();
        let batch = vec![BatchOp::Put {
            key: b"k",
            value: b"v",
        }];
        let proposal = db.propose(batch).await.unwrap();
        assert_eq!(&*proposal.val(b"k").await.unwrap().unwrap(), b"v");

        assert_eq!(proposal.val(b"notfound").await.unwrap(), None);
        proposal.commit().unwrap();

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
        let db = testdb();
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
        proposal.commit().unwrap();
        println!("{:?}", db.root_hash().await.unwrap().unwrap());

        let db = db.reopen();
        println!("{:?}", db.root_hash().await.unwrap().unwrap());
        let committed = db.root_hash().await.unwrap().unwrap();
        let historical = db.revision(committed).await.unwrap();
        assert_eq!(&*historical.val(b"a").await.unwrap().unwrap(), b"1");

        let db = db.replace();
        println!("{:?}", db.root_hash().await.unwrap());
        assert!(db.root_hash().await.unwrap() == initial_root);
    }

    #[tokio::test]
    // test that dropping a proposal removes it from the list of known proposals
    //    /-> P1 - will get committed
    // R1 --> P2 - will get dropped
    //    \-> P3 - will get orphaned, but it's still known
    async fn test_proposal_scope_historic() {
        let db = testdb();
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
        proposal1.commit().unwrap();
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
        let db = testdb();
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
        let proposal2 = proposal1.propose(batch2).unwrap();
        assert_eq!(&*proposal2.val(b"k2").await.unwrap().unwrap(), b"v2");

        let batch3 = vec![BatchOp::Put {
            key: b"k3",
            value: b"v3",
        }];
        let proposal3 = proposal2.propose(batch3).unwrap();
        assert_eq!(&*proposal3.val(b"k3").await.unwrap().unwrap(), b"v3");

        // the proposal is dropped here, but the underlying
        // nodestore is still accessible because it's referenced by the revision manager
        // The third proposal remains referenced
        let p2hash = proposal2.root_hash().await.unwrap().unwrap();
        assert!(db.all_hashes().await.unwrap().contains(&p2hash));
        drop(proposal2);

        // commit the first proposal
        proposal1.commit().unwrap();
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
        let db = testdb();

        // Create and commit some data to get a historical revision
        let batch = vec![BatchOp::Put {
            key: b"historical_key",
            value: b"historical_value",
        }];
        let proposal = db.propose(batch).await.unwrap();
        let historical_hash = proposal.root_hash().await.unwrap().unwrap();
        proposal.commit().unwrap();

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

    /// Test that proposing on a proposal works as expected
    ///
    /// Test creates two batches and proposes them, and verifies that the values are in the correct proposal.
    /// It then commits them one by one, and verifies the latest committed version is correct.
    #[tokio::test]
    async fn test_propose_on_proposal() {
        // number of keys and values to create for this test
        const N: usize = 20;

        let db = testdb();

        // create N keys and values like (key0, value0)..(keyN, valueN)
        let (keys, vals): (Vec<_>, Vec<_>) = (0..N)
            .map(|i| {
                (
                    format!("key{i}").into_bytes(),
                    Box::from(format!("value{i}").as_bytes()),
                )
            })
            .unzip();

        // create two batches, one with the first half of keys and values, and one with the last half keys and values
        let mut kviter = keys.iter().zip(vals.iter()).map_into_batch();

        // create two proposals, second one has a base of the first one
        let proposal1 = db.propose(kviter.by_ref().take(N / 2)).await.unwrap();
        let proposal2 = proposal1.propose(kviter).unwrap();

        // iterate over the keys and values again, checking that the values are in the correct proposal
        let mut kviter = keys.iter().zip(vals.iter());

        // first half of the keys should be in both proposals
        for (k, v) in kviter.by_ref().take(N / 2) {
            assert_eq!(&proposal1.val(k).await.unwrap().unwrap(), v);
            assert_eq!(&proposal2.val(k).await.unwrap().unwrap(), v);
        }

        // remaining keys should only be in the second proposal
        for (k, v) in kviter {
            // second half of keys are in the second proposal
            assert_eq!(&proposal2.val(k).await.unwrap().unwrap(), v);
            // but not in the first
            assert_eq!(proposal1.val(k).await.unwrap(), None);
        }

        proposal1.commit().unwrap();

        // all keys are still in the second proposal (first is no longer accessible)
        for (k, v) in keys.iter().zip(vals.iter()) {
            assert_eq!(&proposal2.val(k).await.unwrap().unwrap(), v);
        }

        // commit the second proposal
        proposal2.commit().unwrap();

        // all keys are in the database
        let committed = db.root_hash().await.unwrap().unwrap();
        let revision = db.revision(committed).await.unwrap();

        for (k, v) in keys.into_iter().zip(vals.into_iter()) {
            assert_eq!(revision.val(k).await.unwrap().unwrap(), v);
        }
    }

    #[tokio::test]
    async fn fuzz_checker() {
        let _ = env_logger::Builder::new().is_test(true).try_init();

        let rng = firewood_storage::SeededRng::from_env_or_random();

        let db = testdb();

        // takes about 0.3s on a mac to run 50 times
        for _ in 0..50 {
            // create a batch of 10 random key-value pairs
            let batch = (0..10).fold(vec![], |mut batch, _| {
                let key: [u8; 32] = rng.random();
                let value: [u8; 8] = rng.random();
                batch.push(BatchOp::Put {
                    key: key.to_vec(),
                    value,
                });
                if rng.random_range(0..5) == 0 {
                    let addon: [u8; 32] = rng.random();
                    let key = [key, addon].concat();
                    let value: [u8; 8] = rng.random();
                    batch.push(BatchOp::Put { key, value });
                }
                batch
            });
            let proposal = db.propose(batch).await.unwrap();
            proposal.commit().unwrap();

            // check the database for consistency, sometimes checking the hashes
            let hash_check = rng.random();
            let report = db.check(CheckOpt {
                hash_check,
                progress_bar: None,
            });
            if report
                .errors
                .iter()
                .filter(|e| !matches!(e, CheckerError::AreaLeaks(_)))
                .count()
                != 0
            {
                db.dump(&mut std::io::stdout()).unwrap();
                panic!("error: {:?}", report.errors);
            }
        }
    }

    #[tokio::test]
    async fn test_deep_propose() {
        const NUM_KEYS: NonZeroUsize = const { NonZeroUsize::new(2).unwrap() };
        const NUM_PROPOSALS: usize = 100;

        let db = testdb();

        let ops = (0..(NUM_KEYS.get() * NUM_PROPOSALS))
            .map(|i| (format!("key{i}"), format!("value{i}")))
            .collect::<Vec<_>>();

        let proposals = ops
            .iter()
            .async_chunk_fold(
                NUM_KEYS,
                Vec::<Proposal<'_>>::with_capacity(NUM_PROPOSALS),
                async |mut proposals, ops| {
                    let proposal = if let Some(parent) = proposals.last() {
                        parent.propose(ops).unwrap()
                    } else {
                        db.propose(ops).await.unwrap()
                    };

                    proposals.push(proposal);
                    proposals
                },
            )
            .await;

        let last_proposal_root_hash = proposals
            .last()
            .unwrap()
            .root_hash()
            .await
            .unwrap()
            .unwrap();

        // commit the proposals
        for proposal in proposals {
            proposal.commit().unwrap();
        }

        // get the last committed revision
        let last_root_hash = db.root_hash().await.unwrap().unwrap();
        let committed = db.revision(last_root_hash.clone()).await.unwrap();

        // the last root hash should be the same as the last proposal root hash
        assert_eq!(last_root_hash, last_proposal_root_hash);

        // check that all the keys and values are still present
        for (k, v) in &ops {
            let found = committed.val(k).await.unwrap();
            assert_eq!(
                found.as_deref(),
                Some(v.as_bytes()),
                "Value for key {k:?} should be {v:?} but was {found:?}",
            );
        }
    }

    /// Test that reading from a proposal during commit works as expected
    #[tokio::test(flavor = "multi_thread")]
    async fn test_read_during_commit() {
        use crate::db::Proposal;

        const CHANNEL_CAPACITY: usize = 8;

        let testdb = testdb();
        let db = &testdb.db;

        let (tx, mut rx): (Sender<Proposal<'_>>, Receiver<Proposal<'_>>) =
            tokio::sync::mpsc::channel(CHANNEL_CAPACITY);
        let (result_tx, mut result_rx) = tokio::sync::mpsc::channel(CHANNEL_CAPACITY);

        tokio_scoped::scope(|scope| {
            // Commit task
            scope.spawn(async move {
                while let Some(proposal) = rx.recv().await {
                    let result = proposal.commit();
                    // send result back to the main thread, both for synchronization and stopping the
                    // test on error
                    result_tx.send(result).await.unwrap();
                }
            });
            scope.spawn(async move {
                // Proposal creation
                for id in 0u32..5000 {
                    // insert a key of length 32 and a value of length 8,
                    // rotating between all zeroes through all 255
                    let batch = vec![BatchOp::Put {
                        key: [id as u8; 32],
                        value: [id as u8; 8],
                    }];
                    let proposal = db.propose(batch).await.unwrap();
                    let last_hash = proposal.root_hash().await.unwrap().unwrap();
                    let view = db.view_sync(last_hash).unwrap();

                    tx.send(proposal).await.unwrap();

                    let key = [id as u8; 32];
                    let value = view.val_sync_bytes(&key).unwrap().unwrap();
                    assert_eq!(&*value, &[id as u8; 8]);
                    result_rx.recv().await.unwrap().unwrap();
                }
                // close the channel, which will cause the commit task to exit
                drop(tx);
            });
        });
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

    fn testdb() -> TestDb {
        let tmpdir = tempfile::tempdir().unwrap();
        let dbpath: PathBuf = [tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
            .iter()
            .collect();
        let dbconfig = DbConfig::builder().build();
        let db = Db::new(dbpath, dbconfig).unwrap();
        TestDb { db, tmpdir }
    }

    impl TestDb {
        fn path(&self) -> PathBuf {
            [self.tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
                .iter()
                .collect()
        }
        fn reopen(self) -> Self {
            let path = self.path();
            drop(self.db);
            let dbconfig = DbConfig::builder().truncate(false).build();

            let db = Db::new(path, dbconfig).unwrap();
            TestDb {
                db,
                tmpdir: self.tmpdir,
            }
        }
        fn replace(self) -> Self {
            let path = self.path();
            drop(self.db);
            let dbconfig = DbConfig::builder().truncate(true).build();

            let db = Db::new(path, dbconfig).unwrap();
            TestDb {
                db,
                tmpdir: self.tmpdir,
            }
        }
    }
}
