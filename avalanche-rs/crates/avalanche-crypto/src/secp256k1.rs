//! secp256k1 ECDSA implementation for Avalanche.
//!
//! Uses the k256 crate for actual cryptographic operations.

use k256::{
    ecdsa::{
        signature::{Signer, Verifier},
        SigningKey, VerifyingKey,
        Signature as K256Signature,
        RecoveryId,
    },
    SecretKey,
};
use sha2::{Sha256, Digest};
use sha3::Keccak256;

use crate::{CryptoError, Result};

/// Compressed public key size (33 bytes).
pub const COMPRESSED_PUBLIC_KEY_SIZE: usize = 33;
/// Uncompressed public key size (65 bytes).
pub const UNCOMPRESSED_PUBLIC_KEY_SIZE: usize = 65;
/// Private key size (32 bytes).
pub const PRIVATE_KEY_SIZE: usize = 32;
/// Signature size (64 bytes, r || s).
pub const SIGNATURE_SIZE: usize = 64;
/// Recoverable signature size (65 bytes, r || s || v).
pub const RECOVERABLE_SIGNATURE_SIZE: usize = 65;

/// A secp256k1 private key.
///
/// Note: SigningKey handles its own secure memory (zeroize on drop).
#[derive(Clone)]
pub struct PrivateKey {
    inner: SigningKey,
}

impl PrivateKey {
    /// Generates a new random private key.
    pub fn generate() -> Self {
        let signing_key = SigningKey::random(&mut rand::thread_rng());
        Self { inner: signing_key }
    }

    /// Creates a private key from raw bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != PRIVATE_KEY_SIZE {
            return Err(CryptoError::InvalidKey(format!(
                "expected {} bytes, got {}",
                PRIVATE_KEY_SIZE,
                bytes.len()
            )));
        }

        let secret_key = SecretKey::from_slice(bytes)
            .map_err(|e| CryptoError::InvalidKey(e.to_string()))?;
        let signing_key = SigningKey::from(secret_key);
        Ok(Self { inner: signing_key })
    }

    /// Creates a private key from a hex string.
    pub fn from_hex(hex: &str) -> Result<Self> {
        let hex = hex.strip_prefix("0x").unwrap_or(hex);
        let bytes = hex::decode(hex)
            .map_err(|e| CryptoError::InvalidKey(e.to_string()))?;
        Self::from_bytes(&bytes)
    }

    /// Returns the raw bytes of the private key.
    pub fn to_bytes(&self) -> [u8; PRIVATE_KEY_SIZE] {
        self.inner.to_bytes().into()
    }

    /// Returns the hex representation of the private key.
    pub fn to_hex(&self) -> String {
        hex::encode(self.to_bytes())
    }

    /// Returns the corresponding public key.
    pub fn public_key(&self) -> PublicKey {
        PublicKey {
            inner: *self.inner.verifying_key(),
        }
    }

    /// Signs a message (hashes it first with SHA-256).
    pub fn sign(&self, message: &[u8]) -> Result<Signature> {
        let hash = Sha256::digest(message);
        self.sign_hash(&hash)
    }

    /// Signs a pre-hashed message.
    pub fn sign_hash(&self, hash: &[u8]) -> Result<Signature> {
        let signature: K256Signature = self.inner
            .try_sign(hash)
            .map_err(|e| CryptoError::SigningFailed(e.to_string()))?;
        Ok(Signature { inner: signature })
    }

    /// Signs a message and returns a recoverable signature.
    pub fn sign_recoverable(&self, message: &[u8]) -> Result<RecoverableSignature> {
        let hash = Sha256::digest(message);
        self.sign_hash_recoverable(&hash)
    }

    /// Signs a pre-hashed message and returns a recoverable signature.
    pub fn sign_hash_recoverable(&self, hash: &[u8]) -> Result<RecoverableSignature> {
        let (signature, recovery_id) = self.inner
            .sign_prehash_recoverable(hash)
            .map_err(|e| CryptoError::SigningFailed(e.to_string()))?;

        Ok(RecoverableSignature {
            signature: Signature { inner: signature },
            recovery_id: recovery_id.to_byte(),
        })
    }
}

impl std::fmt::Debug for PrivateKey {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PrivateKey")
            .field("public_key", &self.public_key().to_hex())
            .finish()
    }
}

/// A secp256k1 public key.
#[derive(Clone, Copy, PartialEq, Eq)]
pub struct PublicKey {
    inner: VerifyingKey,
}

impl PublicKey {
    /// Creates a public key from compressed bytes (33 bytes).
    pub fn from_compressed(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != COMPRESSED_PUBLIC_KEY_SIZE {
            return Err(CryptoError::InvalidKey(format!(
                "expected {} bytes, got {}",
                COMPRESSED_PUBLIC_KEY_SIZE,
                bytes.len()
            )));
        }

        let verifying_key = VerifyingKey::from_sec1_bytes(bytes)
            .map_err(|e| CryptoError::InvalidKey(e.to_string()))?;
        Ok(Self { inner: verifying_key })
    }

    /// Creates a public key from uncompressed bytes (65 bytes).
    pub fn from_uncompressed(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != UNCOMPRESSED_PUBLIC_KEY_SIZE {
            return Err(CryptoError::InvalidKey(format!(
                "expected {} bytes, got {}",
                UNCOMPRESSED_PUBLIC_KEY_SIZE,
                bytes.len()
            )));
        }

        let verifying_key = VerifyingKey::from_sec1_bytes(bytes)
            .map_err(|e| CryptoError::InvalidKey(e.to_string()))?;
        Ok(Self { inner: verifying_key })
    }

    /// Creates a public key from a hex string.
    pub fn from_hex(hex: &str) -> Result<Self> {
        let hex = hex.strip_prefix("0x").unwrap_or(hex);
        let bytes = hex::decode(hex)
            .map_err(|e| CryptoError::InvalidKey(e.to_string()))?;

        match bytes.len() {
            COMPRESSED_PUBLIC_KEY_SIZE => Self::from_compressed(&bytes),
            UNCOMPRESSED_PUBLIC_KEY_SIZE => Self::from_uncompressed(&bytes),
            _ => Err(CryptoError::InvalidKey(format!(
                "invalid public key length: {}",
                bytes.len()
            ))),
        }
    }

    /// Returns the compressed bytes of the public key (33 bytes).
    pub fn to_compressed(&self) -> [u8; COMPRESSED_PUBLIC_KEY_SIZE] {
        let point = self.inner.to_encoded_point(true);
        let bytes = point.as_bytes();
        let mut result = [0u8; COMPRESSED_PUBLIC_KEY_SIZE];
        result.copy_from_slice(bytes);
        result
    }

    /// Returns the uncompressed bytes of the public key (65 bytes).
    pub fn to_uncompressed(&self) -> [u8; UNCOMPRESSED_PUBLIC_KEY_SIZE] {
        let point = self.inner.to_encoded_point(false);
        let bytes = point.as_bytes();
        let mut result = [0u8; UNCOMPRESSED_PUBLIC_KEY_SIZE];
        result.copy_from_slice(bytes);
        result
    }

    /// Returns the hex representation of the compressed public key.
    pub fn to_hex(&self) -> String {
        hex::encode(self.to_compressed())
    }

    /// Computes the Avalanche address (ripemd160(sha256(compressed_pubkey))).
    pub fn to_address(&self) -> [u8; 20] {
        use ripemd::Ripemd160;

        let compressed = self.to_compressed();
        let sha256_hash = Sha256::digest(&compressed);
        let ripemd_hash = Ripemd160::digest(&sha256_hash);

        let mut address = [0u8; 20];
        address.copy_from_slice(&ripemd_hash);
        address
    }

    /// Computes the Ethereum address (last 20 bytes of keccak256(uncompressed_pubkey[1..])).
    pub fn to_eth_address(&self) -> [u8; 20] {
        let uncompressed = self.to_uncompressed();
        // Skip the 0x04 prefix
        let keccak_hash = Keccak256::digest(&uncompressed[1..]);

        let mut address = [0u8; 20];
        address.copy_from_slice(&keccak_hash[12..]);
        address
    }

    /// Verifies a signature against a message.
    pub fn verify(&self, message: &[u8], signature: &Signature) -> Result<()> {
        let hash = Sha256::digest(message);
        self.verify_hash(&hash, signature)
    }

    /// Verifies a signature against a pre-hashed message.
    pub fn verify_hash(&self, hash: &[u8], signature: &Signature) -> Result<()> {
        self.inner
            .verify(hash, &signature.inner)
            .map_err(|e| CryptoError::VerificationFailed(e.to_string()))
    }
}

impl std::fmt::Debug for PublicKey {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PublicKey")
            .field("hex", &self.to_hex())
            .finish()
    }
}

impl std::hash::Hash for PublicKey {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.to_compressed().hash(state);
    }
}

/// A secp256k1 ECDSA signature.
#[derive(Clone, Copy, PartialEq, Eq)]
pub struct Signature {
    inner: K256Signature,
}

impl Signature {
    /// Creates a signature from raw bytes (64 bytes, r || s).
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != SIGNATURE_SIZE {
            return Err(CryptoError::InvalidSignature(format!(
                "expected {} bytes, got {}",
                SIGNATURE_SIZE,
                bytes.len()
            )));
        }

        let signature = K256Signature::try_from(bytes)
            .map_err(|e| CryptoError::InvalidSignature(e.to_string()))?;
        Ok(Self { inner: signature })
    }

    /// Creates a signature from a hex string.
    pub fn from_hex(hex: &str) -> Result<Self> {
        let hex = hex.strip_prefix("0x").unwrap_or(hex);
        let bytes = hex::decode(hex)
            .map_err(|e| CryptoError::InvalidSignature(e.to_string()))?;
        Self::from_bytes(&bytes)
    }

    /// Returns the raw bytes of the signature (64 bytes).
    pub fn to_bytes(&self) -> [u8; SIGNATURE_SIZE] {
        self.inner.to_bytes().into()
    }

    /// Returns the hex representation of the signature.
    pub fn to_hex(&self) -> String {
        hex::encode(self.to_bytes())
    }

    /// Returns the r component of the signature.
    pub fn r(&self) -> [u8; 32] {
        let bytes = self.to_bytes();
        let mut r = [0u8; 32];
        r.copy_from_slice(&bytes[..32]);
        r
    }

    /// Returns the s component of the signature.
    pub fn s(&self) -> [u8; 32] {
        let bytes = self.to_bytes();
        let mut s = [0u8; 32];
        s.copy_from_slice(&bytes[32..]);
        s
    }
}

impl std::fmt::Debug for Signature {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Signature")
            .field("hex", &self.to_hex())
            .finish()
    }
}

/// A recoverable secp256k1 ECDSA signature (includes recovery ID).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct RecoverableSignature {
    /// The underlying signature.
    pub signature: Signature,
    /// The recovery ID (0 or 1).
    pub recovery_id: u8,
}

impl RecoverableSignature {
    /// Creates a recoverable signature from raw bytes (65 bytes, r || s || v).
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != RECOVERABLE_SIGNATURE_SIZE {
            return Err(CryptoError::InvalidSignature(format!(
                "expected {} bytes, got {}",
                RECOVERABLE_SIGNATURE_SIZE,
                bytes.len()
            )));
        }

        let signature = Signature::from_bytes(&bytes[..64])?;
        let recovery_id = bytes[64];

        if recovery_id > 3 {
            return Err(CryptoError::InvalidSignature(format!(
                "invalid recovery ID: {}",
                recovery_id
            )));
        }

        Ok(Self {
            signature,
            recovery_id,
        })
    }

    /// Returns the raw bytes of the recoverable signature (65 bytes).
    pub fn to_bytes(&self) -> [u8; RECOVERABLE_SIGNATURE_SIZE] {
        let mut bytes = [0u8; RECOVERABLE_SIGNATURE_SIZE];
        bytes[..64].copy_from_slice(&self.signature.to_bytes());
        bytes[64] = self.recovery_id;
        bytes
    }

    /// Recovers the public key from the signature and message.
    pub fn recover(&self, message: &[u8]) -> Result<PublicKey> {
        let hash = Sha256::digest(message);
        self.recover_from_hash(&hash)
    }

    /// Recovers the public key from the signature and pre-hashed message.
    pub fn recover_from_hash(&self, hash: &[u8]) -> Result<PublicKey> {
        let recovery_id = RecoveryId::try_from(self.recovery_id)
            .map_err(|e| CryptoError::InvalidSignature(e.to_string()))?;

        let verifying_key = VerifyingKey::recover_from_prehash(
            hash,
            &self.signature.inner,
            recovery_id,
        ).map_err(|e| CryptoError::VerificationFailed(e.to_string()))?;

        Ok(PublicKey { inner: verifying_key })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_key_generation() {
        let private_key = PrivateKey::generate();
        let public_key = private_key.public_key();

        assert_eq!(private_key.to_bytes().len(), PRIVATE_KEY_SIZE);
        assert_eq!(public_key.to_compressed().len(), COMPRESSED_PUBLIC_KEY_SIZE);
        assert_eq!(public_key.to_uncompressed().len(), UNCOMPRESSED_PUBLIC_KEY_SIZE);
    }

    #[test]
    fn test_key_from_bytes() {
        let private_key = PrivateKey::generate();
        let bytes = private_key.to_bytes();

        let restored = PrivateKey::from_bytes(&bytes).unwrap();
        assert_eq!(
            private_key.public_key().to_compressed(),
            restored.public_key().to_compressed()
        );
    }

    #[test]
    fn test_sign_and_verify() {
        let private_key = PrivateKey::generate();
        let public_key = private_key.public_key();
        let message = b"Hello, Avalanche!";

        let signature = private_key.sign(message).unwrap();
        assert!(public_key.verify(message, &signature).is_ok());

        // Wrong message should fail
        let wrong_message = b"Wrong message";
        assert!(public_key.verify(wrong_message, &signature).is_err());
    }

    #[test]
    fn test_recoverable_signature() {
        let private_key = PrivateKey::generate();
        let public_key = private_key.public_key();
        let message = b"Recoverable signature test";

        let recoverable = private_key.sign_recoverable(message).unwrap();
        let recovered = recoverable.recover(message).unwrap();

        assert_eq!(public_key.to_compressed(), recovered.to_compressed());
    }

    #[test]
    fn test_address_derivation() {
        let private_key = PrivateKey::generate();
        let public_key = private_key.public_key();

        let avax_address = public_key.to_address();
        let eth_address = public_key.to_eth_address();

        assert_eq!(avax_address.len(), 20);
        assert_eq!(eth_address.len(), 20);
        // Addresses should be different (different hash functions)
        assert_ne!(avax_address, eth_address);
    }

    #[test]
    fn test_known_vector() {
        // Test with a known private key
        let private_key = PrivateKey::from_hex(
            "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        ).unwrap();

        let public_key = private_key.public_key();
        let message = b"test";

        let signature = private_key.sign(message).unwrap();
        assert!(public_key.verify(message, &signature).is_ok());
    }

    #[test]
    fn test_signature_serialization() {
        let private_key = PrivateKey::generate();
        let message = b"Serialize me";

        let signature = private_key.sign(message).unwrap();
        let bytes = signature.to_bytes();
        let restored = Signature::from_bytes(&bytes).unwrap();

        assert_eq!(signature.to_bytes(), restored.to_bytes());
    }
}
