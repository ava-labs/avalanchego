// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{node::Node, BranchNode, Key, Merkle, MerkleError, NodeObjRef, NodeType, Value};
use crate::{
    nibbles::{Nibbles, NibblesIterator},
    shale::{DiskAddress, ShaleStore},
    v2::api,
};
use futures::{stream::FusedStream, Stream, StreamExt};
use std::task::Poll;
use std::{cmp::Ordering, iter::once};

/// Represents an ongoing iteration over a node and its children.
enum IterationNode<'a> {
    /// This node has not been returned yet.
    Unvisited {
        /// The key (as nibbles) of this node.
        key: Key,
        node: NodeObjRef<'a>,
    },
    /// This node has been returned. Track which child to visit next.
    Visited {
        /// The key (as nibbles) of this node.
        key: Key,
        /// Returns the non-empty children of this node and their positions
        /// in the node's children array.
        children_iter: Box<dyn Iterator<Item = (u8, DiskAddress)> + Send>,
    },
}

enum NodeStreamState<'a> {
    /// The iterator state is lazily initialized when poll_next is called
    /// for the first time. The iteration start key is stored here.
    StartFromKey(Key),
    Iterating {
        /// Each element is a node that will be visited (i.e. returned)
        /// or has been visited but has unvisited children.
        /// On each call to poll_next we pop the next element.
        /// If it's unvisited, we visit it.
        /// If it's visited, we push its next child onto this stack.
        iter_stack: Vec<IterationNode<'a>>,
    },
}

impl NodeStreamState<'_> {
    fn new(key: Key) -> Self {
        Self::StartFromKey(key)
    }
}

pub struct MerkleNodeStream<'a, S, T> {
    state: NodeStreamState<'a>,
    merkle_root: DiskAddress,
    merkle: &'a Merkle<S, T>,
}

impl<'a, S: ShaleStore<Node> + Send + Sync, T> FusedStream for MerkleNodeStream<'a, S, T> {
    fn is_terminated(&self) -> bool {
        // The top of `iter_stack` is the next node to return.
        // If `iter_stack` is empty, there are no more nodes to visit.
        matches!(&self.state, NodeStreamState::Iterating { iter_stack } if iter_stack.is_empty())
    }
}

impl<'a, S, T> MerkleNodeStream<'a, S, T> {
    /// Returns a new iterator that will iterate over all the nodes in `merkle`
    /// with keys greater than or equal to `key`.
    pub(super) fn new(merkle: &'a Merkle<S, T>, merkle_root: DiskAddress, key: Key) -> Self {
        Self {
            state: NodeStreamState::new(key),
            merkle_root,
            merkle,
        }
    }
}

impl<'a, S: ShaleStore<Node> + Send + Sync, T> Stream for MerkleNodeStream<'a, S, T> {
    type Item = Result<(Key, NodeObjRef<'a>), api::Error>;

    fn poll_next(
        mut self: std::pin::Pin<&mut Self>,
        _cx: &mut std::task::Context<'_>,
    ) -> Poll<Option<Self::Item>> {
        // destructuring is necessary here because we need mutable access to `state`
        // at the same time as immutable access to `merkle`.
        let Self {
            state,
            merkle_root,
            merkle,
        } = &mut *self;

        match state {
            NodeStreamState::StartFromKey(key) => {
                self.state = get_iterator_intial_state(merkle, *merkle_root, key)?;
                self.poll_next(_cx)
            }
            NodeStreamState::Iterating { iter_stack } => {
                while let Some(mut iter_node) = iter_stack.pop() {
                    match iter_node {
                        IterationNode::Unvisited { key, node } => {
                            match node.inner() {
                                NodeType::Branch(branch) => {
                                    // `node` is a branch node. Visit its children next.
                                    iter_stack.push(IterationNode::Visited {
                                        key: key.clone(),
                                        children_iter: Box::new(as_enumerated_children_iter(
                                            branch,
                                        )),
                                    });
                                }
                                NodeType::Leaf(_) => {}
                                NodeType::Extension(_) => {
                                    unreachable!("extension nodes shouldn't exist")
                                }
                            }

                            let key = key_from_nibble_iter(key.iter().copied().skip(1));
                            return Poll::Ready(Some(Ok((key, node))));
                        }
                        IterationNode::Visited {
                            ref key,
                            ref mut children_iter,
                        } => {
                            // We returned `node` already. Visit its next child.
                            let Some((pos, child_addr)) = children_iter.next() else {
                                // We visited all this node's descendants. Go back to its parent.
                                continue;
                            };

                            let child = merkle.get_node(child_addr)?;

                            let partial_path = match child.inner() {
                                NodeType::Branch(branch) => branch.path.iter().copied(),
                                NodeType::Leaf(leaf) => leaf.path.iter().copied(),
                                NodeType::Extension(_) => {
                                    unreachable!("extension nodes shouldn't exist")
                                }
                            };

                            // The child's key is its parent's key, followed by the child's index,
                            // followed by the child's partial path (if any).
                            let child_key: Key = key
                                .iter()
                                .copied()
                                .chain(once(pos))
                                .chain(partial_path)
                                .collect();

                            // There may be more children of this node to visit.
                            iter_stack.push(iter_node);

                            iter_stack.push(IterationNode::Unvisited {
                                key: child_key,
                                node: child,
                            });
                            return self.poll_next(_cx);
                        }
                    }
                }
                Poll::Ready(None)
            }
        }
    }
}

/// Returns the initial state for an iterator over the given `merkle` with root `root_node`
/// which starts at `key`.
fn get_iterator_intial_state<'a, S: ShaleStore<Node> + Send + Sync, T>(
    merkle: &'a Merkle<S, T>,
    root_node: DiskAddress,
    key: &[u8],
) -> Result<NodeStreamState<'a>, api::Error> {
    // Invariant: `node`'s key is a prefix of `key`.
    let mut node = merkle.get_node(root_node)?;

    // Invariant: `matched_key_nibbles` is the key of `node` at the start
    // of each loop iteration.
    let mut matched_key_nibbles = vec![];

    let mut unmatched_key_nibbles = Nibbles::<1>::new(key).into_iter();

    let mut iter_stack: Vec<IterationNode> = vec![];

    loop {
        // `next_unmatched_key_nibble` is the first nibble after `matched_key_nibbles`.
        let Some(next_unmatched_key_nibble) = unmatched_key_nibbles.next() else {
            // The invariant tells us `node` is a prefix of `key`.
            // There is no more `key` left so `node` must be at `key`.
            // Visit and return `node` first.
            match &node.inner {
                NodeType::Branch(_) | NodeType::Leaf(_) => {
                    iter_stack.push(IterationNode::Unvisited {
                        key: Box::from(matched_key_nibbles),
                        node,
                    });
                }
                NodeType::Extension(_) => {
                    unreachable!("extension nodes shouldn't exist")
                }
            }

            return Ok(NodeStreamState::Iterating { iter_stack });
        };

        match &node.inner {
            NodeType::Branch(branch) => {
                // The next nibble in `key` is `next_unmatched_key_nibble`,
                // so all children of `node` with a position > `next_unmatched_key_nibble`
                // should be visited since they are after `key`.
                iter_stack.push(IterationNode::Visited {
                    key: matched_key_nibbles.iter().copied().collect(),
                    children_iter: Box::new(
                        as_enumerated_children_iter(branch)
                            .filter(move |(pos, _)| *pos > next_unmatched_key_nibble),
                    ),
                });

                // Figure out if the child at `next_unmatched_key_nibble` is a prefix of `key`.
                // (i.e. if we should run this loop body again)
                #[allow(clippy::indexing_slicing)]
                let Some(child_addr) = branch.children[next_unmatched_key_nibble as usize] else {
                    // There is no child at `next_unmatched_key_nibble`.
                    // We'll visit `node`'s first child at index > `next_unmatched_key_nibble`
                    // first (if it exists).
                    return Ok(NodeStreamState::Iterating { iter_stack });
                };

                matched_key_nibbles.push(next_unmatched_key_nibble);

                let child = merkle.get_node(child_addr)?;

                let partial_key = match child.inner() {
                    NodeType::Branch(branch) => &branch.path,
                    NodeType::Leaf(leaf) => &leaf.path,
                    NodeType::Extension(_) => {
                        unreachable!("extension nodes shouldn't exist")
                    }
                };

                let (comparison, new_unmatched_key_nibbles) =
                    compare_partial_path(partial_key.iter(), unmatched_key_nibbles);
                unmatched_key_nibbles = new_unmatched_key_nibbles;

                match comparison {
                    Ordering::Less => {
                        // `child` is before `key`.
                        return Ok(NodeStreamState::Iterating { iter_stack });
                    }
                    Ordering::Equal => {
                        // `child` is a prefix of `key`.
                        matched_key_nibbles.extend(partial_key.iter().copied());
                        node = child;
                    }
                    Ordering::Greater => {
                        // `child` is after `key`.
                        let key = matched_key_nibbles
                            .iter()
                            .chain(partial_key.iter())
                            .copied()
                            .collect();
                        iter_stack.push(IterationNode::Unvisited { key, node: child });

                        return Ok(NodeStreamState::Iterating { iter_stack });
                    }
                }
            }
            NodeType::Leaf(leaf) => {
                if compare_partial_path(leaf.path.iter(), unmatched_key_nibbles).0
                    == Ordering::Greater
                {
                    // `child` is after `key`.
                    let key = matched_key_nibbles
                        .iter()
                        .chain(leaf.path.iter())
                        .copied()
                        .collect();
                    iter_stack.push(IterationNode::Unvisited { key, node });
                }
                return Ok(NodeStreamState::Iterating { iter_stack });
            }
            NodeType::Extension(_) => {
                unreachable!("extension nodes shouldn't exist")
            }
        };
    }
}

enum MerkleKeyValueStreamState<'a, S, T> {
    /// The iterator state is lazily initialized when poll_next is called
    /// for the first time. The iteration start key is stored here.
    Uninitialized(Key),
    /// The iterator works by iterating over the nodes in the merkle trie
    /// and returning the key-value pairs for nodes that have values.
    Initialized {
        node_iter: MerkleNodeStream<'a, S, T>,
    },
}

impl<'a, S, T> MerkleKeyValueStreamState<'a, S, T> {
    /// Returns a new iterator that will iterate over all the key-value pairs in `merkle`.
    fn new() -> Self {
        Self::Uninitialized(Box::new([]))
    }

    /// Returns a new iterator that will iterate over all the key-value pairs in `merkle`
    /// with keys greater than or equal to `key`.
    fn with_key(key: Key) -> Self {
        Self::Uninitialized(key)
    }
}

pub struct MerkleKeyValueStream<'a, S, T> {
    state: MerkleKeyValueStreamState<'a, S, T>,
    merkle_root: DiskAddress,
    merkle: &'a Merkle<S, T>,
}

impl<'a, S: ShaleStore<Node> + Send + Sync, T> FusedStream for MerkleKeyValueStream<'a, S, T> {
    fn is_terminated(&self) -> bool {
        matches!(&self.state, MerkleKeyValueStreamState::Initialized { node_iter } if node_iter.is_terminated())
    }
}

impl<'a, S, T> MerkleKeyValueStream<'a, S, T> {
    pub(super) fn new(merkle: &'a Merkle<S, T>, merkle_root: DiskAddress) -> Self {
        Self {
            state: MerkleKeyValueStreamState::new(),
            merkle_root,
            merkle,
        }
    }

    pub(super) fn from_key(merkle: &'a Merkle<S, T>, merkle_root: DiskAddress, key: Key) -> Self {
        Self {
            state: MerkleKeyValueStreamState::with_key(key),
            merkle_root,
            merkle,
        }
    }
}

impl<'a, S: ShaleStore<Node> + Send + Sync, T> Stream for MerkleKeyValueStream<'a, S, T> {
    type Item = Result<(Key, Value), api::Error>;

    fn poll_next(
        mut self: std::pin::Pin<&mut Self>,
        _cx: &mut std::task::Context<'_>,
    ) -> Poll<Option<Self::Item>> {
        // destructuring is necessary here because we need mutable access to `key_state`
        // at the same time as immutable access to `merkle`
        let Self {
            state,
            merkle_root,
            merkle,
        } = &mut *self;

        match state {
            MerkleKeyValueStreamState::Uninitialized(key) => {
                let iter = MerkleNodeStream::new(merkle, *merkle_root, key.clone());
                self.state = MerkleKeyValueStreamState::Initialized { node_iter: iter };
                self.poll_next(_cx)
            }
            MerkleKeyValueStreamState::Initialized { node_iter: iter } => {
                match iter.poll_next_unpin(_cx) {
                    Poll::Ready(node) => match node {
                        Some(Ok((key, node))) => match node.inner() {
                            NodeType::Branch(branch) => {
                                let Some(value) = branch.value.as_ref() else {
                                    // This node doesn't have a value to return.
                                    // Continue to the next node.
                                    return self.poll_next(_cx);
                                };

                                let value = value.to_vec();
                                Poll::Ready(Some(Ok((key, value))))
                            }
                            NodeType::Leaf(leaf) => {
                                let value = leaf.data.to_vec();
                                Poll::Ready(Some(Ok((key, value))))
                            }
                            NodeType::Extension(_) => {
                                unreachable!("extension nodes shouldn't exist")
                            }
                        },
                        Some(Err(e)) => Poll::Ready(Some(Err(e))),
                        None => Poll::Ready(None),
                    },
                    Poll::Pending => Poll::Pending,
                }
            }
        }
    }
}

enum PathIteratorState<'a> {
    Iterating {
        /// The key, as nibbles, of the node at `address`, without the
        /// node's partial path (if any) at the end.
        /// Invariant: If this node has a parent, the parent's key is a
        /// prefix of the key we're traversing to.
        /// Note the node at `address` may not have a key which is a
        /// prefix of the key we're traversing to.
        matched_key: Vec<u8>,
        unmatched_key: NibblesIterator<'a, 0>,
        address: DiskAddress,
    },
    Exhausted,
}

/// Iterates over all nodes on the path to a given key starting from the root.
/// All nodes are branch nodes except possibly the last, which may be a leaf.
/// If the key is in the trie, the last node is the one at the given key.
/// Otherwise, the last node proves the non-existence of the key.
/// Specifically, if during the traversal, we encounter:
/// * A branch node with no child at the index of the next nibble in the key,
///   then the branch node proves the non-existence of the key.
/// * A node (either branch or leaf) whose partial path doesn't match the
///   remaining unmatched key, the node proves the non-existence of the key.
/// Note that thi means that the last node's key isn't necessarily a prefix of
/// the key we're traversing to.
pub struct PathIterator<'a, 'b, S, T> {
    state: PathIteratorState<'b>,
    merkle: &'a Merkle<S, T>,
}

impl<'a, 'b, S: ShaleStore<Node> + Send + Sync, T> PathIterator<'a, 'b, S, T> {
    pub(super) fn new(
        merkle: &'a Merkle<S, T>,
        sentinel_node: NodeObjRef<'a>,
        key: &'b [u8],
    ) -> Self {
        let root = match sentinel_node.inner() {
            NodeType::Branch(branch) => match branch.children[0] {
                Some(root) => root,
                None => {
                    return Self {
                        state: PathIteratorState::Exhausted,
                        merkle,
                    }
                }
            },
            _ => unreachable!("sentinel node is not a branch"),
        };

        Self {
            merkle,
            state: PathIteratorState::Iterating {
                matched_key: vec![],
                unmatched_key: Nibbles::new(key).into_iter(),
                address: root,
            },
        }
    }
}

impl<'a, 'b, S: ShaleStore<Node> + Send + Sync, T> Iterator for PathIterator<'a, 'b, S, T> {
    type Item = Result<(Key, NodeObjRef<'a>), MerkleError>;

    fn next(&mut self) -> Option<Self::Item> {
        // destructuring is necessary here because we need mutable access to `state`
        // at the same time as immutable access to `merkle`.
        let Self { state, merkle } = &mut *self;

        match state {
            PathIteratorState::Exhausted => None,
            PathIteratorState::Iterating {
                matched_key,
                unmatched_key,
                address,
            } => {
                let node = match merkle.get_node(*address) {
                    Ok(node) => node,
                    Err(e) => return Some(Err(e)),
                };

                let partial_path = match node.inner() {
                    NodeType::Branch(branch) => &branch.path,
                    NodeType::Leaf(leaf) => &leaf.path,
                    _ => unreachable!("extension nodes shouldn't exist"),
                };

                let (comparison, unmatched_key) =
                    compare_partial_path(partial_path.iter(), unmatched_key);

                matched_key.extend(partial_path.iter());
                let node_key = matched_key.clone().into_boxed_slice();

                match comparison {
                    Ordering::Less | Ordering::Greater => {
                        self.state = PathIteratorState::Exhausted;
                        Some(Ok((node_key, node)))
                    }
                    Ordering::Equal => match node.inner() {
                        NodeType::Extension(_) => unreachable!(),
                        NodeType::Leaf(_) => {
                            self.state = PathIteratorState::Exhausted;
                            Some(Ok((node_key, node)))
                        }
                        NodeType::Branch(branch) => {
                            let Some(next_unmatched_key_nibble) = unmatched_key.next() else {
                                // There's no more key to match. We're done.
                                self.state = PathIteratorState::Exhausted;
                                return Some(Ok((node_key, node)));
                            };

                            #[allow(clippy::indexing_slicing)]
                            let Some(child) = branch.children[next_unmatched_key_nibble as usize] else {
                                // There's no child at the index of the next nibble in the key.
                                // The node we're traversing to isn't in the trie.
                                self.state = PathIteratorState::Exhausted;
                                return Some(Ok((node_key, node)));
                            };

                            matched_key.push(next_unmatched_key_nibble);

                            *address = child;

                            Some(Ok((node_key, node)))
                        }
                    },
                }
            }
        }
    }
}

/// Takes in an iterator over a node's partial path and an iterator over the
/// unmatched portion of a key.
/// The first returned element is:
/// * [Ordering::Less] if the node is before the key.
/// * [Ordering::Equal] if the node is a prefix of the key.
/// * [Ordering::Greater] if the node is after the key.
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

/// Returns an iterator that returns (`pos`,`child_addr`) for each non-empty child of `branch`,
/// where `pos` is the position of the child in `branch`'s children array.
fn as_enumerated_children_iter(branch: &BranchNode) -> impl Iterator<Item = (u8, DiskAddress)> {
    branch
        .children
        .into_iter()
        .enumerate()
        .filter_map(|(pos, child_addr)| child_addr.map(|child_addr| (pos as u8, child_addr)))
}

fn key_from_nibble_iter<Iter: Iterator<Item = u8>>(mut nibbles: Iter) -> Key {
    let mut data = Vec::with_capacity(nibbles.size_hint().0 / 2);

    while let (Some(hi), Some(lo)) = (nibbles.next(), nibbles.next()) {
        data.push((hi << 4) + lo);
    }

    data.into_boxed_slice()
}

#[cfg(test)]
use super::tests::create_test_merkle;

#[cfg(test)]
#[allow(clippy::indexing_slicing, clippy::unwrap_used)]
mod tests {
    use crate::{
        merkle::Bincode,
        shale::{cached::DynamicMem, compact::CompactSpace},
    };

    use super::*;
    use futures::StreamExt;
    use test_case::test_case;

    impl<S: ShaleStore<Node> + Send + Sync, T> Merkle<S, T> {
        pub(crate) fn node_iter(&self, root: DiskAddress) -> MerkleNodeStream<'_, S, T> {
            MerkleNodeStream::new(self, root, Box::new([]))
        }

        pub(crate) fn node_iter_from(
            &self,
            root: DiskAddress,
            key: Key,
        ) -> MerkleNodeStream<'_, S, T> {
            MerkleNodeStream::new(self, root, key)
        }
    }

    #[test_case(&[]; "empty key")]
    #[test_case(&[1]; "non-empty key")]
    #[tokio::test]
    async fn path_iterate_empty_merkle_empty_key(key: &[u8]) {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let sentinel_node = merkle.get_node(root).unwrap();
        let mut stream = merkle.path_iter(sentinel_node, key);
        assert!(stream.next().is_none());
    }

    #[test_case(&[]; "empty key")]
    #[test_case(&[13]; "prefix of singleton key")]
    #[test_case(&[13, 37]; "match singleton key")]
    #[test_case(&[13, 37,1]; "suffix of singleton key")]
    #[test_case(&[255]; "no key nibbles match singleton key")]
    #[tokio::test]
    async fn path_iterate_singleton_merkle(key: &[u8]) {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(vec![0x13, 0x37], vec![0x42], root).unwrap();

        let sentinel_node = merkle.get_node(root).unwrap();

        let mut stream = merkle.path_iter(sentinel_node, key);
        let (key, node) = match stream.next() {
            Some(Ok((key, node))) => (key, node),
            Some(Err(e)) => panic!("{:?}", e),
            None => panic!("unexpected end of iterator"),
        };

        assert_eq!(key, vec![0x01, 0x03, 0x03, 0x07].into_boxed_slice());
        assert_eq!(node.inner().as_leaf().unwrap().data, vec![0x42].into());

        assert!(stream.next().is_none());
    }

    #[test_case(&[0x00, 0x00, 0x00, 0xFF]; "leaf key")]
    #[test_case(&[0x00, 0x00, 0x00, 0xF3]; "leaf sibling key")]
    #[test_case(&[0x00, 0x00, 0x00, 0xFF, 0x01]; "past leaf key")]
    #[tokio::test]
    async fn path_iterate_non_singleton_merkle_seek_leaf(key: &[u8]) {
        let (merkle, root) = created_populated_merkle();

        let sentinel_node = merkle.get_node(root).unwrap();

        let mut stream = merkle.path_iter(sentinel_node, key);

        let (key, node) = match stream.next() {
            Some(Ok((key, node))) => (key, node),
            Some(Err(e)) => panic!("{:?}", e),
            None => panic!("unexpected end of iterator"),
        };
        assert_eq!(key, vec![0x00, 0x00].into_boxed_slice());
        assert!(node.inner().as_branch().unwrap().value.is_none());

        let (key, node) = match stream.next() {
            Some(Ok((key, node))) => (key, node),
            Some(Err(e)) => panic!("{:?}", e),
            None => panic!("unexpected end of iterator"),
        };
        assert_eq!(
            key,
            vec![0x00, 0x00, 0x00, 0x00, 0x00, 0x00].into_boxed_slice()
        );
        assert_eq!(
            node.inner().as_branch().unwrap().value,
            Some(vec![0x00, 0x00, 0x00].into()),
        );

        let (key, node) = match stream.next() {
            Some(Ok((key, node))) => (key, node),
            Some(Err(e)) => panic!("{:?}", e),
            None => panic!("unexpected end of iterator"),
        };
        assert_eq!(
            key,
            vec![0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0F, 0x0F].into_boxed_slice()
        );
        assert_eq!(
            node.inner().as_leaf().unwrap().data,
            vec![0x00, 0x00, 0x00, 0x0FF].into(),
        );

        assert!(stream.next().is_none());
    }

    #[tokio::test]
    async fn path_iterate_non_singleton_merkle_seek_branch() {
        let (merkle, root) = created_populated_merkle();

        let key = &[0x00, 0x00, 0x00];

        let sentinel_node = merkle.get_node(root).unwrap();
        let mut stream = merkle.path_iter(sentinel_node, key);

        let (key, node) = match stream.next() {
            Some(Ok((key, node))) => (key, node),
            Some(Err(e)) => panic!("{:?}", e),
            None => panic!("unexpected end of iterator"),
        };
        assert_eq!(key, vec![0x00, 0x00].into_boxed_slice());
        assert!(node.inner().as_branch().unwrap().value.is_none());

        let (key, node) = match stream.next() {
            Some(Ok((key, node))) => (key, node),
            Some(Err(e)) => panic!("{:?}", e),
            None => panic!("unexpected end of iterator"),
        };
        assert_eq!(
            key,
            vec![0x00, 0x00, 0x00, 0x00, 0x00, 0x00].into_boxed_slice()
        );
        assert_eq!(
            node.inner().as_branch().unwrap().value,
            Some(vec![0x00, 0x00, 0x00].into()),
        );

        assert!(stream.next().is_none());
    }

    #[tokio::test]
    async fn key_value_iterate_empty() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let stream = merkle.key_value_iter_from_key(root, b"x".to_vec().into_boxed_slice());
        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn node_iterate_empty() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let stream = merkle.node_iter(root);
        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn node_iterate_root_only() {
        let mut merkle = create_test_merkle();

        let root = merkle.init_root().unwrap();

        merkle.insert(vec![0x00], vec![0x00], root).unwrap();

        let mut stream = merkle.node_iter(root);

        let (key, node) = stream.next().await.unwrap().unwrap();

        assert_eq!(key, vec![0x00].into_boxed_slice());
        assert_eq!(node.inner().as_leaf().unwrap().data.to_vec(), vec![0x00]);

        check_stream_is_done(stream).await;
    }

    /// Returns a new [Merkle] with the following structure:
    ///     sentinel
    ///        | 0
    ///        00 <-- branch with no value
    ///     0/  D|   \F
    ///    00   0D0   F <-- leaf with no partial path
    ///  0/ \F
    ///  1   F
    ///
    /// Note the 0000 branch has no value and the F0F0
    /// The number next to each branch is the position of the child in the branch's children array.
    fn created_populated_merkle() -> (Merkle<CompactSpace<Node, DynamicMem>, Bincode>, DiskAddress)
    {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle
            .insert(vec![0x00, 0x00, 0x00], vec![0x00, 0x00, 0x00], root)
            .unwrap();
        merkle
            .insert(
                vec![0x00, 0x00, 0x00, 0x01],
                vec![0x00, 0x00, 0x00, 0x01],
                root,
            )
            .unwrap();
        merkle
            .insert(
                vec![0x00, 0x00, 0x00, 0xFF],
                vec![0x00, 0x00, 0x00, 0xFF],
                root,
            )
            .unwrap();
        merkle
            .insert(vec![0x00, 0xD0, 0xD0], vec![0x00, 0xD0, 0xD0], root)
            .unwrap();
        merkle
            .insert(vec![0x00, 0xFF], vec![0x00, 0xFF], root)
            .unwrap();
        (merkle, root)
    }

    #[tokio::test]
    async fn node_iterator_no_start_key() {
        let (merkle, root) = created_populated_merkle();

        let mut stream = merkle.node_iter(root);

        // Covers case of branch with no value
        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00].into_boxed_slice());
        let node = node.inner().as_branch().unwrap();
        assert!(node.value.is_none());
        assert_eq!(node.path.to_vec(), vec![0x00, 0x00]);

        // Covers case of branch with value
        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0x00, 0x00].into_boxed_slice());
        let node = node.inner().as_branch().unwrap();
        assert_eq!(node.value.clone().unwrap().to_vec(), vec![0x00, 0x00, 0x00]);
        assert_eq!(node.path.to_vec(), vec![0x00, 0x00, 0x00]);

        // Covers case of leaf with partial path
        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0x00, 0x00, 0x01].into_boxed_slice());
        let node = node.inner().as_leaf().unwrap();
        assert_eq!(node.clone().data.to_vec(), vec![0x00, 0x00, 0x00, 0x01]);
        assert_eq!(node.path.to_vec(), vec![0x01]);

        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0x00, 0x00, 0xFF].into_boxed_slice());
        let node = node.inner().as_leaf().unwrap();
        assert_eq!(node.clone().data.to_vec(), vec![0x00, 0x00, 0x00, 0xFF]);
        assert_eq!(node.path.to_vec(), vec![0x0F]);

        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xD0, 0xD0].into_boxed_slice());
        let node = node.inner().as_leaf().unwrap();
        assert_eq!(node.clone().data.to_vec(), vec![0x00, 0xD0, 0xD0]);
        assert_eq!(node.path.to_vec(), vec![0x00, 0x0D, 0x00]); // 0x0D00 becomes 0xDO

        // Covers case of leaf with no partial path
        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xFF].into_boxed_slice());
        let node = node.inner().as_leaf().unwrap();
        assert_eq!(node.clone().data.to_vec(), vec![0x00, 0xFF]);
        assert_eq!(node.path.to_vec(), vec![0x0F]);

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn node_iterator_start_key_between_nodes() {
        let (merkle, root) = created_populated_merkle();

        let mut stream = merkle.node_iter_from(root, vec![0x00, 0x00, 0x01].into_boxed_slice());

        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xD0, 0xD0].into_boxed_slice());
        assert_eq!(
            node.inner().as_leaf().unwrap().clone().data.to_vec(),
            vec![0x00, 0xD0, 0xD0]
        );

        // Covers case of leaf with no partial path
        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xFF].into_boxed_slice());
        assert_eq!(
            node.inner().as_leaf().unwrap().clone().data.to_vec(),
            vec![0x00, 0xFF]
        );

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn node_iterator_start_key_on_node() {
        let (merkle, root) = created_populated_merkle();

        let mut stream = merkle.node_iter_from(root, vec![0x00, 0xD0, 0xD0].into_boxed_slice());

        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xD0, 0xD0].into_boxed_slice());
        assert_eq!(
            node.inner().as_leaf().unwrap().clone().data.to_vec(),
            vec![0x00, 0xD0, 0xD0]
        );

        // Covers case of leaf with no partial path
        let (key, node) = stream.next().await.unwrap().unwrap();
        assert_eq!(key, vec![0x00, 0xFF].into_boxed_slice());
        assert_eq!(
            node.inner().as_leaf().unwrap().clone().data.to_vec(),
            vec![0x00, 0xFF]
        );

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn node_iterator_start_key_after_last_key() {
        let (merkle, root) = created_populated_merkle();

        let stream = merkle.node_iter_from(root, vec![0xFF].into_boxed_slice());

        check_stream_is_done(stream).await;
    }

    #[test_case(Some(&[u8::MIN]); "Starting at first key")]
    #[test_case(None; "No start specified")]
    #[test_case(Some(&[128u8]); "Starting in middle")]
    #[test_case(Some(&[u8::MAX]); "Starting at last key")]
    #[tokio::test]
    async fn key_value_iterate_many(start: Option<&[u8]>) {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        // insert all values from u8::MIN to u8::MAX, with the key and value the same
        for k in u8::MIN..=u8::MAX {
            merkle.insert([k], vec![k], root).unwrap();
        }

        let mut stream = match start {
            Some(start) => merkle.key_value_iter_from_key(root, start.to_vec().into_boxed_slice()),
            None => merkle.key_value_iter(root),
        };

        // we iterate twice because we should get a None then start over
        #[allow(clippy::indexing_slicing)]
        for k in start.map(|r| r[0]).unwrap_or_default()..=u8::MAX {
            let next = stream.next().await.map(|kv| {
                let (k, v) = kv.unwrap();
                assert_eq!(&*k, &*v);
                k
            });

            assert_eq!(next, Some(vec![k].into_boxed_slice()));
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_fused_empty() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        check_stream_is_done(merkle.key_value_iter(root)).await;
    }

    #[tokio::test]
    async fn key_value_table_test() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        // Insert key-values in reverse order to ensure iterator
        // doesn't just return the keys in insertion order.
        for i in (0..=u8::MAX).rev() {
            for j in (0..=u8::MAX).rev() {
                let key = vec![i, j];
                let value = vec![i, j];

                merkle.insert(key, value, root).unwrap();
            }
        }

        // Test with no start key
        let mut stream = merkle.key_value_iter(root);
        for i in 0..=u8::MAX {
            for j in 0..=u8::MAX {
                let expected_key = vec![i, j];
                let expected_value = vec![i, j];

                assert_eq!(
                    stream.next().await.unwrap().unwrap(),
                    (expected_key.into_boxed_slice(), expected_value),
                    "i: {}, j: {}",
                    i,
                    j,
                );
            }
        }
        check_stream_is_done(stream).await;

        // Test with start key
        for i in 0..=u8::MAX {
            let mut stream = merkle.key_value_iter_from_key(root, vec![i].into_boxed_slice());
            for j in 0..=u8::MAX {
                let expected_key = vec![i, j];
                let expected_value = vec![i, j];
                assert_eq!(
                    stream.next().await.unwrap().unwrap(),
                    (expected_key.into_boxed_slice(), expected_value),
                    "i: {}, j: {}",
                    i,
                    j,
                );
            }
            if i == u8::MAX {
                check_stream_is_done(stream).await;
            } else {
                assert_eq!(
                    stream.next().await.unwrap().unwrap(),
                    (vec![i + 1, 0].into_boxed_slice(), vec![i + 1, 0]),
                    "i: {}",
                    i,
                );
            }
        }
    }

    #[tokio::test]
    async fn key_value_fused_full() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let last = vec![0x00, 0x00, 0x00];

        let mut key_values = vec![vec![0x00], vec![0x00, 0x00], last.clone()];

        // branchs with paths (or extensions) will be present as well as leaves with siblings
        for kv in u8::MIN..=u8::MAX {
            let mut last = last.clone();
            last.push(kv);
            key_values.push(last);
        }

        for kv in key_values.iter() {
            merkle.insert(kv, kv.clone(), root).unwrap();
        }

        let mut stream = merkle.key_value_iter(root);

        for kv in key_values.iter() {
            let next = stream.next().await.unwrap().unwrap();
            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&next.1, kv);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_root_with_empty_data() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let key = vec![].into_boxed_slice();
        let value = vec![0x00];

        merkle.insert(&key, value.clone(), root).unwrap();

        let mut stream = merkle.key_value_iter(root);

        assert_eq!(stream.next().await.unwrap().unwrap(), (key, value));
    }

    #[tokio::test]
    async fn key_value_get_branch_and_leaf() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let first_leaf = &[0x00, 0x00];
        let second_leaf = &[0x00, 0x0f];
        let branch = &[0x00];

        merkle
            .insert(first_leaf, first_leaf.to_vec(), root)
            .unwrap();
        merkle
            .insert(second_leaf, second_leaf.to_vec(), root)
            .unwrap();

        merkle.insert(branch, branch.to_vec(), root).unwrap();

        let mut stream = merkle.key_value_iter(root);

        assert_eq!(
            stream.next().await.unwrap().unwrap(),
            (branch.to_vec().into_boxed_slice(), branch.to_vec())
        );

        assert_eq!(
            stream.next().await.unwrap().unwrap(),
            (first_leaf.to_vec().into_boxed_slice(), first_leaf.to_vec())
        );

        assert_eq!(
            stream.next().await.unwrap().unwrap(),
            (
                second_leaf.to_vec().into_boxed_slice(),
                second_leaf.to_vec()
            )
        );
    }

    #[tokio::test]
    async fn key_value_start_at_key_not_in_trie() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let first_key = 0x00;
        let intermediate = 0x80;

        assert!(first_key < intermediate);

        let key_values = vec![
            vec![first_key],
            vec![intermediate, intermediate],
            vec![intermediate, intermediate, intermediate],
        ];
        assert!(key_values[0] < key_values[1]);
        assert!(key_values[1] < key_values[2]);

        for key in key_values.iter() {
            merkle.insert(key, key.to_vec(), root).unwrap();
        }

        let mut stream =
            merkle.key_value_iter_from_key(root, vec![intermediate].into_boxed_slice());

        let first_expected = key_values[1].as_slice();
        let first = stream.next().await.unwrap().unwrap();

        assert_eq!(&*first.0, &*first.1);
        assert_eq!(first.1, first_expected);

        let second_expected = key_values[2].as_slice();
        let second = stream.next().await.unwrap().unwrap();

        assert_eq!(&*second.0, &*second.1);
        assert_eq!(second.1, second_expected);

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_on_branch_with_no_value() {
        let sibling_path = 0x00;
        let branch_path = 0x0f;
        let children = 0..=0x0f;

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        children.clone().for_each(|child_path| {
            let key = vec![sibling_path, child_path];

            merkle.insert(&key, key.clone(), root).unwrap();
        });

        let mut keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![branch_path, child_path];

                merkle.insert(&key, key.clone(), root).unwrap();

                key
            })
            .collect();

        keys.sort();

        let start = keys.iter().position(|key| key[0] == branch_path).unwrap();
        let keys = &keys[start..];

        let mut stream = merkle.key_value_iter_from_key(root, vec![branch_path].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_on_branch_with_value() {
        let sibling_path = 0x00;
        let branch_path = 0x0f;
        let branch_key = vec![branch_path];

        let children = (0..=0xf).map(|val| (val << 4) + val); // 0x00, 0x11, ... 0xff

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle
            .insert(&branch_key, branch_key.clone(), root)
            .unwrap();

        children.clone().for_each(|child_path| {
            let key = vec![sibling_path, child_path];

            merkle.insert(&key, key.clone(), root).unwrap();
        });

        let mut keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![branch_path, child_path];

                merkle.insert(&key, key.clone(), root).unwrap();

                key
            })
            .chain(Some(branch_key.clone()))
            .collect();

        keys.sort();

        let start = keys.iter().position(|key| key == &branch_key).unwrap();
        let keys = &keys[start..];

        let mut stream = merkle.key_value_iter_from_key(root, branch_key.into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_on_extension() {
        let missing = 0x0a;
        let children = (0..=0x0f).filter(|x| *x != missing);
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![child_path];

                merkle.insert(&key, key.clone(), root).unwrap();

                key
            })
            .collect();

        let keys = &keys[(missing as usize)..];

        let mut stream = merkle.key_value_iter_from_key(root, vec![missing].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_overlapping_with_extension_but_greater() {
        let start_key = 0x0a;
        let shared_path = 0x09;
        // 0x0900, 0x0901, ... 0x0a0f
        // path extension is 0x090
        let children = (0..=0x0f).map(|val| vec![shared_path, val]);

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        children.for_each(|key| {
            merkle.insert(&key, key.clone(), root).unwrap();
        });

        let stream = merkle.key_value_iter_from_key(root, vec![start_key].into_boxed_slice());

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_overlapping_with_extension_but_smaller() {
        let start_key = 0x00;
        let shared_path = 0x09;
        // 0x0900, 0x0901, ... 0x0a0f
        // path extension is 0x090
        let children = (0..=0x0f).map(|val| vec![shared_path, val]);

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let keys: Vec<_> = children
            .map(|key| {
                merkle.insert(&key, key.clone(), root).unwrap();
                key
            })
            .collect();

        let mut stream = merkle.key_value_iter_from_key(root, vec![start_key].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_between_siblings() {
        let missing = 0xaa;
        let children = (0..=0xf)
            .map(|val| (val << 4) + val) // 0x00, 0x11, ... 0xff
            .filter(|x| *x != missing);
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![child_path];

                merkle.insert(&key, key.clone(), root).unwrap();

                key
            })
            .collect();

        let keys = &keys[((missing >> 4) as usize)..];

        let mut stream = merkle.key_value_iter_from_key(root, vec![missing].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_greater_than_all_others_leaf() {
        let key = vec![0x00];
        let greater_key = vec![0xff];
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        merkle.insert(key.clone(), key, root).unwrap();
        let stream = merkle.key_value_iter_from_key(root, greater_key.into_boxed_slice());

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn key_value_start_at_key_greater_than_all_others_branch() {
        let greatest = 0xff;
        let children = (0..=0xf)
            .map(|val| (val << 4) + val) // 0x00, 0x11, ... 0xff
            .filter(|x| *x != greatest);
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let keys: Vec<_> = children
            .map(|child_path| {
                let key = vec![child_path];

                merkle.insert(&key, key.clone(), root).unwrap();

                key
            })
            .collect();

        let keys = &keys[((greatest >> 4) as usize)..];

        let mut stream = merkle.key_value_iter_from_key(root, vec![greatest].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    async fn check_stream_is_done<S>(mut stream: S)
    where
        S: FusedStream + Unpin,
    {
        assert!(stream.next().await.is_none());
        assert!(stream.is_terminated());
    }
}
