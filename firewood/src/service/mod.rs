// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use tokio::sync::{mpsc, oneshot};

use crate::{db::DbError, merkle::TrieHash};

mod client;
mod server;

pub type BatchId = u32;
pub type RevId = u32;

#[derive(Debug)]
pub struct RevisionHandle {
    sender: mpsc::Sender<Request>,
    id: u32,
}

/// Client side request object
#[derive(Debug)]
pub enum Request {
    NewRevision {
        root_hash: TrieHash,
        respond_to: oneshot::Sender<Option<RevId>>,
    },

    RevRequest(RevRequest),
}

type OwnedKey = Vec<u8>;
#[allow(dead_code)]
type OwnedVal = Vec<u8>;

#[derive(Debug)]
pub enum RevRequest {
    Get {
        handle: RevId,
        key: OwnedKey,
        respond_to: oneshot::Sender<Result<Vec<u8>, DbError>>,
    },
    #[cfg(feature = "proof")]
    Prove {
        handle: RevId,
        key: OwnedKey,
        respond_to: oneshot::Sender<Result<Proof, MerkleError>>,
    },
    RootHash {
        handle: RevId,
        respond_to: oneshot::Sender<Result<TrieHash, DbError>>,
    },
    Drop {
        handle: RevId,
    },
}
