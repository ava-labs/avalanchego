// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::api::OptionalHashKeyExt;

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
    let eth_hash: TrieHash = KeccakHasher::trie_root(kvs).to_fixed_bytes().into();
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
    use sha3::Digest as _;
    use sha3::Keccak256;

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

        let (mut committed_merkle, mut header) = init_merkle_with_header(&items);

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

            committed_merkle = into_committed(merkle.hash(), &mut header);

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

/// Keccak256 of empty bytes — the codeHash for accounts with no contract code.
fn empty_code_hash() -> [u8; 32] {
    Keccak256::digest([]).into()
}

/// Keccak256 of RLP-encoded empty string (0x80) — the hash of an empty storage trie.
fn empty_trie_root() -> [u8; 32] {
    Keccak256::digest(rlp::NULL_RLP).into()
}

/// RLP-encode an Ethereum account value: [nonce, balance, storageRoot, codeHash].
fn rlp_encode_account(
    nonce: u64,
    balance: u64,
    storage_root: &[u8; 32],
    code_hash: &[u8; 32],
) -> Box<[u8]> {
    use rlp::RlpStream;

    let mut rlp = RlpStream::new_list(4);
    rlp.append(&nonce);
    rlp.append(&balance);
    rlp.append(&storage_root.as_slice());
    rlp.append(&code_hash.as_slice());
    rlp.out().to_vec().into_boxed_slice()
}

/// RLP-encode a 32-byte storage slot value.
fn rlp_encode_storage(value: &[u8; 32]) -> Vec<u8> {
    use rlp::RlpStream;

    let mut rlp = RlpStream::new();
    rlp.append(&value.as_slice());
    rlp.out().to_vec()
}

/// Insert an account (and optional storage entries) into a trie, commit it,
/// then read back the account value and verify the storageRoot field was
/// updated from the original `input_storage_root`.
fn commit_and_read_storage_root(
    account_key: &[u8],
    account_value: &[u8],
    input_storage_root: &[u8; 32],
    storage_entries: &[(&[u8], &[u8])],
) -> Vec<u8> {
    use rlp::Rlp;

    let account_key_hash = Keccak256::digest(account_key);

    let mut items = vec![(
        Box::from(account_key_hash.as_slice()),
        Box::from(account_value),
    )];
    for (suffix, value) in storage_entries {
        let key: Box<[u8]> = [account_key_hash.as_slice(), *suffix].concat().into();
        items.push((key, Box::from(*value)));
    }

    let merkle = init_merkle(items);

    let stored = merkle
        .get_value(account_key_hash.as_slice())
        .unwrap()
        .expect("account should exist");

    let rlp = Rlp::new(&stored);
    let list: Vec<Vec<u8>> = rlp.as_list().unwrap();
    assert!(
        list.len() >= 3,
        "account value should have at least 3 RLP items"
    );

    let persisted_storage_root = list.into_iter().nth(2).unwrap();
    assert_ne!(
        persisted_storage_root.as_slice(),
        input_storage_root.as_slice(),
        "storageRoot should have been updated from its original value"
    );
    persisted_storage_root
}

/// Verify that storageRoot is autocomputed during hashing for both
/// 4-item (standard) and 5-item (coreth with trailing empty byte) account RLP.
#[test_case(&[1u64, 0, 0, 0]; "4 item")]
#[test_case(&[1u64, 0, 0, 0, 0]; "5 item")]
fn test_autocompute_hash(fields: &[u64]) {
    use rlp::RlpStream;

    let account_addr = [0u8; 20];
    let dummy_storage_root = [0u8; 32];

    let mut rlp = RlpStream::new_list(fields.len());
    for field in fields {
        rlp.append(field);
    }
    let account_value: Box<[u8]> = rlp.out().to_vec().into();

    let storage_root =
        commit_and_read_storage_root(&account_addr, &account_value, &dummy_storage_root, &[]);

    assert_eq!(
        storage_root,
        empty_trie_root(),
        "storageRoot should be autocomputed as empty trie root"
    );
}

/// A branch account (one storage entry) should have its storageRoot set to
/// the hash of the single-node storage sub-trie.
#[test]
fn test_persisted_storage_root_one_storage_entry() {
    let account_addr = [0u8; 20];
    let dummy_storage_root = [0u8; 32];
    let account_value = rlp_encode_account(0, 44, &dummy_storage_root, &empty_code_hash());

    let storage_key = [1u8; 32];
    let storage_value = rlp_encode_storage(&[2u8; 32]);

    let storage_root = commit_and_read_storage_root(
        &account_addr,
        &account_value,
        &dummy_storage_root,
        &[(&storage_key, &storage_value)],
    );

    // Expected value: root hash of a standalone storage trie containing
    // just (storage_key, storage_value). Building it the same way the
    // ethhash hasher would lets us assert against a concrete hash rather
    // than just "not empty".
    let expected = init_merkle([(storage_key.as_slice(), storage_value.as_slice())])
        .nodestore()
        .root_hash()
        .expect("standalone storage trie should have a root");

    assert_eq!(
        storage_root.as_slice(),
        expected.as_ref(),
        "storageRoot must match the hash of a standalone single-entry storage trie",
    );
}

/// A branch account (two storage entries) should have its storageRoot set to
/// the hash of the two-node storage sub-trie.
#[test]
fn test_persisted_storage_root_two_storage_entries() {
    let account_addr = [0u8; 20];
    let dummy_storage_root = [0u8; 32];
    let account_value = rlp_encode_account(0, 44, &dummy_storage_root, &empty_code_hash());

    let storage_key_a = [1u8; 32];
    let storage_key_b = [2u8; 32];
    let storage_value_a = rlp_encode_storage(&[0xAAu8; 32]);
    let storage_value_b = rlp_encode_storage(&[0xBBu8; 32]);

    let storage_root = commit_and_read_storage_root(
        &account_addr,
        &account_value,
        &dummy_storage_root,
        &[
            (&storage_key_a, &storage_value_a),
            (&storage_key_b, &storage_value_b),
        ],
    );

    let expected = init_merkle([
        (storage_key_a.as_slice(), storage_value_a.as_slice()),
        (storage_key_b.as_slice(), storage_value_b.as_slice()),
    ])
    .nodestore()
    .root_hash()
    .expect("standalone storage trie should have a root");

    assert_eq!(
        storage_root.as_slice(),
        expected.as_ref(),
        "storageRoot must match the hash of a standalone two-entry storage trie",
    );
}

/// Verify that a range proof bounded to account keys contains the corrected
/// storageRoot fields. The left account has no storage children so its
/// storageRoot must be keccak256(0x80) (empty trie root). The right account
/// has one storage child so its storageRoot must be the computed hash of that
/// storage sub-trie — neither the dummy zeros nor the empty trie root.
///
/// The range proof spans exactly the two account keys and excludes the
/// storage entry that lives under the right account.
#[test]
fn test_range_proof_accounts_have_computed_storage_root() {
    type BoxedAccounts = Box<[(Box<[u8]>, Box<[u8]>)]>;

    let dummy_storage_root = [0u8; 32];
    let empty_root = empty_trie_root();

    // Two accounts sorted by their keccak256 trie keys (left < right).
    let mut accounts: BoxedAccounts = [[0x01u8; 20], [0x02u8; 20]]
        .into_iter()
        .enumerate()
        .map(|(i, addr)| {
            let key = Box::from(Keccak256::digest(addr).as_slice());
            let value = rlp_encode_account(
                i as u64,
                (i as u64 + 1) * 100,
                &dummy_storage_root,
                &empty_code_hash(),
            );
            (key, value)
        })
        .collect();
    accounts.sort_unstable_by(|(a, _), (b, _)| a.cmp(b));
    let left_key = &accounts[0].0;
    let right_key = &accounts[1].0;

    // One storage entry under the right account. Its 64-byte key is
    // right_account_key || storage_suffix, placing it beyond the right
    // account in trie order, so a range proof bounded by right_key
    // naturally excludes it.
    let storage_key: Box<[u8]> = [right_key.as_ref(), &[0xAAu8; 32]].concat().into();
    let storage_value: Box<[u8]> = rlp_encode_storage(&[0x42u8; 32]).into();

    let items: BoxedAccounts = accounts
        .iter()
        .map(|(k, v)| (k.clone(), v.clone()))
        .chain(once((storage_key, storage_value)))
        .collect();
    let merkle = init_merkle(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // Build a range proof over [left_key, right_key]. The storage entry
    // sorts after right_key, so the iterator stops before reaching it.
    let range_proof = merkle
        .range_proof(Some(left_key.as_ref()), Some(right_key.as_ref()), None)
        .unwrap();

    // The range proof must verify against the committed root hash.
    verify_range_proof(
        Some(left_key.as_ref()),
        Some(right_key.as_ref()),
        &root_hash,
        &range_proof,
    )
    .unwrap();

    // Exactly two key-value pairs in the proof.
    assert_eq!(range_proof.iter().len(), 2);

    // Decode and check each account's storageRoot.
    for (key, value) in &range_proof {
        let rlp = rlp::Rlp::new(value.as_ref());
        let list: Vec<Vec<u8>> = rlp
            .as_list()
            .expect("account value should be valid RLP list");
        assert!(
            list.len() >= 4,
            "account RLP should have at least 4 fields, got {} for key {:?}",
            list.len(),
            key.as_ref(),
        );

        let storage_root = &list[2];
        assert_ne!(
            storage_root.as_slice(),
            &dummy_storage_root,
            "storageRoot must not be the original dummy zeros",
        );

        if key.as_ref() == left_key.as_ref() {
            // Left account has no storage children → empty trie root.
            assert_eq!(
                storage_root.as_slice(),
                &empty_root,
                "left account storageRoot should be the empty trie root",
            );
        } else {
            // Right account has a storage child → real computed hash.
            assert_ne!(
                storage_root.as_slice(),
                &empty_root,
                "right account storageRoot should NOT be the empty trie root",
            );
        }
    }
}

/// Find `value_bytes` in the raw [`MemStore`] and overwrite it with `replacement`.
/// The two slices must be the same length. Returns the number of occurrences
/// replaced.
///
/// This is used to simulate legacy databases that stored zeroed hashes inside
/// the RLP-encoded account values. We search for the full serialized value
/// (not just the 32-byte hash) so we won't accidentally corrupt unrelated
/// node hashes or structural data in the [`MemStore`].
fn clobber_value_in_memstore(
    storage: &firewood_storage::MemStore,
    value_bytes: &[u8],
    replacement: &[u8],
) -> usize {
    use firewood_storage::{ReadableStorage, WritableStorage};
    use std::io::Read;
    assert_eq!(value_bytes.len(), replacement.len());

    let mut buf = Vec::new();
    storage
        .stream_from(0)
        .unwrap()
        .read_to_end(&mut buf)
        .unwrap();

    let len = value_bytes.len();
    let mut count = 0;
    for offset in 0..buf.len().saturating_sub(len.saturating_sub(1)) {
        #[expect(clippy::arithmetic_side_effects)]
        if buf.get(offset..offset + len) == Some(value_bytes) {
            storage.write(offset as u64, replacement).unwrap();
            count += 1;
        }
    }
    count
}

/// Given an RLP-encoded account value, return a copy with field 2 (storageRoot)
/// replaced by the given 32-byte value.
fn zero_storage_root_in_rlp(value: &[u8], replacement: &[u8; 32]) -> Vec<u8> {
    let list: Vec<Vec<u8>> = rlp::Rlp::new(value).as_list().unwrap();
    assert!(list.len() >= 3);

    let mut rlp = rlp::RlpStream::new_list(list.len());
    for (i, item) in list.iter().enumerate() {
        if i == 2 {
            rlp.append(&replacement.as_slice());
        } else {
            rlp.append(item);
        }
    }
    rlp.out().to_vec()
}

/// Simulate a pre-fix database: after committing normally (which computes the
/// correct storageRoot), clobber the persisted storageRoot bytes back to zeros
/// in the raw [`MemStore`]. This replicates what old databases look like — correct
/// root hash, but stale zeros in the stored account values.
///
/// Then generate a range proof and verify it still passes. The proof-time fix
/// in `ProofNode::from()` should detect the zeros and recompute the correct
/// storageRoot from the node's children.
#[test]
fn test_range_proof_fixes_legacy_zeroed_storage_root() {
    use crate::RangeProof;
    use firewood_storage::WritableStorage;
    type BoxedAccounts = Box<[(Box<[u8]>, Box<[u8]>)]>;

    let dummy_storage_root = [0u8; 32];

    // Two accounts sorted by their keccak256 trie keys (left < right).
    let mut accounts: BoxedAccounts = [[0x01u8; 20], [0x02u8; 20]]
        .into_iter()
        .enumerate()
        .map(|(i, addr)| {
            let key = Box::from(Keccak256::digest(addr).as_slice());
            let value = rlp_encode_account(
                i as u64,
                (i as u64 + 1) * 100,
                &dummy_storage_root,
                &empty_code_hash(),
            );
            (key, value)
        })
        .collect();
    accounts.sort_unstable_by(|(a, _), (b, _)| a.cmp(b));
    let left_key = &accounts[0].0;
    let right_key = &accounts[1].0;

    // One storage entry under the right account.
    let storage_key: Box<[u8]> = [right_key.as_ref(), &[0xAAu8; 32]].concat().into();
    let storage_value: Box<[u8]> = rlp_encode_storage(&[0x42u8; 32]).into();

    let items: BoxedAccounts = accounts
        .iter()
        .map(|(k, v)| (k.clone(), v.clone()))
        .chain(once((storage_key, storage_value)))
        .collect();
    let (merkle, _header) = init_merkle_with_header(items);
    let root_hash = merkle.nodestore().root_hash().unwrap();

    // ── Phase 1: generate a correct range proof to learn the real storageRoot values ──
    let start_proof = merkle.prove(left_key.as_ref()).unwrap();
    let end_proof = merkle.prove(right_key.as_ref()).unwrap();
    let key_values: BoxedAccounts = accounts
        .iter()
        .map(|(k, _)| {
            let val = merkle
                .get_value(k.as_ref())
                .unwrap()
                .expect("account key should exist");
            (k.to_vec().into_boxed_slice(), val)
        })
        .collect();

    let range_proof = RangeProof::new(start_proof, end_proof, key_values);
    verify_range_proof(
        Some(left_key.as_ref()),
        Some(right_key.as_ref()),
        &root_hash,
        &range_proof,
    )
    .unwrap();

    // ── Phase 2: clobber account values in the MemStore ──
    //
    // For each account, find its full RLP-encoded value in the raw storage
    // and replace it with a copy where the storageRoot field is zeroed.
    // This surgically targets only the value bytes, avoiding collateral
    // damage to node hashes. Then re-open so all reads come from disk.
    let storage = merkle.nodestore().storage().clone();
    for (k, _) in &*accounts {
        let stored = merkle.get_value(k.as_ref()).unwrap().unwrap();
        let zeroed = zero_storage_root_in_rlp(&stored, &dummy_storage_root);
        let replaced = clobber_value_in_memstore(&storage, &stored, &zeroed);
        assert_eq!(
            replaced,
            1,
            "expected exactly one occurrence of account value in MemStore for key {:02x?}",
            k.as_ref(),
        );
    }

    // Overwrite the version string to simulate a pre-hfix database.
    // The old version lacks the persisted storageRoot fix, so the iterator
    // and proof-node paths must recompute the correct values.
    storage.write(0, b"firewood-v1\0\0\0\0\0").unwrap();

    // Re-read the header from the clobbered storage so the version matches.
    let header = NodeStoreHeader::read_from_storage(&*storage).unwrap();

    // Re-open from the clobbered MemStore so all reads come from disk.
    let merkle = Merkle::from(NodeStore::open(&header, storage).unwrap());

    // Sanity check: the stored values now contain dummy zeros.
    for (k, _) in &*accounts {
        let stored = merkle.get_value(k.as_ref()).unwrap().unwrap();
        let list: Vec<Vec<u8>> = rlp::Rlp::new(&stored).as_list().unwrap();
        assert_eq!(
            list[2].as_slice(),
            &dummy_storage_root,
            "stored value should now have zeroed storageRoot",
        );
    }

    // ── Phase 3: generate a range proof from the legacy-style database ──
    // Use the real range_proof() API which collects key_values via the
    // iterator — that's where the fix applies. The proof should verify
    // against the original root hash because both ProofNode construction
    // and the iterator now recompute the correct storageRoot.
    let range_proof = merkle
        .range_proof(Some(left_key.as_ref()), Some(right_key.as_ref()), None)
        .unwrap();

    verify_range_proof(
        Some(left_key.as_ref()),
        Some(right_key.as_ref()),
        &root_hash,
        &range_proof,
    )
    .unwrap();
}
