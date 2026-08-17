// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Proof format headers and validation.
//!
//! This module defines the fixed-size header that prefixes all serialized proofs.
//! The header contains metadata about the proof format including version, hash mode,
//! branching factor, and proof type, enabling quick validation before deserialization.

use firewood_storage::NodeHashAlgorithm;

use super::{magic, types::ProofType};

/// A fixed-size header at the beginning of every serialized proof.
///
/// # Format
///
/// - 8 bytes: A magic value to identify the file type. This is `b"fwdproof"`.
/// - 1 byte: The version of the proof format. Currently `0`.
/// - 1 byte: The hash mode used in the proof. Currently `0` for sha256, `1` for
///   keccak256.
/// - 1 byte: The branching factor of the trie. Currently `16` or `0` for `256`.
/// - 1 byte: The type of proof. See [`ProofType`].
/// - 20 bytes: Reserved for future use and to pad the header to 32 bytes. Ignored
///   when reading, and set to zero when writing.
#[derive(Debug, Clone, Copy, bytemuck_derive::Pod, bytemuck_derive::Zeroable)]
#[repr(C)]
pub struct Header {
    pub(super) magic: [u8; 8],
    pub(super) version: u8,
    pub(super) hash_mode: u8,
    pub(super) branch_factor: u8,
    pub(super) proof_type: u8,
    pub(super) _reserved: [u8; 20],
}

const _: () = {
    assert!(size_of::<Header>() == 32);
};

/// Fields resolved from a successfully validated proof header.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(super) struct ValidatedHeader {
    /// The type of proof encoded in the body.
    pub(super) proof_type: ProofType,
    /// The node-hashing algorithm used to encode the proof body.
    pub(super) node_hash_algorithm: NodeHashAlgorithm,
}

impl From<(ProofType, NodeHashAlgorithm)> for Header {
    fn from((proof_type, hash_mode): (ProofType, NodeHashAlgorithm)) -> Self {
        let hash_mode = match hash_mode {
            NodeHashAlgorithm::MerkleDB => magic::MERKLEDB_HASH_MODE,
            NodeHashAlgorithm::Ethereum => magic::ETHEREUM_HASH_MODE,
        };
        Self {
            magic: *magic::PROOF_HEADER,
            version: magic::PROOF_VERSION,
            hash_mode,
            branch_factor: magic::BRANCH_FACTOR,
            proof_type: proof_type as u8,
            _reserved: [0; 20],
        }
    }
}

impl Header {
    /// Validates the header and returns its resolved fields.
    ///
    /// If `expected_type` is `Some`, the proof type must match (in which case the
    /// returned proof type can be ignored). The resolved algorithm is taken from
    /// the self-describing `hash_mode` header byte: any known mode (`0` =
    /// MerkleDB, `1` = Ethereum) is accepted so a single binary can parse either
    /// wire format; only a truly-unknown byte is rejected.
    /// .
    ///
    /// # Errors
    ///
    /// Returns:
    /// - [`InvalidHeader`] if the header is invalid. See the enum variants for
    ///   possible reasons.
    /// - [`InvalidHeader::UnsupportedHashMode`] if the header's `hash_mode` byte is unrecognized.
    pub(super) fn validate(
        &self,
        expected_type: Option<ProofType>,
    ) -> Result<ValidatedHeader, InvalidHeader> {
        if self.magic != *magic::PROOF_HEADER {
            return Err(InvalidHeader::InvalidMagic { found: self.magic });
        }

        if self.version != magic::PROOF_VERSION {
            return Err(InvalidHeader::UnsupportedVersion {
                found: self.version,
            });
        }

        // Resolve the self-describing hash-mode byte into an algorithm.
        let algorithm = NodeHashAlgorithm::try_from(u64::from(self.hash_mode)).map_err(|_| {
            InvalidHeader::UnsupportedHashMode {
                found: self.hash_mode,
            }
        })?;

        if self.branch_factor != magic::BRANCH_FACTOR {
            return Err(InvalidHeader::UnsupportedBranchFactor {
                found: self.branch_factor,
            });
        }

        match (ProofType::new(self.proof_type), expected_type) {
            (None, expected) => Err(InvalidHeader::InvalidProofType {
                found: self.proof_type,
                expected,
            }),
            (Some(found), Some(expected)) if found != expected => {
                Err(InvalidHeader::InvalidProofType {
                    found: self.proof_type,
                    expected: Some(expected),
                })
            }
            (Some(found), _) => Ok(ValidatedHeader {
                proof_type: found,
                node_hash_algorithm: algorithm,
            }),
        }
    }
}

/// Error when validating the header.
#[derive(Debug, thiserror::Error)]
pub enum InvalidHeader {
    /// Expected a static byte string to prefix the input.
    #[error("invalid magic: found {:016x}; expected {:016x}", u64::from_be_bytes(*found), u64::from_be_bytes(*magic::PROOF_HEADER))]
    InvalidMagic {
        /// The actual bytes found in place where the magic header was expected.
        found: [u8; 8],
    },
    /// The proof was encoded with an unrecognized version.
    #[error(
        "unsupported proof version: found {found:02x}; expected {:02x}",
        magic::PROOF_VERSION
    )]
    UnsupportedVersion {
        /// The version byte found instead of a supported version.
        found: u8,
    },
    /// The proof was encoded for an unknown hash mode (a byte that maps to no
    /// known [`NodeHashAlgorithm`]).
    #[error(
        "unsupported hash mode: found {found:02x} ({}); expected {:02x} (sha256) or {:02x} (keccak256)",
        magic::hash_mode_name(*found),
        0u8,
        1u8,
    )]
    UnsupportedHashMode {
        /// The flag indicating which hash mode created this proof.
        found: u8,
    },
    /// The proof was encoded for an unsupported branching factor.
    #[error(
        "unsupported branch factor: found {}; expected {}",
        *found,
        magic::BRANCH_FACTOR,
    )]
    UnsupportedBranchFactor {
        /// The actual branch factor encoded in the header.
        found: u8,
    },
    /// The header indicated an unexpected or invalid proof type.
    #[error(
        "invalid proof type: found {found:02x} ({}); expected {}",
        ProofType::new(*found).map_or("unknown", ProofType::name),
        DisplayProofType(*expected),
    )]
    InvalidProofType {
        /// The flag from the header.
        found: u8,
        /// The expected type, if any. Otherwise any type was expected and we
        /// found an unknown value.
        expected: Option<ProofType>,
    },
}

struct DisplayProofType(Option<ProofType>);

impl std::fmt::Display for DisplayProofType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self.0 {
            Some(pt) => write!(f, "{:02x} ({})", pt as u8, pt.name()),
            None => write!(f, "one of 0x00 (single), 0x01 (range), 0x02 (change)"),
        }
    }
}
