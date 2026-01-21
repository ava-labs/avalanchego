//! Database helper functions.
//!
//! This module provides utility functions for common database operations,
//! including type-safe serialization of common types.

use crate::{Database, Iteratee, KeyValueReader, KeyValueWriter, Result};
use avalanche_ids::Id;

/// Gets a value from the database, returning a default if not found.
pub fn with_default<D: KeyValueReader + ?Sized>(
    db: &D,
    key: &[u8],
    default: Vec<u8>,
) -> Result<Vec<u8>> {
    match db.get(key)? {
        Some(v) => Ok(v),
        None => Ok(default),
    }
}

/// Counts the number of keys in the database.
pub fn count<D: Iteratee + ?Sized>(db: &D) -> Result<usize> {
    let mut iter = db.new_iterator();
    let mut count = 0;
    while iter.next() {
        count += 1;
    }
    iter.release();
    Ok(count)
}

/// Calculates the total size of all keys and values in the database.
pub fn size<D: Iteratee + ?Sized>(db: &D) -> Result<usize> {
    let mut iter = db.new_iterator();
    let mut total = 0;
    while iter.next() {
        total += iter.key().len() + iter.value().len();
    }
    iter.release();
    Ok(total)
}

/// Deletes all keys in the database atomically via a single batch.
pub fn atomic_clear<D: Database + ?Sized>(db: &D) -> Result<()> {
    let mut batch = db.new_batch();
    let mut iter = db.new_iterator();

    while iter.next() {
        batch.delete(iter.key())?;
    }
    iter.release();

    batch.write()
}

/// Deletes all keys with the given prefix atomically via a single batch.
pub fn atomic_clear_prefix<D: Database + ?Sized>(db: &D, prefix: &[u8]) -> Result<()> {
    let mut batch = db.new_batch();
    let mut iter = db.new_iterator_with_prefix(prefix);

    while iter.next() {
        batch.delete(iter.key())?;
    }
    iter.release();

    batch.write()
}

/// Deletes all keys in the database using multiple batches if needed.
/// This is more memory-efficient for large databases.
pub fn clear<D: Database + ?Sized>(db: &D, batch_size: usize) -> Result<()> {
    let mut iter = db.new_iterator();
    let mut batch = db.new_batch();
    let mut count = 0;

    while iter.next() {
        batch.delete(iter.key())?;
        count += 1;

        if count >= batch_size {
            batch.write()?;
            batch = db.new_batch();
            count = 0;
        }
    }
    iter.release();

    if count > 0 {
        batch.write()?;
    }

    Ok(())
}

/// Deletes all keys with the given prefix using multiple batches if needed.
pub fn clear_prefix<D: Database + ?Sized>(db: &D, prefix: &[u8], batch_size: usize) -> Result<()> {
    let mut iter = db.new_iterator_with_prefix(prefix);
    let mut batch = db.new_batch();
    let mut count = 0;

    while iter.next() {
        batch.delete(iter.key())?;
        count += 1;

        if count >= batch_size {
            batch.write()?;
            batch = db.new_batch();
            count = 0;
        }
    }
    iter.release();

    if count > 0 {
        batch.write()?;
    }

    Ok(())
}

// ============================================================================
// Type-safe getters and putters
// ============================================================================

/// Puts an ID into the database.
pub fn put_id<D: KeyValueWriter + ?Sized>(db: &D, key: &[u8], id: &Id) -> Result<()> {
    db.put(key, id.as_bytes())
}

/// Gets an ID from the database.
pub fn get_id<D: KeyValueReader + ?Sized>(db: &D, key: &[u8]) -> Result<Option<Id>> {
    match db.get(key)? {
        Some(data) => {
            let id = Id::from_slice(&data).map_err(|e| {
                crate::DatabaseError::Corruption(format!("invalid ID: {}", e))
            })?;
            Ok(Some(id))
        }
        None => Ok(None),
    }
}

/// Puts a u64 into the database (big-endian).
pub fn put_u64<D: KeyValueWriter + ?Sized>(db: &D, key: &[u8], value: u64) -> Result<()> {
    db.put(key, &value.to_be_bytes())
}

/// Gets a u64 from the database (big-endian).
pub fn get_u64<D: KeyValueReader + ?Sized>(db: &D, key: &[u8]) -> Result<Option<u64>> {
    match db.get(key)? {
        Some(data) => {
            if data.len() != 8 {
                return Err(crate::DatabaseError::Corruption(format!(
                    "invalid u64 length: expected 8, got {}",
                    data.len()
                )));
            }
            let bytes: [u8; 8] = data.try_into().unwrap();
            Ok(Some(u64::from_be_bytes(bytes)))
        }
        None => Ok(None),
    }
}

/// Puts a u32 into the database (big-endian).
pub fn put_u32<D: KeyValueWriter + ?Sized>(db: &D, key: &[u8], value: u32) -> Result<()> {
    db.put(key, &value.to_be_bytes())
}

/// Gets a u32 from the database (big-endian).
pub fn get_u32<D: KeyValueReader + ?Sized>(db: &D, key: &[u8]) -> Result<Option<u32>> {
    match db.get(key)? {
        Some(data) => {
            if data.len() != 4 {
                return Err(crate::DatabaseError::Corruption(format!(
                    "invalid u32 length: expected 4, got {}",
                    data.len()
                )));
            }
            let bytes: [u8; 4] = data.try_into().unwrap();
            Ok(Some(u32::from_be_bytes(bytes)))
        }
        None => Ok(None),
    }
}

/// Puts a bool into the database.
pub fn put_bool<D: KeyValueWriter + ?Sized>(db: &D, key: &[u8], value: bool) -> Result<()> {
    db.put(key, &[if value { 1 } else { 0 }])
}

/// Gets a bool from the database.
pub fn get_bool<D: KeyValueReader + ?Sized>(db: &D, key: &[u8]) -> Result<Option<bool>> {
    match db.get(key)? {
        Some(data) => {
            if data.len() != 1 {
                return Err(crate::DatabaseError::Corruption(format!(
                    "invalid bool length: expected 1, got {}",
                    data.len()
                )));
            }
            Ok(Some(data[0] != 0))
        }
        None => Ok(None),
    }
}

/// Puts a timestamp (Unix seconds) into the database.
pub fn put_timestamp<D: KeyValueWriter + ?Sized>(
    db: &D,
    key: &[u8],
    timestamp: i64,
) -> Result<()> {
    db.put(key, &timestamp.to_be_bytes())
}

/// Gets a timestamp (Unix seconds) from the database.
pub fn get_timestamp<D: KeyValueReader + ?Sized>(db: &D, key: &[u8]) -> Result<Option<i64>> {
    match db.get(key)? {
        Some(data) => {
            if data.len() != 8 {
                return Err(crate::DatabaseError::Corruption(format!(
                    "invalid timestamp length: expected 8, got {}",
                    data.len()
                )));
            }
            let bytes: [u8; 8] = data.try_into().unwrap();
            Ok(Some(i64::from_be_bytes(bytes)))
        }
        None => Ok(None),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::MemDb;

    #[test]
    fn test_with_default() {
        let db = MemDb::new();

        let result = with_default(&db, b"missing", b"default".to_vec()).unwrap();
        assert_eq!(result, b"default".to_vec());

        db.put(b"key", b"value").unwrap();
        let result = with_default(&db, b"key", b"default".to_vec()).unwrap();
        assert_eq!(result, b"value".to_vec());
    }

    #[test]
    fn test_count() {
        let db = MemDb::new();

        assert_eq!(count(&db).unwrap(), 0);

        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();
        db.put(b"c", b"3").unwrap();

        assert_eq!(count(&db).unwrap(), 3);
    }

    #[test]
    fn test_size() {
        let db = MemDb::new();

        assert_eq!(size(&db).unwrap(), 0);

        db.put(b"key", b"value").unwrap(); // 3 + 5 = 8
        assert_eq!(size(&db).unwrap(), 8);
    }

    #[test]
    fn test_atomic_clear() {
        let db = MemDb::new();
        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();

        atomic_clear(&db).unwrap();

        assert_eq!(count(&db).unwrap(), 0);
    }

    #[test]
    fn test_atomic_clear_prefix() {
        let db = MemDb::new();
        db.put(b"prefix/a", b"1").unwrap();
        db.put(b"prefix/b", b"2").unwrap();
        db.put(b"other/c", b"3").unwrap();

        atomic_clear_prefix(&db, b"prefix/").unwrap();

        assert!(!db.has(b"prefix/a").unwrap());
        assert!(!db.has(b"prefix/b").unwrap());
        assert!(db.has(b"other/c").unwrap());
    }

    #[test]
    fn test_clear_batched() {
        let db = MemDb::new();
        for i in 0..100 {
            db.put(format!("key{}", i).as_bytes(), b"value").unwrap();
        }

        clear(&db, 10).unwrap();

        assert_eq!(count(&db).unwrap(), 0);
    }

    #[test]
    fn test_put_get_id() {
        let db = MemDb::new();
        let id = Id::from_slice(&[1u8; 32]).unwrap();

        put_id(&db, b"id", &id).unwrap();
        let retrieved = get_id(&db, b"id").unwrap();

        assert_eq!(retrieved, Some(id));
    }

    #[test]
    fn test_put_get_u64() {
        let db = MemDb::new();

        put_u64(&db, b"num", 123456789).unwrap();
        assert_eq!(get_u64(&db, b"num").unwrap(), Some(123456789));
        assert_eq!(get_u64(&db, b"missing").unwrap(), None);
    }

    #[test]
    fn test_put_get_u32() {
        let db = MemDb::new();

        put_u32(&db, b"num", 12345).unwrap();
        assert_eq!(get_u32(&db, b"num").unwrap(), Some(12345));
    }

    #[test]
    fn test_put_get_bool() {
        let db = MemDb::new();

        put_bool(&db, b"true", true).unwrap();
        put_bool(&db, b"false", false).unwrap();

        assert_eq!(get_bool(&db, b"true").unwrap(), Some(true));
        assert_eq!(get_bool(&db, b"false").unwrap(), Some(false));
        assert_eq!(get_bool(&db, b"missing").unwrap(), None);
    }

    #[test]
    fn test_put_get_timestamp() {
        let db = MemDb::new();

        put_timestamp(&db, b"time", 1700000000).unwrap();
        assert_eq!(get_timestamp(&db, b"time").unwrap(), Some(1700000000));
    }

    #[test]
    fn test_corruption_errors() {
        let db = MemDb::new();

        // Write invalid data
        db.put(b"bad_id", b"too short").unwrap();
        assert!(matches!(
            get_id(&db, b"bad_id"),
            Err(crate::DatabaseError::Corruption(_))
        ));

        db.put(b"bad_u64", b"short").unwrap();
        assert!(matches!(
            get_u64(&db, b"bad_u64"),
            Err(crate::DatabaseError::Corruption(_))
        ));
    }
}
