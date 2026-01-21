//! Avalanche serialization codec.
//!
//! This crate provides binary serialization compatible with the Go implementation.
//! The codec uses big-endian byte order for all multi-byte integers.
//!
//! # Wire Format
//!
//! - `u8`: 1 byte
//! - `u16`: 2 bytes, big-endian
//! - `u32`: 4 bytes, big-endian
//! - `u64`: 8 bytes, big-endian
//! - `bool`: 1 byte (0x00 = false, 0x01 = true)
//! - `String`: 2-byte length prefix (u16) + UTF-8 bytes
//! - `Vec<T>`: 4-byte length prefix (u32) + elements
//! - Fixed arrays: elements only (no length prefix)

mod packer;

pub use packer::{PackError, Packer, UnpackError, Unpacker};

/// Maximum string length (u16::MAX = 65535).
pub const MAX_STRING_LEN: usize = u16::MAX as usize;

/// Trait for types that can be packed into bytes.
pub trait Pack {
    /// Packs this value into the packer.
    fn pack(&self, packer: &mut Packer);

    /// Returns the packed size of this value in bytes.
    fn packed_size(&self) -> usize;
}

/// Trait for types that can be unpacked from bytes.
pub trait Unpack: Sized {
    /// Unpacks a value from the unpacker.
    ///
    /// # Errors
    ///
    /// Returns an error if there are not enough bytes or the data is invalid.
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError>;
}

// Implement Pack for primitive types
impl Pack for u8 {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_byte(*self);
    }

    fn packed_size(&self) -> usize {
        1
    }
}

impl Pack for u16 {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_short(*self);
    }

    fn packed_size(&self) -> usize {
        2
    }
}

impl Pack for u32 {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_int(*self);
    }

    fn packed_size(&self) -> usize {
        4
    }
}

impl Pack for u64 {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_long(*self);
    }

    fn packed_size(&self) -> usize {
        8
    }
}

impl Pack for bool {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_bool(*self);
    }

    fn packed_size(&self) -> usize {
        1
    }
}

impl Pack for str {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_str(self);
    }

    fn packed_size(&self) -> usize {
        2 + self.len()
    }
}

impl Pack for String {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_str(self);
    }

    fn packed_size(&self) -> usize {
        2 + self.len()
    }
}

impl<T: Pack> Pack for [T] {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_int(self.len() as u32);
        for item in self {
            item.pack(packer);
        }
    }

    fn packed_size(&self) -> usize {
        4 + self.iter().map(Pack::packed_size).sum::<usize>()
    }
}

impl<T: Pack> Pack for Vec<T> {
    fn pack(&self, packer: &mut Packer) {
        self.as_slice().pack(packer);
    }

    fn packed_size(&self) -> usize {
        self.as_slice().packed_size()
    }
}

impl<const N: usize> Pack for [u8; N] {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_fixed_bytes(self);
    }

    fn packed_size(&self) -> usize {
        N
    }
}

// Implement Unpack for primitive types
impl Unpack for u8 {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        unpacker.unpack_byte()
    }
}

impl Unpack for u16 {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        unpacker.unpack_short()
    }
}

impl Unpack for u32 {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        unpacker.unpack_int()
    }
}

impl Unpack for u64 {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        unpacker.unpack_long()
    }
}

impl Unpack for bool {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        unpacker.unpack_bool()
    }
}

impl Unpack for String {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        unpacker.unpack_string()
    }
}

impl<T: Unpack> Unpack for Vec<T> {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let len = unpacker.unpack_int()? as usize;
        let mut vec = Vec::with_capacity(len.min(1024)); // Limit pre-allocation
        for _ in 0..len {
            vec.push(T::unpack(unpacker)?);
        }
        Ok(vec)
    }
}

impl<const N: usize> Unpack for [u8; N] {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        unpacker.unpack_fixed_bytes()
    }
}

// Implement for ID types from avalanche-ids
impl Pack for avalanche_ids::Id {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_fixed_bytes(self.as_bytes());
    }

    fn packed_size(&self) -> usize {
        avalanche_ids::ID_LEN
    }
}

impl Unpack for avalanche_ids::Id {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let bytes: [u8; avalanche_ids::ID_LEN] = unpacker.unpack_fixed_bytes()?;
        Ok(avalanche_ids::Id::from_bytes(bytes))
    }
}

impl Pack for avalanche_ids::ShortId {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_fixed_bytes(self.as_bytes());
    }

    fn packed_size(&self) -> usize {
        avalanche_ids::SHORT_ID_LEN
    }
}

impl Unpack for avalanche_ids::ShortId {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let bytes: [u8; avalanche_ids::SHORT_ID_LEN] = unpacker.unpack_fixed_bytes()?;
        Ok(avalanche_ids::ShortId::from_bytes(bytes))
    }
}

impl Pack for avalanche_ids::NodeId {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_fixed_bytes(self.as_bytes());
    }

    fn packed_size(&self) -> usize {
        avalanche_ids::NODE_ID_LEN
    }
}

impl Unpack for avalanche_ids::NodeId {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let bytes: [u8; avalanche_ids::NODE_ID_LEN] = unpacker.unpack_fixed_bytes()?;
        Ok(avalanche_ids::NodeId::from_bytes(bytes))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_pack_unpack_primitives() {
        let mut packer = Packer::new(64);

        42u8.pack(&mut packer);
        1000u16.pack(&mut packer);
        100_000u32.pack(&mut packer);
        10_000_000_000u64.pack(&mut packer);
        true.pack(&mut packer);
        false.pack(&mut packer);

        let bytes = packer.into_bytes();
        let mut unpacker = Unpacker::new(&bytes);

        assert_eq!(u8::unpack(&mut unpacker).unwrap(), 42);
        assert_eq!(u16::unpack(&mut unpacker).unwrap(), 1000);
        assert_eq!(u32::unpack(&mut unpacker).unwrap(), 100_000);
        assert_eq!(u64::unpack(&mut unpacker).unwrap(), 10_000_000_000);
        assert!(bool::unpack(&mut unpacker).unwrap());
        assert!(!bool::unpack(&mut unpacker).unwrap());
    }

    #[test]
    fn test_pack_unpack_string() {
        let mut packer = Packer::new(64);
        "hello world".pack(&mut packer);

        let bytes = packer.into_bytes();
        let mut unpacker = Unpacker::new(&bytes);

        assert_eq!(String::unpack(&mut unpacker).unwrap(), "hello world");
    }

    #[test]
    fn test_pack_unpack_vec() {
        let mut packer = Packer::new(64);
        vec![1u32, 2, 3, 4, 5].pack(&mut packer);

        let bytes = packer.into_bytes();
        let mut unpacker = Unpacker::new(&bytes);

        assert_eq!(
            Vec::<u32>::unpack(&mut unpacker).unwrap(),
            vec![1, 2, 3, 4, 5]
        );
    }

    #[test]
    fn test_pack_unpack_id() {
        use avalanche_ids::Id;

        let id = Id::from_bytes([42u8; 32]);
        let mut packer = Packer::new(64);
        id.pack(&mut packer);

        let bytes = packer.into_bytes();
        let mut unpacker = Unpacker::new(&bytes);

        assert_eq!(Id::unpack(&mut unpacker).unwrap(), id);
    }
}
