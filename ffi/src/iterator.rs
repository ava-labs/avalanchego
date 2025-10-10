// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::ops::{Deref, DerefMut};

use derive_where::derive_where;
use firewood::v2::api::BoxKeyValueIter;

/// An opaque wrapper around a [`BoxKeyValueIter`].
#[derive_where(Debug)]
#[derive_where(skip_inner)]
pub struct IteratorHandle<'view>(BoxKeyValueIter<'view>);

impl<'view> From<BoxKeyValueIter<'view>> for IteratorHandle<'view> {
    fn from(value: BoxKeyValueIter<'view>) -> Self {
        IteratorHandle(value)
    }
}

impl<'view> Deref for IteratorHandle<'view> {
    type Target = BoxKeyValueIter<'view>;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl DerefMut for IteratorHandle<'_> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

#[derive(Debug)]
pub struct CreateIteratorResult<'db>(pub IteratorHandle<'db>);
