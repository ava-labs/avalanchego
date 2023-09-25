// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

/// Client side connection structure
///
/// A connection is used to send messages to the firewood thread.
use std::fmt::Debug;
use std::mem::take;
use std::thread::JoinHandle;
use std::{path::Path, thread};

use tokio::sync::{mpsc, oneshot};

use crate::api::Revision;

use crate::merkle::MerkleError;
use crate::v2::api::Proof;
use crate::{
    db::{DbConfig, DbError},
    merkle::TrieHash,
};
use async_trait::async_trait;

use super::server::FirewoodService;
use super::{Request, RevRequest, RevisionHandle};

/// A `Connection` represents a connection to the thread running firewood
/// The type specified is how you want to refer to your key values; this is
/// something like `Vec<u8>` or `&[u8]`
#[derive(Debug)]
pub struct Connection<N: Send> {
    sender: Option<mpsc::Sender<Request<N>>>,
    handle: Option<JoinHandle<FirewoodService>>,
}

impl<N: Send> Drop for Connection<N> {
    fn drop(&mut self) {
        drop(take(&mut self.sender));
        take(&mut self.handle)
            .unwrap()
            .join()
            .expect("Couldn't join with the firewood thread");
    }
}

impl<N: Send + 'static> Connection<N> {
    #[allow(dead_code)]
    fn new<P: AsRef<Path>>(path: P, cfg: DbConfig) -> Self {
        let (sender, receiver) = mpsc::channel(1_000)
            as (
                tokio::sync::mpsc::Sender<Request<N>>,
                tokio::sync::mpsc::Receiver<Request<N>>,
            );
        let owned_path = path.as_ref().to_path_buf();
        let handle = thread::Builder::new()
            .name("firewood-receiver".to_owned())
            .spawn(move || FirewoodService::new(receiver, owned_path, cfg))
            .expect("thread creation failed");
        Self {
            sender: Some(sender),
            handle: Some(handle),
        }
    }
}

impl<N: Send> super::RevisionHandle<N> {
    pub async fn close(self) {
        let _ = self
            .sender
            .send(Request::RevRequest(RevRequest::Drop { handle: self.id }))
            .await;
    }
}

#[async_trait]
impl<N: Send> Revision<N> for super::RevisionHandle<N> {
    async fn kv_root_hash(&self) -> Result<TrieHash, DbError> {
        let (send, recv) = oneshot::channel();
        let msg = Request::RevRequest(RevRequest::RootHash {
            handle: self.id,
            respond_to: send,
        });
        let _ = self.sender.send(msg).await;
        recv.await.expect("Actor task has been killed")
    }

    async fn kv_get<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DbError> {
        let (send, recv) = oneshot::channel();
        let _ = Request::RevRequest::<N>(RevRequest::Get {
            handle: self.id,
            key: key.as_ref().to_vec(),
            respond_to: send,
        });
        recv.await.expect("Actor task has been killed")
    }

    async fn prove<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Proof<N>, MerkleError> {
        let (send, recv) = oneshot::channel();
        let msg = Request::RevRequest(RevRequest::Prove {
            handle: self.id,
            key: key.as_ref().to_vec(),
            respond_to: send,
        });
        self.sender.send(msg).await.expect("channel failed");
        recv.await.expect("channel failed")
    }

    async fn verify_range_proof<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _proof: Proof<N>,
        _first_key: K,
        _last_key: K,
        _keys: Vec<K>,
        _values: Vec<K>,
    ) {
        todo!()
    }
    async fn root_hash(&self) -> Result<TrieHash, DbError> {
        let (send, recv) = oneshot::channel();
        let msg = Request::RevRequest(RevRequest::RootHash {
            handle: self.id,
            respond_to: send,
        });
        self.sender.send(msg).await.expect("channel failed");
        recv.await.expect("channel failed")
    }

    async fn dump<W: std::io::Write + Send + Sync>(&self, _writer: W) -> Result<(), DbError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn dump_account<W: std::io::Write + Send + Sync, K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
        _writer: W,
    ) -> Result<(), DbError> {
        todo!()
    }

    async fn kv_dump<W: std::io::Write + Send + Sync>(&self, _writer: W) -> Result<(), DbError> {
        unimplemented!();
    }

    #[cfg(feature = "eth")]
    async fn get_balance<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
    ) -> Result<primitive_types::U256, DbError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn get_code<K: AsRef<[u8]> + Send + Sync>(&self, _key: K) -> Result<Vec<u8>, DbError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn get_nonce<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
    ) -> Result<crate::api::Nonce, DbError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn get_state<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
        _sub_key: K,
    ) -> Result<Vec<u8>, DbError> {
        todo!()
    }
}

#[async_trait]
impl<N: Send> crate::api::Db<RevisionHandle<N>, N> for Connection<N>
where
    tokio::sync::mpsc::Sender<Request<N>>: From<tokio::sync::mpsc::Sender<Request<N>>>,
{
    async fn get_revision(&self, root_hash: TrieHash) -> Option<RevisionHandle<N>> {
        let (send, recv) = oneshot::channel();
        let msg = Request::NewRevision {
            root_hash,
            respond_to: send,
        };
        self.sender
            .as_ref()
            .unwrap()
            .send(msg)
            .await
            .expect("channel failed");
        let id = recv.await.unwrap();
        id.map(|id| RevisionHandle {
            sender: self.sender.as_ref().unwrap().clone(),
            id,
        })
    }
}

// TODO: add a meaningful test with `Proposal` and `Revisions`.
