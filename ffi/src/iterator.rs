// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use derive_where::derive_where;
use firewood::merkle;
use firewood::v2::api::{self, BoxKeyValueIter};
use std::iter::FusedIterator;

type KeyValueItem = (merkle::Key, merkle::Value);

/// An opaque wrapper around a [`BoxKeyValueIter`].
#[derive(Default)]
#[derive_where(Debug)]
#[derive_where(skip_inner)]
pub struct IteratorHandle<'view>(Option<BoxKeyValueIter<'view>>);

impl<'view> From<BoxKeyValueIter<'view>> for IteratorHandle<'view> {
    fn from(value: BoxKeyValueIter<'view>) -> Self {
        IteratorHandle(Some(value))
    }
}

impl Iterator for IteratorHandle<'_> {
    type Item = Result<KeyValueItem, api::Error>;

    fn next(&mut self) -> Option<Self::Item> {
        let out = self.0.as_mut()?.next();
        if out.is_none() {
            // iterator exhausted; drop it so the NodeStore can be released
            self.0 = None;
        }
        out
    }
}

impl FusedIterator for IteratorHandle<'_> {}

#[expect(clippy::missing_errors_doc)]
impl IteratorHandle<'_> {
    pub fn iter_next_n(&mut self, n: usize) -> Result<Vec<KeyValueItem>, api::Error> {
        self.by_ref().take(n).collect()
    }
}

#[derive(Debug, Default)]
pub struct CreateIteratorResult<'db>(pub IteratorHandle<'db>);
