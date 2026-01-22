//! Merkle Patricia Trie implementation.
//!
//! This is the core trie implementation providing:
//! - O(log n) insert, get, delete operations
//! - Root hash computation for state commitments
//! - Database-backed persistence

use std::sync::Arc;

use parking_lot::RwLock;
use thiserror::Error;

use crate::db::{HashKey, TrieDb};
use crate::nibbles::Nibbles;
use crate::node::{BranchNode, ExtensionNode, LeafNode, Node, NodeRef};
use crate::{keccak256, EMPTY_ROOT};

/// Trie errors.
#[derive(Debug, Error)]
pub enum TrieError {
    #[error("Node not found: {0}")]
    NodeNotFound(String),

    #[error("Invalid node encoding")]
    InvalidNode,

    #[error("Key not found")]
    KeyNotFound,

    #[error("Database error: {0}")]
    DatabaseError(String),
}

/// A Merkle Patricia Trie.
pub struct Trie<D: TrieDb> {
    db: Arc<D>,
    root: RwLock<NodeRef>,
}

impl<D: TrieDb> Trie<D> {
    /// Creates a new empty trie.
    pub fn new(db: Arc<D>) -> Self {
        Self {
            db,
            root: RwLock::new(NodeRef::Empty),
        }
    }

    /// Creates a trie with an existing root.
    pub fn from_root(db: Arc<D>, root_hash: HashKey) -> Self {
        Self {
            db,
            root: RwLock::new(NodeRef::Hash(root_hash)),
        }
    }

    /// Returns the root hash.
    pub fn root_hash(&self) -> HashKey {
        match &*self.root.read() {
            NodeRef::Empty => EMPTY_ROOT,
            NodeRef::Hash(h) => *h,
            NodeRef::Inline(data) => keccak256(data),
        }
    }

    /// Returns true if the trie is empty.
    pub fn is_empty(&self) -> bool {
        self.root.read().is_empty()
    }

    /// Gets a value by key.
    pub fn get(&self, key: &[u8]) -> Result<Option<Vec<u8>>, TrieError> {
        let nibbles = Nibbles::from_bytes(key);
        self.get_at(&self.root.read(), &nibbles)
    }

    /// Inserts a key-value pair.
    pub fn insert(&self, key: &[u8], value: Vec<u8>) -> Result<(), TrieError> {
        if value.is_empty() {
            return self.delete(key);
        }

        let nibbles = Nibbles::from_bytes(key);
        let root = self.root.read().clone();
        let new_root = self.insert_at(root, nibbles, value)?;
        *self.root.write() = new_root;
        Ok(())
    }

    /// Deletes a key.
    pub fn delete(&self, key: &[u8]) -> Result<(), TrieError> {
        let nibbles = Nibbles::from_bytes(key);
        let root = self.root.read().clone();
        let new_root = self.delete_at(root, nibbles)?;
        *self.root.write() = new_root;
        Ok(())
    }

    /// Commits all nodes to the database.
    pub fn commit(&self) {
        self.db.commit();
    }

    // Internal helper to get a value at a node
    fn get_at(&self, node_ref: &NodeRef, path: &Nibbles) -> Result<Option<Vec<u8>>, TrieError> {
        match node_ref {
            NodeRef::Empty => Ok(None),

            NodeRef::Inline(data) => {
                let node = Node::decode(data).ok_or(TrieError::InvalidNode)?;
                self.get_at_node(&node, path)
            }

            NodeRef::Hash(hash) => {
                let data = self
                    .db
                    .get(hash)
                    .ok_or_else(|| TrieError::NodeNotFound(hex::encode(hash)))?;
                let node = Node::decode(&data).ok_or(TrieError::InvalidNode)?;
                self.get_at_node(&node, path)
            }
        }
    }

    fn get_at_node(&self, node: &Node, path: &Nibbles) -> Result<Option<Vec<u8>>, TrieError> {
        match node {
            Node::Leaf(leaf) => {
                if leaf.path.as_slice() == path.as_slice() {
                    Ok(Some(leaf.value.clone()))
                } else {
                    Ok(None)
                }
            }

            Node::Extension(ext) => {
                let prefix_len = ext.path.len();
                if path.len() >= prefix_len
                    && path.slice_to(prefix_len).as_slice() == ext.path.as_slice()
                {
                    self.get_at(&ext.child, &path.slice_from(prefix_len))
                } else {
                    Ok(None)
                }
            }

            Node::Branch(branch) => {
                if path.is_empty() {
                    Ok(branch.value.clone())
                } else {
                    let nibble = path.first().unwrap();
                    self.get_at(&branch.children[nibble as usize], &path.slice_from(1))
                }
            }
        }
    }

    // Internal helper to insert at a node
    fn insert_at(
        &self,
        node_ref: NodeRef,
        path: Nibbles,
        value: Vec<u8>,
    ) -> Result<NodeRef, TrieError> {
        match node_ref {
            NodeRef::Empty => {
                // Create a new leaf
                let leaf = LeafNode::new(path, value);
                Ok(self.store_node(Node::Leaf(leaf)))
            }

            NodeRef::Inline(data) => {
                let node = Node::decode(&data).ok_or(TrieError::InvalidNode)?;
                self.insert_at_node(node, path, value)
            }

            NodeRef::Hash(hash) => {
                let data = self
                    .db
                    .get(&hash)
                    .ok_or_else(|| TrieError::NodeNotFound(hex::encode(hash)))?;
                let node = Node::decode(&data).ok_or(TrieError::InvalidNode)?;
                self.insert_at_node(node, path, value)
            }
        }
    }

    fn insert_at_node(
        &self,
        node: Node,
        path: Nibbles,
        value: Vec<u8>,
    ) -> Result<NodeRef, TrieError> {
        match node {
            Node::Leaf(leaf) => self.insert_at_leaf(leaf, path, value),
            Node::Extension(ext) => self.insert_at_extension(ext, path, value),
            Node::Branch(branch) => self.insert_at_branch(branch, path, value),
        }
    }

    fn insert_at_leaf(
        &self,
        leaf: LeafNode,
        path: Nibbles,
        value: Vec<u8>,
    ) -> Result<NodeRef, TrieError> {
        let common_len = leaf.path.common_prefix_len(&path);

        if common_len == leaf.path.len() && common_len == path.len() {
            // Same key - replace value
            let new_leaf = LeafNode::new(path, value);
            return Ok(self.store_node(Node::Leaf(new_leaf)));
        }

        // Need to create a branch
        let mut branch = BranchNode::new();

        if common_len == leaf.path.len() {
            // Existing leaf becomes value in branch
            branch.value = Some(leaf.value);
        } else {
            // Existing leaf goes in child
            let old_nibble = leaf.path.get(common_len).unwrap();
            let old_remaining = leaf.path.slice_from(common_len + 1);
            let old_leaf = LeafNode::new(old_remaining, leaf.value);
            branch.children[old_nibble as usize] = self.store_node(Node::Leaf(old_leaf));
        }

        if common_len == path.len() {
            // New value goes in branch
            branch.value = Some(value);
        } else {
            // New value goes in child
            let new_nibble = path.get(common_len).unwrap();
            let new_remaining = path.slice_from(common_len + 1);
            let new_leaf = LeafNode::new(new_remaining, value);
            branch.children[new_nibble as usize] = self.store_node(Node::Leaf(new_leaf));
        }

        let branch_ref = self.store_node(Node::Branch(branch));

        if common_len > 0 {
            // Need extension to reach branch
            let ext = ExtensionNode::new(path.slice_to(common_len), branch_ref);
            Ok(self.store_node(Node::Extension(ext)))
        } else {
            Ok(branch_ref)
        }
    }

    fn insert_at_extension(
        &self,
        ext: ExtensionNode,
        path: Nibbles,
        value: Vec<u8>,
    ) -> Result<NodeRef, TrieError> {
        let common_len = ext.path.common_prefix_len(&path);

        if common_len == ext.path.len() {
            // Path matches extension - continue into child
            let remaining = path.slice_from(common_len);
            let new_child = self.insert_at(ext.child, remaining, value)?;

            let new_ext = ExtensionNode::new(ext.path, new_child);
            return Ok(self.store_node(Node::Extension(new_ext)));
        }

        // Need to split the extension
        let mut branch = BranchNode::new();

        // Handle the existing extension's remaining path
        let ext_remaining = ext.path.slice_from(common_len);
        let ext_nibble = ext_remaining.first().unwrap();
        let ext_after = ext_remaining.slice_from(1);

        if ext_after.is_empty() {
            // Extension child goes directly into branch
            branch.children[ext_nibble as usize] = ext.child;
        } else {
            // Need new extension for remaining path
            let new_ext = ExtensionNode::new(ext_after, ext.child);
            branch.children[ext_nibble as usize] = self.store_node(Node::Extension(new_ext));
        }

        // Handle the new path
        let path_remaining = path.slice_from(common_len);
        if path_remaining.is_empty() {
            // Value goes in branch
            branch.value = Some(value);
        } else {
            let path_nibble = path_remaining.first().unwrap();
            let path_after = path_remaining.slice_from(1);
            let new_leaf = LeafNode::new(path_after, value);
            branch.children[path_nibble as usize] = self.store_node(Node::Leaf(new_leaf));
        }

        let branch_ref = self.store_node(Node::Branch(branch));

        if common_len > 0 {
            // Need extension to reach branch
            let new_ext = ExtensionNode::new(path.slice_to(common_len), branch_ref);
            Ok(self.store_node(Node::Extension(new_ext)))
        } else {
            Ok(branch_ref)
        }
    }

    fn insert_at_branch(
        &self,
        mut branch: BranchNode,
        path: Nibbles,
        value: Vec<u8>,
    ) -> Result<NodeRef, TrieError> {
        if path.is_empty() {
            // Value goes in branch
            branch.value = Some(value);
        } else {
            // Insert into appropriate child
            let nibble = path.first().unwrap();
            let remaining = path.slice_from(1);
            let child = std::mem::replace(&mut branch.children[nibble as usize], NodeRef::Empty);
            let new_child = self.insert_at(child, remaining, value)?;
            branch.children[nibble as usize] = new_child;
        }

        Ok(self.store_node(Node::Branch(branch)))
    }

    // Internal helper to delete at a node
    fn delete_at(&self, node_ref: NodeRef, path: Nibbles) -> Result<NodeRef, TrieError> {
        match node_ref {
            NodeRef::Empty => Ok(NodeRef::Empty),

            NodeRef::Inline(data) => {
                let node = Node::decode(&data).ok_or(TrieError::InvalidNode)?;
                self.delete_at_node(node, path)
            }

            NodeRef::Hash(hash) => {
                let data = self
                    .db
                    .get(&hash)
                    .ok_or_else(|| TrieError::NodeNotFound(hex::encode(hash)))?;
                let node = Node::decode(&data).ok_or(TrieError::InvalidNode)?;
                self.delete_at_node(node, path)
            }
        }
    }

    fn delete_at_node(&self, node: Node, path: Nibbles) -> Result<NodeRef, TrieError> {
        match node {
            Node::Leaf(leaf) => {
                if leaf.path.as_slice() == path.as_slice() {
                    Ok(NodeRef::Empty)
                } else {
                    // Key not found - return unchanged
                    Ok(self.store_node(Node::Leaf(leaf)))
                }
            }

            Node::Extension(ext) => {
                let prefix_len = ext.path.len();
                if path.len() >= prefix_len
                    && path.slice_to(prefix_len).as_slice() == ext.path.as_slice()
                {
                    let new_child = self.delete_at(ext.child, path.slice_from(prefix_len))?;
                    self.simplify_extension(ext.path, new_child)
                } else {
                    // Key not found
                    Ok(self.store_node(Node::Extension(ext)))
                }
            }

            Node::Branch(mut branch) => {
                if path.is_empty() {
                    branch.value = None;
                } else {
                    let nibble = path.first().unwrap();
                    let child =
                        std::mem::replace(&mut branch.children[nibble as usize], NodeRef::Empty);
                    let new_child = self.delete_at(child, path.slice_from(1))?;
                    branch.children[nibble as usize] = new_child;
                }

                self.simplify_branch(branch)
            }
        }
    }

    // Simplify a branch node after deletion
    fn simplify_branch(&self, branch: BranchNode) -> Result<NodeRef, TrieError> {
        let child_count = branch.child_count();
        let has_value = branch.value.is_some();

        if child_count == 0 && !has_value {
            // Empty branch
            return Ok(NodeRef::Empty);
        }

        if child_count == 0 && has_value {
            // Branch with only value becomes leaf
            let leaf = LeafNode::new(Nibbles::new(), branch.value.unwrap());
            return Ok(self.store_node(Node::Leaf(leaf)));
        }

        if child_count == 1 && !has_value {
            // Single child - can be collapsed
            let nibble = branch.single_child_index().unwrap();
            let child = &branch.children[nibble as usize];

            // Fetch the child to see if we can merge
            let child_node = self.resolve_node(child)?;

            match child_node {
                Node::Leaf(leaf) => {
                    // Merge nibble into leaf path
                    let mut new_path = Nibbles::from_nibbles(&[nibble]);
                    new_path.extend(&leaf.path);
                    let new_leaf = LeafNode::new(new_path, leaf.value);
                    return Ok(self.store_node(Node::Leaf(new_leaf)));
                }
                Node::Extension(ext) => {
                    // Merge nibble into extension path
                    let mut new_path = Nibbles::from_nibbles(&[nibble]);
                    new_path.extend(&ext.path);
                    let new_ext = ExtensionNode::new(new_path, ext.child);
                    return Ok(self.store_node(Node::Extension(new_ext)));
                }
                Node::Branch(_) => {
                    // Create extension to branch
                    let ext = ExtensionNode::new(Nibbles::from_nibbles(&[nibble]), child.clone());
                    return Ok(self.store_node(Node::Extension(ext)));
                }
            }
        }

        // Keep branch as is
        Ok(self.store_node(Node::Branch(branch)))
    }

    // Simplify an extension node after child modification
    fn simplify_extension(&self, path: Nibbles, child: NodeRef) -> Result<NodeRef, TrieError> {
        if child.is_empty() {
            return Ok(NodeRef::Empty);
        }

        let child_node = self.resolve_node(&child)?;

        match child_node {
            Node::Leaf(leaf) => {
                // Merge extension path with leaf
                let new_path = path.concat(&leaf.path);
                let new_leaf = LeafNode::new(new_path, leaf.value);
                Ok(self.store_node(Node::Leaf(new_leaf)))
            }
            Node::Extension(ext) => {
                // Merge extensions
                let new_path = path.concat(&ext.path);
                let new_ext = ExtensionNode::new(new_path, ext.child);
                Ok(self.store_node(Node::Extension(new_ext)))
            }
            Node::Branch(_) => {
                // Keep extension
                let ext = ExtensionNode::new(path, child);
                Ok(self.store_node(Node::Extension(ext)))
            }
        }
    }

    // Resolve a node reference to a Node
    fn resolve_node(&self, node_ref: &NodeRef) -> Result<Node, TrieError> {
        match node_ref {
            NodeRef::Empty => Err(TrieError::NodeNotFound("empty".to_string())),
            NodeRef::Inline(data) => Node::decode(data).ok_or(TrieError::InvalidNode),
            NodeRef::Hash(hash) => {
                let data = self
                    .db
                    .get(hash)
                    .ok_or_else(|| TrieError::NodeNotFound(hex::encode(hash)))?;
                Node::decode(&data).ok_or(TrieError::InvalidNode)
            }
        }
    }

    // Store a node and return its reference
    fn store_node(&self, node: Node) -> NodeRef {
        let encoded = node.encode();
        if encoded.len() < 32 {
            NodeRef::Inline(encoded)
        } else {
            let hash = keccak256(&encoded);
            self.db.put(hash, encoded);
            NodeRef::Hash(hash)
        }
    }
}

impl<D: TrieDb> Clone for Trie<D> {
    fn clone(&self) -> Self {
        Self {
            db: self.db.clone(),
            root: RwLock::new(self.root.read().clone()),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::MemoryTrieDb;

    fn new_trie() -> Trie<MemoryTrieDb> {
        Trie::new(Arc::new(MemoryTrieDb::new()))
    }

    #[test]
    fn test_empty_trie() {
        let trie = new_trie();
        assert!(trie.is_empty());
        assert_eq!(trie.root_hash(), EMPTY_ROOT);
    }

    #[test]
    fn test_single_insert() {
        let trie = new_trie();
        trie.insert(b"hello", b"world".to_vec()).unwrap();

        assert!(!trie.is_empty());
        assert_eq!(trie.get(b"hello").unwrap(), Some(b"world".to_vec()));
        assert_eq!(trie.get(b"other").unwrap(), None);
    }

    #[test]
    fn test_multiple_inserts() {
        let trie = new_trie();

        trie.insert(b"key1", b"value1".to_vec()).unwrap();
        trie.insert(b"key2", b"value2".to_vec()).unwrap();
        trie.insert(b"key3", b"value3".to_vec()).unwrap();

        assert_eq!(trie.get(b"key1").unwrap(), Some(b"value1".to_vec()));
        assert_eq!(trie.get(b"key2").unwrap(), Some(b"value2".to_vec()));
        assert_eq!(trie.get(b"key3").unwrap(), Some(b"value3".to_vec()));
    }

    #[test]
    fn test_update() {
        let trie = new_trie();

        trie.insert(b"key", b"value1".to_vec()).unwrap();
        assert_eq!(trie.get(b"key").unwrap(), Some(b"value1".to_vec()));

        trie.insert(b"key", b"value2".to_vec()).unwrap();
        assert_eq!(trie.get(b"key").unwrap(), Some(b"value2".to_vec()));
    }

    #[test]
    fn test_delete() {
        let trie = new_trie();

        trie.insert(b"key1", b"value1".to_vec()).unwrap();
        trie.insert(b"key2", b"value2".to_vec()).unwrap();

        trie.delete(b"key1").unwrap();
        assert_eq!(trie.get(b"key1").unwrap(), None);
        assert_eq!(trie.get(b"key2").unwrap(), Some(b"value2".to_vec()));

        trie.delete(b"key2").unwrap();
        assert_eq!(trie.get(b"key2").unwrap(), None);
        assert!(trie.is_empty());
    }

    #[test]
    fn test_delete_empty_value() {
        let trie = new_trie();

        trie.insert(b"key", b"value".to_vec()).unwrap();
        trie.insert(b"key", vec![]).unwrap(); // Should delete

        assert_eq!(trie.get(b"key").unwrap(), None);
    }

    #[test]
    fn test_root_changes() {
        let trie = new_trie();
        let empty_root = trie.root_hash();

        trie.insert(b"key", b"value".to_vec()).unwrap();
        let root1 = trie.root_hash();
        assert_ne!(root1, empty_root);

        trie.insert(b"key2", b"value2".to_vec()).unwrap();
        let root2 = trie.root_hash();
        assert_ne!(root2, root1);

        trie.delete(b"key2").unwrap();
        let root3 = trie.root_hash();
        assert_eq!(root3, root1); // Should return to previous root
    }

    #[test]
    fn test_common_prefix_keys() {
        let trie = new_trie();

        trie.insert(b"abc", b"1".to_vec()).unwrap();
        trie.insert(b"abd", b"2".to_vec()).unwrap();
        trie.insert(b"ab", b"3".to_vec()).unwrap();

        assert_eq!(trie.get(b"abc").unwrap(), Some(b"1".to_vec()));
        assert_eq!(trie.get(b"abd").unwrap(), Some(b"2".to_vec()));
        assert_eq!(trie.get(b"ab").unwrap(), Some(b"3".to_vec()));
    }

    #[test]
    fn test_long_keys() {
        let trie = new_trie();

        let key1 = vec![0u8; 100];
        let key2 = vec![1u8; 100];
        let key3 = vec![0u8; 50];

        trie.insert(&key1, b"v1".to_vec()).unwrap();
        trie.insert(&key2, b"v2".to_vec()).unwrap();
        trie.insert(&key3, b"v3".to_vec()).unwrap();

        assert_eq!(trie.get(&key1).unwrap(), Some(b"v1".to_vec()));
        assert_eq!(trie.get(&key2).unwrap(), Some(b"v2".to_vec()));
        assert_eq!(trie.get(&key3).unwrap(), Some(b"v3".to_vec()));
    }

    #[test]
    fn test_deterministic_root() {
        // Same keys inserted in different order should give same root
        let trie1 = new_trie();
        trie1.insert(b"a", b"1".to_vec()).unwrap();
        trie1.insert(b"b", b"2".to_vec()).unwrap();
        trie1.insert(b"c", b"3".to_vec()).unwrap();

        let trie2 = new_trie();
        trie2.insert(b"c", b"3".to_vec()).unwrap();
        trie2.insert(b"a", b"1".to_vec()).unwrap();
        trie2.insert(b"b", b"2".to_vec()).unwrap();

        assert_eq!(trie1.root_hash(), trie2.root_hash());
    }

    #[test]
    fn test_from_root() {
        let db = Arc::new(MemoryTrieDb::new());
        let trie1 = Trie::new(db.clone());

        trie1.insert(b"key1", b"value1".to_vec()).unwrap();
        trie1.insert(b"key2", b"value2".to_vec()).unwrap();
        let root = trie1.root_hash();

        // Create new trie from same root
        let trie2 = Trie::from_root(db, root);
        assert_eq!(trie2.get(b"key1").unwrap(), Some(b"value1".to_vec()));
        assert_eq!(trie2.get(b"key2").unwrap(), Some(b"value2".to_vec()));
    }

    #[test]
    fn test_many_entries() {
        let trie = new_trie();

        // Insert 1000 entries
        for i in 0u32..1000 {
            let key = i.to_be_bytes();
            let value = format!("value{}", i).into_bytes();
            trie.insert(&key, value).unwrap();
        }

        // Verify all entries
        for i in 0u32..1000 {
            let key = i.to_be_bytes();
            let expected = format!("value{}", i).into_bytes();
            assert_eq!(trie.get(&key).unwrap(), Some(expected));
        }

        // Delete half
        for i in (0u32..1000).step_by(2) {
            let key = i.to_be_bytes();
            trie.delete(&key).unwrap();
        }

        // Verify remaining
        for i in 0u32..1000 {
            let key = i.to_be_bytes();
            if i % 2 == 0 {
                assert_eq!(trie.get(&key).unwrap(), None);
            } else {
                let expected = format!("value{}", i).into_bytes();
                assert_eq!(trie.get(&key).unwrap(), Some(expected));
            }
        }
    }
}
