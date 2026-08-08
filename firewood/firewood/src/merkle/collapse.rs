// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::cmp::Ordering;

use firewood_storage::{
    Child, Mutable, Node, NodeStore, Path, PathComponent, Propose, ReadableStorage,
};

use crate::{ProofError, api, merkle::Merkle};

/// Returns `true` when a nibble at position `child_nib` under the accumulated
/// prefix `acc_prefix` could contain keys within `[start_nib, end_nib]`.
///
/// This is a fast, nibble-level check used by [`Merkle::child_in_range`] as
/// a first pass. It may return `true` for straddling nibbles — positions
/// where the boundary passes through the subtree — even if no actual keys
/// in the subtree are in range. `child_in_range` recurses into the subtree
/// to resolve those cases.
fn nibble_in_range(
    acc_prefix: &[u8],
    nibble: PathComponent,
    start_nib: &[u8],
    end_nib: &[u8],
) -> bool {
    let depth = acc_prefix.len();
    let child_nib = nibble.0.as_u8();

    // Split each boundary at `depth`. The left half is compared against
    // the same-length prefix of acc_prefix; the right half's first element
    // (if any) is compared against child_nib to break ties.
    let split = depth.min(start_nib.len());
    #[expect(
        clippy::disallowed_methods,
        reason = "split is min(depth, len), always in bounds"
    )]
    let (start_pre, start_rest) = start_nib.split_at(split);
    #[expect(
        clippy::disallowed_methods,
        reason = "split is min(depth, len), always in bounds"
    )]
    let (acc_start, _) = acc_prefix.split_at(split);
    let above_start = match acc_start.cmp(start_pre) {
        Ordering::Greater => true,
        Ordering::Less => false,
        Ordering::Equal => match start_rest.first() {
            None => true,
            Some(&boundary) => child_nib >= boundary,
        },
    };

    let split = depth.min(end_nib.len());
    #[expect(
        clippy::disallowed_methods,
        reason = "split is min(depth, len), always in bounds"
    )]
    let (end_pre, end_rest) = end_nib.split_at(split);
    #[expect(
        clippy::disallowed_methods,
        reason = "split is min(depth, len), always in bounds"
    )]
    let (acc_end, _) = acc_prefix.split_at(split);
    let below_end = match acc_end.cmp(end_pre) {
        Ordering::Less => true,
        Ordering::Greater => false,
        Ordering::Equal => match end_rest.first() {
            None => false,
            Some(&boundary) => child_nib <= boundary,
        },
    };

    above_start && below_end
}

/// Builds the accumulated nibble prefix for a child node by extending
/// `prefix` with the descent nibble and the child's `partial_path`.
fn build_child_prefix(prefix: &[u8], nibble: u8, node: &Node) -> Box<[u8]> {
    let pp = &node.partial_path().0;
    // slice lengths are bounded by isize::MAX. Sum cannot overflow.
    #[allow(clippy::arithmetic_side_effects)]
    let capacity = prefix.len() + 1 + pp.len();
    let mut v = Vec::with_capacity(capacity);
    v.extend_from_slice(prefix);
    v.push(nibble);
    v.extend_from_slice(pp);
    v.into_boxed_slice()
}

/// Verifies that `remaining` starts with the node's partial path and returns
/// the unconsumed suffix. Errors if `remaining` is too short or the nibbles
/// don't match.
fn consume_partial_path<'a>(
    remaining: &'a [PathComponent],
    node: &Node,
) -> Result<&'a [PathComponent], api::Error> {
    let pp = node.partial_path();
    let (consumed, rest) = remaining
        .split_at_checked(pp.len())
        .ok_or(api::Error::ProofError(ProofError::ProofNodeUnreachable))?;
    if !consumed.iter().zip(pp.iter()).all(|(a, b)| a.as_u8() == *b) {
        return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
    }
    Ok(rest)
}

impl<S: ReadableStorage> Merkle<NodeStore<Mutable<Propose>, S>> {
    /// Collapse intermediate branches between two consecutive proof-path
    /// positions.
    ///
    /// The proof implies a direct path from the parent to the child with
    /// no intermediate branch nodes. If the fork's trie has extra branches
    /// along this path (from out-of-range keys), this method removes their
    /// off-path children on the outside of the range and flattens
    /// single-child branches.
    ///
    /// Between consecutive proof nodes in `end_root`, the path is direct:
    /// intermediate nodes have only the on-path child. Removes all
    /// non-on-path children and values from intermediate branches so the
    /// proving trie matches `end_root`'s path-compressed structure.
    /// Collapse the proving trie's root so its structure matches the end
    /// trie's root path. When out-of-range deletions cause the end trie's
    /// root to compress (e.g., root `partial_path` changes from `[]` to `[1]`),
    /// the proposal root still has the old shape. This strips non-on-path
    /// children from the root and flattens single-child branches so the
    /// root's `partial_path` matches the first proof node's key.
    pub(crate) fn collapse_root_to_path(
        &mut self,
        target: &[PathComponent],
        range: Option<(&[u8], &[u8])>,
    ) -> Result<(), api::Error> {
        // The root's partial_path consumes some prefix of target.
        // Only collapse if target extends beyond the root's partial_path.
        let mut root_node = self
            .nodestore
            .root_mut()
            .take()
            .expect("reconciliation guarantees a root node exists");

        let pp_len = root_node.partial_path().len();
        if let Some((prefix, remaining)) = target.split_at_checked(pp_len)
            && !remaining.is_empty()
            && prefix
                .iter()
                .zip(root_node.partial_path().0.iter())
                .all(|(t, p)| t.as_u8() == *p)
        {
            // On error the root is left empty, but the caller discards
            // the proving trie on any verification failure.
            let root_prefix: Box<[u8]> = root_node.partial_path().0.iter().copied().collect();
            root_node = self.collapse_strip(root_node, remaining, &root_prefix, range)?;
        }

        *self.nodestore.root_mut() = Some(root_node);
        Ok(())
    }

    /// `range`: `(start_nibbles, end_nibbles)` for the proven range.
    /// In-range children that are also proposal-local trigger rejection.
    pub(crate) fn collapse_branch_to_path(
        &mut self,
        from: &[PathComponent],
        to: &[PathComponent],
        range: Option<(&[u8], &[u8])>,
    ) -> Result<(), api::Error> {
        // `to` must start with `from` since consecutive proof nodes form a
        // parent-child path — the child's key is always a prefix extension
        // of the parent's key.
        let suffix = to
            .strip_prefix(from)
            .ok_or(api::Error::ProofError(ProofError::ProofNodeUnreachable))?;
        let parent_prefix: Box<[u8]> = from.iter().map(|c| c.as_u8()).collect();

        let Some(root_node) = self.nodestore.root_mut().take() else {
            return Ok(());
        };
        let root_node = self.collapse_navigate(root_node, from, suffix, &parent_prefix, range)?;
        *self.nodestore.root_mut() = Some(root_node);
        Ok(())
    }

    /// Finds the parent proof node in the proving trie so that the
    /// intermediate structure between it and the next proof node can be
    /// collapsed. Navigate from `node` down through `key` to reach the
    /// parent proof node, then call `collapse_descend` with `suffix`.
    ///
    /// Must be called after `reconcile_branch_proof_node` to ensure that
    /// each proof node exists as a branch in the proving trie.
    ///                                                                                                                                                                                                                                           
    /// # Parameters                                                                                                                                                                                                                              
    /// - `node`: Current node in the proving trie being traversed.                                                                                                                                                                               
    /// - `key`: Remaining path to the parent proof node, shortened at each
    ///   recursive level as partial paths and child nibbles are consumed.                                                                                                                                                                        
    /// - `suffix`: Path from the parent proof node to the child proof node,
    ///   passed through unchanged to `collapse_descend`.                                                                                                                                                                                         
    /// - `parent_prefix`: Nibble path from root to the parent proof node,
    ///   used by `collapse_strip` for in-range child detection.                                                                                                                                                                                  
    /// - `range`: Optional proven range boundaries for detecting tampered
    ///   `batch_ops`.
    fn collapse_navigate(
        &mut self,
        mut node: Node,
        key: &[PathComponent],
        suffix: &[PathComponent],
        parent_prefix: &[u8],
        range: Option<(&[u8], &[u8])>,
    ) -> Result<Node, api::Error> {
        // get a reference to the partial path for ease of reading
        let pp = &node.partial_path().0;

        // find the point where the key differs from the partial path
        // unwrap_or handles the case where they are subsets of each other
        let diverge = key
            .iter()
            .zip(pp.iter())
            .position(|(pc, nibble)| pc.as_u8() != *nibble)
            .unwrap_or(key.len().min(pp.len()));

        if diverge != pp.len() {
            // partial_path not fully consumed — key diverges or is a prefix
            // of partial_path. Either way, from-node isn't reachable here.
            return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
        }

        #[expect(
            clippy::disallowed_methods,
            reason = "diverge == pp.len() <= key.len() by the check above"
        )]
        let (_, key_rest) = key.split_at(diverge);

        let Some((&child_component, deeper)) = key_rest.split_first() else {
            // Exact match — arrived at the parent proof node.
            return self.collapse_descend(node, suffix, parent_prefix, range);
        };

        // key extends past partial_path — descend into child.
        let Some(branch) = node.as_branch_mut() else {
            return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
        };

        // Temporarily take ownership of the child so we can resolve it to a
        // mutable Node, recurse, and put the (possibly modified) node back.
        let Some(child) = branch.children.take(child_component) else {
            return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
        };

        let child_node = self.read_for_update(child)?;
        let child_node =
            self.collapse_navigate(child_node, deeper, suffix, parent_prefix, range)?;
        branch.children[child_component] = Some(Child::Node(child_node));
        Ok(node)
    }

    /// Descend from the parent proof node into its on-path child, then
    /// strip non-on-path children from intermediate branches along `path`.
    ///
    /// The first component of `path` selects the child to descend into —
    /// we don't modify the parent proof node itself.
    fn collapse_descend(
        &mut self,
        mut node: Node,
        path: &[PathComponent],
        acc_prefix: &[u8],
        range: Option<(&[u8], &[u8])>,
    ) -> Result<Node, api::Error> {
        let Some((&first, remaining)) = path.split_first() else {
            return Ok(node);
        };

        let Some(branch) = node.as_branch_mut() else {
            // Cannot descend from parent proof node to child proof node if
            // the parent proof node is not a branch.
            return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
        };

        let Some(child) = branch.children.take(first) else {
            // No child pointer exists on path to child proof node.
            return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
        };

        let mut child_node = self.read_for_update(child)?;

        let child_prefix = build_child_prefix(acc_prefix, first.0.as_u8(), &child_node);

        let after_child = consume_partial_path(remaining, &child_node)?;

        child_node = self.collapse_strip(child_node, after_child, &child_prefix, range)?;

        branch.children[first] = Some(Child::Node(child_node));
        Ok(node)
    }

    /// Returns `true` when the subtree rooted at `child` contains any key
    /// within `[start_nib, end_nib]`.
    ///
    /// Reads the child node, computes its full nibble prefix, and checks
    /// whether the key is in range. For leaves this is a direct comparison.
    /// For branches, recurses into children whose nibble passes
    /// [`nibble_in_range`] to resolve straddling positions.
    fn child_in_range(
        &self,
        child: &Child,
        acc_prefix: &[u8],
        nibble: PathComponent,
        start_nib: &[u8],
        end_nib: &[u8],
    ) -> Result<bool, api::Error> {
        if !nibble_in_range(acc_prefix, nibble, start_nib, end_nib) {
            return Ok(false);
        }

        let child_node = child.as_shared_node(&self.nodestore)?;
        let pfx = build_child_prefix(acc_prefix, nibble.0.as_u8(), &child_node);
        let key_in_range = pfx.as_ref() >= start_nib && pfx.as_ref() <= end_nib;

        let Some(branch) = child_node.as_branch() else {
            return Ok(key_in_range);
        };

        if key_in_range && branch.value.is_some() {
            return Ok(true);
        }

        for (child_nibble, child_slot) in &branch.children {
            let Some(inner_child) = child_slot else {
                continue;
            };
            if self.child_in_range(inner_child, &pfx, child_nibble, start_nib, end_nib)? {
                return Ok(true);
            }
        }
        Ok(false)
    }

    /// Strip non-on-path children from an intermediate branch and recurse.
    /// Flattens single-child branches to match `end_root`'s path-compressed
    /// structure.
    ///
    /// Non-on-path children containing in-range keys trigger
    /// `EndRootMismatch` — they indicate tampered `batch_ops` since
    /// `end_root` has no branch at this position. Non-on-path children
    /// containing only out-of-range keys are stripped normally (accounted
    /// for by proof hashes).
    fn collapse_strip(
        &mut self,
        mut node: Node,
        path: &[PathComponent],
        acc_prefix: &[u8],
        range: Option<(&[u8], &[u8])>,
    ) -> Result<Node, api::Error> {
        let Some((&on_path, remaining)) = path.split_first() else {
            return Ok(node);
        };

        let Some(branch) = node.as_branch_mut() else {
            // Path is non-empty but node is not a branch — cannot descend further.
            return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
        };

        for (nibble, slot) in &mut branch.children {
            if nibble == on_path {
                continue;
            }

            if let Some((start_nib, end_nib)) = range
                && let Some(child) = slot.as_ref()
                && self.child_in_range(child, acc_prefix, nibble, start_nib, end_nib)?
            {
                return Err(api::Error::ProofError(ProofError::EndRootMismatch));
            }

            *slot = None;
        }
        branch.value = None;

        // Recurse into the on-path child.
        let Some(child) = branch.children.take(on_path) else {
            // No child pointer exists on path to child proof node.
            return Err(api::Error::ProofError(ProofError::ProofNodeUnreachable));
        };

        let mut child_node = self.read_for_update(child)?;
        let deeper = consume_partial_path(remaining, &child_node)?;
        if !deeper.is_empty() {
            let child_prefix = build_child_prefix(acc_prefix, on_path.0.as_u8(), &child_node);
            child_node = self.collapse_strip(child_node, deeper, &child_prefix, range)?;
        }

        branch.children[on_path] = Some(Child::Node(child_node));

        // If exactly one child remains, flatten by merging partial paths.
        let Some((child_idx, only_child)) = branch.children.take_only_child() else {
            return Ok(node);
        };
        let mut merged = self.read_for_update(only_child)?;
        let merged_path = Path::from_nibbles_iterator(
            branch
                .partial_path
                .iter()
                .chain(std::iter::once(&child_idx.as_u8()))
                .chain(merged.partial_path().iter())
                .copied(),
        );
        merged.update_partial_path(merged_path);
        Ok(merged)
    }
}

#[cfg(test)]
#[allow(clippy::unwrap_used)]
mod tests {
    use firewood_storage::{DeletedNodeTracking, MemStore};

    use super::*;

    fn create_test_merkle() -> Merkle<NodeStore<Mutable<Propose>, MemStore>> {
        let memstore = MemStore::default();
        let nodestore =
            NodeStore::new_empty_proposal(memstore.into(), DeletedNodeTracking::Enabled);
        Merkle { nodestore }
    }

    type TestMerkle = Merkle<NodeStore<Mutable<Propose>, MemStore>>;

    fn branch_child(m: &mut TestMerkle, keys: &[&[u8]], nibble: u8) -> Child {
        for key in keys {
            m.insert(key, Box::from(b"v".as_slice())).unwrap();
        }
        let mut root = m.nodestore.root_mut().take().unwrap();
        root.as_branch_mut()
            .unwrap()
            .children
            .take(PathComponent::try_new(nibble).unwrap())
            .unwrap()
    }

    fn check_child_in_range(
        setup: impl FnOnce(&mut TestMerkle) -> Child,
        acc_prefix: &[u8],
        nibble: u8,
        start_nib: &[u8],
        end_nib: &[u8],
        expected: bool,
    ) {
        let mut merkle = create_test_merkle();
        let child = setup(&mut merkle);
        let pc = PathComponent::try_new(nibble).unwrap();
        let result = merkle
            .child_in_range(&child, acc_prefix, pc, start_nib, end_nib)
            .unwrap();
        assert_eq!(
            result, expected,
            "acc_prefix={acc_prefix:x?} nibble={nibble:#x} range=[{start_nib:x?}, {end_nib:x?}]"
        );
    }

    #[test]
    fn test_child_in_range() {
        // nibble clearly out of range — returns false without reading
        check_child_in_range(
            |m| branch_child(m, &[b"\x50", b"\x60"], 0x5),
            &[],
            0x5,
            &[0x8],
            &[0xf],
            false,
        );

        // leaf: nibble straddles start, full key [0,0] < start [0,1]
        check_child_in_range(
            |m| branch_child(m, &[b"\x00", b"\x10"], 0x0),
            &[],
            0x0,
            &[0x0, 0x1],
            &[0x1, 0x0],
            false,
        );

        // leaf: nibble straddles start, full key [0,5] >= start [0,1]
        check_child_in_range(
            |m| branch_child(m, &[b"\x05", b"\x10"], 0x0),
            &[],
            0x0,
            &[0x0, 0x1],
            &[0x1, 0x0],
            true,
        );

        // leaf: nibble straddles end, full key [3,f] > end [3,0]
        check_child_in_range(
            |m| branch_child(m, &[b"\x3f", b"\x10"], 0x3),
            &[],
            0x3,
            &[0x1, 0x0],
            &[0x3, 0x0],
            false,
        );

        // branch: all children out of range ([0,0,0,0] and [0,0,5,0] both < [0,1])
        check_child_in_range(
            |m| branch_child(m, &[b"\x00\x00", b"\x00\x50", b"\x10"], 0x0),
            &[],
            0x0,
            &[0x0, 0x1],
            &[0x0, 0xf],
            false,
        );

        // branch: one child in range ([0,1,0,0] >= [0,1])
        check_child_in_range(
            |m| branch_child(m, &[b"\x00\x00", b"\x01\x00", b"\x10"], 0x0),
            &[],
            0x0,
            &[0x0, 0x1],
            &[0x0, 0xf],
            true,
        );

        // branch with value: key [0,0] in range [0,0]..[0,f], has value
        check_child_in_range(
            |m| branch_child(m, &[b"\x00", b"\x00\x50", b"\x10"], 0x0),
            &[],
            0x0,
            &[0x0, 0x0],
            &[0x0, 0xf],
            true,
        );
    }

    #[test]
    fn test_nibble_in_range() {
        let pc = |n| PathComponent::try_new(n).unwrap();

        // inside range
        assert!(nibble_in_range(&[0xa], pc(0x5), &[0xa, 0x0], &[0xa, 0xf]));
        // before start
        assert!(!nibble_in_range(&[0xa], pc(0x2), &[0xa, 0x5], &[0xa, 0xf]));
        // after end
        assert!(!nibble_in_range(&[0xa], pc(0xf), &[0xa, 0x0], &[0xa, 0x5]));
        // at start boundary
        assert!(nibble_in_range(&[0xa], pc(0x5), &[0xa, 0x5], &[0xa, 0xf]));
        // at end boundary
        assert!(nibble_in_range(&[0xa], pc(0x5), &[0xa, 0x0], &[0xa, 0x5]));
        // acc_prefix past start — in range regardless of nibble
        assert!(nibble_in_range(&[0xb], pc(0x0), &[0xa, 0x5], &[0xf, 0x0]));
        // acc_prefix before end — in range regardless of nibble
        assert!(nibble_in_range(&[0xa], pc(0xf), &[0x0, 0x0], &[0xb, 0x0]));
        // empty prefix
        assert!(nibble_in_range(&[], pc(0x5), &[0x0], &[0xf]));
        assert!(!nibble_in_range(&[], pc(0x5), &[0x6], &[0xf]));
        // start shorter than depth — child is past start
        assert!(nibble_in_range(
            &[0xa, 0xb],
            pc(0x0),
            &[0xa],
            &[0xa, 0xb, 0xf]
        ));
        // end shorter than depth — child is past end
        assert!(!nibble_in_range(
            &[0xa, 0xb],
            pc(0x0),
            &[0xa, 0xb, 0x0],
            &[0xa]
        ));
    }

    #[test]
    fn test_collapse_navigate_exact_match() {
        let pc = |n: u8| PathComponent::try_new(n).unwrap();
        let mut merkle = create_test_merkle();

        // Under \x10, branch with children at 2 and 3.
        // Under \x10\x2, branch with children at 1 and 2.
        // \x20 forces the root to have an empty partial path.
        merkle
            .insert(b"\x10\x21", Box::from(b"a".as_slice()))
            .unwrap();
        merkle
            .insert(b"\x10\x22", Box::from(b"b".as_slice()))
            .unwrap();
        merkle
            .insert(b"\x10\x30", Box::from(b"c".as_slice()))
            .unwrap();
        merkle.insert(b"\x20", Box::from(b"d".as_slice())).unwrap();
        let node = merkle.nodestore.root_mut().take().unwrap();

        // Navigates down nibble 1 because the key parameter is [pc(1), pc(0)].
        // From the suffix, the child proof is at \x10\x21. `parent_prefix` is
        // the nibble path from the root to the parent proof node which will be
        // passed to `collapse_strip`. In this case, `collapse_strip` will strip
        // \x10\x22.
        let root_node = merkle
            .collapse_navigate(node, &[pc(1), pc(0)], &[pc(2), pc(1)], &[0x1, 0x0], None)
            .unwrap();

        let branch = root_node.as_branch().unwrap();

        // Root children unchanged
        assert!(branch.children[pc(1)].is_some());
        assert!(branch.children[pc(2)].is_some());

        // Put the root back into the merkle and check for the stripped node.
        *merkle.nodestore.root_mut() = Some(root_node);
        // \x10\x21 is on-path — should still exist
        assert!(merkle.get_value(b"\x10\x21").unwrap().is_some());
        // \x10\x22 is off-path — should be stripped
        assert!(merkle.get_value(b"\x10\x22").unwrap().is_none());
        // \x10\x30 is a sibling of the on-path child at the parent proof node — not stripped
        assert!(merkle.get_value(b"\x10\x30").unwrap().is_some());
    }

    #[test]
    fn test_collapse_navigate_recurse() {
        let pc = |n: u8| PathComponent::try_new(n).unwrap();
        let mut merkle = create_test_merkle();

        // Under \x1, branch with children at nibbles 0 and 1.
        // Under \x10\x2, branch with children at nibbles 1 and 3.
        // \x11 prevents path compression under nibble 1.
        // \x20 forces the root to have an empty partial path.
        merkle
            .insert(b"\x10\x21", Box::from(b"a".as_slice()))
            .unwrap();
        merkle
            .insert(b"\x10\x23", Box::from(b"b".as_slice()))
            .unwrap();
        merkle.insert(b"\x11", Box::from(b"c".as_slice())).unwrap();
        merkle.insert(b"\x20", Box::from(b"d".as_slice())).unwrap();
        let node = merkle.nodestore.root_mut().take().unwrap();

        // Navigates down nibble 1 because the key parameter is [pc(1)].
        // Unlike exact_match, the key is consumed by child descent rather
        // than partial path matching — the branch under nibble 1 has no
        // partial path. Recurse with empty key → exact match at that branch.
        // From the suffix, the child proof is at \x10\x21. `parent_prefix`
        // is the nibble path from root to the parent proof node, passed to
        // `collapse_strip`. In this case, `collapse_strip` will strip \x10\x23.
        let node = merkle
            .collapse_navigate(node, &[pc(1)], &[pc(0), pc(2), pc(1)], &[0x1], None)
            .unwrap();

        // Put the root back into the merkle and check for the stripped node.
        *merkle.nodestore.root_mut() = Some(node);
        // \x10\x21 is on-path — should still exist
        assert!(merkle.get_value(b"\x10\x21").unwrap().is_some());
        // \x10\x23 is off-path — should be stripped
        assert!(merkle.get_value(b"\x10\x23").unwrap().is_none());
    }

    #[test]
    fn test_collapse_navigate_errors() {
        let pc = |n: u8| PathComponent::try_new(n).unwrap();
        let mut merkle = create_test_merkle();

        // Single key \x10 → root has partial_path [1, 0]
        merkle.insert(b"\x10", Box::from(b"a".as_slice())).unwrap();
        let node = merkle.nodestore.root_mut().take().unwrap();

        // Invalid key [2, 0] diverges at first nibble.
        let result = merkle.collapse_navigate(node.clone(), &[pc(2), pc(0)], &[pc(0)], &[], None);
        assert!(matches!(
            result,
            Err(api::Error::ProofError(ProofError::ProofNodeUnreachable))
        ));

        // Passing invalid key [1, 0, 5]. Consumes partial_path [1, 0] then tries
        // to descend, but node is a leaf — should error.
        let result =
            merkle.collapse_navigate(node.clone(), &[pc(1), pc(0), pc(5)], &[pc(0)], &[], None);
        assert!(matches!(
            result,
            Err(api::Error::ProofError(ProofError::ProofNodeUnreachable))
        ));

        // Put root node back into the merkle and add \x20 to make root a branch
        // with children at 1 and 2 and no partial path.
        *merkle.nodestore.root_mut() = Some(node);
        merkle.insert(b"\x20", Box::from(b"b".as_slice())).unwrap();
        let node = merkle.nodestore.root_mut().take().unwrap();

        // Invalid key [3] points to nibble 3 which doesn't exist — should error.
        let result = merkle.collapse_navigate(node, &[pc(3)], &[pc(0)], &[], None);
        assert!(matches!(
            result,
            Err(api::Error::ProofError(ProofError::ProofNodeUnreachable))
        ));

        // Range covers \x10\x22 in this test. An in-range off-path child
        // should result in an `EndRootMismatch`.
        let mut merkle = create_test_merkle();
        merkle
            .insert(b"\x10\x21", Box::from(b"a".as_slice()))
            .unwrap();
        merkle
            .insert(b"\x10\x22", Box::from(b"b".as_slice()))
            .unwrap();
        merkle
            .insert(b"\x10\x30", Box::from(b"c".as_slice()))
            .unwrap();
        merkle.insert(b"\x20", Box::from(b"d".as_slice())).unwrap();
        let node = merkle.nodestore.root_mut().take().unwrap();

        // Key is [1, 0], and suffix is [2, 1], which means it will navigate to the
        // \x10, then call `collapse_descend` to traverse down nibble 2. There is
        // one off-path child entry to strip at nibble 2 (\x10\x22), but that child
        // is inside the range which will result in an `EndRootMismatch` error.
        let result = merkle.collapse_navigate(
            node,
            &[pc(1), pc(0)],
            &[pc(2), pc(1)],
            &[0x1, 0x0],
            Some((&[0x1, 0x0, 0x2, 0x0], &[0x1, 0x0, 0x2, 0xf])),
        );
        assert!(matches!(
            result,
            Err(api::Error::ProofError(ProofError::EndRootMismatch))
        ));
    }

    #[test]
    fn test_collapse_navigate_mismatched_partial_path() {
        let pc = |n: u8| PathComponent::try_new(n).unwrap();
        let mut merkle = create_test_merkle();

        // Under [1, 0, 2]: leaf with partial_path [1] for \x10\x21
        merkle
            .insert(b"\x10\x21", Box::from(b"a".as_slice()))
            .unwrap();
        merkle
            .insert(b"\x10\x30", Box::from(b"b".as_slice()))
            .unwrap();
        merkle.insert(b"\x20", Box::from(b"c".as_slice())).unwrap();
        let node = merkle.nodestore.root_mut().take().unwrap();

        // suffix [1, 0, 2, 9] — nibble 9 doesn't match child's partial path [1]
        let result = merkle.collapse_navigate(node, &[], &[pc(1), pc(0), pc(2), pc(9)], &[], None);
        assert!(matches!(
            result,
            Err(api::Error::ProofError(ProofError::ProofNodeUnreachable))
        ));
    }
}
