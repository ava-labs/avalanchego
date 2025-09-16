// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 8 occurrences after enabling the lint."
)]
#![expect(
    clippy::from_iter_instead_of_collect,
    reason = "Found 1 occurrences after enabling the lint."
)]
#![expect(
    clippy::inline_always,
    reason = "Found 1 occurrences after enabling the lint."
)]

// TODO: remove bitflags, we only use one bit
use bitflags::bitflags;
use smallvec::SmallVec;
use std::fmt::{self, Debug, LowerHex};
use std::iter::{FusedIterator, once};
use std::ops::Add;

static NIBBLES: [u8; 16] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];

/// Path is part or all of a node's path in the trie.
/// Each element is a nibble.
#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Default)]
pub struct Path(pub SmallVec<[u8; 64]>);

impl Debug for Path {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        for nib in &self.0 {
            if *nib > 0xf {
                write!(f, "[invalid {:02x}] ", *nib)?;
            } else {
                write!(f, "{:x} ", *nib)?;
            }
        }
        Ok(())
    }
}

impl LowerHex for Path {
    // TODO: handle fill / alignment / etc
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        if self.0.is_empty() {
            write!(f, "[]")
        } else {
            if f.alternate() {
                write!(f, "0x")?;
            }
            for nib in &self.0 {
                write!(f, "{:x}", *nib)?;
            }
            Ok(())
        }
    }
}

impl std::ops::Deref for Path {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        &self.0
    }
}

impl<T: AsRef<[u8]>> From<T> for Path {
    fn from(value: T) -> Self {
        Self(SmallVec::from_slice(value.as_ref()))
    }
}

bitflags! {
    // should only ever be the size of a nibble
    struct Flags: u8 {
        const ODD_LEN  = 0b0001;
    }
}

impl Path {
    /// Return an iterator over the encoded bytes
    pub fn iter_encoded(&self) -> impl Iterator<Item = u8> {
        let mut flags = Flags::empty();

        let has_odd_len = self.0.len() & 1 == 1;

        let extra_byte = if has_odd_len {
            flags.insert(Flags::ODD_LEN);

            None
        } else {
            Some(0)
        };

        once(flags.bits())
            .chain(extra_byte)
            .chain(self.0.iter().copied())
    }

    /// Creates a Path from a [Iterator] or other iterator that returns
    /// nibbles
    pub fn from_nibbles_iterator<T: Iterator<Item = u8>>(nibbles_iter: T) -> Self {
        Path(SmallVec::from_iter(nibbles_iter))
    }

    /// Creates an empty Path
    #[must_use]
    pub fn new() -> Self {
        Path(SmallVec::new())
    }

    /// Read from an iterator that returns nibbles with a prefix
    /// The prefix is one optional byte -- if not present, the Path is empty
    /// If there is one byte, and the byte contains a [`Flags::ODD_LEN`] (0x1)
    /// then there is another discarded byte after that.
    #[cfg(test)]
    pub fn from_encoded_iter<Iter: Iterator<Item = u8>>(mut iter: Iter) -> Self {
        let flags = Flags::from_bits_retain(iter.next().unwrap_or_default());

        if !flags.contains(Flags::ODD_LEN) {
            let _ = iter.next();
        }

        Self(iter.collect())
    }

    /// Add nibbles to the end of a path
    pub fn extend<T: IntoIterator<Item = u8>>(&mut self, iter: T) {
        self.0.extend(iter);
    }

    /// Create an iterator that returns the bytes from the underlying nibbles
    /// If there is an odd nibble at the end, it is dropped
    #[must_use]
    pub fn bytes_iter(&self) -> BytesIterator<'_> {
        BytesIterator {
            nibbles_iter: self.iter(),
        }
    }
}

/// Returns the nibbles in `nibbles_iter` as compressed bytes.
/// That is, each two nibbles are combined into a single byte.
#[derive(Debug)]
pub struct BytesIterator<'a> {
    nibbles_iter: std::slice::Iter<'a, u8>,
}

impl Add<Path> for Path {
    type Output = Path;
    fn add(self, other: Path) -> Self::Output {
        let mut new = self.clone();
        new.extend(other.iter().copied());
        new
    }
}

impl Iterator for BytesIterator<'_> {
    type Item = u8;
    fn next(&mut self) -> Option<Self::Item> {
        if let Some(&hi) = self.nibbles_iter.next()
            && let Some(&lo) = self.nibbles_iter.next()
        {
            return Some(hi * 16 + lo);
        }
        None
    }

    // this helps make the collection into a box faster
    fn size_hint(&self) -> (usize, Option<usize>) {
        (
            self.nibbles_iter.size_hint().0 / 2,
            self.nibbles_iter.size_hint().1.map(|max| max / 2),
        )
    }
}

/// Iterates over the nibbles in `data`.
/// That is, each byte in `data` is converted to two nibbles.
#[derive(Clone, Debug)]
pub struct NibblesIterator<'a> {
    data: &'a [u8],
    head: usize,
    tail: usize,
}

impl FusedIterator for NibblesIterator<'_> {}

impl Iterator for NibblesIterator<'_> {
    type Item = u8;

    #[cfg(feature = "branch_factor_256")]
    fn next(&mut self) -> Option<Self::Item> {
        if self.is_empty() {
            return None;
        }
        let result = self.data[self.head];
        self.head += 1;
        Some(result)
    }

    #[cfg(not(feature = "branch_factor_256"))]
    fn next(&mut self) -> Option<Self::Item> {
        if self.is_empty() {
            return None;
        }
        let result = if self.head.is_multiple_of(2) {
            #[expect(clippy::indexing_slicing)]
            NIBBLES[(self.data[self.head / 2] >> 4) as usize]
        } else {
            #[expect(clippy::indexing_slicing)]
            NIBBLES[(self.data[self.head / 2] & 0xf) as usize]
        };
        self.head += 1;
        Some(result)
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        let remaining = self.tail - self.head;
        (remaining, Some(remaining))
    }

    fn nth(&mut self, n: usize) -> Option<Self::Item> {
        self.head += std::cmp::min(n, self.tail - self.head);
        self.next()
    }
}

impl<'a> NibblesIterator<'a> {
    #[cfg(not(feature = "branch_factor_256"))]
    const BYTES_PER_NIBBLE: usize = 2;
    #[cfg(feature = "branch_factor_256")]
    const BYTES_PER_NIBBLE: usize = 1;

    #[inline(always)]
    const fn is_empty(&self) -> bool {
        self.head == self.tail
    }

    /// Returns a new `NibblesIterator` over the given `data`.
    /// Each byte in `data` is converted to two nibbles.
    #[must_use]
    pub const fn new(data: &'a [u8]) -> Self {
        NibblesIterator {
            data,
            head: 0,
            tail: Self::BYTES_PER_NIBBLE * data.len(),
        }
    }
}

impl DoubleEndedIterator for NibblesIterator<'_> {
    fn next_back(&mut self) -> Option<Self::Item> {
        if self.is_empty() {
            return None;
        }

        let result = if self.tail.is_multiple_of(2) {
            #[expect(clippy::indexing_slicing)]
            NIBBLES[(self.data[self.tail / 2 - 1] & 0xf) as usize]
        } else {
            #[expect(clippy::indexing_slicing)]
            NIBBLES[(self.data[self.tail / 2] >> 4) as usize]
        };
        self.tail -= 1;

        Some(result)
    }

    fn nth_back(&mut self, n: usize) -> Option<Self::Item> {
        self.tail -= std::cmp::min(n, self.tail - self.head);
        self.next_back()
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use std::fmt::Debug;
    use test_case::test_case;

    #[cfg(not(feature = "branch_factor_256"))]
    static TEST_BYTES: [u8; 4] = [0xde, 0xad, 0xbe, 0xef];

    #[test]
    #[cfg(not(feature = "branch_factor_256"))]
    fn happy_regular_nibbles() {
        let iter = NibblesIterator::new(&TEST_BYTES);
        let expected = [0xd, 0xe, 0xa, 0xd, 0xb, 0xe, 0xe, 0xf];

        assert!(iter.eq(expected));
    }

    #[test]
    #[cfg(not(feature = "branch_factor_256"))]
    fn size_hint() {
        let mut iter = NibblesIterator::new(&TEST_BYTES);
        assert_eq!((8, Some(8)), iter.size_hint());
        let _ = iter.next();
        assert_eq!((7, Some(7)), iter.size_hint());
    }

    #[test]
    #[cfg(not(feature = "branch_factor_256"))]
    fn backwards() {
        let iter = NibblesIterator::new(&TEST_BYTES).rev();
        let expected = [0xf, 0xe, 0xe, 0xb, 0xd, 0xa, 0xe, 0xd];
        assert!(iter.eq(expected));
    }

    #[test]
    #[cfg(not(feature = "branch_factor_256"))]
    fn nth_back() {
        let mut iter = NibblesIterator::new(&TEST_BYTES);
        assert_eq!(iter.nth_back(0), Some(0xf));
        assert_eq!(iter.nth_back(0), Some(0xe));
        assert_eq!(iter.nth_back(1), Some(0xb));
        assert_eq!(iter.nth_back(2), Some(0xe));
        assert_eq!(iter.nth_back(0), Some(0xd));
        assert_eq!(iter.nth_back(0), None);
    }

    #[test]
    fn empty() {
        let nib = NibblesIterator::new(&[]);
        assert!(nib.is_empty());
        let it = nib.into_iter();
        assert!(it.is_empty());
        assert_eq!(it.size_hint().0, 0);
    }

    #[test]
    #[cfg(not(feature = "branch_factor_256"))]
    fn not_empty_because_of_data() {
        let mut iter = NibblesIterator::new(&[1]);
        assert!(!iter.is_empty());
        assert!(!iter.is_empty());
        assert_eq!(iter.size_hint(), (2, Some(2)));
        assert_eq!(iter.next(), Some(0));
        assert!(!iter.is_empty());
        assert_eq!(iter.size_hint(), (1, Some(1)));
        assert_eq!(iter.next(), Some(1));
        assert!(iter.is_empty());
        assert_eq!(iter.size_hint(), (0, Some(0)));
    }

    #[test_case([0, 0, 2, 3], [2, 3])]
    #[test_case([1, 2, 3, 4], [2, 3, 4])]
    fn encode_decode<T: AsRef<[u8]> + PartialEq + Debug, U: AsRef<[u8]>>(encode: T, expected: U) {
        let from_encoded = Path::from_encoded_iter(encode.as_ref().iter().copied());
        assert_eq!(
            from_encoded.0,
            SmallVec::<[u8; 32]>::from_slice(expected.as_ref())
        );
        let to_encoded = from_encoded.iter_encoded().collect::<SmallVec<[u8; 32]>>();
        assert_eq!(encode.as_ref(), to_encoded.as_ref());
    }

    #[test_case(Path::new(), "[]", "[]")]
    #[test_case(Path::from([0x12, 0x34, 0x56, 0x78]), "12345678", "0x12345678")]
    fn test_fmt_lower_hex(path: Path, expected: &str, expected_with_prefix: &str) {
        assert_eq!(format!("{path:x}"), expected);
        assert_eq!(format!("{path:#x}"), expected_with_prefix);
    }
}
