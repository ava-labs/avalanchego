// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{iter::FusedIterator, ops::Index};

static NIBBLES: [u8; 16] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];

/// Nibbles is a newtype that contains only a reference to a [u8], and produces
/// nibbles. Nibbles can be indexed using nib\[x\] or you can get an iterator
/// with `into_iter()`
///
/// Nibbles can be constructed with a number of leading zeroes. This is used
/// in firewood because there is a sentinel node, so we always want the first
/// byte to be 0
///
/// When creating a Nibbles object, use the syntax `Nibbles::<N>(r)` where
/// `N` is the number of leading zero bytes you need and `r` is a reference to
/// a [u8]
///
/// # Examples
///
/// ```
/// # use firewood::nibbles;
/// # fn main() {
/// let nib = nibbles::Nibbles::<0>::new(&[0x56, 0x78]);
/// assert_eq!(nib.into_iter().collect::<Vec<_>>(), [0x5, 0x6, 0x7, 0x8]);
///
/// // nibbles can be efficiently advanced without rendering the
/// // intermediate values
/// assert_eq!(nib.into_iter().skip(3).collect::<Vec<_>>(), [0x8]);
///
/// // nibbles can also be indexed
///
/// assert_eq!(nib[1], 0x6);
///
/// // or reversed
/// assert_eq!(nib.into_iter().rev().next(), Some(0x8));
/// # }
/// ```
#[derive(Debug, Copy, Clone)]
pub struct Nibbles<'a, const LEADING_ZEROES: usize>(&'a [u8]);

impl<'a, const LEADING_ZEROES: usize> Index<usize> for Nibbles<'a, LEADING_ZEROES> {
    type Output = u8;

    fn index(&self, index: usize) -> &Self::Output {
        match index {
            _ if index < LEADING_ZEROES => &NIBBLES[0],
            _ if (index - LEADING_ZEROES) % 2 == 0 =>
            {
                #[allow(clippy::indexing_slicing)]
                #[allow(clippy::indexing_slicing)]
                &NIBBLES[(self.0[(index - LEADING_ZEROES) / 2] >> 4) as usize]
            }
            #[allow(clippy::indexing_slicing)]
            #[allow(clippy::indexing_slicing)]
            _ => &NIBBLES[(self.0[(index - LEADING_ZEROES) / 2] & 0xf) as usize],
        }
    }
}

impl<'a, const LEADING_ZEROES: usize> IntoIterator for Nibbles<'a, LEADING_ZEROES> {
    type Item = u8;
    type IntoIter = NibblesIterator<'a, LEADING_ZEROES>;

    #[must_use]
    fn into_iter(self) -> Self::IntoIter {
        NibblesIterator {
            data: self,
            head: 0,
            tail: self.len(),
        }
    }
}

impl<'a, const LEADING_ZEROES: usize> Nibbles<'a, LEADING_ZEROES> {
    #[must_use]
    pub const fn len(&self) -> usize {
        LEADING_ZEROES + 2 * self.0.len()
    }

    #[must_use]
    pub const fn is_empty(&self) -> bool {
        LEADING_ZEROES == 0 && self.0.is_empty()
    }

    pub const fn new(inner: &'a [u8]) -> Self {
        Nibbles(inner)
    }
}

/// An iterator returned by [Nibbles::into_iter]
/// See their documentation for details.
#[derive(Clone, Debug)]
pub struct NibblesIterator<'a, const LEADING_ZEROES: usize> {
    data: Nibbles<'a, LEADING_ZEROES>,
    head: usize,
    tail: usize,
}

impl<'a, const LEADING_ZEROES: usize> FusedIterator for NibblesIterator<'a, LEADING_ZEROES> {}

impl<'a, const LEADING_ZEROES: usize> Iterator for NibblesIterator<'a, LEADING_ZEROES> {
    type Item = u8;

    fn next(&mut self) -> Option<Self::Item> {
        if self.is_empty() {
            return None;
        }
        #[allow(clippy::indexing_slicing)]
        let result = Some(self.data[self.head]);
        self.head += 1;
        result
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

impl<'a, const LEADING_ZEROES: usize> NibblesIterator<'a, LEADING_ZEROES> {
    #[inline(always)]
    pub const fn is_empty(&self) -> bool {
        self.head == self.tail
    }
}

impl<'a, const LEADING_ZEROES: usize> DoubleEndedIterator for NibblesIterator<'a, LEADING_ZEROES> {
    fn next_back(&mut self) -> Option<Self::Item> {
        if self.is_empty() {
            return None;
        }
        self.tail -= 1;
        #[allow(clippy::indexing_slicing)]
        Some(self.data[self.tail])
    }

    fn nth_back(&mut self, n: usize) -> Option<Self::Item> {
        self.tail -= std::cmp::min(n, self.tail - self.head);
        self.next_back()
    }
}

#[cfg(test)]
#[allow(clippy::indexing_slicing)]
mod test {
    use super::Nibbles;
    static TEST_BYTES: [u8; 4] = [0xdeu8, 0xad, 0xbe, 0xef];

    #[test]
    fn happy_regular_nibbles() {
        let nib = Nibbles::<0>(&TEST_BYTES);
        let expected = [0xdu8, 0xe, 0xa, 0xd, 0xb, 0xe, 0xe, 0xf];
        for v in expected.into_iter().enumerate() {
            assert_eq!(nib[v.0], v.1, "{v:?}");
        }
    }

    #[test]
    fn leading_zero_nibbles_index() {
        let nib = Nibbles::<1>(&TEST_BYTES);
        let expected = [0u8, 0xd, 0xe, 0xa, 0xd, 0xb, 0xe, 0xe, 0xf];
        for v in expected.into_iter().enumerate() {
            assert_eq!(nib[v.0], v.1, "{v:?}");
        }
    }
    #[test]
    fn leading_zero_nibbles_iter() {
        let nib = Nibbles::<1>(&TEST_BYTES);
        let expected: [u8; 9] = [0u8, 0xd, 0xe, 0xa, 0xd, 0xb, 0xe, 0xe, 0xf];
        expected.into_iter().eq(nib);
    }

    #[test]
    fn skip_skips_zeroes() {
        let nib1 = Nibbles::<1>(&TEST_BYTES);
        let nib0 = Nibbles::<0>(&TEST_BYTES);
        assert!(nib1.into_iter().skip(1).eq(nib0.into_iter()));
    }

    #[test]
    #[should_panic]
    fn out_of_bounds_panics() {
        let nib = Nibbles::<0>(&TEST_BYTES);
        let _ = nib[8];
    }

    #[test]
    fn last_nibble() {
        let nib = Nibbles::<0>(&TEST_BYTES);
        assert_eq!(nib[7], 0xf);
    }

    #[test]
    fn size_hint_0() {
        let nib = Nibbles::<0>(&TEST_BYTES);
        let mut nib_iter = nib.into_iter();
        assert_eq!((8, Some(8)), nib_iter.size_hint());
        let _ = nib_iter.next();
        assert_eq!((7, Some(7)), nib_iter.size_hint());
    }

    #[test]
    fn size_hint_1() {
        let nib = Nibbles::<1>(&TEST_BYTES);
        let mut nib_iter = nib.into_iter();
        assert_eq!((9, Some(9)), nib_iter.size_hint());
        let _ = nib_iter.next();
        assert_eq!((8, Some(8)), nib_iter.size_hint());
    }

    #[test]
    fn backwards() {
        let nib = Nibbles::<1>(&TEST_BYTES);
        let nib_iter = nib.into_iter().rev();
        let expected = [0xf, 0xe, 0xe, 0xb, 0xd, 0xa, 0xe, 0xd, 0x0];

        assert!(nib_iter.eq(expected));
    }

    #[test]
    fn empty() {
        let nib = Nibbles::<0>(&[]);
        assert!(nib.is_empty());
        let it = nib.into_iter();
        assert!(it.is_empty());
        assert_eq!(it.size_hint().0, 0);
    }

    #[test]
    fn not_empty_because_of_leading_nibble() {
        let nib = Nibbles::<1>(&[]);
        assert!(!nib.is_empty());
        let mut it = nib.into_iter();
        assert!(!it.is_empty());
        assert_eq!(it.size_hint(), (1, Some(1)));
        assert_eq!(it.next(), Some(0));
        assert!(it.is_empty());
        assert_eq!(it.size_hint(), (0, Some(0)));
    }
    #[test]
    fn not_empty_because_of_data() {
        let nib = Nibbles::<0>(&[1]);
        assert!(!nib.is_empty());
        let mut it = nib.into_iter();
        assert!(!it.is_empty());
        assert_eq!(it.size_hint(), (2, Some(2)));
        assert_eq!(it.next(), Some(0));
        assert!(!it.is_empty());
        assert_eq!(it.size_hint(), (1, Some(1)));
        assert_eq!(it.next(), Some(1));
        assert!(it.is_empty());
        assert_eq!(it.size_hint(), (0, Some(0)));
    }
}
