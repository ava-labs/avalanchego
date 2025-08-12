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

mod metrics_setup;
mod value;

use std::collections::HashMap;
use std::ffi::{CStr, CString, c_char};
use std::fmt::{self, Display, Formatter};
use std::ops::Deref;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Mutex, RwLock};

use firewood::db::{Db, DbConfig, DbViewSync as _, DbViewSyncBytes, Proposal};
use firewood::manager::{CacheReadStrategy, RevisionManagerConfig};

use firewood::v2::api::{HashKey, KeyValuePairIter};
use metrics::counter;

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

/// A handle to the database, returned by `fwd_create_db` and `fwd_open_db`.
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
    cached_view: Mutex<Option<(HashKey, Box<dyn DbViewSyncBytes>)>>,

    /// The database
    db: Db,
}

impl From<Db> for DatabaseHandle<'_> {
    fn from(db: Db) -> Self {
        Self {
            db,
            proposals: RwLock::new(HashMap::new()),
            cached_view: Mutex::new(None),
        }
    }
}

impl DatabaseHandle<'_> {
    fn clear_cached_view(&self) {
        self.cached_view
            .lock()
            .expect("cached_view lock is poisoned")
            .take();
    }
}

impl Deref for DatabaseHandle<'_> {
    type Target = Db;

    fn deref(&self) -> &Self::Target {
        &self.db
    }
}

/// Gets the value associated with the given key from the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `key` - The key to look up, in `BorrowedBytes` form
///
/// # Returns
///
/// A `Value` containing the requested value.
/// A `Value` containing {0, "error message"} if the get failed.
/// There is one error case that may be expected to be null by the caller,
/// but should be handled externally: The database has no entries - "IO error: Root hash not found"
/// This is expected behavior if the database is empty.
///
/// # Safety
///
/// The caller must:
///  * ensure that `db` is a valid pointer returned by `open_db`
///  * ensure that `key` is a valid pointer to a `Value` struct
///  * call `free_value` to free the memory associated with the returned `Value`
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_latest(
    db: Option<&DatabaseHandle<'_>>,
    key: BorrowedBytes<'_>,
) -> Value {
    get_latest(db, &key).unwrap_or_else(Into::into)
}

/// This function is not exposed to the C API.
/// Internal call for `fwd_get_latest` to remove error handling from the C API
#[doc(hidden)]
fn get_latest(db: Option<&DatabaseHandle<'_>>, key: &[u8]) -> Result<Value, String> {
    let db = db.ok_or("db should be non-null")?;
    // Find root hash.
    // Matches `hash` function but we use the TrieHash type here
    let Some(root) = db.root_hash_sync().map_err(|e| e.to_string())? else {
        return Ok(Value::default());
    };

    // Find revision assoicated with root.
    let rev = db.revision_sync(root).map_err(|e| e.to_string())?;

    // Get value associated with key.
    let value = rev
        .val_sync_bytes(key)
        .map_err(|e| e.to_string())?
        .ok_or("")?;
    Ok(value.into())
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
) -> Value {
    get_from_proposal(db, id, &key).unwrap_or_else(Into::into)
}

/// This function is not exposed to the C API.
/// Internal call for `fwd_get_from_proposal` to remove error handling from the C API
#[doc(hidden)]
fn get_from_proposal(
    db: Option<&DatabaseHandle<'_>>,
    id: ProposalId,
    key: &[u8],
) -> Result<Value, String> {
    let db = db.ok_or("db should be non-null")?;
    // Get proposal from ID.
    let proposals = db
        .proposals
        .read()
        .map_err(|_| "proposal lock is poisoned")?;
    let proposal = proposals.get(&id).ok_or("proposal not found")?;

    // Get value associated with key.
    let value = proposal
        .val_sync(key)
        .map_err(|e| e.to_string())?
        .ok_or("")?;
    Ok(value.into())
}

/// Gets a value assoicated with the given root hash and key.
///
/// The hash may refer to a historical revision or an existing proposal.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `root` - The root hash to look up, in `BorrowedBytes` form
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
/// * ensure that `db` is a valid pointer returned by `open_db`
/// * ensure that `key` is a valid pointer to a `Value` struct
/// * ensure that `root` is a valid pointer to a `Value` struct
/// * call `free_value` to free the memory associated with the returned `Value`
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_from_root(
    db: Option<&DatabaseHandle<'_>>,
    root: BorrowedBytes<'_>,
    key: BorrowedBytes<'_>,
) -> Value {
    get_from_root(db, &root, &key).unwrap_or_else(Into::into)
}

/// Internal call for `fwd_get_from_root` to remove error handling from the C API
#[doc(hidden)]
fn get_from_root(
    db: Option<&DatabaseHandle<'_>>,
    root: &[u8],
    key: &[u8],
) -> Result<Value, String> {
    let db = db.ok_or("db should be non-null")?;
    let requested_root = HashKey::try_from(root).map_err(|e| e.to_string())?;
    let mut cached_view = db.cached_view.lock().expect("cached_view lock is poisoned");
    let value = match cached_view.as_ref() {
        // found the cached view, use it
        Some((root_hash, view)) if root_hash == &requested_root => {
            counter!("firewood.ffi.cached_view.hit").increment(1);
            view.val_sync_bytes(key)
        }
        // If what was there didn't match the requested root, we need a new view, so we
        // update the cache
        _ => {
            counter!("firewood.ffi.cached_view.miss").increment(1);
            let rev = view_sync_from_root(db, root)?;
            let result = rev.val_sync_bytes(key);
            *cached_view = Some((requested_root.clone(), rev));
            result
        }
    }
    .map_err(|e| e.to_string())?
    .ok_or("")?;

    Ok(value.into())
}
fn view_sync_from_root(
    db: &DatabaseHandle<'_>,
    root: &[u8],
) -> Result<Box<dyn DbViewSyncBytes>, String> {
    let rev = db
        .view_sync(HashKey::try_from(root).map_err(|e| e.to_string())?)
        .map_err(|e| e.to_string())?;
    Ok(rev)
}

/// Puts the given key-value pairs into the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `values` - A `BorrowedKeyValuePairs` struct containing the key-value pairs to put.
///
/// # Returns
///
/// The new root hash of the database, in Value form.
/// A `Value` containing {0, "error message"} if the commit failed.
///
/// # Errors
///
/// * `"key-value pair is null"` - A `KeyValue` struct is null
/// * `"db should be non-null"` - The database handle is null
/// * `"couldn't get key-value pair"` - A `KeyValue` struct is null
/// * `"proposed revision is empty"` - The proposed revision is empty
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
pub unsafe extern "C" fn fwd_batch(
    db: Option<&DatabaseHandle<'_>>,
    values: BorrowedKeyValuePairs,
) -> Value {
    batch(db, &values).unwrap_or_else(Into::into)
}

/// Internal call for `fwd_batch` to remove error handling from the C API
#[doc(hidden)]
fn batch(db: Option<&DatabaseHandle<'_>>, values: &[KeyValuePair<'_>]) -> Result<Value, String> {
    let db = db.ok_or("db should be non-null")?;
    let start = coarsetime::Instant::now();

    // Create a batch of operations to perform.
    let batch = values.iter().map_into_batch();

    // Propose the batch of operations.
    let proposal = db.propose_sync(batch).map_err(|e| e.to_string())?;
    let propose_time = start.elapsed().as_millis();
    counter!("firewood.ffi.propose_ms").increment(propose_time);

    let hash_val = proposal
        .root_hash_sync()
        .map_err(|e| e.to_string())?
        .ok_or("Proposed revision is empty")?
        .as_slice()
        .into();

    // Commit the proposal.
    proposal.commit_sync().map_err(|e| e.to_string())?;

    // Get the root hash of the database post-commit.
    let propose_plus_commit_time = start.elapsed().as_millis();
    counter!("firewood.ffi.batch_ms").increment(propose_plus_commit_time);
    counter!("firewood.ffi.commit_ms")
        .increment(propose_plus_commit_time.saturating_sub(propose_time));
    counter!("firewood.ffi.batch").increment(1);
    Ok(hash_val)
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
    let proposal = db.propose_sync(batch).map_err(|e| e.to_string())?;

    // Get the root hash of the new proposal.
    let mut root_hash: Value = match proposal.root_hash_sync().map_err(|e| e.to_string())? {
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
    let new_proposal = proposal.propose_sync(batch).map_err(|e| e.to_string())?;
    drop(guard); // Drop the read lock before we get the write lock.

    // Get the root hash of the new proposal.
    let mut root_hash: Value = match new_proposal.root_hash_sync().map_err(|e| e.to_string())? {
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
pub unsafe extern "C" fn fwd_commit(db: Option<&DatabaseHandle<'_>>, proposal_id: u32) -> Value {
    commit(db, proposal_id).map_or_else(Into::into, Into::into)
}

/// Internal call for `fwd_commit` to remove error handling from the C API
#[doc(hidden)]
fn commit(db: Option<&DatabaseHandle<'_>>, proposal_id: u32) -> Result<(), String> {
    let db = db.ok_or("db should be non-null")?;
    let proposal = db
        .proposals
        .write()
        .map_err(|_| "proposal lock is poisoned")?
        .remove(&proposal_id)
        .ok_or("proposal not found")?;

    // Get the proposal hash and cache the view. We never cache an empty proposal.
    let proposal_hash = proposal.root_hash_sync();

    if let Ok(Some(proposal_hash)) = proposal_hash {
        let mut guard = db.cached_view.lock().expect("cached_view lock is poisoned");
        match db.view_sync(proposal_hash.clone()) {
            Ok(view) => *guard = Some((proposal_hash, view)),
            Err(_) => *guard = None, // Clear cache on error
        }
        drop(guard);
    }

    // Commit the proposal
    let result = proposal.commit_sync().map_err(|e| e.to_string());

    // Clear the cache, which will force readers after this point to find the committed root hash
    db.clear_cached_view();

    result
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
/// * `db` - The database handle returned by `open_db`
///
/// # Returns
///
/// A `Value` containing the root hash of the database.
/// A `Value` containing {0, "error message"} if the root hash could not be retrieved.
/// One expected error is "IO error: Root hash not found" if the database is empty.
/// This should be handled by the caller.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must ensure that `db` is a valid pointer returned by `open_db`
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_root_hash(db: Option<&DatabaseHandle<'_>>) -> Value {
    root_hash(db).unwrap_or_else(Into::into)
}

/// This function is not exposed to the C API.
/// Internal call for `fwd_root_hash` to remove error handling from the C API
#[doc(hidden)]
fn root_hash(db: Option<&DatabaseHandle<'_>>) -> Result<Value, String> {
    let db = db.ok_or("db should be non-null")?;
    db.root_hash_sync()
        .map_err(|e| e.to_string())?
        .map(|root| Value::from(root.as_slice()))
        .map_or_else(|| Ok(Value::default()), Ok)
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
pub unsafe extern "C" fn fwd_free_value(value: Option<&mut Value>) {
    let value = value.expect("value should be non-null");
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
}

/// Struct returned by `fwd_create_db` and `fwd_open_db`
#[derive(Debug)]
#[repr(C)]
pub struct DatabaseCreationResult {
    pub db: Option<Box<DatabaseHandle<'static>>>,
    pub error_str: Option<std::ptr::NonNull<u8>>,
}

impl From<Result<Db, String>> for DatabaseCreationResult {
    fn from(result: Result<Db, String>) -> Self {
        match result {
            Ok(db) => DatabaseCreationResult {
                db: Some(Box::new(db.into())),
                error_str: None,
            },
            Err(error_msg) => {
                let error_cstring = CString::new(error_msg).unwrap_or_default().into_raw();
                DatabaseCreationResult {
                    db: None,
                    error_str: std::ptr::NonNull::new(error_cstring.cast::<u8>()),
                }
            }
        }
    }
}

/// Frees the memory associated with a `DatabaseCreationResult`.
/// This only needs to be called if the `error_str` field is non-null.
///
/// # Arguments
///
/// * `result` - The `DatabaseCreationResult` to free, previously returned from `fwd_create_db` or `fwd_open_db`.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must ensure that `result` is a valid pointer.
///
/// # Panics
///
/// This function panics if `result` is `null`.
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_free_database_error_result(
    result: Option<&mut DatabaseCreationResult>,
) {
    let result = result.expect("result should be non-null");
    // Free the error string if it exists
    if let Some(nonnull) = result.error_str {
        let raw_str = nonnull.cast::<c_char>().as_ptr();
        let cstr = unsafe { CString::from_raw(raw_str) };
        drop(cstr);
    }
    // Note: we don't free the db pointer as it's managed by the caller
}

/// Start metrics recorder for this process.
///
/// # Returns
///
/// A `Value` containing {0, null} if the metrics recorder was initialized.
/// A `Value` containing {0, "error message"} if an error occurs.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_start_metrics() -> Value {
    metrics_setup::setup_metrics()
        .map_err(|e| e.to_string())
        .map_or_else(Into::into, Into::into)
}

/// Start metrics recorder and exporter for this process.
///
/// * `metrics_port` - the port where metrics will be exposed at
///
/// # Returns
///
/// A `Value` containing {0, null} if the metrics recorder was initialized and
/// the exporter was started.
/// A `Value` containing {0, "error message"} if an error occurs.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_start_metrics_with_exporter(metrics_port: u16) -> Value {
    metrics_setup::setup_metrics_with_exporter(metrics_port)
        .map_err(|e| e.to_string())
        .map_or_else(Into::into, Into::into)
}

/// Gather latest metrics for this process.
///
/// # Returns
///
/// A `Value` containing {len, bytes} representing the latest metrics for this process.
/// A `Value` containing {0, "error message"} if unable to get the latest metrics.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_gather() -> Value {
    metrics_setup::gather_metrics().map_or_else(Into::into, |s| s.as_bytes().into())
}

/// Common arguments, accepted by both `fwd_create_db()` and `fwd_open_db()`.
///
/// * `path` - The path to the database file, which will be truncated if passed to `fwd_create_db()`
///   otherwise should exist if passed to `fwd_open_db()`.
/// * `cache_size` - The size of the node cache, returns an error if <= 0
/// * `free_list_cache_size` - The size of the free list cache, returns an error if <= 0
/// * `revisions` - The maximum number of revisions to keep; firewood currently requires this to be at least 2.
/// * `strategy` - The cache read strategy to use, 0 for writes only,
///   1 for branch reads, and 2 for all reads.
/// * `truncate` - Whether to truncate the database file if it exists.
///   Returns an error if the value is not 0, 1, or 2.
#[repr(C)]
pub struct CreateOrOpenArgs<'a> {
    path: BorrowedBytes<'a>,
    cache_size: usize,
    free_list_cache_size: usize,
    revisions: usize,
    strategy: u8,
    truncate: bool,
}

/// Open a database with the given cache size and maximum number of revisions
///
/// # Arguments
///
/// See `CreateOrOpenArgs`.
///
/// # Returns
///
/// A database handle, or panics if it cannot be created
///
/// # Safety
///
/// This function uses raw pointers so it is unsafe.
/// It is the caller's responsibility to ensure that path is a valid pointer to a null-terminated string.
/// The caller must also ensure that the cache size is greater than 0 and that the number of revisions is at least 2.
/// The caller must call `close` to free the memory associated with the returned database handle.
///
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_open_db(args: CreateOrOpenArgs) -> DatabaseCreationResult {
    unsafe { open_db(&args) }.into()
}

/// Internal call for `fwd_open_db` to remove error handling from the C API
#[doc(hidden)]
unsafe fn open_db(args: &CreateOrOpenArgs) -> Result<Db, String> {
    let cfg = DbConfig::builder()
        .truncate(args.truncate)
        .manager(manager_config(
            args.cache_size,
            args.free_list_cache_size,
            args.revisions,
            args.strategy,
        )?)
        .build();

    if args.path.is_empty() {
        return Err("path should not be empty".to_string());
    }
    let path = args
        .path
        .as_str()
        .map_err(|e| format!("Invalid database path: {e}"))?;
    Db::new_sync(path, cfg).map_err(|e| e.to_string())
}

/// Arguments for logging
///
/// * `path` - The file path where logs for this process are stored. By
///   default, this is set to /tmp/firewood-log.txt
/// * `filter_level` - The filter level for logs. By default, this is set to info.
#[repr(C)]
pub struct LogArgs<'a> {
    path: BorrowedBytes<'a>,
    filter_level: BorrowedBytes<'a>,
}

/// Start logs for this process.
///
/// # Arguments
///
/// See `LogArgs`.
///
/// # Returns
///
/// A `Value` containing {0, null} if the global logger was initialized.
/// A `Value` containing {0, "error message"} if an error occurs.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_start_logs(args: LogArgs<'_>) -> Value {
    start_logs(&args).map_or_else(Into::into, Into::into)
}

#[cfg(feature = "logger")]
#[doc(hidden)]
fn start_logs(log_args: &LogArgs) -> Result<(), String> {
    use env_logger::Target::Pipe;
    use std::fs::OpenOptions;
    use std::path::Path;

    let log_path = log_args
        .path
        .as_str()
        .map_err(|e| format!("Invalid log path: {e}"))?;
    let log_path = if log_path.is_empty() {
        std::borrow::Cow::Owned(std::env::temp_dir().join("firewood-log.txt"))
    } else {
        std::borrow::Cow::Borrowed(std::path::Path::new(log_path))
    };

    let log_dir = log_path.parent().unwrap_or_else(|| Path::new("."));
    std::fs::create_dir_all(log_dir).map_err(|e| e.to_string())?;

    let level = if log_args.filter_level.is_empty() {
        "info"
    } else {
        log_args
            .filter_level
            .as_str()
            .map_err(|e| format!("Invalid log level: {e}"))?
    }
    .parse::<log::LevelFilter>()
    .map_err(|e| format!("failed to parse log level: {e}"))?;

    let file = OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(false)
        .open(log_path)
        .map_err(|e| e.to_string())?;

    env_logger::Builder::new()
        .filter_level(level)
        .target(Pipe(Box::new(file)))
        .try_init()
        .map_err(|e| e.to_string())?;

    Ok(())
}

#[cfg(not(feature = "logger"))]
#[doc(hidden)]
fn start_logs(_log_args: &LogArgs) -> Result<(), String> {
    Err(String::from("logger feature is disabled"))
}

#[doc(hidden)]
fn manager_config(
    cache_size: usize,
    free_list_cache_size: usize,
    revisions: usize,
    strategy: u8,
) -> Result<RevisionManagerConfig, String> {
    let cache_read_strategy = match strategy {
        0 => CacheReadStrategy::WritesOnly,
        1 => CacheReadStrategy::BranchReads,
        2 => CacheReadStrategy::All,
        _ => return Err("invalid cache strategy".to_string()),
    };
    let config = RevisionManagerConfig::builder()
        .node_cache_size(
            cache_size
                .try_into()
                .map_err(|_| "cache size should be non-zero")?,
        )
        .max_revisions(revisions)
        .cache_read_strategy(cache_read_strategy)
        .free_list_cache_size(
            free_list_cache_size
                .try_into()
                .map_err(|_| "free list cache size should be non-zero")?,
        )
        .build();
    Ok(config)
}

/// Close and free the memory for a database handle
///
/// # Safety
///
/// This function uses raw pointers so it is unsafe.
/// It is the caller's responsibility to ensure that the database handle is valid.
/// Using the db after calling this function is undefined behavior
///
/// # Arguments
///
/// * `db` - The database handle to close, previously returned from a call to `open_db()`
///
/// # Panics
///
/// This function panics if:
/// * `db` is `None` (null pointer)
/// * A lock is poisoned
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_close_db(db: Option<&mut DatabaseHandle>) {
    let db_handle = db.expect("db should be non-null");

    // Explicitly clear the downstream items. Drop will do these in order, so this
    // code is defensive in case someone reorders the struct memebers of DatabaseHandle.
    db_handle
        .proposals
        .write()
        .expect("proposals lock is poisoned")
        .clear();
    db_handle.clear_cached_view();

    // The database handle will be dropped automatically when db_handle goes out of scope
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
