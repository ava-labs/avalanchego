// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood_storage::{
    Mutable, NodeStore, Propose, ReadableStorage, ValueDigest, logger::warn, replace_list_field,
};

use crate::proofs::eth::ACCOUNT_DEPTH_NIBBLES;
use crate::{ProofError, ProofNode, Value, merkle::Merkle};

impl<S: ReadableStorage> Merkle<NodeStore<Mutable<Propose>, S>> {
    /// Reconciles a branch proof node against the in-memory proving merkle.
    ///
    /// This helper creates any missing branch structure for the proof node's
    /// key, then reconciles the branch value with the proof node's value.
    /// If the values already match, it returns successfully without changes.
    /// If they differ (including when one side has a value and the other
    /// does not), `on_conflict` is invoked to decide the final branch value.
    ///
    /// ## Arguments
    ///
    /// * `proof_node` - A branch proof node containing the key (as nibble
    ///   path components) and an optional value digest to reconcile.
    /// * `on_conflict` - Called only when the proof node's value and the
    ///   existing branch value conflict. Returning `Ok(Some(value))` stores
    ///   that value in the branch, `Ok(None)` clears the branch value, and
    ///   `Err(...)` aborts reconciliation with that error.
    ///
    /// ## Errors
    ///
    /// Returns an error if the proof is structurally invalid or if
    /// `on_conflict` returns `Err` while resolving a value conflict.
    pub(crate) fn reconcile_branch_proof_node(
        &mut self,
        proof_node: &ProofNode,
        on_conflict: impl FnOnce(&ProofNode) -> Result<Option<Value>, ProofError>,
    ) -> Result<(), ProofError> {
        let key_nibbles: Box<[u8]> = proof_node
            .key
            .iter()
            .map(|component| component.as_u8())
            .collect();

        if !key_nibbles.len().is_multiple_of(2)
            && matches!(proof_node.value_digest, Some(ValueDigest::Value(_)))
        {
            return Err(ProofError::ValueAtOddNibbleLength);
        }

        self.insert_branch_from_nibbles(&key_nibbles)?;

        // insert_branch_from_nibbles guarantees a branch exists at this path
        // and that all nodes along it are in-memory, so this cannot fail.
        let branch = Self::get_branch_from_nibbles_mut(self.nodestore.root_mut(), &proof_node.key)
            .ok_or(ProofError::NodeNotInTrie)?;

        let proof_value = match proof_node.value_digest.as_ref() {
            Some(ValueDigest::Value(v)) => Some(v.as_ref()),
            #[cfg(not(feature = "ethhash"))]
            Some(digest @ ValueDigest::Hash(_)) => {
                // In merkledb mode, large values (>= 32 bytes) are stored as
                // hashes in serialized proofs. If the branch's value hashes to
                // the same value, the proof and trie agree — no conflict.
                // Otherwise fall through with proof_value = None:
                //  - branch has no value: None == None → Ok(()), no callback.
                //  - branch has mismatched value: None != Some(v) → callback.
                match branch.value.as_deref() {
                    Some(v) if digest.as_ref().verify(v) => return Ok(()),
                    _ => None,
                }
            }
            _ => None,
        };

        if proof_value == branch.value.as_deref() {
            return Ok(());
        }

        // Ethhash account values may differ from the proof's value in just
        // the `storageRoot` field. This happens when the proposal was built
        // from a subset of the account's storage children: live hashing
        // splices in a partial storageRoot, while the proof carries the
        // full on-disk value. Both produce the same final hash because
        // `Preimage::write` always recomputes storageRoot from the current
        // children at hash time, but byte equality fails.
        //
        // This early-return is safe regardless of how the divergence arose: the
        // caller (`verify_change_proof_root_hash`) still gates acceptance on the
        // final root-hash check, so relaxing here only avoids a spurious per-node
        // `UnexpectedValue`. It never widens what proofs are accepted.
        if cfg!(feature = "ethhash")
            && proof_node.key.len() == ACCOUNT_DEPTH_NIBBLES
            && let (Some(pv), Some(bv)) = (proof_value, branch.value.as_deref())
            && account_values_equal_except_storage_root(pv, bv)
        {
            return Ok(());
        }

        // Values differ — let the caller decide what to do.
        branch.value = on_conflict(proof_node)?;
        Ok(())
    }
}

/// Two ethhash account RLP values are equivalent for hashing if they agree
/// on every field except `storageRoot` (index 2). `Preimage::write` always
/// recomputes that field from the current children, so the on-disk byte
/// difference is invisible in the final hash.
///
/// This is a hashing-equivalence check, not a general account-equality
/// predicate: it is only valid because `Preimage::write` re-derives
/// `storageRoot` from the children. Do not call it anywhere that does not
/// recompute `storageRoot`, or it would treat two different accounts as equal.
///
/// Logs a warning when either side fails to parse as account RLP. The
/// resulting `false` return falls through to `on_conflict`, which surfaces
/// the conflict as `UnexpectedValue`. The warning gives operators a clearer
/// signal that the underlying cause is malformed data, not a value mismatch.
fn account_values_equal_except_storage_root(a: &[u8], b: &[u8]) -> bool {
    let zeros = [0u8; 32];
    match (
        replace_list_field(a, 2, &zeros),
        replace_list_field(b, 2, &zeros),
    ) {
        (Ok(na), Ok(nb)) => na == nb,
        (a_res, b_res) => {
            warn!(
                "malformed account RLP at depth 64 during reconcile: proof={:?} branch={:?}",
                a_res.err(),
                b_res.err(),
            );
            false
        }
    }
}
