use firewood::{merkle_util::*, proof::Proof};

fn merkle_build_test<K: AsRef<[u8]> + std::cmp::Ord + Clone, V: AsRef<[u8]> + Clone>(
    items: Vec<(K, V)>,
    meta_size: u64,
    compact_size: u64,
) -> Result<MerkleSetup, DataStoreError> {
    let mut merkle = new_merkle(meta_size, compact_size);
    for (k, v) in items.iter() {
        merkle.insert(k, v.as_ref().to_vec())?;
    }
    let merkle_root = merkle.root_hash().unwrap();
    let items_copy = items.clone();
    let reference_root = triehash::trie_root::<keccak_hasher::KeccakHasher, _, _, _>(items);
    println!(
        "ours: {}, correct: {}",
        hex::encode(merkle_root.0),
        hex::encode(reference_root)
    );
    if merkle_root.0 != reference_root {
        for (k, v) in items_copy {
            println!("{} => {}", hex::encode(k), hex::encode(v));
        }
        println!("{:?}", merkle.dump()?);
        panic!();
    }
    Ok(merkle)
}

#[test]
#[allow(unused_must_use)]
fn test_root_hash_simple_insertions() {
    let items = vec![
        ("do", "verb"),
        ("doe", "reindeer"),
        ("dog", "puppy"),
        ("doge", "coin"),
        ("horse", "stallion"),
        ("ddd", "ok"),
    ];
    let merkle = merkle_build_test(items, 0x10000, 0x10000).unwrap();
    merkle.dump();
}

#[test]
#[allow(unused_must_use)]
fn test_root_hash_fuzz_insertions() {
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
        for _ in 0..10000 {
            let val: Vec<u8> = (0..8).map(|_| rng.borrow_mut().gen()).collect();
            items.push((keygen(), val));
        }
        merkle_build_test(items, 0x1000000, 0x1000000);
    }
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
    for i in 0..1000 {
        let mut items = std::collections::HashMap::new();
        for _ in 0..100 {
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
        for _ in 0..1000 {
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
fn test_one_element_proof() -> Result<(), DataStoreError> {
    let items = vec![("k", "v")];
    let merkle = merkle_build_test(items, 0x10000, 0x10000)?;
    let key = "k";

    let proof = merkle.prove(key)?;
    assert!(!proof.0.is_empty());

    let verify_proof = merkle.verify_proof(key, &proof)?;
    assert!(verify_proof.is_some());

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
#[allow(unused_must_use)]
fn test_bad_proof() {
    let items = vec![
        ("do", "verb"),
        ("doe", "reindeer"),
        ("dog", "puppy"),
        ("doge", "coin"),
        ("horse", "stallion"),
        ("ddd", "ok"),
    ];
    let merkle = merkle_build_test(items, 0x10000, 0x10000);
    let key = "ddd";

    let proof = merkle.as_ref().unwrap().prove(key);
    assert!(!proof.as_ref().unwrap().0.is_empty());

    // Delete an entry from the generated proofs.
    let new_proof = Proof(proof.unwrap().0.drain().take(1).collect());
    merkle.unwrap().verify_proof(key, &new_proof).is_err();
}

#[test]
fn test_missing_key_proof() -> Result<(), DataStoreError> {
    let items = vec![("k", "v")];
    let merkle = merkle_build_test(items, 0x10000, 0x10000)?;
    let key = "x";

    let proof = merkle.prove(key)?;
    assert!(!proof.0.is_empty());

    let verify_proof = merkle.verify_proof(key, &proof)?;
    assert!(verify_proof.is_none());

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
fn test_range_proof() -> Result<(), DataStoreError> {
    let mut items = vec![
        ("doa", "verb"),
        ("doe", "reindeer"),
        ("dog", "puppy"),
        ("ddd", "ok"),
    ];
    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;
    let start = 0;
    let end = &items.len() - 1;

    let mut proof = merkle.prove(items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(items[end].0)?;
    assert!(!end_proof.0.is_empty());

    proof.concat_proofs(end_proof);

    let mut keys = Vec::new();
    let mut vals = Vec::new();
    for i in start + 1..end {
        keys.push(&items[i].0);
        vals.push(&items[i].1);
    }

    merkle.verify_range_proof(&proof, &items[start].0, &items[end].0, keys, vals)?;

    Ok(())
}

#[test]
fn test_range_proof_with_non_existent_proof() -> Result<(), DataStoreError> {
    let mut items = vec![
        (std::str::from_utf8(&[0x7]).unwrap(), "verb"),
        (std::str::from_utf8(&[0x4]).unwrap(), "reindeer"),
        (std::str::from_utf8(&[0x5]).unwrap(), "puppy"),
        (std::str::from_utf8(&[0x6]).unwrap(), "coin"),
        (std::str::from_utf8(&[0x3]).unwrap(), "stallion"),
    ];

    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;
    let start = 0;
    let end = &items.len() - 1;

    let mut proof = merkle.prove(std::str::from_utf8(&[0x2]).unwrap())?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(std::str::from_utf8(&[0x8]).unwrap())?;
    assert!(!end_proof.0.is_empty());

    proof.concat_proofs(end_proof);

    let mut keys = Vec::new();
    let mut vals = Vec::new();
    for i in start..=end {
        keys.push(&items[i].0);
        vals.push(&items[i].1);
    }

    merkle.verify_range_proof(&proof, &items[start].0, &items[end].0, keys, vals)?;

    Ok(())
}

#[test]
#[allow(unused_must_use)]
fn test_range_proof_with_invalid_non_existent_proof() {
    let mut items = vec![
        (std::str::from_utf8(&[0x8]).unwrap(), "verb"),
        (std::str::from_utf8(&[0x4]).unwrap(), "reindeer"),
        (std::str::from_utf8(&[0x5]).unwrap(), "puppy"),
        (std::str::from_utf8(&[0x6]).unwrap(), "coin"),
        (std::str::from_utf8(&[0x2]).unwrap(), "stallion"),
    ];

    items.sort();
    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000);
    let start = 0;
    let end = &items.len() - 1;

    let mut proof = merkle
        .as_ref()
        .unwrap()
        .prove(std::str::from_utf8(&[0x3]).unwrap());
    assert!(!proof.as_ref().unwrap().0.is_empty());
    let end_proof = merkle
        .as_ref()
        .unwrap()
        .prove(std::str::from_utf8(&[0x7]).unwrap());
    assert!(!end_proof.as_ref().unwrap().0.is_empty());

    proof.as_mut().unwrap().concat_proofs(end_proof.unwrap());

    let mut keys = Vec::new();
    let mut vals = Vec::new();
    // Create gap
    for i in start + 2..end - 1 {
        keys.push(&items[i].0);
        vals.push(&items[i].1);
    }

    merkle
        .unwrap()
        .verify_range_proof(
            proof.as_ref().unwrap(),
            &items[start].0,
            &items[end].0,
            keys,
            vals,
        )
        .is_err();
}

#[test]
// The start and end nodes are both the same.
fn test_one_element_range_proof() -> Result<(), DataStoreError> {
    let mut items = vec![("key1", "value1"), ("key2", "value2"), ("key3", "value3")];
    items.sort();

    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;
    let start = 0;
    let end = &items.len() - 1;

    let mut start_proof = merkle.prove(&items[start].0)?;
    assert!(!start_proof.0.is_empty());
    let end_proof = merkle.prove(&items[start].0)?; // start and end nodes are the same
    assert!(!end_proof.0.is_empty());

    start_proof.concat_proofs(end_proof);

    let mut keys = Vec::new();
    let mut vals = Vec::new();
    for i in start..=end {
        keys.push(&items[i].0);
        vals.push(&items[i].1);
    }

    assert!(merkle.verify_range_proof(&start_proof, &items[start].0, &items[end].0, keys, vals)?);

    Ok(())
}

#[test]
// The range proof starts from 0 (root) to the last one
fn test_all_elements_proof() -> Result<(), DataStoreError> {
    let mut items = vec![("key1", "value1"), ("key2", "value2"), ("key3", "value3")];
    items.sort();

    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;
    let start = 0;
    let end = &items.len() - 1;

    let mut proof = merkle.prove(&items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(&items[end].0)?; // start and end nodes are the same
    assert!(!end_proof.0.is_empty());

    proof.concat_proofs(end_proof);

    let mut keys = Vec::new();
    let mut vals = Vec::new();
    for i in start..=end {
        keys.push(&items[i].0);
        vals.push(&items[i].1);
    }

    merkle.verify_range_proof(&proof, &items[start].0, &items[end].0, keys, vals)?;

    Ok(())
}

#[test]
// Special case when there is a provided edge proof but zero key/value pairs.
fn test_missing_key_value_pairs() -> Result<(), DataStoreError> {
    let mut items = vec![("key1", "value1"), ("key2", "value2"), ("key3", "value3")];
    items.sort();

    let merkle = merkle_build_test(items.clone(), 0x10000, 0x10000)?;
    let start = 0;
    let end = &items.len() - 1;

    let mut proof = merkle.prove(&items[start].0)?;
    assert!(!proof.0.is_empty());
    let end_proof = merkle.prove(&items[end].0)?; // start and end nodes are the same
    assert!(!end_proof.0.is_empty());

    proof.concat_proofs(end_proof);

    // key and value vectors are intentionally empty
    let keys: Vec<&&str> = Vec::new();
    let vals: Vec<&&str> = Vec::new();

    assert_eq!(
        merkle.verify_range_proof(&proof, &items[start].0, &items[end].0, keys, vals),
        Err(DataStoreError::ProofVerificationError)
    );

    Ok(())
}
