// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// HINT WHEN REFERENCING TYPES OUTSIDE THIS LIBRARY:
// - Anything that is outside the crate must be included as a `type` alias (not just
//   a `use`) in order for cbindgen to generate an opaque forward declaration. The type
//   alias can have a doc comment which will be included in the generated header file.
// - The value must be boxed, or otherwise used via a pointer. This is because only
//   a forward declaration is generated and callers will be unable to instantiate the
//   type without a complete definition.

#![doc = include_str!("../README.md")]
#![expect(
    unsafe_code,
    reason = "This is an FFI library, so unsafe code is expected."
)]
#![cfg_attr(
    not(target_pointer_width = "64"),
    forbid(
        clippy::cast_possible_truncation,
        reason = "non-64 bit target likely to cause issues during u64 to usize conversions"
    )
)]

mod arc_cache;
mod handle;
mod logging;
mod metrics_setup;
mod proofs;
mod proposal;
mod value;

use firewood::v2::api::DbView;

pub use crate::handle::*;
pub use crate::logging::*;
pub use crate::proofs::*;
pub use crate::proposal::*;
pub use crate::value::*;

#[cfg(unix)]
#[global_allocator]
#[doc(hidden)]
static GLOBAL: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

/// Invokes a closure and returns the result as a [`CResult`].
///
/// If the closure panics, it will return [`CResult::from_panic`] with the panic
/// information.
#[inline]
fn invoke<T: CResult, V: Into<T>>(once: impl FnOnce() -> V) -> T {
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(once)) {
        Ok(result) => result.into(),
        Err(panic) => T::from_panic(panic),
    }
}

/// Invokes a closure that requires a handle and returns the result as a [`NullHandleResult`].
///
/// If the provided handle is [`None`], the function will return early with the
/// [`NullHandleResult::null_handle_pointer_error`] result.
///
/// Otherwise, the closure is invoked with the handle. If the closure panics,
/// it will be caught and returned as a [`CResult::from_panic`].
#[inline]
fn invoke_with_handle<H, T: NullHandleResult, V: Into<T>>(
    handle: Option<H>,
    once: impl FnOnce(H) -> V,
) -> T {
    match handle {
        Some(handle) => invoke(move || once(handle)),
        None => T::null_handle_pointer_error(),
    }
}

/// Gets the value associated with the given key from the database for the
/// latest revision.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`]
/// * `key` - The key to look up as a [`BorrowedBytes`]
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the provided database handle is null.
/// - [`ValueResult::RevisionNotFound`] if no revision was found for the root
///   (i.e., there is no current root).
/// - [`ValueResult::None`] if the key was not found.
/// - [`ValueResult::Some`] if the key was found with the associated value.
/// - [`ValueResult::Err`] if an error occurred while retrieving the value.
///
/// # Safety
///
/// The caller must:
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`].
/// * ensure that `key` is valid for [`BorrowedBytes`]
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error or value.
///
/// [`BorrowedBytes`]: crate::value::BorrowedBytes
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_latest(
    db: Option<&DatabaseHandle>,
    key: BorrowedBytes,
) -> ValueResult {
    invoke_with_handle(db, move |db| db.get_latest(key))
}

/// Gets the value associated with the given key from the proposal provided.
///
/// # Arguments
///
/// * `handle` - The proposal handle returned by [`fwd_propose_on_db`] or
///   [`fwd_propose_on_proposal`].
/// * `key` - The key to look up, as a [`BorrowedBytes`].
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the provided database handle is null.
/// - [`ValueResult::None`] if the key was not found.
/// - [`ValueResult::Some`] if the key was found with the associated value.
/// - [`ValueResult::Err`] if an error occurred while retrieving the value.
///
/// # Safety
///
/// The caller must:
/// * ensure that `handle` is a valid pointer to a [`ProposalHandle`]
/// * ensure that `key` is valid for [`BorrowedBytes`]
/// * call [`fwd_free_owned_bytes`] to free the memory associated [`OwnedBytes`]
///   returned in the result.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_from_proposal(
    handle: Option<&ProposalHandle<'_>>,
    key: BorrowedBytes,
) -> ValueResult {
    invoke_with_handle(handle, move |handle| handle.val(key))
}

/// Gets a value assoicated with the given root hash and key.
///
/// The hash may refer to a historical revision or an existing proposal.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`]
/// * `root` - The root hash to look up as a [`BorrowedBytes`]
/// * `key` - The key to look up as a [`BorrowedBytes`]
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the provided database handle is null.
/// - [`ValueResult::RevisionNotFound`] if no revision was found for the specified root.
/// - [`ValueResult::None`] if the key was not found.
/// - [`ValueResult::Some`] if the key was found with the associated value.
/// - [`ValueResult::Err`] if an error occurred while retrieving the value.
///
/// # Safety
///
/// The caller must:
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`]
/// * ensure that `root` is a valid for [`BorrowedBytes`]
/// * ensure that `key` is a valid for [`BorrowedBytes`]
/// * call [`fwd_free_owned_bytes`] to free the memory associated [`OwnedBytes`]
///   returned in the result.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_from_root(
    db: Option<&DatabaseHandle>,
    root: BorrowedBytes,
    key: BorrowedBytes,
) -> ValueResult {
    invoke_with_handle(db, move |db| {
        db.get_from_root(root.as_ref().try_into()?, key)
    })
}

/// Puts the given key-value pairs into the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`]
/// * `values` - A [`BorrowedKeyValuePairs`] containing the key-value pairs to put.
///
/// # Returns
///
/// - [`HashResult::NullHandlePointer`] if the provided database handle is null.
/// - [`HashResult::None`] if the commit resulted in an empty database.
/// - [`HashResult::Some`] if the commit was successful, containing the new root hash.
/// - [`HashResult::Err`] if an error occurred while committing the batch.
///
/// # Safety
///
/// The caller must:
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`]
/// * ensure that `values` is valid for [`BorrowedKeyValuePairs`]
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error ([`HashKey`] does not need to be freed as it is returned by
///   value).
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_batch(
    db: Option<&DatabaseHandle>,
    values: BorrowedKeyValuePairs<'_>,
) -> HashResult {
    invoke_with_handle(db, move |db| db.create_batch(values))
}

/// Proposes a batch of operations to the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`]
/// * `values` - A [`BorrowedKeyValuePairs`] containing the key-value pairs to put.
///
/// # Returns
///
/// - [`ProposalResult::NullHandlePointer`] if the provided database handle is null.
/// - [`ProposalResult::Ok`] if the proposal was created, with the proposal handle
///   and calculated root hash.
/// - [`ProposalResult::Err`] if an error occurred while creating the proposal.
///
/// # Safety
///
/// The caller must:
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`]
/// * ensure that `values` is valid for [`BorrowedKeyValuePairs`]
/// * call [`fwd_commit_proposal`] or [`fwd_free_proposal`] to free the memory
///   associated with the proposal. And, the caller must ensure this is done
///   before calling [`fwd_close_db`] to avoid memory leaks or undefined behavior.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_propose_on_db<'db>(
    db: Option<&'db DatabaseHandle>,
    values: BorrowedKeyValuePairs<'_>,
) -> ProposalResult<'db> {
    invoke_with_handle(db, move |db| db.create_proposal_handle(values))
}

/// Proposes a batch of operations to the database on top of an existing proposal.
///
/// # Arguments
///
/// * `handle` - The proposal handle returned by [`fwd_propose_on_db`] or
///   [`fwd_propose_on_proposal`].
/// * `values` - A [`BorrowedKeyValuePairs`] containing the key-value pairs to put.
///
/// # Returns
///
/// - [`ProposalResult::NullHandlePointer`] if the provided database handle is null.
/// - [`ProposalResult::Ok`] if the proposal was created, with the proposal handle
///   and calculated root hash.
/// - [`ProposalResult::Err`] if an error occurred while creating the proposal.
///
/// # Safety
///
/// The caller must:
/// * ensure that `handle` is a valid pointer to a [`ProposalHandle`]
/// * ensure that `values` is valid for [`BorrowedKeyValuePairs`]
/// * call [`fwd_commit_proposal`] or [`fwd_free_proposal`] to free the memory
///   associated with the proposal. And, the caller must ensure this is done
///   before calling [`fwd_close_db`] to avoid memory leaks or undefined behavior.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_propose_on_proposal<'db>(
    handle: Option<&ProposalHandle<'db>>,
    values: BorrowedKeyValuePairs<'_>,
) -> ProposalResult<'db> {
    invoke_with_handle(handle, move |p| p.create_proposal_handle(values))
}

/// Commits a proposal to the database.
///
/// This function will consume the proposal regardless of whether the commit
/// is successful.
///
/// # Arguments
///
/// * `handle` - The proposal handle returned by [`fwd_propose_on_db`] or
///   [`fwd_propose_on_proposal`].
///
/// # Returns
///
/// # Returns
///
/// - [`HashResult::NullHandlePointer`] if the provided database handle is null.
/// - [`HashResult::None`] if the commit resulted in an empty database.
/// - [`HashResult::Some`] if the commit was successful, containing the new root hash.
/// - [`HashResult::Err`] if an error occurred while committing the batch.
///
/// # Safety
///
/// The caller must:
/// * ensure that `handle` is a valid pointer to a [`ProposalHandle`]
/// * ensure that `handle` is not used again after this function is called.
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error ([`HashKey`] does not need to be freed as it is returned
///   by value).
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_commit_proposal(
    proposal: Option<Box<ProposalHandle<'_>>>,
) -> HashResult {
    invoke_with_handle(proposal, move |proposal| {
        proposal.commit_proposal(|commit_time| {
            metrics::counter!("firewood.ffi.commit_ms").increment(commit_time.as_millis());
            metrics::counter!("firewood.ffi.commit").increment(1);
        })
    })
}

/// Consumes the [`ProposalHandle`], cancels the proposal, and frees the memory.
///
/// # Arguments
///
/// * `proposal` - A pointer to a [`ProposalHandle`] previously returned from a
///   function from this library.
///
/// # Returns
///
/// - [`VoidResult::NullHandlePointer`] if the provided proposal handle is null.
/// - [`VoidResult::Ok`] if the proposal was successfully cancelled and freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
///
/// # Safety
///
/// The caller must ensure that the `proposal` is not null and that it points to
/// a valid [`ProposalHandle`] previously returned by a function from this library.
///
/// The caller must ensure that the proposal was not committed. [`fwd_commit_proposal`]
/// will consume the proposal automatically.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_free_proposal(
    proposal: Option<Box<ProposalHandle<'_>>>,
) -> VoidResult {
    invoke_with_handle(proposal, drop)
}

/// Get the root hash of the latest version of the database
///
/// # Argument
///
/// * `db` - The database handle returned by [`fwd_open_db`]
///
/// # Returns
///
/// - [`HashResult::NullHandlePointer`] if the provided database handle is null.
/// - [`HashResult::None`] if the database is empty.
/// - [`HashResult::Some`] with the root hash of the database.
/// - [`HashResult::Err`] if an error occurred while looking up the root hash.
///
/// # Safety
///
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`]
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error ([`HashKey`] does not need to be freed as it is returned
///   by value).
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_root_hash(db: Option<&DatabaseHandle>) -> HashResult {
    invoke_with_handle(db, DatabaseHandle::current_root_hash)
}

/// Start metrics recorder for this process.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the recorder was initialized.
/// - [`VoidResult::Err`] if an error occurs during initialization.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_start_metrics() -> VoidResult {
    invoke(metrics_setup::setup_metrics)
}

/// Start metrics recorder and exporter for this process.
///
/// # Arguments
///
/// * `metrics_port` - the port where metrics will be exposed at
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the recorder was initialized.
/// - [`VoidResult::Err`] if an error occurs during initialization.
///
/// # Safety
///
/// The caller must:
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error (if any).
#[unsafe(no_mangle)]
pub extern "C" fn fwd_start_metrics_with_exporter(metrics_port: u16) -> VoidResult {
    invoke(move || metrics_setup::setup_metrics_with_exporter(metrics_port))
}

/// Gather latest metrics for this process.
///
/// # Returns
///
/// - [`ValueResult::None`] if the gathered metrics resulted in an empty string.
/// - [`ValueResult::Some`] the gathered metrics as an [`OwnedBytes`] (with
///   guaranteed to be utf-8 data, not null terminated).
/// - [`ValueResult::Err`] if an error occurred while retrieving the value.
///
/// # Safety
///
/// The caller must:
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error or value.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_gather() -> ValueResult {
    invoke(metrics_setup::gather_metrics)
}

/// Open a database with the given arguments.
///
/// # Arguments
///
/// See [`DatabaseHandleArgs`].
///
/// # Returns
///
/// - [`HandleResult::Ok`] with the database handle if successful.
/// - [`HandleResult::Err`] if an error occurs while opening the database.
///
/// # Safety
///
/// The caller must:
/// - ensure that the database is freed with [`fwd_close_db`] when no longer needed.
/// - ensure that the database handle is freed only after freeing or committing
///   all proposals created on it.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_open_db(args: DatabaseHandleArgs) -> HandleResult {
    invoke(move || DatabaseHandle::new(args))
}

/// Start logs for this process.
///
/// # Arguments
///
/// See [`LogArgs`].
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the recorder was initialized.
/// - [`VoidResult::Err`] if an error occurs during initialization.
///
/// # Safety
///
/// The caller must:
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error (if any).
#[unsafe(no_mangle)]
pub extern "C" fn fwd_start_logs(args: LogArgs) -> VoidResult {
    invoke(move || args.start_logging())
}

/// Close and free the memory for a database handle
///
/// # Arguments
///
/// * `db` - The database handle to close, previously returned from a call to [`fwd_open_db`].
///
/// # Returns
///
/// - [`VoidResult::NullHandlePointer`] if the provided database handle is null.
/// - [`VoidResult::Ok`] if the database handle was successfully closed and freed.
/// - [`VoidResult::Err`] if the process panics while closing the database handle.
///
/// # Safety
///
/// Callers must ensure that:
///
/// - `db` is a valid pointer to a [`DatabaseHandle`] returned by [`fwd_open_db`].
/// - There are no handles to any open proposals. If so, they must be freed first
///   using [`fwd_free_proposal`].
/// - The database handle is not used after this function is called.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_close_db(db: Option<Box<DatabaseHandle>>) -> VoidResult {
    invoke_with_handle(db, drop)
}

/// Consumes the [`OwnedBytes`] and frees the memory associated with it.
///
/// # Arguments
///
/// * `bytes` - The [`OwnedBytes`] struct to free, previously returned from any
///   function from this library.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the memory was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
///
/// # Safety
///
/// The caller must ensure that the `bytes` struct is valid and that the memory
/// it points to is uniquely owned by this object. However, if `bytes.ptr` is null,
/// this function does nothing.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_free_owned_bytes(bytes: OwnedBytes) -> VoidResult {
    invoke(move || drop(bytes))
}
