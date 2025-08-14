// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::v2::api::OptionalHashKeyExt;

use super::*;
use ethereum_types::H256;
use hash_db::Hasher;
use plain_hasher::PlainHasher;
use sha3::{Digest, Keccak256};
use test_case::test_case;

#[derive(Default, Debug, Clone, PartialEq, Eq, Hash)]
pub struct KeccakHasher;

impl KeccakHasher {
    fn trie_root<I, K, V>(items: I) -> H256
    where
        I: IntoIterator<Item = (K, V)>,
        K: AsRef<[u8]> + Ord,
        V: AsRef<[u8]>,
    {
        firewood_triehash::trie_root::<Self, _, _, _>(items)
    }
}

impl Hasher for KeccakHasher {
    type Out = H256;
    type StdHasher = PlainHasher;
    const LENGTH: usize = 32;

    #[inline]
    fn hash(x: &[u8]) -> Self::Out {
        let mut hasher = Keccak256::new();
        hasher.update(x);
        let result = hasher.finalize();
        H256::from_slice(result.as_slice())
    }
}

#[test_case([("doe", "reindeer")])]
#[test_case([("doe", "reindeer"),("dog", "puppy"),("dogglesworth", "cat")])]
#[test_case([("doe", "reindeer"),("dog", "puppy"),("dogglesworth", "cacatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatt")])]
#[test_case([("dogglesworth", "cacatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatt")])]
fn test_root_hash_eth_compatible<I, K, V>(kvs: I)
where
    I: Clone + IntoIterator<Item = (K, V)>,
    K: AsRef<[u8]> + Ord,
    V: AsRef<[u8]>,
{
    let merkle = init_merkle(kvs.clone());
    let firewood_hash = merkle.nodestore.root_hash().unwrap_or_else(TrieHash::empty);
    let eth_hash = KeccakHasher::trie_root(kvs).to_fixed_bytes().into();
    assert_eq!(firewood_hash, eth_hash);
}

#[test_case(
            "0000000000000000000000000000000000000002",
            "f844802ca00000000000000000000000000000000000000000000000000000000000000000a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
            &[],
            "c00ca9b8e6a74b03f6b1ae2db4a65ead348e61b74b339fe4b117e860d79c7821"
    )]
#[test_case(
            "0000000000000000000000000000000000000002",
            "f844802ca00000000000000000000000000000000000000000000000000000000000000000a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
            &[
                    ("48078cfed56339ea54962e72c37c7f588fc4f8e5bc173827ba75cb10a63a96a5", "a00200000000000000000000000000000000000000000000000000000000000000")
            ],
            "91336bf4e6756f68e1af0ad092f4a551c52b4a66860dc31adbd736f0acbadaf6"
    )]
#[test_case(
            "0000000000000000000000000000000000000002",
            "f844802ca00000000000000000000000000000000000000000000000000000000000000000a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
            &[
                    ("48078cfed56339ea54962e72c37c7f588fc4f8e5bc173827ba75cb10a63a96a5", "a00200000000000000000000000000000000000000000000000000000000000000"),
                    ("0e81f83a84964b811dd1b8328262a9f57e6bc3e5e7eb53627d10437c73c4b8da", "a02800000000000000000000000000000000000000000000000000000000000000"),
            ],
            "c267104830880c966c2cc8c669659e4bfaf3126558dbbd6216123b457944001b"
    )]
fn test_eth_compatible_accounts(
    account: &str,
    account_value: &str,
    key_suffixes_and_values: &[(&str, &str)],
    expected_root: &str,
) {
    use sha2::Digest as _;
    use sha3::Keccak256;

    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("trace"))
        .is_test(true)
        .try_init()
        .ok();

    let account = make_key(account);
    let expected_key_hash = Keccak256::digest(&account);

    let items = once((
        Box::from(expected_key_hash.as_slice()),
        make_key(account_value),
    ))
    .chain(key_suffixes_and_values.iter().map(|(key_suffix, value)| {
        let key = expected_key_hash
            .iter()
            .copied()
            .chain(make_key(key_suffix).iter().copied())
            .collect();
        let value = make_key(value);
        (key, value)
    }))
    .collect::<Vec<(Box<_>, Box<_>)>>();

    let merkle = init_merkle(items);
    let firewood_hash = merkle.nodestore.root_hash();

    assert_eq!(
        firewood_hash,
        TrieHash::try_from(&*make_key(expected_root)).ok()
    );
}

/// helper method to convert a hex encoded string into a boxed slice
fn make_key(hex_str: &str) -> Key {
    hex::decode(hex_str).unwrap().into_boxed_slice()
}

#[test]
fn test_root_hash_random_deletions() {
    use rand::seq::SliceRandom;
    let rng = firewood_storage::SeededRng::from_option(Some(42));
    let max_len0 = 8;
    let max_len1 = 4;
    let keygen = || {
        let (len0, len1): (usize, usize) = {
            (
                rng.random_range(1..=max_len0),
                rng.random_range(1..=max_len1),
            )
        };
        (0..len0)
            .map(|_| rng.random_range(0..2))
            .chain((0..len1).map(|_| rng.random()))
            .collect()
    };

    for i in 0..10 {
        let mut items = std::collections::HashMap::<Key, Value>::new();

        for _ in 0..10 {
            let val = (0..8).map(|_| rng.random()).collect();
            items.insert(keygen(), val);
        }

        let mut items_ordered: Vec<_> = items.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
        items_ordered.sort_unstable();
        items_ordered.shuffle(&mut &rng);

        let mut committed_merkle = init_merkle(&items);

        for (k, v) in items_ordered {
            let mut merkle = committed_merkle.fork().unwrap();
            assert_eq!(merkle.get_value(&k).unwrap().as_deref(), Some(v.as_ref()));

            merkle.remove(&k).unwrap();

            // assert_eq(None) and not assert(is_none) for better error messages
            assert_eq!(merkle.get_value(&k).unwrap().as_deref(), None);

            items.remove(&k);

            for (k, v) in &items {
                assert_eq!(merkle.get_value(k).unwrap().as_deref(), Some(v.as_ref()));
            }

            committed_merkle = into_committed(merkle.hash(), committed_merkle.nodestore());

            let h: TrieHash = KeccakHasher::trie_root(&items).to_fixed_bytes().into();

            let h0 = committed_merkle
                .nodestore()
                .root_hash()
                .or_default_root_hash()
                .unwrap();

            assert_eq!(h, h0);
        }

        println!("i = {i}");
    }
}
