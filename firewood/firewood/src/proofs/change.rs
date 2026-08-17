// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{fmt::Debug, num::NonZeroUsize};

use firewood_storage::{DefaultHashMode, HashMode, NodeHashAlgorithm};

use crate::{
    Proof, ProofCollection, ProofError,
    api::{self, FrozenChangeProof, HashKey},
    db::BatchOp,
    proofs::ProofEdge,
};

/// A change proof can demonstrate that by applying the provided array of `BatchOp`s to a Merkle
/// trie with given start root hash, the resulting trie will have the given end root hash. It
/// consists of the following:
/// - A start proof: proves that the smallest key does/doesn't exist
/// - An end proof: proves that the largest key does/doesn't exist
/// - The actual `BatchOp`s that specify the difference between the start and end tries.
pub struct ChangeProof<K: AsRef<[u8]> + Debug, V: AsRef<[u8]> + Debug, H> {
    start_proof: Proof<H>,
    end_proof: Proof<H>,
    batch_ops: Box<[BatchOp<K, V>]>,
    /// The hash algorithm this proof was constructed or parsed with. For proofs
    /// built in this binary it is the compile default; for proofs parsed via
    /// [`FrozenChangeProof::from_slice`](crate::api::FrozenChangeProof::from_slice)
    /// it is resolved from the self-describing header byte.
    hash_mode: NodeHashAlgorithm,
}

impl<K, V, H> std::fmt::Debug for ChangeProof<K, V, H>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
    H: ProofCollection,
    H::Node: Debug,
{
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ChangeProof")
            .field("start_proof", &self.start_proof)
            .field("end_proof", &self.end_proof)
            .field("batch_ops", &self.batch_ops)
            .field("hash_mode", &self.hash_mode)
            .finish()
    }
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
        // Proofs built in this binary carry the compile-default mode; the parse
        // path stamps the resolved header mode via `new_with_hash_mode`.
        Self::with_hash_mode(
            start_proof,
            end_proof,
            key_values,
            DefaultHashMode::ALGORITHM,
        )
    }

    /// Like [`ChangeProof::new`], but records the [`NodeHashAlgorithm`] the proof
    /// was encoded with. Used by the parse path
    /// ([`FrozenChangeProof::from_slice`](crate::api::FrozenChangeProof::from_slice))
    /// to stamp the mode resolved from the self-describing header byte.
    #[must_use]
    pub(crate) const fn with_hash_mode(
        start_proof: Proof<H>,
        end_proof: Proof<H>,
        key_values: Box<[BatchOp<K, V>]>,
        hash_mode: NodeHashAlgorithm,
    ) -> Self {
        Self {
            start_proof,
            end_proof,
            batch_ops: key_values,
            hash_mode,
        }
    }

    /// The hash algorithm this proof was constructed or parsed with.
    #[must_use]
    pub const fn hash_mode(&self) -> NodeHashAlgorithm {
        self.hash_mode
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
    /// The right edge of the range the proof proves: the key the end proof is
    /// anchored at, as the verifier determines it. This is `end_key` when the
    /// proof covers the requested range, and the last key in `batch_ops` when the
    /// proof stops short of it — or when stopping short cannot be ruled out.
    /// `compute_right_edge_key` decides it and documents each case. Verification
    /// asserts completeness over `[start_key, right_edge_key]` only, which can be
    /// narrower than the range requested.
    ///
    /// The root hash verifier uses this as the right boundary for
    /// `compute_outside_children` and reconciliation. A caller continuing past
    /// this proof must resume strictly above the last operation's key, because
    /// both bounds of a request are inclusive —
    /// [`find_next_key_after_change_proof`] computes that resume point.
    pub right_edge_key: Option<Box<[u8]>>,
}

type FrozenBatchOp = BatchOp<Box<[u8]>, Box<[u8]>>;

/// Determine the next key range to fetch after this change proof.
///
/// `end_key` must be the upper bound of the request that produced `proof`.
///
/// Returns `None` when nothing further remains to fetch; otherwise returns a
/// continuation whose start is the smallest key strictly above the last key this
/// proof covered, paired with the same `end_key`.
///
/// The continuation starts strictly above the last covered key because both
/// bounds of a proof request are inclusive. A continuation that started at that
/// key would cover it again, report the same last key, and never advance.
///
/// # Trusting `None`
///
/// `None` means the requested range holds no further changes, and that is only
/// true of a proof whose root hash has been verified — by
/// [`Db::verify_change_proof`] or [`verify_change_proof_root_hash`]. Structural
/// validation alone accepts a proof with no operations for a range that does have
/// changes, because every check that would notice is expressed against the
/// operation list, which an empty list satisfies.
///
/// [`Db::verify_change_proof`]: crate::db::Db::verify_change_proof
/// [`verify_change_proof_root_hash`]: crate::merkle::verify_change_proof_root_hash
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
        // The proof reports no changes in the requested range, so nothing in
        // that range remains to fetch. Whether the caller wants keys beyond
        // `end_key` is its own bookkeeping, not something this proof can say.
        return Ok(None);
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

    Ok(Some((
        super::lex_successor(last_op.key()),
        end_key.map(Box::from),
    )))
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
    edge: ProofEdge,
) -> Result<(), api::Error> {
    let result = match proof.value_digest(key, end_root) {
        Ok(result) => result,
        Err(ProofError::Empty) => None,
        // Any `UnexpectedHash` from this boundary `value_digest` walk is by
        // construction an edge-proof failure — re-stamp it with which edge.
        Err(ProofError::UnexpectedHash { expected, actual }) => {
            return Err(api::Error::ProofError(ProofError::EdgeProofHashMismatch {
                edge,
                expected,
                actual,
            }));
        }
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

/// Compute the right edge of the proven range: the key the end proof is anchored
/// at. Verification asserts completeness up to it and no further.
///
/// A generator that stops short anchors its end proof at the last op it sent
/// rather than at `end_key`. A value lookup at the last op key decides which:
///
/// - A value there: that key is the proof's terminal, so it is the edge.
/// - Absent, with a trailing `Delete`: a proof of absence cannot name the key it
///   was built for, so a truncated reply looks identical to a complete one. Take
///   that key and judge the range the reply provably covers.
/// - Otherwise: `end_key`, or the last op key when the request was unbounded.
///
/// Narrowing is sound. The lookup succeeds only for a proof that is a complete
/// statement about that key, every op sits at or below the edge, and the caller's
/// next request covers the remainder.
///
/// The second arm costs a wasted round: a `Delete` of a key absent from both
/// revisions is true but inert, so the reply verifies and nothing changes. The
/// caller resumes just past that key, so the next reply can be padded the same
/// way with a fresh absent key, indefinitely. Rejecting such a `Delete` would
/// require the key to be present in the caller's state, which is what idempotent
/// re-application and overlapping proofs depend on not requiring. Nothing false
/// is accepted.
fn compute_right_edge_key<'a>(
    proof: &FrozenChangeProof,
    end_root: &HashKey,
    last_op_key: Option<&'a [u8]>,
    end_key: Option<&'a [u8]>,
) -> Option<&'a [u8]> {
    let Some(anchor) = last_op_key else {
        return end_key;
    };
    match proof.end_proof().value_digest(anchor, end_root) {
        Ok(Some(_)) => Some(anchor),
        Ok(None) if matches!(proof.batch_ops().last(), Some(BatchOp::Delete { .. })) => {
            Some(anchor)
        }
        _ => end_key.or(Some(anchor)),
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
/// # Node Hashing Algorithm
///
/// `algorithm` is the hash mode the caller expects the proof to be encoded
/// with. If the proof's own self-describing mode (from its header byte)
/// disagrees, verification is rejected up front with
/// [`ProofError::HashModeMismatch`] before any node hashing happens.
///
/// # Errors
///
/// Returns [`api::Error::ProofError`] if the proof is structurally invalid,
/// the proof's hash mode does not match `algorithm`, or boundary proof hash
/// chains fail verification.
///
/// On success, returns a [`ChangeProofVerificationContext`] capturing the
/// verification parameters for use by downstream root hash verification.
pub fn verify_change_proof_structure(
    proof: &FrozenChangeProof,
    end_root: HashKey,
    start_key: Option<&[u8]>,
    end_key: Option<&[u8]>,
    algorithm: NodeHashAlgorithm,
    max_length: Option<NonZeroUsize>,
) -> Result<ChangeProofVerificationContext, api::Error> {
    // Reject a proof whose self-describing mode disagrees with the caller's
    // expectation before any hashing happens.
    if proof.hash_mode() != algorithm {
        return Err(api::Error::ProofError(ProofError::HashModeMismatch {
            expected: algorithm,
            found: proof.hash_mode(),
        }));
    }

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
            ProofEdge::Left,
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
    let right_edge_key = compute_right_edge_key(proof, &end_root, last_op_key, end_key);

    // Verify the end boundary proof against end_root at right_edge_key, the key
    // the verifier treats as the proof's anchor. When that key is the last batch
    // op's key, the proof must be consistent with the op type, so pass the op;
    // otherwise the key is just a range bound and both inclusion and exclusion
    // are valid.
    let end_boundary_op =
        last_op.filter(|op| right_edge_key.is_some_and(|k| op.key().as_ref() == k));
    if let Some(key) = right_edge_key {
        verify_boundary_proof(
            proof.end_proof(),
            key,
            &end_root,
            end_boundary_op,
            ProofError::EndProofOperationMismatch,
            ProofEdge::Right,
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
