// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::default_trait_access,
    reason = "Found 3 occurrences after enabling the lint."
)]

use nonzero_ext::nonzero;
use parking_lot::{Mutex, MutexGuard, RwLock};
use std::collections::{HashMap, VecDeque};
use std::io;
use std::num::{NonZero, NonZeroU64};
use std::path::PathBuf;
use std::sync::{Arc, OnceLock};

use firewood_storage::logger::{trace, warn};
use rayon::{ThreadPool, ThreadPoolBuilder};
use typed_builder::TypedBuilder;

use crate::merkle::Merkle;
use crate::persist_worker::{PersistError, PersistWorker};
use crate::root_store::RootStore;
use crate::v2::api::{ArcDynDbView, HashKey, OptionalHashKeyExt};

use firewood_metrics::{firewood_increment, firewood_set};
pub use firewood_storage::CacheReadStrategy;
use firewood_storage::{
    BranchNode, Committed, FileBacked, FileIoError, HashedNodeReader, ImmutableProposal,
    NodeHashAlgorithm, NodeStore, NodeStoreHeader, TrieHash,
};

pub(crate) const DB_FILE_NAME: &str = "firewood.db";

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, TypedBuilder)]
/// Revision manager configuratoin
pub struct RevisionManagerConfig {
    /// The number of committed revisions to keep in memory.
    #[builder(default = 128)]
    max_revisions: usize,

    /// The size of the node cache (number of entries).
    ///
    /// **Deprecated:** Use `node_cache_memory_limit` instead for memory-based sizing.
    /// If specified, this value is multiplied by 128 to estimate memory usage.
    /// Cannot be specified together with `node_cache_memory_limit`.
    #[deprecated(since = "0.2.0", note = "Use node_cache_memory_limit instead")]
    #[builder(default, setter(strip_option))]
    node_cache_size: Option<NonZero<usize>>,

    /// The memory limit for the node cache in bytes.
    ///
    /// If neither this nor `node_cache_size` is specified, defaults to 192MB (1,500,000 × 128).
    /// Cannot be specified together with `node_cache_size`.
    #[builder(default, setter(strip_option))]
    node_cache_memory_limit: Option<NonZero<usize>>,

    #[builder(default_code = "NonZero::new(1000000).expect(\"non-zero\")")]
    free_list_cache_size: NonZero<usize>,

    #[builder(default = CacheReadStrategy::WritesOnly)]
    cache_read_strategy: CacheReadStrategy,
}

impl RevisionManagerConfig {
    /// Compute the actual node cache memory limit from the configuration.
    ///
    /// # Errors
    ///
    /// Returns an error if both `node_cache_size` and `node_cache_memory_limit` are specified.
    #[expect(deprecated)]
    pub(crate) fn compute_node_cache_memory_limit(
        &self,
    ) -> Result<NonZero<usize>, crate::v2::api::Error> {
        // Convert entry count to memory: size × 128 bytes per node (estimate)
        const BYTES_PER_NODE_ESTIMATE: usize = 128;
        // Default: 192MB (equivalent to 1,500,000 nodes × 128 bytes)
        const DEFAULT_MEMORY_LIMIT: usize = 192_000_000;

        match (self.node_cache_size, self.node_cache_memory_limit) {
            (Some(_), Some(_)) => Err(crate::v2::api::Error::ConflictingCacheConfig),
            (Some(size), None) => {
                warn!(
                    "node_cache_size is deprecated as of 0.2.0; use node_cache_memory_limit instead"
                );
                Ok(
                    NonZero::new(size.get().saturating_mul(BYTES_PER_NODE_ESTIMATE))
                        .expect("non-zero size produces non-zero memory"),
                )
            }
            (None, Some(limit)) => Ok(limit),
            (None, None) => Ok(NonZero::new(DEFAULT_MEMORY_LIMIT).expect("default is non-zero")),
        }
    }
}

#[derive(Clone, Debug, TypedBuilder)]
#[non_exhaustive]
/// Configuration manager that contains both truncate and revision manager config
pub struct ConfigManager {
    /// The directory where the database files will be stored (required).
    pub root_dir: PathBuf,
    /// The algorithm used for hashing nodes (required).
    pub node_hash_algorithm: NodeHashAlgorithm,

    /// Whether to create the DB if it doesn't exist.
    #[builder(default = true)]
    pub create: bool,
    /// Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    /// existing contents will be lost.
    #[builder(default = false)]
    pub truncate: bool,
    /// Whether to enable `RootStore`.
    #[builder(default = false)]
    pub root_store: bool,
    /// The maximum number of unpersisted revisions that can exist at a given time.
    #[builder(default = nonzero!(1u64))]
    pub deferred_persistence_commit_count: NonZeroU64,
    /// Revision manager configuration.
    #[builder(default = RevisionManagerConfig::builder().build())]
    pub manager: RevisionManagerConfig,
}

pub type CommittedRevision = Arc<NodeStore<Committed, FileBacked>>;
type ProposedRevision = Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>>;

#[derive(Debug)]
pub(crate) struct RevisionManager {
    /// Maximum number of revisions to keep in memory.
    ///
    /// When this limit is exceeded, the oldest revision is removed from memory.
    /// If `root_store` is `None`, the oldest revision's nodes are added to the
    /// free list for space reuse. Otherwise, the oldest revision
    /// is preserved on disk for historical queries.
    max_revisions: usize,

    /// FIFO queue of committed revisions kept in memory. The queue always
    /// contains at least one revision.
    in_memory_revisions: RwLock<VecDeque<CommittedRevision>>,

    /// Hash-based index of committed revisions kept in memory.
    ///
    /// Maps root hash to the corresponding committed revision for O(1) lookup
    /// performance. This allows efficient retrieval of revisions without
    /// scanning through the `in_memory_revisions` queue.
    by_hash: RwLock<HashMap<TrieHash, CommittedRevision>>,

    /// Active proposals that have not yet been committed.
    proposals: Mutex<Vec<ProposedRevision>>,

    /// Lazily initialized thread pool for parallel operations.
    threadpool: OnceLock<ThreadPool>,

    /// Optional persistent store for historical root addresses.
    ///
    /// When present, enables retrieval of revisions beyond `max_revisions` by
    /// persisting root hash to disk address mappings. This allows historical
    /// queries of arbitrarily old revisions without keeping them in memory.
    root_store: Option<Arc<RootStore>>,

    /// Worker responsible for persisting revisions to disk.
    persist_worker: PersistWorker,
}

#[derive(Debug, thiserror::Error)]
pub(crate) enum RevisionManagerError {
    #[error("Revision for {provided:?} not found")]
    RevisionNotFound { provided: HashKey },
    #[error("Revision for {provided:?} has no address")]
    RevisionWithoutAddress { provided: HashKey },
    #[error(
        "The proposal cannot be committed since it is not a direct child of the most recent commit. Proposal parent: {provided:?}, current root: {expected:?}"
    )]
    NotLatest {
        provided: Option<HashKey>,
        expected: Option<HashKey>,
    },
    #[error("A FileIO error occurred during the commit: {0}")]
    FileIoError(#[from] FileIoError),
    #[error("An IO error occurred while creating the database directory: {0}")]
    IOError(#[from] io::Error),
    #[error("A RootStore error occurred: {0}")]
    RootStoreError(#[source] Box<dyn std::error::Error + Send + Sync>),
    #[error("A deferred persistence error occurred: {0}")]
    PersistError(#[source] PersistError),
}

impl RevisionManager {
    pub fn new(config: ConfigManager) -> Result<Self, RevisionManagerError> {
        if config.create {
            std::fs::create_dir_all(&config.root_dir).map_err(RevisionManagerError::IOError)?;
        }

        let file = config.root_dir.join(DB_FILE_NAME);
        let node_cache_memory_limit =
            config
                .manager
                .compute_node_cache_memory_limit()
                .map_err(|e| {
                    RevisionManagerError::IOError(std::io::Error::new(
                        std::io::ErrorKind::InvalidInput,
                        e,
                    ))
                })?;
        let fb = FileBacked::new(
            file,
            node_cache_memory_limit,
            config.manager.free_list_cache_size,
            config.truncate,
            config.create,
            config.manager.cache_read_strategy,
            config.node_hash_algorithm,
        )?;

        // Acquire an advisory lock on the database file to prevent multiple processes
        // from opening the same database simultaneously
        fb.lock()?;

        let storage = Arc::new(fb);
        let header = match NodeStoreHeader::read_from_storage(storage.as_ref()) {
            Ok(header) => header,
            Err(err) if err.kind() == io::ErrorKind::UnexpectedEof => {
                // Empty file - create a new header for a fresh database
                NodeStoreHeader::new(config.node_hash_algorithm)
            }
            Err(err) => return Err(err.into()),
        };
        let nodestore = Arc::new(NodeStore::open(&header, storage.clone())?);
        let root_store = config
            .root_store
            .then(|| {
                let root_store_dir = config.root_dir.join("root_store");
                RootStore::new(root_store_dir, storage.clone(), config.truncate)
                    .map_err(RevisionManagerError::RootStoreError)
            })
            .transpose()?
            .map(Arc::new);

        if config.truncate {
            header.flush_to(storage.as_ref())?;
            storage.set_len(NodeStoreHeader::SIZE)?;
        }

        let mut by_hash = HashMap::new();
        if let Some(hash) = nodestore.root_hash().or_default_root_hash() {
            by_hash.insert(hash, nodestore.clone());
        }

        let persist_worker = PersistWorker::new(
            config.deferred_persistence_commit_count,
            header,
            root_store.clone(),
        );

        let manager = Self {
            max_revisions: config.manager.max_revisions,
            in_memory_revisions: RwLock::new(VecDeque::from([nodestore.clone()])),
            by_hash: RwLock::new(by_hash),
            proposals: Mutex::new(Default::default()),
            threadpool: OnceLock::new(),
            root_store,
            persist_worker,
        };

        // On startup, we always write the latest revision to RootStore
        if let Some(root_hash) = manager.current_revision().root_hash() {
            let root_address = manager.current_revision().root_address().ok_or(
                RevisionManagerError::RevisionWithoutAddress {
                    provided: root_hash.clone(),
                },
            )?;

            if let Some(store) = &manager.root_store {
                store
                    .add_root(&root_hash, &root_address)
                    .map_err(RevisionManagerError::RootStoreError)?;
            }
        }

        Ok(manager)
    }

    /// Commit a proposal
    /// To commit a proposal involves a few steps:
    /// 1. Check if the persist worker has failed.
    ///    If so, return the error as this means we won't be able to make any
    ///    further progress.
    /// 2. Commit check.
    ///    The proposal's parent must be the last committed revision, otherwise the commit fails.
    ///    It only contains the address of the nodes that are deleted, which should be very small.
    /// 3. Revision reaping.
    ///    If more than the maximum number of revisions are kept in memory, the
    ///    oldest revision is removed from memory and sent to the `PersistWorker`
    ///    for reaping.
    /// 4. Signal to the `PersistWorker` to persist this revision.
    /// 5. Set last committed revision.
    ///    Set last committed revision in memory.
    /// 6. Proposal Cleanup.
    ///    Any other proposals that have this proposal as a parent should be reparented to the committed version.
    ///
    /// Steps 1 through 5 are executed behind a lock to maintain the invariant
    /// that only one revision can commit at a time.
    #[fastrace::trace(short_name = true)]
    #[crate::metrics("proposal.commit", "proposal commit to storage")]
    pub fn commit(&self, proposal: ProposedRevision) -> Result<(), RevisionManagerError> {
        // Hold a write lock on `in_memory_revisions` for the duration of the
        // critical section (steps 1-5). This is necessary because:
        // 1. Without the lock, two proposals with the same parent could pass the
        //    commit check simultaneously, allowing both to commit.
        // 2. New proposals rely on the latest committed revision via
        //    `current_revision()`, which takes a read lock on `in_memory_revisions`.
        //    The write lock here prevents proposals from being created against
        //    an older revision while a newer revision is mid-commit.
        let mut in_memory_revisions = self.in_memory_revisions.write();

        // 1. Check if the persist worker has failed.
        self.persist_worker
            .check_error()
            .map_err(RevisionManagerError::PersistError)?;

        // 2. Commit check
        let current_revision = in_memory_revisions
            .back()
            .expect("there is always one revision");
        if !proposal.parent_hash_is(current_revision.root_hash()) {
            return Err(RevisionManagerError::NotLatest {
                provided: proposal.root_hash(),
                expected: current_revision.root_hash(),
            });
        }

        let committed = proposal.as_committed();

        // 3. Revision reaping
        // When we exceed max_revisions, remove the oldest revision from memory
        // and send it to the `PersistWorker`.
        // If you crash after freeing some of these, then the free list will point to nodes that are not actually free.
        // TODO: Handle the case where we get something off the free list that is not free
        while in_memory_revisions.len() >= self.max_revisions {
            let oldest = in_memory_revisions.pop_front().expect("must be present");
            let oldest_hash = oldest.root_hash().or_default_root_hash();
            if let Some(ref hash) = oldest_hash {
                self.by_hash.write().remove(hash);
            }

            // The warning in the docs for `Arc::try_unwrap` does not apply here
            // because `original` is retained and not immediately dropped.
            match Arc::try_unwrap(oldest) {
                Ok(oldest) => self
                    .persist_worker
                    .reap(oldest)
                    .map_err(RevisionManagerError::PersistError)?,
                Err(original) => {
                    warn!("Oldest revision could not be reaped; still referenced");
                    in_memory_revisions.push_front(original);
                    break;
                }
            }
            firewood_set!(crate::registry::ACTIVE_REVISIONS, in_memory_revisions.len());
            firewood_set!(crate::registry::MAX_REVISIONS, self.max_revisions);
        }

        // 4. Signal to the `PersistWorker` to persist this revision.
        let committed: CommittedRevision = committed.into();
        self.persist_worker
            .persist(committed.clone())
            .map_err(RevisionManagerError::PersistError)?;

        // 5. Set last committed revision
        // The revision is added to `by_hash` here while it still exists in `proposals`.
        // The `view()` method relies on this ordering - it checks `proposals` first,
        // then `by_hash`, ensuring the revision is always findable during the transition.
        in_memory_revisions.push_back(committed.clone());
        if let Some(hash) = committed.root_hash().or_default_root_hash() {
            self.by_hash.write().insert(hash, committed.clone());
        }

        // At this point, we can release the lock on the queue of in-memory
        // revisions as we've now set the new latest committed revision.
        drop(in_memory_revisions);

        // 6. Proposal Cleanup
        // Free proposal that is being committed as well as any proposals no longer
        // referenced by anyone else. Track how many were discarded (dropped without commit).
        {
            let mut lock = self.proposals.lock();
            let mut discarded = 0u64;
            lock.retain(|p| {
                let should_retain = !Arc::ptr_eq(&proposal, p) && Arc::strong_count(p) > 1;
                if !should_retain {
                    discarded = discarded.wrapping_add(1);
                }
                should_retain
            });

            if discarded > 0 {
                firewood_increment!(crate::registry::PROPOSALS_DISCARDED, discarded);
            }

            // Update uncommitted proposals gauge after cleanup
            firewood_set!(crate::registry::PROPOSALS_UNCOMMITTED, lock.len());
        }

        // then reparent any proposals that have this proposal as a parent
        for p in &*self.proposals.lock() {
            proposal.commit_reparent(p);
        }

        if crate::logger::trace_enabled() {
            let merkle = Merkle::from(committed);
            if let Ok(s) = merkle.dump_to_string() {
                trace!("{s}");
            }
        }

        Ok(())
    }

    /// View the database at a specific hash.
    /// To view the database at a specific hash involves a few steps:
    /// 1. Try to find it in proposals.
    /// 2. Try to find it in committed revisions.
    pub fn view(&self, root_hash: HashKey) -> Result<ArcDynDbView, RevisionManagerError> {
        // 1. Try to find it in proposals.
        let proposal = self
            .proposals
            .lock()
            .iter()
            .find(|p| p.root_hash().as_ref() == Some(&root_hash))
            .cloned();

        if let Some(proposal) = proposal {
            return Ok(proposal);
        }

        // 2. Try to find it in committed revisions.
        self.revision(root_hash).map(|r| r as ArcDynDbView)
    }

    pub fn add_proposal(&self, proposal: ProposedRevision) {
        let len = {
            let mut lock = self.proposals.lock();
            lock.push(proposal);
            lock.len()
        };
        // Update uncommitted proposals gauge after adding
        firewood_set!(crate::registry::PROPOSALS_UNCOMMITTED, len);
    }

    /// Retrieve a committed revision by its root hash.
    /// To retrieve a revision involves a few steps:
    /// 1. Check the in-memory revision manager.
    /// 2. Check `RootStore` (if it exists).
    pub fn revision(&self, root_hash: HashKey) -> Result<CommittedRevision, RevisionManagerError> {
        // 1. Check the in-memory revision manager.
        if let Some(revision) = self.by_hash.read().get(&root_hash).cloned() {
            return Ok(revision);
        }

        // 2. Check `RootStore` (if it exists).
        let root_store =
            self.root_store
                .as_ref()
                .ok_or(RevisionManagerError::RevisionNotFound {
                    provided: root_hash.clone(),
                })?;
        let revision = root_store
            .get(&root_hash)
            .map_err(RevisionManagerError::RootStoreError)?
            .ok_or(RevisionManagerError::RevisionNotFound {
                provided: root_hash.clone(),
            })?;

        Ok(revision)
    }

    pub fn root_hash(&self) -> Result<Option<HashKey>, RevisionManagerError> {
        Ok(self.current_revision().root_hash())
    }

    pub fn current_revision(&self) -> CommittedRevision {
        self.in_memory_revisions
            .read()
            .back()
            .expect("there is always one revision")
            .clone()
    }

    /// Acquires a lock on the header and returns a guard.
    pub(crate) fn locked_header(&self) -> MutexGuard<'_, NodeStoreHeader> {
        self.persist_worker.locked_header()
    }

    /// Gets or creates a threadpool associated with the revision manager.
    ///
    /// # Panics
    ///
    /// Panics if the it cannot create a thread pool.
    pub fn threadpool(&self) -> &ThreadPool {
        // Note that OnceLock currently doesn't support get_or_try_init (it is available in a
        // nightly release). The get_or_init should be replaced with get_or_try_init once it
        // is available to allow the error to be passed back to the caller.
        self.threadpool.get_or_init(|| {
            ThreadPoolBuilder::new()
                .num_threads(BranchNode::MAX_CHILDREN)
                .build()
                .expect("Error in creating threadpool")
        })
    }

    /// Checks if the `PersistWorker` has errored.
    pub fn check_persist_error(&self) -> Result<(), RevisionManagerError> {
        self.persist_worker
            .check_error()
            .map_err(RevisionManagerError::PersistError)
    }

    /// Closes the revision manager gracefully.
    ///
    /// This method shuts down the background persistence worker and persists
    /// the latest committed revision.
    pub fn close(self) -> Result<(), RevisionManagerError> {
        let current_revision = self.current_revision();
        self.persist_worker
            .close(current_revision)
            .map_err(RevisionManagerError::PersistError)
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use firewood_storage::RootReader;

    use super::*;

    impl RevisionManager {
        /// Get all proposal hashes available.
        pub fn proposal_hashes(&self) -> Vec<TrieHash> {
            self.proposals
                .lock()
                .iter()
                .filter_map(|p| p.root_hash().or_default_root_hash())
                .collect()
        }

        /// Wait until all pending commits have been persisted.
        pub(crate) fn wait_persisted(&self) {
            self.persist_worker.wait_persisted();
        }

        /// Returns true if the root node (if it exists) of this revision is
        /// persisted. Otherwise, returns false.
        ///
        /// ## Errors
        ///
        /// Returns an error if the revision does not exist.
        pub(crate) fn revision_persist_status(
            &self,
            root_hash: TrieHash,
        ) -> Result<bool, RevisionManagerError> {
            let revision = self.revision(root_hash)?;
            Ok(revision
                .root_as_maybe_persisted_node()
                .is_some_and(|node| node.unpersisted().is_none()))
        }
    }

    #[test]
    fn test_file_advisory_lock() {
        // Create a temporary file for testing
        let db_dir = tempfile::tempdir().unwrap();

        let config = ConfigManager::builder()
            .root_dir(db_dir.as_ref().to_path_buf())
            .node_hash_algorithm(NodeHashAlgorithm::compile_option())
            .create(true)
            .truncate(false)
            .build();

        // First database instance should open successfully
        let first_manager = RevisionManager::new(config.clone());
        assert!(
            first_manager.is_ok(),
            "First database should open successfully"
        );

        // Second database instance should fail to open due to file locking
        let second_manager = RevisionManager::new(config.clone());
        assert!(
            second_manager.is_err(),
            "Second database should fail to open"
        );

        // Verify the error message contains the expected information
        let error = second_manager.unwrap_err();
        let error_string = error.to_string();

        assert!(
            error_string.contains("database may be opened by another instance"),
            "Error is missing 'database may be opened by another instance', got: {error_string}"
        );

        // The file lock is held by the FileBacked instance. When we drop the first_manager,
        // the Arc<FileBacked> should be dropped, releasing the file lock.
        drop(first_manager.unwrap());

        // Now the second database should open successfully
        let third_manager = RevisionManager::new(config);
        assert!(
            third_manager.is_ok(),
            "Database should open after first instance is dropped"
        );
    }

    #[test]
    #[allow(clippy::too_many_lines)]
    fn test_slow_concurrent_view_during_commit() {
        use firewood_storage::{
            ImmutableProposal, LeafNode, NibblesIterator, Node, NodeStore, Path,
        };
        use std::sync::Barrier;
        use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
        use std::thread;

        const NUM_ITERATIONS: usize = 1000;
        const NUM_VIEWER_THREADS: usize = 10;
        const NUM_COMMITTER_THREADS: usize = 5;

        // Create a temporary database
        let db_dir = tempfile::tempdir().unwrap();

        let config = ConfigManager::builder()
            .root_dir(db_dir.as_ref().to_path_buf())
            .node_hash_algorithm(NodeHashAlgorithm::compile_option())
            .create(true)
            .manager(
                RevisionManagerConfig::builder()
                    .max_revisions(100_000) // Set very high to prevent reaping during test
                    .build(),
            )
            .build();

        let manager = Arc::new(RevisionManager::new(config).unwrap());

        // Create an initial proposal and commit it to have a non-empty base
        let base_revision = manager.current_revision();
        let mut proposal = NodeStore::new(&*base_revision).unwrap();
        {
            let root = proposal.root_mut();
            *root = Some(Node::Leaf(LeafNode {
                partial_path: Path::from_nibbles_iterator(NibblesIterator::new(b"initial")),
                value: b"value".to_vec().into_boxed_slice(),
            }));
        }
        let proposal: Arc<NodeStore<Arc<ImmutableProposal>, _>> =
            Arc::new(proposal.try_into().unwrap());
        manager.add_proposal(proposal.clone());
        manager.commit(proposal).unwrap();

        let error_count = Arc::new(AtomicUsize::new(0));
        let stop_flag = Arc::new(AtomicBool::new(false));
        let barrier = Arc::new(Barrier::new(NUM_VIEWER_THREADS + NUM_COMMITTER_THREADS));

        let mut handles = vec![];

        // Spawn viewer threads
        for thread_id in 0..NUM_VIEWER_THREADS {
            let manager = Arc::clone(&manager);
            let error_count = Arc::clone(&error_count);
            let stop_flag = Arc::clone(&stop_flag);
            let barrier = Arc::clone(&barrier);

            let handle = thread::spawn(move || {
                barrier.wait(); // Synchronize thread start

                let mut local_errors = 0;
                for _ in 0..NUM_ITERATIONS {
                    if stop_flag.load(Ordering::Relaxed) {
                        break;
                    }

                    // Try to view the current revision
                    let current_hash = manager.current_revision().root_hash();
                    if let Some(hash) = current_hash {
                        match manager.view(hash.clone()) {
                            Ok(_) => {}
                            Err(RevisionManagerError::RevisionNotFound { .. }) => {
                                local_errors += 1;
                                eprintln!("Thread {thread_id}: RevisionNotFound for hash {hash:?}");
                            }
                            Err(e) => {
                                eprintln!("Thread {thread_id}: Unexpected error: {e:?}");
                            }
                        }
                    }

                    // Small yield to increase contention
                    thread::yield_now();
                }

                error_count.fetch_add(local_errors, Ordering::Relaxed);
            });

            handles.push(handle);
        }

        // Spawn committer threads
        for thread_id in 0..NUM_COMMITTER_THREADS {
            let manager = Arc::clone(&manager);
            let stop_flag = Arc::clone(&stop_flag);
            let barrier = Arc::clone(&barrier);

            let handle = thread::spawn(move || {
                barrier.wait(); // Synchronize thread start

                for i in 0..NUM_ITERATIONS {
                    if stop_flag.load(Ordering::Relaxed) {
                        break;
                    }

                    // Create and commit a proposal
                    let current = manager.current_revision();
                    let mut new_proposal = match NodeStore::new(&*current) {
                        Ok(p) => p,
                        Err(e) => {
                            eprintln!("Thread {thread_id}: Failed to create proposal: {e:?}");
                            continue;
                        }
                    };

                    // Modify the proposal
                    {
                        let root = new_proposal.root_mut();
                        let key = format!("key_{thread_id}_{i}");
                        *root = Some(Node::Leaf(LeafNode {
                            partial_path: Path::from_nibbles_iterator(NibblesIterator::new(
                                key.as_bytes(),
                            )),
                            value: format!("value_{i}").as_bytes().to_vec().into_boxed_slice(),
                        }));
                    }

                    let immutable: Arc<NodeStore<Arc<ImmutableProposal>, _>> =
                        Arc::new(match new_proposal.try_into() {
                            Ok(p) => p,
                            Err(e) => {
                                eprintln!(
                                    "Thread {thread_id}: Failed to convert to immutable: {e:?}"
                                );
                                continue;
                            }
                        });

                    manager.add_proposal(immutable.clone());

                    match manager.commit(immutable) {
                        Ok(()) | Err(RevisionManagerError::NotLatest { .. }) => {
                            // Expected when multiple threads try to commit
                        }
                        Err(e) => {
                            eprintln!("Thread {thread_id}: Unexpected commit error: {e:?}");
                            stop_flag.store(true, Ordering::Relaxed);
                            break;
                        }
                    }

                    thread::yield_now();
                }
            });

            handles.push(handle);
        }

        // Wait for all threads to complete
        for handle in handles {
            handle.join().unwrap();
        }

        let total_errors = error_count.load(Ordering::Relaxed);

        if total_errors > 0 {
            eprintln!(
                "\nRace condition detected! {total_errors} RevisionNotFound errors occurred."
            );
            eprintln!("This confirms the race condition exists between commit and view.");
        } else {
            eprintln!(
                "\nNo race condition detected in this run. Try running the test multiple times or increasing iterations."
            );
        }

        // For now, we expect the race to occur, so we fail if we see errors
        assert_eq!(
            total_errors, 0,
            "Race condition detected: {total_errors} threads failed to find revisions that should exist"
        );
    }

    #[test]
    fn test_no_fjall_directory_when_root_store_disabled() {
        // Create a temporary directory for the database
        let db_dir = tempfile::tempdir().unwrap();
        let db_path = db_dir.as_ref().to_path_buf();

        // Create a database with root_store disabled (default)
        let config = ConfigManager::builder()
            .root_dir(db_path.clone())
            .node_hash_algorithm(NodeHashAlgorithm::compile_option())
            .create(true)
            .root_store(false)
            .build();

        let _manager = RevisionManager::new(config).unwrap();

        // Verify that the root_store directory does NOT exist
        let root_store_dir = db_path.join("root_store");
        assert!(
            !root_store_dir.exists(),
            "root_store directory should not be created when root_store is disabled"
        );
    }

    #[test]
    fn test_fjall_directory_when_root_store_enabled() {
        // Create a temporary directory for the database
        let db_dir = tempfile::tempdir().unwrap();
        let db_path = db_dir.as_ref().to_path_buf();

        // Create a database with root_store enabled
        let config = ConfigManager::builder()
            .root_dir(db_path.clone())
            .node_hash_algorithm(NodeHashAlgorithm::compile_option())
            .create(true)
            .root_store(true)
            .build();

        let _manager = RevisionManager::new(config).unwrap();

        // Verify that the root_store directory DOES exist
        let root_store_dir = db_path.join("root_store");
        assert!(
            root_store_dir.exists(),
            "root_store directory should be created when root_store is enabled"
        );
    }

    #[test]
    fn test_cache_config_both_specified_error() {
        // Test that specifying both node_cache_size and node_cache_memory_limit returns an error
        #[expect(deprecated)]
        let result = RevisionManagerConfig::builder()
            .node_cache_size(NonZero::new(1000).unwrap())
            .node_cache_memory_limit(NonZero::new(128_000).unwrap())
            .build()
            .compute_node_cache_memory_limit();

        assert!(result.is_err());
        assert!(matches!(
            result.unwrap_err(),
            crate::v2::api::Error::ConflictingCacheConfig
        ));
    }

    #[test]
    fn test_cache_config_default_memory_limit() {
        // Test that when neither field is specified, we get the default memory limit
        let config = RevisionManagerConfig::builder().build();
        let memory_limit = config.compute_node_cache_memory_limit().unwrap();

        // Default should be 192MB (1,500,000 × 128 bytes)
        assert_eq!(memory_limit.get(), 192_000_000);
    }

    #[test]
    fn test_cache_config_size_to_memory_conversion() {
        // Test that node_cache_size is correctly converted to memory (× 128)
        #[expect(deprecated)]
        let config = RevisionManagerConfig::builder()
            .node_cache_size(NonZero::new(1000).unwrap())
            .build();
        let memory_limit = config.compute_node_cache_memory_limit().unwrap();

        assert_eq!(memory_limit.get(), 1000 * 128);
    }

    #[test]
    fn test_cache_config_memory_limit_used_directly() {
        // Test that node_cache_memory_limit is used directly when specified
        let config = RevisionManagerConfig::builder()
            .node_cache_memory_limit(NonZero::new(256_000_000).unwrap())
            .build();
        let memory_limit = config.compute_node_cache_memory_limit().unwrap();

        assert_eq!(memory_limit.get(), 256_000_000);
    }
}
