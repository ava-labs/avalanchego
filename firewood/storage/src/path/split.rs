// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use smallvec::SmallVec;

use super::{PathComponent, TriePath};

/// A trie path that can be (cheaply) split into two sub-paths.
///
/// Implementations are expected to be cheap to split (i.e. no allocations).
pub trait SplitPath: TriePath + Default + Copy {
    /// Splits the path at the given index within the path.
    ///
    /// The returned tuple contains the two sub-paths `(prefix, suffix)`.
    ///
    /// # Panics
    ///
    /// - If `mid > self.len()`.
    fn split_at(self, mid: usize) -> (Self, Self);

    /// Splits the first path component off of this path, returning it along with
    /// the remaining path.
    ///
    /// Returns [`None`] if the path is empty.
    fn split_first(self) -> Option<(PathComponent, Self)>;

    /// Computes the longest common prefix of this path and another path, along
    /// with their respective suffixes.
    fn longest_common_prefix<T: SplitPath>(self, other: T) -> PathCommonPrefix<Self, T> {
        PathCommonPrefix::new(self, other)
    }
}

/// A type that can be converted into a splittable path.
///
/// This trait is analogous to [`IntoIterator`] for [`Iterator`] but for trie
/// paths instead of iterators.
///
/// Like `IntoIterator`, a blanket implementation is provided for all types that
/// already implement [`SplitPath`].
pub trait IntoSplitPath: TriePath {
    /// The splittable path type derived from this type.
    type Path: SplitPath;

    /// Converts this type into a splittable path.
    #[must_use]
    fn into_split_path(self) -> Self::Path;
}

impl<T: SplitPath> IntoSplitPath for T {
    type Path = T;

    #[inline]
    fn into_split_path(self) -> Self::Path {
        self
    }
}

/// The common prefix of two paths, along with their respective suffixes.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct PathCommonPrefix<A, B, C = A> {
    /// The common prefix of the two paths.
    pub common: C,
    /// The suffix of the first path after the common prefix.
    pub a_suffix: A,
    /// The suffix of the second path after the common prefix.
    pub b_suffix: B,
}

impl<A: SplitPath, B: SplitPath> PathCommonPrefix<A, B> {
    /// Computes the common prefix of the two given paths, along with their suffixes.
    pub fn new(a: A, b: B) -> Self {
        let mid = a
            .components()
            .zip(b.components())
            .take_while(|&(a, b)| a == b)
            .count();
        let (common, a_suffix) = a.split_at(mid);
        let (_, b_suffix) = b.split_at(mid);
        Self {
            common,
            a_suffix,
            b_suffix,
        }
    }
}

impl<A: SplitPath, B: SplitPath, C> PathCommonPrefix<A, B, C> {
    /// Converts this into its constituent parts, where the suffixes are
    /// optionally empty.
    #[expect(clippy::type_complexity)]
    pub fn split_first_parts(self) -> (Option<(PathComponent, A)>, Option<(PathComponent, B)>, C) {
        (
            self.a_suffix.split_first(),
            self.b_suffix.split_first(),
            self.common,
        )
    }
}

impl SplitPath for &[PathComponent] {
    fn split_at(self, mid: usize) -> (Self, Self) {
        self.split_at(mid)
    }

    fn split_first(self) -> Option<(PathComponent, Self)> {
        match self.split_first() {
            Some((&first, rest)) => Some((first, rest)),
            None => None,
        }
    }
}

impl<'a, const N: usize> IntoSplitPath for &'a [PathComponent; N] {
    type Path = &'a [PathComponent];

    fn into_split_path(self) -> Self::Path {
        self
    }
}

impl<'a> IntoSplitPath for &'a Vec<PathComponent> {
    type Path = &'a [PathComponent];

    fn into_split_path(self) -> Self::Path {
        self
    }
}

impl<'a, A: smallvec::Array<Item = PathComponent>> IntoSplitPath for &'a SmallVec<A> {
    type Path = &'a [PathComponent];

    fn into_split_path(self) -> Self::Path {
        self
    }
}

#[cfg(test)]
mod tests {
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

    struct LcpTest<'a> {
        a: &'a [PathComponent],
        b: &'a [PathComponent],
        expected_common: &'a [PathComponent],
        expected_a_suffix: &'a [PathComponent],
        expected_b_suffix: &'a [PathComponent],
    }

    #[test_case(
        LcpTest {
            a: &path![1, 2, 3],
            b: &path![1, 2, 3],
            expected_common: &path![1, 2, 3],
            expected_a_suffix: &path![],
            expected_b_suffix: &path![],
        };
        "identical paths"
    )]
    #[test_case(
        LcpTest {
            a: &path![1, 2, 3],
            b: &path![1, 2, 4],
            expected_common: &path![1, 2],
            expected_a_suffix: &path![3],
            expected_b_suffix: &path![4],
        };
        "diverging paths"
    )]
    #[test_case(
        LcpTest {
            a: &path![1, 2, 3],
            b: &path![1, 2, 3, 4, 5],
            expected_common: &path![1, 2, 3],
            expected_a_suffix: &path![],
            expected_b_suffix: &path![4, 5],
        };
        "a is a strict prefix of b"
    )]
    #[test_case(
        LcpTest {
            a: &path![1, 2, 3, 4, 5],
            b: &path![1, 2, 3],
            expected_common: &path![1, 2, 3],
            expected_a_suffix: &path![4, 5],
            expected_b_suffix: &path![],
        };
        "b is a strict prefix of a"
    )]
    #[test_case(
        LcpTest {
            a: &path![1, 2, 3],
            b: &path![4, 5, 6],
            expected_common: &path![],
            expected_a_suffix: &path![1, 2, 3],
            expected_b_suffix: &path![4, 5, 6],
        };
        "no common prefix"
    )]
    fn test_longest_common_prefix(case: LcpTest<'_>) {
        let PathCommonPrefix {
            common,
            a_suffix,
            b_suffix,
        } = SplitPath::longest_common_prefix(case.a, case.b);

        assert_eq!(common, case.expected_common);
        assert_eq!(a_suffix, case.expected_a_suffix);
        assert_eq!(b_suffix, case.expected_b_suffix);
    }

    struct SplitFirstTest<'a> {
        path: &'a [PathComponent],
        expected: Option<(PathComponent, &'a [PathComponent])>,
    }

    #[test_case(
        SplitFirstTest {
            path: &path![],
            expected: None,
        };
        "empty path"
    )]
    #[test_case(
        SplitFirstTest{
            path: &path![1],
            expected: Some((pc!(1), &path![])),
        };
        "single element path"
    )]
    #[test_case(
        SplitFirstTest{
            path: &path![1, 2, 3],
            expected: Some((pc!(1), &path![2, 3])),
        };
        "path with multiple elements"
    )]
    fn test_split_first(case: SplitFirstTest<'_>) {
        let result = SplitPath::split_first(case.path);
        assert_eq!(result, case.expected);
    }
}
