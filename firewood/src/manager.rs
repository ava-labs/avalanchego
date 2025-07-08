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
#[cfg(feature = "ethhash")]
use std::sync::OnceLock;
use std::sync::{Arc, Mutex, RwLock};

use firewood_storage::logger::{trace, warn};
use metrics::gauge;
use typed_builder::TypedBuilder;

use crate::merkle::Merkle;
use crate::v2::api::HashKey;

pub use firewood_storage::CacheReadStrategy;
use firewood_storage::{
    Committed, FileBacked, FileIoError, HashedNodeReader, ImmutableProposal, NodeStore, TrieHash,
};

#[derive(Clone, Debug, TypedBuilder)]
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

    #[cfg(feature = "ethhash")]
    empty_hash: OnceLock<TrieHash>,
}

#[derive(Debug, thiserror::Error)]
pub(crate) enum RevisionManagerError {
    #[error(
        "The proposal cannot be committed since it is not a direct child of the most recent commit"
    )]
    NotLatest,
    #[error("Revision not found")]
    RevisionNotFound,
    #[error("An IO error occurred during the commit")]
    FileIoError(#[from] FileIoError),
}

impl RevisionManager {
    pub fn new(
        filename: PathBuf,
        truncate: bool,
        config: RevisionManagerConfig,
    ) -> Result<Self, FileIoError> {
        let storage = Arc::new(FileBacked::new(
            filename,
            config.node_cache_size,
            config.free_list_cache_size,
            truncate,
            config.cache_read_strategy,
        )?);
        let nodestore = if truncate {
            Arc::new(NodeStore::new_empty_committed(storage.clone())?)
        } else {
            Arc::new(NodeStore::open(storage.clone())?)
        };
        let manager = Self {
            max_revisions: config.max_revisions,
            historical: RwLock::new(VecDeque::from([nodestore.clone()])),
            by_hash: RwLock::new(Default::default()),
            proposals: Mutex::new(Default::default()),
            // committing_proposals: Default::default(),
            #[cfg(feature = "ethhash")]
            empty_hash: OnceLock::new(),
        };

        if let Some(hash) = nodestore.root_hash().or_else(|| manager.empty_trie_hash()) {
            manager
                .by_hash
                .write()
                .expect("poisoned lock")
                .insert(hash, nodestore.clone());
        }

        if truncate {
            nodestore.flush_header_with_padding()?;
        }

        Ok(manager)
    }

    pub fn all_hashes(&self) -> Vec<TrieHash> {
        self.historical
            .read()
            .expect("poisoned lock")
            .iter()
            .filter_map(|r| r.root_hash().or_else(|| self.empty_trie_hash()))
            .chain(
                self.proposals
                    .lock()
                    .expect("poisoned lock")
                    .iter()
                    .filter_map(|p| p.root_hash().or_else(|| self.empty_trie_hash())),
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
    /// 4. Set last committed revision.
    ///    Set last committed revision in memory.
    ///    Another commit can start after this but before the node flush is completed.
    /// 5. Free list flush.
    ///    Persist/write the free list header.
    ///    The free list is flushed first to prevent future allocations from using the space allocated to this proposal.
    ///    This should be done in a single write since the free list headers are small, and must be persisted to disk before starting the next step.
    /// 6. Node flush.
    ///    Persist/write all the nodes to disk.
    ///    Note that since these are all freshly allocated nodes, they will never be referred to by any prior commit.
    ///    After flushing all nodes, the file should be flushed to disk (fsync) before performing the next step.
    /// 7. Root move.
    ///    The root address on disk must be updated.
    ///    This write can be delayed, but would mean that recovery will not roll forward to this revision.
    /// 8. Proposal Cleanup.
    ///    Any other proposals that have this proposal as a parent should be reparented to the committed version.
    #[fastrace::trace(short_name = true)]
    #[crate::metrics("firewood.proposal.commit", "proposal commit to storage")]
    pub fn commit(&self, proposal: ProposedRevision) -> Result<(), RevisionManagerError> {
        // 1. Commit check
        let current_revision = self.current_revision();
        if !proposal.parent_hash_is(current_revision.root_hash()) {
            return Err(RevisionManagerError::NotLatest);
        }

        let mut committed = proposal.as_committed();

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
            if let Some(oldest_hash) = oldest.root_hash().or_else(|| self.empty_trie_hash()) {
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

        // 4. Set last committed revision
        let committed: CommittedRevision = committed.into();
        self.historical
            .write()
            .expect("poisoned lock")
            .push_back(committed.clone());
        if let Some(hash) = committed.root_hash().or_else(|| self.empty_trie_hash()) {
            self.by_hash
                .write()
                .expect("poisoned lock")
                .insert(hash, committed.clone());
        }
        // TODO: We could allow other commits to start here using the pending list

        // 5. Free list flush, which will prevent allocating on top of the nodes we are about to write
        proposal.flush_freelist()?;

        // 6. Node flush
        proposal.flush_nodes()?;

        // 7. Root move
        proposal.flush_header()?;

        // 8. Proposal Cleanup
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
            trace!("{}", merkle.dump().expect("failed to dump merkle"));
        }

        Ok(())
    }
}

impl RevisionManager {
    pub fn add_proposal(&self, proposal: ProposedRevision) {
        self.proposals.lock().expect("poisoned lock").push(proposal);
    }

    pub fn view(
        &self,
        root_hash: HashKey,
    ) -> Result<Box<dyn crate::db::DbViewSyncBytes>, RevisionManagerError> {
        // First try to find it in committed revisions
        if let Ok(committed) = self.revision(root_hash.clone()) {
            return Ok(Box::new(committed));
        }

        // If not found in committed revisions, try proposals
        let proposal = self
            .proposals
            .lock()
            .expect("poisoned lock")
            .iter()
            .find(|p| p.root_hash().as_ref() == Some(&root_hash))
            .cloned()
            .ok_or(RevisionManagerError::RevisionNotFound)?;

        Ok(Box::new(proposal))
    }

    pub fn revision(&self, root_hash: HashKey) -> Result<CommittedRevision, RevisionManagerError> {
        self.by_hash
            .read()
            .expect("poisoned lock")
            .get(&root_hash)
            .cloned()
            .ok_or(RevisionManagerError::RevisionNotFound)
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
    #[cfg(not(feature = "ethhash"))]
    #[inline]
    pub const fn empty_trie_hash(&self) -> Option<TrieHash> {
        None
    }

    #[cfg(feature = "ethhash")]
    #[inline]
    pub fn empty_trie_hash(&self) -> Option<TrieHash> {
        Some(
            self.empty_hash
                .get_or_init(firewood_storage::empty_trie_hash)
                .clone(),
        )
    }
}

#[cfg(test)]
mod tests {
    // TODO
}
