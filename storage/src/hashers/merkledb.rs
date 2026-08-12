// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 1 occurrences after enabling the lint."
)]

//! Merkledb compatible hashing algorithm.

use crate::hashednode::{HasUpdate, Hashable};
use crate::node::ExtendableBytes;
use crate::node::branch::Serializable;
use crate::{
    HashMode, HashType, MerkleDbHash, NodeHashAlgorithm, Path, TrieHash, TriePath,
    TriePathAsPackedBytes, ValueDigest,
};
use integer_encoding::VarInt;
use sha2::{Digest, Sha256};
use std::io::{Error, Read};

const MAX_VARINT_SIZE: usize = 10;
const BITS_PER_NIBBLE: u64 = 4;

impl HasUpdate for Sha256 {
    fn update<T: AsRef<[u8]>>(&mut self, data: T) {
        sha2::Digest::update(self, data);
    }
}

impl HashMode for MerkleDbHash {
    const ALGORITHM: NodeHashAlgorithm = NodeHashAlgorithm::MerkleDB;

    /// Returns the root hash of an empty MerkleDB trie.
    fn default_root_hash() -> Option<TrieHash> {
        None
    }

    /// Returns true if the nibble length is even, false otherwise.
    fn is_valid_key(key: &Path) -> bool {
        key.0.len().is_multiple_of(2)
    }

    fn to_hash<T: Hashable>(node: &T) -> HashType {
        let mut hasher = Sha256::new();

        Self::write_preimage(node, &mut hasher);
        HashType::from(TrieHash::from(hasher.finalize()))
    }

    fn write_preimage<T: Hashable>(node: &T, buf: &mut impl HasUpdate) {
        let children = node.children();

        let num_children = children.count() as u64;

        add_varint_to_buf(buf, num_children);

        for (index, hash) in &children {
            if let Some(hash) = hash {
                add_varint_to_buf(buf, u64::from(index.as_u8()));
                buf.update(hash);
            }
        }

        // Add value digest (if any) to hash pre-image
        add_value_digest_to_buf(buf, node.value_digest());

        // Add key length (in bits) to hash pre-image
        let key = node.full_path();
        let key_bit_len = BITS_PER_NIBBLE * key.len() as u64;
        add_varint_to_buf(buf, key_bit_len);
        // Add key to hash pre-image
        key.as_packed_bytes().for_each(|byte| buf.update([byte]));
    }

    fn write_child_hash<W: ExtendableBytes>(hash: &HashType, buf: &mut W) -> Result<(), Error> {
        match hash {
            HashType::Hash(h) => {
                // The merkledb child-hash format is a bare 32 bytes, identical
                // to `TrieHash::write_to`.
                h.write_to(buf);
                Ok(())
            }
            HashType::Rlp(_) => Err(Error::new(
                std::io::ErrorKind::InvalidData,
                "merkledb child hash is RLP-encoded (database corruption)",
            )),
        }
    }

    fn read_child_hash(reader: &mut impl Read) -> Result<HashType, Error> {
        // The merkledb child-hash format is a bare 32 bytes.
        Ok(HashType::from(TrieHash::from_reader(reader)?))
    }
}

fn add_value_digest_to_buf<H: HasUpdate, T: AsRef<[u8]>>(
    buf: &mut H,
    value_digest: Option<ValueDigest<T>>,
) {
    let Some(value_digest) = value_digest else {
        let value_exists: u8 = 0;
        buf.update([value_exists]);
        return;
    };

    let value_exists: u8 = 1;
    buf.update([value_exists]);

    add_len_and_value_to_buf(buf, value_digest.make_hash());
}

#[inline]
/// Writes the length of `value` and `value` to `buf`.
fn add_len_and_value_to_buf<H: HasUpdate, V: AsRef<[u8]>>(buf: &mut H, value: V) {
    let value_len = value.as_ref().len();
    buf.update([value_len as u8]);
    buf.update(value);
}

#[inline]
/// Encodes `value` as a varint and writes it to `buf`.
fn add_varint_to_buf<H: HasUpdate>(buf: &mut H, value: u64) {
    let mut buf_arr = [0u8; MAX_VARINT_SIZE];
    let len = value.encode_var(&mut buf_arr);
    buf.update(
        buf_arr
            .get(..len)
            .expect("length is always less than MAX_VARINT_SIZE"),
    );
}
