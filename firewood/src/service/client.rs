/// Client side connection structure
///
/// A connection is used to send messages to the firewood thread.
use std::fmt::Debug;
use std::mem::take;
use std::thread::JoinHandle;
use std::{path::Path, thread};

use tokio::sync::{mpsc, oneshot};

use crate::api::Revision;
use crate::db::DBRevConfig;
use crate::{
    api,
    db::{DBConfig, DBError},
    merkle,
};
use async_trait::async_trait;

use super::server::FirewoodService;
use super::{BatchHandle, BatchRequest, Request, RevRequest, RevisionHandle};

/// A `Connection` represents a connection to the thread running firewood
/// The type specified is how you want to refer to your key values; this is
/// something like `Vec<u8>` or `&[u8]`
#[derive(Debug)]
pub struct Connection {
    sender: Option<mpsc::Sender<Request>>,
    handle: Option<JoinHandle<FirewoodService>>,
}

impl Drop for Connection {
    fn drop(&mut self) {
        drop(take(&mut self.sender));
        take(&mut self.handle)
            .unwrap()
            .join()
            .expect("Couldn't join with the firewood thread");
    }
}

impl Connection {
    #[allow(dead_code)]
    fn new<P: AsRef<Path>>(path: P, cfg: DBConfig) -> Self {
        let (sender, receiver) = mpsc::channel(1_000)
            as (
                tokio::sync::mpsc::Sender<Request>,
                tokio::sync::mpsc::Receiver<Request>,
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

#[async_trait]
impl api::WriteBatch for BatchHandle {
    async fn kv_insert<K: AsRef<[u8]> + Send + Sync, V: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        val: V,
    ) -> Result<Self, DBError> {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::KvInsert {
                handle: self.id,
                key: key.as_ref().to_vec(),
                val: val.as_ref().to_vec(),
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(_) => Ok(self),
            Err(_e) => Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }

    async fn kv_remove<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
    ) -> Result<(Self, Option<Vec<u8>>), DBError> {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::KvRemove {
                handle: self.id,
                key: key.as_ref().to_vec(),
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(Ok(v)) => Ok((self, v)),
            Ok(Err(e)) => Err(e),
            Err(_e) => Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }

    #[cfg(feature = "eth")]
    async fn set_balance<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        balance: primitive_types::U256,
    ) -> Result<Self, DBError> {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::SetBalance {
                handle: self.id,
                key: key.as_ref().to_vec(),
                balance,
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(Ok(_)) => Ok(self),
            Ok(Err(e)) => Err(e),
            Err(_e) => Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }

    #[cfg(feature = "eth")]
    async fn set_code<K, V>(self, key: K, code: V) -> Result<Self, DBError>
    where
        K: AsRef<[u8]> + Send + Sync,
        V: AsRef<[u8]> + Send + Sync,
    {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::SetCode {
                handle: self.id,
                key: key.as_ref().to_vec(),
                code: code.as_ref().to_vec(),
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(Ok(_)) => Ok(self),
            Ok(Err(e)) => Err(e),
            Err(_e) => Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }

    #[cfg(feature = "eth")]
    async fn set_nonce<K: AsRef<[u8]> + Send + Sync>(
        self,
        key: K,
        nonce: u64,
    ) -> Result<Self, DBError> {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::SetNonce {
                handle: self.id,
                key: key.as_ref().to_vec(),
                nonce,
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(Ok(_)) => Ok(self),
            Ok(Err(e)) => Err(e),
            Err(_e) => Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }

    #[cfg(feature = "eth")]
    async fn set_state<K, SK, S>(self, key: K, sub_key: SK, state: S) -> Result<Self, DBError>
    where
        K: AsRef<[u8]> + Send + Sync,
        SK: AsRef<[u8]> + Send + Sync,
        S: AsRef<[u8]> + Send + Sync,
    {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::SetState {
                handle: self.id,
                key: key.as_ref().to_vec(),
                sub_key: sub_key.as_ref().to_vec(),
                state: state.as_ref().to_vec(),
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(Ok(_)) => Ok(self),
            Ok(Err(e)) => Err(e),
            Err(_e) => Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }
    #[cfg(feature = "eth")]
    async fn create_account<K: AsRef<[u8]> + Send + Sync>(self, key: K) -> Result<Self, DBError> {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::CreateAccount {
                handle: self.id,
                key: key.as_ref().to_vec(),
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(Ok(_)) => Ok(self),
            Ok(Err(e)) => Err(e),
            Err(_e) => Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }

    #[cfg(feature = "eth")]
    async fn delete_account<K: AsRef<[u8]> + Send + Sync>(
        self,
        _key: K,
        _acc: &mut Option<crate::account::Account>,
    ) -> Result<Self, DBError> {
        todo!()
    }

    async fn no_root_hash(self) -> Self {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::NoRootHash {
                handle: self.id,
                respond_to: send,
            }))
            .await;
        let _ = recv.await;
        self
    }

    async fn commit(self) {
        let (send, recv) = oneshot::channel();
        let _ = self
            .sender
            .send(Request::BatchRequest(BatchRequest::Commit {
                handle: self.id,
                respond_to: send,
            }))
            .await;
        return match recv.await {
            Ok(_) => (),
            Err(_e) => (), // Err(DBError::InvalidParams), // TODO: need a special error for comm failures
        };
    }
}

impl super::RevisionHandle {
    pub async fn close(self) {
        let _ = self
            .sender
            .send(Request::RevRequest(RevRequest::Drop { handle: self.id }))
            .await;
    }
}

#[async_trait]
impl Revision for super::RevisionHandle {
    async fn kv_root_hash(&self) -> Result<merkle::Hash, DBError> {
        let (send, recv) = oneshot::channel();
        let msg = Request::RevRequest(RevRequest::RootHash {
            handle: self.id,
            respond_to: send,
        });
        let _ = self.sender.send(msg).await;
        recv.await.expect("Actor task has been killed")
    }

    async fn kv_get<K: AsRef<[u8]> + Send + Sync>(&self, key: K) -> Result<Vec<u8>, DBError> {
        let (send, recv) = oneshot::channel();
        let _ = Request::RevRequest(RevRequest::Get {
            handle: self.id,
            key: key.as_ref().to_vec(),
            respond_to: send,
        });
        recv.await.expect("Actor task has been killed")
    }

    #[cfg(feature = "proof")]
    async fn prove<K: AsRef<[u8]> + Send + Sync>(
        &self,
        key: K,
    ) -> Result<crate::proof::Proof, merkle::MerkleError> {
        let (send, recv) = oneshot::channel();
        let msg = Request::RevRequest(RevRequest::Prove {
            handle: self.id,
            key: key.as_ref().to_vec(),
            respond_to: send,
        });
        self.sender.send(msg).await.expect("channel failed");
        recv.await.expect("channel failed")
    }

    #[cfg(feature = "proof")]
    async fn verify_range_proof<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _proof: crate::proof::Proof,
        _first_key: K,
        _last_key: K,
        _keys: Vec<K>,
        _values: Vec<K>,
    ) {
        todo!()
    }
    async fn root_hash(&self) -> Result<merkle::Hash, DBError> {
        let (send, recv) = oneshot::channel();
        let msg = Request::RevRequest(RevRequest::RootHash {
            handle: self.id,
            respond_to: send,
        });
        self.sender.send(msg).await.expect("channel failed");
        recv.await.expect("channel failed")
    }

    async fn dump<W: std::io::Write + Send + Sync>(&self, _writer: W) -> Result<(), DBError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn dump_account<W: std::io::Write + Send + Sync, K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
        _writer: W,
    ) -> Result<(), DBError> {
        todo!()
    }

    async fn kv_dump<W: std::io::Write + Send + Sync>(&self, _writer: W) -> Result<(), DBError> {
        unimplemented!();
    }

    #[cfg(feature = "eth")]
    async fn get_balance<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
    ) -> Result<primitive_types::U256, DBError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn get_code<K: AsRef<[u8]> + Send + Sync>(&self, _key: K) -> Result<Vec<u8>, DBError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn get_nonce<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
    ) -> Result<crate::api::Nonce, DBError> {
        todo!()
    }

    #[cfg(feature = "eth")]
    async fn get_state<K: AsRef<[u8]> + Send + Sync>(
        &self,
        _key: K,
        _sub_key: K,
    ) -> Result<Vec<u8>, DBError> {
        todo!()
    }
}

#[async_trait]
impl crate::api::DB<BatchHandle, RevisionHandle> for Connection
where
    tokio::sync::mpsc::Sender<Request>: From<tokio::sync::mpsc::Sender<Request>>,
{
    async fn new_writebatch(&self) -> BatchHandle {
        let (send, recv) = oneshot::channel();
        let msg = Request::NewBatch { respond_to: send };
        self.sender
            .as_ref()
            .unwrap()
            .send(msg)
            .await
            .expect("channel failed");
        let id = recv.await;
        let id = id.unwrap();
        BatchHandle {
            sender: self.sender.as_ref().unwrap().clone(),
            id,
        }
    }

    async fn get_revision(&self, nback: usize, cfg: Option<DBRevConfig>) -> Option<RevisionHandle> {
        let (send, recv) = oneshot::channel();
        let msg = Request::NewRevision {
            nback,
            cfg,
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

#[cfg(test)]
mod test {
    use crate::{api::WriteBatch, api::DB, db::WALConfig};
    use std::path::PathBuf;

    use super::*;
    #[tokio::test]
    async fn sender_api() {
        let key = b"key";
        // test using a subdirectory of CARGO_TARGET_DIR which is
        // cleaned up on `cargo clean`
        let tmpdb = [
            &std::env::var("CARGO_TARGET_DIR").unwrap_or("/tmp".to_string()),
            "sender_api_test_db",
        ];
        let tmpdb = tmpdb.into_iter().collect::<PathBuf>();
        let conn = Connection::new(tmpdb, db_config());
        let batch = conn.new_writebatch().await;
        let batch = batch.kv_insert(key, b"val").await.unwrap();
        #[cfg(feature = "eth")]
        {
            let batch = batch.set_code(key, b"code").await.unwrap();
            let batch = batch.set_nonce(key, 42).await.unwrap();
            let batch = batch.set_state(key, b"subkey", b"state").await.unwrap();
            let batch = batch.create_account(key).await.unwrap();
            let batch = batch.no_root_hash().await;
        }
        let (batch, oldvalue) = batch.kv_remove(key).await.unwrap();
        assert_eq!(oldvalue, Some(b"val".to_vec()));
        batch.commit().await;
        let batch = conn.new_writebatch().await;
        let batch = batch.kv_insert(b"k2", b"val").await.unwrap();
        batch.commit().await;

        assert_ne!(
            conn.get_revision(1, None)
                .await
                .unwrap()
                .root_hash()
                .await
                .unwrap(),
            merkle::Hash([0; 32])
        );
    }

    fn db_config() -> DBConfig {
        DBConfig::builder()
            .wal(WALConfig::builder().max_revisions(10).build())
            .truncate(true)
            .build()
    }
}
