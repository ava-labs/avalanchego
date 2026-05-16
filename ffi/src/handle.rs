// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::num::{NonZeroU64, NonZeroUsize};
use std::sync::Arc;

use firewood::{
    api::{
        self, ArcDynDbView, Db as _, DbView, FrozenChangeProof, HashKey, HashKeyExt, IntoBatchIter,
        KeyType,
    },
    db::{Db, DbConfig},
    manager::RevisionManagerConfig,
};
use firewood_storage::{Committed, FileBacked, NodeStore};

use crate::{BatchOp, BorrowedBytes, CView, CreateProposalResult};

use crate::revision::{GetRevisionResult, RevisionHandle};
use firewood_metrics::MetricsContext;

/// The hashing mode to use for the database.
///
/// This determines the cryptographic hash function and trie structure used.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub enum NodeHashAlgorithm {
    /// MerkleDB Firewood hashing (SHA-256 based)
    MerkleDB = 0,
    /// Ethereum-compatible hashing (Keccak-256 based)
    Ethereum = 1,
}

impl From<NodeHashAlgorithm> for firewood_storage::NodeHashAlgorithm {
    fn from(alg: NodeHashAlgorithm) -> Self {
        match alg {
            NodeHashAlgorithm::MerkleDB => firewood_storage::NodeHashAlgorithm::MerkleDB,
            NodeHashAlgorithm::Ethereum => firewood_storage::NodeHashAlgorithm::Ethereum,
        }
    }
}

/// Arguments for creating or opening a database. These are passed to [`fwd_open_db`]
///
/// [`fwd_open_db`]: crate::fwd_open_db
#[repr(C)]
#[derive(Debug)]
pub struct DatabaseHandleArgs<'a> {
    /// The path to the database directory.
    ///
    /// This must be a valid UTF-8 string.
    ///
    /// If this is empty, an error will be returned.
    pub dir: BorrowedBytes<'a>,

    /// Whether to enable `RootStore`.
    ///
    /// Note: Setting this feature will only track new revisions going forward
    /// and will not contain revisions from a prior database instance that didn't
    /// enable `root_store`.
    pub root_store: bool,

    /// The optional memory limit for the node cache in bytes.
    ///
    /// Set to `0` to leave this unset and rely on the default configured in
    /// `RevisionManagerConfig`.
    pub node_cache_memory_limit: usize,

    /// The size of the free list cache.
    ///
    /// Opening returns an error if this is zero.
    pub free_list_cache_size: usize,

    /// The maximum number of revisions to keep.
    ///
    /// Must be > `deferred_persistence_commit_count`.
    pub revisions: usize,

    /// The cache read strategy to use.
    ///
    /// This must be one of the following:
    ///
    /// - `0`: No cache.
    /// - `1`: Cache only branch reads.
    /// - `2`: Cache all reads.
    ///
    /// Opening returns an error if this is not one of the above values.
    pub strategy: u8,

    /// Whether to truncate the database file if it exists.
    pub truncate: bool,

    /// Whether to enable expensive metrics recording for this database handle.
    ///
    /// Expensive metrics are disabled by default.
    pub expensive_metrics: bool,

    /// The hashing mode to use for the database.
    ///
    /// This must match the compile-time feature:
    /// - [`NodeHashAlgorithm::Ethereum`] if the `ethhash` feature is enabled
    /// - [`NodeHashAlgorithm::MerkleDB`] if the `ethhash` feature is disabled
    ///
    /// Opening returns an error if this does not match the compile-time feature.
    pub node_hash_algorithm: NodeHashAlgorithm,

    /// The maximum number of unpersisted revisions that can exist at a given time.
    ///
    /// Note: `revisions` must be > `deferred_persistence_commit_count`.
    pub deferred_persistence_commit_count: u64,
}

impl DatabaseHandleArgs<'_> {
    fn as_rev_manager_config(&self) -> Result<RevisionManagerConfig, api::Error> {
        let cache_read_strategy = match self.strategy {
            0 => firewood::manager::CacheReadStrategy::WritesOnly,
            1 => firewood::manager::CacheReadStrategy::BranchReads,
            2 => firewood::manager::CacheReadStrategy::All,
            _ => return Err(invalid_data("invalid cache strategy")),
        };
        let free_list_cache_size = NonZeroUsize::new(self.free_list_cache_size)
            .ok_or_else(|| invalid_data("free list cache size should be non-zero"))?;
        let commit_count = NonZeroU64::new(self.deferred_persistence_commit_count)
            .ok_or(api::Error::ZeroCommitCount)?;

        let memory_limit = NonZeroUsize::new(self.node_cache_memory_limit);

        let config = {
            let builder = RevisionManagerConfig::builder()
                .max_revisions(self.revisions)
                .cache_read_strategy(cache_read_strategy)
                .free_list_cache_size(free_list_cache_size)
                .deferred_persistence_commit_count(commit_count);

            if let Some(memory_limit) = memory_limit {
                builder.node_cache_memory_limit(memory_limit).build()
            } else {
                builder.build()
            }
        };

        Ok(config)
    }
}

/// A handle to the database, returned by `fwd_open_db`.
///
/// These handles are passed to the other FFI functions.
///
#[derive(Debug)]
pub struct DatabaseHandle {
    /// The database
    db: Db,
    metrics_context: MetricsContext,
}

impl DatabaseHandle {
    /// Creates a new database handle from the given arguments.
    ///
    /// # Errors
    ///
    /// If the path is empty, or if the configuration is invalid, this will return an error.
    pub fn new(args: DatabaseHandleArgs<'_>) -> Result<Self, api::Error> {
        let metrics_context = MetricsContext::new(args.expensive_metrics);

        let cfg = DbConfig::builder()
            .node_hash_algorithm(args.node_hash_algorithm.into())
            .truncate(args.truncate)
            .manager(args.as_rev_manager_config()?)
            .root_store(args.root_store)
            .build();

        let path = args
            .dir
            .as_str()
            .map_err(|err| invalid_data(format!("database path contains invalid utf-8: {err}")))?;

        if path.is_empty() {
            return Err(invalid_data("database path cannot be empty"));
        }

        let db = Db::new(path, cfg)?;
        Ok(Self {
            db,
            metrics_context,
        })
    }

    /// Returns the current root hash of the database.
    ///
    /// # Errors
    ///
    /// Never errors.
    pub fn current_root_hash(&self) -> Option<HashKey> {
        self.db.root_hash()
    }

    /// Returns a value from the database for the given key from the latest root hash.
    ///
    /// # Errors
    ///
    /// An error is returned if there was an i/o error while reading the value.
    pub fn get_latest(&self, key: impl KeyType) -> Result<Option<Box<[u8]>>, api::Error> {
        let Some(root) = self.current_root_hash() else {
            return Err(api::Error::RevisionNotFound {
                provided: HashKey::default_root_hash(),
            });
        };

        self.db.revision(root)?.val(key)
    }

    /// Creates and commits a proposal with the given values.
    ///
    /// # Errors
    ///
    /// An error is returned if the proposal could not be created.
    pub fn create_batch<'a>(
        &self,
        values: impl AsRef<[BatchOp<'a>]> + 'a,
    ) -> Result<Option<HashKey>, api::Error> {
        let CreateProposalResult { handle } = self.create_proposal_handle(values.as_ref())?;
        handle.commit_proposal()
    }

    /// Returns an owned handle to the revision corresponding to the provided root hash.
    ///
    /// # Errors
    ///
    /// Returns an error if could not get the view from underlying database for the specified
    /// root hash, for example when the revision does not exist or an I/O error occurs while
    /// accessing the database.
    pub fn get_revision(&self, root: HashKey) -> Result<GetRevisionResult<'_>, api::Error> {
        let view = self.db.view(root.clone())?;
        let historical = match self.db.revision(root.clone()) {
            Ok(rev) => Some(rev),
            Err(api::Error::RevisionNotFound { .. }) => None,
            Err(err) => return Err(err),
        };
        Ok(GetRevisionResult {
            handle: RevisionHandle::new(view, historical, self.metrics_context, self),
            root_hash: root,
        })
    }

    /// Reconstructs a view on top of an existing historical node store.
    ///
    /// # Errors
    ///
    /// Returns an error if reconstruction fails.
    pub fn reconstruct_from_view<'db>(
        &'db self,
        parent: &Arc<NodeStore<Committed, FileBacked>>,
        batch: impl IntoBatchIter,
    ) -> Result<firewood::db::ReconstructedView<'db>, api::Error> {
        self.db.reconstruct_from_view(parent, batch)
    }

    pub(crate) fn view(&self, root: HashKey) -> Result<ArcDynDbView, api::Error> {
        self.db.view(root)
    }

    pub(crate) fn merge_key_value_range(
        &self,
        first_key: Option<impl KeyType>,
        last_key: Option<impl KeyType>,
        key_values: impl IntoIterator<Item: api::KeyValuePair>,
    ) -> Result<CreateProposalResult<'_>, api::Error> {
        CreateProposalResult::new(self, || {
            self.db
                .merge_key_value_range(first_key, last_key, key_values)
        })
    }

    /// Create a Change Proof between two revisions specified by the start and end hash.
    ///
    /// Delegates to [`firewood::db::Db::change_proof`].
    pub(crate) fn change_proof(
        &self,
        start_hash: HashKey,
        end_hash: HashKey,
        start_key: Option<&[u8]>,
        end_key: Option<&[u8]>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenChangeProof, api::Error> {
        self.db
            .change_proof(start_hash, end_hash, start_key, end_key, limit)
    }

    /// Verify a change proof and create a proposal from it.
    ///
    /// Performs structural validation, applies batch ops to the latest
    /// revision, and verifies the root hash against `end_root`. The proof
    /// is borrowed, not consumed.
    ///
    /// # Errors
    ///
    /// Returns an error if structural validation fails or the root hash
    /// doesn't match `end_root`.
    pub fn verify_change_proof(
        &self,
        proof: &FrozenChangeProof,
        end_root: HashKey,
        start_key: Option<&[u8]>,
        end_key: Option<&[u8]>,
        max_length: Option<NonZeroUsize>,
    ) -> Result<CreateProposalResult<'_>, api::Error> {
        CreateProposalResult::new(self, || {
            self.db
                .verify_change_proof(proof, end_root, start_key, end_key, max_length)
        })
    }

    /// Dumps the Trie structure of the latest revision to a DOT (Graphviz) format string.
    ///
    /// # Errors
    ///
    /// An error is returned if there was an i/o error while dumping the trie.
    pub fn dump_to_string(&self) -> Result<String, api::Error> {
        self.db.dump_to_string().map_err(api::Error::from)
    }

    /// Closes the database gracefully.
    ///
    /// # Errors
    ///
    /// An error is returned if the persistence background thread panicked or
    /// errored during execution.
    pub fn close(self) -> Result<(), api::Error> {
        self.db.close()
    }
}

impl<'db> CView<'db> for &'db crate::DatabaseHandle {
    fn handle(&self) -> &'db crate::DatabaseHandle {
        self
    }

    fn create_proposal(
        self,
        values: impl IntoBatchIter,
    ) -> Result<firewood::db::Proposal<'db>, api::Error> {
        self.db.propose(values)
    }
}

impl crate::MetricsContextExt for DatabaseHandle {
    fn metrics_context(&self) -> Option<MetricsContext> {
        Some(self.metrics_context)
    }
}

fn invalid_data(error: impl Into<Box<dyn std::error::Error + Send + Sync>>) -> api::Error {
    api::Error::IO(std::io::Error::new(std::io::ErrorKind::InvalidData, error))
}
