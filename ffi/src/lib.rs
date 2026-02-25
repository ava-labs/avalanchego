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
mod iterator;
mod logging;
mod metrics;
mod proofs;
mod proposal;
mod registry;
#[cfg(feature = "block-replay")]
mod replay;
mod revision;
mod value;

use firewood::v2::api::DbView;
use firewood_metrics::{firewood_increment, firewood_record, set_metrics_context};

pub use crate::handle::*;
pub use crate::iterator::*;
pub use crate::logging::*;
use crate::metrics::MetricsContextExt;
pub use crate::proofs::*;
pub use crate::proposal::*;
pub use crate::revision::*;
pub use crate::value::*;

#[global_allocator]
#[doc(hidden)]
static GLOBAL: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

/// Invokes a closure and returns the result as a [`CResult`].
///
/// If the closure panics, it will return [`CResult::from_panic`] with the panic
/// information.
#[inline]
fn invoke<T: CResult, V: Into<T>>(once: impl FnOnce() -> V) -> T {
    #[cfg(panic = "unwind")]
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(once)) {
        Ok(result) => result.into(),
        Err(panic) => T::from_panic(panic),
    }

    #[cfg(not(panic = "unwind"))]
    {
        once().into()
    }
}

/// Invokes a closure with context from the handle.
#[inline]
fn invoke_with_handle<H: MetricsContextExt, T: NullHandleResult, V: Into<T>>(
    handle: Option<H>,
    once: impl FnOnce(H) -> V,
) -> T {
    match handle {
        Some(handle) => {
            let _guard = set_metrics_context(handle.metrics_context());
            invoke(move || once(handle))
        }
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
pub extern "C" fn fwd_get_latest(db: Option<&DatabaseHandle>, key: BorrowedBytes) -> ValueResult {
    #[cfg(feature = "block-replay")]
    if db.is_some() {
        replay::record_get_latest(key);
    }

    invoke_with_handle(db, move |db| db.get_latest(key))
}

/// Returns an iterator optionally starting from a key in the provided revision.
///
/// # Arguments
///
/// * `revision` - The revision handle returned by [`fwd_get_revision`].
/// * `key` - The key to look up as a [`BorrowedBytes`]
///
/// # Returns
///
/// - [`IteratorResult::NullHandlePointer`] if the provided revision handle is null.
/// - [`IteratorResult::Ok`] if the iterator was created, with the iterator handle.
/// - [`IteratorResult::Err`] if an error occurred while creating the iterator.
///
/// # Safety
///
/// The caller must:
/// * ensure that `revision` is a valid pointer to a [`RevisionHandle`]
/// * ensure that `key` is a valid [`BorrowedBytes`]
/// * call [`fwd_free_iterator`] to free the memory associated with the iterator.
///
#[unsafe(no_mangle)]
pub extern "C" fn fwd_iter_on_revision<'view>(
    revision: Option<&'view RevisionHandle>,
    key: BorrowedBytes,
) -> IteratorResult<'view> {
    invoke_with_handle(revision, move |rev| rev.iter_from(Some(key.as_slice())))
}

/// Returns an iterator on the provided proposal optionally starting from a key
///
/// # Arguments
///
/// * `handle` - The proposal handle returned by [`fwd_propose_on_db`] or
///   [`fwd_propose_on_proposal`].
/// * `key` - The key to look up as a [`BorrowedBytes`]
///
/// # Returns
///
/// - [`IteratorResult::NullHandlePointer`] if the provided proposal handle is null.
/// - [`IteratorResult::Ok`] if the iterator was created, with the iterator handle.
/// - [`IteratorResult::Err`] if an error occurred while creating the iterator.
///
/// # Safety
///
/// The caller must:
/// * ensure that `handle` is a valid pointer to a [`ProposalHandle`]
/// * ensure that `key` is a valid for [`BorrowedBytes`]
/// * call [`fwd_free_iterator`] to free the memory associated with the iterator.
///
#[unsafe(no_mangle)]
pub extern "C" fn fwd_iter_on_proposal<'p>(
    handle: Option<&'p ProposalHandle<'_>>,
    key: BorrowedBytes,
) -> IteratorResult<'p> {
    invoke_with_handle(handle, move |p| p.iter_from(Some(key.as_slice())))
}

/// Retrieves the next item from the iterator.
///
/// # Arguments
///
/// * `handle` - The iterator handle returned by [`fwd_iter_on_revision`] or
///   [`fwd_iter_on_proposal`].
///
/// # Returns
///
/// - [`KeyValueResult::NullHandlePointer`] if the provided iterator handle is null.
/// - [`KeyValueResult::None`] if the iterator is exhausted (no remaining values). Once returned,
///   subsequent calls will continue returning [`KeyValueResult::None`]. You may still call this
///   safely, but freeing the iterator with [`fwd_free_iterator`] is recommended.
/// - [`KeyValueResult::Some`] if the next item on iterator was retrieved, with the associated
///   key value pair.
/// - [`KeyValueResult::Err`] if an I/O error occurred while retrieving the next item. Most
///   iterator errors are non-reentrant. Once returned, the iterator should be considered
///   invalid and must be freed with [`fwd_free_iterator`].
///
/// # Safety
///
/// The caller must:
/// * ensure that `handle` is a valid pointer to a [`IteratorHandle`].
/// * call [`fwd_free_owned_kv_pair`] on returned [`OwnedKeyValuePair`]
///   to free the memory associated with the returned value.
///
#[unsafe(no_mangle)]
pub extern "C" fn fwd_iter_next(handle: Option<&mut IteratorHandle<'_>>) -> KeyValueResult {
    invoke_with_handle(handle, Iterator::next)
}

/// Retrieves the next batch of items from the iterator.
///
/// # Arguments
///
/// * `handle` - The iterator handle returned by [`fwd_iter_on_revision`] or
///   [`fwd_iter_on_proposal`].
///
/// # Returns
///
/// - [`KeyValueBatchResult::NullHandlePointer`] if the provided iterator handle is null.
/// - [`KeyValueBatchResult::Some`] with up to `n` key/value pairs. If the iterator is
///   exhausted, this may be fewer than `n`, including zero items.
/// - [`KeyValueBatchResult::Err`] if an I/O error occurred while retrieving items. Most
///   iterator errors are non-reentrant. Once returned, the iterator should be considered
///   invalid and must be freed with [`fwd_free_iterator`].
///
/// Once an empty batch or items fewer than `n` is returned (iterator exhausted), subsequent calls
/// will continue returning empty batches. You may still call this safely, but freeing the
/// iterator with [`fwd_free_iterator`] is recommended.
///
/// # Safety
///
/// The caller must:
/// * ensure that `handle` is a valid pointer to a [`IteratorHandle`].
/// * call [`fwd_free_owned_key_value_batch`] on the returned batch to free any allocated memory.
///
#[unsafe(no_mangle)]
pub extern "C" fn fwd_iter_next_n(
    handle: Option<&mut IteratorHandle<'_>>,
    n: usize,
) -> KeyValueBatchResult {
    invoke_with_handle(handle, |it| it.iter_next_n(n))
}

/// Consumes the [`IteratorHandle`], destroys the iterator, and frees the memory.
///
/// # Arguments
///
/// * `iterator` - A pointer to a [`IteratorHandle`] previously returned from a
///   function from this library.
///
/// # Returns
///
/// - [`VoidResult::NullHandlePointer`] if the provided iterator handle is null.
/// - [`VoidResult::Ok`] if the iterator was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
///
/// # Safety
///
/// The caller must ensure that the `iterator` is not null and that it points to
/// a valid [`IteratorHandle`] previously returned by a function from this library.
///
#[unsafe(no_mangle)]
pub extern "C" fn fwd_free_iterator(iterator: Option<Box<IteratorHandle<'_>>>) -> VoidResult {
    invoke_with_handle(iterator, drop)
}

/// Gets a handle to the revision identified by the provided root hash.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`].
/// * `root` - The hash of the revision as a [`BorrowedBytes`].
///
/// # Returns
///
/// - [`RevisionResult::NullHandlePointer`] if the provided database handle is null.
/// - [`RevisionResult::Ok`] containing a [`RevisionHandle`] and root hash if the revision exists.
/// - [`RevisionResult::Err`] if the revision cannot be fetched or the root hash is invalid.
///
/// # Safety
///
/// The caller must:
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`].
/// * ensure that `root` is valid for [`BorrowedBytes`].
/// * call [`fwd_free_revision`] to free the returned handle when it is no longer needed.
///
/// [`BorrowedBytes`]: crate::value::BorrowedBytes
/// [`RevisionHandle`]: crate::revision::RevisionHandle
#[unsafe(no_mangle)]
pub extern "C" fn fwd_get_revision(db: Option<&DatabaseHandle>, root: HashKey) -> RevisionResult {
    invoke_with_handle(db, move |db| db.get_revision(root.into()))
}

/// Gets the value associated with the given key from the provided revision handle.
///
/// # Arguments
///
/// * `revision` - The revision handle returned by [`fwd_get_revision`].
/// * `key` - The key to look up as a [`BorrowedBytes`].
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the provided revision handle is null.
/// - [`ValueResult::None`] if the key was not found in the revision.
/// - [`ValueResult::Some`] if the key was found with the associated value.
/// - [`ValueResult::Err`] if an error occurred while retrieving the value.
///
/// # Safety
///
/// The caller must:
/// * ensure that `revision` is a valid pointer to a [`RevisionHandle`].
/// * ensure that `key` is valid for [`BorrowedBytes`].
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the [`OwnedBytes`]
///   returned in the result.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_get_from_revision(
    revision: Option<&RevisionHandle>,
    key: BorrowedBytes,
) -> ValueResult {
    invoke_with_handle(revision, move |rev| rev.val(key))
}

/// Consumes the [`RevisionHandle`] and frees the memory associated with it.
///
/// # Arguments
///
/// * `revision` - A pointer to a [`RevisionHandle`] previously returned by
///   [`fwd_get_revision`].
///
/// # Returns
///
/// - [`VoidResult::NullHandlePointer`] if the provided revision handle is null.
/// - [`VoidResult::Ok`] if the revision handle was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
///
/// # Safety
///
/// The caller must ensure that the revision handle is valid and is not used again after
/// this function is called.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_free_revision(revision: Option<Box<RevisionHandle>>) -> VoidResult {
    invoke_with_handle(revision, drop)
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
pub extern "C" fn fwd_get_from_proposal(
    handle: Option<&ProposalHandle<'_>>,
    key: BorrowedBytes,
) -> ValueResult {
    #[cfg(feature = "block-replay")]
    replay::record_get_from_proposal(handle, key);

    invoke_with_handle(handle, move |handle| handle.val(key))
}

/// Puts the given key-value pairs into the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`]
/// * `values` - A [`BorrowedBatchOps`] containing the batch operations to apply.
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
/// * ensure that `values` is valid for [`BorrowedBatchOps`]
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error ([`HashKey`] does not need to be freed as it is returned by
///   value).
#[unsafe(no_mangle)]
pub extern "C" fn fwd_batch(
    db: Option<&DatabaseHandle>,
    values: BorrowedBatchOps<'_>,
) -> HashResult {
    #[cfg(feature = "block-replay")]
    if db.is_some() {
        replay::record_batch(values);
    }

    invoke_with_handle(db, move |db| db.create_batch(values))
}

/// Proposes a batch of operations to the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`]
/// * `values` - A [`BorrowedBatchOps`] containing the batch operations to apply.
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
/// * ensure that `values` is valid for [`BorrowedBatchOps`]
/// * call [`fwd_commit_proposal`] or [`fwd_free_proposal`] to free the memory
///   associated with the proposal. And, the caller must ensure this is done
///   before calling [`fwd_close_db`] to avoid memory leaks or undefined behavior.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_propose_on_db<'db>(
    db: Option<&'db DatabaseHandle>,
    values: BorrowedBatchOps<'_>,
) -> ProposalResult<'db> {
    let result = invoke_with_handle(db, move |db| db.create_proposal_handle(values));

    #[cfg(feature = "block-replay")]
    if db.is_some() {
        replay::record_propose_on_db(&result, values);
    }

    result
}

/// Proposes a batch of operations to the database on top of an existing proposal.
///
/// # Arguments
///
/// * `handle` - The proposal handle returned by [`fwd_propose_on_db`] or
///   [`fwd_propose_on_proposal`].
/// * `values` - A [`BorrowedBatchOps`] containing the batch operations to apply.
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
/// * ensure that `values` is valid for [`BorrowedBatchOps`]
/// * call [`fwd_commit_proposal`] or [`fwd_free_proposal`] to free the memory
///   associated with the proposal. And, the caller must ensure this is done
///   before calling [`fwd_close_db`] to avoid memory leaks or undefined behavior.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_propose_on_proposal<'db>(
    handle: Option<&ProposalHandle<'db>>,
    values: BorrowedBatchOps<'_>,
) -> ProposalResult<'db> {
    let result = invoke_with_handle(handle, move |p| p.create_proposal_handle(values));

    #[cfg(feature = "block-replay")]
    replay::record_propose_on_proposal(handle, &result, values);

    result
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
pub extern "C" fn fwd_commit_proposal(proposal: Option<Box<ProposalHandle<'_>>>) -> HashResult {
    #[cfg(feature = "block-replay")]
    let proposal_ptr = proposal.as_ref().map(|h| std::ptr::from_ref(&**h));

    let result = invoke_with_handle(proposal, move |proposal| {
        proposal.commit_proposal(|commit_time| {
            firewood_increment!(crate::registry::COMMIT_MS, commit_time.as_millis());
            firewood_increment!(crate::registry::COMMIT_COUNT, 1);
            firewood_record!(
                crate::registry::COMMIT_MS_BUCKET,
                commit_time.as_f64() * 1000.0,
                expensive
            );
        })
    });

    #[cfg(feature = "block-replay")]
    replay::record_commit(proposal_ptr, &result);

    result
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
pub extern "C" fn fwd_free_proposal(proposal: Option<Box<ProposalHandle<'_>>>) -> VoidResult {
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
///
/// # Safety
///
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`]
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned error ([`HashKey`] does not need to be freed as it is returned
///   by value).
#[unsafe(no_mangle)]
pub extern "C" fn fwd_root_hash(db: Option<&DatabaseHandle>) -> HashResult {
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
    invoke(metrics::setup_metrics)
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
    invoke(move || metrics::setup_metrics_with_exporter(metrics_port))
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
    invoke(metrics::gather_metrics)
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
pub extern "C" fn fwd_open_db(args: DatabaseHandleArgs) -> HandleResult {
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
/// This also stops the background persistence thread.
///
/// # Arguments
///
/// * `db` - The database handle to close, previously returned from a call to [`fwd_open_db`].
///
/// # Returns
///
/// - [`VoidResult::NullHandlePointer`] if the provided database handle is null.
/// - [`VoidResult::Ok`] if the database handle was successfully closed and freed.
/// - [`VoidResult::Err`] if the background persistence worker thread panics while
///   closing the database handle or if the background persistence worker thread
///   errored.
///
/// # Safety
///
/// Callers must ensure that:
///
/// - `db` is a valid pointer to a [`DatabaseHandle`] returned by [`fwd_open_db`].
/// - There are no handles to any open proposals. If so, they must be freed first
///   using [`fwd_free_proposal`].
/// - Freeing the database handle does not free outstanding [`RevisionHandle`]s
///   returned by [`fwd_get_revision`]. To prevent leaks, free them separately
///   with [`fwd_free_revision`].
/// - The database handle is not used after this function is called.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_close_db(db: Option<Box<DatabaseHandle>>) -> VoidResult {
    #[cfg(feature = "block-replay")]
    let _ = replay::flush_to_disk();

    invoke_with_handle(db, |db| db.close())
}

/// Flushes buffered block replay operations to disk.
///
/// This function is only meaningful when the `block-replay` feature is enabled
/// and the `FIREWOOD_BLOCK_REPLAY_PATH` environment variable is set. Otherwise,
/// it is a no-op.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the flush succeeded or was a no-op.
/// - [`VoidResult::Err`] if an I/O error occurred during the flush.
#[unsafe(no_mangle)]
#[allow(clippy::missing_const_for_fn)] // Can't be const when block-replay is enabled
pub extern "C" fn fwd_block_replay_flush() -> VoidResult {
    #[cfg(feature = "block-replay")]
    {
        invoke(replay::flush_to_disk)
    }

    #[cfg(not(feature = "block-replay"))]
    {
        VoidResult::Ok
    }
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
pub extern "C" fn fwd_free_owned_bytes(bytes: OwnedBytes) -> VoidResult {
    invoke(move || drop(bytes))
}

/// Consumes the [`OwnedKeyValueBatch`] and frees the memory associated with it.
///
/// # Arguments
///
/// * `batch` - The [`OwnedKeyValueBatch`] struct to free, previously returned from any
///   function from this library.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the memory was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
///
/// # Safety
///
/// The caller must ensure that the `batch` struct is valid and that the memory
/// it points to is uniquely owned by this object. However, if `batch.ptr` is null,
/// this function does nothing.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_free_owned_key_value_batch(batch: OwnedKeyValueBatch) -> VoidResult {
    invoke(move || drop(batch))
}

/// Consumes the [`OwnedKeyValuePair`] and frees the memory associated with it.
///
/// # Arguments
///
/// * `kv` - The [`OwnedKeyValuePair`] struct to free, previously returned from any
///   function from this library.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the memory was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
///
/// # Safety
///
/// The caller must ensure that the `kv` struct is valid.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_free_owned_kv_pair(kv: OwnedKeyValuePair) -> VoidResult {
    invoke(move || drop(kv))
}

/// Dumps the Trie structure of the latest revision of the database to a DOT
/// (Graphviz) format string for debugging.
///
/// # Arguments
///
/// * `db` - The database handle returned by [`fwd_open_db`]
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the provided database handle is null.
/// - [`ValueResult::Some`] with the DOT format string if successful (the data is
///   guaranteed to be utf-8 data, not null terminated).
/// - [`ValueResult::Err`] if an error occurred while dumping the database.
///
/// # Safety
///
/// The caller must:
/// * ensure that `db` is a valid pointer to a [`DatabaseHandle`].
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned value.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_db_dump(db: Option<&DatabaseHandle>) -> ValueResult {
    invoke_with_handle(db, handle::DatabaseHandle::dump_to_string)
}

/// Dumps the Trie structure of a revision to a DOT (Graphviz) format string for debugging.
///
/// # Arguments
///
/// * `revision` - A pointer to a [`RevisionHandle`] previously returned by
///   [`fwd_get_revision`].
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the provided revision handle is null.
/// - [`ValueResult::Some`] with the DOT format string if successful (the data is
///   guaranteed to be utf-8 data, not null terminated).
/// - [`ValueResult::Err`] if an error occurred while dumping the revision.
///
/// # Safety
///
/// The caller must:
/// * ensure that `revision` is a valid pointer to a [`RevisionHandle`].
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned value.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_revision_dump(revision: Option<&RevisionHandle>) -> ValueResult {
    invoke_with_handle(revision, firewood::v2::api::DbView::dump_to_string)
}

/// Dumps the Trie structure of a proposal to a DOT (Graphviz) format string for debugging.
///
/// # Arguments
///
/// * `proposal` - The proposal handle returned by [`fwd_propose_on_db`] or
///   [`fwd_propose_on_proposal`].
///
/// # Returns
///
/// - [`ValueResult::NullHandlePointer`] if the provided proposal handle is null.
/// - [`ValueResult::Some`] with the DOT format string if successful (the data is
///   guaranteed to be utf-8 data, not null terminated).
/// - [`ValueResult::Err`] if an error occurred while dumping the proposal.
///
/// # Safety
///
/// The caller must:
/// * ensure that `proposal` is a valid pointer to a [`ProposalHandle`].
/// * call [`fwd_free_owned_bytes`] to free the memory associated with the
///   returned value.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_proposal_dump(proposal: Option<&ProposalHandle>) -> ValueResult {
    invoke_with_handle(proposal, firewood::v2::api::DbView::dump_to_string)
}
