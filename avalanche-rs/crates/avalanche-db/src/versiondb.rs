//! Version database with copy-on-write semantics.
//!
//! This module provides a database wrapper that buffers all writes in memory
//! until `commit()` is called, allowing for transactional semantics and
//! the ability to abort uncommitted changes.

use std::collections::BTreeMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use parking_lot::RwLock;

use crate::{
    Batch, Batcher, Commitable, Compacter, Database, DatabaseError, DbIterator, HealthChecker,
    Iteratee, KeyValueDeleter, KeyValueReader, KeyValueWriter, KeyValueWriterDeleter, Result,
};

/// Represents a value that may be present, deleted, or absent.
#[derive(Debug, Clone)]
enum ValueState {
    /// The value exists
    Present(Vec<u8>),
    /// The value has been deleted
    Deleted,
}

/// A database wrapper that buffers writes until commit.
///
/// This provides copy-on-write semantics where reads first check the in-memory
/// buffer, then fall through to the underlying database. Writes are accumulated
/// in memory until `commit()` is called.
pub struct VersionDb {
    /// In-memory buffer of uncommitted changes
    mem: Arc<RwLock<BTreeMap<Vec<u8>, ValueState>>>,
    /// The underlying committed database
    db: RwLock<Arc<dyn Database>>,
    /// Whether this wrapper is closed
    closed: Arc<AtomicBool>,
}

impl VersionDb {
    /// Creates a new version database wrapping the given database.
    pub fn new(db: Arc<dyn Database>) -> Self {
        Self {
            mem: Arc::new(RwLock::new(BTreeMap::new())),
            db: RwLock::new(db),
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

impl KeyValueReader for VersionDb {
    fn has(&self, key: &[u8]) -> Result<bool> {
        self.check_closed()?;

        // Check in-memory first
        {
            let mem = self.mem.read();
            if let Some(state) = mem.get(key) {
                return Ok(matches!(state, ValueState::Present(_)));
            }
        }

        // Fall through to underlying database
        let db = self.db.read();
        db.has(key)
    }

    fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>> {
        self.check_closed()?;

        // Check in-memory first
        {
            let mem = self.mem.read();
            if let Some(state) = mem.get(key) {
                return match state {
                    ValueState::Present(v) => Ok(Some(v.clone())),
                    ValueState::Deleted => Ok(None),
                };
            }
        }

        // Fall through to underlying database
        let db = self.db.read();
        db.get(key)
    }
}

impl KeyValueWriter for VersionDb {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        self.check_closed()?;

        let mut mem = self.mem.write();
        mem.insert(key.to_vec(), ValueState::Present(value.to_vec()));
        Ok(())
    }
}

impl KeyValueDeleter for VersionDb {
    fn delete(&self, key: &[u8]) -> Result<()> {
        self.check_closed()?;

        let mut mem = self.mem.write();
        mem.insert(key.to_vec(), ValueState::Deleted);
        Ok(())
    }
}

impl Batcher for VersionDb {
    fn new_batch(&self) -> Box<dyn Batch> {
        Box::new(VersionBatch::new(self.mem.clone(), self.closed.clone()))
    }
}

impl Iteratee for VersionDb {
    fn new_iterator(&self) -> Box<dyn DbIterator> {
        let mem = self.mem.read();
        let mem_snapshot: BTreeMap<Vec<u8>, ValueState> = mem.clone();
        let db = self.db.read();
        let inner_iter = db.new_iterator();
        Box::new(VersionIterator::new(inner_iter, mem_snapshot, None, None))
    }

    fn new_iterator_with_start(&self, start: &[u8]) -> Box<dyn DbIterator> {
        let mem = self.mem.read();
        let mem_snapshot: BTreeMap<Vec<u8>, ValueState> = mem.clone();
        let db = self.db.read();
        let inner_iter = db.new_iterator_with_start(start);
        Box::new(VersionIterator::new(
            inner_iter,
            mem_snapshot,
            Some(start.to_vec()),
            None,
        ))
    }

    fn new_iterator_with_prefix(&self, prefix: &[u8]) -> Box<dyn DbIterator> {
        let mem = self.mem.read();
        let mem_snapshot: BTreeMap<Vec<u8>, ValueState> = mem.clone();
        let db = self.db.read();
        let inner_iter = db.new_iterator_with_prefix(prefix);
        Box::new(VersionIterator::new(
            inner_iter,
            mem_snapshot,
            None,
            Some(prefix.to_vec()),
        ))
    }

    fn new_iterator_with_start_and_prefix(&self, start: &[u8], prefix: &[u8]) -> Box<dyn DbIterator> {
        let mem = self.mem.read();
        let mem_snapshot: BTreeMap<Vec<u8>, ValueState> = mem.clone();
        let db = self.db.read();
        let inner_iter = db.new_iterator_with_start_and_prefix(start, prefix);
        Box::new(VersionIterator::new(
            inner_iter,
            mem_snapshot,
            Some(start.to_vec()),
            Some(prefix.to_vec()),
        ))
    }
}

impl Compacter for VersionDb {
    fn compact(&self, start: &[u8], limit: &[u8]) -> Result<()> {
        self.check_closed()?;
        let db = self.db.read();
        db.compact(start, limit)
    }
}

impl HealthChecker for VersionDb {
    fn health_check(&self) -> Result<()> {
        self.check_closed()?;
        let db = self.db.read();
        db.health_check()
    }
}

impl Database for VersionDb {
    fn close(&self) -> Result<()> {
        self.closed.store(true, Ordering::Release);
        Ok(())
    }

    fn is_closed(&self) -> bool {
        if self.closed.load(Ordering::Acquire) {
            return true;
        }
        let db = self.db.read();
        db.is_closed()
    }
}

impl Commitable for VersionDb {
    fn commit(&self) -> Result<()> {
        self.check_closed()?;

        let mut mem = self.mem.write();
        if mem.is_empty() {
            return Ok(());
        }

        let db = self.db.read();
        let mut batch = db.new_batch();

        // Apply all changes via batch
        for (key, state) in mem.iter() {
            match state {
                ValueState::Present(value) => {
                    batch.put(key, value)?;
                }
                ValueState::Deleted => {
                    batch.delete(key)?;
                }
            }
        }

        batch.write()?;
        mem.clear();
        Ok(())
    }

    fn abort(&self) {
        let mut mem = self.mem.write();
        mem.clear();
    }

    fn commit_batch(&self) -> Result<Box<dyn Batch>> {
        self.check_closed()?;

        let mem = self.mem.read();
        let db = self.db.read();
        let batch = db.new_batch();

        for (key, state) in mem.iter() {
            match state {
                ValueState::Present(value) => {
                    batch.put(key, value)?;
                }
                ValueState::Deleted => {
                    batch.delete(key)?;
                }
            }
        }

        Ok(batch)
    }

    fn set_database(&self, new_db: Arc<dyn Database>) -> Result<()> {
        self.check_closed()?;

        // Commit any pending changes first
        self.commit()?;

        let mut db = self.db.write();
        *db = new_db;
        Ok(())
    }

    fn get_database(&self) -> Arc<dyn Database> {
        let db = self.db.read();
        db.clone()
    }
}

/// A batch for VersionDb that applies to the in-memory buffer.
pub struct VersionBatch {
    /// Shared reference to the VersionDb's memory buffer
    mem: Arc<RwLock<BTreeMap<Vec<u8>, ValueState>>>,
    /// Shared reference to the closed flag
    closed: Arc<AtomicBool>,
    /// Accumulated operations
    ops: RwLock<Vec<(Vec<u8>, ValueState)>>,
    /// Whether the batch has been written
    written: AtomicBool,
}

impl VersionBatch {
    fn new(mem: Arc<RwLock<BTreeMap<Vec<u8>, ValueState>>>, closed: Arc<AtomicBool>) -> Self {
        Self {
            mem,
            closed,
            ops: RwLock::new(Vec::new()),
            written: AtomicBool::new(false),
        }
    }
}

impl KeyValueWriter for VersionBatch {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        let mut ops = self.ops.write();
        ops.push((key.to_vec(), ValueState::Present(value.to_vec())));
        Ok(())
    }
}

impl KeyValueDeleter for VersionBatch {
    fn delete(&self, key: &[u8]) -> Result<()> {
        let mut ops = self.ops.write();
        ops.push((key.to_vec(), ValueState::Deleted));
        Ok(())
    }
}

impl Batch for VersionBatch {
    fn size(&self) -> usize {
        let ops = self.ops.read();
        ops.iter()
            .map(|(k, v)| {
                k.len()
                    + match v {
                        ValueState::Present(val) => val.len(),
                        ValueState::Deleted => 0,
                    }
            })
            .sum()
    }

    fn write(&mut self) -> Result<()> {
        if self.written.load(Ordering::Acquire) {
            return Err(DatabaseError::BatchAlreadyWritten);
        }
        if self.closed.load(Ordering::Acquire) {
            return Err(DatabaseError::Closed);
        }

        let ops = self.ops.read();
        let mut mem = self.mem.write();
        for (key, state) in ops.iter() {
            mem.insert(key.clone(), state.clone());
        }
        self.written.store(true, Ordering::Release);
        Ok(())
    }

    fn reset(&mut self) {
        let mut ops = self.ops.write();
        ops.clear();
        self.written.store(false, Ordering::Release);
    }

    fn replay(&self, writer: &dyn KeyValueWriterDeleter) -> Result<()> {
        let ops = self.ops.read();
        for (key, state) in ops.iter() {
            match state {
                ValueState::Present(value) => writer.put(key, value)?,
                ValueState::Deleted => writer.delete(key)?,
            }
        }
        Ok(())
    }
}

/// An iterator that merges in-memory and underlying database entries.
pub struct VersionIterator {
    /// Pre-computed merged entries
    entries: Vec<(Vec<u8>, Vec<u8>)>,
    index: usize,
    started: bool,
    error: Option<DatabaseError>,
}

impl VersionIterator {
    fn new(
        mut inner: Box<dyn DbIterator>,
        mem: BTreeMap<Vec<u8>, ValueState>,
        start: Option<Vec<u8>>,
        prefix: Option<Vec<u8>>,
    ) -> Self {
        // Collect all inner entries that aren't overridden by memory
        let mut entries: BTreeMap<Vec<u8>, Vec<u8>> = BTreeMap::new();

        while inner.next() {
            let key = inner.key().to_vec();
            // Only include if not overridden in memory
            if !mem.contains_key(&key) {
                entries.insert(key, inner.value().to_vec());
            }
        }

        // Add memory entries (Present only, skip Deleted)
        for (key, state) in &mem {
            // Check start/prefix filters
            let after_start = start.as_ref().map_or(true, |s| key.as_slice() >= s.as_slice());
            let has_prefix = prefix.as_ref().map_or(true, |p| key.starts_with(p));

            if after_start && has_prefix {
                if let ValueState::Present(value) = state {
                    entries.insert(key.clone(), value.clone());
                }
            }
        }

        // Convert to sorted vec
        let entries: Vec<(Vec<u8>, Vec<u8>)> = entries.into_iter().collect();

        Self {
            entries,
            index: 0,
            started: false,
            error: None,
        }
    }
}

impl DbIterator for VersionIterator {
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
    use crate::MemDb;

    #[test]
    fn test_version_basic() {
        let inner = Arc::new(MemDb::new());
        let db = VersionDb::new(inner.clone());

        db.put(b"key", b"value").unwrap();
        assert_eq!(db.get(b"key").unwrap(), Some(b"value".to_vec()));

        // Not committed yet - inner should be empty
        assert!(!inner.has(b"key").unwrap());

        db.commit().unwrap();

        // Now inner should have the value
        assert!(inner.has(b"key").unwrap());
    }

    #[test]
    fn test_version_abort() {
        let inner = Arc::new(MemDb::new());
        let db = VersionDb::new(inner.clone());

        db.put(b"key", b"value").unwrap();
        assert!(db.has(b"key").unwrap());

        db.abort();

        assert!(!db.has(b"key").unwrap());
        assert!(!inner.has(b"key").unwrap());
    }

    #[test]
    fn test_version_delete() {
        let inner = Arc::new(MemDb::new());
        inner.put(b"existing", b"value").unwrap();

        let db = VersionDb::new(inner.clone());

        // Delete existing key
        db.delete(b"existing").unwrap();
        assert!(!db.has(b"existing").unwrap());

        // Inner still has it until commit
        assert!(inner.has(b"existing").unwrap());

        db.commit().unwrap();

        // Now inner should not have it
        assert!(!inner.has(b"existing").unwrap());
    }

    #[test]
    fn test_version_read_through() {
        let inner = Arc::new(MemDb::new());
        inner.put(b"inner_key", b"inner_value").unwrap();

        let db = VersionDb::new(inner);

        // Should read through to inner
        assert!(db.has(b"inner_key").unwrap());
        assert_eq!(
            db.get(b"inner_key").unwrap(),
            Some(b"inner_value".to_vec())
        );
    }

    #[test]
    fn test_version_override() {
        let inner = Arc::new(MemDb::new());
        inner.put(b"key", b"old_value").unwrap();

        let db = VersionDb::new(inner.clone());

        // Override in memory
        db.put(b"key", b"new_value").unwrap();

        // Should see new value
        assert_eq!(db.get(b"key").unwrap(), Some(b"new_value".to_vec()));

        // Inner still has old value
        assert_eq!(inner.get(b"key").unwrap(), Some(b"old_value".to_vec()));

        db.commit().unwrap();

        // Now inner has new value
        assert_eq!(inner.get(b"key").unwrap(), Some(b"new_value".to_vec()));
    }

    #[test]
    fn test_version_iterator() {
        let inner = Arc::new(MemDb::new());
        inner.put(b"a", b"1").unwrap();
        inner.put(b"c", b"3").unwrap();

        let db = VersionDb::new(inner);

        // Add and delete in memory
        db.put(b"b", b"2").unwrap();
        db.delete(b"c").unwrap();

        let mut iter = db.new_iterator();
        let mut results = Vec::new();

        while iter.next() {
            results.push((iter.key().to_vec(), iter.value().to_vec()));
        }
        iter.release();

        // Should have 'a' from inner, 'b' from memory, but not 'c' (deleted)
        assert_eq!(
            results,
            vec![(b"a".to_vec(), b"1".to_vec()), (b"b".to_vec(), b"2".to_vec()),]
        );
    }

    #[test]
    fn test_set_database() {
        let inner1 = Arc::new(MemDb::new());
        let inner2 = Arc::new(MemDb::new());

        let db = VersionDb::new(inner1.clone());
        db.put(b"key", b"value").unwrap();

        // This should commit to inner1 first
        db.set_database(inner2.clone()).unwrap();

        // inner1 should have the committed value
        assert!(inner1.has(b"key").unwrap());

        // New writes go to inner2
        db.put(b"key2", b"value2").unwrap();
        db.commit().unwrap();

        assert!(!inner1.has(b"key2").unwrap());
        assert!(inner2.has(b"key2").unwrap());
    }

    #[test]
    fn test_version_close() {
        let inner = Arc::new(MemDb::new());
        let db = VersionDb::new(inner);

        db.put(b"key", b"value").unwrap();
        db.close().unwrap();

        assert!(db.is_closed());
        assert!(matches!(db.get(b"key"), Err(DatabaseError::Closed)));
    }
}
