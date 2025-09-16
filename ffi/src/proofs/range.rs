// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::num::NonZeroUsize;

use firewood::v2::api::{self, FrozenRangeProof};

use crate::{
    BorrowedBytes, CResult, DatabaseHandle, HashResult, Maybe, NextKeyRangeResult,
    RangeProofResult, ValueResult, VoidResult,
};

/// Arguments for creating a range proof.
#[derive(Debug)]
#[repr(C)]
pub struct CreateRangeProofArgs<'a> {
    /// The root hash of the revision to prove. If `None`, the latest revision
    /// is used.
    pub root: Maybe<BorrowedBytes<'a>>,
    /// The start key of the range to prove. If `None`, the range starts from the
    /// beginning of the keyspace.
    ///
    /// The start key must be less than the end key if both are provided.
    pub start_key: Maybe<BorrowedBytes<'a>>,
    /// The end key of the range to prove. If `None`, the range ends at the end
    /// of the keyspace or until `max_length` items have been been included in
    /// the proof.
    ///
    /// If provided, end key is inclusive if not truncated. Otherwise, the end
    /// key will be the final key in the returned key-value pairs.
    pub end_key: Maybe<BorrowedBytes<'a>>,
    /// The maximum number of key/value pairs to include in the proof. If the
    /// range contains more items than this, the proof will be truncated. If
    /// `0`, there is no limit.
    pub max_length: u32,
}

/// Arguments for verifying a range proof.
#[derive(Debug)]
#[repr(C)]
pub struct VerifyRangeProofArgs<'a> {
    /// The range proof to verify. If null, the function will return
    /// [`VoidResult::NullHandlePointer`]. We need a mutable reference to
    /// update the validation context.
    pub proof: Option<&'a mut RangeProofContext>,
    /// The root hash to verify the proof against. This must match the calculated
    /// hash of the root of the proof.
    pub root: BorrowedBytes<'a>,
    /// The lower bound of the key range that the proof is expected to cover. If
    /// `None`, the proof is expected to cover from the start of the keyspace.
    ///
    /// Must be present if the range proof contains a lower bound proof and must
    /// be absent if the range proof does not contain a lower bound proof.
    pub start_key: Maybe<BorrowedBytes<'a>>,
    /// The upper bound of the key range that the proof is expected to cover. If
    /// `None`, the proof is expected to cover to the end of the keyspace.
    ///
    /// This is ignored if the proof is truncated and does not cover the full,
    /// in which case the upper bound key is the final key in the key-value pairs.
    pub end_key: Maybe<BorrowedBytes<'a>>,
    /// The maximum number of key/value pairs that the proof is expected to cover.
    /// If the proof contains more items than this, it is considered invalid. If
    /// `0`, there is no limit.
    pub max_length: u32,
}

/// FFI context for for a parsed or generated range proof.
#[derive(Debug)]
pub struct RangeProofContext {
    proof: FrozenRangeProof,
    /// Information about the proof discovered during verification that does not
    /// need to be recomputed. Also serves as a token that ensured we have
    /// validated the proof and can skip it during commit.
    _validation_context: (), // placeholder for future use
    /// Information about the proof after it has been committed to the DB. This
    /// allows for easy introspection of the specific revision that was committed
    /// and is needed to optimize discovery of the next key/range as well as
    /// other introspective optimizations.
    _commit_context: (), // placeholder for future use
}

impl From<FrozenRangeProof> for RangeProofContext {
    fn from(proof: FrozenRangeProof) -> Self {
        Self {
            proof,
            _validation_context: (),
            _commit_context: (),
        }
    }
}

/// Generate a range proof for the given range of keys for the latest revision.
///
/// # Arguments
///
/// - `db` - The database to create the proof from.
/// - `args` - The arguments for creating the range proof.
///
/// # Returns
///
/// - [`RangeProofResult::NullHandlePointer`] if the caller provided a null pointer.
/// - [`RangeProofResult::RevisionNotFound`] if the caller provided a root that was
///   not found in the database. The missing root hash is included in the result.
/// - [`RangeProofResult::Ok`] containing a pointer to the `RangeProofContext` if the proof
///   was successfully created.
/// - [`RangeProofResult::Err`] containing an error message if the proof could not be created.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_db_range_proof(
    db: Option<&DatabaseHandle>,
    args: CreateRangeProofArgs,
) -> RangeProofResult {
    crate::invoke_with_handle(db, |db| {
        let root_hash = match args.root {
            Maybe::Some(root) => root.as_ref().try_into()?,
            Maybe::None => db
                .current_root_hash()?
                .ok_or(api::Error::RangeProofOnEmptyTrie)?,
        };

        let view = db.get_root(root_hash)?;
        view.range_proof(
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

/// Verify a range proof against the given start and end keys and root hash. The
/// proof will be updated with the validation context if the proof is valid to
/// avoid re-verifying it during commit.
///
/// # Arguments
///
/// - `args` - The arguments for verifying the range proof.
///
/// # Returns
///
/// - [`VoidResult::NullHandlePointer`] if the caller provided a null pointer to the proof.
/// - [`VoidResult::Ok`] if the proof was successfully verified.
/// - [`VoidResult::Err`] containing an error message if the proof could not be verified.
///
/// # Thread Safety
///
/// It is not safe to call this function concurrently with the same proof context
/// nor is it safe to call any other function that accesses the same proof context
/// concurrently. The caller must ensure exclusive access to the proof context
/// for the duration of the call.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_range_proof_verify(_args: VerifyRangeProofArgs) -> VoidResult {
    CResult::from_err("not yet implemented")
}

/// Verify a range proof and prepare a proposal to later commit or drop. If the
/// proof has already been verified, the cached validation context will be used
/// to avoid re-verifying the proof.
///
/// # Arguments
///
/// - `db` - The database to verify the proof against.
/// - `args` - The arguments for verifying the range proof.
///
/// # Returns
///
/// - [`VoidResult::NullHandlePointer`] if the caller provided a null pointer to either
///   the database or the proof.
/// - [`VoidResult::Ok`] if the proof was successfully verified.
/// - [`VoidResult::Err`] containing an error message if the proof could not be verified
///
/// # Thread Safety
///
/// It is not safe to call this function concurrently with the same proof context
/// nor is it safe to call any other function that accesses the same proof context
/// concurrently. The caller must ensure exclusive access to the proof context
/// for the duration of the call.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_db_verify_range_proof(
    _db: Option<&DatabaseHandle>,
    _args: VerifyRangeProofArgs,
) -> VoidResult {
    CResult::from_err("not yet implemented")
}

/// Verify and commit a range proof to the database.
///
/// If a proposal was previously prepared by a call to [`fwd_db_verify_range_proof`],
/// it will be committed instead of re-verifying the proof. If the proof has not yet
/// been verified, it will be verified now. If the prepared proposal is no longer
/// valid (e.g., the database has changed since it was prepared), a new proposal
/// will be created and committed.
///
/// The proof context will be updated with additional information about the committed
/// proof to allow for optimized introspection of the committed changes.
///
/// # Arguments
///
/// - `db` - The database to commit the changes to.
/// - `args` - The arguments for verifying the range proof.
///
/// # Returns
///
/// - [`HashResult::NullHandlePointer`] if the caller provided a null pointer to either
///   the database or the proof.
/// - [`HashResult::None`] if the proof resulted in an empty database (i.e., all keys were deleted).
/// - [`HashResult::Some`] containing the new root hash if the proof was successfully verified
/// - [`HashResult::Err`] containing an error message if the proof could not be verified or committed.
///
/// # Thread Safety
///
/// It is not safe to call this function concurrently with the same proof context
/// nor is it safe to call any other function that accesses the same proof context
/// concurrently. The caller must ensure exclusive access to the proof context
/// for the duration of the call.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_db_verify_and_commit_range_proof(
    _db: Option<&DatabaseHandle>,
    _args: VerifyRangeProofArgs,
) -> HashResult {
    CResult::from_err("not yet implemented")
}

/// Returns the next key range that should be fetched after processing the
/// current set of key-value pairs in a range proof that was truncated.
///
/// Can be called multiple times to get subsequent disjoint key ranges until
/// it returns [`NextKeyRangeResult::None`], indicating there are no more keys to
/// fetch and the proof is complete.
///
/// # Arguments
///
/// - `proof` - A [`RangeProofContext`] previously returned from the create
///   methods and has been prepared into a proposal or already committed.
///
/// # Returns
///
/// - [`NextKeyRangeResult::NullHandlePointer`] if the caller provided a null pointer.
/// - [`NextKeyRangeResult::NotPrepared`] if the proof has not been prepared into
///   a proposal nor committed to the database.
/// - [`NextKeyRangeResult::None`] if there are no more keys to fetch.
/// - [`NextKeyRangeResult::Some`] containing the next key range to fetch.
/// - [`NextKeyRangeResult::Err`] containing an error message if the next key range
///   could not be determined.
///
/// # Thread Safety
///
/// It is not safe to call this function concurrently with the same proof context
/// nor is it safe to call any other function that accesses the same proof context
/// concurrently. The caller must ensure exclusive access to the proof context
/// for the duration of the call.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_range_proof_find_next_key(
    _proof: Option<&mut RangeProofContext>,
) -> NextKeyRangeResult {
    CResult::from_err("not yet implemented")
}

/// Serialize a `RangeProof` to bytes.
///
/// # Arguments
///
/// - `proof` - A [`RangeProofContext`] previously returned from the create
///   method. If from a parsed proof, the proof will not be verified before
///   serialization.
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the caller provided a null pointer.
/// - [`ValueResult::Some`] containing the serialized bytes if successful.
/// - [`ValueResult::Err`] if the caller provided a null pointer.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_range_proof_to_bytes(proof: Option<&RangeProofContext>) -> ValueResult {
    crate::invoke_with_handle(proof, |ctx| {
        let mut vec = Vec::new();
        ctx.proof.write_to_vec(&mut vec);
        vec
    })
}

/// Deserialize a `RangeProof` from bytes.
///
/// # Arguments
///
/// - `bytes` - The bytes to deserialize the proof from.
///
/// # Returns
///
/// - [`RangeProofResult::NullHandlePointer`] if the caller provided a null or zero-length slice.
/// - [`RangeProofResult::Ok`] containing a pointer to the `RangeProofContext` if the proof
///   was successfully parsed. This does not imply that the proof is valid, only that it is
///   well-formed. The verify method must be called to ensure the proof is cryptographically valid.
/// - [`RangeProofResult::Err`] containing an error message if the proof could not be parsed.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_range_proof_from_bytes(bytes: BorrowedBytes) -> RangeProofResult {
    crate::invoke(move || {
        FrozenRangeProof::from_slice(&bytes).map_err(|err| {
            api::Error::ProofError(firewood::proof::ProofError::Deserialization(err))
        })
    })
}

/// Frees the memory associated with a `RangeProofContext`.
///
/// # Arguments
///
/// * `proof` - The `RangeProofContext` to free, previously returned from any Rust function.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the memory was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_free_range_proof(proof: Option<Box<RangeProofContext>>) -> VoidResult {
    crate::invoke_with_handle(proof, drop)
}
