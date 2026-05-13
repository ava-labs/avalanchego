// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Attack detection: forged, omitted, or spurious batch ops.

use super::*;

type OwnedBatchOps = Vec<BatchOp<Box<[u8]>, Box<[u8]>>>;

/// Helper: returns true if the (possibly corrupted) proof is rejected by
/// either the structural check or the root hash check.
fn is_rejected(
    db: &Db,
    proof: &FrozenChangeProof,
    end_root: api::HashKey,
    start_key: Option<&[u8]>,
    end_key: Option<&[u8]>,
    start_root: api::HashKey,
) -> bool {
    match verify_change_proof_structure(proof, end_root, start_key, end_key, None) {
        Err(_) => true,
        Ok(ctx) => verify_and_check(db, proof, &ctx, start_root).is_err(),
    }
}

#[test]
fn test_crafted_omitted_change_detected() {
    let (source, target, root1_target, _ds, _dt) = setup_source_target![
        (b"\x10", b"v0"),
        (b"\x20", b"v1"),
        (b"\x30", b"v2"),
        (b"\x40", b"v3")
    ];

    let (root1_source, root2) =
        setup_2nd_commit!(source, [(b"\x20", b"changed1"), (b"\x30", b"changed2")]);

    let valid = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x10"),
            Some(b"\x40"),
            None,
        )
        .unwrap();

    let mut shortened: Vec<BatchOp<Key, Value>> = valid.batch_ops().to_vec();
    shortened.remove(0);
    let crafted = FrozenChangeProof::new(
        crate::Proof::new(valid.start_proof().as_ref().into()),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        shortened.into_boxed_slice(),
    );

    let ctx =
        verify_change_proof_structure(&crafted, root2, Some(b"\x10"), Some(b"\x40"), None).unwrap();
    let err = verify_and_check(&target, &crafted, &ctx, root1_target)
        .expect_err("omitted change must be detected");
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::EndRootMismatch)
    ));
}

#[test]
fn test_crafted_forged_value_detected() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x10", b"v0"), (b"\xa0", b"v1")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x10", b"real")]);

    let valid = source
        .change_proof(root1_source, root2.clone(), None, None, None)
        .unwrap();

    let mut forged_ops: Vec<BatchOp<Key, Value>> = valid.batch_ops().to_vec();
    forged_ops[0] = BatchOp::Put {
        key: b"\x10".to_vec().into(),
        value: b"FORGED".to_vec().into(),
    };
    let crafted = FrozenChangeProof::new(
        crate::Proof::new(valid.start_proof().as_ref().into()),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        forged_ops.into_boxed_slice(),
    );

    let ctx = verify_change_proof_structure(&crafted, root2, None, None, None).unwrap();
    let err = verify_and_check(&target, &crafted, &ctx, root1_target)
        .expect_err("forged value must be detected");
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::UnexpectedValue)
    ));
}

#[test]
fn test_crafted_spurious_batch_op_detected() {
    let (source, _dir_source) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\xa0", b"changed")]);

    let valid = source
        .change_proof(root1_source, root2.clone(), None, None, None)
        .unwrap();

    let mut ops: Vec<BatchOp<Key, Value>> = valid.batch_ops().to_vec();
    ops.push(BatchOp::Put {
        key: b"\xf0".to_vec().into(),
        value: b"spurious".to_vec().into(),
    });
    ops.sort_by(|a, b| a.key().cmp(b.key()));

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(valid.start_proof().as_ref().into()),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    let err = verify_change_proof_structure(&crafted, root2, None, None, None)
        .expect_err("structural check should reject spurious batch op");
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::ShouldBePrefixOfNextKey)
    ));
}

/// Attacker adds a spurious Put at `start_key` when `start_key` doesn't
/// exist in `end_root`. The start proof is an exclusion proof. The
/// `StartProofOperationMismatch` check catches this: Put expects inclusion
/// but the proof is exclusion.
#[test]
fn test_crafted_spurious_put_at_start_key_boundary() {
    // Keys \x20 and \x90 exist. \x10 (start_key) does NOT exist.
    let (source, _dir_source) = setup_db![(b"\x20", b"v2"), (b"\x90", b"v9")];

    // Change \x20 on source
    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x20", b"changed")]);

    // Generate bounded proof for [\x10, \x90].
    // Start proof for \x10 is an exclusion proof (\x10 doesn't exist).
    let honest = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x10".as_ref()),
            Some(b"\x90".as_ref()),
            None,
        )
        .unwrap();

    // Add spurious Put(\x10, fake) — start_key becomes first_op_key.
    let mut ops: Vec<BatchOp<Key, Value>> = honest.batch_ops().to_vec();
    ops.push(BatchOp::Put {
        key: b"\x10".to_vec().into(),
        value: b"fake".to_vec().into(),
    });
    ops.sort_by(|a, b| a.key().cmp(b.key()));

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(honest.start_proof().as_ref().into()),
        crate::Proof::new(honest.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    let err = verify_change_proof_structure(&crafted, root2, Some(b"\x10"), Some(b"\x90"), None)
        .expect_err("structural check should reject spurious Put at start_key");
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::StartProofOperationMismatch)
    ));
}

/// Attacker adds a spurious Delete at `start_key` when `start_key` EXISTS
/// in `end_root`. The start proof is an inclusion proof, but the Delete
/// claims the key was removed — `StartProofOperationMismatch`.
#[test]
fn test_crafted_spurious_delete_at_start_key_boundary() {
    // Keys \x20 and \x90 exist. start_key \x20 EXISTS in end_root.
    let (source, _dir_source) = setup_db![(b"\x20", b"v2"), (b"\x90", b"v9")];

    // Change \x90 on source (not \x20 — \x20 stays in end_root).
    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x90", b"changed")]);

    // Generate bounded proof for [\x20, \x90].
    // Start proof for \x20 is an inclusion proof (\x20 exists).
    let honest = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x20".as_ref()),
            Some(b"\x90".as_ref()),
            None,
        )
        .unwrap();

    // Add spurious Delete(\x20) — start_key = first_op_key.
    // The start proof is inclusion (key exists) but Delete claims
    // it was removed — contradiction.
    let mut ops: Vec<BatchOp<Key, Value>> = honest.batch_ops().to_vec();
    ops.push(BatchOp::Delete {
        key: b"\x20".to_vec().into(),
    });
    ops.sort_by(|a, b| a.key().cmp(b.key()));

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(honest.start_proof().as_ref().into()),
        crate::Proof::new(honest.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    let err = verify_change_proof_structure(&crafted, root2, Some(b"\x20"), Some(b"\x90"), None)
        .expect_err("structural check should reject spurious Delete at start_key");
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::StartProofOperationMismatch)
    ));
}

/// Spurious Put at `end_key` when `end_key` doesn't exist (exclusion end proof).
/// The end proof is exclusion but Put expects inclusion — mismatch.
///
/// Exercises: `EndProofOperationMismatch`
#[test]
fn test_crafted_spurious_put_at_end_key_boundary() {
    let (source, _dir_source) = setup_db![(b"\x20", b"v2"), (b"\x90", b"v9")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x20", b"changed")]);

    // end_key \xb0 doesn't exist — exclusion end proof.
    let honest = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x20"),
            Some(b"\xb0"),
            None,
        )
        .unwrap();

    // Inject Put at \xb0 (end_key). end proof is exclusion but Put expects inclusion.
    let mut ops: Vec<BatchOp<Key, Value>> = honest.batch_ops().to_vec();
    ops.push(BatchOp::Put {
        key: b"\xb0".to_vec().into(),
        value: b"fake".to_vec().into(),
    });
    ops.sort_by(|a, b| a.key().cmp(b.key()));

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(honest.start_proof().as_ref().into()),
        crate::Proof::new(honest.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    let err =
        verify_change_proof_structure(&crafted, root2.clone(), Some(b"\x20"), Some(b"\xb0"), None)
            .expect_err("structural check should reject spurious Put at end_key");
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::EndProofOperationMismatch)
    ));
}

/// Spurious Delete at `end_key` when `end_key` EXISTS (inclusion end proof).
/// The end proof is inclusion but Delete expects exclusion — mismatch.
///
/// Exercises: `EndProofOperationMismatch`
#[test]
fn test_crafted_spurious_delete_at_end_key_boundary() {
    let (source, _dir_source) = setup_db![(b"\x20", b"v2"), (b"\x90", b"v9")];

    // Change \x20, leave \x90 unchanged.
    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x20", b"changed")]);

    // end_key \x90 EXISTS — inclusion end proof.
    let honest = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x20"),
            Some(b"\x90"),
            None,
        )
        .unwrap();

    // Inject Delete at \x90 (end_key). end proof is inclusion but Delete expects exclusion.
    let mut ops: Vec<BatchOp<Key, Value>> = honest.batch_ops().to_vec();
    ops.push(BatchOp::Delete {
        key: b"\x90".to_vec().into(),
    });
    ops.sort_by(|a, b| a.key().cmp(b.key()));

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(honest.start_proof().as_ref().into()),
        crate::Proof::new(honest.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    let err =
        verify_change_proof_structure(&crafted, root2.clone(), Some(b"\x20"), Some(b"\x90"), None)
            .expect_err("structural check should reject spurious Delete at end_key");
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::EndProofOperationMismatch)
    ));
}

/// The `start_key` value MUST be checked for inclusion proofs. If the
/// proposal has a different value at `start_key` than `end_root`, the
/// reconciliation or root hash comparison should detect the mismatch.
#[test]
fn test_crafted_tampered_start_key_value_detected() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x10", b"v0"), (b"\x30", b"v1")];

    let (root1_source, root2) =
        setup_2nd_commit!(source, [(b"\x10", b"changed"), (b"\x30", b"changed")]);

    let honest = source
        .change_proof(root1_source, root2.clone(), Some(b"\x10"), None, None)
        .unwrap();

    // Verify the honest proof works.
    let ctx =
        verify_change_proof_structure(&honest, root2.clone(), Some(b"\x10"), None, None).unwrap();
    verify_and_check(&target, &honest, &ctx, root1_target.clone()).unwrap();

    // Tamper: replace Put(\x10, "changed") with Put(\x10, "WRONG").
    let mut tampered_ops: Vec<BatchOp<Key, Value>> = honest.batch_ops().to_vec();
    let idx = tampered_ops
        .iter()
        .position(|op| op.key().as_ref() == b"\x10")
        .unwrap();
    tampered_ops[idx] = BatchOp::Put {
        key: b"\x10".to_vec().into(),
        value: b"WRONG".to_vec().into(),
    };

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(honest.start_proof().as_ref().into()),
        crate::Proof::new(honest.end_proof().as_ref().into()),
        tampered_ops.into_boxed_slice(),
    );

    // Structural checks pass (hash chain is valid), but root hash
    // verification catches the tampered value.
    let ctx2 = verify_change_proof_structure(&crafted, root2, Some(b"\x10"), None, None).unwrap();
    let err = verify_and_check(&target, &crafted, &ctx2, root1_target)
        .expect_err("tampered start_key value must be detected");
    assert!(
        matches!(
            err,
            api::Error::ProofError(
                crate::ProofError::UnexpectedValue | crate::ProofError::EndRootMismatch,
            )
        ),
        "tampered start_key value must be detected, got {err:?}"
    );
}

/// When start and end proofs share a proof node at the same key but with
/// different content, reconciliation rejects with `ConflictingProofNodes`.
///
/// Take a valid bounded proof and flip a bit in the end proof's root node.
/// This breaks the structural hash chain, so we skip structural validation
/// and call `verify_change_proof_root_hash` directly. The start and end
/// proofs both contain the root node, but now they disagree.
#[test]
fn test_crafted_conflicting_proof_nodes_rejected() {
    let (db, _dir) = setup_db![(b"\x10", b"a"), (b"\x20", b"b")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x10", b"A"), (b"\x20", b"B")]);

    let valid = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(b"\x10"),
            Some(b"\x20"),
            None,
        )
        .unwrap();

    // Modify a child hash in the end proof's root node so it differs from
    // the start proof's root node (which remains untouched).
    let mut end_nodes: Vec<crate::ProofNode> = valid.end_proof().as_ref().to_vec();
    assert!(!end_nodes.is_empty(), "end proof should have nodes");

    let root_node = &mut end_nodes[0];
    let child_hash = root_node
        .child_hashes
        .iter_mut()
        .find_map(|(_, h)| h.as_mut())
        .expect("root node should have at least one child hash");
    let bytes: [u8; 32] = child_hash.clone().into_triehash().into();
    let mut new_bytes = bytes;
    new_bytes[0] ^= 1;
    *child_hash = firewood_storage::TrieHash::from(new_bytes).into_hash_type();

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(valid.start_proof().as_ref().into()),
        crate::Proof::new(end_nodes.into_boxed_slice()),
        valid.batch_ops().into(),
    );

    // Skip structural validation (the modified hash chain won't pass).
    let ctx = ChangeProofVerificationContext {
        end_root: root2,
        start_key: Some(b"\x10".to_vec().into()),
        end_key: Some(b"\x20".to_vec().into()),
        right_edge_key: Some(b"\x20".to_vec().into()),
    };

    let parent = db.revision(root1).unwrap();
    let proposal = db.apply_change_proof_to_parent(&crafted, &*parent).unwrap();
    let err = verify_change_proof_root_hash(&crafted, &ctx, &proposal).unwrap_err();
    assert!(
        matches!(
            err,
            api::Error::ProofError(crate::ProofError::ConflictingProofNodes)
        ),
        "expected ConflictingProofNodes, got {err:?}"
    );
}

/// A proof node at an odd nibble depth carrying a value must be rejected.
/// Values can only exist at even nibble depths (complete byte boundaries).
///
/// Keys `\x10` and `\x15` share nibble 1 at depth 0, then fork at depth 1
/// (nibbles 0 vs 5). The branch at depth 1 is at odd nibble length. We
/// inject a spurious value into that odd-depth proof node.
#[test]
fn test_crafted_value_at_odd_nibble_length_rejected() {
    let (db, _dir) = setup_db![(b"\x10", b"a"), (b"\x15", b"b"), (b"\x30", b"c")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x30", b"changed")]);

    let valid = db
        .change_proof(root1.clone(), root2.clone(), Some(b"\x10"), None, None)
        .unwrap();

    // Find a start proof node at odd nibble depth and inject a value.
    let mut start_nodes: Vec<crate::ProofNode> = valid.start_proof().as_ref().to_vec();
    let odd_idx = start_nodes
        .iter()
        .position(|n| n.key.len() % 2 != 0)
        .expect("should have a proof node at odd nibble depth");

    start_nodes[odd_idx].value_digest = Some(firewood_storage::ValueDigest::Value(
        b"injected".to_vec().into(),
    ));

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(start_nodes.into_boxed_slice()),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        valid.batch_ops().into(),
    );

    // Skip structural validation (injected value breaks the hash chain).
    let ctx = ChangeProofVerificationContext {
        end_root: root2,
        start_key: Some(b"\x10".to_vec().into()),
        end_key: None,
        right_edge_key: None,
    };

    let parent = db.revision(root1).unwrap();
    let proposal = db.apply_change_proof_to_parent(&crafted, &*parent).unwrap();
    let err = verify_change_proof_root_hash(&crafted, &ctx, &proposal).unwrap_err();
    assert!(
        matches!(
            err,
            api::Error::ProofError(crate::ProofError::ValueAtOddNibbleLength)
        ),
        "expected ValueAtOddNibbleLength, got {err:?}"
    );
}

/// Craft start/end proofs from two disjoint bounded proofs, skipping the
/// shared root node. The proofs diverge at depth zero — reconciliation
/// should detect the inconsistency.
#[test]
fn test_crafted_divergence_at_depth_zero() {
    let (source, target, root1_target, _ds, _dt) = setup_source_target![
        (b"\x10", b"v0"),
        (b"\x11", b"v1"),
        (b"\xa0", b"v2"),
        (b"\xa1", b"v3")
    ];
    let root1_source = root1_target.clone();

    let changes: Vec<BatchOp<&[u8], &[u8]>> = vec![
        BatchOp::Put {
            key: b"\x10",
            value: b"changed",
        },
        BatchOp::Put {
            key: b"\xa0",
            value: b"changed2",
        },
    ];
    source.propose(changes).unwrap().commit().unwrap();
    let root2 = source.root_hash().unwrap();

    let left = source
        .change_proof(
            root1_source.clone(),
            root2.clone(),
            Some(b"\x10"),
            Some(b"\x11"),
            None,
        )
        .unwrap();
    let right = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\xa0"),
            Some(b"\xa1"),
            None,
        )
        .unwrap();

    // Skip shared root to create divergence at position 0
    let s = left.start_proof().as_ref();
    let e = right.end_proof().as_ref();
    assert!(s.len() >= 2 && e.len() >= 2);

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(s[1..].into()),
        crate::Proof::new(e[1..].into()),
        Box::new([BatchOp::Put {
            key: b"\x50".to_vec().into(),
            value: b"mid".to_vec().into(),
        }]),
    );

    let parent = target.revision(root1_target).unwrap();
    let proposal = target
        .apply_change_proof_to_parent(&crafted, &*parent)
        .unwrap();

    let verification = ChangeProofVerificationContext {
        end_root: root2,
        start_key: Some(b"\x10".to_vec().into()),
        end_key: Some(b"\xa1".to_vec().into()),
        right_edge_key: Some(b"\xa1".to_vec().into()),
    };

    let err = verify_change_proof_root_hash(&crafted, &verification, &proposal).unwrap_err();
    assert!(
        matches!(
            err,
            api::Error::ProofError(
                crate::ProofError::UnexpectedValue | crate::ProofError::EndRootMismatch,
            )
        ),
        "crafted proof with skipped root should be rejected, got {err:?}"
    );
}

/// Keys \x10\x50, \x10\x58, and \x10\x5f share a branch at [1, 0, 5].
/// Deleting \x10\x50 makes the start proof an exclusion proof with
/// divergent siblings at nibbles 8 and f. The branch survives deletion
/// because two children remain. Stripping nibble 8's child hash from
/// the proof node at [1, 0, 5] should cause rejection.
#[test]
fn test_crafted_stripped_divergent_child_rejected() {
    let (source, target, root1_target, _ds, _dt) = setup_source_target![
        (b"\x10\x50", b"a"),
        (b"\x10\x58", b"b"),
        (b"\x10\x5f", b"d"),
        (b"\x30\x00", b"c")
    ];
    let root1_source = root1_target.clone();

    source
        .propose(vec![
            BatchOp::Delete {
                key: b"\x10\x50" as &[u8],
            },
            BatchOp::Put {
                key: b"\x30\x00",
                value: b"z",
            },
        ])
        .unwrap()
        .commit()
        .unwrap();
    let root2 = source.root_hash().unwrap();

    let valid = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x10\x50"),
            Some(b"\x30\x00"),
            None,
        )
        .unwrap();

    // Valid proof should pass.
    let ctx = verify_change_proof_structure(
        &valid,
        root2.clone(),
        Some(b"\x10\x50"),
        Some(b"\x30\x00"),
        None,
    )
    .unwrap();
    verify_and_check(&target, &valid, &ctx, root1_target.clone()).unwrap();

    // Find the proof node at [1, 0, 5] and strip the divergent child's
    // hash (nibble 8, leading to \x10\x58).
    let mut start_nodes: Vec<crate::ProofNode> = valid.start_proof().as_ref().to_vec();
    let branch_idx = start_nodes
        .iter()
        .position(|n| n.key.as_byte_slice() == [1, 0, 5])
        .expect("should have a proof node at [1, 0, 5]");

    let nibble_8 = firewood_storage::PathComponent::try_new(8).unwrap();
    start_nodes[branch_idx].child_hashes[nibble_8] = None;

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(start_nodes.into_boxed_slice()),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        valid.batch_ops().into(),
    );

    assert!(
        is_rejected(
            &target,
            &crafted,
            root2,
            Some(b"\x10\x50"),
            Some(b"\x30\x00"),
            root1_target
        ),
        "stripping divergent child hash should cause rejection"
    );
}

/// Security test: an attacker removes `Delete(\x05)` from `batch_ops`.
///
/// `\x05` is in-range (>= `start_key` `\x01`) and shares nibble 0 with the
/// out-of-range `\x00`. If the collapse step unconditionally strips all
/// non-on-path children (without checking for in-range keys), the proving
/// trie is flattened to match `end_root` and the tampered proof is accepted.
///
/// The verifier would then commit the incomplete `batch_ops`, leaving `\x05`
/// in their state when `end_root` says it shouldn't exist.
#[test]
fn test_crafted_omitted_delete_at_straddling_nibble() {
    // start_root: \x00, \x05, \x10
    let (db, _dir) = setup_db![(b"\x00", b"a"), (b"\x05", b"e"), (b"\x10", b"b")];
    let root1 = db.root_hash().unwrap();

    // end_root: only \x10 (both \x00 and \x05 deleted)
    db.propose(vec![
        BatchOp::Delete {
            key: b"\x00" as &[u8],
        },
        BatchOp::Delete { key: b"\x05" },
        BatchOp::Put {
            key: b"\x10",
            value: b"changed",
        },
    ])
    .unwrap()
    .commit()
    .unwrap();
    let root2 = db.root_hash().unwrap();

    // Generate the honest proof (start_key=\x01: \x00 delete is out of range)
    let honest = db
        .change_proof(root1.clone(), root2.clone(), Some(b"\x01"), None, None)
        .unwrap();

    // The honest proof should have Delete(\x05) and Put(\x10, "changed")
    assert_eq!(honest.batch_ops().len(), 2);

    // Craft a tampered proof: remove Delete(\x05), keep only Put(\x10)
    let tampered = FrozenChangeProof::new(
        crate::Proof::new(honest.start_proof().as_ref().into()),
        crate::Proof::new(honest.end_proof().as_ref().into()),
        Box::new([BatchOp::Put {
            key: b"\x10".to_vec().into(),
            value: b"changed".to_vec().into(),
        }]),
    );

    assert!(
        is_rejected(&db, &tampered, root2, Some(b"\x01"), None, root1),
        "tampered proof with omitted Delete at straddling nibble should be rejected"
    );
}

/// State injection via `collapse_root_to_path` (found by TLA+ model).
///
/// An attacker injects a spurious key at a different first nibble than the
/// proof path. The collapse strips the injected key's nibble (it's
/// "non-on-path"), so the hash computation never sees it.
/// The range-safety check in `collapse_strip` detects the in-range child
/// before stripping and rejects with `EndRootMismatch`.
#[test]
fn test_collapse_root_hides_spurious_key() {
    let (db, _dir) = setup_db![(b"\x90", b"orig")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x90", b"new!")]);

    let valid_proof = db
        .change_proof(root1.clone(), root2.clone(), None, None, None)
        .unwrap();

    assert_eq!(valid_proof.batch_ops().len(), 1);

    let mut ops: OwnedBatchOps = valid_proof.batch_ops().to_vec();
    ops.insert(
        0,
        BatchOp::Put {
            key: b"\x10".to_vec().into_boxed_slice(),
            value: b"evil".to_vec().into_boxed_slice(),
        },
    );

    let attack_proof = crate::ChangeProof::new(
        crate::Proof::new(valid_proof.start_proof().as_ref().into()),
        crate::Proof::new(valid_proof.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    assert!(
        is_rejected(&db, &attack_proof, root2, None, None, root1),
        "spurious Put at \\x10 was NOT rejected — \
         collapse_root_to_path stripped nibble 1 (non-on-path \
         relative to the end proof through nibble 9), hiding \
         the injected key from the hash computation"
    );
}

// ── Adversarial tests (5-key setup with injection helpers) ──────────────

/// 5 well-separated 32-byte keys at nibbles 0x10, 0x30, 0x50, 0x70, 0x90.
fn five_keys() -> [[u8; 32]; 5] {
    let mut arr = [[0u8; 32]; 5];
    arr[0][0] = 0x10; // A
    arr[1][0] = 0x30; // B
    arr[2][0] = 0x50; // C
    arr[3][0] = 0x70; // D
    arr[4][0] = 0x90; // E
    arr
}

/// Non-existent gap boundaries between A..B and D..E.
fn gap_boundaries() -> ([u8; 32], [u8; 32]) {
    let mut start = [0u8; 32];
    start[0] = 0x20;
    let mut end = [0u8; 32];
    end[0] = 0x80;
    (start, end)
}

/// Helper for the common adversarial pattern: generate a valid bounded proof
/// for the 5-key setup, inject a spurious operation, and assert rejection.
#[allow(clippy::too_many_arguments)]
fn inject_and_assert_rejected(
    db: &Db,
    root1: api::HashKey,
    root2: api::HashKey,
    valid_proof: &FrozenChangeProof,
    start_key: &[u8],
    end_key: &[u8],
    spurious_op: BatchOp<Box<[u8]>, Box<[u8]>>,
    msg: &str,
) {
    let mut ops: OwnedBatchOps = valid_proof.batch_ops().to_vec();
    let pos = ops
        .binary_search_by(|op| op.key().as_ref().cmp(spurious_op.key().as_ref()))
        .unwrap_or_else(|i| i);
    ops.insert(pos, spurious_op);

    let attack_proof = crate::ChangeProof::new(
        crate::Proof::new(valid_proof.start_proof().as_ref().into()),
        crate::Proof::new(valid_proof.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    assert!(
        is_rejected(
            db,
            &attack_proof,
            root2,
            Some(start_key),
            Some(end_key),
            root1
        ),
        "{msg}"
    );
}

/// Verifies that a spurious Delete for an in-range key that exists in the end
/// trie (but is unchanged between revisions) is correctly rejected.
///
/// Setup:
///   - Start trie and end trie both contain keys A, B, C, D, E (with some
///     actual changes at other keys so `batch_ops` is non-empty).
///   - The change proof is bounded by non-existent gap keys around B..D.
///   - Key C is unchanged between revisions and is NOT in `batch_ops`.
///   - An attacker adds `Delete { key: C }` to `batch_ops`.
#[test]
fn test_spurious_delete_in_range_is_rejected() {
    let keys = five_keys();
    let (db, _dir) = setup_db![
        (&keys[0], &[0xAAu8; 20]),
        (&keys[1], &[0xBBu8; 20]),
        (&keys[2], &[0xCCu8; 20]),
        (&keys[3], &[0xDDu8; 20]),
        (&keys[4], &[0xEEu8; 20])
    ];
    let (root1, root2) = setup_2nd_commit!(db, [(&keys[1], &[0xB2u8; 20])]);

    let (start_key, end_key) = gap_boundaries();
    let valid_proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(&start_key),
            Some(&end_key),
            None,
        )
        .unwrap();

    assert!(
        !is_rejected(
            &db,
            &valid_proof,
            root2.clone(),
            Some(&start_key),
            Some(&end_key),
            root1.clone(),
        ),
        "valid proof should pass"
    );

    inject_and_assert_rejected(
        &db,
        root1,
        root2,
        &valid_proof,
        &start_key,
        &end_key,
        BatchOp::Delete {
            key: keys[2].to_vec().into_boxed_slice(),
        },
        "spurious Delete of in-range key C should be rejected",
    );
}

/// Variant: the spurious delete target shares a first nibble with a key that
/// IS in `batch_ops`, forcing them into the same top-level subtree. The subtree
/// must be recomputed because it contains a changed key, so the delete of the
/// unchanged sibling should be visible.
#[test]
fn test_spurious_delete_same_subtree_as_changed_key() {
    let mut keys = five_keys();
    keys[2][0] = 0x38; // C — target, same first nibble as B (0x30)

    let (db, _dir) = setup_db![
        (&keys[0], &[0xAAu8; 20]),
        (&keys[1], &[0xBBu8; 20]),
        (&keys[2], &[0xCCu8; 20]),
        (&keys[3], &[0xDDu8; 20]),
        (&keys[4], &[0xEEu8; 20])
    ];
    let (root1, root2) = setup_2nd_commit!(db, [(&keys[1], &[0xB2u8; 20])]);

    let (start_key, end_key) = gap_boundaries();
    let valid_proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(&start_key),
            Some(&end_key),
            None,
        )
        .unwrap();

    inject_and_assert_rejected(
        &db,
        root1,
        root2,
        &valid_proof,
        &start_key,
        &end_key,
        BatchOp::Delete {
            key: keys[2].to_vec().into_boxed_slice(),
        },
        "spurious Delete of C (same subtree as changed B) was NOT rejected",
    );
}

/// Same attack with EXISTING boundary keys (inclusion proofs).
#[test]
fn test_spurious_delete_with_existing_boundaries() {
    let keys = five_keys();
    let (db, _dir) = setup_db![
        (&keys[0], &[0xAAu8; 20]),
        (&keys[1], &[0xBBu8; 20]),
        (&keys[2], &[0xCCu8; 20]),
        (&keys[3], &[0xDDu8; 20]),
        (&keys[4], &[0xEEu8; 20])
    ];
    let (root1, root2) = setup_2nd_commit!(db, [(&keys[1], &[0xB2u8; 20])]);

    let valid_proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(&keys[0]),
            Some(&keys[4]),
            None,
        )
        .unwrap();

    assert!(
        !is_rejected(
            &db,
            &valid_proof,
            root2.clone(),
            Some(&keys[0]),
            Some(&keys[4]),
            root1.clone(),
        ),
        "valid proof should pass"
    );

    inject_and_assert_rejected(
        &db,
        root1,
        root2,
        &valid_proof,
        &keys[0],
        &keys[4],
        BatchOp::Delete {
            key: keys[2].to_vec().into_boxed_slice(),
        },
        "spurious Delete with existing boundaries was NOT rejected",
    );
}

/// Control test: swapping the value of a Put that IS in `batch_ops` is correctly
/// rejected. This confirms the hybrid hash works for keys that were changed
/// between revisions (their subtree is computed from the proposal, not borrowed
/// from proof nodes).
#[test]
fn test_swapped_value_in_range_is_rejected() {
    let keys = five_keys();
    let (db, _dir) = setup_db![
        (&keys[0], &[0xAAu8; 20]),
        (&keys[1], &[0xBBu8; 20]),
        (&keys[2], &[0xCCu8; 20]),
        (&keys[3], &[0xDDu8; 20]),
        (&keys[4], &[0xEEu8; 20])
    ];
    let (root1, root2) =
        setup_2nd_commit!(db, [(&keys[1], &[0xB2u8; 20]), (&keys[3], &[0xD2u8; 20])]);

    let (start_key, end_key) = gap_boundaries();
    let valid_proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(&start_key),
            Some(&end_key),
            None,
        )
        .unwrap();

    assert!(
        valid_proof.batch_ops().len() >= 2,
        "expected at least 2 batch_ops"
    );

    let mut ops: OwnedBatchOps = valid_proof.batch_ops().to_vec();
    let first_put_idx = ops
        .iter()
        .position(|op| matches!(op, BatchOp::Put { .. }))
        .expect("should have at least one Put");
    let key = ops[first_put_idx].key().clone();
    ops[first_put_idx] = BatchOp::Put {
        key,
        value: [0xFFu8; 20].to_vec().into_boxed_slice(),
    };

    let attack_proof = crate::ChangeProof::new(
        crate::Proof::new(valid_proof.start_proof().as_ref().into()),
        crate::Proof::new(valid_proof.end_proof().as_ref().into()),
        ops.into_boxed_slice(),
    );

    assert!(
        is_rejected(
            &db,
            &attack_proof,
            root2,
            Some(&start_key),
            Some(&end_key),
            root1,
        ),
        "swapped value of in-range key B was NOT rejected"
    );
}

/// Control test: a spurious Put for a non-existent in-range key is correctly
/// rejected when the key falls in a subtree that is recomputed (not borrowed).
#[test]
fn test_spurious_put_in_range_is_rejected() {
    let keys = five_keys();
    let (db, _dir) = setup_db![
        (&keys[0], &[0xAAu8; 20]),
        (&keys[1], &[0xBBu8; 20]),
        (&keys[2], &[0xCCu8; 20]),
        (&keys[3], &[0xDDu8; 20]),
        (&keys[4], &[0xEEu8; 20])
    ];
    let (root1, root2) = setup_2nd_commit!(db, [(&keys[1], &[0xB2u8; 20])]);

    let (start_key, end_key) = gap_boundaries();
    let valid_proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(&start_key),
            Some(&end_key),
            None,
        )
        .unwrap();

    let mut fake_key = [0u8; 32];
    fake_key[0] = 0x60;
    inject_and_assert_rejected(
        &db,
        root1,
        root2,
        &valid_proof,
        &start_key,
        &end_key,
        BatchOp::Put {
            key: fake_key.to_vec().into_boxed_slice(),
            value: [0xFFu8; 20].to_vec().into_boxed_slice(),
        },
        "spurious Put of in-range key 0x60 was NOT rejected",
    );
}
