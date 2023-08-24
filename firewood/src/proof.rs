// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{
    db::DbError,
    merkle::{
        to_nibble_array, BranchNode, ExtNode, LeafNode, Merkle, MerkleError, Node, NodeType,
        PartialPath, NBRANCH,
    },
    merkle_util::{new_merkle, DataStoreError, MerkleSetup},
};
use nix::errno::Errno;
use sha3::Digest;
use shale::disk_address::DiskAddress;
use shale::ShaleError;
use shale::ShaleStore;

use std::cmp::Ordering;
use std::error::Error;
use std::fmt;
use std::ops::Deref;

use crate::v2::api::Proof;

#[derive(Debug)]
pub enum ProofError {
    DecodeError,
    NoSuchNode,
    ProofNodeMissing,
    InconsistentProofData,
    NonMonotonicIncreaseRange,
    RangeHasDeletion,
    InvalidData,
    InvalidProof,
    InvalidEdgeKeys,
    InconsistentEdgeKeys,
    NodesInsertionError,
    NodeNotInTrie,
    InvalidNode(MerkleError),
    EmptyRange,
    ForkLeft,
    ForkRight,
    SystemError(Errno),
    Shale(ShaleError),
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

impl From<rlp::DecoderError> for ProofError {
    fn from(_: rlp::DecoderError) -> ProofError {
        ProofError::DecodeError
    }
}

impl fmt::Display for ProofError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            ProofError::DecodeError => write!(f, "decoding"),
            ProofError::NoSuchNode => write!(f, "no such node"),
            ProofError::ProofNodeMissing => write!(f, "proof node missing"),
            ProofError::InconsistentProofData => write!(f, "inconsistent proof data"),
            ProofError::NonMonotonicIncreaseRange => write!(f, "nonmonotonic range increase"),
            ProofError::RangeHasDeletion => write!(f, "range has deletion"),
            ProofError::InvalidData => write!(f, "invalid data"),
            ProofError::InvalidProof => write!(f, "invalid proof"),
            ProofError::InvalidEdgeKeys => write!(f, "invalid edge keys"),
            ProofError::InconsistentEdgeKeys => write!(f, "inconsistent edge keys"),
            ProofError::NodesInsertionError => write!(f, "node insertion error"),
            ProofError::NodeNotInTrie => write!(f, "node not in trie"),
            ProofError::InvalidNode(e) => write!(f, "invalid node: {e:?}"),
            ProofError::EmptyRange => write!(f, "empty range"),
            ProofError::ForkLeft => write!(f, "fork left"),
            ProofError::ForkRight => write!(f, "fork right"),
            ProofError::SystemError(e) => write!(f, "system error: {e:?}"),
            ProofError::InvalidRootHash => write!(f, "invalid root hash provided"),
            ProofError::Shale(e) => write!(f, "shale error: {e:?}"),
        }
    }
}

impl Error for ProofError {}

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

impl<N: AsRef<[u8]> + Send> Proof<N> {
    /// verify_proof checks merkle proofs. The given proof must contain the value for
    /// key in a trie with the given root hash. VerifyProof returns an error if the
    /// proof contains invalid trie nodes or the wrong value.
    ///
    /// The generic N represents the storage for the node data
    pub fn verify_proof<K: AsRef<[u8]>>(
        &self,
        key: K,
        root_hash: [u8; 32],
    ) -> Result<Option<Vec<u8>>, ProofError> {
        let mut key_nibbles = Vec::new();
        key_nibbles.extend(key.as_ref().iter().copied().flat_map(to_nibble_array));

        let mut remaining_key_nibbles: &[u8] = &key_nibbles;
        let mut cur_hash = root_hash;
        let proofs_map = &self.0;
        let mut index = 0;

        loop {
            let cur_proof = proofs_map
                .get(&cur_hash)
                .ok_or(ProofError::ProofNodeMissing)?;
            let (sub_proof, size) =
                self.locate_subproof(remaining_key_nibbles, cur_proof.as_ref())?;
            index += size;

            match sub_proof {
                // Return when reaching the end of the key.
                Some(p) if index == key_nibbles.len() => return Ok(Some(p.rlp)),
                // The trie doesn't contain the key.
                Some(SubProof { hash: None, .. }) | None => return Ok(None),
                Some(p) => {
                    cur_hash = p.hash.unwrap();
                    remaining_key_nibbles = &key_nibbles[index..];
                }
            }
        }
    }

    fn locate_subproof(
        &self,
        key_nibbles: &[u8],
        rlp_encoded_node: &[u8],
    ) -> Result<(Option<SubProof>, usize), ProofError> {
        let rlp = rlp::Rlp::new(rlp_encoded_node);

        match rlp.item_count() {
            Ok(EXT_NODE_SIZE) => {
                let decoded_key_nibbles: Vec<_> = rlp
                    .at(0)
                    .unwrap()
                    .as_val::<Vec<u8>>()
                    .unwrap()
                    .into_iter()
                    .flat_map(to_nibble_array)
                    .collect();
                let (cur_key_path, term) = PartialPath::decode(decoded_key_nibbles);
                let cur_key = cur_key_path.into_inner();

                let rlp = rlp.at(1).unwrap();

                let data = if rlp.is_data() {
                    rlp.as_val::<Vec<u8>>().unwrap()
                } else {
                    rlp.as_raw().to_vec()
                };

                // Check if the key of current node match with the given key.
                if key_nibbles.len() < cur_key.len() || key_nibbles[..cur_key.len()] != cur_key {
                    return Ok((None, 0));
                }

                if term {
                    Ok((
                        Some(SubProof {
                            rlp: data,
                            hash: None,
                        }),
                        cur_key.len(),
                    ))
                } else {
                    self.generate_subproof(data)
                        .map(|subproof| (Some(subproof), cur_key.len()))
                }
            }

            Ok(BRANCH_NODE_SIZE) if key_nibbles.is_empty() => Err(ProofError::NoSuchNode),

            Ok(BRANCH_NODE_SIZE) => {
                let index = key_nibbles[0];
                let rlp = rlp.at(index as usize).unwrap();

                let data = if rlp.is_data() {
                    rlp.as_val::<Vec<u8>>().unwrap()
                } else {
                    rlp.as_raw().to_vec()
                };

                self.generate_subproof(data)
                    .map(|subproof| (Some(subproof), 1))
            }

            _ => Err(ProofError::DecodeError),
        }
    }

    fn generate_subproof(&self, data: Vec<u8>) -> Result<SubProof, ProofError> {
        match data.len() {
            0..=31 => {
                let sub_hash = sha3::Keccak256::digest(&data).into();
                Ok(SubProof {
                    rlp: data,
                    hash: Some(sub_hash),
                })
            }
            32 => {
                let sub_hash: &[u8] = &data;
                let sub_hash = sub_hash.try_into().unwrap();

                Ok(SubProof {
                    rlp: data,
                    hash: Some(sub_hash),
                })
            }
            _ => Err(ProofError::DecodeError),
        }
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

        // Remove all internal calcuated RLP values. All the removed parts should
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
            // TODO(Hao): (Optimization) If a node is alreay decode we don't need to decode again.
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

            u_ref = merkle
                .get_node(chd_ptr)
                .map_err(|_| ProofError::DecodeError)?;

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

                        let c_ref = merkle
                            .get_node(chd_ptr)
                            .map_err(|_| ProofError::DecodeError)?;

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

    /// Decode the RLP value to generate the corresponding type of node, and locate the subproof.
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
        let rlp = rlp::Rlp::new(buf);
        let size = rlp.item_count()?;

        match size {
            EXT_NODE_SIZE => {
                let cur_key_path: Vec<_> = rlp
                    .at(0)?
                    .as_val::<Vec<u8>>()?
                    .into_iter()
                    .flat_map(to_nibble_array)
                    .collect();

                let (cur_key_path, term) = PartialPath::decode(cur_key_path);
                let cur_key = cur_key_path.into_inner();

                let rlp = rlp.at(1)?;

                let data = if rlp.is_data() {
                    rlp.as_val::<Vec<u8>>()?
                } else {
                    rlp.as_raw().to_vec()
                };

                // Check if the key of current node match with the given key.
                if key.len() < cur_key.len() || key[..cur_key.len()] != cur_key {
                    let ext_ptr = get_ext_ptr(merkle, term, Data(data), CurKey(cur_key))?;

                    return Ok((ext_ptr, None, 0));
                }

                let subproof = if term {
                    Some(SubProof {
                        rlp: data.clone(),
                        hash: None,
                    })
                } else {
                    self.generate_subproof(data.clone()).map(Some)?
                };

                let cur_key_len = cur_key.len();

                let ext_ptr = get_ext_ptr(merkle, term, Data(data), CurKey(cur_key))?;

                Ok((ext_ptr, subproof, cur_key_len))
            }

            BRANCH_NODE_SIZE => {
                let data_rlp = rlp.at(NBRANCH)?;

                // Extract the value of the branch node.
                // Skip if rlp is empty data
                let value = if !data_rlp.is_empty() {
                    let data = if data_rlp.is_data() {
                        data_rlp.as_val::<Vec<u8>>().unwrap()
                    } else {
                        data_rlp.as_raw().to_vec()
                    };

                    Some(data)
                } else {
                    None
                };

                // Record rlp values of all children.
                let mut chd_eth_rlp: [Option<Vec<u8>>; NBRANCH] = Default::default();

                for (i, chd) in rlp.into_iter().take(NBRANCH).enumerate() {
                    if !chd.is_empty() {
                        // Skip if chd is empty data
                        let data = if chd.is_data() {
                            chd.as_val()?
                        } else {
                            chd.as_raw().to_vec()
                        };

                        chd_eth_rlp[i] = Some(data);
                    }
                }

                // If the node is the last one to be decoded, then no subproof to be extracted.
                if end_node {
                    let branch_ptr = build_branch_ptr(merkle, value, chd_eth_rlp)?;

                    return Ok((branch_ptr, None, 1));
                }

                if key.is_empty() {
                    return Err(ProofError::NoSuchNode);
                }

                // Check if the subproof with the given key exist.
                let index = key[0] as usize;

                let Some(data) = chd_eth_rlp[index].clone() else {
                    let branch_ptr = build_branch_ptr(merkle, value, chd_eth_rlp)?;

                    return Ok((branch_ptr, None, 1));
                };

                let branch_ptr = build_branch_ptr(merkle, value, chd_eth_rlp)?;
                let subproof = self.generate_subproof(data)?;

                Ok((branch_ptr, Some(subproof), 1))
            }

            // RLP length can only be the two cases above.
            _ => Err(ProofError::DecodeError),
        }
    }
}

struct CurKey(Vec<u8>);
struct Data(Vec<u8>);

fn get_ext_ptr<S: ShaleStore<Node> + Send + Sync>(
    merkle: &Merkle<S>,
    term: bool,
    Data(data): Data,
    CurKey(cur_key): CurKey,
) -> Result<DiskAddress, ProofError> {
    let node = if term {
        NodeType::Leaf(LeafNode::new(cur_key, data))
    } else {
        NodeType::Extension(ExtNode::new(cur_key, DiskAddress::null(), Some(data)))
    };

    merkle
        .new_node(Node::new(node))
        .map(|node| node.as_ptr())
        .map_err(|_| ProofError::DecodeError)
}

fn build_branch_ptr<S: ShaleStore<Node> + Send + Sync>(
    merkle: &Merkle<S>,
    value: Option<Vec<u8>>,
    chd_eth_rlp: [Option<Vec<u8>>; NBRANCH],
) -> Result<DiskAddress, ProofError> {
    let node = BranchNode::new([None; NBRANCH], value, chd_eth_rlp);
    let node = NodeType::Branch(node);
    let node = Node::new(node);

    merkle
        .new_node(node)
        .map_err(|_| ProofError::ProofNodeMissing)
        .map(|node| node.as_ptr())
}

// unset_internal removes all internal node references.
// It should be called after a trie is constructed with two edge paths. Also
// the given boundary keys must be the one used to construct the edge paths.
//
// It's the key step for range proof. The precalucated RLP value of all internal
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
                u_ref = merkle
                    .get_node(left_node.unwrap())
                    .map_err(|_| ProofError::DecodeError)?;
                index += 1;
            }

            NodeType::Extension(n) => {
                // If either the key of left proof or right proof doesn't match with
                // shortnode, stop here and the forkpoint is the shortnode.
                let cur_key = n.path().clone().into_inner();

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
                u_ref = merkle
                    .get_node(n.chd())
                    .map_err(|_| ProofError::DecodeError)?;
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

            // unset all internal nodes calculated RLP value in the forkpoint
            for i in left_chunks[index] + 1..right_chunks[index] {
                u_ref
                    .write(|u| {
                        let uu = u.inner_mut().as_branch_mut().unwrap();
                        uu.chd_mut()[i as usize] = None;
                        uu.chd_eth_rlp_mut()[i as usize] = None;
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
            let cur_key = n.path().clone().into_inner();

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
                        pp.chd_eth_rlp_mut()[left_chunks[index - 1] as usize] = None;
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
                            *n.chd_eth_rlp_mut() = None;
                        }
                        NodeType::Branch(n) => {
                            n.chd_mut()[left_chunks[index - 1] as usize] = None;
                            n.chd_eth_rlp_mut()[left_chunks[index - 1] as usize] = None;
                        }
                        _ => {}
                    })
                    .unwrap();
            } else if fork_right.is_ne() {
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        pp.chd_mut()[left_chunks[index - 1] as usize] = None;
                        pp.chd_eth_rlp_mut()[left_chunks[index - 1] as usize] = None;
                    })
                    .unwrap();
            } else if fork_left.is_ne() {
                p_ref
                    .write(|p| {
                        let pp = p.inner_mut().as_branch_mut().expect("not a branch node");
                        pp.chd_mut()[right_chunks[index - 1] as usize] = None;
                        pp.chd_eth_rlp_mut()[right_chunks[index - 1] as usize] = None;
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
                        uu.chd_eth_rlp_mut()[i] = None;
                    })
                    .unwrap();
            }

            drop(u_ref);

            unset_node_ref(merkle, p, node, key, index + 1, remove_left)
        }

        NodeType::Extension(n) if chunks[index..].starts_with(n.path()) => {
            let node = Some(n.chd());
            unset_node_ref(merkle, p, node, key, index + n.path().len(), remove_left)
        }

        NodeType::Extension(n) => {
            let cur_key = n.path();
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
                        pp.chd_eth_rlp_mut()[chunks[index - 1] as usize] = None;
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
                                pp.chd_eth_rlp_mut()[index] = None;
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
                            *n.chd_eth_rlp_mut() = None;
                        }
                        NodeType::Branch(n) => {
                            let index = chunks[index - 1] as usize;

                            n.chd_mut()[index] = None;
                            n.chd_eth_rlp_mut()[index] = None;
                        }
                        _ => {}
                    })
                    .expect("node write failure");
            }

            Ok(())
        }
    }
}
