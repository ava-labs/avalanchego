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

use std::io::{Error, ErrorKind};

use firewood_storage::eth_encoding::nibbles_to_eth_compact;
use firewood_storage::{
    BranchNode, Children, HashType, PathComponent, RlpError, RlpItem, RlpList, ValueDigest,
    encode_list, fix_account_storage_root_value, parse_be_uint, parse_fixed,
};
use sha3::{Digest, Keccak256};
use smallvec::{SmallVec, smallvec};

use crate::api;
use crate::proofs::types::ProofNode;

/// Path length in nibbles at which a node is treated as an Ethereum account
/// (Keccak-256 of a 20-byte address).
pub(crate) const ACCOUNT_DEPTH_NIBBLES: usize = 64;

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
        // storageRoot on pre-hfix databases. We always splice rather than
        // conditioning on the source DB version: the keccak cost is
        // negligible next to the trie-walk I/O, and threading an hfix flag
        // down to the emitter isn't worth the API surface.
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
    for ((_, child), slot) in node.child_hashes.iter().zip(items.iter_mut()) {
        *slot = proof_child_rlp_item(child);
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
            Some(v) => fix_account_storage_root_value(v, &Children::from(&node.child_hashes))
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

/// Emit the byte-string whose Keccak-256 equals an account's `storageRoot`
/// field for the multi-storage-child case.
///
/// In firewood the account node and its storage trie root are the *same*
/// node — `child_hashes` are the storage trie's children. The canonical
/// MPT encoding of that root is a 17-element branch with no value slot,
/// and its keccak is what [`fix_account_storage_root_value`] splices into
/// the account leaf.
///
/// # Returns
///
/// `Some(bytes)` when `account_node` has two or more storage children;
/// `None` otherwise. Callers handle the other shapes separately: no
/// storage → empty proof; a single child nibble → the account's lone child
/// node is itself the storage root and is re-encoded directly.
#[must_use]
pub fn account_storage_root_rlp(account_node: &ProofNode) -> Option<Box<[u8]>> {
    if account_node.child_hashes.count() < 2 {
        return None;
    }
    let mut items: [RlpItem<'_>; BranchNode::MAX_CHILDREN + 1] =
        [RlpItem::Empty; BranchNode::MAX_CHILDREN + 1];
    for ((_, child), slot) in account_node.child_hashes.iter().zip(items.iter_mut()) {
        *slot = proof_child_rlp_item(child);
    }
    // The 17th slot stays empty: at the storage trie root there is no value.
    Some(encode_list(&items))
}

/// Structured errors raised while decoding an account RLP value. Wrapped
/// into [`api::Error::IO`] with [`ErrorKind::InvalidData`] at the public
/// boundary so the decode detail survives `Display` (the `InternalError`
/// variant's `Display` is the bare "Internal error" and would drop it).
#[derive(Debug, thiserror::Error)]
enum AccountDecodeError {
    #[error("malformed account RLP: {0}")]
    Rlp(#[from] RlpError),
    #[error("account RLP has {0} fields, expected at least 4")]
    FieldCount(usize),
    #[error("account field {0}: {1}")]
    Field(&'static str, RlpError),
}

impl From<AccountDecodeError> for api::Error {
    fn from(e: AccountDecodeError) -> Self {
        Error::new(ErrorKind::InvalidData, e).into()
    }
}

/// Decoded fields of an ethereum account RLP value.
///
/// `balance` is zero-padded big-endian to fit the JSON-RPC `quantity` shape
/// without an extra allocation. `storage_root` is the storage trie root as
/// encoded in the account leaf, after firewood's hashing fixup recomputes
/// it from the account's storage children.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AccountFields {
    /// Transaction count for this account.
    pub nonce: u64,
    /// Account balance, zero-padded big-endian.
    pub balance: [u8; 32],
    /// Storage trie root recorded in the account leaf.
    pub storage_root: [u8; 32],
    /// Keccak-256 of the account's contract code (or empty-code hash).
    pub code_hash: [u8; 32],
}

impl AccountFields {
    /// Parse an ethereum account RLP value (`[nonce, balance, storageRoot, codeHash]`).
    ///
    /// # Errors
    ///
    /// Returns [`api::Error::IO`] (kind `InvalidData`) if the RLP is
    /// malformed, the field count is wrong, `nonce` is more than 8 bytes,
    /// `balance` is more than 32 bytes, or either hash field is not exactly
    /// 32 bytes.
    pub fn from_rlp(rlp_bytes: &[u8]) -> Result<Self, api::Error> {
        Self::from_rlp_inner(rlp_bytes).map_err(Into::into)
    }

    fn from_rlp_inner(rlp_bytes: &[u8]) -> Result<Self, AccountDecodeError> {
        let list = RlpList::parse(rlp_bytes)?;
        let fields = list.fields()?;
        // Coreth emits 5-item account RLP with a trailing empty byte
        // (see firewood/src/merkle/tests/ethhash.rs:267-270); accept any
        // list with at least the four standard fields and ignore extras.
        let &[nonce_bytes, balance_bytes, storage_bytes, code_bytes] = fields
            .first_chunk::<4>()
            .ok_or(AccountDecodeError::FieldCount(fields.len()))?;

        let field = |name: &'static str| move |e| AccountDecodeError::Field(name, e);

        Ok(Self {
            nonce: u64::from_be_bytes(parse_be_uint::<8>(nonce_bytes).map_err(field("nonce"))?),
            balance: parse_be_uint::<32>(balance_bytes).map_err(field("balance"))?,
            storage_root: parse_fixed::<32>(storage_bytes).map_err(field("storageRoot"))?,
            code_hash: parse_fixed::<32>(code_bytes).map_err(field("codeHash"))?,
        })
    }
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
mod tests {
    use super::*;
    use firewood_storage::{
        DenseChildren, IntoHashType, PathBuf, RlpList, TrieHash, TriePathFromUnpackedBytes,
    };

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
            child_hashes: DenseChildren::new(),
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
            child_hashes: DenseChildren::new(),
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

    fn hash_child(byte: u8) -> HashType {
        TrieHash::from([byte; 32]).into_hash_type()
    }

    #[test]
    fn branch_empty_partial_path_emits_seventeen_field_list() {
        // Branch with one 32-byte hash child at index 7, no value, empty
        // partial path. Expected output: a single 17-element RLP list.
        let mut children = DenseChildren::new();
        children.insert(PathComponent::ALL[7].0, hash_child(0xAB));
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
        let mut children = DenseChildren::new();
        children.insert(PathComponent::ALL[3].0, hash_child(0xCD));
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

    fn build_account_rlp(
        nonce: &[u8],
        balance: &[u8],
        storage_root: &[u8; 32],
        code_hash: &[u8; 32],
    ) -> Box<[u8]> {
        firewood_storage::encode_list(&[
            RlpItem::Bytes(nonce),
            RlpItem::Bytes(balance),
            RlpItem::Bytes(storage_root),
            RlpItem::Bytes(code_hash),
        ])
    }

    #[test]
    fn decode_account_round_trip() {
        let storage = [0x11u8; 32];
        let code = [0x22u8; 32];
        let bytes = build_account_rlp(&[0x42], &[0x05, 0x00], &storage, &code);
        let fields = AccountFields::from_rlp(&bytes).unwrap();
        assert_eq!(fields.nonce, 0x42);
        assert_eq!(fields.balance[30..], [0x05, 0x00]);
        assert!(fields.balance[..30].iter().all(|&b| b == 0));
        assert_eq!(fields.storage_root, storage);
        assert_eq!(fields.code_hash, code);
    }

    #[test]
    fn decode_account_zero_nonce_zero_balance() {
        let storage = [0u8; 32];
        let code = [0u8; 32];
        // Per RLP minimal encoding, 0 is the empty byte string.
        let bytes = build_account_rlp(&[], &[], &storage, &code);
        let fields = AccountFields::from_rlp(&bytes).unwrap();
        assert_eq!(fields.nonce, 0);
        assert_eq!(fields.balance, [0u8; 32]);
    }

    #[test]
    fn decode_account_rejects_too_few_fields() {
        let bytes = firewood_storage::encode_list(&[
            RlpItem::Bytes(&[0x01]),
            RlpItem::Bytes(&[0x02]),
            RlpItem::Bytes(&[0x03]),
        ]);
        match AccountFields::from_rlp_inner(&bytes) {
            Err(AccountDecodeError::FieldCount(3)) => {}
            other => panic!("expected FieldCount(3), got {other:?}"),
        }
    }

    #[test]
    fn decode_account_accepts_coreth_five_item_shape() {
        // Coreth emits a 5-item account RLP: [nonce, balance, storageRoot,
        // codeHash, trailing-empty]. Extras past field 3 must be ignored.
        let storage = [0x11u8; 32];
        let code = [0x22u8; 32];
        let bytes = firewood_storage::encode_list(&[
            RlpItem::Bytes(&[0x42]),
            RlpItem::Bytes(&[0x05]),
            RlpItem::Bytes(&storage),
            RlpItem::Bytes(&code),
            RlpItem::Empty,
        ]);
        let fields = AccountFields::from_rlp(&bytes).unwrap();
        assert_eq!(fields.nonce, 0x42);
        assert_eq!(fields.storage_root, storage);
        assert_eq!(fields.code_hash, code);
    }

    #[test]
    fn decode_account_rejects_oversized_nonce() {
        let storage = [0u8; 32];
        let code = [0u8; 32];
        let bytes = build_account_rlp(&[1u8; 9], &[], &storage, &code);
        match AccountFields::from_rlp_inner(&bytes) {
            Err(AccountDecodeError::Field("nonce", RlpError::TooLong { actual: 9, max: 8 })) => {}
            other => panic!("expected nonce TooLong, got {other:?}"),
        }
    }

    #[test]
    fn decode_account_rejects_short_storage_root() {
        let code = [0u8; 32];
        let bytes = firewood_storage::encode_list(&[
            RlpItem::Bytes(&[]),
            RlpItem::Bytes(&[]),
            RlpItem::Bytes(&[0u8; 31]),
            RlpItem::Bytes(&code),
        ]);
        match AccountFields::from_rlp_inner(&bytes) {
            Err(AccountDecodeError::Field(
                "storageRoot",
                RlpError::WrongLength {
                    actual: 31,
                    expected: 32,
                },
            )) => {}
            other => panic!("expected storageRoot WrongLength, got {other:?}"),
        }
    }

    #[test]
    fn decode_account_public_wraps_invalid_data() {
        // The public API surface returns api::Error::IO(InvalidData), whose
        // Display carries the decode detail through (InternalError's would
        // drop it).
        let bytes = firewood_storage::encode_list(&[
            RlpItem::Bytes(&[0x01]),
            RlpItem::Bytes(&[0x02]),
            RlpItem::Bytes(&[0x03]),
        ]);
        let err = AccountFields::from_rlp(&bytes).expect_err("3 fields should fail");
        assert!(
            matches!(&err, api::Error::IO(e) if e.kind() == ErrorKind::InvalidData),
            "expected IO(InvalidData), got {err:?}"
        );
        assert!(
            err.to_string()
                .contains("account RLP has 3 fields, expected at least 4"),
            "decode detail must survive Display, got {err}"
        );
    }
}
