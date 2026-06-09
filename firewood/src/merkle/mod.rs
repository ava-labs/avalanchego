// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(test)]
pub(crate) mod tests;

pub(crate) mod changes;
pub(crate) mod childmask;
pub(crate) mod collapse;
mod merge;
/// Parallel merkle
pub mod parallel;
pub(crate) mod reconcile;

use crate::api::{
    self, BatchIter, FrozenChangeProof, FrozenProof, FrozenRangeProof, HashKey, KeyType,
    KeyValuePair, ValueType,
};
use crate::iter::{MerkleKeyValueIter, PathIterator};
use crate::merkle::changes::DiffMerkleNodeStream;
use crate::proofs::ProofEdge;
use crate::proofs::change::ChangeProof;
use crate::proofs::eth::ACCOUNT_DEPTH_NIBBLES;
use crate::{
    ChangeProofVerificationContext, Proof, ProofCollection, ProofError, ProofNode, RangeProof,
};
use firewood_metrics::{HistogramExt, firewood_counter, firewood_histogram};
use firewood_storage::MemStore;
use firewood_storage::{
    BranchNode, Child, Children, FileIoError, HashType, HashableShunt, HashedNodeReader,
    ImmutableProposal, IntoHashType, LeafNode, MaybePersistedNode, Mutable, MutableKind,
    NibblesIterator, Node, NodeStore, Path, PathBuf, PathComponent, Propose, ReadableStorage,
    SharedNode, TrieHash, TrieReader, U4, ValueDigest,
};
use firewood_storage::{
    hash_node_as_storage_trie_root_for_node, hash_node_as_storage_trie_root_parts,
};
use std::borrow::Cow;
use std::collections::{HashMap, HashSet};
use std::fmt::Debug;
use std::io::Error;
use std::iter::once;
use std::num::NonZeroUsize;
use std::sync::Arc;

/// Keys are boxed u8 slices
pub type Key = Box<[u8]>;

/// Values are boxed u8 slices
pub type Value = Box<[u8]>;

use childmask::ChildMask;

macro_rules! write_attributes {
    ($writer:ident, $node:expr, $value:expr) => {
        if !$node.partial_path.0.is_empty() {
            write!($writer, " pp={:x}", $node.partial_path)
                .map_err(|e| FileIoError::from_generic_no_file(e, "write attributes"))?;
        }
        if !$value.is_empty() {
            firewood_storage::format_node_value($value, $writer)
                .map_err(|e| FileIoError::from_generic_no_file(e, "write attributes"))?;
        }
    };
}

/// Returns the value mapped to by `key` in the subtrie rooted at `node`.
fn get_helper<T: TrieReader>(
    nodestore: &T,
    node: &Node,
    key: &[u8],
) -> Result<Option<SharedNode>, FileIoError> {
    // 4 possibilities for the position of the `key` relative to `node`:
    // 1. The node is at `key`
    // 2. The key is above the node (i.e. its ancestor)
    // 3. The key is below the node (i.e. its descendant)
    // 4. Neither is an ancestor of the other
    let path_overlap = PrefixOverlap::from(key, node.partial_path());
    let unique_key = path_overlap.unique_a;
    let unique_node = path_overlap.unique_b;

    match (
        unique_key.split_first().map(|(index, path)| (*index, path)),
        unique_node.split_first(),
    ) {
        (_, Some(_)) => {
            // Case (2) or (4)
            Ok(None)
        }
        (None, None) => Ok(Some(node.clone().into())), // 1. The node is at `key`
        (Some((child_index, remaining_key)), None) => {
            let child_index = PathComponent::try_new(child_index).expect("index is in bounds");
            // 3. The key is below the node (i.e. its descendant)
            match node {
                Node::Leaf(_) => Ok(None),
                Node::Branch(node) => match node.children[child_index].as_ref() {
                    None => Ok(None),
                    Some(Child::Node(child)) => get_helper(nodestore, child, remaining_key),
                    Some(Child::AddressWithHash(addr, _)) => {
                        let child = nodestore.read_node(*addr)?;
                        get_helper(nodestore, &child, remaining_key)
                    }
                    Some(Child::MaybePersisted(maybe_persisted, _)) => {
                        let child = maybe_persisted.as_shared_node(nodestore)?;
                        get_helper(nodestore, &child, remaining_key)
                    }
                },
            }
        }
    }
}

#[derive(Debug)]
/// Merkle operations against a nodestore
pub struct Merkle<T> {
    nodestore: T,
}

impl<T> Merkle<T> {
    pub(crate) fn into_inner(self) -> T {
        self.nodestore
    }
}

impl<T> From<T> for Merkle<T> {
    fn from(nodestore: T) -> Self {
        Merkle { nodestore }
    }
}

/// Verify one edge (left or right) of a range proof.
///
/// Empty proofs are accepted without verification. Otherwise, checks that the
/// requested bound is consistent with the edge key-value pair, then verifies
/// the proof against the root hash.
fn verify_edge<H: ProofCollection + ?Sized>(
    requested_bound: Option<&[u8]>,
    edge_kv: Option<(&[u8], &[u8])>,
    edge_proof: &Proof<H>,
    root_hash: &TrieHash,
    edge: ProofEdge,
) -> Result<(), api::Error> {
    if edge_proof.is_empty() {
        return Ok(());
    }

    let bound_is_lower = matches!(edge, ProofEdge::Left);

    // Validate bound vs edge key ordering
    if let (Some(bound), Some((edge_key, _))) = (requested_bound, edge_kv) {
        let out_of_order = if bound_is_lower {
            bound > edge_key
        } else {
            bound < edge_key
        };
        if out_of_order {
            let proof_error = if bound_is_lower {
                ProofError::RangeProofStartBeyondFirstKey
            } else {
                ProofError::RangeProofEndBeforeLastKey
            };
            return Err(api::Error::ProofError(proof_error));
        }
    }

    // Any `UnexpectedHash` bubbling up from this edge's `verify()` walk is
    // by construction an edge-proof failure — re-stamp it with which edge.
    let annotate = |e: ProofError| match e {
        ProofError::UnexpectedHash { expected, actual } => ProofError::EdgeProofHashMismatch {
            edge,
            expected,
            actual,
        },
        other => other,
    };

    // Verify the proof for this edge
    if let Some(bound) = requested_bound {
        let expected_value: Option<&[u8]> =
            edge_kv.and_then(|(key, value)| (bound == key).then_some(value));
        edge_proof
            .verify(bound, expected_value, root_hash)
            .map_err(|e| api::Error::ProofError(annotate(e)))?;
    } else if let Some((edge_key, edge_value)) = edge_kv {
        edge_proof
            .verify(edge_key, Some(edge_value), root_hash)
            .map_err(|e| api::Error::ProofError(annotate(e)))?;
    }

    Ok(())
}

/// Describes the boundary of one edge of a range proof.
///
/// The variants encode "which edge"; the right edge's inclusivity lives
/// inside [`RightBoundary`]. Splitting these two concerns lets sites that
/// only operate on the right edge (e.g. `verify_proof_node_values`) take
/// `RightBoundary` directly without an `unreachable!()` arm for `Left`,
/// while keeping a single unified type for the one place that genuinely
/// dispatches on left-vs-right at runtime: `compute_outside_children`.
#[derive(Debug)]
pub(crate) enum EdgeBoundary<'a> {
    /// Left edge, inclusive at `start_key`. `None` proves from the very
    /// beginning of the trie.
    Left(Option<&'a [u8]>),
    /// Right edge, with inclusivity carried by the inner variant.
    Right(RightBoundary<'a>),
}

/// The right edge of a range proof.
///
/// `OutOfRange` holds a `Cow` because the only path that produces it —
/// `right_edge` for a terminal value-node whose full key exceeds
/// `last_kv` — derives the boundary by reconstructing the terminal's
/// full byte key from its nibble path, which is owned. `InRange`
/// always borrows from caller-supplied slices.
///
/// Note: "in-range" / "out-of-range" describes whether the boundary
/// key itself belongs to the proven range. This is independent from
/// the inclusion-proof vs. exclusion-proof distinction at the proof
/// layer (which is about whether a key exists in the trie).
#[derive(Debug)]
pub(crate) enum RightBoundary<'a> {
    /// The boundary key (or no upper bound if `None`) is part of the
    /// proven range — the right end is "closed" at `end_key`.
    InRange(Option<&'a [u8]>),
    /// The boundary key is *not* part of the proven range — the right
    /// end is "open" at `end_key`. `end_key` always exists for this
    /// variant; it sits at the end-proof's terminal node.
    OutOfRange(Cow<'a, [u8]>),
}

impl RightBoundary<'_> {
    fn boundary_key(&self) -> Option<&[u8]> {
        match self {
            RightBoundary::InRange(k) => *k,
            RightBoundary::OutOfRange(k) => Some(k.as_ref()),
        }
    }
}

impl EdgeBoundary<'_> {
    fn boundary_key(&self) -> Option<&[u8]> {
        match self {
            EdgeBoundary::Left(k) => *k,
            EdgeBoundary::Right(r) => r.boundary_key(),
        }
    }

    const fn is_left(&self) -> bool {
        matches!(self, EdgeBoundary::Left(_))
    }

    const fn is_right_exclusive(&self) -> bool {
        matches!(self, EdgeBoundary::Right(RightBoundary::OutOfRange(_)))
    }
}

/// For a proof edge path, computes which child indices at each proof node are
/// "outside" the proven range and should use the proof's child hashes.
///
/// For the left edge: children with index < the on-path child are outside.
/// For the right edge: children with index > the on-path child are outside.
///
/// The boundary key (in bytes, not nibbles) is used to determine the
/// on-path nibble at the terminal proof node, since there is no subsequent
/// proof node to derive it from.
///
/// For [`RightBoundary::OutOfRange`], the boundary itself sits at the
/// proof's terminal node and is *not* part of the proven range. The on-path
/// child at the terminal's parent leads only to that terminal, so we mark
/// it outside in addition to the usual "children with index > the on-path
/// child" marking — the hash check then substitutes the proof's stored
/// child hash there instead of recursing into the proving trie's
/// (irrelevant) subtree.
fn compute_outside_children(
    proof_nodes: &[ProofNode],
    boundary: EdgeBoundary<'_>,
) -> Result<HashMap<PathBuf, ChildMask>, ProofError> {
    // Invariant from `right_edge`: `RightBoundary::OutOfRange(K)` is only
    // constructed when `K` is the end-proof's terminal full key (and
    // `K > last_kv`). Pin that here so a future change to `right_edge`
    // that breaks it surfaces immediately in tests.
    if let EdgeBoundary::Right(RightBoundary::OutOfRange(boundary_key)) = &boundary {
        debug_assert!(
            proof_nodes.last().is_some_and(|terminal| {
                terminal
                    .key
                    .iter()
                    .map(|c| c.as_u8())
                    .eq(NibblesIterator::new(boundary_key.as_ref()))
            }),
            "RightBoundary::OutOfRange must match the end-proof terminal's full nibble path",
        );
    }

    let mut result: HashMap<PathBuf, ChildMask> = HashMap::new();

    // Non-terminal nodes: derive the on-path nibble from the next proof node
    let mut pairs = proof_nodes
        .iter()
        .zip(proof_nodes.iter().skip(1))
        .peekable();
    while let Some((parent, child)) = pairs.next() {
        let on_path_nibble = child
            .key
            .get(parent.key.len())
            .ok_or(ProofError::ShouldBePrefixOfNextKey)?;
        let entry = result.entry(parent.key.clone()).or_default();
        *entry = if boundary.is_left() {
            entry.set_below(on_path_nibble.0)
        } else {
            entry.set_above(on_path_nibble.0)
        };

        // Right-edge exclusive: at the terminal's parent (i.e., the last
        // non-terminal pair), the on-path child's whole subtree is just
        // the terminal, which is itself outside the proven range. Mark it
        // outside so the hash check uses the proof's stored child hash
        // here instead of recursing into the proving trie.
        if boundary.is_right_exclusive() && pairs.peek().is_none() {
            *entry = entry.set(on_path_nibble.0);
        }
    }

    // Terminal node: derive the on-path nibble from the boundary key.
    // If the boundary key diverges within the terminal node's partial path,
    // either all or none of the children are outside the range.
    if let (Some(terminal), Some(boundary_key)) = (proof_nodes.last(), boundary.boundary_key()) {
        let boundary_nibbles: Vec<u8> = NibblesIterator::new(boundary_key).collect();

        // Find the first position where boundary and terminal key diverge,
        // capturing the diverging values to avoid re-indexing.
        let divergence = terminal
            .key
            .iter()
            .zip(boundary_nibbles.iter())
            .find(|(tk, bn)| tk.as_u8() != **bn);

        if let Some((tk, bn)) = divergence {
            // Boundary diverges within the terminal's key at this position.
            // If boundary is "past" the terminal (left edge: B > terminal,
            // right edge: B < terminal), all children are outside.
            let all_outside = if boundary.is_left() {
                *bn > tk.as_u8()
            } else {
                *bn < tk.as_u8()
            };
            if all_outside {
                result.insert(terminal.key.clone(), ChildMask::ALL);
            }
            // Otherwise (boundary is "before" terminal), no children are outside.
        } else if let Some(on_path_byte) = boundary_nibbles.get(terminal.key.len()) {
            // Terminal is an ancestor of the boundary key. The next
            // nibble tells us which child leads toward the boundary.
            // Mark children on the far side of that nibble as outside,
            // and also mark the on-path child itself: its subtree may
            // contain keys beyond the proven range, so we must use the
            // proof's hash rather than recomputing it.
            let on_path_nibble = U4::new_masked(*on_path_byte);
            let entry = result.entry(terminal.key.clone()).or_default();
            *entry = if boundary.is_left() {
                entry.set_below(on_path_nibble)
            } else {
                entry.set_above(on_path_nibble)
            }
            .set(on_path_nibble);
        } else if !boundary.is_left() {
            // Boundary is a prefix of or exactly matches the terminal key.
            // For the right edge, all children extend beyond end_key (they
            // represent keys longer than end_key sharing its prefix), so
            // they are outside the proven range.
            result.insert(terminal.key.clone(), ChildMask::ALL);
        }
        // For the left edge when boundary matches/is-prefix-of terminal,
        // children extend beyond start_key and are in-range — no marking.
    }

    Ok(result)
}

/// Compute the hash of a node in the proving trie, merging child hashes
/// from proof nodes for subtrees outside the proven range.
///
/// For branch nodes, in-range children that are in-memory (`Child::Node`)
/// are hashed recursively. Persisted children (`AddressWithHash`,
/// `MaybePersisted`) already carry their hash and are used directly.
/// Out-of-range children get their hash from the corresponding proof node.
///
/// Hashes the node as a normal trie node. Under `ethhash`, when this node
/// is the single storage child of an account at depth 64, the parent
/// instead invokes `compute_root_hash_as_storage_trie_root` to apply the
/// storage-trie-root fold.
fn compute_root_hash_with_proofs(
    node: &Node,
    path_prefix: &[PathComponent],
    proof_nodes: &HashMap<PathBuf, &ProofNode>,
    outside_children: &HashMap<PathBuf, ChildMask>,
) -> HashType {
    match node {
        Node::Leaf(_) => HashableShunt::from_node(path_prefix, node).to_hash(),
        Node::Branch(branch) => {
            let parts = build_branch_parts(branch, path_prefix, proof_nodes, outside_children);
            HashableShunt::new(
                path_prefix,
                parts.partial_path,
                parts.value_digest,
                parts.child_hashes,
            )
            .to_hash()
        }
    }
}

/// Compute the hash of a node as a standalone storage-trie root, applying
/// the account-branch-nibble fold. Invoked by the parent at depth-64
/// account boundaries when this node is the account's lone storage child;
/// the fold matches what live hashing produced when the storage trie was
/// first written.
fn compute_root_hash_as_storage_trie_root(
    node: &Node,
    account_prefix: &[PathComponent],
    branch_nibble: PathComponent,
    proof_nodes: &HashMap<PathBuf, &ProofNode>,
    outside_children: &HashMap<PathBuf, ChildMask>,
) -> HashType {
    match node {
        Node::Leaf(_) => {
            hash_node_as_storage_trie_root_for_node(account_prefix, branch_nibble, node)
        }
        Node::Branch(branch) => {
            let path_prefix: PathBuf = account_prefix
                .iter()
                .copied()
                .chain(once(branch_nibble))
                .collect();
            let parts = build_branch_parts(branch, &path_prefix, proof_nodes, outside_children);
            hash_node_as_storage_trie_root_parts(
                account_prefix,
                branch_nibble,
                parts.partial_path,
                parts.value_digest,
                parts.child_hashes,
            )
        }
    }
}

/// Hashable parts of a branch node assembled by `build_branch_parts`. The
/// caller applies the final hash via either `HashableShunt::new` (normal)
/// or `hash_node_as_storage_trie_root_parts` (the single-storage-child
/// fold used at depth-64 account boundaries under `ethhash`).
struct BranchParts<'b> {
    partial_path: &'b [PathComponent],
    value_digest: Option<ValueDigest<&'b [u8]>>,
    child_hashes: Children<Option<HashType>>,
}

/// Recursive worker for the two `compute_root_hash_with_proofs` entry
/// points. Walks `branch`'s children (recursing into in-range subtrees,
/// copying proof-node hashes for out-of-range slots) and returns the parts
/// the caller needs to hash this node by either terminal helper.
fn build_branch_parts<'b>(
    branch: &'b BranchNode,
    path_prefix: &[PathComponent],
    proof_nodes: &HashMap<PathBuf, &'b ProofNode>,
    outside_children: &HashMap<PathBuf, ChildMask>,
) -> BranchParts<'b> {
    // Build full key for this node: path_prefix ++ partial_path
    let full_key: PathBuf = path_prefix
        .iter()
        .chain(branch.partial_path.as_components().iter())
        .copied()
        .collect();

    let outside_mask = outside_children.get(&full_key);
    let proof_node = proof_nodes.get(&full_key);

    let single_storage_child =
        single_effective_account_child(&full_key, branch, proof_node.copied(), outside_mask);

    let mut child_hashes: Children<Option<HashType>> = Children::new();

    // For children inside the proven range, compute hashes recursively.
    // For children outside the range, use proof node hashes (set below).
    let mut child_prefix: PathBuf = full_key.iter().copied().collect();
    for (nibble, child_opt) in &branch.children {
        let Some(child) = child_opt else { continue };
        if outside_mask.is_some_and(|m| m.is_set(nibble.0)) {
            continue;
        }
        // Persisted children already carry their hash — use it directly
        // instead of resolving and recursing into the subtree.
        if let Child::AddressWithHash(_, hash) | Child::MaybePersisted(_, hash) = child {
            child_hashes[nibble] = Some(hash.clone());
            continue;
        }
        let Child::Node(child_node) = child else {
            unreachable!()
        };
        child_prefix.push(nibble);
        // Apply the storage-trie-root fold for the account's lone storage
        // child; the dispatch lives here at the parent so the child's recursive
        // call doesn't need to carry a flag.
        let child_hash = if cfg!(feature = "ethhash") && single_storage_child == Some(nibble) {
            compute_root_hash_as_storage_trie_root(
                child_node,
                &full_key,
                nibble,
                proof_nodes,
                outside_children,
            )
        } else {
            compute_root_hash_with_proofs(child_node, &child_prefix, proof_nodes, outside_children)
        };
        child_hashes[nibble] = Some(child_hash);
        child_prefix.pop();
    }

    // For children outside the proven range, use proof node hashes.
    if let (Some(pn), Some(outside)) = (proof_node, outside_mask) {
        for (nibble, hash) in pn.child_hashes.iter_present() {
            if outside.is_set(nibble.0) {
                child_hashes[nibble] = Some(hash.clone());
            }
        }
    }

    // Use the branch's value if it exists. Otherwise, fall back to the
    // proof node's value digest (which may be a Hash for out-of-range
    // nodes where no key-value pair was inserted). The proof node's
    // hash chain was verified by `value_digest()` during boundary proof
    // validation, and `reconcile_branch_proof_node` verified the hash
    // against the branch value when present. The digest is trusted.
    let value_digest =
        branch.value.as_deref().map(ValueDigest::Value).or_else(|| {
            proof_node.and_then(|pn| pn.value_digest.as_ref().map(ValueDigest::as_ref))
        });

    BranchParts {
        partial_path: branch.partial_path.as_components(),
        value_digest,
        child_hashes,
    }
}

/// At a depth-64 account branch, return the slot of the single effective
/// storage child (the fake-root case live hashing applies), or `None`.
///
/// An "effective" child is either an in-range branch child or an
/// out-of-range child carried by the proof node — together they reflect
/// the true on-disk shape. Proof verification only; live hashing has its
/// own detection in `hash_helper_inner`. Without `ethhash` there is no
/// account-branch fold, so this always returns `None`.
fn single_effective_account_child(
    full_key: &[PathComponent],
    branch: &BranchNode,
    proof_node: Option<&ProofNode>,
    outside_mask: Option<&ChildMask>,
) -> Option<PathComponent> {
    if !cfg!(feature = "ethhash") || full_key.len() != ACCOUNT_DEPTH_NIBBLES {
        return None;
    }

    let mut only_child: Option<PathComponent> = None;
    // In-range branch children (those NOT marked as outside).
    for (nibble, _) in branch.children.iter_present() {
        if !outside_mask.is_some_and(|m| m.is_set(nibble.0)) {
            if only_child.is_some() {
                return None;
            }
            only_child = Some(nibble);
        }
    }
    // Out-of-range children, taken from the proof node.
    if let (Some(pn), Some(mask)) = (proof_node, outside_mask) {
        for (nibble, _) in pn.child_hashes.iter_present() {
            if mask.is_set(nibble.0) {
                if only_child.is_some() {
                    return None;
                }
                only_child = Some(nibble);
            }
        }
    }

    only_child
}

/// Reject any proof node that carries a value digest (either
/// [`ValueDigest::Value`] or [`ValueDigest::Hash`]) at an odd-length
/// nibble path. Byte keys are always even-nibble, so a value at an
/// odd-length path can't correspond to any real key in the trie — this
/// is the same invariant [`Proof::value_digest`] enforces while walking
/// a proof. This is a cheap structural sanity check that callers run on
/// a proof's nodes before any anchoring decision is made — see e.g. the
/// call before [`right_edge`] in `verify_range_proof`.
fn reject_odd_nibble_value_digests(proof_nodes: &[ProofNode]) -> Result<(), ProofError> {
    for node in proof_nodes {
        if !node.key.len().is_multiple_of(2) && node.value_digest.is_some() {
            return Err(ProofError::ValueAtOddNibbleLength);
        }
    }
    Ok(())
}

/// Compute the right edge of the proven range — the boundary key the
/// end-proof actually anchors at, and whether that boundary is inclusive
/// in-range or out-of-range relative to the proven range.
///
/// A previous heuristic on this code path was: "if the end-proof terminal
/// has a value digest AND its nibble path equals `nibbles(last_kv)`, anchor
/// at `last_kv`; otherwise anchor at the caller's `last_key` bound."
/// Suppose we have two structurally different proof shapes that produce
/// identical end-proofs:
///
/// 1. A complete *exclusion proof* of `last_key` whose path happens to
///    terminate at the node where `last_kv` lives (i.e., the terminal is the
///    real value node for `last_kv`, even though the caller asked about a
///    different — absent — `last_key`).
/// 2. An *inclusion proof of `last_kv`* (which arises when the response is
///    truncated, or whenever `last_kv == last_key`).
///
/// The old heuristic conflated the two. An attacker could drop trailing
/// entries from `key_values` so the new `last_kv` happened to coincide with
/// the proof's terminal; the dropped keys were then absorbed silently into
/// "outside the proven range" via the proof's stored child hashes, and the
/// tampered range still verified against the honest root.
///
/// # What this function does — the three legal shapes
///
/// Classifies the end-proof terminal against `last_kv` and returns a
/// [`RightBoundary`] whose variant encodes inclusivity. The caller treats
/// each variant as follows:
///
/// * [`RightBoundary::InRange`] — runs the full `verify_edge` against
///   the boundary to cryptographically anchor the proof.
/// * [`RightBoundary::OutOfRange`] — skips `verify_edge`; the anchor check
///   is folded into the hash reconstruction via the out-of-range-boundary
///   path in [`compute_outside_children`].
///
/// The classification:
///
/// * **Terminal value-node, full key `K > last_kv`** →
///   `OutOfRange(Cow::Owned(K))`. The proof structurally proves
///   `[first, K)`; `last_kv` is in that range, `K` is not. Covered by
///   `test_divergent_terminal_past_last_kv`.
/// * **Terminal value-node, full key `K == last_kv`** →
///   `InRange(Some(last_kv))`. The end-proof anchors directly at
///   `last_kv`. Covered by
///   `test_dropped_trailing_key_accepted_as_partial_coverage`.
/// * **Everything else** — terminal value-node with `K < last_kv` (whether
///   `K` is a strict prefix of `last_kv` or a divergent lex-less node),
///   terminal has no value digest, `end_proof` empty, or `key_values`
///   empty — → `InRange(fallback_last)`, anchored at the caller's
///   requested bound. Covered by
///   `test_terminal_strict_prefix_of_last_kv_verifies` and
///   `test_tampered_in_range_value_rejected`.
///
/// # Why partial coverage is OK
///
/// In the inclusive-at-`last_kv` shape (case 2), the proven range may be
/// strictly narrower than what the caller requested — the dropped-trailing-
/// key scenario (`test_dropped_trailing_key_accepted_as_partial_coverage`)
/// is the canonical case, since the attacker shrunk it on purpose. This is
/// a *valid* outcome, not a verification error. The caller is expected to
/// compare `key_values.last()` against their requested `last_key`; if the
/// proven range is narrower, they re-request `(last_kv, last_key]`. No data
/// is hidden — at most one extra round trip is induced, and the attacker
/// gains nothing.
///
/// # Why we still take `fallback_last`
///
/// Aspirationally we would like the hash reconstruction to depend *only* on
/// the proof and `key_values`, with `first_key` / `last_key` used solely for
/// structural range checks on the caller-provided bounds. We are not there
/// yet on the right edge: when the end-proof's terminal is a strict prefix
/// of `last_kv` with the on-path child set, [`compute_outside_children`]
/// marks `last_kv`'s on-path child as outside, so the hash reconstruction
/// substitutes the proof's stored child hash and would *not* notice a
/// tampered `last_kv` value. The cryptographic `verify_edge` call against
/// the caller's `last_key` is what catches that case
/// (`test_tampered_in_range_value_rejected`). We have no known attack
/// against the current design; the dependency on the caller's bound is
/// documented here so a future refactor can audit and remove it
/// deliberately if a fully self-contained verifier is wanted.
///
/// # Asymmetry with the left edge
///
/// There is no `LeftOutOfRange` counterpart to `RightBoundary::OutOfRange`.
/// The mirror construction (`terminal_full_key < first_kv` via a divergent
/// leaf in `prove(first_key)`) is empirically handled by the existing
/// [`EdgeBoundary::Left`] path; see `test_divergent_terminal_before_first_kv`.
/// The asymmetry holds because the in-range data on the left side is, by
/// construction, present in `key_values`, so there is no off-path single-node
/// subtree past `first_kv` that could synthesize incorrectly during hash
/// reconstruction.
fn right_edge<'a>(
    end_proof: &[ProofNode],
    last_kv: Option<&'a [u8]>,
    fallback_last: Option<&'a [u8]>,
) -> RightBoundary<'a> {
    let (Some(terminal), Some(last_kv_k)) = (end_proof.last(), last_kv) else {
        return RightBoundary::InRange(fallback_last);
    };

    // Terminal anchors at a real byte key only when it carries a value
    // digest. (Callers reject *any* end_proof node whose nibble path is
    // odd-length but carries a value before reaching here — see
    // [`reject_odd_nibble_value_digests`] — so we don't re-check parity.)
    if terminal.value_digest.is_some() {
        let nibs: Vec<u8> = terminal.key.iter().map(|c| c.as_u8()).collect();
        let terminal_full_key: Vec<u8> = Path::from(nibs.as_slice()).bytes_iter().collect();
        match terminal_full_key.as_slice().cmp(last_kv_k) {
            std::cmp::Ordering::Greater => {
                return RightBoundary::OutOfRange(Cow::Owned(terminal_full_key));
            }
            std::cmp::Ordering::Equal => return RightBoundary::InRange(Some(last_kv_k)),
            std::cmp::Ordering::Less => { /* fall through to fallback */ }
        }
    }

    RightBoundary::InRange(fallback_last)
}

/// Verify that a range proof is valid for the specified key range and root hash.
///
/// This function validates a range proof by constructing a partial trie from the
/// proof data and verifying that it produces the expected root hash.
///
/// # Verification Process
///
/// 1. **Structural validation**: Ensure key-value pairs are in strictly ascending order,
///    within the requested `[first_key, last_key]` range, and reject unexpected proofs
/// 2. **Right-edge anchor derivation**: Determine, from the proof's structure,
///    the actual bound the end-proof anchors at. This may be smaller than
///    `last_key`; partial coverage is a valid outcome, not an error.
/// 3. **Boundary proof verification**: Cryptographically verify the start
///    proof against `first_key`. The right edge is verified against the
///    *proof-derived* right boundary — when the boundary is in-range, via
///    `verify_edge` against either `last_kv` or the caller's `last_key`
///    (whichever the proof anchors at); when the boundary is out-of-range,
///    via the hash reconstruction below, where the anchor is the end-proof
///    terminal's full key.
/// 4. **Trie reconstruction**: Build an in-memory Merkle trie from the key-value pairs
///    and reconcile it with proof nodes
/// 5. **Root hash comparison**: Verify the reconstructed trie's root hash matches the
///    expected hash
///
/// # Partial coverage
///
/// A successful return does **not** guarantee that the proof covered the
/// caller's full requested range `[first_key, last_key]`. The proof
/// generator may have truncated the response (e.g. hit a max-length limit),
/// in which case the proven range is `[first_key, key_values.last()]` with
/// `key_values.last() < last_key`. This is a valid outcome of verification,
/// not an error.
///
/// Callers who need the full requested range must compare
/// `proof.key_values().last()` against their requested `last_key` after a
/// successful verification. If the proven right edge is short, re-request
/// `(key_values.last(), last_key]` and verify that next slice. Iterating
/// this loop is the canonical way to assemble full-range coverage from
/// truncated proofs.
///
/// # Errors
///
/// Returns [`api::Error::ProofError`] if the proof is structurally invalid,
/// keys are outside the requested range, boundary proofs fail verification,
/// or the reconstructed root hash doesn't match.
pub fn verify_range_proof<H: ProofCollection<Node = ProofNode>>(
    first_key: Option<impl KeyType>,
    last_key: Option<impl KeyType>,
    root_hash: &TrieHash,
    proof: &RangeProof<impl KeyType, impl ValueType, H>,
) -> Result<(), api::Error> {
    let first_key_bytes: Option<&[u8]> = first_key.as_ref().map(AsRef::as_ref);
    let last_key_bytes: Option<&[u8]> = last_key.as_ref().map(AsRef::as_ref);

    // Reject invalid range where start > end
    if let (Some(start), Some(end)) = (first_key_bytes, last_key_bytes)
        && start > end
    {
        return Err(api::Error::ProofError(ProofError::StartAfterEnd));
    }

    // check that the keys are in ascending order and within the requested range
    let key_values = proof.key_values();
    if !key_values
        .iter()
        .map(|(key, _)| key.as_ref())
        .is_sorted_by(|a, b| a < b)
    {
        return Err(api::Error::ProofError(
            ProofError::NonMonotonicIncreaseRange,
        ));
    }

    // Validate all keys are within the requested [first_key, last_key] range
    for (key, _) in key_values {
        let k = key.as_ref();
        if first_key_bytes.is_some_and(|start| k < start)
            || last_key_bytes.is_some_and(|end| k > end)
        {
            return Err(api::Error::ProofError(ProofError::KeyOutsideRange));
        }
    }

    if key_values.is_empty() && first_key_bytes.is_none() && last_key_bytes.is_none() {
        return Err(api::Error::ProofError(ProofError::Empty));
    }

    // Reject start proof when no start key is specified (non-canonical proof)
    if first_key_bytes.is_none() && !proof.start_proof().is_empty() {
        return Err(api::Error::ProofError(ProofError::UnexpectedStartProof));
    }

    // Require end proof when an end key is specified. An unbounded
    // request (last_key = None) is allowed to carry an empty end_proof
    // even when key_values is non-empty: `Merkle::range_proof` produces
    // exactly that shape, and the root-hash reconstruction below is the
    // safety net — any key the prover omitted past the last reported
    // entry would change the reconstructed hash.
    if proof.end_proof().is_empty() && last_key_bytes.is_some() {
        return Err(api::Error::ProofError(ProofError::NoEndProof));
    }

    // Sanity-check end_proof's structural invariants before any anchoring
    // decision is made. The right-edge `verify_edge` call below is conditional
    // (skipped when the boundary is exclusive), so without this scan the
    // end_proof would be unvalidated when `right_edge` reads its terminal
    // and could feed `right_edge` an odd-nibble value-node.
    reject_odd_nibble_value_digests(proof.end_proof().as_ref())?;

    let left_edge_kv = key_values.first().map(|(k, v)| (k.as_ref(), v.as_ref()));
    let right_edge_kv = key_values.last().map(|(k, v)| (k.as_ref(), v.as_ref()));

    // Left edge: full verification — the proof must anchor at `first_key`.
    if !proof.start_proof().is_empty() {
        verify_edge(
            first_key_bytes,
            left_edge_kv,
            proof.start_proof(),
            root_hash,
            ProofEdge::Left,
        )?;
    }

    // Right edge:
    //   * InRange (terminal == last_kv, or fallback to the caller's
    //     requested bound): run full `verify_edge`. This is what
    //     cryptographically anchors the proof at `last_kv`'s value when
    //     the proof's terminal sits at or above `last_kv`, since the
    //     hash-reconstruction path treats `last_kv`'s on-path child as
    //     outside-the-range and would otherwise miss a tampered `last_kv`
    //     value.
    //   * OutOfRange (terminal_full_key > last_kv): skip
    //     `proof.verify(...)` — the proof was structurally generated for
    //     a deeper bound, and the cryptographic check is folded into the
    //     hash reconstruction via the out-of-range-boundary path in
    //     `compute_outside_children`. We still keep the ordering sanity
    //     check.
    let last_kv_bytes = key_values.last().map(|(k, _)| k.as_ref());
    let right_boundary = right_edge(proof.end_proof().as_ref(), last_kv_bytes, last_key_bytes);
    match &right_boundary {
        RightBoundary::InRange(bound) => {
            verify_edge(
                *bound,
                right_edge_kv,
                proof.end_proof(),
                root_hash,
                ProofEdge::Right,
            )?;
        }
        RightBoundary::OutOfRange(bound) => {
            // `right_edge` only returns `OutOfRange(K)` when `K > last_kv`
            // strictly (terminal value-node past last_kv). The runtime
            // check below catches a broken contract; the debug_assert pins
            // the intended invariant.
            debug_assert!(
                right_edge_kv.is_none_or(|(edge_key, _)| bound.as_ref() > edge_key),
                "RightBoundary::OutOfRange must be strictly greater than last_kv",
            );
            if let Some((edge_key, _)) = right_edge_kv
                && bound.as_ref() < edge_key
            {
                return Err(api::Error::ProofError(
                    ProofError::RangeProofEndBeforeLastKey,
                ));
            }
        }
    }

    let all_proof_nodes: Box<[&ProofNode]> = proof
        .start_proof()
        .as_ref()
        .iter()
        .chain(proof.end_proof().as_ref())
        .collect();

    verify_proof_node_values(
        &all_proof_nodes,
        first_key_bytes,
        &right_boundary,
        key_values,
    )?;

    verify_range_proof_root_hash(
        &all_proof_nodes,
        key_values,
        proof,
        first_key_bytes,
        right_boundary,
        root_hash,
    )
}

/// Verifies that proof nodes with values within the range are included in `key_values`.
/// Without this check, an attacker could hide key-value pairs that exist on an edge
/// proof path by omitting them from `key_values` while reconciliation silently inserts
/// them, making the root hash correct.
///
/// The right edge of the in-range comparison comes from `right_boundary`:
/// `InRange(b)` includes `b`, `OutOfRange(b)` excludes it,
/// `InRange(None)` has no upper bound.
fn verify_proof_node_values(
    proof_nodes: &[&ProofNode],
    first_key_bytes: Option<&[u8]>,
    right_boundary: &RightBoundary<'_>,
    key_values: &[(impl KeyType, impl ValueType)],
) -> Result<(), api::Error> {
    for proof_node in proof_nodes {
        // Only even-nibble-length keys correspond to byte keys with values
        if !proof_node.key.len().is_multiple_of(2) {
            continue;
        }
        // Only check nodes whose value digest is the actual value (not a Hash)
        let Some(ValueDigest::Value(proof_value_bytes)) = &proof_node.value_digest else {
            continue;
        };
        let proof_value = proof_value_bytes.as_ref();
        let key_nibbles: Vec<u8> = proof_node
            .key
            .iter()
            .map(|component| component.as_u8())
            .collect();
        let node_key_bytes: Vec<u8> = Path::from(key_nibbles.as_slice()).bytes_iter().collect();
        let within_right = match right_boundary {
            RightBoundary::InRange(None) => true,
            RightBoundary::InRange(Some(end)) => node_key_bytes.as_slice() <= *end,
            RightBoundary::OutOfRange(end) => node_key_bytes.as_slice() < end.as_ref(),
        };
        let in_range =
            within_right && first_key_bytes.is_none_or(|start| node_key_bytes.as_slice() >= start);
        if in_range {
            match key_values.binary_search_by(|(k, _)| k.as_ref().cmp(node_key_bytes.as_slice())) {
                Err(_) => {
                    return Err(api::Error::ProofError(
                        ProofError::ProofNodeHasUnincludedValue,
                    ));
                }
                Ok(idx) => {
                    let Some((_, kv_value)) = key_values.get(idx) else {
                        unreachable!("binary_search returned valid index");
                    };
                    if kv_value.as_ref() != proof_value {
                        return Err(api::Error::ProofError(ProofError::ProofNodeValueMismatch));
                    }
                }
            }
        }
    }
    Ok(())
}

/// Reconstructs the trie from key-value pairs and proof nodes, then verifies
/// that the computed root hash matches the expected one.
///
/// `right_boundary` describes the right edge of the proven range. When the
/// variant is [`RightBoundary::OutOfRange`], the boundary key sits at the
/// end-proof's terminal node and is treated as outside the proven range.
fn verify_range_proof_root_hash<H: ProofCollection<Node = ProofNode>>(
    all_proof_nodes: &[&ProofNode],
    key_values: &[(impl KeyType, impl ValueType)],
    proof: &RangeProof<impl KeyType, impl ValueType, H>,
    first_key_bytes: Option<&[u8]>,
    right_boundary: RightBoundary<'_>,
    root_hash: &TrieHash,
) -> Result<(), api::Error> {
    // Build in-memory merkle from key-value pairs
    let memstore = MemStore::default();
    let nodestore = NodeStore::new_empty_proposal(memstore.into());
    let mut proving_merkle: Merkle<NodeStore<Mutable<Propose>, MemStore>> = Merkle::from(nodestore);

    for (key, value) in key_values {
        proving_merkle.insert(key.as_ref(), value.as_ref().into())?;
    }

    let mut proof_node_map: HashMap<PathBuf, &ProofNode> = HashMap::new();
    for proof_node in all_proof_nodes {
        // The callback handles value conflicts between proof nodes and the
        // proving trie. For range proofs:
        //  - ValueDigest::Value: The proof node carries a value that the
        //    proving trie doesn't have (e.g., empty range where no key-value
        //    pairs were inserted). Accept the proof's value.
        //  - ValueDigest::Hash: When the hash matches the branch value,
        //    reconcile_branch_proof_node returns early (no callback). When the
        //    branch has no value, the node is out of range — its subtree is
        //    represented by the proof's stored hash, so we accept it with no
        //    value (`Ok(None)`). A hash mismatch against an *existing* branch
        //    value is a real conflict and is rejected. (In-range range-proof
        //    keys are always present in `key_values`, so a missing branch
        //    value here can only be out of range.)
        //  - ValueDigest::None: Unreachable — if the trie has a value, the
        //    proof node will carry Value or Hash, not None.
        proving_merkle.reconcile_branch_proof_node(proof_node, |pn, branch_value| {
            match &pn.value_digest {
                Some(ValueDigest::Value(v)) => Ok(Some(v.clone())),
                _ if branch_value.is_none() => Ok(None),
                _ => Err(ProofError::UnexpectedValue),
            }
        })?;
        match proof_node_map.entry(proof_node.key.clone()) {
            std::collections::hash_map::Entry::Occupied(existing) => {
                if *existing.get() != *proof_node {
                    return Err(api::Error::ProofError(ProofError::ConflictingProofNodes));
                }
            }
            std::collections::hash_map::Entry::Vacant(entry) => {
                entry.insert(proof_node);
            }
        }
    }

    // Compute which children at each edge node are outside the proven range
    let mut outside_children = compute_outside_children(
        proof.start_proof().as_ref(),
        EdgeBoundary::Left(first_key_bytes),
    )?;
    for (key, flags) in compute_outside_children(
        proof.end_proof().as_ref(),
        EdgeBoundary::Right(right_boundary),
    )? {
        let entry = outside_children.entry(key).or_default();
        *entry |= flags;
    }

    // Compute root hash of the proving trie with proof sibling hashes
    let Some(root_node) = proving_merkle.root() else {
        return Err(api::Error::ProofError(ProofError::Empty));
    };

    let computed =
        compute_root_hash_with_proofs(&root_node, &[], &proof_node_map, &outside_children);

    let expected = root_hash.clone().into_hash_type();
    if computed != expected {
        return Err(api::Error::ProofError(ProofError::UnexpectedHash {
            expected,
            actual: computed,
        }));
    }

    Ok(())
}

/// Verify that the proposal (`start_root` + `batch_ops`) is consistent with
/// `end_root` within the proven range (phase 3 of change proof verification).
///
/// Forks the proposal into a proving trie, reconciles boundary proof nodes,
/// collapses out-of-range branches (rejecting in-range children at
/// intermediate positions), and computes a hybrid root hash. See the
/// [module-level documentation](crate::proofs#change-proof-verification-algorithm)
/// for the full algorithm description.
///
/// # Errors
///
/// Returns [`api::Error::ProofError`] if:
/// - The computed root hash doesn't match `end_root`
/// - Proof nodes conflict with the proving trie at in-range positions
/// - An in-range child is found at an intermediate node between
///   consecutive proof nodes (indicates tampered `batch_ops`)
///
/// # Panics
///
/// Panics if reconciling non-empty boundary proof nodes into the proving
/// trie fails to create a root node (this is structurally guaranteed by
/// `insert_branch_from_nibbles`).
pub fn verify_change_proof_root_hash(
    proof: &FrozenChangeProof,
    verification: &ChangeProofVerificationContext,
    proposal: &crate::db::Proposal<'_>,
) -> Result<(), api::Error> {
    let start_nodes: &[ProofNode] = proof.start_proof().as_ref();
    let end_nodes: &[ProofNode] = proof.end_proof().as_ref();

    // Case 1: Both proofs empty — this is a "complete" proof covering the
    // entire keyspace (no boundaries). The proposal should contain the
    // full target state, so compare its root hash directly against
    // end_root. Also covers the degenerate case of an empty diff.
    if start_nodes.is_empty() && end_nodes.is_empty() {
        let computed = api::DbView::root_hash(proposal).unwrap_or_else(HashKey::empty);
        if computed != verification.end_root {
            return Err(api::Error::ProofError(ProofError::EndRootMismatch));
        }
        return Ok(());
    }

    // Fork the proposal's nodestore into a mutable proving trie.
    // The proposal already contains the correct trie structure; forking
    // preserves it so we don't need to re-insert keys.
    let forked = NodeStore::new(proposal.inner_nodestore())?;
    let mut proving_merkle = Merkle::from(forked);

    // Reconcile all boundary proof nodes into the proving trie via
    // `reconcile_branch_proof_node`. This inserts branch structure matching
    // `end_root`'s layout so that the hash computation produces the same
    // trie shape.
    let mut proof_node_map: HashMap<PathBuf, &ProofNode> = HashMap::new();

    // Start proof nodes that are out of range may have values that differ
    // between end_root (proof) and the proposal (which has start_root's
    // values outside the range). Overwrite with the proof's value so the
    // hash computation uses end_root's value.
    //
    // Out-of-range nodes include:
    //  - proper prefixes of start_key (ancestors on the path)
    //  - divergent nodes whose key is NOT a prefix of start_key (these
    //    appear in exclusion proofs when start_key doesn't exist)
    //
    // Only nodes whose key exactly equals start_key are in-range; value
    // mismatches there are real errors.
    let start_key_nibbles: Vec<u8> = verification
        .start_key
        .as_deref()
        .map(|k| NibblesIterator::new(k).collect())
        .unwrap_or_default();

    for proof_node in start_nodes {
        proving_merkle.reconcile_branch_proof_node(proof_node, |pn, _branch_value| {
            let node_nibbles: Vec<u8> = pn.key.iter().map(|c| c.as_u8()).collect();
            // A start proof node is in-range if its key >= start_key. This
            // covers both inclusion proofs (key == start_key) and exclusion
            // proofs where the terminal node overshoots start_key to the
            // nearest existing key. Reaching this guard at an in-range
            // position is always a real error: the proposal already holds the
            // correct value, so the only way the proof node and branch
            // disagree is tampering — e.g. a dropped in-range `Put` whose
            // boundary proof still carries the omitted key's hash.
            if node_nibbles >= start_key_nibbles {
                Err(ProofError::UnexpectedValue)
            } else {
                match &pn.value_digest {
                    Some(ValueDigest::Value(v)) => Ok(Some(v.clone())),
                    _ => Ok(None),
                }
            }
        })?;
        proof_node_map.insert(proof_node.key.clone(), proof_node);
    }

    // Same treatment for end proof nodes: ancestors and divergent nodes
    // of the right boundary may have out-of-range values that differ
    // from the proposal.
    let end_key_nibbles: Vec<u8> = verification
        .right_edge_key
        .as_deref()
        .map(|k| NibblesIterator::new(k).collect())
        .unwrap_or_default();

    for proof_node in end_nodes {
        proving_merkle.reconcile_branch_proof_node(proof_node, |pn, _branch_value| {
            let node_nibbles: Vec<u8> = pn.key.iter().map(|c| c.as_u8()).collect();
            // Symmetric to the start proof: an end proof node is in-range
            // if its key <= end_key (covers exclusion proofs where the
            // terminal undershoots to the nearest existing key).
            if node_nibbles <= end_key_nibbles {
                Err(ProofError::UnexpectedValue)
            } else {
                match &pn.value_digest {
                    Some(ValueDigest::Value(v)) => Ok(Some(v.clone())),
                    _ => Ok(None),
                }
            }
        })?;
        match proof_node_map.entry(proof_node.key.clone()) {
            std::collections::hash_map::Entry::Occupied(existing) => {
                if *existing.get() != proof_node {
                    return Err(api::Error::ProofError(ProofError::ConflictingProofNodes));
                }
            }
            std::collections::hash_map::Entry::Vacant(entry) => {
                entry.insert(proof_node);
            }
        }
    }

    // Collapse intermediate branches along proof paths. The fork may have
    // extra branch structure from out-of-range keys that doesn't exist in
    // end_root's trie. Between consecutive proof nodes, the proof implies a
    // direct path with no intermediate branches.
    //
    // Out-of-range children are stripped. In-range children that are also
    // proposal-local (created by this proposal's batch_ops, not inherited
    // from the parent) indicate tampered operations and trigger rejection.
    let range = Some((start_key_nibbles.as_slice(), end_key_nibbles.as_slice()));
    for [parent, child] in start_nodes.array_windows() {
        proving_merkle.collapse_branch_to_path(&parent.key, &child.key, range)?;
    }
    for [parent, child] in end_nodes.array_windows() {
        proving_merkle.collapse_branch_to_path(&parent.key, &child.key, range)?;
    }

    // If the end trie's root has a non-empty partial_path, the first proof
    // node sits deeper than the proposal root. Collapse the proving trie's
    // root to match so that out-of-range children above the first proof
    // node are stripped and the root shape matches end_root.
    let first_proof_key = start_nodes
        .first()
        .or_else(|| end_nodes.first())
        .map(|n| n.key.as_ref());
    if let Some(key) = first_proof_key {
        proving_merkle.collapse_root_to_path(key, range)?;
    }

    // Compute which children at each boundary node are outside the proven
    // range via `compute_outside_children`.
    let mut outside_children = compute_outside_children(
        start_nodes,
        EdgeBoundary::Left(verification.start_key.as_deref()),
    )?;
    for (key, flags) in compute_outside_children(
        end_nodes,
        EdgeBoundary::Right(RightBoundary::InRange(
            verification.right_edge_key.as_deref(),
        )),
    )? {
        let entry = outside_children.entry(key).or_default();
        *entry |= flags;
    }

    // Compute the hybrid root hash via `compute_root_hash_with_proofs`.
    // In-range children are hashed from the proving trie. Out-of-range
    // children use hashes from the proof nodes.
    let root_node = proving_merkle
        .root()
        .expect("a non-empty proof reconciliation always leaves behind a root node");

    let computed =
        compute_root_hash_with_proofs(&root_node, &[], &proof_node_map, &outside_children);

    if computed != verification.end_root.clone().into_hash_type() {
        return Err(api::Error::ProofError(ProofError::EndRootMismatch));
    }

    Ok(())
}

impl<T: TrieReader> Merkle<T> {
    pub(crate) fn root(&self) -> Option<SharedNode> {
        self.nodestore.root_node()
    }

    // Must be pub because it is used in FFI calls.
    pub const fn nodestore(&self) -> &T {
        &self.nodestore
    }

    /// Returns a proof that the given key has a certain value,
    /// or that the key isn't in the trie.
    ///
    /// ## Errors
    ///
    /// Returns an error if the trie is empty or an error occurs while reading from storage.
    pub fn prove(&self, key: &[u8]) -> Result<FrozenProof, ProofError> {
        self.root().ok_or(ProofError::Empty)?;

        // PathIterator yields every node on the path from root toward
        // `key`, including divergent nodes whose partial_path doesn't
        // match (they prove the key's absence in exclusion proofs).
        let proof = self
            .path_iter(key)?
            .map(|node| node.map(ProofNode::from))
            .collect::<Result<_, _>>()?;

        Ok(Proof::new(proof))
    }

    /// Merges a sequence of key-value pairs with the base merkle trie, yielding
    /// a sequence of [`BatchOp`]s that describe the changes.
    ///
    /// The key-value range is considered total, meaning keys within the inclusive
    /// bounds that are present within the base trie but not in the key-value iterator
    /// will be yielded as [`BatchOp::Delete`] or [`BatchOp::DeleteRange`] operations.
    ///
    /// # Invariant
    ///
    /// The key-value pairs provided by the `key_values` iterator must be sorted in
    /// ascending order, not contain duplicates, and must lie within the specified
    /// `first_key` and `last_key` bounds (if provided). Behavior is unspecified if
    /// this invariant is violated and no verification is performed.
    ///
    /// # Errors
    ///
    /// Returns an error if there is an issue reading from the underlying trie storage.
    ///
    /// [`BatchOp`]: crate::db::BatchOp
    /// [`BatchOp::Delete`]: crate::db::BatchOp::Delete
    /// [`BatchOp::DeleteRange`]: crate::db::BatchOp::DeleteRange
    pub fn merge_key_value_range<V: ValueType>(
        &self,
        first_key: Option<impl KeyType>,
        last_key: Option<impl KeyType>,
        key_values: impl IntoIterator<Item: KeyValuePair<Value = V>>,
    ) -> impl BatchIter {
        merge::MergeKeyValueIter::new(self, first_key, last_key, key_values)
    }

    pub(crate) fn path_iter<'a>(
        &self,
        key: &'a [u8],
    ) -> Result<PathIterator<'_, 'a, T>, FileIoError> {
        PathIterator::new(&self.nodestore, key)
    }

    pub(super) fn key_value_iter_from_key(&self, key: &[u8]) -> MerkleKeyValueIter<'_, T> {
        MerkleKeyValueIter::from_key(&self.nodestore, key)
    }

    /// Generate a cryptographic proof for a range of key-value pairs in the Merkle trie.
    ///
    /// This method creates a range proof that can be used to verify the existence (or absence)
    /// of a contiguous set of keys within the trie. The proof includes boundary proofs and
    /// the actual key-value pairs within the specified range.
    ///
    /// # Parameters
    ///
    /// * `start_key` - The optional lower bound of the range (inclusive).
    ///   - If `Some(key)`, the proof will include all keys >= this key
    ///   - If `None`, the proof starts from the beginning of the trie
    ///
    /// * `end_key` - The optional upper bound of the range (inclusive).
    ///   - If `Some(key)`, the proof will include all keys <= this key
    ///   - If `None`, the proof extends to the end of the trie
    ///
    /// * `limit` - Optional maximum number of key-value pairs to include in the proof.
    ///   - If `Some(n)`, at most n key-value pairs will be included
    ///   - If `None`, all key-value pairs in the range will be included
    ///   - Useful for paginating through large ranges
    ///   - **NOTE**: avalanchego's limit is based on the entire packet size and not the
    ///     number of key-value pairs. Currently, we only limit by the number of pairs.
    ///
    /// # Returns
    ///
    /// A `FrozenRangeProof` containing:
    /// - Start proof: Merkle proof for the first key in the range
    /// - End proof: Merkle proof for the last key in the range
    /// - Key-value pairs: All entries within the specified bounds (up to the limit)
    ///
    /// # Errors
    ///
    /// * `api::Error::InvalidRange` - If `start_key` > `end_key` when both are provided.
    ///   This ensures the range bounds are logically consistent.
    ///
    /// * `api::Error::RangeProofOnEmptyTrie` - If the trie is empty and the caller
    ///   requests a proof for the entire trie (both `start_key` and `end_key` are `None`).
    ///   This prevents generating meaningless proofs for non-existent data.
    ///
    /// * `api::Error` - Various other errors can occur during proof generation, such as:
    ///   - I/O errors when reading nodes from storage
    ///   - Corrupted trie structure
    ///   - Invalid node references
    pub(super) fn range_proof(
        &self,
        start_key: Option<&[u8]>,
        end_key: Option<&[u8]>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, api::Error> {
        if let (Some(k1), Some(k2)) = (&start_key, &end_key)
            && k1 > k2
        {
            return Err(api::Error::InvalidRange {
                start_key: k1.to_vec().into(),
                end_key: k2.to_vec().into(),
            });
        }

        // if there is a requested lower bound, the start proof must always be
        // for that key even if (especially if) the key is not present in the
        // trie so that the requestor can assert no keys exist between the
        // requested key and the first provided key.
        let start_proof = start_key
            .map(|key| self.prove(key))
            .transpose()?
            .unwrap_or_default();

        let mut iter = self
            .key_value_iter_from_key(start_key.unwrap_or_default())
            .stop_after_key(end_key);

        // don't consume the iterator so we can determine if we hit the
        // limit or exhausted the iterator later
        let key_values = iter
            .by_ref()
            .take(limit.map_or(usize::MAX, NonZeroUsize::get))
            .collect::<Result<Box<_>, FileIoError>>()?;

        if key_values.is_empty() && start_key.is_none() && end_key.is_none() {
            // unbounded range proof yielded no key-values, so the trie must be empty
            return Err(api::Error::RangeProofOnEmptyTrie);
        }

        let end_proof = if let Some(limit) = limit
            && limit.get() <= key_values.len()
            && iter.next().is_some()
        {
            // limit was provided, we hit it, and there is at least one more key
            // end proof is for the last key provided
            key_values.last().map(|(largest_key, _)| &**largest_key)
        } else {
            // limit was not hit or not provided, end proof is for the requested
            // end key so that we can prove we have all keys up to that key
            end_key
        }
        .map(|end_key| self.prove(end_key))
        .transpose()?
        .unwrap_or_default();

        firewood_histogram!(PROOF_KEYS, "kind" => "range").record_integer(key_values.len());

        Ok(RangeProof::new(start_proof, end_proof, key_values))
    }

    pub(crate) fn get_value(&self, key: &[u8]) -> Result<Option<Value>, FileIoError> {
        let Some(node) = self.get_node(key)? else {
            return Ok(None);
        };
        Ok(node.value().map(|v| v.to_vec().into_boxed_slice()))
    }

    pub(crate) fn get_node(&self, key: &[u8]) -> Result<Option<SharedNode>, FileIoError> {
        let Some(root) = self.root() else {
            return Ok(None);
        };

        let key = Path::from_nibbles_iterator(NibblesIterator::new(key));
        get_helper(&self.nodestore, &root, &key)
    }

    /// Dump a node, recursively, to a dot file.
    ///
    /// The `keep_alive` vec holds references to all `MaybePersistedNode`s
    /// created during the dump so their addresses remain stable for dup
    /// detection in `seen`.
    pub(crate) fn dump_node<W: std::io::Write + ?Sized>(
        &self,
        node: &MaybePersistedNode,
        hash: Option<&HashType>,
        seen: &mut HashSet<String>,
        keep_alive: &mut Vec<MaybePersistedNode>,
        writer: &mut W,
    ) -> Result<(), FileIoError> {
        writeln!(writer, "  {node}[label=\"{node}")
            .map_err(Error::other)
            .map_err(|e| FileIoError::new(e, None, 0, None))?;
        if let Some(hash) = hash {
            write!(writer, " H={hash:.6?}")
                .map_err(Error::other)
                .map_err(|e| FileIoError::new(e, None, 0, None))?;
        }

        match &*node.as_shared_node(&self.nodestore)? {
            Node::Branch(b) => {
                write_attributes!(writer, b, &b.value.clone().unwrap_or(Box::from([])));
                writeln!(writer, "\"]")
                    .map_err(|e| FileIoError::from_generic_no_file(e, "write branch"))?;
                for (childidx, child) in &b.children {
                    let (child, child_hash) = match child {
                        None => continue,
                        Some(node) => (node.as_maybe_persisted_node(), node.hash()),
                    };

                    let inserted = seen.insert(format!("{child}"));
                    keep_alive.push(child.clone());
                    if inserted {
                        writeln!(writer, "  {node} -> {child}[label=\"{childidx:x}\"]")
                            .map_err(|e| FileIoError::from_generic_no_file(e, "write branch"))?;
                        self.dump_node(&child, child_hash, seen, keep_alive, writer)?;
                    } else {
                        // We have already seen this child, which shouldn't happen.
                        // Indicate this with a red edge.
                        writeln!(
                            writer,
                            "  {node} -> {child}[label=\"{childidx:x} (dup)\" color=red]"
                        )
                        .map_err(|e| FileIoError::from_generic_no_file(e, "write branch"))?;
                    }
                }
            }
            Node::Leaf(l) => {
                write_attributes!(writer, l, &l.value);
                writeln!(writer, "\" shape=rect]")
                    .map_err(|e| FileIoError::from_generic_no_file(e, "write leaf"))?;
            }
        }
        Ok(())
    }

    /// Dump the trie to a dot file without requiring hashed nodes.
    ///
    /// This works on any `TrieReader` including mutable proposals that
    /// haven't been frozen yet. The root hash will be omitted from the output.
    ///
    /// # Errors
    ///
    /// Returns an error if writing to the output writer fails.
    pub(crate) fn dump<W: std::io::Write + ?Sized>(&self, writer: &mut W) -> Result<(), Error> {
        let root = self.nodestore.root_as_maybe_persisted_node();

        writeln!(writer, "digraph Merkle {{\n  rankdir=LR;").map_err(Error::other)?;
        if let Some(root) = root {
            writeln!(writer, " root -> {root}")
                .map_err(Error::other)
                .map_err(|e| FileIoError::new(e, None, 0, None))
                .map_err(Error::other)?;
            let mut seen = HashSet::new();
            let mut keep_alive = Vec::new();
            self.dump_node(&root, None, &mut seen, &mut keep_alive, writer)
                .map_err(Error::other)?;
        }
        writeln!(writer, "}}")
            .map_err(Error::other)
            .map_err(|e| FileIoError::new(e, None, 0, None))
            .map_err(Error::other)?;

        Ok(())
    }

    /// Dump the trie to a string (for testing or logging).
    ///
    /// # Errors
    ///
    /// Returns an error if writing to the string fails.
    pub(crate) fn dump_to_string(&self) -> Result<String, Error> {
        let mut buffer = Vec::new();
        self.dump(&mut buffer)?;
        String::from_utf8(buffer).map_err(Error::other)
    }
}

impl<T: HashedNodeReader> Merkle<T> {
    /// Generate a change proof
    ///
    /// # Errors
    ///
    /// * `api::Error::InvalidRange` - If `start_key` > `end_key` when both are provided.
    ///   This ensures the range bounds are logically consistent.
    ///
    /// * `api::Error` - Various other errors can occur during proof generation, such as:
    ///   - I/O errors when reading nodes from storage
    ///   - Corrupted trie structure
    ///   - Invalid node references
    pub fn change_proof(
        &self,
        start_key: Option<&[u8]>,
        end_key: Option<&[u8]>,
        source_trie: &T,
        limit: Option<NonZeroUsize>,
    ) -> Result<api::FrozenChangeProof, api::Error> {
        if let (Some(k1), Some(k2)) = (&start_key, &end_key)
            && k1 > k2
        {
            return Err(api::Error::InvalidRange {
                start_key: k1.to_vec().into(),
                end_key: k2.to_vec().into(),
            });
        }

        // If there is a requested lower bound, the start proof must always be
        // for that key even if (especially if) the key is not present in the
        // trie so that the requestor can assert no keys exist between the
        // requested key and the first provided key.
        let start_proof = start_key
            .map(|key| self.prove(key))
            .transpose()?
            .unwrap_or_default();

        // Create a difference iterator between the two tries with the given start
        // key and end key.
        let iter_stop_key: Key = end_key.unwrap_or_default().into();
        let mut iter = DiffMerkleNodeStream::new(
            source_trie,
            self.nodestore(),
            start_key.unwrap_or_default().into(),
        )?
        .map_while(|op| match op {
            Ok(op) => {
                if iter_stop_key.is_empty() || op.key() <= &iter_stop_key {
                    Some(Ok(op))
                } else {
                    None
                }
            }
            Err(e) => Some(Err(e)),
        });

        // Create an array of `BatchOp`s representing the difference between the two
        // revisions. The size of the array is bounded by `limit`.
        let batch_ops = iter
            .by_ref()
            .take(limit.map_or(usize::MAX, NonZeroUsize::get))
            .collect::<Result<Box<_>, FileIoError>>()?;

        // Check whether the limit cut off remaining items.
        let hit_limit = iter.next().transpose()?.is_some();

        // When the limit was hit, the end proof is for the last key in
        // batch_ops (the actual right edge of what was produced). When
        // all items fit, the end proof is for end_key so the verifier
        // can check the full requested range.
        let end_proof_key = if hit_limit {
            batch_ops.last().map(|op| &**op.key()).or(end_key)
        } else {
            end_key.or(batch_ops.last().map(|op| &**op.key()))
        };
        let end_proof = end_proof_key
            .map(|key| self.prove(key))
            .transpose()?
            .unwrap_or_default();

        firewood_histogram!(PROOF_KEYS, "kind" => "change").record_integer(batch_ops.len());

        Ok(ChangeProof::new(start_proof, end_proof, batch_ops))
    }
}

#[cfg(test)]
impl<F: firewood_storage::Parentable, S: ReadableStorage> Merkle<NodeStore<F, S>> {
    /// Forks the current Merkle trie into a new mutable proposal.
    ///
    /// ## Errors
    ///
    /// Returns an error if the nodestore cannot be created. See [`NodeStore::new`].
    pub fn fork(&self) -> Result<Merkle<NodeStore<Mutable<Propose>, S>>, FileIoError> {
        NodeStore::new(&self.nodestore).map(Into::into)
    }
}

impl<S: ReadableStorage> TryFrom<Merkle<NodeStore<Mutable<Propose>, S>>>
    for Merkle<NodeStore<Arc<ImmutableProposal>, S>>
{
    type Error = FileIoError;
    fn try_from(m: Merkle<NodeStore<Mutable<Propose>, S>>) -> Result<Self, Self::Error> {
        Ok(Merkle {
            nodestore: m.nodestore.try_into()?,
        })
    }
}

#[cfg(any(test, feature = "test_utils"))]
impl<S: ReadableStorage> Merkle<NodeStore<Mutable<Propose>, S>> {
    /// Convert a merkle backed by a `Mutable<Propose>` into an `ImmutableProposal`
    ///
    /// This function is only used in benchmarks and tests
    ///
    /// ## Panics
    ///
    /// Panics if the conversion fails. This should only be used in tests or benchmarks.
    #[must_use]
    pub fn hash(self) -> Merkle<NodeStore<Arc<ImmutableProposal>, S>> {
        self.try_into().expect("failed to convert")
    }
}

impl<K: MutableKind, S: ReadableStorage> Merkle<NodeStore<Mutable<K>, S>> {
    fn read_for_update(&mut self, child: Child) -> Result<Node, FileIoError> {
        match child {
            Child::Node(node) => Ok(node),
            Child::AddressWithHash(addr, _) => self.nodestore.read_for_update(addr.into()),
            Child::MaybePersisted(node, _) => self.nodestore.read_for_update(node),
        }
    }

    /// Map `key` to `value` in the trie.
    /// Each element of key is 2 nibbles.
    #[cfg_attr(feature = "test_utils", expect(clippy::missing_errors_doc))]
    pub fn insert(&mut self, key: &[u8], value: Value) -> Result<(), FileIoError> {
        self.insert_from_iter(NibblesIterator::new(key), value)
    }

    /// Map `key` to `value` in the trie when `key` is a `NibblesIterator`
    pub(crate) fn insert_from_iter(
        &mut self,
        key: NibblesIterator<'_>,
        value: Value,
    ) -> Result<(), FileIoError> {
        let key = Path::from_nibbles_iterator(key);
        let root = self.nodestore.root_mut();
        let Some(root_node) = std::mem::take(root) else {
            // The trie is empty. Create a new leaf node with `value` and set
            // it as the root.
            let root_node = Node::Leaf(LeafNode {
                partial_path: key,
                value,
            });
            *root = root_node.into();
            return Ok(());
        };

        let root_node = self.insert_helper(root_node, key.as_ref(), value)?;
        *self.nodestore.root_mut() = root_node.into();
        Ok(())
    }

    /// Map `key` to `value` into the subtrie rooted at `node`.
    /// Each element of `key` is 1 nibble.
    /// Returns the new root of the subtrie.
    fn insert_helper(
        &mut self,
        mut node: Node,
        key: &[u8],
        value: Value,
    ) -> Result<Node, FileIoError> {
        // 4 possibilities for the position of the `key` relative to `node`:
        // 1. The node is at `key`
        // 2. The key is above the node (i.e. its ancestor)
        // 3. The key is below the node (i.e. its descendant)
        // 4. Neither is an ancestor of the other
        let path_overlap = PrefixOverlap::from(key, node.partial_path().as_ref());

        let unique_key = path_overlap.unique_a;
        let unique_node = path_overlap.unique_b;

        match (
            unique_key
                .split_first()
                .map(|(index, path)| (*index, path.into())),
            unique_node
                .split_first()
                .map(|(index, path)| (*index, path.into())),
        ) {
            (None, None) => {
                // 1. The node is at `key`
                node.update_value(value);
                firewood_counter!(INSERT, "operation" => "update").increment(1);
                Ok(node)
            }
            (None, Some((child_index, partial_path))) => {
                let child_index = PathComponent::try_new(child_index).expect("valid component");
                // 2. The key is above the node (i.e. its ancestor)
                // Make a new branch node and insert the current node as a child.
                //    ...                ...
                //     |     -->          |
                //    node               key
                //                        |
                //                       node
                let mut branch = BranchNode {
                    partial_path: path_overlap.shared.into(),
                    value: Some(value),
                    children: Children::new(),
                };

                // Shorten the node's partial path since it has a new parent.
                node.update_partial_path(partial_path);
                branch.children[child_index] = Some(Child::Node(node));
                firewood_counter!(INSERT, "operation" => "above").increment(1);

                Ok(Node::Branch(Box::new(branch)))
            }
            (Some((child_index, partial_path)), None) => {
                let child_index = PathComponent::try_new(child_index).expect("valid component");
                // 3. The key is below the node (i.e. its descendant)
                //    ...                         ...
                //     |                           |
                //    node         -->            node
                //     |                           |
                //    ... (key may be below)       ... (key is below)
                match node {
                    Node::Branch(ref mut branch) => {
                        let Some(child) = branch.children.take(child_index) else {
                            // There is no child at this index.
                            // Create a new leaf and put it here.
                            let new_leaf = Node::Leaf(LeafNode {
                                value,
                                partial_path,
                            });
                            branch.children[child_index] = Some(Child::Node(new_leaf));
                            firewood_counter!(INSERT, "operation" => "below").increment(1);
                            return Ok(node);
                        };
                        let child = self.read_for_update(child)?;
                        let child = self.insert_helper(child, partial_path.as_ref(), value)?;
                        branch.children[child_index] = Some(Child::Node(child));
                        Ok(node)
                    }
                    Node::Leaf(leaf) => {
                        // Turn this node into a branch node and put a new leaf as a child.
                        let mut branch = BranchNode {
                            partial_path: leaf.partial_path,
                            value: Some(leaf.value),
                            children: Children::new(),
                        };

                        let new_leaf = Node::Leaf(LeafNode {
                            value,
                            partial_path,
                        });

                        branch.children[child_index] = Some(Child::Node(new_leaf));

                        firewood_counter!(INSERT, "operation" => "split").increment(1);
                        Ok(Node::Branch(Box::new(branch)))
                    }
                }
            }
            (Some((key_index, key_partial_path)), Some((node_index, node_partial_path))) => {
                let key_index = PathComponent::try_new(key_index).expect("valid component");
                let node_index = PathComponent::try_new(node_index).expect("valid component");
                // 4. Neither is an ancestor of the other
                //    ...                         ...
                //     |                           |
                //    node         -->            branch
                //     |                           |    \
                //                               node   key
                // Make a branch node that has both the current node and a new leaf node as children.
                let mut branch = BranchNode {
                    partial_path: path_overlap.shared.into(),
                    value: None,
                    children: Children::new(),
                };

                node.update_partial_path(node_partial_path);
                branch.children[node_index] = Some(Child::Node(node));

                let new_leaf = Node::Leaf(LeafNode {
                    value,
                    partial_path: key_partial_path,
                });
                branch.children[key_index] = Some(Child::Node(new_leaf));

                firewood_counter!(INSERT, "operation" => "split").increment(1);
                Ok(Node::Branch(Box::new(branch)))
            }
        }
    }

    /// Ensures a branch exists at `key` in the subtrie rooted at `node`.
    /// Each element of `key` is 1 nibble.
    fn insert_branch_helper(&mut self, mut node: Node, key: &[u8]) -> Result<Node, FileIoError> {
        let path_overlap = PrefixOverlap::from(key, node.partial_path().as_ref());

        let unique_key = path_overlap.unique_a;
        let unique_node = path_overlap.unique_b;

        match (
            unique_key
                .split_first()
                .map(|(index, path)| (*index, path.into())),
            unique_node
                .split_first()
                .map(|(index, path)| (*index, path.into())),
        ) {
            (None, None) => match node {
                Node::Branch(_) => Ok(node),
                Node::Leaf(leaf) => {
                    let branch = BranchNode {
                        partial_path: leaf.partial_path,
                        value: Some(leaf.value),
                        children: Children::new(),
                    };
                    Ok(Node::Branch(Box::new(branch)))
                }
            },
            (None, Some((child_index, partial_path))) => {
                let child_index = PathComponent::try_new(child_index).expect("valid component");

                let mut branch = BranchNode {
                    partial_path: path_overlap.shared.into(),
                    value: None,
                    children: Children::new(),
                };

                node.update_partial_path(partial_path);
                branch.children[child_index] = Some(Child::Node(node));

                Ok(Node::Branch(Box::new(branch)))
            }
            (Some((child_index, partial_path)), None) => {
                let child_index = PathComponent::try_new(child_index).expect("valid component");

                match node {
                    Node::Branch(ref mut branch) => {
                        let Some(child) = branch.children.take(child_index) else {
                            let new_branch = Node::Branch(Box::new(BranchNode {
                                partial_path,
                                value: None,
                                children: Children::new(),
                            }));
                            branch.children[child_index] = Some(Child::Node(new_branch));
                            return Ok(node);
                        };

                        let child = self.read_for_update(child)?;
                        let child = self.insert_branch_helper(child, partial_path.as_ref())?;
                        branch.children[child_index] = Some(Child::Node(child));
                        Ok(node)
                    }
                    Node::Leaf(leaf) => {
                        let mut branch = BranchNode {
                            partial_path: leaf.partial_path,
                            value: Some(leaf.value),
                            children: Children::new(),
                        };

                        let new_branch = Node::Branch(Box::new(BranchNode {
                            partial_path,
                            value: None,
                            children: Children::new(),
                        }));
                        branch.children[child_index] = Some(Child::Node(new_branch));

                        Ok(Node::Branch(Box::new(branch)))
                    }
                }
            }
            (Some((key_index, key_partial_path)), Some((node_index, node_partial_path))) => {
                let key_index = PathComponent::try_new(key_index).expect("valid component");
                let node_index = PathComponent::try_new(node_index).expect("valid component");

                let mut branch = BranchNode {
                    partial_path: path_overlap.shared.into(),
                    value: None,
                    children: Children::new(),
                };

                node.update_partial_path(node_partial_path);
                branch.children[node_index] = Some(Child::Node(node));

                let new_branch = Node::Branch(Box::new(BranchNode {
                    partial_path: key_partial_path,
                    value: None,
                    children: Children::new(),
                }));
                branch.children[key_index] = Some(Child::Node(new_branch));

                Ok(Node::Branch(Box::new(branch)))
            }
        }
    }

    /// Navigate to the branch at `key` and return a mutable reference.
    /// `key` is provided as a slice of [`PathComponent`] values representing
    /// the path to traverse. Returns `None` if the path doesn't lead to an
    /// in-memory branch.
    ///
    /// This is the mutable counterpart to `get_node_from_nibbles`. It only
    /// works for in-memory nodes (`Child::Node`), which is guaranteed after
    /// `insert_branch_from_nibbles`.
    pub(crate) fn get_branch_from_nibbles_mut<'a>(
        root: &'a mut Option<Node>,
        key: &[PathComponent],
    ) -> Option<&'a mut BranchNode> {
        let node = root.as_mut()?;
        Self::get_branch_from_nibbles_mut_helper(node, key)
    }

    fn get_branch_from_nibbles_mut_helper<'a>(
        node: &'a mut Node,
        key: &[PathComponent],
    ) -> Option<&'a mut BranchNode> {
        let branch = node.as_branch_mut()?;
        let pp = &branch.partial_path;
        let shared_len = key
            .iter()
            .zip(pp.iter())
            .take_while(|(a, b)| a.as_u8() == **b)
            .count();
        if shared_len != pp.len() {
            return None;
        }
        let rest = key.get(shared_len..)?;
        let Some((&child_nibble, deeper)) = rest.split_first() else {
            return Some(branch);
        };
        match &mut branch.children[child_nibble] {
            Some(Child::Node(child_node)) => {
                Self::get_branch_from_nibbles_mut_helper(child_node, deeper)
            }
            _ => None,
        }
    }

    /// Removes the value associated with the given `key`.
    /// Returns the value that was removed, if any.
    /// Otherwise returns `None`.
    /// Each element of `key` is 2 nibbles.
    pub(crate) fn remove(&mut self, key: &[u8]) -> Result<Option<Value>, FileIoError> {
        self.remove_from_iter(NibblesIterator::new(key))
    }

    /// Removes the value associated with the given `key` where `key` is a `NibblesIterator`
    /// Returns the value that was removed, if any.
    /// Otherwise returns `None`.
    /// Each element of `key` is 2 nibbles.
    pub(crate) fn remove_from_iter(
        &mut self,
        key: NibblesIterator<'_>,
    ) -> Result<Option<Value>, FileIoError> {
        let key = Path::from_nibbles_iterator(key);
        let root = self.nodestore.root_mut();
        let Some(root_node) = std::mem::take(root) else {
            // The trie is empty. There is nothing to remove.
            firewood_counter!(REMOVE, "prefix" => "false", "result" => "nonexistent").increment(1);
            return Ok(None);
        };

        let (root_node, removed_value) = self.remove_helper(root_node, &key)?;
        *self.nodestore.root_mut() = root_node;
        if removed_value.is_some() {
            firewood_counter!(REMOVE, "prefix" => "false", "result" => "success").increment(1);
        } else {
            firewood_counter!(REMOVE, "prefix" => "false", "result" => "nonexistent").increment(1);
        }
        Ok(removed_value)
    }

    /// Removes the value associated with the given `key` from the subtrie rooted at `node`.
    /// Returns the new root of the subtrie and the value that was removed, if any.
    /// Each element of `key` is 1 nibble.
    fn remove_helper(
        &mut self,
        node: Node,
        key: &[u8],
    ) -> Result<(Option<Node>, Option<Value>), FileIoError> {
        // 4 possibilities for the position of the `key` relative to `node`:
        // 1. The node is at `key`
        // 2. The key is above the node (i.e. its ancestor)
        // 3. The key is below the node (i.e. its descendant)
        // 4. Neither is an ancestor of the other
        let path_overlap = PrefixOverlap::from(key, node.partial_path().as_ref());

        let unique_key = path_overlap.unique_a;
        let unique_node = path_overlap.unique_b;

        match (
            unique_key
                .split_first()
                .map(|(index, path)| (*index, Path::from(path))),
            unique_node.split_first(),
        ) {
            (_, Some(_)) => {
                // Case (2) or (4)
                Ok((Some(node), None))
            }
            (None, None) => {
                // 1. The node is at `key`
                match node {
                    Node::Branch(mut branch) => {
                        let Some(removed_value) = branch.value.take() else {
                            // The branch has no value. Return the node as is.
                            return Ok((Some(Node::Branch(branch)), None));
                        };

                        Ok((self.flatten_branch(branch)?, Some(removed_value)))
                    }
                    Node::Leaf(leaf) => Ok((None, Some(leaf.value))),
                }
            }
            (Some((child_index, child_partial_path)), None) => {
                let child_index = PathComponent::try_new(child_index).expect("valid component");
                // 3. The key is below the node (i.e. its descendant)
                match node {
                    // we found a non-matching leaf node, so the value does not exist
                    Node::Leaf(_) => Ok((Some(node), None)),
                    Node::Branch(mut branch) => {
                        let Some(child) = branch.children.take(child_index) else {
                            // child does not exist, so the value does not exist
                            return Ok((Some(Node::Branch(branch)), None));
                        };
                        let child = self.read_for_update(child)?;

                        let (child, removed_value) =
                            self.remove_helper(child, child_partial_path.as_ref())?;

                        branch.children[child_index] = child.map(Child::Node);

                        Ok((self.flatten_branch(branch)?, removed_value))
                    }
                }
            }
        }
    }

    /// Removes any key-value pairs with keys that have the given `prefix`.
    /// Returns the number of key-value pairs removed.
    pub(crate) fn remove_prefix(&mut self, prefix: &[u8]) -> Result<usize, FileIoError> {
        self.remove_prefix_from_iter(NibblesIterator::new(prefix))
    }

    /// Removes any key-value pairs with keys that have the given `prefix` where `prefix` is a `NibblesIterator`
    /// Returns the number of key-value pairs removed.
    pub(crate) fn remove_prefix_from_iter(
        &mut self,
        prefix: NibblesIterator<'_>,
    ) -> Result<usize, FileIoError> {
        let prefix = Path::from_nibbles_iterator(prefix);
        let root = self.nodestore.root_mut();
        let Some(root_node) = std::mem::take(root) else {
            // The trie is empty. There is nothing to remove.
            firewood_counter!(REMOVE, "prefix" => "true", "result" => "nonexistent").increment(1);
            return Ok(0);
        };

        let mut deleted = 0;
        let root_node = self.remove_prefix_helper(root_node, &prefix, &mut deleted)?;
        firewood_counter!(REMOVE, "prefix" => "true", "result" => "success")
            .increment(deleted as u64);
        *self.nodestore.root_mut() = root_node;
        Ok(deleted)
    }

    fn remove_prefix_helper(
        &mut self,
        node: Node,
        key: &[u8],
        deleted: &mut usize,
    ) -> Result<Option<Node>, FileIoError> {
        // 4 possibilities for the position of the `key` relative to `node`:
        // 1. The node is at `key`, in which case we need to delete this node and all its children.
        // 2. The key is above the node (i.e. its ancestor), so the parent needs to be restructured (TODO(rkuris)).
        // 3. The key is below the node (i.e. its descendant), so continue traversing the trie.
        // 4. Neither is an ancestor of the other, in which case there's no work to do.
        let path_overlap = PrefixOverlap::from(key, node.partial_path().as_ref());

        let unique_key = path_overlap.unique_a;
        let unique_node = path_overlap.unique_b;

        match (
            unique_key
                .split_first()
                .map(|(index, path)| (*index, Path::from(path))),
            unique_node.split_first(),
        ) {
            (None, _) => {
                // 1. The node is at `key`, or we're just above it
                // so we can start deleting below here
                match node {
                    Node::Branch(branch) => {
                        if branch.value.is_some() {
                            // a KV pair was in the branch itself
                            *deleted = deleted.saturating_add(1);
                        }
                        self.delete_children(branch, deleted)?;
                    }
                    Node::Leaf(_) => {
                        // the prefix matched only a leaf, so we remove it and indicate only one item was removed
                        *deleted = deleted.saturating_add(1);
                    }
                }
                Ok(None)
            }
            (_, Some(_)) => {
                // Case (2) or (4)
                Ok(Some(node))
            }
            (Some((child_index, child_partial_path)), None) => {
                let child_index = PathComponent::try_new(child_index).expect("valid component");
                // 3. The key is below the node (i.e. its descendant)
                match node {
                    Node::Leaf(_) => Ok(Some(node)),
                    Node::Branch(mut branch) => {
                        let Some(child) = branch.children.take(child_index) else {
                            return Ok(Some(Node::Branch(branch)));
                        };
                        let child = self.read_for_update(child)?;

                        let child =
                            self.remove_prefix_helper(child, child_partial_path.as_ref(), deleted)?;

                        branch.children[child_index] = child.map(Child::Node);

                        self.flatten_branch(branch)
                    }
                }
            }
        }
    }

    /// Recursively deletes all children of a branch node.
    fn delete_children(
        &mut self,
        mut branch: Box<BranchNode>,
        deleted: &mut usize,
    ) -> Result<(), FileIoError> {
        if branch.value.is_some() {
            // a KV pair was in the branch itself
            *deleted = deleted.saturating_add(1);
        }
        for (_, child) in &mut branch.children {
            let Some(child) = child.take() else {
                continue;
            };
            let child = self.read_for_update(child)?;
            match child {
                Node::Branch(child_branch) => {
                    self.delete_children(child_branch, deleted)?;
                }
                Node::Leaf(_) => {
                    *deleted = deleted.saturating_add(1);
                }
            }
        }
        Ok(())
    }

    /// Flattens a branch node into a new node.
    ///
    /// - If the branch has no value and no children, returns `None`.
    /// - If the branch has a value and no children, it becomes a leaf node.
    /// - If the branch has no value and exactly one child, it is replaced by that child
    ///   with an updated partial path.
    /// - If the branch has a value and any children, it is returned as-is.
    /// - If the branch has no value and multiple children, it is returned as-is.
    fn flatten_branch(
        &mut self,
        mut branch_node: Box<BranchNode>,
    ) -> Result<Option<Node>, FileIoError> {
        let mut children_iter = branch_node.children.each_mut().into_iter();

        let (child_index, child) = loop {
            let Some((child_index, child_slot)) = children_iter.next() else {
                // The branch has no children. Turn it into a leaf.
                return match branch_node.value {
                    Some(value) => Ok(Some(Node::Leaf(LeafNode {
                        value,
                        partial_path: branch_node.partial_path,
                    }))),
                    None => Ok(None),
                };
            };

            let Some(child) = child_slot.take() else {
                continue;
            };

            if branch_node.value.is_some() || children_iter.any(|(_, slot)| slot.is_some()) {
                // put the child back in its slot since we removed it
                child_slot.replace(child);

                // explicitly drop the iterator to release the mutable borrow on branch_node
                drop(children_iter);

                // The branch has a value or more than 1 child, so it can't be flattened.
                return Ok(Some(Node::Branch(branch_node)));
            }

            // we have found the only child
            break (child_index, child);
        };

        // The branch has only 1 child. Remove the branch and return the child.
        let mut child = self.read_for_update(child)?;

        // The child's partial path is the concatenation of its (now removed) parent,
        // its (former) child index, and its partial path.
        let child_partial_path = Path::from_nibbles_iterator(
            branch_node
                .partial_path
                .iter()
                .chain(once(&child_index.as_u8()))
                .chain(child.partial_path().iter())
                .copied(),
        );
        child.update_partial_path(child_partial_path);

        Ok(Some(child))
    }
}

impl<S: ReadableStorage> Merkle<NodeStore<Mutable<Propose>, S>> {
    /// Returns the node mapped to by `key_nibbles` where each key element is a
    /// single nibble.
    #[cfg(test)]
    pub(crate) fn get_node_from_nibbles(
        &self,
        key_nibbles: &[u8],
    ) -> Result<Option<SharedNode>, FileIoError> {
        let Some(root) = self.root() else {
            return Ok(None);
        };

        get_helper(&self.nodestore, &root, key_nibbles)
    }

    /// Ensures a branch exists at `key_nibbles` where each key element is a
    /// single nibble.
    ///
    /// This creates missing branch structure without inserting a value at the
    /// target key. Existing values and descendants are preserved.
    pub(crate) fn insert_branch_from_nibbles(
        &mut self,
        key_nibbles: &[u8],
    ) -> Result<(), FileIoError> {
        let root = self.nodestore.root_mut();
        let Some(root_node) = std::mem::take(root) else {
            let branch = BranchNode {
                partial_path: key_nibbles.into(),
                value: None,
                children: Children::new(),
            };
            *root = Node::Branch(Box::new(branch)).into();
            return Ok(());
        };

        let root_node = self.insert_branch_helper(root_node, key_nibbles)?;
        *self.nodestore.root_mut() = root_node.into();
        Ok(())
    }
}

/// The [`PrefixOverlap`] type represents the _shared_ and _unique_ parts of two potentially overlapping slices.
/// As the type-name implies, the `shared` property only constitues a shared *prefix*.
/// The `unique_*` properties, [`unique_a`][`PrefixOverlap::unique_a`] and [`unique_b`][`PrefixOverlap::unique_b`]
/// are set based on the argument order passed into the [`from`][`PrefixOverlap::from`] constructor.
#[derive(Debug)]
struct PrefixOverlap<'a, T> {
    shared: &'a [T],
    unique_a: &'a [T],
    unique_b: &'a [T],
}

impl<'a, T: PartialEq> PrefixOverlap<'a, T> {
    fn from(a: &'a [T], b: &'a [T]) -> Self {
        let split_index = a
            .iter()
            .zip(b)
            .position(|(a, b)| *a != *b)
            .unwrap_or_else(|| std::cmp::min(a.len(), b.len()));

        #[expect(
            clippy::disallowed_methods,
            reason = "split_index is min(a.len(), b.len()) or earlier, always <= a.len()"
        )]
        let (shared, unique_a) = a.split_at(split_index);
        let unique_b = b.get(split_index..).expect("");

        Self {
            shared,
            unique_a,
            unique_b,
        }
    }
}
