// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::convert::Into;
use std::num::NonZeroUsize;

#[cfg(feature = "ethhash")]
use firewood_storage::{RlpList, TrieHash};

use firewood::{
    ProofError,
    api::{self, FrozenChangeProof},
};

use crate::{
    BorrowedBytes, ChangeProofResult, DatabaseHandle, HashKey, HashResult, KeyRange, Maybe,
    NextKeyRangeResult, OwnedBytes, ValueResult, VoidResult,
};

#[cfg(feature = "ethhash")]
const EMPTY_CODE_HASH: [u8; 32] = [
    // "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
    0xc5, 0xd2, 0x46, 0x01, 0x86, 0xf7, 0x23, 0x3c, 0x92, 0x7e, 0x7d, 0xb2, 0xdc, 0xc7, 0x03, 0xc0,
    0xe5, 0x00, 0xb6, 0x53, 0xca, 0x82, 0x27, 0x3b, 0x7b, 0xfa, 0xd8, 0x04, 0x5d, 0x85, 0xa4, 0x70,
];

/// Arguments for creating a change proof.
#[derive(Debug)]
#[repr(C)]
pub struct CreateChangeProofArgs<'a> {
    /// The root hash of the starting revision. This must be provided.
    /// If the root is not found in the database, the function will return
    /// [`ChangeProofResult::StartRevisionNotFound`].
    pub start_root: HashKey,
    /// The root hash of the ending revision. This must be provided.
    /// If the root is not found in the database, the function will return
    /// [`ChangeProofResult::EndRevisionNotFound`].
    pub end_root: HashKey,
    /// The start key of the range to create the proof for. If `None`, the range
    /// starts from the beginning of the keyspace.
    pub start_key: Maybe<BorrowedBytes<'a>>,
    /// The end key of the range to create the proof for. If `None`, the range
    /// ends at the end of the keyspace or until `max_length` items have been
    /// included in the proof.
    pub end_key: Maybe<BorrowedBytes<'a>>,
    /// The maximum number of key/value pairs to include in the proof. If the
    /// range contains more items than this, the proof will be truncated. If
    /// `0`, there is no limit.
    pub max_length: u32,
}

/// FFI context for a parsed or generated change proof. The proof is borrowed
/// (not consumed) during verification via `fwd_db_verify_change_proof`, so it
/// remains available for serialization and `find_next_key` afterward.
#[derive(Debug)]
pub struct ChangeProofContext {
    proof: Option<FrozenChangeProof>,
}

impl From<FrozenChangeProof> for ChangeProofContext {
    fn from(proof: FrozenChangeProof) -> Self {
        Self { proof: Some(proof) }
    }
}

impl ChangeProofContext {
    /// Returns the underlying proof, if present.
    #[must_use]
    pub const fn proof(&self) -> Option<&FrozenChangeProof> {
        self.proof.as_ref()
    }

    /// Returns the next key range to fetch for a change proof,
    /// or `None` if there are no more keys to fetch.
    ///
    /// Only inspects the proof structure — does not require a proposal.
    /// `end_key` is the original requested end key passed to the proof
    /// generator.
    fn find_next_key(&self, end_key: Option<&[u8]>) -> Result<Option<KeyRange>, api::Error> {
        let proof = self
            .proof
            .as_ref()
            .ok_or(api::Error::ProofError(ProofError::ProofIsNone))?;
        firewood::find_next_key_after_change_proof(proof, end_key)
    }
}

/// A key range that should be fetched to continue iterating through a range
/// or change proof that was truncated. Represents a half-open range
/// `[start_key, end_key)`. If `end_key` is `None`, the range is unbounded
/// and continues to the end of the keyspace.
#[derive(Debug)]
#[repr(C)]
pub struct NextKeyRange {
    /// The start key of the next range to fetch.
    pub start_key: OwnedBytes,

    /// If set, a non-inclusive upper bound for the next range to fetch. If not
    /// set, the range is unbounded (this is the final range).
    pub end_key: Maybe<OwnedBytes>,
}

#[derive(Debug)]
#[non_exhaustive]
pub struct CodeIteratorHandle<'a> {
    #[cfg(feature = "ethhash")]
    inner: std::slice::Iter<'a, KeyValuePair>,
    // uninhabitable fields make the struct impossible to construct when the feature is disabled
    #[cfg(not(feature = "ethhash"))]
    void: std::convert::Infallible,
    #[cfg(not(feature = "ethhash"))]
    marker: std::marker::PhantomData<&'a ()>,
}

type KeyValuePair = (Box<[u8]>, Box<[u8]>);

impl Iterator for CodeIteratorHandle<'_> {
    type Item = Result<HashKey, api::Error>;

    fn next(&mut self) -> Option<Self::Item> {
        #[cfg(not(feature = "ethhash"))]
        match self.void {}

        #[cfg(feature = "ethhash")]
        self.inner.find_map(|(key, value)| {
            if key.len() != 32 {
                return None;
            }

            let Ok(code_hash_slice) = RlpList::parse(value).and_then(|l| l.nth_bytes(3)) else {
                return Some(Err(api::Error::ProofError(ProofError::InvalidValueFormat)));
            };
            let code_hash: HashKey = TrieHash::try_from(code_hash_slice).ok()?.into();
            if code_hash == TrieHash::from(EMPTY_CODE_HASH).into() {
                return None;
            }

            Some(Ok(code_hash))
        })
    }
}

impl<'a> CodeIteratorHandle<'a> {
    /// Create a new code hash iterator from the given key/value pairs.
    /// The key/value pairs should be the raw entries from the
    /// underlying proof.
    ///
    /// The iterator must be freed after use.
    ///
    /// Arguments:
    /// - `key_values` - The key/value pairs from the proof.
    ///
    /// Returns:
    /// - `Ok(CodeIteratorHandle)` if the iterator was successfully created.
    /// - `Err(api::Error)` if the iterator could not be created.
    ///
    /// # Errors
    ///
    /// - Returns `api::Error::FeatureNotSupported` if the `ethhash` feature
    ///   is not enabled.
    #[cfg_attr(feature = "ethhash", allow(clippy::missing_const_for_fn))]
    #[cfg_attr(not(feature = "ethhash"), allow(unused_variables))]
    pub fn new(key_values: &'a [KeyValuePair]) -> Result<Self, api::Error> {
        #[cfg(not(feature = "ethhash"))]
        {
            Err(api::Error::FeatureNotSupported(
                "ethhash code hash iterator".to_owned(),
            ))
        }

        #[cfg(feature = "ethhash")]
        {
            Ok(CodeIteratorHandle {
                inner: key_values.iter(),
            })
        }
    }
}

/// Create a change proof for the given range of keys between two roots.
///
/// # Arguments
///
/// - `db` - The database to create the proof from.
/// - `args` - The arguments for creating the change proof.
///
/// # Returns
///
/// - [`ChangeProofResult::NullHandlePointer`] if the caller provided a null pointer.
/// - [`ChangeProofResult::StartRevisionNotFound`] if the caller provided a start root
///   that was not found in the database. The missing root hash is included in the result.
///   If both the start root and end root are missing, then only the end root is
///   reported.
/// - [`ChangeProofResult::EndRevisionNotFound`] if the caller provided an end root
///   that was not found in the database. The missing root hash is included in the result.
///   If both the start root and end root are missing, then only the end root is
///   reported.
/// - [`ChangeProofResult::Ok`] containing a pointer to the `ChangeProofContext` if the proof
///   was successfully created.
/// - [`ChangeProofResult::Err`] containing an error message if the proof could not be created.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_db_change_proof(
    db: Option<&DatabaseHandle>,
    args: CreateChangeProofArgs,
) -> ChangeProofResult {
    crate::invoke_with_handle(db, |db| {
        db.change_proof(
            args.start_root.into(),
            args.end_root.into(),
            args.start_key
                .as_ref()
                .map(BorrowedBytes::as_slice)
                .into_option(),
            args.end_key
                .as_ref()
                .map(BorrowedBytes::as_slice)
                .into_option(),
            NonZeroUsize::new(args.max_length as usize),
        )
    })
}

/// Verify a change proof and create a standard proposal.
///
/// Performs structural validation, applies batch ops to the latest
/// revision, and verifies the root hash against `end_root`. The proof is
/// borrowed, not consumed — the caller retains it for `find_next_key` or
/// serialization.
///
/// # Returns
///
/// - `ProposalResult::NullHandlePointer` if the caller provided a null
///   pointer to either the database or the proof.
/// - `ProposalResult::Ok` if verification succeeded and a proposal was
///   created.
/// - `ProposalResult::Err` containing an error message if verification
///   failed.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_db_verify_change_proof<'db>(
    db: Option<&'db DatabaseHandle>,
    proof: Option<&ChangeProofContext>,
    args: CreateChangeProofArgs<'_>,
) -> crate::ProposalResult<'db> {
    let handle = db.and_then(|db| proof.map(|p| (db, p)));
    crate::invoke_with_handle(handle, |(db, ctx)| {
        let proof = ctx
            .proof()
            .ok_or(api::Error::ProofError(ProofError::ProofIsNone))?;
        db.verify_change_proof(
            proof,
            args.end_root.into(),
            args.start_key.into_option().as_deref(),
            args.end_key.into_option().as_deref(),
            NonZeroUsize::new(args.max_length as usize),
        )
    })
}

/// Verify and commit a change proof in a single call.
///
/// Verifies structural validity and root hash, creates a proposal, and
/// commits it with automatic rebase if needed. The proof is borrowed,
/// not consumed — it remains available for `fwd_change_proof_find_next_key`
/// or serialization afterward.
///
/// # Returns
///
/// - [`HashResult::NullHandlePointer`] if the caller provided a null pointer
///   to either the database or the proof.
/// - [`HashResult::None`] if the trie has no root hash (merkledb mode only;
///   ethhash always returns a root hash, even for an empty trie).
/// - [`HashResult::Some`] containing the new root hash.
/// - [`HashResult::Err`] if verification or commit failed.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_db_verify_and_commit_change_proof(
    db: Option<&DatabaseHandle>,
    proof: Option<&ChangeProofContext>,
    args: CreateChangeProofArgs<'_>,
) -> HashResult {
    let handle = db.and_then(|db| proof.map(|p| (db, p)));
    crate::invoke_with_handle(handle, |(db, ctx)| {
        let proof = ctx
            .proof()
            .ok_or(api::Error::ProofError(ProofError::ProofIsNone))?;
        let proposal = db.verify_change_proof(
            proof,
            args.end_root.into(),
            args.start_key.into_option().as_deref(),
            args.end_key.into_option().as_deref(),
            NonZeroUsize::new(args.max_length as usize),
        )?;
        proposal.handle.commit_proposal_with_rebase()
    })
}

/// Determine the next key range to fetch for a change proof.
///
/// The proof is not consumed by this call. `end_key` is the original
/// requested end key passed to the proof generator.
///
/// Returns:
/// - [`NextKeyRangeResult::None`] if there are no more keys to fetch.
/// - [`NextKeyRangeResult::Some`] containing the next key range to fetch.
/// - [`NextKeyRangeResult::Err`] if an error occurred.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_change_proof_find_next_key(
    proof: Option<&ChangeProofContext>,
    end_key: Maybe<BorrowedBytes>,
) -> NextKeyRangeResult {
    crate::invoke_with_handle(proof, |ctx| {
        ctx.find_next_key(end_key.into_option().as_deref())
    })
}

fn serialize_change_proof(
    proof: Option<&FrozenChangeProof>,
) -> Result<Option<Box<[u8]>>, api::Error> {
    let proof = proof.ok_or(api::Error::ProofError(ProofError::ProofIsNone))?;
    let mut vec = Vec::new();
    proof.write_to_vec(&mut vec);
    Ok(Some(vec.into_boxed_slice()))
}

/// Serialize a `ChangeProof` to bytes.
///
/// # Arguments
///
/// - `proof` - A [`ChangeProofContext`] previously returned from the create
///   method.
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the caller provided a null pointer.
/// - [`ValueResult::Some`] containing the serialized bytes if successful.
/// - [`ValueResult::Err`] if serialization failed.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_change_proof_to_bytes(proof: Option<&ChangeProofContext>) -> ValueResult {
    crate::invoke_with_handle(proof, |ctx| serialize_change_proof(ctx.proof.as_ref()))
}

/// Deserialize a `ChangeProof` from bytes.
///
/// # Arguments
///
/// * `bytes` - The bytes to deserialize the proof from.
///
/// # Returns
///
/// - [`ChangeProofResult::NullHandlePointer`] if the caller provided a null or zero-length slice.
/// - [`ChangeProofResult::Ok`] containing a pointer to the `ChangeProofContext` if the proof
///   was successfully parsed. This does not imply that the proof is valid, only that it is
///   well-formed. Use [`fwd_db_verify_change_proof`] or
///   [`fwd_db_verify_and_commit_change_proof`] to verify the proof.
/// - [`ChangeProofResult::Err`] containing an error message if the proof could not be parsed.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_change_proof_from_bytes(bytes: BorrowedBytes) -> ChangeProofResult {
    crate::invoke(move || {
        FrozenChangeProof::from_slice(&bytes)
            .map_err(|err| api::Error::ProofError(ProofError::Deserialization(err)))
    })
}

/// Frees the memory associated with a `ChangeProofContext`.
///
/// # Arguments
///
/// * `proof` - The `ChangeProofContext` to free, previously returned from any Rust function.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the memory was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_free_change_proof(proof: Option<Box<ChangeProofContext>>) -> VoidResult {
    crate::invoke_with_handle(proof, drop)
}

impl crate::MetricsContextExt for ChangeProofContext {
    fn metrics_context(&self) -> Option<firewood_metrics::MetricsContext> {
        None
    }
}

impl crate::MetricsContextExt for (&DatabaseHandle, &ChangeProofContext) {
    fn metrics_context(&self) -> Option<firewood_metrics::MetricsContext> {
        self.0.metrics_context()
    }
}

impl crate::MetricsContextExt for CodeIteratorHandle<'_> {
    fn metrics_context(&self) -> Option<firewood_metrics::MetricsContext> {
        None
    }
}
