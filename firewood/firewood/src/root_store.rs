// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use fjall::{Config, Keyspace, PartitionCreateOptions, PartitionHandle, PersistMode};
use parking_lot::Mutex;
use std::{
    path::Path,
    sync::{Arc, Weak},
};
use weak_table::WeakValueHashMap;

use derive_where::derive_where;
use firewood_storage::{Committed, FileBacked, IntoHashType, LinearAddress, NodeStore, TrieHash};

use crate::manager::CommittedRevision;

const FJALL_PARTITION_NAME: &str = "firewood";

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
    /// Creates or opens an instance of `RootStore`.
    ///
    /// Args:
    /// - `path`: the directory where `RootStore` will write to.
    /// - `storage`: the underlying store to create nodestores from.
    /// - `node_hash_algorithm`: the hash algorithm used for nodes.
    /// - `truncate`: whether to truncate existing data.
    ///
    /// # Errors
    ///
    /// Will return an error if unable to create or open an instance of `RootStore`.
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

    /// `add_root` persists a revision's address to `RootStore`.
    ///
    /// Args:
    /// - hash: the hash of the revision
    /// - address: the address of the revision
    ///
    /// # Errors
    ///
    /// Will return an error if unable to persist the revision address to the
    /// underlying datastore
    pub fn add_root(
        &self,
        hash: &TrieHash,
        address: &LinearAddress,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        self.items.insert(**hash, address.get().to_be_bytes())?;

        self.keyspace.persist(PersistMode::Buffer)?;

        Ok(())
    }

    /// `get` retrieves a committed revision by its hash.
    ///
    /// To retrieve a committed revision involves a few steps:
    /// 1. Check if the committed revision is cached.
    /// 2. If the committed revision is not cached, query the underlying
    ///    datastore for the revision's root address.
    /// 3. Construct the committed revision.
    ///
    /// Args:
    /// - hash: the hash of the revision
    ///
    /// # Errors
    ///
    ///  Will return an error if unable to query the underlying datastore or if
    ///  the stored address is invalid.
    ///
    /// # Panics
    ///
    ///  Will panic if the latest revision does not exist.
    pub fn get(
        &self,
        hash: &TrieHash,
    ) -> Result<Option<CommittedRevision>, Box<dyn std::error::Error + Send + Sync>> {
        // Obtain the lock to prevent multiple threads from caching the same result.
        let mut revision_cache = self.revision_cache.lock();

        // 1. Check if the committed revision is cached.
        if let Some(v) = revision_cache.get(hash) {
            return Ok(Some(v));
        }

        // 2. If the committed revision is not cached, query the underlying
        //    datastore for the revision's root address.
        let Some(v) = self.items.get(**hash)? else {
            return Ok(None);
        };

        let array: [u8; 8] = v.as_ref().try_into()?;
        let addr = LinearAddress::new(u64::from_be_bytes(array))
            .ok_or("invalid address: empty address")?;

        // 3. Construct the committed revision.
        let nodestore = Arc::new(NodeStore::with_root(
            hash.clone().into_hash_type(),
            addr,
            self.storage.clone(),
        ));

        // Cache for future lookups.
        revision_cache.insert(hash.clone(), nodestore.clone());

        Ok(Some(nodestore))
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use super::*;
    use firewood_storage::{CacheReadStrategy, FileBacked, NodeHashAlgorithm, NodeStore};
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

        // Create a revision to cache.
        let revision = Arc::new(NodeStore::new_empty_committed(file_backed.clone()));

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
