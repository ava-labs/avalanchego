// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::{
    merkle::{Bincode, Node, Proof, ProofError},
    merkle_util::{new_merkle, DataStoreError, MerkleSetup},
    // TODO: we should not be using shale from an integration test
    shale::{cached::DynamicMem, compact::CompactSpace},
};
use rand::Rng;
use std::collections::HashMap;

type Store = CompactSpace<Node, DynamicMem>;

fn merkle_build_test<K: AsRef<[u8]> + std::cmp::Ord + Clone, V: AsRef<[u8]> + Clone>(
    items: Vec<(K, V)>,
    meta_size: u64,
    compact_size: u64,
) -> Result<MerkleSetup<Store, Bincode>, DataStoreError> {
    let mut merkle = new_merkle(meta_size, compact_size);

    for (k, v) in items.iter() {
        merkle.insert(k, v.as_ref().to_vec())?;
    }

    Ok(merkle)
}

#[test]
fn test_root_hash_simple_insertions() -> Result<(), DataStoreError> {
    let items = vec![
        ("do", "verb"),
        ("doe", "reindeer"),
        ("dog", "puppy"),
        ("doge", "coin"),
        ("horse", "stallion"),
        ("ddd", "ok"),
    ];
    let merkle = merkle_build_test(items, 0x10000, 0x10000)?;

    merkle.dump()?;

    Ok(())
}

#[test]
fn test_root_hash_fuzz_insertions() -> Result<(), DataStoreError> {
    use rand::{rngs::StdRng, Rng, SeedableRng};
    let rng = std::cell::RefCell::new(StdRng::seed_from_u64(42));
    let max_len0 = 8;
    let max_len1 = 4;
    let keygen = || {
        let (len0, len1): (usize, usize) = {
            let mut rng = rng.borrow_mut();
            (
                rng.gen_range(1..max_len0 + 1),
                rng.gen_range(1..max_len1 + 1),
            )
        };
        let key: Vec<u8> = (0..len0)
            .map(|_| rng.borrow_mut().gen_range(0..2))
            .chain((0..len1).map(|_| rng.borrow_mut().gen()))
            .collect();
        key
    };

    for _ in 0..10 {
        let mut items = Vec::new();
        for _ in 0..10 {
            let val: Vec<u8> = (0..8).map(|_| rng.borrow_mut().gen()).collect();
            items.push((keygen(), val));
        }
        merkle_build_test(items, 0x1000000, 0x1000000)?;
    }

    Ok(())
}

#[test]
fn test_root_hash_reversed_deletions() -> Result<(), DataStoreError> {
    use rand::{rngs::StdRng, Rng, SeedableRng};
    let rng = std::cell::RefCell::new(StdRng::seed_from_u64(42));
    let max_len0 = 8;
    let max_len1 = 4;
    let keygen = || {
        let (len0, len1): (usize, usize) = {
            let mut rng = rng.borrow_mut();
            (
                rng.gen_range(1..max_len0 + 1),
                rng.gen_range(1..max_len1 + 1),
            )
        };
        let key: Vec<u8> = (0..len0)
            .map(|_| rng.borrow_mut().gen_range(0..2))
            .chain((0..len1).map(|_| rng.borrow_mut().gen()))
            .collect();
        key
    };
    for i in 0..10 {
        let mut items = std::collections::HashMap::new();
        for _ in 0..10 {
            let val: Vec<u8> = (0..8).map(|_| rng.borrow_mut().gen()).collect();
            items.insert(keygen(), val);
        }
        let mut items: Vec<_> = items.into_iter().collect();
        items.sort();
        let mut merkle = new_merkle(0x100000, 0x100000);
        let mut hashes = Vec::new();
        let mut dumps = Vec::new();
        for (k, v) in items.iter() {
            dumps.push(merkle.dump());
            merkle.insert(k, v.to_vec())?;
            hashes.push(merkle.root_hash());
        }
        hashes.pop();
        println!("----");
        let mut prev_dump = merkle.dump()?;
        for (((k, _), h), d) in items
            .iter()
            .rev()
            .zip(hashes.iter().rev())
            .zip(dumps.iter().rev())
        {
            merkle.remove(k)?;
            let h0 = merkle.root_hash()?.0;
            if h.as_ref().unwrap().0 != h0 {
                for (k, _) in items.iter() {
                    println!("{}", hex::encode(k));
                }
                println!(
                    "{} != {}",
                    hex::encode(**h.as_ref().unwrap()),
                    hex::encode(h0)
                );
                println!("== before {} ===", hex::encode(k));
                print!("{prev_dump}");
                println!("== after {} ===", hex::encode(k));
                print!("{}", merkle.dump()?);
                println!("== should be ===");
                print!("{:?}", d);
                panic!();
            }
            prev_dump = merkle.dump()?;
        }
        println!("i = {i}");
    }
    Ok(())
}

#[test]
fn test_root_hash_random_deletions() -> Result<(), DataStoreError> {
    use rand::{rngs::StdRng, seq::SliceRandom, Rng, SeedableRng};
    let rng = std::cell::RefCell::new(StdRng::seed_from_u64(42));
    let max_len0 = 8;
    let max_len1 = 4;
    let keygen = || {
        let (len0, len1): (usize, usize) = {
            let mut rng = rng.borrow_mut();
            (
                rng.gen_range(1..max_len0 + 1),
                rng.gen_range(1..max_len1 + 1),
            )
        };
        let key: Vec<u8> = (0..len0)
            .map(|_| rng.borrow_mut().gen_range(0..2))
            .chain((0..len1).map(|_| rng.borrow_mut().gen()))
            .collect();
        key
    };
    for i in 0..10 {
        let mut items = std::collections::HashMap::new();
        for _ in 0..10 {
            let val: Vec<u8> = (0..8).map(|_| rng.borrow_mut().gen()).collect();
            items.insert(keygen(), val);
        }
        let mut items_ordered: Vec<_> = items.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
        items_ordered.sort();
        items_ordered.shuffle(&mut *rng.borrow_mut());
        let mut merkle = new_merkle(0x100000, 0x100000);
        for (k, v) in items.iter() {
            merkle.insert(k, v.to_vec())?;
        }
        for (k, _) in items_ordered.into_iter() {
            assert!(merkle.get(&k)?.is_some());
            assert!(merkle.get_mut(&k)?.is_some());
            merkle.remove(&k)?;
            assert!(merkle.get(&k)?.is_none());
            assert!(merkle.get_mut(&k)?.is_none());
            items.remove(&k);
            for (k, v) in items.iter() {
                assert_eq!(&*merkle.get(k)?.unwrap(), &v[..]);
                assert_eq!(&*merkle.get_mut(k)?.unwrap().get(), &v[..]);
            }
            let h = triehash::trie_root::<keccak_hasher::KeccakHasher, Vec<_>, _, _>(
                items.iter().collect(),
            );
            let h0 = merkle.root_hash()?;
            if h[..] != *h0 {
                println!("{} != {}", hex::encode(h), hex::encode(*h0));
            }
        }
        println!("i = {i}");
    }
    Ok(())
}

#[test]
fn test_proof() -> Result<(), DataStoreError> {
    let set = generate_random_data(500);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;
    let (keys, vals): (Vec<&[u8; 32]>, Vec<&[u8; 20]>) = items.into_iter().unzip();

    for (i, key) in keys.iter().enumerate() {
        let proof = merkle.prove(key)?;
        assert!(!proof.0.is_empty());
        let val = merkle.verify_proof(key, &proof)?;
        assert!(val.is_some());
        assert_eq!(val.unwrap(), vals[i]);
    }

    Ok(())
}

#[test]
/// Verify the proofs that end with leaf node with the given key.
fn test_proof_end_with_leaf() -> Result<(), DataStoreError> {
    let items = vec![
        ("do", "verb"),
        ("doe", "reindeer"),
        ("dog", "puppy"),
        ("doge", "coin"),
        ("horse", "stallion"),
        ("ddd", "ok"),
    ];
    let merkle = merkle_build_test(items, 0x10000, 0x10000)?;
    let key = "doe";

    let proof = merkle.prove(key)?;
    assert!(!proof.0.is_empty());

    let verify_proof = merkle.verify_proof(key, &proof)?;
    assert!(verify_proof.is_some());

    Ok(())
}

#[test]
/// Verify the proofs that end with branch node with the given key.
fn test_proof_end_with_branch() -> Result<(), DataStoreError> {
    let items = vec![
        ("d", "verb"),
        ("do", "verb"),
        ("doe", "reindeer"),
        ("e", "coin"),
    ];
    let merkle = merkle_build_test(items, 0x10000, 0x10000)?;
    let key = "d";

    let proof = merkle.prove(key)?;
    assert!(!proof.0.is_empty());

    let verify_proof = merkle.verify_proof(key, &proof)?;
    assert!(verify_proof.is_some());

    Ok(())
}

#[test]
fn test_bad_proof() -> Result<(), DataStoreError> {
    let set = generate_random_data(800);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;
    let (keys, _): (Vec<&[u8; 32]>, Vec<&[u8; 20]>) = items.into_iter().unzip();

    for (_, key) in keys.iter().enumerate() {
        let mut proof = merkle.prove(key)?;
        assert!(!proof.0.is_empty());

        // Delete an entry from the generated proofs.
        let len = proof.0.len();
        let new_proof = Proof(proof.0.drain().take(len - 1).collect());
        assert!(merkle.verify_proof(key, &new_proof).is_err());
    }

    Ok(())
}

#[test]
// Tests that missing keys can also be proven. The test explicitly uses a single
// entry trie and checks for missing keys both before and after the single entry.
fn test_missing_key_proof() -> Result<(), DataStoreError> {
    let items = vec![("k", "v")];
    let merkle = merkle_build_test(items, 0x10000, 0x10000)?;
    for key in &["a", "j", "l", "z"] {
        let proof = merkle.prove(key)?;
        assert!(!proof.0.is_empty());
        assert!(proof.0.len() == 1);

        let val = merkle.verify_proof(key, &proof)?;
        assert!(val.is_none());
    }

    Ok(())
}

#[test]
fn test_empty_tree_proof() -> Result<(), DataStoreError> {
    let items: Vec<(&str, &str)> = Vec::new();
    let merkle = merkle_build_test(items, 0x10000, 0x10000)?;
    let key = "x";

    let proof = merkle.prove(key)?;
    assert!(proof.0.is_empty());

    Ok(())
}

#[test]
// Tests normal range proof with both edge proofs as the existent proof.
// The test cases are generated randomly.
fn test_range_proof() -> Result<(), ProofError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    for _ in 0..10 {
        let start = rand::thread_rng().gen_range(0..items.len());
        let end = rand::thread_rng().gen_range(0..items.len() - start) + start - 1;

        if end <= start {
            continue;
        }

        let mut proof = merkle.prove(items[start].0)?;
        assert!(!proof.0.is_empty());
        let end_proof = merkle.prove(items[end - 1].0)?;
        assert!(!end_proof.0.is_empty());
        proof.concat_proofs(end_proof);

        let mut keys = Vec::new();
        let mut vals = Vec::new();
        for item in items[start..end].iter() {
            keys.push(&item.0);
            vals.push(&item.1);
        }

        merkle.verify_range_proof(&proof, &items[start].0, &items[end - 1].0, keys, vals)?;
    }
    Ok(())
}

#[test]
// Tests a few cases which the proof is wrong.
// The prover is expected to detect the error.
fn test_bad_range_proof() -> Result<(), ProofError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    for _ in 0..10 {
        let start = rand::thread_rng().gen_range(0..items.len());
        let end = rand::thread_rng().gen_range(0..items.len() - start) + start - 1;

        if end <= start {
            continue;
        }

        let mut proof = merkle.prove(items[start].0)?;
        assert!(!proof.0.is_empty());
        let end_proof = merkle.prove(items[end - 1].0)?;
        assert!(!end_proof.0.is_empty());
        proof.concat_proofs(end_proof);

        let mut keys: Vec<[u8; 32]> = Vec::new();
        let mut vals: Vec<[u8; 20]> = Vec::new();
        for item in items[start..end].iter() {
            keys.push(*item.0);
            vals.push(*item.1);
        }

        let test_case: u32 = rand::thread_rng().gen_range(0..6);
        let index = rand::thread_rng().gen_range(0..end - start);
        match test_case {
            0 => {
                // Modified key
                keys[index] = rand::thread_rng().gen::<[u8; 32]>(); // In theory it can't be same
            }
            1 => {
                // Modified val
                vals[index] = rand::thread_rng().gen::<[u8; 20]>(); // In theory it can't be same
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
                let index_1 = rand::thread_rng().gen_range(0..end - start);
                let index_2 = rand::thread_rng().gen_range(0..end - start);
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
        assert!(merkle
            .verify_range_proof(&proof, *items[start].0, *items[end - 1].0, keys, vals)
            .is_err());
    }

    Ok(())
}

#[test]
// Tests normal range proof with two non-existent proofs.
// The test cases are generated randomly.
fn test_range_proof_with_non_existent_proof() -> Result<(), ProofError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    for _ in 0..10 {
        let start = rand::thread_rng().gen_range(0..items.len());
        let end = rand::thread_rng().gen_range(0..items.len() - start) + start - 1;

        if end <= start {
            continue;
        }

        // Short circuit if the decreased key is same with the previous key
        let first = decrease_key(items[start].0);
        if start != 0 && first.as_ref() == items[start - 1].0.as_ref() {
            continue;
        }
        // Short circuit if the decreased key is underflow
        if first.as_ref() > items[start].0.as_ref() {
            continue;
        }
        // Short circuit if the increased key is same with the next key
        let last = increase_key(items[end - 1].0);
        if end != items.len() && last.as_ref() == items[end].0.as_ref() {
            continue;
        }
        // Short circuit if the increased key is overflow
        if last.as_ref() < items[end - 1].0.as_ref() {
            continue;
        }

        let mut proof = merkle.prove(first)?;
        assert!(!proof.0.is_empty());
        let end_proof = merkle.prove(last)?;
        assert!(!end_proof.0.is_empty());
        proof.concat_proofs(end_proof);

        let mut keys: Vec<[u8; 32]> = Vec::new();
        let mut vals: Vec<[u8; 20]> = Vec::new();
        for item in items[start..end].iter() {
            keys.push(*item.0);
            vals.push(*item.1);
        }

        merkle.verify_range_proof(&proof, first, last, keys, vals)?;
    }

    // Special case, two edge proofs for two edge key.
    let first: [u8; 32] = [0; 32];
    let last: [u8; 32] = [255; 32];
    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(last)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    let (keys, vals): (Vec<&[u8; 32]>, Vec<&[u8; 20]>) = items.into_iter().unzip();
    merkle.verify_range_proof(&proof, &first, &last, keys, vals)?;

    Ok(())
}

#[test]
// Tests such scenarios:
// - There exists a gap between the first element and the left edge proof
// - There exists a gap between the last element and the right edge proof
fn test_range_proof_with_invalid_non_existent_proof() -> Result<(), ProofError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    // Case 1
    let mut start = 100;
    let mut end = 200;
    let first = decrease_key(items[start].0);

    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(items[end - 1].0)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    start = 105; // Gap created
    let mut keys: Vec<[u8; 32]> = Vec::new();
    let mut vals: Vec<[u8; 20]> = Vec::new();
    // Create gap
    for item in items[start..end].iter() {
        keys.push(*item.0);
        vals.push(*item.1);
    }
    assert!(merkle
        .verify_range_proof(&proof, first, *items[end - 1].0, keys, vals)
        .is_err());

    // Case 2
    start = 100;
    end = 200;
    let last = increase_key(items[end - 1].0);

    let mut proof = merkle.prove(items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(last)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    end = 195; // Capped slice
    let mut keys: Vec<[u8; 32]> = Vec::new();
    let mut vals: Vec<[u8; 20]> = Vec::new();
    // Create gap
    for item in items[start..end].iter() {
        keys.push(*item.0);
        vals.push(*item.1);
    }
    assert!(merkle
        .verify_range_proof(&proof, *items[start].0, last, keys, vals)
        .is_err());

    Ok(())
}

#[test]
// Tests the proof with only one element. The first edge proof can be existent one or
// non-existent one.
fn test_one_element_range_proof() -> Result<(), ProofError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    // One element with existent edge proof, both edge proofs
    // point to the SAME key.
    let start = 1000;
    let start_proof = merkle.prove(items[start].0)?;
    assert!(!start_proof.0.is_empty());

    merkle.verify_range_proof(
        &start_proof,
        &items[start].0,
        &items[start].0,
        vec![&items[start].0],
        vec![&items[start].1],
    )?;

    // One element with left non-existent edge proof
    let first = decrease_key(items[start].0);
    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(items[start].0)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    merkle.verify_range_proof(
        &proof,
        first,
        *items[start].0,
        vec![*items[start].0],
        vec![*items[start].1],
    )?;

    // One element with right non-existent edge proof
    let last = increase_key(items[start].0);
    let mut proof = merkle.prove(items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(last)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    merkle.verify_range_proof(
        &proof,
        *items[start].0,
        last,
        vec![*items[start].0],
        vec![*items[start].1],
    )?;

    // One element with two non-existent edge proofs
    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(last)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    merkle.verify_range_proof(
        &proof,
        first,
        last,
        vec![*items[start].0],
        vec![*items[start].1],
    )?;

    // Test the mini trie with only a single element.
    let key = rand::thread_rng().gen::<[u8; 32]>();
    let val = rand::thread_rng().gen::<[u8; 20]>();
    let merkle = merkle_build_test(vec![(key, val)], 0x10000, 0x10000)?;

    let first: [u8; 32] = [0; 32];
    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(key)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    merkle.verify_range_proof(&proof, first, key, vec![key], vec![val])?;

    Ok(())
}

#[test]
// Tests the range proof with all elements.
// The edge proofs can be nil.
fn test_all_elements_proof() -> Result<(), ProofError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    let item_iter = items.clone().into_iter();
    let keys: Vec<&[u8; 32]> = item_iter.clone().map(|item| item.0).collect();
    let vals: Vec<&[u8; 20]> = item_iter.map(|item| item.1).collect();

    let empty_proof = Proof(HashMap::<[u8; 32], Vec<u8>>::new());
    let empty_key: [u8; 32] = [0; 32];
    merkle.verify_range_proof(
        &empty_proof,
        &empty_key,
        &empty_key,
        keys.clone(),
        vals.clone(),
    )?;

    // With edge proofs, it should still work.
    let start = 0;
    let end = &items.len() - 1;

    let mut proof = merkle.prove(items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(items[end].0)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    merkle.verify_range_proof(
        &proof,
        items[start].0,
        items[end].0,
        keys.clone(),
        vals.clone(),
    )?;

    // Even with non-existent edge proofs, it should still work.
    let first: [u8; 32] = [0; 32];
    let last: [u8; 32] = [255; 32];
    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(last)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    merkle.verify_range_proof(&proof, &first, &last, keys, vals)?;

    Ok(())
}

#[test]
// Tests the range proof with "no" element. The first edge proof must
// be a non-existent proof.
fn test_empty_range_proof() -> Result<(), ProofError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    let cases = [(items.len() - 1, false)];
    for (_, c) in cases.iter().enumerate() {
        let first = increase_key(items[c.0].0);
        let proof = merkle.prove(first)?;
        assert!(!proof.0.is_empty());

        // key and value vectors are intentionally empty.
        let keys: Vec<[u8; 32]> = Vec::new();
        let vals: Vec<[u8; 20]> = Vec::new();

        if c.1 {
            assert!(merkle
                .verify_range_proof(&proof, first, first, keys, vals)
                .is_err());
        } else {
            merkle.verify_range_proof(&proof, first, first, keys, vals)?;
        }
    }

    Ok(())
}

#[test]
// Focuses on the small trie with embedded nodes. If the gapped
// node is embedded in the trie, it should be detected too.
fn test_gapped_range_proof() -> Result<(), ProofError> {
    let mut items = Vec::new();
    // Sorted entries
    for i in 0..10_u32 {
        let mut key: [u8; 32] = [0; 32];
        for (index, d) in i.to_be_bytes().iter().enumerate() {
            key[index] = *d;
        }
        items.push((key, i.to_be_bytes()));
    }
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    let first = 2;
    let last = 8;

    let mut proof = merkle.prove(items[first].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(items[last - 1].0)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    let middle = (first + last) / 2 - first;
    let (keys, vals): (Vec<&[u8; 32]>, Vec<&[u8; 4]>) = items[first..last]
        .iter()
        .enumerate()
        .filter(|(pos, _)| *pos != middle)
        .map(|(_, item)| (&item.0, &item.1))
        .unzip();

    assert!(merkle
        .verify_range_proof(&proof, &items[0].0, &items[items.len() - 1].0, keys, vals)
        .is_err());

    Ok(())
}

#[test]
// Tests the element is not in the range covered by proofs.
fn test_same_side_proof() -> Result<(), DataStoreError> {
    let set = generate_random_data(4096);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    let pos = 1000;
    let mut last = decrease_key(items[pos].0);
    let mut first = last;
    first = decrease_key(&first);

    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(last)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    assert!(merkle
        .verify_range_proof(&proof, first, last, vec![*items[pos].0], vec![items[pos].1])
        .is_err());

    first = increase_key(items[pos].0);
    last = first;
    last = increase_key(&last);

    let mut proof = merkle.prove(first)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(last)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    assert!(merkle
        .verify_range_proof(&proof, first, last, vec![*items[pos].0], vec![items[pos].1])
        .is_err());

    Ok(())
}

#[test]
// Tests the range starts from zero.
fn test_single_side_range_proof() -> Result<(), ProofError> {
    for _ in 0..10 {
        let mut set = HashMap::new();
        for _ in 0..4096_u32 {
            let key = rand::thread_rng().gen::<[u8; 32]>();
            let val = rand::thread_rng().gen::<[u8; 20]>();
            set.insert(key, val);
        }
        let mut items = Vec::from_iter(set.iter());
        items.sort();
        let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

        let cases = vec![0, 1, 100, 1000, items.len() - 1];
        for case in cases {
            let start: [u8; 32] = [0; 32];
            let mut proof = merkle.prove(start)?;
            assert!(!proof.0.is_empty());
            let end_proof = merkle.prove(items[case].0)?;
            assert!(!end_proof.0.is_empty());
            proof.concat_proofs(end_proof);

            let item_iter = items.clone().into_iter().take(case + 1);
            let keys = item_iter.clone().map(|item| *item.0).collect();
            let vals = item_iter.map(|item| item.1).collect();

            merkle.verify_range_proof(&proof, start, *items[case].0, keys, vals)?;
        }
    }
    Ok(())
}

#[test]
// Tests the range ends with 0xffff...fff.
fn test_reverse_single_side_range_proof() -> Result<(), ProofError> {
    for _ in 0..10 {
        let mut set = HashMap::new();
        for _ in 0..1024_u32 {
            let key = rand::thread_rng().gen::<[u8; 32]>();
            let val = rand::thread_rng().gen::<[u8; 20]>();
            set.insert(key, val);
        }
        let mut items = Vec::from_iter(set.iter());
        items.sort();
        let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

        let cases = vec![0, 1, 100, 1000, items.len() - 1];
        for case in cases {
            let end: [u8; 32] = [255; 32];
            let mut proof = merkle.prove(items[case].0)?;
            assert!(!proof.0.is_empty());
            let end_proof = merkle.prove(end)?;
            assert!(!end_proof.0.is_empty());
            proof.concat_proofs(end_proof);

            let item_iter = items.clone().into_iter().skip(case);
            let keys = item_iter.clone().map(|item| item.0).collect();
            let vals = item_iter.map(|item| item.1).collect();

            merkle.verify_range_proof(&proof, items[case].0, &end, keys, vals)?;
        }
    }
    Ok(())
}

#[test]
// Tests the range starts with zero and ends with 0xffff...fff.
fn test_both_sides_range_proof() -> Result<(), ProofError> {
    for _ in 0..10 {
        let mut set = HashMap::new();
        for _ in 0..4096_u32 {
            let key = rand::thread_rng().gen::<[u8; 32]>();
            let val = rand::thread_rng().gen::<[u8; 20]>();
            set.insert(key, val);
        }
        let mut items = Vec::from_iter(set.iter());
        items.sort();
        let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

        let start: [u8; 32] = [0; 32];
        let end: [u8; 32] = [255; 32];

        let mut proof = merkle.prove(start)?;
        assert!(!proof.0.is_empty());
        let end_proof = merkle.prove(end)?;
        assert!(!end_proof.0.is_empty());
        proof.concat_proofs(end_proof);

        let (keys, vals): (Vec<&[u8; 32]>, Vec<&[u8; 20]>) = items.into_iter().unzip();
        merkle.verify_range_proof(&proof, &start, &end, keys, vals)?;
    }
    Ok(())
}

#[test]
// Tests normal range proof with both edge proofs
// as the existent proof, but with an extra empty value included, which is a
// noop technically, but practically should be rejected.
fn test_empty_value_range_proof() -> Result<(), ProofError> {
    let set = generate_random_data(512);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    // Create a new entry with a slightly modified key
    let mid_index = items.len() / 2;
    let key = increase_key(items[mid_index - 1].0);
    let empty_data: [u8; 20] = [0; 20];
    items.splice(mid_index..mid_index, [(&key, &empty_data)].iter().cloned());

    let start = 1;
    let end = items.len() - 1;

    let mut proof = merkle.prove(items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(items[end - 1].0)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    let item_iter = items.clone().into_iter().skip(start).take(end - start);
    let keys = item_iter.clone().map(|item| item.0).collect();
    let vals = item_iter.map(|item| item.1).collect();
    assert!(merkle
        .verify_range_proof(&proof, items[start].0, items[end - 1].0, keys, vals)
        .is_err());

    Ok(())
}

#[test]
// Tests the range proof with all elements,
// but with an extra empty value included, which is a noop technically, but
// practically should be rejected.
fn test_all_elements_empty_value_range_proof() -> Result<(), ProofError> {
    let set = generate_random_data(512);
    let mut items = Vec::from_iter(set.iter());
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    // Create a new entry with a slightly modified key
    let mid_index = items.len() / 2;
    let key = increase_key(items[mid_index - 1].0);
    let empty_data: [u8; 20] = [0; 20];
    items.splice(mid_index..mid_index, [(&key, &empty_data)].iter().cloned());

    let start = 0;
    let end = items.len() - 1;

    let mut proof = merkle.prove(items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(items[end].0)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    let item_iter = items.clone().into_iter();
    let keys = item_iter.clone().map(|item| item.0).collect();
    let vals = item_iter.map(|item| item.1).collect();
    assert!(merkle
        .verify_range_proof(&proof, items[start].0, items[end].0, keys, vals)
        .is_err());

    Ok(())
}

#[test]
fn test_range_proof_keys_with_shared_prefix() -> Result<(), ProofError> {
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
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    let start = hex::decode("0000000000000000000000000000000000000000000000000000000000000000")
        .expect("Decoding failed");
    let end = hex::decode("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
        .expect("Decoding failed");

    let mut proof = merkle.prove(&start)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(&end)?;
    assert!(!end_proof.0.is_empty());
    proof.concat_proofs(end_proof);

    let item_iter = items.into_iter();
    let keys = item_iter.clone().map(|item| item.0).collect();
    let vals = item_iter.map(|item| item.1).collect();

    merkle.verify_range_proof(&proof, start, end, keys, vals)?;

    Ok(())
}

#[test]
// Tests a malicious proof, where the proof is more or less the
// whole trie. This is to match corresponding test in geth.
fn test_bloadted_range_proof() -> Result<(), ProofError> {
    // Use a small trie
    let mut items = Vec::new();
    for i in 0..100_u32 {
        let mut key: [u8; 32] = [0; 32];
        let mut data: [u8; 20] = [0; 20];
        for (index, d) in i.to_be_bytes().iter().enumerate() {
            key[index] = *d;
            data[index] = *d;
        }
        items.push((key, data));
    }
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;

    // In the 'malicious' case, we add proofs for every single item
    // (but only one key/value pair used as leaf)
    let mut proof = Proof(HashMap::new());
    let mut keys = Vec::new();
    let mut vals = Vec::new();
    for (i, item) in items.iter().enumerate() {
        let cur_proof = merkle.prove(item.0)?;
        assert!(!cur_proof.0.is_empty());
        proof.concat_proofs(cur_proof);
        if i == 50 {
            keys.push(item.0);
            vals.push(item.1);
        }
    }

    merkle.verify_range_proof(&proof, keys[0], keys[keys.len() - 1], keys, vals)?;

    Ok(())
}

fn generate_random_data(n: u32) -> HashMap<[u8; 32], [u8; 20]> {
    let mut items: HashMap<[u8; 32], [u8; 20]> = HashMap::new();
    for i in 0..100_u32 {
        let mut key: [u8; 32] = [0; 32];
        let mut data: [u8; 20] = [0; 20];
        for (index, d) in i.to_be_bytes().iter().enumerate() {
            key[index] = *d;
            data[index] = *d;
        }
        items.insert(key, data);

        let mut more_key: [u8; 32] = [0; 32];
        for (index, d) in (i + 10).to_be_bytes().iter().enumerate() {
            more_key[index] = *d;
        }
        items.insert(more_key, data);
    }

    for _ in 0..n {
        let key = rand::thread_rng().gen::<[u8; 32]>();
        let val = rand::thread_rng().gen::<[u8; 20]>();
        items.insert(key, val);
    }
    items
}

fn increase_key(key: &[u8; 32]) -> [u8; 32] {
    let mut new_key = *key;
    for i in (0..key.len()).rev() {
        if new_key[i] == 0xff {
            new_key[i] = 0x00;
        } else {
            new_key[i] += 1;
            break;
        }
    }
    new_key
}

fn decrease_key(key: &[u8; 32]) -> [u8; 32] {
    let mut new_key = *key;
    for i in (0..key.len()).rev() {
        if new_key[i] == 0x00 {
            new_key[i] = 0xff;
        } else {
            new_key[i] -= 1;
            break;
        }
    }
    new_key
}
