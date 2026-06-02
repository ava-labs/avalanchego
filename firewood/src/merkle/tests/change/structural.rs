// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Structural validation of change proofs (Phase 1).
//!
//! One test per error variant in [`verify_change_proof_structure`], covering
//! argument validation, boundary proof presence, key ordering, and hash chain
//! verification against `end_root`.
//!
//! Variants tested elsewhere:
//! - `StartProofOperationMismatch` — attack.rs (adversarial boundary injection)
//! - `EndProofOperationMismatch` — attack.rs (adversarial boundary injection)

use super::*;
use test_case::test_case;

#[test]
fn test_inverted_range_rejected() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let proof = db
        .change_proof(root1, root2.clone(), Some(b"\x10"), Some(b"\xa0"), None)
        .unwrap();

    let err = verify_change_proof_structure(&proof, root2, Some(b"\xa0"), Some(b"\x10"), None)
        .unwrap_err();
    assert!(matches!(
        err,
        api::Error::InvalidRange { start_key, end_key }
            if *start_key == *b"\xa0" && *end_key == *b"\x10"
    ));
}

/// Start proof present but `requested_start_key` is None — the proof
/// is unverifiable so the structural check rejects it.
#[test]
fn test_unexpected_start_proof() {
    let (db, _dir) = setup_db![(b"\x00", b"v0"), (b"\x10", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x05", b"v2")]);

    let proof = db
        .change_proof(root1, root2.clone(), Some(b"\x00"), Some(b"\x10"), None)
        .unwrap();

    let err = verify_change_proof_structure(&proof, root2, None, Some(b"\x10"), None).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::UnexpectedStartProof)
    ));
}

/// The `key <= prev.key()` check rejects both duplicate and reverse-ordered keys.
#[test_case(b"\x50", b"\x50" ; "duplicate keys rejected")]
#[test_case(b"\xa0", b"\x50" ; "reverse order rejected")]
#[test_case(b"\x50", b"" ; "empty key after non-empty rejected")]
fn test_crafted_out_of_order_or_dup_keys(key_a: &[u8], key_b: &[u8]) {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let valid = db
        .change_proof(root1, root2.clone(), None, None, None)
        .unwrap();

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(valid.start_proof().as_ref().into()),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        Box::new([
            BatchOp::Put {
                key: key_a.to_vec().into(),
                value: b"a".to_vec().into(),
            },
            BatchOp::Put {
                key: key_b.to_vec().into(),
                value: b"b".to_vec().into(),
            },
        ]),
    );

    let err = verify_change_proof_structure(&crafted, root2, None, None, None).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::ChangeProofKeysNotSorted)
    ));
}

#[test]
fn test_start_key_larger_than_first_key() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let proof = db
        .change_proof(root1, root2.clone(), None, None, None)
        .unwrap();
    let err = verify_change_proof_structure(&proof, root2, Some(b"\xff"), None, None).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::StartKeyLargerThanFirstKey)
    ));
}

#[test]
fn test_end_key_less_than_last_key() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let proof = db
        .change_proof(root1, root2.clone(), None, None, None)
        .unwrap();
    let err = verify_change_proof_structure(&proof, root2, None, Some(b"\x01"), None).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::EndKeyLessThanLastKey)
    ));
}

/// Proof has 2 batch ops but `max_length` is 1.
#[test]
fn test_proof_larger_than_max_length() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid_"), (b"\x60", b"mid2")]);

    let proof = db
        .change_proof(root1, root2.clone(), None, None, None)
        .unwrap();
    let err =
        verify_change_proof_structure(&proof, root2, None, None, NonZeroUsize::new(1)).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::ProofIsLargerThanMaxLength)
    ));
}

/// Non-empty `batch_ops` with `requested_start_key` and `requested_end_key`
/// but both boundary proofs empty.
#[test]
fn test_crafted_missing_boundary_proof() {
    let proof = FrozenChangeProof::new(
        crate::Proof::new(Box::new([])),
        crate::Proof::new(Box::new([])),
        Box::new([BatchOp::Put {
            key: b"\x50".to_vec().into(),
            value: b"value".to_vec().into(),
        }]),
    );
    let err = verify_change_proof_structure(
        &proof,
        api::HashKey::empty(),
        Some(b"\x10"),
        Some(b"\xa0"),
        None,
    )
    .unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::MissingBoundaryProof)
    ));
}

/// Non-empty `batch_ops` with start proof present but end proof stripped.
#[test]
fn test_crafted_missing_end_proof() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let valid = db
        .change_proof(root1, root2.clone(), None, None, None)
        .unwrap();
    assert!(
        !valid.end_proof().is_empty(),
        "generator must produce end proof for non-empty ops"
    );

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(valid.start_proof().as_ref().into()),
        crate::Proof::new(Box::new([])),
        valid.batch_ops().into(),
    );
    let err = verify_change_proof_structure(&crafted, root2, None, None, None).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::MissingEndProof)
    ));
}

/// Non-empty end proof with empty `batch_ops` and no `requested_end_key`.
#[test]
fn test_crafted_unexpected_end_proof() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x20", b"changed")]);

    let valid = db
        .change_proof(root1, root2.clone(), None, Some(b"\x20"), None)
        .unwrap();
    assert!(!valid.end_proof().is_empty());

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(Box::new([])),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        Box::new([]),
    );

    let err = verify_change_proof_structure(&crafted, root2, None, None, None).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::UnexpectedEndProof)
    ));
}

/// Boundary proof hash chain was built for `root2` but verified against
/// `HashKey::empty()` — the root hash in the proof nodes won't match,
/// producing `UnexpectedHash`.
#[test]
fn test_boundary_proof_rejects_wrong_root() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let proof = db
        .change_proof(root1, root2, Some(b"\x10"), Some(b"\xa0"), None)
        .unwrap();
    let err = verify_change_proof_structure(
        &proof,
        api::HashKey::empty(),
        Some(b"\x10"),
        Some(b"\xa0"),
        None,
    )
    .unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::EdgeProofHashMismatch {
            edge: crate::ProofEdge::Left,
            ..
        })
    ));
}

/// Right-edge companion to `test_boundary_proof_rejects_wrong_root`: with no
/// `start_key` the start proof is empty and skipped, so the wrong-root failure
/// surfaces on the *end* boundary and must be annotated `ProofEdge::Right`.
#[test]
fn test_boundary_proof_rejects_wrong_root_right_edge() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let proof = db
        .change_proof(root1, root2, None, Some(b"\xa0"), None)
        .unwrap();
    assert!(proof.start_proof().is_empty());
    let err =
        verify_change_proof_structure(&proof, api::HashKey::empty(), None, Some(b"\xa0"), None)
            .unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::EdgeProofHashMismatch {
            edge: crate::ProofEdge::Right,
            ..
        })
    ));
}

#[test]
fn test_crafted_delete_range_rejected() {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\xa0", b"v1")];
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x50", b"mid")]);

    let valid = db
        .change_proof(root1, root2.clone(), None, None, None)
        .unwrap();

    let crafted = FrozenChangeProof::new(
        crate::Proof::new(valid.start_proof().as_ref().into()),
        crate::Proof::new(valid.end_proof().as_ref().into()),
        Box::new([BatchOp::DeleteRange {
            prefix: b"\x50".to_vec().into(),
        }]),
    );

    let err = verify_change_proof_structure(&crafted, root2, None, None, None).unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::DeleteRangeFoundInChangeProof)
    ));
}

#[test]
fn test_crafted_missing_end_proof_empty_batch_ops() {
    let proof = FrozenChangeProof::new(
        crate::Proof::new(Box::new([])),
        crate::Proof::new(Box::new([])),
        Box::new([]),
    );
    let err =
        verify_change_proof_structure(&proof, api::HashKey::empty(), None, Some(b"\x50"), None)
            .unwrap_err();
    assert!(matches!(
        err,
        api::Error::ProofError(crate::ProofError::MissingEndProof)
    ));
}
