//! Merkle Patricia Trie implementation for Avalanche.
//!
//! This module provides a production-ready MPT implementation with:
//! - Efficient O(log n) lookups, inserts, and deletes
//! - Cryptographic proofs for state verification
//! - Database-backed persistence with caching
//! - Snapshot and rollback support

mod nibbles;
mod node;
mod trie;
mod proof;
mod db;

pub use nibbles::Nibbles;
pub use node::{Node, NodeRef, BranchNode, ExtensionNode, LeafNode};
pub use trie::{Trie, TrieError};
pub use proof::{Proof, ProofError, verify_proof};
pub use db::{TrieDb, MemoryTrieDb, HashKey};

use sha3::{Digest, Keccak256};

/// Hash a value using Keccak256.
pub fn keccak256(data: &[u8]) -> [u8; 32] {
    let mut hasher = Keccak256::new();
    hasher.update(data);
    hasher.finalize().into()
}

/// The empty trie root hash (keccak256 of empty RLP).
pub const EMPTY_ROOT: [u8; 32] = [
    0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6,
    0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0, 0xf8, 0x6e,
    0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0,
    0x01, 0x62, 0x2f, 0xb5, 0xe3, 0x63, 0xb4, 0x21,
];

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_keccak256() {
        let hash = keccak256(b"hello");
        assert_eq!(hash.len(), 32);
    }

    #[test]
    fn test_empty_root() {
        // Empty RLP is 0x80
        let hash = keccak256(&[0x80]);
        assert_eq!(hash, EMPTY_ROOT);
    }
}
