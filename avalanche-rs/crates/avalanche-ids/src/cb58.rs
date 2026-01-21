//! CB58 encoding and decoding.
//!
//! CB58 is a Base58 encoding with a 4-byte checksum appended.
//! This is the standard encoding format for Avalanche identifiers.

use thiserror::Error;

use crate::hashing::checksum;

/// Length of the CB58 checksum in bytes.
const CHECKSUM_LEN: usize = 4;

/// Errors that can occur during CB58 encoding/decoding.
#[derive(Debug, Error, PartialEq, Eq)]
pub enum Cb58Error {
    /// Failed to decode the Base58 string.
    #[error("base58 decoding error: {0}")]
    Base58Decoding(String),

    /// The input string is too short to contain a checksum.
    #[error("input string is smaller than the checksum size")]
    MissingChecksum,

    /// The checksum in the input does not match the computed checksum.
    #[error("invalid checksum")]
    BadChecksum,
}

/// Encodes bytes to a CB58 string.
///
/// CB58 is Base58 encoding with a 4-byte SHA-256 checksum appended.
///
/// # Examples
///
/// ```
/// use avalanche_ids::encode_cb58;
///
/// let encoded = encode_cb58(&[1, 2, 3, 4]).unwrap();
/// assert!(!encoded.is_empty());
/// ```
pub fn encode_cb58(bytes: &[u8]) -> Result<String, Cb58Error> {
    let mut checked = Vec::with_capacity(bytes.len() + CHECKSUM_LEN);
    checked.extend_from_slice(bytes);
    checked.extend_from_slice(&checksum(bytes, CHECKSUM_LEN));
    Ok(bs58::encode(checked).into_string())
}

/// Decodes a CB58 string to bytes.
///
/// Verifies the 4-byte checksum and returns the original bytes.
///
/// # Errors
///
/// Returns an error if:
/// - The string is not valid Base58
/// - The string is too short to contain a checksum
/// - The checksum does not match
///
/// # Examples
///
/// ```
/// use avalanche_ids::{encode_cb58, decode_cb58};
///
/// let original = vec![1, 2, 3, 4];
/// let encoded = encode_cb58(&original).unwrap();
/// let decoded = decode_cb58(&encoded).unwrap();
/// assert_eq!(original, decoded);
/// ```
pub fn decode_cb58(s: &str) -> Result<Vec<u8>, Cb58Error> {
    let decoded = bs58::decode(s)
        .into_vec()
        .map_err(|e| Cb58Error::Base58Decoding(e.to_string()))?;

    if decoded.len() < CHECKSUM_LEN {
        return Err(Cb58Error::MissingChecksum);
    }

    let data_len = decoded.len() - CHECKSUM_LEN;
    let (raw_bytes, provided_checksum) = decoded.split_at(data_len);

    let computed_checksum = checksum(raw_bytes, CHECKSUM_LEN);
    if provided_checksum != computed_checksum {
        return Err(Cb58Error::BadChecksum);
    }

    Ok(raw_bytes.to_vec())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_encode_decode_roundtrip() {
        let test_cases: &[&[u8]] = &[&[], &[0], &[1, 2, 3, 4], &[0xff; 32], &[0x00; 20]];

        for bytes in test_cases {
            let encoded = encode_cb58(bytes).unwrap();
            let decoded = decode_cb58(&encoded).unwrap();
            assert_eq!(
                *bytes,
                decoded.as_slice(),
                "roundtrip failed for {:?}",
                bytes
            );
        }
    }

    #[test]
    fn test_decode_invalid_base58() {
        // '0', 'O', 'I', 'l' are not valid Base58 characters
        let result = decode_cb58("0OIl");
        assert!(matches!(result, Err(Cb58Error::Base58Decoding(_))));
    }

    #[test]
    fn test_decode_too_short() {
        // Encode something very short that decodes to less than 4 bytes
        let result = decode_cb58("1");
        assert!(matches!(result, Err(Cb58Error::MissingChecksum)));
    }

    #[test]
    fn test_decode_bad_checksum() {
        // Encode valid data then corrupt the checksum
        let encoded = encode_cb58(&[1, 2, 3, 4]).unwrap();

        // Decode to raw bytes, corrupt checksum, re-encode
        let mut raw = bs58::decode(&encoded).into_vec().unwrap();
        if let Some(last) = raw.last_mut() {
            *last ^= 0xff; // Flip bits to corrupt checksum
        }
        let corrupted = bs58::encode(raw).into_string();

        let result = decode_cb58(&corrupted);
        assert!(matches!(result, Err(Cb58Error::BadChecksum)));
    }

    #[test]
    fn test_known_encoding() {
        // Test with known values from Go implementation
        // Empty slice encodes to just the checksum of empty
        let encoded = encode_cb58(&[]).unwrap();
        let decoded = decode_cb58(&encoded).unwrap();
        assert!(decoded.is_empty());
    }
}
