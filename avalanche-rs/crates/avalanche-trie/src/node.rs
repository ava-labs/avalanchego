//! Trie node types.
//!
//! The Merkle Patricia Trie uses three node types:
//! - Branch: 16 children (one per nibble) + optional value
//! - Extension: compressed path segment pointing to another node
//! - Leaf: compressed path segment with value

use crate::nibbles::Nibbles;
use crate::keccak256;

/// A reference to a node - either inline data or a hash.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NodeRef {
    /// Inline node data (for nodes < 32 bytes when encoded).
    Inline(Vec<u8>),
    /// Hash reference to a node stored in the database.
    Hash([u8; 32]),
    /// Empty/null reference.
    Empty,
}

impl NodeRef {
    /// Returns the hash if this is a hash reference.
    pub fn as_hash(&self) -> Option<&[u8; 32]> {
        match self {
            NodeRef::Hash(h) => Some(h),
            _ => None,
        }
    }

    /// Returns true if this is empty.
    pub fn is_empty(&self) -> bool {
        matches!(self, NodeRef::Empty)
    }

    /// Encodes the node reference for RLP.
    pub fn encode(&self) -> Vec<u8> {
        match self {
            NodeRef::Empty => vec![0x80], // Empty string in RLP
            NodeRef::Inline(data) => data.clone(),
            NodeRef::Hash(hash) => encode_bytes(hash),
        }
    }
}

/// A branch node with 16 children and an optional value.
#[derive(Debug, Clone)]
pub struct BranchNode {
    /// Children (one per nibble 0-15).
    pub children: [NodeRef; 16],
    /// Optional value stored at this node.
    pub value: Option<Vec<u8>>,
}

impl BranchNode {
    /// Creates an empty branch node.
    pub fn new() -> Self {
        Self {
            children: std::array::from_fn(|_| NodeRef::Empty),
            value: None,
        }
    }

    /// Creates a branch node with a value.
    pub fn with_value(value: Vec<u8>) -> Self {
        Self {
            children: std::array::from_fn(|_| NodeRef::Empty),
            value: Some(value),
        }
    }

    /// Returns the child at the given nibble.
    pub fn child(&self, nibble: u8) -> &NodeRef {
        &self.children[nibble as usize]
    }

    /// Sets the child at the given nibble.
    pub fn set_child(&mut self, nibble: u8, child: NodeRef) {
        self.children[nibble as usize] = child;
    }

    /// Counts non-empty children.
    pub fn child_count(&self) -> usize {
        self.children.iter().filter(|c| !c.is_empty()).count()
    }

    /// Returns the index of the only non-empty child, if there's exactly one.
    pub fn single_child_index(&self) -> Option<u8> {
        let mut found = None;
        for (i, child) in self.children.iter().enumerate() {
            if !child.is_empty() {
                if found.is_some() {
                    return None; // More than one child
                }
                found = Some(i as u8);
            }
        }
        found
    }

    /// Encodes the branch node using RLP-like encoding.
    pub fn encode(&self) -> Vec<u8> {
        // Branch node: list of 17 items (16 children + value)
        let mut items: Vec<Vec<u8>> = Vec::with_capacity(17);

        for child in &self.children {
            items.push(child.encode());
        }

        // Value (empty string if none)
        match &self.value {
            Some(v) => items.push(encode_bytes(v)),
            None => items.push(vec![0x80]),
        }

        encode_list(&items)
    }

    /// Decodes a branch node from RLP-like encoding.
    pub fn decode(data: &[u8]) -> Option<Self> {
        let items = decode_list(data)?;
        if items.len() != 17 {
            return None;
        }

        let mut children: [NodeRef; 16] = std::array::from_fn(|_| NodeRef::Empty);
        for (i, item) in items[..16].iter().enumerate() {
            children[i] = decode_node_ref(item)?;
        }

        let value = decode_value(&items[16]);

        Some(Self { children, value })
    }
}

impl Default for BranchNode {
    fn default() -> Self {
        Self::new()
    }
}

/// An extension node with a compressed path.
#[derive(Debug, Clone)]
pub struct ExtensionNode {
    /// The path segment (in hex-prefix encoding).
    pub path: Nibbles,
    /// Reference to the child node.
    pub child: NodeRef,
}

impl ExtensionNode {
    /// Creates a new extension node.
    pub fn new(path: Nibbles, child: NodeRef) -> Self {
        Self { path, child }
    }

    /// Encodes the extension node.
    pub fn encode(&self) -> Vec<u8> {
        let path_encoded = self.path.encode_hex_prefix(false);
        let items = vec![encode_bytes(&path_encoded), self.child.encode()];
        encode_list(&items)
    }

    /// Decodes an extension node.
    pub fn decode(data: &[u8]) -> Option<Self> {
        let items = decode_list(data)?;
        if items.len() != 2 {
            return None;
        }

        let path_bytes = decode_bytes(&items[0])?;
        let (path, is_leaf) = Nibbles::decode_hex_prefix(&path_bytes)?;
        if is_leaf {
            return None; // This is a leaf, not extension
        }

        let child = decode_node_ref(&items[1])?;

        Some(Self { path, child })
    }
}

/// A leaf node with a path and value.
#[derive(Debug, Clone)]
pub struct LeafNode {
    /// The remaining path segment (in hex-prefix encoding).
    pub path: Nibbles,
    /// The value stored at this leaf.
    pub value: Vec<u8>,
}

impl LeafNode {
    /// Creates a new leaf node.
    pub fn new(path: Nibbles, value: Vec<u8>) -> Self {
        Self { path, value }
    }

    /// Encodes the leaf node.
    pub fn encode(&self) -> Vec<u8> {
        let path_encoded = self.path.encode_hex_prefix(true);
        let items = vec![encode_bytes(&path_encoded), encode_bytes(&self.value)];
        encode_list(&items)
    }

    /// Decodes a leaf node.
    pub fn decode(data: &[u8]) -> Option<Self> {
        let items = decode_list(data)?;
        if items.len() != 2 {
            return None;
        }

        let path_bytes = decode_bytes(&items[0])?;
        let (path, is_leaf) = Nibbles::decode_hex_prefix(&path_bytes)?;
        if !is_leaf {
            return None; // This is an extension, not leaf
        }

        let value = decode_bytes(&items[1])?;

        Some(Self { path, value })
    }
}

/// A trie node (branch, extension, or leaf).
#[derive(Debug, Clone)]
pub enum Node {
    Branch(BranchNode),
    Extension(ExtensionNode),
    Leaf(LeafNode),
}

impl Node {
    /// Encodes the node.
    pub fn encode(&self) -> Vec<u8> {
        match self {
            Node::Branch(n) => n.encode(),
            Node::Extension(n) => n.encode(),
            Node::Leaf(n) => n.encode(),
        }
    }

    /// Decodes a node from encoded data.
    pub fn decode(data: &[u8]) -> Option<Self> {
        let items = decode_list(data)?;

        match items.len() {
            17 => {
                // Branch node
                Some(Node::Branch(BranchNode::decode(data)?))
            }
            2 => {
                // Extension or Leaf - check hex-prefix
                let path_bytes = decode_bytes(&items[0])?;
                if path_bytes.is_empty() {
                    return None;
                }

                let prefix = path_bytes[0] >> 4;
                let is_leaf = prefix & 2 != 0;

                if is_leaf {
                    Some(Node::Leaf(LeafNode::decode(data)?))
                } else {
                    Some(Node::Extension(ExtensionNode::decode(data)?))
                }
            }
            _ => None,
        }
    }

    /// Computes the hash of this node.
    pub fn hash(&self) -> [u8; 32] {
        keccak256(&self.encode())
    }

    /// Returns a reference to this node (hash if >= 32 bytes, inline otherwise).
    pub fn to_ref(&self) -> NodeRef {
        let encoded = self.encode();
        if encoded.len() >= 32 {
            NodeRef::Hash(keccak256(&encoded))
        } else {
            NodeRef::Inline(encoded)
        }
    }
}

// RLP-like encoding helpers

/// Encodes a byte slice.
fn encode_bytes(data: &[u8]) -> Vec<u8> {
    if data.len() == 1 && data[0] < 0x80 {
        // Single byte < 0x80 is encoded as itself
        vec![data[0]]
    } else if data.len() < 56 {
        // Short string: 0x80 + len, then data
        let mut result = vec![0x80 + data.len() as u8];
        result.extend_from_slice(data);
        result
    } else {
        // Long string: 0xb7 + len_of_len, then len, then data
        let len = data.len();
        let len_bytes = encode_length(len);
        let mut result = vec![0xb7 + len_bytes.len() as u8];
        result.extend_from_slice(&len_bytes);
        result.extend_from_slice(data);
        result
    }
}

/// Encodes a list of already-encoded items.
fn encode_list(items: &[Vec<u8>]) -> Vec<u8> {
    let total_len: usize = items.iter().map(|i| i.len()).sum();

    let mut result = if total_len < 56 {
        vec![0xc0 + total_len as u8]
    } else {
        let len_bytes = encode_length(total_len);
        let mut r = vec![0xf7 + len_bytes.len() as u8];
        r.extend_from_slice(&len_bytes);
        r
    };

    for item in items {
        result.extend_from_slice(item);
    }

    result
}

/// Encodes a length as big-endian bytes (no leading zeros).
fn encode_length(len: usize) -> Vec<u8> {
    if len == 0 {
        return vec![];
    }

    let mut bytes = Vec::new();
    let mut n = len;
    while n > 0 {
        bytes.push((n & 0xff) as u8);
        n >>= 8;
    }
    bytes.reverse();
    bytes
}

/// Decodes a byte slice from RLP.
fn decode_bytes(data: &[u8]) -> Option<Vec<u8>> {
    if data.is_empty() {
        return None;
    }

    let prefix = data[0];

    if prefix < 0x80 {
        // Single byte
        Some(vec![prefix])
    } else if prefix <= 0xb7 {
        // Short string
        let len = (prefix - 0x80) as usize;
        if data.len() < 1 + len {
            return None;
        }
        Some(data[1..1 + len].to_vec())
    } else if prefix <= 0xbf {
        // Long string
        let len_of_len = (prefix - 0xb7) as usize;
        if data.len() < 1 + len_of_len {
            return None;
        }
        let len = decode_length(&data[1..1 + len_of_len])?;
        if data.len() < 1 + len_of_len + len {
            return None;
        }
        Some(data[1 + len_of_len..1 + len_of_len + len].to_vec())
    } else {
        // List prefix - not a string
        None
    }
}

/// Decodes a list of items from RLP.
fn decode_list(data: &[u8]) -> Option<Vec<Vec<u8>>> {
    if data.is_empty() {
        return None;
    }

    let prefix = data[0];
    let (content_start, content_len) = if prefix <= 0xf7 {
        if prefix < 0xc0 {
            return None; // Not a list
        }
        let len = (prefix - 0xc0) as usize;
        (1, len)
    } else {
        let len_of_len = (prefix - 0xf7) as usize;
        if data.len() < 1 + len_of_len {
            return None;
        }
        let len = decode_length(&data[1..1 + len_of_len])?;
        (1 + len_of_len, len)
    };

    if data.len() < content_start + content_len {
        return None;
    }

    let content = &data[content_start..content_start + content_len];
    let mut items = Vec::new();
    let mut pos = 0;

    while pos < content.len() {
        let item_len = item_length(&content[pos..])?;
        items.push(content[pos..pos + item_len].to_vec());
        pos += item_len;
    }

    Some(items)
}

/// Returns the total length of an RLP item.
fn item_length(data: &[u8]) -> Option<usize> {
    if data.is_empty() {
        return None;
    }

    let prefix = data[0];

    if prefix < 0x80 {
        Some(1)
    } else if prefix <= 0xb7 {
        Some(1 + (prefix - 0x80) as usize)
    } else if prefix <= 0xbf {
        let len_of_len = (prefix - 0xb7) as usize;
        let len = decode_length(&data[1..1 + len_of_len])?;
        Some(1 + len_of_len + len)
    } else if prefix <= 0xf7 {
        Some(1 + (prefix - 0xc0) as usize)
    } else {
        let len_of_len = (prefix - 0xf7) as usize;
        let len = decode_length(&data[1..1 + len_of_len])?;
        Some(1 + len_of_len + len)
    }
}

/// Decodes a big-endian length.
fn decode_length(bytes: &[u8]) -> Option<usize> {
    let mut len = 0usize;
    for byte in bytes {
        len = len.checked_mul(256)?;
        len = len.checked_add(*byte as usize)?;
    }
    Some(len)
}

/// Decodes a node reference from RLP.
fn decode_node_ref(data: &[u8]) -> Option<NodeRef> {
    if data.is_empty() {
        return Some(NodeRef::Empty);
    }

    if data == [0x80] {
        return Some(NodeRef::Empty);
    }

    let prefix = data[0];

    // Check if it's a byte string (potential hash)
    if prefix <= 0xbf {
        // It's a string - check if it's a 32-byte hash
        if let Some(bytes) = decode_bytes(data) {
            if bytes.len() == 32 {
                let mut hash = [0u8; 32];
                hash.copy_from_slice(&bytes);
                return Some(NodeRef::Hash(hash));
            }
        }
    }

    // Otherwise it's an inline node (could be a list or short string node)
    Some(NodeRef::Inline(data.to_vec()))
}

/// Decodes a value (returns None for empty string).
fn decode_value(data: &[u8]) -> Option<Vec<u8>> {
    if data == [0x80] {
        return None;
    }
    decode_bytes(data)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_branch_node_encode_decode() {
        let mut branch = BranchNode::new();
        branch.value = Some(b"test".to_vec());

        let encoded = branch.encode();
        let decoded = BranchNode::decode(&encoded).unwrap();

        assert_eq!(decoded.value, Some(b"test".to_vec()));
        assert_eq!(decoded.child_count(), 0);
    }

    #[test]
    fn test_leaf_node_encode_decode() {
        let leaf = LeafNode::new(Nibbles::from_nibbles(&[1, 2, 3, 4]), b"hello".to_vec());

        let encoded = leaf.encode();
        let decoded = LeafNode::decode(&encoded).unwrap();

        assert_eq!(decoded.path.as_slice(), &[1, 2, 3, 4]);
        assert_eq!(decoded.value, b"hello".to_vec());
    }

    #[test]
    fn test_extension_node_encode_decode() {
        let ext = ExtensionNode::new(
            Nibbles::from_nibbles(&[1, 2, 3, 4]),
            NodeRef::Hash([0xaa; 32]),
        );

        let encoded = ext.encode();
        let decoded = ExtensionNode::decode(&encoded).unwrap();

        assert_eq!(decoded.path.as_slice(), &[1, 2, 3, 4]);
        assert!(matches!(decoded.child, NodeRef::Hash(_)));
    }

    #[test]
    fn test_node_decode_dispatch() {
        // Test leaf
        let leaf = LeafNode::new(Nibbles::from_nibbles(&[1, 2]), b"v".to_vec());
        let encoded = leaf.encode();
        let decoded = Node::decode(&encoded).unwrap();
        assert!(matches!(decoded, Node::Leaf(_)));

        // Test extension
        let ext = ExtensionNode::new(Nibbles::from_nibbles(&[1, 2]), NodeRef::Hash([0; 32]));
        let encoded = ext.encode();
        let decoded = Node::decode(&encoded).unwrap();
        assert!(matches!(decoded, Node::Extension(_)));

        // Test branch
        let branch = BranchNode::with_value(b"val".to_vec());
        let encoded = branch.encode();
        let decoded = Node::decode(&encoded).unwrap();
        assert!(matches!(decoded, Node::Branch(_)));
    }

    #[test]
    fn test_encode_bytes() {
        assert_eq!(encode_bytes(&[0x42]), vec![0x42]);
        assert_eq!(encode_bytes(&[0x80]), vec![0x81, 0x80]);
        assert_eq!(encode_bytes(&[1, 2, 3]), vec![0x83, 1, 2, 3]);
        assert_eq!(encode_bytes(&[]), vec![0x80]);
    }

    #[test]
    fn test_node_ref_inline_vs_hash() {
        // Small node should be inline
        let small_leaf = LeafNode::new(Nibbles::from_nibbles(&[1]), b"v".to_vec());
        let node = Node::Leaf(small_leaf);
        let node_ref = node.to_ref();
        assert!(matches!(node_ref, NodeRef::Inline(_)));

        // Large node should be hash
        let large_leaf = LeafNode::new(Nibbles::from_bytes(&[0; 32]), vec![0; 100]);
        let node = Node::Leaf(large_leaf);
        let node_ref = node.to_ref();
        assert!(matches!(node_ref, NodeRef::Hash(_)));
    }
}
