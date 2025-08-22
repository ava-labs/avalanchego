// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod try_extend;

pub(crate) use self::try_extend::TryExtend;
use crate::merkle::{Key, Value};
use crate::v2::api;

use firewood_storage::{
    BranchNode, Child, FileIoError, NibblesIterator, Node, PathIterItem, SharedNode, TrieReader,
};
use std::cmp::Ordering;
use std::iter::FusedIterator;

/// Represents an ongoing iteration over a node and its children.
enum IterationNode {
    /// This node has not been returned yet.
    Unvisited {
        /// The key (as nibbles) of this node.
        key: Key,
        node: SharedNode,
    },
    /// This node has been returned. Track which child to visit next.
    Visited {
        /// The key (as nibbles) of this node.
        key: Key,
        /// Returns the non-empty children of this node and their positions
        /// in the node's children array.
        children_iter: Box<dyn Iterator<Item = (u8, Child)> + Send>,
    },
}

impl std::fmt::Debug for IterationNode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Unvisited { key, node } => f
                .debug_struct("Unvisited")
                .field("key", key)
                .field("node", node)
                .finish(),
            Self::Visited {
                key,
                children_iter: _,
            } => f.debug_struct("Visited").field("key", key).finish(),
        }
    }
}

#[derive(Debug)]
enum NodeIterState {
    /// The iterator state is lazily initialized when `poll_next` is called
    /// for the first time. The iteration start key is stored here.
    StartFromKey(Key),
    Iterating {
        /// Each element is a node that will be visited (i.e. returned)
        /// or has been visited but has unvisited children.
        /// On each call to `poll_next` we pop the next element.
        /// If it's unvisited, we visit it.
        /// If it's visited, we push its next child onto this stack.
        iter_stack: Vec<IterationNode>,
    },
}

#[derive(Debug)]
/// An iterator of nodes in order starting from a specific point in the trie.
pub struct MerkleNodeIter<'a, T> {
    state: NodeIterState,
    merkle: &'a T,
}

impl From<Key> for NodeIterState {
    fn from(key: Key) -> Self {
        Self::StartFromKey(key)
    }
}

impl<'a, T: TrieReader> MerkleNodeIter<'a, T> {
    /// Returns a new iterator that will iterate over all the nodes in `merkle`
    /// with keys greater than or equal to `key`.
    pub(super) fn new(merkle: &'a T, key: Key) -> Self {
        Self {
            state: NodeIterState::from(key),
            merkle,
        }
    }
}

impl<T: TrieReader> Iterator for MerkleNodeIter<'_, T> {
    type Item = Result<(Key, SharedNode), FileIoError>;

    fn next(&mut self) -> Option<Self::Item> {
        'outer: loop {
            match &mut self.state {
                NodeIterState::StartFromKey(key) => {
                    match get_iterator_intial_state(self.merkle, key) {
                        Ok(state) => self.state = state,
                        Err(e) => return Some(Err(e)),
                    }
                }
                NodeIterState::Iterating { iter_stack } => {
                    while let Some(mut iter_node) = iter_stack.pop() {
                        match iter_node {
                            IterationNode::Unvisited { key, node } => {
                                match &*node {
                                    Node::Leaf(_) => {}
                                    Node::Branch(branch) => {
                                        // `node` is a branch node. Visit its children next.
                                        iter_stack.push(IterationNode::Visited {
                                            key: key.clone(),
                                            children_iter: Box::new(as_enumerated_children_iter(
                                                branch,
                                            )),
                                        });
                                    }
                                }

                                let key = key_from_nibble_iter(key.iter().copied());
                                return Some(Ok((key, node)));
                            }
                            IterationNode::Visited {
                                ref key,
                                ref mut children_iter,
                            } => {
                                // We returned `node` already. Visit its next child.
                                let Some((pos, child)) = children_iter.next() else {
                                    // We visited all this node's descendants. Go back to its parent.
                                    continue;
                                };

                                let child = match child {
                                    Child::AddressWithHash(addr, _) => {
                                        match self.merkle.read_node(addr) {
                                            Ok(node) => node,
                                            Err(e) => return Some(Err(e)),
                                        }
                                    }
                                    Child::Node(node) => node.clone().into(),
                                    Child::MaybePersisted(maybe_persisted, _) => {
                                        // For MaybePersisted, we need to get the node
                                        match maybe_persisted.as_shared_node(self.merkle) {
                                            Ok(node) => node,
                                            Err(e) => return Some(Err(e)),
                                        }
                                    }
                                };

                                let child_partial_path = child.partial_path().iter().copied();

                                // The child's key is its parent's key, followed by the child's index,
                                // followed by the child's partial path (if any).
                                let child_key: Key = key
                                    .iter()
                                    .copied()
                                    .chain(Some(pos))
                                    .chain(child_partial_path)
                                    .collect();

                                // There may be more children of this node to visit.
                                // Visit it again after visiting its `child`.
                                iter_stack.push(iter_node);

                                iter_stack.push(IterationNode::Unvisited {
                                    key: child_key,
                                    node: child,
                                });

                                continue 'outer;
                            }
                        }
                    }

                    return None;
                }
            }
        }
    }
}

impl<T: TrieReader> FusedIterator for MerkleNodeIter<'_, T> {}

/// Returns the initial state for an iterator over the given `merkle` which starts at `key`.
fn get_iterator_intial_state<T: TrieReader>(
    merkle: &T,
    key: &[u8],
) -> Result<NodeIterState, FileIoError> {
    let Some(root) = merkle.root_node() else {
        // This merkle is empty.
        return Ok(NodeIterState::Iterating { iter_stack: vec![] });
    };
    let mut node = root;

    // Invariant: `matched_key_nibbles` is the path before `node`'s
    // partial path at the start of each loop iteration.
    let mut matched_key_nibbles = vec![];

    let mut unmatched_key_nibbles = NibblesIterator::new(key);

    let mut iter_stack: Vec<IterationNode> = vec![];

    loop {
        // See if `node`'s key is a prefix of `key`.
        let partial_path = node.partial_path();

        let (comparison, new_unmatched_key_nibbles) =
            compare_partial_path(partial_path.iter(), unmatched_key_nibbles);
        unmatched_key_nibbles = new_unmatched_key_nibbles;

        matched_key_nibbles.extend(partial_path.iter());

        match comparison {
            Ordering::Less => {
                // `node` is before `key`. It shouldn't be visited
                // and neither should its descendants.
                return Ok(NodeIterState::Iterating { iter_stack });
            }
            Ordering::Greater => {
                // `node` is after `key`. Visit it first.
                iter_stack.push(IterationNode::Unvisited {
                    key: Box::from(matched_key_nibbles),
                    node,
                });
                return Ok(NodeIterState::Iterating { iter_stack });
            }
            Ordering::Equal => match &*node {
                Node::Leaf(_) => {
                    iter_stack.push(IterationNode::Unvisited {
                        key: matched_key_nibbles.clone().into_boxed_slice(),
                        node,
                    });
                    return Ok(NodeIterState::Iterating { iter_stack });
                }
                Node::Branch(branch) => {
                    let Some(next_unmatched_key_nibble) = unmatched_key_nibbles.next() else {
                        // There is no more key to traverse.
                        iter_stack.push(IterationNode::Unvisited {
                            key: matched_key_nibbles.clone().into_boxed_slice(),
                            node,
                        });

                        return Ok(NodeIterState::Iterating { iter_stack });
                    };

                    // There is no child at `next_unmatched_key_nibble`.
                    // We'll visit `node`'s first child at index > `next_unmatched_key_nibble`
                    // first (if it exists).
                    iter_stack.push(IterationNode::Visited {
                        key: matched_key_nibbles.clone().into_boxed_slice(),
                        children_iter: Box::new(
                            as_enumerated_children_iter(branch)
                                .filter(move |(pos, _)| *pos > next_unmatched_key_nibble),
                        ),
                    });

                    #[expect(clippy::indexing_slicing)]
                    let child = &branch.children[next_unmatched_key_nibble as usize];
                    node = match child {
                        None => return Ok(NodeIterState::Iterating { iter_stack }),
                        Some(Child::AddressWithHash(addr, _)) => merkle.read_node(*addr)?,
                        Some(Child::Node(node)) => (*node).clone().into(), // TODO can we avoid cloning this?
                        Some(Child::MaybePersisted(maybe_persisted, _)) => {
                            // For MaybePersisted, we need to get the node
                            maybe_persisted.as_shared_node(merkle)?
                        }
                    };

                    matched_key_nibbles.push(next_unmatched_key_nibble);
                }
            },
        }
    }
}

#[derive(Debug)]
/// An iterator of key-value pairs in order starting from a specific point in the trie.
pub struct MerkleKeyValueIter<'a, T> {
    iter: MerkleNodeIter<'a, T>,
}

impl<'a, T: TrieReader> From<&'a T> for MerkleKeyValueIter<'a, T> {
    fn from(merkle: &'a T) -> Self {
        Self {
            iter: MerkleNodeIter::new(merkle, Box::new([])),
        }
    }
}

impl<'a, T: TrieReader> MerkleKeyValueIter<'a, T> {
    /// Construct a [`MerkleKeyValueIter`] that will iterate over all the key-value pairs in `merkle`
    /// starting from a particular key
    pub fn from_key<K: AsRef<[u8]>>(merkle: &'a T, key: K) -> Self {
        Self {
            iter: MerkleNodeIter::new(merkle, key.as_ref().into()),
        }
    }
}

impl<T: TrieReader> Iterator for MerkleKeyValueIter<'_, T> {
    type Item = Result<(Key, Value), api::Error>;

    fn next(&mut self) -> Option<Self::Item> {
        self.iter.find_map(|result| {
            result
                .map(|(key, node)| {
                    match &*node {
                        Node::Branch(branch) => {
                            let Some(value) = branch.value.as_ref() else {
                                // This node doesn't have a value to return.
                                // Continue to the next node.
                                return None;
                            };
                            Some((key, value.clone()))
                        }
                        Node::Leaf(leaf) => Some((key, leaf.value.clone())),
                    }
                })
                .map_err(Into::into)
                .transpose()
        })
    }
}

impl<T: TrieReader> FusedIterator for MerkleKeyValueIter<'_, T> {}

#[derive(Debug)]
enum PathIteratorState<'a> {
    Iterating {
        /// The key, as nibbles, of the node at `address`, without the
        /// node's partial path (if any) at the end.
        /// Invariant: If this node has a parent, the parent's key is a
        /// prefix of the key we're traversing to.
        /// Note the node at `address` may not have a key which is a
        /// prefix of the key we're traversing to.
        matched_key: Vec<u8>,
        unmatched_key: NibblesIterator<'a>,
        node: SharedNode,
    },
    Exhausted,
}

/// Iterates over all nodes on the path to a given key starting from the root.
///
/// All nodes are branch nodes except possibly the last, which may be a leaf.
/// All returned nodes have keys which are a prefix of the given key.
/// If the given key is in the trie, the last node is at that key.
#[derive(Debug)]
pub struct PathIterator<'a, 'b, T> {
    state: PathIteratorState<'b>,
    merkle: &'a T,
}

impl<'a, 'b, T: TrieReader> PathIterator<'a, 'b, T> {
    pub(super) fn new(merkle: &'a T, key: &'b [u8]) -> Result<Self, FileIoError> {
        let Some(root) = merkle.root_node() else {
            return Ok(Self {
                state: PathIteratorState::Exhausted,
                merkle,
            });
        };

        Ok(Self {
            merkle,
            state: PathIteratorState::Iterating {
                matched_key: vec![],
                unmatched_key: NibblesIterator::new(key),
                node: root,
            },
        })
    }
}

impl<T: TrieReader> Iterator for PathIterator<'_, '_, T> {
    type Item = Result<PathIterItem, FileIoError>;

    fn next(&mut self) -> Option<Self::Item> {
        // destructuring is necessary here because we need mutable access to `state`
        // at the same time as immutable access to `merkle`.
        let Self { state, merkle } = &mut *self;

        match state {
            PathIteratorState::Exhausted => None,
            PathIteratorState::Iterating {
                matched_key,
                unmatched_key,
                node,
            } => {
                let partial_path = match &**node {
                    Node::Branch(branch) => &branch.partial_path,
                    Node::Leaf(leaf) => &leaf.partial_path,
                };

                let (comparison, unmatched_key) =
                    compare_partial_path(partial_path.iter(), unmatched_key);

                match comparison {
                    Ordering::Less | Ordering::Greater => {
                        self.state = PathIteratorState::Exhausted;
                        None
                    }
                    Ordering::Equal => {
                        matched_key.extend(partial_path.iter());
                        let node_key = matched_key.clone().into_boxed_slice();

                        match &**node {
                            Node::Leaf(_) => {
                                // We're at a leaf so we're done.
                                let node = node.clone();
                                self.state = PathIteratorState::Exhausted;
                                Some(Ok(PathIterItem {
                                    key_nibbles: node_key.clone(),
                                    node,
                                    next_nibble: None,
                                }))
                            }
                            Node::Branch(branch) => {
                                // We're at a branch whose key is a prefix of `key`.
                                // Find its child (if any) that matches the next nibble in the key.
                                let Some(next_unmatched_key_nibble) = unmatched_key.next() else {
                                    // We're at the node at `key` so we're done.
                                    let node = node.clone();
                                    self.state = PathIteratorState::Exhausted;
                                    return Some(Ok(PathIterItem {
                                        key_nibbles: node_key.clone(),
                                        node,
                                        next_nibble: None,
                                    }));
                                };

                                #[expect(clippy::indexing_slicing)]
                                let child = &branch.children[next_unmatched_key_nibble as usize];
                                match child {
                                    None => {
                                        // There's no child at the index of the next nibble in the key.
                                        // There's no node at `key` in this trie so we're done.
                                        let node = node.clone();
                                        self.state = PathIteratorState::Exhausted;
                                        Some(Ok(PathIterItem {
                                            key_nibbles: node_key.clone(),
                                            node,
                                            next_nibble: None,
                                        }))
                                    }
                                    Some(Child::AddressWithHash(child_addr, _)) => {
                                        let child = match merkle.read_node(*child_addr) {
                                            Ok(child) => child,
                                            Err(e) => return Some(Err(e)),
                                        };

                                        let node_key = matched_key.clone().into_boxed_slice();
                                        matched_key.push(next_unmatched_key_nibble);

                                        let ret = node.clone();
                                        *node = child;

                                        Some(Ok(PathIterItem {
                                            key_nibbles: node_key,
                                            node: ret,
                                            next_nibble: Some(next_unmatched_key_nibble),
                                        }))
                                    }
                                    Some(Child::Node(child)) => {
                                        let node_key = matched_key.clone().into_boxed_slice();
                                        matched_key.push(next_unmatched_key_nibble);

                                        let ret = node.clone();
                                        *node = child.clone().into();

                                        Some(Ok(PathIterItem {
                                            key_nibbles: node_key,
                                            node: ret,
                                            next_nibble: Some(next_unmatched_key_nibble),
                                        }))
                                    }
                                    Some(Child::MaybePersisted(maybe_persisted, _)) => {
                                        let child = match maybe_persisted.as_shared_node(merkle) {
                                            Ok(child) => child,
                                            Err(e) => return Some(Err(e)),
                                        };

                                        let node_key = matched_key.clone().into_boxed_slice();
                                        matched_key.push(next_unmatched_key_nibble);

                                        Some(Ok(PathIterItem {
                                            key_nibbles: node_key,
                                            node: child,
                                            next_nibble: Some(next_unmatched_key_nibble),
                                        }))
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

/// Takes in an iterator over a node's partial path and an iterator over the
/// unmatched portion of a key.
/// The first returned element is:
/// * [`Ordering::Less`] if the node is before the key.
/// * [`Ordering::Equal`] if the node is a prefix of the key.
/// * [`Ordering::Greater`] if the node is after the key.
///
/// The second returned element is the unmatched portion of the key after the
/// partial path has been matched.
fn compare_partial_path<'a, I1, I2>(
    partial_path_iter: I1,
    mut unmatched_key_nibbles_iter: I2,
) -> (Ordering, I2)
where
    I1: Iterator<Item = &'a u8>,
    I2: Iterator<Item = u8>,
{
    for next_partial_path_nibble in partial_path_iter {
        let Some(next_key_nibble) = unmatched_key_nibbles_iter.next() else {
            return (Ordering::Greater, unmatched_key_nibbles_iter);
        };

        match next_partial_path_nibble.cmp(&next_key_nibble) {
            Ordering::Less => return (Ordering::Less, unmatched_key_nibbles_iter),
            Ordering::Greater => return (Ordering::Greater, unmatched_key_nibbles_iter),
            Ordering::Equal => {}
        }
    }

    (Ordering::Equal, unmatched_key_nibbles_iter)
}

/// Returns an iterator that returns (`pos`,`child`) for each non-empty child of `branch`,
/// where `pos` is the position of the child in `branch`'s children array.
fn as_enumerated_children_iter(branch: &BranchNode) -> impl Iterator<Item = (u8, Child)> + use<> {
    branch
        .children
        .clone()
        .into_iter()
        .enumerate()
        .filter_map(|(pos, child)| child.map(|child| (pos as u8, child)))
}

#[cfg(feature = "branch_factor_256")]
fn key_from_nibble_iter<Iter: Iterator<Item = u8>>(nibbles: Iter) -> Key {
    nibbles.collect()
}

#[cfg(not(feature = "branch_factor_256"))]
fn key_from_nibble_iter<Iter: Iterator<Item = u8>>(mut nibbles: Iter) -> Key {
    let mut data = Vec::with_capacity(nibbles.size_hint().0 / 2);

    while let (Some(hi), Some(lo)) = (nibbles.next(), nibbles.next()) {
        let byte = hi
            .checked_shl(4)
            .and_then(|v| v.checked_add(lo))
            .expect("Nibble overflow while constructing byte");
        data.push(byte);
    }

    data.into_boxed_slice()
}

#[cfg(test)]
#[expect(clippy::indexing_slicing, clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::merkle::Merkle;
    use firewood_storage::{ImmutableProposal, MemStore, MutableProposal, NodeStore};
    use std::sync::Arc;
    use test_case::test_case;

    pub(super) fn create_test_merkle() -> Merkle<NodeStore<MutableProposal, MemStore>> {
        let memstore = MemStore::new(vec![]);
        let memstore = Arc::new(memstore);
        let nodestore = NodeStore::new_empty_proposal(memstore);
        Merkle::from(nodestore)
    }

    #[test_case(&[]; "empty key")]
    #[test_case(&[1]; "non-empty key")]
    fn path_iterate_empty_merkle_empty_key(key: &[u8]) {
        let merkle = create_test_merkle();
        let mut iter = merkle.path_iter(key).unwrap();
        assert!(iter.next().is_none());
    }

    #[test_case(&[],false; "empty key")]
    #[test_case(&[0xBE,0xE0],false; "prefix of singleton key")]
    #[test_case(&[0xBE, 0xEF],true; "match singleton key")]
    #[test_case(&[0xBE, 0xEF,0x10],true; "suffix of singleton key")]
    #[test_case(&[0xF0],false; "no key nibbles match singleton key")]
    fn path_iterate_singleton_merkle(key: &[u8], should_yield_elt: bool) {
        let mut merkle = create_test_merkle();

        merkle.insert(&[0xBE, 0xEF], Box::new([0x42])).unwrap();

        let mut iter = merkle.path_iter(key).unwrap();
        let node = match iter.next() {
            Some(Ok(item)) => item,
            Some(Err(e)) => panic!("{e:?}"),
            None => {
                assert!(!should_yield_elt);
                return;
            }
        };

        assert!(should_yield_elt);
        #[cfg(not(feature = "branch_factor_256"))]
        assert_eq!(
            node.key_nibbles,
            vec![0x0B, 0x0E, 0x0E, 0x0F].into_boxed_slice()
        );
        #[cfg(feature = "branch_factor_256")]
        assert_eq!(node.key_nibbles, vec![0xBE, 0xEF].into_boxed_slice());
        assert_eq!(node.node.as_leaf().unwrap().value, Box::from([0x42]));
        assert_eq!(node.next_nibble, None);

        assert!(iter.next().is_none());
    }

    #[test_case(&[0x00, 0x00, 0x00, 0xFF]; "leaf key")]
    #[test_case(&[0x00, 0x00, 0x00, 0xFF, 0x01]; "leaf key suffix")]
    fn path_iterate_non_singleton_merkle_seek_leaf(key: &[u8]) {
        let merkle = created_populated_merkle();

        let mut iter = merkle.path_iter(key).unwrap();

        let node = match iter.next() {
            Some(Ok(node)) => node,
            Some(Err(e)) => panic!("{e:?}"),
            None => panic!("unexpected end of iterator"),
        };
        #[cfg(not(feature = "branch_factor_256"))]
        assert_eq!(node.key_nibbles, vec![0x00, 0x00].into_boxed_slice());
        #[cfg(feature = "branch_factor_256")]
        assert_eq!(node.key_nibbles, vec![0].into_boxed_slice());
        assert_eq!(node.next_nibble, Some(0));
        assert!(node.node.as_branch().unwrap().value.is_none());

        let node = match iter.next() {
            Some(Ok(node)) => node,
            Some(Err(e)) => panic!("{e:?}"),
            None => panic!("unexpected end of iterator"),
        };
        #[cfg(not(feature = "branch_factor_256"))]
        assert_eq!(
            node.key_nibbles,
            vec![0x00, 0x00, 0x00, 0x00, 0x00, 0x00].into_boxed_slice()
        );
        #[cfg(feature = "branch_factor_256")]
        assert_eq!(node.key_nibbles, vec![0, 0, 0].into_boxed_slice());

        #[cfg(not(feature = "branch_factor_256"))]
        assert_eq!(node.next_nibble, Some(0x0F));
        #[cfg(feature = "branch_factor_256")]
        assert_eq!(node.next_nibble, Some(0xFF));

        assert_eq!(
            node.node.as_branch().unwrap().value,
            Some(vec![0x00, 0x00, 0x00].into_boxed_slice()),
        );

        let node = match iter.next() {
            Some(Ok(node)) => node,
            Some(Err(e)) => panic!("{e:?}"),
            None => panic!("unexpected end of iterator"),
        };
        #[cfg(not(feature = "branch_factor_256"))]
        assert_eq!(
            node.key_nibbles,
            vec![0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0F, 0x0F].into_boxed_slice()
        );
        assert_eq!(node.next_nibble, None);
        assert_eq!(
            node.node.as_leaf().unwrap().value,
            Box::from([0x00, 0x00, 0x00, 0x0FF])
        );

        assert!(iter.next().is_none());
    }

    #[test_case(&[0x00, 0x00, 0x00]; "branch key")]
    #[test_case(&[0x00, 0x00, 0x00, 0x10]; "branch key suffix (but not a leaf key)")]
    fn path_iterate_non_singleton_merkle_seek_branch(key: &[u8]) {
        let merkle = created_populated_merkle();

        let mut iter = merkle.path_iter(key).unwrap();

        let node = match iter.next() {
            Some(Ok(node)) => node,
            Some(Err(e)) => panic!("{e:?}"),
            None => panic!("unexpected end of iterator"),
        };
        // TODO: make this branch factor 16 compatible
        #[cfg(not(feature = "branch_factor_256"))]
        assert_eq!(node.key_nibbles, vec![0x00, 0x00].into_boxed_slice());

        assert!(node.node.as_branch().unwrap().value.is_none());
        assert_eq!(node.next_nibble, Some(0));

        let node = match iter.next() {
            Some(Ok(node)) => node,
            Some(Err(e)) => panic!("{e:?}"),
            None => panic!("unexpected end of iterator"),
        };
        #[cfg(not(feature = "branch_factor_256"))]
        assert_eq!(
            node.key_nibbles,
            vec![0x00, 0x00, 0x00, 0x00, 0x00, 0x00].into_boxed_slice()
        );
        assert_eq!(
            node.node.as_branch().unwrap().value,
            Some(vec![0x00, 0x00, 0x00].into_boxed_slice()),
        );
        assert_eq!(node.next_nibble, None);

        assert!(iter.next().is_none());
    }

    #[test]
    fn key_value_iterate_empty() {
        let merkle = create_test_merkle();
        let iter = merkle.key_value_iter_from_key(b"x".to_vec().into_boxed_slice());
        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn node_iterate_empty() {
        let merkle = create_test_merkle();
        let iter = MerkleNodeIter::new(merkle.nodestore(), Box::new([]));
        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn node_iterate_root_only() {
        let mut merkle = create_test_merkle();

        merkle.insert(&[0x00], Box::new([0x00])).unwrap();

        let mut iter = MerkleNodeIter::new(merkle.nodestore(), Box::new([]));

        let (key, node) = iter.next().unwrap().unwrap();

        assert_eq!(key, vec![0x00].into_boxed_slice());
        assert_eq!(node.as_leaf().unwrap().value.to_vec(), vec![0x00]);

        assert_iterator_is_exhausted(iter);
    }

    /// Returns a new [Merkle] with the following key-value pairs:
    /// Note each hex symbol in the keys below is a nibble (not two nibbles).
    /// Each hex symbol in the values below is a byte.
    /// 000000 --> 000000
    /// 00000001 -->00000001
    /// 000000FF --> 000000FF
    /// 00D0D0 --> 00D0D0
    /// 00FF --> 00FF
    /// structure:
    ///        00 <-- branch with no value
    ///     0/  D|   \F
    ///   000   0D0   F <-- leaf with no partial path
    ///  0/ \F
    ///  1   F
    ///
    /// The number next to each branch is the position of the child in the branch's children array.
    fn created_populated_merkle() -> Merkle<NodeStore<MutableProposal, MemStore>> {
        let mut merkle = create_test_merkle();

        merkle
            .insert(&[0x00, 0x00, 0x00], Box::new([0x00, 0x00, 0x00]))
            .unwrap();
        merkle
            .insert(
                &[0x00, 0x00, 0x00, 0x01],
                Box::new([0x00, 0x00, 0x00, 0x01]),
            )
            .unwrap();
        merkle
            .insert(
                &[0x00, 0x00, 0x00, 0xFF],
                Box::new([0x00, 0x00, 0x00, 0xFF]),
            )
            .unwrap();
        merkle
            .insert(&[0x00, 0xD0, 0xD0], Box::new([0x00, 0xD0, 0xD0]))
            .unwrap();
        merkle
            .insert(&[0x00, 0xFF], Box::new([0x00, 0xFF]))
            .unwrap();
        merkle
    }

    #[test]
    fn node_iterator_no_start_key() {
        let merkle = created_populated_merkle();

        let mut iter = MerkleNodeIter::new(merkle.nodestore(), Box::new([]));

        // Covers case of branch with no value
        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00].into_boxed_slice());
        let node = node.as_branch().unwrap();
        assert!(node.value.is_none());

        // Covers case of branch with value
        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0x00, 0x00].into_boxed_slice());
        let node = node.as_branch().unwrap();
        assert_eq!(node.value.clone().unwrap().to_vec(), vec![0x00, 0x00, 0x00]);

        // Covers case of leaf with partial path
        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0x00, 0x00, 0x01].into_boxed_slice());
        let node = node.as_leaf().unwrap();
        assert_eq!(node.clone().value.to_vec(), vec![0x00, 0x00, 0x00, 0x01]);

        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0x00, 0x00, 0xFF].into_boxed_slice());
        let node = node.as_leaf().unwrap();
        assert_eq!(node.clone().value.to_vec(), vec![0x00, 0x00, 0x00, 0xFF]);

        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xD0, 0xD0].into_boxed_slice());
        let node = node.as_leaf().unwrap();
        assert_eq!(node.clone().value.to_vec(), vec![0x00, 0xD0, 0xD0]);

        // Covers case of leaf with no partial path
        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xFF].into_boxed_slice());
        let node = node.as_leaf().unwrap();
        assert_eq!(node.clone().value.to_vec(), vec![0x00, 0xFF]);

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn node_iterator_start_key_between_nodes() {
        let merkle = created_populated_merkle();

        let mut iter = MerkleNodeIter::new(
            merkle.nodestore(),
            vec![0x00, 0x00, 0x01].into_boxed_slice(),
        );

        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xD0, 0xD0].into_boxed_slice());
        assert_eq!(
            node.as_leaf().unwrap().clone().value.to_vec(),
            vec![0x00, 0xD0, 0xD0]
        );

        // Covers case of leaf with no partial path
        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xFF].into_boxed_slice());
        assert_eq!(
            node.as_leaf().unwrap().clone().value.to_vec(),
            vec![0x00, 0xFF]
        );

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn node_iterator_start_key_on_node() {
        let merkle = created_populated_merkle();

        let mut iter = MerkleNodeIter::new(
            merkle.nodestore(),
            vec![0x00, 0xD0, 0xD0].into_boxed_slice(),
        );

        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xD0, 0xD0].into_boxed_slice());
        assert_eq!(
            node.as_leaf().unwrap().clone().value.to_vec(),
            vec![0x00, 0xD0, 0xD0]
        );

        // Covers case of leaf with no partial path
        let (key, node) = iter.next().unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xFF].into_boxed_slice());
        assert_eq!(
            node.as_leaf().unwrap().clone().value.to_vec(),
            vec![0x00, 0xFF]
        );

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn node_iterator_start_key_after_last_key() {
        let merkle = created_populated_merkle();

        let iter = MerkleNodeIter::new(merkle.nodestore(), vec![0xFF].into_boxed_slice());

        assert_iterator_is_exhausted(iter);
    }

    #[test_case(Some(&[u8::MIN]); "Starting at first key")]
    #[test_case(None; "No start specified")]
    #[test_case(Some(&[128u8]); "Starting in middle")]
    #[test_case(Some(&[u8::MAX]); "Starting at last key")]
    fn key_value_iterate_many(start: Option<&[u8]>) {
        let mut merkle = create_test_merkle();

        // insert all values from u8::MIN to u8::MAX, with the key and value the same
        for k in u8::MIN..=u8::MAX {
            merkle.insert(&[k], Box::new([k])).unwrap();
        }

        let mut iter = match start {
            Some(start) => merkle.key_value_iter_from_key(start.to_vec().into_boxed_slice()),
            None => merkle.key_value_iter(),
        };

        // we iterate twice because we should get a None then start over
        #[expect(clippy::indexing_slicing)]
        for k in start.map(|r| r[0]).unwrap_or_default()..=u8::MAX {
            let next = iter.next().map(|kv| {
                let (k, v) = kv.unwrap();
                assert_eq!(&*k, &*v);
                k
            });

            assert_eq!(next, Some(vec![k].into_boxed_slice()));
        }

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_fused_empty() {
        let merkle = create_test_merkle();
        assert_iterator_is_exhausted(merkle.key_value_iter());
    }

    #[test]
    fn key_value_table_test() {
        let mut merkle = create_test_merkle();

        let max: u8 = 100;
        // Insert key-values in reverse order to ensure iterator
        // doesn't just return the keys in insertion order.
        for i in (0..=max).rev() {
            for j in (0..=max).rev() {
                let key = &[i, j];
                let value = Box::new([i, j]);

                merkle.insert(key, value).unwrap();
            }
        }

        // Test with no start key
        let mut iter = merkle.key_value_iter();
        for i in 0..=max {
            for j in 0..=max {
                let expected_key = vec![i, j];
                let expected_value = vec![i, j];

                assert_eq!(
                    iter.next().unwrap().unwrap(),
                    (
                        expected_key.into_boxed_slice(),
                        expected_value.into_boxed_slice(),
                    ),
                    "i: {i}, j: {j}",
                );
            }
        }
        assert_iterator_is_exhausted(iter);

        // Test with start key
        for i in 0..=max {
            let mut iter = merkle.key_value_iter_from_key(vec![i].into_boxed_slice());
            for j in 0..=max {
                let expected_key = vec![i, j];
                let expected_value = vec![i, j];
                assert_eq!(
                    iter.next().unwrap().unwrap(),
                    (
                        expected_key.into_boxed_slice(),
                        expected_value.into_boxed_slice(),
                    ),
                    "i: {i}, j: {j}",
                );
            }
            if i == max {
                assert_iterator_is_exhausted(iter);
            } else {
                assert_eq!(
                    iter.next().unwrap().unwrap(),
                    (
                        vec![i + 1, 0].into_boxed_slice(),
                        vec![i + 1, 0].into_boxed_slice(),
                    ),
                    "i: {i}",
                );
            }
        }
    }

    #[test]
    fn key_value_fused_full() {
        let mut merkle = create_test_merkle();

        let last = vec![0x00, 0x00, 0x00];

        let mut key_values = vec![vec![0x00], vec![0x00, 0x00], last.clone()];

        // branchs with paths (or extensions) will be present as well as leaves with siblings
        for kv in u8::MIN..=u8::MAX {
            let mut last = last.clone();
            last.push(kv);
            key_values.push(last);
        }

        for kv in &key_values {
            merkle.insert(kv, kv.clone().into_boxed_slice()).unwrap();
        }

        let mut iter = merkle.key_value_iter();

        for kv in &key_values {
            let next = iter.next().unwrap().unwrap();
            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.1, &**kv);
        }

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_root_with_empty_value() {
        let mut merkle = create_test_merkle();

        let key = vec![].into_boxed_slice();
        let value = [0x00];

        merkle.insert(&key, value.into()).unwrap();

        let mut iter = merkle.key_value_iter();

        assert_eq!(iter.next().unwrap().unwrap(), (key, value.into()));
    }

    #[test]
    fn key_value_get_branch_and_leaf() {
        let mut merkle = create_test_merkle();

        let first_leaf = [0x00, 0x00];
        let second_leaf = [0x00, 0x0f];
        let branch = [0x00];

        merkle.insert(&first_leaf, first_leaf.into()).unwrap();
        merkle.insert(&second_leaf, second_leaf.into()).unwrap();

        merkle.insert(&branch, branch.into()).unwrap();

        let immutable_merkle: Merkle<NodeStore<Arc<ImmutableProposal>, _>> =
            merkle.try_into().unwrap();
        println!("{}", immutable_merkle.dump_to_string().unwrap());
        merkle = immutable_merkle.fork().unwrap();

        let mut iter = merkle.key_value_iter();

        assert_eq!(
            iter.next().unwrap().unwrap(),
            (branch.into(), branch.into())
        );

        assert_eq!(
            iter.next().unwrap().unwrap(),
            (first_leaf.into(), first_leaf.into())
        );

        assert_eq!(
            iter.next().unwrap().unwrap(),
            (second_leaf.into(), second_leaf.into())
        );
    }

    #[test]
    fn key_value_start_at_key_not_in_trie() {
        let mut merkle = create_test_merkle();

        let first_key = 0x00;
        let intermediate = 0x80;

        assert!(first_key < intermediate);

        let key_values = [
            vec![first_key],
            vec![intermediate, intermediate],
            vec![intermediate, intermediate, intermediate],
        ];
        assert!(key_values[0] < key_values[1]);
        assert!(key_values[1] < key_values[2]);

        for key in &key_values {
            merkle.insert(key, key.clone().into_boxed_slice()).unwrap();
        }

        let mut iter = merkle.key_value_iter_from_key(vec![intermediate].into_boxed_slice());

        let first_expected = key_values[1].as_slice();
        let first = iter.next().unwrap().unwrap();

        assert_eq!(&*first.0, &*first.1);
        assert_eq!(&*first.1, first_expected);

        let second_expected = key_values[2].as_slice();
        let second = iter.next().unwrap().unwrap();

        assert_eq!(&*second.0, &*second.1);
        assert_eq!(&*second.1, second_expected);

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_on_branch_with_no_value() {
        let sibling_path = 0x00;
        let branch_path = 0x0f;
        let children = 0..=0x0f;

        let mut merkle = create_test_merkle();

        children.clone().for_each(|child_path| {
            let key = vec![sibling_path, child_path];

            merkle.insert(&key, key.clone().into()).unwrap();
        });

        let mut keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![branch_path, child_path];

                merkle.insert(&key, key.clone().into()).unwrap();

                key
            })
            .collect();

        keys.sort();

        let start = keys.iter().position(|key| key[0] == branch_path).unwrap();
        let keys = &keys[start..];

        let mut iter = merkle.key_value_iter_from_key(vec![branch_path].into_boxed_slice());

        for key in keys {
            let next = iter.next().unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_on_branch_with_value() {
        let sibling_path = 0x00;
        let branch_path = 0x0f;
        let branch_key = vec![branch_path];

        let children = (0..=0xf).map(|val| (val << 4) + val); // 0x00, 0x11, ... 0xff

        let mut merkle = create_test_merkle();

        merkle
            .insert(&branch_key, branch_key.clone().into())
            .unwrap();

        children.clone().for_each(|child_path| {
            let key = vec![sibling_path, child_path];

            merkle.insert(&key, key.clone().into()).unwrap();
        });

        let mut keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![branch_path, child_path];

                merkle.insert(&key, key.clone().into()).unwrap();

                key
            })
            .chain(Some(branch_key.clone()))
            .collect();

        keys.sort();

        let start = keys.iter().position(|key| key == &branch_key).unwrap();
        let keys = &keys[start..];

        let mut iter = merkle.key_value_iter_from_key(branch_key.into_boxed_slice());

        for key in keys {
            let next = iter.next().unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_on_extension() {
        let missing = 0x0a;
        let children = (0..=0x0f).filter(|x| *x != missing);
        let mut merkle = create_test_merkle();

        let keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![child_path];

                merkle.insert(&key, key.clone().into()).unwrap();

                key
            })
            .collect();

        let keys = &keys[(missing as usize)..];

        let mut iter = merkle.key_value_iter_from_key(vec![missing].into_boxed_slice());

        for key in keys {
            let next = iter.next().unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_overlapping_with_extension_but_greater() {
        let start_key = 0x0a;
        let shared_path = 0x09;
        // 0x0900, 0x0901, ... 0x0a0f
        // path extension is 0x090
        let children = (0..=0x0f).map(|val| vec![shared_path, val]);

        let mut merkle = create_test_merkle();

        children.for_each(|key| {
            merkle.insert(&key, key.clone().into()).unwrap();
        });

        let iter = merkle.key_value_iter_from_key(vec![start_key].into_boxed_slice());

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_overlapping_with_extension_but_smaller() {
        let start_key = 0x00;
        let shared_path = 0x09;
        // 0x0900, 0x0901, ... 0x0a0f
        // path extension is 0x090
        let children = (0..=0x0f).map(|val| vec![shared_path, val]);

        let mut merkle = create_test_merkle();

        let keys: Vec<_> = children
            .inspect(|key| {
                merkle.insert(key, key.clone().into()).unwrap();
            })
            .collect();

        let mut iter = merkle.key_value_iter_from_key(vec![start_key].into_boxed_slice());

        for key in keys {
            let next = iter.next().unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_between_siblings() {
        let missing = 0xaa;
        let children = (0..=0xf)
            .map(|val| (val << 4) + val) // 0x00, 0x11, ... 0xff
            .filter(|x| *x != missing);
        let mut merkle = create_test_merkle();

        let keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![child_path];

                merkle.insert(&key, key.clone().into()).unwrap();

                key
            })
            .collect();

        let keys = &keys[((missing >> 4) as usize)..];

        let mut iter = merkle.key_value_iter_from_key(vec![missing].into_boxed_slice());

        for key in keys {
            let next = iter.next().unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_greater_than_all_others_leaf() {
        let key = [0x00];
        let greater_key = [0xff];
        let mut merkle = create_test_merkle();
        merkle.insert(&key, key.into()).unwrap();

        let iter = merkle.key_value_iter_from_key(greater_key);

        assert_iterator_is_exhausted(iter);
    }

    #[test]
    fn key_value_start_at_key_greater_than_all_others_branch() {
        let greatest = 0xff;
        let children = (0..=0xf)
            .map(|val| (val << 4) + val) // 0x00, 0x11, ... 0xff
            .filter(|x| *x != greatest);
        let mut merkle = create_test_merkle();

        let keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![child_path];

                merkle.insert(&key, key.clone().into()).unwrap();

                key
            })
            .collect();

        let keys = &keys[((greatest >> 4) as usize)..];

        let mut iter = merkle.key_value_iter_from_key(vec![greatest].into_boxed_slice());

        for key in keys {
            let next = iter.next().unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        assert_iterator_is_exhausted(iter);
    }

    fn assert_iterator_is_exhausted<I: FusedIterator>(mut iter: I) {
        assert!(iter.next().is_none());
    }
}
