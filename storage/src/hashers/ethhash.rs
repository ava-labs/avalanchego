// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Ethereum-compatible node hashing
//!
//! This module implements the [`EthHash`] [`HashMode`] for firewood nodes so
//! that the resulting root hash matches what an Ethereum Modified Merkle
//! Patricia Trie (MPT) would produce for the same key/value set. It is always
//! compiled (both hash modes coexist in one binary); [`EthHash`] is the active
//! mode when the `ethhash` feature selects it as `DefaultHashMode` (used for
//! Avalanche C-Chain state).
//!
//! ## Why this is non-trivial
//!
//! Firewood stores a trie of (up-to-)16-ary branch nodes with partial-path
//! compression. Ethereum's MPT is conceptually the same, but the on-the-wire
//! encoding differs in several ways that must all be reproduced bit-exactly or
//! the root hash will not match:
//!
//! 1. **Hex-prefix / "compact" path encoding** — partial paths are packed with
//!    a one-byte header encoding `(is_leaf, odd_nibble_count)` plus an optional
//!    leading low nibble. See [`nibbles_to_eth_compact`].
//! 2. **Inline children (the "<32 byte" rule)** — a child whose RLP encoding is
//!    less than 32 bytes is embedded *inline* in the parent's RLP instead of
//!    being replaced by its 32-byte hash. In firewood's [`HashType`] this is
//!    the distinction between [`HashType::Hash`] and [`HashType::Rlp`].
//!    [`HashMode::to_hash`] preserves whichever form fits (`< 32` → `Rlp`, else
//!    `Hash`); branch encoders then inline `Rlp` children via
//!    [`RlpItem::Raw`] and hash-reference `Hash`
//!    children via [`RlpItem::Bytes`].
//! 3. **17-element branch lists** — a branch is always RLP-encoded as
//!    `[child_0, ..., child_15, value]`. Missing children are `0x80` (empty RLP
//!    bytes), not omitted.
//! 4. **Two-level state trie** — Ethereum's world state is a trie of accounts,
//!    where each account's value is RLP `[nonce, balance, storageRoot, codeHash]`
//!    and `storageRoot` is the root hash of a *separate* storage trie for that
//!    account. See the account-node section below.
//!
//! ## Account nodes (depth 64)
//!
//! In firewood an "account node" is any node whose full key is exactly 64
//! nibbles (= 32 bytes, the Keccak-256 of the account address). At that depth
//! the node's value is account RLP, and its *children*, if any, are the
//! storage trie for that account.
//!
//! Two things happen at account nodes that do not happen elsewhere:
//!
//! - **`storageRoot` is recomputed at hash time.** When computing a node's
//!   hash, we always derive the `storageRoot` slot from the node's children
//!   and splice it into the account RLP via
//!   [`replace_list_field`], regardless of
//!   whatever value is currently in that slot. The *persisted* bytes are not
//!   updated by this splice, so callers reading the raw value may observe a
//!   stale or zero `storageRoot` even though the trie's root hash is correct;
//!   the proof and iteration paths re-apply the fix on read via
//!   [`fix_account_storage_root_value`](crate::fix_account_storage_root_value).
//! - **The single-child "fake root" trick**. An account with exactly one
//!   storage slot looks like a firewood branch with one child, but the
//!   equivalent Ethereum storage trie is a single leaf at the *root*. To get
//!   the same hash, we conceptually prepend the child's branch nibble to its
//!   partial path and re-hash it as if it were a standalone root node. See
//!   the `children == 1` arm in [`HashMode::write_preimage`] and `hash_helper_inner`'s
//!   `fake_root_extra_nibble` in `nodestore::hash`.
//!
//! ## Where the storage root gets spliced
//!
//! There are two sites that compute and splice `storageRoot` into account
//! RLP, and they must stay in lockstep:
//!
//! 1. **At hash time**, during [`HashMode::write_preimage`]: we compute the storage-trie
//!    hash and splice it into the RLP used for the account node's own hash.
//!    This makes the account's contribution to the state root correct.
//!
//! 2. **At proof / iteration time**, in
//!    [`fix_account_storage_root_value`](crate::fix_account_storage_root_value)
//!    (`nodestore::hash`): the persisted account bytes may hold a stale or
//!    zero `storageRoot` (callers supply the bytes verbatim, and hashing does
//!    not write them back), so proof generation and key/value enumeration
//!    re-splice the correct `storageRoot` before returning.
//!
//! Both sites must produce bit-identical bytes for the same inputs or
//! proof verification will disagree with the trie's root hash.
//! `fix_account_storage_root_value` runs at proof time where all storage-trie
//! children are guaranteed to be 32-byte hashes (32-byte storage keys always
//! yield encodings ≥ 32 bytes), so it treats [`HashType::Rlp`] as
//! `unreachable!`; [`HashMode::write_preimage`] runs during hashing and must handle
//! inline [`HashType::Rlp`] children.
//!
//! ## Reviewer checklist
//!
//! When reviewing changes in this module or its callers in `nodestore::hash`:
//!
//! - Does the change alter RLP shape? If so, verify both the multi-child branch
//!   path *and* the single-child "fake root" path produce bit-identical output
//!   to geth/reth for a known fixture.
//! - Does it change where the account depth (64 nibbles) check lives? The two
//!   splice sites must agree on the definition.
//! - Does it change how [`HashType::Rlp`] vs [`HashType::Hash`] is chosen? The
//!   32-byte cutoff in [`HashMode::to_hash`] must match what parent encoders
//!   assume when emitting [`RlpItem::Raw`] vs
//!   [`RlpItem::Bytes`].
//! - Does it touch the storage-root splice? Note that Coreth may append fields
//!   beyond the standard 4, so [`replace_list_field`]
//!   only touches index 2 and leaves any trailing fields intact.

use crate::eth_encoding::nibbles_to_eth_compact;
use crate::logger::warn;
use crate::node::ExtendableBytes;
use crate::node::branch::Serializable;
use crate::rlp::{NULL_RLP, RlpItem, encode_list, replace_list_field};
use crate::{
    BranchNode, EthHash, HashMode, HashType, Hashable, NodeHashAlgorithm, Path, TrieHash, TriePath,
    ValueDigest, hashednode::HasUpdate, logger::trace,
};
use sha3::{Digest, Keccak256};
use smallvec::SmallVec;
use std::io::{Error, Read};

impl HasUpdate for Keccak256 {
    fn update<T: AsRef<[u8]>>(&mut self, data: T) {
        sha3::Digest::update(self, data);
    }
}

impl HashMode for EthHash {
    const ALGORITHM: NodeHashAlgorithm = NodeHashAlgorithm::Ethereum;

    /// Returns the hash of an empty Ethereum trie: `keccak256(0x80)`.
    fn default_root_hash() -> Option<TrieHash> {
        // keccak256(0x80): "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
        const EMPTY_RLP_HASH: [u8; 32] = [
            0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6, 0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0,
            0xf8, 0x6e, 0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0, 0x01, 0x62, 0x2f, 0xb5,
            0xe3, 0x63, 0xb4, 0x21,
        ];
        Some(EMPTY_RLP_HASH.into())
    }

    /// Returns true if the nibble length is 64 (account key) or 128 (storage-slot key).
    fn is_valid_key(key: &Path) -> bool {
        matches!(key.0.len(), 64 | 128)
    }

    fn to_hash<T: Hashable>(node: &T) -> HashType {
        // first collect the thing that would be hashed, and if it's smaller than a hash,
        // just use it directly
        let mut collector = SmallVec::with_capacity(32);
        Self::write_preimage(node, &mut collector);

        trace!(
            "SIZE WAS {} {}",
            node.full_path().len(),
            hex::encode(&collector),
        );

        if collector.len() >= 32 {
            HashType::Hash(Keccak256::digest(collector).into())
        } else {
            HashType::Rlp(collector)
        }
    }

    fn write_preimage<T: Hashable>(node: &T, buf: &mut impl HasUpdate) {
        let is_account = node.full_path().len() == 64;
        trace!("is_account: {is_account}");

        let child_hashes = node.children();

        let children = child_hashes.count();

        if children == 0 {
            // since there are no children, this must be a leaf
            // we append two items, the partial_path, encoded, and the value
            // note that leaves must always have a value, so we know there
            // will be 2 items
            let path = nibbles_to_eth_compact(node.partial_path(), true);
            // The Ethereum scheme never stores a value as a hash, so a `Hash`
            // digest is unexpected here; treat it as no value.
            let value_bytes = node.value_digest().and_then(|vd| match vd {
                ValueDigest::Value(bytes) => Some(bytes),
                ValueDigest::Hash(_) => None,
            });

            // For accounts, splice the empty-trie storage root hash into the
            // account RLP. If the splice fails (malformed value), fall back
            // to the raw value.
            let empty_root = Keccak256::digest(NULL_RLP);
            let account_value: Option<Box<[u8]>> = if is_account {
                value_bytes.and_then(|bytes| replace_list_field(bytes, 2, empty_root.as_ref()).ok())
            } else {
                None
            };

            let value_item = match (account_value.as_deref(), value_bytes) {
                (Some(updated), _) => RlpItem::Bytes(updated),
                (_, Some(bytes)) => RlpItem::Bytes(bytes),
                _ => RlpItem::Empty,
            };

            let bytes = encode_list(&[RlpItem::Bytes(&path), value_item]);
            trace!("partial path {:?}", node.partial_path().display());
            trace!("serialized leaf-rlp: {:?}", hex::encode(&bytes));
            buf.update(&bytes);
        } else {
            // for a branch, there are always 16 children and a value
            // Child::None we encode as RLP empty_data (0x80)
            let mut items: [RlpItem<'_>; BranchNode::MAX_CHILDREN + 1] =
                [RlpItem::Empty; BranchNode::MAX_CHILDREN + 1];
            for ((_, child), slot) in (&child_hashes).into_iter().zip(items.iter_mut()) {
                *slot = match child {
                    Some(HashType::Hash(hash)) => RlpItem::Bytes(hash.as_slice()),
                    Some(HashType::Rlp(rlp_bytes)) => RlpItem::Raw(rlp_bytes),
                    None => RlpItem::Empty,
                };
            }

            // For branch nodes, the 17th element is the value.
            // Account nodes (depth 32) handle values differently — the value
            // lives in the account RLP, not directly in the branch — so we
            // emit Empty here and splice it in below.
            if !is_account && let Some(ValueDigest::Value(digest)) = node.value_digest() {
                items[BranchNode::MAX_CHILDREN] = RlpItem::Bytes(digest);
            }
            let bytes = encode_list(&items);
            trace!("pass 1 bytes {:02X?}", hex::encode(&bytes));

            // we've collected all the children in bytes

            let updated_bytes: Box<[u8]> = if is_account {
                // need to get the value again
                if let Some(ValueDigest::Value(rlp_encoded_bytes)) = node.value_digest() {
                    // rlp_encoded_bytes needs to be decoded
                    // TODO(rkuris): Handle corruption
                    // needs to be the hash of the RLP encoding of the root node that
                    // would have existed here (instead of this account node)
                    // the "root node" is actually this branch node iff there is
                    // more than one child. If there is only one child, then the
                    // child is actually the root node, so we need the hash of that
                    // child here.
                    let replacement_hash = if children == 1 {
                        // we need to treat this child like it's a root node, so the partial path is
                        // actually one longer than it is reported
                        match child_hashes
                            .iter()
                            .find_map(|(_, c)| c.as_ref())
                            .expect("we know there is exactly one child")
                        {
                            HashType::Hash(hash) => hash.clone(),
                            HashType::Rlp(rlp_bytes) => {
                                let path = nibbles_to_eth_compact(node.partial_path(), true);
                                let bytes =
                                    encode_list(&[RlpItem::Bytes(&path), RlpItem::Raw(rlp_bytes)]);
                                TrieHash::from(Keccak256::digest(bytes))
                            }
                        }
                    } else {
                        TrieHash::from(Keccak256::digest(&bytes))
                    };
                    trace!("replacement hash {:?}", hex::encode(&replacement_hash));

                    let updated =
                        replace_list_field(rlp_encoded_bytes, 2, replacement_hash.as_ref())
                            .unwrap_or_else(|_| Box::from(rlp_encoded_bytes));
                    trace!("updated encoded value {:02X?}", hex::encode(&updated));
                    updated
                } else {
                    // treat like non-account since it didn't have a value
                    warn!(
                        "Account node {:x?} without value",
                        node.full_path().display(),
                    );
                    bytes
                }
            } else {
                bytes
            };

            let partial_path = node.partial_path();
            if partial_path.is_empty() {
                trace!("pass 2=bytes {:02X?}", hex::encode(&updated_bytes));
                buf.update(&updated_bytes);
            } else {
                let path = nibbles_to_eth_compact(partial_path, is_account);
                // if the RLP is short enough, we can use it as-is, otherwise we hash it
                // to make the maximum length 32 bytes
                let value_item = if updated_bytes.len() > 31 && !is_account {
                    let hashed_bytes = Keccak256::digest(&updated_bytes);
                    let final_bytes = encode_list(&[
                        RlpItem::Bytes(&path),
                        RlpItem::Bytes(hashed_bytes.as_ref()),
                    ]);
                    trace!("pass 2 bytes {:02X?}", hex::encode(&final_bytes));
                    buf.update(&final_bytes);
                    return;
                } else {
                    RlpItem::Bytes(&updated_bytes)
                };
                let final_bytes = encode_list(&[RlpItem::Bytes(&path), value_item]);
                trace!("pass 2 bytes {:02X?}", hex::encode(&final_bytes));
                buf.update(&final_bytes);
            }
        }
    }

    fn write_child_hash<W: ExtendableBytes>(hash: &HashType, buf: &mut W) -> Result<(), Error> {
        // Delegate to the existing `HashOrRlp` codec so the on-disk bytes stay
        // byte-for-byte frozen: `0x00` + 32 bytes for a full hash, or
        // `len` + RLP bytes for an inline-RLP child.
        HashType::write_to(hash, buf);
        Ok(())
    }

    fn read_child_hash(reader: &mut impl Read) -> Result<HashType, Error> {
        HashType::from_reader(reader)
    }
}
