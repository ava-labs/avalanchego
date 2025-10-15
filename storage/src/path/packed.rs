// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::TriePathAsPackedBytes;

use super::{PathComponent, SplitPath, TriePath, TriePathFromPackedBytes};

/// A packed representation of a trie path where each byte encodes two path components.
///
/// This is borrowed over the underlying byte slice and does not allocate.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default)]
pub struct PackedPathRef<'a> {
    prefix: Option<PathComponent>,
    middle: &'a [u8],
    suffix: Option<PathComponent>,
}

/// An iterator over the components of a [`PackedPathRef`].
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
#[must_use = "iterators are lazy and do nothing unless consumed"]
pub struct PackedPathComponents<'a> {
    path: PackedPathRef<'a>,
}

/// An iterator over the components of a path that yields them packed into bytes.
///
/// If there is a trailing component without a pair, it is padded with zero.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
#[must_use = "iterators are lazy and do nothing unless consumed"]
pub struct PackedBytes<I> {
    iter: I,
}

impl<I: Iterator<Item = PathComponent>> PackedBytes<I> {
    /// Creates a new iterator over the packed bytes of the given component iterator.
    pub const fn new(iter: I) -> Self {
        Self { iter }
    }
}

impl TriePath for PackedPathRef<'_> {
    type Components<'a>
        = PackedPathComponents<'a>
    where
        Self: 'a;

    fn len(&self) -> usize {
        self.middle
            .len()
            .wrapping_mul(2)
            .wrapping_add(usize::from(self.prefix.is_some()))
            .wrapping_add(usize::from(self.suffix.is_some()))
    }

    fn components(&self) -> Self::Components<'_> {
        PackedPathComponents { path: *self }
    }
}

impl SplitPath for PackedPathRef<'_> {
    fn split_at(self, mid: usize) -> (Self, Self) {
        assert!(mid <= self.len(), "mid > self.len()");

        if let Some(mid) = mid.checked_sub(usize::from(self.prefix.is_some())) {
            // the mid consumed the prefix, if it existed
            if mid.is_multiple_of(2) {
                // middle is split cleanly
                let (a_middle, b_middle) = self.middle.split_at(mid / 2);
                let prefix = Self {
                    prefix: self.prefix,
                    middle: a_middle,
                    suffix: None,
                };
                let suffix = Self {
                    prefix: None,
                    middle: b_middle,
                    suffix: self.suffix,
                };
                (prefix, suffix)
            } else {
                // mid splits a middle component into the suffix of `prefix` and the
                // prefix of `suffix`
                let (a_middle, b_middle) = self.middle.split_at(mid / 2);
                let Some((&middle_byte, b_middle)) = b_middle.split_first() else {
                    // `mid` is oob of `b_middle`, which happens if self.suffix is Some,
                    // and `mid` is self.len()
                    return (self, Self::default());
                };

                let (upper, lower) = PathComponent::new_pair(middle_byte);
                let prefix = Self {
                    prefix: self.prefix,
                    middle: a_middle,
                    suffix: Some(upper),
                };
                let suffix = Self {
                    prefix: Some(lower),
                    middle: b_middle,
                    suffix: self.suffix,
                };
                (prefix, suffix)
            }
        } else {
            // `prefix` is some and `mid` is zero
            (Self::default(), self)
        }
    }

    fn split_first(self) -> Option<(PathComponent, Self)> {
        let mut iter = self.into_iter();
        let first = iter.next()?;
        Some((first, iter.path))
    }
}

impl<'input> TriePathFromPackedBytes<'input> for PackedPathRef<'input> {
    fn path_from_packed_bytes(bytes: &'input [u8]) -> Self {
        Self {
            prefix: None,
            middle: bytes,
            suffix: None,
        }
    }
}

impl<'a> IntoIterator for PackedPathRef<'a> {
    type Item = PathComponent;

    type IntoIter = PackedPathComponents<'a>;

    fn into_iter(self) -> Self::IntoIter {
        PackedPathComponents { path: self }
    }
}

impl Iterator for PackedPathComponents<'_> {
    type Item = PathComponent;

    fn next(&mut self) -> Option<Self::Item> {
        if let Some(next) = self.path.prefix.take() {
            return Some(next);
        }

        if let Some((&next, rest)) = self.path.middle.split_first() {
            let (upper, lower) = PathComponent::new_pair(next);
            self.path.prefix = Some(lower);
            self.path.middle = rest;
            return Some(upper);
        }

        self.path.suffix.take()
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        let len = self.path.len();
        (len, Some(len))
    }
}

impl DoubleEndedIterator for PackedPathComponents<'_> {
    fn next_back(&mut self) -> Option<Self::Item> {
        if let Some(next) = self.path.suffix.take() {
            return Some(next);
        }

        if let Some((&last, rest)) = self.path.middle.split_last() {
            let (upper, lower) = PathComponent::new_pair(last);
            self.path.suffix = Some(upper);
            self.path.middle = rest;
            return Some(lower);
        }

        self.path.prefix.take()
    }
}

impl ExactSizeIterator for PackedPathComponents<'_> {}

impl std::iter::FusedIterator for PackedPathComponents<'_> {}

impl<I: Iterator<Item = PathComponent>> Iterator for PackedBytes<I> {
    type Item = u8;

    fn next(&mut self) -> Option<Self::Item> {
        let hi = self.iter.next()?;
        let lo = self.iter.next().unwrap_or(const { PathComponent::ALL[0] });
        Some(hi.join(lo))
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        let (lower, upper) = self.iter.size_hint();
        let lower = lower.div_ceil(2);
        let upper = upper.map(|u| u.div_ceil(2));
        (lower, upper)
    }
}

impl<I: DoubleEndedIterator<Item = PathComponent>> DoubleEndedIterator for PackedBytes<I> {
    fn next_back(&mut self) -> Option<Self::Item> {
        let lo = self.iter.next_back()?;
        let hi = self
            .iter
            .next_back()
            .unwrap_or(const { PathComponent::ALL[0] });
        Some(hi.join(lo))
    }
}

impl<I: ExactSizeIterator<Item = PathComponent>> ExactSizeIterator for PackedBytes<I> {}

impl<I: std::iter::FusedIterator<Item = PathComponent>> std::iter::FusedIterator
    for PackedBytes<I>
{
}

impl<T: TriePath + ?Sized> TriePathAsPackedBytes for T {
    type PackedBytesIter<'a>
        = PackedBytes<T::Components<'a>>
    where
        Self: 'a;

    fn as_packed_bytes(&self) -> Self::PackedBytesIter<'_> {
        PackedBytes::new(self.components())
    }
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used)]

    // NB: tests do not need to worry about 256-ary tries because this module is
    // only compiled when the "branch_factor_256" feature is not enabled.

    use test_case::test_case;

    use super::*;

    /// Yields a [`PathComponent`] failing to compile if the given expression is
    /// not a valid path component.
    macro_rules! pc {
        ($elem:expr) => {
            const { PathComponent::ALL[$elem] }
        };
    }

    /// Yields an array of [`PathComponent`]s failing to compile if any of the
    /// given expressions are not valid path components.
    ///
    /// The expression yields an array, not a slice, and a reference must be taken
    /// to convert it to a slice.
    macro_rules! path {
        ($($elem:expr),* $(,)?) => {
            [ $( pc!($elem), )* ]
        };
    }

    struct FromPackedBytesTest<'a> {
        bytes: &'a [u8],
        components: &'a [PathComponent],
    }

    #[test_case(
        FromPackedBytesTest{
            bytes: &[],
            components: &path![],
        };
        "empty path"
    )]
    #[test_case(
        FromPackedBytesTest{
            bytes: b"a",
            components: &path![6, 1],
        };
        "single byte path"
    )]
    #[test_case(
        FromPackedBytesTest{
            bytes: b"abc",
            components: &path![6, 1, 6, 2, 6, 3],
        };
        "multi-byte path"
    )]
    fn test_from_packed_bytes(case: FromPackedBytesTest<'_>) {
        let path = PackedPathRef::path_from_packed_bytes(case.bytes);
        assert_eq!(path.len(), case.components.len());

        assert!(path.components().eq(case.components.iter().copied()));
        assert!(
            path.components()
                .rev()
                .eq(case.components.iter().copied().rev())
        );

        assert!(path.as_packed_bytes().eq(case.bytes.iter().copied()));
        assert!(
            path.as_packed_bytes()
                .rev()
                .eq(case.bytes.iter().copied().rev())
        );

        assert!(TriePath::path_eq(&path, &case.components));
        assert!(TriePath::path_cmp(&path, &case.components).is_eq());
    }

    struct SplitAtTest<'a> {
        path: PackedPathRef<'a>,
        mid: usize,
        a: &'a [PathComponent],
        b: &'a [PathComponent],
    }

    #[test_case(
        SplitAtTest{
            path: PackedPathRef::path_from_packed_bytes(b""),
            mid: 0,
            a: &path![],
            b: &path![],
        };
        "empty path"
    )]
    #[test_case(
        SplitAtTest{
            path: PackedPathRef::path_from_packed_bytes(b"a"),
            mid: 0,
            a: &path![],
            b: &path![6, 1],
        };
        "single byte path, split at 0"
    )]
    #[test_case(
        SplitAtTest{
            path: PackedPathRef::path_from_packed_bytes(b"a"),
            mid: 1,
            a: &path![6],
            b: &path![1],
        };
        "single byte path, split at 1"
    )]
    #[test_case(
        SplitAtTest{
            path: PackedPathRef::path_from_packed_bytes(b"a"),
            mid: 2,
            a: &path![6, 1],
            b: &path![],
        };
        "single byte path, split at len"
    )]
    #[test_case(
        SplitAtTest{
            path: PackedPathRef::path_from_packed_bytes(b"abc"),
            mid: 2,
            a: &path![6, 1],
            b: &path![6, 2, 6, 3],
        };
        "multi byte path, split at 2"
    )]
    fn test_split_at(case: SplitAtTest<'_>) {
        let (a, b) = case.path.split_at(case.mid);
        assert_eq!(a.len(), case.mid);
        assert_eq!(a.len().wrapping_add(b.len()), case.path.len());
        assert!(a.path_eq(&case.a));
        assert!(b.path_eq(&case.b));
        assert!(a.append(b).path_eq(&case.path));
        if let Some(mid) = case.mid.checked_sub(1) {
            let (_, path) = dbg!(case.path.split_first()).unwrap();
            let (_, b) = dbg!(path.split_at(mid));
            assert!(
                b.path_eq(&case.b),
                "{} != {} ({}) (mid = {mid})",
                b.display(),
                case.b.display(),
                path.display(),
            );
        }
        if let Some(len) = case.path.len().checked_sub(1)
            && len >= case.mid
        {
            let (path, _) = dbg!(case.path.split_at(len));
            let (a, _) = dbg!(path.split_at(case.mid));
            assert!(
                a.path_eq(&case.a),
                "{} != {} ({}) (len = {len})",
                a.display(),
                case.a.display(),
                path.display(),
            );
        }
    }

    struct AsPackedBytesTest<'a, T> {
        path: T,
        expected: &'a [u8],
    }

    #[test_case(
        AsPackedBytesTest {
            path: PackedPathRef::path_from_packed_bytes(&[]),
            expected: &[],
        };
        "empty packed path"
    )]
    #[test_case(
        AsPackedBytesTest::<&[PathComponent]> {
            path: &[],
            expected: &[],
        };
        "empty unpacked path"
    )]
    #[test_case(
        AsPackedBytesTest {
            path: PackedPathRef::path_from_packed_bytes(b"abc"),
            expected: b"abc",
        };
        "multi-byte packed path"
    )]
    #[test_case(
        AsPackedBytesTest::<&[PathComponent]> {
            path: &[PathComponent::ALL[6]],
            expected: &[0x60],
        };
        "odd-lengthed unpacked path"
    )]
    #[test_case(
        AsPackedBytesTest::<&[PathComponent]> {
            path: &[PathComponent::ALL[6], PathComponent::ALL[1]],
            expected: &[0x61],
        };
        "even-lengthed unpacked path"
    )]
    #[test_case(
        AsPackedBytesTest::<&[PathComponent]> {
            path: &[PathComponent::ALL[6], PathComponent::ALL[1], PathComponent::ALL[2]],
            expected: &[0x61, 0x20],
        };
        "three-length unpacked path"
    )]
    fn test_as_packed_bytes<T: TriePath>(case: AsPackedBytesTest<'_, T>) {
        let as_packed = case.path.as_packed_bytes().collect::<Vec<u8>>();
        assert_eq!(*as_packed, *case.expected);
    }
}
