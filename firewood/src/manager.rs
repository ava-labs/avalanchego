// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::cast_precision_loss,
    reason = "Found 2 occurrences after enabling the lint."
)]
#![expect(
    clippy::default_trait_access,
    reason = "Found 3 occurrences after enabling the lint."
)]

use std::collections::{HashMap, VecDeque};
use std::num::NonZero;
use std::path::PathBuf;
use std::sync::{Arc, Mutex, RwLock};

use firewood_storage::logger::{trace, warn};
use metrics::gauge;
use typed_builder::TypedBuilder;

use crate::merkle::Merkle;
use crate::v2::api::{ArcDynDbView, HashKey, OptionalHashKeyExt};

pub use firewood_storage::CacheReadStrategy;
use firewood_storage::{
    Committed, FileBacked, FileIoError, HashedNodeReader, ImmutableProposal, NodeStore, TrieHash,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, TypedBuilder)]
/// Revision manager configuratoin
pub struct RevisionManagerConfig {
    /// The number of historical revisions to keep in memory.
    #[builder(default = 128)]
    max_revisions: usize,

    /// The size of the node cache
    #[builder(default_code = "NonZero::new(1500000).expect(\"non-zero\")")]
    node_cache_size: NonZero<usize>,

    #[builder(default_code = "NonZero::new(40000).expect(\"non-zero\")")]
    free_list_cache_size: NonZero<usize>,

    #[builder(default = CacheReadStrategy::WritesOnly)]
    cache_read_strategy: CacheReadStrategy,
}

#[derive(Clone, Debug, TypedBuilder)]
/// Configuration manager that contains both truncate and revision manager config
pub struct ConfigManager {
    /// Whether to create the DB if it doesn't exist.
    #[builder(default = true)]
    pub create: bool,
    /// Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    /// existing contents will be lost.
    #[builder(default = false)]
    pub truncate: bool,
    /// Revision manager configuration.
    #[builder(default = RevisionManagerConfig::builder().build())]
    pub manager: RevisionManagerConfig,
}

type CommittedRevision = Arc<NodeStore<Committed, FileBacked>>;
type ProposedRevision = Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>>;

#[derive(Debug)]
pub(crate) struct RevisionManager {
    /// Maximum number of revisions to keep on disk
    max_revisions: usize,

    /// The list of revisions that are on disk; these point to the different roots
    /// stored in the filebacked storage.
    historical: RwLock<VecDeque<CommittedRevision>>,
    proposals: Mutex<Vec<ProposedRevision>>,
    // committing_proposals: VecDeque<Arc<ProposedImmutable>>,
    by_hash: RwLock<HashMap<TrieHash, CommittedRevision>>,
}

#[derive(Debug, thiserror::Error)]
pub(crate) enum RevisionManagerError {
    #[error("Revision for {provided:?} not found")]
    RevisionNotFound { provided: HashKey },
    #[error(
        "The proposal cannot be committed since it is not a direct child of the most recent commit. Proposal parent: {provided:?}, current root: {expected:?}"
    )]
    NotLatest {
        provided: Option<HashKey>,
        expected: Option<HashKey>,
    },
    #[error("An IO error occurred during the commit")]
    FileIoError(#[from] FileIoError),
}

impl RevisionManager {
    pub fn new(filename: PathBuf, config: ConfigManager) -> Result<Self, FileIoError> {
        let fb = FileBacked::new(
            filename,
            config.manager.node_cache_size,
            config.manager.free_list_cache_size,
            config.truncate,
            config.create,
            config.manager.cache_read_strategy,
        )?;

        // Acquire an advisory lock on the database file to prevent multiple processes
        // from opening the same database simultaneously
        fb.lock()?;

        let storage = Arc::new(fb);
        let nodestore = Arc::new(NodeStore::open(storage.clone())?);
        let manager = Self {
            max_revisions: config.manager.max_revisions,
            historical: RwLock::new(VecDeque::from([nodestore.clone()])),
            by_hash: RwLock::new(Default::default()),
            proposals: Mutex::new(Default::default()),
            // committing_proposals: Default::default(),
        };

        if let Some(hash) = nodestore.root_hash().or_default_root_hash() {
            manager
                .by_hash
                .write()
                .expect("poisoned lock")
                .insert(hash, nodestore.clone());
        }

        if config.truncate {
            nodestore.flush_header_with_padding()?;
        }

        Ok(manager)
    }

    pub fn all_hashes(&self) -> Vec<TrieHash> {
        self.historical
            .read()
            .expect("poisoned lock")
            .iter()
            .filter_map(|r| r.root_hash().or_default_root_hash())
            .chain(
                self.proposals
                    .lock()
                    .expect("poisoned lock")
                    .iter()
                    .filter_map(|p| p.root_hash().or_default_root_hash()),
            )
            .collect()
    }

    /// Commit a proposal
    /// To commit a proposal involves a few steps:
    /// 1. Commit check.
    ///    The proposal's parent must be the last committed revision, otherwise the commit fails.
    /// 2. Persist delete list.
    ///    The list of all nodes that were to be deleted for this proposal must be fully flushed to disk.
    ///    The address of the root node and the root hash is also persisted.
    ///    Note that this is *not* a write ahead log.
    ///    It only contains the address of the nodes that are deleted, which should be very small.
    /// 3. Revision reaping. If more than the maximum number of revisions are kept in memory, the
    ///    oldest revision is reaped.
    /// 4. Persist to disk. This includes flushing everything to disk.
    /// 5. Set last committed revision.
    ///    Set last committed revision in memory.
    /// 6. Proposal Cleanup.
    ///    Any other proposals that have this proposal as a parent should be reparented to the committed version.
    #[fastrace::trace(short_name = true)]
    #[crate::metrics("firewood.proposal.commit", "proposal commit to storage")]
    pub fn commit(&self, proposal: ProposedRevision) -> Result<(), RevisionManagerError> {
        // 1. Commit check
        let current_revision = self.current_revision();
        if !proposal.parent_hash_is(current_revision.root_hash()) {
            return Err(RevisionManagerError::NotLatest {
                provided: proposal.root_hash(),
                expected: current_revision.root_hash(),
            });
        }

        let mut committed = proposal.as_committed(&current_revision);

        // 2. Persist delete list for this committed revision to disk for recovery

        // 3 Take the deleted entries from the oldest revision and mark them as free for this revision
        // If you crash after freeing some of these, then the free list will point to nodes that are not actually free.
        // TODO: Handle the case where we get something off the free list that is not free
        while self.historical.read().expect("poisoned lock").len() >= self.max_revisions {
            let oldest = self
                .historical
                .write()
                .expect("poisoned lock")
                .pop_front()
                .expect("must be present");
            if let Some(oldest_hash) = oldest.root_hash().or_default_root_hash() {
                self.by_hash
                    .write()
                    .expect("poisoned lock")
                    .remove(&oldest_hash);
            }

            // This `try_unwrap` is safe because nobody else will call `try_unwrap` on this Arc
            // in a different thread, so we don't have to worry about the race condition where
            // the Arc we get back is not usable as indicated in the docs for `try_unwrap`.
            // This guarantee is there because we have a `&mut self` reference to the manager, so
            // the compiler guarantees we are the only one using this manager.
            match Arc::try_unwrap(oldest) {
                Ok(oldest) => oldest.reap_deleted(&mut committed)?,
                Err(original) => {
                    warn!("Oldest revision could not be reaped; still referenced");
                    self.historical
                        .write()
                        .expect("poisoned lock")
                        .push_front(original);
                    break;
                }
            }
            gauge!("firewood.active_revisions")
                .set(self.historical.read().expect("poisoned lock").len() as f64);
            gauge!("firewood.max_revisions").set(self.max_revisions as f64);
        }

        // 4. Persist to disk.
        // TODO: We can probably do this in another thread, but it requires that
        // we move the header out of NodeStore, which is in a future PR.
        committed.persist()?;

        // 5. Set last committed revision
        let committed: CommittedRevision = committed.into();
        self.historical
            .write()
            .expect("poisoned lock")
            .push_back(committed.clone());
        if let Some(hash) = committed.root_hash().or_default_root_hash() {
            self.by_hash
                .write()
                .expect("poisoned lock")
                .insert(hash, committed.clone());
        }

        // 6. Proposal Cleanup
        // Free proposal that is being committed as well as any proposals no longer
        // referenced by anyone else.
        self.proposals
            .lock()
            .expect("poisoned lock")
            .retain(|p| !Arc::ptr_eq(&proposal, p) && Arc::strong_count(p) > 1);

        // then reparent any proposals that have this proposal as a parent
        for p in &*self.proposals.lock().expect("poisoned lock") {
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
}

impl RevisionManager {
    pub fn add_proposal(&self, proposal: ProposedRevision) {
        self.proposals.lock().expect("poisoned lock").push(proposal);
    }

    pub fn view(&self, root_hash: HashKey) -> Result<ArcDynDbView, RevisionManagerError> {
        // First try to find it in committed revisions
        if let Ok(committed) = self.revision(root_hash.clone()) {
            return Ok(committed);
        }

        // If not found in committed revisions, try proposals
        let proposal = self
            .proposals
            .lock()
            .expect("poisoned lock")
            .iter()
            .find(|p| p.root_hash().as_ref() == Some(&root_hash))
            .cloned()
            .ok_or(RevisionManagerError::RevisionNotFound {
                provided: root_hash,
            })?;

        Ok(proposal)
    }

    pub fn revision(&self, root_hash: HashKey) -> Result<CommittedRevision, RevisionManagerError> {
        self.by_hash
            .read()
            .expect("poisoned lock")
            .get(&root_hash)
            .cloned()
            .ok_or(RevisionManagerError::RevisionNotFound {
                provided: root_hash,
            })
    }

    pub fn root_hash(&self) -> Result<Option<HashKey>, RevisionManagerError> {
        Ok(self.current_revision().root_hash())
    }

    pub fn current_revision(&self) -> CommittedRevision {
        self.historical
            .read()
            .expect("poisoned lock")
            .back()
            .expect("there is always one revision")
            .clone()
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use tempfile::NamedTempFile;

    #[test]
    fn test_file_advisory_lock() {
        // Create a temporary file for testing
        let temp_file = NamedTempFile::new().unwrap();
        let db_path = temp_file.path().to_path_buf();

        let config = ConfigManager::builder()
            .create(true)
            .truncate(false)
            .build();

        // First database instance should open successfully
        let first_manager = RevisionManager::new(db_path.clone(), config.clone());
        assert!(
            first_manager.is_ok(),
            "First database should open successfully"
        );

        // Second database instance should fail to open due to file locking
        let second_manager = RevisionManager::new(db_path.clone(), config.clone());
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
        let third_manager = RevisionManager::new(db_path, config);
        assert!(
            third_manager.is_ok(),
            "Database should open after first instance is dropped"
        );
    }
}
