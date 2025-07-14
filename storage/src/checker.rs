// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::logger::warn;
use crate::range_set::LinearAddressRangeSet;
use crate::{
    CheckerError, Committed, HashedNodeReader, LinearAddress, Node, NodeReader, NodeStore,
    WritableStorage,
};

/// [`NodeStore`] checker
// TODO: S needs to be writeable if we ask checker to fix the issues
impl<S: WritableStorage> NodeStore<Committed, S> {
    /// Go through the filebacked storage and check for any inconsistencies. It proceeds in the following steps:
    /// 1. Check the header
    /// 2. traverse the trie and check the nodes
    /// 3. check the free list
    /// 4. check missed areas - what are the spaces between trie nodes and free lists we have traversed?
    /// # Errors
    /// Returns a [`CheckerError`] if the database is inconsistent.
    /// # Panics
    /// Panics if the header has too many free lists, which can never happen since freelists have a fixed size.
    // TODO: report all errors, not just the first one
    // TODO: add merkle hash checks as well
    pub fn check(&self) -> Result<(), CheckerError> {
        // 1. Check the header
        let db_size = self.size();
        let file_size = self.physical_size()?;
        if db_size < file_size {
            return Err(CheckerError::InvalidDBSize {
                db_size,
                description: format!(
                    "db size should not be smaller than the file size ({file_size})"
                ),
            });
        }

        let mut visited = LinearAddressRangeSet::new(db_size)?;

        // 2. traverse the trie and check the nodes
        if let Some(root_address) = self.root_address() {
            // the database is not empty, traverse the trie
            self.visit_trie(root_address, &mut visited)?;
        }

        // 3. check the free list - this can happen in parallel with the trie traversal
        self.visit_freelist(&mut visited)?;

        // 4. check missed areas - what are the spaces between trie nodes and free lists we have traversed?
        let leaked_ranges = visited.complement();
        if !leaked_ranges.is_empty() {
            warn!("Found leaked ranges: {leaked_ranges}");
        }

        Ok(())
    }

    /// Recursively traverse the trie from the given root address.
    fn visit_trie(
        &self,
        subtree_root_address: LinearAddress,
        visited: &mut LinearAddressRangeSet,
    ) -> Result<(), CheckerError> {
        let (_, area_size) = self.area_index_and_size(subtree_root_address)?;
        visited.insert_area(subtree_root_address, area_size)?;

        if let Node::Branch(branch) = self.read_node(subtree_root_address)?.as_ref() {
            // this is an internal node, traverse the children
            for (_, address) in branch.children_addresses() {
                self.visit_trie(address, visited)?;
            }
        }

        Ok(())
    }

    /// Traverse all the free areas in the freelist
    fn visit_freelist(&self, visited: &mut LinearAddressRangeSet) -> Result<(), CheckerError> {
        for free_area in self.free_list_iter_inner(0) {
            let (addr, stored_area_index, free_list_id) = free_area?;
            let area_size = Self::size_from_area_index(stored_area_index);
            if free_list_id != stored_area_index {
                return Err(CheckerError::FreelistAreaSizeMismatch {
                    address: addr,
                    size: area_size,
                    actual_free_list: free_list_id,
                    expected_free_list: stored_area_index,
                });
            }
            visited.insert_area(addr, area_size)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod test {
    #![expect(clippy::unwrap_used)]
    #![expect(clippy::indexing_slicing)]

    use super::*;
    use crate::linear::memory::MemStore;
    use crate::nodestore::NodeStoreHeader;
    use crate::nodestore::alloc::test_utils::{
        test_write_free_area, test_write_header, test_write_new_node,
    };
    use crate::nodestore::alloc::{AREA_SIZES, FreeLists};
    use crate::{BranchNode, Child, HashType, LeafNode, NodeStore, Path};

    #[test]
    // This test creates a simple trie and checks that the checker traverses it correctly.
    // We use primitive calls here to do a low-level check.
    // TODO: add a high-level test in the firewood crate
    fn checker_traverse_correct_trie() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        // set up a basic trie:
        // -------------------------
        // |     |  X  |  X  | ... |    Root node
        // -------------------------
        //    |
        //    V
        // -------------------------
        // |  X  |     |  X  | ... |    Branch node
        // -------------------------
        //          |
        //          V
        // -------------------------
        // |   [0,1] -> [3,4,5]    |    Leaf node
        // -------------------------
        let mut high_watermark = NodeStoreHeader::SIZE;
        let leaf = Node::Leaf(LeafNode {
            partial_path: Path::from([0, 1]),
            value: Box::new([3, 4, 5]),
        });
        let leaf_addr = LinearAddress::new(high_watermark).unwrap();
        let leaf_area = test_write_new_node(&nodestore, &leaf, high_watermark);
        high_watermark += leaf_area;

        let mut branch_children: [Option<Child>; BranchNode::MAX_CHILDREN] = Default::default();
        branch_children[1] = Some(Child::AddressWithHash(leaf_addr, HashType::default()));
        let branch = Node::Branch(Box::new(BranchNode {
            partial_path: Path::from([0]),
            value: None,
            children: branch_children,
        }));
        let branch_addr = LinearAddress::new(high_watermark).unwrap();
        let branch_area = test_write_new_node(&nodestore, &branch, high_watermark);
        high_watermark += branch_area;

        let mut root_children: [Option<Child>; BranchNode::MAX_CHILDREN] = Default::default();
        root_children[0] = Some(Child::AddressWithHash(branch_addr, HashType::default()));
        let root = Node::Branch(Box::new(BranchNode {
            partial_path: Path::from([]),
            value: None,
            children: root_children,
        }));
        let root_addr = LinearAddress::new(high_watermark).unwrap();
        let root_area = test_write_new_node(&nodestore, &root, high_watermark);
        high_watermark += root_area;

        // write the header
        test_write_header(
            &mut nodestore,
            high_watermark,
            Some(root_addr),
            FreeLists::default(),
        );

        // verify that all of the space is accounted for - since there is no free area
        let mut visited = LinearAddressRangeSet::new(high_watermark).unwrap();
        nodestore.visit_trie(root_addr, &mut visited).unwrap();
        let complement = visited.complement();
        assert_eq!(complement.into_iter().collect::<Vec<_>>(), vec![]);
    }

    #[test]
    fn traverse_correct_freelist() {
        use rand::Rng;

        let mut rng = crate::test_utils::seeded_rng();

        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        // write free areas
        let mut high_watermark = NodeStoreHeader::SIZE;
        let mut freelist = FreeLists::default();
        for (area_index, area_size) in AREA_SIZES.iter().enumerate() {
            let mut next_free_block = None;
            let num_free_areas = rng.random_range(0..4);
            for _ in 0..num_free_areas {
                test_write_free_area(
                    &nodestore,
                    next_free_block,
                    area_index as u8,
                    high_watermark,
                );
                next_free_block = Some(LinearAddress::new(high_watermark).unwrap());
                high_watermark += area_size;
            }

            freelist[area_index] = next_free_block;
        }

        // write header
        test_write_header(&mut nodestore, high_watermark, None, freelist);

        // test that the we traversed all the free areas
        let mut visited = LinearAddressRangeSet::new(high_watermark).unwrap();
        nodestore.visit_freelist(&mut visited).unwrap();
        let complement = visited.complement();
        assert_eq!(complement.into_iter().collect::<Vec<_>>(), vec![]);
    }
}
