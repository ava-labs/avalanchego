// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::manager::RevisionManagerError;
use crate::merkle::{Key, Value};
use crate::proof::{Proof, ProofError, ProofNode};
pub use crate::range_proof::RangeProof;
use async_trait::async_trait;
use firewood_storage::{FileIoError, TrieHash};
use futures::Stream;
use std::fmt::Debug;
use std::num::NonZeroUsize;
use std::sync::Arc;

/// A `KeyType` is something that can be xcast to a u8 reference,
/// and can be sent and shared across threads. References with
/// lifetimes are not allowed (hence 'static)
pub trait KeyType: AsRef<[u8]> + Send + Sync + Debug {}

impl<T> KeyType for T where T: AsRef<[u8]> + Send + Sync + Debug {}

/// A `ValueType` is the same as a `KeyType`. However, these could
/// be a different type from the `KeyType` on a given API call.
/// For example, you might insert `{key: "key", value: [0u8]}`
/// This also means that the type of all the keys for a single
/// API call must be the same, as well as the type of all values
/// must be the same.
pub trait ValueType: AsRef<[u8]> + Send + Sync + Debug {}

impl<T> ValueType for T where T: AsRef<[u8]> + Send + Sync + Debug {}

/// The type and size of a single hash key
/// These are 256-bit hashes that are used for a variety of reasons:
///  - They identify a version of the datastore at a specific point
///    in time
///  - They are used to provide integrity at different points in a
///    proof
pub type HashKey = firewood_storage::TrieHash;

/// An extension trait for the [`HashKey`] type to provide additional methods.
pub trait HashKeyExt: Sized {
    /// Default root hash for an empty database.
    fn default_root_hash() -> Option<HashKey>;
}

/// An extension trait for an optional `HashKey` type to provide additional methods.
pub trait OptionalHashKeyExt: Sized {
    /// Returns the default root hash if the current value is [`None`].
    fn or_default_root_hash(self) -> Option<HashKey>;
}

#[cfg(not(feature = "ethhash"))]
impl HashKeyExt for HashKey {
    /// Creates a new `HashKey` representing the empty root hash.
    #[inline]
    fn default_root_hash() -> Option<HashKey> {
        None
    }
}

#[cfg(feature = "ethhash")]
impl HashKeyExt for HashKey {
    #[inline]
    fn default_root_hash() -> Option<HashKey> {
        const EMPTY_RLP_HASH: [u8; size_of::<TrieHash>()] = [
            // "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
            0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6, 0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0,
            0xf8, 0x6e, 0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0, 0x01, 0x62, 0x2f, 0xb5,
            0xe3, 0x63, 0xb4, 0x21,
        ];

        Some(EMPTY_RLP_HASH.into())
    }
}

impl OptionalHashKeyExt for Option<HashKey> {
    #[inline]
    fn or_default_root_hash(self) -> Option<HashKey> {
        self.or_else(HashKey::default_root_hash)
    }
}

/// A frozen proof is a proof that is stored in immutable memory.
pub type FrozenRangeProof = RangeProof<Key, Value, Box<[ProofNode]>>;

/// A frozen proof uses an immutable collection of proof nodes.
pub type FrozenProof = Proof<Box<[ProofNode]>>;

/// A key/value pair operation. Only put (upsert) and delete are
/// supported
#[derive(Debug)]
pub enum BatchOp<K: KeyType, V: ValueType> {
    /// Upsert a key/value pair
    Put {
        /// the key
        key: K,
        /// the value
        value: V,
    },

    /// Delete a key
    Delete {
        /// The key
        key: K,
    },

    /// Delete a range of keys by prefix
    DeleteRange {
        /// The prefix of the keys to delete
        prefix: K,
    },
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
    HashNotFound {
        /// the provided hash key
        provided: HashKey,
    },

    /// Incorrect root hash for commit
    #[error("Incorrect root hash for commit: {provided:?} != {current:?}")]
    IncorrectRootHash {
        /// the provided root hash
        provided: HashKey,
        /// the current root hash
        current: HashKey,
    },

    /// Invalid range
    #[error("Invalid range: {start_key:?} > {end_key:?}")]
    InvalidRange {
        /// The provided starting key
        start_key: Key,
        /// The provided ending key
        end_key: Key,
    },

    #[error("IO error: {0}")]
    /// An IO error occurred
    IO(#[from] std::io::Error),

    #[error("File IO error: {0}")]
    /// A file I/O error occurred
    FileIO(#[from] FileIoError),

    /// Cannot commit a committed proposal
    #[error("Cannot commit a committed proposal")]
    AlreadyCommitted,

    /// Internal error
    #[error("Internal error")]
    InternalError(Box<dyn std::error::Error + Send>),

    /// Range too small
    #[error("Range too small")]
    RangeTooSmall,

    /// Request `RangeProof` for empty trie
    #[error("request RangeProof for empty trie")]
    RangeProofOnEmptyTrie,

    /// Request `RangeProof` for empty range
    #[error("the latest revision is empty and has no root hash")]
    LatestIsEmpty,

    /// This is not the latest proposal
    #[error("commit the parents of this proposal first")]
    NotLatest,

    /// Sibling already committed
    #[error("sibling already committed")]
    SiblingCommitted,

    /// Proof error
    #[error("proof error")]
    ProofError(#[from] ProofError),

    /// Revision not found
    #[error("revision not found")]
    RevisionNotFound,

    /// An invalid root hash was provided
    #[error(transparent)]
    InvalidRootHash(#[from] firewood_storage::InvalidTrieHashLength),
}

impl From<RevisionManagerError> for Error {
    fn from(err: RevisionManagerError) -> Self {
        match err {
            RevisionManagerError::FileIoError(io_err) => Error::FileIO(io_err),
            RevisionManagerError::NotLatest => Error::NotLatest,
            RevisionManagerError::RevisionNotFound => Error::RevisionNotFound,
        }
    }
}

/// The database interface. The methods here operate on the most
/// recently committed revision, and allow the creation of a new
/// [Proposal] or a new [DbView] based on a specific historical
/// revision.
#[async_trait]
pub trait Db {
    /// The type of a historical revision
    type Historical: DbView;

    /// The type of a proposal
    type Proposal<'db>: DbView + Proposal
    where
        Self: 'db;

    /// Get a reference to a specific view based on a hash
    ///
    /// # Arguments
    ///
    /// - `hash` - Identifies the revision for the view
    async fn revision(&self, hash: TrieHash) -> Result<Arc<Self::Historical>, Error>;

    /// Get the hash of the most recently committed version
    ///
    /// # Note
    ///
    /// If the database is empty, this will return None, unless the ethhash feature is enabled.
    /// In that case, we return the special ethhash compatible empty trie hash.
    async fn root_hash(&self) -> Result<Option<TrieHash>, Error>;

    /// Get all the hashes available
    async fn all_hashes(&self) -> Result<Vec<TrieHash>, Error>;

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
    async fn propose<'db, K: KeyType, V: ValueType>(
        &'db self,
        data: Batch<K, V>,
    ) -> Result<Self::Proposal<'db>, Error>
    where
        Self: 'db;
}

/// A view of the database at a specific time.
///
/// There are a few ways to create a [DbView]:
/// 1. From [Db::revision] which gives you a view for a specific
///    historical revision
/// 2. From [Db::propose] which is a view on top of the most recently
///    committed revision with changes applied; or
/// 3. From [Proposal::propose] which is a view on top of another proposal.
#[async_trait]
pub trait DbView {
    /// The type of a stream of key/value pairs
    type Stream<'view>: Stream<Item = Result<(Key, Value), Error>>
    where
        Self: 'view;

    /// Get the root hash for the current DbView
    ///
    /// # Note
    ///
    /// If the database is empty, this will return None, unless the ethhash feature is enabled.
    /// In that case, we return the special ethhash compatible empty trie hash.
    async fn root_hash(&self) -> Result<Option<HashKey>, Error>;

    /// Get the value of a specific key
    async fn val<K: KeyType>(&self, key: K) -> Result<Option<Value>, Error>;

    /// Obtain a proof for a single key
    async fn single_key_proof<K: KeyType>(&self, key: K) -> Result<FrozenProof, Error>;

    /// Obtain a range proof over a set of keys
    ///
    /// # Arguments
    ///
    /// * `first_key` - If None, start at the lowest key
    /// * `last_key` - If None, continue to the end of the database
    /// * `limit` - The maximum number of keys in the range proof
    ///
    async fn range_proof<K: KeyType>(
        &self,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, Error>;

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
    #[expect(clippy::missing_errors_doc)]
    fn iter_option<K: KeyType>(&self, first_key: Option<K>) -> Result<Self::Stream<'_>, Error>;

    /// Obtain a stream over the keys/values of this view, starting from the beginning
    #[expect(clippy::missing_errors_doc)]
    #[expect(clippy::iter_not_returning_iterator)]
    fn iter(&self) -> Result<Self::Stream<'_>, Error> {
        self.iter_option(Option::<Key>::None)
    }

    /// Obtain a stream over the key/values, starting at a specific key
    #[expect(clippy::missing_errors_doc)]
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
    /// The type of a proposal
    type Proposal: DbView + Proposal;

    /// Commit this revision
    async fn commit(self) -> Result<(), Error>;

    /// Propose a new revision on top of an existing proposal
    ///
    /// # Arguments
    ///
    /// * `data` - the batch changes to apply
    ///
    /// # Return value
    ///
    /// A new proposal
    ///
    async fn propose<K: KeyType, V: ValueType>(
        &self,
        data: Batch<K, V>,
    ) -> Result<Self::Proposal, Error>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    #[cfg(feature = "ethhash")]
    fn test_ethhash_compat_default_root_hash_equals_empty_rlp_hash() {
        use sha3::Digest as _;

        assert_eq!(
            TrieHash::default_root_hash(),
            sha3::Keccak256::digest(rlp::NULL_RLP)
                .as_slice()
                .try_into()
                .ok(),
        );
    }

    #[test]
    #[cfg(not(feature = "ethhash"))]
    fn test_firewood_default_root_hash_equals_none() {
        assert_eq!(TrieHash::default_root_hash(), None);
    }
}
