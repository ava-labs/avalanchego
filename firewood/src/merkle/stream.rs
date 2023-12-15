// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{node::Node, LeafNode, Merkle, MerkleError, NodeType, ObjRef};
use crate::{
    shale::{DiskAddress, ShaleStore},
    v2::api,
};
use futures::Stream;
use std::task::Poll;

pub(super) enum IteratorState<'a> {
    /// Start iterating at the beginning of the trie,
    /// returning the lowest key/value pair first
    StartAtBeginning,
    /// Start iterating at the specified key
    StartAtKey(Vec<u8>),
    /// Continue iterating after the given last_node and parents
    Iterating {
        last_node: ObjRef<'a>,
        parents: Vec<(ObjRef<'a>, u8)>,
    },
}
impl IteratorState<'_> {
    pub(super) fn new<K: AsRef<[u8]>>(starting: Option<K>) -> Self {
        match starting {
            None => Self::StartAtBeginning,
            Some(key) => Self::StartAtKey(key.as_ref().to_vec()),
        }
    }
}

// The default state is to start at the beginning
impl<'a> Default for IteratorState<'a> {
    fn default() -> Self {
        Self::StartAtBeginning
    }
}

/// A MerkleKeyValueStream iterates over keys/values for a merkle trie.
/// This iterator is not fused. If you read past the None value, you start
/// over at the beginning. If you need a fused iterator, consider using
/// std::iter::fuse
pub struct MerkleKeyValueStream<'a, S, T> {
    pub(super) key_state: IteratorState<'a>,
    pub(super) merkle_root: DiskAddress,
    pub(super) merkle: &'a Merkle<S, T>,
}

impl<'a, S: ShaleStore<Node> + Send + Sync, T> Stream for MerkleKeyValueStream<'a, S, T> {
    type Item = Result<(Vec<u8>, Vec<u8>), api::Error>;

    fn poll_next(
        mut self: std::pin::Pin<&mut Self>,
        _cx: &mut std::task::Context<'_>,
    ) -> Poll<Option<Self::Item>> {
        // Note that this sets the key_state to StartAtBeginning temporarily
        let found_key = match std::mem::take(&mut self.key_state) {
            IteratorState::StartAtBeginning => {
                let root_node = self
                    .merkle
                    .get_node(self.merkle_root)
                    .map_err(|e| api::Error::InternalError(Box::new(e)))?;
                let mut last_node = root_node;
                let mut parents = vec![];
                let leaf = loop {
                    match last_node.inner() {
                        NodeType::Branch(branch) => {
                            let Some((leftmost_position, leftmost_address)) = branch
                                .children
                                .iter()
                                .enumerate()
                                .filter_map(|(i, addr)| addr.map(|addr| (i, addr)))
                                .next()
                            else {
                                // we already exhausted the branch node. This happens with an empty trie
                                // ... or a corrupt one
                                return if parents.is_empty() {
                                    // empty trie
                                    Poll::Ready(None)
                                } else {
                                    // branch with NO children, not at the top
                                    Poll::Ready(Some(Err(api::Error::InternalError(Box::new(
                                        MerkleError::ParentLeafBranch,
                                    )))))
                                };
                            };

                            let next = self
                                .merkle
                                .get_node(leftmost_address)
                                .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                            parents.push((last_node, leftmost_position as u8));

                            last_node = next;
                        }
                        NodeType::Leaf(leaf) => break leaf,
                        NodeType::Extension(_) => todo!(),
                    }
                };

                // last_node should have a leaf; compute the key and value
                let current_key = key_from_parents_and_leaf(&parents, leaf);

                self.key_state = IteratorState::Iterating { last_node, parents };

                current_key
            }
            IteratorState::StartAtKey(key) => {
                // TODO: support finding the next key after K
                let root_node = self
                    .merkle
                    .get_node(self.merkle_root)
                    .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                let (found_node, parents) = self
                    .merkle
                    .get_node_and_parents_by_key(root_node, &key)
                    .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                let Some(last_node) = found_node else {
                    return Poll::Ready(None);
                };

                let returned_key_value = match last_node.inner() {
                    NodeType::Branch(branch) => (key, branch.value.to_owned().unwrap().to_vec()),
                    NodeType::Leaf(leaf) => (key, leaf.data.to_vec()),
                    NodeType::Extension(_) => todo!(),
                };

                self.key_state = IteratorState::Iterating { last_node, parents };

                return Poll::Ready(Some(Ok(returned_key_value)));
            }
            IteratorState::Iterating {
                last_node,
                mut parents,
            } => {
                match last_node.inner() {
                    NodeType::Branch(branch) => {
                        // previously rendered the value from a branch node, so walk down to the first available child
                        let Some((child_position, child_address)) = branch
                            .children
                            .iter()
                            .enumerate()
                            .filter_map(|(child_position, &addr)| {
                                addr.map(|addr| (child_position, addr))
                            })
                            .next()
                        else {
                            // Branch node with no children?
                            return Poll::Ready(Some(Err(api::Error::InternalError(Box::new(
                                MerkleError::ParentLeafBranch,
                            )))));
                        };

                        parents.push((last_node, child_position as u8)); // remember where we walked down from

                        let current_node = self
                            .merkle
                            .get_node(child_address)
                            .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                        let found_key = key_from_parents(&parents);

                        self.key_state = IteratorState::Iterating {
                            // continue iterating from here
                            last_node: current_node,
                            parents,
                        };

                        found_key
                    }
                    NodeType::Leaf(leaf) => {
                        let mut next = parents.pop().map(|(node, position)| (node, Some(position)));
                        loop {
                            match next {
                                None => return Poll::Ready(None),
                                Some((parent, child_position)) => {
                                    // Assume all parents are branch nodes
                                    let children = parent.inner().as_branch().unwrap().chd();

                                    // we use wrapping_add here because the value might be u8::MAX indicating that
                                    // we want to go down branch
                                    let start_position =
                                        child_position.map(|pos| pos + 1).unwrap_or_default();

                                    let Some((found_position, found_address)) = children
                                        .iter()
                                        .enumerate()
                                        .skip(start_position as usize)
                                        .filter_map(|(offset, addr)| {
                                            addr.map(|addr| (offset as u8, addr))
                                        })
                                        .next()
                                    else {
                                        next = parents
                                            .pop()
                                            .map(|(node, position)| (node, Some(position)));
                                        continue;
                                    };

                                    // we push (node, None) which will start at the beginning of the next branch node
                                    let child = self
                                        .merkle
                                        .get_node(found_address)
                                        .map(|node| (node, None))
                                        .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                                    // stop_descending if:
                                    //  - on a branch and it has a value; OR
                                    //  - on a leaf
                                    let stop_descending = match child.0.inner() {
                                        NodeType::Branch(branch) => branch.value.is_some(),
                                        NodeType::Leaf(_) => true,
                                        NodeType::Extension(_) => todo!(),
                                    };

                                    next = Some(child);

                                    parents.push((parent, found_position));

                                    if stop_descending {
                                        break;
                                    }
                                }
                            }
                        }
                        // recompute current_key
                        // TODO: Can we keep current_key updated as we walk the tree instead of building it from the top all the time?
                        let current_key = key_from_parents_and_leaf(&parents, leaf);

                        self.key_state = IteratorState::Iterating {
                            last_node: next.unwrap().0,
                            parents,
                        };

                        current_key
                    }

                    NodeType::Extension(_) => todo!(),
                }
            }
        };

        // figure out the value to return from the state
        // if we get here, we're sure to have something to return
        // TODO: It's possible to return a reference to the data since the last_node is
        // saved in the iterator
        let return_value = match &self.key_state {
            IteratorState::Iterating {
                last_node,
                parents: _,
            } => {
                let value = match last_node.inner() {
                    NodeType::Branch(branch) => branch.value.to_owned().unwrap().to_vec(),
                    NodeType::Leaf(leaf) => leaf.data.to_vec(),
                    NodeType::Extension(_) => todo!(),
                };

                (found_key, value)
            }
            _ => unreachable!(),
        };

        Poll::Ready(Some(Ok(return_value)))
    }
}

/// Compute a key from a set of parents
fn key_from_parents(parents: &[(ObjRef, u8)]) -> Vec<u8> {
    parents[1..]
        .chunks_exact(2)
        .map(|parents| (parents[0].1 << 4) + parents[1].1)
        .collect::<Vec<u8>>()
}
fn key_from_parents_and_leaf(parents: &[(ObjRef, u8)], leaf: &LeafNode) -> Vec<u8> {
    let mut iter = parents[1..]
        .iter()
        .map(|parent| parent.1)
        .chain(leaf.path.to_vec());
    let mut data = Vec::with_capacity(iter.size_hint().0);
    while let (Some(hi), Some(lo)) = (iter.next(), iter.next()) {
        data.push((hi << 4) + lo);
    }
    data
}

// CAUTION: only use with nibble iterators
trait IntoBytes: Iterator<Item = u8> {
    fn nibbles_into_bytes(&mut self) -> Vec<u8> {
        let mut data = Vec::with_capacity(self.size_hint().0 / 2);

        while let (Some(hi), Some(lo)) = (self.next(), self.next()) {
            data.push((hi << 4) + lo);
        }

        data
    }
}
impl<T: Iterator<Item = u8>> IntoBytes for T {}

#[cfg(test)]
use super::tests::create_test_merkle;

#[cfg(test)]
mod tests {
    use crate::nibbles::Nibbles;

    use super::*;
    use futures::StreamExt;
    use test_case::test_case;

    #[tokio::test]
    async fn iterate_empty() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let mut it = merkle.get_iter(Some(b"x"), root).unwrap();
        let next = it.next().await;
        assert!(next.is_none());
    }

    #[test_case(Some(&[u8::MIN]); "Starting at first key")]
    #[test_case(None; "No start specified")]
    #[test_case(Some(&[128u8]); "Starting in middle")]
    #[test_case(Some(&[u8::MAX]); "Starting at last key")]
    #[tokio::test]
    async fn iterate_many(start: Option<&[u8]>) {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        // insert all values from u8::MIN to u8::MAX, with the key and value the same
        for k in u8::MIN..=u8::MAX {
            merkle.insert([k], vec![k], root).unwrap();
        }

        let mut it = merkle.get_iter(start, root).unwrap();
        // we iterate twice because we should get a None then start over
        for k in start.map(|r| r[0]).unwrap_or_default()..=u8::MAX {
            let next = it.next().await.map(|kv| {
                let (k, v) = kv.unwrap();
                assert_eq!(k, v);
                k
            });

            assert_eq!(next, Some(vec![k]));
        }

        assert!(it.next().await.is_none());
    }

    #[ignore]
    #[tokio::test]
    async fn get_branch_and_leaf() {
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

        let mut stream = merkle.get_iter(None::<&[u8]>, root).unwrap();

        assert_eq!(
            stream.next().await.unwrap().unwrap(),
            (branch.to_vec(), branch.to_vec())
        );

        assert_eq!(
            stream.next().await.unwrap().unwrap(),
            (first_leaf.to_vec(), first_leaf.to_vec())
        );

        assert_eq!(
            stream.next().await.unwrap().unwrap(),
            (second_leaf.to_vec(), second_leaf.to_vec())
        );
    }

    #[ignore]
    #[tokio::test]
    async fn start_at_key_not_in_trie() {
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

        let mut stream = merkle.get_iter(Some([intermediate]), root).unwrap();

        let first_expected = key_values[1].as_slice();
        let first = stream.next().await.unwrap().unwrap();

        assert_eq!(first.0, first.1);
        assert_eq!(first.0, first_expected);

        let second_expected = key_values[2].as_slice();
        let second = stream.next().await.unwrap().unwrap();

        assert_eq!(second.0, second.1);
        assert_eq!(second.0, second_expected);

        let done = stream.next().await;

        assert!(done.is_none());
    }

    #[test]
    fn remaining_bytes() {
        let data = &[1];
        let nib: Nibbles<'_, 0> = Nibbles::<0>::new(data);
        let mut it = nib.into_iter();
        assert_eq!(it.nibbles_into_bytes(), data.to_vec());
    }

    #[test]
    fn remaining_bytes_off() {
        let data = &[1];
        let nib: Nibbles<'_, 0> = Nibbles::<0>::new(data);
        let mut it = nib.into_iter();
        it.next();
        assert_eq!(it.nibbles_into_bytes(), vec![]);
    }
}
