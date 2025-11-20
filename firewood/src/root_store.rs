// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use fjall::{Config, Keyspace, PartitionCreateOptions, PartitionHandle, PersistMode};
use std::{fmt::Debug, path::Path};

use derive_where::derive_where;
use firewood_storage::{LinearAddress, TrieHash};

const FJALL_PARTITION_NAME: &str = "firewood";

pub trait RootStore: Debug {
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
    fn add_root(
        &self,
        hash: &TrieHash,
        address: &LinearAddress,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>>;

    /// `get` returns the address of a revision.
    ///
    /// Args:
    /// - hash: the hash of the revision
    ///
    /// # Errors
    ///
    ///  Will return an error if unable to query the underlying datastore.
    fn get(
        &self,
        hash: &TrieHash,
    ) -> Result<Option<LinearAddress>, Box<dyn std::error::Error + Send + Sync>>;

    /// Returns whether revision nodes should be added to the free list.
    fn allow_space_reuse(&self) -> bool;
}

#[derive(Debug)]
pub struct NoOpStore {}

impl RootStore for NoOpStore {
    fn add_root(
        &self,
        _hash: &TrieHash,
        _address: &LinearAddress,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        Ok(())
    }

    fn get(
        &self,
        _hash: &TrieHash,
    ) -> Result<Option<LinearAddress>, Box<dyn std::error::Error + Send + Sync>> {
        Ok(None)
    }

    fn allow_space_reuse(&self) -> bool {
        true
    }
}

#[derive_where(Debug)]
#[derive_where(skip_inner)]
pub struct FjallStore {
    keyspace: Keyspace,
    items: PartitionHandle,
}

impl FjallStore {
    /// Creates or opens an instance of `FjallStore`.
    ///
    /// Args:
    /// - path: the directory where `FjallStore` will write to.
    ///
    /// # Errors
    ///
    /// Will return an error if unable to create or open an instance of `FjallStore`.
    pub fn new<P: AsRef<Path>>(
        path: P,
    ) -> Result<FjallStore, Box<dyn std::error::Error + Send + Sync>> {
        let keyspace = Config::new(path).open()?;
        let items =
            keyspace.open_partition(FJALL_PARTITION_NAME, PartitionCreateOptions::default())?;

        Ok(Self { keyspace, items })
    }
}

impl RootStore for FjallStore {
    fn add_root(
        &self,
        hash: &TrieHash,
        address: &LinearAddress,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        self.items.insert(**hash, address.get().to_be_bytes())?;

        self.keyspace.persist(PersistMode::Buffer)?;

        Ok(())
    }

    fn get(
        &self,
        hash: &TrieHash,
    ) -> Result<Option<LinearAddress>, Box<dyn std::error::Error + Send + Sync>> {
        let Some(v) = self.items.get(**hash)? else {
            return Ok(None);
        };

        let array: [u8; 8] = v.as_ref().try_into()?;

        Ok(LinearAddress::new(u64::from_be_bytes(array)))
    }

    fn allow_space_reuse(&self) -> bool {
        false
    }
}
