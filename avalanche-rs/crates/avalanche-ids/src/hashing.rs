//! Cryptographic hashing utilities.
//!
//! Provides SHA-256 (256-bit) and RIPEMD-160 (160-bit) hashing functions
//! compatible with the Go implementation.

use ripemd::Ripemd160;
use sha2::{Digest, Sha256};

/// A 256-bit (32-byte) hash value.
pub type Hash256 = [u8; 32];

/// A 160-bit (20-byte) hash value.
pub type Hash160 = [u8; 20];

/// Computes a SHA-256 hash of the input bytes.
///
/// # Examples
///
/// ```
/// use avalanche_ids::compute_hash256;
///
/// let hash = compute_hash256(b"hello world");
/// assert_eq!(hash.len(), 32);
/// ```
#[must_use]
pub fn compute_hash256(data: &[u8]) -> Hash256 {
    let mut hasher = Sha256::new();
    hasher.update(data);
    hasher.finalize().into()
}

/// Computes a RIPEMD-160 hash of the input bytes.
///
/// Note: RIPEMD-160 is used for address generation, similar to Bitcoin.
/// While not generally recommended for new applications, its use here
/// is considered acceptable due to the small input size (public keys).
///
/// # Examples
///
/// ```
/// use avalanche_ids::compute_hash160;
///
/// let hash = compute_hash160(b"hello world");
/// assert_eq!(hash.len(), 20);
/// ```
#[must_use]
pub fn compute_hash160(data: &[u8]) -> Hash160 {
    let mut hasher = Ripemd160::new();
    hasher.update(data);
    hasher.finalize().into()
}

/// Computes a checksum of the given length from the SHA-256 hash.
///
/// Returns the last `length` bytes of the SHA-256 hash.
///
/// # Panics
///
/// Panics if `length` is greater than 32.
#[must_use]
pub fn checksum(data: &[u8], length: usize) -> Vec<u8> {
    assert!(length <= 32, "checksum length must be <= 32");
    let hash = compute_hash256(data);
    hash[32 - length..].to_vec()
}

/// Converts a public key to an address using hash160(hash256(pubkey)).
///
/// This matches the Go implementation's `PubkeyBytesToAddress`.
#[must_use]
pub fn pubkey_to_address(pubkey: &[u8]) -> Hash160 {
    compute_hash160(&compute_hash256(pubkey))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_compute_hash256() {
        // Known SHA-256 hash of empty string
        let hash = compute_hash256(b"");
        let expected =
            hex::decode("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
                .unwrap();
        assert_eq!(hash.as_slice(), expected.as_slice());
    }

    #[test]
    fn test_compute_hash160() {
        // Known RIPEMD-160 hash of empty string
        let hash = compute_hash160(b"");
        let expected = hex::decode("9c1185a5c5e9fc54612808977ee8f548b2258d31").unwrap();
        assert_eq!(hash.as_slice(), expected.as_slice());
    }

    #[test]
    fn test_checksum() {
        let data = b"test data";
        let cs = checksum(data, 4);
        assert_eq!(cs.len(), 4);

        // Verify it's the last 4 bytes of SHA-256
        let hash = compute_hash256(data);
        assert_eq!(cs, hash[28..32]);
    }

    #[test]
    fn test_pubkey_to_address() {
        let pubkey = [0u8; 33]; // Compressed public key placeholder
        let addr = pubkey_to_address(&pubkey);
        assert_eq!(addr.len(), 20);
    }
}
