// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Cryptographic proof system for Merkle tries.
//!
//! This module provides a complete proof system for verifying the presence or absence
//! of key-value pairs in Firewood's Merkle trie implementation without requiring access
//! to the entire trie structure. The proof system enables efficient verification of trie
//! state using cryptographic hashes.
//!
//! # Overview
//!
//! Firewood's proof system consists of several key components:
//!
//! - **Single-key proofs** ([`Proof`]): Verify that a specific key-value pair exists
//!   (or doesn't exist) in a trie with a given root hash.
//! - **Range proofs** ([`RangeProof`]): Verify that a contiguous set of key-value pairs
//!   exists within a specific key range.
//! - **Change proofs** ([`ChangeProof`]): Verify that a set of key-value changes
//!   between two trie revisions is consistent with a target root hash.
//! - **Serialization format**: A compact binary format for transmitting proofs over the
//!   network or storing them persistently.
//!
//! # Architecture
//!
//! The proof system is organized into several submodules:
//!
//! - `types`: Core proof types including [`Proof`], [`ProofNode`], [`ProofError`], and
//!   [`ProofCollection`].
//! - `range`: Range proof implementation for verifying multiple consecutive keys.
//! - `change`: Change proof type and structural verification.
//! - `header`: Proof format headers and validation.
//! - `reader`: Proof reading and deserialization utilities.
//! - `ser`: Proof serialization implementation (internal).
//! - `de`: Proof deserialization implementation (internal).
//! - `childmask` (in `merkle`): Compact bitmap for tracking present children.
//! - `magic`: Magic constants for proof format identification (internal).
//!
//! # Proof Format
//!
//! Proofs are serialized in a compact binary format that includes:
//!
//! 1. A 32-byte header identifying the proof type, version, hash mode, and branching factor
//! 2. A sequence of proof nodes, each containing:
//!    - The node's key path (variable length)
//!    - The node's value or value hash (if present)
//!    - A bitmap indicating which children are present
//!    - The hash of each present child
//!
//! Change proofs additionally serialize their `batch_ops` as a sequence of tagged
//! Put/Delete operations after the boundary proof nodes. See `ser.rs` for details.
//!
//! The serialization format is versioned to allow for future evolution while maintaining
//! backward compatibility with proof verification.
//!
//! # Range Proof Verification Algorithm
//!
//! Range proof verification confirms that a contiguous set of key-value pairs
//! exists within a specific key range of a trie with a given root hash.
//!
//! [`verify_range_proof`] proceeds in two phases:
//!
//! ## Phase 1 — Structural and boundary validation
//!
//! Validates that key-value pairs are strictly ascending and within the
//! requested `[first_key, last_key]` range. Proof nodes carrying in-range
//! values must appear in the key-value list (prevents an attacker from
//! hiding keys that sit on a proof path). Start and end boundary proofs
//! are verified against `root_hash` via hash chain checks.
//!
//! ## Phase 2 — Root hash verification
//!
//! A fresh in-memory **proving trie** is built from the key-value pairs.
//! Boundary proof nodes are reconciled into it via
//! `reconcile_branch_proof_node`, which inserts branch structure matching
//! the original trie's layout. Value mismatches between the proof and the
//! **proving trie** cause early rejection (see Step 2 of the change proof
//! algorithm for details on value conflict handling).
//! `compute_root_hash_with_proofs` then computes a **hybrid root hash**:
//! in-range children are hashed from the **proving trie**, while
//! out-of-range children (identified by `compute_outside_children`) use
//! hashes from the proof nodes. The result must match `root_hash`.
//!
//! # Change Proof Verification Algorithm
//!
//! Change proof verification confirms that applying a set of `BatchOp`s to a trie
//! with `start_root` produces a trie consistent with `end_root`, without requiring
//! the verifier to hold the full `end_root` trie.
//!
//! ## Terminology
//!
//! - **requested_start_key / requested_end_key**: The key range bounds
//!   passed to the proof generator.
//!
//! - **proof_key_values**: The key/value pairs included in the proof (the
//!   `batch_ops`), including `first_proof_key` and `last_proof_key`.
//!
//! - **first_proof_key / last_proof_key**: First and last keys within
//!   `proof_key_values`.
//!
//! - **left_edge_proof / right_edge_proof**: Merkle inclusion/exclusion
//!   proofs anchoring the left and right boundaries of the proof to the
//!   root hash.
//!
//! - **left_edge_proof_key / right_edge_proof_key**: The key each edge
//!   proof was generated for. These may or may not coincide with
//!   `requested_start_key` / `requested_end_key`.
//!
//! - **proving trie**: A mutable fork of the proposal used to verify
//!   the root hash. It is reshaped to match `end_root`'s boundary
//!   structure so that computing its hybrid root hash reproduces
//!   `end_root` when the `batch_ops` are correct.
//!
//! - **hybrid root hash**: A root hash computed by combining two sources:
//!   in-range children are hashed directly from the **proving trie**, while
//!   out-of-range children use hashes provided by the boundary proof
//!   nodes. This allows verification without the full trie.
//!
//! ```text
//! Keyspace layout diagram
//!
//! Along the main line, "x" represents an exclusion proof and
//! "i" represents an inclusion proof. No proofs are provided at
//! points indicated with a "*"
//!
//!     left_edge_proof_key                  right_edge_proof_key
//!     (possible locations)                 (possible locations)
//!       |    |   |   |                        |   |   |     |
//!       v    v   v   v                        v   v   v     v
//!   |---x----x---x---i----------*-*-----------i---x---x-----x---|
//!   ^        ^       ^          ^ ^           ^       ^         ^
//!   |        |       |          | |           |       |         |
//!  0x00      |  first_proof_key | |    last_proof_key |       0xff..
//!            |       ^          | |           ^       |     
//!         requested_start_key   | |        requested_end_key
//!       (2 possible locations)  | |        (2 possible locations)
//!                               | |
//!                     other proof_key_values
//! ```
//!
//! The `left_edge_proof_key` can land at any of the four positions
//! shown. Only when it coincides with `requested_start_key` (the `i`
//! position) is the left edge proof an inclusion proof; all other
//! positions produce exclusion proofs. The symmetric property holds
//! for `right_edge_proof_key` on the right side.
//!
//! Note: the diagram shows the general case with two edge proofs and
//! multiple keys. Edge cases include: a single proof key (where
//! `first_proof_key` = `last_proof_key`), empty `batch_ops` (no proof
//! keys), or absent edge proofs (when the range covers the entire
//! keyspace).
//!
//! Note: `requested_start_key` and `right_edge_key` (which equals
//! `requested_end_key` unless the proof was truncated) are used during
//! root hash verification (Phase 3) to distinguish in-range from
//! out-of-range nodes. This determines which proof node values to
//! adopt during branch expansion and which children to check during
//! branch collapsing.
//!
//! ## Verification phases
//!
//! Verification proceeds in three phases:
//!
//! ## Phase 1 — Structural validation ([`verify_change_proof_structure`])
//!
//! Validates the proof's internal consistency: key ordering, range bounds,
//! absence of `DeleteRange` ops, and boundary proof hash chain verification
//! against `end_root`. The start proof anchors the left edge of the proven
//! range; the end proof anchors the right edge. Both are standard Merkle
//! inclusion/exclusion proofs.
//!
//! ## Phase 2 — Apply batch ops
//!
//! The verifier applies the proof's `batch_ops` to its own copy of
//! `start_root`, producing a proposal. This is the same commit path used
//! for normal trie operations. The proposal now contains `start_root`'s
//! full trie structure with the batch_ops' changes applied on top.
//!
//! ## Phase 3 — Root hash verification ([`verify_change_proof_root_hash`])
//!
//! The verifier forks the proposal into a mutable **proving trie**,
//! reshapes it to match `end_root`'s boundary structure, and computes a
//! hybrid root hash. If the hash matches `end_root`, the batch_ops are
//! consistent with the claimed state transition.
//!
//! The verification proceeds in four steps:
//!
//! ### Step 1 — Fork the proposal
//!
//! The proposal's nodestore (which contains `start_root` + `batch_ops`)
//! is forked into a mutable nodestore — the **proving trie**. The fork is
//! mutable because the subsequent steps will reshape it to match the
//! start and end boundary proof structures. The **proving trie** starts
//! with the full trie including both in-range data (from `batch_ops`)
//! and out-of-range data (inherited from `start_root`).
//!
//! ### Step 2 — Branch expansion
//!
//! `reconcile_branch_proof_node` ensures branch nodes exist at every
//! proof-path position with structure matching `end_root`. This expands
//! the **proving trie** by inserting any branch nodes that the boundary
//! proofs require but that may not exist in the proposal.
//!
//! Out-of-range proof nodes (proper prefixes of the boundary keys, or
//! divergent nodes in exclusion proofs) may carry values from `end_root`
//! that differ from the proposal (which has `start_root`'s values
//! outside the range). The proof's value is adopted for these positions
//! so the hash computation reflects `end_root`.
//!
//! For exclusion proofs, the terminal proof node may overshoot the
//! boundary key to the nearest existing key. Such nodes are in-range
//! (>= `requested_start_key` for start proofs, <= `requested_end_key`
//! for end proofs) and
//! value conflicts there are real errors — the proposal already has
//! the correct value from the batch_ops.
//!
//! ### Step 3 — Branch collapsing
//!
//! Between consecutive proof nodes in `end_root`, the path is direct —
//! no intermediate branches exist (if they did, those nodes would
//! themselves be proof nodes). The **proving trie**, forked from the
//! proposal, may have extra branch structure from out-of-range keys at
//! these intermediate positions.
//!
//! `collapse_branch_to_path` walks from each parent proof node to its
//! child proof node and strips non-on-path children at intermediate
//! nodes, then flattens single-child branches so the trie shape matches
//! `end_root`'s path-compressed structure.
//!
//! **Range-safety invariant**: before stripping a child, the collapse
//! checks whether the child's nibble prefix falls within
//! `[requested_start_key, right_edge_key]`. An in-range child at an
//! intermediate position cannot
//! exist in `end_root` (there is no branch here), so its presence
//! indicates tampered `batch_ops` — either a key was illegitimately
//! created (e.g., changing a Delete to a Put) or a required Delete was
//! omitted. The proof is rejected with `EndRootMismatch`.
//!
//! Out-of-range children are stripped normally — they represent
//! `start_root` structure that doesn't exist in `end_root` and will be
//! accounted for by the proof's boundary hashes.
//!
//! `collapse_root_to_path` applies the same logic to the root itself,
//! handling cases where out-of-range deletions caused `end_root`'s root
//! to path-compress (e.g., root partial_path changes from `[]` to
//! `[1]`).
//!
//! ### Step 4 — Hybrid hash computation
//!
//! `compute_outside_children` determines which children at each
//! boundary proof node fall outside the proven range (left of
//! `requested_start_key` or right of `right_edge_key`).
//!
//! `compute_root_hash_with_proofs` recursively walks the **proving trie**:
//!
//! - **In-range children** (not in the outside mask): hashed from the
//!   **proving trie**. Persisted children already carry their hash;
//!   in-memory children are hashed recursively.
//! - **Out-of-range children** (in the outside mask): substituted with
//!   hashes from the corresponding proof node's `child_hashes`.
//!
//! The resulting root hash is compared against `end_root`. A mismatch
//! means the batch_ops are inconsistent with the claimed state
//! transition.
//!
//! # Range vs. Change Proof Verification
//!
//! Both algorithms build a **proving trie**, expand boundary proof nodes
//! into it, and compute a hybrid root hash. They differ in three ways:
//!
//! 1. **Proving trie origin** — Range proofs build a fresh in-memory trie
//!    from the key-value pairs provided in the proof. Change proofs fork
//!    `start_root`'s nodestore, which contains the full trie (in-range
//!    *and* out-of-range keys).
//!
//! 2. **Branch collapsing** — Change proofs require a collapse step to
//!    strip `start_root`'s out-of-range branch structure that doesn't
//!    exist in `end_root`. Range proofs skip this — the **proving trie** was
//!    built from only in-range keys, so no extra branches exist.
//!
//! 3. **Out-of-range value trust** — Range proofs unconditionally accept
//!    the proof's value when it conflicts with the **proving trie** (the trie
//!    has no out-of-range data to preserve). Change proofs are selective:
//!    out-of-range ancestors adopt the proof's value (reflecting
//!    `end_root`), but in-range positions keep `start_root`'s value since
//!    the batch_ops will update them.

pub(crate) mod change;
pub(super) mod de;
pub mod eth;
pub(crate) mod header;
pub(crate) mod range;
pub(crate) mod reader;
pub(super) mod ser;
#[cfg(test)]
mod tests;
pub(crate) mod types;

pub use self::change::{
    ChangeProof, ChangeProofVerificationContext, find_next_key_after_change_proof,
    verify_change_proof_structure,
};

pub use self::header::InvalidHeader;
pub use self::range::{
    KeyRange, RangeProof, RangeProofVerificationContext, find_next_key_after_range_proof,
    verify_range_proof_structure,
};
pub use self::reader::ReadError;
pub use self::types::{
    EmptyProofCollection, Proof, ProofCollection, ProofEdge, ProofError, ProofNode, ProofType,
};

pub(super) mod magic {
    //! Magic constants for proof format identification.
    //!
    //! These constants are used in proof headers to identify the proof format,
    //! version, hash mode, and branching factor. They enable proof readers to
    //! quickly validate that a proof is compatible with the current implementation.

    /// Magic header bytes identifying a Firewood proof: `b"fwdproof"`
    pub const PROOF_HEADER: &[u8; 8] = b"fwdproof";

    /// Current proof format version: `0`
    pub const PROOF_VERSION: u8 = 0;

    /// Hash mode identifier for SHA-256 hashing
    #[cfg(not(feature = "ethhash"))]
    pub const HASH_MODE: u8 = 0;

    /// Hash mode identifier for Keccak-256 hashing (Ethereum-compatible)
    #[cfg(feature = "ethhash")]
    pub const HASH_MODE: u8 = 1;

    /// Returns the human-readable name for a hash mode identifier.
    pub const fn hash_mode_name(v: u8) -> &'static str {
        match v {
            0 => "sha256",
            1 => "keccak256",
            _ => "unknown",
        }
    }

    /// Branching factor identifier for branch factor 16
    pub const BRANCH_FACTOR: u8 = 16;

    /// `BatchOp` constants for serialization
    pub const BATCH_PUT: u8 = 0;
    pub const BATCH_DELETE: u8 = 1;
    pub const BATCH_DELETE_RANGE: u8 = 2;
}
