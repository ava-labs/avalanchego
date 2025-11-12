// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt::Debug;
#[cfg(test)]
use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use firewood_storage::{LinearAddress, TrieHash};

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
}

#[cfg(test)]
#[derive(Debug, Default)]
pub struct MockStore {
    roots: Arc<Mutex<HashMap<TrieHash, LinearAddress>>>,
    should_fail: bool,
}

#[cfg(test)]
impl MockStore {
    /// Returns an instance of `MockStore` that fails for all `add_root` and `get` calls.
    #[must_use]
    pub fn with_failures() -> Self {
        Self {
            should_fail: true,
            ..Default::default()
        }
    }
}

#[cfg(test)]
impl RootStore for MockStore {
    fn add_root(
        &self,
        hash: &TrieHash,
        address: &LinearAddress,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        if self.should_fail {
            return Err("Adding roots should fail".into());
        }

        self.roots
            .lock()
            .expect("poisoned lock")
            .insert(hash.clone(), *address);
        Ok(())
    }

    fn get(
        &self,
        hash: &TrieHash,
    ) -> Result<Option<LinearAddress>, Box<dyn std::error::Error + Send + Sync>> {
        if self.should_fail {
            return Err("Getting roots should fail".into());
        }

        Ok(self.roots.lock().expect("poisoned lock").get(hash).copied())
    }
}
