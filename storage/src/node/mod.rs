// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use enum_as_inner::EnumAsInner;
use serde::{Deserialize, Serialize};
use std::{fmt::Debug, sync::Arc};

mod branch;
mod leaf;
pub mod path;

pub use branch::BranchNode;
pub use branch::Child;
pub use leaf::LeafNode;

use crate::Path;

/// A node, either a Branch or Leaf

// TODO: explain why Branch is boxed but Leaf is not
#[derive(PartialEq, Eq, Clone, Debug, EnumAsInner, Serialize, Deserialize)]
pub enum Node {
    /// This node is a [BranchNode]
    Branch(Box<BranchNode>),
    /// This node is a [LeafNode]
    Leaf(LeafNode),
}

impl Default for Node {
    fn default() -> Self {
        Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: Box::default(),
        })
    }
}

impl Node {
    /// Returns the partial path of the node.
    pub fn partial_path(&self) -> &Path {
        match self {
            Node::Branch(b) => &b.partial_path,
            Node::Leaf(l) => &l.partial_path,
        }
    }

    /// Updates the partial path of the node to `partial_path`.
    pub fn update_partial_path(&mut self, partial_path: Path) {
        match self {
            Node::Branch(b) => b.partial_path = partial_path,
            Node::Leaf(l) => l.partial_path = partial_path,
        }
    }

    /// Updates the value of the node to `value`.
    pub fn update_value(&mut self, value: Box<[u8]>) {
        match self {
            Node::Branch(b) => b.value = Some(value),
            Node::Leaf(l) => l.value = value,
        }
    }

    /// Returns a new `Arc<Node>` which is the same as `self` but with the given `partial_path`.
    pub fn new_with_partial_path(self: &Node, partial_path: Path) -> Node {
        match self {
            Node::Branch(b) => Node::Branch(Box::new(BranchNode {
                partial_path,
                value: b.value.clone(),
                children: b.children.clone(),
            })),
            Node::Leaf(l) => Node::Leaf(LeafNode {
                partial_path,
                value: l.value.clone(),
            }),
        }
    }

    /// Returns Some(value) inside the node, or None if the node is a branch
    /// with no value.
    pub fn value(&self) -> Option<&[u8]> {
        match self {
            Node::Branch(b) => b.value.as_deref(),
            Node::Leaf(l) => Some(&l.value),
        }
    }
}

/// A path iterator item, which has the key nibbles up to this point,
/// a node, the address of the node, and the nibble that points to the
/// next child down the list
#[derive(Debug)]
pub struct PathIterItem {
    /// The key of the node at `address` as nibbles.
    pub key_nibbles: Box<[u8]>,
    /// A reference to the node
    pub node: Arc<Node>,
    /// The next item returned by the iterator is a child of `node`.
    /// Specifically, it's the child at index `next_nibble` in `node`'s
    /// children array.
    /// None if `node` is the last node in the path.
    pub next_nibble: Option<u8>,
}
