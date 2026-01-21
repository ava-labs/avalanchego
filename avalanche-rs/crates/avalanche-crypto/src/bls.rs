//! BLS signature implementation for Avalanche validators.
//!
//! Uses the blst crate for BLS12-381 signatures.

use blst::min_pk::{
    SecretKey as BlstSecretKey,
    PublicKey as BlstPublicKey,
    Signature as BlstSignature,
    AggregateSignature,
    AggregatePublicKey,
};
use blst::BLST_ERROR;
use zeroize::Zeroize;

use crate::{CryptoError, Result};

/// BLS secret key size (32 bytes).
pub const BLS_SECRET_KEY_SIZE: usize = 32;
/// BLS public key size (48 bytes, compressed G1 point).
pub const BLS_PUBLIC_KEY_SIZE: usize = 48;
/// BLS signature size (96 bytes, compressed G2 point).
pub const BLS_SIGNATURE_SIZE: usize = 96;

/// Domain separation tag for Avalanche BLS signatures.
const DST: &[u8] = b"BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_";

/// A BLS secret key.
#[derive(Clone, Zeroize)]
#[zeroize(drop)]
pub struct BlsSecretKey {
    #[zeroize(skip)]
    inner: BlstSecretKey,
}

impl BlsSecretKey {
    /// Generates a new random BLS secret key.
    pub fn generate() -> Self {
        let mut ikm = [0u8; 32];
        rand::RngCore::fill_bytes(&mut rand::thread_rng(), &mut ikm);

        let inner = BlstSecretKey::key_gen(&ikm, &[])
            .expect("key generation should not fail with valid input");

        ikm.zeroize();

        Self { inner }
    }

    /// Creates a BLS secret key from raw bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != BLS_SECRET_KEY_SIZE {
            return Err(CryptoError::InvalidKey(format!(
                "expected {} bytes, got {}",
                BLS_SECRET_KEY_SIZE,
                bytes.len()
            )));
        }

        let inner = BlstSecretKey::from_bytes(bytes)
            .map_err(|_| CryptoError::InvalidKey("invalid BLS secret key".to_string()))?;

        Ok(Self { inner })
    }

    /// Returns the raw bytes of the secret key.
    pub fn to_bytes(&self) -> [u8; BLS_SECRET_KEY_SIZE] {
        self.inner.to_bytes()
    }

    /// Returns the corresponding public key.
    pub fn public_key(&self) -> BlsPublicKey {
        BlsPublicKey {
            inner: self.inner.sk_to_pk(),
        }
    }

    /// Signs a message.
    pub fn sign(&self, message: &[u8]) -> BlsSignature {
        let sig = self.inner.sign(message, DST, &[]);
        BlsSignature { inner: sig }
    }
}

impl std::fmt::Debug for BlsSecretKey {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BlsSecretKey")
            .field("public_key", &self.public_key())
            .finish()
    }
}

/// A BLS public key.
#[derive(Clone)]
pub struct BlsPublicKey {
    inner: BlstPublicKey,
}

impl BlsPublicKey {
    /// Creates a public key from compressed bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != BLS_PUBLIC_KEY_SIZE {
            return Err(CryptoError::InvalidKey(format!(
                "expected {} bytes, got {}",
                BLS_PUBLIC_KEY_SIZE,
                bytes.len()
            )));
        }

        let inner = BlstPublicKey::from_bytes(bytes)
            .map_err(|_| CryptoError::InvalidKey("invalid BLS public key".to_string()))?;

        Ok(Self { inner })
    }

    /// Returns the compressed bytes of the public key.
    pub fn to_bytes(&self) -> [u8; BLS_PUBLIC_KEY_SIZE] {
        self.inner.to_bytes()
    }

    /// Returns the hex representation of the public key.
    pub fn to_hex(&self) -> String {
        hex::encode(self.to_bytes())
    }

    /// Verifies a signature against a message.
    pub fn verify(&self, message: &[u8], signature: &BlsSignature) -> Result<()> {
        let result = signature.inner.verify(true, message, DST, &[], &self.inner, true);

        if result == BLST_ERROR::BLST_SUCCESS {
            Ok(())
        } else {
            Err(CryptoError::VerificationFailed(format!(
                "BLS verification failed: {:?}",
                result
            )))
        }
    }

    /// Aggregates multiple public keys into one.
    pub fn aggregate(keys: &[BlsPublicKey]) -> Result<Self> {
        if keys.is_empty() {
            return Err(CryptoError::InvalidKey("cannot aggregate empty key list".to_string()));
        }

        let refs: Vec<&BlstPublicKey> = keys.iter().map(|k| &k.inner).collect();
        let agg = AggregatePublicKey::aggregate(&refs, true)
            .map_err(|e| CryptoError::InvalidKey(format!("aggregation failed: {:?}", e)))?;

        Ok(Self {
            inner: agg.to_public_key(),
        })
    }
}

impl std::fmt::Debug for BlsPublicKey {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BlsPublicKey")
            .field("hex", &self.to_hex())
            .finish()
    }
}

impl PartialEq for BlsPublicKey {
    fn eq(&self, other: &Self) -> bool {
        self.to_bytes() == other.to_bytes()
    }
}

impl Eq for BlsPublicKey {}

impl std::hash::Hash for BlsPublicKey {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.to_bytes().hash(state);
    }
}

/// A BLS signature.
#[derive(Clone)]
pub struct BlsSignature {
    inner: BlstSignature,
}

impl BlsSignature {
    /// Creates a signature from compressed bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        if bytes.len() != BLS_SIGNATURE_SIZE {
            return Err(CryptoError::InvalidSignature(format!(
                "expected {} bytes, got {}",
                BLS_SIGNATURE_SIZE,
                bytes.len()
            )));
        }

        let inner = BlstSignature::from_bytes(bytes)
            .map_err(|_| CryptoError::InvalidSignature("invalid BLS signature".to_string()))?;

        Ok(Self { inner })
    }

    /// Returns the compressed bytes of the signature.
    pub fn to_bytes(&self) -> [u8; BLS_SIGNATURE_SIZE] {
        self.inner.to_bytes()
    }

    /// Returns the hex representation of the signature.
    pub fn to_hex(&self) -> String {
        hex::encode(self.to_bytes())
    }

    /// Aggregates multiple signatures into one.
    pub fn aggregate(signatures: &[BlsSignature]) -> Result<Self> {
        if signatures.is_empty() {
            return Err(CryptoError::InvalidSignature(
                "cannot aggregate empty signature list".to_string()
            ));
        }

        let refs: Vec<&BlstSignature> = signatures.iter().map(|s| &s.inner).collect();
        let agg = AggregateSignature::aggregate(&refs, true)
            .map_err(|e| CryptoError::InvalidSignature(format!("aggregation failed: {:?}", e)))?;

        Ok(Self {
            inner: agg.to_signature(),
        })
    }

    /// Verifies an aggregated signature against multiple public keys and messages.
    pub fn verify_aggregate(
        &self,
        public_keys: &[BlsPublicKey],
        messages: &[&[u8]],
    ) -> Result<()> {
        if public_keys.len() != messages.len() {
            return Err(CryptoError::VerificationFailed(
                "mismatched public keys and messages".to_string()
            ));
        }

        let pk_refs: Vec<&BlstPublicKey> = public_keys.iter().map(|k| &k.inner).collect();

        let result = self.inner.aggregate_verify(true, messages, DST, &pk_refs, true);

        if result == BLST_ERROR::BLST_SUCCESS {
            Ok(())
        } else {
            Err(CryptoError::VerificationFailed(format!(
                "aggregate BLS verification failed: {:?}",
                result
            )))
        }
    }
}

impl std::fmt::Debug for BlsSignature {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BlsSignature")
            .field("hex", &self.to_hex())
            .finish()
    }
}

impl PartialEq for BlsSignature {
    fn eq(&self, other: &Self) -> bool {
        self.to_bytes() == other.to_bytes()
    }
}

impl Eq for BlsSignature {}

/// Proof of possession for BLS public key.
#[derive(Clone, Debug)]
pub struct ProofOfPossession {
    /// The signature proving possession of the secret key.
    pub signature: BlsSignature,
}

impl ProofOfPossession {
    /// Creates a proof of possession for a secret key.
    pub fn create(secret_key: &BlsSecretKey) -> Self {
        let public_key = secret_key.public_key();
        let message = public_key.to_bytes();
        let signature = secret_key.sign(&message);
        Self { signature }
    }

    /// Verifies the proof of possession for a public key.
    pub fn verify(&self, public_key: &BlsPublicKey) -> Result<()> {
        let message = public_key.to_bytes();
        public_key.verify(&message, &self.signature)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_bls_key_generation() {
        let secret_key = BlsSecretKey::generate();
        let public_key = secret_key.public_key();

        assert_eq!(secret_key.to_bytes().len(), BLS_SECRET_KEY_SIZE);
        assert_eq!(public_key.to_bytes().len(), BLS_PUBLIC_KEY_SIZE);
    }

    #[test]
    fn test_bls_sign_and_verify() {
        let secret_key = BlsSecretKey::generate();
        let public_key = secret_key.public_key();
        let message = b"Hello, BLS!";

        let signature = secret_key.sign(message);
        assert!(public_key.verify(message, &signature).is_ok());

        // Wrong message should fail
        let wrong_message = b"Wrong message";
        assert!(public_key.verify(wrong_message, &signature).is_err());
    }

    #[test]
    fn test_bls_serialization() {
        let secret_key = BlsSecretKey::generate();
        let bytes = secret_key.to_bytes();
        let restored = BlsSecretKey::from_bytes(&bytes).unwrap();

        assert_eq!(
            secret_key.public_key().to_bytes(),
            restored.public_key().to_bytes()
        );
    }

    #[test]
    fn test_bls_signature_aggregation() {
        let sk1 = BlsSecretKey::generate();
        let sk2 = BlsSecretKey::generate();
        let sk3 = BlsSecretKey::generate();

        let pk1 = sk1.public_key();
        let pk2 = sk2.public_key();
        let pk3 = sk3.public_key();

        let msg1 = b"message 1";
        let msg2 = b"message 2";
        let msg3 = b"message 3";

        let sig1 = sk1.sign(msg1);
        let sig2 = sk2.sign(msg2);
        let sig3 = sk3.sign(msg3);

        // Aggregate signatures
        let agg_sig = BlsSignature::aggregate(&[sig1, sig2, sig3]).unwrap();

        // Verify aggregate
        let messages: Vec<&[u8]> = vec![msg1, msg2, msg3];
        assert!(agg_sig.verify_aggregate(&[pk1, pk2, pk3], &messages).is_ok());
    }

    #[test]
    fn test_bls_public_key_aggregation() {
        let sk1 = BlsSecretKey::generate();
        let sk2 = BlsSecretKey::generate();

        let pk1 = sk1.public_key();
        let pk2 = sk2.public_key();

        let agg_pk = BlsPublicKey::aggregate(&[pk1, pk2]).unwrap();
        assert_eq!(agg_pk.to_bytes().len(), BLS_PUBLIC_KEY_SIZE);
    }

    #[test]
    fn test_proof_of_possession() {
        let secret_key = BlsSecretKey::generate();
        let public_key = secret_key.public_key();

        let pop = ProofOfPossession::create(&secret_key);
        assert!(pop.verify(&public_key).is_ok());

        // Different public key should fail
        let other_sk = BlsSecretKey::generate();
        let other_pk = other_sk.public_key();
        assert!(pop.verify(&other_pk).is_err());
    }
}
