// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(clippy::unwrap_used, clippy::indexing_slicing)]

use integer_encoding::VarInt;
use test_case::test_case;

use firewood_storage::{
    Children, IntoHashType, PathComponent, SeededRng, TrieHash, ValueDigest, logger::debug,
};

use super::{
    header::InvalidHeader,
    magic,
    reader::ReadError,
    types::{Proof, ProofNode, ProofType},
};
use crate::api::{FrozenChangeProof, FrozenRangeProof};
use crate::db::BatchOp;

fn create_valid_range_proof() -> (FrozenRangeProof, Vec<u8>) {
    let merkle = crate::merkle::tests::init_merkle((0u8..=10).map(|k| ([k], [k])));
    let proof = merkle
        .range_proof(Some(&[2u8]), Some(&[8u8]), std::num::NonZeroUsize::new(5))
        .unwrap();
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    (proof, serialized)
}

fn create_valid_change_proof() -> (FrozenChangeProof, Vec<u8>) {
    let proof = FrozenChangeProof::new(
        Proof::new(Box::<[ProofNode]>::from([])),
        Proof::new(Box::<[ProofNode]>::from([])),
        Box::new([
            BatchOp::Put {
                key: Box::from(b"key1".as_slice()),
                value: Box::from(b"val1".as_slice()),
            },
            BatchOp::Delete {
                key: Box::from(b"key2".as_slice()),
            },
            BatchOp::DeleteRange {
                prefix: Box::from(b"key3".as_slice()),
            },
        ]),
    );
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    (proof, serialized)
}

#[test]
fn test_range_proof_roundtrip() {
    let (_, serialized) = create_valid_range_proof();
    let parsed = FrozenRangeProof::from_slice(&serialized).expect("roundtrip should succeed");
    let mut re_serialized = Vec::new();
    parsed.write_to_vec(&mut re_serialized);
    assert_eq!(serialized, re_serialized);
}

#[test]
fn test_change_proof_roundtrip() {
    let (_, serialized) = create_valid_change_proof();
    let parsed = FrozenChangeProof::from_slice(&serialized).expect("roundtrip should succeed");
    let mut re_serialized = Vec::new();
    parsed.write_to_vec(&mut re_serialized);
    assert_eq!(serialized, re_serialized);
}

#[test_case(
    |data| *<&mut [u8; 8]>::try_from(&mut data[0..8]).unwrap() = *b"badmagic",
    |err| matches!(err, InvalidHeader::InvalidMagic { found } if found == b"badmagic");
    "invalid magic"
)]
#[test_case(
    |data| data[8] = 99,
    |err| matches!(err, InvalidHeader::UnsupportedVersion { found: 99 });
    "unsupported version"
)]
#[test_case(
    |data| data[9] = 99,
    |err| matches!(err, InvalidHeader::UnsupportedHashMode { found: 99 });
    "unsupported hash mode"
)]
#[test_case(
    |data| data[10] = 99,
    |err| matches!(err, InvalidHeader::UnsupportedBranchFactor { found: 99 });
    "unsupported branch factor"
)]
#[test_case(
    |data| data[11] = 99,
    |err| matches!(err, InvalidHeader::InvalidProofType { found: 99, expected: Some(ProofType::Range) });
    "invalid proof type"
)]
#[test_case(
    |data| data[11] = ProofType::Change as u8,
    |err| matches!(err, InvalidHeader::InvalidProofType { found: 2, expected: Some(ProofType::Range) });
    "wrong proof type"
)]
fn test_invalid_header(
    mutator: impl FnOnce(&mut Vec<u8>),
    expected: impl FnOnce(&InvalidHeader) -> bool,
) {
    let (_, mut data) = create_valid_range_proof();

    mutator(&mut data);

    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::InvalidHeader(err)) => assert!(expected(&err), "unexpected error: {err}"),
        other => panic!("Expected ReadError::InvalidHeader, got: {other:?}"),
    }
}

#[test_case(
    |_, data| data.truncate(20),
    "header",
    32, // expected len
    20; // found len
    "incomplete header"
)]
#[test_case(
    |_, data| data.truncate(31),
    "header",
    32, // expected len
    31; // found len
    "header one byte short"
)]
#[test_case(
    |_, data| data.truncate(32),
    "array length",
    1, // expected len
    0; // found len
    "no varint after header"
)]
#[test_case(
    |proof, data| {
        #[expect(clippy::arithmetic_side_effects)]
        data.truncate(
            32
            + proof.start_proof().len().required_space()
            + proof.start_proof()[0].key.len().required_space()
            // truncate after the key length varint but before the key bytes
        );
    },
    "byte slice",
    1, // expected len
    0; // found len
    "truncated node key"
)]
fn test_incomplete_item(
    mutator: impl FnOnce(&FrozenRangeProof, &mut Vec<u8>),
    item: &'static str,
    expected_len: usize,
    found_len: usize,
) {
    let (proof, mut data) = create_valid_range_proof();

    debug!("data len: {}", data.len());
    debug!("proof: {proof:#?}");
    debug!("data: {}", hex::encode(&data));

    mutator(&proof, &mut data);

    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::IncompleteItem {
            item: found_item,
            offset: _,
            expected,
            found,
        }) => {
            assert_eq!(
                found_item, item,
                "unexpected `item` value, got: {found_item}, wanted: {item}; {data:?}"
            );
            assert_eq!(
                expected, expected_len,
                "unexpected `expected` value, got: {expected}, wanted: {expected_len}; {data:?}"
            );
            assert_eq!(
                found, found_len,
                "unexpected `found` value, got: {found}, wanted: {found_len}; {data:?}"
            );
        }
        other => panic!("Expected ReadError::IncompleteItem, got: {other:?}"),
    }
}

#[test_case(
    |proof, data| data[32
        + proof.start_proof().len().required_space()
        + proof.start_proof()[0].key.len().required_space()
        + proof.start_proof()[0].key.len()
        + proof.start_proof()[0].partial_len.required_space()
        // Corrupt the option discriminant for the value digest (should be 0 or 1)
    ] = 3, // invalid option discriminant
    "option discriminant",
    "0 or 1",
    "3";
    "invalid option discriminant"
)]
#[test_case(
    |_, data| *<&mut [u8; 10]>::try_from(&mut data[32..42]).unwrap() = [0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89],
    "array length",
    "byte with no MSB within 9 bytes",
    "[128, 129, 130, 131, 132, 133, 134, 135, 136, 137]";
    "invalid varint"
)]
#[test_case(
    |_, data| data.extend_from_slice(&[0xFF; 100]), // extend data with invalid trailing bytes
    "trailing bytes",
    "no data after the proof",
    "100 bytes";
    "extra trailing bytes"
)]
fn test_invalid_item(
    mutator: impl FnOnce(&FrozenRangeProof, &mut Vec<u8>),
    item: &'static str,
    expected: &'static str,
    found: &'static str,
) {
    let (proof, mut data) = create_valid_range_proof();

    mutator(&proof, &mut data);

    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::InvalidItem {
            item: found_item,
            offset: _,
            expected: found_expected,
            found: found_found,
        }) => {
            assert_eq!(
                found_item, item,
                "unexpected `item` value, got: {found_item}, wanted: {item}"
            );
            assert_eq!(
                found_expected, expected,
                "unexpected `expected` value, got: {found_expected}, wanted: {expected}"
            );
            assert_eq!(
                found_found, found,
                "unexpected `found` value, got: {found_found}, wanted: {found}"
            );
        }
        other => panic!("Expected ReadError::InvalidItem, got: {other:?}"),
    }
}

#[test]
fn test_partial_key_len_exceeds_key_len() {
    let (proof, mut data) = create_valid_range_proof();

    let node = &proof.start_proof()[0];
    let key_len = node.key.len();
    let original_partial_len_size = node.partial_len.required_space();
    let invalid_partial_len: usize = key_len + 1;

    let offset =
        32 + proof.start_proof().len().required_space() + key_len.required_space() + key_len;

    data.splice(
        offset..offset + original_partial_len_size,
        invalid_partial_len.encode_var_vec(),
    );

    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::InvalidItem {
            item,
            expected,
            found,
            ..
        }) => {
            assert_eq!(item, "partial key length");
            assert_eq!(expected, "value less than or equal to the key length");
            assert_eq!(found, invalid_partial_len.to_string());
        }
        other => panic!("Expected ReadError::InvalidItem, got: {other:?}"),
    }
}

#[test]
fn test_empty_proof() {
    #[rustfmt::skip]
    let bytes = [
        b'f', b'w', b'd', b'p', b'r', b'o', b'o', b'f', // magic
        0, // version
        magic::HASH_MODE,
        magic::BRANCH_FACTOR,
        ProofType::Range as u8,
        0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // reserved
        0, // start proof length = 0
        0, // end proof length = 0
        0, // key-value pairs length = 0
    ];

    match FrozenRangeProof::from_slice(&bytes) {
        Ok(proof) => {
            assert!(proof.start_proof().is_empty());
            assert!(proof.end_proof().is_empty());
            assert!(proof.key_values().is_empty());
        }
        Err(err) => panic!("Expected valid empty proof, got error: {err}"),
    }
}

#[test_case(
    |data| *<&mut [u8; 8]>::try_from(&mut data[0..8]).unwrap() = *b"badmagic",
    |err| matches!(err, InvalidHeader::InvalidMagic { found } if found == b"badmagic");
    "invalid magic"
)]
#[test_case(
    |data| data[8] = 99,
    |err| matches!(err, InvalidHeader::UnsupportedVersion { found: 99 });
    "unsupported version"
)]
#[test_case(
    |data| data[9] = 99,
    |err| matches!(err, InvalidHeader::UnsupportedHashMode { found: 99 });
    "unsupported hash mode"
)]
#[test_case(
    |data| data[10] = 99,
    |err| matches!(err, InvalidHeader::UnsupportedBranchFactor { found: 99 });
    "unsupported branch factor"
)]
#[test_case(
    |data| data[11] = 99,
    |err| matches!(err, InvalidHeader::InvalidProofType { found: 99, expected: Some(ProofType::Change) });
    "invalid proof type"
)]
#[test_case(
    |data| data[11] = ProofType::Range as u8,
    |err| matches!(err, InvalidHeader::InvalidProofType { found: 1, expected: Some(ProofType::Change) });
    "wrong proof type"
)]
fn test_change_proof_invalid_header(
    mutator: impl FnOnce(&mut Vec<u8>),
    expected: impl FnOnce(&InvalidHeader) -> bool,
) {
    let (_, mut data) = create_valid_change_proof();

    mutator(&mut data);

    match FrozenChangeProof::from_slice(&data) {
        Err(ReadError::InvalidHeader(err)) => assert!(expected(&err), "unexpected error: {err}"),
        other => panic!("Expected ReadError::InvalidHeader, got: {other:?}"),
    }
}

#[test_case(
    |data| data.truncate(20),
    "header",
    32, // expected len
    20; // found len
    "incomplete header"
)]
#[test_case(
    |data| data.truncate(31),
    "header",
    32, // expected len
    31; // found len
    "header one byte short"
)]
#[test_case(
    |data| data.truncate(32),
    "array length",
    1, // expected len
    0; // found len
    "no varint after header"
)]
fn test_change_proof_incomplete_item(
    mutator: impl FnOnce(&mut Vec<u8>),
    item: &'static str,
    expected_len: usize,
    found_len: usize,
) {
    let (_, mut data) = create_valid_change_proof();

    mutator(&mut data);

    match FrozenChangeProof::from_slice(&data) {
        Err(ReadError::IncompleteItem {
            item: found_item,
            offset: _,
            expected,
            found,
        }) => {
            assert_eq!(
                found_item, item,
                "unexpected `item` value, got: {found_item}, wanted: {item}; {data:?}"
            );
            assert_eq!(
                expected, expected_len,
                "unexpected `expected` value, got: {expected}, wanted: {expected_len}; {data:?}"
            );
            assert_eq!(
                found, found_len,
                "unexpected `found` value, got: {found}, wanted: {found_len}; {data:?}"
            );
        }
        other => panic!("Expected ReadError::IncompleteItem, got: {other:?}"),
    }
}

#[test_case(
    |_, data| *<&mut [u8; 10]>::try_from(&mut data[32..42]).unwrap() = [0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89],
    "array length",
    "byte with no MSB within 9 bytes",
    "[128, 129, 130, 131, 132, 133, 134, 135, 136, 137]";
    "invalid varint"
)]
#[test_case(
    |_, data| data.extend_from_slice(&[0xFF; 100]),
    "trailing bytes",
    "no data after the proof",
    "100 bytes";
    "extra trailing bytes"
)]
#[test_case(
    // Layout: 32 (header) + 1 (start_proof len=0) + 1 (end_proof len=0) + 1 (batch_ops len=3) = offset 35
    // Byte at offset 35 is the first BatchOp discriminant (BATCH_PUT = 0)
    |_, data| data[35] = 99,
    "option discriminant",
    "0, 1, or 2",
    "99";
    "invalid batch op discriminant"
)]
fn test_change_proof_invalid_item(
    mutator: impl FnOnce(&FrozenChangeProof, &mut Vec<u8>),
    item: &'static str,
    expected: &'static str,
    found: &'static str,
) {
    let (proof, mut data) = create_valid_change_proof();

    mutator(&proof, &mut data);

    match FrozenChangeProof::from_slice(&data) {
        Err(ReadError::InvalidItem {
            item: found_item,
            offset: _,
            expected: found_expected,
            found: found_found,
        }) => {
            assert_eq!(
                found_item, item,
                "unexpected `item` value, got: {found_item}, wanted: {item}"
            );
            assert_eq!(
                found_expected, expected,
                "unexpected `expected` value, got: {found_expected}, wanted: {expected}"
            );
            assert_eq!(
                found_found, found,
                "unexpected `found` value, got: {found_found}, wanted: {found}"
            );
        }
        other => panic!("Expected ReadError::InvalidItem, got: {other:?}"),
    }
}

/// Constructs a `ProofNode` with the given nibble key, partial length, optional
/// value, and children at the specified nibble indices.
fn make_proof_node(
    key_nibbles: &[u8],
    partial_len: usize,
    value: Option<Box<[u8]>>,
    child_nibbles: &[u8],
) -> ProofNode {
    let key = key_nibbles
        .iter()
        .map(|&n| PathComponent::try_new(n).unwrap())
        .collect();
    let mut child_hashes = Children::new();
    for &nibble in child_nibbles {
        child_hashes[PathComponent::try_new(nibble).unwrap()] =
            Some(TrieHash::from([0u8; 32]).into_hash_type());
    }
    ProofNode {
        key,
        partial_len,
        value_digest: value.map(ValueDigest::Value),
        child_hashes,
    }
}

/// Wraps a single `ProofNode` in a minimal `FrozenRangeProof` and serializes it.
fn make_range_proof_from_single_node(node: ProofNode) -> (FrozenRangeProof, Vec<u8>) {
    let proof = FrozenRangeProof::new(
        Proof::new(Box::new([node])),
        Proof::new(Box::<[ProofNode]>::from([])),
        Box::new([]),
    );
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    (proof, serialized)
}

/// Verifies that deserializing `serialized` and re-serializing produces the same bytes.
fn assert_range_proof_round_trip(serialized: Vec<u8>) {
    let parsed = FrozenRangeProof::from_slice(&serialized).expect("deserialization should succeed");
    let mut re_serialized = Vec::new();
    parsed.write_to_vec(&mut re_serialized);
    assert_eq!(serialized, re_serialized, "round-trip bytes must match");
}

#[test]
fn test_proof_node_leaf_round_trip() {
    // Leaf: no children, no value, empty key
    let node = make_proof_node(&[], 0, None, &[]);
    let (_, serialized) = make_range_proof_from_single_node(node);
    assert_range_proof_round_trip(serialized);
}

#[test]
fn test_proof_node_single_child_round_trip() {
    // Branch with one child at nibble index 7
    let node = make_proof_node(&[1, 2, 3], 0, None, &[7]);
    let (_, serialized) = make_range_proof_from_single_node(node);
    assert_range_proof_round_trip(serialized);
}

#[test]
fn test_proof_node_all_children_round_trip() {
    // Branch with all 16 children present (ChildMask = 0xFFFF)
    let all_nibbles: Vec<u8> = (0u8..16).collect();
    let node = make_proof_node(&[0], 0, None, &all_nibbles);
    let (_, serialized) = make_range_proof_from_single_node(node);
    assert_range_proof_round_trip(serialized);
}

#[cfg(not(feature = "ethhash"))]
#[test]
fn test_value_digest_hash_round_trip() {
    // Values >= 32 bytes are converted to a hash by make_hash() during serialization.
    // The round-trip bytes should still match because re-serializing a Hash-variant
    // node also produces a hash discriminant (1) rather than a value discriminant (0).
    let value: Box<[u8]> = vec![0xABu8; 32].into_boxed_slice();
    let node = make_proof_node(&[1, 2], 0, Some(value), &[]);
    let (_, serialized) = make_range_proof_from_single_node(node);
    assert_range_proof_round_trip(serialized);
}

#[test_case(
    BatchOp::Put { key: Box::from(b"k".as_slice()), value: Box::from(b"v".as_slice()) };
    "put"
)]
#[test_case(
    BatchOp::Delete { key: Box::from(b"k".as_slice()) };
    "delete"
)]
#[test_case(
    BatchOp::DeleteRange { prefix: Box::from(b"k".as_slice()) };
    "delete range"
)]
fn test_change_proof_batch_op_variant(op: BatchOp<Box<[u8]>, Box<[u8]>>) {
    let proof = FrozenChangeProof::new(
        Proof::new(Box::<[ProofNode]>::from([])),
        Proof::new(Box::<[ProofNode]>::from([])),
        Box::new([op]),
    );
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    let parsed =
        FrozenChangeProof::from_slice(&serialized).expect("deserialization should succeed");
    let mut re_serialized = Vec::new();
    parsed.write_to_vec(&mut re_serialized);
    assert_eq!(serialized, re_serialized, "round-trip bytes must match");
}

#[test]
fn test_proof_node_partial_len_boundaries() {
    // partial_len = 0: no shared prefix with the parent node
    let node = make_proof_node(&[1, 2, 3, 4], 0, None, &[]);
    let (_, serialized) = make_range_proof_from_single_node(node);
    assert_range_proof_round_trip(serialized);

    // partial_len = key.len(): entire key is shared with the parent
    let node = make_proof_node(&[1, 2, 3, 4], 4, None, &[]);
    let (_, serialized) = make_range_proof_from_single_node(node);
    assert_range_proof_round_trip(serialized);
}

// These tests use manually constructed proofs with known byte layouts.
//
// Layout for make_range_proof_from_single_node(make_proof_node(&[1, 2, 3], 0, None, &[])):
//   [32]=0x01 (start_proof count)
//   [33]=0x03 (key byte length)    [34]=0x01  [35]=0x02  [36]=0x03 (key bytes)
//   [37]=0x00 (partial_len)        [38]=0x00  (option discriminant = None)
//   [39]=0x00  [40]=0x00           (ChildMask = 0)
//   [41]=0x00 (end_proof count)    [42]=0x00  (key_values count)
//
// Layout for make_range_proof_from_single_node(make_proof_node(&[1, 2, 3], 0, Some(b"v"), &[])):
//   [32..37] same as above
//   [38]=0x01 (option discriminant = Some)   [39]=0x00 (value digest discriminant = Value)
//   [40]=0x01 (value byte length)            [41]=0x76 (b'v')
//   [42]=0x00  [43]=0x00 (ChildMask = 0)     [44]=0x00  [45]=0x00
//
// Layout for make_range_proof_from_single_node(make_proof_node(&[1], 0, None, &[7])):
//   [32]=0x01  [33]=0x01  [34]=0x01  (count, key len, key byte)
//   [35]=0x00  [36]=0x00             (partial_len, option discriminant = None)
//   [37]=0x80  [38]=0x00             (ChildMask — nibble 7 = bit 7 of low byte)
//   Non-ethhash: [39..71] = TrieHash (32 bytes)
//   Ethhash:     [39] = HashType discriminant, [40..72] = TrieHash

#[test]
fn test_invalid_path_nibble() {
    let node = make_proof_node(&[1, 2, 3], 0, None, &[]);
    let (_, mut data) = make_range_proof_from_single_node(node);
    data[34] = 0x10; // first key byte set to an invalid nibble (16 > 15)
    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::InvalidItem { item, .. }) => assert_eq!(item, "path"),
        other => panic!("Expected InvalidItem {{ item: \"path\" }}, got: {other:?}"),
    }
}

#[test]
fn test_invalid_value_digest_discriminant() {
    let node = make_proof_node(&[1, 2, 3], 0, Some(Box::from(b"v".as_slice())), &[]);
    let (_, mut data) = make_range_proof_from_single_node(node);
    data[39] = 2; // invalid ValueDigest discriminant (must be 0 or 1)
    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::InvalidItem {
            item,
            expected,
            found,
            ..
        }) => {
            assert_eq!(item, "value digest discriminant");
            assert_eq!(expected, "0 (value) or 1 (hash)");
            assert_eq!(found, "2");
        }
        other => {
            panic!("Expected InvalidItem {{ item: \"value digest discriminant\" }}, got: {other:?}")
        }
    }
}

#[test_case(38, "option discriminant", 1, 0; "option discriminant")]
#[test_case(39, "children map", 2, 0; "children map zero bytes")]
#[test_case(40, "children map", 2, 1; "children map one byte")]
fn test_incomplete_item_known_layout(
    truncate_at: usize,
    item: &'static str,
    expected_len: usize,
    found_len: usize,
) {
    let node = make_proof_node(&[1, 2, 3], 0, None, &[]);
    let (_, mut data) = make_range_proof_from_single_node(node);
    data.truncate(truncate_at);
    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::IncompleteItem {
            item: found_item,
            expected,
            found,
            ..
        }) => {
            assert_eq!(found_item, item);
            assert_eq!(expected, expected_len);
            assert_eq!(found, found_len);
        }
        other => panic!("Expected IncompleteItem {{ item: {item:?} }}, got: {other:?}"),
    }
}

#[cfg(not(feature = "ethhash"))]
#[test]
fn test_incomplete_trie_hash() {
    let node = make_proof_node(&[1], 0, None, &[7]);
    let (_, mut data) = make_range_proof_from_single_node(node);
    data.truncate(39); // ChildMask ends at [38]; TrieHash starts at [39]
    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::IncompleteItem {
            item,
            expected,
            found,
            ..
        }) => {
            assert_eq!(item, "trie hash");
            assert_eq!(expected, 32);
            assert_eq!(found, 0);
        }
        other => panic!("Expected IncompleteItem {{ item: \"trie hash\" }}, got: {other:?}"),
    }
}

#[cfg(feature = "ethhash")]
#[test]
fn test_incomplete_hash_type_discriminant() {
    let node = make_proof_node(&[1], 0, None, &[7]);
    let (_, mut data) = make_range_proof_from_single_node(node);
    data.truncate(39); // ChildMask ends at [38]; HashType discriminant is at [39]
    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::IncompleteItem {
            item,
            expected,
            found,
            ..
        }) => {
            assert_eq!(item, "hash type discriminant");
            assert_eq!(expected, 1);
            assert_eq!(found, 0);
        }
        other => {
            panic!("Expected IncompleteItem {{ item: \"hash type discriminant\" }}, got: {other:?}")
        }
    }
}

#[cfg(feature = "ethhash")]
#[test]
fn test_invalid_hash_type_discriminant() {
    let node = make_proof_node(&[1], 0, None, &[7]);
    let (_, mut data) = make_range_proof_from_single_node(node);
    data[39] = 2; // invalid HashType discriminant (must be 0 or 1)
    match FrozenRangeProof::from_slice(&data) {
        Err(ReadError::InvalidItem {
            item,
            expected,
            found,
            ..
        }) => {
            assert_eq!(item, "hash type discriminant");
            assert_eq!(expected, "0 (hash) or 1 (rlp)");
            assert_eq!(found, "2");
        }
        other => {
            panic!("Expected InvalidItem {{ item: \"hash type discriminant\" }}, got: {other:?}")
        }
    }
}

#[test]
fn test_change_proof_incomplete_batch_op_discriminant() {
    // Layout of create_valid_change_proof() after the 32-byte header:
    //   [32]=0x00 (start_proof count=0)  [33]=0x00 (end_proof count=0)
    //   [34]=0x03 (batch_ops count=3)    [35]=0x00 (first BatchOp discriminant)
    let (_, mut data) = create_valid_change_proof();
    data.truncate(35); // cut before the first BatchOp discriminant byte
    match FrozenChangeProof::from_slice(&data) {
        Err(ReadError::IncompleteItem {
            item,
            expected,
            found,
            ..
        }) => {
            assert_eq!(item, "option discriminant");
            assert_eq!(expected, 1);
            assert_eq!(found, 0);
        }
        other => {
            panic!("Expected IncompleteItem {{ item: \"option discriminant\" }}, got: {other:?}")
        }
    }
}

/// Generates a random `ProofNode` using `rng`.
fn generate_random_proof_node(rng: &SeededRng) -> ProofNode {
    let key_len = rng.random_range(0usize..=32);
    let key = (0..key_len)
        .map(|_| PathComponent::try_new(rng.random_range(0u8..16)).unwrap())
        .collect();
    let partial_len = if key_len == 0 {
        0
    } else {
        rng.random_range(0..=key_len)
    };
    let value_digest = rng.random::<bool>().then(|| {
        let val_len = rng.random_range(0usize..=64);
        let value: Box<[u8]> = (0..val_len).map(|_| rng.random::<u8>()).collect();
        ValueDigest::Value(value)
    });
    let mut child_hashes = Children::new();
    for nibble in 0u8..16 {
        if rng.random::<bool>() {
            child_hashes[PathComponent::try_new(nibble).unwrap()] =
                Some(TrieHash::from(rng.random::<[u8; 32]>()).into_hash_type());
        }
    }
    ProofNode {
        key,
        partial_len,
        value_digest,
        child_hashes,
    }
}

/// Generates a random `FrozenRangeProof` and returns it with its serialized bytes.
///
/// The seed used is printed to stderr by `SeededRng` so failures can be reproduced.
fn generate_random_range_proof(rng: &SeededRng) -> (FrozenRangeProof, Vec<u8>) {
    let start_nodes: Box<[ProofNode]> = (0..rng.random_range(0usize..=5))
        .map(|_| generate_random_proof_node(rng))
        .collect();
    let end_nodes: Box<[ProofNode]> = (0..rng.random_range(0usize..=5))
        .map(|_| generate_random_proof_node(rng))
        .collect();
    let key_values: Box<[_]> = (0..rng.random_range(0usize..=10))
        .map(|_| {
            let key_len = rng.random_range(0usize..=32);
            let key: Box<[u8]> = (0..key_len).map(|_| rng.random::<u8>()).collect();
            let val_len = rng.random_range(0usize..=32);
            let val: Box<[u8]> = (0..val_len).map(|_| rng.random::<u8>()).collect();
            (key, val)
        })
        .collect();

    let proof = FrozenRangeProof::new(Proof::new(start_nodes), Proof::new(end_nodes), key_values);
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    (proof, serialized)
}

/// Generates a random `FrozenChangeProof` and returns it with its serialized bytes.
fn generate_random_change_proof(rng: &SeededRng) -> (FrozenChangeProof, Vec<u8>) {
    let start_nodes: Box<[ProofNode]> = (0..rng.random_range(0usize..=5))
        .map(|_| generate_random_proof_node(rng))
        .collect();
    let end_nodes: Box<[ProofNode]> = (0..rng.random_range(0usize..=5))
        .map(|_| generate_random_proof_node(rng))
        .collect();
    let batch_ops: Box<[_]> = (0..rng.random_range(0usize..=10))
        .map(|_| {
            let key_len = rng.random_range(0usize..=32);
            let key: Box<[u8]> = (0..key_len).map(|_| rng.random::<u8>()).collect();
            match rng.random_range(0u8..3) {
                0 => {
                    let val_len = rng.random_range(0usize..=32);
                    let val: Box<[u8]> = (0..val_len).map(|_| rng.random::<u8>()).collect();
                    BatchOp::Put { key, value: val }
                }
                1 => BatchOp::Delete { key },
                _ => BatchOp::DeleteRange { prefix: key },
            }
        })
        .collect();

    let proof = FrozenChangeProof::new(Proof::new(start_nodes), Proof::new(end_nodes), batch_ops);
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    (proof, serialized)
}

#[test]
fn test_slow_range_proof_roundtrip_fuzz() {
    let rng = SeededRng::from_env_or_random();
    for i in 0..100 {
        let (proof, bytes) = generate_random_range_proof(&rng);
        debug!("iteration {i}: proof: {proof:#?}");
        debug!("iteration {i}: bytes: {}", hex::encode(&bytes));

        let parsed = FrozenRangeProof::from_slice(&bytes).expect("generated proof should be valid");
        let mut re_bytes = Vec::new();
        parsed.write_to_vec(&mut re_bytes);
        assert_eq!(bytes, re_bytes, "re-serialized bytes must match original");
    }
}

#[test]
fn test_slow_change_proof_roundtrip_fuzz() {
    let rng = SeededRng::from_env_or_random();
    for i in 0..100 {
        let (proof, bytes) = generate_random_change_proof(&rng);
        debug!("iteration {i}: proof: {proof:#?}");
        debug!("iteration {i}: bytes: {}", hex::encode(&bytes));

        let parsed =
            FrozenChangeProof::from_slice(&bytes).expect("generated proof should be valid");
        let mut re_bytes = Vec::new();
        parsed.write_to_vec(&mut re_bytes);
        assert_eq!(bytes, re_bytes, "re-serialized bytes must match original");
    }
}

#[test]
fn test_slow_malformed_proof_fuzz() {
    let rng = SeededRng::from_env_or_random();
    for i in 0..200 {
        let (_, original_bytes) = generate_random_range_proof(&rng);
        let mut data = original_bytes.clone();

        // Corrupt 1–3 bytes at random positions.
        let num_corruptions = rng.random_range(1usize..=3);
        for _ in 0..num_corruptions {
            if data.is_empty() {
                break;
            }
            let pos = rng.random_range(0..data.len());
            let old = data[pos];
            let new_val = rng.random::<u8>();
            debug!("iteration {i}: corrupted byte {pos}: {old} -> {new_val}");
            data[pos] = new_val;
        }
        debug!("iteration {i}: corrupted bytes: {}", hex::encode(&data));

        match FrozenRangeProof::from_slice(&data) {
            Err(err) => {
                debug!("iteration {i}: parse error (expected): {err}");
            }
            Ok(parsed) => {
                debug!("iteration {i}: corruption produced valid proof (checking stability)");
                // Verify idempotency: serialize(parsed) should be stable across two round-trips.
                let mut re_bytes = Vec::new();
                parsed.write_to_vec(&mut re_bytes);
                let re_parsed = FrozenRangeProof::from_slice(&re_bytes)
                    .expect("re-serialized proof should parse cleanly");
                let mut re_re_bytes = Vec::new();
                re_parsed.write_to_vec(&mut re_re_bytes);
                assert_eq!(
                    re_bytes, re_re_bytes,
                    "re-serialized proof must be idempotent"
                );
            }
        }
    }
}
