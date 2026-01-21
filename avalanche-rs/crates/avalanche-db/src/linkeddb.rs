//! Linked database implementation.
//!
//! This module provides a doubly-linked list backed by a database.
//! Entries are ordered by insertion time (most recent first).
//! It uses LRU caching for frequently accessed nodes.

use std::num::NonZeroUsize;
use std::sync::Arc;

use lru::LruCache;
use parking_lot::RwLock;

use crate::{
    Database, DatabaseError, DbIterator, KeyValueDeleter, KeyValueReader, KeyValueWriter,
    LinkedDatabase, Result,
};

/// Special key for storing the head pointer.
const HEAD_KEY: &[u8] = b"__linked_head__";

/// A node in the linked list.
#[derive(Debug, Clone)]
struct Node {
    value: Vec<u8>,
    has_next: bool,
    next: Vec<u8>,
    has_prev: bool,
    prev: Vec<u8>,
}

impl Node {
    fn new(value: Vec<u8>) -> Self {
        Self {
            value,
            has_next: false,
            next: Vec::new(),
            has_prev: false,
            prev: Vec::new(),
        }
    }

    fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::new();

        // Value
        let value_len = self.value.len() as u32;
        buf.extend_from_slice(&value_len.to_be_bytes());
        buf.extend_from_slice(&self.value);

        // Next
        buf.push(u8::from(self.has_next));
        let next_len = self.next.len() as u32;
        buf.extend_from_slice(&next_len.to_be_bytes());
        buf.extend_from_slice(&self.next);

        // Prev
        buf.push(u8::from(self.has_prev));
        let prev_len = self.prev.len() as u32;
        buf.extend_from_slice(&prev_len.to_be_bytes());
        buf.extend_from_slice(&self.prev);

        buf
    }

    fn decode(data: &[u8]) -> Result<Self> {
        let mut offset = 0;

        // Value
        if data.len() < offset + 4 {
            return Err(DatabaseError::Corruption("node too short".into()));
        }
        let value_len = u32::from_be_bytes(data[offset..offset + 4].try_into().unwrap()) as usize;
        offset += 4;

        if data.len() < offset + value_len {
            return Err(DatabaseError::Corruption("node value truncated".into()));
        }
        let value = data[offset..offset + value_len].to_vec();
        offset += value_len;

        // Next
        if data.len() < offset + 1 {
            return Err(DatabaseError::Corruption("node missing has_next".into()));
        }
        let has_next = data[offset] == 1;
        offset += 1;

        if data.len() < offset + 4 {
            return Err(DatabaseError::Corruption("node missing next length".into()));
        }
        let next_len = u32::from_be_bytes(data[offset..offset + 4].try_into().unwrap()) as usize;
        offset += 4;

        if data.len() < offset + next_len {
            return Err(DatabaseError::Corruption("node next truncated".into()));
        }
        let next = data[offset..offset + next_len].to_vec();
        offset += next_len;

        // Prev
        if data.len() < offset + 1 {
            return Err(DatabaseError::Corruption("node missing has_prev".into()));
        }
        let has_prev = data[offset] == 1;
        offset += 1;

        if data.len() < offset + 4 {
            return Err(DatabaseError::Corruption("node missing prev length".into()));
        }
        let prev_len = u32::from_be_bytes(data[offset..offset + 4].try_into().unwrap()) as usize;
        offset += 4;

        if data.len() < offset + prev_len {
            return Err(DatabaseError::Corruption("node prev truncated".into()));
        }
        let prev = data[offset..offset + prev_len].to_vec();

        Ok(Self {
            value,
            has_next,
            next,
            has_prev,
            prev,
        })
    }
}

/// A doubly-linked list backed by a database.
pub struct LinkedDb {
    db: Arc<dyn Database>,
    cache: RwLock<LruCache<Vec<u8>, Node>>,
    head_key_cache: RwLock<Option<Option<Vec<u8>>>>,
}

impl LinkedDb {
    /// Creates a new linked database with the given cache size.
    pub fn new(db: Arc<dyn Database>, cache_size: usize) -> Self {
        let cache_size = NonZeroUsize::new(cache_size).unwrap_or(NonZeroUsize::new(1024).unwrap());
        Self {
            db,
            cache: RwLock::new(LruCache::new(cache_size)),
            head_key_cache: RwLock::new(None),
        }
    }

    /// Creates a new linked database with default cache size.
    pub fn with_default_cache(db: Arc<dyn Database>) -> Self {
        Self::new(db, 1024)
    }

    fn get_node(&self, key: &[u8]) -> Result<Option<Node>> {
        {
            let mut cache = self.cache.write();
            if let Some(node) = cache.get(&key.to_vec()) {
                return Ok(Some(node.clone()));
            }
        }

        if let Some(data) = self.db.get(key)? {
            let node = Node::decode(&data)?;
            let mut cache = self.cache.write();
            cache.put(key.to_vec(), node.clone());
            Ok(Some(node))
        } else {
            Ok(None)
        }
    }

    fn put_node(&self, key: &[u8], node: &Node) -> Result<()> {
        let data = node.encode();
        self.db.put(key, &data)?;
        let mut cache = self.cache.write();
        cache.put(key.to_vec(), node.clone());
        Ok(())
    }

    fn delete_node(&self, key: &[u8]) -> Result<()> {
        self.db.delete(key)?;
        let mut cache = self.cache.write();
        cache.pop(&key.to_vec());
        Ok(())
    }

    fn get_head_key_internal(&self) -> Result<Option<Vec<u8>>> {
        {
            let cache = self.head_key_cache.read();
            if let Some(ref cached) = *cache {
                return Ok(cached.clone());
            }
        }

        let head = self.db.get(HEAD_KEY)?;
        let mut cache = self.head_key_cache.write();
        *cache = Some(head.clone());
        Ok(head)
    }

    fn set_head_key(&self, key: Option<&[u8]>) -> Result<()> {
        match key {
            Some(k) => self.db.put(HEAD_KEY, k)?,
            None => self.db.delete(HEAD_KEY)?,
        }
        let mut cache = self.head_key_cache.write();
        *cache = Some(key.map(|k| k.to_vec()));
        Ok(())
    }
}

impl KeyValueReader for LinkedDb {
    fn has(&self, key: &[u8]) -> Result<bool> {
        {
            let cache = self.cache.read();
            if cache.peek(&key.to_vec()).is_some() {
                return Ok(true);
            }
        }
        self.db.has(key)
    }

    fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>> {
        match self.get_node(key)? {
            Some(node) => Ok(Some(node.value)),
            None => Ok(None),
        }
    }
}

impl KeyValueWriter for LinkedDb {
    fn put(&self, key: &[u8], value: &[u8]) -> Result<()> {
        let existing = self.get_node(key)?;

        if let Some(mut node) = existing {
            node.value = value.to_vec();
            self.put_node(key, &node)?;
        } else {
            let mut new_node = Node::new(value.to_vec());

            if let Some(head_key) = self.get_head_key_internal()? {
                new_node.has_next = true;
                new_node.next = head_key.clone();

                if let Some(mut old_head) = self.get_node(&head_key)? {
                    old_head.has_prev = true;
                    old_head.prev = key.to_vec();
                    self.put_node(&head_key, &old_head)?;
                }
            }

            self.put_node(key, &new_node)?;
            self.set_head_key(Some(key))?;
        }

        Ok(())
    }
}

impl KeyValueDeleter for LinkedDb {
    fn delete(&self, key: &[u8]) -> Result<()> {
        let node = match self.get_node(key)? {
            Some(n) => n,
            None => return Ok(()),
        };

        if node.has_prev {
            if let Some(mut prev_node) = self.get_node(&node.prev)? {
                prev_node.has_next = node.has_next;
                prev_node.next = node.next.clone();
                self.put_node(&node.prev, &prev_node)?;
            }
        }

        if node.has_next {
            if let Some(mut next_node) = self.get_node(&node.next)? {
                next_node.has_prev = node.has_prev;
                next_node.prev = node.prev.clone();
                self.put_node(&node.next, &next_node)?;
            }
        }

        let head_key = self.get_head_key_internal()?;
        if head_key.as_deref() == Some(key) {
            if node.has_next {
                self.set_head_key(Some(&node.next))?;
            } else {
                self.set_head_key(None)?;
            }
        }

        self.delete_node(key)
    }
}

impl LinkedDatabase for LinkedDb {
    fn is_empty(&self) -> Result<bool> {
        Ok(self.get_head_key_internal()?.is_none())
    }

    fn head_key(&self) -> Result<Option<Vec<u8>>> {
        self.get_head_key_internal()
    }

    fn head(&self) -> Result<Option<(Vec<u8>, Vec<u8>)>> {
        let head_key = match self.get_head_key_internal()? {
            Some(k) => k,
            None => return Ok(None),
        };

        let node = match self.get_node(&head_key)? {
            Some(n) => n,
            None => return Err(DatabaseError::Corruption("head missing".into())),
        };

        Ok(Some((head_key, node.value)))
    }

    fn new_iterator(&self) -> Box<dyn DbIterator> {
        let head = self.get_head_key_internal().ok().flatten();
        Box::new(LinkedIterator::new(self.db.clone(), head))
    }

    fn new_iterator_with_start(&self, start: &[u8]) -> Box<dyn DbIterator> {
        Box::new(LinkedIterator::new(self.db.clone(), Some(start.to_vec())))
    }
}

/// An iterator over linked database entries.
pub struct LinkedIterator {
    db: Arc<dyn Database>,
    current_key: Option<Vec<u8>>,
    current_value: Vec<u8>,
    next_key: Option<Vec<u8>>,
    error: Option<DatabaseError>,
}

impl LinkedIterator {
    fn new(db: Arc<dyn Database>, start: Option<Vec<u8>>) -> Self {
        Self {
            db,
            current_key: None,
            current_value: Vec::new(),
            next_key: start,
            error: None,
        }
    }

    fn load_node(&mut self, key: &[u8]) -> Option<Node> {
        match self.db.get(key) {
            Ok(Some(data)) => match Node::decode(&data) {
                Ok(node) => Some(node),
                Err(e) => {
                    self.error = Some(e);
                    None
                }
            },
            Ok(None) => None,
            Err(e) => {
                self.error = Some(e);
                None
            }
        }
    }
}

impl DbIterator for LinkedIterator {
    fn next(&mut self) -> bool {
        let key = match self.next_key.take() {
            Some(k) => k,
            None => return false,
        };

        if let Some(node) = self.load_node(&key) {
            self.current_key = Some(key);
            self.current_value = node.value;
            self.next_key = if node.has_next { Some(node.next) } else { None };
            true
        } else {
            false
        }
    }

    fn error(&self) -> Option<&DatabaseError> {
        self.error.as_ref()
    }

    fn key(&self) -> &[u8] {
        self.current_key.as_deref().unwrap_or(&[])
    }

    fn value(&self) -> &[u8] {
        &self.current_value
    }

    fn release(&mut self) {
        self.current_key = None;
        self.current_value.clear();
        self.next_key = None;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::MemDb;

    #[test]
    fn test_linked_empty() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        assert!(db.is_empty().unwrap());
        assert!(db.head_key().unwrap().is_none());
        assert!(db.head().unwrap().is_none());
    }

    #[test]
    fn test_linked_put_get() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        db.put(b"key1", b"value1").unwrap();
        db.put(b"key2", b"value2").unwrap();

        assert_eq!(db.get(b"key1").unwrap(), Some(b"value1".to_vec()));
        assert_eq!(db.get(b"key2").unwrap(), Some(b"value2".to_vec()));
        assert_eq!(db.get(b"key3").unwrap(), None);
    }

    #[test]
    fn test_linked_head() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        db.put(b"first", b"1").unwrap();
        assert_eq!(db.head_key().unwrap(), Some(b"first".to_vec()));

        db.put(b"second", b"2").unwrap();
        assert_eq!(db.head_key().unwrap(), Some(b"second".to_vec()));

        let (key, value) = db.head().unwrap().unwrap();
        assert_eq!(key, b"second".to_vec());
        assert_eq!(value, b"2".to_vec());
    }

    #[test]
    fn test_linked_delete_middle() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();
        db.put(b"c", b"3").unwrap();

        db.delete(b"b").unwrap();

        let mut iter = db.new_iterator();
        let mut results = Vec::new();
        while iter.next() {
            results.push(iter.key().to_vec());
        }

        assert_eq!(results, vec![b"c".to_vec(), b"a".to_vec()]);
    }

    #[test]
    fn test_linked_delete_head() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();

        db.delete(b"b").unwrap();

        assert_eq!(db.head_key().unwrap(), Some(b"a".to_vec()));
        assert!(!db.has(b"b").unwrap());
    }

    #[test]
    fn test_linked_delete_all() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();

        db.delete(b"b").unwrap();
        db.delete(b"a").unwrap();

        assert!(db.is_empty().unwrap());
    }

    #[test]
    fn test_linked_update_value() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        db.put(b"key", b"old").unwrap();
        db.put(b"key", b"new").unwrap();

        assert_eq!(db.get(b"key").unwrap(), Some(b"new".to_vec()));
    }

    #[test]
    fn test_linked_iterator() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

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
                (b"c".to_vec(), b"3".to_vec()),
                (b"b".to_vec(), b"2".to_vec()),
                (b"a".to_vec(), b"1".to_vec()),
            ]
        );
    }

    #[test]
    fn test_linked_iterator_with_start() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        db.put(b"a", b"1").unwrap();
        db.put(b"b", b"2").unwrap();
        db.put(b"c", b"3").unwrap();

        let mut iter = db.new_iterator_with_start(b"b");
        let mut results = Vec::new();
        while iter.next() {
            results.push(iter.key().to_vec());
        }
        iter.release();

        assert_eq!(results, vec![b"b".to_vec(), b"a".to_vec()]);
    }

    #[test]
    fn test_linked_empty_iterator() {
        let inner = Arc::new(MemDb::new());
        let db = LinkedDb::with_default_cache(inner);

        let mut iter = db.new_iterator();
        assert!(!iter.next());
    }

    #[test]
    fn test_node_encode_decode() {
        let node = Node {
            value: b"test value".to_vec(),
            has_next: true,
            next: b"next_key".to_vec(),
            has_prev: true,
            prev: b"prev_key".to_vec(),
        };

        let encoded = node.encode();
        let decoded = Node::decode(&encoded).unwrap();

        assert_eq!(decoded.value, node.value);
        assert_eq!(decoded.has_next, node.has_next);
        assert_eq!(decoded.next, node.next);
        assert_eq!(decoded.has_prev, node.has_prev);
        assert_eq!(decoded.prev, node.prev);
    }
}
