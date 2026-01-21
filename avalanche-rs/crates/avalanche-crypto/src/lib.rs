//! Cryptographic primitives for Avalanche.
//!
//! This crate provides real cryptographic implementations:
//! - secp256k1 ECDSA for transaction signing
//! - BLS signatures for validator signing
//! - Key derivation and management

pub mod bls;
pub mod secp256k1;
pub mod keychain;

pub use bls::{BlsPublicKey, BlsSecretKey, BlsSignature};
pub use secp256k1::{PrivateKey, PublicKey, Signature, RecoverableSignature};
pub use keychain::{Keychain, KeychainError};

use thiserror::Error;

/// Cryptographic errors.
#[derive(Debug, Error)]
pub enum CryptoError {
    #[error("invalid key: {0}")]
    InvalidKey(String),
    #[error("invalid signature: {0}")]
    InvalidSignature(String),
    #[error("signing failed: {0}")]
    SigningFailed(String),
    #[error("verification failed: {0}")]
    VerificationFailed(String),
    #[error("key derivation failed: {0}")]
    DerivationFailed(String),
}

pub type Result<T> = std::result::Result<T, CryptoError>;
