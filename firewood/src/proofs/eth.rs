// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Ethereum MPT-RLP emission for [`ProofNode`].
//!
//! Produces the canonical Ethereum Modified Merkle Patricia Trie wire encoding
//! that an `eth_getProof` verifier expects. The native Firewood proof format
//! is its own self-describing layout; Ethereum tooling cannot consume it
//! directly. This module emits the buffers verifiers actually want.
//!
//! Always compiled. The encoding logic is the same regardless of which hash
//! algorithm built the underlying trie; when the trie was hashed with
//! SHA-256 the emitted bytes are still structurally well-formed MPT RLP but
//! won't verify under a Keccak verifier. Callers are expected to know what
//! database they opened.
//!
//! The Ethereum-account convention (treat depth-64 nodes as accounts and
//! splice the storage trie's root into the value at field index 2) only
//! fires when the `ethhash` feature is enabled. `is_account` evaluates to
//! `false` without that feature, so the storage-root splice and the
//! inline-extension override both fall through naturally and the emitter
//! produces vanilla MPT-RLP for every node.
//!
//! The bulk of the encoding mirrors [`firewood_storage::Preimage`] for the
//! ethhash hasher (see `storage/src/hashers/ethhash.rs`). Where the hasher
//! collapses encoded bytes into a hash, this module keeps the bytes so a
//! verifier can walk them.

use firewood_storage::eth_encoding::nibbles_to_eth_compact;
use firewood_storage::{
    BranchNode, Children, HashType, PathComponent, RlpItem, ValueDigest, encode_list,
    fix_account_storage_root_value,
};
use sha3::{Digest, Keccak256};
use smallvec::{SmallVec, smallvec};

use crate::proofs::types::ProofNode;

/// Path length in nibbles at which a node is treated as an Ethereum account
/// (Keccak-256 of a 20-byte address).
const ACCOUNT_DEPTH_NIBBLES: usize = 64;

/// Emit one or two canonical MPT-RLP nodes for `node`.
///
/// The emitter accepts a `ProofNode` at any depth. When the `ethhash`
/// feature is enabled, nibble depth 64 (i.e. 32 bytes, the Keccak-256 of
/// an account address) gets the Ethereum account-node convention: the
/// node's value is treated as account RLP, and the `storageRoot` field
/// (index 2) is recomputed from the node's children before encoding.
/// Without `ethhash` (or at any other depth) the value is emitted
/// verbatim. Callers walking a state-trie proof from root to leaf can
/// call this on every `ProofNode` in the path; the depth-64 special case
/// only fires on the account node itself.
///
/// Returns a single buffer for leaves and for branches with empty partial
/// paths. Returns two buffers when a firewood node combines a non-empty
/// partial path with a children-bearing branch whose RLP exceeds 31 bytes:
/// an MPT verifier needs both the extension node it walks and the branch
/// node the extension references by hash. The two buffers are returned in
/// **proof walk order** `[outer_extension, inner_branch]` so callers can
/// concatenate the result into a root-to-leaf proof list without
/// reordering. The inline-extension case (branch RLP < 32 bytes, or any
/// account node) collapses into a single self-contained buffer.
#[must_use]
pub fn proof_node_to_mpt_rlp(node: &ProofNode) -> SmallVec<[Box<[u8]>; 2]> {
    // `partial_len` is validated against `key.len()` during deserialization,
    // so the slice from `partial_len..` is always in range; the `unwrap_or`
    // is just to satisfy clippy::indexing_slicing.
    let key: &[PathComponent] = node.key.as_ref();
    let partial_path = key.get(node.partial_len..).unwrap_or(&[]);
    // The account-node concept only exists in ethhash mode. Without that
    // feature, depth-64 nodes are plain MPT nodes and no special handling
    // fires.
    let is_account = firewood_storage::NodeHashAlgorithm::compile_option().is_ethereum()
        && key.len() == ACCOUNT_DEPTH_NIBBLES;
    let value_bytes = node.value_digest.as_ref().and_then(ValueDigest::value);

    if node.child_hashes.count() == 0 {
        // Leaf. For account leaves, splice the empty-trie storage root into
        // the account RLP value; the on-disk value may carry a stale or zero
        // storageRoot on pre-hfix databases.
        let compact_path = nibbles_to_eth_compact(partial_path, true);
        let account_value: Option<Box<[u8]>> = if is_account {
            value_bytes.and_then(|bytes| {
                let empty_children: Children<Option<HashType>> = Children::new();
                fix_account_storage_root_value(bytes, &empty_children)
            })
        } else {
            None
        };

        let value_item = match (account_value.as_deref(), value_bytes) {
            (Some(updated), _) => RlpItem::Bytes(updated),
            (_, Some(raw)) => RlpItem::Bytes(raw),
            _ => RlpItem::Empty,
        };
        return smallvec![encode_list(&[RlpItem::Bytes(&compact_path), value_item])];
    }

    // Branch (has children).
    let mut items: [RlpItem<'_>; BranchNode::MAX_CHILDREN + 1] =
        [RlpItem::Empty; BranchNode::MAX_CHILDREN + 1];
    for ((_, child), slot) in (&node.child_hashes).into_iter().zip(items.iter_mut()) {
        *slot = proof_child_rlp_item(child.as_ref());
    }
    // The 17th element is the value. Account nodes carry their value in
    // account RLP that gets spliced in below, not in the branch slot.
    if !is_account && let Some(bytes) = value_bytes {
        items[BranchNode::MAX_CHILDREN] = RlpItem::Bytes(bytes);
    }
    let branch_bytes = encode_list(&items);

    // For account nodes, the state trie sees a leaf with value = account RLP.
    // Compute the storage trie's root from this node's children and splice
    // it into the account RLP value at field index 2. If the splice fails
    // (malformed account RLP), fall back to the raw account RLP rather than
    // to the value-less branch encoding; this mirrors the ethhash hasher's
    // fallback at ethhash.rs:234-236 so a verifier sees the same bytes the
    // hasher hashed.
    let inner_bytes: Box<[u8]> = if is_account {
        match value_bytes {
            Some(v) => fix_account_storage_root_value(v, &node.child_hashes)
                .unwrap_or_else(|| Box::from(v)),
            None => branch_bytes,
        }
    } else {
        branch_bytes
    };

    if partial_path.is_empty() {
        return smallvec![inner_bytes];
    }

    let compact_path = nibbles_to_eth_compact(partial_path, is_account);

    if inner_bytes.len() > 31 && !is_account {
        // Extension wraps a hashed branch. Returned in proof walk order
        // [outer, inner]: a root-to-leaf verifier first follows the parent's
        // child reference to `outer` (an extension), then follows `outer`'s
        // own child reference (keccak(inner)) to `inner`.
        let hash = Keccak256::digest(&inner_bytes);
        let outer = encode_list(&[
            RlpItem::Bytes(&compact_path),
            RlpItem::Bytes(hash.as_slice()),
        ]);
        let mut out: SmallVec<[Box<[u8]>; 2]> = SmallVec::new();
        out.push(outer);
        out.push(inner_bytes);
        out
    } else {
        // Inline form: the inner bytes fit (or this is an account, which
        // the ethhash hasher always inlines). Mirroring the hasher here:
        // see ethhash.rs:309-320.
        smallvec![encode_list(&[
            RlpItem::Bytes(&compact_path),
            RlpItem::Bytes(&inner_bytes),
        ])]
    }
}

/// Build the synthetic storage-trie leaf for the "account with exactly one
/// storage slot" case.
///
/// In Ethereum's storage trie a single entry is represented as one leaf at
/// the trie root, whose path is the slot's 32-byte Keccak-hashed key. In
/// Firewood the same data lives as a child of the account node (the account
/// is a branch with one child). To produce the same `storageHash` the
/// verifier expects, we synthesize an MPT leaf whose path is the branch
/// nibble prepended to the storage child's partial path and whose value is
/// the slot's stored bytes.
///
/// `storage_child` is the depth-65+ proof node returned by
/// `single_key_proof(account_key ++ slot_key)`. The branch nibble is the
/// first nibble of the storage child's full path past the account depth;
/// the remaining nibbles are the child's partial path; the value is the
/// child's `value_digest`.
///
/// Returns `None` if `storage_child` does not start at the account depth
/// boundary (the caller passed something other than the depth-65 storage
/// child) or has no value to embed.
///
/// Reproduces the construction at `storage/src/hashers/ethhash.rs:263-277`.
///
/// Note: the design plan named this parameter `account_node`, but
/// constructing the synthetic leaf requires the storage child's partial
/// path and value, neither of which the account `ProofNode` carries
/// (account `child_hashes` only hold hashes). Callers must pass the
/// storage child directly.
#[must_use]
pub fn synth_storage_leaf_rlp(storage_child: &ProofNode) -> Option<Box<[u8]>> {
    let full_path: &[PathComponent] = storage_child.key.as_ref();
    let leaf_path = full_path.get(ACCOUNT_DEPTH_NIBBLES..)?;
    if leaf_path.is_empty() {
        return None;
    }
    let value_bytes = storage_child.value_digest.as_ref()?.value()?;
    let compact_path = nibbles_to_eth_compact(leaf_path, true);
    Some(encode_list(&[
        RlpItem::Bytes(&compact_path),
        RlpItem::Bytes(value_bytes),
    ]))
}

/// Encode one child slot of a branch as an [`RlpItem`] for proof emission.
///
/// Distinct from `firewood_storage::nodestore::hash::child_to_rlp_item`,
/// which is account-depth-only and treats inline-RLP children as
/// `unreachable!()`. This emitter runs at any depth, so it must surface
/// inline-RLP children as `RlpItem::Raw` (verbatim inlining) for a
/// verifier to walk them.
#[cfg(feature = "ethhash")]
fn proof_child_rlp_item(child: Option<&HashType>) -> RlpItem<'_> {
    match child {
        Some(HashType::Hash(hash)) => RlpItem::Bytes(hash.as_slice()),
        Some(HashType::Rlp(rlp_bytes)) => RlpItem::Raw(rlp_bytes),
        None => RlpItem::Empty,
    }
}

#[cfg(not(feature = "ethhash"))]
fn proof_child_rlp_item(child: Option<&HashType>) -> RlpItem<'_> {
    // Without ethhash, HashType is just a 32-byte TrieHash and there is no
    // inline-RLP form; every present child is referenced by hash.
    match child {
        Some(hash) => RlpItem::Bytes(hash.as_slice()),
        None => RlpItem::Empty,
    }
}

#[cfg(test)]
#[expect(clippy::indexing_slicing, clippy::unwrap_used, clippy::cast_sign_loss)]
mod tests {
    use super::*;
    use firewood_storage::{IntoHashType, PathBuf, RlpList, TrieHash, TriePathFromUnpackedBytes};

    fn pathbuf_from_nibbles(nibbles: &[u8]) -> PathBuf {
        let components =
            <Vec<PathComponent>>::path_from_unpacked_bytes(nibbles).expect("valid nibble sequence");
        PathBuf::from(components)
    }

    fn leaf_proof_node(nibbles: &[u8], value: &[u8]) -> ProofNode {
        ProofNode {
            key: pathbuf_from_nibbles(nibbles),
            partial_len: 0,
            value_digest: Some(ValueDigest::Value(value.into())),
            child_hashes: Children::new(),
        }
    }

    #[test]
    fn leaf_emits_two_field_list() {
        let node = leaf_proof_node(&[1, 2, 3, 4], b"hello");
        let bytes = proof_node_to_mpt_rlp(&node);
        assert_eq!(bytes.len(), 1, "leaf should emit a single buffer");
        let list = RlpList::parse(&bytes[0]).expect("emitted buffer should be a valid RLP list");
        let fields = list.fields().expect("byte-string fields");
        assert_eq!(fields.len(), 2, "leaf RLP should be [path, value]");
        assert_eq!(fields[1], b"hello", "value field should round-trip");
    }

    #[test]
    fn leaf_with_no_value_emits_empty_value_slot() {
        let node = ProofNode {
            key: pathbuf_from_nibbles(&[1, 2]),
            partial_len: 0,
            value_digest: None,
            child_hashes: Children::new(),
        };
        let bytes = proof_node_to_mpt_rlp(&node);
        assert_eq!(bytes.len(), 1);
        let list = RlpList::parse(&bytes[0]).unwrap();
        let fields = list.fields().unwrap();
        assert_eq!(fields.len(), 2);
        assert!(
            fields[1].is_empty(),
            "missing value digest should produce an empty value field"
        );
    }

    #[test]
    fn synth_storage_leaf_rlp_rejects_account_depth_node() {
        // 64-nibble key has no storage portion.
        let key: Vec<u8> = (0..64).map(|i| (i % 16) as u8).collect();
        let account = leaf_proof_node(&key, b"account-rlp");
        assert!(
            synth_storage_leaf_rlp(&account).is_none(),
            "synth requires the storage child, not the account node"
        );
    }

    #[test]
    fn synth_storage_leaf_rlp_rejects_node_without_value() {
        // depth-65 node but no value.
        let key: Vec<u8> = (0..65).map(|i| (i % 16) as u8).collect();
        let node = ProofNode {
            key: pathbuf_from_nibbles(&key),
            partial_len: 64,
            value_digest: None,
            child_hashes: Children::new(),
        };
        assert!(synth_storage_leaf_rlp(&node).is_none());
    }

    fn hash_child(byte: u8) -> HashType {
        TrieHash::from([byte; 32]).into_hash_type()
    }

    #[test]
    fn branch_empty_partial_path_emits_seventeen_field_list() {
        // Branch with one 32-byte hash child at index 7, no value, empty
        // partial path. Expected output: a single 17-element RLP list.
        let mut children: Children<Option<HashType>> = Children::new();
        children.replace(PathComponent::ALL[7], Some(hash_child(0xAB)));
        let node = ProofNode {
            key: pathbuf_from_nibbles(&[]),
            partial_len: 0,
            value_digest: None,
            child_hashes: children,
        };
        let bytes = proof_node_to_mpt_rlp(&node);
        assert_eq!(bytes.len(), 1, "empty partial path emits one buffer");
        let list = RlpList::parse(&bytes[0]).unwrap();
        let fields = list.fields().unwrap();
        assert_eq!(fields.len(), 17, "branch encodes as a 17-element list");
        for (i, field) in fields.iter().enumerate().take(16) {
            if i == 7 {
                assert_eq!(field.len(), 32, "child slot 7 holds 32-byte hash");
                assert_eq!(field, &[0xABu8; 32]);
            } else {
                assert!(field.is_empty(), "child slot {i} is empty");
            }
        }
        assert!(fields[16].is_empty(), "value slot is empty");
    }

    #[test]
    fn branch_with_partial_path_long_inner_emits_two_buffers() {
        // Non-account branch with non-empty partial path; the 17-element
        // branch RLP exceeds 31 bytes, so the verifier needs both the
        // inner branch and an outer extension that hash-references it.
        let mut children: Children<Option<HashType>> = Children::new();
        children.replace(PathComponent::ALL[3], Some(hash_child(0xCD)));
        let node = ProofNode {
            key: pathbuf_from_nibbles(&[5, 6, 7]),
            partial_len: 0,
            value_digest: None,
            child_hashes: children,
        };
        let bytes = proof_node_to_mpt_rlp(&node);
        assert_eq!(bytes.len(), 2, "long-inner branch emits two buffers");

        // Buffers are returned in proof walk order: outer first, inner second.
        let outer = RlpList::parse(&bytes[0]).unwrap();
        let outer_fields = outer.fields().unwrap();
        assert_eq!(outer_fields.len(), 2, "outer is a 2-element extension");
        assert_eq!(
            outer_fields[1].len(),
            32,
            "extension's child reference is a 32-byte hash"
        );

        let inner = RlpList::parse(&bytes[1]).unwrap();
        assert_eq!(
            inner.fields().unwrap().len(),
            17,
            "inner buffer is the 17-element branch list"
        );

        // The hash referenced in the outer must equal keccak(inner).
        let inner_hash = Keccak256::digest(&bytes[1]);
        assert_eq!(outer_fields[1], inner_hash.as_slice());
    }

    #[test]
    fn synth_storage_leaf_rlp_emits_two_field_list() {
        // A storage child at depth 65 (account depth + 1 nibble): branch
        // nibble is full_path[64], the rest of the partial path is empty
        // here for simplicity.
        let mut key: Vec<u8> = (0..64).map(|i| (i % 16) as u8).collect();
        key.push(7); // branch nibble
        let child = ProofNode {
            key: pathbuf_from_nibbles(&key),
            partial_len: 64,
            value_digest: Some(ValueDigest::Value(b"slot-value".as_slice().into())),
            child_hashes: Children::new(),
        };
        let bytes = synth_storage_leaf_rlp(&child).expect("synthesized leaf");
        let list = RlpList::parse(&bytes).unwrap();
        let fields = list.fields().unwrap();
        assert_eq!(fields.len(), 2);
        assert_eq!(fields[1], b"slot-value");
        // The encoded path should be a 1-nibble odd-leaf form: 0x37 (3 marks
        // odd-leaf, 7 is the orphan nibble).
        assert_eq!(fields[0], &[0x37][..]);
    }
}
