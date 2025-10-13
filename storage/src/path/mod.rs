// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod component;
mod joined;

pub use self::component::PathComponent;
pub use self::joined::JoinedPath;

/// A trie path of components with different underlying representations.
///
/// The underlying representation does not need to be a contiguous array of
/// [`PathComponent`], but it must be possible to iterate over them in order
/// as well as have a known length.
pub trait TriePath {
    /// The iterator returned by [`TriePath::components`].
    type Components<'a>: Iterator<Item = PathComponent> + Clone + 'a
    where
        Self: 'a;

    /// The length, in path components, of this path.
    fn len(&self) -> usize;

    /// Returns true if this path is empty (i.e. has length 0).
    fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Returns an iterator over the components of this path.
    fn components(&self) -> Self::Components<'_>;

    /// Appends the provided path segment to this path, returning a new joined
    /// path that represents the concatenation of the two paths.
    ///
    /// The returned path is a view over the two input paths and does not
    /// allocate. The input paths are consumed and ownership is taken.
    fn append<S>(self, suffix: S) -> JoinedPath<Self, S>
    where
        Self: Sized,
        S: TriePath,
    {
        JoinedPath::new(self, suffix)
    }

    /// Prepends the provided path segment to this path, returning a new joined
    /// path that represents the concatenation of the two paths.
    ///
    /// The inverse of [`TriePath::append`].
    ///
    /// The returned path is a view over the two input paths and does not
    /// allocate. The input paths are consumed and ownership is taken.
    fn prepend<P>(self, prefix: P) -> JoinedPath<P, Self>
    where
        Self: Sized,
        P: TriePath,
    {
        prefix.append(self)
    }

    /// Compares this path against another path for equality using path component
    /// equality.
    ///
    /// This is analogous to [`Iterator::eq`] and is different than [`PartialEq`]
    /// which may have different semantics depending on the underlying type and
    /// representation as well as may not be implemented for the cross-type
    /// comparisons.
    fn path_eq<T: TriePath + ?Sized>(&self, other: &T) -> bool {
        self.len() == other.len() && self.components().eq(other.components())
    }

    /// Compares this path against another path using path-component lexicographic
    /// ordering. Strict prefixes are less than their longer counterparts.
    ///
    /// This is analogous to [`Iterator::cmp`] and is different than [`Ord`]
    /// which may have different semantics depending on the underlying type and
    /// representation as well as may not be implemented for the cross-type
    /// comparisons.
    fn path_cmp<T: TriePath + ?Sized>(&self, other: &T) -> std::cmp::Ordering {
        self.components().cmp(other.components())
    }

    /// Returns a wrapper type that implements [`std::fmt::Display`] and
    /// [`std::fmt::Debug`] for this path.
    fn display(&self) -> DisplayPath<'_, Self> {
        DisplayPath { path: self }
    }
}

/// Constructor for a trie path from a set of unpacked bytes; where each byte
/// is a whole path component regardless of the normal width of a path component.
///
/// For 256-ary tries, this is the bytes as-is.
///
/// For hexary tries, each byte must occupy only the lower 4 bits. Any byte with
/// a bit set in the upper 4 bits will result in an error.
pub trait TriePathFromUnpackedBytes<'input>: TriePath + Sized {
    /// The error type returned if the bytes are invalid.
    type Error;

    /// Constructs a path from the given unpacked bytes.
    ///
    /// For hexary tries, each byte must be in the range 0x00 to 0x0F inclusive.
    /// Any byte outside this range will result in an error.
    ///
    /// # Errors
    ///
    /// - The input is invalid.
    fn path_from_unpacked_bytes(bytes: &'input [u8]) -> Result<Self, Self::Error>;
}

#[inline]
fn display_path(
    f: &mut std::fmt::Formatter<'_>,
    mut comp: impl Iterator<Item = PathComponent>,
) -> std::fmt::Result {
    comp.try_for_each(|c| write!(f, "{c}"))
}

/// A wrapper type that implements [`Display`](std::fmt::Display) and
/// [`Debug`](std::fmt::Debug) for any type that implements [`TriePath`].
pub struct DisplayPath<'a, P: TriePath + ?Sized> {
    path: &'a P,
}

impl<P: TriePath + ?Sized> std::fmt::Debug for DisplayPath<'_, P> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        display_path(f, self.path.components())
    }
}

impl<P: TriePath + ?Sized> std::fmt::Display for DisplayPath<'_, P> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        display_path(f, self.path.components())
    }
}

impl<T: TriePath + ?Sized> TriePath for &T {
    type Components<'a>
        = T::Components<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        (**self).len()
    }

    fn components(&self) -> Self::Components<'_> {
        (**self).components()
    }
}

impl<T: TriePath + ?Sized> TriePath for &mut T {
    type Components<'a>
        = T::Components<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        (**self).len()
    }

    fn components(&self) -> Self::Components<'_> {
        (**self).components()
    }
}

impl<T: TriePath + ?Sized> TriePath for Box<T> {
    type Components<'a>
        = T::Components<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        (**self).len()
    }

    fn components(&self) -> Self::Components<'_> {
        (**self).components()
    }
}

impl<T: TriePath + ?Sized> TriePath for std::rc::Rc<T> {
    type Components<'a>
        = T::Components<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        (**self).len()
    }

    fn components(&self) -> Self::Components<'_> {
        (**self).components()
    }
}

impl<T: TriePath + ?Sized> TriePath for std::sync::Arc<T> {
    type Components<'a>
        = T::Components<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        (**self).len()
    }

    fn components(&self) -> Self::Components<'_> {
        (**self).components()
    }
}
