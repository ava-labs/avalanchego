// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood_storage::ValueDigest;
use integer_encoding::VarInt;

use crate::{
    proof::ProofNode,
    proofs::{bitmap::ChildrenMap, header::Header, proof_type::ProofType},
    v2::api::FrozenRangeProof,
};

impl FrozenRangeProof {
    /// Serializes this proof into the provided byte vector.
    ///
    /// # Format
    ///
    /// The V0 serialization format for a range proof is:
    ///
    #[expect(
        rustdoc::private_intra_doc_links,
        reason = "Header and ProofType are not exported"
    )]
    /// - A 32-byte [`Header`] with the proof type set to [`ProofType::Range`].
    /// - The start proof, serialized as a _sequence_ of [`ProofNode`]s
    /// - The end proof, serialized as a _sequence_ of [`ProofNode`]s
    /// - The key-value pairs, serialized as a _sequence_ of `(key, value)` tuples.
    ///
    /// Each [`ProofNode`] is serialized as:
    /// - The key, serialized as a _sequence_ of bytes where each byte is a nibble
    ///   of the appropriate branching factor. E.g., if the trie has a branching
    ///   factor of 16, each byte is only the lower 4 bits of the byte and the
    ///   upper 4 bits are all zero.
    /// - A variable-length integer indicating the length of the parent's key of
    ///   the `key`. I.e., `key[partial_len..]` is the partial path of this node.
    /// - The value digest:
    ///   - If there is no value for the node, a single byte with the value `0`.
    ///   - If there is a value for the node, a single byte with the value `1`
    ///     followed by:
    ///     - If the value digest is of a full value, a single byte with the value
    ///       `0` followed by the value serialized as a _sequence_ of bytes.
    ///     - If the value digest is of a hash, a single byte with the value `1`
    ///       followed by the hash serialized as exactly 32 bytes.
    ///     - See [`ValueDigest::make_hash`] as to when a value digest is a hash.
    /// - The children bitmap, which is a fixed-size bit field where each bit
    ///   indicates whether the corresponding child is present. The size of the
    ///   bitmap is `branching_factor / 8` bytes. E.g., if the branching factor is
    ///   16, the bitmap is 2 bytes (16 bits) and if the branching factor is 256,
    ///   the bitmap is 32 bytes (256 bits). All zero bits indicate that the node
    ///   is a leaf node and no children follow.
    /// - For each child that is present, as indicated by the bitmap, the child's
    ///   node ID is serialized.
    ///   - If the trie is using MerkleDB hashing, the ID is a fixed 32-byte
    ///     sha256 hash.
    ///   - If the trie is using Ethereum hashing, the ID is serialized as:
    ///     - A single byte with the value `0` if the ID is a keccak256 hash;
    ///       followed by the fixed 32-byte hash.
    ///     - A single byte with the value `1` if the ID is an RLP encoded node;
    ///       followed by the RLP encoded bytes serialized as a _sequence_ of
    ///       bytes. This occurs when the hash input (RLP encoded node) is smaller
    ///       than 32 bytes; in which case, the hash result is the input value
    ///       unhashed.
    ///
    /// Each _sequence_ mentioned above is prefixed with a variable-length integer
    /// indicating the number of items in the sequence.
    ///
    /// Variable-length integers are encoded using unsigned LEB128.
    pub fn write_to_vec(&self, out: &mut Vec<u8>) {
        Header::from(ProofType::Range).write_item(out);
        self.write_item(out);
    }
}

trait PushVarInt {
    fn push_var_int<VI: VarInt>(&mut self, v: VI);
}

impl PushVarInt for Vec<u8> {
    fn push_var_int<VI: VarInt>(&mut self, v: VI) {
        let mut buf = [0u8; 10];
        let n = v.encode_var(&mut buf);
        #[expect(clippy::indexing_slicing)]
        self.extend_from_slice(&buf[..n]);
    }
}

trait WriteItem {
    fn write_item(&self, out: &mut Vec<u8>);
}

impl WriteItem for FrozenRangeProof {
    fn write_item(&self, out: &mut Vec<u8>) {
        self.start_proof().write_item(out);
        self.end_proof().write_item(out);
        self.key_values().write_item(out);
    }
}

impl WriteItem for ProofNode {
    fn write_item(&self, out: &mut Vec<u8>) {
        self.key.write_item(out);
        out.push_var_int(self.partial_len);
        self.value_digest.write_item(out);
        ChildrenMap::new(&self.child_hashes).write_item(out);
        for (_, child) in self.child_hashes.iter_present() {
            child.write_item(out);
        }
    }
}

impl<T: WriteItem> WriteItem for Option<T> {
    fn write_item(&self, out: &mut Vec<u8>) {
        if let Some(v) = self {
            out.push(1);
            v.write_item(out);
        } else {
            out.push(0);
        }
    }
}

impl<T: WriteItem> WriteItem for [T] {
    fn write_item(&self, out: &mut Vec<u8>) {
        out.push_var_int(self.len());
        for item in self {
            item.write_item(out);
        }
    }
}

impl WriteItem for [u8] {
    fn write_item(&self, out: &mut Vec<u8>) {
        out.push_var_int(self.len());
        out.extend_from_slice(self);
    }
}

impl<T: AsRef<[u8]>> WriteItem for ValueDigest<T> {
    fn write_item(&self, out: &mut Vec<u8>) {
        match self.make_hash() {
            ValueDigest::Value(v) => {
                out.push(0);
                v.write_item(out);
            }
            #[cfg(not(feature = "ethhash"))]
            ValueDigest::Hash(h) => {
                out.push(1);
                h.write_item(out);
            }
        }
    }
}

impl WriteItem for Header {
    fn write_item(&self, out: &mut Vec<u8>) {
        out.extend_from_slice(bytemuck::bytes_of(self));
    }
}

impl WriteItem for firewood_storage::TrieHash {
    fn write_item(&self, out: &mut Vec<u8>) {
        out.extend_from_slice(self.as_ref());
    }
}

#[cfg(feature = "ethhash")]
impl WriteItem for firewood_storage::HashType {
    fn write_item(&self, out: &mut Vec<u8>) {
        match self {
            firewood_storage::HashType::Hash(h) => {
                out.push(0);
                h.write_item(out);
            }
            firewood_storage::HashType::Rlp(h) => {
                out.push(1);
                h.write_item(out);
            }
        }
    }
}

impl WriteItem for ChildrenMap {
    fn write_item(&self, out: &mut Vec<u8>) {
        out.extend_from_slice(bytemuck::bytes_of(self));
    }
}

impl<K: AsRef<[u8]>, V: AsRef<[u8]>> WriteItem for (K, V) {
    fn write_item(&self, out: &mut Vec<u8>) {
        let (key, value) = self;
        key.as_ref().write_item(out);
        value.as_ref().write_item(out);
    }
}
