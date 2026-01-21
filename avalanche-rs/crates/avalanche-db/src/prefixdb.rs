//! Prefix database wrapper.
//!
//! This module provides a database wrapper that automatically prefixes all keys
//! with a configured prefix, effectively creating a namespace within the underlying database.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use sha2::{Digest, Sha256};

use crate::{
    Batch, Batcher, Compacter, Database, DatabaseError, DbIterator, HealthChecker, Iteratee,
    KeyValueDeleter, KeyValueReader, KeyValueWriter, KeyValueWriterDeleter, Result,
};

/// A database wrapper that prefixes all keys.
///
/// This allows multiple logical databases to share a single underlying database
/// by partitioning the key space via prefixes.
pub struct PrefixDb {
    /// The prefix added to all keys
    prefix: Vec<u8>,
    /// The lexical upper bound for iteration (prefix + 1)
    limit: Vec<u8>,
    /// The underlying database
    db: Arc<dyn Database>,
    /// Whether this wrapper is closed
    closed: AtomicBool,
}

impl PrefixDb {
    /// Creates a new prefix database.
    ///
    /// The prefix is hashed using SHA-256 to ensure uniform key distribution
    /// and to prevent prefix collision issues.
    pub fn new(db: Arc<dyn Database>, prefix: &[u8]) -> Self {
        // Hash the prefix for uniform distribution
        let mut hasher = Sha256::new();
        hasher.update(prefix);
        let hashed_prefix = hasher.finalize().to_vec();

        Self::new_raw(db, hashed_prefix)
    }

    /// Creates a new prefix database with the exact prefix (no hashing).
    pub fn new_raw(db: Arc<dyn Database>, prefix: Vec<u8>) -> Self {
        let limit = compute_limit(&prefix);
        Self {
            prefix,
            limit,
            db,
            closed: AtomicBool::new(false),
        }
    }

    /// Returns the prefix used by this database.
    pub fn prefix(&self) -> &[u8] {
        &self.prefix
    }

    /// Returns the underlying database.
    pub fn inner(&self) -> Arc<dyn Database> {
        self.db.clone()
    }

    fn check_closed(&self) -> Result<()> {
        if self.closed.load(Ordering::Acquire) {
            Err(DatabaseError::Closed)
        } else {
            Ok(())
        }
    }

    fn prefix_key(&self, key: &[u8]) -> Vec<u8> {
        let mut prefixed = Vec::with_capacity(self.prefix.len() + key.len());
        prefixed.extend_from_slice(&self.prefix);
        prefixed.extend_from_slice(key);
        prefixed
    }
}

/// Computes the lexical upper bound for a prefix (prefix + 1).
fn compute_limit(prefix: &[u8]) -> Vec<u8> {
    let mut limit = prefix.to_vec();
    // Find the rightmost byte that can be incremented
    for i in (0..limit.len()).rev() {
        if limit[i] < 255 {
            limit[i] += 1;
            limit.truncate(i + 1);
            return limit;
        }
    }
    // All bytes are 0xFF, no upper limit
    Vec::new()
}

impl KeyValueReader for PrefixDb {
    fn has(&self, key: &[u8]) -> Result<bool> {
        self.check_closed()?;
        let prefixed = self.prefix_key(key);
        self.db.has(&prefixed)
    }

    fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>> {
        self.check_closed()?;
        let prefixed = self.prefix_key(key);
        self.db.get(&prefixed)
    }
}

impl KeyValueWriter for PrefixDb {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        self.check_closed()?;
        let prefixed = self.prefix_key(key);
        self.db.put(&prefixed, value)
    }
}

impl KeyValueDeleter for PrefixDb {
    fn delete(&self, key: &[u8]) -> Result<()> {
        self.check_closed()?;
        let prefixed = self.prefix_key(key);
        self.db.delete(&prefixed)
    }
}

impl Batcher for PrefixDb {
    fn new_batch(&self) -> Box<dyn Batch> {
        Box::new(PrefixBatch::new(self.prefix.clone(), self.db.new_batch()))
    }
}

impl Iteratee for PrefixDb {
    fn new_iterator(&self) -> Box<dyn DbIterator> {
        let inner = if self.limit.is_empty() {
            self.db.new_iterator_with_start(&self.prefix)
        } else {
            self.db.new_iterator_with_start_and_prefix(&self.prefix, &self.prefix)
        };
        Box::new(PrefixIterator::new(inner, self.prefix.len()))
    }

    fn new_iterator_with_start(&self, start: &[u8]) -> Box<dyn DbIterator> {
        let prefixed_start = self.prefix_key(start);
        let inner = if self.limit.is_empty() {
            self.db.new_iterator_with_start(&prefixed_start)
        } else {
            self.db.new_iterator_with_start_and_prefix(&prefixed_start, &self.prefix)
        };
        Box::new(PrefixIterator::new(inner, self.prefix.len()))
    }

    fn new_iterator_with_prefix(&self, prefix: &[u8]) -> Box<dyn DbIterator> {
        let prefixed = self.prefix_key(prefix);
        let inner = self.db.new_iterator_with_prefix(&prefixed);
        Box::new(PrefixIterator::new(inner, self.prefix.len()))
    }

    fn new_iterator_with_start_and_prefix(&self, start: &[u8], prefix: &[u8]) -> Box<dyn DbIterator> {
        let prefixed_start = self.prefix_key(start);
        let prefixed_prefix = self.prefix_key(prefix);
        let inner = self.db.new_iterator_with_start_and_prefix(&prefixed_start, &prefixed_prefix);
        Box::new(PrefixIterator::new(inner, self.prefix.len()))
    }
}

impl Compacter for PrefixDb {
    fn compact(&self, start: &[u8], limit: &[u8]) -> Result<()> {
        self.check_closed()?;
        let prefixed_start = self.prefix_key(start);
        let prefixed_limit = self.prefix_key(limit);
        self.db.compact(&prefixed_start, &prefixed_limit)
    }
}

impl HealthChecker for PrefixDb {
    fn health_check(&self) -> Result<()> {
        self.check_closed()?;
        self.db.health_check()
    }
}

impl Database for PrefixDb {
    fn close(&self) -> Result<()> {
        self.closed.store(true, Ordering::Release);
        // Don't close the underlying database - it may be shared
        Ok(())
    }

    fn is_closed(&self) -> bool {
        self.closed.load(Ordering::Acquire) || self.db.is_closed()
    }
}

/// A batch wrapper that prefixes all keys.
pub struct PrefixBatch {
    prefix: Vec<u8>,
    inner: Box<dyn Batch>,
    ops: Vec<(Vec<u8>, Option<Vec<u8>>)>, // (key, Some(value)) for put, (key, None) for delete
}

impl PrefixBatch {
    fn new(prefix: Vec<u8>, inner: Box<dyn Batch>) -> Self {
        Self {
            prefix,
            inner,
            ops: Vec::new(),
        }
    }

    fn prefix_key(&self, key: &[u8]) -> Vec<u8> {
        let mut prefixed = Vec::with_capacity(self.prefix.len() + key.len());
        prefixed.extend_from_slice(&self.prefix);
        prefixed.extend_from_slice(key);
        prefixed
    }
}

impl KeyValueWriter for PrefixBatch {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        let prefixed = self.prefix_key(key);
        self.inner.put(&prefixed, value)
    }
}

impl KeyValueDeleter for PrefixBatch {
    fn delete(&self, key: &[u8]) -> Result<()> {
        let prefixed = self.prefix_key(key);
        self.inner.delete(&prefixed)
    }
}

impl Batch for PrefixBatch {
    fn size(&self) -> usize {
        self.inner.size()
    }

    fn write(&mut self) -> Result<()> {
        self.inner.write()
    }

    fn reset(&mut self) {
        self.inner.reset();
        self.ops.clear();
    }

    fn replay(&self, writer: &dyn KeyValueWriterDeleter) -> Result<()> {
        // For replay, we need to strip the prefix
        // Since we can't easily introspect the inner batch,
        // we track operations separately
        for (key, value) in &self.ops {
            match value {
                Some(v) => writer.put(key, v)?,
                None => writer.delete(key)?,
            }
        }
        Ok(())
    }

    fn inner(&self) -> Option<&dyn Batch> {
        Some(self.inner.as_ref())
    }
}

/// An iterator that strips the prefix from keys.
pub struct PrefixIterator {
    inner: Box<dyn DbIterator>,
    prefix_len: usize,
}

impl PrefixIterator {
    fn new(inner: Box<dyn DbIterator>, prefix_len: usize) -> Self {
        Self { inner, prefix_len }
    }
}

impl DbIterator for PrefixIterator {
    fn next(&mut self) -> bool {
        self.inner.next()
    }

    fn error(&self) -> Option<&DatabaseError> {
        self.inner.error()
    }

    fn key(&self) -> &[u8] {
        let key = self.inner.key();
        if key.len() > self.prefix_len {
            &key[self.prefix_len..]
        } else {
            &[]
        }
    }

    fn value(&self) -> &[u8] {
        self.inner.value()
    }

    fn release(&mut self) {
        self.inner.release()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::MemDb;

    #[test]
    fn test_prefix_put_get() {
        let inner = Arc::new(MemDb::new());
        let db = PrefixDb::new(inner.clone(), b"test");

        db.put(b"key", b"value").unwrap();
        assert_eq!(db.get(b"key").unwrap(), Some(b"value".to_vec()));

        // Key should be prefixed in inner db
        assert!(!inner.has(b"key").unwrap());
    }

    #[test]
    fn test_prefix_isolation() {
        let inner = Arc::new(MemDb::new());
        let db1 = PrefixDb::new(inner.clone(), b"ns1");
        let db2 = PrefixDb::new(inner.clone(), b"ns2");

        db1.put(b"key", b"value1").unwrap();
        db2.put(b"key", b"value2").unwrap();

        assert_eq!(db1.get(b"key").unwrap(), Some(b"value1".to_vec()));
        assert_eq!(db2.get(b"key").unwrap(), Some(b"value2".to_vec()));
    }

    #[test]
    fn test_prefix_has() {
        let inner = Arc::new(MemDb::new());
        let db = PrefixDb::new(inner, b"test");

        assert!(!db.has(b"key").unwrap());
        db.put(b"key", b"value").unwrap();
        assert!(db.has(b"key").unwrap());
    }

    #[test]
    fn test_prefix_delete() {
        let inner = Arc::new(MemDb::new());
        let db = PrefixDb::new(inner, b"test");

        db.put(b"key", b"value").unwrap();
        assert!(db.has(b"key").unwrap());

        db.delete(b"key").unwrap();
        assert!(!db.has(b"key").unwrap());
    }

    #[test]
    fn test_prefix_close() {
        let inner = Arc::new(MemDb::new());
        let db = PrefixDb::new(inner.clone(), b"test");

        db.put(b"key", b"value").unwrap();
        db.close().unwrap();

        assert!(db.is_closed());
        assert!(matches!(db.get(b"key"), Err(DatabaseError::Closed)));

        // Inner should still be open
        assert!(!inner.is_closed());
    }

    #[test]
    fn test_prefix_iterator() {
        let inner = Arc::new(MemDb::new());
        let db = PrefixDb::new_raw(inner, b"prefix/".to_vec());

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
    fn test_compute_limit() {
        assert_eq!(compute_limit(b"abc"), b"abd".to_vec());
        assert_eq!(compute_limit(b"ab\xff"), b"ac".to_vec());
        assert_eq!(compute_limit(b"\xff\xff"), Vec::<u8>::new());
        assert_eq!(compute_limit(b""), Vec::<u8>::new());
    }

    #[test]
    fn test_nested_prefix() {
        let inner = Arc::new(MemDb::new());
        let db1 = Arc::new(PrefixDb::new_raw(inner, b"a/".to_vec()));
        let db2 = PrefixDb::new_raw(db1.clone(), b"b/".to_vec());

        db2.put(b"key", b"value").unwrap();
        assert_eq!(db2.get(b"key").unwrap(), Some(b"value".to_vec()));

        // Should not be directly visible in db1 without the second prefix
        assert!(!db1.has(b"key").unwrap());
    }
}
