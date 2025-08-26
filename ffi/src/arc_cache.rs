// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! A simple single-item cache for database views.
//!
//! This module provides [`ArcCache`], a thread-safe cache that holds at most one
//! key-value pair. It's specifically designed to cache database views in the FFI
//! layer to improve performance by avoiding repeated view creation for the same
//! root hash during database operations.

use std::sync::{Arc, Mutex, MutexGuard, PoisonError};

/// A thread-safe single-item cache that stores key-value pairs as `Arc<V>`.
///
/// This cache is optimized for scenarios where you frequently access the same
/// item and want to avoid expensive recomputation. It holds at most one cached
/// entry and replaces it when a different key is requested.
///
/// The cache is thread-safe and uses a mutex to protect concurrent access.
/// Values are stored as `Arc<V>` to allow cheap cloning and sharing across
/// threads.
#[derive(Debug)]
pub struct ArcCache<K, V: ?Sized> {
    cache: Mutex<Option<(K, Arc<V>)>>,
}

impl<K: PartialEq, V: ?Sized> ArcCache<K, V> {
    pub const fn new() -> Self {
        ArcCache {
            cache: Mutex::new(None),
        }
    }

    /// Gets the cached value for the given key, or creates and caches a new value.
    ///
    /// If the cache contains an entry with a key equal to the provided key,
    /// returns a clone of the cached `Arc<V>`. Otherwise, calls the factory
    /// function to create a new value, caches it, and returns it.
    ///
    /// # Cache Behavior
    ///
    /// - Cache hit: Returns the cached value immediately
    /// - Cache miss: Clears any existing cache entry, calls factory, caches the result
    /// - Factory error: Cache is cleared and the error is propagated
    ///
    /// # Arguments
    ///
    /// * `key` - The key to look up or cache
    /// * `factory` - A function that creates the value if not cached. It receives
    ///   a reference to the key as an argument.
    ///
    /// # Errors
    ///
    /// Returns any error produced by the factory function.
    pub fn get_or_try_insert_with<E>(
        &self,
        key: K,
        factory: impl FnOnce(&K) -> Result<Arc<V>, E>,
    ) -> Result<Arc<V>, E> {
        let mut cache = self.lock();
        if let Some((cached_key, value)) = cache.as_ref()
            && *cached_key == key
        {
            return Ok(Arc::clone(value));
        }

        // clear the cache before running the factory in case it fails
        *cache = None;

        let value = factory(&key)?;
        *cache = Some((key, Arc::clone(&value)));

        Ok(value)
    }

    /// Clears the cache, removing any stored key-value pair.
    pub fn clear(&self) {
        self.lock().take();
    }

    fn lock(&self) -> MutexGuard<'_, Option<(K, Arc<V>)>> {
        self.cache.lock().unwrap_or_else(PoisonError::into_inner)
    }
}

impl<K: PartialEq, T: ?Sized> Default for ArcCache<K, T> {
    fn default() -> Self {
        Self::new()
    }
}
