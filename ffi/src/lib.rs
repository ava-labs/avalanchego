// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::collections::HashMap;
use std::ffi::{CStr, CString, OsStr, c_char};
use std::fmt::{self, Display, Formatter};
use std::ops::Deref;
#[cfg(unix)]
use std::os::unix::ffi::OsStrExt as _;
use std::path::Path;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::{Arc, RwLock};

use firewood::db::{BatchOp as DbBatchOp, Db, DbConfig, DbViewSync as _, Proposal};
use firewood::manager::{CacheReadStrategy, RevisionManagerConfig};

use metrics::counter;

#[doc(hidden)]
mod metrics_setup;

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
pub struct DatabaseHandle<'p> {
    /// List of oustanding proposals, by ID
    // Keep proposals first, as they must be dropped before the database handle is dropped due to lifetime
    // issues.
    proposals: RwLock<HashMap<ProposalId, Arc<Proposal<'p>>>>,
    /// The database
    db: Db,
}

impl From<Db> for DatabaseHandle<'_> {
    fn from(db: Db) -> Self {
        Self {
            db,
            proposals: RwLock::new(HashMap::new()),
        }
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
/// * `key` - The key to look up, in `Value` form
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
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_latest(db: *const DatabaseHandle, key: Value) -> Value {
    get_latest(db, &key).unwrap_or_else(Into::into)
}

/// This function is not exposed to the C API.
/// Internal call for `fwd_get_latest` to remove error handling from the C API
#[doc(hidden)]
fn get_latest(db: *const DatabaseHandle, key: &Value) -> Result<Value, String> {
    // Check db is valid.
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;

    // Find root hash.
    // Matches `hash` function but we use the TrieHash type here
    let Some(root) = db.root_hash_sync().map_err(|e| e.to_string())? else {
        return Ok(Value::default());
    };

    // Find revision assoicated with root.
    let rev = db.revision_sync(root).map_err(|e| e.to_string())?;

    // Get value associated with key.
    let value = rev
        .val_sync(key.as_slice())
        .map_err(|e| e.to_string())?
        .ok_or_else(String::new)?;
    Ok(value.into())
}

/// Gets the value associated with the given key from the proposal provided.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `id` - The ID of the proposal to get the value from
/// * `key` - The key to look up, in `Value` form
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
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_from_proposal(
    db: *const DatabaseHandle,
    id: ProposalId,
    key: Value,
) -> Value {
    get_from_proposal(db, id, &key).unwrap_or_else(Into::into)
}

/// This function is not exposed to the C API.
/// Internal call for `fwd_get_from_proposal` to remove error handling from the C API
#[doc(hidden)]
fn get_from_proposal(
    db: *const DatabaseHandle,
    id: ProposalId,
    key: &Value,
) -> Result<Value, String> {
    // Check db is valid.
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;

    // Get proposal from ID.
    let proposals = db
        .proposals
        .read()
        .map_err(|_| "proposal lock is poisoned")?;
    let proposal = proposals
        .get(&id)
        .ok_or_else(|| String::from("proposal not found"))?;

    // Get value associated with key.
    let value = proposal
        .val_sync(key.as_slice())
        .map_err(|e| e.to_string())?
        .ok_or_else(String::new)?;
    Ok(value.into())
}

/// Gets a value assoicated with the given historical root hash and key.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `root` - The root hash to look up, in `Value` form
/// * `key` - The key to look up, in `Value` form
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
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get_from_root(
    db: *const DatabaseHandle,
    root: Value,
    key: Value,
) -> Value {
    get_from_root(db, &root, &key).unwrap_or_else(Into::into)
}

/// Internal call for `fwd_get_from_root` to remove error handling from the C API
#[doc(hidden)]
fn get_from_root(db: *const DatabaseHandle, root: &Value, key: &Value) -> Result<Value, String> {
    // Check db is valid.
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;

    // Get the revision associated with the root hash.
    let rev = db
        .revision_sync(root.as_slice().try_into()?)
        .map_err(|e| e.to_string())?;

    // Get value associated with key.
    let value = rev
        .val_sync(key.as_slice())
        .map_err(|e| e.to_string())?
        .ok_or_else(String::new)?;
    Ok(value.into())
}

/// A `KeyValue` represents a key-value pair, passed to the FFI.
#[repr(C)]
#[allow(unused)]
#[unsafe(no_mangle)]
pub struct KeyValue {
    key: Value,
    value: Value,
}

/// Puts the given key-value pairs into the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
/// * `nkeys` - The number of key-value pairs to put
/// * `values` - A pointer to an array of `KeyValue` structs
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
    db: *const DatabaseHandle,
    nkeys: usize,
    values: *const KeyValue,
) -> Value {
    batch(db, nkeys, values).unwrap_or_else(Into::into)
}

/// Converts a slice of `KeyValue` structs to a vector of `DbBatchOp` structs.
///
/// # Arguments
///
/// * `values` - A slice of `KeyValue` structs
///
/// # Returns
fn convert_to_batch(values: &[KeyValue]) -> Vec<DbBatchOp<&[u8], &[u8]>> {
    let mut batch = Vec::with_capacity(values.len());
    for kv in values {
        if kv.value.len == 0 {
            batch.push(DbBatchOp::DeleteRange {
                prefix: kv.key.as_slice(),
            });
        } else {
            batch.push(DbBatchOp::Put {
                key: kv.key.as_slice(),
                value: kv.value.as_slice(),
            });
        }
    }
    batch
}

/// Internal call for `fwd_batch` to remove error handling from the C API
#[doc(hidden)]
fn batch(
    db: *const DatabaseHandle,
    nkeys: usize,
    values: *const KeyValue,
) -> Result<Value, String> {
    let start = coarsetime::Instant::now();
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;
    if values.is_null() {
        return Err(String::from("key-value list is null"));
    }

    // Create a batch of operations to perform.
    let key_value_ref = unsafe { std::slice::from_raw_parts(values, nkeys) };
    let batch = convert_to_batch(key_value_ref);

    // Propose the batch of operations.
    let proposal = db.propose_sync(batch).map_err(|e| e.to_string())?;
    let propose_time = start.elapsed().as_millis();
    counter!("firewood.ffi.propose_ms").increment(propose_time);

    let hash_val = proposal
        .root_hash_sync()
        .map_err(|e| e.to_string())?
        .ok_or_else(|| String::from("Proposed revision is empty"))?
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
/// * `nkeys` - The number of key-value pairs to put
/// * `values` - A pointer to an array of `KeyValue` structs
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
pub unsafe extern "C" fn fwd_propose_on_db(
    db: *const DatabaseHandle,
    nkeys: usize,
    values: *const KeyValue,
) -> Value {
    // Note: the id is guaranteed to be non-zero
    // because we use an atomic counter that starts at 1.
    propose_on_db(db, nkeys, values).unwrap_or_else(Into::into)
}

/// Internal call for `fwd_propose_on_db` to remove error handling from the C API
#[doc(hidden)]
fn propose_on_db(
    db: *const DatabaseHandle,
    nkeys: usize,
    values: *const KeyValue,
) -> Result<Value, String> {
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;
    if values.is_null() {
        return Err(String::from("key-value list is null"));
    }

    // Create a batch of operations to perform.
    let key_value_ref = unsafe { std::slice::from_raw_parts(values, nkeys) };
    let batch = convert_to_batch(key_value_ref);

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
/// * `nkeys` - The number of key-value pairs to put
/// * `values` - A pointer to an array of `KeyValue` structs
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
    db: *const DatabaseHandle,
    proposal_id: ProposalId,
    nkeys: usize,
    values: *const KeyValue,
) -> Value {
    // Note: the id is guaranteed to be non-zero
    // because we use an atomic counter that starts at 1.
    propose_on_proposal(db, proposal_id, nkeys, values).unwrap_or_else(Into::into)
}

/// Internal call for `fwd_propose_on_proposal` to remove error handling from the C API
#[doc(hidden)]
fn propose_on_proposal(
    db: *const DatabaseHandle,
    proposal_id: ProposalId,
    nkeys: usize,
    values: *const KeyValue,
) -> Result<Value, String> {
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;
    if values.is_null() {
        return Err(String::from("key-value list is null"));
    }

    // Create a batch of operations to perform.
    let key_value_ref = unsafe { std::slice::from_raw_parts(values, nkeys) };
    let batch = convert_to_batch(key_value_ref);

    // Get proposal from ID.
    // We need write access to add the proposal after we create it.
    let guard = db
        .proposals
        .write()
        .expect("failed to acquire write lock on proposals");
    let proposal = guard
        .get(&proposal_id)
        .ok_or_else(|| String::from("proposal not found"))?;
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
pub unsafe extern "C" fn fwd_commit(db: *const DatabaseHandle, proposal_id: u32) -> Value {
    commit(db, proposal_id).map_or_else(Into::into, Into::into)
}

/// Internal call for `fwd_commit` to remove error handling from the C API
#[doc(hidden)]
fn commit(db: *const DatabaseHandle, proposal_id: u32) -> Result<(), String> {
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;
    let proposal = db
        .proposals
        .write()
        .map_err(|_| "proposal lock is poisoned")?
        .remove(&proposal_id)
        .ok_or_else(|| String::from("proposal not found"))?;
    proposal.commit_sync().map_err(|e| e.to_string())
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
pub unsafe extern "C" fn fwd_drop_proposal(db: *const DatabaseHandle, proposal_id: u32) -> Value {
    drop_proposal(db, proposal_id).map_or_else(Into::into, Into::into)
}

/// Internal call for `fwd_drop_proposal` to remove error handling from the C API
#[doc(hidden)]
fn drop_proposal(db: *const DatabaseHandle, proposal_id: u32) -> Result<(), String> {
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;
    let mut proposals = db
        .proposals
        .write()
        .map_err(|_| "proposal lock is poisoned")?;
    proposals
        .remove(&proposal_id)
        .ok_or_else(|| String::from("proposal not found"))?;
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
pub unsafe extern "C" fn fwd_root_hash(db: *const DatabaseHandle) -> Value {
    // Check db is valid.
    root_hash(db).unwrap_or_else(Into::into)
}

/// This function is not exposed to the C API.
/// Internal call for `fwd_root_hash` to remove error handling from the C API
#[doc(hidden)]
fn root_hash(db: *const DatabaseHandle) -> Result<Value, String> {
    // Check db is valid.
    let db = unsafe { db.as_ref() }.ok_or_else(|| String::from("db should be non-null"))?;

    // Get the root hash of the database.
    hash(db)
}

/// This function is not exposed to the C API.
/// It returns the current hash of an already-fetched database handle
#[doc(hidden)]
fn hash(db: &Db) -> Result<Value, String> {
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
#[derive(Debug)]
#[repr(C)]
pub struct Value {
    pub len: usize,
    pub data: *const u8,
}

impl Display for Value {
    fn fmt(&self, f: &mut Formatter) -> fmt::Result {
        match (self.len, self.data.is_null()) {
            (0, true) => write!(f, "[not found]"),
            (0, false) => write!(f, "[error] {}", unsafe {
                CStr::from_ptr(self.data.cast::<c_char>()).to_string_lossy()
            }),
            (len, true) => write!(f, "[id] {len}"),
            (_, false) => write!(f, "[data] {:?}", self.as_slice()),
        }
    }
}

impl Default for Value {
    fn default() -> Self {
        Self {
            len: 0,
            data: std::ptr::null(),
        }
    }
}

impl Value {
    #[must_use]
    pub const fn as_slice(&self) -> &[u8] {
        if self.data.is_null() {
            &[]
        } else {
            unsafe { std::slice::from_raw_parts(self.data, self.len) }
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
        let data = Box::leak(data).as_ptr();
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
                data: cstr.cast::<u8>(),
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
            data: std::ptr::null(),
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
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_free_value(value: *const Value) {
    // Check value is valid.
    let value = unsafe { value.as_ref() }.expect("value should be non-null");

    if value.data.is_null() {
        return; // nothing to free, but valid behavior.
    }

    // We assume that if the length is 0, then the data is a null-terminated string.
    if value.len > 0 {
        let recreated_box = unsafe {
            Box::from_raw(std::slice::from_raw_parts_mut(
                value.data.cast_mut(),
                value.len,
            ))
        };
        drop(recreated_box);
    } else {
        let raw_str = value.data as *mut c_char;
        let cstr = unsafe { CString::from_raw(raw_str) };
        drop(cstr);
    }
}

/// Common arguments, accepted by both `fwd_create_db()` and `fwd_open_db()`.
///
/// * `path` - The path to the database file, which will be truncated if passed to `fwd_create_db()`
///   otherwise should exist if passed to `fwd_open_db()`.
/// * `cache_size` - The size of the node cache, panics if <= 0
/// * `revisions` - The maximum number of revisions to keep; firewood currently requires this to be at least 2
#[repr(C)]
pub struct CreateOrOpenArgs {
    path: *const std::ffi::c_char,
    cache_size: usize,
    revisions: usize,
    strategy: u8,
    metrics_port: u16,
}

/// Create a database with the given cache size and maximum number of revisions, as well
/// as a specific cache strategy
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
pub unsafe extern "C" fn fwd_create_db(args: CreateOrOpenArgs) -> *const DatabaseHandle<'static> {
    let cfg = DbConfig::builder()
        .truncate(true)
        .manager(manager_config(
            args.cache_size,
            args.revisions,
            args.strategy,
        ))
        .build();
    unsafe { common_create(args.path, args.metrics_port, cfg) }
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
pub unsafe extern "C" fn fwd_open_db(args: CreateOrOpenArgs) -> *const DatabaseHandle<'static> {
    let cfg = DbConfig::builder()
        .truncate(false)
        .manager(manager_config(
            args.cache_size,
            args.revisions,
            args.strategy,
        ))
        .build();
    unsafe { common_create(args.path, args.metrics_port, cfg) }
}

/// Internal call for `fwd_create_db` and `fwd_open_db` to remove error handling from the C API
#[doc(hidden)]
unsafe fn common_create(
    path: *const std::ffi::c_char,
    metrics_port: u16,
    cfg: DbConfig,
) -> *const DatabaseHandle<'static> {
    #[cfg(feature = "logger")]
    let _ = env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info"))
        .try_init();

    let path = unsafe { CStr::from_ptr(path) };
    #[cfg(unix)]
    let path: &Path = OsStr::from_bytes(path.to_bytes()).as_ref();
    #[cfg(windows)]
    let path: &Path = OsStr::new(path.to_str().expect("path should be valid UTF-8")).as_ref();
    if metrics_port > 0 {
        metrics_setup::setup_metrics(metrics_port);
    }
    let db = Db::new_sync(path, cfg).expect("db initialization should succeed");
    Box::into_raw(Box::new(db.into()))
}

#[doc(hidden)]
fn manager_config(cache_size: usize, revisions: usize, strategy: u8) -> RevisionManagerConfig {
    let cache_read_strategy = match strategy {
        0 => CacheReadStrategy::WritesOnly,
        1 => CacheReadStrategy::BranchReads,
        2 => CacheReadStrategy::All,
        _ => panic!("invalid cache strategy"),
    };
    RevisionManagerConfig::builder()
        .node_cache_size(
            cache_size
                .try_into()
                .expect("cache size should always be non-zero"),
        )
        .max_revisions(revisions)
        .cache_read_strategy(cache_read_strategy)
        .build()
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
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_close_db(db: *mut DatabaseHandle) {
    let _ = unsafe { Box::from_raw(db) };
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
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
            data: cstr.as_ptr().cast::<u8>(),
        };
        assert_eq!(format!("{value}"), "[error] test");
    }

    #[test]
    fn test_value_display_with_data() {
        let value = Value {
            len: 4,
            data: Box::leak(b"test".to_vec().into_boxed_slice()).as_ptr(),
        };
        assert_eq!(format!("{value}"), "[data] [116, 101, 115, 116]");
    }

    #[test]
    fn test_value_display_with_id() {
        let value = Value {
            len: 4,
            data: std::ptr::null(),
        };
        assert_eq!(format!("{value}"), "[id] 4");
    }
}
