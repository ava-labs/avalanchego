// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use fjall::{Config, Keyspace, PartitionCreateOptions, PartitionHandle, PersistMode};
use std::path::Path;

use derive_where::derive_where;
use firewood_storage::{LinearAddress, TrieHash};

const FJALL_PARTITION_NAME: &str = "firewood";

#[derive_where(Debug)]
#[derive_where(skip_inner)]
pub struct RootStore {
    keyspace: Keyspace,
    items: PartitionHandle,
}

impl RootStore {
    /// Creates or opens an instance of `RootStore`.
    ///
    /// Args:
    /// - path: the directory where `RootStore` will write to.
    ///
    /// # Errors
    ///
    /// Will return an error if unable to create or open an instance of `RootStore`.
    pub fn new<P: AsRef<Path>>(
        path: P,
    ) -> Result<RootStore, Box<dyn std::error::Error + Send + Sync>> {
        let keyspace = Config::new(path).open()?;
        let items =
            keyspace.open_partition(FJALL_PARTITION_NAME, PartitionCreateOptions::default())?;

        Ok(Self { keyspace, items })
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

    /// `get` returns the address of a revision.
    ///
    /// Args:
    /// - hash: the hash of the revision
    ///
    /// # Errors
    ///
    ///  Will return an error if unable to query the underlying datastore.
    pub fn get(
        &self,
        hash: &TrieHash,
    ) -> Result<Option<LinearAddress>, Box<dyn std::error::Error + Send + Sync>> {
        let Some(v) = self.items.get(**hash)? else {
            return Ok(None);
        };

        let array: [u8; 8] = v.as_ref().try_into()?;

        Ok(LinearAddress::new(u64::from_be_bytes(array)))
    }
}
