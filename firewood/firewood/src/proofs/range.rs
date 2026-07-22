// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Range proofs for Merkle tries.
//!
//! This module provides the [`RangeProof`] type, which enables efficient verification
//! that a contiguous set of key-value pairs exists within a Merkle trie without requiring
//! access to the entire trie structure.
//!
//! # Overview
//!
//! A range proof consists of three components:
//!
//! 1. **Start proof**: A Merkle proof establishing the lower boundary of the range.
//!    This proves either the first key in the range or the nearest key after the
//!    range start, ensuring no keys exist between the range start and the first
//!    included key.
//!
//! 2. **End proof**: A Merkle proof establishing the upper boundary of the range.
//!    This proves either the last key in the range or the nearest key before the
//!    range end, ensuring no keys exist between the last included key and the range end.
//!
//! 3. **Key-value pairs**: The actual consecutive entries from the trie that fall
//!    within the proven range boundaries, in lexicographic order.
//!
//! # Use Cases
//!
//! Range proofs are particularly valuable in blockchain contexts for:
//!
//! - **State synchronization**: Nodes can efficiently sync portions of state by
//!   requesting and verifying range proofs for specific key ranges.
//! - **Light client verification**: Light clients can verify specific state ranges
//!   without downloading the entire state trie.
//! - **Efficient auditing**: Auditors can verify that all keys in a specific range
//!   match expected values.
//! - **Sparse state queries**: Applications can query and verify multiple related
//!   keys in a single proof.
//!
//! # Example
//!
//! ```rust,ignore
//! use firewood::RangeProof;
//!
//! // Create a range proof for keys from "key1" to "key5"
//! let range_proof: RangeProof<Vec<u8>, Vec<u8>, Vec<ProofNode>> =
//!     db.get_range_proof(b"key1", b"key5")?;
//!
//! // Iterate over the key-value pairs in the proof
//! for (key, value) in &range_proof {
//!     println!("{:?} -> {:?}", key, value);
//! }
//!
//! // Check if the proof is empty
//! if range_proof.is_empty() {
//!     println!("No keys in range");
//! }
//! ```

use std::num::NonZeroUsize;

use crate::api::{self, FrozenRangeProof, HashKey};
use crate::merkle::verify_range_proof;
use crate::proofs::ProofError;

use super::types::{Proof, ProofCollection};

/// `(start_key, end_key)` describing the next key range to fetch after a
/// range or change proof. Returned by `find_next_key_after_*_proof`.
pub type KeyRange = (Box<[u8]>, Option<Box<[u8]>>);

/// Verification context captured after structural validation of a range proof.
/// Stored so that downstream logic (root hash verification, `find_next_key`)
/// can reference the original verification parameters without re-validating.
#[derive(Debug)]
pub struct RangeProofVerificationContext {
    /// The expected root hash of the trie.
    pub root: HashKey,
    /// The lower bound of the verified key range, if any.
    pub start_key: Option<Box<[u8]>>,
    /// The upper bound of the verified key range, if any.
    pub end_key: Option<Box<[u8]>>,
    /// The maximum number of key/value pairs the proof was permitted to
    /// contain. `None` means no limit.
    pub max_length: Option<NonZeroUsize>,
}

/// Verify structural properties of a range proof and produce a
/// [`RangeProofVerificationContext`] capturing the verification parameters
/// for use by downstream logic.
///
/// Enforces `max_length` against the proof's key-value count, then runs
/// the cryptographic range-proof verification via
/// [`verify_range_proof`].
///
/// # Errors
///
/// Returns [`api::Error::ProofError`] with
/// [`ProofError::ProofIsLargerThanMaxLength`] if the proof contains more
/// key-value pairs than `max_length` permits, or any error surfaced by
/// the underlying range-proof verification.
pub fn verify_range_proof_structure(
    proof: &FrozenRangeProof,
    root: HashKey,
    start_key: Option<&[u8]>,
    end_key: Option<&[u8]>,
    max_length: Option<NonZeroUsize>,
) -> Result<RangeProofVerificationContext, api::Error> {
    if let Some(max) = max_length
        && proof.key_values().len() > max.get()
    {
        return Err(api::Error::ProofError(
            ProofError::ProofIsLargerThanMaxLength,
        ));
    }

    verify_range_proof(start_key, end_key, &root, proof)?;

    Ok(RangeProofVerificationContext {
        root,
        start_key: start_key.map(Box::from),
        end_key: end_key.map(Box::from),
        max_length,
    })
}

/// Determine the next key range to fetch after this range proof.
///
/// Returns `None` when the originally-requested range is fully accounted
/// for; otherwise returns `Some((last_key, end_key))`.
///
/// # Errors
///
/// Currently does not return errors; the signature is `Result` for parity
/// with the change-proof counterpart and to allow future error paths.
pub fn find_next_key_after_range_proof(
    proof: &FrozenRangeProof,
    verification: &RangeProofVerificationContext,
) -> Result<Option<KeyRange>, api::Error> {
    // TODO(#352): proper implementation, this naively returns the last key
    // in the range, which is correct, but not ideal.
    let Some((last_key, _)) = proof.key_values().last() else {
        // no key-values in the proof, so we are done
        return Ok(None);
    };

    if proof.end_proof().is_empty() {
        // unbounded, so we are done
        return Ok(None);
    }

    if let Some(ref end_key) = verification.end_key
        && **last_key >= **end_key
    {
        // reached or exceeded the end key, so we are done
        return Ok(None);
    }

    Ok(Some((last_key.clone(), verification.end_key.clone())))
}

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
#[derive(PartialEq)]
pub struct RangeProof<K, V, H> {
    start_proof: Proof<H>,
    end_proof: Proof<H>,
    key_values: Box<[(K, V)]>,
}

impl<K, V, H> std::fmt::Debug for RangeProof<K, V, H>
where
    K: std::fmt::Debug,
    V: std::fmt::Debug,
    H: ProofCollection,
    H::Node: std::fmt::Debug,
{
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("RangeProof")
            .field("start_proof", &self.start_proof)
            .field("end_proof", &self.end_proof)
            .field("key_values", &self.key_values)
            .finish()
    }
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
///
/// This type is not re-exported at the top level; it is only accessible through
/// the iterator trait implementations on [`RangeProof`].
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

    use crate::api::TryIntoBatch;

    use super::*;

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
        let batch_iter = iter.map(TryIntoBatch::try_into_batch);
        let batches: Vec<_> = batch_iter.collect::<Result<_, _>>().unwrap();

        assert_eq!(batches.len(), 1);
        // The batch should be a Put operation since value is non-empty
        if let crate::api::BatchOp::Put { key, value } = &batches[0] {
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
