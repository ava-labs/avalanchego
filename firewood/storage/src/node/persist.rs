// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use parking_lot::Mutex;
use std::{fmt::Display, sync::Arc};

use crate::{FileIoError, LinearAddress, NodeReader, SharedNode};

/// A node that is either in memory or on disk.
///
/// In-memory nodes that can be moved to disk. This structure allows that to happen
/// atomically.
///
/// `MaybePersistedNode` owns a reference-counted pointer to a mutex-protected
/// enum representing either an un-persisted (or allocating) node or the
/// linear address of a persisted node.
///
/// This type is complicated, so here is a breakdown of the types involved:
///
/// | Item                   | Description                                            |
/// |------------------------|--------------------------------------------------------|
/// | [`MaybePersistedNode`] | Newtype wrapper around the remaining items.            |
/// | [Arc]                  | Reference counted pointer to a mutexed enum            |
/// | `Mutex`                | Protects the inner enum during updates                 |
/// | `MaybePersisted`       | Enum of either `Unpersisted` or `Persisted`            |
/// | variant `Unpersisted`  | The shared node, in memory, for unpersisted nodes      |
/// | -> [`SharedNode`]      | A `triomphe::Arc` of a [Node](`crate::Node`)           |
/// | variant `Persisted`    | The address of a persisted node.                       |
/// | -> [`LinearAddress`]   | A 64-bit address for a persisted node on disk.         |
///
/// Traversing these pointers does incur a runtime penalty.
///
/// When an `Unpersisted` node is `Persisted` using [`MaybePersistedNode::persist_at`],
/// the enum value inside the mutex is replaced under the lock. Subsequent accesses
/// to any instance of it, including any clones, will see the `Persisted` node address.
#[derive(Debug, Clone)]
pub struct MaybePersistedNode(Arc<Mutex<MaybePersisted>>);

impl PartialEq<MaybePersistedNode> for MaybePersistedNode {
    fn eq(&self, other: &MaybePersistedNode) -> bool {
        // if underlying mutex is same, this is necessary to avoid deadlock
        if Arc::ptr_eq(&self.0, &other.0) {
            return true;
        }
        *self.0.lock() == *other.0.lock()
    }
}

impl Eq for MaybePersistedNode {}

impl From<SharedNode> for MaybePersistedNode {
    fn from(node: SharedNode) -> Self {
        MaybePersistedNode(Arc::new(Mutex::new(MaybePersisted::Unpersisted(node))))
    }
}

impl From<LinearAddress> for MaybePersistedNode {
    fn from(address: LinearAddress) -> Self {
        MaybePersistedNode(Arc::new(Mutex::new(MaybePersisted::Persisted(address))))
    }
}

impl From<&MaybePersistedNode> for Option<LinearAddress> {
    fn from(node: &MaybePersistedNode) -> Option<LinearAddress> {
        match &*node.0.lock() {
            MaybePersisted::Unpersisted(_) => None,
            MaybePersisted::Allocated(address, _) | MaybePersisted::Persisted(address) => {
                Some(*address)
            }
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
        match &*self.0.lock() {
            MaybePersisted::Allocated(_, node) | MaybePersisted::Unpersisted(node) => {
                Ok(node.clone())
            }
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
        match &*self.0.lock() {
            MaybePersisted::Unpersisted(_) => None,
            MaybePersisted::Allocated(address, _) | MaybePersisted::Persisted(address) => {
                Some(*address)
            }
        }
    }

    /// Returns a reference to the unpersisted node if it is unpersisted.
    ///
    /// # Returns
    ///
    /// Returns `Some(&Self)` if the node is unpersisted, otherwise `None`.
    #[must_use]
    pub fn unpersisted(&self) -> Option<&Self> {
        match &*self.0.lock() {
            MaybePersisted::Allocated(_, _) | MaybePersisted::Unpersisted(_) => Some(self),
            MaybePersisted::Persisted(_) => None,
        }
    }

    /// Updates the internal state to indicate this node is persisted at the specified disk address.
    ///
    /// This method changes the internal state of the `MaybePersistedNode` from `Mem` to `Disk`,
    /// indicating that the node has been written to the specified disk location.
    ///
    /// This is done under a `Mutex` lock.
    ///
    /// # Arguments
    ///
    /// * `addr` - The `LinearAddress` where the node has been persisted on disk
    pub fn persist_at(&self, addr: LinearAddress) {
        *self.0.lock() = MaybePersisted::Persisted(addr);
    }

    /// Updates the internal state to indicate this node is allocated at the specified disk address.
    ///
    /// This method changes the internal state of the `MaybePersistedNode` to `Allocated`,
    /// indicating that the node has been allocated on disk but is still in memory.
    ///
    /// This is done under a `Mutex` lock.
    ///
    /// # Arguments
    ///
    /// * `addr` - The `LinearAddress` where the node has been allocated on disk
    pub fn allocate_at(&self, addr: LinearAddress) {
        let mut guard = self.0.lock();
        let node = {
            match &*guard {
                MaybePersisted::Unpersisted(node) | MaybePersisted::Allocated(_, node) => {
                    node.clone()
                }
                MaybePersisted::Persisted(_) => {
                    unreachable!("Cannot allocate a node that is already persisted on disk");
                }
            }
        };
        *guard = MaybePersisted::Allocated(addr, node);
    }

    /// Returns the address and shared node if this node is in the Allocated state.
    ///
    /// # Returns
    ///
    /// Returns `Some((LinearAddress, SharedNode))` if the node is in the Allocated state,
    /// otherwise `None`.
    #[must_use]
    pub fn allocated_info(&self) -> Option<(LinearAddress, SharedNode)> {
        match &*self.0.lock() {
            MaybePersisted::Allocated(addr, node) => Some((*addr, node.clone())),
            _ => None,
        }
    }
}

/// Display the `MaybePersistedNode` as a string.
///
/// This is used in the dump utility.
///
/// We render these:
///   For disk addresses, just the address
///   For shared nodes, the address of the [`SharedNode`] object, prefixed with a 'M'
///
/// If instead you want the node itself, use [`MaybePersistedNode::as_shared_node`] first.
impl Display for MaybePersistedNode {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self.0.try_lock() {
            Some(guard) => match &*guard {
                MaybePersisted::Unpersisted(node) => write!(f, "M{:p}", (*node).as_ptr()),
                MaybePersisted::Allocated(addr, node) => {
                    write!(f, "A{:p}@{addr}", (*node).as_ptr())
                }
                MaybePersisted::Persisted(addr) => write!(f, "{addr}"),
            },
            None => write!(f, "<locked>"),
        }
    }
}

/// The internal state of a `MaybePersistedNode`.
///
/// This enum represents the three possible states of a `MaybePersisted`:
/// - `Unpersisted(SharedNode)`: The node is currently in memory
/// - `Allocated(LinearAddress, SharedNode)`: The node is allocated on disk but being flushed to disk
/// - `Persisted(LinearAddress)`: The node is currently on disk at the specified address
#[derive(Debug, PartialEq, Eq)]
enum MaybePersisted {
    Unpersisted(SharedNode),
    Allocated(LinearAddress, SharedNode),
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
        let store = NodeStore::new_empty_committed(mem_store);
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

        let addr = LinearAddress::new(addr.get()).unwrap();
        maybe_persisted_node.persist_at(addr);
        assert!(maybe_persisted_node.as_shared_node(&store).is_err());
        assert_eq!(Some(addr), Option::from(&maybe_persisted_node));
        Ok(())
    }

    #[test]
    fn test_from_linear_address() {
        let addr: LinearAddress = nonzero!(1024u64).into();
        let maybe_persisted_node = MaybePersistedNode::from(addr);
        assert_eq!(Some(addr), Option::from(&maybe_persisted_node));
    }

    #[test]
    fn test_clone_shares_underlying_shared_node() -> Result<(), FileIoError> {
        let mem_store = MemStore::new(vec![]).into();
        let store = NodeStore::new_empty_committed(mem_store);
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
        let addr = nonzero!(1024u64).into();
        original.persist_at(addr);

        // Both original and clone should now be persisted since they share the same
        // mutex-protected pointer
        assert!(original.as_shared_node(&store).is_err());
        assert!(cloned.as_shared_node(&store).is_err());
        assert_eq!(Some(addr), Option::from(&original));
        assert_eq!(Some(addr), Option::from(&cloned));

        // 'node' is no longer referenced by anyone but our local copy,
        // so the count should be 1
        assert_eq!(triomphe::Arc::strong_count(&node), 1);

        Ok(())
    }

    #[test]
    fn test_allocated_info() {
        let node = SharedNode::new(Node::Leaf(LeafNode {
            partial_path: Path::new(),
            value: vec![123].into(),
        }));

        let maybe_persisted = MaybePersistedNode::from(node.clone());

        // Initially unpersisted, so allocated_info should return None
        assert!(maybe_persisted.allocated_info().is_none());

        // Allocate the node
        let addr = LinearAddress::new(2048).unwrap();
        maybe_persisted.allocate_at(addr);

        // Now allocated_info should return Some
        let (retrieved_addr, retrieved_node) = maybe_persisted.allocated_info().unwrap();
        assert_eq!(retrieved_addr, addr);
        assert_eq!(retrieved_node, node);

        // Persist the node
        maybe_persisted.persist_at(addr);

        // After persisting, allocated_info should return None again
        assert!(maybe_persisted.allocated_info().is_none());
    }
}
