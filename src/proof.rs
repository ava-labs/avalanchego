use crate::merkle::*;
use crate::merkle_util::*;

use serde::{Deserialize, Serialize};
use sha3::Digest;
use shale::ObjPtr;

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
            if v.as_ref().len() == 0 {
                return Err(ProofError::RangeHasDeletion)
            }
        }

        // Special case, there is no edge proof at all. The given range is expected
        // to be the whole leaf-set in the trie.
        if self.0.len() == 0 {
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
        self.proof_to_path(first_key, root_hash, &mut merkle_setup)?;

        // Pass the root node here, the second path will be merged
        // with the first one. For the last edge proof, non-existent
        // proof is also allowed.
        self.proof_to_path(last_key, root_hash, &mut merkle_setup)?;

        // let merkle = merkle_setup.get_merkle_mut();
        for index in 0..keys.len() {
            merkle_setup.insert(keys[index].as_ref(), vals[index].as_ref().to_vec())
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
        &self, key: K, root_hash: [u8; 32], merkle_setup: &mut MerkleSetup,
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
        // let mut root = ObjPtr::null();
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
                _ => return Err(ProofError::DecodeError),
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
                        drop(u_ref);
                        return Ok(())
                    }

                    // The trie doesn't contain the key.
                    if p.hash.is_none() {
                        drop(u_ref);
                        return Ok(())
                    }
                    cur_hash = p.hash.unwrap();
                    cur_key = &chunks[key_index..];
                }
                // The trie doesn't contain the key.
                None => {
                    drop(u_ref);
                    return Ok(())
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
            _ => Err(ProofError::DecodeError),
        }
    }
}
