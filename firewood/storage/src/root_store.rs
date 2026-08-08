// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! The root store is used to store the address of roots by hash
//! so they can be recreated later.
//!
//! It is used only when enabled at database open time.

use firewood_metrics::firewood_counter;
use fjall::{Config, Keyspace, PartitionCreateOptions, PartitionHandle, PersistMode};
use parking_lot::Mutex;
use std::{
    path::Path,
    sync::{Arc, Weak},
};
use weak_table::WeakValueHashMap;

use derive_where::derive_where;

use crate::linear::filebacked::FileBacked;
use crate::nodestore::{Committed, DeletedNodeTracking, LinearAddress, NodeStore};
use crate::{IntoHashType, TrieHash};

/// Type alias for a committed revision stored in the root store.
pub type CommittedRevision = Arc<NodeStore<Committed, FileBacked>>;

/// fjall uses partitions. We store all firewood-specific data in a
/// partition named 'firewood'
const FJALL_PARTITION_NAME: &str = "firewood";

/// This structure holds everything related to an open root store
#[derive_where(Debug)]
#[derive_where(skip_inner)]
pub struct RootStore {
    keyspace: Keyspace,
    items: PartitionHandle,
    storage: Arc<FileBacked>,
    /// Cache of reconstructed revisions by hash.
    revision_cache: Mutex<WeakValueHashMap<TrieHash, Weak<NodeStore<Committed, FileBacked>>>>,
}

impl RootStore {
    /// Creates or opens an instance of `RootStore`
    ///
    /// # Arguments
    ///
    /// - `path` - the directory where `RootStore` will write to
    /// - `storage` - the underlying store to create nodestores from
    /// - `truncate` - whether to truncate existing data
    ///
    /// # Errors
    ///
    /// Returns the raw underlying database error if any
    pub fn new<P: AsRef<Path>>(
        path: P,
        storage: Arc<FileBacked>,
        truncate: bool,
    ) -> Result<RootStore, Box<dyn std::error::Error + Send + Sync>> {
        let keyspace = Config::new(path).open()?;

        if truncate {
            let items =
                keyspace.open_partition(FJALL_PARTITION_NAME, PartitionCreateOptions::default())?;
            keyspace.delete_partition(items)?;
        }

        let items =
            keyspace.open_partition(FJALL_PARTITION_NAME, PartitionCreateOptions::default())?;
        let revision_cache = Mutex::new(WeakValueHashMap::new());

        Ok(Self {
            keyspace,
            items,
            storage,
            revision_cache,
        })
    }

    /// `add_root` persists a revision's root address to the rootstore
    ///
    /// # Arguments
    ///
    /// - `hash` - the [`TrieHash`] of the revision
    /// - `address` - the [`LinearAddress`] of the root node for this revision
    ///
    /// # Errors
    ///
    /// Returns the raw underlying database error if any
    pub fn add_root(
        &self,
        hash: &TrieHash,
        address: &LinearAddress,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        // BLOCKING: `insert` is a synchronous write into fjall's in-memory buffer (typically
        // fast), but `persist` below triggers an fsync-equivalent flush to protect against
        // application crashes. This can block for milliseconds on slow or busy storage.
        // `add_root` is called on every commit from the background persist loop, so slow flushes
        // directly extend the time the `NodeStoreHeader` mutex is held by `persist_to_disk`.
        self.items.insert(**hash, address.get().to_be_bytes())?;

        // Flush the keyspace to protect against application crashes, but not
        // OS crashes. Refer to the fjall docs for more details.
        self.keyspace.persist(PersistMode::Buffer)?;

        Ok(())
    }

    /// `get` retrieves a committed revision [`NodeStore`] by its hash
    ///
    /// Updates the `rootstore_get` metric with a result label value:
    ///
    /// - `cached` if the nodestore was found in the cache
    /// - `fetched` if the nodestore was fetched and constructed
    /// - `notfound` if the hash could not be found
    ///
    /// # Arguments
    ///
    /// - `hash` - Identifies the revision to retrieve
    ///
    /// # Return Value
    ///
    /// - `None` - if the hash is not available from the root store
    /// - `Some(NodeStore<CommittedRevision>)` - on success
    ///
    /// # Errors
    ///
    /// Returns the raw underlying database error if any
    pub fn get(
        &self,
        hash: &TrieHash,
    ) -> Result<Option<CommittedRevision>, Box<dyn std::error::Error + Send + Sync>> {
        // 1. Obtain the lock to prevent multiple threads from caching the same result
        // BLOCKING: mutex lock held for the entire `get` operation, including the fjall read
        // (step 3) and NodeStore construction (step 4) on a cache miss. A cache miss against a
        // cold fjall partition may involve disk I/O, holding the lock for milliseconds and
        // serializing all concurrent historical revision lookups.
        let mut revision_cache = self.revision_cache.lock();

        // 2. Check if the committed revision is cached.
        if let Some(v) = revision_cache.get(hash) {
            // found in the cache
            firewood_counter!(ROOTSTORE_GET, "result" => "cached").increment(1);
            return Ok(Some(v));
        }

        // 3. Not cached, query the datastore
        // BLOCKING: fjall read — may involve disk I/O on a cold page cache. The revision_cache
        // mutex above is still held, so this blocks all other callers for the I/O duration.
        let Some(v) = self.items.get(**hash)? else {
            // not in the datastore
            firewood_counter!(ROOTSTORE_GET, "result" => "notfound").increment(1);
            return Ok(None);
        };

        let array: [u8; 8] = v.as_ref().try_into()?;
        let addr = LinearAddress::new(u64::from_be_bytes(array))
            .ok_or("invalid address: empty address")?;

        // 4. Construct the committed revision
        let nodestore = Arc::new(NodeStore::with_root(
            hash.clone().into_hash_type(),
            addr,
            self.storage.clone(),
            // `RootStore` only exists when the database was opened in
            // archival mode, where deleted nodes are never tracked.
            DeletedNodeTracking::Disabled,
        )?);

        // 5. Cache for future lookups.
        revision_cache.insert(hash.clone(), nodestore.clone());

        firewood_counter!(ROOTSTORE_GET, "result" => "fetched").increment(1);

        Ok(Some(nodestore))
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::CacheReadStrategy;
    use crate::linear::filebacked::FileBacked;
    use crate::nodestore::{NodeHashAlgorithm, NodeStore};
    use std::num::NonZero;
    use std::sync::Arc;

    #[test]
    fn test_cache_hit() {
        let tmpdir = tempfile::tempdir().unwrap();

        let db_path = tmpdir.as_ref().join("testdb");
        let file_backed = Arc::new(
            FileBacked::new(
                db_path,
                NonZero::new(1024).unwrap(),
                NonZero::new(1024).unwrap(),
                false,
                true,
                CacheReadStrategy::WritesOnly,
                NodeHashAlgorithm::compile_option(),
            )
            .unwrap(),
        );

        let root_store_dir = tmpdir.as_ref().join("root_store");
        let root_store = RootStore::new(root_store_dir, file_backed.clone(), false).unwrap();

        // Create a revision to cache. `RootStore` implies archival mode,
        // where deleted nodes are never tracked.
        let revision = Arc::new(NodeStore::new_empty_committed(
            file_backed.clone(),
            DeletedNodeTracking::Disabled,
        ));

        let hash = TrieHash::from_bytes([1; 32]);
        root_store
            .revision_cache
            .lock()
            .insert(hash.clone(), revision.clone());

        // Since the underlying datastore is empty, this should get the revision
        // from the cache.
        let retrieved_revision = root_store.get(&hash).unwrap().unwrap();

        assert!(Arc::ptr_eq(&revision, &retrieved_revision));
    }

    #[test]
    fn test_nonexistent_revision() {
        let tmpdir = tempfile::tempdir().unwrap();

        let db_path = tmpdir.as_ref().join("testdb");
        let file_backed = Arc::new(
            FileBacked::new(
                db_path,
                NonZero::new(1024).unwrap(),
                NonZero::new(1024).unwrap(),
                false,
                true,
                CacheReadStrategy::WritesOnly,
                NodeHashAlgorithm::compile_option(),
            )
            .unwrap(),
        );

        let root_store_dir = tmpdir.as_ref().join("root_store");
        let root_store = RootStore::new(root_store_dir, file_backed.clone(), false).unwrap();

        // Try to get a hash that doesn't exist in the cache nor in the underlying datastore.
        let nonexistent_hash = TrieHash::from_bytes([1; 32]);
        assert!(root_store.get(&nonexistent_hash).unwrap().is_none());
    }
}
