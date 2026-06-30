// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Empty start trie tests.
//!
//! When the start revision is empty, every key in the end revision is a new
//! insertion. These tests exercise the interaction between empty start tries
//! and the divergent child / value skip logic.

use super::*;

/// Empty start trie, single key inserted. Complete proof (no bounds).
#[test]
fn test_empty_start_trie_single_key_no_bounds() {
    let (db, _dir) = setup_db![];
    let (empty_root, root2) = setup_2nd_commit!(db, [(b"\x50", b"hello")]);

    let proof = db
        .change_proof(empty_root, root2.clone(), None, None, None)
        .unwrap();
    let ctx = verify_change_proof_structure(&proof, root2.clone(), None, None, None).unwrap();

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();

    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}

/// Empty start trie, multiple keys inserted, bounded range with inclusion
/// start proof (`start_key` exists in end trie).
#[test]
fn test_empty_start_trie_bounded_inclusion() {
    let (db, _dir) = setup_db![];
    let (empty_root, root2) = setup_2nd_commit!(
        db,
        [
            (b"\x10", b"a"),
            (b"\x20", b"b"),
            (b"\x30", b"c"),
            (b"\x40", b"d")
        ]
    );

    let proof = db
        .change_proof(
            empty_root.clone(),
            root2.clone(),
            Some(b"\x10"),
            Some(b"\x30"),
            None,
        )
        .unwrap();
    let ctx =
        verify_change_proof_structure(&proof, root2.clone(), Some(b"\x10"), Some(b"\x30"), None)
            .unwrap();

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();

    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}

/// Empty start trie, bounded range with exclusion start proof
/// (`start_key` does NOT exist in end trie).
#[test]
fn test_empty_start_trie_bounded_exclusion() {
    let (db, _dir) = setup_db![];
    let (empty_root, root2) =
        setup_2nd_commit!(db, [(b"\x10", b"a"), (b"\x20", b"b"), (b"\x30", b"c")]);

    let proof = db
        .change_proof(
            empty_root.clone(),
            root2.clone(),
            Some(b"\x05"),
            Some(b"\x30"),
            None,
        )
        .unwrap();
    let ctx =
        verify_change_proof_structure(&proof, root2.clone(), Some(b"\x05"), Some(b"\x30"), None)
            .unwrap();

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();

    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}

/// Empty start trie with prefix keys (key is a prefix of another key).
/// Tests that branch nodes with values at prefix keys are handled
/// correctly when the start trie is empty. Includes an empty key (`b""`)
/// which places a value on the root node itself.
#[test]
fn test_empty_start_trie_prefix_keys() {
    let (db, _dir) = setup_db![];
    let (empty_root, root2) = setup_2nd_commit!(
        db,
        [
            (b"", b"root_val"),
            (b"\x10", b"prefix"),
            (b"\x10\x50", b"child1"),
            (b"\x10\x50\xaa", b"grandchild"),
            (b"\x20", b"other"),
        ]
    );

    let (target, _dir_target) = setup_db![];
    let empty_root_target = target.root_hash().unwrap();

    // Range [\x10\x50, \x20]: start_key \x10\x50 exists (inclusion).
    // The node at \x10 is on the start proof path with a value, but
    // its byte key < start_key — value check must be skipped.
    let proof = db
        .change_proof(
            empty_root.clone(),
            root2.clone(),
            Some(b"\x10\x50"),
            Some(b"\x20"),
            None,
        )
        .unwrap();
    let ctx = verify_change_proof_structure(
        &proof,
        root2.clone(),
        Some(b"\x10\x50"),
        Some(b"\x20"),
        None,
    )
    .unwrap();
    verify_and_check(&target, &proof, &ctx, empty_root_target.clone()).unwrap();

    // Range [\x10\x50\x00, \x20]: start_key doesn't exist (exclusion).
    // Exercises the divergent child path with an empty start trie.
    let proof = db
        .change_proof(
            empty_root.clone(),
            root2.clone(),
            Some(b"\x10\x50\x00"),
            Some(b"\x20"),
            None,
        )
        .unwrap();
    let ctx = verify_change_proof_structure(
        &proof,
        root2.clone(),
        Some(b"\x10\x50\x00"),
        Some(b"\x20"),
        None,
    )
    .unwrap();
    verify_and_check(&target, &proof, &ctx, empty_root_target).unwrap();
}
