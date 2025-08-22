// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::{
    db::{Db, DbConfig},
    manager::RevisionManagerConfig,
    v2::api::{self, ArcDynDbView, Db as _, DbView, HashKey, Proposal as _},
};
use metrics::counter;

use crate::{BorrowedBytes, DatabaseHandle, KeyValuePair};

/// Arguments for creating or opening a database. These are passed to [`fwd_open_db`]
///
/// [`fwd_open_db`]: crate::fwd_open_db
#[repr(C)]
#[derive(Debug)]
pub struct DatabaseHandleArgs<'a> {
    /// The path to the database file.
    ///
    /// This must be a valid UTF-8 string, even on Windows.
    ///
    /// If this is empty, an error will be returned.
    pub path: BorrowedBytes<'a>,

    /// The size of the node cache.
    ///
    /// Opening returns an error if this is zero.
    pub cache_size: usize,

    /// The size of the free list cache.
    ///
    /// Opening returns an error if this is zero.
    pub free_list_cache_size: usize,

    /// The maximum number of revisions to keep.
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
}

impl DatabaseHandleArgs<'_> {
    fn as_rev_manager_config(&self) -> Result<RevisionManagerConfig, api::Error> {
        let cache_read_strategy = match self.strategy {
            0 => firewood::manager::CacheReadStrategy::WritesOnly,
            1 => firewood::manager::CacheReadStrategy::BranchReads,
            2 => firewood::manager::CacheReadStrategy::All,
            _ => return Err(invalid_data("invalid cache strategy")),
        };
        let config = RevisionManagerConfig::builder()
            .node_cache_size(
                self.cache_size
                    .try_into()
                    .map_err(|_| invalid_data("cache size should be non-zero"))?,
            )
            .max_revisions(self.revisions)
            .cache_read_strategy(cache_read_strategy)
            .free_list_cache_size(
                self.free_list_cache_size
                    .try_into()
                    .map_err(|_| invalid_data("free list cache size should be non-zero"))?,
            )
            .build();
        Ok(config)
    }
}

impl DatabaseHandle<'_> {
    /// Creates a new database handle from the given arguments.
    ///
    /// # Errors
    ///
    /// If the path is empty, or if the configuration is invalid, this will return an error.
    pub fn new(args: DatabaseHandleArgs<'_>) -> Result<Self, api::Error> {
        let cfg = DbConfig::builder()
            .truncate(args.truncate)
            .manager(args.as_rev_manager_config()?)
            .build();

        let path = args
            .path
            .as_str()
            .map_err(|err| invalid_data(format!("database path contains invalid utf-8: {err}")))?;

        if path.is_empty() {
            return Err(invalid_data("database path cannot be empty"));
        }

        Db::new(path, cfg).map(Self::from)
    }

    /// Returns the current root hash of the database.
    ///
    /// # Errors
    ///
    /// An error is returned if there was an i/o error while reading the root hash.
    pub fn current_root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        self.db.root_hash()
    }

    /// Creates a proposal with the given values and returns the proposal and the start time.
    ///
    /// # Errors
    ///
    /// An error is returned if the proposal could not be created.
    pub fn create_batch<'kvp>(
        &self,
        values: (impl AsRef<[KeyValuePair<'kvp>]> + 'kvp),
    ) -> Result<Option<HashKey>, api::Error> {
        let start = coarsetime::Instant::now();

        let proposal = self.db.propose(values.as_ref())?;

        let propose_time = start.elapsed().as_millis();
        counter!("firewood.ffi.propose_ms").increment(propose_time);

        let hash_val = proposal.root_hash()?;

        proposal.commit()?;

        let propose_plus_commit_time = start.elapsed().as_millis();
        counter!("firewood.ffi.batch_ms").increment(propose_plus_commit_time);
        counter!("firewood.ffi.commit_ms")
            .increment(propose_plus_commit_time.saturating_sub(propose_time));
        counter!("firewood.ffi.batch").increment(1);

        Ok(hash_val)
    }

    /// Commits a proposal with the given ID.
    ///
    /// # Errors
    ///
    /// An error is returned if the proposal could not be committed, or if the proposal ID is invalid.
    pub fn commit_proposal(&self, proposal_id: u32) -> Result<Option<HashKey>, String> {
        let proposal = self
            .proposals
            .write()
            .map_err(|_| "proposal lock is poisoned")?
            .remove(&proposal_id)
            .ok_or("proposal not found")?;

        // Get the proposal hash and cache the view. We never cache an empty proposal.
        let proposal_hash = proposal.root_hash().map_err(|e| e.to_string())?;

        if let Some(ref hash_key) = proposal_hash {
            _ = self.get_root(hash_key.clone());
        }

        // Commit the proposal
        let result = proposal.commit().map_err(|e| e.to_string());

        // Clear the cache, which will force readers after this point to find the committed root hash
        self.clear_cached_view();

        result.map(|()| proposal_hash)
    }

    pub(crate) fn get_root(&self, root: HashKey) -> Result<ArcDynDbView, api::Error> {
        let mut cache_miss = false;
        let view = self.cached_view.get_or_try_insert_with(root, |key| {
            cache_miss = true;
            self.db.view(HashKey::clone(key))
        })?;

        if cache_miss {
            counter!("firewood.ffi.cached_view.miss").increment(1);
        } else {
            counter!("firewood.ffi.cached_view.hit").increment(1);
        }

        Ok(view)
    }

    pub(crate) fn clear_cached_view(&self) {
        self.cached_view.clear();
    }
}

fn invalid_data(error: impl Into<Box<dyn std::error::Error + Send + Sync>>) -> api::Error {
    api::Error::IO(std::io::Error::new(std::io::ErrorKind::InvalidData, error))
}
