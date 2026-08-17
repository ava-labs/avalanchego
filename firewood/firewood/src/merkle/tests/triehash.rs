// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::*;
use test_case::test_case;

#[test_case(vec![], None; "empty trie")]
#[test_case(vec![(&[0],&[0])], Some("073615413d814b23383fc2c8d8af13abfffcb371b654b98dbf47dd74b1e4d1b9"); "root")]
#[test_case(vec![(&[0,1],&[0,1])], Some("28e67ae4054c8cdf3506567aa43f122224fe65ef1ab3e7b7899f75448a69a6fd"); "root with partial path")]
#[test_case(vec![(&[0],&[1;32])], Some("ba0283637f46fa807280b7d08013710af08dfdc236b9b22f9d66e60592d6c8a3"); "leaf value >= 32 bytes")]
#[test_case(vec![(&[0],&[0]),(&[0,1],&[1;32])], Some("3edbf1fdd345db01e47655bcd0a9a456857c4093188cf35c5c89b8b0fb3de17e"); "branch value >= 32 bytes")]
#[test_case(vec![(&[0],&[0]),(&[0,1],&[0,1])], Some("c3bdc20aff5cba30f81ffd7689e94e1dbeece4a08e27f0104262431604cf45c6"); "root with leaf child")]
#[test_case(vec![(&[0],&[0]),(&[0,1],&[0,1]),(&[0,1,2],&[0,1,2])], Some("229011c50ad4d5c2f4efe02b8db54f361ad295c4eee2bf76ea4ad1bb92676f97"); "root with branch child")]
#[test_case(vec![(&[0],&[0]),(&[0,1],&[0,1]),(&[0,8],&[0,8]),(&[0,1,2],&[0,1,2])], Some("a683b4881cb540b969f885f538ba5904699d480152f350659475a962d6240ef9"); "root with branch child and leaf child")]
fn test_root_hash_merkledb_compatible(kvs: Vec<(&[u8], &[u8])>, expected_hash: Option<&str>) {
    let merkle = init_merkle(kvs);
    let Some(got_hash) = merkle.nodestore.root_hash() else {
        assert!(expected_hash.is_none());
        return;
    };

    let expected_hash = expected_hash.unwrap();

    // This hash is from merkledb
    let expected_hash: [u8; 32] = hex::decode(expected_hash).unwrap().try_into().unwrap();

    assert_eq!(got_hash, TrieHash::from(expected_hash));
}
