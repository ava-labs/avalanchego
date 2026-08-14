// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Deterministic regression tests for change-proof verification at range
//! boundaries, where a boundary key shares a trie-descent path with a
//! neighbouring key.
//!
//! Soundness — a tampered, forged, or omitted op must be rejected:
//! - `test_tampered_right_edge_delete_to_put_is_rejected` (#2091): in a
//!   no-bounds proof, whose end proof anchors on the highest changed key
//!   (`merkle/mod.rs` `change_proof`: `end_key.or(batch_ops.last())`), a deleted
//!   key sharing the anchor's branch but sorting below it is flipped from
//!   Delete to Put.
//! - `test_forged_in_range_delete_to_put_is_rejected` (#2138): an in-range op
//!   whose key is a prefix of the end bound is flipped from Delete to Put.
//! - `test_unbounded_end_omitted_in_range_delete_is_rejected`: an omitted
//!   in-range delete under the start boundary's on-path child, with an unbounded
//!   right edge (`end_key == None`). The +∞ end bound keeps the key in range, so
//!   the child is recomputed rather than taken from the start proof.
//! - `test_split_boundary_child_omitted_in_range_delete_is_rejected`: the
//!   boundary child is genuinely split — it holds both an in-range and an
//!   out-of-range key — and an omitted in-range delete must still be rejected.
//! - `test_split_start_boundary_child_omitted_in_range_delete_is_rejected`: the
//!   left-edge mirror of the split case. The two edges share one code path with
//!   inverted comparisons, so each needs its own split coverage.
//!
//! Completeness — an honest out-of-range deletion just past a boundary must
//! still verify:
//! - `test_out_of_range_delete_past_end_bound_verifies` (#2136): end bound,
//!   where the deleted key extends the bound.
//! - `test_out_of_range_delete_below_start_bound_verifies`: start bound, where
//!   the bound extends the deleted key.

use super::*;
use crate::{ChangeProof, Proof};

/// `batch_ops` as they come off a generated proof.
type OwnedOps = Box<[BatchOp<Box<[u8]>, Box<[u8]>>]>;

/// Verifies `proof` against the given inclusive bounds, returning the rejection
/// reason on failure.
fn verify(
    db: &Db,
    proof: &FrozenChangeProof,
    start_root: &api::HashKey,
    end_root: &api::HashKey,
    start_key: Option<&[u8]>,
    end_key: Option<&[u8]>,
) -> Result<(), api::Error> {
    let ctx = verify_change_proof_structure(proof, end_root.clone(), start_key, end_key, None)?;
    verify_and_check(db, proof, &ctx, start_root.clone())
}

/// Commit `end_batch` onto `db`, returning the roots before and after. Unlike
/// `setup_2nd_commit!` this takes arbitrary ops, so it can apply deletes.
fn commit_batch(db: &Db, end_batch: Vec<BatchOp<&[u8], &[u8]>>) -> (api::HashKey, api::HashKey) {
    let start_root = db.root_hash().unwrap();
    db.propose(end_batch).unwrap().commit().unwrap();
    (start_root, db.root_hash().unwrap())
}

/// Rebuild `proof` with different `batch_ops`, keeping both boundary proofs.
fn replace_ops(proof: &FrozenChangeProof, batch_ops: OwnedOps) -> FrozenChangeProof {
    ChangeProof::new(
        Proof::new(proof.start_proof().as_ref().into()),
        Proof::new(proof.end_proof().as_ref().into()),
        batch_ops,
    )
}

/// Rebuild `proof` with the `Delete` at `victim` flipped to a `Put`.
fn forge_delete_to_put(proof: &FrozenChangeProof, victim: &[u8]) -> FrozenChangeProof {
    let ops: Vec<_> = proof
        .batch_ops()
        .iter()
        .map(|op| match op {
            BatchOp::Delete { key } if key.as_ref() == victim => BatchOp::Put {
                key: key.clone(),
                value: Box::from(&b"forged"[..]),
            },
            other => other.clone(),
        })
        .collect();
    replace_ops(proof, ops.into_boxed_slice())
}

/// Rebuild `proof` claiming nothing changed, keeping both boundary proofs.
fn omit_all_ops(proof: &FrozenChangeProof) -> FrozenChangeProof {
    replace_ops(proof, Vec::new().into_boxed_slice())
}

#[test]
fn test_tampered_right_edge_delete_to_put_is_rejected() {
    // `0xf0` and `0xfa` keep the `f` branch (nibble path `[f]`) real in the END
    // trie. `0xf51c` (victim) and `0xf5cd` (the max changed key) both live under
    // that branch's child `5`, and both are deleted — so in the end trie child
    // `5` of the `f` branch is absent, and `prove(0xf5cd)` is an exclusion proof
    // terminating at the `f` branch that marks child `5` out-of-range. The
    // victim `0xf51c` sorts below the anchor `0xf5cd` but in that same on-path
    // child.
    let (db, _dir) = setup_db![
        (b"\x10".as_slice(), b"low".as_slice()),
        (b"\xf0".as_slice(), b"fz".as_slice()),
        (b"\xfa".as_slice(), b"fa".as_slice()),
        (b"\xf5\x1c".as_slice(), b"victim".as_slice()), // <- 0xf51c (victim)
        (b"\xf5\xcd".as_slice(), b"anchor".as_slice())  // <- 0xf5cd (anchor)
    ];
    let (start_root, end_root) = commit_batch(
        &db,
        vec![
            BatchOp::Delete { key: b"\xf5\x1c" },
            BatchOp::Delete { key: b"\xf5\xcd" },
        ],
    );

    // No-bounds change proof; its end proof anchors on the max op key 0xf5cd.
    let proof = db
        .change_proof(start_root.clone(), end_root.clone(), None, None, None)
        .unwrap();
    verify(&db, &proof, &start_root, &end_root, None, None).expect("honest proof must verify");

    // Tamper: Delete{0xf51c} -> Put{0xf51c, "forged"}.
    let mutated = forge_delete_to_put(&proof, b"\xf5\x1c");
    assert!(
        matches!(
            verify(&db, &mutated, &start_root, &end_root, None, None),
            Err(api::Error::ProofError(crate::ProofError::EndRootMismatch))
        ),
        "an in-range key left in the proposal must force its boundary child to be \
         recomputed, not taken from the proof. 0xf51c sorts below the deleted \
         anchor 0xf5cd, so it is in range even though it sits under the right \
         edge's 0xf5 branch"
    );
}

/// An honest change proof must verify when an out-of-range key just past the
/// end bound was deleted. `0xfb00` sorts after the bound `0xfb` (a longer key
/// extending a shorter one comes later), so its deletion is out of range and
/// correctly not in `batch_ops`. The verifier's rebuilt trie retains `0xfb00`
/// under the `f` branch's `b` child, so that child must be taken from the proof
/// rather than recomputed — recomputing it would surface the retained key and
/// mismatch `end_root`, rejecting the proof with `EndRootMismatch`.
#[test]
fn test_out_of_range_delete_past_end_bound_verifies() {
    let (db, _dir) = setup_db![(b"\xfb\x00".as_slice(), b"\x00".as_slice())];
    let (start_root, end_root) = commit_batch(
        &db,
        vec![
            BatchOp::Delete { key: b"\xfb\x00" },
            BatchOp::Put {
                key: b"\xf7",
                value: b"\x00",
            },
            BatchOp::Put {
                key: b"\xf1",
                value: b"\x00",
            },
        ],
    );

    let (sk, ek) = (b"\x00".as_slice(), b"\xfb".as_slice());
    let proof = db
        .change_proof(
            start_root.clone(),
            end_root.clone(),
            Some(sk),
            Some(ek),
            None,
        )
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 2);
    verify(&db, &proof, &start_root, &end_root, Some(sk), Some(ek)).expect(
        "honest change proof over [0x00, 0xfb] must verify. The deletion of \
         the out-of-range 0xfb00 (past the end bound 0xfb, which is its \
         prefix) must not cause an EndRootMismatch",
    );
}

/// A forged in-range Delete-to-Put must be rejected. `0x56` is in range
/// (`0x56 < 0x5600`) and a prefix of the non-existent end bound `0x5600`,
/// while `0x5601` is out of range (`> 0x5600`) sharing the `0x56` path. `0x56`
/// sits at the boundary terminal as a branch value, so being in range is what
/// keeps that value from being cleared: it is hashed from the proposal, which
/// surfaces the forged value. Clearing it would let the proof supply the digest
/// instead and the forgery would go unchecked.
#[test]
fn test_forged_in_range_delete_to_put_is_rejected() {
    let (db, _dir) = setup_db![
        (b"\x56".as_slice(), b"\x01".as_slice()),
        (b"\x56\x01".as_slice(), b"\x01".as_slice())
    ];
    let (start_root, end_root) = commit_batch(&db, vec![BatchOp::Delete { key: b"\x56" }]);

    let (sk, ek) = (b"\x00".as_slice(), b"\x56\x00".as_slice());
    let proof = db
        .change_proof(
            start_root.clone(),
            end_root.clone(),
            Some(sk),
            Some(ek),
            None,
        )
        .unwrap();
    verify(&db, &proof, &start_root, &end_root, Some(sk), Some(ek))
        .expect("honest proof must verify");

    let forged = forge_delete_to_put(&proof, b"\x56");
    assert!(
        matches!(
            verify(&db, &forged, &start_root, &end_root, Some(sk), Some(ek)),
            Err(api::Error::ProofError(crate::ProofError::EndRootMismatch))
        ),
        "an in-range branch value must be kept and hashed from the proposal, not \
         cleared and supplied by the proof. 0x56 is in range (0x56 < 0x5600) even \
         though it prefixes the end bound"
    );
}

/// An honest change proof must verify when an out-of-range key just below the
/// start bound was deleted, where the start bound extends that deleted key
/// (`0xd44f` extends `0xd4`). This is the left-edge mirror of the end-bound
/// case: there the deleted key extends the bound, here the bound extends the
/// deleted key. Both edges resolve the on-path child the same way.
///
/// start trie:  `{ 0xd4: 0x00, 0xdb: 0x00 }`
/// end trie:    `{ 0xd5: 0x00, 0xdb: 0x00 }`  (`0xd4` deleted, `0xd5` added)
/// proof range: `[0xd44f, 0xf9]`
///
/// `0xd4 < sk = 0xd44f`, so deleting it is out of range and correctly not in
/// `batch_ops`. The verifier's rebuilt trie retains `0xd4` under the `d`
/// branch, so that child must be taken from the start proof rather than
/// recomputed — recomputing it would surface the retained key and mismatch
/// `end_root`, rejecting the proof with `EndRootMismatch`. `0xd5` and `0xdb` are
/// in range (`0xdb` keeps the `d` branch non-trivial). Minimized from
/// change-proof fuzz seed 8534711138888643184 (`start_nonexistent` scenario).
#[test]
fn test_out_of_range_delete_below_start_bound_verifies() {
    let (db, _dir) = setup_db![
        (b"\xd4".as_slice(), b"\x00".as_slice()),
        (b"\xdb".as_slice(), b"\x00".as_slice())
    ];
    let (start_root, end_root) = commit_batch(
        &db,
        vec![
            BatchOp::Delete { key: b"\xd4" },
            BatchOp::Put {
                key: b"\xd5",
                value: b"\x00",
            },
        ],
    );

    let (sk, ek) = (b"\xd4\x4f".as_slice(), b"\xf9".as_slice());
    let proof = db
        .change_proof(
            start_root.clone(),
            end_root.clone(),
            Some(sk),
            Some(ek),
            None,
        )
        .unwrap();
    verify(&db, &proof, &start_root, &end_root, Some(sk), Some(ek)).expect(
        "honest change proof over [0xd44f, 0xf9] must verify. The deletion of \
         the out-of-range 0xd4 (below sk, and a prefix of it) must not cause \
         an EndRootMismatch",
    );
}

/// A change proof with an unbounded right edge (`end_key == None`) must not hide
/// an omitted in-range deletion. When `right_edge_key` resolves to `None`, the
/// end bound is +∞, not the empty (minimum) key. Treating it as the minimum
/// judges every key out of range, so the start boundary's on-path child is
/// wrongly marked outside and taken from the proof instead of recomputed from
/// the proposal — hiding the omission.
///
/// start trie `{ 0x10, 0x53, 0x60 }`, end trie deletes in-range `0x53`, range
/// `[0x52, +∞)`. The forge drops both the `Delete` and the end proof, so an
/// empty batch with `end_key == None` makes `right_edge_key` `None`.
#[test]
fn test_unbounded_end_omitted_in_range_delete_is_rejected() {
    let (db, _dir) = setup_db![
        (b"\x10".as_slice(), b"low".as_slice()),
        (b"\x53".as_slice(), b"victim".as_slice()),
        (b"\x60".as_slice(), b"hi".as_slice())
    ];
    let (start_root, end_root) = commit_batch(&db, vec![BatchOp::Delete { key: b"\x53" }]);

    let sk = b"\x52".as_slice();
    let proof = db
        .change_proof(start_root.clone(), end_root.clone(), Some(sk), None, None)
        .unwrap();
    verify(&db, &proof, &start_root, &end_root, Some(sk), None).expect("honest proof must verify");

    // Forge: drop the in-range Delete and the end proof, so `right_edge_key`
    // resolves to None (unbounded end). Dropping the end proof is essential —
    // reusing the honest one leaves `right_edge_key` non-empty.
    let forged = ChangeProof::new(
        Proof::new(proof.start_proof().as_ref().into()),
        Proof::new(Vec::new().into_boxed_slice()),
        Vec::new().into_boxed_slice(),
    );
    assert!(
        matches!(
            verify(&db, &forged, &start_root, &end_root, Some(sk), None),
            Err(api::Error::ProofError(crate::ProofError::EndRootMismatch))
        ),
        "an in-range key left in the proposal must force its boundary child to be \
         recomputed, not taken from the proof. With no end bound the range is \
         [0x52, +∞), so 0x53 is in range"
    );
}

/// A boundary child that is genuinely split — holding both an in-range and an
/// out-of-range key at once — must still reject an omitted in-range delete. The
/// end bound `0xfb50` descends into child `b` of the terminal `[f]` branch. The
/// old trie holds two keys under that child: `0xfb10` (in range, `< 0xfb50`)
/// and `0xfb90` (out of range, `> 0xfb50`). The end trie deletes both, so child
/// `b` is absent there and the right-edge terminal is the `[f]` branch. Only
/// `Delete 0xfb10` is in range, so it is the only op in the change list.
///
/// An attacker replays the honest boundary proofs with an empty change list.
/// The proposal (old trie + no ops) still holds `0xfb10`, so its child `b` is
/// split: `{0xfb10 in, 0xfb90 out}`. Because the child holds an in-range key it
/// is recomputed from the proposal — surfacing the un-deleted `0xfb10` — and
/// the root-hash check fails. The out-of-range `0xfb90` sharing the child must
/// not let it be taken wholesale from the proof.
#[test]
fn test_split_boundary_child_omitted_in_range_delete_is_rejected() {
    let (db, _dir) = setup_db![
        (b"\x10".as_slice(), b"a".as_slice()),
        (b"\xf1".as_slice(), b"a".as_slice()),
        (b"\xf7".as_slice(), b"a".as_slice()),
        (b"\xfb\x10".as_slice(), b"in".as_slice()),
        (b"\xfb\x90".as_slice(), b"out".as_slice())
    ];
    let (start_root, end_root) = commit_batch(
        &db,
        vec![
            BatchOp::Delete { key: b"\xfb\x10" },
            BatchOp::Delete { key: b"\xfb\x90" },
        ],
    );

    let (sk, ek) = (b"\x00".as_slice(), b"\xfb\x50".as_slice());
    let proof = db
        .change_proof(
            start_root.clone(),
            end_root.clone(),
            Some(sk),
            Some(ek),
            None,
        )
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    verify(&db, &proof, &start_root, &end_root, Some(sk), Some(ek))
        .expect("honest proof must verify");

    // Forge: drop the in-range Delete{0xfb10}, keeping the honest boundary
    // proofs. The proposal keeps 0xfb10, so boundary child b is split.
    let forged = omit_all_ops(&proof);
    assert!(
        matches!(
            verify(&db, &forged, &start_root, &end_root, Some(sk), Some(ek)),
            Err(api::Error::ProofError(crate::ProofError::EndRootMismatch))
        ),
        "a split boundary child must be recomputed from the proposal. The end \
         boundary's child holds the in-range 0xfb10 as well as the out-of-range \
         0xfb90, so it cannot be taken from the proof"
    );
}

/// Left-edge mirror of `test_split_boundary_child_omitted_in_range_delete_is_rejected`.
/// The start bound `0xd450` descends into child `4` of the `[d]` branch, and that
/// child is split: `0xd410` sorts below the bound (out of range) while `0xd490`
/// sorts above it (in range). The end trie deletes both, so child `4` is absent
/// there and the left-edge terminal is the `[d]` branch. Only `Delete 0xd490` is
/// in range, so it is the only op in the change list.
///
/// An attacker replays the honest boundary proofs with an empty change list. The
/// proposal keeps `0xd490`, so child `4` still holds an in-range key and must be
/// recomputed from the proposal rather than taken from the start proof. The
/// out-of-range `0xd410` sharing the child must not let it be taken wholesale.
///
/// `0xd1` and `0xdb` keep the `[d]` branch non-trivial in the end trie, and
/// `0x10` keeps the root a branch.
#[test]
fn test_split_start_boundary_child_omitted_in_range_delete_is_rejected() {
    let (db, _dir) = setup_db![
        (b"\x10".as_slice(), b"a".as_slice()),
        (b"\xd1".as_slice(), b"a".as_slice()),
        (b"\xd4\x10".as_slice(), b"out".as_slice()),
        (b"\xd4\x90".as_slice(), b"in".as_slice()),
        (b"\xdb".as_slice(), b"a".as_slice())
    ];
    let (start_root, end_root) = commit_batch(
        &db,
        vec![
            BatchOp::Delete { key: b"\xd4\x10" },
            BatchOp::Delete { key: b"\xd4\x90" },
        ],
    );

    let (sk, ek) = (b"\xd4\x50".as_slice(), b"\xf0".as_slice());
    let proof = db
        .change_proof(
            start_root.clone(),
            end_root.clone(),
            Some(sk),
            Some(ek),
            None,
        )
        .unwrap();
    assert_eq!(proof.batch_ops().len(), 1);
    verify(&db, &proof, &start_root, &end_root, Some(sk), Some(ek))
        .expect("honest proof must verify");

    // Forge: drop the in-range Delete{0xd490}, keeping the honest boundary
    // proofs. The proposal keeps 0xd490, so boundary child 4 is split.
    let forged = omit_all_ops(&proof);
    assert!(
        matches!(
            verify(&db, &forged, &start_root, &end_root, Some(sk), Some(ek)),
            Err(api::Error::ProofError(crate::ProofError::EndRootMismatch))
        ),
        "a split boundary child must be recomputed from the proposal. The start \
         boundary's child holds the in-range 0xd490 as well as the out-of-range \
         0xd410, so it cannot be taken from the proof"
    );
}
