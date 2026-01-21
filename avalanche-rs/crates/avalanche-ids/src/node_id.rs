//! Node identifier type.

use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Deserializer, Serialize, Serializer};
use thiserror::Error;

use crate::cb58::{decode_cb58, encode_cb58, Cb58Error};
use crate::hashing::{compute_hash160, compute_hash256};
use crate::short::ShortId;
use crate::NODE_ID_LEN;

/// The prefix for NodeID string representations.
pub const NODE_ID_PREFIX: &str = "NodeID-";

/// A 20-byte identifier for validator nodes.
///
/// NodeIDs are derived from the node's TLS certificate:
/// `RIPEMD160(SHA256(certificate_der))`.
///
/// # String Representation
///
/// NodeIDs are displayed with a "NodeID-" prefix followed by CB58 encoding.
///
/// # Examples
///
/// ```
/// use avalanche_ids::NodeId;
///
/// // Create from bytes
/// let id = NodeId::from_bytes([0u8; 20]);
/// assert!(id.is_empty());
///
/// // String format includes prefix
/// let s = id.to_string();
/// assert!(s.starts_with("NodeID-"));
///
/// // Parse from string
/// let id: NodeId = "NodeID-111111111111111111116DBWJs".parse().unwrap();
/// assert!(id.is_empty());
/// ```
#[derive(Clone, Copy, PartialEq, Eq, Hash, Default)]
pub struct NodeId([u8; NODE_ID_LEN]);

/// Errors that can occur when parsing a NodeID.
#[derive(Debug, Error)]
pub enum NodeIdError {
    /// The CB58 decoding failed.
    #[error("cb58 decoding failed: {0}")]
    Cb58(#[from] Cb58Error),

    /// The decoded bytes have the wrong length.
    #[error("invalid length: expected {expected} bytes, got {actual}")]
    InvalidLength { expected: usize, actual: usize },

    /// The NodeID string is missing the required prefix.
    #[error("missing NodeID- prefix")]
    MissingPrefix,

    /// The NodeID string is too short.
    #[error("node ID string too short")]
    TooShort,
}

impl NodeId {
    /// The empty (zero) NodeID.
    pub const EMPTY: Self = Self([0u8; NODE_ID_LEN]);

    /// Creates a NodeID from a 20-byte array.
    #[must_use]
    pub const fn from_bytes(bytes: [u8; NODE_ID_LEN]) -> Self {
        Self(bytes)
    }

    /// Creates a NodeID from a slice, returning an error if the length is wrong.
    ///
    /// # Errors
    ///
    /// Returns `NodeIdError::InvalidLength` if the slice is not exactly 20 bytes.
    pub fn from_slice(bytes: &[u8]) -> Result<Self, NodeIdError> {
        if bytes.len() != NODE_ID_LEN {
            return Err(NodeIdError::InvalidLength {
                expected: NODE_ID_LEN,
                actual: bytes.len(),
            });
        }
        let mut arr = [0u8; NODE_ID_LEN];
        arr.copy_from_slice(bytes);
        Ok(Self(arr))
    }

    /// Creates a NodeID from a TLS certificate's DER-encoded bytes.
    ///
    /// The NodeID is computed as RIPEMD160(SHA256(certificate_der)).
    #[must_use]
    pub fn from_cert(cert_der: &[u8]) -> Self {
        let sha = compute_hash256(cert_der);
        Self(compute_hash160(&sha))
    }

    /// Creates a NodeID from a ShortID.
    #[must_use]
    pub fn from_short_id(short_id: ShortId) -> Self {
        Self(*short_id.as_bytes())
    }

    /// Returns the NodeID as a byte slice.
    #[must_use]
    pub fn as_bytes(&self) -> &[u8; NODE_ID_LEN] {
        &self.0
    }

    /// Converts this NodeID to a ShortID.
    #[must_use]
    pub const fn to_short_id(&self) -> ShortId {
        ShortId::from_bytes(self.0)
    }

    /// Returns true if this is the empty (zero) NodeID.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.0 == [0u8; NODE_ID_LEN]
    }

    /// Returns the hex-encoded representation of this NodeID (without prefix).
    #[must_use]
    pub fn to_hex(&self) -> String {
        hex::encode(self.0)
    }

    /// Compares this NodeID with another, returning the ordering.
    #[must_use]
    pub fn compare(&self, other: &Self) -> std::cmp::Ordering {
        self.0.cmp(&other.0)
    }
}

impl fmt::Display for NodeId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match encode_cb58(&self.0) {
            Ok(s) => write!(f, "{NODE_ID_PREFIX}{s}"),
            Err(_) => write!(f, "{NODE_ID_PREFIX}<invalid>"),
        }
    }
}

impl fmt::Debug for NodeId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "NodeId({})", self)
    }
}

impl FromStr for NodeId {
    type Err = NodeIdError;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        if s.len() <= NODE_ID_PREFIX.len() + 2 {
            return Err(NodeIdError::TooShort);
        }

        let stripped = s
            .strip_prefix(NODE_ID_PREFIX)
            .ok_or(NodeIdError::MissingPrefix)?;
        let bytes = decode_cb58(stripped)?;
        Self::from_slice(&bytes)
    }
}

impl PartialOrd for NodeId {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl Ord for NodeId {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.0.cmp(&other.0)
    }
}

impl AsRef<[u8]> for NodeId {
    fn as_ref(&self) -> &[u8] {
        &self.0
    }
}

impl From<[u8; NODE_ID_LEN]> for NodeId {
    fn from(bytes: [u8; NODE_ID_LEN]) -> Self {
        Self::from_bytes(bytes)
    }
}

impl From<NodeId> for [u8; NODE_ID_LEN] {
    fn from(id: NodeId) -> Self {
        id.0
    }
}

impl From<ShortId> for NodeId {
    fn from(short_id: ShortId) -> Self {
        Self::from_short_id(short_id)
    }
}

impl From<NodeId> for ShortId {
    fn from(node_id: NodeId) -> Self {
        node_id.to_short_id()
    }
}

impl Serialize for NodeId {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        if serializer.is_human_readable() {
            serializer.serialize_str(&self.to_string())
        } else {
            serializer.serialize_bytes(&self.0)
        }
    }
}

impl<'de> Deserialize<'de> for NodeId {
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
    fn test_empty_node_id() {
        let id = NodeId::EMPTY;
        assert!(id.is_empty());
        assert_eq!(id.as_bytes(), &[0u8; 20]);
    }

    #[test]
    fn test_from_bytes() {
        let bytes = [42u8; 20];
        let id = NodeId::from_bytes(bytes);
        assert_eq!(id.as_bytes(), &bytes);
        assert!(!id.is_empty());
    }

    #[test]
    fn test_from_slice() {
        let bytes = [42u8; 20];
        let id = NodeId::from_slice(&bytes).unwrap();
        assert_eq!(id.as_bytes(), &bytes);

        // Wrong length should fail
        assert!(NodeId::from_slice(&[1, 2, 3]).is_err());
    }

    #[test]
    fn test_from_cert() {
        let cert = b"fake certificate data";
        let id = NodeId::from_cert(cert);
        assert!(!id.is_empty());

        // Same cert should produce same NodeID
        let id2 = NodeId::from_cert(cert);
        assert_eq!(id, id2);
    }

    #[test]
    fn test_string_roundtrip() {
        let id = NodeId::from_bytes([42u8; 20]);
        let s = id.to_string();
        assert!(s.starts_with(NODE_ID_PREFIX));

        let parsed: NodeId = s.parse().unwrap();
        assert_eq!(id, parsed);
    }

    #[test]
    fn test_string_missing_prefix() {
        let result = "somestringwithoutprefix".parse::<NodeId>();
        assert!(matches!(result, Err(NodeIdError::MissingPrefix)));
    }

    #[test]
    fn test_string_too_short() {
        let result = "NodeID-".parse::<NodeId>();
        assert!(matches!(result, Err(NodeIdError::TooShort)));
    }

    #[test]
    fn test_short_id_conversion() {
        let bytes = [42u8; 20];
        let node_id = NodeId::from_bytes(bytes);
        let short_id = node_id.to_short_id();
        assert_eq!(short_id.as_bytes(), &bytes);

        let back = NodeId::from_short_id(short_id);
        assert_eq!(node_id, back);
    }

    #[test]
    fn test_ordering() {
        let id1 = NodeId::from_bytes([0u8; 20]);
        let id2 = NodeId::from_bytes([1u8; 20]);
        assert!(id1 < id2);
    }

    #[test]
    fn test_json_serialization() {
        let id = NodeId::from_bytes([42u8; 20]);
        let json = serde_json::to_string(&id).unwrap();
        assert!(json.contains(NODE_ID_PREFIX));

        let parsed: NodeId = serde_json::from_str(&json).unwrap();
        assert_eq!(id, parsed);
    }

    #[test]
    fn test_hex() {
        let bytes = [
            0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
        ];
        let id = NodeId::from_bytes(bytes);
        assert!(id.to_hex().starts_with("123456789abcdef0"));
    }
}
