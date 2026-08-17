// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::iter::Chain;

use super::{SplitPath, TriePath};

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

    fn as_component_slice(&self) -> super::PartialPath<'_> {
        if self.prefix.is_empty() {
            self.suffix.as_component_slice()
        } else if self.suffix.is_empty() {
            self.prefix.as_component_slice()
        } else {
            let mut buf = super::PathBuf::with_capacity(self.len());
            buf.extend(self.prefix.components());
            buf.extend(self.suffix.components());
            super::PartialPath::Owned(buf)
        }
    }
}

impl<P: SplitPath, S: SplitPath> SplitPath for JoinedPath<P, S> {
    fn split_at(self, mid: usize) -> (Self, Self) {
        if let Some(mid) = mid.checked_sub(self.prefix.len()) {
            let (a_suffix, b_suffix) = self.suffix.split_at(mid);
            let prefix: Self = Self {
                prefix: self.prefix,
                suffix: a_suffix,
            };
            let suffix = Self {
                prefix: P::default(),
                suffix: b_suffix,
            };
            (prefix, suffix)
        } else {
            let (a_prefix, b_prefix) = self.prefix.split_at(mid);
            let prefix = Self {
                prefix: a_prefix,
                suffix: S::default(),
            };
            let suffix: Self = Self {
                prefix: b_prefix,
                suffix: self.suffix,
            };
            (prefix, suffix)
        }
    }

    fn split_first(self) -> Option<(super::PathComponent, Self)> {
        if let Some((first, prefix)) = self.prefix.split_first() {
            Some((
                first,
                Self {
                    prefix,
                    suffix: self.suffix,
                },
            ))
        } else if let Some((first, suffix)) = self.suffix.split_first() {
            Some((
                first,
                Self {
                    prefix: P::default(),
                    suffix,
                },
            ))
        } else {
            None
        }
    }
}
