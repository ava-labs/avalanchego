// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::merkle;
use firewood::v2::api;
use std::fmt;

use crate::revision::{GetRevisionResult, RevisionHandle};
use crate::{
    ChangeProofContext, CreateIteratorResult, CreateProposalResult, HashKey, IteratorHandle,
    NextKeyRange, OwnedBytes, OwnedKeyValuePair, ProposalHandle, RangeProofContext,
};

/// The result type returned from an FFI function that returns no value but may
/// return an error.
#[derive(Debug)]
#[repr(C)]
pub enum VoidResult {
    /// The caller provided a null pointer to the input handle.
    NullHandlePointer,

    /// The operation was successful and no error occurred.
    Ok,

    /// An error occurred and the message is returned as an [`OwnedBytes`]. Its
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl From<()> for VoidResult {
    fn from((): ()) -> Self {
        VoidResult::Ok
    }
}

impl<E: fmt::Display> From<Result<(), E>> for VoidResult {
    fn from(value: Result<(), E>) -> Self {
        match value {
            Ok(()) => VoidResult::Ok,
            Err(err) => VoidResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// The result type returned from the open or create database functions.
#[derive(Debug)]
#[repr(C)]
pub enum HandleResult {
    /// The database was opened or created successfully and the handle is
    /// returned as an opaque pointer.
    ///
    /// The caller must ensure that [`fwd_close_db`] is called to free resources
    /// associated with this handle when it is no longer needed.
    ///
    /// [`fwd_close_db`]: crate::fwd_close_db
    Ok(Box<crate::DatabaseHandle>),

    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl<E: fmt::Display> From<Result<crate::DatabaseHandle, E>> for HandleResult {
    fn from(value: Result<crate::DatabaseHandle, E>) -> Self {
        match value {
            Ok(handle) => HandleResult::Ok(Box::new(handle)),
            Err(err) => HandleResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// A result type returned from FFI functions that retrieve a single value.
#[derive(Debug)]
#[repr(C)]
pub enum ValueResult {
    /// The caller provided a null pointer to a database handle.
    NullHandlePointer,
    /// The provided root was not found in the database.
    RevisionNotFound(HashKey),
    /// The provided key was not found in the database or proposal.
    None,
    /// A value was found and is returned.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this value.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Some(OwnedBytes),
    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl<E: fmt::Display> From<Result<String, E>> for ValueResult {
    fn from(value: Result<String, E>) -> Self {
        match value {
            Ok(data) => ValueResult::Some(data.into_bytes().into()),
            Err(err) => ValueResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

impl From<Result<Option<Box<[u8]>>, api::Error>> for ValueResult {
    fn from(value: Result<Option<Box<[u8]>>, api::Error>) -> Self {
        match value {
            Ok(None) => ValueResult::None,
            Err(api::Error::RevisionNotFound { provided }) => ValueResult::RevisionNotFound(
                HashKey::from(provided.unwrap_or_else(api::HashKey::empty)),
            ),
            Ok(Some(data)) => ValueResult::Some(data.into()),
            Err(err) => ValueResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

impl From<Result<Option<Box<[u8]>>, firewood::db::DbError>> for ValueResult {
    fn from(value: Result<Option<Box<[u8]>>, firewood::db::DbError>) -> Self {
        value.map_err(api::Error::from).into()
    }
}

impl From<Vec<u8>> for ValueResult {
    fn from(value: Vec<u8>) -> Self {
        value.into_boxed_slice().into()
    }
}

impl From<Box<[u8]>> for ValueResult {
    fn from(value: Box<[u8]>) -> Self {
        ValueResult::Some(value.into())
    }
}

/// A result type returned from FFI functions return the database root hash. This
/// may or may not be after a mutation.
#[derive(Debug)]
#[repr(C)]
pub enum HashResult {
    /// The caller provided a null pointer to a database handle.
    NullHandlePointer,
    /// The proposal resulted in an empty database or the database currently has
    /// no root hash.
    None,
    /// The mutation was successful and the root hash is returned, if this result
    /// was from a mutation. Otherwise, this is the current root hash of the
    /// database.
    Some(HashKey),
    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl<E: fmt::Display> From<Result<Option<api::HashKey>, E>> for HashResult {
    fn from(value: Result<Option<api::HashKey>, E>) -> Self {
        match value {
            Ok(None) => HashResult::None,
            Ok(Some(hash)) => HashResult::Some(HashKey::from(hash)),
            Err(err) => HashResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// A result type returned from FFI functions that create or parse range proofs.
///
/// The caller must ensure that [`fwd_free_range_proof`] is called to
/// free the memory associated with the returned context when it is no longer
/// needed.
///
/// [`fwd_free_range_proof`]: crate::fwd_free_range_proof
#[derive(Debug)]
#[repr(C)]
pub enum RangeProofResult {
    /// The caller provided a null pointer to the input handle.
    NullHandlePointer,
    /// The provided root was not found in the database.
    RevisionNotFound(HashKey),
    /// A range proof was requested on an empty trie.
    EmptyTrie,
    /// The proof was successfully created or parsed.
    ///
    /// If the value was parsed from a serialized proof, this does not imply that
    /// the proof is valid, only that it is well-formed. The verify method must
    /// be called to ensure the proof is cryptographically valid.
    Ok(Box<RangeProofContext>),
    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl From<Result<api::FrozenRangeProof, api::Error>> for RangeProofResult {
    fn from(value: Result<api::FrozenRangeProof, api::Error>) -> Self {
        match value {
            Ok(proof) => RangeProofResult::Ok(Box::new(proof.into())),
            Err(api::Error::RevisionNotFound { provided }) => RangeProofResult::RevisionNotFound(
                HashKey::from(provided.unwrap_or_else(api::HashKey::empty)),
            ),
            Err(api::Error::RangeProofOnEmptyTrie) => RangeProofResult::EmptyTrie,
            Err(err) => RangeProofResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// A result type returned from FFI functions that create or parse change proofs.
///
/// The caller must ensure that [`fwd_free_change_proof`] is called to
/// free the memory associated with the returned context when it is no longer
/// needed.
///
/// [`fwd_free_change_proof`]: crate::fwd_free_change_proof
#[derive(Debug)]
#[repr(C)]
pub enum ChangeProofResult {
    /// The caller provided a null pointer to the input handle.
    NullHandlePointer,
    /// The provided root was not found in the database.
    RevisionNotFound(HashKey),
    /// The proof was successfully created or parsed.
    ///
    /// If the value was parsed from a serialized proof, this does not imply that
    /// the proof is valid, only that it is well-formed. The verify method must
    /// be called to ensure the proof is cryptographically valid.
    Ok(Box<ChangeProofContext>),
    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

#[derive(Debug)]
#[repr(C)]
pub enum NextKeyRangeResult {
    /// The caller provided a null pointer to the input handle.
    NullHandlePointer,
    /// The proof has not prepared into a proposal nor committed to the database.
    NotPrepared,
    /// There are no more keys to fetch.
    None,
    /// The next key range to fetch is returned.
    Some(NextKeyRange),
    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

/// A result type returned from FFI functions that create a proposal but do not
/// commit it to the database.
#[derive(Debug)]
#[repr(C)]
pub enum ProposalResult<'db> {
    /// The caller provided a null pointer to a database handle.
    NullHandlePointer,
    /// Buulding the proposal was successful and the proposal ID and root hash
    /// are returned.
    Ok {
        /// An opaque pointer to the [`ProposalHandle`] that can be use to create
        /// an additional proposal or later commit. The caller must ensure that this
        /// pointer is freed with [`fwd_free_proposal`] if it is not committed.
        ///
        /// [`fwd_free_proposal`]: crate::fwd_free_proposal
        // note: opaque pointers mut be boxed because the FFI does not the structure definition.
        handle: Box<ProposalHandle<'db>>,
        /// The root hash of the proposal. Zeroed if the proposal resulted in an
        /// empty database.
        root_hash: HashKey,
    },
    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

/// A result type returned from FFI functions that create an iterator
#[derive(Debug)]
#[repr(C)]
pub enum IteratorResult<'db> {
    /// The caller provided a null pointer to a revision/proposal handle.
    NullHandlePointer,
    /// Building the iterator was successful and the iterator handle is returned
    Ok {
        /// An opaque pointer to the [`IteratorHandle`].
        /// The value should be freed with [`fwd_free_iterator`]
        ///
        /// [`fwd_free_iterator`]: crate::fwd_free_iterator
        handle: Box<IteratorHandle<'db>>,
    },
    /// An error occurred and the message is returned as an [`OwnedBytes`].
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

/// A result type returned from iterator FFI functions
#[derive(Debug)]
#[repr(C)]
pub enum KeyValueResult {
    /// The caller provided a null pointer to an iterator handle.
    NullHandlePointer,
    /// The iterator is exhausted
    None,
    /// The next item is returned.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with the key and the value of this pair.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Some(OwnedKeyValuePair),
    /// An error occurred and the message is returned as an [`OwnedBytes`]. The
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl<E: fmt::Display> From<Option<Result<(merkle::Key, merkle::Value), E>>> for KeyValueResult {
    fn from(value: Option<Result<(merkle::Key, merkle::Value), E>>) -> Self {
        match value {
            Some(value) => match value {
                Ok(value) => KeyValueResult::Some(value.into()),
                Err(err) => KeyValueResult::Err(err.to_string().into_bytes().into()),
            },
            None => KeyValueResult::None,
        }
    }
}

impl<'db> From<CreateIteratorResult<'db>> for IteratorResult<'db> {
    fn from(value: CreateIteratorResult<'db>) -> Self {
        IteratorResult::Ok {
            handle: Box::new(value.0),
        }
    }
}

impl<'db, E: fmt::Display> From<Result<CreateIteratorResult<'db>, E>> for IteratorResult<'db> {
    fn from(value: Result<CreateIteratorResult<'db>, E>) -> Self {
        match value {
            Ok(res) => res.into(),
            Err(err) => IteratorResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// A result type returned from FFI functions that get a revision
#[derive(Debug)]
#[repr(C)]
pub enum RevisionResult {
    /// The caller provided a null pointer to a database handle.
    NullHandlePointer,
    /// The provided root was not found in the database.
    RevisionNotFound(HashKey),
    /// Getting the revision was successful and the revision handle and root
    /// hash are returned.
    Ok {
        /// An opaque pointer to the [`RevisionHandle`].
        /// The value should be freed with [`fwd_free_revision`]
        ///
        /// [`fwd_free_revision`]: crate::fwd_free_revision
        handle: Box<RevisionHandle>,
        /// The root hash of the revision.
        root_hash: HashKey,
    },
    /// An error occurred and the message is returned as an [`OwnedBytes`]. The
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl From<GetRevisionResult> for RevisionResult {
    fn from(value: GetRevisionResult) -> Self {
        RevisionResult::Ok {
            handle: Box::new(value.handle),
            root_hash: HashKey::from(value.root_hash),
        }
    }
}

impl From<Result<GetRevisionResult, api::Error>> for RevisionResult {
    fn from(value: Result<GetRevisionResult, api::Error>) -> Self {
        match value {
            Ok(res) => res.into(),
            Err(api::Error::RevisionNotFound { provided }) => RevisionResult::RevisionNotFound(
                HashKey::from(provided.unwrap_or_else(api::HashKey::empty)),
            ),
            Err(err) => RevisionResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

impl<'db, E: fmt::Display> From<Result<CreateProposalResult<'db>, E>> for ProposalResult<'db> {
    fn from(value: Result<CreateProposalResult<'db>, E>) -> Self {
        match value {
            Ok(CreateProposalResult { handle, .. }) => ProposalResult::Ok {
                root_hash: handle.hash_key().unwrap_or_default(),
                handle: Box::new(handle),
            },
            Err(err) => ProposalResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// Helper trait to handle the different result types returned from FFI functions.
///
/// Once Try trait is stable, we can use that instead of this trait:
///
/// ```ignore
/// impl std::ops::FromResidual<Option<std::convert::Infallible>> for VoidResult {
///     #[inline]
///     fn from_residual(residual: Option<std::convert::Infallible>) -> Self {
///         match residual {
///             None => VoidResult::NullHandlePointer,
///             // no other branches are needed because `std::convert::Infallible` is uninhabited
///             // this compiles without error because the compiler knows that Some(_) is impossible
///             // see: https://github.com/rust-lang/rust/blob/3fb1b53a9dbfcdf37a4b67d35cde373316829930/library/core/src/option.rs#L2627-L2631
///             // and: https://doc.rust-lang.org/nomicon/exotic-sizes.html#empty-types
///         }
///     }
/// }
/// ```
pub(crate) trait NullHandleResult: CResult {
    fn null_handle_pointer_error() -> Self;
}

pub(crate) trait CResult: Sized {
    fn from_err(err: impl ToString) -> Self;

    fn from_panic(panic: Box<dyn std::any::Any + Send>) -> Self
    where
        Self: Sized,
    {
        Self::from_err(Panic::from(panic))
    }
}

macro_rules! impl_null_handle_result {
    ($($Enum:ty),* $(,)?) => {
        $(
            impl NullHandleResult for $Enum {
                fn null_handle_pointer_error() -> Self {
                    Self::NullHandlePointer
                }
            }
        )*
    };
}

macro_rules! impl_cresult {
    ($($Enum:ty),* $(,)?) => {
        $(
            impl CResult for $Enum {
                fn from_err(err: impl ToString) -> Self {
                    Self::Err(err.to_string().into_bytes().into())
                }
            }
        )*
    };
}

impl_null_handle_result!(
    VoidResult,
    ValueResult,
    HashResult,
    RangeProofResult,
    ChangeProofResult,
    NextKeyRangeResult,
    ProposalResult<'_>,
    IteratorResult<'_>,
    RevisionResult,
    KeyValueResult,
);

impl_cresult!(
    VoidResult,
    ValueResult,
    HashResult,
    HandleResult,
    RangeProofResult,
    ChangeProofResult,
    NextKeyRangeResult,
    ProposalResult<'_>,
    IteratorResult<'_>,
    RevisionResult,
    KeyValueResult,
);

enum Panic {
    Static(&'static str),
    Formatted(String),
    SendSyncErr(Box<dyn std::error::Error + Send + Sync>),
    SendErr(Box<dyn std::error::Error + Send>),
    Unknown(#[expect(unused)] Box<dyn std::any::Any + Send>),
    // TODO: add variant to capture backtrace with panic hook
    // https://doc.rust-lang.org/stable/std/panic/fn.set_hook.html
}

impl From<Box<dyn std::any::Any + Send>> for Panic {
    fn from(panic: Box<dyn std::any::Any + Send>) -> Self {
        macro_rules! downcast {
            ($Variant:ident($panic:ident)) => {
                let $panic = match $panic.downcast() {
                    Ok(panic) => return Panic::$Variant(*panic),
                    Err(panic) => panic,
                };
            };
        }

        downcast!(Static(panic));
        downcast!(Formatted(panic));
        downcast!(SendSyncErr(panic));
        downcast!(SendErr(panic));

        Self::Unknown(panic)
    }
}

impl fmt::Display for Panic {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Panic::Static(msg) => f.pad(msg),
            Panic::Formatted(msg) => f.pad(msg),
            Panic::SendSyncErr(err) => err.fmt(f),
            Panic::SendErr(err) => err.fmt(f),
            Panic::Unknown(_) => f.pad("unknown panic type recovered"),
        }
    }
}
