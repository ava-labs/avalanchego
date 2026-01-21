//! In-memory database implementation.
//!
//! This module provides a simple in-memory key-value store backed by a `BTreeMap`.
//! It's primarily useful for testing and ephemeral data storage.

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use parking_lot::RwLock;

use crate::{
    Batch, Batcher, Compacter, Database, DatabaseError, DbIterator, HealthChecker, Iteratee,
    KeyValueDeleter, KeyValueReader, KeyValueWriter, KeyValueWriterDeleter, Result,
};

/// An in-memory key-value database.
///
/// Thread-safe via `RwLock`. All values are cloned on read/write for safety.
#[derive(Debug)]
pub struct MemDb {
    data: Arc<RwLock<BTreeMap<Vec<u8>, Vec<u8>>>>,
    closed: Arc<AtomicBool>,
}

impl Default for MemDb {
    fn default() -> Self {
        Self::new()
    }
}

impl MemDb {
    /// Creates a new empty in-memory database.
    #[must_use]
    pub fn new() -> Self {
        Self {
            data: Arc::new(RwLock::new(BTreeMap::new())),
            closed: Arc::new(AtomicBool::new(false)),
        }
    }

    /// Creates a new in-memory database with initial data.
    #[must_use]
    pub fn with_data(data: BTreeMap<Vec<u8>, Vec<u8>>) -> Self {
        Self {
            data: Arc::new(RwLock::new(data)),
            closed: Arc::new(AtomicBool::new(false)),
        }
    }

    fn check_closed(&self) -> Result<()> {
        if self.closed.load(Ordering::Acquire) {
            Err(DatabaseError::Closed)
        } else {
            Ok(())
        }
    }
}

impl KeyValueReader for MemDb {
    fn has(&self, key: &[u8]) -> Result<bool> {
        self.check_closed()?;
        let data = self.data.read();
        Ok(data.contains_key(key))
    }

    fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>> {
        self.check_closed()?;
        let data = self.data.read();
        Ok(data.get(key).cloned())
    }
}

impl KeyValueWriter for MemDb {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        self.check_closed()?;
        let mut data = self.data.write();
        data.insert(key.to_vec(), value.to_vec());
        Ok(())
    }
}

impl KeyValueDeleter for MemDb {
    fn delete(&self, key: &[u8]) -> Result<()> {
        self.check_closed()?;
        let mut data = self.data.write();
        data.remove(key);
        Ok(())
    }
}

impl Batcher for MemDb {
    fn new_batch(&self) -> Box<dyn Batch> {
        Box::new(MemBatch::new(self.data.clone(), self.closed.clone()))
    }
}

impl Iteratee for MemDb {
    fn new_iterator(&self) -> Box<dyn DbIterator> {
        let data = self.data.read();
        let entries: Vec<(Vec<u8>, Vec<u8>)> = data.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
        Box::new(MemIterator::new(entries))
    }

    fn new_iterator_with_start(&self, start: &[u8]) -> Box<dyn DbIterator> {
        let data = self.data.read();
        let entries: Vec<(Vec<u8>, Vec<u8>)> = data
            .range(start.to_vec()..)
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect();
        Box::new(MemIterator::new(entries))
    }

    fn new_iterator_with_prefix(&self, prefix: &[u8]) -> Box<dyn DbIterator> {
        let data = self.data.read();
        let entries: Vec<(Vec<u8>, Vec<u8>)> = data
            .range(prefix.to_vec()..)
            .take_while(|(k, _)| k.starts_with(prefix))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect();
        Box::new(MemIterator::new(entries))
    }

    fn new_iterator_with_start_and_prefix(&self, start: &[u8], prefix: &[u8]) -> Box<dyn DbIterator> {
        let data = self.data.read();
        let effective_start = if start > prefix { start } else { prefix };
        let entries: Vec<(Vec<u8>, Vec<u8>)> = data
            .range(effective_start.to_vec()..)
            .take_while(|(k, _)| k.starts_with(prefix))
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect();
        Box::new(MemIterator::new(entries))
    }
}

impl Compacter for MemDb {
    fn compact(&self, _start: &[u8], _limit: &[u8]) -> Result<()> {
        self.check_closed()
    }
}

impl HealthChecker for MemDb {
    fn health_check(&self) -> Result<()> {
        self.check_closed()
    }
}

impl Database for MemDb {
    fn close(&self) -> Result<()> {
        self.closed.store(true, Ordering::Release);
        Ok(())
    }

    fn is_closed(&self) -> bool {
        self.closed.load(Ordering::Acquire)
    }
}

/// A batch operation (put or delete).
#[derive(Debug, Clone)]
enum BatchOp {
    Put { key: Vec<u8>, value: Vec<u8> },
    Delete { key: Vec<u8> },
}

/// A batch of operations for MemDb.
///
/// Collects operations and applies them atomically when written.
#[derive(Debug)]
pub struct MemBatch {
    /// Shared reference to the database's data
    data: Arc<RwLock<BTreeMap<Vec<u8>, Vec<u8>>>>,
    /// Shared reference to the closed flag
    closed: Arc<AtomicBool>,
    /// Accumulated operations
    ops: RwLock<Vec<BatchOp>>,
    /// Current size of operations
    size: RwLock<usize>,
    /// Whether the batch has been written
    written: AtomicBool,
}

impl MemBatch {
    /// Creates a new batch connected to the database.
    fn new(data: Arc<RwLock<BTreeMap<Vec<u8>, Vec<u8>>>>, closed: Arc<AtomicBool>) -> Self {
        Self {
            data,
            closed,
            ops: RwLock::new(Vec::new()),
            size: RwLock::new(0),
            written: AtomicBool::new(false),
        }
    }
}

impl KeyValueWriter for MemBatch {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        let mut ops = self.ops.write();
        let mut size = self.size.write();
        ops.push(BatchOp::Put {
            key: key.to_vec(),
            value: value.to_vec(),
        });
        *size += key.len() + value.len();
        Ok(())
    }
}

impl KeyValueDeleter for MemBatch {
    fn delete(&self, key: &[u8]) -> Result<()> {
        let mut ops = self.ops.write();
        let mut size = self.size.write();
        ops.push(BatchOp::Delete { key: key.to_vec() });
        *size += key.len();
        Ok(())
    }
}

impl Batch for MemBatch {
    fn size(&self) -> usize {
        *self.size.read()
    }

    fn write(&mut self) -> Result<()> {
        if self.written.load(Ordering::Acquire) {
            return Err(DatabaseError::BatchAlreadyWritten);
        }
        if self.closed.load(Ordering::Acquire) {
            return Err(DatabaseError::Closed);
        }

        let ops = self.ops.read();
        let mut data = self.data.write();
        for op in ops.iter() {
            match op {
                BatchOp::Put { key, value } => {
                    data.insert(key.clone(), value.clone());
                }
                BatchOp::Delete { key } => {
                    data.remove(key);
                }
            }
        }
        self.written.store(true, Ordering::Release);
        Ok(())
    }

    fn reset(&mut self) {
        self.ops.write().clear();
        *self.size.write() = 0;
        self.written.store(false, Ordering::Release);
    }

    fn replay(&self, writer: &dyn KeyValueWriterDeleter) -> Result<()> {
        let ops = self.ops.read();
        for op in ops.iter() {
            match op {
                BatchOp::Put { key, value } => {
                    writer.put(key, value)?;
                }
                BatchOp::Delete { key } => {
                    writer.delete(key)?;
                }
            }
        }
        Ok(())
    }
}

/// An iterator over MemDb entries.
pub struct MemIterator {
    entries: Vec<(Vec<u8>, Vec<u8>)>,
    index: usize,
    started: bool,
    error: Option<DatabaseError>,
}

impl MemIterator {
    fn new(entries: Vec<(Vec<u8>, Vec<u8>)>) -> Self {
        Self {
            entries,
            index: 0,
            started: false,
            error: None,
        }
    }
}

impl DbIterator for MemIterator {
    fn next(&mut self) -> bool {
        if !self.started {
            self.started = true;
            return !self.entries.is_empty();
        }

        self.index += 1;
        self.index < self.entries.len()
    }

    fn error(&self) -> Option<&DatabaseError> {
        self.error.as_ref()
    }

    fn key(&self) -> &[u8] {
        if self.started && self.index < self.entries.len() {
            &self.entries[self.index].0
        } else {
            &[]
        }
    }

    fn value(&self) -> &[u8] {
        if self.started && self.index < self.entries.len() {
            &self.entries[self.index].1
        } else {
            &[]
        }
    }

    fn release(&mut self) {
        self.entries.clear();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new() {
        let db = MemDb::new();
        assert!(!db.is_closed());
    }

    #[test]
    fn test_put_get() {
        let db = MemDb::new();

        db.put(b"key1", b"value1").unwrap();
        db.put(b"key2", b"value2").unwrap();

        assert_eq!(db.get(b"key1").unwrap(), Some(b"value1".to_vec()));
        assert_eq!(db.get(b"key2").unwrap(), Some(b"value2".to_vec()));
        assert_eq!(db.get(b"key3").unwrap(), None);
    }

    #[test]
    fn test_has() {
        let db = MemDb::new();

        db.put(b"key", b"value").unwrap();

        assert!(db.has(b"key").unwrap());
        assert!(!db.has(b"missing").unwrap());
    }

    #[test]
    fn test_delete() {
        let db = MemDb::new();

        db.put(b"key", b"value").unwrap();
        assert!(db.has(b"key").unwrap());

        db.delete(b"key").unwrap();
        assert!(!db.has(b"key").unwrap());

        db.delete(b"nonexistent").unwrap();
    }

    #[test]
    fn test_close() {
        let db = MemDb::new();
        db.put(b"key", b"value").unwrap();

        db.close().unwrap();
        assert!(db.is_closed());

        assert!(matches!(db.get(b"key"), Err(DatabaseError::Closed)));
        assert!(matches!(db.put(b"key", b"value"), Err(DatabaseError::Closed)));
        assert!(matches!(db.has(b"key"), Err(DatabaseError::Closed)));
    }

    #[test]
    fn test_batch() {
        let db = MemDb::new();

        let mut batch = db.new_batch();
        batch.put(b"key1", b"value1").unwrap();
        batch.put(b"key2", b"value2").unwrap();
        batch.delete(b"key1").unwrap();
        batch.write().unwrap();

        assert!(!db.has(b"key1").unwrap());
        assert!(db.has(b"key2").unwrap());
    }

    #[test]
    fn test_iterator() {
        let db = MemDb::new();
        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();
        db.put(b"c", b"3").unwrap();

        let mut iter = db.new_iterator();
        let mut results = Vec::new();

        while iter.next() {
            results.push((iter.key().to_vec(), iter.value().to_vec()));
        }
        iter.release();

        assert_eq!(
            results,
            vec![
                (b"a".to_vec(), b"1".to_vec()),
                (b"b".to_vec(), b"2".to_vec()),
                (b"c".to_vec(), b"3".to_vec()),
            ]
        );
    }

    #[test]
    fn test_iterator_with_start() {
        let db = MemDb::new();
        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();
        db.put(b"c", b"3").unwrap();

        let mut iter = db.new_iterator_with_start(b"b");
        let mut results = Vec::new();

        while iter.next() {
            results.push(iter.key().to_vec());
        }
        iter.release();

        assert_eq!(results, vec![b"b".to_vec(), b"c".to_vec()]);
    }

    #[test]
    fn test_iterator_with_prefix() {
        let db = MemDb::new();
        db.put(b"prefix/a", b"1").unwrap();
        db.put(b"prefix/b", b"2").unwrap();
        db.put(b"other/c", b"3").unwrap();

        let mut iter = db.new_iterator_with_prefix(b"prefix/");
        let mut results = Vec::new();

        while iter.next() {
            results.push(iter.key().to_vec());
        }
        iter.release();

        assert_eq!(results, vec![b"prefix/a".to_vec(), b"prefix/b".to_vec()]);
    }

    #[test]
    fn test_empty_iterator() {
        let db = MemDb::new();

        let mut iter = db.new_iterator();
        assert!(!iter.next());
        assert!(iter.key().is_empty());
        assert!(iter.value().is_empty());
    }

    #[test]
    fn test_health_check() {
        let db = MemDb::new();
        assert!(db.health_check().is_ok());

        db.close().unwrap();
        assert!(matches!(db.health_check(), Err(DatabaseError::Closed)));
    }

    #[test]
    fn test_compact() {
        let db = MemDb::new();
        assert!(db.compact(b"", b"").is_ok());
    }
}
