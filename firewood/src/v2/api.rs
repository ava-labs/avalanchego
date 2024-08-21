// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::manager::RevisionManagerError;
use crate::proof::ProofNode;
pub use crate::range_proof::RangeProof;
use crate::{merkle::MerkleError, proof::Proof};
use async_trait::async_trait;
use futures::Stream;
use std::{fmt::Debug, sync::Arc};
use storage::TrieHash;

/// A `KeyType` is something that can be xcast to a u8 reference,
/// and can be sent and shared across threads. References with
/// lifetimes are not allowed (hence 'static)
pub trait KeyType: AsRef<[u8]> + Send + Sync + Debug {}

impl<T> KeyType for T where T: AsRef<[u8]> + Send + Sync + Debug {}

/// A `ValueType` is the same as a `KeyType`. However, these could
/// be a different type from the `KeyType` on a given API call.
/// For example, you might insert {key: "key", value: vec!\[0u8\]}
/// This also means that the type of all the keys for a single
/// API call must be the same, as well as the type of all values
/// must be the same.
pub trait ValueType: AsRef<[u8]> + Send + Sync + Debug + 'static {}

impl<T> ValueType for T where T: AsRef<[u8]> + Send + Sync + Debug + 'static {}

/// The type and size of a single hash key
/// These are 256-bit hashes that are used for a variety of reasons:
///  - They identify a version of the datastore at a specific point
///    in time
///  - They are used to provide integrity at different points in a
///    proof
pub type HashKey = storage::TrieHash;

/// A key/value pair operation. Only put (upsert) and delete are
/// supported
#[derive(Debug)]
pub enum BatchOp<K: KeyType, V: ValueType> {
    Put { key: K, value: V },
    Delete { key: K },
}

/// A list of operations to consist of a batch that
/// can be proposed
pub type Batch<K, V> = Vec<BatchOp<K, V>>;

/// A convenience implementation to convert a vector of key/value
/// pairs into a batch of insert operations
#[must_use]
pub fn vec_into_batch<K: KeyType, V: ValueType>(value: Vec<(K, V)>) -> Batch<K, V> {
    value
        .into_iter()
        .map(|(key, value)| BatchOp::Put { key, value })
        .collect()
}

/// Errors returned through the API
#[derive(thiserror::Error, Debug)]
#[non_exhaustive]
pub enum Error {
    /// A given hash key is not available in the database
    #[error("Hash not found for key: {provided:?}")]
    HashNotFound { provided: HashKey },

    /// Incorrect root hash for commit
    #[error("Incorrect root hash for commit: {provided:?} != {current:?}")]
    IncorrectRootHash { provided: HashKey, current: HashKey },

    /// Invalid range
    #[error("Invalid range: {start_key:?} > {end_key:?}")]
    InvalidRange {
        start_key: Box<[u8]>,
        end_key: Box<[u8]>,
    },

    #[error("IO error: {0}")]
    IO(#[from] std::io::Error),

    #[error("Invalid proposal")]
    InvalidProposal,

    // Cloned proposals are problematic because if they are committed, then you could
    // create another proposal from this committed proposal, so we error at commit time
    // if there are outstanding clones
    #[error("Cannot commit a cloned proposal")]
    CannotCommitClonedProposal,

    #[error("Internal error")]
    InternalError(Box<dyn std::error::Error + Send>),

    #[error("Range too small")]
    RangeTooSmall,

    #[error("request RangeProof for empty trie")]
    RangeProofOnEmptyTrie,
}

impl From<MerkleError> for Error {
    fn from(err: MerkleError) -> Self {
        // TODO: do a better job
        Error::InternalError(Box::new(err))
    }
}

impl From<RevisionManagerError> for Error {
    fn from(err: RevisionManagerError) -> Self {
        // TODO: do a better job
        Error::InternalError(Box::new(err))
    }
}

/// The database interface, which includes a type for a static view of
/// the database (the DbView). The most common implementation of the DbView
/// is the api::DbView trait defined next.
#[async_trait]
pub trait Db {
    type Historical: DbView;

    type Proposal<'p>: DbView + Proposal
    where
        Self: 'p;

    /// Get a reference to a specific view based on a hash
    ///
    /// # Arguments
    ///
    /// - `hash` - Identifies the revision for the view
    async fn revision(&self, hash: TrieHash) -> Result<Arc<Self::Historical>, Error>;

    /// Get the hash of the most recently committed version
    async fn root_hash(&self) -> Result<Option<TrieHash>, Error>;

    /// Propose a change to the database via a batch
    ///
    /// This proposal assumes it is based off the most recently
    /// committed transaction
    ///
    /// # Arguments
    ///
    /// * `data` - A batch consisting of [BatchOp::Put] and
    ///            [BatchOp::Delete] operations to apply
    ///
    async fn propose<'p, K: KeyType, V: ValueType>(
        &'p mut self,
        data: Batch<K, V>,
    ) -> Result<Arc<Self::Proposal<'p>>, Error>
    where
        Self: 'p;
}

/// A view of the database at a specific time. These are wrapped with
/// a Weak reference when fetching via a call to [Db::revision], as these
/// can disappear because they became too old.
///
/// You only need a DbView if you need to read from a snapshot at a given
/// root. Don't hold a strong reference to the DbView as it prevents older
/// views from being cleaned up.
///
/// A [Proposal] requires implementing DbView
#[async_trait]
pub trait DbView {
    type Stream<'a>: Stream<Item = Result<(Box<[u8]>, Vec<u8>), Error>>
    where
        Self: 'a;

    /// Get the root hash for the current DbView
    async fn root_hash(&self) -> Result<Option<HashKey>, Error>;

    /// Get the value of a specific key
    async fn val<K: KeyType>(&self, key: K) -> Result<Option<Box<[u8]>>, Error>;

    /// Obtain a proof for a single key
    async fn single_key_proof<K: KeyType>(&self, key: K)
        -> Result<Option<Proof<ProofNode>>, Error>;

    /// Obtain a range proof over a set of keys
    ///
    /// # Arguments
    ///
    /// * `first_key` - If None, start at the lowest key
    /// * `last_key` - If None, continue to the end of the database
    /// * `limit` - The maximum number of keys in the range proof
    ///
    async fn range_proof<K: KeyType, V: Send + Sync>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<usize>,
    ) -> Result<Option<RangeProof<Box<[u8]>, Box<[u8]>, ProofNode>>, Error>;

    /// Obtain a stream over the keys/values of this view, using an optional starting point
    ///
    /// # Arguments
    ///
    /// * `first_key` - If None, start at the lowest key
    ///
    /// # Note
    ///
    /// If you always want to start at the beginning, [DbView::iter] is easier to use
    /// If you always provide a key, [DbView::iter_from] is easier to use
    ///
    fn iter_option<K: KeyType>(&self, first_key: Option<K>) -> Result<Self::Stream<'_>, Error>;

    /// Obtain a stream over the keys/values of this view, starting from the beginning
    fn iter(&self) -> Result<Self::Stream<'_>, Error> {
        self.iter_option(Option::<Box<[u8]>>::None)
    }

    /// Obtain a stream over the key/values, starting at a specific key
    fn iter_from<K: KeyType + 'static>(&self, first_key: K) -> Result<Self::Stream<'_>, Error> {
        self.iter_option(Some(first_key))
    }
}

/// A proposal for a new revision of the database.
///
/// A proposal may be committed, which consumes the
/// [Proposal] and return the generic type T, which
/// is the same thing you get if you call [Db::root_hash]
/// immediately after committing, and then call
/// [Db::revision] with the returned revision.
///
/// A proposal type must also implement everything in a
/// [DbView], which means you can fetch values from it or
/// obtain proofs.
#[async_trait]
pub trait Proposal: DbView + Send + Sync {
    type Proposal: DbView + Proposal;

    /// Commit this revision
    async fn commit(self: Arc<Self>) -> Result<(), Error>;

    /// Propose a new revision on top of an existing proposal
    ///
    /// # Arguments
    ///
    /// * `data` - the batch changes to apply
    ///
    /// # Return value
    ///
    /// A reference to a new proposal
    ///
    async fn propose<K: KeyType, V: ValueType>(
        self: Arc<Self>,
        data: Batch<K, V>,
    ) -> Result<Arc<Self::Proposal>, Error>;
}
