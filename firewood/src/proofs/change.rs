// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{fmt::Debug, num::NonZeroUsize};

use crate::{
    Proof, ProofCollection, ProofError,
    api::{self, FrozenChangeProof, HashKey},
    db::BatchOp,
};

/// A change proof can demonstrate that by applying the provided array of `BatchOp`s to a Merkle
/// trie with given start root hash, the resulting trie will have the given end root hash. It
/// consists of the following:
/// - A start proof: proves that the smallest key does/doesn't exist
/// - An end proof: proves the the largest key does/doesn't exist
/// - The actual `BatchOp`s that specify the difference between the start and end tries.
#[derive(Debug)]
pub struct ChangeProof<K: AsRef<[u8]> + Debug, V: AsRef<[u8]> + Debug, H> {
    start_proof: Proof<H>,
    end_proof: Proof<H>,
    batch_ops: Box<[BatchOp<K, V>]>,
}

impl<K, V, H> ChangeProof<K, V, H>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
    H: ProofCollection,
{
    /// Create a new change proof with the given start and end proofs
    /// and the `BatchOp`s that are included in the proof.
    #[must_use]
    pub const fn new(
        start_proof: Proof<H>,
        end_proof: Proof<H>,
        key_values: Box<[BatchOp<K, V>]>,
    ) -> Self {
        Self {
            start_proof,
            end_proof,
            batch_ops: key_values,
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

    /// Returns the `BatchOp`s included in the change proof, which may be empty.
    #[must_use]
    pub const fn batch_ops(&self) -> &[BatchOp<K, V>] {
        &self.batch_ops
    }

    /// Returns true if the change proof is empty, meaning it has no start or end proof
    /// and no `BatchOp`s.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.start_proof.is_empty() && self.end_proof.is_empty() && self.batch_ops.is_empty()
    }

    /// Returns an iterator over the `BatchOp`s in this change proof.
    ///
    /// The iterator yields references to the `BatchOp`s in the order they
    /// appear in the proof (which should be lexicographic order as they appear
    /// in the trie).
    #[must_use]
    pub fn iter(&self) -> ChangeProofIter<'_, K, V> {
        ChangeProofIter(self.batch_ops.iter())
    }
}

/// An iterator over the `BatchOp`s in a `ChangeProof`.
///
/// This iterator yields references to the `BatchOp`s contained within
/// the change proof in the order they appear (lexicographic order).
///
/// This type is not re-exported at the top level; it is only accessible through
/// the iterator trait implementations on `ChangeProof`.
#[derive(Debug)]
pub struct ChangeProofIter<'a, K: AsRef<[u8]> + Debug, V: AsRef<[u8]> + Debug>(
    std::slice::Iter<'a, BatchOp<K, V>>,
);

impl<'a, K, V> Iterator for ChangeProofIter<'a, K, V>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
{
    type Item = &'a BatchOp<K, V>;

    fn next(&mut self) -> Option<Self::Item> {
        self.0.next()
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        self.0.size_hint()
    }
}

impl<K, V> ExactSizeIterator for ChangeProofIter<'_, K, V>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
{
}

impl<K, V> std::iter::FusedIterator for ChangeProofIter<'_, K, V>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
{
}

impl<'a, K, V, H> IntoIterator for &'a ChangeProof<K, V, H>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
    H: ProofCollection,
{
    type Item = &'a BatchOp<K, V>;
    type IntoIter = ChangeProofIter<'a, K, V>;

    fn into_iter(self) -> Self::IntoIter {
        self.iter()
    }
}

// ── Change proof verification ──────────────────────────────────────────────

/// Verification context captured after structural validation of a change proof.
/// Stored so that downstream logic (root hash verification, `find_next_key`) can
/// reference the original verification parameters without re-validating.
#[derive(Debug)]
pub struct ChangeProofVerificationContext {
    /// The expected root hash of the ending revision.
    pub end_root: HashKey,
    /// The lower bound of the verified key range, if any.
    pub start_key: Option<Box<[u8]>>,
    /// The upper bound of the verified key range, if any.
    pub end_key: Option<Box<[u8]>>,
    /// The actual right edge of the proven range. This equals `end_key`
    /// when all items fit within the limit, or the last key in `batch_ops`
    /// when the proof may have been truncated (`batch_ops.len() ==
    /// max_length`). The root hash verifier uses this as the right
    /// boundary for `compute_outside_children` and reconciliation.
    pub right_edge_key: Option<Box<[u8]>>,
}

type FrozenBatchOp = BatchOp<Box<[u8]>, Box<[u8]>>;

/// Determine the next key range to fetch after this change proof.
///
/// Inspects the proof structure only — does not require a proposal.
/// `end_key` is the original requested upper bound passed to the proof
/// generator.
///
/// Returns `None` if the proof confirms there are no more keys in the
/// requested range; otherwise returns `Some((last_op.key, end_key))` as
/// a continuation.
///
/// # Errors
///
/// Returns [`ProofError::EndKeyLessThanLastKey`] when `last_op.key` is
/// strictly greater than `end_key` — this indicates the proof was
/// generated against a different `end_key` than the one supplied here.
pub fn find_next_key_after_change_proof(
    proof: &FrozenChangeProof,
    end_key: Option<&[u8]>,
) -> Result<Option<super::range::KeyRange>, api::Error> {
    let Some(last_op) = proof.batch_ops().last() else {
        // No changes in this range. If bounded, continue from end_key.
        return Ok(end_key.map(|ek| (Box::from(ek), None)));
    };

    if proof.end_proof().is_empty() {
        return Ok(None);
    }

    if let Some(end_key) = end_key {
        if **last_op.key() > *end_key {
            return Err(api::Error::ProofError(ProofError::EndKeyLessThanLastKey));
        }
        if **last_op.key() == *end_key {
            return Ok(None);
        }
    }

    Ok(Some((last_op.key().clone(), end_key.map(Box::from))))
}

/// Verify a boundary proof against `end_root` and optionally check that the
/// proof's inclusion/exclusion result is consistent with `boundary_op`.
///
/// When `boundary_op` is `Some`, a `Put` must be an inclusion proof (key
/// present) and a `Delete` must be an exclusion proof (key absent).
/// When `boundary_op` is `None` the key is an arbitrary range bound and
/// both outcomes are valid.
fn verify_boundary_proof<C: ProofCollection>(
    proof: &Proof<C>,
    key: &[u8],
    end_root: &HashKey,
    boundary_op: Option<&FrozenBatchOp>,
    mismatch_error: ProofError,
) -> Result<(), api::Error> {
    let result = match proof.value_digest(key, end_root) {
        Ok(result) => result,
        Err(ProofError::Empty) => None,
        Err(e) => return Err(api::Error::ProofError(e)),
    };

    match boundary_op {
        Some(BatchOp::Put { .. }) if result.is_none() => {
            Err(api::Error::ProofError(mismatch_error))
        }
        Some(BatchOp::Delete { .. }) if result.is_some() => {
            Err(api::Error::ProofError(mismatch_error))
        }
        _ => Ok(()),
    }
}

/// Compute the right edge of the proven range.
///
/// The proof may have been truncated by the generator's limit when:
///   1. `batch_ops.len() == max_length` (limit was potentially hit)
///   2. the end proof is an inclusion proof of the last batch op key
///      (the generator produced the proof at that key, not at `end_key`)
///
/// When truncated, the right edge is `last_op_key`. Otherwise it is
/// `end_key` (falling back to `last_op_key` for unbounded ranges).
fn compute_right_edge_key<'a>(
    proof: &FrozenChangeProof,
    end_root: &HashKey,
    last_op_key: Option<&'a [u8]>,
    end_key: Option<&'a [u8]>,
    max_length: Option<NonZeroUsize>,
) -> Option<&'a [u8]> {
    let possibly_truncated = max_length.is_some_and(|n| proof.batch_ops().len() == n.get());
    let truncated = possibly_truncated
        && last_op_key.is_some_and(|k| {
            proof
                .end_proof()
                .value_digest(k, end_root)
                .ok()
                .flatten()
                .is_some()
        });
    if truncated {
        last_op_key
    } else {
        end_key.or(last_op_key)
    }
}

/// Verify structural properties and boundary proofs of a change proof.
///
/// Performs the following checks:
/// - Range validity (`start_key` ≤ `end_key`)
/// - No `DeleteRange` operations
/// - `batch_ops` length does not exceed `max_length`
/// - Keys are sorted and unique
/// - Boundary key constraints (`start_key` ≤ first batch key, `end_key` ≥ last batch key)
/// - Boundary proof completeness (non-empty `batch_ops` with bounds requires at least one proof)
/// - Start and end proof hash chain verification against `end_root`
/// - End proof inclusion/exclusion consistency with the last batch operation
///
/// # Errors
///
/// Returns [`api::Error::ProofError`] if the proof is structurally invalid
/// or boundary proof hash chains fail verification.
///
/// On success, returns a [`ChangeProofVerificationContext`] capturing the
/// verification parameters for use by downstream root hash verification.
pub fn verify_change_proof_structure(
    proof: &FrozenChangeProof,
    end_root: HashKey,
    start_key: Option<&[u8]>,
    end_key: Option<&[u8]>,
    max_length: Option<NonZeroUsize>,
) -> Result<ChangeProofVerificationContext, api::Error> {
    let batch_ops = proof.batch_ops();

    // --- O(1) checks first ---

    // Check batch_ops length <= max_length
    if let Some(max_length) = max_length
        && batch_ops.len() > max_length.get()
    {
        return Err(api::Error::ProofError(
            ProofError::ProofIsLargerThanMaxLength,
        ));
    }

    // Reject inverted ranges early. The generator enforces this, but the
    // verifier must independently validate because start_key/end_key
    // come from the caller, not the proof.
    if let (Some(start), Some(end)) = (start_key, end_key)
        && start > end
    {
        return Err(api::Error::InvalidRange {
            start_key: start.to_vec().into(),
            end_key: end.to_vec().into(),
        });
    }

    // Validate boundary proof presence against batch_ops, start_key,
    // and end_key. The honest generator follows strict rules about when
    // proofs should be present. These O(1) checks reject malformed
    // proofs before expensive O(n) scans.

    // A start_proof anchors the first batch op to end_root at start_key.
    // Without start_key we have no key to verify the proof against, so a
    // non-empty start_proof is rejected as unverifiable.
    if !proof.start_proof().is_empty() && start_key.is_none() {
        return Err(api::Error::ProofError(ProofError::UnexpectedStartProof));
    }

    match (batch_ops.is_empty(), proof.end_proof().is_empty()) {
        // batch_ops present but no end_proof — always an error. The end
        // proof anchors the last batch key to end_root; without it an
        // attacker could truncate batch_ops and the verifier couldn't
        // detect the omission. This applies even when proving through the
        // end of the DB, because the proof still needs to bind the last
        // key's inclusion/exclusion to the claimed root hash.
        // Distinguish "no boundary proofs at all" from "just missing end".
        (false, true) => {
            if proof.start_proof().is_empty() && (start_key.is_some() || end_key.is_some()) {
                return Err(api::Error::ProofError(ProofError::MissingBoundaryProof));
            }
            return Err(api::Error::ProofError(ProofError::MissingEndProof));
        }
        // No batch_ops, end_proof present but no end_key — the honest
        // generator never produces this.
        (true, false) if end_key.is_none() => {
            return Err(api::Error::ProofError(ProofError::UnexpectedEndProof));
        }
        // No batch_ops, no end_proof, but end_key present — missing.
        (true, true) if end_key.is_some() => {
            return Err(api::Error::ProofError(ProofError::MissingEndProof));
        }
        // all other cases are fine
        _ => {}
    }

    // Check start key not greater than first batch op key
    if let (Some(start_key), Some(first_key)) = (start_key, batch_ops.first())
        && *start_key > **first_key.key()
    {
        return Err(api::Error::ProofError(
            ProofError::StartKeyLargerThanFirstKey,
        ));
    }

    // Check end key not less than last batch op key
    if let (Some(end_key), Some(last_key)) = (end_key, batch_ops.last())
        && *end_key < **last_key.key()
    {
        return Err(api::Error::ProofError(ProofError::EndKeyLessThanLastKey));
    }

    // Verify start boundary proof against end_root.
    // When start_key is None, the start proof must be empty (enforced by
    // the UnexpectedStartProof check above), so there is nothing to verify.
    // When first_op_key == start_key, the proof must be consistent with
    // the op type (Put→inclusion, Delete→exclusion). Otherwise start_key
    // is an arbitrary range bound and both outcomes are valid.
    if let Some(start_key) = start_key {
        let boundary_op = batch_ops
            .first()
            .filter(|op| op.key().as_ref() == start_key);
        verify_boundary_proof(
            proof.start_proof(),
            start_key,
            &end_root,
            boundary_op,
            ProofError::StartProofOperationMismatch,
        )?;
    }

    // Single-pass O(n) scan: reject DeleteRange ops and verify keys are
    // sorted and unique. The honest diff algorithm only produces Put and
    // Delete ops; a crafted proof could use DeleteRange to delete keys
    // outside the proven range. After the loop, last_op holds the last
    // batch op for end proof verification.
    let mut last_op: Option<&BatchOp<_, _>> = None;
    for op in batch_ops {
        if matches!(op, BatchOp::DeleteRange { .. }) {
            return Err(api::Error::ProofError(
                ProofError::DeleteRangeFoundInChangeProof,
            ));
        }
        let key = op.key();
        if let Some(prev) = last_op
            && key <= prev.key()
        {
            return Err(api::Error::ProofError(ProofError::ChangeProofKeysNotSorted));
        }
        last_op = Some(op);
    }

    let last_op_key = last_op.map(|op| op.key().as_ref());
    let right_edge_key = compute_right_edge_key(proof, &end_root, last_op_key, end_key, max_length);

    // Verify end boundary proof against end_root. The end proof was
    // generated for right_edge_key. The boundary_op check applies when
    // the right edge matches the last batch op key (meaning the proof
    // must be consistent with the op type); otherwise the key is just a
    // range bound and both inclusion/exclusion are valid.
    let end_boundary_op =
        last_op.filter(|op| right_edge_key.is_some_and(|k| op.key().as_ref() == k));
    if let Some(key) = right_edge_key {
        verify_boundary_proof(
            proof.end_proof(),
            key,
            &end_root,
            end_boundary_op,
            ProofError::EndProofOperationMismatch,
        )?;
    }

    Ok(ChangeProofVerificationContext {
        end_root,
        start_key: start_key.map(Box::from),
        end_key: end_key.map(Box::from),
        right_edge_key: right_edge_key.map(Box::from),
    })
}

#[cfg(test)]
mod tests {
    #![expect(clippy::unwrap_used, reason = "Tests can use unwrap")]
    #![expect(clippy::indexing_slicing, reason = "Tests can use indexing")]

    use super::*;
    use crate::merkle::{Key, Value};

    #[test]
    fn test_change_proof_iterator() {
        let key_values: Box<[BatchOp<Key, Value>]> = Box::new([
            BatchOp::Put {
                key: b"key1".to_vec().into_boxed_slice(),
                value: b"value1".to_vec().into_boxed_slice(),
            },
            BatchOp::Put {
                key: b"key2".to_vec().into_boxed_slice(),
                value: b"value2".to_vec().into_boxed_slice(),
            },
            BatchOp::Put {
                key: b"key3".to_vec().into_boxed_slice(),
                value: b"value3".to_vec().into_boxed_slice(),
            },
        ]);

        let start_proof = Proof::empty();
        let end_proof = Proof::empty();

        let change_proof = ChangeProof::new(start_proof, end_proof, key_values);

        let mut iter = change_proof.iter();
        assert_eq!(iter.len(), 3);

        let first = iter.next().unwrap();
        assert!(
            matches!(first, BatchOp::Put { key, value } if **key == *b"key1" && **value == *b"value1"),
        );

        let second = iter.next().unwrap();
        assert!(
            matches!(second, BatchOp::Put { key, value } if **key == *b"key2" && **value == *b"value2"),
        );

        let third = iter.next().unwrap();
        assert!(
            matches!(third, BatchOp::Put { key, value } if **key == *b"key3" && **value == *b"value3"),
        );

        assert!(iter.next().is_none());
    }

    #[test]
    fn test_change_proof_into_iterator() {
        let key_values: Box<[BatchOp<Key, Value>]> = Box::new([
            BatchOp::Put {
                key: b"a".to_vec().into_boxed_slice(),
                value: b"alpha".to_vec().into_boxed_slice(),
            },
            BatchOp::Put {
                key: b"b".to_vec().into_boxed_slice(),
                value: b"beta".to_vec().into_boxed_slice(),
            },
        ]);

        let start_proof = Proof::empty();
        let end_proof = Proof::empty();
        let change_proof = ChangeProof::new(start_proof, end_proof, key_values);

        let mut items = Vec::new();
        for item in &change_proof {
            items.push(item);
        }

        assert_eq!(items.len(), 2);
        assert!(
            matches!(items[0], BatchOp::Put{ key, value } if **key == *b"a" && **value == *b"alpha"),
        );
        assert!(
            matches!(items[1], BatchOp::Put{ key, value } if **key == *b"b" && **value == *b"beta"),
        );
    }

    #[test]
    fn test_empty_change_proof_iterator() {
        let key_values: Box<[BatchOp<Key, Value>]> = Box::new([]);
        let start_proof = Proof::empty();
        let end_proof = Proof::empty();
        let change_proof = ChangeProof::new(start_proof, end_proof, key_values);

        let mut iter = change_proof.iter();
        assert_eq!(iter.len(), 0);
        assert!(iter.next().is_none());

        let items: Vec<_> = change_proof.into_iter().collect();
        assert!(items.is_empty());
    }
}
