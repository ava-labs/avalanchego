// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::num::NonZeroUsize;

use firewood::{
    logger::warn,
    v2::api::{self, DbView, FrozenRangeProof, HashKey},
};

use crate::{
    BorrowedBytes, DatabaseHandle, HashResult, Maybe, NextKeyRange, NextKeyRangeResult,
    RangeProofResult, ValueResult, VoidResult,
};

/// Arguments for creating a range proof.
#[derive(Debug)]
#[repr(C)]
pub struct CreateRangeProofArgs<'a> {
    /// The root hash of the revision to prove.
    pub root: crate::HashKey,
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
pub struct VerifyRangeProofArgs<'a, 'db> {
    /// The range proof to verify. If null, the function will return
    /// [`VoidResult::NullHandlePointer`]. We need a mutable reference to
    /// update the validation context.
    pub proof: Option<&'a mut RangeProofContext<'db>>,
    /// The root hash to verify the proof against. This must match the calculated
    /// hash of the root of the proof.
    pub root: crate::HashKey,
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
pub struct RangeProofContext<'db> {
    proof: FrozenRangeProof,
    state: RangeProofState<'db>,
}

#[derive(Debug)]
enum RangeProofState<'db> {
    Unverified,
    Verified,
    Proposed(crate::ProposalHandle<'db>),
    Committed(Option<HashKey>),
}

impl From<FrozenRangeProof> for RangeProofContext<'_> {
    fn from(proof: FrozenRangeProof) -> Self {
        Self {
            proof,
            state: RangeProofState::Unverified,
        }
    }
}

impl<'db> RangeProofState<'db> {
    const fn is_verified(&self) -> bool {
        !matches!(self, Self::Unverified)
    }

    const fn is_proposed_or_committed(&self) -> bool {
        matches!(self, Self::Proposed(_) | Self::Committed(_))
    }

    const fn is_committed(&self) -> bool {
        matches!(self, Self::Committed(_))
    }

    fn take_proposal(&mut self) -> Option<crate::ProposalHandle<'db>> {
        match std::mem::replace(self, Self::Unverified) {
            Self::Proposed(proposal) => Some(proposal),
            other => {
                // put it back (so we don't need to write unsafe/panicking code)
                *self = other;
                None
            }
        }
    }

    fn root_hash(&self) -> Result<Option<HashKey>, api::Error> {
        match self {
            Self::Committed(root) => Ok(root.clone()),
            Self::Proposed(proposal) => proposal.root_hash(),
            _ => Ok(None),
        }
    }
}

impl<'db> RangeProofContext<'db> {
    fn verify(
        &mut self,
        _root: HashKey,
        _start_key: Option<&[u8]>,
        _end_key: Option<&[u8]>,
        _max_length: Option<NonZeroUsize>,
    ) -> Result<(), api::Error> {
        if !self.state.is_verified() {
            return Ok(());
        }

        warn!("range proof verification not yet implemented");
        self.state = RangeProofState::Verified;
        Ok(())
    }

    fn verify_and_propose(
        &mut self,
        db: &'db crate::DatabaseHandle,
        root: HashKey,
        start_key: Option<&[u8]>,
        end_key: Option<&[u8]>,
        max_length: Option<NonZeroUsize>,
    ) -> Result<(), api::Error> {
        if self.state.is_proposed_or_committed() {
            return Ok(());
        }

        self.verify(root, start_key, end_key, max_length)?;

        let proposal = db.merge_key_value_range(start_key, end_key, self.proof.key_values())?;
        self.state = RangeProofState::Proposed(proposal.handle);

        Ok(())
    }

    fn verify_and_commit(
        &mut self,
        db: &'db crate::DatabaseHandle,
        root: HashKey,
        start_key: Option<&[u8]>,
        end_key: Option<&[u8]>,
        max_length: Option<NonZeroUsize>,
    ) -> Result<Option<HashKey>, api::Error> {
        if self.state.is_committed() {
            return self.state.root_hash();
        }

        self.verify(root, start_key, end_key, max_length)?;

        let proposal_handle = if let Some(proposal) = self.state.take_proposal() {
            proposal
        } else {
            db.merge_key_value_range(start_key, end_key, self.proof.key_values())?
                .handle
        };

        let hash = proposal_handle.commit_proposal(|commit_time| {
            metrics::counter!("firewood.ffi.commit_ms").increment(commit_time.as_millis());
            metrics::counter!("firewood.ffi.merge").increment(1);
        })?;

        self.state = RangeProofState::Committed(hash.clone());
        Ok(hash)
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
) -> RangeProofResult<'static> {
    // static lifetime is safe because the returned `RangeProofResult` does not
    // retain a reference to the provided database handle.

    crate::invoke_with_handle(db, |db| {
        let view = db.get_root(args.root.into())?;
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
pub extern "C" fn fwd_range_proof_verify(args: VerifyRangeProofArgs) -> VoidResult {
    let VerifyRangeProofArgs {
        proof,
        root,
        start_key,
        end_key,
        max_length,
    } = args;

    crate::invoke_with_handle(proof, |ctx| {
        let start_key = start_key.into_option();
        let end_key = end_key.into_option();
        ctx.verify(
            root.into(),
            start_key.as_deref(),
            end_key.as_deref(),
            NonZeroUsize::new(max_length as usize),
        )
    })
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
pub extern "C" fn fwd_db_verify_range_proof<'db>(
    db: Option<&'db DatabaseHandle>,
    args: VerifyRangeProofArgs<'_, 'db>,
) -> VoidResult {
    let VerifyRangeProofArgs {
        proof,
        root,
        start_key,
        end_key,
        max_length,
    } = args;

    let handle = db.and_then(|db| proof.map(|p| (db, p)));

    crate::invoke_with_handle(handle, |(db, ctx)| {
        let start_key = start_key.into_option();
        let end_key = end_key.into_option();
        ctx.verify_and_propose(
            db,
            root.into(),
            start_key.as_deref(),
            end_key.as_deref(),
            NonZeroUsize::new(max_length as usize),
        )
    })
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
pub extern "C" fn fwd_db_verify_and_commit_range_proof<'db>(
    db: Option<&'db DatabaseHandle>,
    args: VerifyRangeProofArgs<'_, 'db>,
) -> HashResult {
    let VerifyRangeProofArgs {
        proof,
        root,
        start_key,
        end_key,
        max_length,
    } = args;

    let handle = db.and_then(|db| proof.map(|p| (db, p)));

    crate::invoke_with_handle(handle, |(db, ctx)| {
        let start_key = start_key.into_option();
        let end_key = end_key.into_option();
        ctx.verify_and_commit(
            db,
            root.into(),
            start_key.as_deref(),
            end_key.as_deref(),
            NonZeroUsize::new(max_length as usize),
        )
    })
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
    proof: Option<&mut RangeProofContext>,
) -> NextKeyRangeResult {
    // TODO(#352): proper implementation, this naively retuns the last key in
    // in the range, which is correct, but not sufficient.
    crate::invoke_with_handle(proof, |ctx| match ctx.state {
        RangeProofState::Unverified | RangeProofState::Verified => NextKeyRangeResult::NotPrepared,
        RangeProofState::Proposed(_) | RangeProofState::Committed(_) => {
            if ctx.proof.end_proof().is_empty() {
                // unbounded, so we are done
                NextKeyRangeResult::None
            } else {
                match ctx.proof.key_values().last() {
                    Some((key, _)) => NextKeyRangeResult::Some(NextKeyRange {
                        start_key: key.clone().into(),
                        end_key: None.into(),
                    }),
                    None => {
                        // no key-values in the proof, so we are done
                        NextKeyRangeResult::None
                    }
                }
            }
        }
    })
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
pub extern "C" fn fwd_range_proof_from_bytes(
    bytes: BorrowedBytes<'_>,
) -> RangeProofResult<'static> {
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
