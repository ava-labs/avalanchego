// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

/// An error similar to [`std::num::TryFromIntError`] but able to be created
/// within our crate.
///
/// The std error does not have a public constructor, and neither does ours.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, thiserror::Error)]
#[non_exhaustive]
#[error("out of range integral type conversion attempted")]
pub struct TryFromIntError;

/// A 4-bit unsigned integer representing a hexary digit (0-15, inclusive) used
/// as a path component in hexary tries.
///
/// The internal representation is a `u8` where only the lower 4 bits are used.
///
/// Niche optimizations are enabled through the inner representation to enable
/// memory efficiency when used in data structures like [`Option`] as well as
/// allowing for faster indexing into [`Children`](crate::node::Children) arrays
/// by enabling the compiler to optimize away bounds checks.
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct U4(Repr);

impl U4 {
    /// The number of bits required to represent a single u4.
    pub const BITS: u32 = 4;

    /// The minimum value of a [`U4`], representing zero and the leftmost child
    /// in a node's children array.
    pub const MIN: Self = Self(Repr::Ox0);

    /// The maximum value of a [`U4`], representing fifteen and the rightmost child
    /// in a node's children array.
    pub const MAX: Self = Self(Repr::OxF);

    /// Fallibly converts a `u8` to a [`U4`], returning [`None`] if the
    /// value is out of range.
    #[inline]
    #[must_use]
    pub const fn try_new(v: u8) -> Option<Self> {
        // FIXME(rust-lang/rust#143874): Option::map is not yet const
        match Repr::try_new(v) {
            Some(repr) => Some(Self(repr)),
            None => None,
        }
    }

    /// Creates a new [`U4`] without checking that the provided value
    /// is valid.
    ///
    /// # Safety
    ///
    /// The caller must ensure that the value is between 0 and 15 (inclusive).
    /// Providing a value outside of this range results in immediate undefined
    /// behavior.
    #[inline]
    #[must_use]
    pub const unsafe fn new_unchecked(v: u8) -> Self {
        #![expect(unsafe_code)]

        debug_assert!(v <= 0xF);
        // SAFETY: the caller must ensure that `v` is a valid value (0 to 15).
        unsafe { Self(Repr::new_unchecked(v)) }
    }

    /// Creates a new [`U4`] using only the lower 4 bits of the input value and
    /// ignoring the upper 4 bits.
    #[inline]
    #[must_use]
    pub const fn new_masked(v: u8) -> Self {
        #[expect(unsafe_code)]
        // SAFETY: the value is masked to be between 0 and 15.
        unsafe {
            Self::new_unchecked(v & 0xF)
        }
    }

    /// Creates a new [`U4`] using only the upper 4 bits of the input value and
    /// ignoring the lower 4 bits.
    #[inline]
    #[must_use]
    pub const fn new_shifted(v: u8) -> Self {
        #[expect(unsafe_code)]
        // SAFETY: the value is shifted to be between 0 and 15. The extra mask is
        // redundant but added for extra clarity.
        unsafe {
            Self::new_unchecked((v >> 4) & 0xF)
        }
    }

    /// Creates a pair of [`U4`]s from a single `u8`, where the first element
    /// is created from the upper 4 bits and the second element is created from
    /// the lower 4 bits.
    #[inline]
    #[must_use]
    pub const fn new_pair(v: u8) -> (Self, Self) {
        (Self::new_shifted(v), Self::new_masked(v))
    }

    /// Casts the [`U4`] to a `u8`.
    #[inline]
    #[must_use]
    pub const fn as_u8(self) -> u8 {
        self.0 as u8
    }

    /// Casts the [`U4`] to a `usize`.
    #[inline]
    #[must_use]
    pub const fn as_usize(self) -> usize {
        self.0 as usize
    }

    /// Joins this [`U4`] with another [`U4`] to create a single `u8` where
    /// this component is the upper 4 bits and the provided component is the
    /// lower 4 bits.
    #[inline]
    #[must_use]
    pub const fn join(self, lower: U4) -> u8 {
        (self.as_u8() << 4) | lower.as_u8()
    }
}

impl TryFrom<u8> for U4 {
    type Error = TryFromIntError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        Self::try_new(value).ok_or(TryFromIntError)
    }
}

/// The internal representation of a [`U4`].
///
/// This enum explicitly represents each of the 16 possible values (0b0000 to 0b1111)
/// of a 4-bit unsigned integer. It is used to optimize memory usage and performance
/// in scenarios where many instances of `U4` are stored, such as in a hexary trie,
///
/// For example, when using [`Option<U4>`], the resulting size is still 1 byte
/// due to Rust's niche optimization, where [`Option::None`] can be represented by
/// any of the unused bit patterns of the inner [`Repr`] enum.
///
/// Additionally, when using [`U4`] as an index into a fixed-size array of 16 elements,
/// like the [`Children`](crate::Children) array in the hexary trie, the compiler can
/// optimize away bounds checks and remove possible panic points. This is because the
/// compiler knows that the value of [`U4`] can only be one of the 16 valid indices.
#[derive(Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
enum Repr {
    Ox0,
    Ox1,
    Ox2,
    Ox3,
    Ox4,
    Ox5,
    Ox6,
    Ox7,
    Ox8,
    Ox9,
    OxA,
    OxB,
    OxC,
    OxD,
    OxE,
    OxF,
}

impl Repr {
    #[inline]
    #[must_use]
    const unsafe fn new_unchecked(v: u8) -> Self {
        #![expect(unsafe_code)]
        // SAFETY: the caller must ensure that `v` is a valid value (0 to 15).
        unsafe { Self::try_new(v).unwrap_unchecked() }
    }

    #[inline]
    #[must_use]
    const fn try_new(v: u8) -> Option<Self> {
        // this could have been a transmute but then dead code detection would
        // annoyingly complain about each variant going unused. the final code
        // is the same regardless: https://rust.godbolt.org/z/6v6sddf6d
        match v {
            0x0 => Some(Self::Ox0),
            0x1 => Some(Self::Ox1),
            0x2 => Some(Self::Ox2),
            0x3 => Some(Self::Ox3),
            0x4 => Some(Self::Ox4),
            0x5 => Some(Self::Ox5),
            0x6 => Some(Self::Ox6),
            0x7 => Some(Self::Ox7),
            0x8 => Some(Self::Ox8),
            0x9 => Some(Self::Ox9),
            0xA => Some(Self::OxA),
            0xB => Some(Self::OxB),
            0xC => Some(Self::OxC),
            0xD => Some(Self::OxD),
            0xE => Some(Self::OxE),
            0xF => Some(Self::OxF),
            _ => None,
        }
    }
}

impl std::fmt::Debug for U4 {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Debug::fmt(&self.as_u8(), f)
    }
}

impl std::fmt::Display for U4 {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Display::fmt(&self.as_u8(), f)
    }
}

impl std::fmt::LowerHex for U4 {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::LowerHex::fmt(&self.as_u8(), f)
    }
}

impl std::fmt::UpperHex for U4 {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::UpperHex::fmt(&self.as_u8(), f)
    }
}

impl std::fmt::Binary for U4 {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        std::fmt::Binary::fmt(&self.as_u8(), f)
    }
}

#[cfg(test)]
mod tests {
    use test_case::test_case;

    use super::*;

    #[test_case(0x00, Some(U4(Repr::Ox0)); "0x00 -> 0")]
    #[test_case(0x01, Some(U4(Repr::Ox1)); "0x01 -> 1")]
    #[test_case(0x02, Some(U4(Repr::Ox2)); "0x02 -> 2")]
    #[test_case(0x03, Some(U4(Repr::Ox3)); "0x03 -> 3")]
    #[test_case(0x04, Some(U4(Repr::Ox4)); "0x04 -> 4")]
    #[test_case(0x05, Some(U4(Repr::Ox5)); "0x05 -> 5")]
    #[test_case(0x06, Some(U4(Repr::Ox6)); "0x06 -> 6")]
    #[test_case(0x07, Some(U4(Repr::Ox7)); "0x07 -> 7")]
    #[test_case(0x08, Some(U4(Repr::Ox8)); "0x08 -> 8")]
    #[test_case(0x09, Some(U4(Repr::Ox9)); "0x09 -> 9")]
    #[test_case(0x0A, Some(U4(Repr::OxA)); "0x0A -> 10")]
    #[test_case(0x0B, Some(U4(Repr::OxB)); "0x0B -> 11")]
    #[test_case(0x0C, Some(U4(Repr::OxC)); "0x0C -> 12")]
    #[test_case(0x0D, Some(U4(Repr::OxD)); "0x0D -> 13")]
    #[test_case(0x0E, Some(U4(Repr::OxE)); "0x0E -> 14")]
    #[test_case(0x0F, Some(U4(Repr::OxF)); "0x0F -> 15")]
    #[test_case(0x10, None; "0x10 -> None")]
    #[test_case(0xFF, None; "0xFF -> None")]
    fn test_try_new(input: u8, expected: Option<U4>) {
        assert_eq!(U4::try_new(input), expected);
    }

    #[test_case(0x00, U4(Repr::Ox0); "0x00 -> 0")]
    #[test_case(0x01, U4(Repr::Ox1); "0x01 -> 1")]
    #[test_case(0x02, U4(Repr::Ox2); "0x02 -> 2")]
    #[test_case(0x03, U4(Repr::Ox3); "0x03 -> 3")]
    #[test_case(0x04, U4(Repr::Ox4); "0x04 -> 4")]
    #[test_case(0x05, U4(Repr::Ox5); "0x05 -> 5")]
    #[test_case(0x06, U4(Repr::Ox6); "0x06 -> 6")]
    #[test_case(0x07, U4(Repr::Ox7); "0x07 -> 7")]
    #[test_case(0x08, U4(Repr::Ox8); "0x08 -> 8")]
    #[test_case(0x09, U4(Repr::Ox9); "0x09 -> 9")]
    #[test_case(0x0A, U4(Repr::OxA); "0x0A -> 10")]
    #[test_case(0x0B, U4(Repr::OxB); "0x0B -> 11")]
    #[test_case(0x0C, U4(Repr::OxC); "0x0C -> 12")]
    #[test_case(0x0D, U4(Repr::OxD); "0x0D -> 13")]
    #[test_case(0x0E, U4(Repr::OxE); "0x0E -> 14")]
    #[test_case(0x0F, U4(Repr::OxF); "0x0F -> 15")]
    #[test_case(0x10, U4(Repr::Ox0); "0x10 -> 0")]
    #[test_case(0x20, U4(Repr::Ox0); "0x20 -> 0")]
    #[test_case(0x30, U4(Repr::Ox0); "0x30 -> 0")]
    #[test_case(0x40, U4(Repr::Ox0); "0x40 -> 0")]
    #[test_case(0x50, U4(Repr::Ox0); "0x50 -> 0")]
    #[test_case(0x60, U4(Repr::Ox0); "0x60 -> 0")]
    #[test_case(0x70, U4(Repr::Ox0); "0x70 -> 0")]
    #[test_case(0x80, U4(Repr::Ox0); "0x80 -> 0")]
    #[test_case(0x90, U4(Repr::Ox0); "0x90 -> 0")]
    #[test_case(0xA0, U4(Repr::Ox0); "0xA0 -> 0")]
    #[test_case(0xB0, U4(Repr::Ox0); "0xB0 -> 0")]
    #[test_case(0xC0, U4(Repr::Ox0); "0xC0 -> 0")]
    #[test_case(0xD0, U4(Repr::Ox0); "0xD0 -> 0")]
    #[test_case(0xE0, U4(Repr::Ox0); "0xE0 -> 0")]
    #[test_case(0xF0, U4(Repr::Ox0); "0xF0 -> 0")]
    #[test_case(0xFF, U4(Repr::OxF); "0xFF -> 15")]
    fn test_new_masked(input: u8, expected: U4) {
        assert_eq!(U4::new_masked(input), expected);
    }

    #[test_case(0x00, U4(Repr::Ox0); "0x00 -> 0")]
    #[test_case(0x01, U4(Repr::Ox0); "0x01 -> 0")]
    #[test_case(0x02, U4(Repr::Ox0); "0x02 -> 0")]
    #[test_case(0x03, U4(Repr::Ox0); "0x03 -> 0")]
    #[test_case(0x04, U4(Repr::Ox0); "0x04 -> 0")]
    #[test_case(0x05, U4(Repr::Ox0); "0x05 -> 0")]
    #[test_case(0x06, U4(Repr::Ox0); "0x06 -> 0")]
    #[test_case(0x07, U4(Repr::Ox0); "0x07 -> 0")]
    #[test_case(0x08, U4(Repr::Ox0); "0x08 -> 0")]
    #[test_case(0x09, U4(Repr::Ox0); "0x09 -> 0")]
    #[test_case(0x0A, U4(Repr::Ox0); "0x0A -> 0")]
    #[test_case(0x0B, U4(Repr::Ox0); "0x0B -> 0")]
    #[test_case(0x0C, U4(Repr::Ox0); "0x0C -> 0")]
    #[test_case(0x0D, U4(Repr::Ox0); "0x0D -> 0")]
    #[test_case(0x0E, U4(Repr::Ox0); "0x0E -> 0")]
    #[test_case(0x0F, U4(Repr::Ox0); "0x0F -> 0")]
    #[test_case(0x10, U4(Repr::Ox1); "0x10 -> 1")]
    #[test_case(0x20, U4(Repr::Ox2); "0x20 -> 2")]
    #[test_case(0x30, U4(Repr::Ox3); "0x30 -> 3")]
    #[test_case(0x40, U4(Repr::Ox4); "0x40 -> 4")]
    #[test_case(0x50, U4(Repr::Ox5); "0x50 -> 5")]
    #[test_case(0x60, U4(Repr::Ox6); "0x60 -> 6")]
    #[test_case(0x70, U4(Repr::Ox7); "0x70 -> 7")]
    #[test_case(0x80, U4(Repr::Ox8); "0x80 -> 8")]
    #[test_case(0x90, U4(Repr::Ox9); "0x90 -> 9")]
    #[test_case(0xA0, U4(Repr::OxA); "0xA0 -> 10")]
    #[test_case(0xB0, U4(Repr::OxB); "0xB0 -> 11")]
    #[test_case(0xC0, U4(Repr::OxC); "0xC0 -> 12")]
    #[test_case(0xD0, U4(Repr::OxD); "0xD0 -> 13")]
    #[test_case(0xE0, U4(Repr::OxE); "0xE0 -> 14")]
    #[test_case(0xF0, U4(Repr::OxF); "0xF0 -> 15")]
    #[test_case(0xFF, U4(Repr::OxF); "0xFF -> 15")]
    fn test_new_shifted(input: u8, expected: U4) {
        assert_eq!(U4::new_shifted(input), expected);
    }

    #[test_case(0x00, (U4(Repr::Ox0), U4(Repr::Ox0)); "0x00 -> (0, 0)")]
    #[test_case(0x0F, (U4(Repr::Ox0), U4(Repr::OxF)); "0x0F -> (0, 15)")]
    #[test_case(0xF0, (U4(Repr::OxF), U4(Repr::Ox0)); "0xF0 -> (15, 0)")]
    #[test_case(0xFF, (U4(Repr::OxF), U4(Repr::OxF)); "0xFF -> (15, 15)")]
    fn test_new_pair_then_join(input: u8, expected: (U4, U4)) {
        let (upper, lower) = U4::new_pair(input);
        assert_eq!((upper, lower), expected);
        assert_eq!(upper.join(lower), input);
    }
}
