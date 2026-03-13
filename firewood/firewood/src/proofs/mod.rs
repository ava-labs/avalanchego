// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Cryptographic proof system for Merkle tries.
//!
//! This module provides a complete proof system for verifying the presence or absence
//! of key-value pairs in Firewood's Merkle trie implementation without requiring access
//! to the entire trie structure. The proof system enables efficient verification of trie
//! state using cryptographic hashes.
//!
//! # Overview
//!
//! Firewood's proof system consists of several key components:
//!
//! - **Single-key proofs** ([`Proof`]): Verify that a specific key-value pair exists
//!   (or doesn't exist) in a trie with a given root hash.
//! - **Range proofs** ([`RangeProof`]): Verify that a contiguous set of key-value pairs
//!   exists within a specific key range.
//! - **Serialization format**: A compact binary format for transmitting proofs over the
//!   network or storing them persistently.
//!
//! # Architecture
//!
//! The proof system is organized into several submodules:
//!
//! - `types`: Core proof types including [`Proof`], [`ProofNode`], [`ProofError`], and
//!   [`ProofCollection`].
//! - `range`: Range proof implementation for verifying multiple consecutive keys.
//! - `header`: Proof format headers and validation.
//! - `reader`: Proof reading and deserialization utilities.
//! - `ser`: Proof serialization implementation (internal).
//! - `de`: Proof deserialization implementation (internal).
//! - `childmap`: Compact bitmap for tracking present children (internal).
//! - `magic`: Magic constants for proof format identification (internal).
//!
//! # Usage
//!
//! For most use cases, import proof types directly from the top level of the crate:
//!
//! ```rust,ignore
//! use firewood::{Proof, ProofNode, RangeProof};
//!
//! // Verify a single key
//! let proof: Proof<Vec<ProofNode>> = /* ... */;
//! proof.verify(b"key", Some(b"value"), &root_hash)?;
//!
//! // Verify a key range
//! let range_proof: RangeProof<Vec<u8>, Vec<u8>, Vec<ProofNode>> = /* ... */;
//! for (key, value) in &range_proof {
//!     // Process key-value pairs
//! }
//! ```
//!
//! # Proof Format
//!
//! Proofs are serialized in a compact binary format that includes:
//!
//! 1. A 32-byte header identifying the proof type, version, hash mode, and branching factor
//! 2. A sequence of proof nodes, each containing:
//!    - The node's key path (variable length)
//!    - The node's value or value hash (if present)
//!    - A bitmap indicating which children are present
//!    - The hash of each present child
//!
//! The serialization format is versioned to allow for future evolution while maintaining
//! backward compatibility with proof verification.

pub(super) mod childmap;
pub(super) mod de;
pub(crate) mod header;
pub(crate) mod range;
pub(crate) mod reader;
pub(super) mod ser;
#[cfg(test)]
mod tests;
pub(crate) mod types;

pub use self::header::InvalidHeader;
pub use self::range::RangeProof;
pub use self::reader::ReadError;
pub use self::types::{
    EmptyProofCollection, Proof, ProofCollection, ProofError, ProofNode, ProofType,
};

pub(super) mod magic {
    //! Magic constants for proof format identification.
    //!
    //! These constants are used in proof headers to identify the proof format,
    //! version, hash mode, and branching factor. They enable proof readers to
    //! quickly validate that a proof is compatible with the current implementation.

    /// Magic header bytes identifying a Firewood proof: `b"fwdproof"`
    pub const PROOF_HEADER: &[u8; 8] = b"fwdproof";

    /// Current proof format version: `0`
    pub const PROOF_VERSION: u8 = 0;

    /// Hash mode identifier for SHA-256 hashing
    #[cfg(not(feature = "ethhash"))]
    pub const HASH_MODE: u8 = 0;

    /// Hash mode identifier for Keccak-256 hashing (Ethereum-compatible)
    #[cfg(feature = "ethhash")]
    pub const HASH_MODE: u8 = 1;

    /// Returns the human-readable name for a hash mode identifier.
    pub const fn hash_mode_name(v: u8) -> &'static str {
        match v {
            0 => "sha256",
            1 => "keccak256",
            _ => "unknown",
        }
    }

    /// Branching factor identifier for branch factor 16
    pub const BRANCH_FACTOR: u8 = 16;

    /// `BatchOp` constants for serialization
    pub const BATCH_PUT: u8 = 0;
    pub const BATCH_DELETE: u8 = 1;
    pub const BATCH_DELETE_RANGE: u8 = 2;
}
