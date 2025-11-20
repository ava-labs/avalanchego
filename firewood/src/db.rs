// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::missing_errors_doc,
    reason = "Found 12 occurrences after enabling the lint."
)]

#[cfg(test)]
mod tests;

use crate::iter::MerkleKeyValueIter;
use crate::merkle::{Merkle, Value};
use crate::root_store::{FjallStore, NoOpStore, RootStore};
pub use crate::v2::api::BatchOp;
use crate::v2::api::{
    self, ArcDynDbView, FrozenProof, FrozenRangeProof, HashKey, IntoBatchIter, KeyType,
    KeyValuePair, OptionalHashKeyExt,
};

use crate::manager::{ConfigManager, RevisionManager, RevisionManagerConfig, RevisionManagerError};
use firewood_storage::{
    CheckOpt, CheckerReport, Committed, FileBacked, FileIoError, HashedNodeReader,
    ImmutableProposal, NodeStore, Parentable, ReadableStorage, TrieReader,
};
use metrics::{counter, describe_counter};
use std::io::Write;
use std::num::NonZeroUsize;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use thiserror::Error;
use typed_builder::TypedBuilder;

use crate::merkle::parallel::ParallelMerkle;

#[derive(Error, Debug)]
#[non_exhaustive]
/// Represents the different types of errors that can occur in the database.
pub enum DbError {
    /// I/O error
    #[error("I/O error: {0:?}")]
    FileIo(#[from] FileIoError),
}

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

impl<P: Parentable, S: ReadableStorage> api::DbView for NodeStore<P, S>
where
    NodeStore<P, S>: TrieReader,
{
    type Iter<'view>
        = MerkleKeyValueIter<'view, Self>
    where
        Self: 'view;

    fn root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        Ok(HashedNodeReader::root_hash(self).or_default_root_hash())
    }

    fn val<K: api::KeyType>(&self, key: K) -> Result<Option<Value>, api::Error> {
        let merkle = Merkle::from(self);
        Ok(merkle.get_value(key.as_ref())?)
    }

    fn single_key_proof<K: api::KeyType>(&self, key: K) -> Result<FrozenProof, api::Error> {
        let merkle = Merkle::from(self);
        merkle.prove(key.as_ref()).map_err(api::Error::from)
    }

    fn range_proof<K: api::KeyType>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, api::Error> {
        Merkle::from(self).range_proof(
            first_key.as_ref().map(AsRef::as_ref),
            last_key.as_ref().map(AsRef::as_ref),
            limit,
        )
    }

    fn iter_option<K: KeyType>(&self, first_key: Option<K>) -> Result<Self::Iter<'_>, api::Error> {
        match first_key {
            Some(key) => Ok(MerkleKeyValueIter::from_key(self, key)),
            None => Ok(MerkleKeyValueIter::from(self)),
        }
    }
}

#[allow(dead_code)]
#[derive(Clone, Debug)]
pub enum UseParallel {
    Never,
    BatchSize(usize),
    Always,
}

/// Database configuration.
#[derive(Clone, TypedBuilder, Debug)]
#[non_exhaustive]
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
    // Whether to perform parallel proposal creation. If set to BatchSize, then firewood
    // performs parallel proposal creation if the batch is >= to the BatchSize value.
    // TODO: Experimentally determine the right value for BatchSize.
    #[builder(default = UseParallel::BatchSize(8))]
    pub use_parallel: UseParallel,
    /// `RootStore` directory path
    #[builder(default = None)]
    pub root_store_dir: Option<PathBuf>,
}

#[derive(Debug)]
/// A database instance.
pub struct Db {
    metrics: Arc<DbMetrics>,
    manager: RevisionManager,
    use_parallel: UseParallel,
}

impl api::Db for Db {
    type Historical = NodeStore<Committed, FileBacked>;

    type Proposal<'db>
        = Proposal<'db>
    where
        Self: 'db;

    fn revision(&self, root_hash: HashKey) -> Result<Arc<Self::Historical>, api::Error> {
        let nodestore = self.manager.revision(root_hash)?;
        Ok(nodestore)
    }

    fn root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        Ok(self.manager.root_hash()?.or_default_root_hash())
    }

    fn all_hashes(&self) -> Result<Vec<HashKey>, api::Error> {
        Ok(self.manager.all_hashes())
    }

    fn propose(&self, batch: impl IntoBatchIter) -> Result<Self::Proposal<'_>, api::Error> {
        self.propose_with_parent(batch, &self.manager.current_revision())
    }
}

impl Db {
    /// Create a new database instance.
    pub fn new<P: AsRef<Path>>(db_path: P, cfg: DbConfig) -> Result<Self, api::Error> {
        let root_store: Box<dyn RootStore + Send + Sync> = match &cfg.root_store_dir {
            Some(path) => {
                Box::new(FjallStore::new(path).map_err(RevisionManagerError::RootStoreError)?)
            }
            None => Box::new(NoOpStore {}),
        };

        Self::with_root_store(db_path, cfg, root_store)
    }

    fn with_root_store<P: AsRef<Path>>(
        db_path: P,
        cfg: DbConfig,
        root_store: Box<dyn RootStore + Send + Sync>,
    ) -> Result<Self, api::Error> {
        let metrics = Arc::new(DbMetrics {
            proposals: counter!("firewood.proposals"),
        });
        describe_counter!("firewood.proposals", "Number of proposals created");
        let config_manager = ConfigManager::builder()
            .create(cfg.create_if_missing)
            .truncate(cfg.truncate)
            .manager(cfg.manager)
            .build();

        let manager =
            RevisionManager::new(db_path.as_ref().to_path_buf(), config_manager, root_store)?;
        let db = Self {
            metrics,
            manager,
            use_parallel: cfg.use_parallel,
        };
        Ok(db)
    }

    /// Synchronously get a view, either committed or proposed
    pub fn view(&self, root_hash: HashKey) -> Result<ArcDynDbView, api::Error> {
        self.manager.view(root_hash).map_err(Into::into)
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

    /// Create a proposal with a specified parent. A proposal is created in parallel if `use_parallel`
    /// is `Always` or if `use_parallel` is `BatchSize` and the batch is >= to the `BatchSize` value.
    ///
    /// # Panics
    ///
    /// Panics if the revision manager cannot create a thread pool.
    #[fastrace::trace(name = "propose")]
    fn propose_with_parent<F: Parentable>(
        &self,
        batch: impl IntoBatchIter,
        parent: &NodeStore<F, FileBacked>,
    ) -> Result<Proposal<'_>, api::Error> {
        // If use_parallel is BatchSize, then perform parallel proposal creation if the batch
        // size is >= BatchSize.
        let batch = batch.into_iter();
        let use_parallel = match self.use_parallel {
            UseParallel::Never => false,
            UseParallel::Always => true,
            UseParallel::BatchSize(required_size) => batch.size_hint().0 >= required_size,
        };
        let immutable = if use_parallel {
            let mut parallel_merkle = ParallelMerkle::default();
            let _span = fastrace::Span::enter_with_local_parent("parallel_merkle");
            parallel_merkle.create_proposal(parent, batch, self.manager.threadpool())?
        } else {
            let proposal = NodeStore::new(parent)?;
            let mut merkle = Merkle::from(proposal);
            let span = fastrace::Span::enter_with_local_parent("merkleops");
            for res in batch.into_batch_iter::<api::Error>() {
                match res? {
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
            let _span = fastrace::Span::enter_with_local_parent("freeze");
            let nodestore = merkle.into_inner();
            Arc::new(nodestore.try_into()?)
        };
        self.manager.add_proposal(immutable.clone());

        self.metrics.proposals.increment(1);

        Ok(Proposal {
            nodestore: immutable,
            db: self,
        })
    }

    /// Merge a range of key-values into a new proposal on top of the current
    /// root revision.
    ///
    /// All items within the range `(first_key..=last_key)` will be replaced with
    /// the provided key-values from the iterator. I.e., any existing keys within
    /// the range that are not present in the provided key-values will be deleted,
    /// any duplicate keys will be overwritten, and any new keys will be inserted.
    ///
    /// Invariant: `key_values` must be sorted by key in ascending order; however,
    /// because debug assertions are disabled, this is not checked.
    pub fn merge_key_value_range(
        &self,
        first_key: Option<impl KeyType>,
        last_key: Option<impl KeyType>,
        key_values: impl IntoIterator<Item: KeyValuePair>,
    ) -> Result<Proposal<'_>, api::Error> {
        self.merge_key_value_range_with_parent(
            first_key,
            last_key,
            key_values,
            &self.manager.current_revision(),
        )
    }

    /// Merge a range of key-values into a new proposal on top of a specified parent.
    ///
    /// All items within the range `(first_key..=last_key)` will be replaced with
    /// the provided key-values from the iterator. I.e., any existing keys within
    /// the range that are not present in the provided key-values will be deleted,
    /// any duplicate keys will be overwritten, and any new keys will be inserted.
    ///
    /// Invariant: `key_values` must be sorted by key in ascending order; however,
    /// because debug assertions are disabled, this is not checked.
    pub fn merge_key_value_range_with_parent<F: Parentable>(
        &self,
        first_key: Option<impl KeyType>,
        last_key: Option<impl KeyType>,
        key_values: impl IntoIterator<Item: KeyValuePair>,
        parent: &NodeStore<F, FileBacked>,
    ) -> Result<Proposal<'_>, api::Error>
    where
        NodeStore<F, FileBacked>: TrieReader,
    {
        let merkle = Merkle::from(parent);
        let merge_ops = merkle.merge_key_value_range(first_key, last_key, key_values);
        self.propose_with_parent(merge_ops, merkle.nodestore())
    }
}

#[derive(Debug)]
/// A user-visible database proposal
pub struct Proposal<'db> {
    nodestore: Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>>,
    db: &'db Db,
}

impl api::DbView for Proposal<'_> {
    type Iter<'view>
        = MerkleKeyValueIter<'view, NodeStore<Arc<ImmutableProposal>, FileBacked>>
    where
        Self: 'view;

    fn root_hash(&self) -> Result<Option<api::HashKey>, api::Error> {
        api::DbView::root_hash(&*self.nodestore)
    }

    fn val<K: KeyType>(&self, key: K) -> Result<Option<Value>, api::Error> {
        api::DbView::val(&*self.nodestore, key)
    }

    fn single_key_proof<K: KeyType>(&self, key: K) -> Result<FrozenProof, api::Error> {
        api::DbView::single_key_proof(&*self.nodestore, key)
    }

    fn range_proof<K: KeyType>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, api::Error> {
        api::DbView::range_proof(&*self.nodestore, first_key, last_key, limit)
    }

    fn iter_option<K: KeyType>(&self, first_key: Option<K>) -> Result<Self::Iter<'_>, api::Error> {
        api::DbView::iter_option(&*self.nodestore, first_key)
    }
}

impl<'db> api::Proposal for Proposal<'db> {
    type Proposal = Proposal<'db>;

    fn propose(&self, batch: impl IntoBatchIter) -> Result<Self::Proposal, api::Error> {
        self.create_proposal(batch)
    }

    fn commit(self) -> Result<(), api::Error> {
        Ok(self.db.manager.commit(self.nodestore)?)
    }
}

impl Proposal<'_> {
    #[crate::metrics("firewood.proposal.create", "database proposal creation")]
    fn create_proposal(&self, batch: impl IntoBatchIter) -> Result<Self, api::Error> {
        self.db.propose_with_parent(batch, &self.nodestore)
    }
}

#[cfg(test)]
mod test {
    #![expect(clippy::unwrap_used)]

    use core::iter::Take;
    use std::collections::HashMap;
    use std::iter::Peekable;
    use std::num::NonZeroUsize;
    use std::ops::{Deref, DerefMut};
    use std::path::PathBuf;

    use firewood_storage::{
        CheckOpt, CheckerError, HashedNodeReader, IntoHashType, NodeStore, TrieHash,
    };

    use crate::db::{Db, Proposal, UseParallel};
    use crate::manager::RevisionManagerConfig;
    use crate::root_store::RootStore;
    use crate::v2::api::{Db as _, DbView, Proposal as _};

    use super::{BatchOp, DbConfig};

    /// A chunk of an iterator, provided by [`IterExt::chunk_fold`] to the folding
    /// function.
    type Chunk<'chunk, 'base, T> = &'chunk mut Take<&'base mut Peekable<T>>;

    trait IterExt: Iterator {
        /// Asynchronously folds the iterator with chunks of a specified size. The last
        /// chunk may be smaller than the specified size.
        ///
        /// The folding function is a closure that takes an accumulator and a
        /// chunk of the underlying iterator, and returns a new accumulator.
        ///
        /// # Panics
        ///
        /// If the folding function does not consume the entire chunk, it will panic.
        ///
        /// If the folding function panics, the iterator will be dropped (because this
        /// method consumes `self`).
        fn chunk_fold<B, F>(self, chunk_size: NonZeroUsize, init: B, mut f: F) -> B
        where
            Self: Sized,
            F: for<'a, 'b> FnMut(B, Chunk<'a, 'b, Self>) -> B,
        {
            let chunk_size = chunk_size.get();
            let mut iter = self.peekable();
            let mut acc = init;
            while iter.peek().is_some() {
                let mut chunk = iter.by_ref().take(chunk_size);
                acc = f(acc, chunk.by_ref());
                assert!(chunk.next().is_none(), "entire chunk was not consumed");
            }
            acc
        }
    }

    impl<T: Iterator> IterExt for T {}

    #[cfg(test)]
    impl Db {
        /// Extract the root store by consuming the database instance.
        /// This is primarily used for reopening or replacing the database with the same root store.
        pub fn into_root_store(self) -> Box<dyn RootStore + Send + Sync> {
            self.manager.into_root_store()
        }
    }

    #[test]
    fn test_proposal_reads() {
        let db = TestDb::new();
        let batch = vec![BatchOp::Put {
            key: b"k",
            value: b"v",
        }];
        let proposal = db.propose(batch).unwrap();
        assert_eq!(&*proposal.val(b"k").unwrap().unwrap(), b"v");

        assert_eq!(proposal.val(b"notfound").unwrap(), None);
        proposal.commit().unwrap();

        let batch = vec![BatchOp::Put {
            key: b"k",
            value: b"v2",
        }];
        let proposal = db.propose(batch).unwrap();
        assert_eq!(&*proposal.val(b"k").unwrap().unwrap(), b"v2");

        let committed = db.root_hash().unwrap().unwrap();
        let historical = db.revision(committed).unwrap();
        assert_eq!(&*historical.val(b"k").unwrap().unwrap(), b"v");
    }

    #[test]
    fn reopen_test() {
        let db = TestDb::new();
        let initial_root = db.root_hash().unwrap();
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
        let proposal = db.propose(batch).unwrap();
        proposal.commit().unwrap();
        println!("{:?}", db.root_hash().unwrap().unwrap());

        let db = db.reopen();
        println!("{:?}", db.root_hash().unwrap().unwrap());
        let committed = db.root_hash().unwrap().unwrap();
        let historical = db.revision(committed).unwrap();
        assert_eq!(&*historical.val(b"a").unwrap().unwrap(), b"1");
        drop(historical);

        let db = db.replace();
        println!("{:?}", db.root_hash().unwrap());
        assert!(db.root_hash().unwrap() == initial_root);
    }

    #[test]
    // test that dropping a proposal removes it from the list of known proposals
    //    /-> P1 - will get committed
    // R1 --> P2 - will get dropped
    //    \-> P3 - will get orphaned, but it's still known
    fn test_proposal_scope_historic() {
        let db = TestDb::new();
        let batch1 = vec![BatchOp::Put {
            key: b"k1",
            value: b"v1",
        }];
        let proposal1 = db.propose(batch1).unwrap();
        assert_eq!(&*proposal1.val(b"k1").unwrap().unwrap(), b"v1");

        let batch2 = vec![BatchOp::Put {
            key: b"k2",
            value: b"v2",
        }];
        let proposal2 = db.propose(batch2).unwrap();
        assert_eq!(&*proposal2.val(b"k2").unwrap().unwrap(), b"v2");

        let batch3 = vec![BatchOp::Put {
            key: b"k3",
            value: b"v3",
        }];
        let proposal3 = db.propose(batch3).unwrap();
        assert_eq!(&*proposal3.val(b"k3").unwrap().unwrap(), b"v3");

        // the proposal is dropped here, but the underlying
        // nodestore is still accessible because it's referenced by the revision manager
        // The third proposal remains referenced
        let p2hash = proposal2.root_hash().unwrap().unwrap();
        assert!(db.all_hashes().unwrap().contains(&p2hash));
        drop(proposal2);

        // commit the first proposal
        proposal1.commit().unwrap();
        // Ensure we committed the first proposal's data
        let committed = db.root_hash().unwrap().unwrap();
        let historical = db.revision(committed).unwrap();
        assert_eq!(&*historical.val(b"k1").unwrap().unwrap(), b"v1");

        // the second proposal shouldn't be available to commit anymore
        assert!(!db.all_hashes().unwrap().contains(&p2hash));

        // the third proposal should still be contained within the all_hashes list
        // would be deleted if another proposal was committed and proposal3 was dropped here
        let hash3 = proposal3.root_hash().unwrap().unwrap();
        assert!(db.manager.all_hashes().contains(&hash3));
    }

    #[test]
    // test that dropping a proposal removes it from the list of known proposals
    // R1 - base revision
    //  \-> P1 - will get committed
    //   \-> P2 - will get dropped
    //    \-> P3 - will get orphaned, but it's still known
    fn test_proposal_scope_orphan() {
        let db = TestDb::new();
        let batch1 = vec![BatchOp::Put {
            key: b"k1",
            value: b"v1",
        }];
        let proposal1 = db.propose(batch1).unwrap();
        assert_eq!(&*proposal1.val(b"k1").unwrap().unwrap(), b"v1");

        let batch2 = vec![BatchOp::Put {
            key: b"k2",
            value: b"v2",
        }];
        let proposal2 = proposal1.propose(batch2).unwrap();
        assert_eq!(&*proposal2.val(b"k2").unwrap().unwrap(), b"v2");

        let batch3 = vec![BatchOp::Put {
            key: b"k3",
            value: b"v3",
        }];
        let proposal3 = proposal2.propose(batch3).unwrap();
        assert_eq!(&*proposal3.val(b"k3").unwrap().unwrap(), b"v3");

        // the proposal is dropped here, but the underlying
        // nodestore is still accessible because it's referenced by the revision manager
        // The third proposal remains referenced
        let p2hash = proposal2.root_hash().unwrap().unwrap();
        assert!(db.all_hashes().unwrap().contains(&p2hash));
        drop(proposal2);

        // commit the first proposal
        proposal1.commit().unwrap();
        // Ensure we committed the first proposal's data
        let committed = db.root_hash().unwrap().unwrap();
        let historical = db.revision(committed).unwrap();
        assert_eq!(&*historical.val(b"k1").unwrap().unwrap(), b"v1");

        // the second proposal shouldn't be available to commit anymore
        assert!(!db.all_hashes().unwrap().contains(&p2hash));

        // the third proposal should still be contained within the all_hashes list
        let hash3 = proposal3.root_hash().unwrap().unwrap();
        assert!(db.manager.all_hashes().contains(&hash3));

        // moreover, the data from the second and third proposals should still be available
        // through proposal3
        assert_eq!(&*proposal3.val(b"k2").unwrap().unwrap(), b"v2");
        assert_eq!(&*proposal3.val(b"k3").unwrap().unwrap(), b"v3");
    }

    #[test]
    fn test_view_sync() {
        let db = TestDb::new();

        // Create and commit some data to get a historical revision
        let batch = vec![BatchOp::Put {
            key: b"historical_key",
            value: b"historical_value",
        }];
        let proposal = db.propose(batch).unwrap();
        let historical_hash = proposal.root_hash().unwrap().unwrap();
        proposal.commit().unwrap();

        // Create a new proposal (uncommitted)
        let batch = vec![BatchOp::Put {
            key: b"proposal_key",
            value: b"proposal_value",
        }];
        let proposal = db.propose(batch).unwrap();
        let proposal_hash = proposal.root_hash().unwrap().unwrap();

        // Test that view_sync can find the historical revision
        let historical_view = db.view(historical_hash).unwrap();
        let value = historical_view.val(b"historical_key").unwrap().unwrap();
        assert_eq!(&*value, b"historical_value");

        // Test that view_sync can find the proposal
        let proposal_view = db.view(proposal_hash).unwrap();
        let value = proposal_view.val(b"proposal_key").unwrap().unwrap();
        assert_eq!(&*value, b"proposal_value");
    }

    #[test]
    fn test_propose_parallel_reopen() {
        fn insert_commit(db: &TestDb, kv: u8) {
            let keys: Vec<[u8; 1]> = vec![[kv; 1]];
            let vals: Vec<Box<[u8]>> = vec![Box::new([kv; 1])];
            let kviter = keys.iter().zip(vals.iter());
            let proposal = db.propose(kviter).unwrap();
            proposal.commit().unwrap();
        }

        // Create, insert, close, open, insert
        let db = TestDb::new_with_config(
            DbConfig::builder()
                .use_parallel(UseParallel::Always)
                .build(),
        );
        insert_commit(&db, 1);
        let db = db.reopen_with_config(
            DbConfig::builder()
                .truncate(false)
                .use_parallel(UseParallel::Always)
                .build(),
        );
        insert_commit(&db, 2);
        // Check that the keys are still there after the commits
        let committed = db.revision(db.root_hash().unwrap().unwrap()).unwrap();
        let keys: Vec<[u8; 1]> = vec![[1; 1], [2; 1]];
        let vals: Vec<Box<[u8]>> = vec![Box::new([1; 1]), Box::new([2; 1])];
        let kviter = keys.iter().zip(vals.iter());
        for (k, v) in kviter {
            assert_eq!(&committed.val(k).unwrap().unwrap(), v);
        }
        drop(db);

        // Open-db1, insert, open-db2, insert
        let db1 = TestDb::new_with_config(
            DbConfig::builder()
                .use_parallel(UseParallel::Always)
                .build(),
        );
        insert_commit(&db1, 1);
        let db2 = TestDb::new_with_config(
            DbConfig::builder()
                .use_parallel(UseParallel::Always)
                .build(),
        );
        insert_commit(&db2, 2);
        let committed1 = db1.revision(db1.root_hash().unwrap().unwrap()).unwrap();
        let committed2 = db2.revision(db2.root_hash().unwrap().unwrap()).unwrap();
        let keys: Vec<[u8; 1]> = vec![[1; 1], [2; 1]];
        let vals: Vec<Box<[u8]>> = vec![Box::new([1; 1]), Box::new([2; 1])];
        let mut kviter = keys.iter().zip(vals.iter());
        let (k, v) = kviter.next().unwrap();
        assert_eq!(&committed1.val(k).unwrap().unwrap(), v);
        let (k, v) = kviter.next().unwrap();
        assert_eq!(&committed2.val(k).unwrap().unwrap(), v);
    }

    #[test]
    fn test_propose_parallel() {
        const N: usize = 100;
        let db = TestDb::new_with_config(
            DbConfig::builder()
                .use_parallel(UseParallel::Always)
                .build(),
        );

        // Test an empty proposal
        let keys: Vec<[u8; 0]> = Vec::new();
        let vals: Vec<Box<[u8]>> = Vec::new();

        let kviter = keys.iter().zip(vals.iter());
        let proposal = db.propose(kviter).unwrap();
        proposal.commit().unwrap();

        // Create a proposal consisting of a single entry and an empty key.
        let keys: Vec<[u8; 0]> = vec![[0; 0]];

        // Note that if the value is [], then it is interpreted as a DeleteRange.
        // Instead, set value to [0]
        let vals: Vec<Box<[u8]>> = vec![Box::new([0; 1])];

        let kviter = keys.iter().zip(vals.iter());
        let proposal = db.propose(kviter).unwrap();

        let kviter = keys.iter().zip(vals.iter());
        for (k, v) in kviter {
            assert_eq!(&proposal.val(k).unwrap().unwrap(), v);
        }
        proposal.commit().unwrap();

        // Check that the key is still there after the commit
        let committed = db.revision(db.root_hash().unwrap().unwrap()).unwrap();
        let kviter = keys.iter().zip(vals.iter());
        for (k, v) in kviter {
            assert_eq!(&committed.val(k).unwrap().unwrap(), v);
        }

        // Create a proposal that deletes the previous entry
        let vals: Vec<Box<[u8]>> = vec![Box::new([0; 0])];
        let kviter = keys.iter().zip(vals.iter());
        let proposal = db.propose(kviter).unwrap();

        let kviter = keys.iter().zip(vals.iter());
        for (k, _v) in kviter {
            assert_eq!(proposal.val(k).unwrap(), None);
        }
        proposal.commit().unwrap();

        // Create a proposal that inserts 0 to 999
        let (keys, vals): (Vec<_>, Vec<_>) = (0..1000)
            .map(|i| {
                (
                    format!("key{i}").into_bytes(),
                    Box::from(format!("value{i}").as_bytes()),
                )
            })
            .unzip();

        let kviter = keys.iter().zip(vals.iter());
        let proposal = db.propose(kviter).unwrap();
        let kviter = keys.iter().zip(vals.iter());
        for (k, v) in kviter {
            assert_eq!(&proposal.val(k).unwrap().unwrap(), v);
        }
        proposal.commit().unwrap();

        // Create a proposal that deletes all of the even entries
        let (keys, vals): (Vec<_>, Vec<_>) = (0..1000)
            .filter_map(|i| {
                if i % 2 != 0 {
                    Some::<(Vec<u8>, Box<[u8]>)>((format!("key{i}").into_bytes(), Box::new([])))
                } else {
                    None
                }
            })
            .unzip();

        let kviter = keys.iter().zip(vals.iter());
        let proposal = db.propose(kviter).unwrap();
        let kviter = keys.iter().zip(vals.iter());
        for (k, _v) in kviter {
            assert_eq!(proposal.val(k).unwrap(), None);
        }
        proposal.commit().unwrap();

        // Create a proposal that deletes using empty prefix
        let keys: Vec<[u8; 0]> = vec![[0; 0]];
        let vals: Vec<Box<[u8]>> = vec![Box::new([0; 0])];
        let kviter = keys.iter().zip(vals.iter());
        let proposal = db.propose(kviter).unwrap();
        proposal.commit().unwrap();

        // Create N keys and values like (key0, value0)..(keyN, valueN)
        let rng = firewood_storage::SeededRng::from_env_or_random();
        let (keys, vals): (Vec<_>, Vec<_>) = (0..N)
            .map(|i| {
                (
                    rng.random::<[u8; 32]>(),
                    Box::from(format!("value{i}").as_bytes()),
                )
            })
            .unzip();

        // Looping twice to test that we are reusing the thread pool.
        for _ in 0..2 {
            let kviter = keys.iter().zip(vals.iter());
            let proposal = db.propose(kviter).unwrap();

            // iterate over the keys and values again, checking that the values are in the correct proposal
            let kviter = keys.iter().zip(vals.iter());

            for (k, v) in kviter {
                assert_eq!(&proposal.val(k).unwrap().unwrap(), v);
            }
            proposal.commit().unwrap();
        }
    }

    /// Test that proposing on a proposal works as expected
    ///
    /// Test creates two batches and proposes them, and verifies that the values are in the correct proposal.
    /// It then commits them one by one, and verifies the latest committed version is correct.
    #[test]
    fn test_propose_on_proposal() {
        // number of keys and values to create for this test
        const N: usize = 20;

        let db = TestDb::new();

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
        let mut kviter = keys.iter().zip(vals.iter());

        // create two proposals, second one has a base of the first one
        let proposal1 = db.propose(kviter.by_ref().take(N / 2)).unwrap();
        let proposal2 = proposal1.propose(kviter).unwrap();

        // iterate over the keys and values again, checking that the values are in the correct proposal
        let mut kviter = keys.iter().zip(vals.iter());

        // first half of the keys should be in both proposals
        for (k, v) in kviter.by_ref().take(N / 2) {
            assert_eq!(&proposal1.val(k).unwrap().unwrap(), v);
            assert_eq!(&proposal2.val(k).unwrap().unwrap(), v);
        }

        // remaining keys should only be in the second proposal
        for (k, v) in kviter {
            // second half of keys are in the second proposal
            assert_eq!(&proposal2.val(k).unwrap().unwrap(), v);
            // but not in the first
            assert_eq!(proposal1.val(k).unwrap(), None);
        }

        proposal1.commit().unwrap();

        // all keys are still in the second proposal (first is no longer accessible)
        for (k, v) in keys.iter().zip(vals.iter()) {
            assert_eq!(&proposal2.val(k).unwrap().unwrap(), v);
        }

        // commit the second proposal
        proposal2.commit().unwrap();

        // all keys are in the database
        let committed = db.root_hash().unwrap().unwrap();
        let revision = db.revision(committed).unwrap();

        for (k, v) in keys.into_iter().zip(vals.into_iter()) {
            assert_eq!(revision.val(k).unwrap().unwrap(), v);
        }
    }

    #[test]
    fn fuzz_checker() {
        let rng = firewood_storage::SeededRng::from_env_or_random();

        let db = TestDb::new();

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
            let proposal = db.propose(batch).unwrap();
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

    #[test]
    fn test_deep_propose() {
        const NUM_KEYS: NonZeroUsize = const { NonZeroUsize::new(2).unwrap() };
        const NUM_PROPOSALS: usize = 100;

        let db = TestDb::new();

        let ops = (0..(NUM_KEYS.get() * NUM_PROPOSALS))
            .map(|i| (format!("key{i}"), format!("value{i}")))
            .collect::<Vec<_>>();

        let proposals = ops.iter().chunk_fold(
            NUM_KEYS,
            Vec::<Proposal<'_>>::with_capacity(NUM_PROPOSALS),
            |mut proposals, ops| {
                let proposal = if let Some(parent) = proposals.last() {
                    parent.propose(ops).unwrap()
                } else {
                    db.propose(ops).unwrap()
                };

                proposals.push(proposal);
                proposals
            },
        );

        let last_proposal_root_hash = proposals.last().unwrap().root_hash().unwrap().unwrap();

        // commit the proposals
        for proposal in proposals {
            proposal.commit().unwrap();
        }

        // get the last committed revision
        let last_root_hash = db.root_hash().unwrap().unwrap();
        let committed = db.revision(last_root_hash.clone()).unwrap();

        // the last root hash should be the same as the last proposal root hash
        assert_eq!(last_root_hash, last_proposal_root_hash);

        // check that all the keys and values are still present
        for (k, v) in &ops {
            let found = committed.val(k).unwrap();
            assert_eq!(
                found.as_deref(),
                Some(v.as_bytes()),
                "Value for key {k:?} should be {v:?} but was {found:?}",
            );
        }
    }

    /// Test that reading from a proposal during commit works as expected
    #[test]
    fn test_read_during_commit() {
        use crate::db::Proposal;

        const CHANNEL_CAPACITY: usize = 8;

        let testdb = TestDb::new();
        let db = &testdb.db;

        let (tx, rx) = std::sync::mpsc::sync_channel::<Proposal<'_>>(CHANNEL_CAPACITY);
        let (result_tx, result_rx) = std::sync::mpsc::sync_channel(CHANNEL_CAPACITY);

        // scope will block until all scope-spawned threads finish
        std::thread::scope(|scope| {
            // Commit task
            scope.spawn(move || {
                while let Ok(proposal) = rx.recv() {
                    let result = proposal.commit();
                    // send result back to the main thread, both for synchronization and stopping the
                    // test on error
                    result_tx.send(result).unwrap();
                }
            });
            scope.spawn(move || {
                // Proposal creation
                for id in 0u32..5000 {
                    // insert a key of length 32 and a value of length 8,
                    // rotating between all zeroes through all 255
                    let batch = vec![BatchOp::Put {
                        key: [id as u8; 32],
                        value: [id as u8; 8],
                    }];
                    let proposal = db.propose(batch).unwrap();
                    let last_hash = proposal.root_hash().unwrap().unwrap();
                    let view = db.view(last_hash).unwrap();

                    tx.send(proposal).unwrap();

                    let key = [id as u8; 32];
                    let value = view.val(&key).unwrap().unwrap();
                    assert_eq!(&*value, &[id as u8; 8]);
                    result_rx.recv().unwrap().unwrap();
                }
                // close the channel, which will cause the commit task to exit
                drop(tx);
            });
        });
    }

    #[test]
    fn test_resurrect_unpersisted_root() {
        let db = TestDb::new();

        // First, create a revision to retrieve
        let key = b"key";
        let value = b"value";
        let batch = vec![BatchOp::Put { key, value }];

        let proposal = db.propose(batch).unwrap();
        let root_hash = proposal.root_hash().unwrap().unwrap();
        proposal.commit().unwrap();

        let root_address = db
            .revision(root_hash.clone())
            .unwrap()
            .root_address()
            .unwrap();

        // Next, overwrite the kv-pair with a new revision
        let new_value = b"new_value";
        let batch = vec![BatchOp::Put {
            key,
            value: new_value,
        }];

        let proposal = db.propose(batch).unwrap();
        proposal.commit().unwrap();

        // Finally, reopen the database and make sure that we can retrieve the first revision
        let db = db.reopen();

        let latest_root_hash = db.root_hash().unwrap().unwrap();
        let latest_revision = db.revision(latest_root_hash).unwrap();

        let latest_value = latest_revision.val(key).unwrap().unwrap();
        assert_eq!(new_value, latest_value.as_ref());

        let node_store =
            NodeStore::with_root(root_hash.into_hash_type(), root_address, latest_revision);

        let retrieved_value = node_store.val(key).unwrap().unwrap();
        assert_eq!(value, retrieved_value.as_ref());
    }

    /// Verifies that persisted revisions are still accessible when reopening the database.
    #[test]
    fn test_fjall_store() {
        let db = TestDb::new_with_fjall_store(DbConfig::builder().build());

        // First, create a revision to retrieve
        let key = b"key";
        let value = b"value";
        let batch = vec![BatchOp::Put { key, value }];

        let proposal = db.propose(batch).unwrap();
        let root_hash = proposal.root_hash().unwrap().unwrap();
        proposal.commit().unwrap();

        // Next, overwrite the kv-pair with a new revision
        let new_value = b"new_value";
        let batch = vec![BatchOp::Put {
            key,
            value: new_value,
        }];

        let proposal = db.propose(batch).unwrap();
        proposal.commit().unwrap();

        // Reopen the database and verify that the database can access a persisted revision
        let db = db.reopen();

        let view = db.view(root_hash).unwrap();
        let retrieved_value = view.val(key).unwrap().unwrap();
        assert_eq!(value, retrieved_value.as_ref());
    }

    #[test]
    fn test_rootstore_empty_db_reopen() {
        let db = TestDb::new_with_fjall_store(DbConfig::builder().build());

        db.reopen();
    }

    /// Verifies that revisions exceeding the in-memory limit can still be retrieved.
    #[test]
    fn test_fjall_store_with_capped_max_revisions() {
        const NUM_REVISIONS: usize = 10;

        let dbconfig = DbConfig::builder()
            .manager(RevisionManagerConfig::builder().max_revisions(5).build())
            .build();
        let db = TestDb::new_with_fjall_store(dbconfig);

        // Create and commit 10 proposals
        let key = b"root_store";
        let revisions: HashMap<TrieHash, _> = (0..NUM_REVISIONS)
            .map(|i| {
                let value = i.to_be_bytes();
                let batch = vec![BatchOp::Put { key, value }];
                let proposal = db.propose(batch).unwrap();
                let root_hash = proposal.root_hash().unwrap().unwrap();
                proposal.commit().unwrap();

                (root_hash, value)
            })
            .collect();

        // Verify that we can access all revisions with their correct values
        for (root_hash, value) in &revisions {
            let revision = db.revision(root_hash.clone()).unwrap();
            let retrieved_value = revision.val(key).unwrap().unwrap();
            assert_eq!(value.as_slice(), retrieved_value.as_ref());
        }

        let db = db.reopen();

        // Verify that we can access all revisions with their correct values
        // after reopening
        for (root_hash, value) in &revisions {
            let revision = db.revision(root_hash.clone()).unwrap();
            let retrieved_value = revision.val(key).unwrap().unwrap();
            assert_eq!(value.as_slice(), retrieved_value.as_ref());
        }
    }

    // Testdb is a helper struct for testing the Db. Once it's dropped, the directory and file disappear
    pub(super) struct TestDb {
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

    impl TestDb {
        pub fn new() -> Self {
            TestDb::new_with_config(DbConfig::builder().build())
        }

        pub fn new_with_config(dbconfig: DbConfig) -> Self {
            let tmpdir = tempfile::tempdir().unwrap();
            let dbpath: PathBuf = [tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
                .iter()
                .collect();
            let db = Db::new(dbpath, dbconfig).unwrap();
            TestDb { db, tmpdir }
        }

        /// Creates a new test database with `FjallStore` enabled.
        ///
        /// Overrides `root_store_dir` in dbconfig to provide a directory for `FjallStore`.
        pub fn new_with_fjall_store(dbconfig: DbConfig) -> Self {
            let tmpdir = tempfile::tempdir().unwrap();
            let dbpath: PathBuf = [tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
                .iter()
                .collect();
            let root_store_path = tmpdir.as_ref().join("fjall_store");

            let dbconfig = DbConfig {
                root_store_dir: Some(root_store_path),
                ..dbconfig
            };

            let db = Db::new(dbpath, dbconfig).unwrap();
            TestDb { db, tmpdir }
        }

        pub fn reopen(self) -> Self {
            self.reopen_with_config(DbConfig::builder().truncate(false).build())
        }

        pub fn reopen_with_config(self, dbconfig: DbConfig) -> Self {
            let path = self.path();
            let TestDb { db, tmpdir } = self;

            let root_store = db.into_root_store();

            let db = Db::with_root_store(path, dbconfig, root_store).unwrap();
            TestDb { db, tmpdir }
        }

        pub fn replace(self) -> Self {
            let path = self.path();
            let TestDb { db, tmpdir } = self;

            let root_store = db.into_root_store();

            let dbconfig = DbConfig::builder().truncate(true).build();
            let db = Db::with_root_store(path, dbconfig, root_store).unwrap();
            TestDb { db, tmpdir }
        }

        pub fn path(&self) -> PathBuf {
            [self.tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
                .iter()
                .collect()
        }
    }
}
