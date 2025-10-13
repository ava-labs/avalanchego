// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::iter::Chain;

use super::TriePath;

/// Joins two path segments into a single path, retaining the original segments
/// without needing to allocate a new contiguous array.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct JoinedPath<P, S> {
    /// The prefix segment of the path.
    pub prefix: P,

    /// The suffix segment of the path.
    pub suffix: S,
}

impl<P: TriePath, S: TriePath> JoinedPath<P, S> {
    /// Creates a new joined path from the given prefix and suffix.
    ///
    /// This does not allocate and takes ownership of the input segments.
    pub const fn new(prefix: P, suffix: S) -> Self {
        Self { prefix, suffix }
    }
}

impl<P: TriePath, S: TriePath> TriePath for JoinedPath<P, S> {
    type Components<'a>
        = Chain<P::Components<'a>, S::Components<'a>>
    where
        Self: 'a;

    fn len(&self) -> usize {
        self.prefix
            .len()
            .checked_add(self.suffix.len())
            .expect("joined path length overflowed usize")
    }

    fn is_empty(&self) -> bool {
        self.prefix.is_empty() && self.suffix.is_empty()
    }

    fn components(&self) -> Self::Components<'_> {
        self.prefix.components().chain(self.suffix.components())
    }
}
