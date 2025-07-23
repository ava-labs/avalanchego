// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Hash Module
//!
//! This module contains all node hashing functionality for the nodestore, including
//! specialized support for Ethereum-compatible hash processing.

#[cfg(feature = "ethhash")]
use crate::Children;
use crate::hashednode::hash_node;
use crate::linear::FileIoError;
use crate::logger::trace;
use crate::node::Node;
use crate::{Child, HashType, MaybePersistedNode, NodeStore, Path, ReadableStorage, SharedNode};

use super::NodeReader;
#[cfg(feature = "ethhash")]
use std::ops::Deref;

/// Classified children for ethereum hash processing
#[cfg(feature = "ethhash")]
pub(super) struct ClassifiedChildren<'a> {
    pub(super) unhashed: Vec<(usize, Node)>,
    pub(super) hashed: Vec<(usize, (MaybePersistedNode, &'a mut HashType))>,
}

impl<T, S: ReadableStorage> NodeStore<T, S>
where
    NodeStore<T, S>: NodeReader,
{
    /// Helper function to classify children for ethereum hash processing
    /// We have some special cases based on the number of children
    /// and whether they are hashed or unhashed, so we need to classify them.
    #[cfg(feature = "ethhash")]
    pub(super) fn ethhash_classify_children<'a>(
        &self,
        children: &'a mut Children<Child>,
    ) -> ClassifiedChildren<'a> {
        children.iter_mut().enumerate().fold(
            ClassifiedChildren {
                unhashed: Vec::new(),
                hashed: Vec::new(),
            },
            |mut acc, (idx, child)| {
                match child {
                    None => {}
                    Some(Child::AddressWithHash(a, h)) => {
                        // Convert address to MaybePersistedNode
                        let maybe_persisted_node = MaybePersistedNode::from(*a);
                        acc.hashed.push((idx, (maybe_persisted_node, h)));
                    }
                    Some(Child::Node(node)) => acc.unhashed.push((idx, node.clone())),
                    Some(Child::MaybePersisted(maybe_persisted, h)) => {
                        // For MaybePersisted, we need to get the address if it's persisted
                        if let Some(addr) = maybe_persisted.as_linear_address() {
                            let maybe_persisted_node = MaybePersistedNode::from(addr);
                            acc.hashed.push((idx, (maybe_persisted_node, h)));
                        } else {
                            // If not persisted, we need to get the node to hash it
                            if let Ok(node) = maybe_persisted.as_shared_node(&self) {
                                acc.unhashed.push((idx, node.deref().clone()));
                            }
                        }
                    }
                }
                acc
            },
        )
    }

    /// Hashes `node`, which is at the given `path_prefix`, and its children recursively.
    /// Returns the hashed node and its hash.
    pub(super) fn hash_helper(
        #[cfg(feature = "ethhash")] &self,
        mut node: Node,
        path_prefix: &mut Path,
        #[cfg(feature = "ethhash")] fake_root_extra_nibble: Option<u8>,
    ) -> Result<(MaybePersistedNode, HashType), FileIoError> {
        // If this is a branch, find all unhashed children and recursively hash them.
        trace!("hashing {node:?} at {path_prefix:?}");
        if let Node::Branch(ref mut b) = node {
            // special case code for ethereum hashes at the account level
            #[cfg(feature = "ethhash")]
            let make_fake_root = if path_prefix.0.len().saturating_add(b.partial_path.0.len()) == 64
            {
                // looks like we're at an account branch
                // tally up how many hashes we need to deal with
                let ClassifiedChildren {
                    unhashed,
                    mut hashed,
                } = self.ethhash_classify_children(&mut b.children);
                trace!("hashed {hashed:?} unhashed {unhashed:?}");
                if hashed.len() == 1 {
                    // we were left with one hashed node that must be rehashed
                    let invalidated_node = hashed.first_mut().expect("hashed is not empty");
                    // Extract the address from the MaybePersistedNode
                    let addr = invalidated_node
                        .1
                        .0
                        .as_linear_address()
                        .expect("hashed node should be persisted");
                    let mut hashable_node = self.read_node(addr)?.deref().clone();
                    let original_length = path_prefix.len();
                    path_prefix.0.extend(b.partial_path.0.iter().copied());
                    if unhashed.is_empty() {
                        hashable_node.update_partial_path(Path::from_nibbles_iterator(
                            std::iter::once(invalidated_node.0 as u8)
                                .chain(hashable_node.partial_path().0.iter().copied()),
                        ));
                    } else {
                        path_prefix.0.push(invalidated_node.0 as u8);
                    }
                    let hash = hash_node(&hashable_node, path_prefix);
                    path_prefix.0.truncate(original_length);
                    *invalidated_node.1.1 = hash;
                }
                // handle the single-child case for an account special below
                if hashed.is_empty() && unhashed.len() == 1 {
                    Some(unhashed.last().expect("only one").0 as u8)
                } else {
                    None
                }
            } else {
                // not a single child
                None
            };

            // branch children cases:
            // 1. 1 child, already hashed
            // 2. >1 child, already hashed,
            // 3. 1 hashed child, 1 unhashed child
            // 4. 0 hashed, 1 unhashed <-- handle child special
            // 5. 1 hashed, >0 unhashed <-- rehash case
            // 6. everything already hashed

            for (nibble, child) in b.children.iter_mut().enumerate() {
                // If this is empty or already hashed, we're done
                // Empty matches None, and non-Node types match Some(None) here, so we want
                // Some(Some(node))
                let Some(child_node) = child.as_mut().and_then(|child| child.as_mut_node()) else {
                    continue;
                };

                // remove the child from the children array, we will replace it with a hashed variant
                let child_node = std::mem::take(child_node);

                // Hash this child and update
                // we extend and truncate path_prefix to reduce memory allocations
                let original_length = path_prefix.len();
                path_prefix.0.extend(b.partial_path.0.iter().copied());
                #[cfg(feature = "ethhash")]
                if make_fake_root.is_none() {
                    // we don't push the nibble there is only one unhashed child and
                    // we're on an account
                    path_prefix.0.push(nibble as u8);
                }
                #[cfg(not(feature = "ethhash"))]
                path_prefix.0.push(nibble as u8);

                #[cfg(feature = "ethhash")]
                let (child_node, child_hash) =
                    self.hash_helper(child_node, path_prefix, make_fake_root)?;
                #[cfg(not(feature = "ethhash"))]
                let (child_node, child_hash) = Self::hash_helper(child_node, path_prefix)?;

                *child = Some(Child::MaybePersisted(child_node, child_hash));
                trace!("child now {child:?}");
                path_prefix.0.truncate(original_length);
            }
        }
        // At this point, we either have a leaf or a branch with all children hashed.
        // if the encoded child hash <32 bytes then we use that RLP

        #[cfg(feature = "ethhash")]
        // if we have a child that is the only child of an account branch, we will hash this child as if it
        // is a root node. This means we have to take the nibble from the parent and prefix it to the partial path
        let hash = if let Some(nibble) = fake_root_extra_nibble {
            let mut fake_root = node.clone();
            trace!("old node: {fake_root:?}");
            fake_root.update_partial_path(Path::from_nibbles_iterator(
                std::iter::once(nibble).chain(fake_root.partial_path().0.iter().copied()),
            ));
            trace!("new node: {fake_root:?}");
            hash_node(&fake_root, path_prefix)
        } else {
            hash_node(&node, path_prefix)
        };

        #[cfg(not(feature = "ethhash"))]
        let hash = hash_node(&node, path_prefix);

        Ok((SharedNode::new(node).into(), hash))
    }
}
