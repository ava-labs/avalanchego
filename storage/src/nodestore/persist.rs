// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! # Persist Module
//!
//! This module handles all persistence operations for the nodestore, including writing
//! headers, nodes, and metadata to storage with support for different I/O backends.
//!
//! ## I/O Backend Support
//!
//! This module supports multiple I/O backends through conditional compilation:
//!
//! - **Standard I/O** - `#[cfg(not(feature = "io-uring"))]` - Uses standard file operations
//! - **io-uring** - `#[cfg(feature = "io-uring")]` - Uses Linux io-uring for async I/O
//!
//! This feature flag is automatically enabled when running on Linux, and disabled for all other platforms.
//!
//! The io-uring implementation provides:
//! - Asynchronous batch operations
//! - Reduced system call overhead
//! - Better performance for high-throughput workloads
//!
//! ## Performance Considerations
//!
//! - Nodes are written in batches to minimize I/O overhead
//! - Metrics are collected for flush operation timing
//! - Memory-efficient serialization with pre-allocated buffers
//! - Ring buffer management for io-uring operations

use std::iter::FusedIterator;

use crate::linear::FileIoError;
use crate::nodestore::AreaIndex;
use crate::{Child, firewood_counter};
use coarsetime::Instant;

use crate::{MaybePersistedNode, NodeReader, WritableStorage};

#[cfg(test)]
use crate::RootReader;

use super::alloc::NodeAllocator;
use super::header::NodeStoreHeader;
use super::{Committed, NodeStore};

#[cfg(not(test))]
use super::RootReader;

impl<T, S: WritableStorage> NodeStore<T, S> {
    /// Persist the header from this proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the header cannot be written.
    pub fn flush_header(&self) -> Result<(), FileIoError> {
        let header_bytes = bytemuck::bytes_of(&self.header);
        self.storage.write(0, header_bytes)?;
        Ok(())
    }

    /// Persist the header, including all the padding
    /// This is only done the first time we write the header
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the header cannot be written.
    pub fn flush_header_with_padding(&self) -> Result<(), FileIoError> {
        let mut header_bytes = bytemuck::bytes_of(&self.header).to_vec();
        header_bytes.resize(NodeStoreHeader::SIZE as usize, 0);
        debug_assert_eq!(header_bytes.len(), NodeStoreHeader::SIZE as usize);

        self.storage.write(0, &header_bytes)?;
        Ok(())
    }

    /// Persist the freelist from this proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the free list cannot be written to storage.
    pub fn flush_freelist(&self) -> Result<(), FileIoError> {
        self.flush_freelist_from(&self.header)
    }

    /// Persist the freelist from the given header to storage
    ///
    /// This function is used to ensure that the freelist is advanced after allocating
    /// nodes for writing. This allows the database to be recovered from an I/O error while
    /// persisting a revision to disk.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if the free list cannot be written to storage.
    #[fastrace::trace(name = "firewood.flush_freelist")]
    pub(crate) fn flush_freelist_from(&self, header: &NodeStoreHeader) -> Result<(), FileIoError> {
        let free_list_bytes = bytemuck::bytes_of(header.free_lists());
        let free_list_offset = NodeStoreHeader::free_lists_offset();
        self.storage.write(free_list_offset, free_list_bytes)?;
        Ok(())
    }
}

/// Iterator that returns unpersisted nodes in depth first order.
///
/// This iterator assumes the root node is unpersisted and will return it as the
/// last item. It looks at each node and traverses the children in depth first order.
/// A stack of child iterators is maintained to properly handle nested branches.
struct UnPersistedNodeIterator<'a, N> {
    store: &'a N,
    stack: Vec<MaybePersistedNode>,
    child_iter_stack: Vec<Box<dyn Iterator<Item = MaybePersistedNode> + 'a>>,
}

impl<N: NodeReader + RootReader> FusedIterator for UnPersistedNodeIterator<'_, N> {}

impl<'a, N: NodeReader + RootReader> UnPersistedNodeIterator<'a, N> {
    /// Creates a new iterator over unpersisted nodes in depth-first order.
    fn new(store: &'a N) -> Self {
        let root = store.root_as_maybe_persisted_node();

        // we must have an unpersisted root node to use this iterator
        // It's hard to tell at compile time if this is the case, so we assert it here
        // TODO: can we use another trait or generic to enforce this?
        debug_assert!(root.as_ref().is_none_or(|r| r.unpersisted().is_some()));
        let (child_iter_stack, stack) = if let Some(root) = root {
            if let Some(branch) = root
                .as_shared_node(store)
                .expect("in memory, so no io")
                .as_branch()
            {
                // Create an iterator over unpersisted children
                let unpersisted_children: Vec<MaybePersistedNode> = branch
                    .children
                    .iter_present()
                    .filter_map(|(_, child)| child.unpersisted().cloned())
                    .collect();

                (
                    vec![Box::new(unpersisted_children.into_iter())
                        as Box<dyn Iterator<Item = MaybePersistedNode> + 'a>],
                    vec![root],
                )
            } else {
                // root is a leaf
                (vec![], vec![root])
            }
        } else {
            (vec![], vec![])
        };

        Self {
            store,
            stack,
            child_iter_stack,
        }
    }
}

impl<N: NodeReader + RootReader> Iterator for UnPersistedNodeIterator<'_, N> {
    type Item = MaybePersistedNode;

    fn next(&mut self) -> Option<Self::Item> {
        // Try to get the next child from the current child iterator
        while let Some(current_iter) = self.child_iter_stack.last_mut() {
            if let Some(next_child) = current_iter.next() {
                let shared_node = next_child
                    .as_shared_node(self.store)
                    .expect("in memory, so IO is impossible");

                // It's a branch, so we need to get its children
                if let Some(branch) = shared_node.as_branch() {
                    // Create an iterator over unpersisted children
                    let unpersisted_children: Vec<MaybePersistedNode> = branch
                        .children
                        .iter_present()
                        .filter_map(|(_, child)| child.unpersisted().cloned())
                        .collect();

                    // Push new child iterator to the stack
                    if !unpersisted_children.is_empty() {
                        self.child_iter_stack
                            .push(Box::new(unpersisted_children.into_iter()));
                    }
                    self.stack.push(next_child); // visit this node after the children
                } else {
                    // leaf
                    return Some(next_child);
                }
            } else {
                // Current iterator is exhausted, remove it
                self.child_iter_stack.pop();
            }
        }

        // No more children to process, pop the next node from the stack
        self.stack.pop()
    }
}

/// Helper function to serialize a node into a bump allocator and allocate storage for it
///
/// # Errors
///
/// Returns a [`FileIoError`] if the node cannot be allocated in storage.
fn serialize_node_to_bump<'a>(
    bump: &'a bumpalo::Bump,
    shared_node: &crate::SharedNode,
    node_allocator: &mut NodeAllocator<'_, impl WritableStorage>,
) -> Result<(&'a [u8], crate::LinearAddress, usize), FileIoError> {
    let mut bytes = bumpalo::collections::Vec::new_in(bump);
    shared_node.as_bytes(AreaIndex::MIN, &mut bytes);
    let (persisted_address, area_size_index) = node_allocator.allocate_node(bytes.as_slice())?;
    *bytes.get_mut(0).expect("byte was reserved") = area_size_index.get();
    bytes.shrink_to_fit();
    let slice = bytes.into_bump_slice();
    Ok((slice, persisted_address, area_size_index.size() as usize))
}

/// Helper function to process unpersisted nodes with batching and overflow detection
///
/// This function iterates through all unpersisted nodes, serializes them into a bump allocator,
/// and flushes them in batches when the bump allocator is about to overflow.
///
/// # Errors
///
/// Returns a [`FileIoError`] if any node cannot be serialized, allocated, or written to storage.
pub(super) fn process_unpersisted_nodes<N, S, F>(
    bump: &mut bumpalo::Bump,
    node_allocator: &mut NodeAllocator<'_, S>,
    node_store: &N,
    bump_size_limit: usize,
    mut write_fn: F,
) -> Result<(), FileIoError>
where
    N: NodeReader + RootReader,
    S: WritableStorage,
    F: FnMut(Vec<(&[u8], crate::LinearAddress, MaybePersistedNode)>) -> Result<(), FileIoError>,
{
    let mut allocated_objects = Vec::new();

    // Process each unpersisted node directly from the iterator
    for node in UnPersistedNodeIterator::new(node_store) {
        let shared_node = node
            .as_shared_node(node_store)
            .expect("in memory, so no IO");

        // Serialize the node into the bump allocator
        let (slice, persisted_address, idx_size) =
            serialize_node_to_bump(bump, &shared_node, node_allocator)?;

        allocated_objects.push((slice, persisted_address, node));

        // we pause if we can't allocate another node of the same size as the last one
        // This isn't a guarantee that we won't exceed bump_size_limit
        // but it's a good enough approximation
        let might_overflow = bump.allocated_bytes() > bump_size_limit.saturating_sub(idx_size);
        if might_overflow {
            // must persist freelist before writing anything
            node_allocator.flush_freelist()?;
            write_fn(allocated_objects)?;
            allocated_objects = Vec::new();
            bump.reset();
        }
    }
    if !allocated_objects.is_empty() {
        // Flush the freelist using the node_allocator before the final write
        node_allocator.flush_freelist()?;
        write_fn(allocated_objects)?;
    }

    Ok(())
}

impl<S: WritableStorage + 'static> NodeStore<Committed, S> {
    /// Persist all the nodes of a proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any node cannot be written to storage.
    #[fastrace::trace(short_name = true)]
    pub fn flush_nodes(&mut self) -> Result<NodeStoreHeader, FileIoError> {
        let flush_start = Instant::now();

        let header = {
            #[cfg(feature = "io-uring")]
            {
                use crate::FileBacked;
                // Try to use io-uring if available and storage type supports it
                let this = self as &mut dyn std::any::Any;
                if let Some(file_backed) = this.downcast_mut::<NodeStore<Committed, FileBacked>>() {
                    file_backed.flush_nodes_io_uring()?
                } else {
                    self.flush_nodes_generic()?
                }
            }
            #[cfg(not(feature = "io-uring"))]
            {
                self.flush_nodes_generic()?
            }
        };

        let flush_time = flush_start.elapsed().as_millis();
        firewood_counter!("firewood.flush_nodes", "amount flushed nodes").increment(flush_time);

        Ok(header)
    }

    /// Persist all the nodes of a proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any node cannot be written to storage.
    fn flush_nodes_generic(&self) -> Result<NodeStoreHeader, FileIoError> {
        use bumpalo::Bump;

        let mut header = self.header;
        let mut node_allocator = NodeAllocator::new(self.storage.as_ref(), &mut header);
        let mut bump = Bump::with_capacity(super::INITIAL_BUMP_SIZE);

        process_unpersisted_nodes(
            &mut bump,
            &mut node_allocator,
            self,
            super::INITIAL_BUMP_SIZE,
            |allocated_objects| self.write_nodes_generic(allocated_objects),
        )?;

        Ok(header)
    }

    /// Write a batch of serialized nodes to storage
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any node cannot be written to storage.
    fn write_nodes_generic(
        &self,
        allocated_objects: Vec<(&[u8], crate::LinearAddress, MaybePersistedNode)>,
    ) -> Result<(), FileIoError> {
        // Collect addresses and nodes for caching
        let mut cached_nodes = Vec::new();

        for (serialized, persisted_address, node) in allocated_objects {
            self.storage.write(persisted_address.get(), serialized)?;

            // Allocate the node to store the address, then collect for caching and persistence
            node.allocate_at(persisted_address);
            cached_nodes.push(node);
        }

        self.storage.write_cached_nodes(cached_nodes)?;

        Ok(())
    }
}

impl<S: WritableStorage + 'static> NodeStore<Committed, S> {
    /// Persist the entire nodestore to storage.
    ///
    /// This method performs a complete persistence operation by:
    /// 1. Flushing all nodes to storage
    /// 2. Setting the root address in the header
    /// 3. Flushing the header to storage
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any of the persistence operations fail.
    #[fastrace::trace(short_name = true)]
    pub fn persist(&mut self) -> Result<(), FileIoError> {
        // First persist all the nodes
        self.header = self.flush_nodes()?;

        // Set the root address in the header based on the persisted root
        let root_address = self.kind.root.as_ref().and_then(Child::persisted_address);
        self.header.set_root_address(root_address);

        // Finally persist the header
        self.flush_header()?;

        Ok(())
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used, clippy::indexing_slicing)]
mod tests {
    use super::*;
    use crate::{
        Child, Children, HashType, ImmutableProposal, LinearAddress, NodeStore, Path,
        PathComponent, SharedNode,
        linear::memory::MemStore,
        node::{BranchNode, LeafNode, Node},
        nodestore::MutableProposal,
    };
    use std::sync::Arc;

    fn into_committed(
        ns: NodeStore<std::sync::Arc<ImmutableProposal>, MemStore>,
        parent: &NodeStore<Committed, MemStore>,
    ) -> NodeStore<Committed, MemStore> {
        ns.flush_freelist().unwrap();
        ns.flush_header().unwrap();
        let mut ns = ns.as_committed(parent);
        ns.flush_nodes().unwrap();
        ns
    }

    /// Helper to create a test node store with a specific root
    fn create_test_store_with_root(root: Node) -> NodeStore<MutableProposal, MemStore> {
        let mem_store = MemStore::new(vec![]).into();
        let mut store = NodeStore::new_empty_proposal(mem_store);
        store.root_mut().replace(root);
        store
    }

    /// Helper to create a leaf node
    fn create_leaf(path: &[u8], value: &[u8]) -> Node {
        Node::Leaf(LeafNode {
            partial_path: Path::from(path),
            value: value.to_vec().into_boxed_slice(),
        })
    }

    /// Helper to create a branch node with children
    fn create_branch(
        path: &[u8],
        value: Option<&[u8]>,
        children: Vec<(PathComponent, Node)>,
    ) -> Node {
        let mut branch = BranchNode {
            partial_path: Path::from(path),
            value: value.map(|v| v.to_vec().into_boxed_slice()),
            children: Children::new(),
        };

        for (index, child) in children {
            let shared_child = SharedNode::new(child);
            let maybe_persisted = MaybePersistedNode::from(shared_child);
            let hash = HashType::empty();
            branch.children[index] = Some(Child::MaybePersisted(maybe_persisted, hash));
        }

        Node::Branch(Box::new(branch))
    }

    #[test]
    fn test_empty_nodestore() {
        let mem_store = MemStore::new(vec![]).into();
        let store = NodeStore::new_empty_proposal(mem_store);
        let mut iter = UnPersistedNodeIterator::new(&store);

        assert!(iter.next().is_none());
    }

    #[test]
    fn test_single_leaf_node() {
        let leaf = create_leaf(&[1, 2, 3], &[4, 5, 6]);
        let store = create_test_store_with_root(leaf.clone());
        let mut iter =
            UnPersistedNodeIterator::new(&store).map(|node| node.as_shared_node(&store).unwrap());

        // Should return the leaf node
        let node = iter.next().unwrap();
        assert_eq!(*node, leaf);

        // Should be exhausted
        assert!(iter.next().is_none());
    }

    #[test]
    fn test_branch_with_single_child() {
        let leaf = create_leaf(&[7, 8], &[9, 10]);
        let branch = create_branch(
            &[1, 2],
            Some(&[3, 4]),
            vec![(PathComponent::ALL[5], leaf.clone())],
        );
        let store = create_test_store_with_root(branch.clone());
        let mut iter =
            UnPersistedNodeIterator::new(&store).map(|node| node.as_shared_node(&store).unwrap());

        // Should return child first (depth-first)
        let node = iter.next().unwrap();
        assert_eq!(*node, leaf);

        // Then the branch
        let node = iter.next().unwrap();
        assert_eq!(&*node, &branch);

        assert!(iter.next().is_none());

        // verify iterator is fused
        assert!(iter.next().is_none());
    }

    #[test]
    fn test_branch_with_multiple_children() {
        let leaves = [
            create_leaf(&[1], &[10]),
            create_leaf(&[2], &[20]),
            create_leaf(&[3], &[30]),
        ];
        let branch = create_branch(
            &[0],
            None,
            vec![
                (PathComponent::ALL[1], leaves[0].clone()),
                (PathComponent::ALL[5], leaves[1].clone()),
                (PathComponent::ALL[10], leaves[2].clone()),
            ],
        );
        let store = create_test_store_with_root(branch.clone());

        // Collect all nodes
        let nodes: Vec<_> = UnPersistedNodeIterator::new(&store)
            .map(|node| node.as_shared_node(&store).unwrap())
            .collect();

        // Should have 4 nodes total (3 leaves + 1 branch)
        assert_eq!(nodes.len(), 4);

        // The branch should be last (depth-first)
        assert_eq!(&*nodes[3], &branch);

        // Children should come first - verify all expected leaf nodes are present
        let children_nodes = &nodes[0..3];
        assert!(children_nodes.iter().any(|n| **n == leaves[0]));
        assert!(children_nodes.iter().any(|n| **n == leaves[1]));
        assert!(children_nodes.iter().any(|n| **n == leaves[2]));
    }

    #[test]
    fn test_nested_branches() {
        let leaves = [
            create_leaf(&[1], &[100]),
            create_leaf(&[2], &[200]),
            create_leaf(&[3], &[255]),
        ];

        // Create a nested structure: root -> branch1 -> leaf[0]
        //                                -> leaf[1]
        //                                -> branch2 -> leaf[2]
        let inner_branch = create_branch(
            &[10],
            Some(&[50]),
            vec![(PathComponent::ALL[0], leaves[2].clone())],
        );

        let mut children = Children::new();
        for (value, (_, slot)) in [
            // unpersisted leaves
            Child::MaybePersisted(
                MaybePersistedNode::from(SharedNode::new(leaves[0].clone())),
                HashType::empty(),
            ),
            Child::MaybePersisted(
                MaybePersistedNode::from(SharedNode::new(leaves[1].clone())),
                HashType::empty(),
            ),
            // unpersisted branch
            Child::MaybePersisted(
                MaybePersistedNode::from(SharedNode::new(inner_branch.clone())),
                HashType::empty(),
            ),
            // persisted branch
            Child::MaybePersisted(
                MaybePersistedNode::from(LinearAddress::new(42).unwrap()),
                HashType::empty(),
            ),
        ]
        .into_iter()
        .zip(children.iter_mut())
        {
            slot.replace(value);
        }

        let root_branch: Node = BranchNode {
            partial_path: Path::new(),
            value: None,
            children,
        }
        .into();

        let store = create_test_store_with_root(root_branch.clone());

        // Collect all nodes
        let nodes: Vec<_> = UnPersistedNodeIterator::new(&store)
            .map(|node| node.as_shared_node(&store).unwrap())
            .collect();

        // Should have 5 nodes total (3 leaves + 2 branches)
        assert_eq!(nodes.len(), 5);

        // The root branch should be last (depth-first)
        assert_eq!(**nodes.last().unwrap(), root_branch);

        // Find positions of some nodes
        let root_pos = nodes.iter().position(|n| **n == root_branch).unwrap();
        let inner_branch_pos = nodes.iter().position(|n| **n == inner_branch).unwrap();
        let leaf3_pos = nodes.iter().position(|n| **n == leaves[2]).unwrap();

        // Verify depth-first ordering: leaf3 should come before inner_branch,
        // inner_branch should come before root_branch
        assert!(leaf3_pos < inner_branch_pos);
        assert!(inner_branch_pos < root_pos);
    }

    #[test]
    fn test_into_committed_with_generic_storage() {
        // Create a base committed store with MemStore
        let mem_store = MemStore::new(vec![]);
        let base_committed = NodeStore::new_empty_committed(mem_store.into());

        // Create a mutable proposal from the base
        let mut mutable_store = NodeStore::new(&base_committed).unwrap();

        // Add some nodes to the mutable store
        let leaf1 = create_leaf(&[1, 2, 3], b"value1");
        let leaf2 = create_leaf(&[4, 5, 6], b"value2");
        let branch = create_branch(
            &[0],
            Some(b"branch_value"),
            vec![
                (PathComponent::ALL[1], leaf1.clone()),
                (PathComponent::ALL[2], leaf2.clone()),
            ],
        );

        mutable_store.root_mut().replace(branch.clone());

        // Convert to immutable proposal
        let immutable_store: NodeStore<Arc<ImmutableProposal>, _> =
            mutable_store.try_into().unwrap();

        // Commit the immutable store
        let committed_store = into_committed(immutable_store, &base_committed);

        // Verify the committed store has the expected values
        let root = committed_store.kind.root.as_ref().unwrap();
        let root_maybe_persisted = root.as_maybe_persisted_node();
        let root_node = root_maybe_persisted
            .as_shared_node(&committed_store)
            .unwrap();
        assert_eq!(*root_node.partial_path(), Path::from(&[0]));
        assert_eq!(root_node.value(), Some(&b"branch_value"[..]));
        assert!(root_node.is_branch());
        let root_branch = root_node.as_branch().unwrap();
        assert_eq!(root_branch.children.count(), 2);

        let child1 = root_branch.children[PathComponent::ALL[1]]
            .as_ref()
            .unwrap();
        let child1_maybe_persisted = child1.as_maybe_persisted_node();
        let child1_node = child1_maybe_persisted
            .as_shared_node(&committed_store)
            .unwrap();
        assert_eq!(*child1_node.partial_path(), Path::from(&[1, 2, 3]));
        assert_eq!(child1_node.value(), Some(&b"value1"[..]));

        let child2 = root_branch.children[PathComponent::ALL[2]]
            .as_ref()
            .unwrap();
        let child2_maybe_persisted = child2.as_maybe_persisted_node();
        let child2_node = child2_maybe_persisted
            .as_shared_node(&committed_store)
            .unwrap();
        assert_eq!(*child2_node.partial_path(), Path::from(&[4, 5, 6]));
        assert_eq!(child2_node.value(), Some(&b"value2"[..]));
    }
}
