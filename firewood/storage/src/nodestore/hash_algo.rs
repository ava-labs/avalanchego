// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

/// An error indicating that an integer could not be converted into a [`NodeHashAlgorithm`].
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, thiserror::Error)]
#[error("invalid integer for HashMode: {0}; expected 0 for MerkleDB or 1 for EthereumStateTrie")]
pub struct NodeHashAlgorithmTryFromIntError(u64);

/// The hash algorithm used by the node store.
///
/// This must currently match the compile-time option. However, it will eventually
/// be made a runtime option when initializing the node store. Therefore, the
/// option is required when initializing and opening the node store to ensure
/// compatibility and to detect any mismatches.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum NodeHashAlgorithm {
    /// Use the MerkleDB node hashing algorithm, with sha256 as the hash function.
    MerkleDB,
    /// Use the Ethereum state trie node hashing algorithm, with keccak256 as the
    /// hash function.
    Ethereum,
}

impl NodeHashAlgorithm {
    /// Returns the hash algorithm selected by the current build.
    ///
    /// Today this is decided at compile time by the `ethhash` feature; when
    /// the algorithm becomes runtime-selectable (issue #1088), this is the
    /// single function that switches over and every caller picks up the new
    /// behavior automatically.
    #[must_use]
    #[inline]
    pub const fn compile_option() -> Self {
        if cfg!(feature = "ethhash") {
            NodeHashAlgorithm::Ethereum
        } else {
            NodeHashAlgorithm::MerkleDB
        }
    }

    /// Returns whether this hash algorithm matches the one selected by the
    /// current build (see [`Self::compile_option`]).
    #[must_use]
    pub const fn matches_compile_option(self) -> bool {
        self.is_ethereum() == Self::compile_option().is_ethereum()
    }

    /// Returns whether this is the Ethereum hash algorithm (Keccak-256,
    /// account-aware nodes).
    ///
    /// Callers that need to gate ethereum-mode proofs or encoding should
    /// read `NodeHashAlgorithm::compile_option().is_ethereum()` instead of
    /// checking the `ethhash` feature directly. When this enum becomes
    /// runtime-selectable, every caller picks up the new behavior for free.
    #[must_use]
    pub const fn is_ethereum(self) -> bool {
        matches!(self, NodeHashAlgorithm::Ethereum)
    }

    pub(crate) const fn name(self) -> &'static str {
        match self {
            NodeHashAlgorithm::MerkleDB => "MerkleDB",
            NodeHashAlgorithm::Ethereum => "Ethereum",
        }
    }

    // TODO(#1088): remove this after implementing runtime selection of hash algorithms
    pub(crate) fn validate_init(self) -> std::io::Result<()> {
        if self.matches_compile_option() {
            Ok(())
        } else {
            Err(std::io::Error::new(
                std::io::ErrorKind::Unsupported,
                format!(
                    "node store hash algorithm mismatch: want to initialize with {}, \
                     but build option is for {}",
                    self.name(),
                    Self::compile_option().name()
                ),
            ))
        }
    }

    pub(crate) fn validate_open(self, expected: NodeHashAlgorithm) -> std::io::Result<()> {
        if self == expected {
            Ok(())
        } else {
            Err(std::io::Error::new(
                std::io::ErrorKind::Unsupported,
                format!(
                    "node store hash algorithm mismatch: want to open with {}, \
                     but file header indicates {}",
                    expected.name(),
                    self.name()
                ),
            ))
        }
    }
}

impl TryFrom<u64> for NodeHashAlgorithm {
    type Error = NodeHashAlgorithmTryFromIntError;

    fn try_from(value: u64) -> Result<Self, Self::Error> {
        match value {
            0 => Ok(NodeHashAlgorithm::MerkleDB),
            1 => Ok(NodeHashAlgorithm::Ethereum),
            other => Err(NodeHashAlgorithmTryFromIntError(other)),
        }
    }
}
