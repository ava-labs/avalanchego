//! 32-byte identifier type.

use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Deserializer, Serialize, Serializer};
use thiserror::Error;

use crate::cb58::{decode_cb58, encode_cb58, Cb58Error};
use crate::hashing::compute_hash256;
use crate::ID_LEN;

/// A 32-byte (256-bit) identifier used throughout Avalanche.
///
/// IDs are used to identify blocks, transactions, chains, and other entities.
/// They are typically the SHA-256 hash of the entity's contents.
///
/// # String Representation
///
/// IDs use CB58 encoding (Base58 with checksum) for human-readable strings.
///
/// # Examples
///
/// ```
/// use avalanche_ids::Id;
///
/// // Create from bytes
/// let id = Id::from_bytes([0u8; 32]);
/// assert!(id.is_empty());
///
/// // Parse from string
/// let id: Id = "11111111111111111111111111111111LpoYY".parse().unwrap();
/// assert!(id.is_empty());
/// ```
#[derive(Clone, Copy, PartialEq, Eq, Hash, Default)]
pub struct Id([u8; ID_LEN]);

/// Errors that can occur when parsing an ID.
#[derive(Debug, Error)]
pub enum IdError {
    /// The CB58 decoding failed.
    #[error("cb58 decoding failed: {0}")]
    Cb58(#[from] Cb58Error),

    /// The decoded bytes have the wrong length.
    #[error("invalid length: expected {expected} bytes, got {actual}")]
    InvalidLength { expected: usize, actual: usize },
}

impl Id {
    /// The empty (zero) ID.
    pub const EMPTY: Self = Self([0u8; ID_LEN]);

    /// Creates an ID from a 32-byte array.
    #[must_use]
    pub const fn from_bytes(bytes: [u8; ID_LEN]) -> Self {
        Self(bytes)
    }

    /// Creates an ID from a slice, returning an error if the length is wrong.
    ///
    /// # Errors
    ///
    /// Returns `IdError::InvalidLength` if the slice is not exactly 32 bytes.
    pub fn from_slice(bytes: &[u8]) -> Result<Self, IdError> {
        if bytes.len() != ID_LEN {
            return Err(IdError::InvalidLength {
                expected: ID_LEN,
                actual: bytes.len(),
            });
        }
        let mut arr = [0u8; ID_LEN];
        arr.copy_from_slice(bytes);
        Ok(Self(arr))
    }

    /// Computes an ID from the SHA-256 hash of the given bytes.
    #[must_use]
    pub fn from_hash(bytes: &[u8]) -> Self {
        Self(compute_hash256(bytes))
    }

    /// Returns the ID as a byte slice.
    #[must_use]
    pub fn as_bytes(&self) -> &[u8; ID_LEN] {
        &self.0
    }

    /// Returns true if this is the empty (zero) ID.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.0 == [0u8; ID_LEN]
    }

    /// Returns the hex-encoded representation of this ID.
    #[must_use]
    pub fn to_hex(&self) -> String {
        hex::encode(self.0)
    }

    /// Returns the bit value at the given index.
    ///
    /// Returns 0 or 1.
    ///
    /// # Panics
    ///
    /// Panics if `index >= 256`.
    #[must_use]
    pub fn bit(&self, index: usize) -> u8 {
        assert!(index < ID_LEN * 8, "bit index out of range");
        let byte_index = index / 8;
        let bit_index = index % 8;
        (self.0[byte_index] >> bit_index) & 1
    }

    /// XORs this ID with another and returns the result.
    ///
    /// The original ID is not modified.
    #[must_use]
    pub fn xor(&self, other: &Self) -> Self {
        let mut result = *self;
        for (a, b) in result.0.iter_mut().zip(other.0.iter()) {
            *a ^= b;
        }
        result
    }

    /// Prefixes this ID with the given prefix values and returns a new hashed ID.
    ///
    /// This is used to derive new IDs from existing ones while maintaining uniqueness.
    #[must_use]
    pub fn prefix(&self, prefixes: &[u64]) -> Self {
        let mut buf = Vec::with_capacity(prefixes.len() * 8 + ID_LEN);
        for p in prefixes {
            buf.extend_from_slice(&p.to_be_bytes());
        }
        buf.extend_from_slice(&self.0);
        Self::from_hash(&buf)
    }

    /// Appends the given suffix values to this ID and returns a new hashed ID.
    ///
    /// This is used for ACP-77 validation IDs.
    #[must_use]
    pub fn append(&self, suffixes: &[u32]) -> Self {
        let mut buf = Vec::with_capacity(ID_LEN + suffixes.len() * 4);
        buf.extend_from_slice(&self.0);
        for s in suffixes {
            buf.extend_from_slice(&s.to_be_bytes());
        }
        Self::from_hash(&buf)
    }

    /// Compares this ID with another, returning the ordering.
    #[must_use]
    pub fn compare(&self, other: &Self) -> std::cmp::Ordering {
        self.0.cmp(&other.0)
    }
}

impl fmt::Display for Id {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match encode_cb58(&self.0) {
            Ok(s) => write!(f, "{s}"),
            Err(_) => write!(f, "<invalid>"),
        }
    }
}

impl fmt::Debug for Id {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Id({})", self)
    }
}

impl FromStr for Id {
    type Err = IdError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let bytes = decode_cb58(s)?;
        Self::from_slice(&bytes)
    }
}

impl PartialOrd for Id {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for Id {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.0.cmp(&other.0)
    }
}

impl AsRef<[u8]> for Id {
    fn as_ref(&self) -> &[u8] {
        &self.0
    }
}

impl From<[u8; ID_LEN]> for Id {
    fn from(bytes: [u8; ID_LEN]) -> Self {
        Self::from_bytes(bytes)
    }
}

impl From<Id> for [u8; ID_LEN] {
    fn from(id: Id) -> Self {
        id.0
    }
}

impl Serialize for Id {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        if serializer.is_human_readable() {
            let s = encode_cb58(&self.0).map_err(serde::ser::Error::custom)?;
            serializer.serialize_str(&s)
        } else {
            serializer.serialize_bytes(&self.0)
        }
    }
}

impl<'de> Deserialize<'de> for Id {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        if deserializer.is_human_readable() {
            let s = String::deserialize(deserializer)?;
            s.parse().map_err(serde::de::Error::custom)
        } else {
            let bytes = <Vec<u8>>::deserialize(deserializer)?;
            Self::from_slice(&bytes).map_err(serde::de::Error::custom)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_empty_id() {
        let id = Id::EMPTY;
        assert!(id.is_empty());
        assert_eq!(id.as_bytes(), &[0u8; 32]);
    }

    #[test]
    fn test_from_bytes() {
        let bytes = [42u8; 32];
        let id = Id::from_bytes(bytes);
        assert_eq!(id.as_bytes(), &bytes);
        assert!(!id.is_empty());
    }

    #[test]
    fn test_from_slice() {
        let bytes = [42u8; 32];
        let id = Id::from_slice(&bytes).unwrap();
        assert_eq!(id.as_bytes(), &bytes);

        // Wrong length should fail
        assert!(Id::from_slice(&[1, 2, 3]).is_err());
    }

    #[test]
    fn test_from_hash() {
        let id = Id::from_hash(b"test");
        assert!(!id.is_empty());

        // Same input should produce same hash
        let id2 = Id::from_hash(b"test");
        assert_eq!(id, id2);

        // Different input should produce different hash
        let id3 = Id::from_hash(b"other");
        assert_ne!(id, id3);
    }

    #[test]
    fn test_string_roundtrip() {
        let id = Id::from_bytes([42u8; 32]);
        let s = id.to_string();
        let parsed: Id = s.parse().unwrap();
        assert_eq!(id, parsed);
    }

    #[test]
    fn test_hex() {
        let bytes = [
            0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
            0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        ];
        let id = Id::from_bytes(bytes);
        assert!(id.to_hex().starts_with("123456789abcdef0"));
    }

    #[test]
    fn test_bit() {
        let mut bytes = [0u8; 32];
        bytes[0] = 0b00000001; // First bit is 1
        bytes[1] = 0b00000010; // Second byte, bit 1 is 1
        let id = Id::from_bytes(bytes);

        assert_eq!(id.bit(0), 1);
        assert_eq!(id.bit(1), 0);
        assert_eq!(id.bit(9), 1); // Bit 1 of byte 1
        assert_eq!(id.bit(10), 0);
    }

    #[test]
    fn test_xor() {
        let id1 = Id::from_bytes([0xff; 32]);
        let id2 = Id::from_bytes([0xff; 32]);
        let xored = id1.xor(&id2);
        assert!(xored.is_empty());

        let id3 = Id::from_bytes([0xaa; 32]);
        let id4 = Id::from_bytes([0x55; 32]);
        let xored2 = id3.xor(&id4);
        assert_eq!(xored2.as_bytes(), &[0xff; 32]);
    }

    #[test]
    fn test_prefix() {
        let id = Id::from_bytes([1u8; 32]);
        let prefixed = id.prefix(&[1, 2, 3]);
        assert_ne!(id, prefixed);

        // Same prefix should give same result
        let prefixed2 = id.prefix(&[1, 2, 3]);
        assert_eq!(prefixed, prefixed2);

        // Different prefix should give different result
        let prefixed3 = id.prefix(&[1, 2, 4]);
        assert_ne!(prefixed, prefixed3);
    }

    #[test]
    fn test_ordering() {
        let id1 = Id::from_bytes([0u8; 32]);
        let id2 = Id::from_bytes([1u8; 32]);
        assert!(id1 < id2);
    }

    #[test]
    fn test_json_serialization() {
        let id = Id::from_bytes([42u8; 32]);
        let json = serde_json::to_string(&id).unwrap();
        let parsed: Id = serde_json::from_str(&json).unwrap();
        assert_eq!(id, parsed);
    }
}
