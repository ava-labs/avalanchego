// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::cmp::Ordering;
use std::collections::HashMap;

use crate::shale::ObjWriteSizeError;
use crate::shale::{disk_address::DiskAddress, ShaleError, ShaleStore};
use crate::v2::api::HashKey;
use nix::errno::Errno;
use sha3::Digest;
use thiserror::Error;

use crate::nibbles::Nibbles;
use crate::nibbles::NibblesIterator;
use crate::{
    db::DbError,
    merkle::{to_nibble_array, Merkle, MerkleError, Node, NodeType},
    merkle_util::{new_merkle, DataStoreError, MerkleSetup},
};

use super::{BinarySerde, NodeObjRef};

#[derive(Debug, Error)]
pub enum ProofError {
    #[error("decoding error")]
    DecodeError(#[from] bincode::Error),
    #[error("no such node")]
    NoSuchNode,
    #[error("proof node missing")]
    ProofNodeMissing,
    #[error("inconsistent proof data")]
    InconsistentProofData,
    #[error("non-monotonic range increase")]
    NonMonotonicIncreaseRange,
    #[error("invalid data")]
    InvalidData,
    #[error("invalid proof")]
    InvalidProof,
    #[error("invalid edge keys")]
    InvalidEdgeKeys,
    #[error("node insertion error")]
    NodesInsertionError,
    #[error("node not in trie")]
    NodeNotInTrie,
    #[error("invalid node {0:?}")]
    InvalidNode(#[from] MerkleError),
    #[error("empty range")]
    EmptyRange,
    #[error("fork left")]
    ForkLeft,
    #[error("fork right")]
    ForkRight,
    #[error("system error: {0:?}")]
    SystemError(Errno),
    #[error("shale error: {0:?}")]
    Shale(ShaleError),
    #[error("invalid root hash")]
    InvalidRootHash,
    #[error("{0}")]
    WriteError(#[from] ObjWriteSizeError),
}

impl From<DataStoreError> for ProofError {
    fn from(d: DataStoreError) -> ProofError {
        match d {
            DataStoreError::InsertionError => ProofError::NodesInsertionError,
            DataStoreError::RootHashError => ProofError::InvalidRootHash,
            _ => ProofError::InvalidProof,
        }
    }
}

impl From<DbError> for ProofError {
    fn from(d: DbError) -> ProofError {
        match d {
            DbError::InvalidParams => ProofError::InvalidProof,
            DbError::Merkle(e) => ProofError::InvalidNode(e),
            DbError::System(e) => ProofError::SystemError(e),
            DbError::KeyNotFound => ProofError::InvalidEdgeKeys,
            DbError::CreateError => ProofError::NoSuchNode,
            // TODO: fix better by adding a new error to ProofError
            #[allow(clippy::unwrap_used)]
            DbError::IO(e) => {
                ProofError::SystemError(nix::errno::Errno::from_i32(e.raw_os_error().unwrap()))
            }
            DbError::Shale(e) => ProofError::Shale(e),
            DbError::InvalidProposal => ProofError::InvalidProof,
        }
    }
}

/// A proof that a single key is present
///
/// The generic N represents the storage for the node data
#[derive(Clone, Debug)]
pub struct Proof<N>(pub HashMap<HashKey, N>);

/// `SubProof` contains the value or the hash of a node that maps
/// to a single proof step. If reaches an end step during proof verification,
/// the `SubProof` should be the `Value` variant.

#[derive(Debug)]
enum SubProof {
    Data(Vec<u8>),
    Hash(HashKey),
}

impl<N: AsRef<[u8]> + Send> Proof<N> {
    /// verify_proof checks merkle proofs. The given proof must contain the value for
    /// key in a trie with the given root hash. VerifyProof returns an error if the
    /// proof contains invalid trie nodes or the wrong value.
    ///
    /// The generic N represents the storage for the node data
    pub fn verify<K: AsRef<[u8]>>(
        &self,
        key: K,
        root_hash: HashKey,
    ) -> Result<Option<Vec<u8>>, ProofError> {
        let mut key_nibbles = Nibbles::<0>::new(key.as_ref()).into_iter();

        let mut cur_hash = root_hash;
        let proofs_map = &self.0;

        loop {
            let cur_proof = proofs_map
                .get(&cur_hash)
                .ok_or(ProofError::ProofNodeMissing)?;

            let node = NodeType::decode(cur_proof.as_ref())?;
            // TODO: I think this will currently fail if the key is &[];
            let (sub_proof, traversed_nibbles) = locate_subproof(key_nibbles, node)?;
            key_nibbles = traversed_nibbles;

            cur_hash = match sub_proof {
                // Return when reaching the end of the key.
                Some(SubProof::Data(value)) if key_nibbles.is_empty() => return Ok(Some(value)),
                // The trie doesn't contain the key.
                Some(SubProof::Hash(hash)) => hash,
                _ => return Ok(None),
            };
        }
    }

    pub fn extend(&mut self, other: Proof<N>) {
        self.0.extend(other.0)
    }

    pub fn verify_range_proof<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self,
        root_hash: HashKey,
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        vals: Vec<V>,
    ) -> Result<bool, ProofError> {
        if keys.len() != vals.len() {
            return Err(ProofError::InconsistentProofData);
        }

        // Ensure the received batch is monotonic increasing and contains no deletions
        #[allow(clippy::indexing_slicing)]
        if !keys.windows(2).all(|w| w[0].as_ref() < w[1].as_ref()) {
            return Err(ProofError::NonMonotonicIncreaseRange);
        }

        // Use in-memory merkle
        let mut merkle_setup = new_merkle(0x10000, 0x10000);

        // Special case, there is no edge proof at all. The given range is expected
        // to be the whole leaf-set in the trie.
        if self.0.is_empty() {
            for (index, k) in keys.iter().enumerate() {
                #[allow(clippy::indexing_slicing)]
                merkle_setup.insert(k, vals[index].as_ref().to_vec())?;
            }

            let merkle_root = &*merkle_setup.root_hash()?;

            return if merkle_root == &root_hash {
                Ok(false)
            } else {
                Err(ProofError::InvalidProof)
            };
        }

        // Special case when there is a provided edge proof but zero key/value pairs,
        // ensure there are no more accounts / slots in the trie.
        if keys.is_empty() {
            let proof_to_path =
                self.proof_to_path(first_key, root_hash, &mut merkle_setup, true)?;
            return match proof_to_path {
                Some(_) => Err(ProofError::InvalidData),
                None => Ok(false),
            };
        }

        // Special case, there is only one element and two edge keys are same.
        // In this case, we can't construct two edge paths. So handle it here.
        if keys.len() == 1 && first_key.as_ref() == last_key.as_ref() {
            let data =
                self.proof_to_path(first_key.as_ref(), root_hash, &mut merkle_setup, false)?;

            #[allow(clippy::indexing_slicing)]
            return if first_key.as_ref() != keys[0].as_ref() {
                // correct proof but invalid key
                Err(ProofError::InvalidEdgeKeys)
            } else {
                match data {
                    #[allow(clippy::indexing_slicing)]
                    Some(val) if val == vals[0].as_ref() => Ok(true),
                    None => Ok(false),
                    _ => Err(ProofError::InvalidData),
                }
            };
        }

        // Ok, in all other cases, we require two edge paths available.
        // First check the validity of edge keys.
        if first_key.as_ref() >= last_key.as_ref() {
            return Err(ProofError::InvalidEdgeKeys);
        }

        // Convert the edge proofs to edge trie paths. Then we can
        // have the same tree architecture with the original one.
        // For the first edge proof, non-existent proof is allowed.
        self.proof_to_path(first_key.as_ref(), root_hash, &mut merkle_setup, true)?;

        // Pass the root node here, the second path will be merged
        // with the first one. For the last edge proof, non-existent
        // proof is also allowed.
        self.proof_to_path(last_key.as_ref(), root_hash, &mut merkle_setup, true)?;

        // Remove all internal caculated values. All the removed parts should
        // be re-filled(or re-constructed) by the given leaves range.
        let fork_at_root =
            unset_internal(&mut merkle_setup, first_key.as_ref(), last_key.as_ref())?;

        // If the fork point is the root, the trie should be empty, start with a new one.
        if fork_at_root {
            merkle_setup = new_merkle(0x100000, 0x100000);
        }

        for (key, val) in keys.iter().zip(vals.iter()) {
            merkle_setup.insert(key.as_ref(), val.as_ref().to_vec())?;
        }

        // Calculate the hash
        let merkle_root = &*merkle_setup.root_hash()?;

        if merkle_root == &root_hash {
            Ok(true)
        } else {
            Err(ProofError::InvalidProof)
        }
    }

    /// proofToPath converts a merkle proof to trie node path. The main purpose of
    /// this function is recovering a node path from the merkle proof stream. All
    /// necessary nodes will be resolved and leave the remaining as hashnode.
    ///
    /// The given edge proof is allowed to be an existent or non-existent proof.
    fn proof_to_path<K: AsRef<[u8]>, S: ShaleStore<Node> + Send + Sync, T: BinarySerde>(
        &self,
        key: K,
        root_hash: HashKey,
        merkle_setup: &mut MerkleSetup<S, T>,
        allow_non_existent_node: bool,
    ) -> Result<Option<Vec<u8>>, ProofError> {
        // Start with the sentinel root
        let sentinel = merkle_setup.get_sentinel_address();
        let merkle = merkle_setup.get_merkle_mut();
        let mut parent_node_ref = merkle
            .get_node(sentinel)
            .map_err(|_| ProofError::NoSuchNode)?;

        let mut key_nibbles = Nibbles::<1>::new(key.as_ref()).into_iter().peekable();

        let mut child_hash = root_hash;
        let proofs_map = &self.0;

        let sub_proof = loop {
            // Link the child to the parent based on the node type.
            // if a child is already linked, use it instead
            let child_node = match &parent_node_ref.inner() {
                #[allow(clippy::indexing_slicing)]
                NodeType::Branch(n) => {
                    let Some(child_index) = key_nibbles.next().map(usize::from) else {
                        break None;
                    };

                    match n.chd()[child_index] {
                        // If the child already resolved, then use the existing node.
                        Some(node) => merkle.get_node(node)?,
                        None => {
                            let child_node = decode_subproof(merkle, proofs_map, &child_hash)?;

                            // insert the leaf to the empty slot
                            parent_node_ref.write(|node| {
                                #[allow(clippy::indexing_slicing)]
                                let node = node
                                    .inner_mut()
                                    .as_branch_mut()
                                    .expect("parent_node_ref must be a branch");
                                node.chd_mut()[child_index] = Some(child_node.as_ptr());
                            })?;

                            child_node
                        }
                    }
                }

                // We should not hit a leaf node as a parent.
                _ => return Err(ProofError::InvalidNode(MerkleError::ParentLeafBranch)),
            };

            // find the encoded subproof of the child if the partial-path and nibbles match
            let encoded_sub_proof = match child_node.inner() {
                NodeType::Leaf(n) => {
                    break n
                        .path
                        .iter()
                        .copied()
                        .eq(key_nibbles) // all nibbles have to match
                        .then(|| n.data().to_vec());
                }

                NodeType::Branch(n) => {
                    let paths_match = n
                        .path
                        .iter()
                        .copied()
                        .all(|nibble| Some(nibble) == key_nibbles.next());

                    if !paths_match {
                        break None;
                    }

                    if let Some(index) = key_nibbles.peek() {
                        let subproof = n
                            .chd_encode()
                            .get(*index as usize)
                            .and_then(|inner| inner.as_ref())
                            .map(|data| &**data);

                        subproof
                    } else {
                        break n.value.as_ref().map(|data| data.to_vec());
                    }
                }
            };

            match encoded_sub_proof {
                None => break None,
                Some(encoded) => {
                    let hash = generate_subproof_hash(encoded)?;
                    child_hash = hash;
                }
            }

            parent_node_ref = child_node;
        };

        match sub_proof {
            Some(data) => Ok(Some(data)),
            None if allow_non_existent_node => Ok(None),
            None => Err(ProofError::NodeNotInTrie),
        }
    }
}

fn decode_subproof<'a, S: ShaleStore<Node>, T, N: AsRef<[u8]>>(
    merkle: &'a Merkle<S, T>,
    proofs_map: &HashMap<HashKey, N>,
    child_hash: &HashKey,
) -> Result<NodeObjRef<'a>, ProofError> {
    let child_proof = proofs_map
        .get(child_hash)
        .ok_or(ProofError::ProofNodeMissing)?;
    let child_node = NodeType::decode(child_proof.as_ref())?;
    let node = merkle.put_node(Node::from(child_node))?;
    Ok(node)
}

fn locate_subproof(
    mut key_nibbles: NibblesIterator<'_, 0>,
    node: NodeType,
) -> Result<(Option<SubProof>, NibblesIterator<'_, 0>), ProofError> {
    match node {
        NodeType::Leaf(n) => {
            let cur_key = &n.path().0;
            // Check if the key of current node match with the given key
            // and consume the current-key portion of the nibbles-iterator
            let does_not_match = key_nibbles.size_hint().0 < cur_key.len()
                || !cur_key.iter().all(|val| key_nibbles.next() == Some(*val));

            if does_not_match {
                return Ok((None, Nibbles::<0>::new(&[]).into_iter()));
            }

            let encoded: Vec<u8> = n.data().to_vec();

            let sub_proof = SubProof::Data(encoded);

            Ok((sub_proof.into(), key_nibbles))
        }
        NodeType::Branch(n) => {
            let partial_path = &n.path.0;

            let does_not_match = key_nibbles.size_hint().0 < partial_path.len()
                || !partial_path
                    .iter()
                    .all(|val| key_nibbles.next() == Some(*val));

            if does_not_match {
                return Ok((None, Nibbles::<0>::new(&[]).into_iter()));
            }

            let Some(index) = key_nibbles.next().map(|nib| nib as usize) else {
                let encoded = n.value;

                let sub_proof = encoded.map(|encoded| SubProof::Data(encoded.into_inner()));

                return Ok((sub_proof, key_nibbles));
            };

            // consume items returning the item at index
            #[allow(clippy::indexing_slicing)]
            let data = n.chd_encode()[index]
                .as_ref()
                .ok_or(ProofError::InvalidData)?;
            generate_subproof(data).map(|subproof| (Some(subproof), key_nibbles))
        }
    }
}

fn generate_subproof_hash(encoded: &[u8]) -> Result<HashKey, ProofError> {
    match encoded.len() {
        0..=31 => {
            let sub_hash = sha3::Keccak256::digest(encoded).into();
            Ok(sub_hash)
        }

        32 => {
            let sub_hash = encoded
                .try_into()
                .expect("slice length checked in match arm");

            Ok(sub_hash)
        }

        len => Err(ProofError::DecodeError(Box::new(
            bincode::ErrorKind::Custom(format!("invalid proof length: {len}")),
        ))),
    }
}

fn generate_subproof(encoded: &[u8]) -> Result<SubProof, ProofError> {
    Ok(SubProof::Hash(generate_subproof_hash(encoded)?))
}

// unset_internal removes all internal node references.
// It should be called after a trie is constructed with two edge paths. Also
// the given boundary keys must be the one used to construct the edge paths.
//
// It's the key step for range proof. The precalculated encoded value of all internal
// nodes should be removed. But if the proof is valid,
// the missing children will be filled, otherwise it will be thrown anyway.
//
// Note we have the assumption here the given boundary keys are different
// and right is larger than left.
//
// The return value indicates if the fork point is root node. If so, unset the
// entire trie.
fn unset_internal<K: AsRef<[u8]>, S: ShaleStore<Node> + Send + Sync, T: BinarySerde>(
    merkle_setup: &mut MerkleSetup<S, T>,
    left: K,
    right: K,
) -> Result<bool, ProofError> {
    // Add the sentinel root
    let mut left_chunks = vec![0];
    left_chunks.extend(left.as_ref().iter().copied().flat_map(to_nibble_array));
    // Add the sentinel root
    let mut right_chunks = vec![0];
    right_chunks.extend(right.as_ref().iter().copied().flat_map(to_nibble_array));
    let root = merkle_setup.get_sentinel_address();
    let merkle = merkle_setup.get_merkle_mut();
    let mut u_ref = merkle.get_node(root).map_err(|_| ProofError::NoSuchNode)?;
    let mut parent = DiskAddress::null();

    let mut fork_left = Ordering::Equal;
    let mut fork_right = Ordering::Equal;

    let mut index = 0;
    loop {
        match &u_ref.inner() {
            #[allow(clippy::indexing_slicing)]
            NodeType::Branch(n) => {
                // If either the key of left proof or right proof doesn't match with
                // stop here, this is the forkpoint.
                let path = &*n.path;

                if !path.is_empty() {
                    [fork_left, fork_right] = [&left_chunks[index..], &right_chunks[index..]]
                        .map(|chunks| chunks.chunks(path.len()).next().unwrap_or_default())
                        .map(|key| key.cmp(path));

                    if !fork_left.is_eq() || !fork_right.is_eq() {
                        break;
                    }

                    index += path.len();
                }

                // If either the node pointed by left proof or right proof is nil,
                // stop here and the forkpoint is the fullnode.
                #[allow(clippy::indexing_slicing)]
                let left_node = n.chd()[left_chunks[index] as usize];
                #[allow(clippy::indexing_slicing)]
                let right_node = n.chd()[right_chunks[index] as usize];

                match (left_node.as_ref(), right_node.as_ref()) {
                    (None, _) | (_, None) => break,
                    (left, right) if left != right => break,
                    _ => (),
                };

                parent = u_ref.as_ptr();
                u_ref = merkle.get_node(left_node.expect("left_node none"))?;
                index += 1;
            }

            #[allow(clippy::indexing_slicing)]
            NodeType::Leaf(n) => {
                let path = &*n.path;

                [fork_left, fork_right] = [&left_chunks[index..], &right_chunks[index..]]
                    .map(|chunks| chunks.chunks(path.len()).next().unwrap_or_default())
                    .map(|key| key.cmp(path));

                break;
            }
        }
    }

    match &u_ref.inner() {
        NodeType::Branch(n) => {
            if fork_left.is_lt() && fork_right.is_lt() {
                return Err(ProofError::EmptyRange);
            }

            if fork_left.is_gt() && fork_right.is_gt() {
                return Err(ProofError::EmptyRange);
            }

            if fork_left.is_ne() && fork_right.is_ne() {
                // The fork point is root node, unset the entire trie
                if parent.is_null() {
                    return Ok(true);
                }

                let mut p_ref = merkle
                    .get_node(parent)
                    .map_err(|_| ProofError::NoSuchNode)?;
                #[allow(clippy::unwrap_used)]
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        #[allow(clippy::indexing_slicing)]
                        (pp.chd_mut()[left_chunks[index - 1] as usize] = None);
                        #[allow(clippy::indexing_slicing)]
                        (pp.chd_encoded_mut()[left_chunks[index - 1] as usize] = None);
                    })
                    .unwrap();

                return Ok(false);
            }

            let p = u_ref.as_ptr();
            index += n.path.len();

            // Only one proof points to non-existent key.
            if fork_right.is_ne() {
                #[allow(clippy::indexing_slicing)]
                let left_node = n.chd()[left_chunks[index] as usize];

                drop(u_ref);
                #[allow(clippy::indexing_slicing)]
                unset_node_ref(merkle, p, left_node, &left_chunks[index..], 1, false)?;
                return Ok(false);
            }

            if fork_left.is_ne() {
                #[allow(clippy::indexing_slicing)]
                let right_node = n.chd()[right_chunks[index] as usize];

                drop(u_ref);
                #[allow(clippy::indexing_slicing)]
                unset_node_ref(merkle, p, right_node, &right_chunks[index..], 1, true)?;
                return Ok(false);
            };

            #[allow(clippy::indexing_slicing)]
            let left_node = n.chd()[left_chunks[index] as usize];
            #[allow(clippy::indexing_slicing)]
            let right_node = n.chd()[right_chunks[index] as usize];

            // unset all internal nodes calculated encoded value in the forkpoint
            #[allow(clippy::indexing_slicing, clippy::unwrap_used)]
            for i in left_chunks[index] + 1..right_chunks[index] {
                u_ref
                    .write(|u| {
                        let uu = u.inner_mut().as_branch_mut().unwrap();
                        #[allow(clippy::indexing_slicing)]
                        (uu.chd_mut()[i as usize] = None);
                        #[allow(clippy::indexing_slicing)]
                        (uu.chd_encoded_mut()[i as usize] = None);
                    })
                    .unwrap();
            }

            drop(u_ref);

            #[allow(clippy::indexing_slicing)]
            unset_node_ref(merkle, p, left_node, &left_chunks[index..], 1, false)?;

            #[allow(clippy::indexing_slicing)]
            unset_node_ref(merkle, p, right_node, &right_chunks[index..], 1, true)?;

            Ok(false)
        }

        NodeType::Leaf(_) => {
            if fork_left.is_lt() && fork_right.is_lt() {
                return Err(ProofError::EmptyRange);
            }

            if fork_left.is_gt() && fork_right.is_gt() {
                return Err(ProofError::EmptyRange);
            }

            let mut p_ref = merkle
                .get_node(parent)
                .map_err(|_| ProofError::NoSuchNode)?;

            #[allow(clippy::unwrap_used)]
            if fork_left.is_ne() && fork_right.is_ne() {
                p_ref
                    .write(|p| {
                        if let NodeType::Branch(n) = p.inner_mut() {
                            #[allow(clippy::indexing_slicing)]
                            (n.chd_mut()[left_chunks[index - 1] as usize] = None);
                            #[allow(clippy::indexing_slicing)]
                            (n.chd_encoded_mut()[left_chunks[index - 1] as usize] = None);
                        }
                    })
                    .unwrap();
            } else if fork_right.is_ne() {
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        #[allow(clippy::indexing_slicing)]
                        (pp.chd_mut()[left_chunks[index - 1] as usize] = None);
                        #[allow(clippy::indexing_slicing)]
                        (pp.chd_encoded_mut()[left_chunks[index - 1] as usize] = None);
                    })
                    .unwrap();
            } else if fork_left.is_ne() {
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        #[allow(clippy::indexing_slicing)]
                        (pp.chd_mut()[right_chunks[index - 1] as usize] = None);
                        #[allow(clippy::indexing_slicing)]
                        (pp.chd_encoded_mut()[right_chunks[index - 1] as usize] = None);
                    })
                    .unwrap();
            }

            Ok(false)
        }
    }
}

// unset removes all internal node references either the left most or right most.
// It can meet these scenarios:
//
//   - The given path is existent in the trie, unset the associated nodes with the
//     specific direction
//   - The given path is non-existent in the trie
//   - the fork point is a fullnode, the corresponding child pointed by path
//     is nil, return
//   - the fork point is a shortnode, the shortnode is included in the range,
//     keep the entire branch and return.
//   - the fork point is a shortnode, the shortnode is excluded in the range,
//     unset the entire branch.
fn unset_node_ref<K: AsRef<[u8]>, S: ShaleStore<Node> + Send + Sync, T: BinarySerde>(
    merkle: &Merkle<S, T>,
    parent: DiskAddress,
    node: Option<DiskAddress>,
    key: K,
    index: usize,
    remove_left: bool,
) -> Result<(), ProofError> {
    let Some(node) = node else {
        // If the node is nil, then it's a child of the fork point
        // fullnode(it's a non-existent branch).
        return Ok(());
    };

    let mut chunks = Vec::new();
    chunks.extend(key.as_ref());

    #[allow(clippy::unwrap_used)]
    let mut u_ref = merkle.get_node(node).map_err(|_| ProofError::NoSuchNode)?;
    let p = u_ref.as_ptr();

    if index >= chunks.len() {
        return Err(ProofError::InvalidProof);
    }

    #[allow(clippy::indexing_slicing)]
    match &u_ref.inner() {
        NodeType::Branch(n) if chunks[index..].starts_with(&n.path) => {
            let index = index + n.path.len();
            let child_index = chunks[index] as usize;

            let node = n.chd()[child_index];

            let iter = if remove_left {
                0..child_index
            } else {
                child_index + 1..16
            };

            #[allow(clippy::unwrap_used)]
            for i in iter {
                u_ref
                    .write(|u| {
                        let uu = u.inner_mut().as_branch_mut().unwrap();
                        #[allow(clippy::indexing_slicing)]
                        (uu.chd_mut()[i] = None);
                        #[allow(clippy::indexing_slicing)]
                        (uu.chd_encoded_mut()[i] = None);
                    })
                    .unwrap();
            }

            drop(u_ref);

            unset_node_ref(merkle, p, node, key, index + 1, remove_left)
        }

        NodeType::Branch(n) => {
            let cur_key = &n.path;

            // Find the fork point, it's a non-existent branch.
            //
            // for (true, Ordering::Less)
            // The key of fork shortnode is less than the path
            // (it belongs to the range), unset the entire
            // branch. The parent must be a fullnode.
            //
            // for (false, Ordering::Greater)
            // The key of fork shortnode is greater than the
            // path(it belongs to the range), unset the entrie
            // branch. The parent must be a fullnode. Otherwise the
            // key is not part of the range and should remain in the
            // cached hash.
            #[allow(clippy::indexing_slicing)]
            let should_unset_entire_branch = matches!(
                (remove_left, cur_key.cmp(&chunks[index..])),
                (true, Ordering::Less) | (false, Ordering::Greater)
            );

            #[allow(clippy::indexing_slicing, clippy::unwrap_used)]
            if should_unset_entire_branch {
                let mut p_ref = merkle
                    .get_node(parent)
                    .map_err(|_| ProofError::NoSuchNode)?;

                p_ref
                    .write(|p| match p.inner_mut() {
                        NodeType::Branch(pp) => {
                            pp.chd_mut()[chunks[index - 1] as usize] = None;
                            pp.chd_encoded_mut()[chunks[index - 1] as usize] = None;
                        }
                        NodeType::Leaf(_) => (),
                    })
                    .unwrap();
            }

            Ok(())
        }

        NodeType::Leaf(n) => {
            let mut p_ref = merkle
                .get_node(parent)
                .map_err(|_| ProofError::NoSuchNode)?;
            let cur_key = n.path();

            // Similar to branch node, we need to compare the path to see if the node
            // needs to be unset.
            #[allow(clippy::indexing_slicing)]
            if !(chunks[index..]).starts_with(cur_key) {
                #[allow(clippy::indexing_slicing)]
                match (cur_key.cmp(&chunks[index..]), remove_left) {
                    (Ordering::Greater, false) | (Ordering::Less, true) => {
                        p_ref
                            .write(|p| {
                                let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                                let index = chunks[index - 1] as usize;
                                pp.chd_mut()[index] = None;
                                pp.chd_encoded_mut()[index] = None;
                            })
                            .expect("node write failure");
                    }
                    _ => (),
                }
            } else {
                p_ref
                    .write(|p| {
                        if let NodeType::Branch(n) = p.inner_mut() {
                            #[allow(clippy::indexing_slicing)]
                            let index = chunks[index - 1] as usize;

                            n.chd_mut()[index] = None;
                            n.chd_encoded_mut()[index] = None;
                        }
                    })
                    .expect("node write failure");
            }

            Ok(())
        }
    }
}
