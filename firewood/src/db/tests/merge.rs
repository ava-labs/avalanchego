// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{
    db::BatchOp,
    v2::api::{Db, DbView, Proposal},
};

use super::*;
use test_case::test_case;

#[test_case(
        &[],
        None,
        None,
        &[],
        &[];
        "empty everything - no initial data, no merge data"
    )]
#[test_case(
        &[(b"key1", b"val1"), (b"key2", b"val2"), (b"key3", b"val3")],
        Some(b"key1".as_slice()),
        Some(b"key3".as_slice()),
        &[(b"key1", b"new1"), (b"key2", b"new2"), (b"key3", b"new3")],
        &[(b"key1", Some(b"new1")), (b"key2", Some(b"new2")), (b"key3", Some(b"new3"))];
        "basic happy path - update all values in range"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3"), (b"d", b"4")],
        Some(b"b".as_slice()),
        Some(b"c".as_slice()),
        &[],
        &[(b"a", Some(b"1")), (b"b", None), (b"c", None), (b"d", Some(b"4"))];
        "empty input iterator - deletes all keys in range"
    )]
#[test_case(
        &[],
        None,
        None,
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3")],
        &[(b"a", Some(b"1")), (b"b", Some(b"2")), (b"c", Some(b"3"))];
        "empty base trie - insert all keys"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3")],
        None,
        None,
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3")],
        &[(b"a", Some(b"1")), (b"b", Some(b"2")), (b"c", Some(b"3"))];
        "no changes - identical key-values"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3")],
        Some(b"a".as_slice()),
        Some(b"c".as_slice()),
        &[(b"x", b"10"), (b"y", b"20"), (b"z", b"30")],
        &[(b"a", None), (b"b", None), (b"c", None), (b"x", Some(b"10")), (b"y", Some(b"20")), (b"z", Some(b"30"))];
        "full replacement - completely different keys"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3"), (b"d", b"4")],
        Some(b"b".as_slice()),
        Some(b"c".as_slice()),
        &[(b"b", b"new2"), (b"c2", b"inserted")],
        &[(b"a", Some(b"1")), (b"b", Some(b"new2")), (b"c", None), (b"c2", Some(b"inserted")), (b"d", Some(b"4"))];
        "partial overlap - mix of updates, inserts, deletes"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3")],
        Some(b"b".as_slice()),
        Some(b"b".as_slice()),
        &[(b"b", b"updated")],
        &[(b"a", Some(b"1")), (b"b", Some(b"updated")), (b"c", Some(b"3"))];
        "single key range - first_key equals last_key"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3")],
        None,
        None,
        &[(b"x", b"10"), (b"y", b"20")],
        &[(b"a", None), (b"b", None), (b"c", None), (b"x", Some(b"10")), (b"y", Some(b"20"))];
        "unbounded range - replace entire trie"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3"), (b"d", b"4")],
        Some(b"b".as_slice()),
        None,
        &[(b"b", b"new2"), (b"c", b"new3")],
        &[(b"a", Some(b"1")), (b"b", Some(b"new2")), (b"c", Some(b"new3")), (b"d", None)];
        "left-bounded only - from key b to end"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3"), (b"d", b"4")],
        None,
        Some(b"c".as_slice()),
        &[(b"a", b"new1"), (b"b", b"new2")],
        &[(b"a", Some(b"new1")), (b"b", Some(b"new2")), (b"c", None), (b"d", Some(b"4"))];
        "right-bounded only - from start to key c"
    )]
#[test_case(
        &[(b"a", b"1"), (b"e", b"5"), (b"f", b"6")],
        Some(b"b".as_slice()),
        Some(b"d".as_slice()),
        &[(b"b", b"2"), (b"c", b"3"), (b"d", b"4")],
        &[(b"a", Some(b"1")), (b"b", Some(b"2")), (b"c", Some(b"3")), (b"d", Some(b"4")), (b"e", Some(b"5")), (b"f", Some(b"6"))];
        "gap range - merge into range with no existing keys"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3")],
        Some(b"b".as_slice()),
        Some(b"c".as_slice()),
        &[(b"b", b"updated"), (b"c", b"3")],
        &[(b"a", Some(b"1")), (b"b", Some(b"updated")), (b"c", Some(b"3"))];
        "value updates only - same keys, one different value"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3"), (b"d", b"4"), (b"e", b"5")],
        Some(b"b".as_slice()),
        Some(b"d".as_slice()),
        &[],
        &[(b"a", Some(b"1")), (b"b", None), (b"c", None), (b"d", None), (b"e", Some(b"5"))];
        "delete middle range - empty iterator deletes b, c, d"
    )]
#[test_case(
        &[(b"app", b"1"), (b"apple", b"2"), (b"application", b"3")],
        Some(b"app".as_slice()),
        Some(b"application".as_slice()),
        &[(b"app", b"new1"), (b"apply", b"4")],
        &[(b"app", Some(b"new1")), (b"apple", None), (b"application", None), (b"apply", Some(b"4"))];
        "common prefix keys - test branch navigation"
    )]
#[test_case(
        &[(b"a", b"1"), (b"b", b"2"), (b"c", b"3"), (b"d", b"4"), (b"e", b"5")],
        Some(b"b".as_slice()),
        Some(b"d".as_slice()),
        &[(b"b2", b"new"), (b"c", b"updated")],
        &[(b"a", Some(b"1")), (b"b", None), (b"b2", Some(b"new")), (b"c", Some(b"updated")), (b"d", None), (b"e", Some(b"5"))];
        "boundary precision - a and e should remain unchanged"
    )]
fn test_merge_key_value_range(
    initial_kvs: &[(&[u8], &[u8])],
    first_key: Option<&[u8]>,
    last_key: Option<&[u8]>,
    merge_kvs: &[(&[u8], &[u8])],
    expected_kvs: &[(&[u8], Option<&[u8]>)],
) {
    let db = TestDb::new();

    if !initial_kvs.is_empty() {
        db.propose(initial_kvs).unwrap().commit().unwrap();
    }

    let proposal = db
        .merge_key_value_range(first_key, last_key, merge_kvs)
        .unwrap();

    for (key, expected_value) in expected_kvs {
        let actual_value = proposal.val(key).unwrap();
        match expected_value {
            Some(v) => {
                assert_eq!(
                    actual_value.as_deref(),
                    Some(*v),
                    "Key {key:?} should have value {v:?}, but got {actual_value:?}"
                );
            }
            None => {
                assert!(
                    actual_value.is_none(),
                    "Key {key:?} should be deleted, but got {actual_value:?}"
                );
            }
        }
    }

    let merge_root_hash = proposal.root_hash().unwrap();

    // Create a fresh database with the same initial state
    let db2 = TestDb::new();
    if !initial_kvs.is_empty() {
        db2.propose(initial_kvs).unwrap().commit().unwrap();
    }

    // Apply the expected operations manually
    let mut batch = Vec::new();
    if let Some(root) = db2.root_hash().unwrap() {
        batch.extend(
            db2.revision(root)
                .unwrap()
                .iter_option(first_key)
                .unwrap()
                .take_while(|res| {
                    if let Some(last_key) = last_key {
                        match res {
                            Ok((key, _)) => **key <= *last_key,
                            Err(_) => true,
                        }
                    } else {
                        true
                    }
                })
                .map(|res| res.map(|(key, _)| BatchOp::Delete { key })),
        );
    }
    batch.extend(merge_kvs.iter().map(|(key, value)| {
        Ok(BatchOp::Put {
            key: (*key).into(),
            value,
        })
    }));

    let manual_proposal = db2.propose(batch).unwrap();
    let manual_root_hash = manual_proposal.root_hash().unwrap();

    assert_eq!(
        merge_root_hash, manual_root_hash,
        "Root hash from merge should match root hash from manual operations"
    );

    // Commit the proposal and verify persistence
    proposal.commit().unwrap();

    // Verify the committed state
    if let Some(root) = db.root_hash().unwrap() {
        let committed = db.revision(root).unwrap();
        for (key, expected_value) in expected_kvs {
            let actual_value = committed.val(key).unwrap();
            match expected_value {
                Some(v) => {
                    assert_eq!(
                        actual_value.as_deref(),
                        Some(*v),
                        "After commit, key {key:?} should have value {v:?}, but got {actual_value:?}"
                    );
                }
                None => {
                    assert!(
                        actual_value.is_none(),
                        "After commit, key {key:?} should be deleted, but got {actual_value:?}"
                    );
                }
            }
        }
    } else {
        for (key, expected_value) in expected_kvs {
            assert!(
                expected_value.is_none(),
                "Database is empty, but expected key {key:?} to have value {expected_value:?}"
            );
        }
    }
}
