//! RocksDB-backed persistent database implementation.
//!
//! This module provides a production-ready persistent database using RocksDB.
//! Enable with the `rocksdb` feature flag.

use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use parking_lot::RwLock;
use rocksdb::{DBCompactionStyle, DBCompressionType, IteratorMode, Options, WriteBatch, DB};

use crate::{
    Batch, Batcher, Compacter, Database, DatabaseError, DbIterator, HealthChecker, Iteratee,
    KeyValueDeleter, KeyValueReader, KeyValueWriter, KeyValueWriterDeleter, Result,
};

/// Default cache size (256 MB).
const DEFAULT_CACHE_SIZE: usize = 256 * 1024 * 1024;

/// Default write buffer size (64 MB).
const DEFAULT_WRITE_BUFFER_SIZE: usize = 64 * 1024 * 1024;

/// Default max open files.
const DEFAULT_MAX_OPEN_FILES: i32 = 1024;

/// Configuration for RocksDB.
#[derive(Debug, Clone)]
pub struct RocksDbConfig {
    /// Path to the database directory.
    pub path: std::path::PathBuf,
    /// LRU cache size in bytes.
    pub cache_size: usize,
    /// Write buffer size in bytes.
    pub write_buffer_size: usize,
    /// Maximum number of open files.
    pub max_open_files: i32,
    /// Whether to create the database if it doesn't exist.
    pub create_if_missing: bool,
    /// Error if the database exists (for fresh databases only).
    pub error_if_exists: bool,
}

impl Default for RocksDbConfig {
    fn default() -> Self {
        Self {
            path: std::path::PathBuf::from("./db"),
            cache_size: DEFAULT_CACHE_SIZE,
            write_buffer_size: DEFAULT_WRITE_BUFFER_SIZE,
            max_open_files: DEFAULT_MAX_OPEN_FILES,
            create_if_missing: true,
            error_if_exists: false,
        }
    }
}

impl RocksDbConfig {
    /// Creates a new configuration with the given path.
    pub fn with_path<P: AsRef<Path>>(path: P) -> Self {
        Self {
            path: path.as_ref().to_path_buf(),
            ..Default::default()
        }
    }

    /// Builds RocksDB options from this configuration.
    fn build_options(&self) -> Options {
        let mut opts = Options::default();
        opts.create_if_missing(self.create_if_missing);
        opts.set_error_if_exists(self.error_if_exists);
        opts.set_max_open_files(self.max_open_files);
        opts.set_write_buffer_size(self.write_buffer_size);
        opts.set_compaction_style(DBCompactionStyle::Level);
        opts.set_compression_type(DBCompressionType::Lz4);
        opts.set_level_compaction_dynamic_level_bytes(true);
        opts.increase_parallelism(num_cpus::get() as i32);
        opts.set_allow_concurrent_memtable_write(true);
        opts.set_enable_write_thread_adaptive_yield(true);
        opts
    }
}

/// A RocksDB-backed database.
pub struct RocksDb {
    /// The underlying RocksDB instance.
    db: Arc<DB>,
    /// Whether the database is closed.
    closed: AtomicBool,
    /// Path to the database.
    path: std::path::PathBuf,
}

impl RocksDb {
    /// Opens a RocksDB database with the given configuration.
    pub fn open(config: RocksDbConfig) -> Result<Self> {
        let opts = config.build_options();
        let db = DB::open(&opts, &config.path)
            .map_err(|e| DatabaseError::Io(format!("failed to open rocksdb: {}", e)))?;

        Ok(Self {
            db: Arc::new(db),
            closed: AtomicBool::new(false),
            path: config.path,
        })
    }

    /// Opens a RocksDB database at the given path with default settings.
    pub fn open_default<P: AsRef<Path>>(path: P) -> Result<Self> {
        Self::open(RocksDbConfig::with_path(path))
    }

    /// Returns the database path.
    pub fn path(&self) -> &Path {
        &self.path
    }

    fn check_closed(&self) -> Result<()> {
        if self.closed.load(Ordering::SeqCst) {
            Err(DatabaseError::Closed)
        } else {
            Ok(())
        }
    }
}

impl KeyValueReader for RocksDb {
    fn has(&self, key: &[u8]) -> Result<bool> {
        self.check_closed()?;
        let result = self
            .db
            .get_pinned(key)
            .map_err(|e| DatabaseError::Io(e.to_string()))?;
        Ok(result.is_some())
    }

    fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>> {
        self.check_closed()?;
        self.db
            .get(key)
            .map_err(|e| DatabaseError::Io(e.to_string()))
    }
}

impl KeyValueWriter for RocksDb {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        self.check_closed()?;
        self.db
            .put(key, value)
            .map_err(|e| DatabaseError::Io(e.to_string()))
    }
}

impl KeyValueDeleter for RocksDb {
    fn delete(&self, key: &[u8]) -> Result<()> {
        self.check_closed()?;
        self.db
            .delete(key)
            .map_err(|e| DatabaseError::Io(e.to_string()))
    }
}

impl Iteratee for RocksDb {
    fn new_iterator(&self) -> Box<dyn DbIterator> {
        Box::new(RocksDbIterator::new(self.db.clone(), None, None))
    }

    fn new_iterator_with_start(&self, start: &[u8]) -> Box<dyn DbIterator> {
        Box::new(RocksDbIterator::new(
            self.db.clone(),
            Some(start.to_vec()),
            None,
        ))
    }

    fn new_iterator_with_prefix(&self, prefix: &[u8]) -> Box<dyn DbIterator> {
        Box::new(RocksDbIterator::new(
            self.db.clone(),
            None,
            Some(prefix.to_vec()),
        ))
    }

    fn new_iterator_with_start_and_prefix(&self, start: &[u8], prefix: &[u8]) -> Box<dyn DbIterator> {
        Box::new(RocksDbIterator::new(
            self.db.clone(),
            Some(start.to_vec()),
            Some(prefix.to_vec()),
        ))
    }
}

impl Batcher for RocksDb {
    fn new_batch(&self) -> Box<dyn Batch> {
        Box::new(RocksDbBatch::new(self.db.clone()))
    }
}

impl Compacter for RocksDb {
    fn compact(&self, start: &[u8], limit: &[u8]) -> Result<()> {
        self.check_closed()?;
        self.db.compact_range(Some(start), Some(limit));
        Ok(())
    }
}

impl HealthChecker for RocksDb {
    fn health_check(&self) -> Result<()> {
        self.check_closed()?;
        // Try a simple read to verify the database is functional
        let _ = self
            .db
            .get(b"__health_check__")
            .map_err(|e| DatabaseError::Io(e.to_string()))?;
        Ok(())
    }
}

impl Database for RocksDb {
    fn close(&self) -> Result<()> {
        if self.closed.swap(true, Ordering::SeqCst) {
            return Ok(()); // Already closed
        }
        // RocksDB handles cleanup on drop
        Ok(())
    }

    fn is_closed(&self) -> bool {
        self.closed.load(Ordering::SeqCst)
    }
}

/// A batch of writes for RocksDB.
///
/// Stores operations in a list and builds the WriteBatch when writing
/// to maintain thread safety (WriteBatch is not Sync).
pub struct RocksDbBatch {
    /// The underlying database.
    db: Arc<DB>,
    /// Operations for replay.
    ops: RwLock<Vec<BatchOp>>,
}

#[derive(Clone)]
enum BatchOp {
    Put(Vec<u8>, Vec<u8>),
    Delete(Vec<u8>),
}

impl RocksDbBatch {
    fn new(db: Arc<DB>) -> Self {
        Self {
            db,
            ops: RwLock::new(Vec::new()),
        }
    }
}

impl KeyValueWriter for RocksDbBatch {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        self.ops
            .write()
            .push(BatchOp::Put(key.to_vec(), value.to_vec()));
        Ok(())
    }
}

impl KeyValueDeleter for RocksDbBatch {
    fn delete(&self, key: &[u8]) -> Result<()> {
        self.ops.write().push(BatchOp::Delete(key.to_vec()));
        Ok(())
    }
}

impl Batch for RocksDbBatch {
    fn size(&self) -> usize {
        self.ops.read().len()
    }

    fn write(&mut self) -> Result<()> {
        // Build the batch from ops
        let mut batch = WriteBatch::default();
        for op in self.ops.read().iter() {
            match op {
                BatchOp::Put(key, value) => batch.put(key, value),
                BatchOp::Delete(key) => batch.delete(key),
            }
        }
        self.db
            .write(batch)
            .map_err(|e| DatabaseError::Io(e.to_string()))?;
        Ok(())
    }

    fn reset(&mut self) {
        self.ops.write().clear();
    }

    fn replay(&self, writer: &dyn KeyValueWriterDeleter) -> Result<()> {
        for op in self.ops.read().iter() {
            match op {
                BatchOp::Put(key, value) => writer.put(key, value)?,
                BatchOp::Delete(key) => writer.delete(key)?,
            }
        }
        Ok(())
    }
}

/// An iterator over RocksDB.
///
/// This implementation loads data into memory to avoid lifetime issues
/// with RocksDB's iterator API. For very large datasets, consider using
/// pagination or streaming approaches.
pub struct RocksDbIterator {
    /// Loaded key-value pairs.
    data: Vec<(Vec<u8>, Vec<u8>)>,
    /// Current position in data.
    position: usize,
    /// Error state.
    error: Option<DatabaseError>,
}

impl RocksDbIterator {
    fn new(db: Arc<DB>, start: Option<Vec<u8>>, prefix: Option<Vec<u8>>) -> Self {
        let mut data = Vec::new();
        let mut error = None;

        let mode = match &start {
            Some(start) => IteratorMode::From(start, rocksdb::Direction::Forward),
            None => match &prefix {
                Some(prefix) => IteratorMode::From(prefix, rocksdb::Direction::Forward),
                None => IteratorMode::Start,
            },
        };

        // Load all matching data into memory
        let iter = db.iterator(mode);
        for item in iter {
            match item {
                Ok((key, value)) => {
                    // Check prefix filter
                    if let Some(ref p) = prefix {
                        if !key.starts_with(p) {
                            break; // Past prefix range
                        }
                    }
                    data.push((key.to_vec(), value.to_vec()));
                }
                Err(e) => {
                    error = Some(DatabaseError::Io(e.to_string()));
                    break;
                }
            }
        }

        Self {
            data,
            position: 0,
            error,
        }
    }
}

impl DbIterator for RocksDbIterator {
    fn next(&mut self) -> bool {
        if self.position < self.data.len() {
            self.position += 1;
            true
        } else {
            false
        }
    }

    fn error(&self) -> Option<&DatabaseError> {
        self.error.as_ref()
    }

    fn key(&self) -> &[u8] {
        if self.position > 0 && self.position <= self.data.len() {
            &self.data[self.position - 1].0
        } else {
            &[]
        }
    }

    fn value(&self) -> &[u8] {
        if self.position > 0 && self.position <= self.data.len() {
            &self.data[self.position - 1].1
        } else {
            &[]
        }
    }

    fn release(&mut self) {
        self.data.clear();
    }
}

/// Get the number of CPUs (fallback if num_cpus crate isn't available).
mod num_cpus {
    pub fn get() -> usize {
        std::thread::available_parallelism()
            .map(|n| n.get())
            .unwrap_or(4)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn test_rocksdb_basic_operations() {
        let dir = tempdir().unwrap();
        let db = RocksDb::open_default(dir.path()).unwrap();

        // Test put/get
        db.put(b"key1", b"value1").unwrap();
        assert_eq!(db.get(b"key1").unwrap(), Some(b"value1".to_vec()));

        // Test has
        assert!(db.has(b"key1").unwrap());
        assert!(!db.has(b"key2").unwrap());

        // Test delete
        db.delete(b"key1").unwrap();
        assert!(!db.has(b"key1").unwrap());
        assert_eq!(db.get(b"key1").unwrap(), None);
    }

    #[test]
    fn test_rocksdb_batch() {
        let dir = tempdir().unwrap();
        let db = RocksDb::open_default(dir.path()).unwrap();

        let mut batch = db.new_batch();
        batch.put(b"key1", b"value1").unwrap();
        batch.put(b"key2", b"value2").unwrap();
        batch.delete(b"key1").unwrap();
        batch.write().unwrap();

        assert!(!db.has(b"key1").unwrap());
        assert!(db.has(b"key2").unwrap());
    }

    #[test]
    fn test_rocksdb_iterator() {
        let dir = tempdir().unwrap();
        let db = RocksDb::open_default(dir.path()).unwrap();

        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();
        db.put(b"c", b"3").unwrap();

        let mut iter = db.new_iterator();
        let mut keys = Vec::new();
        while iter.next() {
            keys.push(iter.key().to_vec());
        }
        iter.release();

        assert_eq!(keys, vec![b"a".to_vec(), b"b".to_vec(), b"c".to_vec()]);
    }

    #[test]
    fn test_rocksdb_prefix_iterator() {
        let dir = tempdir().unwrap();
        let db = RocksDb::open_default(dir.path()).unwrap();

        db.put(b"prefix/a", b"1").unwrap();
        db.put(b"prefix/b", b"2").unwrap();
        db.put(b"other/c", b"3").unwrap();

        let mut iter = db.new_iterator_with_prefix(b"prefix/");
        let mut keys = Vec::new();
        while iter.next() {
            keys.push(iter.key().to_vec());
        }
        iter.release();

        assert_eq!(
            keys,
            vec![b"prefix/a".to_vec(), b"prefix/b".to_vec()]
        );
    }

    #[test]
    fn test_rocksdb_persistence() {
        let dir = tempdir().unwrap();

        // Write data and close
        {
            let db = RocksDb::open_default(dir.path()).unwrap();
            db.put(b"persistent", b"data").unwrap();
            db.close().unwrap();
        }

        // Reopen and verify
        {
            let db = RocksDb::open_default(dir.path()).unwrap();
            assert_eq!(db.get(b"persistent").unwrap(), Some(b"data".to_vec()));
        }
    }

    #[test]
    fn test_rocksdb_health_check() {
        let dir = tempdir().unwrap();
        let db = RocksDb::open_default(dir.path()).unwrap();

        assert!(db.health_check().is_ok());
        db.close().unwrap();
        assert!(db.health_check().is_err());
    }
}
