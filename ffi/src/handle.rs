// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::{
    db::{Db, DbConfig},
    manager::RevisionManagerConfig,
    v2::api::{self, ArcDynDbView, HashKey},
};
use metrics::counter;

use crate::{BorrowedBytes, DatabaseHandle};

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
