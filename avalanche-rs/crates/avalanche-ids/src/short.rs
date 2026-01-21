//! 20-byte identifier type.

use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Deserializer, Serialize, Serializer};
use thiserror::Error;

use crate::cb58::{decode_cb58, encode_cb58, Cb58Error};
use crate::hashing::compute_hash160;
use crate::SHORT_ID_LEN;

/// A 20-byte (160-bit) identifier used for addresses.
///
/// ShortIDs are typically derived from public keys using RIPEMD-160(SHA-256(pubkey)).
///
/// # String Representation
///
/// ShortIDs use CB58 encoding (Base58 with checksum) for human-readable strings.
///
/// # Examples
///
/// ```
/// use avalanche_ids::ShortId;
///
/// // Create from bytes
/// let id = ShortId::from_bytes([0u8; 20]);
/// assert!(id.is_empty());
///
/// // Parse from string
/// let id: ShortId = "111111111111111111116DBWJs".parse().unwrap();
/// assert!(id.is_empty());
/// ```
#[derive(Clone, Copy, PartialEq, Eq, Hash, Default)]
pub struct ShortId([u8; SHORT_ID_LEN]);

/// Errors that can occur when parsing a ShortID.
#[derive(Debug, Error)]
pub enum ShortIdError {
    /// The CB58 decoding failed.
    #[error("cb58 decoding failed: {0}")]
    Cb58(#[from] Cb58Error),

    /// The decoded bytes have the wrong length.
    #[error("invalid length: expected {expected} bytes, got {actual}")]
    InvalidLength { expected: usize, actual: usize },

    /// Missing expected prefix.
    #[error("missing prefix: expected '{expected}'")]
    MissingPrefix { expected: String },
}

impl ShortId {
    /// The empty (zero) ShortID.
    pub const EMPTY: Self = Self([0u8; SHORT_ID_LEN]);

    /// Creates a ShortID from a 20-byte array.
    #[must_use]
    pub const fn from_bytes(bytes: [u8; SHORT_ID_LEN]) -> Self {
        Self(bytes)
    }

    /// Creates a ShortID from a slice, returning an error if the length is wrong.
    ///
    /// # Errors
    ///
    /// Returns `ShortIdError::InvalidLength` if the slice is not exactly 20 bytes.
    pub fn from_slice(bytes: &[u8]) -> Result<Self, ShortIdError> {
        if bytes.len() != SHORT_ID_LEN {
            return Err(ShortIdError::InvalidLength {
                expected: SHORT_ID_LEN,
                actual: bytes.len(),
            });
        }
        let mut arr = [0u8; SHORT_ID_LEN];
        arr.copy_from_slice(bytes);
        Ok(Self(arr))
    }

    /// Computes a ShortID from the RIPEMD-160 hash of the given bytes.
    #[must_use]
    pub fn from_hash(bytes: &[u8]) -> Self {
        Self(compute_hash160(bytes))
    }

    /// Parses a ShortID from a prefixed string (e.g., "X-avax1...").
    ///
    /// # Errors
    ///
    /// Returns an error if the string doesn't start with the expected prefix
    /// or if the CB58 decoding fails.
    pub fn from_prefixed_string(s: &str, prefix: &str) -> Result<Self, ShortIdError> {
        let stripped = s
            .strip_prefix(prefix)
            .ok_or_else(|| ShortIdError::MissingPrefix {
                expected: prefix.to_string(),
            })?;
        stripped.parse()
    }

    /// Returns the ShortID as a byte slice.
    #[must_use]
    pub fn as_bytes(&self) -> &[u8; SHORT_ID_LEN] {
        &self.0
    }

    /// Returns true if this is the empty (zero) ShortID.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.0 == [0u8; SHORT_ID_LEN]
    }

    /// Returns the hex-encoded representation of this ShortID.
    #[must_use]
    pub fn to_hex(&self) -> String {
        hex::encode(self.0)
    }

    /// Returns the CB58 representation with a prefix.
    #[must_use]
    pub fn to_prefixed_string(&self, prefix: &str) -> String {
        format!("{}{}", prefix, self)
    }

    /// Compares this ShortID with another, returning the ordering.
    #[must_use]
    pub fn compare(&self, other: &Self) -> std::cmp::Ordering {
        self.0.cmp(&other.0)
    }
}

impl fmt::Display for ShortId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match encode_cb58(&self.0) {
            Ok(s) => write!(f, "{s}"),
            Err(_) => write!(f, "<invalid>"),
        }
    }
}

impl fmt::Debug for ShortId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "ShortId({})", self)
    }
}

impl FromStr for ShortId {
    type Err = ShortIdError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let bytes = decode_cb58(s)?;
        Self::from_slice(&bytes)
    }
}

impl PartialOrd for ShortId {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for ShortId {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.0.cmp(&other.0)
    }
}

impl AsRef<[u8]> for ShortId {
    fn as_ref(&self) -> &[u8] {
        &self.0
    }
}

impl From<[u8; SHORT_ID_LEN]> for ShortId {
    fn from(bytes: [u8; SHORT_ID_LEN]) -> Self {
        Self::from_bytes(bytes)
    }
}

impl From<ShortId> for [u8; SHORT_ID_LEN] {
    fn from(id: ShortId) -> Self {
        id.0
    }
}

impl Serialize for ShortId {
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

impl<'de> Deserialize<'de> for ShortId {
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
    fn test_empty_short_id() {
        let id = ShortId::EMPTY;
        assert!(id.is_empty());
        assert_eq!(id.as_bytes(), &[0u8; 20]);
    }

    #[test]
    fn test_from_bytes() {
        let bytes = [42u8; 20];
        let id = ShortId::from_bytes(bytes);
        assert_eq!(id.as_bytes(), &bytes);
        assert!(!id.is_empty());
    }

    #[test]
    fn test_from_slice() {
        let bytes = [42u8; 20];
        let id = ShortId::from_slice(&bytes).unwrap();
        assert_eq!(id.as_bytes(), &bytes);

        // Wrong length should fail
        assert!(ShortId::from_slice(&[1, 2, 3]).is_err());
    }

    #[test]
    fn test_from_hash() {
        let id = ShortId::from_hash(b"test");
        assert!(!id.is_empty());

        // Same input should produce same hash
        let id2 = ShortId::from_hash(b"test");
        assert_eq!(id, id2);
    }

    #[test]
    fn test_string_roundtrip() {
        let id = ShortId::from_bytes([42u8; 20]);
        let s = id.to_string();
        let parsed: ShortId = s.parse().unwrap();
        assert_eq!(id, parsed);
    }

    #[test]
    fn test_prefixed_string() {
        let id = ShortId::from_bytes([42u8; 20]);
        let prefix = "X-avax1";
        let prefixed = id.to_prefixed_string(prefix);
        assert!(prefixed.starts_with(prefix));

        let parsed = ShortId::from_prefixed_string(&prefixed, prefix).unwrap();
        assert_eq!(id, parsed);
    }

    #[test]
    fn test_prefixed_string_missing_prefix() {
        let result = ShortId::from_prefixed_string("somestring", "X-avax1");
        assert!(matches!(result, Err(ShortIdError::MissingPrefix { .. })));
    }

    #[test]
    fn test_hex() {
        let bytes = [
            0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        ];
        let id = ShortId::from_bytes(bytes);
        assert!(id.to_hex().starts_with("123456789abcdef0"));
    }

    #[test]
    fn test_ordering() {
        let id1 = ShortId::from_bytes([0u8; 20]);
        let id2 = ShortId::from_bytes([1u8; 20]);
        assert!(id1 < id2);
    }

    #[test]
    fn test_json_serialization() {
        let id = ShortId::from_bytes([42u8; 20]);
        let json = serde_json::to_string(&id).unwrap();
        let parsed: ShortId = serde_json::from_str(&json).unwrap();
        assert_eq!(id, parsed);
    }
}
