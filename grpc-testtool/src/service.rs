// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::db::{Db, DbConfig};
use firewood::storage::WalConfig;
use firewood::v2::{api::Db as _, api::Error};

use std::path::Path;
use std::{
    collections::HashMap,
    ops::Deref,
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    },
};
use tokio::sync::Mutex;
use tonic::Status;

pub mod database;
pub mod db;
pub mod process;

trait IntoStatusResultExt<T> {
    fn into_status_result(self) -> Result<T, Status>;
}

impl<T> IntoStatusResultExt<T> for Result<T, Error> {
    // We map errors from bad arguments into Status::invalid_argument; all other errors are Status::internal errors
    fn into_status_result(self) -> Result<T, Status> {
        self.map_err(|err| match err {
            Error::IncorrectRootHash { .. } | Error::HashNotFound { .. } | Error::RangeTooSmall => {
                Status::invalid_argument(err.to_string())
            }
            Error::IO { .. } | Error::InternalError { .. } | Error::InvalidProposal => {
                Status::internal(err.to_string())
            }
            _ => Status::internal(err.to_string()),
        })
    }
}

#[derive(Debug)]
pub struct Database {
    db: Db,
    iterators: Arc<Mutex<Iterators>>,
}

impl Database {
    pub async fn new<P: AsRef<Path>>(path: P, history_length: u32) -> Result<Self, Error> {
        // try to create the parents for this directory, but it's okay if it fails; it will get caught in Db::new
        std::fs::create_dir_all(&path).ok();
        // TODO: truncate should be false
        // see https://github.com/ava-labs/firewood/issues/418
        let cfg = DbConfig::builder()
            .wal(WalConfig::builder().max_revisions(history_length).build())
            .truncate(true)
            .build();

        let db = Db::new(path, &cfg).await?;

        Ok(Self {
            db,
            iterators: Default::default(),
        })
    }
}

impl Deref for Database {
    type Target = Db;

    fn deref(&self) -> &Self::Target {
        &self.db
    }
}

impl Database {
    async fn latest(&self) -> Result<Arc<<Db as firewood::v2::api::Db>::Historical>, Error> {
        let root_hash = self.root_hash().await?;
        self.revision(root_hash).await
    }
}

// TODO: implement Iterator
#[derive(Debug)]
struct Iter;

#[derive(Default, Debug)]
struct Iterators {
    map: HashMap<u64, Iter>,
    next_id: AtomicU64,
}

impl Iterators {
    fn insert(&mut self, iter: Iter) -> u64 {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        self.map.insert(id, iter);
        id
    }

    fn _get(&self, id: u64) -> Option<&Iter> {
        self.map.get(&id)
    }

    fn remove(&mut self, id: u64) {
        self.map.remove(&id);
    }
}

#[derive(Debug)]
pub struct ProcessServer;
