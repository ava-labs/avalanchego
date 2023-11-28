// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::cmp::Ordering;
use std::ops::Deref;

use crate::shale::{disk_address::DiskAddress, ShaleError, ShaleStore};
use nix::errno::Errno;
use sha3::Digest;
use thiserror::Error;

use crate::nibbles::Nibbles;
use crate::nibbles::NibblesIterator;
use crate::{
    db::DbError,
    merkle::{to_nibble_array, Merkle, MerkleError, Node, NodeType},
    merkle_util::{new_merkle, DataStoreError, MerkleSetup},
    v2::api::Proof,
};

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
    #[error("range has deletion")]
    RangeHasDeletion,
    #[error("invalid data")]
    InvalidData,
    #[error("invalid proof")]
    InvalidProof,
    #[error("invalid edge keys")]
    InvalidEdgeKeys,
    #[error("inconsisent edge keys")]
    InconsistentEdgeKeys,
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
            DbError::IO(e) => {
                ProofError::SystemError(nix::errno::Errno::from_i32(e.raw_os_error().unwrap()))
            }
            DbError::Shale(e) => ProofError::Shale(e),
            DbError::InvalidProposal => ProofError::InvalidProof,
        }
    }
}

/// SubProof contains the encoded value and the hash value of a node that maps
/// to a single proof step. If reaches an end step during proof verification,
/// the hash value will be none, and the encoded value will be the value of the
/// node.

#[derive(Debug)]
struct SubProof {
    encoded: Vec<u8>,
    hash: Option<[u8; 32]>,
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
        root_hash: [u8; 32],
    ) -> Result<Option<Vec<u8>>, ProofError> {
        let mut key_nibbles = Nibbles::<0>::new(key.as_ref()).into_iter();

        let mut cur_hash = root_hash;
        let proofs_map = &self.0;

        let value_node = loop {
            let cur_proof = proofs_map
                .get(&cur_hash)
                .ok_or(ProofError::ProofNodeMissing)?;

            let node = NodeType::decode(cur_proof.as_ref())?;
            let (sub_proof, traversed_nibbles) = locate_subproof(key_nibbles, node)?;
            key_nibbles = traversed_nibbles;

            cur_hash = match sub_proof {
                // Return when reaching the end of the key.
                Some(p) if key_nibbles.is_empty() => break p.encoded,
                // The trie doesn't contain the key.
                Some(SubProof {
                    hash: Some(hash), ..
                }) => hash,
                _ => return Ok(None),
            };
        };

        let value = match NodeType::decode(&value_node) {
            Ok(NodeType::Branch(branch)) => branch.value().as_ref().map(|v| v.to_vec()),
            Ok(NodeType::Leaf(leaf)) => leaf.data().to_vec().into(),
            _ => return Ok(value_node.into()),
        };

        Ok(value)
    }

    pub fn concat_proofs(&mut self, other: Proof<N>) {
        self.0.extend(other.0)
    }

    pub fn verify_range_proof<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self,
        root_hash: [u8; 32],
        first_key: K,
        last_key: K,
        keys: Vec<K>,
        vals: Vec<V>,
    ) -> Result<bool, ProofError> {
        if keys.len() != vals.len() {
            return Err(ProofError::InconsistentProofData);
        }

        // Ensure the received batch is monotonic increasing and contains no deletions
        if !keys.windows(2).all(|w| w[0].as_ref() < w[1].as_ref()) {
            return Err(ProofError::NonMonotonicIncreaseRange);
        }

        if !vals.iter().all(|v| !v.as_ref().is_empty()) {
            return Err(ProofError::RangeHasDeletion);
        }

        // Use in-memory merkle
        let mut merkle_setup = new_merkle(0x10000, 0x10000);

        // Special case, there is no edge proof at all. The given range is expected
        // to be the whole leaf-set in the trie.
        if self.0.is_empty() {
            for (index, k) in keys.iter().enumerate() {
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

            return if first_key.as_ref() != keys[0].as_ref() {
                // correct proof but invalid key
                Err(ProofError::InvalidEdgeKeys)
            } else {
                match data {
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

        // TODO(Hao): different length edge keys should be supported
        if first_key.as_ref().len() != last_key.as_ref().len() {
            return Err(ProofError::InconsistentEdgeKeys);
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
    fn proof_to_path<KV: AsRef<[u8]>, S: ShaleStore<Node> + Send + Sync>(
        &self,
        key: KV,
        root_hash: [u8; 32],
        merkle_setup: &mut MerkleSetup<S>,
        allow_non_existent_node: bool,
    ) -> Result<Option<Vec<u8>>, ProofError> {
        // Start with the sentinel root
        let root = merkle_setup.get_root();
        let merkle = merkle_setup.get_merkle_mut();
        let mut u_ref = merkle.get_node(root).map_err(|_| ProofError::NoSuchNode)?;

        let mut chunks = Vec::new();
        chunks.extend(key.as_ref().iter().copied().flat_map(to_nibble_array));

        let mut cur_key: &[u8] = &chunks;
        let mut cur_hash = root_hash;
        let proofs_map = &self.0;
        let mut key_index = 0;
        let mut branch_index = 0;

        loop {
            let cur_proof = proofs_map
                .get(&cur_hash)
                .ok_or(ProofError::ProofNodeMissing)?;
            // TODO(Hao): (Optimization) If a node is already decode we don't need to decode again.
            let (mut chd_ptr, sub_proof, size) =
                self.decode_node(merkle, cur_key, cur_proof.as_ref(), false)?;

            // Link the child to the parent based on the node type.
            match &u_ref.inner() {
                NodeType::Branch(n) => match n.chd()[branch_index] {
                    // If the child already resolved, then use the existing node.
                    Some(node) => {
                        chd_ptr = node;
                    }
                    None => {
                        // insert the leaf to the empty slot
                        u_ref
                            .write(|u| {
                                let uu = u.inner_mut().as_branch_mut().unwrap();
                                uu.chd_mut()[branch_index] = Some(chd_ptr);
                            })
                            .unwrap();
                    }
                },

                NodeType::Extension(_) => {
                    // If the child already resolved, then use the existing node.
                    let node = u_ref.inner().as_extension().unwrap().chd();

                    if node.is_null() {
                        u_ref
                            .write(|u| {
                                let uu = u.inner_mut().as_extension_mut().unwrap();
                                *uu.chd_mut() = chd_ptr;
                            })
                            .unwrap();
                    } else {
                        chd_ptr = node;
                    }
                }

                // We should not hit a leaf node as a parent.
                _ => return Err(ProofError::InvalidNode(MerkleError::ParentLeafBranch)),
            };

            u_ref = merkle.get_node(chd_ptr)?;

            // If the new parent is a branch node, record the index to correctly link the next child to it.
            if u_ref.inner().as_branch().is_some() {
                branch_index = chunks[key_index] as usize;
            }

            key_index += size;

            match sub_proof {
                // The trie doesn't contain the key. It's possible
                // the proof is a non-existing proof, but at least
                // we can prove all resolved nodes are correct, it's
                // enough for us to prove range.
                None => {
                    return allow_non_existent_node
                        .then_some(None)
                        .ok_or(ProofError::NodeNotInTrie);
                }
                Some(p) => {
                    // Return when reaching the end of the key.
                    if key_index == chunks.len() {
                        cur_key = &chunks[key_index..];
                        let mut data = None;

                        // Decode the last subproof to get the value.
                        if let Some(p_hash) = p.hash.as_ref() {
                            let proof =
                                proofs_map.get(p_hash).ok_or(ProofError::ProofNodeMissing)?;

                            chd_ptr = self.decode_node(merkle, cur_key, proof.as_ref(), true)?.0;

                            // Link the child to the parent based on the node type.
                            match &u_ref.inner() {
                                NodeType::Branch(n) if n.chd()[branch_index].is_none() => {
                                    // insert the leaf to the empty slot
                                    u_ref
                                        .write(|u| {
                                            let uu = u.inner_mut().as_branch_mut().unwrap();
                                            uu.chd_mut()[branch_index] = Some(chd_ptr);
                                        })
                                        .unwrap();
                                }

                                // If the child already resolved, then use the existing node.
                                NodeType::Branch(_) => {}

                                NodeType::Extension(_)
                                    if u_ref.inner().as_extension().unwrap().chd().is_null() =>
                                {
                                    u_ref
                                        .write(|u| {
                                            let uu = u.inner_mut().as_extension_mut().unwrap();
                                            *uu.chd_mut() = chd_ptr;
                                        })
                                        .unwrap();
                                }

                                // If the child already resolved, then use the existing node.
                                NodeType::Extension(_) => {}

                                // We should not hit a leaf node as a parent.
                                _ => {
                                    return Err(ProofError::InvalidNode(
                                        MerkleError::ParentLeafBranch,
                                    ))
                                }
                            };
                        }

                        drop(u_ref);

                        let c_ref = merkle.get_node(chd_ptr)?;

                        match &c_ref.inner() {
                            NodeType::Branch(n) => {
                                if let Some(v) = n.value() {
                                    data = Some(v.deref().to_vec());
                                }
                            }
                            NodeType::Leaf(n) => {
                                // Return the value on the node only when the key matches exactly
                                // (e.g. the length path of subproof node is 0).
                                if p.hash.is_none() || (p.hash.is_some() && n.path().len() == 0) {
                                    data = Some(n.data().deref().to_vec());
                                }
                            }
                            _ => (),
                        }

                        return Ok(data);
                    }

                    // The trie doesn't contain the key.
                    if p.hash.is_none() {
                        return if allow_non_existent_node {
                            Ok(None)
                        } else {
                            Err(ProofError::NodeNotInTrie)
                        };
                    };

                    cur_hash = p.hash.unwrap();
                    cur_key = &chunks[key_index..];
                }
            }
        }
    }

    /// Decode the value to generate the corresponding type of node, and locate the subproof.
    ///
    /// # Arguments
    ///
    /// * `end_node` - A boolean indicates whether this is the end node to decode, thus no `key`
    ///                to be present.
    fn decode_node<S: ShaleStore<Node> + Send + Sync>(
        &self,
        merkle: &Merkle<S>,
        key: &[u8],
        buf: &[u8],
        end_node: bool,
    ) -> Result<(DiskAddress, Option<SubProof>, usize), ProofError> {
        let node = NodeType::decode(buf)?;
        let new_node = merkle
            .put_node(Node::from(node))
            .map_err(ProofError::InvalidNode)?;
        let addr = new_node.as_ptr();
        match new_node.inner() {
            NodeType::Leaf(n) => {
                let cur_key = n.path().0.as_ref();
                if !key.contains_other(cur_key) {
                    return Ok((addr, None, 0));
                }

                let subproof = SubProof {
                    encoded: n.data().to_vec(),
                    hash: None,
                };
                Ok((addr, subproof.into(), cur_key.len()))
            }
            NodeType::Extension(n) => {
                let cur_key = n.path.0.as_ref();

                if !key.contains_other(cur_key) {
                    return Ok((addr, None, 0));
                }

                let encoded = n.chd_encoded().ok_or(ProofError::InvalidData)?.to_vec();
                let subproof = generate_subproof(encoded).map(Some)?;

                Ok((addr, subproof, cur_key.len()))
            }
            // If the node is the last one to be decoded, then no subproof to be extracted.
            NodeType::Branch(_) if end_node => Ok((addr, None, 1)),
            NodeType::Branch(_) if key.is_empty() => Err(ProofError::NoSuchNode),
            NodeType::Branch(n) => {
                // Check if the subproof with the given key exist.
                let index = key[0] as usize;
                let Some(data) = &n.chd_encode()[index] else {
                    return Ok((addr, None, 1));
                };
                let subproof = generate_subproof(data.to_vec())?;
                Ok((addr, Some(subproof), 1))
            }
        }
    }
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

            let encoded = n.data().to_vec();

            let sub_proof = SubProof {
                encoded,
                hash: None,
            };

            Ok((sub_proof.into(), key_nibbles))
        }
        NodeType::Extension(n) => {
            let cur_key = &n.path.0;
            // Check if the key of current node match with the given key
            // and consume the current-key portion of the nibbles-iterator
            let does_not_match = key_nibbles.size_hint().0 < cur_key.len()
                || !cur_key.iter().all(|val| key_nibbles.next() == Some(*val));

            if does_not_match {
                return Ok((None, Nibbles::<0>::new(&[]).into_iter()));
            }
            let data = n.chd_encoded().ok_or(ProofError::InvalidData)?.to_vec();
            let sub_proof = generate_subproof(data)?;

            Ok((sub_proof.into(), key_nibbles))
        }
        NodeType::Branch(_) if key_nibbles.is_empty() => Err(ProofError::NoSuchNode),
        NodeType::Branch(n) => {
            let index = key_nibbles.next().unwrap() as usize;
            // consume items returning the item at index
            let data = n.chd_encode()[index]
                .as_ref()
                .ok_or(ProofError::InvalidData)?
                .to_vec();
            generate_subproof(data).map(|subproof| (Some(subproof), key_nibbles))
        }
    }
}

fn generate_subproof(data: Vec<u8>) -> Result<SubProof, ProofError> {
    match data.len() {
        0..=31 => {
            let sub_hash = sha3::Keccak256::digest(&data).into();
            Ok(SubProof {
                encoded: data,
                hash: Some(sub_hash),
            })
        }

        32 => {
            let sub_hash: &[u8] = &data;
            let sub_hash = sub_hash.try_into().unwrap();

            Ok(SubProof {
                encoded: data,
                hash: Some(sub_hash),
            })
        }

        len => Err(ProofError::DecodeError(Box::new(
            bincode::ErrorKind::Custom(format!("invalid proof length: {len}")),
        ))),
    }
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
fn unset_internal<K: AsRef<[u8]>, S: ShaleStore<Node> + Send + Sync>(
    merkle_setup: &mut MerkleSetup<S>,
    left: K,
    right: K,
) -> Result<bool, ProofError> {
    // Add the sentinel root
    let mut left_chunks = vec![0];
    left_chunks.extend(left.as_ref().iter().copied().flat_map(to_nibble_array));
    // Add the sentinel root
    let mut right_chunks = vec![0];
    right_chunks.extend(right.as_ref().iter().copied().flat_map(to_nibble_array));
    let root = merkle_setup.get_root();
    let merkle = merkle_setup.get_merkle_mut();
    let mut u_ref = merkle.get_node(root).map_err(|_| ProofError::NoSuchNode)?;
    let mut parent = DiskAddress::null();

    let mut fork_left: Ordering = Ordering::Equal;
    let mut fork_right: Ordering = Ordering::Equal;

    let mut index = 0;
    loop {
        match &u_ref.inner() {
            NodeType::Branch(n) => {
                // If either the node pointed by left proof or right proof is nil,
                // stop here and the forkpoint is the fullnode.
                let left_node = n.chd()[left_chunks[index] as usize];
                let right_node = n.chd()[right_chunks[index] as usize];

                match (left_node.as_ref(), right_node.as_ref()) {
                    (None, _) | (_, None) => break,
                    (left, right) if left != right => break,
                    _ => (),
                };

                parent = u_ref.as_ptr();
                u_ref = merkle.get_node(left_node.unwrap())?;
                index += 1;
            }

            NodeType::Extension(n) => {
                // If either the key of left proof or right proof doesn't match with
                // shortnode, stop here and the forkpoint is the shortnode.
                let cur_key = n.path.clone().into_inner();

                fork_left = if left_chunks.len() - index < cur_key.len() {
                    left_chunks[index..].cmp(&cur_key)
                } else {
                    left_chunks[index..index + cur_key.len()].cmp(&cur_key)
                };

                fork_right = if right_chunks.len() - index < cur_key.len() {
                    right_chunks[index..].cmp(&cur_key)
                } else {
                    right_chunks[index..index + cur_key.len()].cmp(&cur_key)
                };

                if !fork_left.is_eq() || !fork_right.is_eq() {
                    break;
                }

                parent = u_ref.as_ptr();
                u_ref = merkle.get_node(n.chd())?;
                index += cur_key.len();
            }

            NodeType::Leaf(n) => {
                let cur_key = n.path();

                fork_left = if left_chunks.len() - index < cur_key.len() {
                    left_chunks[index..].cmp(cur_key)
                } else {
                    left_chunks[index..index + cur_key.len()].cmp(cur_key)
                };

                fork_right = if right_chunks.len() - index < cur_key.len() {
                    right_chunks[index..].cmp(cur_key)
                } else {
                    right_chunks[index..index + cur_key.len()].cmp(cur_key)
                };

                break;
            }
        }
    }

    match &u_ref.inner() {
        NodeType::Branch(n) => {
            let left_node = n.chd()[left_chunks[index] as usize];
            let right_node = n.chd()[right_chunks[index] as usize];

            // unset all internal nodes calculated encoded value in the forkpoint
            for i in left_chunks[index] + 1..right_chunks[index] {
                u_ref
                    .write(|u| {
                        let uu = u.inner_mut().as_branch_mut().unwrap();
                        uu.chd_mut()[i as usize] = None;
                        uu.chd_encoded_mut()[i as usize] = None;
                    })
                    .unwrap();
            }

            let p = u_ref.as_ptr();
            drop(u_ref);
            unset_node_ref(merkle, p, left_node, &left_chunks[index..], 1, false)?;
            unset_node_ref(merkle, p, right_node, &right_chunks[index..], 1, true)?;
            Ok(false)
        }

        NodeType::Extension(n) => {
            // There can have these five scenarios:
            // - both proofs are less than the trie path => no valid range
            // - both proofs are greater than the trie path => no valid range
            // - left proof is less and right proof is greater => valid range, unset the shortnode entirely
            // - left proof points to the shortnode, but right proof is greater
            // - right proof points to the shortnode, but left proof is less
            let node = n.chd();
            let cur_key = n.path.clone().into_inner();

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
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        pp.chd_mut()[left_chunks[index - 1] as usize] = None;
                        pp.chd_encoded_mut()[left_chunks[index - 1] as usize] = None;
                    })
                    .unwrap();

                return Ok(false);
            }

            let p = u_ref.as_ptr();
            drop(u_ref);

            // Only one proof points to non-existent key.
            if fork_right.is_ne() {
                unset_node_ref(
                    merkle,
                    p,
                    Some(node),
                    &left_chunks[index..],
                    cur_key.len(),
                    false,
                )?;

                return Ok(false);
            }

            if fork_left.is_ne() {
                unset_node_ref(
                    merkle,
                    p,
                    Some(node),
                    &right_chunks[index..],
                    cur_key.len(),
                    true,
                )?;

                return Ok(false);
            }

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

            if fork_left.is_ne() && fork_right.is_ne() {
                p_ref
                    .write(|p| match p.inner_mut() {
                        NodeType::Extension(n) => {
                            *n.chd_mut() = DiskAddress::null();
                            *n.chd_encoded_mut() = None;
                        }
                        NodeType::Branch(n) => {
                            n.chd_mut()[left_chunks[index - 1] as usize] = None;
                            n.chd_encoded_mut()[left_chunks[index - 1] as usize] = None;
                        }
                        _ => {}
                    })
                    .unwrap();
            } else if fork_right.is_ne() {
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        pp.chd_mut()[left_chunks[index - 1] as usize] = None;
                        pp.chd_encoded_mut()[left_chunks[index - 1] as usize] = None;
                    })
                    .unwrap();
            } else if fork_left.is_ne() {
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        pp.chd_mut()[right_chunks[index - 1] as usize] = None;
                        pp.chd_encoded_mut()[right_chunks[index - 1] as usize] = None;
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
fn unset_node_ref<K: AsRef<[u8]>, S: ShaleStore<Node> + Send + Sync>(
    merkle: &Merkle<S>,
    parent: DiskAddress,
    node: Option<DiskAddress>,
    key: K,
    index: usize,
    remove_left: bool,
) -> Result<(), ProofError> {
    if node.is_none() {
        // If the node is nil, then it's a child of the fork point
        // fullnode(it's a non-existent branch).
        return Ok(());
    }

    let mut chunks = Vec::new();
    chunks.extend(key.as_ref());

    let mut u_ref = merkle
        .get_node(node.unwrap())
        .map_err(|_| ProofError::NoSuchNode)?;
    let p = u_ref.as_ptr();

    match &u_ref.inner() {
        NodeType::Branch(n) => {
            let child_index = chunks[index] as usize;

            let node = n.chd()[child_index];

            let iter = if remove_left {
                0..child_index
            } else {
                child_index + 1..16
            };

            for i in iter {
                u_ref
                    .write(|u| {
                        let uu = u.inner_mut().as_branch_mut().unwrap();
                        uu.chd_mut()[i] = None;
                        uu.chd_encoded_mut()[i] = None;
                    })
                    .unwrap();
            }

            drop(u_ref);

            unset_node_ref(merkle, p, node, key, index + 1, remove_left)
        }

        NodeType::Extension(n) if chunks[index..].starts_with(&n.path) => {
            let node = Some(n.chd());
            unset_node_ref(merkle, p, node, key, index + n.path.len(), remove_left)
        }

        NodeType::Extension(n) => {
            let cur_key = &n.path;
            let mut p_ref = merkle
                .get_node(parent)
                .map_err(|_| ProofError::NoSuchNode)?;

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
            let should_unset_entire_branch = matches!(
                (remove_left, cur_key.cmp(&chunks[index..])),
                (true, Ordering::Less) | (false, Ordering::Greater)
            );

            if should_unset_entire_branch {
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        pp.chd_mut()[chunks[index - 1] as usize] = None;
                        pp.chd_encoded_mut()[chunks[index - 1] as usize] = None;
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
            if !(chunks[index..]).starts_with(cur_key) {
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
                    .write(|p| match p.inner_mut() {
                        NodeType::Extension(n) => {
                            *n.chd_mut() = DiskAddress::null();
                            *n.chd_encoded_mut() = None;
                        }
                        NodeType::Branch(n) => {
                            let index = chunks[index - 1] as usize;

                            n.chd_mut()[index] = None;
                            n.chd_encoded_mut()[index] = None;
                        }
                        _ => {}
                    })
                    .expect("node write failure");
            }

            Ok(())
        }
    }
}

pub trait ContainsOtherExt {
    fn contains_other(&self, other: Self) -> bool;
}

impl<T> ContainsOtherExt for &[T]
where
    [T]: PartialEq<[T]>,
{
    fn contains_other(&self, other: Self) -> bool {
        self.len() >= other.len() && &self[..other.len()] == other
    }
}
