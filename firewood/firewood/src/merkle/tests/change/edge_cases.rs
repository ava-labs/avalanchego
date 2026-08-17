// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Edge cases: odd depths, divergence, exclusion, deletion, children.

use super::*;
use std::num::NonZeroUsize;
use tempfile::TempDir;
use test_case::test_case;

#[test]
fn test_empty_batch_ops_with_nonempty_proofs() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];

    // Change only \x30 (outside range [\x10, \x20])
    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x30", b"changed")]);

    let proof = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x10"),
            Some(b"\x20"),
            None,
        )
        .unwrap();
    assert!(proof.batch_ops().is_empty());
    assert!(!proof.start_proof().is_empty());
    assert!(!proof.end_proof().is_empty());

    let ctx =
        verify_change_proof_structure(&proof, root2, Some(b"\x10"), Some(b"\x20"), None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

#[test]
fn test_odd_depth_proof_node_accepted() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x12", b"v0"), (b"\x13", b"v1"), (b"\x50", b"v2")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x50", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\x14"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    assert!(
        proof.start_proof().last().unwrap().key.len() % 2 == 1,
        "start proof should be an exclusion proof with an odd nibble depth"
    );
    assert!(
        proof
            .start_proof()
            .value_digest(b"\x14", &root2)
            .unwrap()
            .is_none()
    );
    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x14"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

#[test]
fn test_start_proof_inclusion_with_children_below() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\xab", b"v0"), (b"\xab\xcd", b"v1"), (b"\xf0", b"v2")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\xf0", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\xab"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    assert!(
        proof
            .start_proof()
            .value_digest(b"\xab", &root2)
            .unwrap()
            .is_some(),
        "start proof should be an inclusion proof for \\xab"
    );
    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\xab"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

#[test]
fn test_end_proof_inclusion_with_children_below() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\xab", b"v0"), (b"\xab\xcd", b"v1"), (b"\xf0", b"v2")];

    let (root1_source, root2) = setup_2nd_commit!(
        source,
        [(b"\xab", b"c0"), (b"\xab\xcd", b"c1"), (b"\xf0", b"c2")]
    );

    // Limited to 1 — last_op_key \xab is a prefix with children below.
    let proof = source
        .change_proof(
            root1_source,
            root2.clone(),
            None,
            None,
            NonZeroUsize::new(1),
        )
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    assert!(proof.start_proof().is_empty());
    let ctx =
        verify_change_proof_structure(&proof, root2, None, None, NonZeroUsize::new(1)).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

#[test]
fn test_divergence_parent_start_key_exhausted() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x12\x01", b"v0"), (b"\x12\x02", b"v1"), (b"\xf0", b"v2")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\xf0", b"changed")]);

    let proof = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x12"),
            Some(b"\xf0"),
            None,
        )
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    // Start key is exhausted at the parent branch — exclusion proof.
    assert!(
        proof
            .start_proof()
            .value_digest(b"\x12", &root2)
            .unwrap()
            .is_none(),
        "start proof should be an exclusion proof for \\x12"
    );
    let ctx =
        verify_change_proof_structure(&proof, root2, Some(b"\x12"), Some(b"\xf0"), None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

// Verify that the children of the last node in the start proof are checked
// during verification. The start bound \x10 is a branch node (not a leaf),
// so the verifier must inspect its children (\x10\x01, \x10\x02) to ensure
// they haven't been tampered with. The actual change is to \x30, which is
// on a completely separate branch, so this isolates the child-checking logic.
//
// Trie structure (both dbs start identical):
//
//       [root]
//       /    \
//   [0x1_]   [0x3_]
//    /   \      |
// [_0_1] [_0_2] [_0]
//  "a"    "b"   "c"
//
// The change proof query uses start=\x10 (the branch node), end=None.
// Only \x30 changes ("c" -> "changed"), but the verifier must still
// validate that \x10's children are intact.
#[test]
fn test_start_tail_last_node_children_checked() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x10\x01", b"a"), (b"\x10\x02", b"b"), (b"\x30", b"c")];

    // Only modify \x30 on the source; \x10's children are untouched
    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x30", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\x10"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    // Exclusion proof — last node is a branch whose children must be verified.
    assert!(
        proof
            .start_proof()
            .value_digest(b"\x10", &root2)
            .unwrap()
            .is_none(),
        "start proof should be an exclusion proof for \\x10"
    );
    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x10"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

#[test]
fn test_start_proof_exclusion_for_deleted_key() {
    let (source, _dir_source) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_source = source.root_hash().unwrap();
    let (target, _dir_target) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_target = target.root_hash().unwrap();

    source
        .propose(vec![
            BatchOp::Delete { key: b"\x10" },
            BatchOp::Put {
                key: b"\x30",
                value: b"changed",
            },
        ])
        .unwrap()
        .commit()
        .unwrap();
    let root2 = source.root_hash().unwrap();

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\x10"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 2); // Delete(\x10) + Put(\x30)
    assert!(
        proof
            .start_proof()
            .value_digest(b"\x10", &root2)
            .unwrap()
            .is_none(),
        "start proof should be an exclusion proof for deleted \\x10"
    );
    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x10"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Verify that change proof verification succeeds when the only key
/// changed between two revisions falls outside the requested range,
/// so the proof's `batch_ops` for the queried subrange is empty.
///
/// R1 has `\x00`, R2 adds `\x00\x10`. The query range
/// `[\x00\x10\x20, \x00\x10\x30]` excludes the new key. The shared
/// prefix node at `\x00\x10` carries a value that legitimately differs
/// between the proposal (R1 + empty ops) and the end trie (R2), but
/// because `\x00\x10` < `\x00\x10\x20` the value check should be
/// skipped.
#[test]
fn test_disjoint_proof() {
    // R1: trie with a single key \x00
    let (db, _dir) = setup_db![(b"\x00", b"v0")];

    // R2: add \x00\x10 (outside query range [\x00\x10\x20, \x00\x10\x30])
    let (root1, root2) = setup_2nd_commit!(db, [(b"\x00\x10", b"v1")]);

    let first = b"\x00\x10\x20";
    let last = b"\x00\x10\x30";

    let proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(first.as_ref()),
            Some(last.as_ref()),
            None,
        )
        .unwrap();
    assert!(proof.batch_ops().is_empty());

    let verification = verify_change_proof_structure(
        &proof,
        root2,
        Some(first.as_slice()),
        Some(last.as_slice()),
        None,
    )
    .unwrap();

    verify_and_check(&db, &proof, &verification, root1).unwrap();
}

/// The `prove()` fix includes divergent children in start proofs. This
/// exercises the fixed path — a Delete near `start_key` under a shared
/// branch produces a valid proof. Before the fix, the proof generator
/// omitted the divergent child, leaving a verification gap.
#[test]
fn test_boundary_child_gap_closed_for_start_key() {
    // \x10\x50 and \x10\x58 share a branch at nibble path [1,0].
    let (source, _dir_source) = setup_db![
        (b"\x10\x50", b"a"),
        (b"\x10\x58", b"b"),
        (b"\x30\x00", b"c")
    ];
    let root1_source = source.root_hash().unwrap();
    let (target, _dir_target) = setup_db![
        (b"\x10\x50", b"a"),
        (b"\x10\x58", b"b"),
        (b"\x30\x00", b"c")
    ];
    let root1_target = target.root_hash().unwrap();

    // Delete \x10\x50, change \x30\x00. \x10\x58 is unchanged.
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

    let honest = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x10\x50"),
            Some(b"\x30\x00"),
            None,
        )
        .unwrap();
    assert_eq!(honest.batch_ops().len(), 2); // Delete(\x10\x50) + Put(\x30\x00)

    let ctx =
        verify_change_proof_structure(&honest, root2, Some(b"\x10\x50"), Some(b"\x30\x00"), None)
            .unwrap();
    verify_and_check(&target, &honest, &ctx, root1_target).unwrap();
}

/// Case A of `compute_outside_children`: boundary key diverges within
/// the terminal node's partial path.
///
/// Keys `\x10\x50` and `\x10\x58` share a branch with partial path containing
/// nibble 5. `start_key` `\x10\x40` diverges at nibble 4 < 5, so the start
/// boundary is "before" the terminal — no children are marked outside.
#[test]
fn test_terminal_divergence_within_partial_path() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x10\x50", b"a"), (b"\x10\x58", b"b"), (b"\x30", b"c")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x30", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\x10\x40"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x10\x40"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Case B of `compute_outside_children`: terminal is an ancestor of the
/// boundary key, and all children are outside the range. Their hashes
/// must come from the proof node, not the proving trie.
///
/// Keys `\x10\x01`, `\x10\x02`, `\x10\x03` create a branch at `[1,0,0]`
/// with children at nibbles 1, 2, and 3. `start_key` `\x10\x05` has
/// on-path nibble 5 at the terminal, so nibbles 1, 2, and 3 are all
/// below 5 and marked outside.
#[test]
fn test_terminal_ancestor_all_children_outside() {
    let (source, target, root1_target, _ds, _dt) = setup_source_target![
        (b"\x10\x01", b"a"),
        (b"\x10\x02", b"b"),
        (b"\x10\x03", b"c"),
        (b"\x30", b"d")
    ];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x30", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\x10\x05"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);

    // Terminal is at [1, 0, 0] — ancestor of start_key [1, 0, 0, 5].
    // All children (nibbles 1, 2, 3) are below on-path nibble 5 → all outside.
    let terminal = proof.start_proof().as_ref().last().unwrap();
    assert_eq!(terminal.key.len(), 3);

    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x10\x05"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Keys `\x10\x01`, `\x10\x02`, `\x10\x03` share nibble path `[1,0,0]`
/// and diverge at the 4th nibble. There is no node at `[1,0]` due to path
/// compression — the branch is at `[1,0,0]`. The start proof for `\x10`
/// (nibbles `[1,0]`) includes this branch as a divergent node since the
/// key is exhausted before the node's partial path is fully consumed.
/// All children of this terminal are in-range (>= `\x10`), so none are
/// marked outside by `compute_outside_children`.
#[test]
fn test_terminal_divergent_node_all_children_in_range() {
    let (source, target, root1_target, _ds, _dt) = setup_source_target![
        (b"\x10\x01", b"a"),
        (b"\x10\x02", b"b"),
        (b"\x10\x03", b"c"),
        (b"\x30", b"d")
    ];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x30", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\x10"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);

    // Terminal proof node is at [1, 0, 0]. There is no node at [1, 0] because
    // path compression merges it into the branch at [1, 0, 0]. The path iterator
    // includes this as a divergent node since start_key \x10 (nibbles [1, 0])
    // is exhausted before the node's partial path is fully consumed.
    let terminal = proof.start_proof().as_ref().last().unwrap();
    assert_eq!(terminal.key.len(), 3);

    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x10"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Case C of `compute_outside_children` (right edge): boundary is a
/// prefix of the terminal node's key. All children extend beyond
/// `end_key`, so all are marked outside.
///
/// Key `\x20` is a prefix of `\x20\xab`. `end_key` = `\x20` — children of
/// the `\x20` node extend beyond `end_key` and must use proof hashes.
#[test]
fn test_right_edge_boundary_prefix_of_terminal() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x10", b"a"), (b"\x20", b"b"), (b"\x20\xab", b"c")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x10", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), None, Some(b"\x20"), None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    let ctx = verify_change_proof_structure(&proof, root2, None, Some(b"\x20"), None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// An out-of-range prefix node's value changes between root1 and root2.
/// The proof's value must be adopted during `reconcile_branch_proof_node`
/// so the hybrid hash matches `end_root`.
///
/// `\x10` has a value that changes from `"old_pfx"` to `"new_pfx"`. The query
/// range `[\x10\x50, \x30]` excludes `\x10` (it's a proper prefix of `start_key`),
/// so `\x10` is out-of-range. The start proof path passes through `\x10`, and
/// reconciliation must adopt the proof's value (`"new_pfx"`) to match `end_root`.
#[test]
fn test_out_of_range_value_change_adopted() {
    let (source, target, root1_target, _ds, _dt) = setup_source_target![
        (b"\x10", b"old_pfx"),
        (b"\x10\x50", b"val0000"),
        (b"\x30", b"val1000")
    ];

    let (root1_source, root2) =
        setup_2nd_commit!(source, [(b"\x10", b"new_pfx"), (b"\x30", b"changed")]);

    let proof = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x10\x50"),
            Some(b"\x30"),
            None,
        )
        .unwrap();
    // Only \x30 is in range [\x10\x50, \x30]; \x10's change is out of range.
    assert_eq!(proof.batch_ops().len(), 1);
    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x10\x50"), Some(b"\x30"), None)
        .unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// A key that is a proper prefix of `start_key` has a value in root1 that
/// is deleted in root2. Because the prefix key is outside the query range
/// (shorter key < `start_key`), it is not in `batch_ops`. The proving trie
/// must clear this stale value so the computed hash matches `end_root`.
///
/// Previously, `reconcile_branch_proof_node` returned early when the proof
/// node had no value, leaving the stale value in the proving trie. This
/// caused `EndRootMismatch` during root hash verification. Now, the
/// out-of-range callback clears the stale value so the hash matches
/// `end_root`.
#[test]
fn test_change_proof_prefix_key_deleted_in_end_root() {
    let (db, _dir) = setup_db![
        (b"\xab", b"prefix_value"),
        (b"\xab\xcd", b"full_key"),
        (b"\xab\xef", b"sibling"),
        (b"\xff", b"high"),
    ];
    let root1 = db.root_hash().unwrap();

    db.propose(vec![
        BatchOp::Delete {
            key: b"\xab" as &[u8],
        },
        BatchOp::Put {
            key: b"\xab\xcd",
            value: b"updated",
        },
    ])
    .unwrap()
    .commit()
    .unwrap();
    let root2 = db.root_hash().unwrap();

    // b"\xab" < b"\xab\x00", so the deleted prefix key is outside the range.
    // The start proof passes through [a,b] where root1 has a value and root2
    // does not — reconciliation must clear the stale value.
    let proof = db
        .change_proof(
            root1.clone(),
            root2.clone(),
            Some(b"\xab\x00".as_slice()),
            Some(b"\xff".as_slice()),
            None,
        )
        .unwrap();
    // Only \xab\xcd update is in range; \xab deletion is out of range.
    assert_eq!(proof.batch_ops().len(), 1);

    let ctx = verify_change_proof_structure(
        &proof,
        root2.clone(),
        Some(b"\xab\x00".as_slice()),
        Some(b"\xff".as_slice()),
        None,
    )
    .unwrap();

    verify_and_check(&db, &proof, &ctx, root1).unwrap();
}

/// Mirror of `test_start_proof_exclusion_for_deleted_key` for the end side.
/// `end_key` existed in root1 but is deleted in root2, so the end proof
/// is an exclusion proof. Start and end proofs take different code paths
/// (`compute_right_edge_key`, `verify_boundary_proof` with different
/// `boundary_op`), so both sides need coverage.
#[test]
fn test_end_proof_exclusion_for_deleted_key() {
    let (source, _dir_source) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_source = source.root_hash().unwrap();
    let (target, _dir_target) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_target = target.root_hash().unwrap();

    source
        .propose(vec![
            BatchOp::Put {
                key: b"\x10",
                value: b"changed",
            },
            BatchOp::Delete { key: b"\x30" },
        ])
        .unwrap()
        .commit()
        .unwrap();
    let root2 = source.root_hash().unwrap();

    let proof = source
        .change_proof(root1_source, root2.clone(), None, Some(b"\x30"), None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 2); // Put(\x10) + Delete(\x30)
    assert!(
        proof
            .end_proof()
            .value_digest(b"\x30", &root2)
            .unwrap()
            .is_none(),
        "end proof should be an exclusion proof for deleted \\x30"
    );
    let ctx = verify_change_proof_structure(&proof, root2, None, Some(b"\x30"), None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Single-point range: `start_key == end_key`. Both boundary proofs anchor
/// at the same key. If that key was changed, `batch_ops` has exactly 1 entry.
#[test]
fn test_single_point_range() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];

    let (root1_source, root2) = setup_2nd_commit!(source, [(b"\x20", b"changed")]);

    let proof = source
        .change_proof(
            root1_source,
            root2.clone(),
            Some(b"\x20"),
            Some(b"\x20"),
            None,
        )
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    // Both proofs anchor at the same key — start is inclusion, end is inclusion.
    assert!(
        proof
            .start_proof()
            .value_digest(b"\x20", &root2)
            .unwrap()
            .is_some()
    );
    assert!(
        proof
            .end_proof()
            .value_digest(b"\x20", &root2)
            .unwrap()
            .is_some()
    );
    let ctx =
        verify_change_proof_structure(&proof, root2, Some(b"\x20"), Some(b"\x20"), None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Out-of-range deletion compresses the root's `partial_path`. The proving
/// trie's root (forked from the proposal) retains the old shape, and
/// `collapse_root_to_path` must reshape it to match `end_root`.
///
/// Root1: keys at nibbles 0 and 1. Root at `[]`.
/// Root2: `\x01` deleted → root compresses to `partial_path` `[1]`.
/// Bounded proof `[\x10, None]` excludes the deleted key.
///
/// Found by `ChangeProofVerification.tla` `HonestProofAccepted` invariant.
#[test]
fn test_out_of_range_root_compression() {
    let (db, _dir) = setup_db![
        (b"\x01", b"alpha"),
        (b"\x10", b"betax"),
        (b"\x11", b"gamma")
    ];
    let root1 = db.root_hash().unwrap();

    db.propose(vec![
        BatchOp::Delete { key: b"\x01" },
        BatchOp::Put {
            key: b"\x10",
            value: b"beta2",
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
            Some(b"\x10".as_slice()),
            None,
            None,
        )
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    // After deleting \x01, root compresses from partial_path [] to [1].
    assert_eq!(
        proof
            .start_proof()
            .first()
            .unwrap()
            .key
            .iter()
            .map(|c| c.as_u8())
            .collect::<Vec<_>>(),
        vec![1],
        "root should compress to [1] after deletion"
    );

    let ctx =
        verify_change_proof_structure(&proof, root2.clone(), Some(b"\x10"), None, None).unwrap();
    verify_and_check(&db, &proof, &ctx, root1).unwrap();
}

/// Reverse of root compression: out-of-range insert adds a key at a new
/// first nibble, expanding root's `partial_path` from `[1]` to `[]`.
#[test]
fn test_out_of_range_root_expansion() {
    let (source, _dir_source) = setup_db![(b"\x10", b"a"), (b"\x11", b"b")];
    let root1_source = source.root_hash().unwrap();
    let (target, _dir_target) = setup_db![(b"\x10", b"a"), (b"\x11", b"b")];
    let root1_target = target.root_hash().unwrap();

    // Insert \x01 (nibble 0, out of range) and change \x10 (in range).
    // Root expands from partial_path [1] to [].
    source
        .propose(vec![
            BatchOp::Put {
                key: b"\x01" as &[u8],
                value: b"new" as &[u8],
            },
            BatchOp::Put {
                key: b"\x10",
                value: b"changed",
            },
        ])
        .unwrap()
        .commit()
        .unwrap();
    let root2 = source.root_hash().unwrap();

    let proof = source
        .change_proof(root1_source, root2.clone(), Some(b"\x10"), None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    // After inserting \x01, root expands from partial_path [1] to [].
    assert!(
        proof.start_proof().first().unwrap().key.is_empty(),
        "root should expand to [] after insertion"
    );

    let ctx = verify_change_proof_structure(&proof, root2, Some(b"\x10"), None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// The root node itself has a value (key `b""`) that changes between
/// revisions. Tests that root-level value deltas are handled correctly.
#[test]
fn test_root_value_change() {
    let (source, target, root1_target, _ds, _dt) =
        setup_source_target![(b"", b"root_old"), (b"\x10", b"v0")];

    let (root1_source, root2) =
        setup_2nd_commit!(source, [(b"", b"root_new"), (b"\x10", b"changed")]);

    let proof = source
        .change_proof(root1_source, root2.clone(), None, None, None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 2);

    let ctx = verify_change_proof_structure(&proof, root2, None, None, None).unwrap();
    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// Change proof with `ValueDigest::Hash` at out-of-range prefix keys.
/// In merkledb mode, values >= 32 bytes are serialized as Hash. The
/// proving trie may not have the value at these out-of-range positions,
/// so `compute_root_hash_with_proofs` must fall back to the proof node's
/// Hash digest.
#[cfg(not(feature = "ethhash"))]
#[test]
fn test_change_proof_with_hashed_out_of_range_value() {
    let (source, target, root1_target, _ds, _dt) = setup_source_target![
        (b"\x10", [0u8; 64].as_slice()),
        (b"\x10\x50", b"child"),
        (b"\x30", b"other")
    ];

    // Change \x10's value (out of range) and \x30 (in range).
    source
        .propose(vec![
            BatchOp::Put {
                key: b"\x10" as &[u8],
                value: [1u8; 64].as_slice(),
            },
            BatchOp::Put {
                key: b"\x30",
                value: b"changed",
            },
        ])
        .unwrap()
        .commit()
        .unwrap();
    let root2 = source.root_hash().unwrap();

    // Range [\x10\x50, \x30]: \x10 is out of range (prefix of start_key).
    let proof = source
        .change_proof(
            root1_target.clone(),
            root2.clone(),
            Some(b"\x10\x50"),
            Some(b"\x30"),
            None,
        )
        .unwrap();

    // Serialize/deserialize to convert large values to Hash.
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    let deserialized = crate::api::FrozenChangeProof::from_slice(&serialized).unwrap();

    let ctx =
        verify_change_proof_structure(&deserialized, root2, Some(b"\x10\x50"), Some(b"\x30"), None)
            .unwrap();
    verify_and_check(&target, &deserialized, &ctx, root1_target).unwrap();
}

/// The proven range narrows to the last operation's key whether that operation
/// is a `Put` or a `Delete`, on a flat trie where neither key is a prefix of
/// another.
///
/// A `Put`'s key has a value in `end_root`, so a truncated proof's end proof —
/// anchored at that key — carries the value and the truncation test finds it: a
/// stop-short is recognised directly. A `Delete`'s key is absent from `end_root`,
/// so no value confirms the anchor and a proof of absence cannot identify the key
/// it was built for; the range narrows because a stop-short there cannot be ruled
/// out. Either way verification asserts the range the reply provably covers,
/// which is what makes a trailing `Delete` the only shape where a stop-short can
/// go unrecognised.
#[test_case(true, NonZeroUsize::new(1), b"\x10" ; "put, one op")]
#[test_case(true, NonZeroUsize::new(2), b"\x20" ; "put, two ops")]
#[test_case(true, NonZeroUsize::new(3), b"\x30" ; "put, three ops")]
#[test_case(false, NonZeroUsize::new(1), b"\x10" ; "delete, one op")]
#[test_case(false, NonZeroUsize::new(2), b"\x20" ; "delete, two ops")]
#[test_case(false, NonZeroUsize::new(3), b"\x30" ; "delete, three ops")]
fn test_truncation_narrows_to_the_last_op(
    trailing_put: bool,
    limit: Option<NonZeroUsize>,
    expected_edge: &[u8],
) {
    let (db, _dir) = setup_db![
        (b"\x10", b"v0"),
        (b"\x20", b"v1"),
        (b"\x30", b"v2"),
        (b"\x40", b"v3"),
        (b"\x50", b"v4")
    ];
    let root1 = db.root_hash().unwrap();

    // Change the first four keys: overwrite them (Put) or remove them (Delete).
    let keys: [&[u8]; 4] = [b"\x10", b"\x20", b"\x30", b"\x40"];
    let ops: Vec<BatchOp<&[u8], &[u8]>> = keys
        .into_iter()
        .map(|key| {
            if trailing_put {
                BatchOp::Put {
                    key,
                    value: &b"c"[..],
                }
            } else {
                BatchOp::Delete { key }
            }
        })
        .collect();
    db.propose(ops).unwrap().commit().unwrap();
    let root2 = db.root_hash().unwrap();

    let proof = db
        .change_proof(root1, root2.clone(), None, Some(b"\x50"), limit)
        .unwrap();
    let ctx = verify_change_proof_structure(&proof, root2, None, Some(b"\x50"), limit).unwrap();

    let last_is_put = matches!(proof.batch_ops().last(), Some(BatchOp::Put { .. }));
    assert_eq!(
        last_is_put, trailing_put,
        "fixture must end in the expected op kind"
    );
    assert_eq!(
        ctx.right_edge_key.as_deref(),
        Some(expected_edge),
        "the range must narrow to the last operation's key"
    );
}

/// The prefix geometry, where the truncation test is easiest to fool: the last
/// operation's key is a proper prefix of a later key, so the path toward that
/// later key runs through the last operation's own node.
///
/// Both narrow to the last operation's key. The `Put` is recognised outright,
/// since its node carries a value. The `Delete` narrows because the surviving
/// branch on that path holds no value, so its absence is validly stated and
/// stopping short cannot be ruled out.
#[test_case(true, Some(&b"\x10"[..]) ; "put is recognised")]
#[test_case(false, Some(&b"\x10"[..]) ; "delete narrows to its own key")]
fn test_truncation_classification_under_a_prefix_key(
    trailing_put: bool,
    expected_edge: Option<&[u8]>,
) {
    let (db, _dir) = setup_db![(b"\x10", b"v0"), (b"\x10\x00", b"v1"), (b"\x20", b"v2")];
    let root1 = db.root_hash().unwrap();

    // Two changes so a limit of one truncates, with \x10 first either way.
    let ops: Vec<BatchOp<&[u8], &[u8]>> = if trailing_put {
        vec![
            BatchOp::Put {
                key: &b"\x10"[..],
                value: &b"c0"[..],
            },
            BatchOp::Put {
                key: &b"\x20"[..],
                value: &b"c2"[..],
            },
        ]
    } else {
        vec![
            BatchOp::Delete { key: &b"\x10"[..] },
            BatchOp::Delete { key: &b"\x20"[..] },
        ]
    };
    db.propose(ops).unwrap().commit().unwrap();
    let root2 = db.root_hash().unwrap();

    let limit = NonZeroUsize::new(1);
    let proof = db
        .change_proof(root1, root2.clone(), None, Some(b"\x20"), limit)
        .unwrap();
    let ctx = verify_change_proof_structure(&proof, root2, None, Some(b"\x20"), limit).unwrap();

    assert_eq!(ctx.right_edge_key.as_deref(), expected_edge);
}

/// A proof covering its whole requested range keeps its full proven range when the
/// last operation's key is a proper prefix of the requested bound.
///
/// The end proof here is anchored at the bound, and its nodes include the last
/// operation's own node carrying a value — so a lookup answering from any node on
/// the path would read this as having stopped short. It does not, because a proof
/// answers only about the key it was built for and refuses the query otherwise.
///
/// Compare [`test_a_terminal_at_the_last_operation_narrows_the_range`], where the
/// last operation's node is the proof's terminal, the lookup answers, and the
/// range narrows.
#[test]
fn test_prefix_bound_does_not_narrow_a_complete_proof() {
    let (source, _ds) = setup_db![(b"\x10", b"v0"), (b"\x10\x20", b"v1")];
    let (target, _dt) = setup_db![(b"\x10", b"v0"), (b"\x10\x20", b"v1")];
    let root1_target = target.root_hash().unwrap();

    // One change, no limit: nothing is left out.
    let (root1, root2) = setup_2nd_commit!(source, [(b"\x10", b"c0")]);
    assert_eq!(root1, root1_target);

    let proof = source
        .change_proof(root1, root2.clone(), None, Some(b"\x10\x20"), None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1, "the single change is included");
    assert_eq!(
        proof.end_proof().len(),
        2,
        "the path to the bound runs through the last operation's own node"
    );
    assert!(
        proof
            .end_proof()
            .iter()
            .all(|node| node.value_digest.is_some()),
        "both nodes carry a value, which is what makes this a false-positive risk"
    );
    assert!(
        proof.end_proof().value_digest(b"\x10", &root2).is_err(),
        "a proof anchored at the bound must refuse to answer about a shorter key"
    );

    let ctx = verify_change_proof_structure(&proof, root2, None, Some(b"\x10\x20"), None).unwrap();
    assert_eq!(
        ctx.right_edge_key.as_deref(),
        ctx.end_key.as_deref(),
        "a complete proof keeps the range it was asked for"
    );

    verify_and_check(&target, &proof, &ctx, root1_target).unwrap();
}

/// A proof covering its whole requested range still narrows when the last
/// operation's node is the proof's terminal and carries a value. A single-key
/// trie is the clearest case: its root is that key's leaf, so the end proof for
/// any bound terminates there and the lookup answers with the value.
///
/// Narrowing here is truthful and costs nothing: the one change sits at the
/// narrowed edge, so it is checked, and a caller resuming above it finds the rest
/// of the requested range empty on its next request. Compare
/// [`test_prefix_bound_does_not_narrow_a_complete_proof`], where the last
/// operation's node is interior and the lookup refuses.
#[test]
fn test_a_terminal_at_the_last_operation_narrows_the_range() {
    let (source, _ds) = setup_db![(b"\x10", b"v0")];
    let (target, _dt) = setup_db![(b"\x10", b"v0")];
    let root1_target = target.root_hash().unwrap();

    // One change, no cap, bound well above the only key: nothing stops short.
    let (root1, root2) = setup_2nd_commit!(source, [(b"\x10", b"c0")]);
    assert_eq!(root1, root1_target);

    let proof = source
        .change_proof(root1.clone(), root2.clone(), None, Some(b"\xff"), None)
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1, "the single change is included");
    assert!(
        proof
            .end_proof()
            .value_digest(b"\x10", &root2)
            .unwrap()
            .is_some(),
        "the terminal is the last op's own node and carries its value"
    );

    let ctx = verify_change_proof_structure(&proof, root2, None, Some(b"\xff"), None).unwrap();
    assert_eq!(
        ctx.right_edge_key.as_deref(),
        Some(&b"\x10"[..]),
        "the terminal-valued node narrows the range to the last op's key"
    );
    assert_ne!(
        ctx.right_edge_key.as_deref(),
        ctx.end_key.as_deref(),
        "the narrowed edge sits below the requested bound"
    );
    verify_and_check(&target, &proof, &ctx, root1).unwrap();
}

/// An operation as it arrives inside a change proof.
type FrozenBatchOp = BatchOp<Box<[u8]>, Box<[u8]>>;

/// A proof that stops short on a `Delete`, with the databases and roots it was
/// built from.
struct StoppedShort {
    source: Db,
    target: Db,
    start_root: api::HashKey,
    end_root: api::HashKey,
    proof: FrozenChangeProof,
    _dirs: (TempDir, TempDir),
}

/// Build the fixture where a proof stops short on a `Delete`: four keys, three
/// changes, and a limit of two so the reply ends on the removal of `\x20` while
/// the requested bound is still `\xff`.
fn stopped_short_on_a_delete() -> StoppedShort {
    let (source, ds) = setup_db![
        (b"\x10", b"v0"),
        (b"\x20", b"v1"),
        (b"\x30", b"v2"),
        (b"\x40", b"v3")
    ];
    let (target, dt) = setup_db![
        (b"\x10", b"v0"),
        (b"\x20", b"v1"),
        (b"\x30", b"v2"),
        (b"\x40", b"v3")
    ];
    let root1 = source.root_hash().unwrap();
    let ops: Vec<BatchOp<&[u8], &[u8]>> = vec![
        BatchOp::Put {
            key: &b"\x10"[..],
            value: &b"c0"[..],
        },
        BatchOp::Delete { key: &b"\x20"[..] },
        BatchOp::Delete { key: &b"\x30"[..] },
    ];
    source.propose(ops).unwrap().commit().unwrap();
    let root2 = source.root_hash().unwrap();

    let proof = source
        .change_proof(
            root1.clone(),
            root2.clone(),
            None,
            Some(b"\xff"),
            NonZeroUsize::new(2),
        )
        .unwrap();
    assert!(
        matches!(proof.batch_ops().last(), Some(BatchOp::Delete { .. })),
        "the reply must end on a removal"
    );
    StoppedShort {
        source,
        target,
        start_root: root1,
        end_root: root2,
        proof,
        _dirs: (ds, dt),
    }
}

/// A proof that stops short on a `Delete` while `end_root` still holds a key
/// above the stopping point, with the databases and roots it was built from.
///
/// The sibling of [`stopped_short_on_a_delete`]. There the keyspace above the
/// stopping point is empty, so the end proof also validates against the
/// requested bound. Here the walk toward the bound needs the surviving key's
/// node, which the proof does not carry — so the two shapes reach the narrowing
/// decision from different directions.
fn stopped_short_below_a_surviving_key() -> StoppedShort {
    let (source, ds) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\xf0", b"v2")];
    let (target, dt) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\xf0", b"v2")];
    let root1 = source.root_hash().unwrap();
    let ops: Vec<BatchOp<&[u8], &[u8]>> = vec![
        BatchOp::Put {
            key: &b"\x10"[..],
            value: &b"A"[..],
        },
        BatchOp::Delete { key: &b"\x20"[..] },
        BatchOp::Put {
            key: &b"\xf0"[..],
            value: &b"C"[..],
        },
    ];
    source.propose(ops).unwrap().commit().unwrap();
    let root2 = source.root_hash().unwrap();

    let proof = source
        .change_proof(
            root1.clone(),
            root2.clone(),
            None,
            Some(b"\xff"),
            NonZeroUsize::new(2),
        )
        .unwrap();
    assert!(
        matches!(proof.batch_ops().last(), Some(BatchOp::Delete { .. })),
        "the reply must end on a removal"
    );
    StoppedShort {
        source,
        target,
        start_root: root1,
        end_root: root2,
        proof,
        _dirs: (ds, dt),
    }
}

/// A proof that stops short on a removal is accepted over the range it provably
/// covers, rather than judged against the wider range that was requested.
///
/// Verification narrows the proven range to the trailing removal's key and checks
/// the reply on that basis. Judging it against the requested bound would demand
/// operations the reply never claimed to carry, and reject an honest reply.
#[test]
fn test_stopping_short_on_a_delete_is_accepted_over_the_narrowed_range() {
    let f = stopped_short_on_a_delete();

    let ctx = verify_change_proof_structure(
        &f.proof,
        f.end_root.clone(),
        None,
        Some(b"\xff"),
        NonZeroUsize::new(2),
    )
    .unwrap();
    assert_eq!(
        ctx.right_edge_key.as_deref(),
        Some(&b"\x20"[..]),
        "the proven range narrows to the trailing removal's key"
    );
    assert_ne!(
        ctx.right_edge_key.as_deref(),
        ctx.end_key.as_deref(),
        "the narrowed edge sits below the requested bound"
    );
    verify_and_check(&f.target, &f.proof, &ctx, f.start_root).unwrap();
}

/// The sibling shape, where `end_root` still holds a key above the stopping
/// point. Validating the end proof against the requested bound would need the
/// surviving key's node, which the proof does not carry, so this shape reaches
/// the narrowing decision through structural validation rather than the root
/// hash. Narrowing happens in the same place, so the reply is accepted here too.
#[test]
fn test_stopping_short_below_a_surviving_key_is_accepted() {
    let f = stopped_short_below_a_surviving_key();

    let ctx = verify_change_proof_structure(
        &f.proof,
        f.end_root.clone(),
        None,
        Some(b"\xff"),
        NonZeroUsize::new(2),
    )
    .unwrap();
    assert_eq!(
        ctx.right_edge_key.as_deref(),
        Some(&b"\x20"[..]),
        "the proven range narrows to the trailing removal's key"
    );
    verify_and_check(&f.target, &f.proof, &ctx, f.start_root).unwrap();
}

/// Narrowing requires the last operation to be a removal. A mismatch whose last
/// operation is a `Put` still rejects: stopping short on a `Put` is recognised
/// outright, so a `Put` the end proof reports absent is inconsistent rather than
/// truncated, and the requested range stands for the root hash to judge.
#[test]
fn test_mismatch_ending_in_a_put_is_still_rejected() {
    let (source, _ds) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let (target, _dt) = setup_db![(b"\x10", b"v0"), (b"\x20", b"v1"), (b"\x30", b"v2")];
    let root1_target = target.root_hash().unwrap();
    let (root1, root2) = setup_2nd_commit!(source, [(b"\x10", b"c0"), (b"\x20", b"c1")]);

    let honest = source
        .change_proof(root1, root2.clone(), None, Some(b"\xff"), None)
        .unwrap();
    assert!(matches!(
        honest.batch_ops().last(),
        Some(BatchOp::Put { .. })
    ));

    // Forge the last operation's value, leaving every key and both proofs intact.
    let mut ops: Vec<FrozenBatchOp> = honest.batch_ops().to_vec();
    let Some(BatchOp::Put { value, .. }) = ops.last_mut() else {
        panic!("the last operation must be a Put");
    };
    *value = Box::from(&b"forged"[..]);
    let tampered = crate::ChangeProof::new(
        crate::Proof::new(honest.start_proof().to_vec().into_boxed_slice()),
        crate::Proof::new(honest.end_proof().to_vec().into_boxed_slice()),
        ops.into_boxed_slice(),
    );

    let ctx = verify_change_proof_structure(&tampered, root2, None, Some(b"\xff"), None).unwrap();
    let err = verify_and_check(&target, &tampered, &ctx, root1_target).unwrap_err();
    assert!(
        matches!(
            err,
            api::Error::ProofError(crate::ProofError::EndRootMismatch)
        ),
        "expected a plain mismatch, got {err:?}"
    );
}

/// Narrowing also requires the end proof to be a valid statement that the
/// removal's key is absent. Splicing in an end proof about an unrelated key
/// leaves a proof that cannot be anchored at the last operation, so the range
/// stays wide and the failure stands.
#[test]
fn test_an_unrelated_end_proof_does_not_narrow() {
    let f = stopped_short_below_a_surviving_key();

    // An end proof anchored at \x10: a complete proof over [None, \x10].
    let donor = f
        .source
        .change_proof(
            f.start_root.clone(),
            f.end_root.clone(),
            None,
            Some(b"\x10"),
            None,
        )
        .unwrap();
    let spliced = crate::ChangeProof::new(
        crate::Proof::new(f.proof.start_proof().to_vec().into_boxed_slice()),
        crate::Proof::new(donor.end_proof().to_vec().into_boxed_slice()),
        f.proof.batch_ops().to_vec().into_boxed_slice(),
    );

    match verify_change_proof_structure(
        &spliced,
        f.end_root.clone(),
        None,
        Some(b"\xff"),
        NonZeroUsize::new(2),
    ) {
        // Structural validation rejects this shape outright, before a right edge
        // is ever handed downstream.
        Err(err) => assert!(
            matches!(
                err,
                api::Error::ProofError(crate::ProofError::ShouldBePrefixOfNextKey)
            ),
            "expected the spliced end proof to fail its own walk, got {err:?}"
        ),
        // If a future change lets it through phase 1, the range must stay wide
        // and the proof must still be rejected.
        Ok(ctx) => {
            assert_eq!(
                ctx.right_edge_key.as_deref(),
                Some(&b"\xff"[..]),
                "an end proof that cannot be anchored at the last operation keeps the wide range"
            );
            verify_and_check(&f.target, &spliced, &ctx, f.start_root.clone())
                .expect_err("a spliced end proof must not be accepted");
        }
    }
}
