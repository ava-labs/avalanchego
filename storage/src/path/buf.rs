// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{ComponentIter, IntoSplitPath, PathComponent, TriePath};

/// An owned buffer of path components.
pub type PathBuf = smallvec::SmallVec<[PathComponent; 32]>;

/// A trie path represented as a slice of path components, either borrowed or owned.
pub enum PartialPath<'a> {
    /// A borrowed slice of path components.
    Borrowed(&'a [PathComponent]),

    /// An owned buffer of path components.
    Owned(PathBuf),
}

impl std::fmt::Debug for PartialPath<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.display().fmt(f)
    }
}

impl std::fmt::Display for PartialPath<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.display().fmt(f)
    }
}

impl PartialPath<'_> {
    /// Returns the path as a slice of path components.
    #[inline]
    #[must_use]
    pub fn as_slice(&self) -> &[PathComponent] {
        match self {
            PartialPath::Borrowed(slice) => slice,
            PartialPath::Owned(buf) => buf.as_slice(),
        }
    }

    /// Converts this partial path into an owned path buffer.
    ///
    /// If the path is already owned, this is a no-op.
    /// If the path is borrowed, this allocates a new buffer and copies the
    /// components into it.
    #[inline]
    #[must_use]
    pub fn into_owned(self) -> PathBuf {
        match self {
            PartialPath::Borrowed(slice) => slice.into(),
            PartialPath::Owned(buf) => buf,
        }
    }

    /// Returns true if this partial path is a borrowed slice.
    #[inline]
    #[must_use]
    pub const fn is_borrowed(&self) -> bool {
        matches!(self, PartialPath::Borrowed(_))
    }

    /// Returns true if this partial path is an owned buffer.
    #[inline]
    #[must_use]
    pub const fn is_owned(&self) -> bool {
        matches!(self, PartialPath::Owned(_))
    }

    /// Acquires a mutable reference to the owned path buffer, converting
    /// the path to an owned buffer if it is currently a borrowed slice.
    pub fn to_mut(&mut self) -> &mut PathBuf {
        if let Self::Borrowed(buf) = self {
            *self = Self::Owned((*buf).into());
        }

        if let Self::Owned(buf) = self {
            buf
        } else {
            unreachable!()
        }
    }
}

impl std::ops::Deref for PartialPath<'_> {
    type Target = [PathComponent];

    fn deref(&self) -> &Self::Target {
        self.as_slice()
    }
}

impl std::borrow::Borrow<[PathComponent]> for PartialPath<'_> {
    fn borrow(&self) -> &[PathComponent] {
        self.as_slice()
    }
}

impl AsRef<[PathComponent]> for PartialPath<'_> {
    fn as_ref(&self) -> &[PathComponent] {
        self.as_slice()
    }
}

impl TriePath for PartialPath<'_> {
    type Components<'a>
        = ComponentIter<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        self.as_slice().len()
    }

    fn components(&self) -> Self::Components<'_> {
        self.as_slice().iter().copied()
    }

    fn as_component_slice(&self) -> PartialPath<'_> {
        PartialPath::Borrowed(self.as_slice())
    }
}

impl<'a> IntoSplitPath for &'a PartialPath<'_> {
    type Path = &'a [PathComponent];

    #[inline]
    fn into_split_path(self) -> Self::Path {
        self
    }
}
