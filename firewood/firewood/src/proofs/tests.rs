// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(clippy::unwrap_used, clippy::indexing_slicing)]

use integer_encoding::VarInt;
use test_case::test_case;

use crate::{
    proofs::{header::InvalidHeader, magic, proof_type::ProofType, reader::ReadError},
    v2::api::FrozenRangeProof,
};

fn create_valid_range_proof() -> (FrozenRangeProof, Vec<u8>) {
    let merkle = crate::merkle::tests::init_merkle((0u8..=10).map(|k| ([k], [k])));
    let proof = merkle
        .range_proof(Some(&[2u8]), Some(&[8u8]), std::num::NonZeroUsize::new(5))
        .unwrap();
    let mut serialized = Vec::new();
    proof.write_to_vec(&mut serialized);
    (proof, serialized)
}

#[test_case(
    |data| data[0..8].copy_from_slice(b"badmagic"),
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
#[cfg_attr(not(feature = "branch_factor_256"), test_case(
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
))]
fn test_incomplete_item(
    mutator: impl FnOnce(&FrozenRangeProof, &mut Vec<u8>),
    item: &'static str,
    expected_len: usize,
    found_len: usize,
) {
    let (proof, mut data) = create_valid_range_proof();

    eprintln!("data len: {}", data.len());
    eprintln!("proof: {proof:#?}");
    eprintln!("data: {}", hex::encode(&data));

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
    |_, data| data[32..42].copy_from_slice(&[0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89]),
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
