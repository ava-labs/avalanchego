// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::missing_errors_doc,
    reason = "Found 6 occurrences after enabling the lint."
)]
#![expect(
    clippy::missing_panics_doc,
    reason = "Found 1 occurrences after enabling the lint."
)]
#![expect(
    clippy::too_many_lines,
    reason = "Found 1 occurrences after enabling the lint."
)]

use crate::proof::{Proof, ProofError, ProofNode};
#[cfg(test)]
use crate::range_proof::RangeProof;
#[cfg(test)]
use crate::stream::MerkleKeyValueStream;
use crate::stream::PathIterator;
use crate::v2::api::FrozenProof;
#[cfg(test)]
use crate::v2::api::{self, FrozenRangeProof};
use firewood_storage::{
    BranchNode, Child, FileIoError, HashType, Hashable, HashedNodeReader, ImmutableProposal,
    IntoHashType, LeafNode, MaybePersistedNode, MutableProposal, NibblesIterator, Node, NodeStore,
    Path, ReadableStorage, SharedNode, TrieReader, ValueDigest,
};
#[cfg(test)]
use futures::{StreamExt, TryStreamExt};
use metrics::counter;
use std::collections::HashSet;
use std::fmt::{Debug, Write};
#[cfg(test)]
use std::future::ready;
use std::io::Error;
use std::iter::once;
#[cfg(test)]
use std::num::NonZeroUsize;
use std::sync::Arc;

/// Keys are boxed u8 slices
pub type Key = Box<[u8]>;

/// Values are vectors
/// TODO: change to Box<[u8]>
pub type Value = Vec<u8>;

// convert a set of nibbles into a printable string
// panics if there is a non-nibble byte in the set
#[cfg(not(feature = "branch_factor_256"))]
fn nibbles_formatter<X: IntoIterator<Item = u8>>(nib: X) -> String {
    nib.into_iter()
        .map(|c| {
            *b"0123456789abcdef"
                .get(c as usize)
                .expect("requires nibbles") as char
        })
        .collect::<String>()
}

#[cfg(feature = "branch_factor_256")]
fn nibbles_formatter<X: IntoIterator<Item = u8>>(nib: X) -> String {
    let collected: Box<[u8]> = nib.into_iter().collect();
    hex::encode(&collected)
}

macro_rules! write_attributes {
    ($writer:ident, $node:expr, $value:expr) => {
        if !$node.partial_path.0.is_empty() {
            write!(
                $writer,
                " pp={}",
                nibbles_formatter($node.partial_path.0.clone())
            )
            .map_err(|e| FileIoError::from_generic_no_file(e, "write attributes"))?;
        }
        if !$value.is_empty() {
            match std::str::from_utf8($value) {
                Ok(string) if string.chars().all(char::is_alphanumeric) => {
                    write!($writer, " val={}", string)
                        .map_err(|e| FileIoError::from_generic_no_file(e, "write attributes"))?;
                }
                _ => {
                    let hex = hex::encode($value);
                    if hex.len() > 6 {
                        write!($writer, " val={:.6}...", hex).map_err(|e| {
                            FileIoError::from_generic_no_file(e, "write attributes")
                        })?;
                    } else {
                        write!($writer, " val={}", hex).map_err(|e| {
                            FileIoError::from_generic_no_file(e, "write attributes")
                        })?;
                    }
                }
            }
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
            // 3. The key is below the node (i.e. its descendant)
            match node {
                Node::Leaf(_) => Ok(None),
                Node::Branch(node) => match node
                    .children
                    .get(child_index as usize)
                    .expect("index is in bounds")
                {
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

impl<T: TrieReader> Merkle<T> {
    pub(crate) fn root(&self) -> Option<SharedNode> {
        self.nodestore.root_node()
    }

    #[cfg(test)]
    pub(crate) const fn nodestore(&self) -> &T {
        &self.nodestore
    }

    /// Returns a proof that the given key has a certain value,
    /// or that the key isn't in the trie.
    pub fn prove(&self, key: &[u8]) -> Result<FrozenProof, ProofError> {
        let Some(root) = self.root() else {
            return Err(ProofError::Empty);
        };

        // Get the path to the key
        let path_iter = self.path_iter(key)?;
        let mut proof = Vec::new();
        for node in path_iter {
            let node = node?;
            proof.push(ProofNode::from(node));
        }

        if proof.is_empty() {
            // No nodes, even the root, are before `key`.
            // The root alone proves the non-existence of `key`.
            // TODO reduce duplicate code with ProofNode::from<PathIterItem>
            let mut child_hashes: [Option<HashType>; BranchNode::MAX_CHILDREN] =
                [const { None }; BranchNode::MAX_CHILDREN];
            if let Some(branch) = root.as_branch() {
                // TODO danlaine: can we avoid indexing?
                #[expect(clippy::indexing_slicing)]
                for (i, hash) in branch.children_hashes() {
                    child_hashes[i] = Some(hash.clone());
                }
            }

            proof.push(ProofNode {
                key: root.partial_path().bytes(),
                #[cfg(feature = "ethhash")]
                partial_len: root.partial_path().0.len(),
                value_digest: root
                    .value()
                    .map(|value| ValueDigest::Value(value.to_vec().into_boxed_slice())),
                child_hashes,
            });
        }

        Ok(Proof::new(proof.into_boxed_slice()))
    }

    /// Verify a proof that a key has a certain value, or that the key isn't in the trie.
    pub fn verify_range_proof<V: AsRef<[u8]>>(
        &self,
        _proof: &Proof<impl Hashable>,
        _first_key: &[u8],
        _last_key: &[u8],
        _keys: Vec<&[u8]>,
        _vals: Vec<V>,
    ) -> Result<bool, ProofError> {
        todo!()
    }

    pub(crate) fn path_iter<'a>(
        &self,
        key: &'a [u8],
    ) -> Result<PathIterator<'_, 'a, T>, FileIoError> {
        PathIterator::new(&self.nodestore, key)
    }

    #[cfg(test)]
    pub(super) fn key_value_iter(&self) -> MerkleKeyValueStream<'_, T> {
        MerkleKeyValueStream::from(&self.nodestore)
    }

    #[cfg(test)]
    pub(super) fn key_value_iter_from_key<K: AsRef<[u8]>>(
        &self,
        key: K,
    ) -> MerkleKeyValueStream<'_, T> {
        // TODO danlaine: change key to &[u8]
        MerkleKeyValueStream::from_key(&self.nodestore, key.as_ref())
    }

    #[cfg(test)]
    pub(super) async fn range_proof(
        &self,
        start_key: Option<&[u8]>,
        end_key: Option<&[u8]>,
        limit: Option<NonZeroUsize>,
    ) -> Result<FrozenRangeProof, api::Error> {
        if let (Some(k1), Some(k2)) = (&start_key, &end_key) {
            if k1 > k2 {
                return Err(api::Error::InvalidRange {
                    start_key: k1.to_vec().into(),
                    end_key: k2.to_vec().into(),
                });
            }
        }

        let mut stream = match start_key {
            // TODO: fix the call-site to force the caller to do the allocation
            Some(key) => self.key_value_iter_from_key(key.to_vec().into_boxed_slice()),
            None => self.key_value_iter(),
        };

        // fetch the first key from the stream
        let first_result = stream.next().await;

        // transpose the Option<Result<T, E>> to Result<Option<T>, E>
        // If this is an error, the ? operator will return it
        let Some((first_key, first_value)) = first_result.transpose()? else {
            // The trie is empty.
            if start_key.is_none() && end_key.is_none() {
                // The caller requested a range proof over an empty trie.
                return Err(api::Error::RangeProofOnEmptyTrie);
            }

            let start_proof = start_key
                .map(|start_key| self.prove(start_key))
                .transpose()?;

            let end_proof = end_key.map(|end_key| self.prove(end_key)).transpose()?;

            return Ok(RangeProof {
                start_proof,
                key_values: Box::new([]),
                end_proof,
            });
        };

        let start_proof = self.prove(&first_key)?;
        let limit = limit.map(|old_limit| old_limit.get().saturating_sub(1));

        let mut key_values = vec![(first_key, first_value.into_boxed_slice())];

        // we stop streaming if either we hit the limit or the key returned was larger
        // than the largest key requested
        key_values.extend(
            stream
                .take(limit.unwrap_or(usize::MAX))
                .take_while(|kv| {
                    // no last key asked for, so keep going
                    let Some(last_key) = end_key else {
                        return ready(true);
                    };

                    // return the error if there was one
                    let Ok(kv) = kv else {
                        return ready(true);
                    };

                    // keep going if the key returned is less than the last key requested
                    ready(&*kv.0 <= last_key)
                })
                .map(|kv| kv.map(|(k, v)| (k, v.into())))
                .try_collect::<Vec<(Box<[u8]>, Box<[u8]>)>>()
                .await?,
        );

        let end_proof = key_values
            .last()
            .map(|(largest_key, _)| self.prove(largest_key))
            .transpose()?;

        debug_assert!(end_proof.is_some());

        Ok(RangeProof {
            start_proof: Some(start_proof),
            key_values: key_values.into(),
            end_proof,
        })
    }

    pub(crate) fn get_value(&self, key: &[u8]) -> Result<Option<Box<[u8]>>, FileIoError> {
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
}

impl<T: HashedNodeReader> Merkle<T> {
    /// Dump a node, recursively, to a dot file
    pub(crate) fn dump_node(
        &self,
        node: &MaybePersistedNode,
        hash: Option<&HashType>,
        seen: &mut HashSet<String>,
        writer: &mut dyn Write,
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
                for (childidx, child) in b.children.iter().enumerate() {
                    let (child, child_hash) = match child {
                        None => continue,
                        Some(node) => (node.as_maybe_persisted_node(), node.hash()),
                    };

                    let inserted = seen.insert(format!("{child}"));
                    if inserted {
                        writeln!(writer, "  {node} -> {child}[label=\"{childidx}\"]")
                            .map_err(|e| FileIoError::from_generic_no_file(e, "write branch"))?;
                        self.dump_node(&child, child_hash, seen, writer)?;
                    } else {
                        // We have already seen this child, which shouldn't happen.
                        // Indicate this with a red edge.
                        writeln!(
                            writer,
                            "  {node} -> {child}[label=\"{childidx} (dup)\" color=red]"
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

    /// Dump the trie to a dot file.
    ///
    /// This function is primarily used in testing, but also has an API implementation
    ///
    /// Dot files can be rendered using `dot -Tpng -o output.png input.dot`
    /// or online at <https://dreampuf.github.io/GraphvizOnline>
    pub(crate) fn dump(&self) -> Result<String, Error> {
        let root = self.nodestore.root_as_maybe_persisted_node();

        let mut result = String::new();
        writeln!(result, "digraph Merkle {{\n  rankdir=LR;").map_err(Error::other)?;
        if let (Some(root), Some(root_hash)) = (root, self.nodestore.root_hash()) {
            writeln!(result, " root -> {root}")
                .map_err(Error::other)
                .map_err(|e| FileIoError::new(e, None, 0, None))
                .map_err(Error::other)?;
            let mut seen = HashSet::new();
            self.dump_node(
                &root,
                Some(&root_hash.into_hash_type()),
                &mut seen,
                &mut result,
            )
            .map_err(Error::other)?;
        }
        write!(result, "}}")
            .map_err(Error::other)
            .map_err(|e| FileIoError::new(e, None, 0, None))
            .map_err(Error::other)?;

        Ok(result)
    }
}

impl<S: ReadableStorage> TryFrom<Merkle<NodeStore<MutableProposal, S>>>
    for Merkle<NodeStore<Arc<ImmutableProposal>, S>>
{
    type Error = FileIoError;
    fn try_from(m: Merkle<NodeStore<MutableProposal, S>>) -> Result<Self, Self::Error> {
        Ok(Merkle {
            nodestore: m.nodestore.try_into()?,
        })
    }
}

impl<S: ReadableStorage> Merkle<NodeStore<MutableProposal, S>> {
    /// Convert a merkle backed by an `MutableProposal` into an `ImmutableProposal`
    ///
    /// This function is only used in benchmarks and tests
    #[must_use]
    pub fn hash(self) -> Merkle<NodeStore<Arc<ImmutableProposal>, S>> {
        self.try_into().expect("failed to convert")
    }

    /// Map `key` to `value` in the trie.
    /// Each element of key is 2 nibbles.
    pub fn insert(&mut self, key: &[u8], value: Box<[u8]>) -> Result<(), FileIoError> {
        let key = Path::from_nibbles_iterator(NibblesIterator::new(key));

        let root = self.nodestore.mut_root();

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
        *self.nodestore.mut_root() = root_node.into();
        Ok(())
    }

    /// Map `key` to `value` into the subtrie rooted at `node`.
    /// Each element of `key` is 1 nibble.
    /// Returns the new root of the subtrie.
    pub fn insert_helper(
        &mut self,
        mut node: Node,
        key: &[u8],
        value: Box<[u8]>,
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
                counter!("firewood.insert", "merkle" => "update").increment(1);
                Ok(node)
            }
            (None, Some((child_index, partial_path))) => {
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
                    children: [const { None }; BranchNode::MAX_CHILDREN],
                };

                // Shorten the node's partial path since it has a new parent.
                node.update_partial_path(partial_path);
                branch.update_child(child_index, Some(Child::Node(node)));
                counter!("firewood.insert", "merkle"=>"above").increment(1);

                Ok(Node::Branch(Box::new(branch)))
            }
            (Some((child_index, partial_path)), None) => {
                // 3. The key is below the node (i.e. its descendant)
                //    ...                         ...
                //     |                           |
                //    node         -->            node
                //     |                           |
                //    ... (key may be below)       ... (key is below)
                match node {
                    Node::Branch(ref mut branch) => {
                        #[expect(clippy::indexing_slicing)]
                        let child = match std::mem::take(&mut branch.children[child_index as usize])
                        {
                            None => {
                                // There is no child at this index.
                                // Create a new leaf and put it here.
                                let new_leaf = Node::Leaf(LeafNode {
                                    value,
                                    partial_path,
                                });
                                branch.update_child(child_index, Some(Child::Node(new_leaf)));
                                counter!("firewood.insert", "merkle"=>"below").increment(1);
                                return Ok(node);
                            }
                            Some(Child::Node(child)) => child,
                            Some(Child::AddressWithHash(addr, _)) => {
                                self.nodestore.read_for_update(addr.into())?
                            }
                            Some(Child::MaybePersisted(maybe_persisted, _)) => {
                                self.nodestore.read_for_update(maybe_persisted.clone())?
                            }
                        };

                        let child = self.insert_helper(child, partial_path.as_ref(), value)?;
                        branch.update_child(child_index, Some(Child::Node(child)));
                        Ok(node)
                    }
                    Node::Leaf(ref mut leaf) => {
                        // Turn this node into a branch node and put a new leaf as a child.
                        let mut branch = BranchNode {
                            partial_path: std::mem::replace(&mut leaf.partial_path, Path::new()),
                            value: Some(std::mem::take(&mut leaf.value)),
                            children: [const { None }; BranchNode::MAX_CHILDREN],
                        };

                        let new_leaf = Node::Leaf(LeafNode {
                            value,
                            partial_path,
                        });

                        branch.update_child(child_index, Some(Child::Node(new_leaf)));

                        counter!("firewood.insert", "merkle"=>"split").increment(1);
                        Ok(Node::Branch(Box::new(branch)))
                    }
                }
            }
            (Some((key_index, key_partial_path)), Some((node_index, node_partial_path))) => {
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
                    children: [const { None }; BranchNode::MAX_CHILDREN],
                };

                node.update_partial_path(node_partial_path);
                branch.update_child(node_index, Some(Child::Node(node)));

                let new_leaf = Node::Leaf(LeafNode {
                    value,
                    partial_path: key_partial_path,
                });
                branch.update_child(key_index, Some(Child::Node(new_leaf)));

                counter!("firewood.insert", "merkle" => "split").increment(1);
                Ok(Node::Branch(Box::new(branch)))
            }
        }
    }

    /// Removes the value associated with the given `key`.
    /// Returns the value that was removed, if any.
    /// Otherwise returns `None`.
    /// Each element of `key` is 2 nibbles.
    pub fn remove(&mut self, key: &[u8]) -> Result<Option<Box<[u8]>>, FileIoError> {
        let key = Path::from_nibbles_iterator(NibblesIterator::new(key));

        let root = self.nodestore.mut_root();
        let Some(root_node) = std::mem::take(root) else {
            // The trie is empty. There is nothing to remove.
            counter!("firewood.remove", "prefix" => "false", "result" => "nonexistent")
                .increment(1);
            return Ok(None);
        };

        let (root_node, removed_value) = self.remove_helper(root_node, &key)?;
        *self.nodestore.mut_root() = root_node;
        if removed_value.is_some() {
            counter!("firewood.remove", "prefix" => "false", "result" => "success").increment(1);
        } else {
            counter!("firewood.remove", "prefix" => "false", "result" => "nonexistent")
                .increment(1);
        }
        Ok(removed_value)
    }

    /// Removes the value associated with the given `key` from the subtrie rooted at `node`.
    /// Returns the new root of the subtrie and the value that was removed, if any.
    /// Each element of `key` is 1 nibble.
    #[expect(clippy::type_complexity)]
    fn remove_helper(
        &mut self,
        mut node: Node,
        key: &[u8],
    ) -> Result<(Option<Node>, Option<Box<[u8]>>), FileIoError> {
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
                match &mut node {
                    Node::Branch(branch) => {
                        let Some(removed_value) = branch.value.take() else {
                            // The branch has no value. Return the node as is.
                            return Ok((Some(node), None));
                        };

                        // This branch node has a value.
                        // If it has multiple children, return the node as is.
                        // Otherwise, its only child becomes the root of this subtrie.
                        let mut children_iter =
                            branch
                                .children
                                .iter_mut()
                                .enumerate()
                                .filter_map(|(index, child)| {
                                    child.as_mut().map(|child| (index, child))
                                });

                        let (child_index, child) = children_iter
                            .next()
                            .expect("branch node must have children");

                        if children_iter.next().is_some() {
                            // The branch has more than 1 child so it can't be removed.
                            Ok((Some(node), Some(removed_value)))
                        } else {
                            // The branch's only child becomes the root of this subtrie.
                            let mut child = match child {
                                Child::Node(child_node) => std::mem::take(child_node),
                                Child::AddressWithHash(addr, _) => {
                                    self.nodestore.read_for_update((*addr).into())?
                                }
                                Child::MaybePersisted(maybe_persisted, _) => {
                                    self.nodestore.read_for_update(maybe_persisted.clone())?
                                }
                            };

                            // The child's partial path is the concatenation of its (now removed) parent,
                            // its (former) child index, and its partial path.
                            match child {
                                Node::Branch(ref mut child_branch) => {
                                    let partial_path = Path::from_nibbles_iterator(
                                        branch
                                            .partial_path
                                            .iter()
                                            .copied()
                                            .chain(once(child_index as u8))
                                            .chain(child_branch.partial_path.iter().copied()),
                                    );
                                    child_branch.partial_path = partial_path;
                                }
                                Node::Leaf(ref mut leaf) => {
                                    let partial_path = Path::from_nibbles_iterator(
                                        branch
                                            .partial_path
                                            .iter()
                                            .copied()
                                            .chain(once(child_index as u8))
                                            .chain(leaf.partial_path.iter().copied()),
                                    );
                                    leaf.partial_path = partial_path;
                                }
                            }

                            let node_partial_path =
                                std::mem::replace(&mut branch.partial_path, Path::new());

                            let partial_path = Path::from_nibbles_iterator(
                                branch
                                    .partial_path
                                    .iter()
                                    .chain(once(&(child_index as u8)))
                                    .chain(node_partial_path.iter())
                                    .copied(),
                            );

                            node.update_partial_path(partial_path);

                            Ok((Some(child), Some(removed_value)))
                        }
                    }
                    Node::Leaf(leaf) => {
                        let removed_value = std::mem::take(&mut leaf.value);
                        Ok((None, Some(removed_value)))
                    }
                }
            }
            (Some((child_index, child_partial_path)), None) => {
                // 3. The key is below the node (i.e. its descendant)
                match node {
                    // we found a non-matching leaf node, so the value does not exist
                    Node::Leaf(_) => Ok((Some(node), None)),
                    Node::Branch(ref mut branch) => {
                        #[expect(clippy::indexing_slicing)]
                        let child = match std::mem::take(&mut branch.children[child_index as usize])
                        {
                            None => {
                                return Ok((Some(node), None));
                            }
                            Some(Child::Node(node)) => node,
                            Some(Child::AddressWithHash(addr, _)) => {
                                self.nodestore.read_for_update(addr.into())?
                            }
                            Some(Child::MaybePersisted(maybe_persisted, _)) => {
                                self.nodestore.read_for_update(maybe_persisted.clone())?
                            }
                        };

                        let (child, removed_value) =
                            self.remove_helper(child, child_partial_path.as_ref())?;

                        if let Some(child) = child {
                            branch.update_child(child_index, Some(Child::Node(child)));
                        } else {
                            branch.update_child(child_index, None);
                        }

                        let mut children_iter =
                            branch
                                .children
                                .iter_mut()
                                .enumerate()
                                .filter_map(|(index, child)| {
                                    child.as_mut().map(|child| (index, child))
                                });

                        let Some((child_index, child)) = children_iter.next() else {
                            // The branch has no children. Turn it into a leaf.
                            let leaf = Node::Leaf(LeafNode {
                                    value: branch.value.take().expect(
                                        "branch node must have a value if it previously had only 1 child",
                                    ),
                                    partial_path: branch.partial_path.clone(), // TODO remove clone
                                });
                            return Ok((Some(leaf), removed_value));
                        };

                        // if there is more than one child or the branch has a value, return it
                        if branch.value.is_some() || children_iter.next().is_some() {
                            return Ok((Some(node), removed_value));
                        }

                        // The branch has only 1 child. Remove the branch and return the child.
                        let mut child = match child {
                            Child::Node(child_node) => std::mem::replace(
                                child_node,
                                Node::Leaf(LeafNode {
                                    value: Box::default(),
                                    partial_path: Path::new(),
                                }),
                            ),
                            Child::AddressWithHash(addr, _) => {
                                self.nodestore.read_for_update((*addr).into())?
                            }
                            Child::MaybePersisted(maybe_persisted, _) => {
                                self.nodestore.read_for_update(maybe_persisted.clone())?
                            }
                        };

                        // The child's partial path is the concatenation of its (now removed) parent,
                        // its (former) child index, and its partial path.
                        let child_partial_path = Path::from_nibbles_iterator(
                            branch
                                .partial_path
                                .iter()
                                .chain(once(&(child_index as u8)))
                                .chain(child.partial_path().iter())
                                .copied(),
                        );
                        child.update_partial_path(child_partial_path);

                        Ok((Some(child), removed_value))
                    }
                }
            }
        }
    }

    /// Removes any key-value pairs with keys that have the given `prefix`.
    /// Returns the number of key-value pairs removed.
    pub fn remove_prefix(&mut self, prefix: &[u8]) -> Result<usize, FileIoError> {
        let prefix = Path::from_nibbles_iterator(NibblesIterator::new(prefix));

        let root = self.nodestore.mut_root();
        let Some(root_node) = std::mem::take(root) else {
            // The trie is empty. There is nothing to remove.
            counter!("firewood.remove", "prefix" => "true", "result" => "nonexistent").increment(1);
            return Ok(0);
        };

        let mut deleted = 0;
        let root_node = self.remove_prefix_helper(root_node, &prefix, &mut deleted)?;
        counter!("firewood.remove", "prefix" => "true", "result" => "success")
            .increment(deleted as u64);
        *self.nodestore.mut_root() = root_node;
        Ok(deleted)
    }

    fn remove_prefix_helper(
        &mut self,
        mut node: Node,
        key: &[u8],
        deleted: &mut usize,
    ) -> Result<Option<Node>, FileIoError> {
        // 4 possibilities for the position of the `key` relative to `node`:
        // 1. The node is at `key`, in which case we need to delete this node and all its children.
        // 2. The key is above the node (i.e. its ancestor), so the parent needs to be restructured (TODO).
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
                match &mut node {
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
                // 3. The key is below the node (i.e. its descendant)
                match node {
                    Node::Leaf(_) => Ok(Some(node)),
                    Node::Branch(ref mut branch) => {
                        #[expect(clippy::indexing_slicing)]
                        let child = match std::mem::take(&mut branch.children[child_index as usize])
                        {
                            None => {
                                return Ok(Some(node));
                            }
                            Some(Child::Node(node)) => node,
                            Some(Child::AddressWithHash(addr, _)) => {
                                self.nodestore.read_for_update(addr.into())?
                            }
                            Some(Child::MaybePersisted(maybe_persisted, _)) => {
                                self.nodestore.read_for_update(maybe_persisted.clone())?
                            }
                        };

                        let child =
                            self.remove_prefix_helper(child, child_partial_path.as_ref(), deleted)?;

                        if let Some(child) = child {
                            branch.update_child(child_index, Some(Child::Node(child)));
                        } else {
                            branch.update_child(child_index, None);
                        }

                        let mut children_iter =
                            branch
                                .children
                                .iter_mut()
                                .enumerate()
                                .filter_map(|(index, child)| {
                                    child.as_mut().map(|child| (index, child))
                                });

                        let Some((child_index, child)) = children_iter.next() else {
                            // The branch has no children. Turn it into a leaf.
                            let leaf = Node::Leaf(LeafNode {
                                    value: branch.value.take().expect(
                                        "branch node must have a value if it previously had only 1 child",
                                    ),
                                    partial_path: branch.partial_path.clone(), // TODO remove clone
                                });
                            return Ok(Some(leaf));
                        };

                        // if there is more than one child or the branch has a value, return it
                        if branch.value.is_some() || children_iter.next().is_some() {
                            return Ok(Some(node));
                        }

                        // The branch has only 1 child. Remove the branch and return the child.
                        let mut child = match child {
                            Child::Node(child_node) => std::mem::replace(
                                child_node,
                                Node::Leaf(LeafNode {
                                    value: Box::default(),
                                    partial_path: Path::new(),
                                }),
                            ),
                            Child::AddressWithHash(addr, _) => {
                                self.nodestore.read_for_update((*addr).into())?
                            }
                            Child::MaybePersisted(maybe_persisted, _) => {
                                self.nodestore.read_for_update(maybe_persisted.clone())?
                            }
                        };

                        // The child's partial path is the concatenation of its (now removed) parent,
                        // its (former) child index, and its partial path.
                        let child_partial_path = Path::from_nibbles_iterator(
                            branch
                                .partial_path
                                .iter()
                                .chain(once(&(child_index as u8)))
                                .chain(child.partial_path().iter())
                                .copied(),
                        );
                        child.update_partial_path(child_partial_path);

                        Ok(Some(child))
                    }
                }
            }
        }
    }

    /// Recursively deletes all children of a branch node.
    fn delete_children(
        &mut self,
        branch: &mut BranchNode,
        deleted: &mut usize,
    ) -> Result<(), FileIoError> {
        if branch.value.is_some() {
            // a KV pair was in the branch itself
            *deleted = deleted.saturating_add(1);
        }
        for children in &mut branch.children {
            // read the child node
            let child = match children {
                Some(Child::Node(node)) => node,
                Some(Child::AddressWithHash(addr, _)) => {
                    &mut self.nodestore.read_for_update((*addr).into())?
                }
                Some(Child::MaybePersisted(maybe_persisted, _)) => {
                    // For MaybePersisted, we need to get the node to update it
                    // We can't get a mutable reference from SharedNode, so we need to handle this differently
                    // For now, we'll skip this child since we can't modify it
                    let _shared_node = maybe_persisted.as_shared_node(&self.nodestore)?;
                    continue;
                }
                None => continue,
            };
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
}

/// Returns an iterator where each element is the result of combining
/// 2 nibbles of `nibbles`. If `nibbles` is odd length, panics in
/// debug mode and drops the final nibble in release mode.
pub fn nibbles_to_bytes_iter(nibbles: &[u8]) -> impl Iterator<Item = u8> {
    debug_assert_eq!(nibbles.len() & 1, 0);
    #[expect(clippy::indexing_slicing)]
    nibbles.chunks_exact(2).map(|p| (p[0] << 4) | p[1])
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

        let (shared, unique_a) = a.split_at(split_index);
        let unique_b = b.get(split_index..).expect("");

        Self {
            shared,
            unique_a,
            unique_b,
        }
    }
}

#[cfg(test)]
mod tests {
    #![expect(clippy::indexing_slicing, clippy::unwrap_used)]
    #![expect(
        clippy::items_after_statements,
        reason = "Found 1 occurrences after enabling the lint."
    )]

    use super::*;
    use firewood_storage::{MemStore, MutableProposal, NodeStore, RootReader, TrieHash};
    use rand::rngs::StdRng;
    use rand::{Rng, SeedableRng, rng};
    use test_case::test_case;

    // Returns n random key-value pairs.
    fn generate_random_kvs(seed: u64, n: usize) -> Vec<(Vec<u8>, Vec<u8>)> {
        eprintln!("Seed {seed}: to rerun with this data, export FIREWOOD_TEST_SEED={seed}");

        let mut rng = StdRng::seed_from_u64(seed);

        let mut kvs: Vec<(Vec<u8>, Vec<u8>)> = Vec::new();
        for _ in 0..n {
            let key_len = rng.random_range(1..=4096);
            let key: Vec<u8> = (0..key_len).map(|_| rng.random()).collect();

            let val_len = rng.random_range(1..=4096);
            let val: Vec<u8> = (0..val_len).map(|_| rng.random()).collect();

            kvs.push((key, val));
        }

        kvs
    }

    #[test]
    fn test_get_regression() {
        let mut merkle = create_in_memory_merkle();

        merkle.insert(&[0], Box::new([0])).unwrap();
        assert_eq!(merkle.get_value(&[0]).unwrap(), Some(Box::from([0])));

        merkle.insert(&[1], Box::new([1])).unwrap();
        assert_eq!(merkle.get_value(&[1]).unwrap(), Some(Box::from([1])));

        merkle.insert(&[2], Box::new([2])).unwrap();
        assert_eq!(merkle.get_value(&[2]).unwrap(), Some(Box::from([2])));

        let merkle = merkle.hash();

        assert_eq!(merkle.get_value(&[0]).unwrap(), Some(Box::from([0])));
        assert_eq!(merkle.get_value(&[1]).unwrap(), Some(Box::from([1])));
        assert_eq!(merkle.get_value(&[2]).unwrap(), Some(Box::from([2])));

        for result in merkle.path_iter(&[2]).unwrap() {
            result.unwrap();
        }
    }

    #[test]
    fn insert_one() {
        let mut merkle = create_in_memory_merkle();
        merkle.insert(b"abc", Box::new([])).unwrap();
    }

    fn create_in_memory_merkle() -> Merkle<NodeStore<MutableProposal, MemStore>> {
        let memstore = MemStore::new(vec![]);

        let nodestore = NodeStore::new_empty_proposal(memstore.into());

        Merkle { nodestore }
    }

    #[test]
    fn test_insert_and_get() {
        let mut merkle = create_in_memory_merkle();

        // insert values
        for key_val in u8::MIN..=u8::MAX {
            let key = vec![key_val];
            let val = Box::new([key_val]);

            merkle.insert(&key, val.clone()).unwrap();

            let fetched_val = merkle.get_value(&key).unwrap();

            // make sure the value was inserted
            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }

        // make sure none of the previous values were forgotten after initial insert
        for key_val in u8::MIN..=u8::MAX {
            let key = vec![key_val];
            let val = vec![key_val];

            let fetched_val = merkle.get_value(&key).unwrap();

            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }
    }

    #[test]
    fn remove_root() {
        let key0 = vec![0];
        let val0 = [0];
        let key1 = vec![0, 1];
        let val1 = [0, 1];
        let key2 = vec![0, 1, 2];
        let val2 = [0, 1, 2];
        let key3 = vec![0, 1, 15];
        let val3 = [0, 1, 15];

        let mut merkle = create_in_memory_merkle();

        merkle.insert(&key0, Box::from(val0)).unwrap();
        merkle.insert(&key1, Box::from(val1)).unwrap();
        merkle.insert(&key2, Box::from(val2)).unwrap();
        merkle.insert(&key3, Box::from(val3)).unwrap();
        // Trie is:
        //   key0
        //    |
        //   key1
        //  /    \
        // key2  key3

        // Test removal of root when it's a branch with 1 branch child
        let removed_val = merkle.remove(&key0).unwrap();
        assert_eq!(removed_val, Some(Box::from(val0)));
        assert!(merkle.get_value(&key0).unwrap().is_none());
        // Removing an already removed key is a no-op
        assert!(merkle.remove(&key0).unwrap().is_none());

        // Trie is:
        //   key1
        //  /    \
        // key2  key3
        // Test removal of root when it's a branch with multiple children
        assert_eq!(merkle.remove(&key1).unwrap(), Some(Box::from(val1)));
        assert!(merkle.get_value(&key1).unwrap().is_none());
        assert!(merkle.remove(&key1).unwrap().is_none());

        // Trie is:
        //   key1 (now has no value)
        //  /    \
        // key2  key3
        let removed_val = merkle.remove(&key2).unwrap();
        assert_eq!(removed_val, Some(Box::from(val2)));
        assert!(merkle.get_value(&key2).unwrap().is_none());
        assert!(merkle.remove(&key2).unwrap().is_none());

        // Trie is:
        // key3
        let removed_val = merkle.remove(&key3).unwrap();
        assert_eq!(removed_val, Some(Box::from(val3)));
        assert!(merkle.get_value(&key3).unwrap().is_none());
        assert!(merkle.remove(&key3).unwrap().is_none());

        assert!(merkle.nodestore.root_node().is_none());
    }

    #[test]
    fn remove_prefix_exact() {
        let mut merkle = two_byte_all_keys();
        for key_val in u8::MIN..=u8::MAX {
            let key = [key_val];
            let got = merkle.remove_prefix(&key).unwrap();
            assert_eq!(got, 1);
            let got = merkle.get_value(&key).unwrap();
            assert!(got.is_none());
        }
    }

    fn two_byte_all_keys() -> Merkle<NodeStore<MutableProposal, MemStore>> {
        let mut merkle = create_in_memory_merkle();
        for key_val in u8::MIN..=u8::MAX {
            let key = [key_val, key_val];
            let val = [key_val];

            merkle.insert(&key, Box::new(val)).unwrap();
            let got = merkle.get_value(&key).unwrap().unwrap();
            assert_eq!(&*got, val);
        }
        merkle
    }

    #[test]
    fn remove_prefix_all() {
        let mut merkle = two_byte_all_keys();
        let got = merkle.remove_prefix(&[]).unwrap();
        assert_eq!(got, 256);
    }

    #[test]
    fn remove_prefix_partial() {
        let mut merkle = create_in_memory_merkle();
        merkle
            .insert(b"abc", Box::from(b"value".as_slice()))
            .unwrap();
        merkle
            .insert(b"abd", Box::from(b"value".as_slice()))
            .unwrap();
        let got = merkle.remove_prefix(b"ab").unwrap();
        assert_eq!(got, 2);
    }

    #[test]
    fn remove_many() {
        let mut merkle = create_in_memory_merkle();

        // insert key-value pairs
        for key_val in u8::MIN..=u8::MAX {
            let key = [key_val];
            let val = [key_val];

            merkle.insert(&key, Box::new(val)).unwrap();
            let got = merkle.get_value(&key).unwrap().unwrap();
            assert_eq!(&*got, val);
        }

        // remove key-value pairs
        for key_val in u8::MIN..=u8::MAX {
            let key = [key_val];
            let val = [key_val];

            let got = merkle.remove(&key).unwrap().unwrap();
            assert_eq!(&*got, val);

            // Removing an already removed key is a no-op
            assert!(merkle.remove(&key).unwrap().is_none());

            let got = merkle.get_value(&key).unwrap();
            assert!(got.is_none());
        }
        assert!(merkle.nodestore.root_node().is_none());
    }

    #[test]
    fn remove_prefix() {
        let mut merkle = create_in_memory_merkle();

        // insert key-value pairs
        for key_val in u8::MIN..=u8::MAX {
            let key = [key_val, key_val];
            let val = [key_val];

            merkle.insert(&key, Box::new(val)).unwrap();
            let got = merkle.get_value(&key).unwrap().unwrap();
            assert_eq!(&*got, val);
        }

        // remove key-value pairs with prefix [0]
        let prefix = [0];
        assert_eq!(merkle.remove_prefix(&[0]).unwrap(), 1);

        // make sure all keys with prefix [0] were removed
        for key_val in u8::MIN..=u8::MAX {
            let key = [key_val, key_val];
            let got = merkle.get_value(&key).unwrap();
            if key[0] == prefix[0] {
                assert!(got.is_none());
            } else {
                assert!(got.is_some());
            }
        }
    }

    #[test]
    fn get_empty_proof() {
        let merkle = create_in_memory_merkle().hash();
        let proof = merkle.prove(b"any-key");
        assert!(matches!(proof.unwrap_err(), ProofError::Empty));
    }

    #[test]
    fn single_key_proof() {
        let mut merkle = create_in_memory_merkle();

        let seed = std::env::var("FIREWOOD_TEST_SEED")
            .ok()
            .map_or_else(
                || None,
                |s| Some(str::parse(&s).expect("couldn't parse FIREWOOD_TEST_SEED; must be a u64")),
            )
            .unwrap_or_else(|| rng().random());

        const TEST_SIZE: usize = 1;

        let kvs = generate_random_kvs(seed, TEST_SIZE);

        for (key, val) in &kvs {
            merkle.insert(key, val.clone().into_boxed_slice()).unwrap();
        }

        let merkle = merkle.hash();

        let root_hash = merkle.nodestore.root_hash().unwrap();

        for (key, value) in kvs {
            let proof = merkle.prove(&key).unwrap();

            proof
                .verify(key.clone(), Some(value.clone()), &root_hash)
                .unwrap();

            {
                // Test that the proof is invalid when the value is different
                let mut value = value.clone();
                value[0] = value[0].wrapping_add(1);
                assert!(proof.verify(key.clone(), Some(value), &root_hash).is_err());
            }

            {
                // Test that the proof is invalid when the hash is different
                assert!(
                    proof
                        .verify(key, Some(value), &TrieHash::default())
                        .is_err()
                );
            }
        }
    }

    #[tokio::test]
    async fn empty_range_proof() {
        let merkle = create_in_memory_merkle();

        assert!(matches!(
            merkle.range_proof(None, None, None).await.unwrap_err(),
            api::Error::RangeProofOnEmptyTrie
        ));
    }

    //     #[tokio::test]
    //     async fn range_proof_invalid_bounds() {
    //         let merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();
    //         let start_key = &[0x01];
    //         let end_key = &[0x00];

    //         match merkle
    //             .range_proof::<&[u8]>(root_addr, Some(start_key), Some(end_key), Some(1))
    //             .await
    //         {
    //             Err(api::Error::InvalidRange {
    //                 first_key,
    //                 last_key,
    //             }) if first_key == start_key && last_key == end_key => (),
    //             Err(api::Error::InvalidRange { .. }) => panic!("wrong bounds on InvalidRange error"),
    //             _ => panic!("expected InvalidRange error"),
    //         }
    //     }

    //     #[tokio::test]
    //     async fn full_range_proof() {
    //         let mut merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();
    //         // insert values
    //         for key_val in u8::MIN..=u8::MAX {
    //             let key = &[key_val];
    //             let val = &[key_val];

    //             merkle.insert(key, val.to_vec(), root_addr).unwrap();
    //         }
    //         merkle.flush_dirty();

    //         let rangeproof = merkle
    //             .range_proof::<&[u8]>(root_addr, None, None, None)
    //             .await
    //             .unwrap()
    //             .unwrap();
    //         assert_eq!(rangeproof.middle.len(), u8::MAX as usize + 1);
    //         assert_ne!(rangeproof.first_key_proof.0, rangeproof.last_key_proof.0);
    //         let left_proof = merkle.prove([u8::MIN], root_addr).unwrap();
    //         let right_proof = merkle.prove([u8::MAX], root_addr).unwrap();
    //         assert_eq!(rangeproof.first_key_proof.0, left_proof.0);
    //         assert_eq!(rangeproof.last_key_proof.0, right_proof.0);
    //     }

    //     #[tokio::test]
    //     async fn single_value_range_proof() {
    //         const RANDOM_KEY: u8 = 42;

    //         let mut merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();
    //         // insert values
    //         for key_val in u8::MIN..=u8::MAX {
    //             let key = &[key_val];
    //             let val = &[key_val];

    //             merkle.insert(key, val.to_vec(), root_addr).unwrap();
    //         }
    //         merkle.flush_dirty();

    //         let rangeproof = merkle
    //             .range_proof(root_addr, Some([RANDOM_KEY]), None, Some(1))
    //             .await
    //             .unwrap()
    //             .unwrap();
    //         assert_eq!(rangeproof.first_key_proof.0, rangeproof.last_key_proof.0);
    //         assert_eq!(rangeproof.middle.len(), 1);
    //     }

    //     #[test]
    //     fn shared_path_proof() {
    //         let mut merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();

    //         let key1 = b"key1";
    //         let value1 = b"1";
    //         merkle.insert(key1, value1.to_vec(), root_addr).unwrap();

    //         let key2 = b"key2";
    //         let value2 = b"2";
    //         merkle.insert(key2, value2.to_vec(), root_addr).unwrap();

    //         let root_hash = merkle.root_hash(root_addr).unwrap();

    //         let verified = {
    //             let key = key1;
    //             let proof = merkle.prove(key, root_addr).unwrap();
    //             proof.verify(key, root_hash.0).unwrap()
    //         };

    //         assert_eq!(verified, Some(value1.to_vec()));

    //         let verified = {
    //             let key = key2;
    //             let proof = merkle.prove(key, root_addr).unwrap();
    //             proof.verify(key, root_hash.0).unwrap()
    //         };

    //         assert_eq!(verified, Some(value2.to_vec()));
    //     }

    //     // this was a specific failing case
    //     #[test]
    //     fn shared_path_on_insert() {
    //         type Bytes = &'static [u8];
    //         let pairs: Vec<(Bytes, Bytes)> = vec![
    //             (
    //                 &[1, 1, 46, 82, 67, 218],
    //                 &[23, 252, 128, 144, 235, 202, 124, 243],
    //             ),
    //             (
    //                 &[1, 0, 0, 1, 1, 0, 63, 80],
    //                 &[99, 82, 31, 213, 180, 196, 49, 242],
    //             ),
    //             (
    //                 &[0, 0, 0, 169, 176, 15],
    //                 &[105, 211, 176, 51, 231, 182, 74, 207],
    //             ),
    //             (
    //                 &[1, 0, 0, 0, 53, 57, 93],
    //                 &[234, 139, 214, 220, 172, 38, 168, 164],
    //             ),
    //         ];

    //         let mut merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();

    //         for (key, val) in &pairs {
    //             let val = val.to_vec();
    //             merkle.insert(key, val.clone(), root_addr).unwrap();

    //             let fetched_val = merkle.get(key, root_addr).unwrap();

    //             // make sure the value was inserted
    //             assert_eq!(fetched_val.as_deref(), val.as_slice().into());
    //         }

    //         for (key, val) in pairs {
    //             let fetched_val = merkle.get(key, root_addr).unwrap();

    //             // make sure the value was inserted
    //             assert_eq!(fetched_val.as_deref(), val.into());
    //         }
    //     }

    //     #[test]
    //     fn overwrite_leaf() {
    //         let key = vec![0x00];
    //         let val = vec![1];
    //         let overwrite = vec![2];

    //         let mut merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();

    //         merkle.insert(&key, val.clone(), root_addr).unwrap();

    //         assert_eq!(
    //             merkle.get(&key, root_addr).unwrap().as_deref(),
    //             Some(val.as_slice())
    //         );

    //         merkle
    //             .insert(&key, overwrite.clone(), root_addr)
    //             .unwrap();

    //         assert_eq!(
    //             merkle.get(&key, root_addr).unwrap().as_deref(),
    //             Some(overwrite.as_slice())
    //         );
    //     }

    #[test]
    fn test_insert_leaf_suffix() {
        // key_2 is a suffix of key, which is a leaf
        let key = vec![0xff];
        let val = [1];
        let key_2 = vec![0xff, 0x00];
        let val_2 = [2];

        let mut merkle = create_in_memory_merkle();

        merkle.insert(&key, Box::new(val)).unwrap();
        merkle.insert(&key_2, Box::new(val_2)).unwrap();

        let got = merkle.get_value(&key).unwrap().unwrap();

        assert_eq!(*got, val);

        let got = merkle.get_value(&key_2).unwrap().unwrap();
        assert_eq!(*got, val_2);
    }

    #[test]
    fn test_insert_leaf_prefix() {
        // key_2 is a prefix of key, which is a leaf
        let key = vec![0xff, 0x00];
        let val = [1];
        let key_2 = vec![0xff];
        let val_2 = [2];

        let mut merkle = create_in_memory_merkle();

        merkle.insert(&key, Box::new(val)).unwrap();
        merkle.insert(&key_2, Box::new(val_2)).unwrap();

        let got = merkle.get_value(&key).unwrap().unwrap();
        assert_eq!(*got, val);

        let got = merkle.get_value(&key_2).unwrap().unwrap();
        assert_eq!(*got, val_2);
    }

    #[test]
    fn test_insert_sibling_leaf() {
        // The node at key is a branch node with children key_2 and key_3.
        // TODO assert in this test that key is the parent of key_2 and key_3.
        // i.e. the node types are branch, leaf, leaf respectively.
        let key = vec![0xff];
        let val = [1];
        let key_2 = vec![0xff, 0x00];
        let val_2 = [2];
        let key_3 = vec![0xff, 0x0f];
        let val_3 = [3];

        let mut merkle = create_in_memory_merkle();

        merkle.insert(&key, Box::new(val)).unwrap();
        merkle.insert(&key_2, Box::new(val_2)).unwrap();
        merkle.insert(&key_3, Box::new(val_3)).unwrap();

        let got = merkle.get_value(&key).unwrap().unwrap();
        assert_eq!(*got, val);

        let got = merkle.get_value(&key_2).unwrap().unwrap();
        assert_eq!(*got, val_2);

        let got = merkle.get_value(&key_3).unwrap().unwrap();
        assert_eq!(*got, val_3);
    }

    #[test]
    fn test_insert_branch_as_branch_parent() {
        let key = vec![0xff, 0xf0];
        let val = [1];
        let key_2 = vec![0xff, 0xf0, 0x00];
        let val_2 = [2];
        let key_3 = vec![0xff];
        let val_3 = [3];

        let mut merkle = create_in_memory_merkle();

        merkle.insert(&key, Box::new(val)).unwrap();
        // key is a leaf

        merkle.insert(&key_2, Box::new(val_2)).unwrap();
        // key is branch with child key_2

        merkle.insert(&key_3, Box::new(val_3)).unwrap();
        // key_3 is a branch with child key
        // key is a branch with child key_3

        let got = merkle.get_value(&key).unwrap().unwrap();
        assert_eq!(&*got, val);

        let got = merkle.get_value(&key_2).unwrap().unwrap();
        assert_eq!(&*got, val_2);

        let got = merkle.get_value(&key_3).unwrap().unwrap();
        assert_eq!(&*got, val_3);
    }

    #[test]
    fn test_insert_overwrite_branch_value() {
        let key = vec![0xff];
        let val = [1];
        let key_2 = vec![0xff, 0x00];
        let val_2 = [2];
        let overwrite = [3];

        let mut merkle = create_in_memory_merkle();

        merkle.insert(&key, Box::new(val)).unwrap();
        merkle.insert(&key_2, Box::new(val_2)).unwrap();

        let got = merkle.get_value(&key).unwrap().unwrap();
        assert_eq!(*got, val);

        let got = merkle.get_value(&key_2).unwrap().unwrap();
        assert_eq!(*got, val_2);

        merkle.insert(&key, Box::new(overwrite)).unwrap();

        let got = merkle.get_value(&key).unwrap().unwrap();
        assert_eq!(*got, overwrite);

        let got = merkle.get_value(&key_2).unwrap().unwrap();
        assert_eq!(*got, val_2);
    }

    #[test]
    fn test_delete_one_child_with_branch_value() {
        let mut merkle = create_in_memory_merkle();
        // insert a parent with a value
        merkle.insert(&[0], Box::new([42u8])).unwrap();
        // insert child1 with a value
        merkle.insert(&[0, 1], Box::new([43u8])).unwrap();
        // insert child2 with a value
        merkle.insert(&[0, 2], Box::new([44u8])).unwrap();

        // now delete one of the children
        let deleted = merkle.remove(&[0, 1]).unwrap();
        assert_eq!(deleted, Some([43u8].to_vec().into_boxed_slice()));

        // make sure the parent still has the correct value
        let got = merkle.get_value(&[0]).unwrap().unwrap();
        assert_eq!(*got, [42u8]);

        // and check the remaining child
        let other_child = merkle.get_value(&[0, 2]).unwrap().unwrap();
        assert_eq!(*other_child, [44u8]);
    }

    //     #[test]
    //     fn single_key_proof_with_one_node() {
    //         let mut merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();
    //         let key = b"key";
    //         let value = b"value";

    //         merkle.insert(key, value.to_vec(), root_addr).unwrap();
    //         let root_hash = merkle.root_hash(root_addr).unwrap();

    //         let proof = merkle.prove(key, root_addr).unwrap();

    //         let verified = proof.verify(key, root_hash.0).unwrap();

    //         assert_eq!(verified, Some(value.to_vec()));
    //     }

    //     #[test]
    //     fn two_key_proof_without_shared_path() {
    //         let mut merkle = create_in_memory_merkle();
    //         let root_addr = merkle.init_sentinel().unwrap();

    //         let key1 = &[0x00];
    //         let key2 = &[0xff];

    //         merkle.insert(key1, key1.to_vec(), root_addr).unwrap();
    //         merkle.insert(key2, key2.to_vec(), root_addr).unwrap();

    //         let root_hash = merkle.root_hash(root_addr).unwrap();

    //         let verified = {
    //             let proof = merkle.prove(key1, root_addr).unwrap();
    //             proof.verify(key1, root_hash.0).unwrap()
    //         };

    //         assert_eq!(verified.as_deref(), Some(key1.as_slice()));
    //     }

    fn merkle_build_test<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        items: Vec<(K, V)>,
    ) -> Result<Merkle<NodeStore<MutableProposal, MemStore>>, FileIoError> {
        let nodestore = NodeStore::new_empty_proposal(MemStore::new(vec![]).into());
        let mut merkle = Merkle::from(nodestore);
        for (k, v) in items {
            merkle.insert(k.as_ref(), Box::from(v.as_ref()))?;
        }

        Ok(merkle)
    }

    #[test]
    fn test_root_hash_simple_insertions() -> Result<(), Error> {
        let items = vec![
            ("do", "verb"),
            ("doe", "reindeer"),
            ("dog", "puppy"),
            ("doge", "coin"),
            ("horse", "stallion"),
            ("ddd", "ok"),
        ];
        let merkle = merkle_build_test(items).unwrap().hash();

        merkle.dump().unwrap();
        Ok(())
    }

    #[cfg(not(feature = "ethhash"))]
    #[test_case(vec![], None; "empty trie")]
    #[test_case(vec![(&[0],&[0])], Some("073615413d814b23383fc2c8d8af13abfffcb371b654b98dbf47dd74b1e4d1b9"); "root")]
    #[test_case(vec![(&[0,1],&[0,1])], Some("28e67ae4054c8cdf3506567aa43f122224fe65ef1ab3e7b7899f75448a69a6fd"); "root with partial path")]
    #[test_case(vec![(&[0],&[1;32])], Some("ba0283637f46fa807280b7d08013710af08dfdc236b9b22f9d66e60592d6c8a3"); "leaf value >= 32 bytes")]
    #[test_case(vec![(&[0],&[0]),(&[0,1],&[1;32])], Some("3edbf1fdd345db01e47655bcd0a9a456857c4093188cf35c5c89b8b0fb3de17e"); "branch value >= 32 bytes")]
    #[test_case(vec![(&[0],&[0]),(&[0,1],&[0,1])], Some("c3bdc20aff5cba30f81ffd7689e94e1dbeece4a08e27f0104262431604cf45c6"); "root with leaf child")]
    #[test_case(vec![(&[0],&[0]),(&[0,1],&[0,1]),(&[0,1,2],&[0,1,2])], Some("229011c50ad4d5c2f4efe02b8db54f361ad295c4eee2bf76ea4ad1bb92676f97"); "root with branch child")]
    #[test_case(vec![(&[0],&[0]),(&[0,1],&[0,1]),(&[0,8],&[0,8]),(&[0,1,2],&[0,1,2])], Some("a683b4881cb540b969f885f538ba5904699d480152f350659475a962d6240ef9"); "root with branch child and leaf child")]
    fn test_root_hash_merkledb_compatible(kvs: Vec<(&[u8], &[u8])>, expected_hash: Option<&str>) {
        // TODO: get the hashes from merkledb and verify compatibility with branch factor 256
        #[cfg(not(feature = "branch_factor_256"))]
        {
            let merkle = merkle_build_test(kvs).unwrap().hash();
            let Some(got_hash) = merkle.nodestore.root_hash() else {
                assert!(expected_hash.is_none());
                return;
            };

            let expected_hash = expected_hash.unwrap();

            // This hash is from merkledb
            let expected_hash: [u8; 32] = hex::decode(expected_hash).unwrap().try_into().unwrap();

            assert_eq!(got_hash, TrieHash::from(expected_hash));
        }
    }

    #[cfg(feature = "ethhash")]
    mod ethhasher {
        use ethereum_types::H256;
        use hash_db::Hasher;
        use plain_hasher::PlainHasher;
        use sha3::{Digest, Keccak256};

        #[derive(Default, Debug, Clone, PartialEq, Eq, Hash)]
        pub struct KeccakHasher;

        impl Hasher for KeccakHasher {
            type Out = H256;
            type StdHasher = PlainHasher;
            const LENGTH: usize = 32;

            #[inline]
            fn hash(x: &[u8]) -> Self::Out {
                let mut hasher = Keccak256::new();
                hasher.update(x);
                let result = hasher.finalize();
                H256::from_slice(result.as_slice())
            }
        }
    }

    #[cfg(feature = "ethhash")]
    #[test_case(&[("doe", "reindeer")])]
    #[test_case(&[("doe", "reindeer"),("dog", "puppy"),("dogglesworth", "cat")])]
    #[test_case(&[("doe", "reindeer"),("dog", "puppy"),("dogglesworth", "cacatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatt")])]
    #[test_case(&[("dogglesworth", "cacatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatcatt")])]
    fn test_root_hash_eth_compatible<T: AsRef<[u8]> + Clone + Ord>(kvs: &[(T, T)]) {
        use ethereum_types::H256;
        use ethhasher::KeccakHasher;
        use firewood_triehash::trie_root;

        let merkle = merkle_build_test(kvs.to_vec()).unwrap().hash();
        let firewood_hash = merkle.nodestore.root_hash().unwrap_or_default();
        let eth_hash = trie_root::<KeccakHasher, _, _, _>(kvs.to_vec());
        let firewood_hash = H256::from_slice(firewood_hash.as_ref());

        assert_eq!(firewood_hash, eth_hash);
    }

    #[cfg(feature = "ethhash")]
    #[test_case(
            "0000000000000000000000000000000000000002",
            "f844802ca00000000000000000000000000000000000000000000000000000000000000000a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
            &[],
            "c00ca9b8e6a74b03f6b1ae2db4a65ead348e61b74b339fe4b117e860d79c7821"
    )]
    #[test_case(
            "0000000000000000000000000000000000000002",
            "f844802ca00000000000000000000000000000000000000000000000000000000000000000a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
            &[
                    ("48078cfed56339ea54962e72c37c7f588fc4f8e5bc173827ba75cb10a63a96a5", "a00200000000000000000000000000000000000000000000000000000000000000")
            ],
            "91336bf4e6756f68e1af0ad092f4a551c52b4a66860dc31adbd736f0acbadaf6"
    )]
    #[test_case(
            "0000000000000000000000000000000000000002",
            "f844802ca00000000000000000000000000000000000000000000000000000000000000000a0c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
            &[
                    ("48078cfed56339ea54962e72c37c7f588fc4f8e5bc173827ba75cb10a63a96a5", "a00200000000000000000000000000000000000000000000000000000000000000"),
                    ("0e81f83a84964b811dd1b8328262a9f57e6bc3e5e7eb53627d10437c73c4b8da", "a02800000000000000000000000000000000000000000000000000000000000000"),
            ],
            "c267104830880c966c2cc8c669659e4bfaf3126558dbbd6216123b457944001b"
    )]
    fn test_eth_compatible_accounts(
        account: &str,
        account_value: &str,
        key_suffixes_and_values: &[(&str, &str)],
        expected_root: &str,
    ) {
        use sha2::Digest as _;
        use sha3::Keccak256;

        env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("trace"))
            .is_test(true)
            .try_init()
            .ok();

        let account = make_box(account);
        let expected_key_hash = Keccak256::digest(&account);

        let items = once((
            Box::from(expected_key_hash.as_slice()),
            make_box(account_value),
        ))
        .chain(key_suffixes_and_values.iter().map(|(key_suffix, value)| {
            let key = expected_key_hash
                .iter()
                .copied()
                .chain(make_box(key_suffix).iter().copied())
                .collect();
            let value = make_box(value);
            (key, value)
        }))
        .collect::<Vec<(Box<_>, Box<_>)>>();

        let merkle = merkle_build_test(items).unwrap().hash();
        let firewood_hash = merkle.nodestore.root_hash();

        assert_eq!(
            firewood_hash,
            TrieHash::try_from(&*make_box(expected_root)).ok()
        );
    }

    // helper method to convert a string into a boxed slice
    #[cfg(feature = "ethhash")]
    fn make_box(hex_str: &str) -> Box<[u8]> {
        hex::decode(hex_str).unwrap().into_boxed_slice()
    }

    #[test]
    fn test_root_hash_fuzz_insertions() -> Result<(), FileIoError> {
        use rand::rngs::StdRng;
        use rand::{Rng, SeedableRng};
        let rng = std::cell::RefCell::new(StdRng::seed_from_u64(42));
        let max_len0 = 8;
        let max_len1 = 4;
        let keygen = || {
            let (len0, len1): (usize, usize) = {
                let mut rng = rng.borrow_mut();
                (
                    rng.random_range(1..=max_len0),
                    rng.random_range(1..=max_len1),
                )
            };
            let key: Vec<u8> = (0..len0)
                .map(|_| rng.borrow_mut().random_range(0..2))
                .chain((0..len1).map(|_| rng.borrow_mut().random()))
                .collect();
            key
        };

        // TODO: figure out why this fails if we use more than 27 iterations with branch_factor_256
        for _ in 0..27 {
            let mut items = Vec::new();

            for _ in 0..100 {
                let val: Vec<u8> = (0..256).map(|_| rng.borrow_mut().random()).collect();
                items.push((keygen(), val));
            }

            // #[cfg(feature = "ethhash")]
            // test_root_hash_eth_compatible(&items);
            merkle_build_test(items)?;
        }

        Ok(())
    }

    #[test]
    fn test_delete_child() {
        let items = vec![("do", "verb")];
        let mut merkle = merkle_build_test(items).unwrap();

        assert_eq!(merkle.remove(b"does_not_exist").unwrap(), None);
        assert_eq!(&*merkle.get_value(b"do").unwrap().unwrap(), b"verb");
    }

    #[test]
    fn test_delete_some() {
        let items = (0..100)
            .map(|n| {
                let key = format!("key{n}");
                let val = format!("value{n}");
                (key.as_bytes().to_vec(), val.as_bytes().to_vec())
            })
            .collect::<Vec<(Vec<u8>, Vec<u8>)>>();
        let mut merkle = merkle_build_test(items.clone()).unwrap();
        merkle.remove_prefix(b"key1").unwrap();
        for item in items {
            let (key, val) = item;
            if key.starts_with(b"key1") {
                assert!(merkle.get_value(&key).unwrap().is_none());
            } else {
                assert_eq!(&*merkle.get_value(&key).unwrap().unwrap(), val.as_slice());
            }
        }
    }

    #[test]
    fn test_root_hash_reversed_deletions() -> Result<(), FileIoError> {
        use rand::rngs::StdRng;
        use rand::{Rng, SeedableRng};

        let _ = env_logger::Builder::new().is_test(true).try_init();

        let seed = std::env::var("FIREWOOD_TEST_SEED")
            .ok()
            .map_or_else(
                || None,
                |s| Some(str::parse(&s).expect("couldn't parse FIREWOOD_TEST_SEED; must be a u64")),
            )
            .unwrap_or_else(|| rng().random());

        eprintln!("Seed {seed}: to rerun with this data, export FIREWOOD_TEST_SEED={seed}");
        let rng = std::cell::RefCell::new(StdRng::seed_from_u64(seed));

        let max_len0 = 8;
        let max_len1 = 4;
        let keygen = || {
            let (len0, len1): (usize, usize) = {
                let mut rng = rng.borrow_mut();
                (
                    rng.random_range(1..=max_len0),
                    rng.random_range(1..=max_len1),
                )
            };
            let key: Vec<u8> = (0..len0)
                .map(|_| rng.borrow_mut().random_range(0..2))
                .chain((0..len1).map(|_| rng.borrow_mut().random()))
                .collect();
            key
        };

        for _ in 0..10 {
            let mut items: Vec<_> = (0..10)
                .map(|_| keygen())
                .map(|key| {
                    let val: Box<[u8]> = (0..8).map(|_| rng.borrow_mut().random()).collect();
                    (key, val)
                })
                .collect();

            items.sort();
            items.dedup_by_key(|(k, _)| k.clone());

            let init_merkle: Merkle<NodeStore<MutableProposal, MemStore>> =
                merkle_build_test::<Vec<u8>, Box<[u8]>>(vec![])?;
            let init_immutable_merkle: Merkle<NodeStore<Arc<ImmutableProposal>, _>> =
                init_merkle.try_into().unwrap();

            let (hashes, complete_immutable_merkle) = items.iter().fold(
                (vec![], init_immutable_merkle),
                |(mut hashes, immutable_merkle), (k, v)| {
                    let root_hash = immutable_merkle.nodestore.root_hash();
                    hashes.push(root_hash);
                    let mut merkle = Merkle::from(
                        NodeStore::new(&Arc::new(immutable_merkle.nodestore)).unwrap(),
                    );
                    merkle.insert(k, v.clone()).unwrap();
                    (hashes, merkle.try_into().unwrap())
                },
            );

            let (new_hashes, _) = items.iter().rev().fold(
                (vec![], complete_immutable_merkle),
                |(mut new_hashes, immutable_merkle_before_removal), (k, _)| {
                    let before = immutable_merkle_before_removal.dump().unwrap();
                    let mut merkle = Merkle::from(
                        NodeStore::new(&Arc::new(immutable_merkle_before_removal.nodestore))
                            .unwrap(),
                    );
                    merkle.remove(k).unwrap();
                    let immutable_merkle_after_removal: Merkle<
                        NodeStore<Arc<ImmutableProposal>, _>,
                    > = merkle.try_into().unwrap();
                    new_hashes.push((
                        immutable_merkle_after_removal.nodestore.root_hash(),
                        k,
                        before,
                        immutable_merkle_after_removal.dump().unwrap(),
                    ));
                    (new_hashes, immutable_merkle_after_removal)
                },
            );

            for (expected_hash, (actual_hash, key, before_removal, after_removal)) in
                hashes.into_iter().rev().zip(new_hashes)
            {
                let key = key.iter().fold(String::new(), |mut s, b| {
                    let _ = write!(s, "{b:02x}");
                    s
                });
                assert_eq!(
                    actual_hash, expected_hash,
                    "\n\nkey: {key}\nbefore:\n{before_removal}\nafter:\n{after_removal}\n\nexpected:\n{expected_hash:?}\nactual:\n{actual_hash:?}\n",
                );
            }
        }

        Ok(())
    }

    // #[test]
    // fn test_root_hash_random_deletions() -> Result<(), Error> {
    //     use rand::rngs::StdRng;
    //     use rand::seq::SliceRandom;
    //     use rand::{Rng, SeedableRng};
    //     let rng = std::cell::RefCell::new(StdRng::seed_from_u64(42));
    //     let max_len0 = 8;
    //     let max_len1 = 4;
    //     let keygen = || {
    //         let (len0, len1): (usize, usize) = {
    //             let mut rng = rng.borrow_mut();
    //             (
    //                 rng.random_range(1..max_len0 + 1),
    //                 rng.random_range(1..max_len1 + 1),
    //             )
    //         };
    //         let key: Vec<u8> = (0..len0)
    //             .map(|_| rng.borrow_mut().random_range(0..2))
    //             .chain((0..len1).map(|_| rng.borrow_mut().random()))
    //             .collect();
    //         key
    //     };

    //     for i in 0..10 {
    //         let mut items = std::collections::HashMap::new();

    //         for _ in 0..10 {
    //             let val: Box<[u8]> = (0..8).map(|_| rng.borrow_mut().random()).collect();
    //             items.insert(keygen(), val);
    //         }

    //         let mut items_ordered: Vec<_> =
    //             items.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
    //         items_ordered.sort();
    //         items_ordered.shuffle(&mut *rng.borrow_mut());
    //         let mut merkle =
    //             Merkle::new(HashedNodeStore::initialize(MemStore::new(vec![])).unwrap());

    //         for (k, v) in items.iter() {
    //             merkle.insert(k, v.clone())?;
    //         }

    //         for (k, v) in items.iter() {
    //             assert_eq!(&*merkle.get(k)?.unwrap(), &v[..]);
    //         }

    //         for (k, _) in items_ordered.into_iter() {
    //             assert!(merkle.get(&k)?.is_some());

    //             merkle.remove(&k)?;

    //             assert!(merkle.get(&k)?.is_none());

    //             items.remove(&k);

    //             for (k, v) in items.iter() {
    //                 assert_eq!(&*merkle.get(k)?.unwrap(), &v[..]);
    //             }

    //             let h = triehash::trie_root::<keccak_hasher::KeccakHasher, Vec<_>, _, _>(
    //                 items.iter().collect(),
    //             );

    //             let h0 = merkle.root_hash()?;

    //             if TrieHash::from(h) != h0 {
    //                 println!("{} != {}", hex::encode(h), hex::encode(*h0));
    //             }
    //         }

    //         println!("i = {i}");
    //     }
    //     Ok(())
    // }

    // #[test]
    // fn test_proof() -> Result<(), Error> {
    //     let set = fixed_and_pseudorandom_data(500);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;
    //     let (keys, vals): (Vec<[u8; 32]>, Vec<[u8; 20]>) = items.into_iter().unzip();

    //     for (i, key) in keys.iter().enumerate() {
    //         let proof = merkle.prove(key)?;
    //         assert!(!proof.0.is_empty());
    //         let val = merkle.verify_proof(key, &proof)?;
    //         assert!(val.is_some());
    //         assert_eq!(val.unwrap(), vals[i]);
    //     }

    //     Ok(())
    // }

    // #[test]
    // /// Verify the proofs that end with leaf node with the given key.
    // fn test_proof_end_with_leaf() -> Result<(), Error> {
    //     let items = vec![
    //         ("do", "verb"),
    //         ("doe", "reindeer"),
    //         ("dog", "puppy"),
    //         ("doge", "coin"),
    //         ("horse", "stallion"),
    //         ("ddd", "ok"),
    //     ];
    //     let merkle = merkle_build_test(items)?;
    //     let key = "doe".as_ref();

    //     let proof = merkle.prove(key)?;
    //     assert!(!proof.0.is_empty());

    //     let verify_proof = merkle.verify_proof(key, &proof)?;
    //     assert!(verify_proof.is_some());

    //     Ok(())
    // }

    // #[test]
    // /// Verify the proofs that end with branch node with the given key.
    // fn test_proof_end_with_branch() -> Result<(), Error> {
    //     let items = vec![
    //         ("d", "verb"),
    //         ("do", "verb"),
    //         ("doe", "reindeer"),
    //         ("e", "coin"),
    //     ];
    //     let merkle = merkle_build_test(items)?;
    //     let key = "d".as_ref();

    //     let proof = merkle.prove(key)?;
    //     assert!(!proof.0.is_empty());

    //     let verify_proof = merkle.verify_proof(key, &proof)?;
    //     assert!(verify_proof.is_some());

    //     Ok(())
    // }

    // #[test]
    // fn test_bad_proof() -> Result<(), Error> {
    //     let set = fixed_and_pseudorandom_data(800);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;
    //     let (keys, _): (Vec<[u8; 32]>, Vec<[u8; 20]>) = items.into_iter().unzip();

    //     for key in keys.iter() {
    //         let mut proof = merkle.prove(key)?;
    //         assert!(!proof.0.is_empty());

    //         // Delete an entry from the generated proofs.
    //         let len = proof.0.len();
    //         let new_proof = Proof(proof.0.drain().take(len - 1).collect());
    //         assert!(merkle.verify_proof(key, &new_proof).is_err());
    //     }

    //     Ok(())
    // }

    // #[test]
    // // Tests that missing keys can also be proven. The test explicitly uses a single
    // // entry trie and checks for missing keys both before and after the single entry.
    // fn test_missing_key_proof() -> Result<(), Error> {
    //     let items = vec![("k", "v")];
    //     let merkle = merkle_build_test(items)?;
    //     for key in ["a", "j", "l", "z"] {
    //         let proof = merkle.prove(key.as_ref())?;
    //         assert!(!proof.0.is_empty());
    //         assert!(proof.0.len() == 1);

    //         let val = merkle.verify_proof(key.as_ref(), &proof)?;
    //         assert!(val.is_none());
    //     }

    //     Ok(())
    // }

    // #[test]
    // fn test_empty_tree_proof() -> Result<(), Error> {
    //     let items: Vec<(&str, &str)> = Vec::new();
    //     let merkle = merkle_build_test(items)?;
    //     let key = "x".as_ref();

    //     let proof = merkle.prove(key)?;
    //     assert!(proof.0.is_empty());

    //     Ok(())
    // }

    // #[test]
    // // Tests normal range proof with both edge proofs as the existent proof.
    // // The test cases are generated randomly.
    // fn test_range_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     for _ in 0..10 {
    //         let start = rand::rng().random_range(0..items.len());
    //         let end = rand::rng().random_range(0..items.len() - start) + start - 1;

    //         if end <= start {
    //             continue;
    //         }

    //         let mut proof = merkle.prove(items[start].0)?;
    //         assert!(!proof.0.is_empty());
    //         let end_proof = merkle.prove(items[end - 1].0)?;
    //         assert!(!end_proof.0.is_empty());
    //         proof.extend(end_proof);

    //         let mut keys = Vec::new();
    //         let mut vals = Vec::new();
    //         for item in items[start..end].iter() {
    //             keys.push(item.0.as_ref());
    //             vals.push(&item.1);
    //         }

    //         merkle.verify_range_proof(&proof, items[start].0, items[end - 1].0, keys, vals)?;
    //     }
    //     Ok(())
    // }

    // #[test]
    // // Tests a few cases which the proof is wrong.
    // // The prover is expected to detect the error.
    // fn test_bad_range_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     for _ in 0..10 {
    //         let start = rand::rng().random_range(0..items.len());
    //         let end = rand::rng().random_range(0..items.len() - start) + start - 1;

    //         if end <= start {
    //             continue;
    //         }

    //         let mut proof = merkle.prove(items[start].0)?;
    //         assert!(!proof.0.is_empty());
    //         let end_proof = merkle.prove(items[end - 1].0)?;
    //         assert!(!end_proof.0.is_empty());
    //         proof.extend(end_proof);

    //         let mut keys: Vec<[u8; 32]> = Vec::new();
    //         let mut vals: Vec<[u8; 20]> = Vec::new();
    //         for item in items[start..end].iter() {
    //             keys.push(*item.0);
    //             vals.push(*item.1);
    //         }

    //         let test_case: u32 = rand::rng().random_range(0..6);
    //         let index = rand::rng().random_range(0..end - start);
    //         match test_case {
    //             0 => {
    //                 // Modified key
    //                 keys[index] = rand::rng().random::<[u8; 32]>(); // In theory it can't be same
    //             }
    //             1 => {
    //                 // Modified val
    //                 vals[index] = rand::rng().random::<[u8; 20]>(); // In theory it can't be same
    //             }
    //             2 => {
    //                 // Gapped entry slice
    //                 if index == 0 || index == end - start - 1 {
    //                     continue;
    //                 }
    //                 keys.remove(index);
    //                 vals.remove(index);
    //             }
    //             3 => {
    //                 // Out of order
    //                 let index_1 = rand::rng().random_range(0..end - start);
    //                 let index_2 = rand::rng().random_range(0..end - start);
    //                 if index_1 == index_2 {
    //                     continue;
    //                 }
    //                 keys.swap(index_1, index_2);
    //                 vals.swap(index_1, index_2);
    //             }
    //             4 => {
    //                 // Set random key to empty, do nothing
    //                 keys[index] = [0; 32];
    //             }
    //             5 => {
    //                 // Set random value to nil
    //                 vals[index] = [0; 20];
    //             }
    //             _ => unreachable!(),
    //         }

    //         let keys_slice = keys.iter().map(|k| k.as_ref()).collect::<Vec<&[u8]>>();
    //         assert!(merkle
    //             .verify_range_proof(&proof, items[start].0, items[end - 1].0, keys_slice, vals)
    //             .is_err());
    //     }

    //     Ok(())
    // }

    // #[test]
    // // Tests normal range proof with two non-existent proofs.
    // // The test cases are generated randomly.
    // fn test_range_proof_with_non_existent_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     for _ in 0..10 {
    //         let start = rand::rng().random_range(0..items.len());
    //         let end = rand::rng().random_range(0..items.len() - start) + start - 1;

    //         if end <= start {
    //             continue;
    //         }

    //         // Short circuit if the decreased key is same with the previous key
    //         let first = decrease_key(items[start].0);
    //         if start != 0 && first.as_ref() == items[start - 1].0.as_ref() {
    //             continue;
    //         }
    //         // Short circuit if the decreased key is underflow
    //         if &first > items[start].0 {
    //             continue;
    //         }
    //         // Short circuit if the increased key is same with the next key
    //         let last = increase_key(items[end - 1].0);
    //         if end != items.len() && last.as_ref() == items[end].0.as_ref() {
    //             continue;
    //         }
    //         // Short circuit if the increased key is overflow
    //         if &last < items[end - 1].0 {
    //             continue;
    //         }

    //         let mut proof = merkle.prove(&first)?;
    //         assert!(!proof.0.is_empty());
    //         let end_proof = merkle.prove(&last)?;
    //         assert!(!end_proof.0.is_empty());
    //         proof.extend(end_proof);

    //         let mut keys: Vec<&[u8]> = Vec::new();
    //         let mut vals: Vec<[u8; 20]> = Vec::new();
    //         for item in items[start..end].iter() {
    //             keys.push(item.0);
    //             vals.push(*item.1);
    //         }

    //         merkle.verify_range_proof(&proof, &first, &last, keys, vals)?;
    //     }

    //     // Special case, two edge proofs for two edge key.
    //     let first = &[0; 32];
    //     let last = &[255; 32];
    //     let mut proof = merkle.prove(first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(last)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     let (keys, vals): (Vec<&[u8; 32]>, Vec<&[u8; 20]>) = items.into_iter().unzip();
    //     let keys = keys.iter().map(|k| k.as_ref()).collect::<Vec<&[u8]>>();
    //     merkle.verify_range_proof(&proof, first, last, keys, vals)?;

    //     Ok(())
    // }

    // #[test]
    // // Tests such scenarios:
    // // - There exists a gap between the first element and the left edge proof
    // // - There exists a gap between the last element and the right edge proof
    // fn test_range_proof_with_invalid_non_existent_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     // Case 1
    //     let mut start = 100;
    //     let mut end = 200;
    //     let first = decrease_key(items[start].0);

    //     let mut proof = merkle.prove(&first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(items[end - 1].0)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     start = 105; // Gap created
    //     let mut keys: Vec<&[u8]> = Vec::new();
    //     let mut vals: Vec<[u8; 20]> = Vec::new();
    //     // Create gap
    //     for item in items[start..end].iter() {
    //         keys.push(item.0);
    //         vals.push(*item.1);
    //     }
    //     assert!(merkle
    //         .verify_range_proof(&proof, &first, items[end - 1].0, keys, vals)
    //         .is_err());

    //     // Case 2
    //     start = 100;
    //     end = 200;
    //     let last = increase_key(items[end - 1].0);

    //     let mut proof = merkle.prove(items[start].0)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&last)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     end = 195; // Capped slice
    //     let mut keys: Vec<&[u8]> = Vec::new();
    //     let mut vals: Vec<[u8; 20]> = Vec::new();
    //     // Create gap
    //     for item in items[start..end].iter() {
    //         keys.push(item.0);
    //         vals.push(*item.1);
    //     }
    //     assert!(merkle
    //         .verify_range_proof(&proof, items[start].0, &last, keys, vals)
    //         .is_err());

    //     Ok(())
    // }

    // #[test]
    // // Tests the proof with only one element. The first edge proof can be existent one or
    // // non-existent one.
    // fn test_one_element_range_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     // One element with existent edge proof, both edge proofs
    //     // point to the SAME key.
    //     let start = 1000;
    //     let start_proof = merkle.prove(items[start].0)?;
    //     assert!(!start_proof.0.is_empty());

    //     merkle.verify_range_proof(
    //         &start_proof,
    //         items[start].0,
    //         items[start].0,
    //         vec![items[start].0],
    //         vec![&items[start].1],
    //     )?;

    //     // One element with left non-existent edge proof
    //     let first = decrease_key(items[start].0);
    //     let mut proof = merkle.prove(&first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(items[start].0)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     merkle.verify_range_proof(
    //         &proof,
    //         &first,
    //         items[start].0,
    //         vec![items[start].0],
    //         vec![*items[start].1],
    //     )?;

    //     // One element with right non-existent edge proof
    //     let last = increase_key(items[start].0);
    //     let mut proof = merkle.prove(items[start].0)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&last)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     merkle.verify_range_proof(
    //         &proof,
    //         items[start].0,
    //         &last,
    //         vec![items[start].0],
    //         vec![*items[start].1],
    //     )?;

    //     // One element with two non-existent edge proofs
    //     let mut proof = merkle.prove(&first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&last)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     merkle.verify_range_proof(
    //         &proof,
    //         &first,
    //         &last,
    //         vec![items[start].0],
    //         vec![*items[start].1],
    //     )?;

    //     // Test the mini trie with only a single element.
    //     let key = rand::rng().random::<[u8; 32]>();
    //     let val = rand::rng().random::<[u8; 20]>();
    //     let merkle = merkle_build_test(vec![(key, val)])?;

    //     let first = &[0; 32];
    //     let mut proof = merkle.prove(first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&key)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     merkle.verify_range_proof(&proof, first, &key, vec![&key], vec![val])?;

    //     Ok(())
    // }

    // #[test]
    // // Tests the range proof with all elements.
    // // The edge proofs can be nil.
    // fn test_all_elements_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     let item_iter = items.clone().into_iter();
    //     let keys: Vec<&[u8]> = item_iter.clone().map(|item| item.0.as_ref()).collect();
    //     let vals: Vec<&[u8; 20]> = item_iter.map(|item| item.1).collect();

    //     let empty_proof = Proof(HashMap::<[u8; 32], Vec<u8>>::new());
    //     let empty_key: [u8; 32] = [0; 32];
    //     merkle.verify_range_proof(
    //         &empty_proof,
    //         &empty_key,
    //         &empty_key,
    //         keys.clone(),
    //         vals.clone(),
    //     )?;

    //     // With edge proofs, it should still work.
    //     let start = 0;
    //     let end = &items.len() - 1;

    //     let mut proof = merkle.prove(items[start].0)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(items[end].0)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     merkle.verify_range_proof(
    //         &proof,
    //         items[start].0,
    //         items[end].0,
    //         keys.clone(),
    //         vals.clone(),
    //     )?;

    //     // Even with non-existent edge proofs, it should still work.
    //     let first = &[0; 32];
    //     let last = &[255; 32];
    //     let mut proof = merkle.prove(first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(last)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     merkle.verify_range_proof(&proof, first, last, keys, vals)?;

    //     Ok(())
    // }

    // #[test]
    // // Tests the range proof with "no" element. The first edge proof must
    // // be a non-existent proof.
    // fn test_empty_range_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     let cases = [(items.len() - 1, false)];
    //     for c in cases.iter() {
    //         let first = increase_key(items[c.0].0);
    //         let proof = merkle.prove(&first)?;
    //         assert!(!proof.0.is_empty());

    //         // key and value vectors are intentionally empty.
    //         let keys: Vec<&[u8]> = Vec::new();
    //         let vals: Vec<[u8; 20]> = Vec::new();

    //         if c.1 {
    //             assert!(merkle
    //                 .verify_range_proof(&proof, &first, &first, keys, vals)
    //                 .is_err());
    //         } else {
    //             merkle.verify_range_proof(&proof, &first, &first, keys, vals)?;
    //         }
    //     }

    //     Ok(())
    // }

    // #[test]
    // // Focuses on the small trie with embedded nodes. If the gapped
    // // node is embedded in the trie, it should be detected too.
    // fn test_gapped_range_proof() -> Result<(), ProofError> {
    //     let mut items = Vec::new();
    //     // Sorted entries
    //     for i in 0..10_u32 {
    //         let mut key = [0; 32];
    //         for (index, d) in i.to_be_bytes().iter().enumerate() {
    //             key[index] = *d;
    //         }
    //         items.push((key, i.to_be_bytes()));
    //     }
    //     let merkle = merkle_build_test(items.clone())?;

    //     let first = 2;
    //     let last = 8;

    //     let mut proof = merkle.prove(&items[first].0)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&items[last - 1].0)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     let middle = (first + last) / 2 - first;
    //     let (keys, vals): (Vec<&[u8]>, Vec<&[u8; 4]>) = items[first..last]
    //         .iter()
    //         .enumerate()
    //         .filter(|(pos, _)| *pos != middle)
    //         .map(|(_, item)| (item.0.as_ref(), &item.1))
    //         .unzip();

    //     assert!(merkle
    //         .verify_range_proof(&proof, &items[0].0, &items[items.len() - 1].0, keys, vals)
    //         .is_err());

    //     Ok(())
    // }

    // #[test]
    // // Tests the element is not in the range covered by proofs.
    // fn test_same_side_proof() -> Result<(), Error> {
    //     let set = fixed_and_pseudorandom_data(4096);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     let pos = 1000;
    //     let mut last = decrease_key(items[pos].0);
    //     let mut first = last;
    //     first = decrease_key(&first);

    //     let mut proof = merkle.prove(&first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&last)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     assert!(merkle
    //         .verify_range_proof(
    //             &proof,
    //             &first,
    //             &last,
    //             vec![items[pos].0],
    //             vec![items[pos].1]
    //         )
    //         .is_err());

    //     first = increase_key(items[pos].0);
    //     last = first;
    //     last = increase_key(&last);

    //     let mut proof = merkle.prove(&first)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&last)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     assert!(merkle
    //         .verify_range_proof(
    //             &proof,
    //             &first,
    //             &last,
    //             vec![items[pos].0],
    //             vec![items[pos].1]
    //         )
    //         .is_err());

    //     Ok(())
    // }

    // #[test]
    // // Tests the range starts from zero.
    // fn test_single_side_range_proof() -> Result<(), ProofError> {
    //     for _ in 0..10 {
    //         let mut set = HashMap::new();
    //         for _ in 0..4096_u32 {
    //             let key = rand::rng().random::<[u8; 32]>();
    //             let val = rand::rng().random::<[u8; 20]>();
    //             set.insert(key, val);
    //         }
    //         let mut items = Vec::from_iter(set.iter());
    //         items.sort();
    //         let merkle = merkle_build_test(items.clone())?;

    //         let cases = vec![0, 1, 100, 1000, items.len() - 1];
    //         for case in cases {
    //             let start = &[0; 32];
    //             let mut proof = merkle.prove(start)?;
    //             assert!(!proof.0.is_empty());
    //             let end_proof = merkle.prove(items[case].0)?;
    //             assert!(!end_proof.0.is_empty());
    //             proof.extend(end_proof);

    //             let item_iter = items.clone().into_iter().take(case + 1);
    //             let keys = item_iter.clone().map(|item| item.0.as_ref()).collect();
    //             let vals = item_iter.map(|item| item.1).collect();

    //             merkle.verify_range_proof(&proof, start, items[case].0, keys, vals)?;
    //         }
    //     }
    //     Ok(())
    // }

    // #[test]
    // // Tests the range ends with 0xffff...fff.
    // fn test_reverse_single_side_range_proof() -> Result<(), ProofError> {
    //     for _ in 0..10 {
    //         let mut set = HashMap::new();
    //         for _ in 0..1024_u32 {
    //             let key = rand::rng().random::<[u8; 32]>();
    //             let val = rand::rng().random::<[u8; 20]>();
    //             set.insert(key, val);
    //         }
    //         let mut items = Vec::from_iter(set.iter());
    //         items.sort();
    //         let merkle = merkle_build_test(items.clone())?;

    //         let cases = vec![0, 1, 100, 1000, items.len() - 1];
    //         for case in cases {
    //             let end = &[255; 32];
    //             let mut proof = merkle.prove(items[case].0)?;
    //             assert!(!proof.0.is_empty());
    //             let end_proof = merkle.prove(end)?;
    //             assert!(!end_proof.0.is_empty());
    //             proof.extend(end_proof);

    //             let item_iter = items.clone().into_iter().skip(case);
    //             let keys = item_iter.clone().map(|item| item.0.as_ref()).collect();
    //             let vals = item_iter.map(|item| item.1).collect();

    //             merkle.verify_range_proof(&proof, items[case].0, end, keys, vals)?;
    //         }
    //     }
    //     Ok(())
    // }

    // #[test]
    // // Tests the range starts with zero and ends with 0xffff...fff.
    // fn test_both_sides_range_proof() -> Result<(), ProofError> {
    //     for _ in 0..10 {
    //         let mut set = HashMap::new();
    //         for _ in 0..4096_u32 {
    //             let key = rand::rng().random::<[u8; 32]>();
    //             let val = rand::rng().random::<[u8; 20]>();
    //             set.insert(key, val);
    //         }
    //         let mut items = Vec::from_iter(set.iter());
    //         items.sort();
    //         let merkle = merkle_build_test(items.clone())?;

    //         let start = &[0; 32];
    //         let end = &[255; 32];

    //         let mut proof = merkle.prove(start)?;
    //         assert!(!proof.0.is_empty());
    //         let end_proof = merkle.prove(end)?;
    //         assert!(!end_proof.0.is_empty());
    //         proof.extend(end_proof);

    //         let (keys, vals): (Vec<&[u8; 32]>, Vec<&[u8; 20]>) = items.into_iter().unzip();
    //         let keys = keys.iter().map(|k| k.as_ref()).collect::<Vec<&[u8]>>();
    //         merkle.verify_range_proof(&proof, start, end, keys, vals)?;
    //     }
    //     Ok(())
    // }

    // #[test]
    // // Tests normal range proof with both edge proofs
    // // as the existent proof, but with an extra empty value included, which is a
    // // noop technically, but practically should be rejected.
    // fn test_empty_value_range_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(512);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     // Create a new entry with a slightly modified key
    //     let mid_index = items.len() / 2;
    //     let key = increase_key(items[mid_index - 1].0);
    //     let empty_value: [u8; 20] = [0; 20];
    //     items.splice(mid_index..mid_index, [(&key, &empty_value)].iter().cloned());

    //     let start = 1;
    //     let end = items.len() - 1;

    //     let mut proof = merkle.prove(items[start].0)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(items[end - 1].0)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     let item_iter = items.clone().into_iter().skip(start).take(end - start);
    //     let keys = item_iter.clone().map(|item| item.0.as_ref()).collect();
    //     let vals = item_iter.map(|item| item.1).collect();
    //     assert!(merkle
    //         .verify_range_proof(&proof, items[start].0, items[end - 1].0, keys, vals)
    //         .is_err());

    //     Ok(())
    // }

    // #[test]
    // // Tests the range proof with all elements,
    // // but with an extra empty value included, which is a noop technically, but
    // // practically should be rejected.
    // fn test_all_elements_empty_value_range_proof() -> Result<(), ProofError> {
    //     let set = fixed_and_pseudorandom_data(512);
    //     let mut items = Vec::from_iter(set.iter());
    //     items.sort();
    //     let merkle = merkle_build_test(items.clone())?;

    //     // Create a new entry with a slightly modified key
    //     let mid_index = items.len() / 2;
    //     let key = increase_key(items[mid_index - 1].0);
    //     let empty_value: [u8; 20] = [0; 20];
    //     items.splice(mid_index..mid_index, [(&key, &empty_value)].iter().cloned());

    //     let start = 0;
    //     let end = items.len() - 1;

    //     let mut proof = merkle.prove(items[start].0)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(items[end].0)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     let item_iter = items.clone().into_iter();
    //     let keys = item_iter.clone().map(|item| item.0.as_ref()).collect();
    //     let vals = item_iter.map(|item| item.1).collect();
    //     assert!(merkle
    //         .verify_range_proof(&proof, items[start].0, items[end].0, keys, vals)
    //         .is_err());

    //     Ok(())
    // }

    // #[test]
    // fn test_range_proof_keys_with_shared_prefix() -> Result<(), ProofError> {
    //     let items = vec![
    //         (
    //             hex::decode("aa10000000000000000000000000000000000000000000000000000000000000")
    //                 .expect("Decoding failed"),
    //             hex::decode("02").expect("Decoding failed"),
    //         ),
    //         (
    //             hex::decode("aa20000000000000000000000000000000000000000000000000000000000000")
    //                 .expect("Decoding failed"),
    //             hex::decode("03").expect("Decoding failed"),
    //         ),
    //     ];
    //     let merkle = merkle_build_test(items.clone())?;

    //     let start = hex::decode("0000000000000000000000000000000000000000000000000000000000000000")
    //         .expect("Decoding failed");
    //     let end = hex::decode("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
    //         .expect("Decoding failed");

    //     let mut proof = merkle.prove(&start)?;
    //     assert!(!proof.0.is_empty());
    //     let end_proof = merkle.prove(&end)?;
    //     assert!(!end_proof.0.is_empty());
    //     proof.extend(end_proof);

    //     let keys = items.iter().map(|item| item.0.as_ref()).collect();
    //     let vals = items.iter().map(|item| item.1.clone()).collect();

    //     merkle.verify_range_proof(&proof, &start, &end, keys, vals)?;

    //     Ok(())
    // }

    // #[test]
    // // Tests a malicious proof, where the proof is more or less the
    // // whole trie. This is to match corresponding test in geth.
    // fn test_bloadted_range_proof() -> Result<(), ProofError> {
    //     // Use a small trie
    //     let mut items = Vec::new();
    //     for i in 0..100_u32 {
    //         let mut key: [u8; 32] = [0; 32];
    //         let mut value: [u8; 20] = [0; 20];
    //         for (index, d) in i.to_be_bytes().iter().enumerate() {
    //             key[index] = *d;
    //             value[index] = *d;
    //         }
    //         items.push((key, value));
    //     }
    //     let merkle = merkle_build_test(items.clone())?;

    //     // In the 'malicious' case, we add proofs for every single item
    //     // (but only one key/value pair used as leaf)
    //     let mut proof = Proof(HashMap::new());
    //     let mut keys = Vec::new();
    //     let mut vals = Vec::new();
    //     for (i, item) in items.iter().enumerate() {
    //         let cur_proof = merkle.prove(&item.0)?;
    //         assert!(!cur_proof.0.is_empty());
    //         proof.extend(cur_proof);
    //         if i == 50 {
    //             keys.push(item.0.as_ref());
    //             vals.push(item.1);
    //         }
    //     }

    //     merkle.verify_range_proof(&proof, keys[0], keys[keys.len() - 1], keys.clone(), vals)?;

    //     Ok(())
    // }

    // generate pseudorandom data, but prefix it with some known data
    // The number of fixed data points is 100; you specify how much random data you want
    // fn fixed_and_pseudorandom_data(random_count: u32) -> HashMap<[u8; 32], [u8; 20]> {
    //     let mut items: HashMap<[u8; 32], [u8; 20]> = HashMap::new();
    //     for i in 0..100_u32 {
    //         let mut key: [u8; 32] = [0; 32];
    //         let mut value: [u8; 20] = [0; 20];
    //         for (index, d) in i.to_be_bytes().iter().enumerate() {
    //             key[index] = *d;
    //             value[index] = *d;
    //         }
    //         items.insert(key, value);

    //         let mut more_key: [u8; 32] = [0; 32];
    //         for (index, d) in (i + 10).to_be_bytes().iter().enumerate() {
    //             more_key[index] = *d;
    //         }
    //         items.insert(more_key, value);
    //     }

    //     // read FIREWOOD_TEST_SEED from the environment. If it's there, parse it into a u64.
    //     let seed = std::env::var("FIREWOOD_TEST_SEED")
    //         .ok()
    //         .map_or_else(
    //             || None,
    //             |s| Some(str::parse(&s).expect("couldn't parse FIREWOOD_TEST_SEED; must be a u64")),
    //         )
    //         .unwrap_or_else(|| rng().random());

    //     // the test framework will only render this in verbose mode or if the test fails
    //     // to re-run the test when it fails, just specify the seed instead of randomly
    //     // selecting one
    //     eprintln!("Seed {seed}: to rerun with this data, export FIREWOOD_TEST_SEED={seed}");
    //     let mut r = StdRng::seed_from_u64(seed);
    //     for _ in 0..random_count {
    //         let key = r.random::<[u8; 32]>();
    //         let val = r.random::<[u8; 20]>();
    //         items.insert(key, val);
    //     }
    //     items
    // }

    // fn increase_key(key: &[u8; 32]) -> [u8; 32] {
    //     let mut new_key = *key;
    //     for ch in new_key.iter_mut().rev() {
    //         let overflow;
    //         (*ch, overflow) = ch.overflowing_add(1);
    //         if !overflow {
    //             break;
    //         }
    //     }
    //     new_key
    // }

    // fn decrease_key(key: &[u8; 32]) -> [u8; 32] {
    //     let mut new_key = *key;
    //     for ch in new_key.iter_mut().rev() {
    //         let overflow;
    //         (*ch, overflow) = ch.overflowing_sub(1);
    //         if !overflow {
    //             break;
    //         }
    //     }
    //     new_key
    // }
    #[test]
    fn remove_nonexistent_with_one() {
        let items = vec![("do", "verb")];
        let mut merkle = merkle_build_test(items).unwrap();

        assert_eq!(merkle.remove(b"does_not_exist").unwrap(), None);
        assert_eq!(&*merkle.get_value(b"do").unwrap().unwrap(), b"verb");
    }
}
