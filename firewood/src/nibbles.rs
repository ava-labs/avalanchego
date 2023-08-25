use std::ops::Index;

static NIBBLES: [u8; 16] = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15];

/// Nibbles is a newtype that contains only a reference to a [u8], and produces
/// nibbles. Nibbles can be indexed using nib\[x\] or you can get an iterator
/// with iter()
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
/// assert_eq!(nib.iter().collect::<Vec<_>>(), [0x5, 0x6, 0x7, 0x8]);
///
/// // nibbles can be efficiently advanced without rendering the
/// // intermediate values
/// assert_eq!(nib.iter().skip(3).collect::<Vec<_>>(), [0x8]);
///
/// // nibbles can also be indexed
///
/// assert_eq!(nib[1], 0x6);
/// # }
/// ```
#[derive(Debug)]
pub struct Nibbles<'a, const LEADING_ZEROES: usize>(&'a [u8]);

impl<'a, const LEADING_ZEROES: usize> Index<usize> for Nibbles<'a, LEADING_ZEROES> {
    type Output = u8;

    fn index(&self, index: usize) -> &Self::Output {
        match index {
            _ if index < LEADING_ZEROES => &NIBBLES[0],
            _ if (index - LEADING_ZEROES) % 2 == 0 => {
                &NIBBLES[(self.0[(index - LEADING_ZEROES) / 2] >> 4) as usize]
            }
            _ => &NIBBLES[(self.0[(index - LEADING_ZEROES) / 2] & 0xf) as usize],
        }
    }
}

impl<'a, const LEADING_ZEROES: usize> Nibbles<'a, LEADING_ZEROES> {
    #[must_use]
    pub fn iter(&self) -> NibblesIterator<'_, LEADING_ZEROES> {
        NibblesIterator { data: self, pos: 0 }
    }

    #[must_use]
    pub fn len(&self) -> usize {
        LEADING_ZEROES + 2 * self.0.len()
    }

    #[must_use]
    pub fn is_empty(&self) -> bool {
        LEADING_ZEROES == 0 && self.0.is_empty()
    }

    pub fn new(inner: &'a [u8]) -> Self {
        Nibbles(inner)
    }
}

/// An interator returned by [Nibbles::iter]
/// See their documentation for details.
#[derive(Debug)]
pub struct NibblesIterator<'a, const LEADING_ZEROES: usize> {
    data: &'a Nibbles<'a, LEADING_ZEROES>,
    pos: usize,
}

impl<'a, const LEADING_ZEROES: usize> Iterator for NibblesIterator<'a, LEADING_ZEROES> {
    type Item = u8;

    fn next(&mut self) -> Option<Self::Item> {
        let result = if self.pos >= LEADING_ZEROES + self.data.0.len() * 2 {
            None
        } else {
            Some(self.data[self.pos])
        };
        self.pos += 1;
        result
    }

    fn nth(&mut self, n: usize) -> Option<Self::Item> {
        self.pos += n;
        self.next()
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        let remaining = self.data.len() - self.pos;
        (remaining, Some(remaining))
    }
}

#[cfg(test)]
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
    fn leadingzero_nibbles_index() {
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
        expected.into_iter().eq(nib.iter());
    }

    #[test]
    fn skip_skips_zeroes() {
        let nib1 = Nibbles::<1>(&TEST_BYTES);
        let nib0 = Nibbles::<0>(&TEST_BYTES);
        assert!(nib1.iter().skip(1).eq(nib0.iter()));
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
        let mut nib_iter = nib.iter();
        assert_eq!((8, Some(8)), nib_iter.size_hint());
        let _ = nib_iter.next();
        assert_eq!((7, Some(7)), nib_iter.size_hint());
    }

    #[test]
    fn size_hint_1() {
        let nib = Nibbles::<1>(&TEST_BYTES);
        let mut nib_iter = nib.iter();
        assert_eq!((9, Some(9)), nib_iter.size_hint());
        let _ = nib_iter.next();
        assert_eq!((8, Some(8)), nib_iter.size_hint());
    }
}
