// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::sync::Arc;

use arc_swap::ArcSwap;

use crate::{FileIoError, LinearAddress, NodeReader, SharedNode};

/// A node that is either in memory or on disk.
///
/// In-memory nodes that can be moved to disk. This structure allows that to happen
/// atomically.
///
/// `MaybePersistedNode` owns a reference counted pointer to an atomically swapable
/// pointer. The atomically swapable pointer points to a reference counted pointer
/// to the enum of either an un-persisted but committed (or committing) node or the
/// linear address of a persisted node.
///
/// This type is complicated, so here is a breakdown of the types involved:
///
/// | Item                   | Description                                            |
/// |------------------------|--------------------------------------------------------|
/// | [`MaybePersistedNode`] | Newtype wrapper around the remaining items.            |
/// | [Arc]                  | reference counted pointer to an updatable pointer.     |
/// | [`ArcSwap`]            | Swappable [Arc]. Actually an `ArcSwapAny`<`Arc`<_>>    |
/// | [Arc]                  | reference-counted pointer to the enum (required by `ArcSwap`) |
/// | `MaybePersisted`       | Enum of either `Unpersisted` or `Persisted`            |
/// | variant `Unpersisted`  | The shared node, in memory, for unpersisted nodes      |
/// | -> [`SharedNode`]      | A `triomphe::Arc` of a [Node](`crate::Node`)           |
/// | variant `Persisted`    | The address of a persisted node.                       |
/// | -> [`LinearAddress`]   | A 64-bit address for a persisted node on disk.         |
///
/// Traversing these pointers does incur a runtime penalty.
///
/// When an `Unpersisted` node is `Persisted` using [`MaybePersistedNode::persist_at`],
/// a new `Arc` is created to the new `MaybePersisted::Persisted` variant and the `ArcSwap`
/// is updated atomically. Subsequent accesses to any instance of it, including any clones,
/// will see the `Persisted` node address.
#[derive(Debug, Clone)]
pub struct MaybePersistedNode(Arc<ArcSwap<MaybePersisted>>);

impl From<SharedNode> for MaybePersistedNode {
    fn from(node: SharedNode) -> Self {
        MaybePersistedNode(Arc::new(ArcSwap::new(Arc::new(
            MaybePersisted::Unpersisted(node),
        ))))
    }
}

impl From<LinearAddress> for MaybePersistedNode {
    fn from(address: LinearAddress) -> Self {
        MaybePersistedNode(Arc::new(ArcSwap::new(Arc::new(MaybePersisted::Persisted(
            address,
        )))))
    }
}

impl From<&MaybePersistedNode> for Option<LinearAddress> {
    fn from(node: &MaybePersistedNode) -> Option<LinearAddress> {
        match node.0.load().as_ref() {
            MaybePersisted::Unpersisted(_) => None,
            MaybePersisted::Persisted(address) => Some(*address),
        }
    }
}

impl MaybePersistedNode {
    /// Converts this `MaybePersistedNode` to a `SharedNode` by reading from the appropriate source.
    ///
    /// If the node is in memory, it returns a clone of the in-memory node.
    /// If the node is on disk, it reads the node from storage using the provided `NodeReader`.
    ///
    /// # Arguments
    ///
    /// * `storage` - A reference to a `NodeReader` implementation that can read nodes from storage
    ///
    /// # Returns
    ///
    /// Returns a `Result<SharedNode, FileIoError>` where:
    /// - `Ok(SharedNode)` contains the node if successfully retrieved
    /// - `Err(FileIoError)` if there was an error reading from storage
    pub fn as_shared_node<S: NodeReader>(&self, storage: &S) -> Result<SharedNode, FileIoError> {
        match self.0.load().as_ref() {
            MaybePersisted::Unpersisted(node) => Ok(node.clone()),
            MaybePersisted::Persisted(address) => storage.read_node(*address),
        }
    }

    /// Returns the linear address of the node if it is persisted on disk.
    ///
    /// # Returns
    ///
    /// Returns `Some(LinearAddress)` if the node is persisted on disk, otherwise `None`.
    #[must_use]
    pub fn as_linear_address(&self) -> Option<LinearAddress> {
        match self.0.load().as_ref() {
            MaybePersisted::Unpersisted(_) => None,
            MaybePersisted::Persisted(address) => Some(*address),
        }
    }

    /// Updates the internal state to indicate this node is persisted at the specified disk address.
    ///
    /// This method changes the internal state of the `MaybePersistedNode` from `Mem` to `Disk`,
    /// indicating that the node has been written to the specified disk location.
    ///
    /// This is done atomically using the `ArcSwap` mechanism.
    ///
    /// # Arguments
    ///
    /// * `addr` - The `LinearAddress` where the node has been persisted on disk
    pub fn persist_at(&self, addr: LinearAddress) {
        self.0.store(Arc::new(MaybePersisted::Persisted(addr)));
    }
}

/// The internal state of a `MaybePersistedNode`.
///
/// This enum represents the two possible states of a `MaybePersisted`:
/// - `Unpersisted(SharedNode)`: The node is currently in memory
/// - `Persisted(LinearAddress)`: The node is currently on disk at the specified address
#[derive(Debug)]
enum MaybePersisted {
    Unpersisted(SharedNode),
    Persisted(LinearAddress),
}

#[cfg(test)]
#[expect(clippy::unwrap_used)]
mod test {
    use nonzero_ext::nonzero;

    use crate::{LeafNode, MemStore, Node, NodeStore, Path};

    use super::*;

    #[test]
    fn test_maybe_persisted_node() -> Result<(), FileIoError> {
        let mem_store = MemStore::new(vec![]).into();
        let store = NodeStore::new_empty_committed(mem_store)?;
        let node = SharedNode::new(Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: vec![0].into(),
        }));
        // create as unpersisted
        let maybe_persisted_node = MaybePersistedNode::from(node.clone());
        let addr = nonzero!(2048u64);
        assert_eq!(maybe_persisted_node.as_shared_node(&store).unwrap(), node);
        assert_eq!(
            Option::<LinearAddress>::None,
            Option::from(&maybe_persisted_node)
        );

        maybe_persisted_node.persist_at(addr);
        assert!(maybe_persisted_node.as_shared_node(&store).is_err());
        assert_eq!(Some(addr), Option::from(&maybe_persisted_node));
        Ok(())
    }

    #[test]
    fn test_from_linear_address() {
        let addr = nonzero!(1024u64);
        let maybe_persisted_node = MaybePersistedNode::from(addr);
        assert_eq!(Some(addr), Option::from(&maybe_persisted_node));
    }

    #[test]
    fn test_clone_shares_underlying_shared_node() -> Result<(), FileIoError> {
        let mem_store = MemStore::new(vec![]).into();
        let store = NodeStore::new_empty_committed(mem_store)?;
        let node = SharedNode::new(Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: vec![42].into(),
        }));

        let original = MaybePersistedNode::from(node.clone());
        let cloned = original.clone();

        // Both should be unpersisted initially
        assert_eq!(original.as_shared_node(&store).unwrap(), node);
        assert_eq!(cloned.as_shared_node(&store).unwrap(), node);
        assert_eq!(Option::<LinearAddress>::None, Option::from(&original));
        assert_eq!(Option::<LinearAddress>::None, Option::from(&cloned));

        // First reference is 'node', second is shared by original and cloned
        assert_eq!(triomphe::Arc::strong_count(&node), 2);

        // Persist the original
        let addr = nonzero!(4096u64);
        original.persist_at(addr);

        // Both original and clone should now be persisted since they share the same ArcSwap
        assert!(original.as_shared_node(&store).is_err());
        assert!(cloned.as_shared_node(&store).is_err());
        assert_eq!(Some(addr), Option::from(&original));
        assert_eq!(Some(addr), Option::from(&cloned));

        // 'node' is no longer referenced by anyone but our local copy,
        // so the count should be 1
        assert_eq!(triomphe::Arc::strong_count(&node), 1);

        Ok(())
    }
}
