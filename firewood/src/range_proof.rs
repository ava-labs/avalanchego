// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::proof::{Proof, ProofCollection};

/// A range proof is a cryptographic proof that demonstrates a contiguous set of key-value pairs
/// exists within a Merkle trie with a given root hash.
///
/// Range proofs are used to efficiently prove the presence (or absence) of multiple consecutive
/// keys in a trie without revealing the entire trie structure. They consist of:
/// - A start proof: proves the existence of the first key in the range (or the nearest key before it)
/// - An end proof: proves the existence of the last key in the range (or the nearest key after it)
/// - The actual key-value pairs within the range
///
/// This allows verification that:
/// 1. The provided key-value pairs are indeed part of the trie
/// 2. There are no other keys between the start and end of the range
/// 3. The trie has the claimed root hash
///
/// Range proofs are particularly useful in blockchain contexts for:
/// - State synchronization between nodes
/// - Light client verification
/// - Efficient auditing of specific key ranges
#[derive(Debug)]
pub struct RangeProof<K, V, H> {
    start_proof: Proof<H>,
    end_proof: Proof<H>,
    key_values: Box<[(K, V)]>,
}

impl<K, V, H> RangeProof<K, V, H>
where
    K: AsRef<[u8]>,
    V: AsRef<[u8]>,
    H: ProofCollection,
{
    /// Create a new range proof with the given start and end proofs
    /// and the key-value pairs that are included in the proof.
    ///
    /// # Parameters
    ///
    /// * `start_proof` - A Merkle proof for the first key in the range, or if the range
    ///   starts before any existing key, a proof for the nearest key that comes after
    ///   the start of the range. This proof establishes the lower boundary of the range
    ///   and ensures no keys exist between the range start and the first included key.
    ///   May be empty if proving from the very beginning of the trie.
    ///
    /// * `end_proof` - A Merkle proof for the last key in the range, or if the range
    ///   extends beyond all existing keys, a proof for the nearest key that comes before
    ///   the end of the range. This proof establishes the upper boundary of the range
    ///   and ensures no keys exist between the last included key and the range end.
    ///   May be empty if proving to the very end of the trie.
    ///
    /// * `key_values` - The actual key-value pairs that exist within the proven range.
    ///   These are the consecutive entries from the trie that fall within the boundaries
    ///   established by the start and end proofs. The keys should be in lexicographic
    ///   order as they appear in the trie. May be empty if proving the absence of keys
    ///   in a range.
    #[must_use]
    pub const fn new(
        start_proof: Proof<H>,
        end_proof: Proof<H>,
        key_values: Box<[(K, V)]>,
    ) -> Self {
        Self {
            start_proof,
            end_proof,
            key_values,
        }
    }

    /// Returns a reference to the start proof, which may be empty.
    #[must_use]
    pub const fn start_proof(&self) -> &Proof<H> {
        &self.start_proof
    }

    /// Returns a reference to the end proof, which may be empty.
    #[must_use]
    pub const fn end_proof(&self) -> &Proof<H> {
        &self.end_proof
    }

    /// Returns the key-value pairs included in the range proof, which may be empty.
    #[must_use]
    pub const fn key_values(&self) -> &[(K, V)] {
        &self.key_values
    }

    /// Returns true if the range proof is empty, meaning it has no start or end proof
    /// and no key-value pairs.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.start_proof.is_empty() && self.end_proof.is_empty() && self.key_values.is_empty()
    }
}
