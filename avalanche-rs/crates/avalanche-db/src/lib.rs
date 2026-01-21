//! Avalanche database abstraction layer.
//!
//! This crate provides the database traits and implementations used by Avalanche,
//! following the same design patterns as the Go implementation for compatibility.
//!
//! # Architecture
//!
//! The database system uses a composable, layered architecture:
//!
//! - **MemDB**: In-memory key-value store for testing and ephemeral data
//! - **PrefixDB**: Namespace wrapper that prefixes all keys
//! - **VersionDB**: Copy-on-write wrapper for transactional semantics
//! - **LinkedDB**: Doubly-linked list backed by a database
//!
//! # Example
//!
//! ```
//! use avalanche_db::{MemDb, KeyValueReader, KeyValueWriter};
//!
//! let db = MemDb::new();
//! db.put(b"key", b"value").unwrap();
//! assert_eq!(db.get(b"key").unwrap(), Some(b"value".to_vec()));
//! ```

mod error;
mod helpers;
mod linkeddb;
mod memdb;
mod prefixdb;
#[cfg(feature = "rocksdb")]
mod rocksdb;
mod versiondb;

pub use error::{DatabaseError, Result};
pub use helpers::*;
pub use linkeddb::LinkedDb;
pub use memdb::MemDb;
pub use prefixdb::PrefixDb;
#[cfg(feature = "rocksdb")]
pub use rocksdb::{RocksDb, RocksDbConfig};
pub use versiondb::VersionDb;

use std::sync::Arc;

/// A key-value reader.
pub trait KeyValueReader: Send + Sync {
    /// Returns whether the key exists in the database.
    fn has(&self, key: &[u8]) -> Result<bool>;

    /// Gets the value for the given key.
    /// Returns `Ok(None)` if the key does not exist.
    fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>>;
}

/// A key-value writer.
pub trait KeyValueWriter: Send + Sync {
    /// Sets the value for the given key.
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()>;
}

/// A key-value deleter.
pub trait KeyValueDeleter: Send + Sync {
    /// Deletes the value for the given key.
    fn delete(&self, key: &[u8]) -> Result<()>;
}

/// Combined read/write/delete interface.
pub trait KeyValueReaderWriterDeleter: KeyValueReader + KeyValueWriter + KeyValueDeleter {}

impl<T> KeyValueReaderWriterDeleter for T where T: KeyValueReader + KeyValueWriter + KeyValueDeleter {}

/// Iterator creation interface.
pub trait Iteratee: Send + Sync {
    /// Creates an iterator over all key-value pairs.
    fn new_iterator(&self) -> Box<dyn DbIterator>;

    /// Creates an iterator starting at the given key.
    fn new_iterator_with_start(&self, start: &[u8]) -> Box<dyn DbIterator>;

    /// Creates an iterator over keys with the given prefix.
    fn new_iterator_with_prefix(&self, prefix: &[u8]) -> Box<dyn DbIterator>;

    /// Creates an iterator starting at the given key with the given prefix.
    fn new_iterator_with_start_and_prefix(&self, start: &[u8], prefix: &[u8]) -> Box<dyn DbIterator>;
}

/// Batch creation interface.
pub trait Batcher: Send + Sync {
    /// Creates a new batch for atomic writes.
    fn new_batch(&self) -> Box<dyn Batch>;
}

/// Compaction interface.
pub trait Compacter: Send + Sync {
    /// Compacts the underlying storage in the given key range.
    fn compact(&self, start: &[u8], limit: &[u8]) -> Result<()>;
}

/// Health check interface.
pub trait HealthChecker: Send + Sync {
    /// Returns the health status of the database.
    fn health_check(&self) -> Result<()>;
}

/// The main database interface combining all capabilities.
pub trait Database:
    KeyValueReader + KeyValueWriter + KeyValueDeleter + Batcher + Iteratee + Compacter + HealthChecker
{
    /// Closes the database.
    fn close(&self) -> Result<()>;

    /// Returns whether the database is closed.
    fn is_closed(&self) -> bool;
}

/// A batch of write operations to be applied atomically.
pub trait Batch: KeyValueWriter + KeyValueDeleter + Send + Sync {
    /// Returns the size of the batch in bytes.
    fn size(&self) -> usize;

    /// Writes the batch to the database.
    fn write(&mut self) -> Result<()>;

    /// Resets the batch for reuse.
    fn reset(&mut self);

    /// Replays the batch operations to another writer.
    fn replay(&self, writer: &dyn KeyValueWriterDeleter) -> Result<()>;

    /// Returns the inner batch if this is a wrapped batch.
    fn inner(&self) -> Option<&dyn Batch> {
        None
    }
}

/// Combined writer/deleter interface for batch replay.
pub trait KeyValueWriterDeleter: KeyValueWriter + KeyValueDeleter {}

impl<T> KeyValueWriterDeleter for T where T: KeyValueWriter + KeyValueDeleter {}

/// An iterator over key-value pairs.
pub trait DbIterator: Send {
    /// Moves to the next key-value pair.
    /// Returns `false` when there are no more pairs.
    fn next(&mut self) -> bool;

    /// Returns any accumulated error.
    fn error(&self) -> Option<&DatabaseError>;

    /// Returns the current key.
    /// Only valid after `next()` returns `true`.
    fn key(&self) -> &[u8];

    /// Returns the current value.
    /// Only valid after `next()` returns `true`.
    fn value(&self) -> &[u8];

    /// Releases resources held by the iterator.
    fn release(&mut self);
}

/// A database that can be committed or aborted (transactional).
pub trait Commitable: Database {
    /// Commits all buffered writes to the underlying database.
    fn commit(&self) -> Result<()>;

    /// Aborts all buffered writes.
    fn abort(&self);

    /// Returns a batch containing all uncommitted writes.
    fn commit_batch(&self) -> Result<Box<dyn Batch>>;

    /// Sets the underlying database.
    fn set_database(&self, db: Arc<dyn Database>) -> Result<()>;

    /// Returns the underlying database.
    fn get_database(&self) -> Arc<dyn Database>;
}

/// A linked database with insertion ordering.
pub trait LinkedDatabase: KeyValueReader + KeyValueWriter + KeyValueDeleter + Send + Sync {
    /// Returns whether the linked database is empty.
    fn is_empty(&self) -> Result<bool>;

    /// Returns the key of the head element.
    fn head_key(&self) -> Result<Option<Vec<u8>>>;

    /// Returns the head key and value.
    fn head(&self) -> Result<Option<(Vec<u8>, Vec<u8>)>>;

    /// Creates an iterator over the linked list (insertion order).
    fn new_iterator(&self) -> Box<dyn DbIterator>;

    /// Creates an iterator starting at the given key.
    fn new_iterator_with_start(&self, start: &[u8]) -> Box<dyn DbIterator>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_memdb_basic_operations() {
        let db = MemDb::new();

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
    fn test_memdb_batch() {
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
    fn test_memdb_iterator() {
        let db = MemDb::new();
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
}
