// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::*;
use crate::range_proof::RangeProof;

type KeyValuePairs = Vec<(Box<[u8]>, Box<[u8]>)>;

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn range_proof_invalid_bounds() {
    let merkle = create_in_memory_merkle().hash();

    let start_key = &[0x01];
    let end_key = &[0x00];

    match merkle.range_proof(Some(start_key), Some(end_key), NonZeroUsize::new(1)) {
        Err(api::Error::InvalidRange {
            start_key: first_key,
            end_key: last_key,
        }) if *first_key == *start_key && *last_key == *end_key => (),
        Err(api::Error::InvalidRange { .. }) => panic!("wrong bounds on InvalidRange error"),
        _ => panic!("expected InvalidRange error"),
    }
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn full_range_proof() {
    let merkle = init_merkle((u8::MIN..=u8::MAX).map(|k| ([k], [k])));

    let rangeproof = merkle.range_proof(None, None, None).unwrap();
    assert_eq!(rangeproof.key_values().len(), u8::MAX as usize + 1);
    assert_ne!(rangeproof.start_proof(), rangeproof.end_proof());
    let left_proof = merkle.prove(&[u8::MIN]).unwrap();
    let right_proof = merkle.prove(&[u8::MAX]).unwrap();
    assert_eq!(rangeproof.start_proof(), &left_proof);
    assert_eq!(rangeproof.end_proof(), &right_proof);
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn single_value_range_proof() {
    const RANDOM_KEY: u8 = 42;

    let merkle = init_merkle((u8::MIN..=u8::MAX).map(|k| ([k], [k])));

    let rangeproof = merkle
        .range_proof(Some(&[RANDOM_KEY]), None, NonZeroUsize::new(1))
        .unwrap();
    assert_eq!(rangeproof.start_proof(), rangeproof.end_proof());
    assert_eq!(rangeproof.key_values().len(), 1);
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn shared_path_proof() {
    let key1 = b"key1";
    let value1 = b"1";

    let key2 = b"key2";
    let value2 = b"2";

    let merkle = init_merkle([(key1, value1), (key2, value2)]);

    let root_hash = merkle.nodestore().root_hash().unwrap();

    let key = key1;
    let proof = merkle.prove(key).unwrap();
    proof.verify(key, Some(value1), &root_hash).unwrap();

    let key = key2;
    let proof = merkle.prove(key).unwrap();
    proof.verify(key, Some(value2), &root_hash).unwrap();
}

// this was a specific failing case
#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn shared_path_on_insert() {
    init_merkle([
        (
            &[1, 1, 46, 82, 67, 218][..],
            &[23, 252, 128, 144, 235, 202, 124, 243][..],
        ),
        (
            &[1, 0, 0, 1, 1, 0, 63, 80],
            &[99, 82, 31, 213, 180, 196, 49, 242],
        ),
        (
            &[0, 0, 0, 169, 176, 15],
            &[105, 211, 176, 51, 231, 182, 74, 207],
        ),
        (
            &[1, 0, 0, 0, 53, 57, 93],
            &[234, 139, 214, 220, 172, 38, 168, 164],
        ),
    ]);
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn overwrite_leaf() {
    let key = &[0x00];
    let val = &[1];
    let overwrite = &[2];

    let mut merkle = create_in_memory_merkle();

    merkle.insert(key, val[..].into()).unwrap();

    assert_eq!(
        merkle.get_value(key).unwrap().as_deref(),
        Some(val.as_slice())
    );

    merkle.insert(key, overwrite[..].into()).unwrap();

    assert_eq!(
        merkle.get_value(key).unwrap().as_deref(),
        Some(overwrite.as_slice())
    );
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn single_key_proof_with_one_node() {
    let key = b"key";
    let value = b"value";

    let merkle = init_merkle([(key, value)]);

    let root_hash = merkle.nodestore().root_hash().unwrap();

    let proof = merkle.prove(key).unwrap();
    proof.verify(key, Some(value), &root_hash).unwrap();
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn two_key_proof_without_shared_path() {
    let key1 = &[0x00];
    let key2 = &[0xff];

    let merkle = init_merkle([(key1, key1), (key2, key2)]);

    let root_hash = merkle.nodestore().root_hash().unwrap();

    let proof = merkle.prove(key1).unwrap();
    proof.verify(key1, Some(key1), &root_hash).unwrap();

    let proof = merkle.prove(key2).unwrap();
    proof.verify(key2, Some(key2), &root_hash).unwrap();
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();
    let set = fixed_and_pseudorandom_data(&rng, 500);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    for (key, val) in items {
        let proof = merkle.prove(key).unwrap();
        assert!(!proof.is_empty());
        proof.verify(key, Some(val), &root_hash).unwrap();
    }
}

#[test]
/// Verify the proofs that end with leaf node with the given key.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_proof_end_with_leaf() {
    let merkle = init_merkle([
        ("do", "verb"),
        ("doe", "reindeer"),
        ("dog", "puppy"),
        ("doge", "coin"),
        ("horse", "stallion"),
        ("ddd", "ok"),
    ]);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let key = b"doe";

    let proof = merkle.prove(key).unwrap();
    assert!(!proof.is_empty());

    proof.verify(key, Some(b"reindeer"), &root_hash).unwrap();
}

#[test]
/// Verify the proofs that end with branch node with the given key.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_proof_end_with_branch() {
    let items = [
        ("d", "verb"),
        ("do", "verb"),
        ("doe", "reindeer"),
        ("e", "coin"),
    ];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    let key = b"d";

    let proof = merkle.prove(key).unwrap();
    assert!(!proof.is_empty());

    proof.verify(key, Some(b"verb"), &root_hash).unwrap();
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_bad_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();
    let set = fixed_and_pseudorandom_data(&rng, 800);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());
    let root_hash = merkle.nodestore().root_hash().unwrap();

    for (key, value) in items {
        let proof = merkle.prove(key).unwrap();
        assert!(!proof.is_empty());

        // Delete an entry from the generated proofs.
        let mut new_proof = proof.into_mutable();
        new_proof.pop();

        // TODO: verify error result matches expected error
        assert!(new_proof.verify(key, Some(value), &root_hash).is_err());
    }
}

#[test]
// Tests that missing keys can also be proven. The test explicitly uses a single
// entry trie and checks for missing keys both before and after the single entry.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_missing_key_proof() {
    let items = [("k", "v")];
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    for key in ["a", "j", "l", "z"] {
        let proof = merkle.prove(key.as_ref()).unwrap();
        assert!(!proof.is_empty());
        assert!(proof.len() == 1);

        proof.verify(key, None::<&[u8]>, &root_hash).unwrap();
    }
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_empty_tree_proof() {
    let items: Vec<(&str, &str)> = Vec::new();
    let merkle = init_merkle(items);
    let key = "x".as_ref();

    let proof_err = merkle.prove(key).unwrap_err();
    assert!(matches!(proof_err, ProofError::Empty), "{proof_err:?}");
}

#[test]
// Tests normal range proof with both edge proofs as the existent proof.
// The test cases are generated randomly.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    for _ in 0..10 {
        let start = rng.random_range(0..items.len());
        let end = rng.random_range(0..items.len() - start) + start - 1;

        if end <= start {
            continue;
        }

        let start_proof = merkle.prove(items[start].0).unwrap();
        let end_proof = merkle.prove(items[end - 1].0).unwrap();

        let key_values: KeyValuePairs = items[start..end]
            .iter()
            .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
            .collect();

        let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        merkle
            .verify_range_proof(
                Some(items[start].0),
                Some(items[end - 1].0),
                &root_hash,
                &range_proof,
            )
            .unwrap();
    }
}

#[test]
// Tests a few cases which the proof is wrong.
// The prover is expected to detect the error.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_bad_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    for _ in 0..10 {
        let start = rng.random_range(0..items.len());
        let end = rng.random_range(0..items.len() - start) + start - 1;

        if end <= start {
            continue;
        }

        let _proof = merkle
            .prove(items[start].0)
            .unwrap()
            .join(merkle.prove(items[end - 1].0).unwrap());

        let mut keys: Vec<[u8; 32]> = Vec::new();
        let mut vals: Vec<[u8; 20]> = Vec::new();
        for item in &items[start..end] {
            keys.push(*item.0);
            vals.push(*item.1);
        }

        let test_case: u32 = rng.random_range(0..6);
        let index = rng.random_range(0..end - start);
        match test_case {
            0 => {
                // Modified key
                keys[index] = rng.random::<[u8; 32]>(); // In theory it can't be same
            }
            1 => {
                // Modified val
                vals[index] = rng.random::<[u8; 20]>(); // In theory it can't be same
            }
            2 => {
                // Gapped entry slice
                if index == 0 || index == end - start - 1 {
                    continue;
                }
                keys.remove(index);
                vals.remove(index);
            }
            3 => {
                // Out of order
                let index_1 = rng.random_range(0..end - start);
                let index_2 = rng.random_range(0..end - start);
                if index_1 == index_2 {
                    continue;
                }
                keys.swap(index_1, index_2);
                vals.swap(index_1, index_2);
            }
            4 => {
                // Set random key to empty, do nothing
                keys[index] = [0; 32];
            }
            5 => {
                // Set random value to nil
                vals[index] = [0; 20];
            }
            _ => unreachable!(),
        }

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
            merkle
                .verify_range_proof(
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
// Tests normal range proof with two non-existent proofs.
// The test cases are generated randomly.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_range_proof_with_non_existent_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    for _ in 0..10 {
        let start = rng.random_range(0..items.len());
        let end = rng.random_range(0..items.len() - start) + start - 1;

        if end <= start {
            continue;
        }

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

        let key_values: KeyValuePairs = items[start..end]
            .iter()
            .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
            .collect();

        let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        merkle
            .verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof)
            .unwrap();
    }

    // Special case, two edge proofs for two edge key.
    let first = &[0; 32];
    let last = &[255; 32];

    let start_proof = merkle.prove(first).unwrap();
    let end_proof = merkle.prove(last).unwrap();

    let key_values: KeyValuePairs = items
        .into_iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    merkle
        .verify_range_proof(Some(first), Some(last), &root_hash, &range_proof)
        .unwrap();
}

#[test]
// Tests such scenarios:
// - There exists a gap between the first element and the left edge proof
// - There exists a gap between the last element and the right edge proof
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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
        merkle
            .verify_range_proof(
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
        merkle
            .verify_range_proof(
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
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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

    let key_values = vec![(
        items[start].0.to_vec().into_boxed_slice(),
        items[start].1.to_vec().into_boxed_slice(),
    )];

    let range_proof = RangeProof::new(
        proof.clone(), // Same proof for start and end
        proof,
        key_values.into_boxed_slice(),
    );

    let root_hash = merkle.nodestore().root_hash().unwrap();

    merkle
        .verify_range_proof(
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

    let key_values_2 = vec![(
        items[start].0.to_vec().into_boxed_slice(),
        items[start].1.to_vec().into_boxed_slice(),
    )];

    let range_proof_2 =
        RangeProof::new(start_proof_2, end_proof_2, key_values_2.into_boxed_slice());

    merkle
        .verify_range_proof(
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

    let key_values_3 = vec![(
        items[start].0.to_vec().into_boxed_slice(),
        items[start].1.to_vec().into_boxed_slice(),
    )];

    let range_proof_3 =
        RangeProof::new(start_proof_3, end_proof_3, key_values_3.into_boxed_slice());

    merkle
        .verify_range_proof(
            Some(items[start].0),
            Some(&last),
            &root_hash,
            &range_proof_3,
        )
        .unwrap();

    // One element with two non-existent edge proofs
    let start_proof_4 = merkle.prove(&first).unwrap();
    let end_proof_4 = merkle.prove(&last).unwrap();

    let key_values_4 = vec![(
        items[start].0.to_vec().into_boxed_slice(),
        items[start].1.to_vec().into_boxed_slice(),
    )];

    let range_proof_4 =
        RangeProof::new(start_proof_4, end_proof_4, key_values_4.into_boxed_slice());

    merkle
        .verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof_4)
        .unwrap();

    // Test the mini trie with only a single element.
    let key = rng.random::<[u8; 32]>();
    let val = rng.random::<[u8; 20]>();
    let merkle_mini = init_merkle(vec![(key, val)]);

    let first = &[0; 32];
    let start_proof_5 = merkle_mini.prove(first).unwrap();
    let end_proof_5 = merkle_mini.prove(&key).unwrap();

    let key_values_5 = vec![(
        key.to_vec().into_boxed_slice(),
        val.to_vec().into_boxed_slice(),
    )];

    let range_proof_5 =
        RangeProof::new(start_proof_5, end_proof_5, key_values_5.into_boxed_slice());

    let root_hash_mini = merkle_mini.nodestore().root_hash().unwrap();

    merkle_mini
        .verify_range_proof(Some(first), Some(&key), &root_hash_mini, &range_proof_5)
        .unwrap();
}

#[test]
// Tests the range proof with all elements.
// The edge proofs can be nil.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_all_elements_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    let item_iter = items.clone().into_iter();
    let _keys: Vec<&[u8]> = item_iter.clone().map(|item| item.0.as_ref()).collect();
    let _vals: Vec<&[u8; 20]> = item_iter.map(|item| item.1).collect();

    let empty_proof = Proof::empty();
    let empty_key: [u8; 32] = [0; 32];

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(
        empty_proof.clone(),
        empty_proof,
        key_values.into_boxed_slice(),
    );

    let root_hash = merkle.nodestore().root_hash().unwrap();

    merkle
        .verify_range_proof(Some(&empty_key), Some(&empty_key), &root_hash, &range_proof)
        .unwrap();

    // With edge proofs, it should still work.
    let start = 0;
    let end = &items.len() - 1;
    let start_proof_2 = merkle.prove(items[start].0).unwrap();
    let end_proof_2 = merkle.prove(items[end].0).unwrap();

    let key_values_2: KeyValuePairs = items
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof_2 =
        RangeProof::new(start_proof_2, end_proof_2, key_values_2.into_boxed_slice());

    merkle
        .verify_range_proof(
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

    let key_values_3: KeyValuePairs = items
        .iter()
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof_3 =
        RangeProof::new(start_proof_3, end_proof_3, key_values_3.into_boxed_slice());

    merkle
        .verify_range_proof(Some(first), Some(last), &root_hash, &range_proof_3)
        .unwrap();
}

#[test]
// Tests the range proof with "no" element. The first edge proof must
// be a non-existent proof.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_empty_range_proof() {
    let rng = firewood_storage::SeededRng::from_env_or_random();

    let set = fixed_and_pseudorandom_data(&rng, 4096);
    let mut items = set.iter().collect::<Vec<_>>();
    items.sort_unstable();
    let merkle = init_merkle(items.clone());

    let cases = [(items.len() - 1, false)];
    for c in &cases {
        let first = increase_key(items[c.0].0);
        let proof = merkle.prove(&first).unwrap();
        assert!(!proof.is_empty());

        // key and value vectors are intentionally empty.
        let key_values: KeyValuePairs = Vec::new();

        let range_proof = RangeProof::new(proof.clone(), proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        if c.1 {
            assert!(
                merkle
                    .verify_range_proof(Some(&first), Some(&first), &root_hash, &range_proof,)
                    .is_err()
            );
        } else {
            merkle
                .verify_range_proof(Some(&first), Some(&first), &root_hash, &range_proof)
                .unwrap();
        }
    }
}

#[test]
// Focuses on the small trie with embedded nodes. If the gapped
// node is embedded in the trie, it should be detected too.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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
        merkle
            .verify_range_proof(
                Some(&items[0].0),
                Some(&items[items.len() - 1].0),
                &root_hash,
                &range_proof,
            )
            .is_err()
    );
}

#[test]
// Tests the element is not in the range covered by proofs.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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

    assert!(
        merkle
            .verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof,)
            .is_err()
    );

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

    assert!(
        merkle
            .verify_range_proof(Some(&first), Some(&last), &root_hash, &range_proof_2,)
            .is_err()
    );
}

#[test]
// Tests the range starts from zero.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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

            let key_values: KeyValuePairs = items
                .iter()
                .take(case + 1)
                .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
                .collect();

            let range_proof =
                RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

            let root_hash = merkle.nodestore().root_hash().unwrap();

            merkle
                .verify_range_proof(Some(start), Some(items[case].0), &root_hash, &range_proof)
                .unwrap();
        }
    }
}

#[test]
// Tests the range ends with 0xffff...fff.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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

            let key_values: KeyValuePairs = items
                .iter()
                .skip(case)
                .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
                .collect();

            let range_proof =
                RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

            let root_hash = merkle.nodestore().root_hash().unwrap();

            merkle
                .verify_range_proof(Some(items[case].0), Some(end), &root_hash, &range_proof)
                .unwrap();
        }
    }
}

#[test]
// Tests the range starts with zero and ends with 0xffff...fff.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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

        let key_values: KeyValuePairs = items
            .into_iter()
            .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
            .collect();

        let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

        let root_hash = merkle.nodestore().root_hash().unwrap();

        merkle
            .verify_range_proof(Some(start), Some(end), &root_hash, &range_proof)
            .unwrap();
    }
}

#[test]
// Tests normal range proof with both edge proofs
// as the existent proof, but with an extra empty value included, which is a
// noop technically, but practically should be rejected.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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
        merkle
            .verify_range_proof(
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
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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
        merkle
            .verify_range_proof(
                Some(items[start].0),
                Some(items[end].0),
                &root_hash,
                &range_proof,
            )
            .is_err()
    );
}

#[test]
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
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

    let key_values: KeyValuePairs = items
        .iter()
        .map(|(k, v)| (k.clone().into_boxed_slice(), v.clone().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    merkle
        .verify_range_proof(Some(&start), Some(&end), &root_hash, &range_proof)
        .unwrap();
}

#[test]
// Tests a malicious proof, where the proof is more or less the
// whole trie. This is to match corresponding test in geth.
#[ignore = "https://github.com/ava-labs/firewood/issues/738"]
fn test_bloadted_range_proof() {
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
    let mut keys = Vec::new();
    let mut vals = Vec::new();
    for (i, item) in items.iter().enumerate() {
        let cur_proof = merkle.prove(&item.0).unwrap();
        assert!(!cur_proof.is_empty());
        proof.extend(cur_proof);
        if i == 50 {
            keys.push(item.0.as_ref());
            vals.push(item.1);
        }
    }

    // Create start and end proofs (same key in this case since only one key-value pair)
    let start_proof = merkle.prove(keys[0]).unwrap();
    let end_proof = merkle.prove(keys[keys.len() - 1]).unwrap();

    // Convert to the format expected by RangeProof
    let key_values: KeyValuePairs = keys
        .iter()
        .zip(vals.iter())
        .map(|(k, v)| (k.to_vec().into_boxed_slice(), v.to_vec().into_boxed_slice()))
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values.into_boxed_slice());

    let root_hash = merkle.nodestore().root_hash().unwrap();

    merkle
        .verify_range_proof(
            Some(keys[0]),
            Some(keys[keys.len() - 1]),
            &root_hash,
            &range_proof,
        )
        .unwrap();
}
