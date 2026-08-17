// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! End-to-end change proof verification across boundary configurations:
//! no edges, left only, right only, and both edges.

use super::*;
use test_case::test_case;

/// Left edge proof is present iff `requested_start_key` is set. Right edge
/// proof is always present (generator produces one for non-empty batch ops).
#[test_case(None,          None,          b"\x20" ; "full keyrange")]
#[test_case(None,          Some(b"\x30"), b"\x10" ; "provided right edge")]
#[test_case(Some(b"\x10"), None,          b"\x30" ; "provided left edge")]
#[test_case(Some(b"\x10"), Some(b"\x30"), b"\x20" ; "provided both edges")]
#[test_case(Some(b"\x15"), Some(b"\x25"), b"\x20" ; "both edges with gap keys")]
fn test_verify(start_key: Option<&[u8]>, end_key: Option<&[u8]>, change_key: &[u8]) {
    let (source, _dir_source) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let (target, _dir_target) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_target = target.root_hash().unwrap();

    let (root1_source, root2) = setup_2nd_commit!(source, [(change_key, b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), start_key, end_key, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    assert_eq!(proof.start_proof().is_empty(), start_key.is_none());
    assert!(!proof.end_proof().is_empty());

    let ctx =
        verify_change_proof_structure(&proof, root2.clone(), start_key, end_key, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target.clone()).unwrap();

    // Apply the proof to target and verify roots converge.
    let parent = target.revision(root1_target).unwrap();
    let proposal = target
        .apply_change_proof_to_parent(&proof, &*parent)
        .unwrap();
    proposal.commit().unwrap();
    assert_eq!(target.root_hash().unwrap(), root2);
}

/// Delete a key outside the requested range while changing one inside it.
/// The start boundary (`b"\x01"`) falls between the deleted key (`b"\x00"`)
/// and the changed key (`b"\x10"`), so the delete is out-of-range and the
/// proof must still verify.
#[test]
fn test_inherited_key_at_start_boundary_accepted() {
    let (db, _dir) = setup_db![(b"\x00", b"a"), (b"\x10", b"b")];
    let root1 = db.root_hash().unwrap();
    db.propose(vec![
        BatchOp::Delete {
            key: b"\x00" as &[u8],
        },
        BatchOp::Put {
            key: b"\x10",
            value: b"changed",
        },
    ])
    .unwrap()
    .commit()
    .unwrap();
    let root2 = db.root_hash().unwrap();

    let proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(b"\x01".as_ref()),
            None,
            None,
        )
        .unwrap();

    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x01"), None, None).unwrap();
    verify_and_check(&db, &proof, &ctx, root1).unwrap();
}

/// When `requested_end_key` is set and the proof is complete (not truncated),
/// the right edge proof anchors at `requested_end_key`, not the last batch key.
#[test]
fn test_generator_uses_end_key_for_complete_proof() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x10", b"changed")]);

    // end_key far beyond last change, no limit — complete proof
    let proof = db
        .change_proof(root1, root2.clone(), None, Some(b"\xff"), None)
        .unwrap();

    // End proof validates against end_key for complete proofs
    proof.end_proof().value_digest(b"\xff", &root2).unwrap();
}
