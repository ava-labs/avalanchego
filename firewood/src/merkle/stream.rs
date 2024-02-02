// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{node::Node, BranchNode, Merkle, NodeObjRef, NodeType};
use crate::{
    nibbles::Nibbles,
    shale::{DiskAddress, ShaleStore},
    v2::api,
};
use futures::{stream::FusedStream, Stream};
use helper_types::{Either, MustUse};
use std::task::Poll;

type Key = Box<[u8]>;
type Value = Vec<u8>;

enum IteratorState<'a> {
    /// Start iterating at the specified key
    StartAtKey(Key),
    /// Continue iterating after the last node in the `visited_node_path`
    Iterating {
        check_child_nibble: bool,
        visited_node_path: Vec<(NodeObjRef<'a>, u8)>,
    },
}

impl IteratorState<'_> {
    fn new() -> Self {
        Self::StartAtKey(vec![].into_boxed_slice())
    }

    fn with_key(key: Key) -> Self {
        Self::StartAtKey(key)
    }
}

/// A MerkleKeyValueStream iterates over keys/values for a merkle trie.
pub struct MerkleKeyValueStream<'a, S, T> {
    key_state: IteratorState<'a>,
    merkle_root: DiskAddress,
    merkle: &'a Merkle<S, T>,
}

impl<'a, S: ShaleStore<Node> + Send + Sync, T> FusedStream for MerkleKeyValueStream<'a, S, T> {
    fn is_terminated(&self) -> bool {
        matches!(&self.key_state, IteratorState::Iterating { visited_node_path, .. } if visited_node_path.is_empty())
    }
}

impl<'a, S, T> MerkleKeyValueStream<'a, S, T> {
    pub(super) fn new(merkle: &'a Merkle<S, T>, merkle_root: DiskAddress) -> Self {
        let key_state = IteratorState::new();

        Self {
            merkle,
            key_state,
            merkle_root,
        }
    }

    pub(super) fn from_key(merkle: &'a Merkle<S, T>, merkle_root: DiskAddress, key: Key) -> Self {
        let key_state = IteratorState::with_key(key);

        Self {
            merkle,
            key_state,
            merkle_root,
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
            key_state,
            merkle_root,
            merkle,
        } = &mut *self;

        match key_state {
            IteratorState::StartAtKey(key) => {
                let root_node = merkle
                    .get_node(*merkle_root)
                    .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                let mut check_child_nibble = false;

                // traverse the trie along each nibble until we find a node with a value
                // TODO: merkle.iter_by_key(key) will simplify this entire code-block.
                let (found_node, mut visited_node_path) = {
                    let mut visited_node_path = vec![];

                    let found_node = merkle
                        .get_node_by_key_with_callbacks(
                            root_node,
                            &key,
                            |node_addr, _| visited_node_path.push(node_addr),
                            |_, _| {},
                        )
                        .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                    let mut nibbles = Nibbles::<1>::new(key).into_iter();

                    let visited_node_path = visited_node_path
                        .into_iter()
                        .map(|node| merkle.get_node(node))
                        .map(|node_result| {
                            let nibbles = &mut nibbles;

                            node_result
                                .map(|node| match node.inner() {
                                    NodeType::Branch(branch) => {
                                        let mut partial_path_iter = branch.path.iter();
                                        let next_nibble = nibbles
                                            .map(|nibble| (Some(nibble), partial_path_iter.next()))
                                            .find(|(a, b)| a.as_ref() != *b);

                                        match next_nibble {
                                            // this case will be hit by all but the last nodes
                                            // unless there is a deviation between the key and the path
                                            None | Some((None, _)) => None,

                                            Some((Some(key_nibble), Some(path_nibble))) => {
                                                check_child_nibble = key_nibble < *path_nibble;
                                                None
                                            }

                                            // path is subset of the key
                                            Some((Some(nibble), None)) => {
                                                check_child_nibble = true;
                                                Some((node, nibble))
                                            }
                                        }
                                    }
                                    NodeType::Leaf(_) => Some((node, 0)),
                                    NodeType::Extension(_) => Some((node, 0)),
                                })
                                .transpose()
                        })
                        .take_while(|node| node.is_some())
                        .flatten()
                        .collect::<Result<Vec<_>, _>>()
                        .map_err(|e| api::Error::InternalError(Box::new(e)))?;

                    (found_node, visited_node_path)
                };

                if let Some(found_node) = found_node {
                    let value = match found_node.inner() {
                        NodeType::Branch(branch) => {
                            check_child_nibble = true;
                            branch.value.as_ref()
                        }
                        NodeType::Leaf(leaf) => Some(&leaf.data),
                        NodeType::Extension(_) => None,
                    };

                    let next_result = value.map(|value| {
                        let value = value.to_vec();

                        Ok((std::mem::take(key), value))
                    });

                    visited_node_path.push((found_node, 0));

                    self.key_state = IteratorState::Iterating {
                        check_child_nibble,
                        visited_node_path,
                    };

                    return Poll::Ready(next_result);
                }

                let found_key = nibble_iter_from_parents(&visited_node_path);
                let found_key = key_from_nibble_iter(found_key);

                if found_key > *key {
                    check_child_nibble = false;
                    visited_node_path.pop();
                }

                self.key_state = IteratorState::Iterating {
                    check_child_nibble,
                    visited_node_path,
                };

                self.poll_next(_cx)
            }

            IteratorState::Iterating {
                check_child_nibble,
                visited_node_path,
            } => {
                let next = find_next_result(merkle, visited_node_path, check_child_nibble)
                    .map_err(|e| api::Error::InternalError(Box::new(e)))
                    .transpose();

                Poll::Ready(next)
            }
        }
    }
}

enum NodeRef<'a> {
    New(NodeObjRef<'a>),
    Visited(NodeObjRef<'a>),
}

#[derive(Debug)]
enum InnerNode<'a> {
    New(&'a NodeType),
    Visited(&'a NodeType),
}

impl<'a> NodeRef<'a> {
    fn inner(&self) -> InnerNode<'_> {
        match self {
            Self::New(node) => InnerNode::New(node.inner()),
            Self::Visited(node) => InnerNode::Visited(node.inner()),
        }
    }

    fn into_node(self) -> NodeObjRef<'a> {
        match self {
            Self::New(node) => node,
            Self::Visited(node) => node,
        }
    }
}

fn find_next_result<'a, S: ShaleStore<Node>, T>(
    merkle: &'a Merkle<S, T>,
    visited_path: &mut Vec<(NodeObjRef<'a>, u8)>,
    check_child_nibble: &mut bool,
) -> Result<Option<(Key, Value)>, super::MerkleError> {
    let next = find_next_node_with_data(merkle, visited_path, *check_child_nibble)?.map(
        |(next_node, value)| {
            let partial_path = match next_node.inner() {
                NodeType::Leaf(leaf) => leaf.path.iter().copied(),
                NodeType::Extension(extension) => extension.path.iter().copied(),
                NodeType::Branch(branch) => branch.path.iter().copied(),
            };

            // always check the child for branch nodes with data
            *check_child_nibble = next_node.inner().is_branch();

            let key =
                key_from_nibble_iter(nibble_iter_from_parents(visited_path).chain(partial_path));

            visited_path.push((next_node, 0));

            (key, value)
        },
    );

    Ok(next)
}

fn find_next_node_with_data<'a, S: ShaleStore<Node>, T>(
    merkle: &'a Merkle<S, T>,
    visited_path: &mut Vec<(NodeObjRef<'a>, u8)>,
    check_child_nibble: bool,
) -> Result<Option<(NodeObjRef<'a>, Vec<u8>)>, super::MerkleError> {
    use InnerNode::*;

    let Some((visited_parent, visited_pos)) = visited_path.pop() else {
        return Ok(None);
    };

    let mut node = NodeRef::Visited(visited_parent);
    let mut pos = visited_pos;
    let mut first_loop = true;

    loop {
        match node.inner() {
            New(NodeType::Leaf(leaf)) => {
                let value = leaf.data.to_vec();
                return Ok(Some((node.into_node(), value)));
            }

            Visited(NodeType::Leaf(_)) | Visited(NodeType::Extension(_)) => {
                let Some((next_parent, next_pos)) = visited_path.pop() else {
                    return Ok(None);
                };

                node = NodeRef::Visited(next_parent);
                pos = next_pos;
            }

            New(NodeType::Extension(extension)) => {
                let child = merkle.get_node(extension.chd())?;

                pos = 0;
                visited_path.push((node.into_node(), pos));

                node = NodeRef::New(child);
            }

            Visited(NodeType::Branch(branch)) => {
                // if the first node that we check is a visited branch, that means that the branch had a value
                // and we need to visit the first child, for all other cases, we need to visit the next child
                let compare_op = if first_loop && check_child_nibble {
                    <u8 as PartialOrd>::ge // >=
                } else {
                    <u8 as PartialOrd>::gt
                };

                let children = get_children_iter(branch)
                    .filter(move |(_, child_pos)| compare_op(child_pos, &pos));

                let found_next_node =
                    next_node(merkle, children, visited_path, &mut node, &mut pos)?;

                if !found_next_node {
                    return Ok(None);
                }
            }

            New(NodeType::Branch(branch)) => {
                if let Some(value) = branch.value.as_ref() {
                    let value = value.to_vec();
                    return Ok(Some((node.into_node(), value)));
                }

                let children = get_children_iter(branch);

                let found_next_node =
                    next_node(merkle, children, visited_path, &mut node, &mut pos)?;

                if !found_next_node {
                    return Ok(None);
                }
            }
        }

        first_loop = false;
    }
}

fn get_children_iter(branch: &BranchNode) -> impl Iterator<Item = (DiskAddress, u8)> {
    branch
        .children
        .into_iter()
        .enumerate()
        .filter_map(|(pos, child_addr)| child_addr.map(|child_addr| (child_addr, pos as u8)))
}

/// This function is a little complicated because we need to be able to early return from the parent
/// when we return `false`. `MustUse` forces the caller to check the inner value of `Result::Ok`.
/// It also replaces `node`
fn next_node<'a, S, T, Iter>(
    merkle: &'a Merkle<S, T>,
    mut children: Iter,
    parents: &mut Vec<(NodeObjRef<'a>, u8)>,
    node: &mut NodeRef<'a>,
    pos: &mut u8,
) -> Result<MustUse<bool>, super::MerkleError>
where
    Iter: Iterator<Item = (DiskAddress, u8)>,
    S: ShaleStore<Node>,
{
    if let Some((child_addr, child_pos)) = children.next() {
        let child = merkle.get_node(child_addr)?;

        *pos = child_pos;
        let node = std::mem::replace(node, NodeRef::New(child));
        parents.push((node.into_node(), *pos));
    } else {
        let Some((next_parent, next_pos)) = parents.pop() else {
            return Ok(false.into());
        };

        *node = NodeRef::Visited(next_parent);
        *pos = next_pos;
    }

    Ok(true.into())
}

/// create an iterator over the key-nibbles from all parents _excluding_ the sentinal node.
fn nibble_iter_from_parents<'a>(parents: &'a [(NodeObjRef, u8)]) -> impl Iterator<Item = u8> + 'a {
    parents
        .iter()
        .skip(1) // always skip the sentinal node
        .flat_map(|(parent, child_nibble)| match parent.inner() {
            NodeType::Branch(branch) => Either::Left(
                branch
                    .path
                    .iter()
                    .copied()
                    .chain(std::iter::once(*child_nibble)),
            ),
            NodeType::Extension(extension) => Either::Right(extension.path.iter().copied()),
            NodeType::Leaf(leaf) => Either::Right(leaf.path.iter().copied()),
        })
}

fn key_from_nibble_iter<Iter: Iterator<Item = u8>>(mut nibbles: Iter) -> Key {
    let mut data = Vec::with_capacity(nibbles.size_hint().0 / 2);

    while let (Some(hi), Some(lo)) = (nibbles.next(), nibbles.next()) {
        data.push((hi << 4) + lo);
    }

    data.into_boxed_slice()
}

mod helper_types {
    use std::ops::Not;

    /// Enums enable stack-based dynamic-dispatch as opposed to heap-based `Box<dyn Trait>`.
    /// This helps us with match arms that return different types that implement the same trait.
    /// It's possible that [rust-lang/rust#63065](https://github.com/rust-lang/rust/issues/63065) will make this unnecessary.
    ///
    /// And this can be replaced by the `either` crate from crates.io if we ever need more functionality.
    pub(super) enum Either<T, U> {
        Left(T),
        Right(U),
    }

    impl<T, U> Iterator for Either<T, U>
    where
        T: Iterator,
        U: Iterator<Item = T::Item>,
    {
        type Item = T::Item;

        fn next(&mut self) -> Option<Self::Item> {
            match self {
                Self::Left(left) => left.next(),
                Self::Right(right) => right.next(),
            }
        }
    }

    #[must_use]
    pub(super) struct MustUse<T>(T);

    impl<T> From<T> for MustUse<T> {
        fn from(t: T) -> Self {
            Self(t)
        }
    }

    impl<T: Not> Not for MustUse<T> {
        type Output = T::Output;

        fn not(self) -> Self::Output {
            self.0.not()
        }
    }
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
#[allow(clippy::indexing_slicing, clippy::unwrap_used)]
mod tests {
    use crate::nibbles::Nibbles;

    use super::*;
    use futures::StreamExt;
    use test_case::test_case;

    #[tokio::test]
    async fn iterate_empty() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let stream = merkle.iter_from(root, b"x".to_vec().into_boxed_slice());
        check_stream_is_done(stream).await;
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

        let mut stream = match start {
            Some(start) => merkle.iter_from(root, start.to_vec().into_boxed_slice()),
            None => merkle.iter(root),
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
    async fn fused_empty() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        check_stream_is_done(merkle.iter(root)).await;
    }

    #[tokio::test]
    async fn fused_full() {
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

        let mut stream = merkle.iter(root);

        for kv in key_values.iter() {
            let next = stream.next().await.unwrap().unwrap();
            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&next.1, kv);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn root_with_empty_data() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let key = vec![].into_boxed_slice();
        let value = vec![0x00];

        merkle.insert(&key, value.clone(), root).unwrap();

        let mut stream = merkle.iter(root);

        assert_eq!(stream.next().await.unwrap().unwrap(), (key, value));
    }

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

        let mut stream = merkle.iter(root);

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

        let mut stream = merkle.iter_from(root, vec![intermediate].into_boxed_slice());

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
    async fn start_at_key_on_branch_with_no_value() {
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

        let mut stream = merkle.iter_from(root, vec![branch_path].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn start_at_key_on_branch_with_value() {
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

        let mut stream = merkle.iter_from(root, branch_key.into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn start_at_key_on_extension() {
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

        let mut stream = merkle.iter_from(root, vec![missing].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn start_at_key_overlapping_with_extension_but_greater() {
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

        let stream = merkle.iter_from(root, vec![start_key].into_boxed_slice());

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn start_at_key_overlapping_with_extension_but_smaller() {
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

        let mut stream = merkle.iter_from(root, vec![start_key].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn start_at_key_between_siblings() {
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

        let mut stream = merkle.iter_from(root, vec![missing].into_boxed_slice());

        for key in keys {
            let next = stream.next().await.unwrap().unwrap();

            assert_eq!(&*next.0, &*next.1);
            assert_eq!(&*next.0, key);
        }

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn start_at_key_greater_than_all_others_leaf() {
        let key = vec![0x00];
        let greater_key = vec![0xff];
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        merkle.insert(key.clone(), key, root).unwrap();
        let stream = merkle.iter_from(root, greater_key.into_boxed_slice());

        check_stream_is_done(stream).await;
    }

    #[tokio::test]
    async fn start_at_key_greater_than_all_others_branch() {
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

        let mut stream = merkle.iter_from(root, vec![greatest].into_boxed_slice());

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
