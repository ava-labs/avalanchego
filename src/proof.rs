use crate::merkle::{to_nibbles, PartialPath};

use serde::{Deserialize, Serialize};
use sha3::Digest;

use std::collections::HashMap;

/// Hash -> RLP encoding map
#[derive(Serialize, Deserialize)]
pub struct Proof(pub HashMap<[u8; 32], Vec<u8>>);

#[derive(Debug)]
pub enum ProofError {
    DecodeError,
    NoSuchNode,
    ProofNodeMissing,
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
        if data_len == 32 {
            let sub_hash: &[u8] = &data;
            let sub_hash = sub_hash.try_into().unwrap();
            Ok(Some(SubProof {
                rlp: data,
                hash: Some(sub_hash),
            }))
        } else if data_len < 32 {
            let sub_hash = sha3::Keccak256::digest(&data).into();
            Ok(Some(SubProof {
                rlp: data,
                hash: Some(sub_hash),
            }))
        } else {
            Err(ProofError::DecodeError)
        }
    }
}
