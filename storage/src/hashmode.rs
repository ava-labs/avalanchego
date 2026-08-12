// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Runtime-selectable node-hashing scheme.
//!
//! [`HashMode`] is the trait that will eventually carry every behavior that
//! differs between the MerkleDB (SHA-256) and Ethereum (Keccak-256) hashing
//! schemes, implemented by the two zero-sized types [`MerkleDbHash`] and
//! [`EthHash`].

use crate::{NodeHashAlgorithm, Path, TrieHash};

/// The node-hashing scheme, implemented by [`MerkleDbHash`] and [`EthHash`].
///
/// The practical set of schemes stays bounded by the [`NodeHashAlgorithm`]
/// discriminants persisted in file headers.
pub trait HashMode: Default + std::fmt::Debug + Send + Sync + 'static {
    /// The persisted algorithm identity for this scheme. Stamped into file
    /// headers and proof headers.
    const ALGORITHM: NodeHashAlgorithm;

    /// The root hash of an empty trie.
    fn default_root_hash() -> Option<TrieHash>;

    /// Whether `key` is a structurally valid full trie key under this scheme.
    fn is_valid_key(key: &Path) -> bool;
}

/// MerkleDB hashing: SHA-256 over a length-prefixed node encoding.
#[derive(Debug, Default)]
pub struct MerkleDbHash;

/// Ethereum hashing: Keccak-256 over the Ethereum Modified Merkle Patricia
/// Trie wire format.
#[derive(Debug, Default)]
pub struct EthHash;

/// The hash mode selected by the `ethhash` feature's compile-time state:
/// `EthHash` when enabled, `MerkleDbHash` otherwise.
#[cfg(feature = "ethhash")]
pub type DefaultHashMode = EthHash;

/// The hash mode selected by the `ethhash` feature's compile-time state:
/// `EthHash` when enabled, `MerkleDbHash` otherwise.
#[cfg(not(feature = "ethhash"))]
pub type DefaultHashMode = MerkleDbHash;

#[cfg(test)]
mod tests {
    use super::*;

    fn path_of_len(len: usize) -> Path {
        Path((0..len).map(|_| 0u8).collect())
    }

    #[test]
    fn default_hash_mode_matches_compile_option() {
        assert_eq!(
            DefaultHashMode::ALGORITHM,
            NodeHashAlgorithm::compile_option()
        );
    }

    #[test]
    #[cfg(feature = "ethhash")]
    fn default_root_hash() {
        assert!(DefaultHashMode::default_root_hash().is_some());
    }

    #[test]
    #[cfg(not(feature = "ethhash"))]
    fn default_root_hash() {
        assert!(DefaultHashMode::default_root_hash().is_none());
    }

    #[test]
    #[cfg(feature = "ethhash")]
    fn is_valid_key() {
        assert!(DefaultHashMode::is_valid_key(&path_of_len(64)));
        assert!(DefaultHashMode::is_valid_key(&path_of_len(128)));
        assert!(!DefaultHashMode::is_valid_key(&path_of_len(66)));
        assert!(!DefaultHashMode::is_valid_key(&path_of_len(63)));
    }

    #[test]
    #[cfg(not(feature = "ethhash"))]
    fn is_valid_key() {
        assert!(DefaultHashMode::is_valid_key(&path_of_len(64)));
        assert!(DefaultHashMode::is_valid_key(&path_of_len(2)));
        assert!(!DefaultHashMode::is_valid_key(&path_of_len(3)));
    }
}
