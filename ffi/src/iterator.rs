// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use derive_where::derive_where;
use firewood::merkle;
use firewood::v2::api;
use firewood::v2::api::BoxKeyValueIter;

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
    type Item = Result<(merkle::Key, merkle::Value), api::Error>;

    fn next(&mut self) -> Option<Self::Item> {
        let out = self.0.as_mut()?.next();
        if out.is_none() {
            // iterator exhausted; drop it so the NodeStore can be released
            self.0 = None;
        }
        out
    }
}

#[derive(Debug, Default)]
pub struct CreateIteratorResult<'db>(pub IteratorHandle<'db>);
