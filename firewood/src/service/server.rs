use std::{
    collections::HashMap,
    path::PathBuf,
    sync::atomic::{AtomicU32, Ordering},
};
use tokio::sync::mpsc::Receiver;

use crate::db::{DBConfig, DBError, DB};

use super::{BatchId, BatchRequest, Request};

macro_rules! handle_req {
    ($in: expr, $out: expr) => {
        let _ = $in.send($out);
    };
}
macro_rules! get_batch {
    ($batches: ident, $handle: ident, $out: expr) => {
        match $batches.remove(&$handle) {
            None => {
                let _ = $out.send(Err(DBError::InvalidParams));
                continue;
            }
            Some(x) => x,
        }
    };
}

#[derive(Copy, Debug, Clone)]
pub struct FirewoodService {}

impl FirewoodService {
    pub fn new(mut receiver: Receiver<Request>, owned_path: PathBuf, cfg: DBConfig) -> Self {
        let db = DB::new(&owned_path.to_string_lossy(), &cfg).unwrap();
        let mut batches = HashMap::<BatchId, crate::db::WriteBatch>::new();
        let lastid = AtomicU32::new(0);
        loop {
            let msg = match receiver.blocking_recv() {
                Some(msg) => msg,
                None => break,
            };
            match msg {
                Request::NewBatch { respond_to } => {
                    let id: BatchId = lastid.fetch_add(1, Ordering::Relaxed);
                    batches.insert(id, db.new_writebatch());
                    let _ = respond_to.send(id);
                }
                Request::RootHash { respond_to } => {
                    handle_req!(respond_to, db.root_hash());
                }
                Request::Get { key, respond_to } => {
                    handle_req!(respond_to, db.kv_get(key));
                }
                Request::BatchRequest(req) => match req {
                    BatchRequest::Commit { handle, respond_to } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        batch.commit();
                        let _ = respond_to.send(Ok(()));
                    }
                    BatchRequest::KvInsert {
                        handle,
                        key,
                        val,
                        respond_to,
                    } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        let resp = match batch.kv_insert(key, val) {
                            Ok(v) => {
                                batches.insert(handle, v);
                                Ok(())
                            }
                            Err(e) => Err(e),
                        };
                        respond_to.send(resp).unwrap();
                    }
                    BatchRequest::KvRemove {
                        handle,
                        key,
                        respond_to,
                    } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        let resp = match batch.kv_remove(key) {
                            Ok(v) => {
                                batches.insert(handle, v.0);
                                Ok(v.1)
                            }
                            Err(e) => Err(e),
                        };
                        respond_to.send(resp).unwrap();
                    }
                    BatchRequest::SetBalance {
                        handle,
                        key,
                        balance,
                        respond_to,
                    } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        let resp = match batch.set_balance(key.as_ref(), balance) {
                            Ok(v) => {
                                batches.insert(handle, v);
                                Ok(())
                            }
                            Err(e) => Err(e),
                        };
                        respond_to.send(resp).unwrap();
                    }
                    BatchRequest::SetCode {
                        handle,
                        key,
                        code,
                        respond_to,
                    } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        let resp = match batch.set_code(key.as_ref(), code.as_ref()) {
                            Ok(v) => {
                                batches.insert(handle, v);
                                Ok(())
                            }
                            Err(e) => Err(e),
                        };
                        respond_to.send(resp).unwrap();
                    }
                    BatchRequest::SetNonce {
                        handle,
                        key,
                        nonce,
                        respond_to,
                    } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        let resp = match batch.set_nonce(key.as_ref(), nonce) {
                            Ok(v) => {
                                batches.insert(handle, v);
                                Ok(())
                            }
                            Err(e) => Err(e),
                        };
                        respond_to.send(resp).unwrap();
                    }
                    BatchRequest::SetState {
                        handle,
                        key,
                        sub_key,
                        state,
                        respond_to,
                    } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        let resp = match batch.set_state(key.as_ref(), sub_key.as_ref(), state) {
                            Ok(v) => {
                                batches.insert(handle, v);
                                Ok(())
                            }
                            Err(e) => Err(e),
                        };
                        respond_to.send(resp).unwrap();
                    }
                    BatchRequest::CreateAccount {
                        handle,
                        key,
                        respond_to,
                    } => {
                        let batch = get_batch!(batches, handle, respond_to);
                        let resp = match batch.create_account(key.as_ref()) {
                            Ok(v) => {
                                batches.insert(handle, v);
                                Ok(())
                            }
                            Err(e) => Err(e),
                        };
                        respond_to.send(resp).unwrap();
                    }
                    BatchRequest::NoRootHash { handle, respond_to } => {
                        // TODO: there's no way to report an error back to the caller here
                        if let Some(batch) = batches.remove(&handle) {
                            batches.insert(handle, batch.no_root_hash());
                        } else {
                            // log!("Invalid handle {handle} passed to no_root_hash");
                        }
                        respond_to.send(()).unwrap();
                    }
                },
            }
        }
        FirewoodService {}
    }
}
