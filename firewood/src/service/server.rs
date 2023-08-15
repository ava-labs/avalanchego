// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{
    collections::HashMap,
    path::PathBuf,
    sync::atomic::{AtomicU32, Ordering},
};
use tokio::sync::mpsc::Receiver;

use crate::db::{Db, DbConfig, DbError};

use super::{Request, RevId};

macro_rules! get_rev {
    ($rev: ident, $handle: ident, $out: expr) => {
        match $rev.get(&$handle) {
            None => {
                let _ = $out.send(Err(DbError::InvalidParams));
                continue;
            }
            Some(x) => x,
        }
    };
}
#[derive(Copy, Debug, Clone)]
pub struct FirewoodService {}

impl FirewoodService {
    pub fn new(mut receiver: Receiver<Request>, owned_path: PathBuf, cfg: DbConfig) -> Self {
        let db = Db::new(owned_path, &cfg).unwrap();
        let mut revs = HashMap::new();
        let lastid = AtomicU32::new(0);
        loop {
            let msg = match receiver.blocking_recv() {
                Some(msg) => msg,
                None => break,
            };
            match msg {
                Request::NewRevision {
                    root_hash,
                    respond_to,
                } => {
                    let id: RevId = lastid.fetch_add(1, Ordering::Relaxed);
                    let msg = match db.get_revision(&root_hash) {
                        Some(rev) => {
                            revs.insert(id, rev);
                            Some(id)
                        }
                        None => None,
                    };
                    let _ = respond_to.send(msg);
                }

                Request::RevRequest(req) => match req {
                    super::RevRequest::Get {
                        handle,
                        key,
                        respond_to,
                    } => {
                        let rev = get_rev!(revs, handle, respond_to);
                        let msg = rev.kv_get(key);
                        let _ = respond_to.send(msg.map_or(Err(DbError::KeyNotFound), Ok));
                    }
                    #[cfg(feature = "proof")]
                    super::RevRequest::Prove {
                        handle,
                        key,
                        respond_to,
                    } => {
                        let msg = revs
                            .get(&handle)
                            .map_or(Err(MerkleError::UnsetInternal), |r| r.prove(key));
                        let _ = respond_to.send(msg);
                    }
                    super::RevRequest::RootHash { handle, respond_to } => {
                        let rev = get_rev!(revs, handle, respond_to);
                        let msg = rev.kv_root_hash();
                        let _ = respond_to.send(msg);
                    }
                    super::RevRequest::Drop { handle } => {
                        revs.remove(&handle);
                    }
                },
            }
        }
        FirewoodService {}
    }
}
