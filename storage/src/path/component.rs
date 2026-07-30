// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use smallvec::SmallVec;

use super::{PartialPath, TriePath, TriePathFromUnpackedBytes};
use bytemuck::TransparentWrapper;
use bytemuck_derive::TransparentWrapper;

/// A path component in a hexary trie; which is only 4 bits (aka a nibble).
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, TransparentWrapper)]
#[repr(transparent)]
pub struct PathComponent(pub crate::u4::U4);

// These compile-time assertions ensure that `PathComponent` and `Option<PathComponent>`
// are both the same size as a u8.
const _: () = assert!(size_of::<PathComponent>() == 1);
const _: () = assert!(size_of::<Option<PathComponent>>() == 1);

/// An iterator over path components.
pub type ComponentIter<'a> = std::iter::Copied<std::slice::Iter<'a, PathComponent>>;

/// Extension methods for slices of path components.
pub trait PathComponentSliceExt {
    /// Casts this slice of path components to a byte slice.
    fn as_byte_slice(&self) -> &[u8];
}

impl PathComponent {
    /// All possible path components.
    ///
    /// This makes it easy to iterate over all possible children of a branch
    /// in a type-safe way. It is preferrable to do:
    ///
    /// ```rust
    /// # use firewood_storage::PathComponent;
    ///
    /// // Example child slots (one per nibble).
    /// # let children = [(); PathComponent::LEN];
    /// for (idx, slot) in PathComponent::ALL.into_iter().zip(children.iter()) {
    ///     let _ = (idx, slot);
    /// }
    /// ```
    ///
    /// instead of using a raw range like (`0..16`) or  [`Iterator::enumerate`],
    /// which does not give a type-safe path component.
    pub const ALL: [Self; Self::LEN] = [
        Self(crate::u4::U4::new_masked(0x0)),
        Self(crate::u4::U4::new_masked(0x1)),
        Self(crate::u4::U4::new_masked(0x2)),
        Self(crate::u4::U4::new_masked(0x3)),
        Self(crate::u4::U4::new_masked(0x4)),
        Self(crate::u4::U4::new_masked(0x5)),
        Self(crate::u4::U4::new_masked(0x6)),
        Self(crate::u4::U4::new_masked(0x7)),
        Self(crate::u4::U4::new_masked(0x8)),
        Self(crate::u4::U4::new_masked(0x9)),
        Self(crate::u4::U4::new_masked(0xA)),
        Self(crate::u4::U4::new_masked(0xB)),
        Self(crate::u4::U4::new_masked(0xC)),
        Self(crate::u4::U4::new_masked(0xD)),
        Self(crate::u4::U4::new_masked(0xE)),
        Self(crate::u4::U4::new_masked(0xF)),
    ];

    /// The number of possible path components.
    pub const LEN: usize = 16;
}

impl PathComponent {
    /// Returns the path component as a [`u8`].
    #[must_use]
    pub const fn as_u8(self) -> u8 {
        self.0.as_u8()
    }

    /// Returns the path component as a [`usize`].
    #[must_use]
    pub const fn as_usize(self) -> usize {
        self.as_u8() as usize
    }

    /// Tries to create a path component from the given [`u8`].
    ///
    /// For hexary tries, the input must be in the range 0x00 to 0x0F inclusive.
    /// Any value outside this range will result in [`None`].
    ///
    /// For 256-ary tries, any value is valid.
    #[must_use]
    pub const fn try_new(value: u8) -> Option<Self> {
        match crate::u4::U4::try_new(value) {
            Some(u4) => Some(Self(u4)),
            None => None,
        }
    }

    /// Creates a pair of path components from a single byte where the
    /// upper 4 bits are the first component and the lower 4 bits are the
    /// second component.
    #[must_use]
    pub const fn new_pair(v: u8) -> (Self, Self) {
        // `new_masked` keeps only the low 4 bits, so shift the high nibble down
        // before masking. (arity's `U4` provides no `new_pair`/`new_shifted`.)
        (
            Self(crate::u4::U4::new_masked(v >> 4)),
            Self(crate::u4::U4::new_masked(v)),
        )
    }

    /// Joins this [`PathComponent`] with another to create a single [`u8`] where
    /// this component is the upper 4 bits and the provided component is the
    /// lower 4 bits.
    #[must_use]
    pub const fn join(self, other: Self) -> u8 {
        // arity's `U4` provides no `join`; compute it directly.
        (self.0.as_u8() << 4) | other.0.as_u8()
    }

    pub(crate) const fn wrapping_next(self) -> Self {
        match crate::u4::U4::try_new(self.0.as_u8().wrapping_add(1)) {
            Some(next) => Self(next),
            None => Self(crate::u4::U4::MIN),
        }
    }
}

impl std::fmt::Debug for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Debug::fmt(&self.0, f)
    }
}

impl std::fmt::Display for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:X}")
    }
}

impl std::fmt::Binary for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Binary::fmt(&self.0, f)
    }
}

impl std::fmt::LowerHex for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::LowerHex::fmt(&self.0, f)
    }
}

impl std::fmt::UpperHex for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::UpperHex::fmt(&self.0, f)
    }
}

impl TriePath for PathComponent {
    type Components<'a>
        = std::option::IntoIter<Self>
    where
        Self: 'a;

    fn len(&self) -> usize {
        1
    }

    fn components(&self) -> Self::Components<'_> {
        Some(*self).into_iter()
    }

    fn as_component_slice(&self) -> PartialPath<'_> {
        PartialPath::Borrowed(std::slice::from_ref(self))
    }
}

impl TriePath for Option<PathComponent> {
    type Components<'a>
        = std::option::IntoIter<PathComponent>
    where
        Self: 'a;

    fn len(&self) -> usize {
        usize::from(self.is_some())
    }

    fn components(&self) -> Self::Components<'_> {
        (*self).into_iter()
    }

    fn as_component_slice(&self) -> PartialPath<'_> {
        PartialPath::Borrowed(self.as_slice())
    }
}

impl TriePath for [PathComponent] {
    type Components<'a>
        = ComponentIter<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        self.len()
    }

    fn components(&self) -> Self::Components<'_> {
        self.iter().copied()
    }

    fn as_component_slice(&self) -> PartialPath<'_> {
        PartialPath::Borrowed(self)
    }
}

impl<const N: usize> TriePath for [PathComponent; N] {
    type Components<'a>
        = ComponentIter<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        N
    }

    fn components(&self) -> Self::Components<'_> {
        self.iter().copied()
    }

    fn as_component_slice(&self) -> PartialPath<'_> {
        PartialPath::Borrowed(self)
    }
}

impl TriePath for Vec<PathComponent> {
    type Components<'a>
        = ComponentIter<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        self.len()
    }

    fn components(&self) -> Self::Components<'_> {
        self.iter().copied()
    }

    fn as_component_slice(&self) -> PartialPath<'_> {
        PartialPath::Borrowed(self.as_slice())
    }
}

impl<A: smallvec::Array<Item = PathComponent>> TriePath for SmallVec<A> {
    type Components<'a>
        = ComponentIter<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        self.len()
    }

    fn components(&self) -> Self::Components<'_> {
        self.iter().copied()
    }

    fn as_component_slice(&self) -> PartialPath<'_> {
        PartialPath::Borrowed(self.as_slice())
    }
}

impl<'input> TriePathFromUnpackedBytes<'input> for &'input [PathComponent] {
    type Error = crate::u4::TryFromIntError;

    fn path_from_unpacked_bytes(bytes: &'input [u8]) -> Result<Self, Self::Error> {
        // `U4::try_from_slice` validates every byte is `< 16` and returns the bytes
        // reinterpreted as `&[U4]` (validation + cast are Miri-tested in
        // arity-index). Reinterpret that `&[U4]` as `&[PathComponent]`.
        match crate::u4::U4::try_from_slice(bytes) {
            Some(u4s) => Ok(u4_slice_as_path_components(u4s)),
            // `None` ⇒ some byte is `>= 16`. arity's `TryFromIntError` is
            // `#[non_exhaustive]`, so obtain it from the failing `U4::try_from`
            // rather than constructing a literal.
            None => Err(bytes
                .iter()
                .copied()
                .find_map(|b| crate::u4::U4::try_from(b).err())
                .expect("try_from_slice returned None, so some byte is out of range")),
        }
    }
}

impl TriePathFromUnpackedBytes<'_> for Vec<PathComponent> {
    type Error = crate::u4::TryFromIntError;

    fn path_from_unpacked_bytes(bytes: &[u8]) -> Result<Self, Self::Error> {
        try_from_maybe_u4(bytes.iter().copied())
    }
}

impl<A: smallvec::Array<Item = PathComponent>> TriePathFromUnpackedBytes<'_> for SmallVec<A> {
    type Error = crate::u4::TryFromIntError;

    fn path_from_unpacked_bytes(bytes: &[u8]) -> Result<Self, Self::Error> {
        try_from_maybe_u4(bytes.iter().copied())
    }
}

impl super::TriePathFromPackedBytes<'_> for Vec<PathComponent> {
    fn path_from_packed_bytes(bytes: &'_ [u8]) -> Self {
        let path = super::PackedPathRef::path_from_packed_bytes(bytes);
        let mut this = Vec::new();
        // reserve_exact is used because we trust that `TriePath::len` returns the exact
        // length but `Vec::extend` won't trust `Iterator::size_hint` and may
        // over/under-allocate.
        this.reserve_exact(path.len());
        this.extend(path.components());
        this
    }
}

impl<A: smallvec::Array<Item = PathComponent>> super::TriePathFromPackedBytes<'_> for SmallVec<A> {
    fn path_from_packed_bytes(bytes: &'_ [u8]) -> Self {
        let path = super::PackedPathRef::path_from_packed_bytes(bytes);
        let mut this = SmallVec::<A>::new();
        // reserve_exact is used because we trust that `TriePath::len` returns the exact
        // length but `SmallVec::extend` won't trust `Iterator::size_hint` and may
        // over/under-allocate.
        this.reserve_exact(path.len());
        this.extend(path.components());
        this
    }
}

impl<'input> TriePathFromUnpackedBytes<'input> for Box<[PathComponent]> {
    type Error = <Vec<PathComponent> as TriePathFromUnpackedBytes<'input>>::Error;

    fn path_from_unpacked_bytes(bytes: &'input [u8]) -> Result<Self, Self::Error> {
        Vec::<PathComponent>::path_from_unpacked_bytes(bytes).map(Into::into)
    }
}

impl<'input> TriePathFromUnpackedBytes<'input> for std::rc::Rc<[PathComponent]> {
    type Error = <Vec<PathComponent> as TriePathFromUnpackedBytes<'input>>::Error;

    fn path_from_unpacked_bytes(bytes: &'input [u8]) -> Result<Self, Self::Error> {
        Vec::<PathComponent>::path_from_unpacked_bytes(bytes).map(Into::into)
    }
}

impl<'input> TriePathFromUnpackedBytes<'input> for std::sync::Arc<[PathComponent]> {
    type Error = <Vec<PathComponent> as TriePathFromUnpackedBytes<'input>>::Error;

    fn path_from_unpacked_bytes(bytes: &'input [u8]) -> Result<Self, Self::Error> {
        Vec::<PathComponent>::path_from_unpacked_bytes(bytes).map(Into::into)
    }
}

impl super::TriePathFromPackedBytes<'_> for Box<[PathComponent]> {
    fn path_from_packed_bytes(bytes: &[u8]) -> Self {
        Vec::<PathComponent>::path_from_packed_bytes(bytes).into()
    }
}

impl super::TriePathFromPackedBytes<'_> for std::rc::Rc<[PathComponent]> {
    fn path_from_packed_bytes(bytes: &[u8]) -> Self {
        Vec::<PathComponent>::path_from_packed_bytes(bytes).into()
    }
}

impl super::TriePathFromPackedBytes<'_> for std::sync::Arc<[PathComponent]> {
    fn path_from_packed_bytes(bytes: &[u8]) -> Self {
        Vec::<PathComponent>::path_from_packed_bytes(bytes).into()
    }
}

impl PathComponentSliceExt for [PathComponent] {
    fn as_byte_slice(&self) -> &[u8] {
        path_components_as_byte_slice(self)
    }
}

impl<T: std::ops::Deref<Target = [PathComponent]>> PathComponentSliceExt for T {
    fn as_byte_slice(&self) -> &[u8] {
        path_components_as_byte_slice(self)
    }
}

/// Reinterprets a `&[U4]` as a `&[PathComponent]`.
///
/// `PathComponent` is `#[repr(transparent)]` over `U4`, so `[U4]` and
/// `[PathComponent]` have identical layout.
#[inline]
fn u4_slice_as_path_components(u4s: &[crate::u4::U4]) -> &[PathComponent] {
    PathComponent::wrap_slice(u4s)
}

/// Reinterprets a `&[PathComponent]` as a `&[u8]`.
#[inline]
fn path_components_as_byte_slice(components: &[PathComponent]) -> &[u8] {
    crate::u4::U4::as_u8_slice(PathComponent::peel_slice(components))
}

#[inline]
fn try_from_maybe_u4<I: FromIterator<PathComponent>>(
    bytes: impl IntoIterator<Item = u8>,
) -> Result<I, crate::u4::TryFromIntError> {
    // `U4::try_from` yields arity's `TryFromIntError` on an out-of-range byte;
    // map each `U4` to a `PathComponent` and collect into `Result<I, _>`.
    bytes
        .into_iter()
        .map(|b| crate::u4::U4::try_from(b).map(PathComponent))
        .collect()
}

#[cfg(test)]
mod tests {
    use std::marker::PhantomData;

    use test_case::test_case;

    use super::*;

    #[test_case(PhantomData::<&[PathComponent]>; "slice")]
    #[test_case(PhantomData::<Box<[PathComponent]>>; "boxed slice")]
    #[test_case(PhantomData::<Vec<PathComponent>>; "vec")]
    #[test_case(PhantomData::<SmallVec<[PathComponent; 32]>>; "smallvec")]
    fn test_path_from_unpacked_bytes_hexary<T>(_: PhantomData<T>)
    where
        T: TriePathFromUnpackedBytes<'static, Error: std::fmt::Debug>,
    {
        let input: &[u8; _] = &[0x00, 0x01, 0x0A, 0x0F];
        let output = <T>::path_from_unpacked_bytes(input).expect("valid input");

        assert_eq!(output.len(), input.len());
        assert_eq!(
            output
                .components()
                .map(PathComponent::as_u8)
                .zip(input.iter().copied())
                .take_while(|&(pc, b)| pc == b)
                .count(),
            input.len(),
        );
    }

    #[test_case(PhantomData::<&[PathComponent]>; "slice")]
    #[test_case(PhantomData::<Box<[PathComponent]>>; "boxed slice")]
    #[test_case(PhantomData::<Vec<PathComponent>>; "vec")]
    #[test_case(PhantomData::<SmallVec<[PathComponent; 32]>>; "smallvec")]
    fn test_path_from_unpacked_bytes_hexary_invalid<T>(_: PhantomData<T>)
    where
        T: TriePathFromUnpackedBytes<'static> + std::fmt::Debug,
    {
        let input: &[u8; _] = &[0x00, 0x10, 0x0A, 0x0F];
        let _ = <T>::path_from_unpacked_bytes(input).expect_err("invalid input");
    }

    #[test_case(0x00, (0x0, 0x0); "0x00")]
    #[test_case(0x0F, (0x0, 0xF); "0x0F low nibble")]
    #[test_case(0xF0, (0xF, 0x0); "0xF0 high nibble")]
    #[test_case(0xAB, (0xA, 0xB); "0xAB")]
    #[test_case(0xFF, (0xF, 0xF); "0xFF")]
    fn test_path_component_new_pair_join(input: u8, expected: (u8, u8)) {
        let (upper, lower) = PathComponent::new_pair(input);
        assert_eq!((upper.as_u8(), lower.as_u8()), expected);
        // join is the inverse of new_pair.
        assert_eq!(upper.join(lower), input);
    }

    #[test]
    fn test_slice_reinterpret_roundtrip() {
        // Forward (checked) then reverse is the identity on valid nibbles.
        let bytes: &[u8] = &[0x00, 0x01, 0x0A, 0x0F, 0x07, 0x0C];
        let components =
            <&[PathComponent]>::path_from_unpacked_bytes(bytes).expect("all nibbles < 16");
        assert_eq!(components.len(), bytes.len());
        assert_eq!(components.as_byte_slice(), bytes);

        // An out-of-range byte is rejected.
        let bad: &[u8] = &[0x00, 0x10];
        assert!(<&[PathComponent]>::path_from_unpacked_bytes(bad).is_err());

        // The empty slice round-trips (the reinterpret must be null-safe).
        let empty: &[u8] = &[];
        let empty_components =
            <&[PathComponent]>::path_from_unpacked_bytes(empty).expect("empty is valid");
        assert!(empty_components.is_empty());
        assert_eq!(empty_components.as_byte_slice(), empty);
    }

    #[test]
    fn test_joined_path() {
        let path = <&[PathComponent] as TriePathFromUnpackedBytes>::path_from_unpacked_bytes(&[
            0x0A, 0x0B, 0x0C,
        ])
        .expect("valid input");

        let with_suffix = path.append(PathComponent::try_new(0x0D).expect("valid"));
        assert_eq!(with_suffix.len(), 4);
        assert_eq!(
            with_suffix
                .components()
                .map(PathComponent::as_u8)
                .collect::<Vec<_>>(),
            vec![0x0A, 0x0B, 0x0C, 0x0D],
        );

        let with_prefix = with_suffix.prepend(PathComponent::try_new(0x09).expect("valid"));
        assert_eq!(with_prefix.len(), 5);
        assert_eq!(
            with_prefix
                .components()
                .map(PathComponent::as_u8)
                .collect::<Vec<_>>(),
            vec![0x09, 0x0A, 0x0B, 0x0C, 0x0D],
        );
    }
}
