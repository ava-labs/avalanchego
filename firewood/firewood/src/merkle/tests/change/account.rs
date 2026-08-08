// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Change proofs over `ethhash` accounts with partial storage.
//!
//! When a change-proof proposal is built from only a subset of an account's
//! storage children, live hashing splices a *partial* `storageRoot` into the
//! account value. That value byte-differs from the proof's full on-disk value
//! even though both hash identically (`Preimage::write` recomputes
//! `storageRoot` from the current children at hash time). Without the
//! `account_values_equal_except_storage_root` relaxation in
//! `reconcile_branch_proof_node`, that mismatch is rejected as
//! `UnexpectedValue`.

use super::*;
use test_case::test_case;

use crate::merkle::tests::ethhash::{empty_code_hash, rlp_encode_account, rlp_encode_storage};

/// The single account every test below builds its storage trie under.
const ACCOUNT_KEY: [u8; 32] = [0x10u8; 32];

/// Build a 64-byte key whose first 32 bytes are `account_key` and whose
/// 33rd byte is `byte_32` (the rest zero). Used to construct storage keys
/// and range boundaries under a fixed account.
fn key_under_account(account_key: &[u8; 32], byte_32: u8) -> [u8; 64] {
    std::array::from_fn(|i| match i {
        0..=31 => account_key[i],
        32 => byte_32,
        _ => 0,
    })
}

/// Shared fixture: a source DB committed from empty to one account
/// (`ACCOUNT_KEY`) with four storage children at suffixes 0x10/0x30/0x60/0xC0.
/// Returns the DB and its tempdir guard (which the caller must keep alive),
/// plus the empty and populated roots.
fn source_with_four_storage_children() -> (Db, tempfile::TempDir, api::HashKey, api::HashKey) {
    let storage_keys: Vec<[u8; 64]> = [0x10u8, 0x30, 0x60, 0xC0]
        .iter()
        .map(|&p| key_under_account(&ACCOUNT_KEY, p))
        .collect();
    let storage_values: Vec<Box<[u8]>> = (1u8..=4)
        .map(|i| rlp_encode_storage(&[i; 32]).into_boxed_slice())
        .collect();
    let account_value = rlp_encode_account(1, 100, &[0u8; 32], &empty_code_hash());

    let (source, dir) = setup_db![];
    let (empty_root, root2) = setup_2nd_commit!(
        source,
        [
            (ACCOUNT_KEY.as_ref(), account_value.as_ref()),
            (storage_keys[0].as_ref(), storage_values[0].as_ref()),
            (storage_keys[1].as_ref(), storage_values[1].as_ref()),
            (storage_keys[2].as_ref(), storage_values[2].as_ref()),
            (storage_keys[3].as_ref(), storage_values[3].as_ref()),
        ]
    );
    (source, dir, empty_root, root2)
}

/// Bound choice for the parameterized partial-storage-children test below.
/// Resolved to an `Option<&[u8]>` against the fixed `ACCOUNT_KEY` and a
/// fixed mid-storage probe (`0x40`, between the storage children at 0x30
/// and 0x60).
#[derive(Debug, Clone, Copy)]
enum Bound {
    /// No bound on this side of the range.
    None,
    /// Bound exactly at `account_key` (32 bytes; the start-proof
    /// terminates at the account branch itself).
    AtAccount,
    /// Bound in the middle of the account's storage children, between
    /// storage[1] and storage[2] (64 bytes; the proof reconciliation
    /// runs through the storage trie).
    MidStorage,
}

/// Change proof from empty to a single account with four storage children
/// (at distinct first nibbles 0x10, 0x30, 0x60, 0xC0), bounded so the
/// `batch_ops` cover only a subset of the storage children. Verified against
/// an empty target.
///
/// The defect is **right-edge only**: storage children sort after the account
/// key, so only an end boundary can truncate an account's storage while the
/// account stays in range.
/// - `left_half_end_bound_only` and `start_at_account_end_mid_storage` keep the
///   account in range with a storage subset → trigger the defect (fail pre-fix).
/// - `right_half_start_bound_only` puts the account out of range via the start
///   bound, so its value is never compared: a guard that passes with and
///   without the fix.
#[test_case(Bound::None, Bound::MidStorage ; "left_half_end_bound_only")]
#[test_case(Bound::AtAccount, Bound::MidStorage ; "start_at_account_end_mid_storage")]
#[test_case(Bound::MidStorage, Bound::None ; "right_half_start_bound_only")]
fn test_change_proof_partial_storage_children_against_empty(start_bound: Bound, end_bound: Bound) {
    let (source, _dir_source, empty_root, root2) = source_with_four_storage_children();

    // Mid-storage probe between the storage children at suffixes 0x30 and 0x60.
    let mid_storage = key_under_account(&ACCOUNT_KEY, 0x40);
    let resolve = |bound: Bound| -> Option<&[u8]> {
        match bound {
            Bound::None => None,
            Bound::AtAccount => Some(ACCOUNT_KEY.as_ref()),
            Bound::MidStorage => Some(mid_storage.as_ref()),
        }
    };
    let start_key = resolve(start_bound);
    let end_key = resolve(end_bound);

    let proof = source
        .change_proof(empty_root.clone(), root2.clone(), start_key, end_key, None)
        .unwrap();
    let ctx =
        verify_change_proof_structure(&proof, root2.clone(), start_key, end_key, None).unwrap();

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();
    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}

/// Cross-batch ordering: apply the RIGHT half first, then the LEFT half.
/// State sync receives batches in arbitrary order. When the right-side
/// batch lands first, the target's depth-64 branch has storage children
/// but no account value (the account is in the left-side batch). When the
/// left batch arrives later, the account value is added to the existing
/// branch and live hashing recomputes storageRoot from the full child set.
/// Final root must match the source's root.
///
/// Note: this test passes both with and without the reconcile-side fix
/// for partial-vs-full storageRoot conflicts. By the time the LEFT batch
/// is applied, the cumulative children in the local DB are complete, so
/// the proposal's storageRoot matches the proof's. The in-range cases of
/// `test_change_proof_partial_storage_children_against_empty` are what
/// actually exercise the reconcile fix; this test guards convergence
/// across batches.
#[test]
fn test_change_proof_arbitrary_order_right_then_left_converges() {
    // Source: empty → account + 4 storage children.
    let (source, _dir_source, empty_root, full_root) = source_with_four_storage_children();

    // mid_key sits between storage[1] and storage[2].
    let mid_key = key_under_account(&ACCOUNT_KEY, 0x40);

    let proof_left = source
        .change_proof(
            empty_root.clone(),
            full_root.clone(),
            None,
            Some(mid_key.as_ref()),
            None,
        )
        .unwrap();
    let ctx_left = verify_change_proof_structure(
        &proof_left,
        full_root.clone(),
        None,
        Some(mid_key.as_ref()),
        None,
    )
    .unwrap();

    let proof_right = source
        .change_proof(
            empty_root.clone(),
            full_root.clone(),
            Some(mid_key.as_ref()),
            None,
            None,
        )
        .unwrap();
    let ctx_right = verify_change_proof_structure(
        &proof_right,
        full_root.clone(),
        Some(mid_key.as_ref()),
        None,
        None,
    )
    .unwrap();

    // Apply RIGHT first, then LEFT, to an empty target.
    let (target, _dir_target) = setup_db![];
    let target_empty_root = target.root_hash().unwrap();

    // Step 1: apply RIGHT half. Verifies, then commits.
    let parent = target.revision(target_empty_root.clone()).unwrap();
    let proposal_right = target
        .apply_change_proof_to_parent(&proof_right, &*parent)
        .unwrap();
    crate::merkle::verify_change_proof_root_hash(&proof_right, &ctx_right, &proposal_right)
        .unwrap();
    proposal_right.commit().unwrap();
    let after_right_root = target.root_hash().unwrap();
    assert_ne!(
        after_right_root, full_root,
        "intermediate state should differ from full state"
    );

    // Step 2: apply LEFT half on top. The account value is in this batch's
    // batch_ops, so it gets added to the existing depth-64 branch (which already
    // has S2/S3/S4 from the right batch), live hashing recomputes storageRoot
    // from the now-full child set.
    let parent = target.revision(after_right_root).unwrap();
    let proposal_left = target
        .apply_change_proof_to_parent(&proof_left, &*parent)
        .unwrap();
    crate::merkle::verify_change_proof_root_hash(&proof_left, &ctx_left, &proposal_left).unwrap();
    proposal_left.commit().unwrap();
    let final_root = target.root_hash().unwrap();

    // Final root must match the source's full-state root.
    assert_eq!(
        final_root, full_root,
        "final root after applying both batches should match source"
    );
}

/// Both a start and an end boundary land inside one account's storage
/// trie: start between storage[1] (0x30) and storage[2] (0x60), end between
/// storage[2] and storage[3] (0xC0). The account sorts before the start bound
/// so it is out-of-range and only the middle storage child (0x60) is in range.
/// Because the account is out-of-range its value is never compared, so this is
/// a **guard**: it passes with and without the relaxation, exercising both
/// edges of the storage trie in a single proof without a false-positive.
/// Verified against an empty target.
#[test]
fn test_change_proof_both_bounds_inside_account_storage() {
    let (source, _dir_source, empty_root, root2) = source_with_four_storage_children();

    let start = key_under_account(&ACCOUNT_KEY, 0x40);
    let end = key_under_account(&ACCOUNT_KEY, 0xB0);

    let proof = source
        .change_proof(
            empty_root.clone(),
            root2.clone(),
            Some(start.as_ref()),
            Some(end.as_ref()),
            None,
        )
        .unwrap();
    let ctx = verify_change_proof_structure(
        &proof,
        root2.clone(),
        Some(start.as_ref()),
        Some(end.as_ref()),
        None,
    )
    .unwrap();

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();
    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}

/// The account is in-range, but a tight end bound (just past the account
/// key, before its first storage slot at suffix 0x10) excludes ALL of its
/// storage children. The proposal inserts the account with no storage children,
/// so live hashing gives it the *empty* storageRoot while the proof carries the
/// full one. This is the zero-storage extreme of the partial-storage case. It
/// **triggers the defect**: `UnexpectedValue` before the
/// `account_values_equal_except_storage_root` relaxation, passes after.
/// Verified against an empty target.
#[test]
fn test_change_proof_account_in_range_no_storage_in_range() {
    let (source, _dir_source, empty_root, root2) = source_with_four_storage_children();

    // end sits between the account key and its first storage slot (suffix
    // 0x10), so the account is in range but none of its storage children are.
    let end = key_under_account(&ACCOUNT_KEY, 0x00);

    let proof = source
        .change_proof(
            empty_root.clone(),
            root2.clone(),
            None,
            Some(end.as_ref()),
            None,
        )
        .unwrap();
    let ctx = verify_change_proof_structure(&proof, root2.clone(), None, Some(end.as_ref()), None)
        .unwrap();

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();
    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}

/// K=1 boundary: an account with a single storage child, which can never be a
/// *partial* subset (it is fully in range or fully excluded). The end bound
/// here sits just before the lone storage slot (suffix 0x10), so the account
/// is in range with zero storage children: the proposal hashes the empty
/// storageRoot while the proof carries the single-child one. **Triggers the
/// defect** (`UnexpectedValue` before the
/// `account_values_equal_except_storage_root` relaxation, passes after).
#[test]
fn test_change_proof_single_storage_child_truncated() {
    let storage_key = key_under_account(&ACCOUNT_KEY, 0x10);
    let storage_value = rlp_encode_storage(&[1u8; 32]);
    let account_value = rlp_encode_account(1, 100, &[0u8; 32], &empty_code_hash());

    let (source, _dir_source) = setup_db![];
    let (empty_root, root2) = setup_2nd_commit!(
        source,
        [
            (ACCOUNT_KEY.as_ref(), account_value.as_ref()),
            (storage_key.as_ref(), storage_value.as_ref()),
        ]
    );

    // end sits between the account key and its single storage slot (suffix
    // 0x10), so the account is in range but its one storage child is not.
    let end = key_under_account(&ACCOUNT_KEY, 0x00);

    let proof = source
        .change_proof(
            empty_root.clone(),
            root2.clone(),
            None,
            Some(end.as_ref()),
            None,
        )
        .unwrap();
    let ctx = verify_change_proof_structure(&proof, root2.clone(), None, Some(end.as_ref()), None)
        .unwrap();

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();
    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}
