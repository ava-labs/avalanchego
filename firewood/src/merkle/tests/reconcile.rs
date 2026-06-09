// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::*;
#[cfg(feature = "ethhash")]
use test_case::test_case;

#[test]
fn test_reconcile_branch_proof_node_creates_missing_branch_without_value() {
    let mut merkle = create_in_memory_merkle();

    let proof_node = test_branch_proof_node(&[0xa, 0xb, 0xc], None);

    merkle
        .reconcile_branch_proof_node(&proof_node, |_| unreachable!())
        .unwrap();

    let node = merkle
        .get_node_from_nibbles(&[0xa, 0xb, 0xc])
        .unwrap()
        .unwrap();
    let branch = node.as_branch().expect("expected branch node");
    assert!(branch.value.is_none());
}

#[test]
fn test_reconcile_branch_proof_node_sets_missing_value_via_callback() {
    let mut merkle = create_in_memory_merkle();
    merkle.insert_branch_from_nibbles(&[0xa, 0xb]).unwrap();

    let proof_node =
        test_branch_proof_node(&[0xa, 0xb], Some(ValueDigest::Value(Box::from([7u8]))));

    // Proof has a value but trie doesn't — callback resolves it.
    merkle
        .reconcile_branch_proof_node(&proof_node, |pn| match &pn.value_digest {
            Some(ValueDigest::Value(v)) => Ok(Some(v.clone())),
            _ => Ok(None),
        })
        .unwrap();

    let node = merkle.get_node_from_nibbles(&[0xa, 0xb]).unwrap().unwrap();
    let branch = node.as_branch().expect("expected branch node");
    assert_eq!(branch.value.as_deref(), Some([7u8].as_slice()));
}

#[test]
fn test_reconcile_branch_proof_node_clears_value_via_callback() {
    let mut merkle = create_in_memory_merkle();
    merkle.insert(&[0xab], Box::from([3u8])).unwrap();
    merkle.insert_branch_from_nibbles(&[0xa, 0xb]).unwrap();

    let proof_node = test_branch_proof_node(&[0xa, 0xb], None);

    // Proof says no value, trie has one — callback returns None to clear it.
    merkle
        .reconcile_branch_proof_node(&proof_node, |_| Ok(None))
        .unwrap();

    let node = merkle.get_node_from_nibbles(&[0xa, 0xb]).unwrap().unwrap();
    let branch = node.as_branch().expect("expected branch node");
    assert!(branch.value.is_none());
}

#[test]
fn test_reconcile_branch_proof_node_rejects_conflict_via_callback() {
    let mut merkle = create_in_memory_merkle();
    merkle.insert(&[0xab], Box::from([1u8])).unwrap();
    merkle.insert_branch_from_nibbles(&[0xa, 0xb]).unwrap();

    let proof_node =
        test_branch_proof_node(&[0xa, 0xb], Some(ValueDigest::Value(Box::from([2u8]))));

    // Callback rejects the conflict.
    let err = merkle
        .reconcile_branch_proof_node(&proof_node, |_| Err(ProofError::UnexpectedValue))
        .unwrap_err();
    assert!(matches!(err, ProofError::UnexpectedValue));

    // Value is unchanged.
    let node = merkle.get_node_from_nibbles(&[0xa, 0xb]).unwrap().unwrap();
    let branch = node.as_branch().expect("expected branch node");
    assert_eq!(branch.value.as_deref(), Some([1u8].as_slice()));
}

/// Checks that the `ethhash` account relaxation in
/// `reconcile_branch_proof_node` is exactly as narrow as intended: at account
/// depth (64 nibbles), two account values that differ *only* in `storageRoot`
/// (field 2) reconcile without conflict, while a difference in any other field
/// still conflicts.
///
/// This exercises the helper directly, without constructing any proofs.
#[cfg(feature = "ethhash")]
#[test_case(100, true  ; "storage_root_only_diff_is_forgiven")]
#[test_case(999, false ; "extra_balance_diff_still_conflicts")]
fn test_reconcile_branch_proof_node_account_storage_root_relaxation(
    proof_balance: u64,
    expect_reconciled: bool,
) {
    use crate::merkle::tests::ethhash::{empty_code_hash, rlp_encode_account};

    // 32-byte account key == 64 nibbles == ACCOUNT_DEPTH_NIBBLES.
    let account_key = [0x11u8; 32];
    let account_nibbles = [0x1u8; 64];

    // The branch holds the full on-disk value. The proof value always carries a
    // different storageRoot; in the conflicting case it also differs in balance.
    let branch_value = rlp_encode_account(1, 100, &[0xAA; 32], &empty_code_hash());
    let proof_value = rlp_encode_account(1, proof_balance, &[0xBB; 32], &empty_code_hash());

    let mut merkle = create_in_memory_merkle();
    merkle.insert(&account_key, branch_value.clone()).unwrap();
    merkle.insert_branch_from_nibbles(&account_nibbles).unwrap();

    let proof_node =
        test_branch_proof_node(&account_nibbles, Some(ValueDigest::Value(proof_value)));

    // on_conflict always rejects, so the outcome reveals whether the relaxation
    // fired: a storageRoot-only diff returns early with Ok and never consults
    // on_conflict; any other field difference falls through to the Err.
    let result =
        merkle.reconcile_branch_proof_node(&proof_node, |_| Err(ProofError::UnexpectedValue));

    if expect_reconciled {
        result.expect("storageRoot-only diff must reconcile without conflict");
    } else {
        assert!(matches!(result.unwrap_err(), ProofError::UnexpectedValue));
    }

    // Shared post-condition: the branch value is untouched either way. The
    // relaxation returns before assignment, and on_conflict's Err propagates
    // before it.
    let node = merkle
        .get_node_from_nibbles(&account_nibbles)
        .unwrap()
        .unwrap();
    let branch = node.as_branch().expect("expected branch node");
    assert_eq!(branch.value.as_deref(), Some(branch_value.as_ref()));
}
