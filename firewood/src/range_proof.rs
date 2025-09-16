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

    /// Returns an iterator over the key-value pairs in this range proof.
    ///
    /// The iterator yields references to the key-value pairs in the order they
    /// appear in the proof (which should be lexicographic order as they appear
    /// in the trie).
    #[must_use]
    pub fn iter(&self) -> RangeProofIter<'_, K, V> {
        RangeProofIter(self.key_values.iter())
    }
}

/// An iterator over the key-value pairs in a [`RangeProof`].
///
/// This iterator yields references to the key-value pairs contained within
/// the range proof in the order they appear (lexicographic order).
#[derive(Debug)]
pub struct RangeProofIter<'a, K, V>(std::slice::Iter<'a, (K, V)>);

impl<'a, K, V> Iterator for RangeProofIter<'a, K, V> {
    type Item = &'a (K, V);

    fn next(&mut self) -> Option<Self::Item> {
        self.0.next()
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        self.0.size_hint()
    }
}

impl<K, V> ExactSizeIterator for RangeProofIter<'_, K, V> {}

impl<K, V> std::iter::FusedIterator for RangeProofIter<'_, K, V> {}

impl<'a, K, V, H> IntoIterator for &'a RangeProof<K, V, H>
where
    K: AsRef<[u8]>,
    V: AsRef<[u8]>,
    H: ProofCollection,
{
    type Item = &'a (K, V);
    type IntoIter = RangeProofIter<'a, K, V>;

    fn into_iter(self) -> Self::IntoIter {
        self.iter()
    }
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used, reason = "Tests can use unwrap")]
    #![expect(clippy::indexing_slicing, reason = "Tests can use indexing")]

    use super::*;
    use crate::v2::api::KeyValuePairIter;

    #[test]
    fn test_range_proof_iterator() {
        // Create test data
        let key_values: Box<[(Vec<u8>, Vec<u8>)]> = Box::new([
            (b"key1".to_vec(), b"value1".to_vec()),
            (b"key2".to_vec(), b"value2".to_vec()),
            (b"key3".to_vec(), b"value3".to_vec()),
        ]);

        // Create empty proofs for testing
        let start_proof = Proof::empty();
        let end_proof = Proof::empty();

        let range_proof = RangeProof::new(start_proof, end_proof, key_values);

        // Test basic iterator functionality
        let mut iter = range_proof.iter();
        assert_eq!(iter.len(), 3);

        let first = iter.next().unwrap();
        assert_eq!(first.0, b"key1");
        assert_eq!(first.1, b"value1");

        let second = iter.next().unwrap();
        assert_eq!(second.0, b"key2");
        assert_eq!(second.1, b"value2");

        let third = iter.next().unwrap();
        assert_eq!(third.0, b"key3");
        assert_eq!(third.1, b"value3");

        assert!(iter.next().is_none());
    }

    #[test]
    fn test_range_proof_into_iterator() {
        let key_values: Box<[(Vec<u8>, Vec<u8>)]> = Box::new([
            (b"a".to_vec(), b"alpha".to_vec()),
            (b"b".to_vec(), b"beta".to_vec()),
        ]);

        let start_proof = Proof::empty();
        let end_proof = Proof::empty();
        let range_proof = RangeProof::new(start_proof, end_proof, key_values);

        // Test that we can use for-loop syntax
        let mut items = Vec::new();
        for item in &range_proof {
            items.push(item);
        }

        assert_eq!(items.len(), 2);
        assert_eq!(items[0].0, b"a");
        assert_eq!(items[0].1, b"alpha");
        assert_eq!(items[1].0, b"b");
        assert_eq!(items[1].1, b"beta");
    }

    #[test]
    fn test_keyvaluepair_iter_trait() {
        let key_values: Box<[(Vec<u8>, Vec<u8>)]> =
            Box::new([(b"test".to_vec(), b"data".to_vec())]);

        let start_proof = Proof::empty();
        let end_proof = Proof::empty();
        let range_proof = RangeProof::new(start_proof, end_proof, key_values);

        // Test that our iterator implements KeyValuePairIter
        let iter = range_proof.iter();

        // Verify we can call methods from KeyValuePairIter
        let batch_iter = iter.map_into_batch();
        let batches: Vec<_> = batch_iter.collect();

        assert_eq!(batches.len(), 1);
        // The batch should be a Put operation since value is non-empty
        if let crate::v2::api::BatchOp::Put { key, value } = &batches[0] {
            assert_eq!(key.as_ref() as &[u8], b"test");
            assert_eq!(value.as_ref() as &[u8], b"data");
        } else {
            panic!("Expected Put operation");
        }
    }

    #[test]
    fn test_empty_range_proof_iterator() {
        let key_values: Box<[(Vec<u8>, Vec<u8>)]> = Box::new([]);
        let start_proof = Proof::empty();
        let end_proof = Proof::empty();
        let range_proof = RangeProof::new(start_proof, end_proof, key_values);

        let mut iter = range_proof.iter();
        assert_eq!(iter.len(), 0);
        assert!(iter.next().is_none());

        let items: Vec<_> = range_proof.into_iter().collect();
        assert!(items.is_empty());
    }
}
