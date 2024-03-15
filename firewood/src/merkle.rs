// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.
use crate::nibbles::Nibbles;
use crate::shale::compact::CompactSpace;
use crate::shale::CachedStore;
use crate::shale::{self, disk_address::DiskAddress, ObjWriteSizeError, ShaleError};
use crate::storage::{StoreRevMut, StoreRevShared};
use crate::v2::api;
use futures::{StreamExt, TryStreamExt};
use sha3::Digest;
use std::{
    collections::HashMap, future::ready, io::Write, iter::once, marker::PhantomData, sync::OnceLock,
};
use thiserror::Error;

mod node;
pub mod proof;
mod stream;
mod trie_hash;

pub use node::{BinarySerde, Bincode, BranchNode, EncodedNode, LeafNode, Node, NodeType, Path};
pub use proof::{Proof, ProofError};
pub use stream::MerkleKeyValueStream;
pub use trie_hash::{TrieHash, TRIE_HASH_LEN};

use self::stream::PathIterator;

type NodeObjRef<'a> = shale::ObjRef<'a, Node>;
type ParentRefs<'a> = Vec<(NodeObjRef<'a>, u8)>;
type ParentAddresses = Vec<(DiskAddress, u8)>;

pub type Key = Box<[u8]>;
type Value = Vec<u8>;

#[derive(Debug, Error)]
pub enum MerkleError {
    #[error("merkle datastore error: {0:?}")]
    Shale(#[from] ShaleError),
    #[error("read only")]
    ReadOnly,
    #[error("node not a branch node")]
    NotBranchNode,
    #[error("format error: {0:?}")]
    Format(#[from] std::io::Error),
    #[error("parent should not be a leaf branch")]
    ParentLeafBranch,
    #[error("removing internal node references failed")]
    UnsetInternal,
    #[error("error updating nodes: {0}")]
    WriteError(#[from] ObjWriteSizeError),
    #[error("merkle serde error: {0}")]
    BinarySerdeError(String),
}

macro_rules! write_node {
    ($self: expr, $r: expr, $modify: expr, $parents: expr, $deleted: expr) => {
        if let Err(_) = $r.write($modify) {
            let ptr = $self.put_node($r.clone())?.as_ptr();
            set_parent(ptr, $parents);
            $deleted.push($r.as_ptr());
            true
        } else {
            false
        }
    };
}

#[derive(Debug)]
pub struct Merkle<S, T> {
    store: CompactSpace<Node, S>,
    phantom: PhantomData<T>,
}

impl<T> From<Merkle<StoreRevMut, T>> for Merkle<StoreRevShared, T> {
    fn from(value: Merkle<StoreRevMut, T>) -> Self {
        let store = value.store.into();
        Merkle {
            store,
            phantom: PhantomData,
        }
    }
}

impl<S: CachedStore, T> Merkle<S, T> {
    pub fn get_node(&self, ptr: DiskAddress) -> Result<NodeObjRef, MerkleError> {
        self.store.get_item(ptr).map_err(Into::into)
    }

    pub fn put_node(&self, node: Node) -> Result<NodeObjRef, MerkleError> {
        self.store.put_item(node, 0).map_err(Into::into)
    }

    fn free_node(&mut self, ptr: DiskAddress) -> Result<(), MerkleError> {
        self.store.free_item(ptr).map_err(Into::into)
    }
}

impl<'de, S, T> Merkle<S, T>
where
    S: CachedStore,
    T: BinarySerde,
    EncodedNode<T>: serde::Serialize + serde::Deserialize<'de>,
{
    pub const fn new(store: CompactSpace<Node, S>) -> Self {
        Self {
            store,
            phantom: PhantomData,
        }
    }

    // TODO: use `encode` / `decode` instead of `node.encode` / `node.decode` after extention node removal.
    #[allow(dead_code)]
    fn encode(&self, node: &NodeType) -> Result<Vec<u8>, MerkleError> {
        let encoded = match node {
            NodeType::Leaf(n) => {
                let children: [Option<Vec<u8>>; BranchNode::MAX_CHILDREN] = Default::default();
                EncodedNode {
                    partial_path: n.partial_path.clone(),
                    children: Box::new(children),
                    value: n.value.clone().into(),
                    phantom: PhantomData,
                }
            }

            NodeType::Branch(n) => {
                // pair up DiskAddresses with encoded children and pick the right one
                let encoded_children = n.chd().iter().zip(n.children_encoded.iter());
                let children = encoded_children
                    .map(|(child_addr, encoded_child)| {
                        child_addr
                            // if there's a child disk address here, get the encoded bytes
                            .map(|addr| {
                                self.get_node(addr)
                                    .and_then(|node| self.encode(node.inner()))
                            })
                            // or look for the pre-fetched bytes
                            .or_else(|| encoded_child.as_ref().map(|child| Ok(child.to_vec())))
                            .transpose()
                    })
                    .collect::<Result<Vec<Option<Vec<u8>>>, MerkleError>>()?
                    .try_into()
                    .expect("MAX_CHILDREN will always be yielded");

                EncodedNode {
                    partial_path: n.partial_path.clone(),
                    children,
                    value: n.value.clone(),
                    phantom: PhantomData,
                }
            }
        };

        T::serialize(&encoded).map_err(|e| MerkleError::BinarySerdeError(e.to_string()))
    }

    #[allow(dead_code)]
    fn decode(&self, buf: &'de [u8]) -> Result<NodeType, MerkleError> {
        let encoded: EncodedNode<T> =
            T::deserialize(buf).map_err(|e| MerkleError::BinarySerdeError(e.to_string()))?;

        if encoded.children.iter().all(|b| b.is_none()) {
            // This is a leaf node
            return Ok(NodeType::Leaf(LeafNode::new(
                encoded.partial_path,
                encoded.value.expect("leaf nodes must always have a value"),
            )));
        }

        Ok(NodeType::Branch(
            BranchNode {
                partial_path: encoded.partial_path,
                children: [None; BranchNode::MAX_CHILDREN],
                value: encoded.value,
                children_encoded: *encoded.children,
            }
            .into(),
        ))
    }
}

impl<S: CachedStore, T> Merkle<S, T> {
    pub fn init_root(&self) -> Result<DiskAddress, MerkleError> {
        self.store
            .put_item(
                Node::from_branch(BranchNode {
                    partial_path: vec![].into(),
                    children: [None; BranchNode::MAX_CHILDREN],
                    value: None,
                    children_encoded: Default::default(),
                }),
                Node::max_branch_node_size(),
            )
            .map_err(MerkleError::Shale)
            .map(|node| node.as_ptr())
    }

    pub fn empty_root() -> &'static TrieHash {
        static V: OnceLock<TrieHash> = OnceLock::new();
        #[allow(clippy::unwrap_used)]
        V.get_or_init(|| {
            TrieHash(
                hex::decode("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
                    .unwrap()
                    .try_into()
                    .unwrap(),
            )
        })
    }

    pub fn root_hash(&self, sentinel: DiskAddress) -> Result<TrieHash, MerkleError> {
        let root = self
            .get_node(sentinel)?
            .inner
            .as_branch()
            .ok_or(MerkleError::NotBranchNode)?
            .children[0];
        Ok(if let Some(root) = root {
            let mut node = self.get_node(root)?;
            let res = *node.get_root_hash(&self.store);
            #[allow(clippy::unwrap_used)]
            if node.is_dirty() {
                node.write(|_| {}).unwrap();
                node.set_dirty(false);
            }
            res
        } else {
            *Self::empty_root()
        })
    }

    fn dump_(&self, u: DiskAddress, w: &mut dyn Write) -> Result<(), MerkleError> {
        let u_ref = self.get_node(u)?;

        let hash = match u_ref.root_hash.get() {
            Some(h) => h,
            None => u_ref.get_root_hash(&self.store),
        };

        write!(w, "{u:?} => {}: ", hex::encode(**hash))?;

        match &u_ref.inner {
            NodeType::Branch(n) => {
                writeln!(w, "{n:?}")?;
                for c in n.children.iter().flatten() {
                    self.dump_(*c, w)?
                }
            }
            #[allow(clippy::unwrap_used)]
            NodeType::Leaf(n) => writeln!(w, "{n:?}").unwrap(),
        }

        Ok(())
    }

    pub fn dump(&self, root: DiskAddress, w: &mut dyn Write) -> Result<(), MerkleError> {
        if root.is_null() {
            write!(w, "<Empty>")?;
        } else {
            self.dump_(root, w)?;
        };
        Ok(())
    }

    pub fn insert<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        val: Vec<u8>,
        root: DiskAddress,
    ) -> Result<(), MerkleError> {
        let (parents, deleted) = self.insert_and_return_updates(key, val, root)?;

        for mut r in parents {
            r.write(|u| u.rehash())?;
        }

        for ptr in deleted {
            self.free_node(ptr)?
        }

        Ok(())
    }

    fn insert_and_return_updates<K: AsRef<[u8]>>(
        &self,
        key: K,
        val: Vec<u8>,
        root: DiskAddress,
    ) -> Result<(impl Iterator<Item = NodeObjRef>, Vec<DiskAddress>), MerkleError> {
        // as we split a node, we need to track deleted nodes and parents
        let mut deleted = Vec::new();
        let mut parents = Vec::new();

        // we use Nibbles::<1> so that 1 zero nibble is at the front
        // this is for the sentinel node, which avoids moving the root
        // and always only has one child
        let mut key_nibbles = Nibbles::<1>::new(key.as_ref()).into_iter();

        let mut node = self.get_node(root)?;

        // walk down the merkle tree starting from next_node, currently the root
        // return None if the value is inserted
        let next_node_and_val = loop {
            let Some(mut next_nibble) = key_nibbles.next() else {
                break Some((node, val));
            };

            let (node_ref, next_node_ptr) = match &node.inner {
                // For a Branch node, we look at the child pointer. If it points
                // to another node, we walk down that. Otherwise, we can store our
                // value as a leaf and we're done
                NodeType::Leaf(n) => {
                    // TODO: avoid extra allocation
                    let key_remainder = once(next_nibble)
                        .chain(key_nibbles.clone())
                        .collect::<Vec<_>>();

                    let overlap = PrefixOverlap::from(&n.partial_path, &key_remainder);

                    #[allow(clippy::indexing_slicing)]
                    match (overlap.unique_a.len(), overlap.unique_b.len()) {
                        // same node, overwrite the value
                        (0, 0) => {
                            self.update_value_and_move_node_if_larger(
                                (&mut parents, &mut deleted),
                                node,
                                val,
                            )?;
                        }

                        // new node is a child of the old node
                        (0, _) => {
                            let (new_leaf_index, new_leaf_path) = {
                                let (index, path) = overlap.unique_b.split_at(1);
                                (index[0], path.to_vec())
                            };

                            let new_leaf = Node::from_leaf(LeafNode::new(Path(new_leaf_path), val));

                            let new_leaf = self.put_node(new_leaf)?.as_ptr();

                            let mut children = [None; BranchNode::MAX_CHILDREN];
                            children[new_leaf_index as usize] = Some(new_leaf);

                            let new_branch = BranchNode {
                                partial_path: Path(overlap.shared.to_vec()),
                                children,
                                value: n.value.clone().into(),
                                children_encoded: Default::default(),
                            };

                            let new_branch = Node::from_branch(new_branch);

                            let new_branch = self.put_node(new_branch)?.as_ptr();

                            set_parent(new_branch, &mut parents);

                            deleted.push(node.as_ptr());
                        }

                        // old node is a child of the new node
                        (_, 0) => {
                            let (old_leaf_index, old_leaf_path) = {
                                let (index, path) = overlap.unique_a.split_at(1);
                                (index[0], path.to_vec())
                            };

                            let new_branch_path = overlap.shared.to_vec();

                            let old_leaf = self
                                .update_path_and_move_node_if_larger(
                                    (&mut parents, &mut deleted),
                                    node,
                                    Path(old_leaf_path.to_vec()),
                                )?
                                .as_ptr();

                            let mut new_branch = BranchNode {
                                partial_path: Path(new_branch_path),
                                children: [None; BranchNode::MAX_CHILDREN],
                                value: Some(val),
                                children_encoded: Default::default(),
                            };

                            new_branch.children[old_leaf_index as usize] = Some(old_leaf);

                            let node = Node::from_branch(new_branch);
                            let node = self.put_node(node)?.as_ptr();

                            set_parent(node, &mut parents);
                        }

                        // nodes are siblings
                        _ => {
                            let (old_leaf_index, old_leaf_path) = {
                                let (index, path) = overlap.unique_a.split_at(1);
                                (index[0], path.to_vec())
                            };

                            let (new_leaf_index, new_leaf_path) = {
                                let (index, path) = overlap.unique_b.split_at(1);
                                (index[0], path.to_vec())
                            };

                            let new_branch_path = overlap.shared.to_vec();

                            let old_leaf = self
                                .update_path_and_move_node_if_larger(
                                    (&mut parents, &mut deleted),
                                    node,
                                    Path(old_leaf_path.to_vec()),
                                )?
                                .as_ptr();

                            let new_leaf = Node::from_leaf(LeafNode::new(Path(new_leaf_path), val));

                            let new_leaf = self.put_node(new_leaf)?.as_ptr();

                            let mut new_branch = BranchNode {
                                partial_path: Path(new_branch_path),
                                children: [None; BranchNode::MAX_CHILDREN],
                                value: None,
                                children_encoded: Default::default(),
                            };

                            new_branch.children[old_leaf_index as usize] = Some(old_leaf);
                            new_branch.children[new_leaf_index as usize] = Some(new_leaf);

                            let node = Node::from_branch(new_branch);
                            let node = self.put_node(node)?.as_ptr();

                            set_parent(node, &mut parents);
                        }
                    }

                    break None;
                }

                NodeType::Branch(n) if n.partial_path.len() == 0 => {
                    #[allow(clippy::indexing_slicing)]
                    match n.children[next_nibble as usize] {
                        Some(c) => (node, c),
                        None => {
                            // insert the leaf to the empty slot
                            // create a new leaf
                            let leaf_ptr = self
                                .put_node(Node::from_leaf(LeafNode::new(
                                    Path(key_nibbles.collect()),
                                    val,
                                )))?
                                .as_ptr();

                            // set the current child to point to this leaf
                            #[allow(clippy::indexing_slicing)]
                            node.write(|node| {
                                node.as_branch_mut().children[next_nibble as usize] =
                                    Some(leaf_ptr);
                                node.rehash();
                            })?;

                            break None;
                        }
                    }
                }

                NodeType::Branch(n) => {
                    // TODO: avoid extra allocation
                    let key_remainder = once(next_nibble)
                        .chain(key_nibbles.clone())
                        .collect::<Vec<_>>();

                    let overlap = PrefixOverlap::from(&n.partial_path, &key_remainder);

                    #[allow(clippy::indexing_slicing)]
                    match (overlap.unique_a.len(), overlap.unique_b.len()) {
                        // same node, overwrite the value
                        (0, 0) => {
                            self.update_value_and_move_node_if_larger(
                                (&mut parents, &mut deleted),
                                node,
                                val,
                            )?;
                            break None;
                        }

                        // new node is a child of the old node
                        (0, _) => {
                            let (new_leaf_index, new_leaf_path) = {
                                let (index, path) = overlap.unique_b.split_at(1);
                                (index[0], path)
                            };

                            (0..overlap.shared.len()).for_each(|_| {
                                key_nibbles.next();
                            });

                            next_nibble = new_leaf_index;

                            match n.children[next_nibble as usize] {
                                Some(ptr) => (node, ptr),
                                None => {
                                    let new_leaf = Node::from_leaf(LeafNode::new(
                                        Path(new_leaf_path.to_vec()),
                                        val,
                                    ));

                                    let new_leaf = self.put_node(new_leaf)?.as_ptr();

                                    #[allow(clippy::indexing_slicing)]
                                    node.write(|node| {
                                        node.as_branch_mut().children[next_nibble as usize] =
                                            Some(new_leaf);
                                        node.rehash();
                                    })?;

                                    break None;
                                }
                            }
                        }

                        // old node is a child of the new node
                        (_, 0) => {
                            let (old_branch_index, old_branch_path) = {
                                let (index, path) = overlap.unique_a.split_at(1);
                                (index[0], path.to_vec())
                            };

                            let new_branch_path = overlap.shared.to_vec();

                            let old_branch = self
                                .update_path_and_move_node_if_larger(
                                    (&mut parents, &mut deleted),
                                    node,
                                    Path(old_branch_path.to_vec()),
                                )?
                                .as_ptr();

                            let mut new_branch = BranchNode {
                                partial_path: Path(new_branch_path),
                                children: [None; BranchNode::MAX_CHILDREN],
                                value: Some(val),
                                children_encoded: Default::default(),
                            };

                            new_branch.children[old_branch_index as usize] = Some(old_branch);

                            let node = Node::from_branch(new_branch);
                            let node = self.put_node(node)?.as_ptr();

                            set_parent(node, &mut parents);

                            break None;
                        }

                        // nodes are siblings
                        _ => {
                            let (old_branch_index, old_branch_path) = {
                                let (index, path) = overlap.unique_a.split_at(1);
                                (index[0], path.to_vec())
                            };

                            let (new_leaf_index, new_leaf_path) = {
                                let (index, path) = overlap.unique_b.split_at(1);
                                (index[0], path.to_vec())
                            };

                            let new_branch_path = overlap.shared.to_vec();

                            let old_branch = self
                                .update_path_and_move_node_if_larger(
                                    (&mut parents, &mut deleted),
                                    node,
                                    Path(old_branch_path.to_vec()),
                                )?
                                .as_ptr();

                            let new_leaf = Node::from_leaf(LeafNode::new(Path(new_leaf_path), val));

                            let new_leaf = self.put_node(new_leaf)?.as_ptr();

                            let mut new_branch = BranchNode {
                                partial_path: Path(new_branch_path),
                                children: [None; BranchNode::MAX_CHILDREN],
                                value: None,
                                children_encoded: Default::default(),
                            };

                            new_branch.children[old_branch_index as usize] = Some(old_branch);
                            new_branch.children[new_leaf_index as usize] = Some(new_leaf);

                            let node = Node::from_branch(new_branch);
                            let node = self.put_node(node)?.as_ptr();

                            set_parent(node, &mut parents);

                            break None;
                        }
                    }
                }
            };

            // push another parent, and follow the next pointer
            parents.push((node_ref, next_nibble));
            node = self.get_node(next_node_ptr)?;
        };

        if let Some((mut node, val)) = next_node_and_val {
            // we walked down the tree and reached the end of the key,
            // but haven't inserted the value yet
            let mut info = None;
            let u_ptr = {
                write_node!(
                    self,
                    node,
                    |u| {
                        info = match &mut u.inner {
                            NodeType::Branch(n) => {
                                n.value = Some(val);
                                None
                            }
                            NodeType::Leaf(n) => {
                                if n.partial_path.len() == 0 {
                                    n.value = val;

                                    None
                                } else {
                                    #[allow(clippy::indexing_slicing)]
                                    let idx = n.partial_path[0];
                                    #[allow(clippy::indexing_slicing)]
                                    (n.partial_path = Path(n.partial_path[1..].to_vec()));
                                    u.rehash();

                                    Some((idx, true, None, val))
                                }
                            }
                        };

                        u.rehash()
                    },
                    &mut parents,
                    &mut deleted
                );

                node.as_ptr()
            };

            if let Some((idx, more, ext, val)) = info {
                let mut chd = [None; BranchNode::MAX_CHILDREN];

                let c_ptr = if more {
                    u_ptr
                } else {
                    deleted.push(u_ptr);
                    #[allow(clippy::unwrap_used)]
                    ext.unwrap()
                };

                #[allow(clippy::indexing_slicing)]
                (chd[idx as usize] = Some(c_ptr));

                let branch = self
                    .put_node(Node::from_branch(BranchNode {
                        partial_path: vec![].into(),
                        children: chd,
                        value: Some(val),
                        children_encoded: Default::default(),
                    }))?
                    .as_ptr();

                set_parent(branch, &mut parents);
            }
        }

        Ok((parents.into_iter().rev().map(|(node, _)| node), deleted))
    }

    pub fn remove<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<Vec<u8>>, MerkleError> {
        if root.is_null() {
            return Ok(None);
        }

        let mut deleted = Vec::new();

        let value = {
            let (node, mut parents) =
                self.get_node_and_parents_by_key(self.get_node(root)?, key)?;

            let Some(mut node) = node else {
                return Ok(None);
            };

            let value = match &node.inner {
                NodeType::Branch(branch) => {
                    let value = branch.value.clone();
                    if value.is_none() {
                        return Ok(None);
                    }

                    let children: Vec<_> = branch
                        .children
                        .iter()
                        .enumerate()
                        .filter_map(|(i, child)| child.map(|child| (i, child)))
                        .collect();

                    // don't change the sentinel node
                    if children.len() == 1 && !parents.is_empty() {
                        let branch_path = &branch.partial_path.0;

                        #[allow(clippy::indexing_slicing)]
                        let (child_index, child) = children[0];
                        let mut child = self.get_node(child)?;

                        child.write(|child| {
                            let child_path = child.inner.path_mut();
                            let path = branch_path
                                .iter()
                                .copied()
                                .chain(once(child_index as u8))
                                .chain(child_path.0.iter().copied())
                                .collect();
                            *child_path = Path(path);

                            child.rehash();
                        })?;

                        set_parent(child.as_ptr(), &mut parents);

                        deleted.push(node.as_ptr());
                    } else {
                        node.write(|node| {
                            node.as_branch_mut().value = None;
                            node.rehash();
                        })?
                    }

                    value
                }

                NodeType::Leaf(n) => {
                    let value = Some(n.value.clone());

                    // TODO: handle unwrap better
                    deleted.push(node.as_ptr());

                    let (mut parent, child_index) = parents.pop().expect("parents is never empty");

                    #[allow(clippy::indexing_slicing)]
                    parent.write(|parent| {
                        parent.as_branch_mut().children[child_index as usize] = None;
                    })?;

                    let branch = parent
                        .inner
                        .as_branch()
                        .expect("parents are always branch nodes");

                    let children: Vec<_> = branch
                        .children
                        .iter()
                        .enumerate()
                        .filter_map(|(i, child)| child.map(|child| (i, child)))
                        .collect();

                    match (children.len(), &branch.value, !parents.is_empty()) {
                        // node is invalid, all single-child nodes should have a value
                        (1, None, true) => {
                            let parent_path = &branch.partial_path.0;

                            #[allow(clippy::indexing_slicing)]
                            let (child_index, child) = children[0];
                            let child = self.get_node(child)?;

                            // TODO:
                            // there's an optimization here for when the paths are the same length
                            // and that clone isn't great but ObjRef causes problems
                            // we can't write directly to the child because we could be changing its size
                            let new_child = match child.inner.clone() {
                                NodeType::Branch(mut child) => {
                                    let path = parent_path
                                        .iter()
                                        .copied()
                                        .chain(once(child_index as u8))
                                        .chain(child.partial_path.0.iter().copied())
                                        .collect();

                                    child.partial_path = Path(path);

                                    Node::from_branch(child)
                                }
                                NodeType::Leaf(mut child) => {
                                    let path = parent_path
                                        .iter()
                                        .copied()
                                        .chain(once(child_index as u8))
                                        .chain(child.partial_path.0.iter().copied())
                                        .collect();

                                    child.partial_path = Path(path);

                                    Node::from_leaf(child)
                                }
                            };

                            let child = self.put_node(new_child)?.as_ptr();

                            set_parent(child, &mut parents);

                            deleted.push(parent.as_ptr());
                        }

                        // branch nodes shouldn't have no children
                        (0, Some(value), true) => {
                            let leaf = Node::from_leaf(LeafNode::new(
                                Path(branch.partial_path.0.clone()),
                                value.clone(),
                            ));

                            let leaf = self.put_node(leaf)?.as_ptr();
                            set_parent(leaf, &mut parents);

                            deleted.push(parent.as_ptr());
                        }

                        _ => parent.write(|parent| parent.rehash())?,
                    }

                    value
                }
            };

            for (mut parent, _) in parents {
                parent.write(|u| u.rehash())?;
            }

            value
        };

        for ptr in deleted.into_iter() {
            self.free_node(ptr)?;
        }

        Ok(value)
    }

    fn remove_tree_(
        &self,
        u: DiskAddress,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<(), MerkleError> {
        let u_ref = self.get_node(u)?;
        match &u_ref.inner {
            NodeType::Branch(n) => {
                for c in n.children.iter().flatten() {
                    self.remove_tree_(*c, deleted)?
                }
            }
            NodeType::Leaf(_) => (),
        }
        deleted.push(u);
        Ok(())
    }

    pub fn remove_tree(&mut self, root: DiskAddress) -> Result<(), MerkleError> {
        let mut deleted = Vec::new();
        if root.is_null() {
            return Ok(());
        }
        self.remove_tree_(root, &mut deleted)?;
        for ptr in deleted.into_iter() {
            self.free_node(ptr)?;
        }
        Ok(())
    }

    fn get_node_by_key<'a, K: AsRef<[u8]>>(
        &'a self,
        node_ref: NodeObjRef<'a>,
        key: K,
    ) -> Result<Option<NodeObjRef<'a>>, MerkleError> {
        let key = key.as_ref();
        let path_iter = self.path_iter(node_ref, key);

        match path_iter.last() {
            None => Ok(None),
            Some(Err(e)) => Err(e),
            Some(Ok((node_key, node))) => {
                let key_nibbles = Nibbles::<0>::new(key).into_iter();
                if key_nibbles.eq(node_key.iter().copied()) {
                    Ok(Some(node))
                } else {
                    Ok(None)
                }
            }
        }
    }

    fn get_node_and_parents_by_key<'a, K: AsRef<[u8]>>(
        &'a self,
        node_ref: NodeObjRef<'a>,
        key: K,
    ) -> Result<(Option<NodeObjRef<'a>>, ParentRefs<'a>), MerkleError> {
        let mut parents = Vec::new();
        let node_ref = self.get_node_by_key_with_callbacks(
            node_ref,
            key,
            |_, _| {},
            |node_ref, nib| {
                parents.push((node_ref, nib));
            },
        )?;

        Ok((node_ref, parents))
    }

    fn get_node_and_parent_addresses_by_key<'a, K: AsRef<[u8]>>(
        &'a self,
        node_ref: NodeObjRef<'a>,
        key: K,
    ) -> Result<(Option<NodeObjRef<'a>>, ParentAddresses), MerkleError> {
        let mut parents = Vec::new();
        let node_ref = self.get_node_by_key_with_callbacks(
            node_ref,
            key,
            |_, _| {},
            |node_ref, nib| {
                parents.push((node_ref.into_ptr(), nib));
            },
        )?;

        Ok((node_ref, parents))
    }

    fn get_node_by_key_with_callbacks<'a, K: AsRef<[u8]>>(
        &'a self,
        mut node_ref: NodeObjRef<'a>,
        key: K,
        mut start_loop_callback: impl FnMut(DiskAddress, u8),
        mut end_loop_callback: impl FnMut(NodeObjRef<'a>, u8),
    ) -> Result<Option<NodeObjRef<'a>>, MerkleError> {
        let mut key_nibbles = Nibbles::<1>::new(key.as_ref()).into_iter();

        loop {
            let Some(mut nib) = key_nibbles.next() else {
                break;
            };

            start_loop_callback(node_ref.as_ptr(), nib);

            let next_ptr = match &node_ref.inner {
                #[allow(clippy::indexing_slicing)]
                NodeType::Branch(n) if n.partial_path.len() == 0 => {
                    match n.children[nib as usize] {
                        Some(c) => c,
                        None => return Ok(None),
                    }
                }
                NodeType::Branch(n) => {
                    let mut n_path_iter = n.partial_path.iter().copied();

                    if n_path_iter.next() != Some(nib) {
                        return Ok(None);
                    }

                    let path_matches = n_path_iter
                        .map(Some)
                        .all(|n_path_nibble| key_nibbles.next() == n_path_nibble);

                    if !path_matches {
                        return Ok(None);
                    }

                    nib = if let Some(nib) = key_nibbles.next() {
                        nib
                    } else {
                        return Ok(if n.value.is_some() {
                            Some(node_ref)
                        } else {
                            None
                        });
                    };

                    #[allow(clippy::indexing_slicing)]
                    match n.children[nib as usize] {
                        Some(c) => c,
                        None => return Ok(None),
                    }
                }
                NodeType::Leaf(n) => {
                    let node_ref = if once(nib)
                        .chain(key_nibbles)
                        .eq(n.partial_path.iter().copied())
                    {
                        Some(node_ref)
                    } else {
                        None
                    };

                    return Ok(node_ref);
                }
            };

            end_loop_callback(node_ref, nib);

            node_ref = self.get_node(next_ptr)?;
        }

        // when we're done iterating over nibbles, check if the node we're at has a value
        let node_ref = match &node_ref.inner {
            NodeType::Branch(n) if n.value.as_ref().is_some() && n.partial_path.is_empty() => {
                Some(node_ref)
            }
            NodeType::Leaf(n) if n.partial_path.len() == 0 => Some(node_ref),
            _ => None,
        };

        Ok(node_ref)
    }

    pub fn get_mut<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<RefMut<S, T>>, MerkleError> {
        if root.is_null() {
            return Ok(None);
        }

        let (ptr, parents) = {
            let root_node = self.get_node(root)?;
            let (node_ref, parents) = self.get_node_and_parent_addresses_by_key(root_node, key)?;

            (node_ref.map(|n| n.into_ptr()), parents)
        };

        Ok(ptr.map(|ptr| RefMut::new(ptr, parents, self)))
    }

    /// Constructs a merkle proof for key. The result contains all encoded nodes
    /// on the path to the value at key. The value itself is also included in the
    /// last node and can be retrieved by verifying the proof.
    ///
    /// If the trie does not contain a value for key, the returned proof contains
    /// all nodes of the longest existing prefix of the key, ending with the node
    /// that proves the absence of the key (at least the root node).
    pub fn prove<K>(&self, key: K, root: DiskAddress) -> Result<Proof<Vec<u8>>, MerkleError>
    where
        K: AsRef<[u8]>,
    {
        let mut proofs = HashMap::new();
        if root.is_null() {
            return Ok(Proof(proofs));
        }

        let sentinel_node = self.get_node(root)?;

        let path_iter = self.path_iter(sentinel_node, key.as_ref());

        let nodes = path_iter
            .map(|result| result.map(|(_, node)| node))
            .collect::<Result<Vec<NodeObjRef>, MerkleError>>()?;

        // Get the hashes of the nodes.
        for node in nodes.into_iter() {
            let encoded = node.get_encoded(&self.store);
            let hash: [u8; TRIE_HASH_LEN] = sha3::Keccak256::digest(encoded).into();
            proofs.insert(hash, encoded.to_vec());
        }
        Ok(Proof(proofs))
    }

    pub fn get<K: AsRef<[u8]>>(
        &self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<Ref>, MerkleError> {
        if root.is_null() {
            return Ok(None);
        }

        let root_node = self.get_node(root)?;
        let node_ref = self.get_node_by_key(root_node, key)?;

        Ok(node_ref.map(Ref))
    }

    pub fn flush_dirty(&self) -> Option<()> {
        self.store.flush_dirty()
    }

    pub fn path_iter<'a, 'b>(
        &'a self,
        sentinel_node: NodeObjRef<'a>,
        key: &'b [u8],
    ) -> PathIterator<'_, 'b, S, T> {
        PathIterator::new(self, sentinel_node, key)
    }

    pub(crate) fn key_value_iter(&self, root: DiskAddress) -> MerkleKeyValueStream<'_, S, T> {
        MerkleKeyValueStream::new(self, root)
    }

    pub(crate) fn key_value_iter_from_key(
        &self,
        root: DiskAddress,
        key: Key,
    ) -> MerkleKeyValueStream<'_, S, T> {
        MerkleKeyValueStream::from_key(self, root, key)
    }

    pub(super) async fn range_proof<K: api::KeyType + Send + Sync>(
        &self,
        root: DiskAddress,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<usize>,
    ) -> Result<Option<api::RangeProof<Vec<u8>, Vec<u8>>>, api::Error> {
        if let (Some(k1), Some(k2)) = (&first_key, &last_key) {
            if k1.as_ref() > k2.as_ref() {
                return Err(api::Error::InvalidRange {
                    first_key: k1.as_ref().to_vec(),
                    last_key: k2.as_ref().to_vec(),
                });
            }
        }

        // limit of 0 is always an empty RangeProof
        if limit == Some(0) {
            return Ok(None);
        }

        let mut stream = match first_key {
            // TODO: fix the call-site to force the caller to do the allocation
            Some(key) => {
                self.key_value_iter_from_key(root, key.as_ref().to_vec().into_boxed_slice())
            }
            None => self.key_value_iter(root),
        };

        // fetch the first key from the stream
        let first_result = stream.next().await;

        // transpose the Option<Result<T, E>> to Result<Option<T>, E>
        // If this is an error, the ? operator will return it
        let Some((first_key, first_value)) = first_result.transpose()? else {
            // nothing returned, either the trie is empty or the key wasn't found
            return Ok(None);
        };

        let first_key_proof = self
            .prove(&first_key, root)
            .map_err(|e| api::Error::InternalError(Box::new(e)))?;
        let limit = limit.map(|old_limit| old_limit - 1);

        let mut middle = vec![(first_key.into_vec(), first_value)];

        // we stop streaming if either we hit the limit or the key returned was larger
        // than the largest key requested
        #[allow(clippy::unwrap_used)]
        middle.extend(
            stream
                .take(limit.unwrap_or(usize::MAX))
                .take_while(|kv_result| {
                    // no last key asked for, so keep going
                    let Some(last_key) = last_key.as_ref() else {
                        return ready(true);
                    };

                    // return the error if there was one
                    let Ok(kv) = kv_result else {
                        return ready(true);
                    };

                    // keep going if the key returned is less than the last key requested
                    ready(&*kv.0 <= last_key.as_ref())
                })
                .map(|kv_result| kv_result.map(|(k, v)| (k.into_vec(), v)))
                .try_collect::<Vec<(Vec<u8>, Vec<u8>)>>()
                .await?,
        );

        // remove the last key from middle and do a proof on it
        let last_key_proof = match middle.last() {
            None => {
                return Ok(Some(api::RangeProof {
                    first_key_proof: first_key_proof.clone(),
                    middle: vec![],
                    last_key_proof: first_key_proof,
                }))
            }
            Some((last_key, _)) => self
                .prove(last_key, root)
                .map_err(|e| api::Error::InternalError(Box::new(e)))?,
        };

        Ok(Some(api::RangeProof {
            first_key_proof,
            middle,
            last_key_proof,
        }))
    }

    /// Try to update the [NodeObjRef]'s path in-place. If the update fails because the node can no longer fit at its old address,
    /// then the old address is marked for deletion and the [Node] (with its update) is inserted at a new address.
    fn update_path_and_move_node_if_larger<'a>(
        &'a self,
        (parents, to_delete): (&mut [(NodeObjRef, u8)], &mut Vec<DiskAddress>),
        mut node: NodeObjRef<'a>,
        path: Path,
    ) -> Result<NodeObjRef<'a>, MerkleError> {
        let write_result = node.write(|node| {
            node.inner_mut().set_path(path);
            node.rehash();
        });

        self.move_node_if_write_failed((parents, to_delete), node, write_result)
    }

    /// Try to update the [NodeObjRef]'s value in-place. If the update fails because the node can no longer fit at its old address,
    /// then the old address is marked for deletion and the [Node] (with its update) is inserted at a new address.
    fn update_value_and_move_node_if_larger<'a>(
        &'a self,
        (parents, to_delete): (&mut [(NodeObjRef, u8)], &mut Vec<DiskAddress>),
        mut node: NodeObjRef<'a>,
        value: Vec<u8>,
    ) -> Result<NodeObjRef, MerkleError> {
        let write_result = node.write(|node| {
            node.inner_mut().set_value(value);
            node.rehash();
        });

        self.move_node_if_write_failed((parents, to_delete), node, write_result)
    }

    /// Checks if the `write_result` is an [ObjWriteSizeError]. If it is, then the `node` is moved to a new address and the old address is marked for deletion.
    fn move_node_if_write_failed<'a>(
        &'a self,
        (parents, deleted): (&mut [(NodeObjRef, u8)], &mut Vec<DiskAddress>),
        mut node: NodeObjRef<'a>,
        write_result: Result<(), ObjWriteSizeError>,
    ) -> Result<NodeObjRef<'a>, MerkleError> {
        if let Err(ObjWriteSizeError) = write_result {
            let old_node_address = node.as_ptr();
            node = self.put_node(node.into_inner())?;
            deleted.push(old_node_address);

            set_parent(node.as_ptr(), parents);
        }

        Ok(node)
    }
}

fn set_parent(new_chd: DiskAddress, parents: &mut [(NodeObjRef, u8)]) {
    #[allow(clippy::unwrap_used)]
    let (p_ref, idx) = parents.last_mut().unwrap();
    #[allow(clippy::unwrap_used)]
    p_ref
        .write(|p| {
            match &mut p.inner {
                #[allow(clippy::indexing_slicing)]
                NodeType::Branch(pp) => pp.children[*idx as usize] = Some(new_chd),
                _ => unreachable!(),
            }
            p.rehash();
        })
        .unwrap();
}

pub struct Ref<'a>(NodeObjRef<'a>);

pub struct RefMut<'a, S, T> {
    ptr: DiskAddress,
    parents: ParentAddresses,
    merkle: &'a mut Merkle<S, T>,
}

impl<'a> std::ops::Deref for Ref<'a> {
    type Target = [u8];
    #[allow(clippy::unwrap_used)]
    fn deref(&self) -> &[u8] {
        match &self.0.inner {
            NodeType::Branch(n) => n.value.as_ref().unwrap(),
            NodeType::Leaf(n) => &n.value,
        }
    }
}

impl<'a, S, T> RefMut<'a, S, T> {
    fn new(ptr: DiskAddress, parents: ParentAddresses, merkle: &'a mut Merkle<S, T>) -> Self {
        Self {
            ptr,
            parents,
            merkle,
        }
    }
}

impl<'a, S: CachedStore, T> RefMut<'a, S, T> {
    #[allow(clippy::unwrap_used)]
    pub fn get(&self) -> Ref {
        Ref(self.merkle.get_node(self.ptr).unwrap())
    }

    pub fn write(&mut self, modify: impl FnOnce(&mut Vec<u8>)) -> Result<(), MerkleError> {
        let mut deleted = Vec::new();
        #[allow(clippy::unwrap_used)]
        {
            let mut u_ref = self.merkle.get_node(self.ptr).unwrap();
            #[allow(clippy::unwrap_used)]
            let mut parents: Vec<_> = self
                .parents
                .iter()
                .map(|(ptr, nib)| (self.merkle.get_node(*ptr).unwrap(), *nib))
                .collect();
            write_node!(
                self.merkle,
                u_ref,
                |u| {
                    #[allow(clippy::unwrap_used)]
                    modify(match &mut u.inner {
                        NodeType::Branch(n) => n.value.as_mut().unwrap(),
                        NodeType::Leaf(n) => &mut n.value,
                    });
                    u.rehash()
                },
                &mut parents,
                &mut deleted
            );
        }
        for ptr in deleted.into_iter() {
            self.merkle.free_node(ptr)?;
        }
        Ok(())
    }
}

// nibbles, high bits first, then low bits
pub const fn to_nibble_array(x: u8) -> [u8; 2] {
    [x >> 4, x & 0b_0000_1111]
}

/// Returns an iterator where each element is the result of combining
/// 2 nibbles of `nibbles`. If `nibbles` is odd length, panics in
/// debug mode and drops the final nibble in release mode.
pub fn nibbles_to_bytes_iter(nibbles: &[u8]) -> impl Iterator<Item = u8> + '_ {
    debug_assert_eq!(nibbles.len() & 1, 0);
    #[allow(clippy::indexing_slicing)]
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
        let mut split_index = 0;

        #[allow(clippy::indexing_slicing)]
        for i in 0..std::cmp::min(a.len(), b.len()) {
            if a[i] != b[i] {
                break;
            }

            split_index += 1;
        }

        let (shared, unique_a) = a.split_at(split_index);
        let (_, unique_b) = b.split_at(split_index);

        Self {
            shared,
            unique_a,
            unique_b,
        }
    }
}

#[cfg(test)]
#[allow(clippy::indexing_slicing, clippy::unwrap_used)]
mod tests {
    use super::*;
    use crate::merkle::node::PlainCodec;
    use shale::{cached::InMemLinearStore, CachedStore};
    use test_case::test_case;

    fn leaf(path: Vec<u8>, value: Vec<u8>) -> Node {
        Node::from_leaf(LeafNode::new(Path(path), value))
    }

    #[test_case(vec![0x12, 0x34, 0x56], &[0x1, 0x2, 0x3, 0x4, 0x5, 0x6])]
    #[test_case(vec![0xc0, 0xff], &[0xc, 0x0, 0xf, 0xf])]
    fn to_nibbles(bytes: Vec<u8>, nibbles: &[u8]) {
        let n: Vec<_> = bytes.into_iter().flat_map(to_nibble_array).collect();
        assert_eq!(n, nibbles);
    }

    fn create_generic_test_merkle<'de, T>() -> Merkle<InMemLinearStore, T>
    where
        T: BinarySerde,
        EncodedNode<T>: serde::Serialize + serde::Deserialize<'de>,
    {
        const RESERVED: usize = 0x1000;

        let mut dm = shale::cached::InMemLinearStore::new(0x10000, 0);
        let compact_header = DiskAddress::null();
        dm.write(
            compact_header.into(),
            &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
                std::num::NonZeroUsize::new(RESERVED).unwrap(),
                std::num::NonZeroUsize::new(RESERVED).unwrap(),
            ))
            .unwrap(),
        )
        .unwrap();
        let compact_header = shale::StoredView::ptr_to_obj(
            &dm,
            compact_header,
            shale::compact::CompactHeader::MSIZE,
        )
        .unwrap();
        let mem_meta = dm;
        let mem_payload = InMemLinearStore::new(0x10000, 0x1);

        let cache = shale::ObjCache::new(1);
        let space =
            shale::compact::CompactSpace::new(mem_meta, mem_payload, compact_header, cache, 10, 16)
                .expect("CompactSpace init fail");

        Merkle::new(space)
    }

    pub(super) fn create_test_merkle() -> Merkle<InMemLinearStore, Bincode> {
        create_generic_test_merkle::<Bincode>()
    }

    fn branch(path: &[u8], value: &[u8], encoded_child: Option<Vec<u8>>) -> Node {
        let (path, value) = (path.to_vec(), value.to_vec());
        let path = Nibbles::<0>::new(&path);
        let path = Path(path.into_iter().collect());

        let children = Default::default();
        let value = if value.is_empty() { None } else { Some(value) };
        let mut children_encoded = <[Option<Vec<u8>>; BranchNode::MAX_CHILDREN]>::default();

        if let Some(child) = encoded_child {
            children_encoded[0] = Some(child);
        }

        Node::from_branch(BranchNode {
            partial_path: path,
            children,
            value,
            children_encoded,
        })
    }

    fn branch_without_value(path: &[u8], encoded_child: Option<Vec<u8>>) -> Node {
        let path = path.to_vec();
        let path = Nibbles::<0>::new(&path);
        let path = Path(path.into_iter().collect());

        let children = Default::default();
        // TODO: Properly test empty value
        let value = None;
        let mut children_encoded = <[Option<Vec<u8>>; BranchNode::MAX_CHILDREN]>::default();

        if let Some(child) = encoded_child {
            children_encoded[0] = Some(child);
        }

        Node::from_branch(BranchNode {
            partial_path: path,
            children,
            value,
            children_encoded,
        })
    }

    #[test_case(leaf(Vec::new(), Vec::new()) ; "empty leaf encoding")]
    #[test_case(leaf(vec![1, 2, 3], vec![4, 5]) ; "leaf encoding")]
    #[test_case(branch(b"", b"value", vec![1, 2, 3].into()) ; "branch with chd")]
    #[test_case(branch(b"", b"value", None); "branch without chd")]
    #[test_case(branch_without_value(b"", None); "branch without value and chd")]
    #[test_case(branch(b"", b"", None); "branch without path value or children")]
    #[test_case(branch(b"", b"value", None) ; "branch with value")]
    #[test_case(branch(&[2], b"", None); "branch with path")]
    #[test_case(branch(b"", b"", vec![1, 2, 3].into()); "branch with children")]
    #[test_case(branch(&[2], b"value", None); "branch with path and value")]
    #[test_case(branch(b"", b"value", vec![1, 2, 3].into()); "branch with value and children")]
    #[test_case(branch(&[2], b"", vec![1, 2, 3].into()); "branch with path and children")]
    #[test_case(branch(&[2], b"value", vec![1, 2, 3].into()); "branch with path value and children")]
    fn encode(node: Node) {
        let merkle = create_test_merkle();

        let node_ref = merkle.put_node(node).unwrap();
        let encoded = node_ref.get_encoded(&merkle.store);
        let new_node = Node::from(NodeType::decode(encoded).unwrap());
        let new_node_encoded = new_node.get_encoded(&merkle.store);

        assert_eq!(encoded, new_node_encoded);
    }

    #[test_case(Bincode::new(), leaf(vec![], vec![4, 5]) ; "leaf without partial path encoding with Bincode")]
    #[test_case(Bincode::new(), leaf(vec![1, 2, 3], vec![4, 5]) ; "leaf with partial path encoding with Bincode")]
    #[test_case(Bincode::new(), branch(b"abcd", b"value", vec![1, 2, 3].into()) ; "branch with partial path and value with Bincode")]
    #[test_case(Bincode::new(), branch(b"abcd", &[], vec![1, 2, 3].into()) ; "branch with partial path and no value with Bincode")]
    #[test_case(Bincode::new(), branch(b"", &[1,3,3,7], vec![1, 2, 3].into()) ; "branch with no partial path and value with Bincode")]
    #[test_case(PlainCodec::new(), leaf(Vec::new(), vec![4, 5]) ; "leaf without partial path encoding with PlainCodec")]
    #[test_case(PlainCodec::new(), leaf(vec![1, 2, 3], vec![4, 5]) ; "leaf with partial path encoding with PlainCodec")]
    #[test_case(PlainCodec::new(), branch(b"abcd", b"value", vec![1, 2, 3].into()) ; "branch with partial path and value with PlainCodec")]
    #[test_case(PlainCodec::new(), branch(b"abcd", &[], vec![1, 2, 3].into()) ; "branch with partial path and no value with PlainCodec")]
    #[test_case(PlainCodec::new(), branch(b"", &[1,3,3,7], vec![1, 2, 3].into()) ; "branch with no partial path and value with PlainCodec")]
    fn node_encode_decode<T>(_codec: T, node: Node)
    where
        T: BinarySerde,
        for<'de> EncodedNode<T>: serde::Serialize + serde::Deserialize<'de>,
    {
        let merkle = create_generic_test_merkle::<T>();
        let node_ref = merkle.put_node(node.clone()).unwrap();

        let encoded = merkle.encode(node_ref.inner()).unwrap();
        let new_node = Node::from(merkle.decode(encoded.as_ref()).unwrap());

        assert_eq!(node, new_node);
    }

    #[test]
    fn insert_and_retrieve_one() {
        let key = b"hello";
        let val = b"world";

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(key, val.to_vec(), root).unwrap();

        let fetched_val = merkle.get(key, root).unwrap();

        assert_eq!(fetched_val.as_deref(), val.as_slice().into());
    }

    #[test]
    fn insert_and_retrieve_multiple() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        // insert values
        for key_val in u8::MIN..=u8::MAX {
            let key = vec![key_val];
            let val = vec![key_val];

            merkle.insert(&key, val.clone(), root).unwrap();

            let fetched_val = merkle.get(&key, root).unwrap();

            // make sure the value was inserted
            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }

        // make sure none of the previous values were forgotten after initial insert
        for key_val in u8::MIN..=u8::MAX {
            let key = vec![key_val];
            let val = vec![key_val];

            let fetched_val = merkle.get(&key, root).unwrap();

            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }
    }

    #[test]
    fn long_insert_and_retrieve_multiple() {
        let key_val: Vec<(&'static [u8], _)> = vec![
            (
                &[0, 0, 0, 1, 0, 101, 151, 236],
                [16, 15, 159, 195, 34, 101, 227, 73],
            ),
            (
                &[0, 0, 1, 107, 198, 92, 205],
                [26, 147, 21, 200, 138, 106, 137, 218],
            ),
            (&[0, 1, 0, 1, 0, 56], [194, 147, 168, 193, 19, 226, 51, 204]),
            (&[1, 90], [101, 38, 25, 65, 181, 79, 88, 223]),
            (
                &[1, 1, 1, 0, 0, 0, 1, 59],
                [105, 173, 182, 126, 67, 166, 166, 196],
            ),
            (
                &[0, 1, 0, 0, 1, 1, 55, 33, 38, 194],
                [90, 140, 160, 53, 230, 100, 237, 236],
            ),
            (
                &[1, 1, 0, 1, 249, 46, 69],
                [16, 104, 134, 6, 57, 46, 200, 35],
            ),
            (
                &[1, 1, 0, 1, 0, 0, 1, 33, 163],
                [95, 97, 187, 124, 198, 28, 75, 226],
            ),
            (
                &[1, 1, 0, 1, 0, 57, 156],
                [184, 18, 69, 29, 96, 252, 188, 58],
            ),
            (&[1, 0, 1, 1, 0, 218], [155, 38, 43, 54, 93, 134, 73, 209]),
        ];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        for (key, val) in &key_val {
            merkle.insert(key, val.to_vec(), root).unwrap();

            let fetched_val = merkle.get(key, root).unwrap();

            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }

        for (key, val) in key_val {
            let fetched_val = merkle.get(key, root).unwrap();

            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }
    }

    #[test]
    fn remove_one() {
        let key = b"hello";
        let val = b"world";

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(key, val.to_vec(), root).unwrap();

        assert_eq!(
            merkle.get(key, root).unwrap().as_deref(),
            val.as_slice().into()
        );

        let removed_val = merkle.remove(key, root).unwrap();
        assert_eq!(removed_val.as_deref(), val.as_slice().into());

        let fetched_val = merkle.get(key, root).unwrap();
        assert!(fetched_val.is_none());
    }

    #[test]
    fn remove_many() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        // insert values
        for key_val in u8::MIN..=u8::MAX {
            let key = &[key_val];
            let val = &[key_val];

            merkle.insert(key, val.to_vec(), root).unwrap();

            let fetched_val = merkle.get(key, root).unwrap();

            // make sure the value was inserted
            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }

        // remove values
        for key_val in u8::MIN..=u8::MAX {
            let key = &[key_val];
            let val = &[key_val];

            let Ok(removed_val) = merkle.remove(key, root) else {
                panic!("({key_val}, {key_val}) missing");
            };

            assert_eq!(removed_val.as_deref(), val.as_slice().into());

            let fetched_val = merkle.get(key, root).unwrap();
            assert!(fetched_val.is_none());
        }
    }

    #[test]
    fn get_empty_proof() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let proof = merkle.prove(b"any-key", root).unwrap();

        assert!(proof.0.is_empty());
    }

    #[tokio::test]
    async fn empty_range_proof() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        assert!(merkle
            .range_proof::<&[u8]>(root, None, None, None)
            .await
            .unwrap()
            .is_none());
    }

    #[tokio::test]
    async fn range_proof_invalid_bounds() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let start_key = &[0x01];
        let end_key = &[0x00];

        match merkle
            .range_proof::<&[u8]>(root, Some(start_key), Some(end_key), Some(1))
            .await
        {
            Err(api::Error::InvalidRange {
                first_key,
                last_key,
            }) if first_key == start_key && last_key == end_key => (),
            Err(api::Error::InvalidRange { .. }) => panic!("wrong bounds on InvalidRange error"),
            _ => panic!("expected InvalidRange error"),
        }
    }

    #[tokio::test]
    async fn full_range_proof() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        // insert values
        for key_val in u8::MIN..=u8::MAX {
            let key = &[key_val];
            let val = &[key_val];

            merkle.insert(key, val.to_vec(), root).unwrap();
        }
        merkle.flush_dirty();

        let rangeproof = merkle
            .range_proof::<&[u8]>(root, None, None, None)
            .await
            .unwrap()
            .unwrap();
        assert_eq!(rangeproof.middle.len(), u8::MAX as usize + 1);
        assert_ne!(rangeproof.first_key_proof.0, rangeproof.last_key_proof.0);
        let left_proof = merkle.prove([u8::MIN], root).unwrap();
        let right_proof = merkle.prove([u8::MAX], root).unwrap();
        assert_eq!(rangeproof.first_key_proof.0, left_proof.0);
        assert_eq!(rangeproof.last_key_proof.0, right_proof.0);
    }

    #[tokio::test]
    async fn single_value_range_proof() {
        const RANDOM_KEY: u8 = 42;

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        // insert values
        for key_val in u8::MIN..=u8::MAX {
            let key = &[key_val];
            let val = &[key_val];

            merkle.insert(key, val.to_vec(), root).unwrap();
        }
        merkle.flush_dirty();

        let rangeproof = merkle
            .range_proof(root, Some([RANDOM_KEY]), None, Some(1))
            .await
            .unwrap()
            .unwrap();
        assert_eq!(rangeproof.first_key_proof.0, rangeproof.last_key_proof.0);
        assert_eq!(rangeproof.middle.len(), 1);
    }

    #[test]
    fn shared_path_proof() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let key1 = b"key1";
        let value1 = b"1";
        merkle.insert(key1, value1.to_vec(), root).unwrap();

        let key2 = b"key2";
        let value2 = b"2";
        merkle.insert(key2, value2.to_vec(), root).unwrap();

        let root_hash = merkle.root_hash(root).unwrap();

        let verified = {
            let key = key1;
            let proof = merkle.prove(key, root).unwrap();
            proof.verify(key, root_hash.0).unwrap()
        };

        assert_eq!(verified, Some(value1.to_vec()));

        let verified = {
            let key = key2;
            let proof = merkle.prove(key, root).unwrap();
            proof.verify(key, root_hash.0).unwrap()
        };

        assert_eq!(verified, Some(value2.to_vec()));
    }

    // this was a specific failing case
    #[test]
    fn shared_path_on_insert() {
        type Bytes = &'static [u8];
        let pairs: Vec<(Bytes, Bytes)> = vec![
            (
                &[1, 1, 46, 82, 67, 218],
                &[23, 252, 128, 144, 235, 202, 124, 243],
            ),
            (
                &[1, 0, 0, 1, 1, 0, 63, 80],
                &[99, 82, 31, 213, 180, 196, 49, 242],
            ),
            (
                &[0, 0, 0, 169, 176, 15],
                &[105, 211, 176, 51, 231, 182, 74, 207],
            ),
            (
                &[1, 0, 0, 0, 53, 57, 93],
                &[234, 139, 214, 220, 172, 38, 168, 164],
            ),
        ];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        for (key, val) in &pairs {
            let val = val.to_vec();
            merkle.insert(key, val.clone(), root).unwrap();

            let fetched_val = merkle.get(key, root).unwrap();

            // make sure the value was inserted
            assert_eq!(fetched_val.as_deref(), val.as_slice().into());
        }

        for (key, val) in pairs {
            let fetched_val = merkle.get(key, root).unwrap();

            // make sure the value was inserted
            assert_eq!(fetched_val.as_deref(), val.into());
        }
    }

    #[test]
    fn overwrite_leaf() {
        let key = vec![0x00];
        let val = vec![1];
        let overwrite = vec![2];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(&key, val.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(val.as_slice())
        );

        merkle.insert(&key, overwrite.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(overwrite.as_slice())
        );
    }

    #[test]
    fn new_leaf_is_a_child_of_the_old_leaf() {
        let key = vec![0xff];
        let val = vec![1];
        let key_2 = vec![0xff, 0x00];
        let val_2 = vec![2];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(&key, val.clone(), root).unwrap();
        merkle.insert(&key_2, val_2.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(val.as_slice())
        );

        assert_eq!(
            merkle.get(&key_2, root).unwrap().as_deref(),
            Some(val_2.as_slice())
        );
    }

    #[test]
    fn old_leaf_is_a_child_of_the_new_leaf() {
        let key = vec![0xff, 0x00];
        let val = vec![1];
        let key_2 = vec![0xff];
        let val_2 = vec![2];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(&key, val.clone(), root).unwrap();
        merkle.insert(&key_2, val_2.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(val.as_slice())
        );

        assert_eq!(
            merkle.get(&key_2, root).unwrap().as_deref(),
            Some(val_2.as_slice())
        );
    }

    #[test]
    fn new_leaf_is_sibling_of_old_leaf() {
        let key = vec![0xff];
        let val = vec![1];
        let key_2 = vec![0xff, 0x00];
        let val_2 = vec![2];
        let key_3 = vec![0xff, 0x0f];
        let val_3 = vec![3];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(&key, val.clone(), root).unwrap();
        merkle.insert(&key_2, val_2.clone(), root).unwrap();
        merkle.insert(&key_3, val_3.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(val.as_slice())
        );

        assert_eq!(
            merkle.get(&key_2, root).unwrap().as_deref(),
            Some(val_2.as_slice())
        );

        assert_eq!(
            merkle.get(&key_3, root).unwrap().as_deref(),
            Some(val_3.as_slice())
        );
    }

    #[test]
    fn old_branch_is_a_child_of_new_branch() {
        let key = vec![0xff, 0xf0];
        let val = vec![1];
        let key_2 = vec![0xff, 0xf0, 0x00];
        let val_2 = vec![2];
        let key_3 = vec![0xff];
        let val_3 = vec![3];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(&key, val.clone(), root).unwrap();
        merkle.insert(&key_2, val_2.clone(), root).unwrap();
        merkle.insert(&key_3, val_3.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(val.as_slice())
        );

        assert_eq!(
            merkle.get(&key_2, root).unwrap().as_deref(),
            Some(val_2.as_slice())
        );

        assert_eq!(
            merkle.get(&key_3, root).unwrap().as_deref(),
            Some(val_3.as_slice())
        );
    }

    #[test]
    fn overlapping_branch_insert() {
        let key = vec![0xff];
        let val = vec![1];
        let key_2 = vec![0xff, 0x00];
        let val_2 = vec![2];

        let overwrite = vec![3];

        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        merkle.insert(&key, val.clone(), root).unwrap();
        merkle.insert(&key_2, val_2.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(val.as_slice())
        );

        assert_eq!(
            merkle.get(&key_2, root).unwrap().as_deref(),
            Some(val_2.as_slice())
        );

        merkle.insert(&key, overwrite.clone(), root).unwrap();

        assert_eq!(
            merkle.get(&key, root).unwrap().as_deref(),
            Some(overwrite.as_slice())
        );

        assert_eq!(
            merkle.get(&key_2, root).unwrap().as_deref(),
            Some(val_2.as_slice())
        );
    }

    #[test]
    fn single_key_proof_with_one_node() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let key = b"key";
        let value = b"value";

        merkle.insert(key, value.to_vec(), root).unwrap();
        let root_hash = merkle.root_hash(root).unwrap();

        let proof = merkle.prove(key, root).unwrap();

        let verified = proof.verify(key, root_hash.0).unwrap();

        assert_eq!(verified, Some(value.to_vec()));
    }

    #[test]
    fn two_key_proof_without_shared_path() {
        let mut merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();

        let key1 = &[0x00];
        let key2 = &[0xff];

        merkle.insert(key1, key1.to_vec(), root).unwrap();
        merkle.insert(key2, key2.to_vec(), root).unwrap();

        let root_hash = merkle.root_hash(root).unwrap();

        let verified = {
            let proof = merkle.prove(key1, root).unwrap();
            proof.verify(key1, root_hash.0).unwrap()
        };

        assert_eq!(verified.as_deref(), Some(key1.as_slice()));
    }

    #[test]
    fn update_leaf_with_larger_path() -> Result<(), MerkleError> {
        let path = vec![0x00];
        let value = vec![0x00];

        let double_path = path
            .clone()
            .into_iter()
            .chain(path.clone())
            .collect::<Vec<_>>();

        let node = Node::from_leaf(LeafNode {
            partial_path: Path::from(path),
            value: value.clone(),
        });

        check_node_update(node, double_path, value)
    }

    #[test]
    fn update_leaf_with_larger_value() -> Result<(), MerkleError> {
        let path = vec![0x00];
        let value = vec![0x00];

        let double_value = value
            .clone()
            .into_iter()
            .chain(value.clone())
            .collect::<Vec<_>>();

        let node = Node::from_leaf(LeafNode {
            partial_path: Path::from(path.clone()),
            value,
        });

        check_node_update(node, path, double_value)
    }

    #[test]
    fn update_branch_with_larger_path() -> Result<(), MerkleError> {
        let path = vec![0x00];
        let value = vec![0x00];

        let double_path = path
            .clone()
            .into_iter()
            .chain(path.clone())
            .collect::<Vec<_>>();

        let node = Node::from_branch(BranchNode {
            partial_path: Path::from(path.clone()),
            children: Default::default(),
            value: Some(value.clone()),
            children_encoded: Default::default(),
        });

        check_node_update(node, double_path, value)
    }

    #[test]
    fn update_branch_with_larger_value() -> Result<(), MerkleError> {
        let path = vec![0x00];
        let value = vec![0x00];

        let double_value = value
            .clone()
            .into_iter()
            .chain(value.clone())
            .collect::<Vec<_>>();

        let node = Node::from_branch(BranchNode {
            partial_path: Path::from(path.clone()),
            children: Default::default(),
            value: Some(value),
            children_encoded: Default::default(),
        });

        check_node_update(node, path, double_value)
    }

    fn check_node_update(
        node: Node,
        new_path: Vec<u8>,
        new_value: Vec<u8>,
    ) -> Result<(), MerkleError> {
        let merkle = create_test_merkle();
        let root = merkle.init_root()?;
        let root = merkle.get_node(root)?;

        let mut node_ref = merkle.put_node(node)?;
        let addr = node_ref.as_ptr();

        // make sure that doubling the path length will fail on a normal write
        let write_result = node_ref.write(|node| {
            node.inner_mut().set_path(Path(new_path.clone()));
            node.inner_mut().set_value(new_value.clone());
            node.rehash();
        });

        assert!(matches!(write_result, Err(ObjWriteSizeError)));

        let mut to_delete = vec![];
        // could be any branch node, convenient to use the root.
        let mut parents = vec![(root, 0)];

        let node = merkle.update_path_and_move_node_if_larger(
            (&mut parents, &mut to_delete),
            node_ref,
            Path(new_path.clone()),
        )?;

        assert_ne!(node.as_ptr(), addr);
        assert_eq!(&to_delete[0], &addr);

        let (path, value) = match node.inner() {
            NodeType::Leaf(leaf) => (&leaf.partial_path, Some(&leaf.value)),
            NodeType::Branch(branch) => (&branch.partial_path, branch.value.as_ref()),
        };

        assert_eq!(path, &Path(new_path));
        assert_eq!(value, Some(&new_value));

        Ok(())
    }
}
