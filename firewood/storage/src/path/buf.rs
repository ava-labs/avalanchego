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

/// A RAII guard that resets a path buffer to its original length when dropped.
///
/// The guard exposes append-only methods to add components to the path buffer
/// but not remove them without dropping the guard. This ensures that the guard
/// will always restore the path buffer to its original state when dropped.
#[must_use]
pub struct PathGuard<'a> {
    buf: &'a mut PathBuf,
    len: usize,
}

impl std::fmt::Debug for PathGuard<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.buf.display().fmt(f)
    }
}

impl Drop for PathGuard<'_> {
    fn drop(&mut self) {
        self.buf.truncate(self.len);
    }
}

impl<'a> PathGuard<'a> {
    /// Creates a new guard that will reset the provided buffer to its current
    /// length when dropped.
    pub fn new(buf: &'a mut PathBuf) -> Self {
        Self {
            len: buf.len(),
            buf,
        }
    }

    /// Creates a new guard that will reset this guard's buffer to its current
    /// length when dropped.
    ///
    /// This allows for nested guards that can be used in recursive algorithms.
    pub fn fork(&mut self) -> PathGuard<'_> {
        PathGuard::new(self.buf)
    }

    /// Fork this guard and append the given segment to the path buffer.
    ///
    /// This is a convenience method that combines `fork` then `extend` in a
    /// single operation for ergonomic one-liners.
    ///
    /// The returned guard will reset the path buffer to its original length,
    /// before appending the given segment, when dropped.
    pub fn fork_append(&mut self, path: impl TriePath) -> PathGuard<'_> {
        let mut fork = self.fork();
        fork.extend(path.components());
        fork
    }

    /// Appends the given component to the path buffer.
    ///
    /// This component will be removed when the guard is dropped.
    pub fn push(&mut self, component: PathComponent) {
        self.buf.push(component);
    }
}

impl std::ops::Deref for PathGuard<'_> {
    type Target = PathBuf;

    fn deref(&self) -> &Self::Target {
        self.buf
    }
}

impl Extend<PathComponent> for PathGuard<'_> {
    fn extend<T: IntoIterator<Item = PathComponent>>(&mut self, iter: T) {
        self.buf.extend(iter);
    }
}
