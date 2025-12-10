// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use smallvec::SmallVec;

use super::{PartialPath, TriePath, TriePathFromUnpackedBytes};

#[cfg(not(feature = "branch_factor_256"))]
/// A path component in a hexary trie; which is only 4 bits (aka a nibble).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PathComponent(pub crate::u4::U4);

#[cfg(feature = "branch_factor_256")]
/// A path component in a 256-ary trie; which is 8 bits (aka a byte).
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PathComponent(pub u8);

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
    /// ```ignore
    /// for (idx, slot) in PathComponent::ALL.into_iter().zip(branch.children.each_ref()) {
    ///     /// use idx and slot
    /// }
    /// ```
    ///
    /// instead of using a raw range like (`0..16`) or  [`Iterator::enumerate`],
    /// which does not give a type-safe path component.
    #[cfg(not(feature = "branch_factor_256"))]
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

    /// All possible path components.
    ///
    /// This makes it easy to iterate over all possible children of a branch
    /// in a type-safe way. It is preferrable to do:
    ///
    /// ```ignore
    /// for (idx, slot) in PathComponent::ALL.into_iter().zip(branch.children.each_ref()) {
    ///     /// use idx and slot
    /// }
    /// ```
    ///
    /// instead of using a raw range like (`0..256`) or  [`Iterator::enumerate`],
    /// which does not give a type-safe path component.
    #[cfg(feature = "branch_factor_256")]
    pub const ALL: [Self; Self::LEN] = {
        let mut all = [Self(0); 256];
        let mut i = 0;
        #[expect(clippy::indexing_slicing)]
        while i < 256 {
            all[i] = Self(i as u8);
            i += 1;
        }
        all
    };

    /// The number of possible path components.
    pub const LEN: usize = if cfg!(feature = "branch_factor_256") {
        256
    } else {
        16
    };
}

impl PathComponent {
    /// Returns the path component as a [`u8`].
    #[must_use]
    pub const fn as_u8(self) -> u8 {
        #[cfg(not(feature = "branch_factor_256"))]
        {
            self.0.as_u8()
        }
        #[cfg(feature = "branch_factor_256")]
        {
            self.0
        }
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
        #[cfg(not(feature = "branch_factor_256"))]
        {
            match crate::u4::U4::try_new(value) {
                Some(u4) => Some(Self(u4)),
                None => None,
            }
        }
        #[cfg(feature = "branch_factor_256")]
        {
            Some(Self(value))
        }
    }

    /// Creates a pair of path components from a single byte where the
    /// upper 4 bits are the first component and the lower 4 bits are the
    /// second component.
    #[cfg(not(feature = "branch_factor_256"))]
    #[must_use]
    pub const fn new_pair(v: u8) -> (Self, Self) {
        let (upper, lower) = crate::u4::U4::new_pair(v);
        (Self(upper), Self(lower))
    }

    /// Joins this [`PathComponent`] with another to create a single [`u8`] where
    /// this component is the upper 4 bits and the provided component is the
    /// lower 4 bits.
    #[cfg(not(feature = "branch_factor_256"))]
    #[must_use]
    pub const fn join(self, other: Self) -> u8 {
        self.0.join(other.0)
    }

    pub(crate) const fn wrapping_next(self) -> Self {
        #[cfg(not(feature = "branch_factor_256"))]
        {
            match crate::u4::U4::try_new(self.0.as_u8().wrapping_add(1)) {
                Some(next) => Self(next),
                None => Self(crate::u4::U4::MIN),
            }
        }
        #[cfg(feature = "branch_factor_256")]
        {
            Self(self.0.wrapping_add(1))
        }
    }
}

impl std::fmt::Display for PathComponent {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        #[cfg(not(feature = "branch_factor_256"))]
        {
            write!(f, "{self:X}")
        }
        #[cfg(feature = "branch_factor_256")]
        {
            write!(f, "{self:02X}")
        }
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

#[cfg(not(feature = "branch_factor_256"))]
impl<'input> TriePathFromUnpackedBytes<'input> for &'input [PathComponent] {
    type Error = crate::u4::TryFromIntError;

    fn path_from_unpacked_bytes(bytes: &'input [u8]) -> Result<Self, Self::Error> {
        if bytes.iter().all(|&b| b <= 0x0F) {
            #[expect(unsafe_code)]
            // SAFETY: we have verified that all bytes are in the valid range for
            // `U4` (0x00 to 0x0F inclusive); therefore, it is now safe for us
            // to reinterpret a &[u8] as a &[PathComponent].
            Ok(unsafe { byte_slice_as_path_components_unchecked(bytes) })
        } else {
            Err(crate::u4::TryFromIntError)
        }
    }
}

#[cfg(not(feature = "branch_factor_256"))]
impl TriePathFromUnpackedBytes<'_> for Vec<PathComponent> {
    type Error = crate::u4::TryFromIntError;

    fn path_from_unpacked_bytes(bytes: &[u8]) -> Result<Self, Self::Error> {
        try_from_maybe_u4(bytes.iter().copied())
    }
}

#[cfg(not(feature = "branch_factor_256"))]
impl<A: smallvec::Array<Item = PathComponent>> TriePathFromUnpackedBytes<'_> for SmallVec<A> {
    type Error = crate::u4::TryFromIntError;

    fn path_from_unpacked_bytes(bytes: &[u8]) -> Result<Self, Self::Error> {
        try_from_maybe_u4(bytes.iter().copied())
    }
}

#[cfg(not(feature = "branch_factor_256"))]
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

#[cfg(not(feature = "branch_factor_256"))]
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

#[cfg(feature = "branch_factor_256")]
impl<'input> TriePathFromUnpackedBytes<'input> for &'input [PathComponent] {
    type Error = std::convert::Infallible;

    fn path_from_unpacked_bytes(bytes: &'input [u8]) -> Result<Self, Self::Error> {
        #[expect(unsafe_code)]
        // SAFETY: u8 is always valid for PathComponent in 256-ary tries.
        Ok(unsafe { byte_slice_as_path_components_unchecked(bytes) })
    }
}

#[cfg(feature = "branch_factor_256")]
impl TriePathFromUnpackedBytes<'_> for Vec<PathComponent> {
    type Error = std::convert::Infallible;

    fn path_from_unpacked_bytes(bytes: &[u8]) -> Result<Self, Self::Error> {
        Ok(bytes.iter().copied().map(PathComponent).collect())
    }
}

#[cfg(feature = "branch_factor_256")]
impl<A: smallvec::Array<Item = PathComponent>> TriePathFromUnpackedBytes<'_> for SmallVec<A> {
    type Error = std::convert::Infallible;

    fn path_from_unpacked_bytes(bytes: &[u8]) -> Result<Self, Self::Error> {
        Ok(bytes.iter().copied().map(PathComponent).collect())
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

// these must also only be included when `branch_factor_256` is not enabled otherwise
// they would conflict with the blanket impl of TriePathFromPackedBytes over
// TriePathFromUnpackedBytes

#[cfg(not(feature = "branch_factor_256"))]
impl super::TriePathFromPackedBytes<'_> for Box<[PathComponent]> {
    fn path_from_packed_bytes(bytes: &[u8]) -> Self {
        Vec::<PathComponent>::path_from_packed_bytes(bytes).into()
    }
}

#[cfg(not(feature = "branch_factor_256"))]
impl super::TriePathFromPackedBytes<'_> for std::rc::Rc<[PathComponent]> {
    fn path_from_packed_bytes(bytes: &[u8]) -> Self {
        Vec::<PathComponent>::path_from_packed_bytes(bytes).into()
    }
}

#[cfg(not(feature = "branch_factor_256"))]
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

#[inline]
const unsafe fn byte_slice_as_path_components_unchecked(bytes: &[u8]) -> &[PathComponent] {
    #![expect(unsafe_code)]

    // SAFETY: The caller must ensure that all bytes are valid for `PathComponent`,
    // which is trivially true for 256-ary tries. For hexary tries, the caller must
    // ensure that each byte is in the range 0x00 to 0x0F inclusive.
    //
    // We also rely on the fact that `PathComponent` is a single element type
    // over `u8` (or `u4` which looks like a `u8` for this purpose).
    //
    // borrow rules ensure that the pointer for `bytes` is not null and
    // `bytes.len()` is always valid. The returned reference will have the same
    // lifetime as `bytes` so it cannot outlive the original slice.
    unsafe {
        &*(std::ptr::slice_from_raw_parts(bytes.as_ptr().cast::<PathComponent>(), bytes.len()))
    }
}

#[inline]
const fn path_components_as_byte_slice(components: &[PathComponent]) -> &[u8] {
    #![expect(unsafe_code)]

    // SAFETY: We rely on the fact that `PathComponent` is a single element type
    // over `u8` (or `u4` which looks like a `u8` for this purpose).
    //
    // borrow rules ensure that the pointer for `components` is not null and
    // `components.len()` is always valid. The returned reference will have the same
    // lifetime as `components` so it cannot outlive the original slice.
    unsafe {
        &*(std::ptr::slice_from_raw_parts(components.as_ptr().cast::<u8>(), components.len()))
    }
}

#[inline]
#[cfg(not(feature = "branch_factor_256"))]
fn try_from_maybe_u4<I: FromIterator<PathComponent>>(
    bytes: impl IntoIterator<Item = u8>,
) -> Result<I, crate::u4::TryFromIntError> {
    bytes
        .into_iter()
        .map(PathComponent::try_new)
        .collect::<Option<I>>()
        .ok_or(crate::u4::TryFromIntError)
}

#[cfg(test)]
mod tests {
    use std::marker::PhantomData;

    use test_case::test_case;

    use super::*;

    #[cfg_attr(not(feature = "branch_factor_256"), test_case(PhantomData::<&[PathComponent]>; "slice"))]
    #[cfg_attr(not(feature = "branch_factor_256"), test_case(PhantomData::<Box<[PathComponent]>>; "boxed slice"))]
    #[cfg_attr(not(feature = "branch_factor_256"), test_case(PhantomData::<Vec<PathComponent>>; "vec"))]
    #[cfg_attr(not(feature = "branch_factor_256"), test_case(PhantomData::<SmallVec<[PathComponent; 32]>>; "smallvec"))]
    #[cfg_attr(feature = "branch_factor_256", test_case(PhantomData::<&[PathComponent]>; "slice"))]
    #[cfg_attr(feature = "branch_factor_256", test_case(PhantomData::<Box<[PathComponent]>>; "boxed slice"))]
    #[cfg_attr(feature = "branch_factor_256", test_case(PhantomData::<Vec<PathComponent>>; "vec"))]
    #[cfg_attr(feature = "branch_factor_256", test_case(PhantomData::<SmallVec<[PathComponent; 32]>>; "smallvec"))]
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

    #[cfg(not(feature = "branch_factor_256"))]
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
