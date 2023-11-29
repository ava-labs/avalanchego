// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::hash::Hash;
use std::mem::size_of;
use std::num::NonZeroUsize;
use std::ops::{Deref, DerefMut};

use bytemuck::NoUninit;

use crate::shale::{CachedStore, ShaleError, Storable};

/// The virtual disk address of an object
#[repr(C)]
#[derive(Debug, Copy, Clone, Eq, Hash, Ord, PartialOrd, PartialEq, NoUninit)]
pub struct DiskAddress(pub Option<NonZeroUsize>);

impl Deref for DiskAddress {
    type Target = Option<NonZeroUsize>;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl DerefMut for DiskAddress {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

impl DiskAddress {
    pub(crate) const MSIZE: u64 = size_of::<Self>() as u64;

    /// Return a None DiskAddress
    pub fn null() -> Self {
        DiskAddress(None)
    }

    /// Indicate whether the DiskAddress is null
    pub fn is_null(&self) -> bool {
        self.is_none()
    }

    /// Convert a NonZeroUsize to a DiskAddress
    pub fn new(addr: NonZeroUsize) -> Self {
        DiskAddress(Some(addr))
    }

    /// Get the little endian bytes for a DiskAddress for storage
    pub fn to_le_bytes(&self) -> [u8; 8] {
        self.0.map(|v| v.get()).unwrap_or_default().to_le_bytes()
    }

    /// Get the inner usize, using 0 if None
    pub fn get(&self) -> usize {
        self.0.map(|v| v.get()).unwrap_or_default()
    }
}

/// Convert from a usize to a DiskAddress
impl From<usize> for DiskAddress {
    fn from(value: usize) -> Self {
        DiskAddress(NonZeroUsize::new(value))
    }
}

/// Convert from a serialized le_bytes to a DiskAddress
impl From<[u8; 8]> for DiskAddress {
    fn from(value: [u8; 8]) -> Self {
        Self::from(usize::from_le_bytes(value))
    }
}

/// Convert from a slice of bytes to a DiskAddress
/// panics if the slice isn't 8 bytes; used for
/// serialization from disk
impl From<&[u8]> for DiskAddress {
    fn from(value: &[u8]) -> Self {
        let bytes: [u8; 8] = value.try_into().unwrap();
        bytes.into()
    }
}

/// Convert a DiskAddress into a usize
/// TODO: panic if the DiskAddress is None
impl From<DiskAddress> for usize {
    fn from(value: DiskAddress) -> usize {
        value.get()
    }
}

/// Add two disk addresses;
/// TODO: panic if either are null
impl std::ops::Add<DiskAddress> for DiskAddress {
    type Output = DiskAddress;

    fn add(self, rhs: DiskAddress) -> Self::Output {
        self + rhs.get()
    }
}

/// Add a usize to a DiskAddress
/// TODO: panic if the DiskAddress is null
impl std::ops::Add<usize> for DiskAddress {
    type Output = DiskAddress;

    fn add(self, rhs: usize) -> Self::Output {
        (self.get() + rhs).into()
    }
}

/// subtract one disk address from another
/// TODO: panic if either are null
impl std::ops::Sub<DiskAddress> for DiskAddress {
    type Output = DiskAddress;

    fn sub(self, rhs: DiskAddress) -> Self::Output {
        self - rhs.get()
    }
}

/// subtract a usize from a diskaddress
/// panic if the DiskAddress is null
impl std::ops::Sub<usize> for DiskAddress {
    type Output = DiskAddress;

    fn sub(self, rhs: usize) -> Self::Output {
        (self.get() - rhs).into()
    }
}

impl std::ops::AddAssign<DiskAddress> for DiskAddress {
    fn add_assign(&mut self, rhs: DiskAddress) {
        *self = *self + rhs;
    }
}

impl std::ops::AddAssign<usize> for DiskAddress {
    fn add_assign(&mut self, rhs: usize) {
        *self = *self + rhs;
    }
}

impl std::ops::SubAssign<DiskAddress> for DiskAddress {
    fn sub_assign(&mut self, rhs: DiskAddress) {
        *self = *self - rhs;
    }
}

impl std::ops::SubAssign<usize> for DiskAddress {
    fn sub_assign(&mut self, rhs: usize) {
        *self = *self - rhs;
    }
}

impl std::ops::BitAnd<usize> for DiskAddress {
    type Output = DiskAddress;

    fn bitand(self, rhs: usize) -> Self::Output {
        (self.get() & rhs).into()
    }
}

impl Storable for DiskAddress {
    fn serialized_len(&self) -> u64 {
        Self::MSIZE
    }

    fn serialize(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        use std::io::{Cursor, Write};
        Cursor::new(to).write_all(&self.0.unwrap().get().to_le_bytes())?;
        Ok(())
    }

    fn deserialize<U: CachedStore>(addr: usize, mem: &U) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        let addrdyn = raw.deref();
        let addrvec = addrdyn.as_deref();
        Ok(Self(NonZeroUsize::new(usize::from_le_bytes(
            addrvec.try_into().unwrap(),
        ))))
    }
}
