// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Core types for Merkle trie proofs.
//!
//! This module defines the fundamental types used to construct, verify, and represent
//! cryptographic proofs in Firewood's Merkle trie implementation. Proofs allow efficient
//! verification that specific key-value pairs exist (or don't exist) in a trie without
//! requiring access to the entire trie structure.
//!
//! # Key Types
//!
//! - [`Proof`]: A generic wrapper around a collection of proof nodes that can verify
//!   the presence or absence of a key-value pair in a trie with a given root hash.
//! - [`ProofNode`]: Represents a single node in a proof path, containing the node's
//!   key, value digest, and child hashes necessary for verification.
//! - [`ProofError`]: Comprehensive error type describing all possible proof validation
//!   failures.
//! - [`ProofCollection`]: Trait for collections that can store proof nodes, enabling
//!   flexible proof representations (e.g., `Vec<ProofNode>`, `Box<[ProofNode]>`).
//! - [`ProofType`]: Internal enum identifying the type of serialized proof (single,
//!   range, or change proof).
//!
//! # Proof Verification
//!
//! Proofs work by providing a path from the root to a target node, where each node
//! includes cryptographic hashes of its children. This allows verification that:
//! 1. The path is valid (each hash matches the computed hash of the next node)
//! 2. The path leads to the claimed key-value pair (inclusion proof)
//! 3. No such key exists (exclusion proof)
//!
//! # Examples
//!
//! ```rust,ignore
//! use firewood::Proof;
//!
//! // Verify that a key-value pair exists in the trie
//! let proof: Proof<Vec<ProofNode>> = /* ... */;
//! proof.verify(b"key", Some(b"value"), &root_hash)?;
//!
//! // Verify that a key does not exist (exclusion proof)
//! proof.verify(b"missing_key", None, &root_hash)?;
//! ```

#![expect(
    clippy::missing_errors_doc,
    reason = "Found 1 occurrences after enabling the lint."
)]

use firewood_storage::{
    Children, FileIoError, HashType, Hashable, IntoHashType, IntoSplitPath, NibblesIterator, Path,
    PathBuf, PathComponent, PathIterItem, Preimage, SplitPath, TrieHash, TriePath, ValueDigest,
};
use thiserror::Error;

use crate::merkle::Value;

#[derive(Debug, Error)]
#[non_exhaustive]
/// Reasons why a proof is invalid
pub enum ProofError {
    /// Non-monotonic range decrease
    #[error("non-monotonic range increase")]
    NonMonotonicIncreaseRange,

    /// Unexpected hash
    #[error("unexpected hash")]
    UnexpectedHash,

    /// Unexpected value
    #[error("unexpected value")]
    UnexpectedValue,

    /// Value mismatch
    #[error("value mismatch")]
    ValueMismatch,

    /// Expected value but got None
    #[error("expected value but got None")]
    ExpectedValue,

    /// Proof is empty
    #[error("proof can't be empty")]
    Empty,

    /// Each proof node key should be a prefix of the proven key
    #[error("each proof node key should be a prefix of the proven key")]
    ShouldBePrefixOfProvenKey,

    /// Each proof node key should be a prefix of the next key
    #[error("each proof node key should be a prefix of the next key")]
    ShouldBePrefixOfNextKey,

    /// Child index is out of bounds
    #[error("child index is out of bounds")]
    ChildIndexOutOfBounds,

    /// Only nodes with even length key can have values
    #[error("only nodes with even length key can have values")]
    ValueAtOddNibbleLength,

    /// Node not in trie
    #[error("node not in trie")]
    NodeNotInTrie,

    /// Error from the merkle package
    #[error("{0:?}")]
    IO(#[from] FileIoError),

    /// Error deserializing a proof
    #[error("error deserializing a proof: {0}")]
    Deserialization(super::ReadError),

    /// Empty range
    #[error("empty range")]
    EmptyRange,

    /// The proof has not yet been verified.
    #[error("the proof has not yet been verified")]
    Unverified,

    /// Invalid value format
    #[error("invalid value format")]
    InvalidValueFormat,

    #[error("larger than max length")]
    ProofIsLargerThanMaxLength,

    /// Change proof keys are not sorted
    #[error("the change proof keys are not sorted")]
    ChangeProofKeysNotSorted,

    /// Start key is larger than first key
    #[error("the start key of the change proof is larger than the first key in the proof array")]
    StartKeyLargerThanFirstKey,

    /// End key is smaller than last key
    #[error("the end key of the change proof is larger than the end key in the proof array")]
    EndKeyLessThanLastKey,

    /// Range proof: requested start key is greater than the first key in the proof
    #[error("the requested start key is greater than the first key in the range proof")]
    RangeProofStartBeyondFirstKey,

    /// Range proof: requested end key is less than the last key in the proof
    #[error("the requested end key is less than the last key in the range proof")]
    RangeProofEndBeforeLastKey,

    #[error("the proof is None as it has been consumed")]
    ProofIsNone,

    // ── Range proof verification variants (from main) ──
    /// Key-value pair is outside the requested range
    #[error("key-value pair is outside the requested range")]
    KeyOutsideRange,

    /// End proof is required when key-value pairs are present or end key is specified
    #[error("no end proof")]
    NoEndProof,

    /// Start proof should be empty when no start key is specified
    #[error("unexpected start proof")]
    UnexpectedStartProof,

    /// Start and end proofs contain conflicting nodes at the same key path
    #[error("conflicting proof nodes at the same key path")]
    ConflictingProofNodes,

    /// Start key is after end key
    #[error("start key is after end key")]
    StartAfterEnd,

    /// A proof node within the range has a value not present in the key-value pairs
    #[error("proof node has a value not included in key-value pairs")]
    ProofNodeHasUnincludedValue,

    #[error("the proposal for a change proof is None as it has been consumed")]
    ProposalIsNone,

    // ── Change proof verification variants ──
    /// Computed root hash after applying `batch_ops` doesn't match expected end root
    #[error("computed root hash after applying batch_ops doesn't match the expected end root")]
    EndRootMismatch,

    /// The end proof's inclusion/exclusion result doesn't match the last
    /// batch op's type when `batch_ops` is non-empty. A Put expects the
    /// key to exist in `end_root` (inclusion); a Delete expects it to be
    /// absent (exclusion). A mismatch indicates the attacker tampered with
    /// `batch_ops` — for example, appending a spurious key changes
    /// `last_op_key`, shifting the end proof's derived key.
    #[error("end proof inclusion/exclusion result doesn't match last batch op type")]
    EndProofOperationMismatch,

    /// The start proof's inclusion/exclusion result doesn't match the first
    /// batch op's type when `first_op_key == start_key`. A Put expects the
    /// key to exist in `end_root` (inclusion); a Delete expects it to be
    /// absent (exclusion). A mismatch indicates the attacker tampered with
    /// `batch_ops` by adding a spurious key at `start_key`.
    #[error("start proof inclusion/exclusion result doesn't match first batch op type")]
    StartProofOperationMismatch,

    /// A `DeleteRange` operation was found in a change proof's batch ops.
    /// `DeleteRange` is not supported in change proofs — honest generators
    /// only produce `Put` and `Delete` operations.
    #[error("DeleteRange operation found in change proof")]
    DeleteRangeFoundInChangeProof,

    /// Bounded change proof with non-empty batch operations requires at
    /// least one boundary proof for verification. Unbounded proofs
    /// (`start_key` and `end_key` both `None`) use direct root hash
    /// comparison and do not require boundary proofs.
    #[error("bounded change proof requires at least one boundary proof")]
    MissingBoundaryProof,

    /// A proof node's value differs from the expected value
    #[error("proof node value does not match the expected value at the same key")]
    ProofNodeValueMismatch,

    /// Start and end boundary proofs share no common prefix — they
    /// disagree on the very first node. Since both proofs have already
    /// passed hash chain verification against the same `end_root`, their
    /// first nodes must be identical. This is unreachable without a hash
    /// collision.
    #[error("boundary proofs diverge at the root node")]
    BoundaryProofsDivergeAtRoot,

    /// Non-empty end proof when no end key is set and no batch operations.
    #[error("unexpected non-empty end proof with no end key and no batch operations")]
    UnexpectedEndProof,

    /// In-range child hash mismatch between proof and proposal.
    #[error("in-range child hash mismatch")]
    InRangeChildMismatch,

    /// Empty end proof when `end_key` is set or `batch_ops` is non-empty.
    #[error("missing end proof: end_key is set or batch_ops is non-empty")]
    MissingEndProof,

    /// Proof node is unreachable in the proving trie during collapse                                                 
    #[error("proof node unreachable in proving trie")]
    ProofNodeUnreachable,

    /// Exclusion proof is incomplete — the terminal node has a child at the
    /// boundary index, so the proof must include that child.
    #[error("exclusion proof missing child at boundary index")]
    ExclusionProofMissingChild,
}

#[derive(Clone, PartialEq, Eq)]
#[non_exhaustive]
/// A node in a proof.
pub struct ProofNode {
    /// The key this node is at. Each byte is a nibble.
    pub key: PathBuf,
    /// The length of the key prefix that is shared with the previous node.
    pub partial_len: usize,
    /// None if the node does not have a value.
    /// Otherwise, the node's value or the hash of its value.
    pub value_digest: Option<ValueDigest<Value>>,
    /// The hash of each child, or None if the child does not exist.
    pub child_hashes: Children<Option<HashType>>,
}

impl std::fmt::Debug for ProofNode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        // Filter the missing children and only show the present ones with their indices
        let child_hashes = self.child_hashes.iter_present().collect::<Vec<_>>();
        // Compute the hash and render it as well
        let hash = firewood_storage::Preimage::to_hash(self);

        f.debug_struct("ProofNode")
            .field("key", &self.key)
            .field("partial_len", &self.partial_len)
            .field("value_digest", &self.value_digest)
            .field("child_hashes", &child_hashes)
            .field("hash", &hash)
            .finish()
    }
}

impl Hashable for ProofNode {
    type LeadingPath<'a>
        = &'a [PathComponent]
    where
        Self: 'a;

    type PartialPath<'a>
        = &'a [PathComponent]
    where
        Self: 'a;

    type FullPath<'a>
        = &'a [PathComponent]
    where
        Self: 'a;

    fn parent_prefix_path(&self) -> Self::LeadingPath<'_> {
        #[expect(
            clippy::disallowed_methods,
            reason = "partial_len is validated <= key.len() during deserialization"
        )]
        let (prefix, _) = self.key.split_at(self.partial_len);
        prefix
    }

    fn partial_path(&self) -> Self::PartialPath<'_> {
        #[expect(
            clippy::disallowed_methods,
            reason = "partial_len is validated <= key.len() during deserialization"
        )]
        let (_, suffix) = self.key.split_at(self.partial_len);
        suffix
    }

    fn full_path(&self) -> Self::FullPath<'_> {
        &self.key
    }

    fn value_digest(&self) -> Option<ValueDigest<&[u8]>> {
        self.value_digest.as_ref().map(ValueDigest::as_ref)
    }

    fn children(&self) -> Children<Option<HashType>> {
        self.child_hashes.clone()
    }
}

impl From<PathIterItem> for ProofNode {
    fn from(item: PathIterItem) -> Self {
        let child_hashes = if let Some(branch) = item.node.as_branch() {
            branch.children_hashes()
        } else {
            Children::new()
        };

        let partial_len = item
            .key_nibbles
            .len()
            .saturating_sub(item.node.partial_path().len());

        let value_digest = item
            .node
            .value()
            .map(|value| ValueDigest::Value(Box::from(value)));

        // For account-depth nodes on databases that need storageRoot
        // recomputation, fix the value from the node's children.
        #[cfg(feature = "ethhash")]
        let value_digest = if item.must_recompute_storage_hash {
            fix_account_storage_root(value_digest, &item.key_nibbles, &child_hashes)
        } else {
            value_digest
        };

        Self {
            key: item.key_nibbles,
            partial_len,
            value_digest,
            child_hashes,
        }
    }
}

/// For account-depth nodes (64 nibbles), replace the storageRoot field in the
/// value with the hash computed from the node's children. This ensures proofs
/// from older databases contain a valid storageRoot.
#[cfg(feature = "ethhash")]
fn fix_account_storage_root(
    value_digest: Option<ValueDigest<Box<[u8]>>>,
    key_nibbles: &PathBuf,
    child_hashes: &Children<Option<HashType>>,
) -> Option<ValueDigest<Box<[u8]>>> {
    if key_nibbles.len() != 64 {
        return value_digest;
    }

    let Some(ValueDigest::Value(value)) = value_digest else {
        return value_digest;
    };

    match firewood_storage::fix_account_storage_root_value(&value, child_hashes) {
        Some(fixed) => Some(ValueDigest::Value(fixed)),
        None => Some(ValueDigest::Value(value)),
    }
}

/// A proof that a given key-value pair either exists or does not exist in a trie.
#[derive(Clone, PartialEq, Eq, Default)]
pub struct Proof<T: ?Sized>(T);

impl<T> std::fmt::Debug for Proof<T>
where
    T: ProofCollection + ?Sized,
    T::Node: std::fmt::Debug,
{
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        struct Indexed<'a, N>(&'a [N]);
        impl<N: std::fmt::Debug> std::fmt::Debug for Indexed<'_, N> {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                f.debug_map().entries(self.0.iter().enumerate()).finish()
            }
        }
        let nodes = self.0.as_ref();
        f.debug_struct("Proof")
            .field("len", &nodes.len())
            .field("nodes", &Indexed(nodes))
            .finish()
    }
}

impl<T: ProofCollection + ?Sized> Proof<T> {
    /// Verify a proof
    pub fn verify<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self,
        key: K,
        expected_value: Option<V>,
        root_hash: &TrieHash,
    ) -> Result<(), ProofError> {
        verify_opt_value_digest(expected_value, self.value_digest(key, root_hash)?)
    }

    /// Verify this proof against `root_hash` for the given `key` and return the
    /// value digest at that key.
    ///
    /// # Returns
    ///
    /// - `Ok(Some(digest))` — **inclusion proof**: the key exists in the trie
    ///   and `digest` is its value digest.
    /// - `Ok(None)` — **exclusion proof**: the proof is valid but the key does
    ///   not exist in the trie revision.
    ///
    /// # Errors
    ///
    /// - [`ProofError::Empty`] — the proof contains no nodes.
    /// - [`ProofError::UnexpectedHash`] — a node's hash does not match the
    ///   expected hash from its parent (or `root_hash` for the first node).
    /// - [`ProofError::ValueAtOddNibbleLength`] — a node whose key has an odd
    ///   number of nibbles carries a value digest, which is structurally invalid.
    /// - [`ProofError::ShouldBePrefixOfProvenKey`] — an intermediate node's key
    ///   is not a prefix of `key`.
    /// - [`ProofError::ShouldBePrefixOfNextKey`] — a node's key is not a prefix
    ///   of the next node's key in the proof.
    /// - [`ProofError::NodeNotInTrie`] — the child pointer from one node to the
    ///   next is absent, meaning the proof path does not exist in the trie.
    pub fn value_digest<K: AsRef<[u8]>>(
        &self,
        key: K,
        root_hash: &TrieHash,
    ) -> Result<Option<ValueDigest<&[u8]>>, ProofError> {
        let key = Path(NibblesIterator::new(key.as_ref()).collect());

        let Some(last_node) = self.0.as_ref().last() else {
            return Err(ProofError::Empty);
        };

        let mut expected_hash = root_hash.clone().into_hash_type();

        let mut iter = self.0.as_ref().iter().peekable();
        while let Some(node) = iter.next() {
            if node.to_hash() != expected_hash {
                return Err(ProofError::UnexpectedHash);
            }

            // Assert that only nodes whose keys are an even number of nibbles
            // have a `value_digest`.
            if !node.full_path().len().is_multiple_of(2) && node.value_digest().is_some() {
                return Err(ProofError::ValueAtOddNibbleLength);
            }

            if let Some(next_node) = iter.peek() {
                // Assert that every non-terminal node's key is a prefix of
                // `key`, and that the proof follows the branch toward `key`.
                let Some(key_nibble) = next_nibble(node.full_path(), key.as_components()) else {
                    return Err(ProofError::ShouldBePrefixOfProvenKey);
                };

                // Assert that every node's key is a prefix of the next node's
                // key, and that the next node is on the path toward `key`.
                if next_nibble(node.full_path(), next_node.full_path()) != Some(key_nibble) {
                    return Err(ProofError::ShouldBePrefixOfNextKey);
                }

                expected_hash = node.children()[key_nibble]
                    .as_ref()
                    .ok_or(ProofError::NodeNotInTrie)?
                    .clone();
            }
        }

        if last_node.full_path().path_eq(key.as_components()) {
            return Ok(last_node.value_digest());
        }

        // Exclusion proof validation.
        //
        // If the last node is an ancestor of the key (its key is a strict
        // prefix of the proven key), check whether a child exists at the
        // next nibble. If a child exists there, the proof is incomplete —
        // it must include that child to demonstrate the key's absence.
        // Without the child, the verifier cannot tell whether the key
        // exists deeper in the trie.
        //
        // When no child exists at the next nibble, the ancestor alone is
        // sufficient: the absence of a child proves the key cannot exist.
        //
        // When the last node's key is NOT an ancestor of the key (it
        // extends past or diverges from the key), this is the valid
        // "divergent child" case — the node occupies the position where
        // the key would be, proving it cannot exist.
        if let Some(next_idx) = next_nibble(last_node.full_path(), key.as_components())
            && last_node.children()[next_idx].is_some()
        {
            return Err(ProofError::ExclusionProofMissingChild);
        }

        Ok(None)
    }

    /// Returns the length of the proof.
    #[must_use]
    pub fn len(&self) -> usize {
        self.0.as_ref().len()
    }

    /// Returns true if the proof is empty.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.0.as_ref().is_empty()
    }
}

impl<T: ProofCollection + ?Sized> std::ops::Deref for Proof<T> {
    type Target = T;

    #[inline]
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl<T: ProofCollection + ?Sized> std::ops::DerefMut for Proof<T> {
    #[inline]
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

impl<T: ProofCollection> Proof<T> {
    /// Constructs a new proof from a collection of proof nodes.
    #[inline]
    #[must_use]
    pub const fn new(proof: T) -> Self {
        Self(proof)
    }
}

impl Proof<EmptyProofCollection> {
    /// Constructs a new empty proof.
    #[inline]
    #[must_use]
    pub const fn empty() -> Self {
        Self::new(EmptyProofCollection)
    }

    /// Converts an empty immutable proof into an empty mutable proof.
    #[inline]
    #[must_use]
    pub const fn into_mutable<T: Hashable>(self) -> Proof<Vec<T>> {
        Proof::new(Vec::new())
    }
}

impl<T: Hashable> Proof<Box<[T]>> {
    /// Converts an immutable proof into a mutable proof.
    #[inline]
    #[must_use]
    pub fn into_mutable(self) -> Proof<Vec<T>> {
        Proof::new(self.0.into_vec())
    }
}

impl<T: Hashable> Proof<Vec<T>> {
    /// Converts a mutable proof into an immutable proof.
    #[inline]
    #[must_use]
    pub fn into_immutable(self) -> Proof<Box<[T]>> {
        Proof::new(self.0.into_boxed_slice())
    }
}

impl<T, V> Proof<V>
where
    T: Hashable,
    V: ProofCollection<Node = T> + IntoIterator<Item = T> + FromIterator<T>,
{
    /// Joins two proofs into one.
    #[inline]
    #[must_use]
    pub fn join<O: ProofCollection<Node = T> + IntoIterator<Item = T>>(
        self,
        other: Proof<O>,
    ) -> Proof<V> {
        self.into_iter().chain(other).collect()
    }
}

impl<V: ProofCollection + FromIterator<V::Node>> FromIterator<V::Node> for Proof<V> {
    #[inline]
    fn from_iter<I: IntoIterator<Item = V::Node>>(iter: I) -> Self {
        Proof(iter.into_iter().collect())
    }
}

impl<V: ProofCollection + Extend<V::Node>> Extend<V::Node> for Proof<V> {
    #[inline]
    fn extend<I: IntoIterator<Item = V::Node>>(&mut self, iter: I) {
        self.0.extend(iter);
    }
}

impl<V: ProofCollection + IntoIterator<Item = V::Node>> IntoIterator for Proof<V> {
    type Item = V::Node;
    type IntoIter = V::IntoIter;

    #[inline]
    fn into_iter(self) -> Self::IntoIter {
        self.0.into_iter()
    }
}

/// A trait representing a collection of proof nodes.
///
/// This allows [`Proof`] to be generic over different types of collections such
/// a `Box<[T]>` or `Vec<T>`, where `T` implements the `Hashable` trait.
pub trait ProofCollection: AsRef<[Self::Node]> {
    /// The type of nodes in the proof collection.
    type Node: Hashable;
}

impl<T: Hashable> ProofCollection for [T] {
    type Node = T;
}

impl<T: Hashable> ProofCollection for Box<[T]> {
    type Node = T;
}

impl<T: Hashable> ProofCollection for Vec<T> {
    type Node = T;
}

/// A zero-sized type to represent an empty proof collection.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
pub struct EmptyProofCollection;

impl AsRef<[ProofNode]> for EmptyProofCollection {
    #[inline]
    fn as_ref(&self) -> &[ProofNode] {
        &[]
    }
}

impl ProofCollection for EmptyProofCollection {
    type Node = ProofNode;
}

/// Returns the next nibble in `c` after `b`.
/// Returns None if `b` is not a strict prefix of `c`.
fn next_nibble(b: impl IntoSplitPath, c: impl IntoSplitPath) -> Option<PathComponent> {
    let b = b.into_split_path();
    let c = c.into_split_path();
    match b.longest_common_prefix(c).split_first_parts() {
        (None, Some((c, _)), _) => Some(c),
        _ => None,
    }
}

fn verify_opt_value_digest(
    expected_value: Option<impl AsRef<[u8]>>,
    found_value: Option<ValueDigest<impl AsRef<[u8]>>>,
) -> Result<(), ProofError> {
    match (expected_value, found_value) {
        (None, None) => Ok(()),
        (Some(_), None) => Err(ProofError::ExpectedValue),
        (None, Some(_)) => Err(ProofError::UnexpectedValue),
        (Some(ref expected), Some(found)) if found.verify(expected) => Ok(()),
        (Some(_), Some(_)) => Err(ProofError::ValueMismatch),
    }
}

/// The type of serialized proof.
///
/// This enum is used internally during proof serialization and deserialization to
/// identify the format and contents of a proof.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ProofType {
    /// A proof for a single key/value pair.
    ///
    /// A proof is a sequence of nodes from the root to a specific node.
    /// Each node in the path includes the hash of its child nodes, allowing
    /// for verification of the integrity of the path.
    ///
    /// A single proof includes the full key and value (if present) of the target
    /// node.
    Single = 0,
    /// A range proof for all key/value pairs over a specific key range.
    ///
    /// A range proof includes a key proof for the beginning and end of the
    /// range, as well as all key/value pairs in the range.
    Range = 1,
    /// A change proof for all key/value pairs that changed between two
    /// versions of the tree.
    ///
    /// A change proof includes a key proof for the beginning and end of the
    /// changed range, as well as all key/value pairs that changed.
    Change = 2,
}

impl ProofType {
    /// Parse a byte into a [`ProofType`].
    #[must_use]
    pub const fn new(v: u8) -> Option<Self> {
        match v {
            0 => Some(ProofType::Single),
            1 => Some(ProofType::Range),
            2 => Some(ProofType::Change),
            _ => None,
        }
    }

    /// Human readable name for the [`ProofType`]
    #[must_use]
    pub const fn name(self) -> &'static str {
        match self {
            ProofType::Single => "single",
            ProofType::Range => "range",
            ProofType::Change => "change",
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Build a `ProofNode` at the given nibble path with the given children and value.
    fn make_node(
        nibbles: &[u8],
        parent_prefix_len: usize,
        value: Option<&[u8]>,
        child_hashes: Children<Option<HashType>>,
    ) -> ProofNode {
        let key: PathBuf = nibbles
            .iter()
            .map(|&b| PathComponent(firewood_storage::U4::new_masked(b)))
            .collect();
        ProofNode {
            key,
            partial_len: parent_prefix_len,
            value_digest: value.map(|v| ValueDigest::Value(v.to_vec().into_boxed_slice())),
            child_hashes,
        }
    }

    /// A proof for key B (nibble 3) must not be accepted as an exclusion
    /// proof for key C (nibble 5).
    #[test]
    fn proof_for_wrong_branch_is_rejected() {
        // Leaf at nibble path [3, 0] with a value.
        let leaf = make_node(&[3, 0], 1, Some(b"val_b"), Children::new());
        let leaf_hash = leaf.to_hash();

        // Root branch with children at nibbles 3 and 5.
        let mut root_children: Children<Option<HashType>> = Children::new();
        root_children[PathComponent(firewood_storage::U4::new_masked(3))] = Some(leaf_hash);
        root_children[PathComponent(firewood_storage::U4::new_masked(5))] =
            Some(HashType::from([0xCC; 32]));
        let root = make_node(&[], 0, None, root_children);
        let root_hash: TrieHash = root.to_hash().into_triehash();

        let proof = Proof::new(vec![root, leaf]);

        // Verifying against key [0x30] (nibble path [3, 0]) should succeed
        // — this is the key the proof was built for.
        assert!(proof.value_digest([0x30u8], &root_hash).is_ok());

        // Verifying against key [0x50] (nibble path [5, 0]) must fail
        // — the proof goes to nibble 3, not nibble 5.
        assert!(matches!(
            proof.value_digest([0x50u8], &root_hash),
            Err(ProofError::ShouldBePrefixOfNextKey),
        ));
    }
}
