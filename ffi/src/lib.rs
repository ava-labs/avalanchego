// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::ffi::{CStr, OsStr};
use std::fmt::{self, Display, Formatter};
use std::os::unix::ffi::OsStrExt as _;
use std::path::Path;

use firewood::db::{BatchOp as DbBatchOp, Db, DbConfig, DbViewSync as _};
use firewood::manager::{CacheReadStrategy, RevisionManagerConfig};

mod metrics_setup;

use metrics::counter;

#[global_allocator]
static GLOBAL: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

#[derive(Debug)]
#[repr(C)]
pub struct Value {
    pub len: usize,
    pub data: *const u8,
}

impl Display for Value {
    fn fmt(&self, f: &mut Formatter) -> fmt::Result {
        write!(f, "{:?}", self.as_slice())
    }
}

/// Gets the value associated with the given key from the database.
///
/// # Arguments
///
/// * `db` - The database handle returned by `open_db`
///
/// # Safety
///
/// The caller must:
///  * ensure that `db` is a valid pointer returned by `open_db`
///  * ensure that `key` is a valid pointer to a `Value` struct
///  * call `free_value` to free the memory associated with the returned `Value`
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_get(db: *mut Db, key: Value) -> Value {
    let db = unsafe { db.as_ref() }.expect("db should be non-null");
    let root = db.root_hash_sync();
    let Ok(Some(root)) = root else {
        return Value {
            len: 0,
            data: std::ptr::null(),
        };
    };
    let rev = db.revision_sync(root).expect("revision should exist");
    let value = rev
        .val_sync(key.as_slice())
        .expect("get should succeed")
        .unwrap_or_default();
    value.into()
}

/// A `KeyValue` struct that represents a key-value pair in the database.
#[repr(C)]
#[allow(unused)]
#[unsafe(no_mangle)]
pub struct KeyValue {
    key: Value,
    value: Value,
}

/// Puts the given key-value pairs into the database.
///
/// # Returns
///
/// The current root hash of the database, in Value form.
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
pub unsafe extern "C" fn fwd_batch(db: *mut Db, nkeys: usize, values: *const KeyValue) -> Value {
    let start = coarsetime::Instant::now();
    let db = unsafe { db.as_ref() }.expect("db should be non-null");
    let mut batch = Vec::with_capacity(nkeys);
    for i in 0..nkeys {
        let kv = unsafe { values.add(i).as_ref() }.expect("values should be non-null");
        if kv.value.len == 0 {
            batch.push(DbBatchOp::DeleteRange {
                prefix: kv.key.as_slice(),
            });
            continue;
        }
        batch.push(DbBatchOp::Put {
            key: kv.key.as_slice(),
            value: kv.value.as_slice(),
        });
    }
    let proposal = db.propose_sync(batch).expect("proposal should succeed");
    let propose_time = start.elapsed().as_millis();
    counter!("firewood.ffi.propose_ms").increment(propose_time);
    proposal.commit_sync().expect("commit should succeed");
    let hash = hash(db);
    let propose_plus_commit_time = start.elapsed().as_millis();
    counter!("firewood.ffi.batch_ms").increment(propose_plus_commit_time);
    counter!("firewood.ffi.commit_ms").increment(propose_plus_commit_time - propose_time);
    counter!("firewood.ffi.batch").increment(1);
    hash
}

/// Get the root hash of the latest version of the database
/// Don't forget to call `free_value` to free the memory associated with the returned `Value`.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must ensure that `db` is a valid pointer returned by `open_db`
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_root_hash(db: *mut Db) -> Value {
    let db = unsafe { db.as_ref() }.expect("db should be non-null");
    hash(db)
}

/// cbindgen::ignore
///
/// This function is not exposed to the C API.
/// It returns the current hash of an already-fetched database handle
fn hash(db: &Db) -> Value {
    let root = db.root_hash_sync().unwrap_or_default().unwrap_or_default();
    Value::from(root.as_slice())
}

impl Value {
    pub fn as_slice(&self) -> &[u8] {
        unsafe { std::slice::from_raw_parts(self.data, self.len) }
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

/// Frees the memory associated with a `Value`.
///
/// # Safety
///
/// This function is unsafe because it dereferences raw pointers.
/// The caller must ensure that `value` is a valid pointer.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_free_value(value: *const Value) {
    let value = unsafe { &*value as &Value };
    if value.len == 0 {
        return;
    }
    let recreated_box = unsafe {
        Box::from_raw(std::slice::from_raw_parts_mut(
            value.data as *mut u8,
            value.len,
        ))
    };
    drop(recreated_box);
}

/// Common arguments, accepted by both `fwd_create_db()` and `fwd_open_db()`.
///
/// * `path` - The path to the database file, which will be truncated if passed to `fwd_create_db()`
///    otherwise should exist if passed to `fwd_open_db()`.
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
pub unsafe extern "C" fn fwd_create_db(args: CreateOrOpenArgs) -> *mut Db {
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
pub unsafe extern "C" fn fwd_open_db(args: CreateOrOpenArgs) -> *mut Db {
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

unsafe fn common_create(
    path: *const std::ffi::c_char,
    metrics_port: u16,
    cfg: DbConfig,
) -> *mut Db {
    #[cfg(feature = "logger")]
    let _ = env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info"))
        .try_init();

    let path = unsafe { CStr::from_ptr(path) };
    let path: &Path = OsStr::from_bytes(path.to_bytes()).as_ref();
    if metrics_port > 0 {
        metrics_setup::setup_metrics(metrics_port);
    }
    Box::into_raw(Box::new(
        Db::new_sync(path, cfg).expect("db initialization should succeed"),
    ))
}

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

/// Close iand free the memory for a database handle
///
/// # Safety
///
/// This function uses raw pointers so it is unsafe.
/// It is the caller's responsibility to ensure that the database handle is valid.
/// Using the db after calling this function is undefined behavior
///
/// # Arguments
///
/// * `db` - The database handle to close, previously returned from a call to open_db()
#[unsafe(no_mangle)]
pub unsafe extern "C" fn fwd_close_db(db: *mut Db) {
    let _ = unsafe { Box::from_raw(db) };
}
