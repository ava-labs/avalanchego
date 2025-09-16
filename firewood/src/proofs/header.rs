// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::proofs::{magic, proof_type::ProofType};

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

impl From<ProofType> for Header {
    fn from(proof_type: ProofType) -> Self {
        Self {
            magic: *magic::PROOF_HEADER,
            version: magic::PROOF_VERSION,
            hash_mode: magic::HASH_MODE,
            branch_factor: magic::BRANCH_FACTOR,
            proof_type: proof_type as u8,
            _reserved: [0; 20],
        }
    }
}

impl Header {
    /// Validates the header, returning the discovered proof type if valid.
    ///
    /// If `expected_type` is `Some`, the proof type must match (in which case the return
    /// value can be ignored).
    ///
    /// # Errors
    ///
    /// Returns an [`InvalidHeader`] if the header is invalid. See the enum variants for
    /// possible reasons.
    pub(super) fn validate(
        &self,
        expected_type: Option<ProofType>,
    ) -> Result<ProofType, InvalidHeader> {
        if self.magic != *magic::PROOF_HEADER {
            return Err(InvalidHeader::InvalidMagic { found: self.magic });
        }

        if self.version != magic::PROOF_VERSION {
            return Err(InvalidHeader::UnsupportedVersion {
                found: self.version,
            });
        }

        if self.hash_mode != magic::HASH_MODE {
            return Err(InvalidHeader::UnsupportedHashMode {
                found: self.hash_mode,
            });
        }

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
            (Some(found), _) => Ok(found),
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
    /// The proof was encoded for an unsupported hash mode.
    #[error(
        "unsupported hash mode: found {found:02x} ({}); expected {:02x} ({})",
        magic::hash_mode_name(*found),
        magic::HASH_MODE,
        magic::hash_mode_name(magic::HASH_MODE)
    )]
    UnsupportedHashMode {
        /// The flag indicating which hash mode created this proof.
        found: u8,
    },
    /// The proof was encoded for an unsupported branching factor.
    #[error(
        "unsupported branch factor: found {}; expected {}",
        magic::widen_branch_factor(*found),
        magic::widen_branch_factor(magic::BRANCH_FACTOR)
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
