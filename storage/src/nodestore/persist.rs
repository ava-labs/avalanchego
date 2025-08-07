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
//!
//!

use std::iter::FusedIterator;

use crate::linear::FileIoError;
use crate::{firewood_counter, firewood_gauge};
use coarsetime::Instant;

#[cfg(feature = "io-uring")]
use crate::logger::trace;

use crate::{FileBacked, MaybePersistedNode, NodeReader, WritableStorage};

#[cfg(test)]
use crate::RootReader;

#[cfg(feature = "io-uring")]
use crate::ReadableStorage;

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
    #[fastrace::trace(short_name = true)]
    pub fn flush_freelist(&self) -> Result<(), FileIoError> {
        // Write the free lists to storage
        let free_list_bytes = bytemuck::bytes_of(self.header.free_lists());
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
                    .iter()
                    .filter_map(|child_opt| {
                        child_opt
                            .as_ref()
                            .and_then(|child| child.unpersisted().cloned())
                    })
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
                        .iter()
                        .filter_map(|child_opt| {
                            child_opt
                                .as_ref()
                                .and_then(|child| child.unpersisted().cloned())
                        })
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

impl<S: WritableStorage + 'static> NodeStore<Committed, S> {
    /// Persist all the nodes of a proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any node cannot be written to storage.
    #[fastrace::trace(short_name = true)]
    pub fn flush_nodes(&mut self) -> Result<NodeStoreHeader, FileIoError> {
        #[cfg(feature = "io-uring")]
        if let Some(this) = self.downcast_to_file_backed() {
            return this.flush_nodes_io_uring();
        }
        self.flush_nodes_generic()
    }

    #[cfg(feature = "io-uring")]
    #[inline]
    fn downcast_to_file_backed(&mut self) -> Option<&mut NodeStore<Committed, FileBacked>> {
        /*
         * FIXME(rust-lang/rfcs#1210, rust-lang/rust#31844):
         *
         * This is a slight hack that exists because rust trait specialization
         * is not yet stable. If the `io-uring` feature is enabled, we attempt to
         * downcast `self` into a `NodeStore<Committed, FileBacked>`, and if successful,
         * we call the specialized `flush_nodes_io_uring` method.
         *
         * During monomorphization, this will be completely optimized out as the
         * type id comparison is done with constants that the compiler can resolve
         * and use to detect dead branches.
         */
        let this = self as &mut dyn std::any::Any;
        this.downcast_mut::<NodeStore<Committed, FileBacked>>()
    }

    /// Persist all the nodes of a proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any node cannot be written to storage.
    fn flush_nodes_generic(&self) -> Result<NodeStoreHeader, FileIoError> {
        let flush_start = Instant::now();

        // keep MaybePersistedNodes to add them to cache and persist them
        let mut cached_nodes = Vec::new();

        let mut header = self.header;
        let mut allocator = NodeAllocator::new(self.storage.as_ref(), &mut header);
        for node in UnPersistedNodeIterator::new(self) {
            let shared_node = node.as_shared_node(self).expect("in memory, so no IO");
            let mut serialized = Vec::new();
            shared_node.as_bytes(0, &mut serialized);

            let (persisted_address, area_size_index) =
                allocator.allocate_node(serialized.as_slice())?;
            *serialized.get_mut(0).expect("byte was reserved") = area_size_index;
            self.storage
                .write(persisted_address.get(), serialized.as_slice())?;

            // Decrement gauge immediately after node is written to storage
            firewood_gauge!(
                "firewood.nodes.unwritten",
                "current number of unwritten nodes"
            )
            .decrement(1.0);

            // Allocate the node to store the address, then collect for caching and persistence
            node.allocate_at(persisted_address);
            cached_nodes.push(node);
        }

        self.storage.write_cached_nodes(cached_nodes)?;

        let flush_time = flush_start.elapsed().as_millis();
        firewood_counter!("firewood.flush_nodes", "flushed node amount").increment(flush_time);

        Ok(header)
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
        let root_address = self
            .kind
            .root
            .as_ref()
            .and_then(crate::MaybePersistedNode::as_linear_address);
        self.header.set_root_address(root_address);

        // Finally persist the header
        self.flush_header()?;

        // Reset unwritten nodes counter to zero since all nodes are now persisted
        self.kind
            .unwritten_nodes
            .store(0, std::sync::atomic::Ordering::Relaxed);

        Ok(())
    }
}

impl NodeStore<Committed, FileBacked> {
    /// Persist all the nodes of a proposal to storage.
    ///
    /// # Errors
    ///
    /// Returns a [`FileIoError`] if any node cannot be written to storage.
    #[fastrace::trace(short_name = true)]
    #[cfg(feature = "io-uring")]
    fn flush_nodes_io_uring(&mut self) -> Result<NodeStoreHeader, FileIoError> {
        use crate::LinearAddress;
        use std::pin::Pin;

        #[derive(Clone, Debug)]
        struct PinnedBufferEntry {
            pinned_buffer: Pin<Box<[u8]>>,
            node: Option<(LinearAddress, MaybePersistedNode)>,
        }

        /// Helper function to handle completion queue entries and check for errors
        /// Returns the number of completed operations
        fn handle_completion_queue(
            storage: &FileBacked,
            completion_queue: io_uring::cqueue::CompletionQueue<'_>,
            saved_pinned_buffers: &mut [PinnedBufferEntry],
        ) -> Result<usize, FileIoError> {
            let mut completed_count = 0usize;
            for entry in completion_queue {
                let item = entry.user_data() as usize;
                let pbe = saved_pinned_buffers
                    .get_mut(item)
                    .expect("should be an index into the array");

                if entry.result()
                    != pbe
                        .pinned_buffer
                        .len()
                        .try_into()
                        .expect("buffer should be small enough")
                {
                    let error = if entry.result() >= 0 {
                        std::io::Error::other("Partial write")
                    } else {
                        std::io::Error::from_raw_os_error(0 - entry.result())
                    };
                    let (addr, _) = pbe.node.as_ref().expect("node should be Some");
                    return Err(storage.file_io_error(
                        error,
                        addr.get(),
                        Some("write failure".to_string()),
                    ));
                }
                // I/O completed successfully
                pbe.node = None;
                completed_count = completed_count.wrapping_add(1);
            }
            Ok(completed_count)
        }

        const RINGSIZE: usize = FileBacked::RINGSIZE as usize;

        let flush_start = Instant::now();

        let mut header = self.header;
        let mut node_allocator = NodeAllocator::new(self.storage.as_ref(), &mut header);

        // Collect addresses and nodes for caching
        let mut cached_nodes = Vec::new();

        let mut ring = self.storage.ring.lock().expect("poisoned lock");
        let mut saved_pinned_buffers = vec![
            PinnedBufferEntry {
                pinned_buffer: Pin::new(Box::new([0; 0])),
                node: None,
            };
            RINGSIZE
        ];

        // Process each unpersisted node directly from the iterator
        for node in UnPersistedNodeIterator::new(self) {
            let shared_node = node.as_shared_node(self).expect("in memory, so no IO");
            let mut serialized = Vec::with_capacity(100); // TODO: better size? we can guess branches are larger
            shared_node.as_bytes(0, &mut serialized);
            let (persisted_address, area_size_index) =
                node_allocator.allocate_node(serialized.as_slice())?;
            *serialized.get_mut(0).expect("byte was reserved") = area_size_index;
            let mut serialized = serialized.into_boxed_slice();

            loop {
                // Find the first available write buffer, enumerate to get the position for marking it completed
                if let Some((pos, pbe)) = saved_pinned_buffers
                    .iter_mut()
                    .enumerate()
                    .find(|(_, pbe)| pbe.node.is_none())
                {
                    pbe.pinned_buffer = std::pin::Pin::new(std::mem::take(&mut serialized));
                    pbe.node = Some((persisted_address, node.clone()));

                    let submission_queue_entry = self
                        .storage
                        .make_op(&pbe.pinned_buffer)
                        .offset(persisted_address.get())
                        .build()
                        .user_data(pos as u64);

                    // SAFETY: the submission_queue_entry's found buffer must not move or go out of scope
                    // until the operation has been completed. This is ensured by having a Some(offset)
                    // and not marking it None until the kernel has said it's done below.
                    #[expect(unsafe_code)]
                    while unsafe { ring.submission().push(&submission_queue_entry) }.is_err() {
                        ring.submitter().squeue_wait().map_err(|e| {
                            self.storage.file_io_error(
                                e,
                                persisted_address.get(),
                                Some("io-uring squeue_wait".to_string()),
                            )
                        })?;
                        trace!("submission queue is full");
                        firewood_counter!("ring.full", "amount of full ring").increment(1);
                    }
                    break;
                }
                // if we get here, that means we couldn't find a place to queue the request, so wait for at least one operation
                // to complete, then handle the completion queue
                firewood_counter!("ring.full", "amount of full ring").increment(1);
                ring.submit_and_wait(1).map_err(|e| {
                    self.storage
                        .file_io_error(e, 0, Some("io-uring submit_and_wait".to_string()))
                })?;
                let completion_queue = ring.completion();
                trace!("competion queue length: {}", completion_queue.len());
                let completed_writes = handle_completion_queue(
                    &self.storage,
                    completion_queue,
                    &mut saved_pinned_buffers,
                )?;

                // Decrement gauge for writes that have actually completed
                if completed_writes > 0 {
                    #[expect(clippy::cast_precision_loss)]
                    firewood_gauge!(
                        "firewood.nodes.unwritten",
                        "current number of unwritten nodes"
                    )
                    .decrement(completed_writes as f64);
                }
            }

            // Allocate the node to store the address, then collect for caching and persistence
            node.allocate_at(persisted_address);
            cached_nodes.push(node);
        }
        let pending = saved_pinned_buffers
            .iter()
            .filter(|pbe| pbe.node.is_some())
            .count();
        ring.submit_and_wait(pending).map_err(|e| {
            self.storage
                .file_io_error(e, 0, Some("io-uring final submit_and_wait".to_string()))
        })?;

        let final_completed_writes =
            handle_completion_queue(&self.storage, ring.completion(), &mut saved_pinned_buffers)?;

        // Decrement gauge for final batch of writes that completed
        if final_completed_writes > 0 {
            #[expect(clippy::cast_precision_loss)]
            firewood_gauge!(
                "firewood.nodes.unwritten",
                "current number of unwritten nodes"
            )
            .decrement(final_completed_writes as f64);
        }

        debug_assert!(
            !saved_pinned_buffers.iter().any(|pbe| pbe.node.is_some()),
            "Found entry with node still set: {:?}",
            saved_pinned_buffers.iter().find(|pbe| pbe.node.is_some())
        );

        self.storage.write_cached_nodes(cached_nodes)?;
        debug_assert!(ring.completion().is_empty());

        let flush_time = flush_start.elapsed().as_millis();
        firewood_counter!("firewood.flush_nodes", "amount flushed nodes").increment(flush_time);

        Ok(header)
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used, clippy::indexing_slicing)]
mod tests {
    use super::*;
    use crate::{
        Child, HashType, ImmutableProposal, LinearAddress, NodeStore, Path, SharedNode,
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
        store.mut_root().replace(root);
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
    fn create_branch(path: &[u8], value: Option<&[u8]>, children: Vec<(u8, Node)>) -> Node {
        let mut branch = BranchNode {
            partial_path: Path::from(path),
            value: value.map(|v| v.to_vec().into_boxed_slice()),
            children: std::array::from_fn(|_| None),
        };

        for (index, child) in children {
            let shared_child = SharedNode::new(child);
            let maybe_persisted = MaybePersistedNode::from(shared_child);
            let hash = HashType::empty();
            branch.children[index as usize] = Some(Child::MaybePersisted(maybe_persisted, hash));
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
        let branch = create_branch(&[1, 2], Some(&[3, 4]), vec![(5, leaf.clone())]);
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
                (1, leaves[0].clone()),
                (5, leaves[1].clone()),
                (10, leaves[2].clone()),
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
        let inner_branch = create_branch(&[10], Some(&[50]), vec![(0, leaves[2].clone())]);

        let mut children = BranchNode::empty_children();
        for (value, slot) in [
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
        let base_committed = NodeStore::new_empty_committed(mem_store.into()).unwrap();

        // Create a mutable proposal from the base
        let mut mutable_store = NodeStore::new(&base_committed).unwrap();

        // Add some nodes to the mutable store
        let leaf1 = create_leaf(&[1, 2, 3], b"value1");
        let leaf2 = create_leaf(&[4, 5, 6], b"value2");
        let branch = create_branch(
            &[0],
            Some(b"branch_value"),
            vec![(1, leaf1.clone()), (2, leaf2.clone())],
        );

        mutable_store.mut_root().replace(branch.clone());

        // Convert to immutable proposal
        let immutable_store: NodeStore<Arc<ImmutableProposal>, _> =
            mutable_store.try_into().unwrap();

        // Commit the immutable store
        let committed_store = into_committed(immutable_store, &base_committed);

        // Verify the committed store has the expected values
        let root = committed_store.kind.root.as_ref().unwrap();
        let root_node = root.as_shared_node(&committed_store).unwrap();
        assert_eq!(*root_node.partial_path(), Path::from(&[0]));
        assert_eq!(root_node.value(), Some(&b"branch_value"[..]));
        assert!(root_node.is_branch());
        let root_branch = root_node.as_branch().unwrap();
        assert_eq!(
            root_branch.children.iter().filter(|c| c.is_some()).count(),
            2
        );

        let child1 = root_branch.children[1].as_ref().unwrap();
        let child1_node = child1
            .as_maybe_persisted_node()
            .as_shared_node(&committed_store)
            .unwrap();
        assert_eq!(*child1_node.partial_path(), Path::from(&[1, 2, 3]));
        assert_eq!(child1_node.value(), Some(&b"value1"[..]));

        let child2 = root_branch.children[2].as_ref().unwrap();
        let child2_node = child2
            .as_maybe_persisted_node()
            .as_shared_node(&committed_store)
            .unwrap();
        assert_eq!(*child2_node.partial_path(), Path::from(&[4, 5, 6]));
        assert_eq!(child2_node.value(), Some(&b"value2"[..]));
    }

    #[cfg(feature = "io-uring")]
    #[test]
    fn test_downcast_to_file_backed() {
        use nonzero_ext::nonzero;

        use crate::CacheReadStrategy;

        {
            let tf = tempfile::NamedTempFile::new().unwrap();
            let path = tf.path().to_owned();

            let fb = Arc::new(
                FileBacked::new(
                    path,
                    nonzero!(10usize),
                    nonzero!(10usize),
                    false,
                    CacheReadStrategy::WritesOnly,
                )
                .unwrap(),
            );

            let mut ns = NodeStore::new_empty_committed(fb.clone()).unwrap();

            assert!(ns.downcast_to_file_backed().is_some());
        }

        {
            let ms = Arc::new(MemStore::new(vec![]));
            let mut ns = NodeStore::new_empty_committed(ms.clone()).unwrap();
            assert!(ns.downcast_to_file_backed().is_none());
        }
    }
}
