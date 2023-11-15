// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::shale::{self, disk_address::DiskAddress, ObjWriteError, ShaleError, ShaleStore};
use crate::v2::api;
use crate::{nibbles::Nibbles, v2::api::Proof};
use futures::Stream;
use sha3::Digest;
use std::{
    cmp::Ordering,
    collections::HashMap,
    io::Write,
    iter::once,
    sync::{atomic::Ordering::Relaxed, OnceLock},
    task::Poll,
};
use thiserror::Error;

mod node;
mod partial_path;
mod trie_hash;

pub use node::{BranchNode, Data, ExtNode, LeafNode, Node, NodeType, NBRANCH};
pub use partial_path::PartialPath;
pub use trie_hash::{TrieHash, TRIE_HASH_LEN};

type ObjRef<'a> = shale::ObjRef<'a, Node>;
type ParentRefs<'a> = Vec<(ObjRef<'a>, u8)>;
type ParentAddresses = Vec<(DiskAddress, u8)>;

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
    WriteError(#[from] ObjWriteError),
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
pub struct Merkle<S> {
    store: Box<S>,
}

impl<S: ShaleStore<Node>> Merkle<S> {
    pub fn get_node(&self, ptr: DiskAddress) -> Result<ObjRef, MerkleError> {
        self.store.get_item(ptr).map_err(Into::into)
    }

    pub fn put_node(&self, node: Node) -> Result<ObjRef, MerkleError> {
        self.store.put_item(node, 0).map_err(Into::into)
    }

    fn free_node(&mut self, ptr: DiskAddress) -> Result<(), MerkleError> {
        self.store.free_item(ptr).map_err(Into::into)
    }
}

impl<S: ShaleStore<Node> + Send + Sync> Merkle<S> {
    pub fn new(store: Box<S>) -> Self {
        Self { store }
    }

    pub fn init_root(&self) -> Result<DiskAddress, MerkleError> {
        self.store
            .put_item(
                Node::branch(BranchNode {
                    children: [None; NBRANCH],
                    value: None,
                    children_encoded: Default::default(),
                }),
                Node::max_branch_node_size(),
            )
            .map_err(MerkleError::Shale)
            .map(|node| node.as_ptr())
    }

    pub fn get_store(&self) -> &dyn ShaleStore<Node> {
        self.store.as_ref()
    }

    pub fn empty_root() -> &'static TrieHash {
        static V: OnceLock<TrieHash> = OnceLock::new();
        V.get_or_init(|| {
            TrieHash(
                hex::decode("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
                    .unwrap()
                    .try_into()
                    .unwrap(),
            )
        })
    }

    pub fn root_hash(&self, root: DiskAddress) -> Result<TrieHash, MerkleError> {
        let root = self
            .get_node(root)?
            .inner
            .as_branch()
            .ok_or(MerkleError::NotBranchNode)?
            .children[0];
        Ok(if let Some(root) = root {
            let mut node = self.get_node(root)?;
            let res = node.get_root_hash::<S>(self.store.as_ref()).clone();
            if node.lazy_dirty.load(Relaxed) {
                node.write(|_| {}).unwrap();
                node.lazy_dirty.store(false, Relaxed);
            }
            res
        } else {
            Self::empty_root().clone()
        })
    }

    fn dump_(&self, u: DiskAddress, w: &mut dyn Write) -> Result<(), MerkleError> {
        let u_ref = self.get_node(u)?;
        write!(
            w,
            "{u:?} => {}: ",
            match u_ref.root_hash.get() {
                Some(h) => hex::encode(**h),
                None => "<lazy>".to_string(),
            }
        )?;
        match &u_ref.inner {
            NodeType::Branch(n) => {
                writeln!(w, "{n:?}")?;
                for c in n.children.iter().flatten() {
                    self.dump_(*c, w)?
                }
            }
            NodeType::Leaf(n) => writeln!(w, "{n:?}").unwrap(),
            NodeType::Extension(n) => {
                writeln!(w, "{n:?}")?;
                self.dump_(n.chd(), w)?
            }
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

    #[allow(clippy::too_many_arguments)]
    fn split(
        &self,
        mut node_to_split: ObjRef,
        parents: &mut [(ObjRef, u8)],
        insert_path: &[u8],
        n_path: Vec<u8>,
        n_value: Option<Data>,
        val: Vec<u8>,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<Option<Vec<u8>>, MerkleError> {
        let node_to_split_address = node_to_split.as_ptr();
        let split_index = insert_path
            .iter()
            .zip(n_path.iter())
            .position(|(a, b)| a != b);

        let new_child_address = if let Some(idx) = split_index {
            // paths diverge
            let new_split_node_path = n_path.split_at(idx + 1).1;
            let (matching_path, new_node_path) = insert_path.split_at(idx + 1);

            node_to_split.write(|node| {
                // TODO: handle unwrap better
                let path = node.inner.path_mut().unwrap();

                *path = PartialPath(new_split_node_path.to_vec());

                node.rehash();
            })?;

            let new_node = Node::leaf(PartialPath(new_node_path.to_vec()), Data(val));
            let leaf_address = self.put_node(new_node)?.as_ptr();

            let mut chd = [None; NBRANCH];

            let last_matching_nibble = matching_path[idx];
            chd[last_matching_nibble as usize] = Some(leaf_address);

            let address = match &node_to_split.inner {
                NodeType::Extension(u) if u.path.len() == 0 => {
                    deleted.push(node_to_split_address);
                    u.chd()
                }
                _ => node_to_split_address,
            };

            chd[n_path[idx] as usize] = Some(address);

            let new_branch = Node::branch(BranchNode {
                children: chd,
                value: None,
                children_encoded: Default::default(),
            });

            let new_branch_address = self.put_node(new_branch)?.as_ptr();

            if idx > 0 {
                self.put_node(Node::from(NodeType::Extension(ExtNode {
                    path: PartialPath(matching_path[..idx].to_vec()),
                    child: new_branch_address,
                    child_encoded: None,
                })))?
                .as_ptr()
            } else {
                new_branch_address
            }
        } else {
            // paths do not diverge
            let (leaf_address, prefix, idx, value) =
                match (insert_path.len().cmp(&n_path.len()), n_value) {
                    // no node-value means this is an extension node and we can therefore continue walking the tree
                    (Ordering::Greater, None) => return Ok(Some(val)),

                    // if the paths are equal, we overwrite the data
                    (Ordering::Equal, _) => {
                        let mut result = Ok(None);

                        write_node!(
                            self,
                            node_to_split,
                            |u| {
                                match &mut u.inner {
                                    NodeType::Leaf(u) => u.1 = Data(val),
                                    NodeType::Extension(u) => {
                                        let write_result =
                                            self.get_node(u.chd()).and_then(|mut b_ref| {
                                                b_ref
                                                    .write(|b| {
                                                        let branch =
                                                            b.inner.as_branch_mut().unwrap();
                                                        branch.value = Some(Data(val));

                                                        b.rehash()
                                                    })
                                                    // if writing fails, delete the child?
                                                    .or_else(|_| {
                                                        let node = self.put_node(b_ref.clone())?;

                                                        let child = u.chd_mut();
                                                        *child = node.as_ptr();

                                                        deleted.push(b_ref.as_ptr());

                                                        Ok(())
                                                    })
                                            });

                                        if let Err(e) = write_result {
                                            result = Err(e);
                                        }
                                    }
                                    NodeType::Branch(_) => unreachable!(),
                                }

                                u.rehash();
                            },
                            parents,
                            deleted
                        );

                        return result;
                    }

                    // if the node-path is greater than the insert path
                    (Ordering::Less, _) => {
                        // key path is a prefix of the path to u
                        node_to_split
                            .write(|u| {
                                // TODO: handle unwraps better
                                let path = u.inner.path_mut().unwrap();
                                *path = PartialPath(n_path[insert_path.len() + 1..].to_vec());

                                u.rehash();
                            })
                            .unwrap();

                        let leaf_address = match &node_to_split.inner {
                            NodeType::Extension(u) if u.path.len() == 0 => {
                                deleted.push(node_to_split_address);
                                u.chd()
                            }
                            _ => node_to_split_address,
                        };

                        (
                            leaf_address,
                            insert_path,
                            n_path[insert_path.len()] as usize,
                            Data(val).into(),
                        )
                    }
                    // insert path is greather than the path of the leaf
                    (Ordering::Greater, Some(n_value)) => {
                        let leaf = Node::leaf(
                            PartialPath(insert_path[n_path.len() + 1..].to_vec()),
                            Data(val),
                        );

                        let leaf_address = self.put_node(leaf)?.as_ptr();

                        deleted.push(node_to_split_address);

                        (
                            leaf_address,
                            n_path.as_slice(),
                            insert_path[n_path.len()] as usize,
                            n_value.into(),
                        )
                    }
                };

            // [parent] (-> [ExtNode]) -> [branch with v] -> [Leaf]
            let mut children = [None; NBRANCH];

            children[idx] = leaf_address.into();

            let branch_address = self
                .put_node(Node::branch(BranchNode {
                    children,
                    value,
                    children_encoded: Default::default(),
                }))?
                .as_ptr();

            if !prefix.is_empty() {
                self.put_node(Node::from(NodeType::Extension(ExtNode {
                    path: PartialPath(prefix.to_vec()),
                    child: branch_address,
                    child_encoded: None,
                })))?
                .as_ptr()
            } else {
                branch_address
            }
        };

        // observation:
        // - leaf/extension node can only be the child of a branch node
        // - branch node can only be the child of a branch/extension node
        // ^^^ I think a leaf can end up being the child of an extension node
        // ^^^ maybe just on delete though? I'm not sure, removing extension-nodes anyway
        set_parent(new_child_address, parents);

        Ok(None)
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
        mut val: Vec<u8>,
        root: DiskAddress,
    ) -> Result<(impl Iterator<Item = ObjRef>, Vec<DiskAddress>), MerkleError> {
        // as we split a node, we need to track deleted nodes and parents
        let mut deleted = Vec::new();
        let mut parents = Vec::new();

        let mut next_node = None;

        // we use Nibbles::<1> so that 1 zero nibble is at the front
        // this is for the sentinel node, which avoids moving the root
        // and always only has one child
        let mut key_nibbles = Nibbles::<1>::new(key.as_ref()).into_iter();

        // walk down the merkle tree starting from next_node, currently the root
        // return None if the value is inserted
        let next_node_and_val = loop {
            let mut node = next_node
                .take()
                .map(Ok)
                .unwrap_or_else(|| self.get_node(root))?;
            let node_ptr = node.as_ptr();

            let Some(current_nibble) = key_nibbles.next() else {
                break Some((node, val));
            };

            let (node, next_node_ptr) = match &node.inner {
                // For a Branch node, we look at the child pointer. If it points
                // to another node, we walk down that. Otherwise, we can store our
                // value as a leaf and we're done
                NodeType::Branch(n) => match n.children[current_nibble as usize] {
                    Some(c) => (node, c),
                    None => {
                        // insert the leaf to the empty slot
                        // create a new leaf
                        let leaf_ptr = self
                            .put_node(Node::leaf(PartialPath(key_nibbles.collect()), Data(val)))?
                            .as_ptr();
                        // set the current child to point to this leaf
                        node.write(|u| {
                            let uu = u.inner.as_branch_mut().unwrap();
                            uu.children[current_nibble as usize] = Some(leaf_ptr);
                            u.rehash();
                        })
                        .unwrap();

                        break None;
                    }
                },

                NodeType::Leaf(n) => {
                    // we collided with another key; make a copy
                    // of the stored key to pass into split
                    let n_path = n.0.to_vec();
                    let n_value = Some(n.1.clone());
                    let rem_path = once(current_nibble).chain(key_nibbles).collect::<Vec<_>>();

                    self.split(
                        node,
                        &mut parents,
                        &rem_path,
                        n_path,
                        n_value,
                        val,
                        &mut deleted,
                    )?;

                    break None;
                }

                NodeType::Extension(n) => {
                    let n_path = n.path.to_vec();
                    let n_ptr = n.chd();
                    let rem_path = once(current_nibble)
                        .chain(key_nibbles.clone())
                        .collect::<Vec<_>>();
                    let n_path_len = n_path.len();

                    if let Some(v) = self.split(
                        node,
                        &mut parents,
                        &rem_path,
                        n_path,
                        None,
                        val,
                        &mut deleted,
                    )? {
                        (0..n_path_len).skip(1).for_each(|_| {
                            key_nibbles.next();
                        });

                        // we couldn't split this, so we
                        // skip n_path items and follow the
                        // extension node's next pointer
                        val = v;

                        (self.get_node(node_ptr)?, n_ptr)
                    } else {
                        // successfully inserted
                        break None;
                    }
                }
            };

            // push another parent, and follow the next pointer
            parents.push((node, current_nibble));
            next_node = self.get_node(next_node_ptr)?.into();
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
                                n.value = Some(Data(val));
                                None
                            }
                            NodeType::Leaf(n) => {
                                if n.0.len() == 0 {
                                    n.1 = Data(val);

                                    None
                                } else {
                                    let idx = n.0[0];
                                    n.0 = PartialPath(n.0[1..].to_vec());
                                    u.rehash();

                                    Some((idx, true, None, val))
                                }
                            }
                            NodeType::Extension(n) => {
                                let idx = n.path[0];
                                let more = if n.path.len() > 1 {
                                    n.path = PartialPath(n.path[1..].to_vec());
                                    true
                                } else {
                                    false
                                };

                                Some((idx, more, Some(n.chd()), val))
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
                let mut chd = [None; NBRANCH];

                let c_ptr = if more {
                    u_ptr
                } else {
                    deleted.push(u_ptr);
                    ext.unwrap()
                };

                chd[idx as usize] = Some(c_ptr);

                let branch = self
                    .put_node(Node::branch(BranchNode {
                        children: chd,
                        value: Some(Data(val)),
                        children_encoded: Default::default(),
                    }))?
                    .as_ptr();

                set_parent(branch, &mut parents);
            }
        }

        Ok((parents.into_iter().rev().map(|(node, _)| node), deleted))
    }

    fn after_remove_leaf(
        &self,
        parents: &mut ParentRefs,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<(), MerkleError> {
        let (b_chd, val) = {
            let (mut b_ref, b_idx) = parents.pop().unwrap();
            // the immediate parent of a leaf must be a branch
            b_ref
                .write(|b| {
                    b.inner.as_branch_mut().unwrap().children[b_idx as usize] = None;
                    b.rehash()
                })
                .unwrap();
            let b_inner = b_ref.inner.as_branch().unwrap();
            let (b_chd, has_chd) = b_inner.single_child();
            if (has_chd && (b_chd.is_none() || b_inner.value.is_some())) || parents.is_empty() {
                return Ok(());
            }
            deleted.push(b_ref.as_ptr());
            (b_chd, b_inner.value.clone())
        };
        let (mut p_ref, p_idx) = parents.pop().unwrap();
        let p_ptr = p_ref.as_ptr();
        if let Some(val) = val {
            match &p_ref.inner {
                NodeType::Branch(_) => {
                    // from: [p: Branch] -> [b (v)]x -> [Leaf]x
                    // to: [p: Branch] -> [Leaf (v)]
                    let leaf = self
                        .put_node(Node::leaf(PartialPath(Vec::new()), val))?
                        .as_ptr();
                    p_ref
                        .write(|p| {
                            p.inner.as_branch_mut().unwrap().children[p_idx as usize] = Some(leaf);
                            p.rehash()
                        })
                        .unwrap();
                }
                NodeType::Extension(n) => {
                    // from: P -> [p: Ext]x -> [b (v)]x -> [leaf]x
                    // to: P -> [Leaf (v)]
                    let leaf = self
                        .put_node(Node::leaf(PartialPath(n.path.clone().into_inner()), val))?
                        .as_ptr();
                    deleted.push(p_ptr);
                    set_parent(leaf, parents);
                }
                _ => unreachable!(),
            }
        } else {
            let (c_ptr, idx) = b_chd.unwrap();
            let mut c_ref = self.get_node(c_ptr)?;
            match &c_ref.inner {
                NodeType::Branch(_) => {
                    drop(c_ref);
                    match &p_ref.inner {
                        NodeType::Branch(_) => {
                            //                            ____[Branch]
                            //                           /
                            // from: [p: Branch] -> [b]x*
                            //                           \____[Leaf]x
                            // to: [p: Branch] -> [Ext] -> [Branch]
                            let ext = self
                                .put_node(Node::from(NodeType::Extension(ExtNode {
                                    path: PartialPath(vec![idx]),
                                    child: c_ptr,
                                    child_encoded: None,
                                })))?
                                .as_ptr();
                            set_parent(ext, &mut [(p_ref, p_idx)]);
                        }
                        NodeType::Extension(_) => {
                            //                         ____[Branch]
                            //                        /
                            // from: [p: Ext] -> [b]x*
                            //                        \____[Leaf]x
                            // to: [p: Ext] -> [Branch]
                            write_node!(
                                self,
                                p_ref,
                                |p| {
                                    let pp = p.inner.as_extension_mut().unwrap();
                                    pp.path.0.push(idx);
                                    *pp.chd_mut() = c_ptr;
                                    p.rehash();
                                },
                                parents,
                                deleted
                            );
                        }
                        _ => unreachable!(),
                    }
                }
                NodeType::Leaf(_) | NodeType::Extension(_) => {
                    match &p_ref.inner {
                        NodeType::Branch(_) => {
                            //                            ____[Leaf/Ext]
                            //                           /
                            // from: [p: Branch] -> [b]x*
                            //                           \____[Leaf]x
                            // to: [p: Branch] -> [Leaf/Ext]
                            let write_result = c_ref.write(|c| {
                                let partial_path = match &mut c.inner {
                                    NodeType::Leaf(n) => &mut n.0,
                                    NodeType::Extension(n) => &mut n.path,
                                    _ => unreachable!(),
                                };

                                partial_path.0.insert(0, idx);
                                c.rehash()
                            });

                            let c_ptr = if write_result.is_err() {
                                deleted.push(c_ptr);
                                self.put_node(c_ref.clone())?.as_ptr()
                            } else {
                                c_ptr
                            };

                            drop(c_ref);

                            p_ref
                                .write(|p| {
                                    p.inner.as_branch_mut().unwrap().children[p_idx as usize] =
                                        Some(c_ptr);
                                    p.rehash()
                                })
                                .unwrap();
                        }
                        NodeType::Extension(n) => {
                            //                               ____[Leaf/Ext]
                            //                              /
                            // from: P -> [p: Ext]x -> [b]x*
                            //                              \____[Leaf]x
                            // to: P -> [p: Leaf/Ext]
                            deleted.push(p_ptr);

                            let write_failed = write_node!(
                                self,
                                c_ref,
                                |c| {
                                    let mut path = n.path.clone().into_inner();
                                    path.push(idx);
                                    let path0 = match &mut c.inner {
                                        NodeType::Leaf(n) => &mut n.0,
                                        NodeType::Extension(n) => &mut n.path,
                                        _ => unreachable!(),
                                    };
                                    path.extend(&**path0);
                                    *path0 = PartialPath(path);
                                    c.rehash()
                                },
                                parents,
                                deleted
                            );

                            if !write_failed {
                                drop(c_ref);
                                set_parent(c_ptr, parents);
                            }
                        }
                        _ => unreachable!(),
                    }
                }
            }
        }
        Ok(())
    }

    fn after_remove_branch(
        &self,
        (c_ptr, idx): (DiskAddress, u8),
        parents: &mut ParentRefs,
        deleted: &mut Vec<DiskAddress>,
    ) -> Result<(), MerkleError> {
        // [b] -> [u] -> [c]
        let (mut b_ref, b_idx) = parents.pop().unwrap();
        let mut c_ref = self.get_node(c_ptr).unwrap();
        match &c_ref.inner {
            NodeType::Branch(_) => {
                drop(c_ref);
                let mut err = None;
                write_node!(
                    self,
                    b_ref,
                    |b| {
                        if let Err(e) = (|| {
                            match &mut b.inner {
                                NodeType::Branch(n) => {
                                    // from: [Branch] -> [Branch]x -> [Branch]
                                    // to: [Branch] -> [Ext] -> [Branch]
                                    n.children[b_idx as usize] = Some(
                                        self.put_node(Node::from(NodeType::Extension(ExtNode {
                                            path: PartialPath(vec![idx]),
                                            child: c_ptr,
                                            child_encoded: None,
                                        })))?
                                        .as_ptr(),
                                    );
                                }
                                NodeType::Extension(n) => {
                                    // from: [Ext] -> [Branch]x -> [Branch]
                                    // to: [Ext] -> [Branch]
                                    n.path.0.push(idx);
                                    *n.chd_mut() = c_ptr
                                }
                                _ => unreachable!(),
                            }
                            b.rehash();
                            Ok(())
                        })() {
                            err = Some(Err(e))
                        }
                    },
                    parents,
                    deleted
                );
                if let Some(e) = err {
                    return e;
                }
            }
            NodeType::Leaf(_) | NodeType::Extension(_) => match &b_ref.inner {
                NodeType::Branch(_) => {
                    // from: [Branch] -> [Branch]x -> [Leaf/Ext]
                    // to: [Branch] -> [Leaf/Ext]
                    let write_result = c_ref.write(|c| {
                        match &mut c.inner {
                            NodeType::Leaf(n) => &mut n.0,
                            NodeType::Extension(n) => &mut n.path,
                            _ => unreachable!(),
                        }
                        .0
                        .insert(0, idx);
                        c.rehash()
                    });
                    if write_result.is_err() {
                        deleted.push(c_ptr);
                        self.put_node(c_ref.clone())?.as_ptr()
                    } else {
                        c_ptr
                    };
                    drop(c_ref);
                    b_ref
                        .write(|b| {
                            b.inner.as_branch_mut().unwrap().children[b_idx as usize] = Some(c_ptr);
                            b.rehash()
                        })
                        .unwrap();
                }
                NodeType::Extension(n) => {
                    // from: P -> [Ext] -> [Branch]x -> [Leaf/Ext]
                    // to: P -> [Leaf/Ext]
                    let write_result = c_ref.write(|c| {
                        let mut path = n.path.clone().into_inner();
                        path.push(idx);
                        let path0 = match &mut c.inner {
                            NodeType::Leaf(n) => &mut n.0,
                            NodeType::Extension(n) => &mut n.path,
                            _ => unreachable!(),
                        };
                        path.extend(&**path0);
                        *path0 = PartialPath(path);
                        c.rehash()
                    });

                    let c_ptr = if write_result.is_err() {
                        deleted.push(c_ptr);
                        self.put_node(c_ref.clone())?.as_ptr()
                    } else {
                        c_ptr
                    };

                    deleted.push(b_ref.as_ptr());
                    drop(c_ref);
                    set_parent(c_ptr, parents);
                }
                _ => unreachable!(),
            },
        }
        Ok(())
    }

    pub fn remove<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<Vec<u8>>, MerkleError> {
        if root.is_null() {
            return Ok(None);
        }

        let (found, parents, deleted) = {
            let (node_ref, mut parents) =
                self.get_node_and_parents_by_key(self.get_node(root)?, key)?;

            let Some(mut node_ref) = node_ref else {
                return Ok(None);
            };
            let mut deleted = Vec::new();
            let mut found = None;

            match &node_ref.inner {
                NodeType::Branch(n) => {
                    let (c_chd, _) = n.single_child();

                    node_ref
                        .write(|u| {
                            found = u.inner.as_branch_mut().unwrap().value.take();
                            u.rehash()
                        })
                        .unwrap();

                    if let Some((c_ptr, idx)) = c_chd {
                        deleted.push(node_ref.as_ptr());
                        self.after_remove_branch((c_ptr, idx), &mut parents, &mut deleted)?
                    }
                }

                NodeType::Leaf(n) => {
                    found = Some(n.1.clone());
                    deleted.push(node_ref.as_ptr());
                    self.after_remove_leaf(&mut parents, &mut deleted)?
                }
                _ => (),
            };

            (found, parents, deleted)
        };

        for (mut r, _) in parents.into_iter().rev() {
            r.write(|u| u.rehash()).unwrap();
        }

        for ptr in deleted.into_iter() {
            self.free_node(ptr)?;
        }

        Ok(found.map(|e| e.0))
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
            NodeType::Extension(n) => self.remove_tree_(n.chd(), deleted)?,
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
        node_ref: ObjRef<'a>,
        key: K,
    ) -> Result<Option<ObjRef<'a>>, MerkleError> {
        self.get_node_by_key_with_callback(node_ref, key, |_, _| {})
    }

    fn get_node_and_parents_by_key<'a, K: AsRef<[u8]>>(
        &'a self,
        node_ref: ObjRef<'a>,
        key: K,
    ) -> Result<(Option<ObjRef<'a>>, ParentRefs<'a>), MerkleError> {
        let mut parents = Vec::new();
        let node_ref = self.get_node_by_key_with_callback(node_ref, key, |node_ref, nib| {
            parents.push((node_ref, nib));
        })?;

        Ok((node_ref, parents))
    }

    fn get_node_and_parent_addresses_by_key<'a, K: AsRef<[u8]>>(
        &'a self,
        node_ref: ObjRef<'a>,
        key: K,
    ) -> Result<(Option<ObjRef<'a>>, ParentAddresses), MerkleError> {
        let mut parents = Vec::new();
        let node_ref = self.get_node_by_key_with_callback(node_ref, key, |node_ref, nib| {
            parents.push((node_ref.into_ptr(), nib));
        })?;

        Ok((node_ref, parents))
    }

    fn get_node_by_key_with_callback<'a, K: AsRef<[u8]>>(
        &'a self,
        mut node_ref: ObjRef<'a>,
        key: K,
        mut loop_callback: impl FnMut(ObjRef<'a>, u8),
    ) -> Result<Option<ObjRef<'a>>, MerkleError> {
        let mut key_nibbles = Nibbles::<1>::new(key.as_ref()).into_iter();

        loop {
            let Some(nib) = key_nibbles.next() else {
                break;
            };

            let next_ptr = match &node_ref.inner {
                NodeType::Branch(n) => match n.children[nib as usize] {
                    Some(c) => c,
                    None => return Ok(None),
                },
                NodeType::Leaf(n) => {
                    let node_ref = if once(nib).chain(key_nibbles).eq(n.0.iter().copied()) {
                        Some(node_ref)
                    } else {
                        None
                    };

                    return Ok(node_ref);
                }
                NodeType::Extension(n) => {
                    let mut n_path_iter = n.path.iter().copied();

                    if n_path_iter.next() != Some(nib) {
                        return Ok(None);
                    }

                    let path_matches = n_path_iter
                        .map(Some)
                        .all(|n_path_nibble| key_nibbles.next() == n_path_nibble);

                    if !path_matches {
                        return Ok(None);
                    }

                    n.chd()
                }
            };

            loop_callback(node_ref, nib);

            node_ref = self.get_node(next_ptr)?;
        }

        // when we're done iterating over nibbles, check if the node we're at has a value
        let node_ref = match &node_ref.inner {
            NodeType::Branch(n) if n.value.as_ref().is_some() => Some(node_ref),
            NodeType::Leaf(n) if n.0.len() == 0 => Some(node_ref),
            _ => None,
        };

        Ok(node_ref)
    }

    pub fn get_mut<K: AsRef<[u8]>>(
        &mut self,
        key: K,
        root: DiskAddress,
    ) -> Result<Option<RefMut<S>>, MerkleError> {
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
        let key_nibbles = Nibbles::<0>::new(key.as_ref());

        let mut proofs = HashMap::new();
        if root.is_null() {
            return Ok(Proof(proofs));
        }

        // Skip the sentinel root
        let root = self
            .get_node(root)?
            .inner
            .as_branch()
            .ok_or(MerkleError::NotBranchNode)?
            .children[0];
        let mut u_ref = match root {
            Some(root) => self.get_node(root)?,
            None => return Ok(Proof(proofs)),
        };

        let mut nskip = 0;
        let mut nodes: Vec<DiskAddress> = Vec::new();

        // TODO: use get_node_by_key (and write proper unit test)
        for (i, nib) in key_nibbles.into_iter().enumerate() {
            if nskip > 0 {
                nskip -= 1;
                continue;
            }
            nodes.push(u_ref.as_ptr());
            let next_ptr: DiskAddress = match &u_ref.inner {
                NodeType::Branch(n) => match n.children[nib as usize] {
                    Some(c) => c,
                    None => break,
                },
                NodeType::Leaf(_) => break,
                NodeType::Extension(n) => {
                    // the key passed in must match the entire remainder of this
                    // extension node, otherwise we break out
                    let n_path = &n.path;
                    let remaining_path = key_nibbles.into_iter().skip(i);
                    if remaining_path.size_hint().0 < n_path.len() {
                        // all bytes aren't there
                        break;
                    }
                    if !remaining_path.take(n_path.len()).eq(n_path.iter().cloned()) {
                        // contents aren't the same
                        break;
                    }
                    nskip = n_path.len() - 1;
                    n.chd()
                }
            };
            u_ref = self.get_node(next_ptr)?;
        }

        match &u_ref.inner {
            NodeType::Branch(n) => {
                if n.value.as_ref().is_some() {
                    nodes.push(u_ref.as_ptr());
                }
            }
            NodeType::Leaf(n) => {
                if n.0.len() == 0 {
                    nodes.push(u_ref.as_ptr());
                }
            }
            _ => (),
        }

        drop(u_ref);
        // Get the hashes of the nodes.
        for node in nodes {
            let node = self.get_node(node)?;
            let encoded = <&[u8]>::clone(&node.get_encoded::<S>(self.store.as_ref()));
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

    pub(crate) fn get_iter<K: AsRef<[u8]>>(
        &self,
        key: Option<K>,
        root: DiskAddress,
    ) -> Result<MerkleKeyValueStream<'_, S>, MerkleError> {
        Ok(MerkleKeyValueStream {
            key_state: IteratorState::new(key),
            merkle_root: root,
            merkle: self,
        })
    }
}

enum IteratorState<'a> {
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
    fn new<K: AsRef<[u8]>>(starting: Option<K>) -> Self {
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
pub struct MerkleKeyValueStream<'a, S> {
    key_state: IteratorState<'a>,
    merkle_root: DiskAddress,
    merkle: &'a Merkle<S>,
}

impl<'a, S: shale::ShaleStore<node::Node> + Send + Sync> Stream for MerkleKeyValueStream<'a, S> {
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
                    .map_err(|e| api::Error::InternalError(e.into()))?;
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
                                .map_err(|e| api::Error::InternalError(e.into()))?;

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
                    .map_err(|e| api::Error::InternalError(e.into()))?;

                let (found_node, parents) = self
                    .merkle
                    .get_node_and_parents_by_key(root_node, &key)
                    .map_err(|e| api::Error::InternalError(e.into()))?;

                let Some(last_node) = found_node else {
                    return Poll::Ready(None);
                };

                let returned_key_value = match last_node.inner() {
                    NodeType::Branch(branch) => (key, branch.value.to_owned().unwrap().to_vec()),
                    NodeType::Leaf(leaf) => (key, leaf.1.to_vec()),
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
                            .map_err(|e| api::Error::InternalError(e.into()))?;

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
                                        .map_err(|e| api::Error::InternalError(e.into()))?;

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
                    NodeType::Leaf(leaf) => leaf.1.to_vec(),
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
        .chain(leaf.0.to_vec());
    let mut data = Vec::with_capacity(iter.size_hint().0);
    while let (Some(hi), Some(lo)) = (iter.next(), iter.next()) {
        data.push((hi << 4) + lo);
    }
    data
}

fn set_parent(new_chd: DiskAddress, parents: &mut [(ObjRef, u8)]) {
    let (p_ref, idx) = parents.last_mut().unwrap();
    p_ref
        .write(|p| {
            match &mut p.inner {
                NodeType::Branch(pp) => pp.children[*idx as usize] = Some(new_chd),
                NodeType::Extension(pp) => *pp.chd_mut() = new_chd,
                _ => unreachable!(),
            }
            p.rehash();
        })
        .unwrap();
}

pub struct Ref<'a>(ObjRef<'a>);

pub struct RefMut<'a, S> {
    ptr: DiskAddress,
    parents: ParentAddresses,
    merkle: &'a mut Merkle<S>,
}

impl<'a> std::ops::Deref for Ref<'a> {
    type Target = [u8];
    fn deref(&self) -> &[u8] {
        match &self.0.inner {
            NodeType::Branch(n) => n.value.as_ref().unwrap(),
            NodeType::Leaf(n) => &n.1,
            _ => unreachable!(),
        }
    }
}

impl<'a, S: ShaleStore<Node> + Send + Sync> RefMut<'a, S> {
    fn new(ptr: DiskAddress, parents: ParentAddresses, merkle: &'a mut Merkle<S>) -> Self {
        Self {
            ptr,
            parents,
            merkle,
        }
    }

    pub fn get(&self) -> Ref {
        Ref(self.merkle.get_node(self.ptr).unwrap())
    }

    pub fn write(&mut self, modify: impl FnOnce(&mut Vec<u8>)) -> Result<(), MerkleError> {
        let mut deleted = Vec::new();
        {
            let mut u_ref = self.merkle.get_node(self.ptr).unwrap();
            let mut parents: Vec<_> = self
                .parents
                .iter()
                .map(|(ptr, nib)| (self.merkle.get_node(*ptr).unwrap(), *nib))
                .collect();
            write_node!(
                self.merkle,
                u_ref,
                |u| {
                    modify(match &mut u.inner {
                        NodeType::Branch(n) => &mut n.value.as_mut().unwrap().0,
                        NodeType::Leaf(n) => &mut n.1 .0,
                        _ => unreachable!(),
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
pub fn to_nibble_array(x: u8) -> [u8; 2] {
    [x >> 4, x & 0b_0000_1111]
}

// given a set of nibbles, take each pair and convert this back into bytes
// if an odd number of nibbles, in debug mode it panics. In release mode,
// the final nibble is dropped
pub fn from_nibbles(nibbles: &[u8]) -> impl Iterator<Item = u8> + '_ {
    debug_assert_eq!(nibbles.len() & 1, 0);
    nibbles.chunks_exact(2).map(|p| (p[0] << 4) | p[1])
}

#[cfg(test)]
mod tests {
    use super::*;
    use futures::StreamExt;
    use node::tests::{extension, leaf};
    use shale::{cached::DynamicMem, compact::CompactSpace, CachedStore};
    use std::sync::Arc;
    use test_case::test_case;

    #[test_case(vec![0x12, 0x34, 0x56], vec![0x1, 0x2, 0x3, 0x4, 0x5, 0x6])]
    #[test_case(vec![0xc0, 0xff], vec![0xc, 0x0, 0xf, 0xf])]
    fn to_nibbles(bytes: Vec<u8>, nibbles: Vec<u8>) {
        let n: Vec<_> = bytes.into_iter().flat_map(to_nibble_array).collect();
        assert_eq!(n, nibbles);
    }

    fn create_test_merkle() -> Merkle<CompactSpace<Node, DynamicMem>> {
        const RESERVED: usize = 0x1000;

        let mut dm = shale::cached::DynamicMem::new(0x10000, 0);
        let compact_header = DiskAddress::null();
        dm.write(
            compact_header.into(),
            &shale::to_dehydrated(&shale::compact::CompactSpaceHeader::new(
                std::num::NonZeroUsize::new(RESERVED).unwrap(),
                std::num::NonZeroUsize::new(RESERVED).unwrap(),
            ))
            .unwrap(),
        );
        let compact_header = shale::StoredView::ptr_to_obj(
            &dm,
            compact_header,
            shale::compact::CompactHeader::MSIZE,
        )
        .unwrap();
        let mem_meta = Arc::new(dm);
        let mem_payload = Arc::new(DynamicMem::new(0x10000, 0x1));

        let cache = shale::ObjCache::new(1);
        let space =
            shale::compact::CompactSpace::new(mem_meta, mem_payload, compact_header, cache, 10, 16)
                .expect("CompactSpace init fail");

        let store = Box::new(space);
        Merkle::new(store)
    }

    fn branch(value: Vec<u8>, encoded_child: Option<Vec<u8>>) -> Node {
        let children = Default::default();
        let value = Some(value).map(Data);
        let mut children_encoded = <[Option<Vec<u8>>; NBRANCH]>::default();

        if let Some(child) = encoded_child {
            children_encoded[0] = Some(child);
        }

        Node::branch(BranchNode {
            children,
            value,
            children_encoded,
        })
    }

    #[test_case(leaf(vec![1, 2, 3], vec![4, 5]) ; "leaf encoding")]
    #[test_case(branch(b"value".to_vec(), vec![1, 2, 3].into()) ; "branch with value")]
    #[test_case(branch(b"value".to_vec(), None); "branch without value")]
    #[test_case(extension(vec![1, 2, 3], DiskAddress::null(), vec![4, 5].into()) ; "extension without child address")]
    fn encode_(node: Node) {
        let merkle = create_test_merkle();

        let node_ref = merkle.put_node(node).unwrap();
        let encoded = node_ref.get_encoded(merkle.store.as_ref());
        let new_node = Node::from(NodeType::decode(encoded).unwrap());
        let new_node_encoded = new_node.get_encoded(merkle.store.as_ref());

        assert_eq!(encoded, new_node_encoded);
    }

    #[test]
    fn insert_and_retrieve() {
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

    #[tokio::test]
    async fn iterate_empty() {
        let merkle = create_test_merkle();
        let root = merkle.init_root().unwrap();
        let mut it = merkle.get_iter(Some(b"x"), root).unwrap();
        let next = it.next().await;
        assert!(next.is_none())
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
            let next = it.next().await.unwrap().unwrap();
            assert_eq!(next.0, next.1,);
            assert_eq!(next.1, vec![k]);
        }
        assert!(it.next().await.is_none());

        // ensure that reading past the end returns all the values
        for k in u8::MIN..=u8::MAX {
            let next = it.next().await.unwrap().unwrap();
            assert_eq!(next.0, next.1);
            assert_eq!(next.1, vec![k]);
        }
        assert!(it.next().await.is_none());
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

            let removed_val = merkle.remove(key, root).unwrap();
            assert_eq!(removed_val.as_deref(), val.as_slice().into());

            let fetched_val = merkle.get(key, root).unwrap();
            assert!(fetched_val.is_none());
        }
    }
}
