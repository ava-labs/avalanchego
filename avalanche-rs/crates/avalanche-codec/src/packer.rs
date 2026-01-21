//! Binary packing and unpacking utilities.
//!
//! This module provides low-level binary serialization that is compatible
//! with the Go implementation's wire format.

use thiserror::Error;

use crate::MAX_STRING_LEN;

/// Errors that can occur during packing.
#[derive(Debug, Error)]
pub enum PackError {
    /// The string exceeds the maximum allowed length.
    #[error("string too long: {len} bytes exceeds max {max}")]
    StringTooLong { len: usize, max: usize },

    /// The packer would exceed its maximum size.
    #[error("packer overflow: need {needed} bytes but max is {max}")]
    Overflow { needed: usize, max: usize },
}

/// Errors that can occur during unpacking.
#[derive(Debug, Error)]
pub enum UnpackError {
    /// Not enough bytes remaining to unpack the requested type.
    #[error("insufficient bytes: need {needed} but only {remaining} remaining")]
    InsufficientBytes { needed: usize, remaining: usize },

    /// Invalid boolean value (not 0 or 1).
    #[error("invalid boolean value: {0}")]
    InvalidBool(u8),

    /// Invalid UTF-8 string.
    #[error("invalid UTF-8: {0}")]
    InvalidUtf8(#[from] std::string::FromUtf8Error),
}

/// A packer for serializing data to bytes.
///
/// All multi-byte integers are written in big-endian order.
///
/// # Examples
///
/// ```
/// use avalanche_codec::Packer;
///
/// let mut packer = Packer::new(1024);
/// packer.pack_int(42);
/// packer.pack_str("hello");
///
/// let bytes = packer.into_bytes();
/// ```
#[derive(Debug)]
pub struct Packer {
    bytes: Vec<u8>,
    max_size: usize,
    error: Option<PackError>,
}

impl Packer {
    /// Creates a new packer with the given initial capacity.
    #[must_use]
    pub fn new(capacity: usize) -> Self {
        Self {
            bytes: Vec::with_capacity(capacity),
            max_size: usize::MAX,
            error: None,
        }
    }

    /// Creates a new packer with a maximum size limit.
    #[must_use]
    pub fn with_max_size(capacity: usize, max_size: usize) -> Self {
        Self {
            bytes: Vec::with_capacity(capacity),
            max_size,
            error: None,
        }
    }

    /// Returns true if an error has occurred.
    #[must_use]
    pub fn errored(&self) -> bool {
        self.error.is_some()
    }

    /// Returns the error if one occurred.
    #[must_use]
    pub fn error(&self) -> Option<&PackError> {
        self.error.as_ref()
    }

    /// Takes the error, leaving None in its place.
    pub fn take_error(&mut self) -> Option<PackError> {
        self.error.take()
    }

    /// Returns the current length in bytes.
    #[must_use]
    pub fn len(&self) -> usize {
        self.bytes.len()
    }

    /// Returns true if the packer is empty.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.bytes.is_empty()
    }

    /// Returns a reference to the packed bytes.
    #[must_use]
    pub fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    /// Consumes the packer and returns the packed bytes.
    #[must_use]
    pub fn into_bytes(self) -> Vec<u8> {
        self.bytes
    }

    /// Ensures there's room for additional bytes.
    fn expand(&mut self, additional: usize) {
        if self.error.is_some() {
            return;
        }

        let new_len = self.bytes.len().saturating_add(additional);
        if new_len > self.max_size {
            self.error = Some(PackError::Overflow {
                needed: new_len,
                max: self.max_size,
            });
        }
    }

    /// Packs a single byte.
    pub fn pack_byte(&mut self, val: u8) {
        self.expand(1);
        if self.error.is_none() {
            self.bytes.push(val);
        }
    }

    /// Packs a u16 (2 bytes, big-endian).
    pub fn pack_short(&mut self, val: u16) {
        self.expand(2);
        if self.error.is_none() {
            self.bytes.extend_from_slice(&val.to_be_bytes());
        }
    }

    /// Packs a u32 (4 bytes, big-endian).
    pub fn pack_int(&mut self, val: u32) {
        self.expand(4);
        if self.error.is_none() {
            self.bytes.extend_from_slice(&val.to_be_bytes());
        }
    }

    /// Packs a u64 (8 bytes, big-endian).
    pub fn pack_long(&mut self, val: u64) {
        self.expand(8);
        if self.error.is_none() {
            self.bytes.extend_from_slice(&val.to_be_bytes());
        }
    }

    /// Packs a boolean (1 byte: 0x00 or 0x01).
    pub fn pack_bool(&mut self, val: bool) {
        self.pack_byte(if val { 1 } else { 0 });
    }

    /// Packs a string (2-byte length prefix + UTF-8 bytes).
    pub fn pack_str(&mut self, val: &str) {
        let len = val.len();
        if len > MAX_STRING_LEN {
            self.error = Some(PackError::StringTooLong {
                len,
                max: MAX_STRING_LEN,
            });
            return;
        }

        self.pack_short(len as u16);
        if self.error.is_none() {
            self.expand(len);
            if self.error.is_none() {
                self.bytes.extend_from_slice(val.as_bytes());
            }
        }
    }

    /// Packs fixed-size bytes (no length prefix).
    pub fn pack_fixed_bytes(&mut self, val: &[u8]) {
        self.expand(val.len());
        if self.error.is_none() {
            self.bytes.extend_from_slice(val);
        }
    }

    /// Packs variable-length bytes (4-byte length prefix + bytes).
    pub fn pack_bytes(&mut self, val: &[u8]) {
        self.pack_int(val.len() as u32);
        if self.error.is_none() {
            self.pack_fixed_bytes(val);
        }
    }
}

impl Default for Packer {
    fn default() -> Self {
        Self::new(256)
    }
}

/// An unpacker for deserializing data from bytes.
///
/// All multi-byte integers are read in big-endian order.
///
/// # Examples
///
/// ```
/// use avalanche_codec::Unpacker;
///
/// let bytes = [0, 0, 0, 42]; // Big-endian u32
/// let mut unpacker = Unpacker::new(&bytes);
/// let value = unpacker.unpack_int().unwrap();
/// assert_eq!(value, 42);
/// ```
#[derive(Debug)]
pub struct Unpacker<'a> {
    bytes: &'a [u8],
    offset: usize,
}

impl<'a> Unpacker<'a> {
    /// Creates a new unpacker from a byte slice.
    #[must_use]
    pub fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, offset: 0 }
    }

    /// Returns the number of remaining bytes.
    #[must_use]
    pub fn remaining(&self) -> usize {
        self.bytes.len().saturating_sub(self.offset)
    }

    /// Returns the current offset.
    #[must_use]
    pub fn offset(&self) -> usize {
        self.offset
    }

    /// Returns true if all bytes have been consumed.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.remaining() == 0
    }

    /// Checks that there are enough bytes remaining.
    fn check_space(&self, needed: usize) -> Result<(), UnpackError> {
        let remaining = self.remaining();
        if remaining < needed {
            return Err(UnpackError::InsufficientBytes { needed, remaining });
        }
        Ok(())
    }

    /// Unpacks a single byte.
    pub fn unpack_byte(&mut self) -> Result<u8, UnpackError> {
        self.check_space(1)?;
        let val = self.bytes[self.offset];
        self.offset += 1;
        Ok(val)
    }

    /// Unpacks a u16 (2 bytes, big-endian).
    pub fn unpack_short(&mut self) -> Result<u16, UnpackError> {
        self.check_space(2)?;
        let val = u16::from_be_bytes([self.bytes[self.offset], self.bytes[self.offset + 1]]);
        self.offset += 2;
        Ok(val)
    }

    /// Unpacks a u32 (4 bytes, big-endian).
    pub fn unpack_int(&mut self) -> Result<u32, UnpackError> {
        self.check_space(4)?;
        let val = u32::from_be_bytes([
            self.bytes[self.offset],
            self.bytes[self.offset + 1],
            self.bytes[self.offset + 2],
            self.bytes[self.offset + 3],
        ]);
        self.offset += 4;
        Ok(val)
    }

    /// Unpacks a u64 (8 bytes, big-endian).
    pub fn unpack_long(&mut self) -> Result<u64, UnpackError> {
        self.check_space(8)?;
        let val = u64::from_be_bytes([
            self.bytes[self.offset],
            self.bytes[self.offset + 1],
            self.bytes[self.offset + 2],
            self.bytes[self.offset + 3],
            self.bytes[self.offset + 4],
            self.bytes[self.offset + 5],
            self.bytes[self.offset + 6],
            self.bytes[self.offset + 7],
        ]);
        self.offset += 8;
        Ok(val)
    }

    /// Unpacks a boolean (1 byte: 0x00 or 0x01).
    pub fn unpack_bool(&mut self) -> Result<bool, UnpackError> {
        let val = self.unpack_byte()?;
        match val {
            0 => Ok(false),
            1 => Ok(true),
            _ => Err(UnpackError::InvalidBool(val)),
        }
    }

    /// Unpacks a string (2-byte length prefix + UTF-8 bytes).
    pub fn unpack_string(&mut self) -> Result<String, UnpackError> {
        let len = self.unpack_short()? as usize;
        self.check_space(len)?;
        let bytes = self.bytes[self.offset..self.offset + len].to_vec();
        self.offset += len;
        Ok(String::from_utf8(bytes)?)
    }

    /// Unpacks fixed-size bytes (no length prefix).
    pub fn unpack_fixed_bytes<const N: usize>(&mut self) -> Result<[u8; N], UnpackError> {
        self.check_space(N)?;
        let mut arr = [0u8; N];
        arr.copy_from_slice(&self.bytes[self.offset..self.offset + N]);
        self.offset += N;
        Ok(arr)
    }

    /// Unpacks variable-length bytes (4-byte length prefix + bytes).
    pub fn unpack_bytes(&mut self) -> Result<Vec<u8>, UnpackError> {
        let len = self.unpack_int()? as usize;
        self.check_space(len)?;
        let bytes = self.bytes[self.offset..self.offset + len].to_vec();
        self.offset += len;
        Ok(bytes)
    }

    /// Returns the remaining bytes without consuming them.
    #[must_use]
    pub fn peek_remaining(&self) -> &[u8] {
        &self.bytes[self.offset..]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_pack_byte() {
        let mut packer = Packer::new(16);
        packer.pack_byte(0x42);
        assert_eq!(packer.bytes(), &[0x42]);
    }

    #[test]
    fn test_pack_short() {
        let mut packer = Packer::new(16);
        packer.pack_short(0x1234);
        assert_eq!(packer.bytes(), &[0x12, 0x34]);
    }

    #[test]
    fn test_pack_int() {
        let mut packer = Packer::new(16);
        packer.pack_int(0x12345678);
        assert_eq!(packer.bytes(), &[0x12, 0x34, 0x56, 0x78]);
    }

    #[test]
    fn test_pack_long() {
        let mut packer = Packer::new(16);
        packer.pack_long(0x123456789ABCDEF0);
        assert_eq!(
            packer.bytes(),
            &[0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0]
        );
    }

    #[test]
    fn test_pack_bool() {
        let mut packer = Packer::new(16);
        packer.pack_bool(true);
        packer.pack_bool(false);
        assert_eq!(packer.bytes(), &[0x01, 0x00]);
    }

    #[test]
    fn test_pack_str() {
        let mut packer = Packer::new(64);
        packer.pack_str("hello");
        assert_eq!(packer.bytes(), &[0x00, 0x05, b'h', b'e', b'l', b'l', b'o']);
    }

    #[test]
    fn test_pack_fixed_bytes() {
        let mut packer = Packer::new(16);
        packer.pack_fixed_bytes(&[1, 2, 3, 4]);
        assert_eq!(packer.bytes(), &[1, 2, 3, 4]);
    }

    #[test]
    fn test_pack_bytes() {
        let mut packer = Packer::new(64);
        packer.pack_bytes(&[1, 2, 3]);
        assert_eq!(packer.bytes(), &[0x00, 0x00, 0x00, 0x03, 1, 2, 3]);
    }

    #[test]
    fn test_unpack_byte() {
        let mut unpacker = Unpacker::new(&[0x42]);
        assert_eq!(unpacker.unpack_byte().unwrap(), 0x42);
        assert!(unpacker.is_empty());
    }

    #[test]
    fn test_unpack_short() {
        let mut unpacker = Unpacker::new(&[0x12, 0x34]);
        assert_eq!(unpacker.unpack_short().unwrap(), 0x1234);
    }

    #[test]
    fn test_unpack_int() {
        let mut unpacker = Unpacker::new(&[0x12, 0x34, 0x56, 0x78]);
        assert_eq!(unpacker.unpack_int().unwrap(), 0x12345678);
    }

    #[test]
    fn test_unpack_long() {
        let mut unpacker = Unpacker::new(&[0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0]);
        assert_eq!(unpacker.unpack_long().unwrap(), 0x123456789ABCDEF0);
    }

    #[test]
    fn test_unpack_bool() {
        let mut unpacker = Unpacker::new(&[0x01, 0x00]);
        assert!(unpacker.unpack_bool().unwrap());
        assert!(!unpacker.unpack_bool().unwrap());
    }

    #[test]
    fn test_unpack_bool_invalid() {
        let mut unpacker = Unpacker::new(&[0x02]);
        assert!(matches!(
            unpacker.unpack_bool(),
            Err(UnpackError::InvalidBool(2))
        ));
    }

    #[test]
    fn test_unpack_string() {
        let mut unpacker = Unpacker::new(&[0x00, 0x05, b'h', b'e', b'l', b'l', b'o']);
        assert_eq!(unpacker.unpack_string().unwrap(), "hello");
    }

    #[test]
    fn test_unpack_fixed_bytes() {
        let mut unpacker = Unpacker::new(&[1, 2, 3, 4]);
        let arr: [u8; 4] = unpacker.unpack_fixed_bytes().unwrap();
        assert_eq!(arr, [1, 2, 3, 4]);
    }

    #[test]
    fn test_unpack_bytes() {
        let mut unpacker = Unpacker::new(&[0x00, 0x00, 0x00, 0x03, 1, 2, 3]);
        assert_eq!(unpacker.unpack_bytes().unwrap(), vec![1, 2, 3]);
    }

    #[test]
    fn test_insufficient_bytes() {
        let mut unpacker = Unpacker::new(&[0x00]);
        assert!(matches!(
            unpacker.unpack_int(),
            Err(UnpackError::InsufficientBytes { .. })
        ));
    }

    #[test]
    fn test_roundtrip() {
        let mut packer = Packer::new(256);
        packer.pack_byte(0x42);
        packer.pack_short(0x1234);
        packer.pack_int(0x12345678);
        packer.pack_long(0x123456789ABCDEF0);
        packer.pack_bool(true);
        packer.pack_str("hello world");
        packer.pack_bytes(&[1, 2, 3, 4, 5]);

        let bytes = packer.into_bytes();
        let mut unpacker = Unpacker::new(&bytes);

        assert_eq!(unpacker.unpack_byte().unwrap(), 0x42);
        assert_eq!(unpacker.unpack_short().unwrap(), 0x1234);
        assert_eq!(unpacker.unpack_int().unwrap(), 0x12345678);
        assert_eq!(unpacker.unpack_long().unwrap(), 0x123456789ABCDEF0);
        assert!(unpacker.unpack_bool().unwrap());
        assert_eq!(unpacker.unpack_string().unwrap(), "hello world");
        assert_eq!(unpacker.unpack_bytes().unwrap(), vec![1, 2, 3, 4, 5]);
        assert!(unpacker.is_empty());
    }

    #[test]
    fn test_max_size_limit() {
        let mut packer = Packer::with_max_size(16, 10);
        packer.pack_bytes(&[0; 20]); // This should fail
        assert!(packer.errored());
    }
}
