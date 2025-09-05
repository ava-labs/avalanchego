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
#![expect(
    clippy::undocumented_unsafe_blocks,
    reason = "https://github.com/ava-labs/firewood/pull/1158 will remove"
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
mod value;

use std::collections::HashMap;
use std::ffi::{CStr, CString, c_char};
use std::fmt::{self, Display, Formatter};
use std::ops::Deref;
use std::sync::RwLock;
use std::sync::atomic::{AtomicU32, Ordering};

use firewood::db::{Db, Proposal};
use firewood::v2::api::{self, Db as _, DbView, KeyValuePairIter, Proposal as _};

use crate::arc_cache::ArcCache;
pub use crate::handle::*;
pub use crate::logging::*;
pub use crate::proofs::*;
pub use crate::value::*;

#[cfg(unix)]
#[global_allocator]
#[doc(hidden)]
static GLOBAL: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

type ProposalId = u32;

#[doc(hidden)]
static ID_COUNTER: AtomicU32 = AtomicU32::new(1);

/// Atomically retrieves the next proposal ID.
#[doc(hidden)]
fn next_id() -> ProposalId {
    ID_COUNTER.fetch_add(1, Ordering::Relaxed)
}

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

/// A handle to the database, returned by `fwd_open_db`.
///
/// These handles are passed to the other FFI functions.
///
#[derive(Debug)]
pub struct DatabaseHandle<'p> {
    /// List of oustanding proposals, by ID
    // Keep proposals first, as they must be dropped before the database handle is dropped due to lifetime
    // issues.
    proposals: RwLock<HashMap<ProposalId, Proposal<'p>>>,

    /// A single cached view to improve performance of reads while committing
    cached_view: ArcCache<api::HashKey, dyn api::DynDbView>,

    /// The database
    db: Db,
}

impl From<Db> for DatabaseHandle<'_> {
    fn from(db: Db) -> Self {
        Self {
            db,
            proposals: RwLock::new(HashMap::new()),
            cached_view: ArcCache::new(),
        }
    }
}

impl Deref for DatabaseHandle<'_> {
    type Target = Db;

    fn deref(&self) -> &Self::Target {
        &self.db
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
/// * `db` - The database handle returned by `open_db`
/// * `id` - The ID of the proposal to get the value from
/// * `key` - The key to look up, in `BorrowedBytes` form
///
/// # Returns
///
/// A `Value` containing the requested value.
/// A `Value` containing {0, "error message"} if the get failed.
///
/// # Safety
///
/// The caller must:
///  * ensure that `db` is a valid pointer returned by `open_db`
///  * ensure that `key` is a valid pointer to a `Value` struct
///  * call `free_value` to free the memory associated with the returned `Value`
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_from_proposal(
    db: Option<&DatabaseHandle<'_>>,
    id: ProposalId,
    key: BorrowedBytes<'_>,
) -> ValueResult {
    invoke_with_handle(db, move |db| db.get_from_proposal(id, key))
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
/// * `db` - The database handle returned by `open_db`
/// * `values` - A `BorrowedKeyValuePairs` struct containing the key-value pairs to put.
///
/// # Returns
///
/// On success, a `Value` containing {len=id, data=hash}. In this case, the
/// hash will always be 32 bytes, and the id will be non-zero.
/// On failure, a `Value` containing {0, "error message"}.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must:
///  * ensure that `db` is a valid pointer returned by `open_db`
///  * ensure that `values` is a valid pointer and that it points to an array of `KeyValue` structs of length `nkeys`.
///  * ensure that the `Value` fields of the `KeyValue` structs are valid pointers.
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_propose_on_db<'p>(
    db: Option<&'p DatabaseHandle<'p>>,
    values: BorrowedKeyValuePairs<'_>,
) -> Value {
    // Note: the id is guaranteed to be non-zero
    // because we use an atomic counter that starts at 1.
    propose_on_db(db, &values).unwrap_or_else(Into::into)
}

/// Internal call for `fwd_propose_on_db` to remove error handling from the C API
#[doc(hidden)]
fn propose_on_db<'p>(
    db: Option<&'p DatabaseHandle<'p>>,
    values: &[KeyValuePair<'_>],
) -> Result<Value, String> {
    let db = db.ok_or("db should be non-null")?;
    // Create a batch of operations to perform.
    let batch = values.iter().map_into_batch();

    // Propose the batch of operations.
    let proposal = db.propose(batch).map_err(|e| e.to_string())?;

    // Get the root hash of the new proposal.
    let mut root_hash: Value = match proposal.root_hash().map_err(|e| e.to_string())? {
        Some(root) => Value::from(root.as_slice()),
        None => String::new().into(),
    };

    // Store the proposal in the map. We need the write lock instead.
    let new_id = next_id(); // Guaranteed to be non-zero
    db.proposals
        .write()
        .map_err(|_| "proposal lock is poisoned")?
        .insert(new_id, proposal);
    root_hash.len = new_id as usize; // Set the length to the proposal ID
    Ok(root_hash)
}

/// Proposes a batch of operations to the database on top of an existing proposal.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `proposal_id` - The ID of the proposal to propose on
/// * `values` - A `BorrowedKeyValuePairs` struct containing the key-value pairs to put.
///
/// # Returns
///
/// On success, a `Value` containing {len=id, data=hash}. In this case, the
/// hash will always be 32 bytes, and the id will be non-zero.
/// On failure, a `Value` containing {0, "error message"}.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must:
///  * ensure that `db` is a valid pointer returned by `open_db`
///  * ensure that `values` is a valid pointer and that it points to an array of `KeyValue` structs of length `nkeys`.
///  * ensure that the `Value` fields of the `KeyValue` structs are valid pointers.
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_propose_on_proposal(
    db: Option<&DatabaseHandle<'_>>,
    proposal_id: ProposalId,
    values: BorrowedKeyValuePairs<'_>,
) -> Value {
    // Note: the id is guaranteed to be non-zero
    // because we use an atomic counter that starts at 1.
    propose_on_proposal(db, proposal_id, &values).unwrap_or_else(Into::into)
}

/// Internal call for `fwd_propose_on_proposal` to remove error handling from the C API
#[doc(hidden)]
fn propose_on_proposal(
    db: Option<&DatabaseHandle<'_>>,
    proposal_id: ProposalId,
    values: &[KeyValuePair<'_>],
) -> Result<Value, String> {
    let db = db.ok_or("db should be non-null")?;
    // Create a batch of operations to perform.
    let batch = values.iter().map_into_batch();

    // Get proposal from ID.
    // We need write access to add the proposal after we create it.
    let guard = db
        .proposals
        .write()
        .expect("failed to acquire write lock on proposals");
    let proposal = guard.get(&proposal_id).ok_or("proposal not found")?;
    let new_proposal = proposal.propose(batch).map_err(|e| e.to_string())?;
    drop(guard); // Drop the read lock before we get the write lock.

    // Get the root hash of the new proposal.
    let mut root_hash: Value = match new_proposal.root_hash().map_err(|e| e.to_string())? {
        Some(root) => Value::from(root.as_slice()),
        None => String::new().into(),
    };

    // Store the proposal in the map. We need the write lock instead.
    let new_id = next_id(); // Guaranteed to be non-zero
    db.proposals
        .write()
        .map_err(|_| "proposal lock is poisoned")?
        .insert(new_id, new_proposal);
    root_hash.len = new_id as usize; // Set the length to the proposal ID
    Ok(root_hash)
}

/// Commits a proposal to the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `proposal_id` - The ID of the proposal to commit
///
/// # Returns
///
/// A `Value` containing {0, null} if the commit was successful.
/// A `Value` containing {0, "error message"} if the commit failed.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must ensure that `db` is a valid pointer returned by `open_db`
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_commit(
    db: Option<&DatabaseHandle<'_>>,
    proposal_id: u32,
) -> HashResult {
    invoke_with_handle(db, move |db| db.commit_proposal(proposal_id))
}

/// Drops a proposal from the database.
/// The propopsal's data is now inaccessible, and can be freed by the `RevisionManager`.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `proposal_id` - The ID of the proposal to drop
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must ensure that `db` is a valid pointer returned by `open_db`
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_drop_proposal(
    db: Option<&DatabaseHandle<'_>>,
    proposal_id: u32,
) -> Value {
    drop_proposal(db, proposal_id).map_or_else(Into::into, Into::into)
}

/// Internal call for `fwd_drop_proposal` to remove error handling from the C API
#[doc(hidden)]
fn drop_proposal(db: Option<&DatabaseHandle<'_>>, proposal_id: u32) -> Result<(), String> {
    let db = db.ok_or("db should be non-null")?;
    let mut proposals = db
        .proposals
        .write()
        .map_err(|_| "proposal lock is poisoned")?;
    proposals.remove(&proposal_id).ok_or("proposal not found")?;
    Ok(())
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

/// A value returned by the FFI.
///
/// This is used in several different ways, including:
/// * An C-style string.
/// * An ID for a proposal.
/// * A byte slice containing data.
///
/// For more details on how the data may be stored, refer to the function signature
/// that returned it or the `From` implementations.
///
/// The data stored in this struct (if `data` is not null) must be manually freed
/// by the caller using `fwd_free_value`.
///
#[derive(Debug, Default)]
#[repr(C)]
pub struct Value {
    pub len: usize,
    pub data: Option<std::ptr::NonNull<u8>>,
}

impl Display for Value {
    fn fmt(&self, f: &mut Formatter) -> fmt::Result {
        match (self.len, self.data) {
            (0, None) => write!(f, "[not found]"),
            (0, Some(data)) => write!(f, "[error] {}", unsafe {
                CStr::from_ptr(data.as_ptr() as *const c_char).to_string_lossy()
            }),
            (len, None) => write!(f, "[id] {len}"),
            (_, Some(_)) => write!(f, "[data] {:?}", self.as_slice()),
        }
    }
}

impl Value {
    #[must_use]
    pub const fn as_slice(&self) -> &[u8] {
        if let Some(data) = self.data {
            // SAFETY: We must assume that if non-null, the C caller provided valid pointer
            // and length, otherwise caller assumes responsibility for undefined behavior.
            unsafe { std::slice::from_raw_parts(data.as_ptr(), self.len) }
        } else {
            &[]
        }
    }
}

impl From<&[u8]> for Value {
    fn from(data: &[u8]) -> Self {
        let boxed: Box<[u8]> = data.into();
        boxed.into()
    }
}

impl From<Box<[u8]>> for Value {
    fn from(data: Box<[u8]>) -> Self {
        let len = data.len();
        let leaked_ptr = Box::leak(data).as_mut_ptr();
        let data = std::ptr::NonNull::new(leaked_ptr);
        Value { len, data }
    }
}

impl From<String> for Value {
    fn from(s: String) -> Self {
        if s.is_empty() {
            Self::default()
        } else {
            let cstr = CString::new(s).unwrap_or_default().into_raw();
            Value {
                len: 0,
                data: std::ptr::NonNull::new(cstr.cast::<u8>()),
            }
        }
    }
}

impl From<u32> for Value {
    fn from(v: u32) -> Self {
        // WARNING: This should only be called with values >= 1.
        // In much of the Go code, v.len == 0 is used to indicate a null-terminated string.
        // This may cause a panic or memory corruption if used incorrectly.
        assert_ne!(v, 0);
        Self {
            len: v as usize,
            data: None,
        }
    }
}

impl From<()> for Value {
    fn from((): ()) -> Self {
        Self::default()
    }
}

/// Frees the memory associated with a `Value`.
///
/// # Arguments
///
/// * `value` - The `Value` to free, previously returned from any Rust function.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must ensure that `value` is a valid pointer.
///
/// # Panics
///
/// This function panics if `value` is `null`.
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_free_value(value: Option<&mut Value>) -> VoidResult {
    invoke_with_handle(value, |value| {
        if let Some(data) = value.data {
            let data_ptr = data.as_ptr();
            // We assume that if the length is 0, then the data is a null-terminated string.
            if value.len > 0 {
                let recreated_box =
                    unsafe { Box::from_raw(std::slice::from_raw_parts_mut(data_ptr, value.len)) };
                drop(recreated_box);
            } else {
                let raw_str = data_ptr.cast::<c_char>();
                let cstr = unsafe { CString::from_raw(raw_str) };
                drop(cstr);
            }
        }
    })
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
/// - The database handle is not used after this function is called.
#[expect(clippy::missing_panics_doc, reason = "panics are captured")]
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_close_db(db: Option<Box<DatabaseHandle<'_>>>) -> VoidResult {
    invoke_with_handle(db, |db| {
        db.proposals
            .write()
            .expect("proposals lock is poisoned")
            .clear();
        db.clear_cached_view();
    })
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

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used)]

    use super::*;

    #[test]
    fn test_invalid_value_display() {
        let value = Value::default();
        assert_eq!(format!("{value}"), "[not found]");
    }

    #[test]
    fn test_value_display_with_error_string() {
        let cstr = CString::new("test").unwrap();
        let value = Value {
            len: 0,
            data: std::ptr::NonNull::new(cstr.as_ptr().cast::<u8>().cast_mut()),
        };
        assert_eq!(format!("{value}"), "[error] test");
    }

    #[test]
    fn test_value_display_with_data() {
        let value = Value {
            len: 4,
            data: std::ptr::NonNull::new(
                Box::leak(b"test".to_vec().into_boxed_slice()).as_mut_ptr(),
            ),
        };
        assert_eq!(format!("{value}"), "[data] [116, 101, 115, 116]");
    }

    #[test]
    fn test_value_display_with_id() {
        let value = Value { len: 4, data: None };
        assert_eq!(format!("{value}"), "[id] 4");
    }
}
