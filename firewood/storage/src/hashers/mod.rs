// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Node preimage hashing
//!
//! This module provides two mutually-exclusive implementations of
//! [`Preimage`](crate::Preimage) (and therefore [`to_hash`](crate::Preimage::to_hash))
//! for trie nodes. Exactly one is compiled in based on feature flags:
//!
//! | Feature           | Module     | Hash       | Compatibility           |
//! |-------------------|------------|------------|-------------------------|
//! | default (off)     | `merkledb` | SHA-256    | Avalanche `merkledb`    |
//! | `ethhash`         | `ethhash`  | Keccak-256 | Ethereum MPT / C-Chain  |
//!
//! The two encodings are **not interchangeable**: a database created under one
//! set of flags cannot be read with the other. Root hashes, proofs, and on-disk
//! node encodings all differ.
//!
//! ## What these modules produce
//!
//! `merkledb` is a straightforward length-prefixed encoding of
//! `(num_children, children, value_digest, key_bit_len, packed_key)` hashed with
//! SHA-256. There are no special cases and no account semantics.
//!
//! `ethhash` reproduces the Ethereum Modified Merkle Patricia
//! Trie (MPT) wire format exactly, including hex-prefix compact path encoding,
//! 17-element branch lists, inline-vs-hashed child references (the "<32 byte"
//! rule), and the two-level state trie where account-depth nodes embed a
//! recursive storage-trie hash. See the `ethhash` module docs for the full
//! picture — it is considerably more involved than `merkledb`.
//!
//! ## Where to look
//!
//! - Hashing the current revision: `ethhash::Preimage::write` /
//!   `merkledb::Preimage::write`, via
//!   [`NodeStore::hash_helper`](crate::NodeStore::hash_helper) in
//!   `nodestore::hash`.
//! - Post-hash fixups for account `storageRoot`:
//!   `fix_account_storage_root_value` in `nodestore::hash` (proof-generation
//!   path; only compiled with `ethhash`).

#[cfg(feature = "ethhash")]
pub(crate) mod ethhash;
#[cfg(not(feature = "ethhash"))]
mod merkledb;
