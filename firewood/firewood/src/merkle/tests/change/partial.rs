// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Partial change proofs: paginated sync where only a subset of the full
//! change set is delivered per round via a `limit` parameter. The receiver
//! commits each partial proof and resumes from the last key returned.
//!
//! When `limit` is set and fewer changes remain than the limit, the proof
//! is indistinguishable from a complete proof (`hit_limit` is false). When
//! the limit actually truncates, the end proof validates against the last
//! returned key rather than the requested `end_key`.

use super::*;
use test_case::test_case;

/// Verify that partial proofs return the expected number of batch ops
/// and pass both structural and root hash verification, across
/// combinations of `start_key`, `end_key`, and `limit`.
#[test_case(None,          None,          Some(1),  1 ; "limit 1 of 3 truncates")]
#[test_case(None,          None,          Some(2),  2 ; "limit 2 of 3 truncates")]
#[test_case(None,          None,          Some(3),  3 ; "limit equals change count not truncated")]
#[test_case(None,          None,          Some(10), 3 ; "limit exceeds change count not truncated")]
#[test_case(Some(b"\x60"), None,          Some(1),  0 ; "start beyond all changes gives empty proof")]
#[test_case(None,          Some(b"\x05"), Some(1),  0 ; "end before first change gives empty proof")]
#[test_case(Some(b"\x20"), Some(b"\x40"), Some(1),  1 ; "bounded and limited")]
#[test_case(Some(b"\x20"), Some(b"\x40"), None,     1 ; "bounded all fit")]
fn test_partial_proof_verified(
    start_key: Option<&[u8]>,
    end_key: Option<&[u8]>,
    limit: Option<usize>,
    expected_ops: usize,
) {
    let (source, _dir_source) = setup_db![(b"\x10", b"v0"), (b"\x30", b"v1"), (b"\x50", b"v2")];
    let (target, _dir_target) = setup_db![(b"\x10", b"v0"), (b"\x30", b"v1"), (b"\x50", b"v2")];
    let root1_target = target.root_hash().unwrap();
    let (root1_source, root2) = setup_2nd_commit!(
        source,
        [(b"\x10", b"c0"), (b"\x30", b"c1"), (b"\x50", b"c2")]
    );
    let limit_nz = limit.and_then(NonZeroUsize::new);

    let proof = source
        .change_proof(root1_source, root2.clone(), start_key, end_key, limit_nz)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), expected_ops);

    let ctx = verify_change_proof_structure(&proof, root2, start_key, end_key, limit_nz).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Sync source→target using a partial proof of 1 key (limit=1), commit,
/// then a proof of the remaining keys (2 passes).
#[test]
fn test_partial_proof_round_trip() {
    let (source, _dir_source) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let (target, _dir_target) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_target = target.root_hash().unwrap();

    let (root1_source, root2) = setup_2nd_commit!(
        source,
        [(b"\x10", b"c0"), (b"\x20", b"c1"), (b"\x30", b"c2")]
    );
    assert_eq!(root1_source, root1_target);

    // Round 1: partial (limit=1)
    let proof1 = source
        .change_proof(
            root1_source.clone(),
            root2.clone(),
            None,
            None,
            NonZeroUsize::new(1),
        )
        .unwrap();
    let ctx1 =
        verify_change_proof_structure(&proof1, root2.clone(), None, None, NonZeroUsize::new(1))
            .unwrap();
    verify_and_check(&target, &proof1, &ctx1, root1_target.clone()).unwrap();

    // Commit round 1
    let parent1 = target.revision(root1_target).unwrap();
    let proposal1 = target
        .apply_change_proof_to_parent(&proof1, &*parent1)
        .unwrap();
    let root_target_after_1 = proposal1.root_hash().unwrap();
    proposal1.commit().unwrap();

    // Round 2: continue from last key
    let next_start = proof1.batch_ops().last().unwrap().key().clone();
    let proof2 = source
        .change_proof(root1_source, root2.clone(), Some(&next_start), None, None)
        .unwrap();
    let ctx2 =
        verify_change_proof_structure(&proof2, root2, Some(&next_start), None, None).unwrap();
    verify_and_check(&target, &proof2, &ctx2, root_target_after_1).unwrap();
}

/// Partial proof (limit=2) where the last batch op before the limit is a
/// Delete, not a Put. This matters because the end proof for a truncated
/// proof validates against the last returned key — a Put produces an
/// inclusion proof, but a Delete produces an exclusion proof, exercising
/// a different verification path.
#[test]
fn test_partial_proof_with_delete_last_op() {
    let (source, _dir_source) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let (target, _dir_target) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_target = target.root_hash().unwrap();

    let changes: Vec<BatchOp<&[u8], &[u8]>> = vec![
        BatchOp::Put {
            key: b"\x10",
            value: b"changed",
        },
        BatchOp::Delete { key: b"\x20" },
        BatchOp::Put {
            key: b"\x30",
            value: b"changed2",
        },
    ];
    source.propose(changes).unwrap().commit().unwrap();
    let root2 = source.root_hash().unwrap();

    let proof = source
        .change_proof(
            root1_target.clone(),
            root2.clone(),
            None,
            None,
            NonZeroUsize::new(2),
        )
        .unwrap();
    let ctx =
        verify_change_proof_structure(&proof, root2, None, None, NonZeroUsize::new(2)).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}
