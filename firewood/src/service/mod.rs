use tokio::sync::{mpsc, oneshot};

use crate::{
    db::{DBError, DBRevConfig},
    merkle::{self, MerkleError},
    proof::Proof,
};

mod client;
mod server;

pub type BatchId = u32;
pub type RevId = u32;

#[derive(Debug)]
pub struct BatchHandle {
    sender: mpsc::Sender<Request>,
    id: u32,
}

#[derive(Debug)]
pub struct RevisionHandle {
    sender: mpsc::Sender<Request>,
    id: u32,
}

/// Client side request object
#[derive(Debug)]
pub enum Request {
    NewBatch {
        respond_to: oneshot::Sender<BatchId>,
    },
    NewRevision {
        nback: usize,
        cfg: Option<DBRevConfig>,
        respond_to: oneshot::Sender<Option<RevId>>,
    },

    BatchRequest(BatchRequest),
    RevRequest(RevRequest),
}

type OwnedKey = Vec<u8>;
type OwnedVal = Vec<u8>;

#[derive(Debug)]
pub enum BatchRequest {
    KvRemove {
        handle: BatchId,
        key: OwnedKey,
        respond_to: oneshot::Sender<Result<Option<Vec<u8>>, DBError>>,
    },
    KvInsert {
        handle: BatchId,
        key: OwnedKey,
        val: OwnedKey,
        respond_to: oneshot::Sender<Result<(), DBError>>,
    },
    Commit {
        handle: BatchId,
        respond_to: oneshot::Sender<Result<(), DBError>>,
    },
    SetBalance {
        handle: BatchId,
        key: OwnedKey,
        balance: primitive_types::U256,
        respond_to: oneshot::Sender<Result<(), DBError>>,
    },
    SetCode {
        handle: BatchId,
        key: OwnedKey,
        code: OwnedVal,
        respond_to: oneshot::Sender<Result<(), DBError>>,
    },
    SetNonce {
        handle: BatchId,
        key: OwnedKey,
        nonce: u64,
        respond_to: oneshot::Sender<Result<(), DBError>>,
    },
    SetState {
        handle: BatchId,
        key: OwnedKey,
        sub_key: OwnedVal,
        state: OwnedVal,
        respond_to: oneshot::Sender<Result<(), DBError>>,
    },
    CreateAccount {
        handle: BatchId,
        key: OwnedKey,
        respond_to: oneshot::Sender<Result<(), DBError>>,
    },
    NoRootHash {
        handle: BatchId,
        respond_to: oneshot::Sender<()>,
    },
}

#[derive(Debug)]
pub enum RevRequest {
    Get {
        handle: RevId,
        key: OwnedKey,
        respond_to: oneshot::Sender<Result<Vec<u8>, DBError>>,
    },
    Prove {
        handle: RevId,
        key: OwnedKey,
        respond_to: oneshot::Sender<Result<Proof, MerkleError>>,
    },
    RootHash {
        handle: RevId,
        respond_to: oneshot::Sender<Result<merkle::Hash, DBError>>,
    },
    Drop {
        handle: RevId,
    },
}
