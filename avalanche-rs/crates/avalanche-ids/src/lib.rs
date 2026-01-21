//! Avalanche identifier types.
//!
//! This crate provides the core identifier types used throughout Avalanche:
//! - [`Id`]: A 32-byte hash used as a general identifier
//! - [`ShortId`]: A 20-byte hash used for addresses
//! - [`NodeId`]: A 20-byte identifier for validator nodes
//!
//! All types support CB58 encoding (Base58 with checksum) for human-readable representation.

mod cb58;
mod hashing;
mod id;
mod node_id;
mod short;

pub use cb58::{decode_cb58, encode_cb58, Cb58Error};
pub use hashing::{compute_hash160, compute_hash256, Hash160, Hash256};
pub use id::Id;
pub use node_id::NodeId;
pub use short::ShortId;

/// Length of an ID in bytes (32 bytes / 256 bits).
pub const ID_LEN: usize = 32;

/// Length of a ShortID in bytes (20 bytes / 160 bits).
pub const SHORT_ID_LEN: usize = 20;

/// Length of a NodeID in bytes (same as ShortID).
pub const NODE_ID_LEN: usize = SHORT_ID_LEN;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_id_roundtrip() {
        let bytes = [42u8; ID_LEN];
        let id = Id::from_bytes(bytes);
        let encoded = id.to_string();
        let decoded = encoded.parse::<Id>().unwrap();
        assert_eq!(id, decoded);
    }

    #[test]
    fn test_short_id_roundtrip() {
        let bytes = [42u8; SHORT_ID_LEN];
        let id = ShortId::from_bytes(bytes);
        let encoded = id.to_string();
        let decoded = encoded.parse::<ShortId>().unwrap();
        assert_eq!(id, decoded);
    }

    #[test]
    fn test_node_id_roundtrip() {
        let bytes = [42u8; NODE_ID_LEN];
        let id = NodeId::from_bytes(bytes);
        let encoded = id.to_string();
        let decoded = encoded.parse::<NodeId>().unwrap();
        assert_eq!(id, decoded);
    }
}
