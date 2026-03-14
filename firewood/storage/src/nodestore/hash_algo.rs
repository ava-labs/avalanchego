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

// This is used in `validate_init` regardless of whether test code is being built.
#[inline]
const fn compile_option() -> NodeHashAlgorithm {
    if cfg!(feature = "ethhash") {
        NodeHashAlgorithm::Ethereum
    } else {
        NodeHashAlgorithm::MerkleDB
    }
}

impl NodeHashAlgorithm {
    /// Returns the hash algorithm that matches the compile-time option.
    ///
    /// Note: This function is only available in test builds or when the
    /// `test_utils` feature is enabled (for upstream testing purposes).
    #[must_use]
    #[cfg(any(test, feature = "test_utils"))]
    pub const fn compile_option() -> Self {
        compile_option()
    }

    /// Returns whether this hash algorithm matches the compile-time option.
    #[must_use]
    pub const fn matches_compile_option(self) -> bool {
        match self {
            NodeHashAlgorithm::MerkleDB => !cfg!(feature = "ethhash"),
            NodeHashAlgorithm::Ethereum => cfg!(feature = "ethhash"),
        }
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
                    compile_option().name()
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
