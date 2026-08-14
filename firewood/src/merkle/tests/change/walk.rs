// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Multi-round client sync walks over change proofs.
//!
//! A client fetches one range at a time, verifies each proof against its own
//! database, commits it, and follows the continuation reported by
//! [`find_next_key_after_change_proof`] until that call reports no more keys.
//! These tests drive the whole loop and check where it ends up.
//!
//! Each round verifies against the client's current tip, which is what
//! [`Db::verify_change_proof`] does, so the client's state advances as the walk
//! proceeds.
//!
//! Every walk is capped. A continuation that fails to advance then fails the
//! test as "did not converge" instead of hanging.

use super::*;
use crate::find_next_key_after_change_proof;
use std::num::NonZeroUsize;
use tempfile::TempDir;
use test_case::test_case;

/// An op as it arrives in a change proof.
type WalkOp = BatchOp<Box<[u8]>, Box<[u8]>>;

/// Outcome of a capped sync walk.
struct Walk {
    /// Number of requests issued.
    rounds: usize,
    /// Every op the walk collected, in the order received.
    ops: Vec<WalkOp>,
    /// The range each round requested, as `(start, end)` hex.
    trace: Vec<(String, String)>,
}

/// Render an optional key bound for a trace line.
fn bound(key: Option<&[u8]>) -> String {
    key.map_or_else(|| "-".to_owned(), hex::encode)
}

/// Render a trace as a single line of ranges.
fn ranges(trace: &[(String, String)]) -> String {
    trace
        .iter()
        .map(|(s, e)| format!("[{s},{e}]"))
        .collect::<Vec<_>>()
        .join(" ")
}

/// Drive a client sync walk from `start_root` to `end_root`, requesting keys up
/// to `end_key` and letting the generator truncate at `limit`.
///
/// Each round asks `source` for a change proof over the range the previous round
/// reported, has `target` verify and commit it, then reads the next range from
/// `find_next_key_after_change_proof`. Requests name `start_root` because that is
/// the revision `source` proves from; `target` applies each proof to its own tip.
///
/// Returns `Err` if the walk exceeds `max_rounds`, which is how a non-advancing
/// continuation surfaces.
fn sync_walk(
    source: &Db,
    target: &Db,
    start_root: &api::HashKey,
    end_root: &api::HashKey,
    end_key: Option<&[u8]>,
    limit: Option<NonZeroUsize>,
    max_rounds: usize,
) -> Result<Walk, String> {
    let mut start_key: Option<Box<[u8]>> = None;
    let mut end_key: Option<Box<[u8]>> = end_key.map(Box::from);
    let mut collected: Vec<WalkOp> = Vec::new();
    let mut trace: Vec<(String, String)> = Vec::new();
    let mut rounds: usize = 0;

    loop {
        rounds = rounds.saturating_add(1);
        trace.push((bound(start_key.as_deref()), bound(end_key.as_deref())));
        if rounds > max_rounds {
            return Err(format!(
                "did not converge in {max_rounds} rounds; ranges requested: {}",
                ranges(&trace)
            ));
        }

        let proof = source
            .change_proof(
                start_root.clone(),
                end_root.clone(),
                start_key.as_deref(),
                end_key.as_deref(),
                limit,
            )
            .map_err(|e| format!("round {rounds}: change_proof failed: {e}"))?;

        target
            .verify_change_proof(
                &proof,
                end_root.clone(),
                start_key.as_deref(),
                end_key.as_deref(),
                limit,
            )
            .map_err(|e| format!("round {rounds}: verify failed: {e}"))?
            .commit()
            .map_err(|e| format!("round {rounds}: commit failed: {e}"))?;

        collected.extend(proof.batch_ops().iter().cloned());

        let next = find_next_key_after_change_proof(&proof, end_key.as_deref())
            .map_err(|e| format!("round {rounds}: find_next_key failed: {e}"))?;
        match next {
            None => {
                return Ok(Walk {
                    rounds,
                    ops: collected,
                    trace,
                });
            }
            Some((next_start, next_end)) => {
                assert_eq!(
                    next_end, end_key,
                    "the continuation must carry the requested bound forward unchanged"
                );
                start_key = Some(next_start);
                end_key = next_end;
            }
        }
    }
}

/// The number of rounds a walk needs: one request per batch of changes, plus one
/// final request that finds nothing left and ends the walk.
///
/// The final request is only needed when the last change sorts strictly below the
/// requested end bound. A walk whose bound is itself a changed key learns it is
/// finished on the round that covers that key, so it needs one round fewer.
#[expect(
    clippy::arithmetic_side_effects,
    reason = "test-only; small operand counts, and the dev-profile overflow canary covers it"
)]
fn expected_rounds(changes: usize, limit: usize) -> usize {
    changes.div_ceil(limit) + 1
}

/// `key00..key99`, the key set every fixture here draws from.
fn hundred_keys() -> Vec<Vec<u8>> {
    (0..100u32)
        .map(|i| format!("key{i:02}").into_bytes())
        .collect()
}

/// Puts for `keys[range]`, each carrying `value`.
fn puts(
    keys: &[Vec<u8>],
    range: impl IntoIterator<Item = usize>,
    value: &[u8],
) -> Vec<BatchOp<Vec<u8>, Vec<u8>>> {
    range
        .into_iter()
        .map(|i| BatchOp::Put {
            key: keys[i].clone(),
            value: value.to_vec(),
        })
        .collect()
}

/// Deletes for `keys[range]`.
fn deletes(
    keys: &[Vec<u8>],
    range: impl IntoIterator<Item = usize>,
) -> Vec<BatchOp<Vec<u8>, Vec<u8>>> {
    range
        .into_iter()
        .map(|i| BatchOp::Delete {
            key: keys[i].clone(),
        })
        .collect()
}

/// Two databases that start in sync, and the roots either side of one batch of
/// changes applied only to `source`.
struct Fixture {
    source: Db,
    target: Db,
    keys: Vec<Vec<u8>>,
    start_root: api::HashKey,
    end_root: api::HashKey,
    /// How many keys differ between the two roots.
    changes: usize,
    _dirs: (TempDir, TempDir),
}

/// The fixture shape used by the Go suite's multi-round walk: 100 keys, the
/// first 50 shared by both databases, the remaining 50 introduced only on the
/// source, and optionally 20 removals over the even-indexed keys below 40.
///
/// The removals matter because a walk resumes from the key just above the last
/// op, so a removed last op means resuming above a key the end revision no longer
/// holds.
fn go_shaped_fixture(has_deletes: bool) -> Fixture {
    let keys = hundred_keys();
    let mut changes = puts(&keys, 50..100, b"v0");
    if has_deletes {
        changes.extend(deletes(&keys, (0..40).step_by(2)));
    }
    fixture_from(keys, 0..50, changes)
}

/// 100 shared keys with two clusters of changes: `key50..key59` and
/// `key80..key89`. A bounded walk between the clusters must leave the upper one
/// alone, which is what makes this shape useful.
fn clustered_fixture() -> Fixture {
    let keys = hundred_keys();
    let mut changes = puts(&keys, 50..60, b"v1");
    changes.extend(puts(&keys, 80..90, b"v1"));
    fixture_from(keys, 0..100, changes)
}

/// 100 shared keys where `key50..key59` are removed and `key80` is changed. A
/// bounded walk below `key80` must apply the removals and leave that change
/// alone, and its closing round starts just above a removed key.
fn removed_cluster_fixture() -> Fixture {
    let keys = hundred_keys();
    let mut changes = deletes(&keys, 50..60);
    changes.extend(puts(&keys, 80..81, b"v1"));
    fixture_from(keys, 0..100, changes)
}

/// Four keys `10/20/30/40`, with `10` changed and `20`/`30` removed. A cap of two
/// makes the first reply stop on the removal of `20`.
fn small_removal_fixture() -> Fixture {
    let keys: Vec<Vec<u8>> = [0x10, 0x20, 0x30, 0x40].map(|b| vec![b]).into();
    let mut changes = puts(&keys, 0..1, b"A");
    changes.extend(deletes(&keys, 1..3));
    let shared = 0..keys.len();
    fixture_from(keys, shared, changes)
}

/// The root a client should hold after applying exactly `ops` to the shared
/// starting state, which is every key in `keys` at `v0`.
fn root_after(keys: &[Vec<u8>], ops: Vec<BatchOp<Vec<u8>, Vec<u8>>>) -> api::HashKey {
    let (db, _dir) = new_db();
    db.propose(puts(keys, 0..keys.len(), b"v0"))
        .unwrap()
        .commit()
        .unwrap();
    db.propose(ops).unwrap().commit().unwrap();
    db.root_hash().unwrap()
}

/// Two synced databases holding `keys[shared]` at `v0`, with `changes` applied to
/// `source` only. Keys outside `shared` exist in neither database until `changes`
/// introduces them.
fn fixture_from(
    keys: Vec<Vec<u8>>,
    shared: std::ops::Range<usize>,
    changes: Vec<BatchOp<Vec<u8>, Vec<u8>>>,
) -> Fixture {
    let (source, ds) = new_db();
    let (target, dt) = new_db();
    let shared = puts(&keys, shared, b"v0");
    source.propose(shared.clone()).unwrap().commit().unwrap();
    target.propose(shared).unwrap().commit().unwrap();
    let start_root = source.root_hash().unwrap();

    let count = changes.len();
    source.propose(changes).unwrap().commit().unwrap();
    let end_root = source.root_hash().unwrap();

    Fixture {
        source,
        target,
        keys,
        start_root,
        end_root,
        changes: count,
        _dirs: (ds, dt),
    }
}

/// Assert a walk collected each changed key exactly once, in order. A count on
/// its own would accept a duplicate paired with an omission.
fn assert_keys_strictly_increase(walk: &Walk) {
    assert!(
        walk.ops.is_sorted_by(|a, b| a.key() < b.key()),
        "collected keys must strictly increase: {}",
        walk.ops
            .iter()
            .map(|op| hex::encode(op.key()))
            .collect::<Vec<_>>()
            .join(" ")
    );
}

/// An unbounded walk must reach `end_root`, and must do it in one round per batch
/// of changes plus the round that ends the walk. Anything more means the
/// continuation repeated a range.
#[test_case(false, 10 ; "no deletes")]
#[test_case(true, 10 ; "with deletes")]
#[test_case(false, 1 ; "one op per round")]
fn test_unbounded_walk_reaches_end_root(has_deletes: bool, limit: usize) {
    let f = go_shaped_fixture(has_deletes);
    let walk = sync_walk(
        &f.source,
        &f.target,
        &f.start_root,
        &f.end_root,
        None,
        NonZeroUsize::new(limit),
        expected_rounds(f.changes, limit).saturating_add(8),
    )
    .unwrap_or_else(|e| panic!("walk must converge: {e}"));

    assert_eq!(f.target.root_hash().unwrap(), f.end_root);
    assert_keys_strictly_increase(&walk);
    assert_eq!(walk.ops.len(), f.changes);
    assert_eq!(
        walk.rounds,
        expected_rounds(f.changes, limit),
        "{}",
        ranges(&walk.trace)
    );
}

/// A bounded walk stops exactly at the bound it asked for, whatever the bound's
/// relation to the changes. `clustered_fixture` changes `key50..key59` and
/// `key80..key89`; the three bounds probe the three positions that matter:
///
/// - below both clusters (`key40`): the walk finds nothing and ends in one round.
/// - between the clusters (`key65`): it fetches the lower cluster and stops, so
///   the upper cluster is never touched; one round of changes plus the closing
///   round.
/// - on the last changed key of the lower cluster (`key59`): the walk learns it
///   is done on the round that covers that key, with no closing round — the
///   `last op == end bound` case.
///
/// In every case the client must end holding exactly the changes at or below the
/// bound, which is what `root_with_only(keys, applied)` checks; a key fetched
/// above the bound would make that root disagree.
#[test_case(b"key40", 1, 50..50 ; "below both clusters")]
#[test_case(b"key65", 2, 50..60 ; "between the clusters")]
#[test_case(b"key59", 1, 50..60 ; "on the last changed key")]
fn test_bounded_walk_stops_at_its_bound(
    bound: &[u8],
    expected_rounds: usize,
    applied: std::ops::Range<usize>,
) {
    let f = clustered_fixture();
    let walk = sync_walk(
        &f.source,
        &f.target,
        &f.start_root,
        &f.end_root,
        Some(bound),
        NonZeroUsize::new(10),
        20,
    )
    .unwrap_or_else(|e| panic!("walk must converge: {e}"));

    assert_eq!(walk.rounds, expected_rounds, "{}", ranges(&walk.trace));
    assert_keys_strictly_increase(&walk);
    assert_eq!(
        f.target.root_hash().unwrap(),
        root_after(&f.keys, puts(&f.keys, applied, b"v1")),
        "the client must hold exactly the changes at or below the bound: {}",
        ranges(&walk.trace)
    );
}

/// The range a client fetches to cover what truncation cost it: bounded above,
/// and starting just above a key the end revision removed. The walks above
/// exercise those two ingredients separately, and this is where they combine.
///
/// The removals fill the request's limit exactly, so the first round ends on the
/// last removal while the requested bound is still higher. The closing round
/// therefore begins just above a removed key, finds nothing, and ends the walk.
/// The change at `key80` sits above the bound and must be left untouched.
#[test]
fn test_bounded_walk_closing_round_starts_above_a_removed_key() {
    let f = removed_cluster_fixture();
    let end = b"key65";
    let walk = sync_walk(
        &f.source,
        &f.target,
        &f.start_root,
        &f.end_root,
        Some(end),
        NonZeroUsize::new(10),
        20,
    )
    .unwrap_or_else(|e| panic!("walk must converge: {e}"));

    assert_eq!(walk.rounds, 2, "{}", ranges(&walk.trace));
    assert_eq!(
        walk.trace[1].0,
        hex::encode([f.keys[59].as_slice(), &[0]].concat()),
        "the closing round must start just above the last removed key: {}",
        ranges(&walk.trace)
    );
    assert_eq!(walk.ops.len(), 10, "only the in-range removals are fetched");
    assert_keys_strictly_increase(&walk);
    assert_eq!(
        f.target.root_hash().unwrap(),
        root_after(&f.keys, deletes(&f.keys, 50..60)),
        "the removals are applied and the change above the bound is not: {}",
        ranges(&walk.trace)
    );
    assert_ne!(f.target.root_hash().unwrap(), f.end_root);
}

/// A walk whose replies stop short on a removal converges with no recovery step.
///
/// The cap falls on the removal of `20`, so the proven range narrows to that key
/// and the reply is accepted over what it provably covers. The continuation is
/// computed from the last operation, so the walk keeps moving toward the bound the
/// client originally asked for.
///
/// This is the walk-level counterpart to the narrowing tests in `edge_cases.rs`:
/// those pin the right edge for a single reply, this pins that a client following
/// the ordinary loop reaches the end revision.
#[test]
fn test_walk_stopping_short_on_a_removal_converges() {
    let f = small_removal_fixture();
    let walk = sync_walk(
        &f.source,
        &f.target,
        &f.start_root,
        &f.end_root,
        Some(&[0xff]),
        NonZeroUsize::new(2),
        20,
    )
    .unwrap_or_else(|e| panic!("the walk must converge with no recovery step: {e}"));

    assert_eq!(f.target.root_hash().unwrap(), f.end_root);
    assert_keys_strictly_increase(&walk);
    assert_eq!(walk.ops.len(), f.changes, "every change is fetched once");
    assert_eq!(
        walk.rounds,
        expected_rounds(f.changes, 2),
        "two rounds of changes, then the closing round: {}",
        ranges(&walk.trace)
    );
}
