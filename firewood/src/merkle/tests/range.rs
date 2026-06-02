// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::*;
use crate::RangeProof;
use firewood_storage::U4;

type KeyValuePairs = Vec<(Box<[u8]>, Box<[u8]>)>;

/// Helper to build a `PathBuf` from a slice of nibble values.
fn nibble_path(nibbles: &[u8]) -> PathBuf {
    nibbles
        .iter()
        .map(|&n| PathComponent(U4::new_masked(n)))
        .collect()
}

/// Helper to build a minimal `ProofNode` with the given nibble key.
fn proof_node(nibbles: &[u8]) -> ProofNode {
    ProofNode {
        key: nibble_path(nibbles),
        partial_len: 0,
        value_digest: None,
        child_hashes: Children::new(),
    }
}

/// Build key-value pairs for range proof verification using the key-value
/// iterator's values rather than the original inserted values. Proof
/// generation may rewrite account storageRoot fields (ethhash), and the
/// iterator applies the same fix, so iterator values match what the proof
/// nodes contain. Panics if a key is not present in the trie — callers are
/// expected to pass keys that were just committed.
fn stored_key_values<K: AsRef<[u8]>>(
    merkle: &Merkle<NodeStore<Committed, MemStore>>,
    keys: impl IntoIterator<Item = K>,
) -> KeyValuePairs {
    keys.into_iter()
        .map(|k| {
            let key = k.as_ref();
            let (found_key, val) = merkle
                .key_value_iter_from_key(key)
                .next()
                .expect("iterator should yield at least one item")
                .expect("iterator should not error");
            assert_eq!(
                found_key.as_ref(),
                key,
                "iterator returned wrong key: wanted {key:?}, got {found_key:?}"
            );
            (key.to_vec().into_boxed_slice(), val)
        })
        .collect()
}

#[test]
fn outside_children_empty_proof() {
    let result = compute_outside_children(&[], EdgeBoundary::Left(None)).unwrap();
    assert!(result.is_empty());
}

#[test]
fn outside_children_single_node_no_boundary() {
    let nodes = [proof_node(&[1, 2])];
    let result = compute_outside_children(&nodes, EdgeBoundary::Left(None)).unwrap();
    assert!(result.is_empty());
}

#[test]
fn outside_children_single_node_exact_match() {
    // Boundary matches terminal exactly — no children marked.
    let nodes = [proof_node(&[1, 2])];
    // boundary key 0x12 expands to nibbles [1, 2]
    let result = compute_outside_children(&nodes, EdgeBoundary::Left(Some(&[0x12]))).unwrap();
    assert!(result.is_empty());
}

#[test]
fn outside_children_single_node_exact_match_right_edge() {
    // Boundary matches terminal exactly on right edge — all children marked outside.
    let nodes = [proof_node(&[1, 2])];
    let result = compute_outside_children(
        &nodes,
        EdgeBoundary::Right(RightBoundary::InRange(Some(&[0x12]))),
    )
    .unwrap();
    // Look up the mask for the terminal node at [1, 2].
    let mask = result[&nibble_path(&[1, 2])];
    for i in 0..16u8 {
        assert!(
            mask.is_set(U4::new_masked(i)),
            "child {i} should be outside on right edge exact match"
        );
    }
}

#[test]
fn outside_children_ancestor_left_edge() {
    // Terminal key [1], boundary key 0x15 → nibbles [1, 5].
    // Terminal is ancestor of boundary. On-path nibble = 5.
    // Left edge: children < 5 are outside, plus child 5 itself.
    let nodes = [proof_node(&[1])];
    let result = compute_outside_children(&nodes, EdgeBoundary::Left(Some(&[0x15]))).unwrap();
    let mask = result[&nibble_path(&[1])];
    // Children 0..=5 should be outside (left of 5, plus 5 itself)
    for i in 0..16u8 {
        assert_eq!(
            mask.is_set(U4::new_masked(i)),
            i <= 5,
            "left edge ancestor: child {i}"
        );
    }
}

#[test]
fn outside_children_ancestor_right_edge() {
    // Terminal key [1], boundary key 0x15 → nibbles [1, 5].
    // Right edge: children > 5 are outside, plus child 5 itself.
    let nodes = [proof_node(&[1])];
    let result = compute_outside_children(
        &nodes,
        EdgeBoundary::Right(RightBoundary::InRange(Some(&[0x15]))),
    )
    .unwrap();
    let mask = result[&nibble_path(&[1])];
    for i in 0..16u8 {
        assert_eq!(
            mask.is_set(U4::new_masked(i)),
            i >= 5,
            "right edge ancestor: child {i}"
        );
    }
}

#[test]
fn outside_children_diverges_past_terminal_left() {
    // Terminal key [1, 3], boundary nibbles [1, 5] (diverge at pos 1: 5 > 3).
    // Left edge + boundary past terminal → all children outside.
    let nodes = [proof_node(&[1, 3])];
    let result = compute_outside_children(&nodes, EdgeBoundary::Left(Some(&[0x15]))).unwrap();
    let mask = result[&nibble_path(&[1, 3])];
    for i in 0..16u8 {
        assert!(
            mask.is_set(U4::new_masked(i)),
            "all outside when boundary past terminal (left): child {i}"
        );
    }
}

#[test]
fn outside_children_diverges_before_terminal_left() {
    // Terminal key [1, 7], boundary nibbles [1, 5] (diverge at pos 1: 5 < 7).
    // Left edge + boundary before terminal → no children outside.
    let nodes = [proof_node(&[1, 7])];
    let result = compute_outside_children(&nodes, EdgeBoundary::Left(Some(&[0x15]))).unwrap();
    assert!(
        !result.contains_key(&nibble_path(&[1, 7])),
        "no mask when boundary before terminal"
    );
}

#[test]
fn outside_children_two_nodes_left_edge() {
    // Parent [1], child [1, 5]. On-path nibble = 5.
    // Left edge: children < 5 on parent are outside.
    let nodes = [proof_node(&[1]), proof_node(&[1, 5])];
    let result = compute_outside_children(&nodes, EdgeBoundary::Left(None)).unwrap();
    let mask = result[&nibble_path(&[1])];
    for i in 0..16u8 {
        assert_eq!(
            mask.is_set(U4::new_masked(i)),
            i < 5,
            "two nodes left: child {i}"
        );
    }
}

#[test]
fn outside_children_two_nodes_right_edge() {
    // Parent [1], child [1, 5]. On-path nibble = 5.
    // Right edge: children > 5 on parent are outside.
    let nodes = [proof_node(&[1]), proof_node(&[1, 5])];
    let result =
        compute_outside_children(&nodes, EdgeBoundary::Right(RightBoundary::InRange(None)))
            .unwrap();
    let mask = result[&nibble_path(&[1])];
    for i in 0..16u8 {
        assert_eq!(
            mask.is_set(U4::new_masked(i)),
            i > 5,
            "two nodes right: child {i}"
        );
    }
}

#[test]
fn outside_children_child_not_prefixed_by_parent() {
    // Child key [2, 5] is not prefixed by parent key [1].
    // child.key.get(parent.key.len()) = [2,5].get(1) = Some(5), so no error;
    // but child key [1] with parent [1, 2] means child.key.get(2) = None → error.
    let nodes = [proof_node(&[1, 2]), proof_node(&[1])];
    let result = compute_outside_children(&nodes, EdgeBoundary::Left(None));
    assert!(
        matches!(result, Err(ProofError::ShouldBePrefixOfNextKey)),
        "expected ShouldBePrefixOfNextKey, got {result:?}"
    );
}

#[test]
// Tests that missing keys can also be proven. The test explicitly uses a single
// entry trie and checks for missing keys both before and after the single entry.
fn test_missing_key_proof() {
    let items = [("k", "v")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    for key in ["a", "j", "l", "z"] {
        let proof = merkle.prove(key.as_ref()).unwrap();
        assert!(!proof.is_empty());
        assert_eq!(proof.len(), 1);

        proof.verify(key, None::<&[u8]>, &root_hash).unwrap();
    }
}

#[test]
// A truncated bounded range proof: caller asks for [start, end] with a limit
// that is hit, so the generator anchors `end_proof` at the last returned key
// rather than at `end`. The verifier must accept the proof when called with
// the originally requested `(start, end)` — those are the only bounds the
// caller has; it cannot predict the post-truncation anchor.
fn test_truncated_bounded_range_proof_round_trip() {
    let items: Vec<([u8; 4], [u8; 4])> = (0u32..1000)
        .map(|i| (i.to_be_bytes(), i.to_be_bytes()))
        .collect();
    let merkle = init_merkle(items.iter().map(|(k, v)| (k.as_slice(), v.as_slice())));
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // Bound covers many keys; limit forces truncation.
    let start = items[10].0;
    let end = items[900].0;
    let limit = NonZeroUsize::new(8).unwrap();

    let range_proof = merkle
        .range_proof(Some(&start), Some(&end), Some(limit))
        .unwrap();

    assert_eq!(range_proof.key_values().len(), limit.get());

    // Verify with the *original* requested bounds — the only ones the caller
    // can provide. End_proof anchors at the last returned key, not `end`.
    verify_range_proof(Some(&start), Some(&end), &root_hash, &range_proof).unwrap();
}

#[test]
// Unbounded full-range proof: start_key = end_key = None. `Merkle::range_proof`
// produces a proof with empty start_proof, empty end_proof, and every key in
// the trie. The verifier must accept this shape — the root-hash reconstruction
// is the safety net. Regression for the FFI path (TestRoundTripSerialization
// et al.) where the prior check rejected empty end_proof whenever key_values
// was non-empty.
fn test_full_range_proof_verifies_unbounded() {
    let merkle = init_merkle((u8::MIN..=u8::MAX).map(|k| ([k], [k])));
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let range_proof = merkle.range_proof(None, None, None).unwrap();
    assert_eq!(range_proof.key_values().len(), u8::MAX as usize + 1);
    assert!(range_proof.start_proof().is_empty());
    assert!(range_proof.end_proof().is_empty());

    verify_range_proof::<_>(
        Option::<&[u8]>::None,
        Option::<&[u8]>::None,
        &root_hash,
        &range_proof,
    )
    .unwrap();
}

#[test]
// End-proof terminal is a value-node strict prefix of last_kv: trie
// {0x05, 0x10, 0x10\x10, 0x10\x50}, range [0x05, 0x10\x30]. The end_proof
// terminates at the branch node `0x10` (which has value "parent" and
// children at nibbles 1 and 5). The terminal's full key 0x10 is *less
// than* last_kv 0x10\x10, so the right-edge anchor is the caller's
// requested bound `0x10\x30` (inclusive); verify_edge then anchors the
// proof against `0x10\x30` and the hash check correctly catches any
// tampering of `0x10\x10`'s value.
fn test_terminal_strict_prefix_of_last_kv_verifies() {
    let items: &[(&[u8], &[u8])] = &[
        (b"\x05", b"before"),
        (b"\x10", b"parent"),
        (b"\x10\x10", b"in_range"),
        (b"\x10\x50", b"after"),
    ];
    let merkle = init_merkle(items.iter().copied());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let range_proof = merkle
        .range_proof(Some(b"\x05".as_slice()), Some(b"\x10\x30".as_slice()), None)
        .unwrap();

    // Honest proof contains the three in-range keys.
    assert_eq!(range_proof.key_values().len(), 3);

    verify_range_proof(
        Some(b"\x05".as_slice()),
        Some(b"\x10\x30".as_slice()),
        &root_hash,
        &range_proof,
    )
    .unwrap();
}

#[test]
// Dropped trailing key collapses to a smaller proven range: same trie as
// `test_terminal_strict_prefix_of_last_kv_verifies`, but an attacker
// omits `0x10\x10` from key_values. The end_proof's terminal is the
// branch `0x10` — terminal_full_key now equals the (tampered) last_kv
// 0x10, so the right-edge anchor is **inclusive at last_kv = 0x10**.
// Under that anchor the proof verifies (it is structurally a valid proof
// of [0x05, 0x10]). The verifier reports success; the *caller* observes
// that `last_kv < the requested bound` and re-requests `(0x10, 0x10\x30]`
// — that follow-up query is what surfaces the omitted `0x10\x10`. No
// information is hidden.
fn test_dropped_trailing_key_accepted_as_partial_coverage() {
    let items: &[(&[u8], &[u8])] = &[
        (b"\x05", b"before"),
        (b"\x10", b"parent"),
        (b"\x10\x10", b"in_range"),
        (b"\x10\x50", b"after"),
    ];
    let merkle = init_merkle(items.iter().copied());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let honest = merkle
        .range_proof(Some(b"\x05".as_slice()), Some(b"\x10\x30".as_slice()), None)
        .unwrap();

    // Drop `0x10\x10` from key_values, keeping the original end_proof.
    let tampered_kvs: KeyValuePairs = honest
        .key_values()
        .iter()
        .filter(|(k, _)| k.as_ref() != b"\x10\x10")
        .map(|(k, v)| (k.as_ref().into(), v.as_ref().into()))
        .collect();
    assert_eq!(tampered_kvs.len(), 2);
    assert_eq!(tampered_kvs.last().unwrap().0.as_ref(), b"\x10");

    let tampered = RangeProof::new(
        crate::Proof::<Box<[ProofNode]>>::new(honest.start_proof().as_ref().into()),
        crate::Proof::<Box<[ProofNode]>>::new(honest.end_proof().as_ref().into()),
        tampered_kvs.into_boxed_slice(),
    );

    // The verifier accepts: the proof is internally consistent and proves
    // `[0x05, 0x10]`. It is *not* the verifier's job to enforce that the
    // proven range matches the requested range — partial coverage is a
    // valid outcome the caller is responsible for handling.
    verify_range_proof(
        Some(b"\x05".as_slice()),
        Some(b"\x10\x30".as_slice()),
        &root_hash,
        &tampered,
    )
    .unwrap();

    // The caller's `last_kv < requested_last` check is what flags partial
    // coverage and should drive a follow-up request.
    assert!(tampered.key_values().last().unwrap().0.as_ref() < b"\x10\x30".as_slice());
}

#[test]
// Tampering the *value* of an in-range key must be rejected. Same trie
// and request as `test_terminal_strict_prefix_of_last_kv_verifies`, but
// the value of `0x10\x10` is altered in key_values. With anchor =
// requested bound `0x10\x30` (inclusive, fallback path),
// `compute_outside_children` keeps `0x10`'s child 1 in-range, so the
// hash check recurses into the proving trie's tampered `0x10\x10` leaf
// and the root hash mismatches.
fn test_tampered_in_range_value_rejected() {
    let items: &[(&[u8], &[u8])] = &[
        (b"\x05", b"before"),
        (b"\x10", b"parent"),
        (b"\x10\x10", b"in_range"),
        (b"\x10\x50", b"after"),
    ];
    let merkle = init_merkle(items.iter().copied());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let honest = merkle
        .range_proof(Some(b"\x05".as_slice()), Some(b"\x10\x30".as_slice()), None)
        .unwrap();

    let mut tampered_kvs: KeyValuePairs = honest
        .key_values()
        .iter()
        .map(|(k, v)| (k.as_ref().into(), v.as_ref().into()))
        .collect();
    // Tamper the value of `0x10\x10`.
    for (k, v) in &mut tampered_kvs {
        if k.as_ref() == b"\x10\x10" {
            *v = b"WRONG".to_vec().into_boxed_slice();
        }
    }

    let tampered = RangeProof::new(
        crate::Proof::<Box<[ProofNode]>>::new(honest.start_proof().as_ref().into()),
        crate::Proof::<Box<[ProofNode]>>::new(honest.end_proof().as_ref().into()),
        tampered_kvs.into_boxed_slice(),
    );

    let result = verify_range_proof(
        Some(b"\x05".as_slice()),
        Some(b"\x10\x30".as_slice()),
        &root_hash,
        &tampered,
    );
    assert!(
        result.is_err(),
        "tampered \\x10\\x10 value should be rejected, got: {result:?}"
    );
}

#[test]
// Divergent terminal past last_kv: trie {0x05, 0x10\x50}, range
// [0x05, 0x10\x30]. The end_proof of `0x10\x30` walks root → leaf
// `0x10\x50` (the leaf's path diverges from `0x10\x30` at the `5` vs `3`
// nibble). terminal.value_digest is set, terminal_full_key = 0x10\x50 >
// last_kv 0x05 → right-edge anchor is **exclusive at 0x10\x50**. The
// proof structurally proves `[0x05, 0x10\x50)`, which covers the
// requested `[0x05, 0x10\x30]`.
fn test_divergent_terminal_past_last_kv() {
    let items: &[(&[u8], &[u8])] = &[(b"\x05", b"a"), (b"\x10\x50", b"z")];
    let merkle = init_merkle(items.iter().copied());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let range_proof = merkle
        .range_proof(Some(b"\x05".as_slice()), Some(b"\x10\x30".as_slice()), None)
        .unwrap();

    // Only `0x05` is in [0x05, 0x10\x30]; `0x10\x50` is past the bound.
    assert_eq!(range_proof.key_values().len(), 1);
    assert_eq!(range_proof.key_values()[0].0.as_ref(), b"\x05");

    verify_range_proof(
        Some(b"\x05".as_slice()),
        Some(b"\x10\x30".as_slice()),
        &root_hash,
        &range_proof,
    )
    .unwrap();
}

#[test]
// Symmetric mirror of `test_divergent_terminal_past_last_kv` on the left
// edge: trie {0x05, 0x10}, range [0x07, 0x10]. `start_key = 0x07` doesn't
// exist; `prove(0x07)` walks root → leaf 0x05 (divergent), so the
// start_proof's terminal is a value-node at full key 0x05, which is
// *less than* `first_kv = 0x10`. This is the structural mirror of the
// right-edge "exclusive at terminal" case — the terminal's full key is
// outside the proven range, on the wrong side of the left boundary.
//
// The current code uses `EdgeBoundary::Left(start_key)` (always inclusive
// on the left edge); if the existing logic implicitly handles this case
// correctly, this test passes. If it doesn't, we'd need a `LeftOutOfRange`
// variant for symmetry.
fn test_divergent_terminal_before_first_kv() {
    let items: &[(&[u8], &[u8])] = &[(b"\x05", b"a"), (b"\x10", b"b")];
    let merkle = init_merkle(items.iter().copied());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let range_proof = merkle
        .range_proof(Some(b"\x07".as_slice()), Some(b"\x10".as_slice()), None)
        .unwrap();

    assert_eq!(range_proof.key_values().len(), 1);
    assert_eq!(range_proof.key_values()[0].0.as_ref(), b"\x10");

    verify_range_proof(
        Some(b"\x07".as_slice()),
        Some(b"\x10".as_slice()),
        &root_hash,
        &range_proof,
    )
    .unwrap();
}

#[test]
// Tests normal range proof with both edge proofs as the existent proof.
// The test cases are generated randomly.
fn test_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    for _ in 0..10 {
        let start = rng.random_range(0..items.len() - 1);
        let end = rng.random_range(start + 1..items.len());

        let start_proof = merkle.prove(items[start].0).unwrap();
        let end_proof = merkle.prove(items[end - 1].0).unwrap();

        let key_values = stored_key_values(&merkle, items[start..end].iter().map(|(k, _)| *k));

        let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        verify_range_proof(
            Some(items[start].0),
            Some(items[end - 1].0),
            &root_hash,
            &range_proof,
        )
        .unwrap();
    }
}

#[test]
// Tests that out-of-order key-value pairs in a range proof are rejected.
fn test_bad_range_proof_out_of_order() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    for _ in 0..10 {
        let start = rng.random_range(0..items.len() - 2);
        let end = rng.random_range(start + 2..items.len());

        let mut keys: Vec<[u8; 32]> = Vec::new();
        let mut vals: Vec<[u8; 20]> = Vec::new();
        for item in &items[start..end] {
            keys.push(*item.0);
            vals.push(*item.1);
        }

        let index_1 = rng.random_range(0..end - start);
        let index_2 = rng.random_range(0..end - start);
        if index_1 == index_2 {
            continue;
        }
        #[expect(
            clippy::disallowed_methods,
            reason = "index_1 and index_2 are in 0..end-start, which is the vec length"
        )]
        keys.swap(index_1, index_2);
        #[expect(
            clippy::disallowed_methods,
            reason = "index_1 and index_2 are in 0..end-start, which is the vec length"
        )]
        vals.swap(index_1, index_2);

        let key_values: KeyValuePairs = keys
            .iter()
            .zip(vals.iter())
            .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
            .collect();

        let start_proof = merkle.prove(items[start].0).unwrap();
        let end_proof = merkle.prove(items[end - 1].0).unwrap();

        let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        assert!(
            verify_range_proof(
                Some(items[start].0),
                Some(items[end - 1].0),
                &root_hash,
                &range_proof,
            )
            .is_err()
        );
    }
}

#[test]
// Detects a modified key via trie reconstruction and root hash mismatch.
fn test_bad_range_proof_modified_key() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start = 100;
    let end = 200;

    let start_proof = merkle.prove(items[start].0).unwrap();
    let end_proof = merkle.prove(items[end - 1].0).unwrap();

    let mut kvs: KeyValuePairs = items[start..end]
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let mid = kvs.len() / 2;
    let mut key = kvs[mid].0.to_vec();
    key[0] ^= 0x01;
    kvs[mid].0 = key.into_boxed_slice();
    kvs.sort_by(|(a, _), (b, _)| a.cmp(b));

    let range_proof = RangeProof::new(start_proof, end_proof, kvs.into_boxed_slice());
    assert!(
        verify_range_proof(
            Some(items[start].0),
            Some(items[end - 1].0),
            &root_hash,
            &range_proof,
        )
        .is_err(),
        "modified key should be detected"
    );
}

#[test]
// Detects a modified value via trie reconstruction and root hash mismatch.
// In ethhash mode, account values at depth 32 have their storageRoot field
// replaced with a computed hash during hashing. A blind XOR on the raw value
// may only affect the storageRoot (or the RLP header that wraps it), making
// the modification invisible to the hash. This test is not meaningful under
// ethhash because the hashing intentionally ignores part of the value.
#[cfg(not(feature = "ethhash"))]
fn test_bad_range_proof_modified_value() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start = 100;
    let end = 200;

    let start_proof = merkle.prove(items[start].0).unwrap();
    let end_proof = merkle.prove(items[end - 1].0).unwrap();

    let mut kvs: KeyValuePairs = items[start..end]
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let mid = kvs.len() / 2;
    let mut val = kvs[mid].1.to_vec();
    val[0] ^= 0x01;
    kvs[mid].1 = val.into_boxed_slice();

    let range_proof = RangeProof::new(start_proof, end_proof, kvs.into_boxed_slice());
    assert!(
        verify_range_proof(
            Some(items[start].0),
            Some(items[end - 1].0),
            &root_hash,
            &range_proof,
        )
        .is_err(),
        "modified value should be detected"
    );
}

#[test]
// Detects gapped entries (missing middle element) via trie reconstruction
// and root hash mismatch.
fn test_bad_range_proof_gapped_entries() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start = 100;
    let end = 200;

    let start_proof = merkle.prove(items[start].0).unwrap();
    let end_proof = merkle.prove(items[end - 1].0).unwrap();

    let mut kvs: KeyValuePairs = items[start..end]
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let mid = kvs.len() / 2;
    kvs.remove(mid);

    let range_proof = RangeProof::new(start_proof, end_proof, kvs.into_boxed_slice());
    assert!(
        verify_range_proof(
            Some(items[start].0),
            Some(items[end - 1].0),
            &root_hash,
            &range_proof,
        )
        .is_err(),
        "gapped entries should be detected"
    );
}

#[test]
// Tests normal range proof with two non-existent proofs.
// The test cases are generated randomly.
fn test_range_proof_with_non_existent_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    for _ in 0..10 {
        let start = rng.random_range(1..items.len() - 2);
        let end = rng.random_range(start + 1..items.len());

        // Short circuit if the decreased key is same with the previous key
        let first = decrease_key(items[start].0);
        if start != 0 && first.as_ref() == items[start - 1].0.as_ref() {
            continue;
        }
        // Short circuit if the decreased key is underflow
        if &first > items[start].0 {
            continue;
        }
        // Short circuit if the increased key is same with the next key
        let last = increase_key(items[end - 1].0);
        if end != items.len() && last.as_ref() == items[end].0.as_ref() {
            continue;
        }
        // Short circuit if the increased key is overflow
        if &last < items[end - 1].0 {
            continue;
        }

        let start_proof = merkle.prove(&first).unwrap();
        let end_proof = merkle.prove(&last).unwrap();

        let key_values = stored_key_values(&merkle, items[start..end].iter().map(|(k, _)| *k));

        let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof).unwrap();
    }

    // Special case, two edge proofs for two edge key.
    let first = &[0; 32];
    let last = &[255; 32];

    let start_proof = merkle.prove(first).unwrap();
    let end_proof = merkle.prove(last).unwrap();

    let key_values = stored_key_values(&merkle, items.iter().map(|(k, _)| *k));

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    verify_range_proof(Some(first), Some(last), &root_hash, &range_proof).unwrap();
}

#[test]
// Tests such scenarios:
// - There exists a gap between the first element and the left edge proof
// - There exists a gap between the last element and the right edge proof
// Detecting gaps requires full trie reconstruction, not yet implemented.
fn test_range_proof_with_invalid_non_existent_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    // Case 1
    let mut start = 100;
    let mut end = 200;
    let first = decrease_key(items[start].0);

    let start_proof = merkle.prove(&first).unwrap();
    let end_proof = merkle.prove(items[end - 1].0).unwrap();

    start = 105; // Gap created
    let key_values: KeyValuePairs = items[start..end]
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    assert!(
        verify_range_proof(
            Some(&first),
            Some(items[end - 1].0),
            &root_hash,
            &range_proof,
        )
        .is_err()
    );

    // Case 2
    start = 100;
    end = 200;
    let last = increase_key(items[end - 1].0);

    let start_proof_2 = merkle.prove(items[start].0).unwrap();
    let end_proof_2 = merkle.prove(&last).unwrap();

    end = 195; // Capped slice
    let key_values_2: KeyValuePairs = items[start..end]
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof_2 =
        RangeProof::new(start_proof_2, end_proof_2, key_values_2.into_boxed_slice());

    let root_hash_2 = merkle.nodestore().root_hash().unwrap();

    assert!(
        verify_range_proof(
            Some(items[start].0),
            Some(&last),
            &root_hash_2,
            &range_proof_2,
        )
        .is_err()
    );
}

#[test]
// Tests the proof with only one element. The first edge proof can be existent one or
// non-existent one.
fn test_one_element_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    // One element with existent edge proof, both edge proofs
    // point to the SAME key.
    let start = 1000;
    let proof = merkle.prove(items[start].0).unwrap();
    assert!(!proof.is_empty());

    let key_values = stored_key_values(&merkle, std::iter::once(*items[start].0));

    let range_proof = RangeProof::new(
        proof.clone(), // Same proof for start and end
        proof,
        key_values.into(),
    );

    let root_hash = merkle.nodestore().root_hash().unwrap();

    verify_range_proof(
        Some(items[start].0),
        Some(items[start].0),
        &root_hash,
        &range_proof,
    )
    .unwrap();

    // One element with left non-existent edge proof
    let first = decrease_key(items[start].0);
    let start_proof_2 = merkle.prove(&first).unwrap();
    let end_proof_2 = merkle.prove(items[start].0).unwrap();

    let key_values_2 = stored_key_values(&merkle, std::iter::once(*items[start].0));

    let range_proof_2 = RangeProof::new(start_proof_2, end_proof_2, key_values_2.into());

    verify_range_proof(
        Some(&first),
        Some(items[start].0),
        &root_hash,
        &range_proof_2,
    )
    .unwrap();

    // One element with right non-existent edge proof
    let last = increase_key(items[start].0);
    let start_proof_3 = merkle.prove(items[start].0).unwrap();
    let end_proof_3 = merkle.prove(&last).unwrap();

    let key_values_3 = stored_key_values(&merkle, std::iter::once(*items[start].0));

    let range_proof_3 = RangeProof::new(start_proof_3, end_proof_3, key_values_3.into());

    verify_range_proof(
        Some(items[start].0),
        Some(&last),
        &root_hash,
        &range_proof_3,
    )
    .unwrap();

    // One element with two non-existent edge proofs
    let start_proof_4 = merkle.prove(&first).unwrap();
    let end_proof_4 = merkle.prove(&last).unwrap();

    let key_values_4 = stored_key_values(&merkle, std::iter::once(*items[start].0));

    let range_proof_4 = RangeProof::new(start_proof_4, end_proof_4, key_values_4.into());

    verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof_4).unwrap();

    // Test the mini trie with only a single element.
    let key = rng.random::<[u8; 32]>();
    let val = rng.random::<[u8; 20]>();
    let merkle_mini = init_merkle(vec![(key, val)]);

    let first = &[0; 32];
    let start_proof_5 = merkle_mini.prove(first).unwrap();
    let end_proof_5 = merkle_mini.prove(&key).unwrap();

    let key_values_5 = stored_key_values(&merkle_mini, std::iter::once(key));

    let range_proof_5 = RangeProof::new(start_proof_5, end_proof_5, key_values_5.into());

    let root_hash_mini = merkle_mini.nodestore().root_hash().unwrap();

    verify_range_proof(Some(first), Some(&key), &root_hash_mini, &range_proof_5).unwrap();
}

#[test]
// Tests the range proof with all elements.
// The edge proofs can be nil.
fn test_all_elements_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    // With edge proofs, all elements proof should work.
    let start = 0;
    let end = &items.len() - 1;
    let start_proof_2 = merkle.prove(items[start].0).unwrap();
    let end_proof_2 = merkle.prove(items[end].0).unwrap();

    let key_values_2 = stored_key_values(&merkle, items.iter().map(|(k, _)| *k));

    let range_proof_2 =
        RangeProof::new(start_proof_2, end_proof_2, key_values_2.into_boxed_slice());

    verify_range_proof(
        Some(items[start].0),
        Some(items[end].0),
        &root_hash,
        &range_proof_2,
    )
    .unwrap();

    // Even with non-existent edge proofs, it should still work.
    let first = &[0; 32];
    let last = &[255; 32];
    let start_proof_3 = merkle.prove(first).unwrap();
    let end_proof_3 = merkle.prove(last).unwrap();

    let key_values_3 = stored_key_values(&merkle, items.iter().map(|(k, _)| *k));

    let range_proof_3 =
        RangeProof::new(start_proof_3, end_proof_3, key_values_3.into_boxed_slice());

    verify_range_proof(Some(first), Some(last), &root_hash, &range_proof_3).unwrap();
}

#[test]
// Empty key_values with a non-existent boundary key past the last entry.
fn test_empty_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let first = increase_key(items[items.len() - 1].0);
    let proof = merkle.prove(&first).unwrap();
    assert!(!proof.is_empty());

    let key_values: KeyValuePairs = Vec::new();
    let range_proof = RangeProof::new(proof.clone(), proof, key_values.into_boxed_slice());

    verify_range_proof(Some(&first), Some(&first), &root_hash, &range_proof).unwrap();
}

#[test]
// Focuses on the small trie with embedded nodes. If the gapped
// node is embedded in the trie, it should be detected too.
fn test_gapped_range_proof() {
    let mut items = Vec::new();
    // Sorted entries
    for i in 0..10_u32 {
        let mut key = [0; 32];
        for (index, d) in i.to_be_bytes().iter().enumerate() {
            key[index] = *d;
        }
        items.push((key, i.to_be_bytes()));
    }
    let merkle = init_merkle(items.clone());

    let first = 2;
    let last = 8;
    let start_proof = merkle.prove(&items[first].0).unwrap();
    let end_proof = merkle.prove(&items[last - 1].0).unwrap();

    let middle = usize::midpoint(first, last) - first;
    let key_values: KeyValuePairs = items[first..last]
        .iter()
        .enumerate()
        .filter(|(pos, _)| *pos != middle)
        .map(|(_, item)| {
            (
                item.0.to_vec().into_boxed_slice(),
                item.1.to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    assert!(
        verify_range_proof(
            Some(&items[first].0),
            Some(&items[last - 1].0),
            &root_hash,
            &range_proof,
        )
        .is_err(),
        "gapped entries should be detected in small trie"
    );
}

#[test]
// Tests the element is not in the range covered by proofs.
fn test_same_side_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    let pos = 1000;
    let mut last = decrease_key(items[pos].0);
    let mut first = last;
    first = decrease_key(&first);

    let start_proof = merkle.prove(&first).unwrap();
    let end_proof = merkle.prove(&last).unwrap();

    let key_values = vec![(
        items[pos].0.to_vec().into_boxed_slice(),
        items[pos].1.to_vec().into_boxed_slice(),
    )];

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    assert!(verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof,).is_err());

    first = increase_key(items[pos].0);
    last = first;
    last = increase_key(&last);

    let start_proof_2 = merkle.prove(&first).unwrap();
    let end_proof_2 = merkle.prove(&last).unwrap();

    let key_values_2 = vec![(
        items[pos].0.to_vec().into_boxed_slice(),
        items[pos].1.to_vec().into_boxed_slice(),
    )];

    let range_proof_2 =
        RangeProof::new(start_proof_2, end_proof_2, key_values_2.into_boxed_slice());

    assert!(verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof_2,).is_err());
}

#[test]
// Tests the range starts from zero.
fn test_single_side_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    for _ in 0..10 {
        let mut set = HashMap::new();
        for _ in 0..4096_u32 {
            let key = rng.random::<[u8; 32]>();
            let val = rng.random::<[u8; 20]>();
            set.insert(key, val);
        }
        let mut items = set.iter().collect::<Vec<_>>();
        items.sort_unstable();
        let merkle = init_merkle(items.clone());

        let cases = vec![0, 1, 100, 1000, items.len() - 1];
        for case in cases {
            let start = &[0; 32];
            let start_proof = merkle.prove(start).unwrap();
            let end_proof = merkle.prove(items[case].0).unwrap();

            let key_values =
                stored_key_values(&merkle, items.iter().take(case + 1).map(|(k, _)| *k));

            let range_proof =
                RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

            let root_hash = merkle.nodestore().root_hash().unwrap();

            verify_range_proof(Some(start), Some(items[case].0), &root_hash, &range_proof).unwrap();
        }
    }
}

#[test]
// Tests the range ends with 0xffff...fff.
fn test_reverse_single_side_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    for _ in 0..10 {
        let mut set = HashMap::new();
        for _ in 0..1024_u32 {
            let key = rng.random::<[u8; 32]>();
            let val = rng.random::<[u8; 20]>();
            set.insert(key, val);
        }
        let mut items = set.iter().collect::<Vec<_>>();
        items.sort_unstable();
        let merkle = init_merkle(items.clone());

        let cases = vec![0, 1, 100, 1000, items.len() - 1];
        for case in cases {
            let end = &[255; 32];

            let start_proof = merkle.prove(items[case].0).unwrap();
            let end_proof = merkle.prove(end).unwrap();

            let key_values = stored_key_values(&merkle, items.iter().skip(case).map(|(k, _)| *k));

            let range_proof =
                RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

            let root_hash = merkle.nodestore().root_hash().unwrap();

            verify_range_proof(Some(items[case].0), Some(end), &root_hash, &range_proof).unwrap();
        }
    }
}

#[test]
// Tests the range starts with zero and ends with 0xffff...fff.
fn test_both_sides_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    for _ in 0..10 {
        let mut set = HashMap::new();
        for _ in 0..4096_u32 {
            let key = rng.random::<[u8; 32]>();
            let val = rng.random::<[u8; 20]>();
            set.insert(key, val);
        }
        let mut items = set.iter().collect::<Vec<_>>();
        items.sort_unstable();
        let merkle = init_merkle(items.clone());

        let start = &[0; 32];
        let end = &[255; 32];
        let start_proof = merkle.prove(start).unwrap();
        let end_proof = merkle.prove(end).unwrap();

        let key_values = stored_key_values(&merkle, items.iter().map(|(k, _)| *k));

        let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        verify_range_proof(Some(start), Some(end), &root_hash, &range_proof).unwrap();
    }
}

#[test]
// Tests normal range proof with both edge proofs
// as the existent proof, but with an extra empty value included, which is a
// noop technically, but practically should be rejected.
fn test_empty_value_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 512);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    // Create a new entry with a slightly modified key
    let mid_index = items.len() / 2;
    let key = increase_key(items[mid_index - 1].0);
    let empty_value: [u8; 20] = [0; 20];
    items.splice(mid_index..mid_index, [(&key, &empty_value)].iter().copied());

    let start = 1;
    let end = items.len() - 1;

    let start_proof = merkle.prove(items[start].0).unwrap();
    let end_proof = merkle.prove(items[end - 1].0).unwrap();

    let key_values: KeyValuePairs = items
        .iter()
        .skip(start)
        .take(end - start)
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    assert!(
        verify_range_proof(
            Some(items[start].0),
            Some(items[end - 1].0),
            &root_hash,
            &range_proof,
        )
        .is_err()
    );
}

#[test]
// Tests the range proof with all elements,
// but with an extra empty value included, which is a noop technically, but
// practically should be rejected.
fn test_all_elements_empty_value_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 512);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    // Create a new entry with a slightly modified key
    let mid_index = items.len() / 2;
    let key = increase_key(items[mid_index - 1].0);
    let empty_value: [u8; 20] = [0; 20];
    items.splice(mid_index..mid_index, [(&key, &empty_value)].iter().copied());

    let start = 0;
    let end = items.len() - 1;

    let start_proof = merkle.prove(items[start].0).unwrap();
    let end_proof = merkle.prove(items[end].0).unwrap();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    assert!(
        verify_range_proof(
            Some(items[start].0),
            Some(items[end].0),
            &root_hash,
            &range_proof,
        )
        .is_err()
    );
}

#[test]
fn test_range_proof_keys_with_shared_prefix() {
    let items = vec![
        (
            hex::decode("aa10000000000000000000000000000000000000000000000000000000000000")
                .expect("Decoding failed"),
            hex::decode("02").expect("Decoding failed"),
        ),
        (
            hex::decode("aa20000000000000000000000000000000000000000000000000000000000000")
                .expect("Decoding failed"),
            hex::decode("03").expect("Decoding failed"),
        ),
    ];
    let merkle = init_merkle(items.clone());

    let start = hex::decode("0000000000000000000000000000000000000000000000000000000000000000")
        .expect("Decoding failed");
    let end = hex::decode("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
        .expect("Decoding failed");

    let start_proof = merkle.prove(&start).unwrap();
    let end_proof = merkle.prove(&end).unwrap();

    let key_values = stored_key_values(&merkle, items.iter().map(|(k, _)| k.as_slice()));

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    verify_range_proof(Some(&start), Some(&end), &root_hash, &range_proof).unwrap();
}

#[test]
// Tests a malicious proof, where the proof is more or less the
// whole trie. This is to match corresponding test in geth.
fn test_bloated_range_proof() {
    // Use a small trie
    let mut items = Vec::new();
    for i in 0..100_u32 {
        let mut key: [u8; 32] = [0; 32];
        let mut value: [u8; 20] = [0; 20];
        for (index, d) in i.to_be_bytes().iter().enumerate() {
            key[index] = *d;
            value[index] = *d;
        }
        items.push((key, value));
    }
    let merkle = init_merkle(items.clone());

    // In the 'malicious' case, we add proofs for every single item
    // (but only one key/value pair used as leaf)
    let mut proof = Proof::empty().into_mutable();
    for item in &items {
        let cur_proof = merkle.prove(&item.0).unwrap();
        assert!(!cur_proof.is_empty());
        proof.extend(cur_proof);
    }

    let target = &items[50];

    // Create start and end proofs (same key in this case since only one key-value pair)
    let start_proof = merkle.prove(&target.0).unwrap();
    let end_proof = merkle.prove(&target.0).unwrap();

    let key_values = stored_key_values(&merkle, std::iter::once(target.0));

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    verify_range_proof(Some(&target.0), Some(&target.0), &root_hash, &range_proof).unwrap();
}

#[test]
// Rejects a range proof containing keys below the requested start key.
fn test_bad_range_proof_key_below_start() {
    let items = [("bb", "v1"), ("cc", "v2"), ("dd", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"cc").unwrap();
    let end_proof = merkle.prove(b"dd").unwrap();

    // Include "bb" which is below the claimed start key "cc"
    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"cc".as_slice()),
        Some(b"dd".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        matches!(
            result,
            Err(crate::api::Error::ProofError(ProofError::KeyOutsideRange))
        ),
        "expected KeyOutsideRange, got {result:?}"
    );
}

#[test]
// Rejects a range proof containing keys above the requested end key.
fn test_bad_range_proof_key_above_end() {
    let items = [("bb", "v1"), ("cc", "v2"), ("dd", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"bb").unwrap();
    let end_proof = merkle.prove(b"cc").unwrap();

    // Include "dd" which is above the claimed end key "cc"
    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"bb".as_slice()),
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        matches!(
            result,
            Err(crate::api::Error::ProofError(ProofError::KeyOutsideRange))
        ),
        "expected KeyOutsideRange, got {result:?}"
    );
}

#[test]
// Rejects a range proof with key-value pairs but no end proof.
fn test_bad_range_proof_missing_end_proof() {
    let items = [("bb", "v1"), ("cc", "v2")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"bb").unwrap();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let empty_end: Proof<Box<[ProofNode]>> = Proof::new(Vec::new().into_boxed_slice());
    let range_proof = RangeProof::new(start_proof, empty_end, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"bb".as_slice()),
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        matches!(
            result,
            Err(crate::api::Error::ProofError(ProofError::NoEndProof))
        ),
        "expected NoEndProof, got {result:?}"
    );
}

#[test]
// Rejects a start proof when no start key is specified.
fn test_bad_range_proof_unexpected_start_proof() {
    let items = [("bb", "v1"), ("cc", "v2")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"bb").unwrap();
    let end_proof = merkle.prove(b"cc").unwrap();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    // Pass None as start key but provide a start proof — should be rejected
    let result = verify_range_proof(
        None::<&[u8]>,
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        matches!(
            result,
            Err(crate::api::Error::ProofError(
                ProofError::UnexpectedStartProof
            ))
        ),
        "expected UnexpectedStartProof, got {result:?}"
    );
}
#[test]
// Fuzz-style test that generates random data, creates range proofs for random
// subranges, and verifies them standalone using only the root hash.
// Covers: both-existent edges, left-nonexistent, right-nonexistent,
// both-nonexistent, single-element, and full-range scenarios.
fn test_range_proof_fuzz() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    // 64 is big enough to provide a signal; 2048 starts to slow things down
    let num_keys = rng.random_range(64..=2048u32);
    let set = fixed_and_pseudorandom_data(&rng, num_keys);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    for _ in 0..50 {
        let scenario = rng.random_range(0..5u8);
        match scenario {
            // Both edges are existing keys
            0 => {
                let start = rng.random_range(0..items.len() - 1);
                let end = rng.random_range((start + 1)..items.len());
                let range_proof = merkle
                    .range_proof(Some(items[start].0), Some(items[end].0), None)
                    .unwrap();
                verify_range_proof(
                    Some(items[start].0),
                    Some(items[end].0),
                    &root_hash,
                    &range_proof,
                )
                .unwrap();
            }
            // Left edge is non-existent
            1 => {
                let start = rng.random_range(1..items.len() - 1);
                let end = rng.random_range((start + 1)..items.len());
                let first = decrease_key(items[start].0);
                if &first >= items[start].0 || (start > 0 && first.as_ref() == items[start - 1].0) {
                    continue;
                }
                let range_proof = merkle
                    .range_proof(Some(&first), Some(items[end].0), None)
                    .unwrap();
                verify_range_proof(Some(&first), Some(items[end].0), &root_hash, &range_proof)
                    .unwrap();
            }
            // Right edge is non-existent
            2 => {
                let start = rng.random_range(0..items.len() - 1);
                let end = rng.random_range(start..items.len() - 1);
                let last = increase_key(items[end].0);
                if &last <= items[end].0
                    || (end + 1 < items.len() && last.as_ref() == items[end + 1].0)
                {
                    continue;
                }
                let range_proof = merkle
                    .range_proof(Some(items[start].0), Some(&last), None)
                    .unwrap();
                verify_range_proof(Some(items[start].0), Some(&last), &root_hash, &range_proof)
                    .unwrap();
            }
            // Both edges are non-existent
            3 => {
                let start = rng.random_range(1..items.len() - 1);
                let end = rng.random_range(start..items.len() - 1);
                let first = decrease_key(items[start].0);
                let last = increase_key(items[end].0);
                if &first >= items[start].0
                    || &last <= items[end].0
                    || (start > 0 && first.as_ref() == items[start - 1].0)
                    || (end + 1 < items.len() && last.as_ref() == items[end + 1].0)
                {
                    continue;
                }
                let range_proof = merkle.range_proof(Some(&first), Some(&last), None).unwrap();
                verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof).unwrap();
            }
            // Single element
            4 => {
                let idx = rng.random_range(0..items.len());
                let range_proof = merkle
                    .range_proof(Some(items[idx].0), Some(items[idx].0), None)
                    .unwrap();
                verify_range_proof(
                    Some(items[idx].0),
                    Some(items[idx].0),
                    &root_hash,
                    &range_proof,
                )
                .unwrap();
            }
            _ => unreachable!(),
        }
    }
}

#[test]
// Rejects a range proof where the end proof has been truncated (missing last node).
fn test_bad_range_proof_truncated_end_proof() {
    let items = [("aa", "v1"), ("bb", "v2"), ("cc", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"aa").unwrap();
    let end_proof = merkle.prove(b"cc").unwrap();

    // Truncate the end proof by removing the last node
    let mut truncated_end = end_proof.into_mutable();
    truncated_end.pop();
    let truncated_end = truncated_end.into_immutable();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, truncated_end, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"aa".as_slice()),
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(result.is_err(), "truncated end proof should be rejected");
}

#[test]
// Rejects a range proof where the start proof has been truncated (missing last node).
fn test_bad_range_proof_truncated_start_proof() {
    let items = [("aa", "v1"), ("bb", "v2"), ("cc", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"aa").unwrap();
    let end_proof = merkle.prove(b"cc").unwrap();

    // Truncate the start proof by removing the last node
    let mut truncated_start = start_proof.into_mutable();
    truncated_start.pop();
    let truncated_start = truncated_start.into_immutable();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(truncated_start, end_proof, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"aa".as_slice()),
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(result.is_err(), "truncated start proof should be rejected");
}

#[test]
// Rejects a range proof containing a proof node with a value at an odd nibble length.
// The odd-nibble-with-value check is defense-in-depth: in practice, corrupting a proof
// node to have an odd-nibble key breaks its hash, so UnexpectedHash is returned first.
// This test verifies the corruption is detected regardless of which check fires.
fn test_bad_range_proof_value_at_odd_nibble() {
    let items = [("aa", "v1"), ("bb", "v2"), ("cc", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"aa").unwrap();
    let end_proof = merkle.prove(b"cc").unwrap();

    // Corrupt the end proof: set a value on a node with an odd-length key.
    let mut corrupt_end = end_proof.into_mutable();
    if let Some(node) = corrupt_end.last_mut() {
        // Extend the key by one nibble to make it odd length
        node.key.push(firewood_storage::PathComponent::ALL[0]);
        node.value_digest = Some(ValueDigest::Value(b"bad".to_vec().into()));
    }
    let corrupt_end = corrupt_end.into_immutable();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, corrupt_end, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"aa".as_slice()),
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        result.is_err(),
        "proof with value at odd nibble length should be rejected"
    );
}

// Tampering an edge proof's terminal value node corrupts its hash relative to
// the hash its parent commits to, so `verify_range_proof` rejects it with
// `EdgeProofHashMismatch`. These two tests pin that the annotation names the
// edge that actually tripped: corrupting the start proof reports `Left`, the
// end proof reports `Right`. Without them, swapping `ProofEdge::{Left,Right}`
// at the `verify_edge` call sites would go unnoticed.
#[test]
fn test_range_proof_left_edge_hash_mismatch() {
    let items = [("aa", "v1"), ("bb", "v2"), ("cc", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let end_proof = merkle.prove(b"cc").unwrap();

    // Corrupt only the start (left) edge proof's terminal node.
    let mut corrupt_start = merkle.prove(b"aa").unwrap().into_mutable();
    if let Some(node) = corrupt_start.last_mut() {
        node.value_digest = Some(ValueDigest::Value(b"tampered".to_vec().into()));
    }
    let corrupt_start = corrupt_start.into_immutable();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(corrupt_start, end_proof, key_values.into_boxed_slice());

    let err = verify_range_proof(
        Some(b"aa".as_slice()),
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    )
    .unwrap_err();
    assert!(
        matches!(
            err,
            api::Error::ProofError(crate::ProofError::EdgeProofHashMismatch {
                edge: crate::ProofEdge::Left,
                ..
            })
        ),
        "expected Left EdgeProofHashMismatch, got: {err:?}"
    );
}

#[test]
fn test_range_proof_right_edge_hash_mismatch() {
    let items = [("aa", "v1"), ("bb", "v2"), ("cc", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let start_proof = merkle.prove(b"aa").unwrap();

    // Corrupt only the end (right) edge proof's terminal node; the start proof
    // stays honest so the left edge passes and the failure lands on the right.
    let mut corrupt_end = merkle.prove(b"cc").unwrap().into_mutable();
    if let Some(node) = corrupt_end.last_mut() {
        node.value_digest = Some(ValueDigest::Value(b"tampered".to_vec().into()));
    }
    let corrupt_end = corrupt_end.into_immutable();

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| {
            (
                k.as_bytes().to_vec().into_boxed_slice(),
                v.as_bytes().to_vec().into_boxed_slice(),
            )
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, corrupt_end, key_values.into_boxed_slice());

    let err = verify_range_proof(
        Some(b"aa".as_slice()),
        Some(b"cc".as_slice()),
        &root_hash,
        &range_proof,
    )
    .unwrap_err();
    assert!(
        matches!(
            err,
            api::Error::ProofError(crate::ProofError::EdgeProofHashMismatch {
                edge: crate::ProofEdge::Right,
                ..
            })
        ),
        "expected Right EdgeProofHashMismatch, got: {err:?}"
    );
}

#[test]
// Rejects a range proof that hides a key-value pair on an edge proof path by
// omitting it from key_values.
fn test_bad_range_proof_hidden_value_on_edge_path() {
    // Create a trie where "b" is an intermediate branch on the path to "ba" and "bc".
    // "b" has a value AND is on the edge proof path for "bc".
    // The range ["a", "bc"] should include "b" and "ba", but we omit "b" from key_values.
    let items = [("b", "v1"), ("ba", "v2"), ("bc", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // Use a non-existent start key so the start proof is an exclusion proof
    let start_proof = merkle.prove(b"a").unwrap();
    let end_proof = merkle.prove(b"bc").unwrap();

    // Include "ba" and "bc" but omit "b" — "b" is on the end proof path
    let key_values: KeyValuePairs = vec![
        (
            b"ba".to_vec().into_boxed_slice(),
            b"v2".to_vec().into_boxed_slice(),
        ),
        (
            b"bc".to_vec().into_boxed_slice(),
            b"v3".to_vec().into_boxed_slice(),
        ),
    ];

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"a".as_slice()),
        Some(b"bc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        matches!(
            result,
            Err(crate::api::Error::ProofError(
                ProofError::ProofNodeHasUnincludedValue
            ))
        ),
        "expected ProofNodeHasUnincludedValue, got {result:?}"
    );
}

#[test]
// Rejects a range proof with a non-existent edge proof that has its final node removed.
// This is the subtle case: since the bound doesn't match any key in key_values,
// verify_edge passes the truncated proof as an exclusion proof (expected_value=None).
// The corruption must be caught by the final root hash check.
fn test_bad_range_proof_truncated_non_existent_edge() {
    let items: Vec<([u8; 32], [u8; 32])> = (0..10_u32)
        .map(|i| {
            let mut key = [0u8; 32];
            let mut value = [0u8; 32];
            #[expect(
                clippy::disallowed_methods,
                reason = "to_be_bytes() returns [u8; 4], matching [..4]"
            )]
            key[..4].copy_from_slice(&i.to_be_bytes());
            #[expect(
                clippy::disallowed_methods,
                reason = "to_be_bytes() returns [u8; 4], matching [..4]"
            )]
            value[..4].copy_from_slice(&(i + 100).to_be_bytes());
            (key, value)
        })
        .collect();

    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // Use a non-existent start key (before the first item)
    let start_bound = {
        let mut k = items[2].0;
        k[31] = k[31].wrapping_sub(1);
        k
    };
    let end_bound = items[7].0;

    let start_proof = merkle.prove(&start_bound).unwrap();
    let end_proof = merkle.prove(&end_bound).unwrap();

    // Truncate the start proof (non-existent edge)
    let mut truncated_start = start_proof.into_mutable();
    truncated_start.pop();
    let truncated_start = truncated_start.into_immutable();

    let key_values: KeyValuePairs = items[2..=7]
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(truncated_start, end_proof, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(start_bound.as_slice()),
        Some(end_bound.as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        result.is_err(),
        "truncated non-existent edge proof should be rejected"
    );
}

#[test]
// Rejects a range proof when start key > end key during verification.
fn test_bad_range_proof_start_after_end() {
    let items = [("bb", "v1"), ("cc", "v2"), ("dd", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let range_proof = merkle
        .range_proof(Some(b"bb".as_slice()), Some(b"dd".as_slice()), None)
        .unwrap();

    // Verify with start > end
    let result = verify_range_proof(
        Some(b"dd".as_slice()),
        Some(b"bb".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        matches!(
            result,
            Err(crate::api::Error::ProofError(ProofError::StartAfterEnd))
        ),
        "expected StartAfterEnd, got {result:?}"
    );
}

#[test]
// Tests range_proof() with a limit parameter. When limit truncates, the
// end proof should anchor at the last returned key, not the requested end_key.
fn test_range_proof_with_limit() {
    let items: Vec<([u8; 32], [u8; 20])> = (0..10u32)
        .map(|i| {
            let mut key = [0u8; 32];
            let mut value = [0u8; 20];
            #[expect(
                clippy::disallowed_methods,
                reason = "to_be_bytes() returns [u8; 4], matching [..4]"
            )]
            key[..4].copy_from_slice(&i.to_be_bytes());
            #[expect(
                clippy::disallowed_methods,
                reason = "to_be_bytes() returns [u8; 4], matching [..4]"
            )]
            value[..4].copy_from_slice(&(i + 100).to_be_bytes());
            (key, value)
        })
        .collect();

    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // Request range [items[0], items[9]] with limit=3. Should return items[0..3].
    let limit = NonZeroUsize::new(3).unwrap();
    let range_proof = merkle
        .range_proof(Some(&items[0].0), Some(&items[9].0), Some(limit))
        .unwrap();

    assert_eq!(range_proof.key_values().len(), 3);

    // The end proof should anchor at items[2] (last returned), not items[9].
    // Verify the truncated proof against the last returned key.
    verify_range_proof(
        Some(&items[0].0),
        Some(&items[2].0),
        &root_hash,
        &range_proof,
    )
    .unwrap();

    // Full range (no limit) should return all 10.
    let full_proof = merkle
        .range_proof(Some(&items[0].0), Some(&items[9].0), None)
        .unwrap();
    assert_eq!(full_proof.key_values().len(), 10);

    verify_range_proof(
        Some(&items[0].0),
        Some(&items[9].0),
        &root_hash,
        &full_proof,
    )
    .unwrap();
}

#[test]
// Demonstrates that verify_range_proof does not verify that proof node values
// match the corresponding values in key_values. A malicious prover can supply
// correct proof nodes (with the real value) but incorrect key_values at an
// intermediate proof-path position. Reconciliation silently overwrites the
// wrong value with the proof's correct one, the hash check passes, and the
// caller trusts the wrong key_values.
//
// Trie: ("b", "v1"), ("ba", "v2"), ("bc", "v3")
// Range: ["a", "bc"]
// The end proof for "bc" traverses the "b" node (which carries "v1").
// We change "b"'s value in key_values to "WRONG" — verification should fail
// but currently passes.
fn test_bad_range_proof_value_mismatch_on_proof_path() {
    let items = [("b", "v1"), ("ba", "v2"), ("bc", "v3")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // "a" is not in the trie → exclusion proof for start
    let start_proof = merkle.prove(b"a").unwrap();
    // "bc" is in the trie → inclusion proof for end
    let end_proof = merkle.prove(b"bc").unwrap();

    // Tampered key_values: "b" has the wrong value, others are correct
    let key_values: KeyValuePairs = vec![
        (
            b"b".to_vec().into_boxed_slice(),
            b"WRONG".to_vec().into_boxed_slice(), // should be "v1"
        ),
        (
            b"ba".to_vec().into_boxed_slice(),
            b"v2".to_vec().into_boxed_slice(),
        ),
        (
            b"bc".to_vec().into_boxed_slice(),
            b"v3".to_vec().into_boxed_slice(),
        ),
    ];

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let result = verify_range_proof(
        Some(b"a".as_slice()),
        Some(b"bc".as_slice()),
        &root_hash,
        &range_proof,
    );
    assert!(
        matches!(
            result,
            Err(crate::api::Error::ProofError(
                ProofError::ProofNodeValueMismatch
            ))
        ),
        "expected ProofNodeValueMismatch, got {result:?}"
    );
}

/// Create and validate a range proof with no left edge and an `end_key` at
/// a branch node with children. Keys are \x10, \x20, and \x20\xab. Proof is
/// generated from None to \x20. Children of the \x20 node should not be
/// included in the proof.
#[test]
fn test_right_edge_boundary_prefix_of_terminal() {
    let merkle = init_merkle([
        (b"\x10" as &[u8], b"a"),
        (b"\x20", b"b"),
        (b"\x20\xab", b"c"),
    ]);
    let root_hash = merkle.nodestore().root_hash().unwrap();
    let proof = merkle.range_proof(None, Some(b"\x20"), None).unwrap();
    // Proof should only have 2 keys.
    assert_eq!(proof.key_values().len(), 2);
    // Child of \x20 not in proof.
    assert!(
        !proof
            .key_values()
            .iter()
            .any(|(k, _)| k.as_ref() == b"\x20\xab"),
        "child of end key should not be included in the proof"
    );
    // End proof should only have 2 proof nodes.
    assert_eq!(proof.end_proof().len(), 2);
    verify_range_proof(None::<&[u8]>, Some(b"\x20"), &root_hash, &proof).unwrap();
}

/// Regression test: range proof verification must handle `ValueDigest::Hash`
/// correctly. In merkledb mode (non-ethhash), values >= 32 bytes are stored
/// as hashes in serialized proof nodes. When a branch node has both a value
/// and children (prefix key), the deserialized proof node carries Hash
/// instead of Value. The reconcile step must not clear the branch value
/// when the hash matches.
///
/// Setup: \x10 is a prefix of \x10\x20, making \x10 a branch with a value
/// AND children. The 32-byte value at \x10 triggers `ValueDigest::Hash` after
/// serialization round-trip.
#[cfg(not(feature = "ethhash"))]
#[test]
fn test_range_proof_with_hashed_value() {
    // Value >= 32 bytes triggers ValueDigest::Hash in merkledb mode
    let big_value = vec![0xab_u8; 32];
    let merkle = init_merkle([
        (b"\x10" as &[u8], big_value.as_slice()),
        (b"\x10\x20", b"child"),
        (b"\x30", b"other"),
    ]);
    let root_hash = merkle.nodestore().root_hash().unwrap();
    let proof = merkle
        .range_proof(Some(b"\x10"), Some(b"\x30"), None)
        .unwrap();

    // Serialize and deserialize to convert large values to Hash digests,
    // simulating a proof received from a peer over the network.
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    let deserialized = crate::api::FrozenRangeProof::from_slice(&serialized).unwrap();

    // Confirm the start proof node carries a Hash digest after round-trip.
    let start_node = deserialized.start_proof().as_ref().last().unwrap();
    assert!(
        matches!(
            start_node.value_digest,
            Some(firewood_storage::ValueDigest::Hash(_))
        ),
        "expected Hash digest for large value after deserialization, got {:?}",
        start_node.value_digest
    );

    // This must pass — the Hash digest matches the branch value.
    verify_range_proof(Some(b"\x10"), Some(b"\x30"), &root_hash, &deserialized).unwrap();
}

/// Regression test: empty range proof with a Hash digest at an out-of-range
/// proof node. The proving trie has no value at that position (no key-value
/// pairs inserted). The Hash proof node is out of range, so its value
/// contribution comes from the parent's proof child hash — the branch value
/// doesn't matter and reconcile should not reject.
#[cfg(not(feature = "ethhash"))]
#[test]
fn test_empty_range_proof_with_hashed_value() {
    // \x10 has a large value (>= 32 bytes), \x10\x20 makes \x10 a branch.
    // Range is past all keys — empty key-value list.
    let big_value = vec![0xab_u8; 32];
    let merkle = init_merkle([
        (b"\x10" as &[u8], big_value.as_slice()),
        (b"\x10\x20", b"child"),
    ]);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // Range starts past all keys — empty proof
    let proof = merkle
        .range_proof(Some(b"\x30"), Some(b"\x40"), None)
        .unwrap();
    assert!(proof.key_values().is_empty());

    // Serialize/deserialize to convert Value to Hash
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    let deserialized = crate::api::FrozenRangeProof::from_slice(&serialized).unwrap();

    // This must pass — the Hash proof node is out of range.
    verify_range_proof(Some(b"\x30"), Some(b"\x40"), &root_hash, &deserialized).unwrap();
}

/// Multi-level trie with hashed values at multiple branch depths.
/// Tests that both in-range and out-of-range branches with Hash digests
/// are handled correctly after serialization round-trip.
///
/// Trie structure:
///   root
///   └── "abc" (value: [0;64], Hash after serialize)      // out-of-range branch
///       ├── "123" → leaf (value: [1;64], Hash)           // in-range
///       └── "def" (value: [2;64], Hash after serialize)  // in-range branch
///           └── "123" → leaf (value: [3;64], Hash)       // in-range
///
/// Range ["abc123"..): "abc" is out-of-range (< `start_key`), its Hash comes
/// from the proof node fallback in `compute_root_hash_with_proofs`. "abcdef"
/// is in-range, its Hash matches the branch value via the fast path in
/// `reconcile_branch_proof_node`.
#[cfg(not(feature = "ethhash"))]
#[test]
fn test_multi_level_range_proof_with_hashed_values() {
    let merkle = init_merkle([
        (b"abc" as &[u8], [0; 64].as_slice()),
        (b"abc123", [1; 64].as_slice()),
        (b"abcdef", [2; 64].as_slice()),
        (b"abcdef123", [3; 64].as_slice()),
    ]);
    let root_hash = merkle.nodestore().root_hash().unwrap();
    let proof = merkle
        .range_proof(Some(b"abc123"), Some(b"\xff"), None)
        .unwrap();

    // All 3 in-range keys should be in the proof
    assert_eq!(proof.key_values().len(), 3);

    // Serialize/deserialize to convert large values to Hash digests
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    let deserialized = crate::api::FrozenRangeProof::from_slice(&serialized).unwrap();

    // Confirm all proof nodes with values carry Hash, not Value (all values >= 32 bytes).
    for proof_node in deserialized
        .start_proof()
        .as_ref()
        .iter()
        .chain(deserialized.end_proof().as_ref())
    {
        if let Some(digest) = &proof_node.value_digest {
            assert!(
                matches!(digest, firewood_storage::ValueDigest::Hash(_)),
                "expected Hash digest after deserialization, got Value at key {:?}",
                proof_node.key
            );
        }
    }

    // This exercises:
    // - "abc" out-of-range: Hash fallback in compute_root_hash_with_proofs
    // - "abcdef" in-range: Hash fast path in reconcile_branch_proof_node
    verify_range_proof(Some(b"abc123"), Some(b"\xff"), &root_hash, &deserialized).unwrap();
}
