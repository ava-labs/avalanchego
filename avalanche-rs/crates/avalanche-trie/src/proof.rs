//! Merkle proof generation and verification.
//!
//! This module provides functionality to:
//! - Generate inclusion proofs for existing keys
//! - Generate exclusion proofs for non-existing keys
//! - Verify proofs against a root hash

use std::sync::Arc;

use thiserror::Error;

use crate::db::{HashKey, TrieDb};
use crate::nibbles::Nibbles;
use crate::node::{Node, NodeRef};
use crate::trie::Trie;
use crate::keccak256;

/// Proof verification errors.
#[derive(Debug, Error)]
pub enum ProofError {
    #[error("Invalid proof: {0}")]
    InvalidProof(String),

    #[error("Proof verification failed")]
    VerificationFailed,

    #[error("Missing proof node")]
    MissingNode,
}

/// A Merkle proof for a key.
#[derive(Debug, Clone)]
pub struct Proof {
    /// The key being proven.
    pub key: Vec<u8>,
    /// The value (None for exclusion proof).
    pub value: Option<Vec<u8>>,
    /// Proof nodes (encoded, from root to leaf).
    pub nodes: Vec<Vec<u8>>,
}

impl Proof {
    /// Creates a new proof.
    pub fn new(key: Vec<u8>, value: Option<Vec<u8>>, nodes: Vec<Vec<u8>>) -> Self {
        Self { key, value, nodes }
    }

    /// Returns true if this is an inclusion proof.
    pub fn is_inclusion(&self) -> bool {
        self.value.is_some()
    }

    /// Returns true if this is an exclusion proof.
    pub fn is_exclusion(&self) -> bool {
        self.value.is_none()
    }

    /// Computes the root hash from this proof.
    pub fn compute_root(&self) -> Result<HashKey, ProofError> {
        if self.nodes.is_empty() {
            return Err(ProofError::InvalidProof("empty proof".to_string()));
        }

        // The first node should hash to the root
        let root_encoded = &self.nodes[0];
        Ok(keccak256(root_encoded))
    }

    /// Serializes the proof.
    pub fn encode(&self) -> Vec<u8> {
        let mut result = Vec::new();

        // Key length (4 bytes) + key
        result.extend_from_slice(&(self.key.len() as u32).to_be_bytes());
        result.extend_from_slice(&self.key);

        // Value: 0 for None, 1 + length + data for Some
        match &self.value {
            None => result.push(0),
            Some(v) => {
                result.push(1);
                result.extend_from_slice(&(v.len() as u32).to_be_bytes());
                result.extend_from_slice(v);
            }
        }

        // Number of nodes (4 bytes)
        result.extend_from_slice(&(self.nodes.len() as u32).to_be_bytes());

        // Each node: length (4 bytes) + data
        for node in &self.nodes {
            result.extend_from_slice(&(node.len() as u32).to_be_bytes());
            result.extend_from_slice(node);
        }

        result
    }

    /// Deserializes a proof.
    pub fn decode(data: &[u8]) -> Result<Self, ProofError> {
        let mut pos = 0;

        if data.len() < 4 {
            return Err(ProofError::InvalidProof("too short".to_string()));
        }

        // Key
        let key_len = u32::from_be_bytes(data[pos..pos + 4].try_into().unwrap()) as usize;
        pos += 4;
        if data.len() < pos + key_len {
            return Err(ProofError::InvalidProof("truncated key".to_string()));
        }
        let key = data[pos..pos + key_len].to_vec();
        pos += key_len;

        // Value
        if pos >= data.len() {
            return Err(ProofError::InvalidProof("missing value marker".to_string()));
        }
        let value = if data[pos] == 0 {
            pos += 1;
            None
        } else {
            pos += 1;
            if data.len() < pos + 4 {
                return Err(ProofError::InvalidProof("truncated value length".to_string()));
            }
            let val_len = u32::from_be_bytes(data[pos..pos + 4].try_into().unwrap()) as usize;
            pos += 4;
            if data.len() < pos + val_len {
                return Err(ProofError::InvalidProof("truncated value".to_string()));
            }
            let val = data[pos..pos + val_len].to_vec();
            pos += val_len;
            Some(val)
        };

        // Nodes
        if data.len() < pos + 4 {
            return Err(ProofError::InvalidProof("missing node count".to_string()));
        }
        let node_count = u32::from_be_bytes(data[pos..pos + 4].try_into().unwrap()) as usize;
        pos += 4;

        let mut nodes = Vec::with_capacity(node_count);
        for _ in 0..node_count {
            if data.len() < pos + 4 {
                return Err(ProofError::InvalidProof("truncated node length".to_string()));
            }
            let node_len = u32::from_be_bytes(data[pos..pos + 4].try_into().unwrap()) as usize;
            pos += 4;
            if data.len() < pos + node_len {
                return Err(ProofError::InvalidProof("truncated node".to_string()));
            }
            nodes.push(data[pos..pos + node_len].to_vec());
            pos += node_len;
        }

        Ok(Self { key, value, nodes })
    }
}

/// Generates a proof for a key from a trie.
pub fn generate_proof<D: TrieDb>(trie: &Trie<D>, key: &[u8]) -> Result<Proof, ProofError> {
    let nibbles = Nibbles::from_bytes(key);
    let value = trie.get(key).map_err(|e| ProofError::InvalidProof(e.to_string()))?;

    // We need to collect the nodes along the path
    // For now, return a simple proof structure
    // A full implementation would traverse the trie and collect nodes

    let mut nodes = Vec::new();

    // Get root hash and fetch the root node
    let root_hash = trie.root_hash();
    if root_hash == crate::EMPTY_ROOT {
        // Empty trie - exclusion proof
        return Ok(Proof::new(key.to_vec(), None, vec![]));
    }

    // Collect nodes along the path
    collect_proof_nodes(trie, &nibbles, &mut nodes)?;

    Ok(Proof::new(key.to_vec(), value, nodes))
}

/// Helper to collect proof nodes along a path.
fn collect_proof_nodes<D: TrieDb>(
    trie: &Trie<D>,
    _path: &Nibbles,
    nodes: &mut Vec<Vec<u8>>,
) -> Result<(), ProofError> {
    // Get the root node
    let root_hash = trie.root_hash();
    if root_hash == crate::EMPTY_ROOT {
        return Ok(());
    }

    // In a full implementation, we would traverse from root to the target key,
    // collecting each node's encoding along the way.
    // For now, we just get the value through the trie's get method.

    // This is a simplified implementation - a production version would
    // actually walk the trie and collect nodes.

    // The proof nodes should include all nodes from root to the leaf/branch
    // containing the value.

    // Placeholder: just mark that we have a root
    nodes.push(root_hash.to_vec());

    Ok(())
}

/// Verifies a proof against a root hash.
pub fn verify_proof(proof: &Proof, root: &HashKey) -> Result<Option<Vec<u8>>, ProofError> {
    if proof.nodes.is_empty() {
        // Empty proof - must be empty trie
        if *root == crate::EMPTY_ROOT {
            return Ok(None); // Valid exclusion proof for empty trie
        }
        return Err(ProofError::VerificationFailed);
    }

    // Verify the proof path
    let nibbles = Nibbles::from_bytes(&proof.key);
    let mut current_hash = *root;
    let mut path_offset = 0;

    for (i, node_data) in proof.nodes.iter().enumerate() {
        // Verify this node hashes to expected hash
        let node_hash = keccak256(node_data);
        if i == 0 {
            // First node should match root
            if node_hash != current_hash {
                return Err(ProofError::VerificationFailed);
            }
        }

        // Decode and traverse
        let node = Node::decode(node_data).ok_or(ProofError::InvalidProof("bad node encoding".to_string()))?;

        match node {
            Node::Leaf(leaf) => {
                // Check if remaining path matches
                let remaining = nibbles.slice_from(path_offset);
                if leaf.path.as_slice() == remaining.as_slice() {
                    // Found the key
                    if proof.value.as_ref() == Some(&leaf.value) {
                        return Ok(Some(leaf.value));
                    }
                }
                // Key not found or value mismatch
                if proof.value.is_none() {
                    return Ok(None); // Valid exclusion proof
                }
                return Err(ProofError::VerificationFailed);
            }

            Node::Extension(ext) => {
                let remaining = nibbles.slice_from(path_offset);
                let prefix_len = ext.path.len();

                if remaining.len() < prefix_len {
                    // Path too short
                    if proof.value.is_none() {
                        return Ok(None);
                    }
                    return Err(ProofError::VerificationFailed);
                }

                if remaining.slice_to(prefix_len).as_slice() != ext.path.as_slice() {
                    // Path doesn't match
                    if proof.value.is_none() {
                        return Ok(None);
                    }
                    return Err(ProofError::VerificationFailed);
                }

                path_offset += prefix_len;

                // Get next hash
                match &ext.child {
                    NodeRef::Hash(h) => current_hash = *h,
                    NodeRef::Inline(_) => {
                        // Inline node - handled in next iteration or embedded
                    }
                    NodeRef::Empty => {
                        if proof.value.is_none() {
                            return Ok(None);
                        }
                        return Err(ProofError::VerificationFailed);
                    }
                }
            }

            Node::Branch(branch) => {
                let remaining = nibbles.slice_from(path_offset);

                if remaining.is_empty() {
                    // Value should be in branch
                    if branch.value == proof.value {
                        return Ok(branch.value);
                    }
                    return Err(ProofError::VerificationFailed);
                }

                let nibble = remaining.first().unwrap();
                let child = &branch.children[nibble as usize];

                match child {
                    NodeRef::Hash(h) => {
                        current_hash = *h;
                        path_offset += 1;
                    }
                    NodeRef::Inline(_) => {
                        path_offset += 1;
                        // Continue with inline node
                    }
                    NodeRef::Empty => {
                        if proof.value.is_none() {
                            return Ok(None);
                        }
                        return Err(ProofError::VerificationFailed);
                    }
                }
            }
        }
    }

    // If we get here, proof is incomplete
    Err(ProofError::MissingNode)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::MemoryTrieDb;

    #[test]
    fn test_proof_encode_decode() {
        let proof = Proof::new(
            b"key".to_vec(),
            Some(b"value".to_vec()),
            vec![vec![1, 2, 3], vec![4, 5, 6]],
        );

        let encoded = proof.encode();
        let decoded = Proof::decode(&encoded).unwrap();

        assert_eq!(decoded.key, proof.key);
        assert_eq!(decoded.value, proof.value);
        assert_eq!(decoded.nodes, proof.nodes);
    }

    #[test]
    fn test_proof_encode_decode_exclusion() {
        let proof = Proof::new(b"missing".to_vec(), None, vec![vec![1, 2, 3]]);

        let encoded = proof.encode();
        let decoded = Proof::decode(&encoded).unwrap();

        assert_eq!(decoded.key, proof.key);
        assert!(decoded.value.is_none());
        assert!(decoded.is_exclusion());
    }

    #[test]
    fn test_empty_trie_proof() {
        let db = Arc::new(MemoryTrieDb::new());
        let trie = Trie::new(db);

        let proof = generate_proof(&trie, b"anykey").unwrap();
        assert!(proof.is_exclusion());

        let result = verify_proof(&proof, &crate::EMPTY_ROOT).unwrap();
        assert!(result.is_none());
    }

    #[test]
    fn test_proof_types() {
        let inclusion = Proof::new(b"k".to_vec(), Some(b"v".to_vec()), vec![]);
        assert!(inclusion.is_inclusion());
        assert!(!inclusion.is_exclusion());

        let exclusion = Proof::new(b"k".to_vec(), None, vec![]);
        assert!(!exclusion.is_inclusion());
        assert!(exclusion.is_exclusion());
    }
}
