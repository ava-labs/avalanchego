// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Hash Module
//!
//! This module contains all node hashing functionality for the nodestore, including
//! specialized support for Ethereum-compatible hash processing.

#[cfg(feature = "ethhash")]
use crate::PathComponent;
use crate::hashednode::hash_node;
use crate::linear::FileIoError;
use crate::logger::trace;
use crate::node::Node;
use crate::{
    Child, Children, HashType, MaybePersistedNode, NodeStore, Path, ReadableStorage, SharedNode,
};

use super::NodeReader;

use std::ops::{Deref, DerefMut};

/// Wrapper around a path that makes sure we truncate what gets extended to the path after it goes out of scope
/// This allows the same memory space to be reused for different path prefixes
#[derive(Debug)]
struct PathGuard<'a> {
    path: &'a mut Path,
    original_length: usize,
}

impl<'a> PathGuard<'a> {
    fn new(path: &'a mut PathGuard<'_>) -> Self {
        Self {
            original_length: path.0.len(),
            path: &mut path.path,
        }
    }

    fn from_path(path: &'a mut Path) -> Self {
        Self {
            original_length: path.0.len(),
            path,
        }
    }
}

impl Drop for PathGuard<'_> {
    fn drop(&mut self) {
        self.path.0.truncate(self.original_length);
    }
}

impl Deref for PathGuard<'_> {
    type Target = Path;
    fn deref(&self) -> &Self::Target {
        self.path
    }
}

impl DerefMut for PathGuard<'_> {
    fn deref_mut(&mut self) -> &mut Self::Target {
        self.path
    }
}

/// Classified children for ethereum hash processing
#[cfg(feature = "ethhash")]
pub(super) struct ClassifiedChildren<'a> {
    pub(super) unhashed: Vec<(PathComponent, Node)>,
    pub(super) hashed: Vec<(PathComponent, (MaybePersistedNode, &'a mut HashType))>,
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
        children: &'a mut Children<Option<Child>>,
    ) -> ClassifiedChildren<'a> {
        children.into_iter().fold(
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
                        // For MaybePersisted, it's important to remember that we've already hashed it
                        acc.hashed.push((idx, (maybe_persisted.clone(), h)));
                    }
                }
                acc
            },
        )
    }

    /// Hashes the given `node` and the subtree rooted at it. The `root_path` should be empty
    /// if this is called from the root, or it should include the partial path if this is called
    /// on a subtrie. Returns the hashed node and its hash.
    ///
    /// # Errors
    ///
    /// Can return a `FileIoError` if it is unable to read a node that it is hashing.
    pub fn hash_helper(
        #[cfg(feature = "ethhash")] &self,
        node: Node,
        mut root_path: Path,
    ) -> Result<(MaybePersistedNode, HashType), FileIoError> {
        #[cfg(not(feature = "ethhash"))]
        let res = Self::hash_helper_inner(node, PathGuard::from_path(&mut root_path))?;
        #[cfg(feature = "ethhash")]
        let res = self.hash_helper_inner(node, PathGuard::from_path(&mut root_path), None)?;
        Ok(res)
    }

    /// Recursive helper that hashes the given `node` and the subtree rooted at it.
    /// This function takes a mut `node` to update the hash in place.
    /// The `path_prefix` is also mut because we will extend it to the path of the child we are hashing in recursive calls - it will be restored after the recursive call returns.
    /// The `num_siblings` is the number of children of the parent node, which includes this node.
    fn hash_helper_inner(
        #[cfg(feature = "ethhash")] &self,
        mut node: Node,
        mut path_prefix: PathGuard<'_>,
        #[cfg(feature = "ethhash")] fake_root_extra_nibble: Option<u8>,
    ) -> Result<(MaybePersistedNode, HashType), FileIoError> {
        // If this is a branch, find all unhashed children and recursively hash them.
        trace!("hashing {node:?} at {path_prefix:?}");
        if let Node::Branch(ref mut b) = node {
            // special case code for ethereum hashes at the account level
            // Both lengths are usize counts of nibbles in a trie path, so their
            // sum cannot overflow on any platform firewood targets.
            #[cfg(feature = "ethhash")]
            let make_fake_root = if path_prefix.0.len().wrapping_add(b.partial_path.0.len()) == 64 {
                // looks like we're at an account branch
                // tally up how many hashes we need to deal with
                let ClassifiedChildren {
                    unhashed,
                    mut hashed,
                } = self.ethhash_classify_children(&mut b.children);
                trace!("hashed {hashed:?} unhashed {unhashed:?}");
                // we were left with one hashed node that must be rehashed
                if let [(child_idx, (child_node, child_hash))] = &mut hashed[..] {
                    // read MaybePersistedNode as a SharedNode so we can hash and update it
                    let mut hashable_node = child_node.as_shared_node(&self)?.deref().clone();
                    let hash = {
                        let mut path_guard = PathGuard::new(&mut path_prefix);
                        path_guard.0.extend(b.partial_path.0.iter().copied());
                        if unhashed.is_empty() {
                            hashable_node.update_partial_path(Path::from_nibbles_iterator(
                                std::iter::once(child_idx.as_u8())
                                    .chain(hashable_node.partial_path().0.iter().copied()),
                            ));
                        } else {
                            path_guard.0.push(child_idx.as_u8());
                        }
                        hash_node(&hashable_node, &path_guard)
                    };
                    **child_hash = hash;
                }
                // handle the single-child case for an account special below
                if hashed.is_empty() && unhashed.len() == 1 {
                    Some(unhashed.last().expect("only one").0.as_u8())
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

            for (nibble, child) in &mut b.children {
                // If this is empty or already hashed, we're done
                // Empty matches None, and non-Node types match Some(None) here, so we want
                // Some(Some(node))
                let Some(child_node) = child.as_mut().and_then(|child| child.as_mut_node()) else {
                    continue;
                };

                // remove the child from the children array, we will replace it with a hashed variant
                let child_node = std::mem::take(child_node);

                // Hash this child and update
                let (child_node, child_hash) = {
                    // we extend and truncate path_prefix to reduce memory allocations]
                    let mut child_path_prefix = PathGuard::new(&mut path_prefix);
                    child_path_prefix.0.extend(b.partial_path.0.iter().copied());
                    #[cfg(feature = "ethhash")]
                    if make_fake_root.is_none() {
                        // we don't push the nibble there is only one unhashed child and
                        // we're on an account
                        child_path_prefix.0.push(nibble.as_u8());
                    }
                    #[cfg(not(feature = "ethhash"))]
                    child_path_prefix.0.push(nibble.as_u8());
                    #[cfg(feature = "ethhash")]
                    let (child_node, child_hash) =
                        self.hash_helper_inner(child_node, child_path_prefix, make_fake_root)?;
                    #[cfg(not(feature = "ethhash"))]
                    let (child_node, child_hash) =
                        Self::hash_helper_inner(child_node, child_path_prefix)?;

                    (child_node, child_hash)
                };

                *child = Some(Child::MaybePersisted(child_node, child_hash));
                trace!("child now {child:?}");
            }
        }

        // For account-depth nodes (branch or leaf), persist the computed
        // storageRoot into the node's RLP-encoded value.
        #[cfg(feature = "ethhash")]
        update_account_storage_root(&mut node, &path_prefix);

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
            hash_node(&fake_root, &path_prefix)
        } else {
            hash_node(&node, &path_prefix)
        };

        #[cfg(not(feature = "ethhash"))]
        let hash = hash_node(&node, &path_prefix);

        Ok((SharedNode::new(node).into(), hash))
    }

    #[cfg(feature = "ethhash")]
    pub(crate) fn compute_node_ethhash(
        node: &Node,
        path_prefix: &Path,
        have_peers: bool,
    ) -> HashType {
        if path_prefix.0.len() == 65 && !have_peers {
            // This is the special case when this node is the only child of an account
            //  - 64 nibbles for account + 1 nibble for its position in account branch node
            let mut fake_root = node.clone();
            fake_root.update_partial_path(Path::from_nibbles_iterator(
                path_prefix
                    .0
                    .last()
                    .into_iter()
                    .chain(fake_root.partial_path().0.iter())
                    .copied(),
            ));
            hash_node(&fake_root, path_prefix)
        } else {
            hash_node(node, path_prefix)
        }
    }
}

/// Given an account node's value and its children's hashes, return the value with the
/// storageRoot field replaced by the computed hash of the storage sub-trie.
///
/// For leaf accounts (no children), the storage root is the empty trie hash.
/// For branch accounts, the storage root is computed from the children's hashes.
///
/// Returns `None` if the value is not well-formed account RLP.
///
/// At account depth (64 nibbles), storage keys are 32 bytes, so every child
/// encoding exceeds 32 bytes and is stored as a `HashType::Hash` — `Rlp`
/// children are impossible here.
#[must_use]
pub fn fix_account_storage_root_value(
    value: &[u8],
    child_hashes: &Children<Option<HashType>>,
) -> Option<Box<[u8]>> {
    use crate::node::BranchNode;
    use crate::rlp::{NULL_RLP, RlpItem, encode_list, replace_list_field};
    use sha3::{Digest, Keccak256};

    let storage_root = if child_hashes.count() == 0 {
        crate::TrieHash::from(Keccak256::digest(NULL_RLP))
    } else {
        let mut child_hashes = child_hashes.clone();
        if let Some((_, child)) = child_hashes.take_only_child() {
            single_child_storage_root(child)
        } else {
            let mut items: [RlpItem<'_>; BranchNode::MAX_CHILDREN + 1] =
                [RlpItem::Empty; BranchNode::MAX_CHILDREN + 1];
            for ((_, child), slot) in (&child_hashes).into_iter().zip(items.iter_mut()) {
                *slot = child_to_rlp_item(child.as_ref());
            }
            crate::TrieHash::from(Keccak256::digest(encode_list(&items)))
        }
    };

    replace_list_field(value, 2, storage_root.as_slice()).ok()
}

/// Persist the computed storageRoot into an account node's RLP-encoded value,
/// in place. Only acts on nodes at account depth (64 nibbles) whose values are
/// well-formed Ethereum account RLP.
///
/// For branch accounts, the storage root is computed from the children's hashes.
/// For leaf accounts (no storage sub-trie), the storage root is the empty trie hash.
#[cfg(feature = "ethhash")]
fn update_account_storage_root(node: &mut Node, path_prefix: &Path) {
    // Both lengths are usize counts of nibbles in a trie path, so their
    // sum cannot overflow on any platform firewood targets.
    let total_depth = path_prefix
        .0
        .len()
        .wrapping_add(node.partial_path().0.len());
    if total_depth != 64 {
        return;
    }

    match node {
        Node::Branch(b) => {
            let Some(old_value) = b.value.as_ref() else {
                return;
            };
            let child_hashes = b.children_hashes();
            if let Some(new_value) = fix_account_storage_root_value(old_value, &child_hashes) {
                b.value = Some(new_value);
            }
        }
        Node::Leaf(l) => {
            let empty_children: Children<Option<HashType>> = Children::new();
            if let Some(new_value) = fix_account_storage_root_value(&l.value, &empty_children) {
                l.value = new_value;
            }
        }
    }
}

/// Extract the `TrieHash` for the single-storage-child case. At account
/// depth storage child encodings always exceed 32 bytes (32-byte keys), so
/// the inline-RLP variant cannot occur in ethhash mode; without ethhash
/// every child is already a hash.
#[cfg(feature = "ethhash")]
fn single_child_storage_root(child: HashType) -> crate::TrieHash {
    match child {
        HashType::Hash(hash) => hash,
        HashType::Rlp(_) => unreachable!(
            "account-depth single storage child cannot have inline RLP: \
             storage leaf encoding with 32-byte keys always exceeds 32 bytes"
        ),
    }
}

#[cfg(not(feature = "ethhash"))]
const fn single_child_storage_root(child: HashType) -> crate::TrieHash {
    // Without ethhash, `HashType` is `TrieHash`.
    child
}

/// Encode one child slot of an account's storage branch as an [`RlpItem`].
/// Mirrors the dispatch the ethhash hasher does inline (see
/// `storage/src/hashers/ethhash.rs::Preimage::write`).
#[cfg(feature = "ethhash")]
fn child_to_rlp_item(child: Option<&HashType>) -> crate::rlp::RlpItem<'_> {
    use crate::rlp::RlpItem;
    match child {
        Some(HashType::Hash(hash)) => RlpItem::Bytes(hash.as_slice()),
        Some(HashType::Rlp(_)) => unreachable!(
            "account-depth storage child cannot have inline RLP: \
             storage node encoding with 32-byte keys always exceeds 32 bytes"
        ),
        None => RlpItem::Empty,
    }
}

#[cfg(not(feature = "ethhash"))]
fn child_to_rlp_item(child: Option<&HashType>) -> crate::rlp::RlpItem<'_> {
    use crate::rlp::RlpItem;
    match child {
        Some(hash) => RlpItem::Bytes(hash.as_slice()),
        None => RlpItem::Empty,
    }
}
