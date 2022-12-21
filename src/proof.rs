use crate::merkle::*;
use crate::merkle_util::*;

use serde::{Deserialize, Serialize};
use sha3::Digest;
use shale::ObjPtr;

use std::cmp::Ordering;
use std::collections::HashMap;

/// Hash -> RLP encoding map
#[derive(Serialize, Deserialize)]
pub struct Proof(pub HashMap<[u8; 32], Vec<u8>>);

#[derive(Debug)]
pub enum ProofError {
    DecodeError,
    NoSuchNode,
    ProofNodeMissing,
    InconsistentProofData,
    NonMonotonicIncreaseRange,
    RangeHasDeletion,
    InvalidProof,
    InvalidEdgeKeys,
    InconsistentEdgeKeys,
    NodesInsertionError,
    NodeNotInTrie,
    InvalidNode,
    EmptyRange,
}

const EXT_NODE_SIZE: usize = 2;
const BRANCH_NODE_SIZE: usize = 17;

/// SubProof contains the RLP encoding and the hash value of a node that maps
/// to a single proof step. If reaches an end step during proof verification,
/// the hash value will be none, and the RLP encoding will be the value of the
/// node.
pub struct SubProof {
    rlp: Vec<u8>,
    hash: Option<[u8; 32]>,
}

impl Proof {
    /// verify_proof checks merkle proofs. The given proof must contain the value for
    /// key in a trie with the given root hash. VerifyProof returns an error if the
    /// proof contains invalid trie nodes or the wrong value.
    pub fn verify_proof<K: AsRef<[u8]>>(&self, key: K, root_hash: [u8; 32]) -> Result<Option<Vec<u8>>, ProofError> {
        let mut chunks = Vec::new();
        chunks.extend(to_nibbles(key.as_ref()));

        let mut cur_key: &[u8] = &chunks;
        let mut cur_hash = root_hash;
        let proofs_map = &self.0;
        let mut index = 0;
        loop {
            let cur_proof = proofs_map.get(&cur_hash).ok_or(ProofError::ProofNodeMissing)?;
            let (sub_proof, size) = self.locate_subproof(cur_key, cur_proof)?;
            index += size;
            match sub_proof {
                Some(p) => {
                    // Return when reaching the end of the key.
                    if index == chunks.len() {
                        return Ok(Some(p.rlp))
                    }

                    // The trie doesn't contain the key.
                    if p.hash.is_none() {
                        return Ok(None)
                    }
                    cur_hash = p.hash.unwrap();
                    cur_key = &chunks[index..];
                }
                // The trie doesn't contain the key.
                None => return Ok(None),
            }
        }
    }

    fn locate_subproof(&self, key: &[u8], buf: &[u8]) -> Result<(Option<SubProof>, usize), ProofError> {
        let rlp = rlp::Rlp::new(buf);
        let size = rlp.item_count().unwrap();
        match size {
            EXT_NODE_SIZE => {
                let cur_key_path: Vec<_> = to_nibbles(&rlp.at(0).unwrap().as_val::<Vec<u8>>().unwrap()).collect();
                let (cur_key_path, term) = PartialPath::decode(cur_key_path);
                let cur_key = cur_key_path.into_inner();

                let rlp = rlp.at(1).unwrap();
                let data = if rlp.is_data() {
                    rlp.as_val::<Vec<u8>>().unwrap()
                } else {
                    rlp.as_raw().to_vec()
                };

                // Check if the key of current node match with the given key.
                if key.len() < cur_key.len() || key[..cur_key.len()] != cur_key {
                    return Ok((None, 0))
                }
                if term {
                    Ok((Some(SubProof { rlp: data, hash: None }), cur_key.len()))
                } else {
                    self.generate_subproof(data).map(|subproof| (subproof, cur_key.len()))
                }
            }
            BRANCH_NODE_SIZE => {
                if key.is_empty() {
                    return Err(ProofError::NoSuchNode)
                }
                let index = key[0];
                let rlp = rlp.at(index as usize).unwrap();
                let data = if rlp.is_data() {
                    rlp.as_val::<Vec<u8>>().unwrap()
                } else {
                    rlp.as_raw().to_vec()
                };
                self.generate_subproof(data).map(|subproof| (subproof, 1))
            }
            _ => Err(ProofError::DecodeError),
        }
    }

    fn generate_subproof(&self, data: Vec<u8>) -> Result<Option<SubProof>, ProofError> {
        let data_len = data.len();
        match data_len {
            32 => {
                let sub_hash: &[u8] = &data;
                let sub_hash = sub_hash.try_into().unwrap();
                Ok(Some(SubProof {
                    rlp: data,
                    hash: Some(sub_hash),
                }))
            }
            0..=31 => {
                let sub_hash = sha3::Keccak256::digest(&data).into();
                Ok(Some(SubProof {
                    rlp: data,
                    hash: Some(sub_hash),
                }))
            }
            _ => Err(ProofError::DecodeError),
        }
    }

    pub fn concat_proofs(&mut self, other: Proof) {
        self.0.extend(other.0)
    }

    pub fn verify_range_proof<K: AsRef<[u8]>, V: AsRef<[u8]>>(
        &self, root_hash: [u8; 32], first_key: K, last_key: K, keys: Vec<K>, vals: Vec<V>,
    ) -> Result<bool, ProofError> {
        if keys.len() != vals.len() {
            return Err(ProofError::InconsistentProofData)
        }

        // Ensure the received batch is monotonic increasing and contains no deletions
        for n in 0..keys.len() - 1 {
            if compare(keys[n].as_ref(), keys[n + 1].as_ref()).is_ge() {
                return Err(ProofError::NonMonotonicIncreaseRange)
            }
        }

        for v in vals.iter() {
            if v.as_ref().is_empty() {
                return Err(ProofError::RangeHasDeletion)
            }
        }

        // Special case, there is no edge proof at all. The given range is expected
        // to be the whole leaf-set in the trie.
        if self.0.is_empty() {
            // Use in-memory merkle
            let mut merkle = new_merkle(0x10000, 0x10000);
            for (index, k) in keys.iter().enumerate() {
                merkle.insert(k, vals[index].as_ref().to_vec())
            }
            let merkle_root = &*merkle.root_hash();
            if merkle_root != &root_hash {
                return Err(ProofError::InvalidProof)
            }
            return Ok(false)
        }
        // TODO(Hao): handle special case when there is a provided edge proof but zero key/value pairs.
        // TODO(Hao): handle special case when there is only one element and two edge keys are same.

        // Ok, in all other cases, we require two edge paths available.
        // First check the validity of edge keys.
        if compare(first_key.as_ref(), last_key.as_ref()).is_ge() {
            return Err(ProofError::InvalidEdgeKeys)
        }

        // TODO(Hao): different length edge keys should be supported
        if first_key.as_ref().len() != last_key.as_ref().len() {
            return Err(ProofError::InconsistentEdgeKeys)
        }

        // Convert the edge proofs to edge trie paths. Then we can
        // have the same tree architecture with the original one.
        // For the first edge proof, non-existent proof is allowed.
        let mut merkle_setup = new_merkle(0x100000, 0x100000);
        self.proof_to_path(first_key.as_ref(), root_hash, &mut merkle_setup, true)?;

        // Pass the root node here, the second path will be merged
        // with the first one. For the last edge proof, non-existent
        // proof is also allowed.
        self.proof_to_path(last_key.as_ref(), root_hash, &mut merkle_setup, true)?;

        // Remove all internal calcuated RLP values. All the removed parts should
        // be re-filled(or re-constructed) by the given leaves range.
        let empty = unset_internal(&mut merkle_setup, first_key.as_ref(), last_key.as_ref())?;
        if empty {
            merkle_setup = new_merkle(0x100000, 0x100000);
        }

        for (i, _) in keys.iter().enumerate() {
            merkle_setup.insert(keys[i].as_ref(), vals[i].as_ref().to_vec())
        }

        // Calculate the hash
        let merkle_root = &*merkle_setup.root_hash();
        if merkle_root != &root_hash {
            return Err(ProofError::InvalidProof)
        }

        return Ok(true)
    }

    /// proofToPath converts a merkle proof to trie node path. The main purpose of
    /// this function is recovering a node path from the merkle proof stream. All
    /// necessary nodes will be resolved and leave the remaining as hashnode.
    ///
    /// The given edge proof is allowed to be an existent or non-existent proof.
    fn proof_to_path<K: AsRef<[u8]>>(
        &self, key: K, root_hash: [u8; 32], merkle_setup: &mut MerkleSetup, allow_non_existent: bool,
    ) -> Result<(), ProofError> {
        let root = merkle_setup.get_root();
        let merkle = merkle_setup.get_merkle_mut();
        let mut u_ref = merkle.get_node(root).map_err(|_| ProofError::NoSuchNode)?;

        let mut chunks = Vec::new();
        chunks.extend(to_nibbles(key.as_ref()));

        let mut cur_key: &[u8] = &chunks;
        let mut cur_hash = root_hash;
        let proofs_map = &self.0;
        let mut key_index = 0;
        let mut branch_index: u8 = 0;
        let mut iter = 0;
        loop {
            let cur_proof = proofs_map.get(&cur_hash).ok_or(ProofError::ProofNodeMissing)?;
            // TODO(Hao): (Optimization) If a node is alreay decode we don't need to decode again.
            let (mut chd_ptr, sub_proof, size) = self.decode_node(merkle, cur_key, cur_proof)?;
            // Link the child to the parent based on the node type.
            match &u_ref.inner() {
                NodeType::Branch(n) => {
                    match n.chd()[branch_index as usize] {
                        // If the child already resolved, then use the existing node.
                        Some(node) => {
                            chd_ptr = Some(node);
                        }
                        None => {
                            // insert the leaf to the empty slot
                            u_ref
                                .write(|u| {
                                    let uu = u.inner_mut().as_branch_mut().unwrap();
                                    uu.chd_mut()[branch_index as usize] = chd_ptr;
                                })
                                .unwrap();
                        }
                    };
                }
                NodeType::Extension(_) => {
                    // If the child already resolved, then use the existing node.
                    let node = u_ref.inner().as_extension().unwrap().chd();
                    if node.is_null() {
                        u_ref
                            .write(|u| {
                                let uu = u.inner_mut().as_extension_mut().unwrap();
                                *uu.chd_mut() = if chd_ptr.is_none() {
                                    ObjPtr::null()
                                } else {
                                    chd_ptr.unwrap()
                                };
                            })
                            .unwrap();
                    } else {
                        chd_ptr = Some(node);
                    }
                }
                // We should not hit a leaf node as a parent.
                _ => return Err(ProofError::InvalidNode),
            };

            if chd_ptr.is_some() {
                u_ref = merkle.get_node(chd_ptr.unwrap()).map_err(|_| ProofError::DecodeError)?;
                // If the new parent is a branch node, record the index to correctly link the next child to it.
                if u_ref.inner().as_branch().is_some() {
                    branch_index = chunks[key_index];
                }
            } else {
                // Root node must be included in the proof.
                if iter == 0 {
                    return Err(ProofError::ProofNodeMissing)
                }
            }
            iter += 1;

            key_index += size;
            match sub_proof {
                Some(p) => {
                    // Return when reaching the end of the key.
                    if key_index == chunks.len() {
                        // Release the handle to the node.
                        drop(u_ref);
                        return Ok(())
                    }

                    // The trie doesn't contain the key.
                    if p.hash.is_none() {
                        drop(u_ref);
                        if allow_non_existent {
                            return Ok(())
                        }
                        return Err(ProofError::NodeNotInTrie)
                    }
                    cur_hash = p.hash.unwrap();
                    cur_key = &chunks[key_index..];
                }
                // The trie doesn't contain the key. It's possible
                // the proof is a non-existing proof, but at least
                // we can prove all resolved nodes are correct, it's
                // enough for us to prove range.
                None => {
                    drop(u_ref);
                    if allow_non_existent {
                        return Ok(())
                    }
                    return Err(ProofError::NodeNotInTrie)
                }
            }
        }
    }

    /// Decode the RLP value to generate the corresponding type of node, and locate the subproof.
    fn decode_node(
        &self, merkle: &Merkle, key: &[u8], buf: &[u8],
    ) -> Result<(Option<ObjPtr<Node>>, Option<SubProof>, usize), ProofError> {
        let rlp = rlp::Rlp::new(buf);
        let size = rlp.item_count().unwrap();
        match size {
            EXT_NODE_SIZE => {
                let cur_key_path: Vec<_> = to_nibbles(&rlp.at(0).unwrap().as_val::<Vec<u8>>().unwrap()).collect();
                let (cur_key_path, term) = PartialPath::decode(cur_key_path);
                let cur_key = cur_key_path.into_inner();

                let rlp = rlp.at(1).unwrap();
                let data = if rlp.is_data() {
                    rlp.as_val::<Vec<u8>>().unwrap()
                } else {
                    rlp.as_raw().to_vec()
                };

                // Check if the key of current node match with the given key.
                if key.len() < cur_key.len() || key[..cur_key.len()] != cur_key {
                    return Ok((None, None, 0))
                }
                if term {
                    let leaf = Node::new(NodeType::Leaf(LeafNode::new(cur_key.clone(), data.clone())));
                    let leaf_ptr = merkle.new_node(leaf).map_err(|_| ProofError::DecodeError)?;
                    Ok((
                        Some(leaf_ptr.as_ptr()),
                        Some(SubProof { rlp: data, hash: None }),
                        cur_key.len(),
                    ))
                } else {
                    let ext_ptr = merkle
                        .new_node(Node::new(NodeType::Extension(ExtNode::new(
                            cur_key.clone(),
                            ObjPtr::null(),
                            Some(data.clone()),
                        ))))
                        .map_err(|_| ProofError::DecodeError)?;
                    let subproof = self.generate_subproof(data).map(|subproof| (subproof, cur_key.len()))?;
                    Ok((Some(ext_ptr.as_ptr()), subproof.0, subproof.1))
                }
            }
            BRANCH_NODE_SIZE => {
                if key.is_empty() {
                    return Err(ProofError::NoSuchNode)
                }

                // Record rlp values of all children.
                let mut chd_eth_rlp: [Option<Vec<u8>>; NBRANCH] = Default::default();
                for i in 0..NBRANCH {
                    let rlp = rlp.at(i).unwrap();
                    // Skip if rlp is empty data
                    if !rlp.is_empty() {
                        let data = if rlp.is_data() {
                            rlp.as_val::<Vec<u8>>().unwrap()
                        } else {
                            rlp.as_raw().to_vec()
                        };
                        chd_eth_rlp[i] = Some(data);
                    }
                }

                // Subproof with the given key must exist.
                let index = key[0] as usize;
                let data: Vec<u8> = if chd_eth_rlp[index].is_none() {
                    return Err(ProofError::DecodeError)
                } else {
                    chd_eth_rlp[index].clone().unwrap()
                };
                let subproof = self.generate_subproof(data).map(|subproof| subproof)?;

                let chd = [None; NBRANCH];
                let t = NodeType::Branch(BranchNode::new(chd, None, chd_eth_rlp));
                let branch_ptr = merkle
                    .new_node(Node::new(t))
                    .map_err(|_| ProofError::ProofNodeMissing)?;
                Ok((Some(branch_ptr.as_ptr()), subproof, 1))
            }
            // RLP length can only be the two cases above.
            _ => Err(ProofError::DecodeError),
        }
    }
}

// unsetInternal removes all internal node references.
// It should be called after a trie is constructed with two edge paths. Also
// the given boundary keys must be the one used to construct the edge paths.
//
// It's the key step for range proof. All visited nodes should be marked dirty
// since the node content might be modified. Besides it can happen that some
// fullnodes only have one child which is disallowed. But if the proof is valid,
// the missing children will be filled, otherwise it will be thrown anyway.
//
// Note we have the assumption here the given boundary keys are different
// and right is larger than left.
fn unset_internal<K: AsRef<[u8]>>(merkle_setup: &mut MerkleSetup, left: K, right: K) -> Result<bool, ProofError> {
    // Add the sentinel root
    let mut left_chunks = vec![0];
    left_chunks.extend(to_nibbles(left.as_ref()));
    // Add the sentinel root
    let mut right_chunks = vec![0];
    right_chunks.extend(to_nibbles(right.as_ref()));
    let mut index = 0;
    let root = merkle_setup.get_root();
    let merkle = merkle_setup.get_merkle_mut();
    let mut u_ref = merkle.get_node(root).map_err(|_| ProofError::NoSuchNode)?;
    let mut parent = ObjPtr::null();

    let mut fork_left: Ordering = Ordering::Equal;
    let mut fork_right: Ordering = Ordering::Equal;

    loop {
        match &u_ref.inner() {
            NodeType::Branch(n) => {
                // If either the node pointed by left proof or right proof is nil,
                // stop here and the forkpoint is the fullnode.
                let left_node = n.chd()[left_chunks[index] as usize];
                let right_node = n.chd()[right_chunks[index] as usize];
                if left_node.is_none() || right_node.is_none() || left_node.unwrap() != right_node.unwrap() {
                    break
                }
                parent = u_ref.as_ptr();
                u_ref = merkle
                    .get_node(left_node.unwrap())
                    .map_err(|_| ProofError::DecodeError)?;
                index += 1;
            }
            NodeType::Extension(n) => {
                // If either the key of left proof or right proof doesn't match with
                // shortnode, stop here and the forkpoint is the shortnode.
                let cur_key = n.path().clone().into_inner();
                if left_chunks.len() - index < cur_key.len() {
                    fork_left = compare(&left_chunks[index..], &cur_key)
                } else {
                    fork_left = compare(&left_chunks[index..index + cur_key.len()], &cur_key)
                }

                if right_chunks.len() - index < cur_key.len() {
                    fork_right = compare(&right_chunks[index..], &cur_key)
                } else {
                    fork_right = compare(&right_chunks[index..index + cur_key.len()], &cur_key)
                }

                if !fork_left.is_eq() || !fork_right.is_eq() {
                    break
                }
                parent = u_ref.as_ptr();
                u_ref = merkle.get_node(n.chd()).map_err(|_| ProofError::DecodeError)?;
                index += cur_key.len();
            }
            _ => return Err(ProofError::InvalidNode),
        }
    }

    match &u_ref.inner() {
        NodeType::Branch(n) => {
            let left_node = n.chd()[left_chunks[index] as usize];
            let right_node = n.chd()[right_chunks[index] as usize];

            // unset all internal nodes calculated RLP value in the forkpoint
            for i in left_chunks[index] + 1..right_chunks[index] {
                u_ref
                    .write(|u| {
                        let uu = u.inner_mut().as_branch_mut().unwrap();
                        uu.chd_eth_rlp_mut()[i as usize] = None;
                    })
                    .unwrap();
            }
            let p = u_ref.as_ptr();
            drop(u_ref);
            unset(merkle, p, left_node, left_chunks[index..].to_vec(), 1, false)?;
            unset(merkle, p, right_node, right_chunks[index..].to_vec(), 1, true)?;
            return Ok(false)
        }
        NodeType::Extension(n) => {
            // There can have these five scenarios:
            // - both proofs are less than the trie path => no valid range
            // - both proofs are greater than the trie path => no valid range
            // - left proof is less and right proof is greater => valid range, unset the shortnode entirely
            // - left proof points to the shortnode, but right proof is greater
            // - right proof points to the shortnode, but left proof is less
            let node = n.chd();
            let cur_key = n.path().clone().into_inner();
            if fork_left.is_lt() && fork_right.is_lt() {
                drop(u_ref);
                return Err(ProofError::EmptyRange)
            }
            if fork_left.is_gt() && fork_right.is_gt() {
                drop(u_ref);
                return Err(ProofError::EmptyRange)
            }
            if !fork_left.is_eq() && !fork_right.is_eq() {
                // The fork point is root node, unset the entire trie
                if parent.is_null() {
                    drop(u_ref);
                    return Ok(true)
                }
                let mut p_ref = merkle.get_node(parent).map_err(|_| ProofError::NoSuchNode)?;
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().unwrap();
                        pp.chd_eth_rlp_mut()[left_chunks[index - 1] as usize] = None;
                    })
                    .unwrap();
                drop(p_ref);
                drop(u_ref);
                return Ok(false)
            }
            let p = u_ref.as_ptr();
            drop(u_ref);
            // Only one proof points to non-existent key.
            if !fork_right.is_eq() {
                unset(
                    merkle,
                    p,
                    Some(node),
                    left_chunks[index..].to_vec(),
                    cur_key.len(),
                    false,
                )?;
                return Ok(false)
            }
            if !fork_left.is_eq() {
                unset(
                    merkle,
                    p,
                    Some(node),
                    right_chunks[index..].to_vec(),
                    cur_key.len(),
                    true,
                )?;
                return Ok(false)
            }
            return Ok(false)
        }
        _ => return Err(ProofError::InvalidNode),
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
fn unset<K: AsRef<[u8]>>(
    merkle: &Merkle, parent: ObjPtr<Node>, node: Option<ObjPtr<Node>>, key: K, index: usize, remove_left: bool,
) -> Result<(), ProofError> {
    if node.is_none() {
        // If the node is nil, then it's a child of the fork point
        // fullnode(it's a non-existent branch).
        return Ok(())
    }
    // Add the sentinel root
    let mut chunks = vec![0];
    chunks.extend(to_nibbles(key.as_ref()));

    let mut u_ref = merkle.get_node(node.unwrap()).map_err(|_| ProofError::NoSuchNode)?;
    let p = u_ref.as_ptr();

    match &u_ref.inner() {
        NodeType::Branch(n) => {
            let node = n.chd()[chunks[index] as usize];
            if remove_left {
                for i in 0..chunks[index] {
                    u_ref
                        .write(|u| {
                            let uu = u.inner_mut().as_branch_mut().unwrap();
                            uu.chd_eth_rlp_mut()[i as usize] = None;
                        })
                        .unwrap();
                }
            } else {
                for i in chunks[index] + 1..16 {
                    u_ref
                        .write(|u| {
                            let uu = u.inner_mut().as_branch_mut().unwrap();
                            uu.chd_eth_rlp_mut()[i as usize] = None;
                        })
                        .unwrap();
                }
            }

            drop(u_ref);
            return unset(merkle, p, node, key, index + 1, remove_left)
        }
        NodeType::Extension(n) => {
            let cur_key = n.path().clone().into_inner();
            let node = n.chd();
            if chunks[index..].len() < cur_key.len() ||
                !compare(&cur_key, &chunks[index..index + cur_key.len()]).is_eq()
            {
                let mut p_ref = merkle.get_node(parent).map_err(|_| ProofError::NoSuchNode)?;
                // Find the fork point, it's an non-existent branch.
                if remove_left {
                    if compare(&cur_key, &chunks[index..]).is_lt() {
                        // The key of fork shortnode is less than the path
                        // (it belongs to the range), unset the entire
                        // branch. The parent must be a fullnode.
                        p_ref
                            .write(|p| {
                                let pp = p.inner_mut().as_branch_mut().unwrap();
                                pp.chd_eth_rlp_mut()[chunks[index - 1] as usize] = None;
                            })
                            .unwrap();
                    }
                    //else {
                    // The key of fork shortnode is greater than the
                    // path(it doesn't belong to the range), keep
                    // it with the cached hash available.
                    //}
                } else {
                    if compare(&cur_key, &chunks[index..]).is_gt() {
                        // The key of fork shortnode is greater than the
                        // path(it belongs to the range), unset the entrie
                        // branch. The parent must be a fullnode.
                        p_ref
                            .write(|p| {
                                let pp = p.inner_mut().as_branch_mut().unwrap();
                                pp.chd_eth_rlp_mut()[chunks[index - 1] as usize] = None;
                            })
                            .unwrap();
                    }
                    //else {
                    // The key of fork shortnode is less than the
                    // path(it doesn't belong to the range), keep
                    // it with the cached hash available.
                    //}
                }
                drop(u_ref);
                drop(p_ref);
                return Ok(())
            }

            drop(u_ref);
            return unset(merkle, p, Some(node), key, index + cur_key.len(), remove_left)
        }
        NodeType::Leaf(_) => (),
    }

    return Ok(())
}
