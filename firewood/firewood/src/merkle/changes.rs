// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt::Debug;
use std::{cmp::Ordering, iter::once};

use firewood_metrics::firewood_increment;
use firewood_storage::{
    Child, FileIoError, HashedNodeReader, Node, NodeReader, Path, SharedNode, TrieHash,
};
use lender::{Lender, Lending};

use crate::{
    Proof, ProofCollection,
    db::BatchOp,
    iter::key_from_nibble_iter,
    merkle::{Key, PrefixOverlap, Value},
};

/// A change proof can demonstrate that by applying the provided array of `BatchOp`s to a Merkle
/// trie with given start root hash, the resulting trie will have the given end root hash. It
/// consists of the following:
/// - A start proof: proves that the smallest key does/doesn't exist
/// - An end proof: proves the the largest key does/doesn't exist
/// - The actual `BatchOp`s that specify the difference between the start and end tries.
#[derive(Debug)]
pub struct ChangeProof<K: AsRef<[u8]> + Debug, V: AsRef<[u8]> + Debug, H> {
    start_proof: Proof<H>,
    end_proof: Proof<H>,
    batch_ops: Box<[BatchOp<K, V>]>,
}

impl<K, V, H> ChangeProof<K, V, H>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
    H: ProofCollection,
{
    /// Create a new change proof with the given start and end proofs
    /// and the `BatchOp`s that are included in the proof.
    #[must_use]
    pub const fn new(
        start_proof: Proof<H>,
        end_proof: Proof<H>,
        key_values: Box<[BatchOp<K, V>]>,
    ) -> Self {
        Self {
            start_proof,
            end_proof,
            batch_ops: key_values,
        }
    }

    /// Returns a reference to the start proof, which may be empty.
    #[must_use]
    pub const fn start_proof(&self) -> &Proof<H> {
        &self.start_proof
    }

    /// Returns a reference to the end proof, which may be empty.
    #[must_use]
    pub const fn end_proof(&self) -> &Proof<H> {
        &self.end_proof
    }

    /// Returns the `BatchOp`s included in the change proof, which may be empty.
    #[must_use]
    pub const fn batch_ops(&self) -> &[BatchOp<K, V>] {
        &self.batch_ops
    }

    /// Returns true if the change proof is empty, meaning it has no start or end proof
    /// and no `BatchOp`s.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.start_proof.is_empty() && self.end_proof.is_empty() && self.batch_ops.is_empty()
    }

    /// Returns an iterator over the `BatchOp`s in this change proof.
    ///
    /// The iterator yields references to the `BatchOp`s in the order they
    /// appear in the proof (which should be lexicographic order as they appear
    /// in the trie).
    #[must_use]
    pub fn iter(&self) -> ChangeProofIter<'_, K, V> {
        ChangeProofIter(self.batch_ops.iter())
    }
}

/// An iterator over the `BatchOp`s in a `ChangeProof`.
///
/// This iterator yields references to the `BatchOp`s contained within
/// the change proof in the order they appear (lexicographic order).
///
/// This type is not re-exported at the top level; it is only accessible through
/// the iterator trait implementations on `ChangeProof`.
#[derive(Debug)]
pub struct ChangeProofIter<'a, K: AsRef<[u8]> + Debug, V: AsRef<[u8]> + Debug>(
    std::slice::Iter<'a, BatchOp<K, V>>,
);

impl<'a, K, V> Iterator for ChangeProofIter<'a, K, V>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
{
    type Item = &'a BatchOp<K, V>;

    fn next(&mut self) -> Option<Self::Item> {
        self.0.next()
    }

    fn size_hint(&self) -> (usize, Option<usize>) {
        self.0.size_hint()
    }
}

impl<K, V> ExactSizeIterator for ChangeProofIter<'_, K, V>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
{
}

impl<K, V> std::iter::FusedIterator for ChangeProofIter<'_, K, V>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
{
}

impl<'a, K, V, H> IntoIterator for &'a ChangeProof<K, V, H>
where
    K: AsRef<[u8]> + Debug,
    V: AsRef<[u8]> + Debug,
    H: ProofCollection,
{
    type Item = &'a BatchOp<K, V>;
    type IntoIter = ChangeProofIter<'a, K, V>;

    fn into_iter(self) -> Self::IntoIter {
        self.iter()
    }
}

/// Enum containing all possible states that we can be in as we iterate through the diff
/// between two Merkle tries.
enum DiffIterationNodeState {
    /// In the `TraverseBoth` state, we only need to consider the next nodes from the left
    /// and right trie in pre-order traversal order.
    TraverseBoth,
    /// In the `TraverseLeft` state, we need to compare the next node from the left trie
    /// with the current node in the right trie (`right_state`).
    TraverseLeft { right_state: ComparableNodeInfo },
    /// In the `TraverseRight` state, we need to compare the next node from the right trie
    /// with the current node in the left trie (`left_state`).
    TraverseRight { left_state: ComparableNodeInfo },
    /// In the `AddRestRight` state, we have reached the end of the left trie and need to
    /// add the remaining keys/values from the right trie to the addition list.
    AddRestRight,
    /// In the `DeleteRestLeft` state, we have reached the end of the right trie and need
    /// add the remaining keys/values from the left trie to the deletion list.
    DeleteRestLeft,
    /// In the `SkipChildren` state, we previously identified that the current nodes from
    /// both tries have matching paths, values, and hashes. This means we don't need to
    /// traverse any of their children. In this state, we call `skip_children` on both tries
    /// to not push their children onto the traversal stack on the next call to `next`.
    /// Then we consider the next nodes from both tries in the same way as `TraverseBoth`.
    SkipChildren,
}

/// Contains all of a node's info that is needed for node comparison in `DiffMerkleNodeStream`.
/// It includes the nodes full path and its hash if available.
#[derive(Clone, Debug)]
struct ComparableNodeInfo {
    path: Path,
    node: SharedNode,
    hash: Option<TrieHash>,
}

impl ComparableNodeInfo {
    // Creates a `ComparableNodeInfo` from a `Child`, its pre-path, and the trie that the `Child`
    // is from for reading the node from storage.
    fn new<T: NodeReader>(pre_path: Path, child: &Child, trie: &T) -> Result<Self, FileIoError> {
        let node = child.as_shared_node(trie)?;
        // We need the full path as the diff algorithm compares the full paths of the current
        // nodes of the two tries.
        let mut full_path = pre_path;
        full_path.extend(node.partial_path().iter().copied());

        Ok(Self {
            path: full_path,
            node,
            hash: child.hash().map(|hash| hash.clone().into_triehash()),
        })
    }
}

/// Iterator that outputs the difference between two tries and skips matching sub-tries.
pub(crate) struct DiffMerkleNodeStream<'a, Left: HashedNodeReader, Right: HashedNodeReader> {
    // Contains the state of the diff traversal. It is only None after calling `next` or
    // `next_internal` if we have reached the end of the traversal.
    state: Option<DiffIterationNodeState>,
    left_tree: PreOrderIterator<'a, Left>,
    right_tree: PreOrderIterator<'a, Right>,
}

impl<'a, Left: HashedNodeReader, Right: HashedNodeReader> DiffMerkleNodeStream<'a, Left, Right> {
    /// Constructor where the left and right tries implement the trait `HashedNodeReader`.
    pub fn new(
        left_tree: &'a Left,
        right_tree: &'a Right,
        start_key: Key,
    ) -> Result<Self, FileIoError> {
        // Create pre-order iterators for the two tries and have them iterate to the start key.
        // If the start key doesn't exist for an iterator, it will set the iterator to the
        // smallest key that is larger than the start key.
        let left_tree = PreOrderIterator::new(left_tree, &start_key)?;
        let right_tree = PreOrderIterator::new(right_tree, &start_key)?;
        Ok(Self {
            state: Some(DiffIterationNodeState::TraverseBoth),
            left_tree,
            right_tree,
        })
    }

    /// Helper function used in `one_step_compare` to check if two `Option<TrieHash>` matches.
    /// Note that this function should only be used if the two nodes in the left and right
    /// tries have the same path and the same value.
    fn hash_match(
        left_hash: Option<&TrieHash>,
        right_hash: Option<&TrieHash>,
    ) -> DiffIterationNodeState {
        if match (left_hash, right_hash) {
            (Some(left_hash), Some(right_hash)) => left_hash == right_hash,
            _ => false,
        } {
            DiffIterationNodeState::SkipChildren
        } else {
            DiffIterationNodeState::TraverseBoth
        }
    }

    /// Called as part of a lock-step synchronized pre-order traversal of the left and right tries. This
    /// function compares the current nodes from the two tries to determine if any operations need to be
    /// deleted (i.e., op appears on the left but not the right trie) or added (i.e., op appears on the
    /// right but not the left trie). It also returns the next iteration state, which can include
    /// traversing down the left or right trie, traversing down both if the current nodes' path on both
    /// tries are the same but their node hashes differ, or skipping the children of the current nodes
    /// from both tries if their node hashes match.
    fn one_step_compare(
        left_state: &ComparableNodeInfo,
        right_state: &ComparableNodeInfo,
    ) -> (DiffIterationNodeState, Option<BatchOp<Key, Value>>) {
        // Compare the full path of the current nodes from the left and right tries.
        match left_state.path.cmp(&right_state.path) {
            // If the left full path is less than the right full path, that means that all of
            // the remaining nodes (and any keys stored in those nodes) from the right trie
            // are greater than the current node on the left trie. Therefore, we should traverse
            // down the left trie until we reach a node that is larger than or equal to the
            // current node on the right trie, and collect all of the keys associated with
            // the nodes that were traversed (excluding the last one) and add them to the set
            // of keys that need to be deleted in the change proof.
            Ordering::Less => {
                // If there is a value in the current node in the left trie, then that value
                // should be included in the set of deleted keys in the change proof. We do
                // this by returning it in the second entry of the tuple in the return value.
                (
                    DiffIterationNodeState::TraverseLeft {
                        right_state: right_state.clone(),
                    },
                    Self::deleted_values(left_state.node.value(), &left_state.path),
                )
            }
            // If the left full path is greater than the right full path, then all of the
            // remaining nodes (and any keys stored in those nodes) from the left trie are greater
            // than the current node on the left trie. Therefore, any remaining keys from the
            // right trie that is smaller than the current node in the left trie are missing from
            // the left trie and should be added as additional keys to the change proof. Therefore,
            // we should traverse the right trie until we reach a node that is smaller than or
            // equal to the current node on the left trie, and collect all of the keys associated
            // with those nodes (excluding the last one) and add them to the set of keys to be
            // added to the change proof.
            Ordering::Greater => {
                // If there is a value in the current node in the right trie, then that value
                // should be included in the set of additional keys in the change proof.
                (
                    DiffIterationNodeState::TraverseRight {
                        left_state: left_state.clone(),
                    },
                    Self::added_values(right_state.node.value(), &right_state.path),
                )
            }
            // If the left and right full paths are equal, then we need to also look at their values
            // (if any) to determine what to add to the change proof. If only the left node has a
            // value, then we know that this key cannot exist in the right trie and the value's key
            // must be added to the delete list. Conversely, if only the right node has a value, then
            // we know that this key does not exist in the left trie and this key/value must be added
            // to the addition list. In both cases, we can transition to TraverseBoth since we are
            // done with the current node in both tries and can continue comparing the tries from
            // their next largest key.
            //
            // For the same reason, we can transition to TraverseBoth if both current nodes don't have
            // values and their hashes don't match. If both current nodes have values but they are
            // not the same, then we put the value from the right node into the addition list as this
            // value has overwritten the old value from the left trie. We can also transition to
            // TraverseBoth as we are also done with both of the current nodes. If the values match,
            // we can transition to TraverseBoth if their hashes don't match since there is nothing
            // to add to the change proof and we are done with these nodes.
            //
            // For the cases where both nodes don't have value or both values are the same, and both
            // hashes match, then we know that everything below the current nodes are identical, and
            // we can transition to the SkipChildren state to not traverse any further down the two
            // tries from the current nodes.
            Ordering::Equal => match (left_state.node.value(), right_state.node.value()) {
                (None, None) => (
                    Self::hash_match(left_state.hash.as_ref(), right_state.hash.as_ref()),
                    None,
                ),
                (Some(val), None) => (
                    DiffIterationNodeState::TraverseBoth,
                    Self::deleted_values(Some(val), &left_state.path),
                ),
                (None, Some(val)) => (
                    DiffIterationNodeState::TraverseBoth,
                    Self::added_values(Some(val), &right_state.path),
                ),
                (Some(left_val), Some(right_val)) => {
                    if left_val == right_val {
                        (
                            Self::hash_match(left_state.hash.as_ref(), right_state.hash.as_ref()),
                            None,
                        )
                    } else {
                        (
                            DiffIterationNodeState::TraverseBoth,
                            Self::added_values(Some(right_val), &right_state.path),
                        )
                    }
                }
            },
        }
    }

    /// Helper function that returns a key/value to be added to the delete list if the current
    /// node from the left trie has a value.
    fn deleted_values(left_value: Option<&[u8]>, left_path: &Path) -> Option<BatchOp<Key, Value>> {
        left_value.map(|_val| BatchOp::Delete {
            key: key_from_nibble_iter(left_path.iter().copied()),
        })
    }

    /// Helper function that returns a key/value to be added to the addition list if the current
    /// node from the right trie has a value.
    fn added_values(right_value: Option<&[u8]>, right_path: &Path) -> Option<BatchOp<Key, Value>> {
        right_value.map(|val| BatchOp::Put {
            key: key_from_nibble_iter(right_path.iter().copied()),
            value: val.into(),
        })
    }

    /// Helper function called in the `TraverseBoth` or `SkipChildren` state. Mainly handles the complexities
    /// introduced when one of the tries has no more nodes. If both tries have nodes remaining, then it calls
    /// `one_step_compare` to complete the state handling.
    fn next_node_from_both(
        &mut self,
    ) -> Result<(DiffIterationNodeState, Option<BatchOp<Key, Value>>), FileIoError> {
        // Get the next node from the left trie.
        let Some(left_state) = self.left_tree.next_node_info()? else {
            // No more nodes in the left trie. For this state, the current node from the right trie has already
            // been accounted for, which means we don't need to include it in the change proof. Transition to
            // `AddRestRight` state where we add the remaining values from the right trie to the addition list.
            return Ok((DiffIterationNodeState::AddRestRight, None));
        };

        // Get the next node from the right trie.
        let Some(right_state) = self.right_tree.next_node_info()? else {
            // No more nodes on the right side. We want to transition to `DeleteRestLeft`, but we don't want to
            // forget about the node that we just retrieved from the left tree.
            return Ok((
                DiffIterationNodeState::DeleteRestLeft,
                Self::deleted_values(left_state.node.value(), &left_state.path),
            ));
        };

        Ok(Self::one_step_compare(left_state, right_state))
    }

    /// Only called by `next` to implement the Iterator trait. Separated out into a separate
    /// function mainly to simplify error handling.
    fn next_internal(&mut self) -> Result<Option<BatchOp<Key, Value>>, FileIoError> {
        // Loops until there is a value to return or if we have reached the end of the
        // traversal. State processing is based on the value of `state`, which we take at the
        // beginning of the loop and reassign before the next iteration. `state` can only be
        // None after calling `next_internal` if we have reached the end of the traversal.
        while let Some(state) = self.state.take() {
            let (next_state, op) = match state {
                DiffIterationNodeState::SkipChildren => {
                    // In the SkipChildren state, the hash and path of the current nodes on
                    // both the left and right tries match. This means we don't need to
                    // traverse down the children of these tries. We can do this by calling
                    // skip_children on the two tries.
                    self.left_tree.skip_children();
                    self.right_tree.skip_children();

                    // Calls helper function that uses the next node from both tries. This
                    // helper function is also called for `TraverseBoth`.`
                    self.next_node_from_both()?
                }
                DiffIterationNodeState::TraverseBoth => self.next_node_from_both()?,
                DiffIterationNodeState::TraverseLeft { right_state } => {
                    // In the `TraverseLeft` state, we use the next node from the left trie to
                    // perform state processing, which is done by calling `one_step_compare`.
                    if let Some(left_state) = self.left_tree.next_node_info()? {
                        Self::one_step_compare(left_state, &right_state)
                    } else {
                        // If we have no more nodes from the left trie, then we transition to
                        // the `AddRestRight` state where we add all of the remaining nodes
                        // from the right trie to the addition list. We also need to add the
                        // the value from the current right node if it has a value.
                        (
                            DiffIterationNodeState::AddRestRight,
                            Self::added_values(right_state.node.value(), &right_state.path),
                        )
                    }
                }
                DiffIterationNodeState::TraverseRight { left_state } => {
                    if let Some(right_state) = self.right_tree.next_node_info()? {
                        Self::one_step_compare(&left_state, right_state)
                    } else {
                        // For `TraverseRight`, if we have no more nodes on the right trie, then
                        // transition to the `DeleteRestLeft` state where we add all of the
                        // remaining nodes from the left trie to the delete list. We also need
                        // to add the value from the current left node if it has a value.
                        (
                            DiffIterationNodeState::DeleteRestLeft,
                            Self::deleted_values(left_state.node.value(), &left_state.path),
                        )
                    }
                }
                DiffIterationNodeState::AddRestRight => {
                    let Some(right_state) = self.right_tree.next_node_info()? else {
                        break; // No more nodes from both tries, which ends the iteration.
                    };
                    // Add the value of the right node to the addition list if it has one, and stay
                    // in this state.
                    (
                        DiffIterationNodeState::AddRestRight,
                        Self::added_values(right_state.node.value(), &right_state.path),
                    )
                }
                DiffIterationNodeState::DeleteRestLeft => {
                    let Some(left_state) = self.left_tree.next_node_info()? else {
                        break; // No more nodes from both tries, which ends the iteration.
                    };
                    // Add the value of the left node to the deletion list if it has one, and stay
                    // in this state.
                    (
                        DiffIterationNodeState::DeleteRestLeft,
                        Self::deleted_values(left_state.node.value(), &left_state.path),
                    )
                }
            };
            // Perform the state transition. Return a value if the previous iteration produced one.
            // Otherwise, loop again to perform the next iteration.
            self.state = Some(next_state);
            if op.is_some() {
                return Ok(op);
            }
        }
        Ok(None)
    }
}

/// Adding support for the Iterator trait
impl<Left: HashedNodeReader, Right: HashedNodeReader> Iterator
    for DiffMerkleNodeStream<'_, Left, Right>
{
    type Item = Result<BatchOp<Key, Value>, FileIoError>;

    fn next(&mut self) -> Option<Self::Item> {
        self.next_internal().transpose()
    }
}

/// Contains the state required for performing pre-order iteration of a Merkle trie. This includes
/// the `ComparableNodeInfo` of the current node, a reference to a trie that implements the
/// `HashedNodeReader` trait, and a stack that contains the `ComparableNodeInfo` of nodes to be
/// traversed with calls to `next` or `next_node_info`. The `node_info` is None at the beginning of
/// the traversal before any node has been processed, or when a `skip_children` has been called.
/// By setting `node_info` to None in the latter case, the children of the current node will be
/// skipped from traversal as they will not be be pushed to the traversal stack in the subsequent
/// `next` or `next_node_info` call.
struct PreOrderIterator<'a, T: HashedNodeReader> {
    node_info: Option<ComparableNodeInfo>,
    trie: &'a T,
    traversal_stack: Vec<ComparableNodeInfo>,
}

/// Implementation for the Lender trait, a lending iterator. A lending iterator rather than a
/// normal iterator is necessary since we want to return a reference to the current
/// `ComparableNodeInfo` that is inside `PreOrderIterator`.
impl<'lend, T: HashedNodeReader> Lending<'lend> for PreOrderIterator<'_, T> {
    type Lend = Result<&'lend ComparableNodeInfo, FileIoError>;
}

impl<T: HashedNodeReader> Lender for PreOrderIterator<'_, T> {
    lender::check_covariance!();

    fn next(&mut self) -> Option<Result<&'_ ComparableNodeInfo, FileIoError>> {
        self.next_node_info().transpose()
    }
}

impl<'a, T: HashedNodeReader> PreOrderIterator<'a, T> {
    /// Create a pre-order iterator for the trie that starts at `start_key`.
    fn new(trie: &'a T, start_key: &Key) -> Result<PreOrderIterator<'a, T>, FileIoError> {
        // If the root node is not None, then push a `ComparableNodeInfo` for the root onto
        // the traversal stack. It will be used on the first call to `next` or `next_node_info`.
        // Because we already have the root node, we create its `ComparableNodeInfo` directly
        // instead of using `ComparableNodeInfo::new` as we don't need to create a `SharedNode`
        // from a `Child`. The full path of the root node is just its partial path since it has
        // no pre-path.
        let traversal_stack = trie
            .root_node()
            .into_iter()
            .map(|root| ComparableNodeInfo {
                path: root.partial_path().clone(),
                node: root,
                hash: trie.root_hash().map(|hash| hash.clone().into_triehash()),
            })
            .collect();

        Self {
            node_info: None,
            traversal_stack,
            trie,
        }
        .iterate_to_key(start_key)
    }

    /// In a textbook implementation of pre-order traversal we would normally pop off the next node
    /// from the traversal stack and push its children onto the stack before returning. However, that
    /// would be inefficient if we want to skip traversing a node's children if its hash matches that
    /// of a node in a different trie, as we would then need to pop its children off the traversal
    /// stack.
    ///
    /// This implementation flips the order such that we don't push a node's children onto the
    /// traversal stack until the subseqent call to `next` or `next_node_info`. This is done by saving
    /// the `ComparableNodeInfo` of the current node in `node_info` and keeping that available for the
    /// next call. Skipping traversal of the children then just involves setting `node_info` to None.
    fn next_node_info(&mut self) -> Result<Option<&ComparableNodeInfo>, FileIoError> {
        firewood_increment!(crate::registry::CHANGE_PROOF_NEXT, 1);

        // Take the info of the current node (which will soon be replaced), and check if it is a
        // branch. If it is, add its children onto the traversal stack.
        if let Some(prev_node_info) = self.node_info.take()
            && let Node::Branch(branch) = &*prev_node_info.node
        {
            // Since a stack is LIFO and we want to perform pre-order traversal, we pushed the
            // children in reverse order.
            for (path_comp, child) in branch.children.iter_present().rev() {
                // Generate the pre-path for this child, and push it onto the traversal stack.
                let child_pre_path = Path::from_nibbles_iterator(
                    prev_node_info
                        .path
                        .iter()
                        .copied()
                        .chain(once(path_comp.as_u8())),
                );
                self.traversal_stack.push(ComparableNodeInfo::new(
                    child_pre_path,
                    child,
                    self.trie,
                )?);
            }
        }

        // Pop a `ComparableNodeInfo` from the traversal stack and set it as the current node's
        // info. If the stack is empty, then return None and the iteration is complete. Otherwise
        // return a reference to the current node's info.
        self.node_info = self.traversal_stack.pop();
        Ok(self.node_info.as_ref())
    }

    /// Calling `skip_children` will clear the current node's info, which will cause the children
    /// of the current node to not be added to the traversal stack on the subsequent `next` or
    /// `next_node_info` call. This will effectively cause the traversal to skip the children of
    /// the current node.
    fn skip_children(&mut self) {
        self.node_info = None;
    }

    /// Modify the iterator to skip all keys prior to the specified key.
    fn iterate_to_key(mut self, key: &Key) -> Result<Self, FileIoError> {
        // Function is a no-op if the key is empty.
        if key.is_empty() {
            return Ok(self);
        }
        // Keep iterating until we have reached the start key. Only traverse branches that can
        // contain the start key.
        loop {
            // Push the children that can contain the keys that are larger than or equal to the
            // the start key onto the traversal stack.
            if let Some(prev_node_info) = self.node_info.take()
                && let Node::Branch(branch) = &*prev_node_info.node
            {
                // Create a reverse iterator on the children that includes the child's pre-path.
                let mut reversed_children_with_pre_path =
                    branch
                        .children
                        .iter_present()
                        .rev()
                        .map(|(path_comp, child)| {
                            (
                                child,
                                Path::from_nibbles_iterator(
                                    prev_node_info
                                        .path
                                        .iter()
                                        .copied()
                                        .chain(once(path_comp.as_u8())),
                                ),
                            )
                        });

                for (child, child_pre_path) in reversed_children_with_pre_path.by_ref() {
                    // We only need to traverse this child if its pre-path is a prefix of the key (including
                    // being equal to the key) or is lexicographically larger than the key.
                    let child_key = key_from_nibble_iter(child_pre_path.iter().copied());
                    let path_overlap = PrefixOverlap::from(key, child_key.as_ref());
                    let unique_node = path_overlap.unique_b;
                    if unique_node.is_empty() || child_key > *key {
                        self.traversal_stack.push(ComparableNodeInfo::new(
                            child_pre_path,
                            child,
                            self.trie,
                        )?);
                        // Once we have found the first child that should be traversed, we can break out of
                        // the loop where we we add the rest of the children without the above test.
                        break;
                    }
                }

                // Add the rest of the children (if any) to the traversal stack.
                for node_info in reversed_children_with_pre_path.map(|(child, child_pre_path)| {
                    ComparableNodeInfo::new(child_pre_path, child, self.trie)
                }) {
                    self.traversal_stack.push(node_info?);
                }
            }

            // Pop the next node's info from the stack
            if let Some(node_info) = self.traversal_stack.pop() {
                // Since pre-order traversal of a trie iterates through the nodes in lexicographical
                // order, we can stop the traversal once we see a node key that is larger than or
                // equal to the key. We stop the traversal by pushing the current `ComparableNodeInfo`
                // back to the stack. Calling `next` or `next_node_info` will process this node.
                let node_key = key_from_nibble_iter(node_info.path.iter().copied());
                if node_key >= *key {
                    self.traversal_stack.push(node_info);
                    return Ok(self);
                }

                // If this node is a leaf, then we don't need to save its info to `node_info` as
                // it has no children to traverse.
                if matches!(*node_info.node, Node::Branch(_)) {
                    // Check if this node's path is a prefix of the key. If it is not (`unique_node`
                    // is not empty), then this node's children cannot be larger than or equal to
                    // the key, and we don't need to include them on the traversal stack.
                    let path_overlap = PrefixOverlap::from(key, node_key.as_ref());
                    let unique_node = path_overlap.unique_b;
                    if unique_node.is_empty() {
                        self.node_info = Some(node_info);
                    }
                }
            } else {
                // Traversal stack is empty. This means the key is lexicographically larger than
                // all of the keys in the trie. Calling `next` or `next_node_info` will return None.
                return Ok(self);
            }
        }
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used, clippy::arithmetic_side_effects)]
mod tests {
    use crate::{
        Proof,
        db::{BatchOp, Db, DbConfig},
        iter::{MerkleKeyValueIter, key_from_nibble_iter},
        merkle::{
            Key, Merkle, Value,
            changes::{ChangeProof, DiffMerkleNodeStream, PreOrderIterator},
        },
        v2::api::{Db as _, DbView, Proposal as _},
    };

    use firewood_storage::{
        Committed, FileBacked, FileIoError, HashedNodeReader, ImmutableProposal, MemStore,
        MutableProposal, NodeStore, SeededRng, TestRecorder, TrieReader,
    };
    use lender::Lender;
    use std::{collections::HashSet, ops::Deref, path::PathBuf, sync::Arc};
    use test_case::test_case;

    type BatchOpVec = Vec<BatchOp<Box<[u8]>, Box<[u8]>>>;
    type ImmutableMemstore = Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>>;

    struct TestDb {
        db: Db,
    }

    impl Deref for TestDb {
        type Target = Db;
        fn deref(&self) -> &Self::Target {
            &self.db
        }
    }

    impl TestDb {
        pub fn new() -> Self {
            let tmpdir = tempfile::tempdir().unwrap();
            let dbconfig = DbConfig::builder().truncate(true).build();
            let dbpath: PathBuf = [tmpdir.path().to_path_buf(), PathBuf::from("testdb")]
                .iter()
                .collect();
            let db = Db::new(dbpath, dbconfig.clone()).unwrap();
            TestDb { db }
        }
    }

    fn diff_merkle_iterator<'a, T, U>(
        tree_left: &'a Merkle<T>,
        tree_right: &'a Merkle<U>,
        start_key: Key,
    ) -> Result<DiffMerkleNodeStream<'a, T, U>, FileIoError>
    where
        T: TrieReader + HashedNodeReader,
        U: TrieReader + HashedNodeReader,
    {
        DiffMerkleNodeStream::new(tree_left.nodestore(), tree_right.nodestore(), start_key)
    }

    fn create_test_merkle() -> Merkle<NodeStore<MutableProposal, MemStore>> {
        let memstore = MemStore::default();
        let nodestore = NodeStore::new_empty_proposal(Arc::new(memstore));
        Merkle::from(nodestore)
    }

    fn populate_merkle(
        mut merkle: Merkle<NodeStore<MutableProposal, MemStore>>,
        items: &[(&[u8], &[u8])],
    ) -> Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> {
        for (key, value) in items {
            merkle
                .insert(key, value.to_vec().into_boxed_slice())
                .unwrap();
        }
        merkle.try_into().unwrap()
    }

    fn apply_ops_and_freeze(
        base: &Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>>,
        ops: &[BatchOp<Key, Value>],
    ) -> Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> {
        let mut fork = base.fork().unwrap();
        for op in ops {
            match op {
                BatchOp::Put { key, value } => {
                    fork.insert(key, value.clone()).unwrap();
                }
                BatchOp::Delete { key } => {
                    fork.remove(key).unwrap();
                }
                BatchOp::DeleteRange { prefix } => {
                    fork.remove_prefix(prefix).unwrap();
                }
            }
        }
        fork.try_into().unwrap()
    }

    fn assert_merkle_eq<L, R>(left: &Merkle<L>, right: &Merkle<R>)
    where
        L: TrieReader,
        R: TrieReader,
    {
        let mut l = MerkleKeyValueIter::from(left.nodestore());
        let mut r = MerkleKeyValueIter::from(right.nodestore());
        let mut key_count = 0;
        loop {
            match (l.next(), r.next()) {
                (None, None) => break,
                (Some(Ok((lk, lv))), Some(Ok((rk, rv)))) => {
                    key_count += 1;
                    if lk != rk {
                        println!(
                            "Key mismatch at position {}: left={:02x?}, right={:02x?}",
                            key_count,
                            lk.as_ref(),
                            rk.as_ref()
                        );
                        // Show a few more keys for context
                        for i in 0..3 {
                            match (l.next(), r.next()) {
                                (Some(Ok((lk2, _))), Some(Ok((rk2, _)))) => {
                                    println!(
                                        "  Next {}: left={:02x?}, right={:02x?}",
                                        i + 1,
                                        lk2.as_ref(),
                                        rk2.as_ref()
                                    );
                                }
                                (Some(Ok((lk2, _))), None) => {
                                    println!(
                                        "  Next {}: left={:02x?}, right=None",
                                        i + 1,
                                        lk2.as_ref()
                                    );
                                }
                                (None, Some(Ok((rk2, _)))) => {
                                    println!(
                                        "  Next {}: left=None, right={:02x?}",
                                        i + 1,
                                        rk2.as_ref()
                                    );
                                }
                                _ => break,
                            }
                        }
                        panic!("keys differ at position {key_count}");
                    }
                    assert_eq!(lv, rv, "values differ at key {:02x?}", lk.as_ref());
                }
                (None, Some(Ok((rk, _)))) => panic!(
                    "Missing key in result at position {}: {rk:02x?}",
                    key_count + 1
                ),
                (Some(Ok((lk, _))), None) => panic!(
                    "Extra key in result at position {}: {lk:02x?}",
                    key_count + 1
                ),
                (Some(Err(e)), _) | (_, Some(Err(e))) => panic!("iteration error: {e:?}"),
            }
        }
    }

    fn random_key_from_hashset<'a>(
        rng: &'a SeededRng,
        set: &'a HashSet<Vec<u8>>,
    ) -> Option<Vec<u8>> {
        let len = set.len();
        if len == 0 {
            return None;
        }
        // Generate a random index within the bounds of the set's size
        let index = rng.random_range(0..len);
        // Use the iterator's nth method to get the element at that index
        set.iter().nth(index).cloned()
    }

    fn gen_delete_key(
        rng: &SeededRng,
        committed_keys: &HashSet<Vec<u8>>,
        seen_keys: &mut HashSet<Vec<u8>>,
    ) -> Option<Vec<u8>> {
        let key_opt = random_key_from_hashset(rng, committed_keys);
        // Just skip the key if it has been seen. Otherwise return it and add the key
        // to seen keys.
        key_opt
            .filter(|del_key| !seen_keys.contains(del_key))
            .inspect(|del_key| {
                seen_keys.insert(del_key.clone());
            })
    }

    // Return a vector of `BatchOp`s and the next starting value for use as the `start_val` in the
    // next call to `gen_random_test_batchops`.
    fn gen_random_test_batchops(
        rng: &SeededRng,
        committed_keys: &HashSet<Vec<u8>>,
        num_keys: usize,
        start_val: usize,
    ) -> (BatchOpVec, usize) {
        const CHANCE_DELETE_PERCENT: usize = 2;
        let mut seen_keys = std::collections::HashSet::new();
        let mut batch = Vec::new();

        while batch.len() < num_keys {
            if !committed_keys.is_empty() && rng.random_range(0..100) < CHANCE_DELETE_PERCENT {
                let del_key = gen_delete_key(rng, committed_keys, &mut seen_keys);
                if let Some(key) = del_key {
                    batch.push(BatchOp::Delete {
                        key: key.into_boxed_slice(),
                    });
                    continue;
                }
                // If we couldn't generate a delete key, then just fall through and create
                // a BatchOp::Put.
            }
            let key_len = rng.random_range(2..=32);
            let key: Vec<u8> = (0..key_len).map(|_| rng.random()).collect();
            // Only add if key is unique
            if seen_keys.insert(key.clone()) {
                batch.push(BatchOp::Put {
                    key: key.into_boxed_slice(),
                    value: Box::from(format!("value{}", batch.len() + start_val).as_bytes()),
                });
            }
        }
        let next_val = batch.len() + start_val;
        (batch, next_val)
    }

    #[test]
    fn test_preorder_iterator() {
        let rng = SeededRng::from_env_or_random();
        let (batch, _) = gen_random_test_batchops(&rng, &HashSet::new(), 1000, 0);

        // Keep a sorted copy of the batch.
        let mut batch_sorted = batch.clone();
        batch_sorted.sort_by(|op1, op2| op1.key().cmp(op2.key()));

        // Insert batch into a merkle trie.
        let mut merkle = create_test_merkle();
        for item in &batch {
            // All of the ops should be Puts
            merkle
                .insert(
                    item.key(),
                    item.value().unwrap().to_vec().into_boxed_slice(),
                )
                .unwrap();
        }
        // freeze and compute the hash
        let merkle: Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> =
            merkle.try_into().unwrap();
        assert!(HashedNodeReader::root_hash(merkle.nodestore()).is_some());

        // Check if the sorted batch and the pre-order traversal have identical values.
        let mut preorder_it = PreOrderIterator::new(merkle.nodestore(), &Key::default()).unwrap();
        let mut batch_sorted_it = batch_sorted.clone().into_iter();
        while let Some(node_info) = preorder_it.next() {
            let node_info = node_info.unwrap();
            assert!(node_info.hash.is_some());
            if let Some(val) = node_info.node.value() {
                let key = key_from_nibble_iter(node_info.path.iter().copied());
                let batch_sorted_item = batch_sorted_it.next().unwrap();
                assert!(
                    *key == **batch_sorted_item.key()
                        && *val == **batch_sorted_item.value().unwrap()
                );
            }
        }
        assert!(batch_sorted_it.next().is_none());

        // Second test where we pick a random key from the sorted batch as the start key, and check
        // the sorted batch and pre-order traversal have identical values when using that start key.
        let mut index = rng.random_range(0..batch_sorted.len());
        let start_key = batch_sorted.get(index).unwrap().key().clone();
        let mut preorder_it = PreOrderIterator::new(merkle.nodestore(), &start_key).unwrap();
        while let Some(node_info) = preorder_it.next() {
            let node_info = node_info.unwrap();
            if let Some(val) = node_info.node.value() {
                let key = key_from_nibble_iter(node_info.path.iter().copied());
                let batch_sorted_item = batch_sorted.get(index).unwrap();
                assert!(
                    *key == **batch_sorted_item.key()
                        && *val == **batch_sorted_item.value().unwrap()
                );
                index += 1;
            }
        }
        assert!(index == batch_sorted.len());

        // Third test that just skips the children after calling `next` once. This should skip all of
        // the children of the root node, causing the next call to `next` to return None.
        let mut preorder_it = PreOrderIterator::new(merkle.nodestore(), &Key::default()).unwrap();
        let _ = preorder_it.next().unwrap();
        preorder_it.skip_children();
        assert!(preorder_it.next().is_none());

        // Fourth test that calls `count` on the lending iterator to count the number of nodes.
        let preorder_it = PreOrderIterator::new(merkle.nodestore(), &Key::default()).unwrap();
        assert!(preorder_it.count() > 0);
    }

    #[test]
    fn test_diff_empty_trees() {
        let m1: Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> =
            create_test_merkle().try_into().unwrap();
        let m2: Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> =
            create_test_merkle().try_into().unwrap();

        let mut diff_iter = diff_merkle_iterator(&m1, &m2, Box::new([])).unwrap();
        assert!(diff_iter.next().is_none());
    }

    #[test]
    fn test_diff_identical_trees() {
        let items = [
            (b"key1".as_slice(), b"value1".as_slice()),
            (b"key2".as_slice(), b"value2".as_slice()),
            (b"key3".as_slice(), b"value3".as_slice()),
        ];

        let m1 = populate_merkle(create_test_merkle(), &items);
        let m2 = populate_merkle(create_test_merkle(), &items);

        let mut diff_iter = diff_merkle_iterator(&m1, &m2, Box::new([])).unwrap();
        assert!(diff_iter.next().is_none());
    }

    #[test]
    fn test_diff_additions_only() {
        let items = [
            (b"key1".as_slice(), b"value1".as_slice()),
            (b"key2".as_slice(), b"value2".as_slice()),
        ];

        let m1: Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> =
            create_test_merkle().try_into().unwrap();
        let m2 = populate_merkle(create_test_merkle(), &items);

        let mut diff_iter = diff_merkle_iterator(&m1, &m2, Box::new([])).unwrap();

        let op1 = diff_iter.next().unwrap().unwrap();
        assert!(
            matches!(op1, BatchOp::Put { key, value } if key == Box::from(b"key1".as_slice()) && value.as_ref() == b"value1")
        );

        let op2 = diff_iter.next().unwrap().unwrap();
        assert!(
            matches!(op2, BatchOp::Put { key, value } if key == Box::from(b"key2".as_slice()) && value.as_ref() == b"value2")
        );

        assert!(diff_iter.next().is_none());
    }

    #[test]
    fn test_diff_deletions_only() {
        let items = [
            (b"key1".as_slice(), b"value1".as_slice()),
            (b"key2".as_slice(), b"value2".as_slice()),
        ];

        let m1 = populate_merkle(create_test_merkle(), &items);
        let m2: Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> =
            create_test_merkle().try_into().unwrap();

        let mut diff_iter = diff_merkle_iterator(&m1, &m2, Box::new([])).unwrap();

        let op1 = diff_iter.next().unwrap().unwrap();
        assert!(matches!(op1, BatchOp::Delete { key } if key == Box::from(b"key1".as_slice())));

        let op2 = diff_iter.next().unwrap().unwrap();
        assert!(matches!(op2, BatchOp::Delete { key } if key == Box::from(b"key2".as_slice())));

        assert!(diff_iter.next().is_none());
    }

    #[test]
    fn test_diff_modifications() {
        let m1 = populate_merkle(create_test_merkle(), &[(b"key1", b"old_value")]);
        let m2 = populate_merkle(create_test_merkle(), &[(b"key1", b"new_value")]);

        let mut diff_iter = diff_merkle_iterator(&m1, &m2, Box::new([])).unwrap();

        let op = diff_iter.next().unwrap().unwrap();
        assert!(
            matches!(op, BatchOp::Put { key, value } if key == Box::from(b"key1".as_slice()) && value.as_ref() == b"new_value")
        );

        assert!(diff_iter.next().is_none());
    }

    #[test]
    fn test_diff_mixed_operations() {
        // m1 has: key1=value1, key2=old_value, key3=value3
        // m2 has: key2=new_value, key4=value4
        // Expected: Delete key1, Put key2=new_value, Delete key3, Put key4=value4

        let m1 = populate_merkle(
            create_test_merkle(),
            &[
                (b"key1", b"value1"), // [6b, 65, 79, 31]
                (b"key2", b"old_value"),
                (b"key3", b"value3"),
            ],
        );

        let m2 = populate_merkle(
            create_test_merkle(),
            &[(b"key2", b"new_value"), (b"key4", b"value4")],
        );

        let mut diff_iter = diff_merkle_iterator(&m1, &m2, Box::new([])).unwrap();

        let op1 = diff_iter.next().unwrap().unwrap();
        assert!(matches!(op1, BatchOp::Delete { key } if key == Box::from(b"key1".as_slice())));

        let op2 = diff_iter.next().unwrap().unwrap();
        assert!(
            matches!(op2, BatchOp::Put { key, value } if key == Box::from(b"key2".as_slice()) && value.as_ref() == b"new_value")
        );

        let op3 = diff_iter.next().unwrap().unwrap();
        assert!(matches!(op3, BatchOp::Delete { key } if key == Box::from(b"key3".as_slice())));

        let op4 = diff_iter.next().unwrap().unwrap();
        assert!(
            matches!(op4, BatchOp::Put { key, value } if key == Box::from(b"key4".as_slice()) && value.as_ref() == b"value4")
        );

        assert!(diff_iter.next().is_none());
    }

    #[test]
    #[expect(clippy::indexing_slicing)]
    fn test_diff_interleaved_keys() {
        // m1: a, c, e
        // m2: b, c, d, f
        // Expected: Delete a, Put b, Put d, Delete e, Put f

        let m1 = populate_merkle(
            create_test_merkle(),
            &[(b"a", b"value_a"), (b"c", b"value_c"), (b"e", b"value_e")],
        );

        let m2 = populate_merkle(
            create_test_merkle(),
            &[
                (b"b", b"value_b"),
                (b"c", b"value_c"),
                (b"d", b"value_d"),
                (b"f", b"value_f"),
            ],
        );

        // First case: No start key
        let diff_iter = diff_merkle_iterator(&m1, &m2, Box::new([])).unwrap();
        let ops: Vec<_> = diff_iter.collect::<Result<Vec<_>, _>>().unwrap();
        assert_eq!(ops.len(), 5);
        assert!(matches!(ops[0], BatchOp::Delete { ref key } if **key == *b"a"));
        assert!(
            matches!(ops[1], BatchOp::Put { ref key, ref value } if **key == *b"b" && **value == *b"value_b")
        );
        assert!(
            matches!(ops[2], BatchOp::Put { ref key, ref value } if **key == *b"d" && **value == *b"value_d")
        );
        assert!(matches!(ops[3], BatchOp::Delete { ref key } if **key == *b"e"));
        assert!(
            matches!(ops[4], BatchOp::Put { ref key, ref value } if **key == *b"f" && **value == *b"value_f")
        );
        // Note: "c" should be skipped as it's identical in both trees

        // Second case: "b" start key
        let diff_iter = diff_merkle_iterator(&m1, &m2, Box::from(b"b".as_slice())).unwrap();
        let ops: Vec<_> = diff_iter.collect::<Result<Vec<_>, _>>().unwrap();
        assert_eq!(ops.len(), 4);
        assert!(
            matches!(ops[0], BatchOp::Put { ref key, ref value } if **key == *b"b" && **value == *b"value_b")
        );
        assert!(
            matches!(ops[1], BatchOp::Put { ref key, ref value } if **key == *b"d" && **value == *b"value_d")
        );
        assert!(matches!(ops[2], BatchOp::Delete { ref key } if **key == *b"e"));
        assert!(
            matches!(ops[3], BatchOp::Put { ref key, ref value } if **key == *b"f" && **value == *b"value_f")
        );

        // Third case: "c" start key
        let diff_iter = diff_merkle_iterator(&m1, &m2, Box::from(b"c".as_slice())).unwrap();
        let ops: Vec<_> = diff_iter.collect::<Result<Vec<_>, _>>().unwrap();
        assert_eq!(ops.len(), 3);
        assert!(
            matches!(ops[0], BatchOp::Put { ref key, ref value } if **key == *b"d" && **value == *b"value_d")
        );
        assert!(matches!(ops[1], BatchOp::Delete { ref key } if **key == *b"e"));
        assert!(
            matches!(ops[2], BatchOp::Put { ref key, ref value } if **key == *b"f" && **value == *b"value_f")
        );

        // Fourth case: "da" start key.
        let diff_iter = diff_merkle_iterator(&m1, &m2, Box::from(b"da".as_slice())).unwrap();
        let ops: Vec<_> = diff_iter.collect::<Result<Vec<_>, _>>().unwrap();
        assert_eq!(ops.len(), 2);
        assert!(matches!(ops[0], BatchOp::Delete { ref key } if **key == *b"e"));
        assert!(
            matches!(ops[1], BatchOp::Put { ref key, ref value } if **key == *b"f" && **value == *b"value_f")
        );

        // Fifth case: "f" start key.
        let diff_iter = diff_merkle_iterator(&m1, &m2, Box::from(b"f".as_slice())).unwrap();
        let ops: Vec<_> = diff_iter.collect::<Result<Vec<_>, _>>().unwrap();
        assert_eq!(ops.len(), 1);
        assert!(
            matches!(ops[0], BatchOp::Put { ref key, ref value } if **key == *b"f" && **value == *b"value_f")
        );

        // Sixth case: "g" start key.
        let diff_iter = diff_merkle_iterator(&m1, &m2, Box::from(b"g".as_slice())).unwrap();
        let ops: Vec<_> = diff_iter.collect::<Result<Vec<_>, _>>().unwrap();
        assert_eq!(ops.len(), 0);
    }

    #[test_case(true, false, 0, 1)] // same value, m1->m2: no put needed, delete prefix/b
    #[test_case(false, false, 1, 1)] // diff value, m1->m2: put prefix/a, delete prefix/b
    #[test_case(true, true, 1, 0)] // same value, m2->m1: no change to prefix/a, add prefix/b
    #[test_case(false, true, 2, 0)] // diff value, m2->m1: update prefix/a, add prefix/b
    fn test_branch_vs_leaf_empty_partial_path_bug(
        same_value: bool,
        backwards: bool,
        expected_puts: usize,
        expected_deletes: usize,
    ) {
        // This test creates a case where one tree has a branch with children, and the
        // other tree has a leaf that matches one of those children - testing that the
        // matching child gets excluded from deletion and properly compared instead.
        //
        // Parameters:
        // - same_value: whether prefix/a has the same value in both trees
        // - backwards: whether to compare m2->m1 instead of m1->m2
        // - expected_puts/expected_deletes: expected operation counts

        // Tree1: Create children under "prefix" but no value at "prefix" itself
        // This creates a branch node at "prefix" with value=None
        let m1 = populate_merkle(
            create_test_merkle(),
            &[
                (b"prefix/a".as_slice(), b"value_a".as_slice()),
                (b"prefix/b".as_slice(), b"value_b".as_slice()),
            ],
        );

        // Tree2: Create just a single value at "prefix/a"
        // Value depends on same_value parameter
        let m2_value: &[u8] = if same_value {
            b"value_a"
        } else {
            b"prefix_a_value"
        };
        let m2 = populate_merkle(create_test_merkle(), &[(b"prefix/a".as_slice(), m2_value)]);

        // Choose direction based on backwards parameter
        let (tree_left, tree_right, direction_desc) = if backwards {
            (m2.nodestore(), m1.nodestore(), "m2->m1")
        } else {
            (m1.nodestore(), m2.nodestore(), "m1->m2")
        };

        //let diff_stream = DiffMerkleKeyValueStreams::new(tree_left, tree_right, Key::default());
        let diff_stream = DiffMerkleNodeStream::new(tree_left, tree_right, Key::default()).unwrap();
        let results: Vec<_> = diff_stream.collect::<Result<Vec<_>, _>>().unwrap();

        let delete_count = results
            .iter()
            .filter(|op| matches!(op, BatchOp::Delete { .. }))
            .count();

        let put_count = results
            .iter()
            .filter(|op| matches!(op, BatchOp::Put { .. }))
            .count();

        // Verify against expected counts
        assert_eq!(
            put_count, expected_puts,
            "Put count mismatch for {direction_desc} (same_value={same_value}, backwards={backwards}), results={results:x?}"
        );
        assert_eq!(
            delete_count, expected_deletes,
            "Delete count mismatch for {direction_desc} (same_value={same_value}, backwards={backwards}), results={results:x?}"
        );
        assert_eq!(
            results.len(),
            expected_puts + expected_deletes,
            "Total operation count mismatch for {direction_desc} (same_value={same_value}, backwards={backwards}), results={results:x?}"
        );

        println!(
            "Branch vs leaf test passed: {direction_desc} (same_value={same_value}, backwards={backwards}) - {put_count} puts, {delete_count} deletes"
        );
    }

    #[test]
    fn test_diff_processes_all_branch_children() {
        // This test verifies the bug fix: ensure that after finding different children
        // at the same position in a branch, the algorithm continues to process remaining children
        let m1 = create_test_merkle();
        let m1 = populate_merkle(
            m1,
            &[
                (b"branch_a/file", b"shared_value"),    // This will be identical
                (b"branch_b/file", b"value1"),          // This will be changed
                (b"branch_c/file", b"left_only_value"), // This will be deleted
            ],
        );

        let m2 = create_test_merkle();
        let m2 = populate_merkle(
            m2,
            &[
                (b"branch_a/file", b"shared_value"),     // Identical to tree1
                (b"branch_b/file", b"value1_modified"),  // Different value
                (b"branch_d/file", b"right_only_value"), // This will be added
            ],
        );

        let diff_stream =
            DiffMerkleNodeStream::new(m1.nodestore(), m2.nodestore(), Key::default()).unwrap();

        let results: Vec<_> = diff_stream.collect::<Result<Vec<_>, _>>().unwrap();

        // Should find all differences:
        // 1. branch_b/file modified
        // 2. branch_c/file deleted
        // 3. branch_d/file added
        assert_eq!(results.len(), 3, "Should find all 3 differences");

        // Verify specific operations
        let mut changes = 0;
        let mut deletions = 0;
        let mut additions = 0;

        for result in &results {
            match result {
                BatchOp::Put { key, value: _ } => {
                    if key.as_ref() == b"branch_b/file" {
                        changes += 1;
                        assert_eq!(&**key, b"branch_b/file");
                    } else if key.as_ref() == b"branch_d/file" {
                        additions += 1;
                        assert_eq!(&**key, b"branch_d/file");
                    }
                }
                BatchOp::Delete { key } => {
                    deletions += 1;
                    assert_eq!(&**key, b"branch_c/file");
                }
                BatchOp::DeleteRange { .. } => {
                    panic!("DeleteRange not expected in this test");
                }
            }
        }

        assert_eq!(changes, 1, "Should have 1 change");
        assert_eq!(deletions, 1, "Should have 1 deletion");
        assert_eq!(additions, 1, "Should have 1 addition");
    }

    #[test]
    fn test_diff_states_coverage() {
        // Create trees with carefully designed structure to trigger the following:
        // 1. Deep branching structure to ensure branch nodes exist
        // 2. Mix of shared, modified, left-only, and right-only content
        // 3. Different tree shapes to force visited states
        let tree1_data = vec![
            // Shared deep structure (will trigger VisitedNodePairState)
            (b"shared/deep/branch/file1".as_slice(), b"value1".as_slice()),
            (b"shared/deep/branch/file2".as_slice(), b"value2".as_slice()),
            (b"shared/deep/branch/file3".as_slice(), b"value3".as_slice()),
            // Modified values (will trigger UnvisitedNodePairState)
            (b"modified/path/file".as_slice(), b"old_value".as_slice()),
            // Left-only deep structure (will trigger VisitedNodeLeftState)
            (
                b"left_only/deep/branch/file1".as_slice(),
                b"left_val1".as_slice(),
            ),
            (
                b"left_only/deep/branch/file2".as_slice(),
                b"left_val2".as_slice(),
            ),
            (
                b"left_only/deep/branch/file3".as_slice(),
                b"left_val3".as_slice(),
            ),
            // Simple left-only (will trigger UnvisitedNodeLeftState)
            (
                b"simple_left_only".as_slice(),
                b"simple_left_value".as_slice(),
            ),
            // Mixed branch with some shared children
            (
                b"mixed_branch/shared_child".as_slice(),
                b"shared".as_slice(),
            ),
            (
                b"mixed_branch/left_child".as_slice(),
                b"left_value".as_slice(),
            ),
        ];

        let tree2_data = vec![
            // Same shared deep structure
            (b"shared/deep/branch/file1".as_slice(), b"value1".as_slice()),
            (b"shared/deep/branch/file2".as_slice(), b"value2".as_slice()),
            (b"shared/deep/branch/file3".as_slice(), b"value3".as_slice()),
            // Modified values
            (b"modified/path/file".as_slice(), b"new_value".as_slice()),
            // Right-only deep structure (will trigger VisitedNodeRightState)
            (
                b"right_only/deep/branch/file1".as_slice(),
                b"right_val1".as_slice(),
            ),
            (
                b"right_only/deep/branch/file2".as_slice(),
                b"right_val2".as_slice(),
            ),
            (
                b"right_only/deep/branch/file3".as_slice(),
                b"right_val3".as_slice(),
            ),
            // Simple right-only (will trigger UnvisitedNodeRightState)
            (
                b"simple_right_only".as_slice(),
                b"simple_right_value".as_slice(),
            ),
            // Mixed branch with some shared children
            (
                b"mixed_branch/shared_child".as_slice(),
                b"shared".as_slice(),
            ),
            (
                b"mixed_branch/right_child".as_slice(),
                b"right_value".as_slice(),
            ),
        ];

        let m1 = populate_merkle(create_test_merkle(), &tree1_data);
        let m2 = populate_merkle(create_test_merkle(), &tree2_data);

        let diff_iter = diff_merkle_iterator(&m1, &m2, Key::default()).unwrap();
        let results: Vec<_> = diff_iter.collect::<Result<Vec<_>, _>>().unwrap();

        // Verify we found the expected differences
        let mut deletions = 0;
        let mut additions = 0;

        for result in &results {
            match result {
                BatchOp::Put { .. } => additions += 1,
                BatchOp::Delete { .. } => deletions += 1,
                BatchOp::DeleteRange { .. } => {
                    panic!("DeleteRange not expected in this test");
                }
            }
        }

        // Expected differences using BatchOp representation:
        // - Both modifications and additions are represented as Put operations
        // - Deletions are Delete operations
        // - We expect multiple operations for the different scenarios
        assert!(deletions >= 4, "Expected at least 4 deletions");
        assert!(
            additions >= 4,
            "Expected at least 4 additions (includes modifications)"
        );

        println!("All 6 diff coverage tests passed:");
    }

    #[test]
    fn test_branch_vs_leaf_state_transitions() {
        // This test specifically covers the branch-vs-leaf scenarios in UnvisitedNodePairState
        // which can trigger different state transitions

        // Tree1: Has a branch structure at "path"
        let m1 = populate_merkle(
            create_test_merkle(),
            &[
                (b"path/file1".as_slice(), b"value1".as_slice()),
                (b"path/file2".as_slice(), b"value2".as_slice()),
            ],
        );

        // Tree2: Has a leaf at "path"
        let m2 = populate_merkle(
            create_test_merkle(),
            &[(b"path".as_slice(), b"leaf_value".as_slice())],
        );

        let diff_stream =
            DiffMerkleNodeStream::new(m1.nodestore(), m2.nodestore(), Key::default()).unwrap();

        let results: Vec<_> = diff_stream.collect::<Result<Vec<_>, _>>().unwrap();

        // Should find:
        // - Deletion of path/file1 and path/file2
        // - Addition of path (leaf)
        assert!(
            results.len() >= 2,
            "Should find multiple differences for branch vs leaf"
        );

        println!(
            "Branch vs leaf transitions test passed with {} operations",
            results.len()
        );
    }

    #[test]
    fn test_diff_with_start_key() {
        let m1 = populate_merkle(
            create_test_merkle(),
            &[
                (b"aaa", b"value1"),
                (b"bbb", b"value2"),
                (b"ccc", b"value3"),
            ],
        );

        let m2 = populate_merkle(
            create_test_merkle(),
            &[
                (b"aaa", b"value2"),   // Same
                (b"bbb", b"modified"), // Modified
                (b"ddd", b"value4"),   // Added
            ],
        );

        // Start from key "bbb" - should skip "aaa"
        let mut diff_iter = diff_merkle_iterator(&m1, &m2, Box::from(b"bbb".as_slice())).unwrap();

        let op1 = diff_iter.next().unwrap().unwrap();
        assert!(
            matches!(op1, BatchOp::Put { ref key, ref value } if **key == *b"bbb" && **value == *b"modified"),
            "Expected first operation to be Put bbb=modified, got: {op1:?}",
        );

        let op2 = diff_iter.next().unwrap().unwrap();
        assert!(matches!(op2, BatchOp::Delete { key } if key == Box::from(b"ccc".as_slice())));

        let op3 = diff_iter.next().unwrap().unwrap();
        assert!(
            matches!(op3, BatchOp::Put { key, value } if key == Box::from(b"ddd".as_slice()) && value.as_ref() == b"value4")
        );

        assert!(diff_iter.next().is_none());
    }

    #[test_case(500)]
    #[test_case(10)]
    #[test_case(3)]
    fn test_diff_random_with_deletions(num_items: usize) {
        let rng = SeededRng::from_env_or_random();

        // Generate random key-value pairs, ensuring uniqueness
        let mut items: Vec<(Vec<u8>, Vec<u8>)> = Vec::new();
        let mut seen_keys = std::collections::HashSet::new();

        while items.len() < num_items {
            let key_len = rng.random_range(2..=32);
            let value_len = rng.random_range(1..=64);

            let key: Vec<u8> = (0..key_len).map(|_| rng.random()).collect();

            // Only add if key is unique
            if seen_keys.insert(key.clone()) {
                let value: Vec<u8> = (0..value_len).map(|_| rng.random()).collect();
                items.push((key, value));
            }
        }

        // Create two identical merkles
        let mut m1 = create_test_merkle();
        let mut m2 = create_test_merkle();

        for (key, value) in &items {
            m1.insert(key, value.clone().into_boxed_slice()).unwrap();
            m2.insert(key, value.clone().into_boxed_slice()).unwrap();
        }

        // Pick two different random indices to delete (if possible)
        if !items.is_empty() {
            let delete_idx1 = rng.random_range(0..items.len());
            m1.remove(&items.get(delete_idx1).unwrap().0).unwrap();
        }
        if items.len() > 1 {
            let mut delete_idx2 = rng.random_range(0..items.len());
            // ensure different index
            while items.len() > 1 && delete_idx2 == 0 {
                // it's okay if equal when len==1
                delete_idx2 = rng.random_range(0..items.len());
            }
            m2.remove(&items.get(delete_idx2).unwrap().0).unwrap();
        }

        // Compute ops and immutable views according to mutability flags
        let (ops, m1_immut, m2_immut): (BatchOpVec, ImmutableMemstore, ImmutableMemstore) = {
            let m1_immut: Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> =
                m1.try_into().unwrap();
            let m2_immut: Merkle<NodeStore<Arc<ImmutableProposal>, MemStore>> =
                m2.try_into().unwrap();
            let ops = diff_merkle_iterator(&m1_immut, &m2_immut, Box::new([]))
                .unwrap()
                .collect::<Result<Vec<_>, _>>()
                .unwrap();
            (ops, m1_immut, m2_immut)
        };

        // Apply ops to left immutable and compare with right immutable
        let left_after = apply_ops_and_freeze(&m1_immut, &ops);
        assert_merkle_eq(&left_after, &m2_immut);
    }

    #[test_case(20, 500)]
    fn test_db_fuzz(num_iterations: usize, num_items: usize) {
        fn one_iteration(
            rng: &SeededRng,
            db: &Db,
            committed: Arc<NodeStore<Committed, FileBacked>>,
            committed_keys: &mut HashSet<Vec<u8>>,
            num_items: usize,
            start_val: usize,
        ) -> (Arc<NodeStore<Committed, FileBacked>>, usize) {
            const CHANCE_COMMIT_PERCENT: usize = 25;
            let proposal = NodeStore::new(&committed).unwrap();
            let mut merkle = Merkle::from(proposal);
            let (batch, next_start_val) =
                gen_random_test_batchops(rng, committed_keys, num_items, start_val);
            for op in batch.clone() {
                match op {
                    BatchOp::Put { key, value } => {
                        let _ = merkle.insert(&key, value);
                    }
                    BatchOp::Delete { key } => {
                        let _ = merkle.remove(&key);
                    }
                    BatchOp::DeleteRange { prefix: _ } => {
                        panic!("should not have any DeleteRanges");
                    }
                }
            }
            let nodestore = merkle.into_inner();

            // Randomly choose between comparing the committed nodestore against a
            // mutable proposal or an immutable proposal.
            let ops = {
                let immutable: NodeStore<Arc<ImmutableProposal>, FileBacked> =
                    nodestore.try_into().unwrap();
                diff_merkle_iterator(&committed.clone().into(), &immutable.into(), Box::new([]))
                    .unwrap()
                    .collect::<Result<Vec<_>, _>>()
            }
            .unwrap();

            // Sort by the key of the BatchOp.
            let mut batch_sorted = batch.clone();
            batch_sorted.sort_by(|op1, op2| op1.key().cmp(op2.key()));

            // Check that that proposal is the same size as the diff.
            assert_eq!(batch_sorted.len(), ops.len());

            // Check all of the keys and values match.
            let mut diff_it = ops.iter();
            for current_entry in batch_sorted.clone() {
                let diff_entry = diff_it.next().unwrap();
                match (&current_entry, &diff_entry) {
                    (BatchOp::Put { key: _, value: _ }, BatchOp::Put { key: _, value: _ }) => {
                        assert_eq!(**current_entry.key(), **diff_entry.key());
                        assert_eq!(current_entry.value(), diff_entry.value());
                    }
                    (BatchOp::Delete { key: _ }, BatchOp::Delete { key: _ }) => {
                        assert_eq!(**current_entry.key(), **diff_entry.key());
                    }
                    _ => {
                        panic!("diff does not match proposal");
                    }
                }
            }

            // There is chance that we want to commit the batch.
            if rng.random_range(0..100) < CHANCE_COMMIT_PERCENT {
                let proposal = db.propose(batch.into_iter()).unwrap();
                proposal.commit().unwrap();
                let committed = db.revision(db.root_hash().unwrap()).unwrap();
                // Have to regenerate the committed keys hash set if we commit the proposal
                committed_keys.clear();
                committed_keys.extend(
                    committed
                        .iter()
                        .unwrap()
                        .map(|kv| kv.unwrap())
                        .map(|kv| kv.0.to_vec()),
                );
                (committed, next_start_val)
            } else {
                (committed, next_start_val)
            }
        }

        let db = TestDb::new();
        let rng = SeededRng::from_env_or_random();
        let mut committed_keys = HashSet::new();
        let start_val = 0;

        // Populate the database with an initial batch of keys, as we would use a range proof
        // instead of a change proof if the database was initially empty.
        let (batch, mut start_val) =
            gen_random_test_batchops(&rng, &committed_keys, num_items, start_val);
        let proposal = db.propose(batch.into_iter()).unwrap();
        proposal.commit().unwrap();

        // Create the committed revision that we will use to compare against proposal. We also generate
        // a hashset of all of the keys in the trie for use in selecting delete keys.
        let mut committed = db.revision(db.root_hash().unwrap()).unwrap();
        committed_keys.extend(
            committed
                .iter()
                .unwrap()
                .map(|kv| kv.unwrap())
                .map(|kv| kv.0.to_vec()),
        );

        // Run multiple iterations, with each iteration having a chance to commit the proposal.
        for _ in 0..num_iterations {
            (committed, start_val) = one_iteration(
                &rng,
                &db,
                committed,
                &mut committed_keys,
                num_items,
                start_val,
            );
        }
    }

    #[test]
    fn test_two_round_diff_with_start_keys() {
        let rng = SeededRng::from_env_or_random();
        // Run this test several times.
        for _ in 0..4 {
            let db = TestDb::new();
            let mut committed_keys = HashSet::new();
            let start_val = 0;

            // Populate the database with an initial batch of keys, as we would use a range proof
            // instead of a change proof if the database was initially empty.
            let (batch, _) = gen_random_test_batchops(&rng, &committed_keys, 1000, start_val);
            let proposal = db.propose(batch.into_iter()).unwrap();
            proposal.commit().unwrap();

            // Create the committed revision that we will use to compare against proposal. We also generate
            // a hashset of all of the keys in the trie for use in selecting delete keys.
            let committed = db.revision(db.root_hash().unwrap()).unwrap();
            committed_keys.extend(
                committed
                    .iter()
                    .unwrap()
                    .map(|kv| kv.unwrap())
                    .map(|kv| kv.0.to_vec()),
            );

            // Create an immutable proposal based on a second batch of keys
            let proposal = NodeStore::new(&committed).unwrap();
            let mut merkle = Merkle::from(proposal);
            let (batch, _) = gen_random_test_batchops(&rng, &committed_keys, 1000, start_val);
            for op in batch.clone() {
                match op {
                    BatchOp::Put { key, value } => {
                        let _ = merkle.insert(&key, value);
                    }
                    BatchOp::Delete { key } => {
                        let _ = merkle.remove(&key);
                    }
                    BatchOp::DeleteRange { prefix: _ } => {
                        panic!("should not have any DeleteRanges");
                    }
                }
            }
            let nodestore = merkle.into_inner();
            let target_immutable: NodeStore<Arc<ImmutableProposal>, FileBacked> =
                nodestore.try_into().unwrap();

            // Now generate a diff iterator, but only create a vector from the first
            // one hundred keys.
            let committed = committed.into();
            let target_immutable = target_immutable.into();
            let mut diff_it =
                diff_merkle_iterator(&committed, &target_immutable, Box::new([])).unwrap();

            let mut partial_vec = Vec::new();
            for _ in 0..100 {
                match diff_it.next() {
                    Some(op) => {
                        partial_vec.push(op.unwrap());
                    }
                    None => break,
                }
            }

            // Call next to get the next key. Only use it for the next key.
            let next_key = diff_it.next().map(|op| op.unwrap().key().clone()).unwrap();

            // Apply ops in partial_vec to the db
            let proposal = db.propose(partial_vec.iter()).unwrap();
            proposal.commit().unwrap();
            let committed = db.revision(db.root_hash().unwrap()).unwrap().into();
            let diff_it =
                diff_merkle_iterator(&committed, &target_immutable, next_key.clone()).unwrap();

            let mut partial_vec = Vec::new();
            for op in diff_it {
                let op = op.unwrap();
                assert!(op.key() >= &next_key);
                partial_vec.push(op);
            }

            // Apply ops in partial_vec to the db one last time
            let proposal = db.propose(partial_vec.iter()).unwrap();
            proposal.commit().unwrap();
            let committed = db.revision(db.root_hash().unwrap()).unwrap().into();
            let first_diff_item = diff_merkle_iterator(&committed, &target_immutable, Box::new([]))
                .unwrap()
                .next();

            // The two tries should now be identical.
            assert!(first_diff_item.is_none());
        }
    }

    #[test]
    fn test_hash_optimization_reduces_next_calls() {
        let recorder = TestRecorder::default();
        recorder.with_local_recorder(|| {
            // Create test data with substantial shared content and unique content
            let tree1_items = [
                // Large shared content that will form identical subtrees
                (
                    b"shared/branch_a/deep/file1".as_slice(),
                    b"shared_value1".as_slice(),
                ),
                (
                    b"shared/branch_a/deep/file2".as_slice(),
                    b"shared_value2".as_slice(),
                ),
                (
                    b"shared/branch_a/deep/file3".as_slice(),
                    b"shared_value3".as_slice(),
                ),
                (b"shared/branch_b/file1".as_slice(), b"shared_b1".as_slice()),
                (b"shared/branch_b/file2".as_slice(), b"shared_b2".as_slice()),
                (
                    b"shared/branch_c/deep/nested/file".as_slice(),
                    b"shared_nested".as_slice(),
                ),
                (b"shared/common".as_slice(), b"common_value".as_slice()),
                // Unique to tree1
                (b"tree1_unique/x".as_slice(), b"x_value".as_slice()),
                (b"tree1_unique/y".as_slice(), b"y_value".as_slice()),
                (b"tree1_unique/z".as_slice(), b"z_value".as_slice()),
            ];

            let tree2_items = [
                // Identical shared content
                (
                    b"shared/branch_a/deep/file1".as_slice(),
                    b"shared_value1".as_slice(),
                ),
                (
                    b"shared/branch_a/deep/file2".as_slice(),
                    b"shared_value2".as_slice(),
                ),
                (
                    b"shared/branch_a/deep/file3".as_slice(),
                    b"shared_value3".as_slice(),
                ),
                (b"shared/branch_b/file1".as_slice(), b"shared_b1".as_slice()),
                (b"shared/branch_b/file2".as_slice(), b"shared_b2".as_slice()),
                (
                    b"shared/branch_c/deep/nested/file".as_slice(),
                    b"shared_nested".as_slice(),
                ),
                (b"shared/common".as_slice(), b"common_value".as_slice()),
                // Unique to tree2
                (b"tree2_unique/p".as_slice(), b"p_value".as_slice()),
                (b"tree2_unique/q".as_slice(), b"q_value".as_slice()),
                (b"tree2_unique/r".as_slice(), b"r_value".as_slice()),
            ];

            let m1 = populate_merkle(create_test_merkle(), &tree1_items);
            let m2 = populate_merkle(create_test_merkle(), &tree2_items);

            // Check the number of next calls on two full tree traversals.
            let diff_nexts_before =
                recorder.counter_value(crate::registry::CHANGE_PROOF_NEXT, &[]);

            let mut preorder_it = PreOrderIterator::new(m1.nodestore(), &Key::default()).unwrap();
            while preorder_it.next().is_some() {}
            let mut preorder_it = PreOrderIterator::new(m2.nodestore(), &Key::default()).unwrap();
            while preorder_it.next().is_some() {}

            let diff_nexts_after =
                recorder.counter_value(crate::registry::CHANGE_PROOF_NEXT, &[]);
            let diff_iteration_count = diff_nexts_after - diff_nexts_before;
            println!("Next calls from traversing tries: {diff_iteration_count}");

            // DIFF TEST: Measure next calls from hash-optimized diff operation
            let diff_nexts_before =
                recorder.counter_value(crate::registry::CHANGE_PROOF_NEXT, &[]);

            let diff_stream =
                DiffMerkleNodeStream::new(m1.nodestore(), m2.nodestore(), Box::new([])).unwrap();
            let diff_immutable_results_count = diff_stream.count();

            let diff_nexts_after =
                recorder.counter_value(crate::registry::CHANGE_PROOF_NEXT, &[]);
            let diff_immutable_nexts = diff_nexts_after - diff_nexts_before;

            println!("Diff next calls: {diff_immutable_nexts}");
            println!("Diff results count: {diff_immutable_results_count}");

            // Should have some next calls
            assert!(diff_immutable_nexts > 0, "Expected next calls from diff operation");

            // Verify hash optimization is working - should call next FEWER times than iterating both tries
            assert!(diff_immutable_nexts < diff_iteration_count, "Hash optimization failed");

            // Verify that diff return the correct number of results
            assert!(diff_immutable_results_count == 6, "Retrieved an unexpected number of results");

            println!("Traversal optimization verified: {diff_immutable_nexts} vs {diff_iteration_count} next calls");
        });
    }

    #[test]
    fn test_change_proof_iterator() {
        // Create test data
        let key_values: Box<[BatchOp<Key, Value>]> = Box::new([
            BatchOp::Put {
                key: b"key1".to_vec().into_boxed_slice(),
                value: b"value1".to_vec().into_boxed_slice(),
            },
            BatchOp::Put {
                key: b"key2".to_vec().into_boxed_slice(),
                value: b"value2".to_vec().into_boxed_slice(),
            },
            BatchOp::Put {
                key: b"key3".to_vec().into_boxed_slice(),
                value: b"value3".to_vec().into_boxed_slice(),
            },
        ]);

        // Create empty proofs for testing
        let start_proof = Proof::empty();
        let end_proof = Proof::empty();

        let change_proof = ChangeProof::new(start_proof, end_proof, key_values);

        // Test basic iterator functionality
        let mut iter = change_proof.iter();
        assert_eq!(iter.len(), 3);

        let first = iter.next().unwrap();
        assert!(
            matches!(first, BatchOp::Put { key, value } if **key == *b"key1" && **value == *b"value1"),
        );

        let second = iter.next().unwrap();
        assert!(
            matches!(second, BatchOp::Put { key, value } if **key == *b"key2" && **value == *b"value2"),
        );

        let third = iter.next().unwrap();
        assert!(
            matches!(third, BatchOp::Put { key, value } if **key == *b"key3" && **value == *b"value3"),
        );

        assert!(iter.next().is_none());
    }

    #[test]
    #[expect(clippy::indexing_slicing)]
    fn test_change_proof_into_iterator() {
        let key_values: Box<[BatchOp<Key, Value>]> = Box::new([
            BatchOp::Put {
                key: b"a".to_vec().into_boxed_slice(),
                value: b"alpha".to_vec().into_boxed_slice(),
            },
            BatchOp::Put {
                key: b"b".to_vec().into_boxed_slice(),
                value: b"beta".to_vec().into_boxed_slice(),
            },
        ]);

        let start_proof = Proof::empty();
        let end_proof = Proof::empty();
        let change_proof = ChangeProof::new(start_proof, end_proof, key_values);

        // Test that we can use for-loop syntax
        let mut items = Vec::new();
        for item in &change_proof {
            items.push(item);
        }

        assert_eq!(items.len(), 2);
        assert!(
            matches!(items[0], BatchOp::Put{ key, value } if **key == *b"a" && **value == *b"alpha"),
        );
        assert!(
            matches!(items[1], BatchOp::Put{ key, value } if **key == *b"b" && **value == *b"beta"),
        );
    }

    #[test]
    fn test_empty_change_proof_iterator() {
        let key_values: Box<[BatchOp<Key, Value>]> = Box::new([]);
        let start_proof = Proof::empty();
        let end_proof = Proof::empty();
        let change_proof = ChangeProof::new(start_proof, end_proof, key_values);

        let mut iter = change_proof.iter();
        assert_eq!(iter.len(), 0);
        assert!(iter.next().is_none());

        let items: Vec<_> = change_proof.into_iter().collect();
        assert!(items.is_empty());
    }
}
