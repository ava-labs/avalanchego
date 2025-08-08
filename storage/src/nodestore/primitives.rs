// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Primitives Module
//!
//! This module contains the primitives for the nodestore, including a list of valid
//! area sizes, `AreaIndex` that uniquely identifies a valid area size, and
//! `LinearAddress` that points to a specific location in the linear storage space.

use crate::TrieHash;

use sha2::{Digest, Sha256};
use std::fmt;
use std::io::{Error, ErrorKind};
use std::num::NonZeroU64;

/// [`super::NodeStore`] divides the linear store into blocks of different sizes.
/// [`AREA_SIZES`] is every valid block size.
const AREA_SIZES: [u64; 23] = [
    16, // Min block size
    32,
    64,
    96,
    128,
    256,
    512,
    768,
    1024,
    1024 << 1,
    1024 << 2,
    1024 << 3,
    1024 << 4,
    1024 << 5,
    1024 << 6,
    1024 << 7,
    1024 << 8,
    1024 << 9,
    1024 << 10,
    1024 << 11,
    1024 << 12,
    1024 << 13,
    1024 << 14,
];

/// Returns an iterator over all valid area sizes.
// TODO: return a named iterator
pub fn area_size_iter() -> impl DoubleEndedIterator<Item = (AreaIndex, u64)> {
    AREA_SIZES
        .iter()
        .enumerate()
        .map(|(i, &size)| (AreaIndex(i as u8), size))
}

pub fn area_size_hash() -> TrieHash {
    let mut hasher = Sha256::new();
    for size in AREA_SIZES {
        hasher.update(size.to_ne_bytes());
    }
    hasher.finalize().into()
}

// TODO: automate this, must stay in sync with above
pub const fn index_name(index: AreaIndex) -> &'static str {
    match index.get() {
        0 => "16",
        1 => "32",
        2 => "64",
        3 => "96",
        4 => "128",
        5 => "256",
        6 => "512",
        7 => "768",
        8 => "1024",
        9 => "2048",
        10 => "4096",
        11 => "8192",
        12 => "16384",
        13 => "32768",
        14 => "65536",
        15 => "131072",
        16 => "262144",
        17 => "524288",
        18 => "1048576",
        19 => "2097152",
        20 => "4194304",
        21 => "8388608",
        22 => "16777216",
        _ => "unknown",
    }
}

/// The type that uniquely identifies a valid area size.
/// This is not usize because we store this as a single byte
#[derive(Copy, Clone, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
#[repr(transparent)]
pub struct AreaIndex(u8);

impl AreaIndex {
    /// The number of different area sizes available.
    pub const NUM_AREA_SIZES: usize = AREA_SIZES.len();

    /// The minimum area index (0).
    pub const MIN: AreaIndex = AreaIndex(0);

    /// The maximum area index (22).
    pub const MAX: AreaIndex = AreaIndex(Self::NUM_AREA_SIZES as u8 - 1);

    /// The minimum area size available for allocation.
    pub const MIN_AREA_SIZE: u64 = AREA_SIZES[0];

    /// The maximum area size available for allocation.
    pub const MAX_AREA_SIZE: u64 = AREA_SIZES[Self::NUM_AREA_SIZES - 1];

    /// Create a new `AreaIndex` from a u8 value, returns None if value is out of range.
    #[inline]
    #[must_use]
    pub const fn new(index: u8) -> Option<Self> {
        if index < Self::NUM_AREA_SIZES as u8 {
            Some(AreaIndex(index))
        } else {
            None
        }
    }

    /// Create an `AreaIndex` from a size in bytes.
    /// Returns the index of the smallest area size >= `n`.
    ///
    /// # Errors
    ///
    /// Returns an error if the size is too large.
    pub fn from_size(n: u64) -> Result<Self, Error> {
        if n > Self::MAX_AREA_SIZE {
            return Err(Error::new(
                ErrorKind::InvalidData,
                format!("Node size {n} is too large"),
            ));
        }

        if n <= Self::MIN_AREA_SIZE {
            return Ok(AreaIndex(0));
        }

        AREA_SIZES
            .iter()
            .position(|&size| size >= n)
            .map(|index| AreaIndex(index as u8))
            .ok_or_else(|| {
                Error::new(
                    ErrorKind::InvalidData,
                    format!("Node size {n} is too large"),
                )
            })
    }

    /// Get the underlying index as u8.
    #[inline]
    #[must_use]
    pub const fn get(self) -> u8 {
        self.0
    }

    /// Get the underlying index as usize.
    #[inline]
    #[must_use]
    pub const fn as_usize(self) -> usize {
        self.0 as usize
    }

    /// Returns the number of different area sizes available.
    #[inline]
    #[must_use]
    pub const fn num_area_sizes() -> usize {
        Self::NUM_AREA_SIZES
    }

    /// Create an `AreaIndex` from a u8 value without bounds checking.
    #[inline]
    #[must_use]
    #[cfg(test)]
    pub const fn from_u8_unchecked(index: u8) -> Self {
        AreaIndex(index)
    }

    /// Get the size of an area index (used by the checker)
    ///
    /// # Panics
    ///
    /// Panics if `index` is out of bounds for the `AREA_SIZES` array.
    #[must_use]
    pub const fn size(self) -> u64 {
        #[expect(clippy::indexing_slicing)]
        AREA_SIZES[self.as_usize()]
    }
}

impl TryFrom<u8> for AreaIndex {
    type Error = Error;

    fn try_from(index: u8) -> Result<Self, Self::Error> {
        AreaIndex::new(index).ok_or_else(|| {
            Error::new(
                ErrorKind::InvalidData,
                format!("Area index out of bounds: {index}"),
            )
        })
    }
}

impl From<AreaIndex> for u8 {
    fn from(area_index: AreaIndex) -> Self {
        area_index.get()
    }
}

impl TryFrom<usize> for AreaIndex {
    type Error = Error;

    fn try_from(index: usize) -> Result<Self, Self::Error> {
        let index_u8: Result<u8, _> = index.try_into();
        index_u8.map(AreaIndex).map_err(|_| {
            Error::new(
                ErrorKind::InvalidData,
                format!("Area index out of bounds: {index}"),
            )
        })
    }
}

impl From<AreaIndex> for usize {
    fn from(area_index: AreaIndex) -> Self {
        area_index.as_usize()
    }
}

impl fmt::Display for AreaIndex {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        std::fmt::Display::fmt(&self.get(), f)
    }
}

/// A linear address in the nodestore storage.
///
/// This represents a non-zero address in the linear storage space.
#[derive(Copy, Clone, Debug, PartialEq, Eq, PartialOrd, Ord, Hash)]
#[repr(transparent)]
pub struct LinearAddress(NonZeroU64);

#[expect(unsafe_code)]
// SAFETY: `LinearAddress` is a wrapper around `NonZeroU64` which is also `ZeroableInOption`.
unsafe impl bytemuck::ZeroableInOption for LinearAddress {}
#[expect(unsafe_code)]
// SAFETY: `LinearAddress` is a wrapper around `NonZeroU64` which is also `PodInOption`.
unsafe impl bytemuck::PodInOption for LinearAddress {}

impl LinearAddress {
    /// Create a new `LinearAddress`, returns None if value is zero.
    #[inline]
    #[must_use]
    pub const fn new(addr: u64) -> Option<Self> {
        match NonZeroU64::new(addr) {
            Some(addr) => Some(LinearAddress(addr)),
            None => None,
        }
    }

    /// Get the underlying address as u64.
    #[inline]
    #[must_use]
    pub const fn get(self) -> u64 {
        self.0.get()
    }

    /// Check if the address is 8-byte aligned.
    #[inline]
    #[must_use]
    pub const fn is_aligned(self) -> bool {
        self.0.get() % (Self::MIN_AREA_SIZE) == 0
    }

    /// The maximum area size available for allocation.
    pub const MAX_AREA_SIZE: u64 = *AREA_SIZES.last().unwrap();

    /// The minimum area size available for allocation.
    pub const MIN_AREA_SIZE: u64 = *AREA_SIZES.first().unwrap();

    /// Returns the number of different area sizes available.
    #[inline]
    #[must_use]
    pub const fn num_area_sizes() -> usize {
        const { AREA_SIZES.len() }
    }

    /// Returns the inner `NonZeroU64`
    #[inline]
    #[must_use]
    pub const fn into_nonzero(self) -> NonZeroU64 {
        self.0
    }

    /// Advances a `LinearAddress` by `n` bytes.
    ///
    /// Returns `None` if the result overflows a u64
    /// Some(LinearAddress) otherwise
    ///
    #[inline]
    #[must_use]
    pub const fn advance(self, n: u64) -> Option<Self> {
        match self.0.checked_add(n) {
            // overflowed
            None => None,

            // It is impossible to add a non-zero positive number to a u64 and get 0 without
            // overflowing, so we don't check for that here, and panic instead.
            Some(sum) => Some(LinearAddress(sum)),
        }
    }

    /// Returns the number of bytes between `other` and `self` if `other` is less than or equal to `self`.
    /// Otherwise, returns `None`.
    #[inline]
    #[must_use]
    pub const fn distance_from(self, other: Self) -> Option<u64> {
        self.0.get().checked_sub(other.0.get())
    }
}

impl std::ops::Deref for LinearAddress {
    type Target = NonZeroU64;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl fmt::Display for LinearAddress {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        std::fmt::Display::fmt(&self.get(), f)
    }
}

impl fmt::LowerHex for LinearAddress {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        std::fmt::LowerHex::fmt(&self.get(), f)
    }
}
impl From<LinearAddress> for u64 {
    fn from(addr: LinearAddress) -> Self {
        addr.get()
    }
}

impl From<NonZeroU64> for LinearAddress {
    fn from(addr: NonZeroU64) -> Self {
        LinearAddress(addr)
    }
}
