// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod range_set;
use range_set::LinearAddressRangeSet;

use crate::logger::warn;
use crate::nodestore::alloc::{AREA_SIZES, AreaIndex, FreeAreaWithMetadata};
use crate::nodestore::is_aligned;
use crate::{
    CheckerError, Committed, HashedNodeReader, LinearAddress, Node, NodeReader, NodeStore,
    StoredAreaParent, TrieNodeParent, WritableStorage,
};

use std::cmp::Ordering;
use std::ops::Range;

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
            self.check_area_aligned(
                root_address,
                StoredAreaParent::TrieNode(TrieNodeParent::Root),
            )?;
            self.visit_trie(root_address, &mut visited)?;
        }

        // 3. check the free list - this can happen in parallel with the trie traversal
        self.visit_freelist(&mut visited)?;

        // 4. check missed areas - what are the spaces between trie nodes and free lists we have traversed?
        let leaked_ranges = visited.complement();
        if !leaked_ranges.is_empty() {
            warn!("Found leaked ranges: {leaked_ranges}");
        }
        let _leaked_areas = self.split_all_leaked_ranges(leaked_ranges);
        // TODO: add leaked areas to the free list

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
            for (child_idx, address) in branch.children_addresses() {
                self.check_area_aligned(
                    address,
                    StoredAreaParent::TrieNode(TrieNodeParent::Parent(
                        subtree_root_address,
                        child_idx,
                    )),
                )?;
                self.visit_trie(address, visited)?;
            }
        }

        Ok(())
    }

    /// Traverse all the free areas in the freelist
    fn visit_freelist(&self, visited: &mut LinearAddressRangeSet) -> Result<(), CheckerError> {
        let mut free_list_iter = self.free_list_iter(0);
        while let Some(free_area) = free_list_iter.next_with_metadata() {
            let FreeAreaWithMetadata {
                addr,
                area_index,
                free_list_id,
                parent,
            } = free_area?;
            self.check_area_aligned(addr, StoredAreaParent::FreeList(parent))?;
            let area_size = Self::size_from_area_index(area_index);
            if free_list_id != area_index {
                return Err(CheckerError::FreelistAreaSizeMismatch {
                    address: addr,
                    size: area_size,
                    actual_free_list: free_list_id,
                    expected_free_list: area_index,
                });
            }
            visited.insert_area(addr, area_size)?;
        }
        Ok(())
    }

    const fn check_area_aligned(
        &self,
        address: LinearAddress,
        parent_ptr: StoredAreaParent,
    ) -> Result<(), CheckerError> {
        if !is_aligned(address) {
            return Err(CheckerError::AreaMisaligned {
                address,
                parent_ptr,
            });
        }
        Ok(())
    }

    /// Wrapper around `split_into_leaked_areas` that iterates over a collection of ranges.
    fn split_all_leaked_ranges(
        &self,
        leaked_ranges: impl IntoIterator<Item = Range<LinearAddress>>,
    ) -> impl Iterator<Item = (LinearAddress, AreaIndex)> {
        leaked_ranges
            .into_iter()
            .flat_map(|range| self.split_range_into_leaked_areas(range))
    }

    /// Split a range of addresses into leaked areas that can be stored in the free list.
    /// We assume that all space within `leaked_range` are leaked and the stored areas are contiguous.
    /// Returns error if the last leaked area extends beyond the end of the range.
    fn split_range_into_leaked_areas(
        &self,
        leaked_range: Range<LinearAddress>,
    ) -> Vec<(LinearAddress, AreaIndex)> {
        let mut leaked = Vec::new();
        let mut current_addr = leaked_range.start;

        // First attempt to read the valid stored areas from the leaked range
        loop {
            let (area_index, area_size) = match self.read_leaked_area(current_addr) {
                Ok(area_index_and_size) => area_index_and_size,
                Err(e) => {
                    warn!("Error reading stored area at {current_addr}: {e}");
                    break;
                }
            };

            let next_addr = current_addr
                .checked_add(area_size)
                .expect("address overflow is impossible");
            match next_addr.cmp(&leaked_range.end) {
                Ordering::Equal => {
                    // we have reached the end of the leaked area, done
                    leaked.push((current_addr, area_index));
                    return leaked;
                }
                Ordering::Greater => {
                    // the last area extends beyond the leaked area - this means the leaked area overlaps with an area we have visited
                    warn!(
                        "Leaked area extends beyond {leaked_range:?}: {current_addr} -> {next_addr}"
                    );
                    break;
                }
                Ordering::Less => {
                    // continue to the next area
                    leaked.push((current_addr, area_index));
                    current_addr = next_addr;
                }
            }
        }

        // We encountered an error, split the rest of the leaked range into areas using heuristics
        // The heuristic is to split the leaked range into areas of the largest size possible - we assume `AREA_SIZE` is in ascending order
        for (area_index, area_size) in AREA_SIZES.iter().enumerate().rev() {
            loop {
                let next_addr = current_addr
                    .checked_add(*area_size)
                    .expect("address overflow is impossible");
                if next_addr <= leaked_range.end {
                    leaked.push((current_addr, area_index as AreaIndex));
                    current_addr = next_addr;
                } else {
                    break;
                }
            }
        }

        // we assume that all areas are aligned to `MIN_AREA_SIZE`, in which case leaked ranges can always be split into free areas perfectly
        debug_assert!(current_addr == leaked_range.end);
        leaked
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
        test_write_free_area, test_write_header, test_write_new_node, test_write_zeroed_area,
    };
    use crate::nodestore::alloc::{AREA_SIZES, FreeLists};
    use crate::{BranchNode, Child, HashType, LeafNode, NodeStore, Path};

    use nonzero_ext::nonzero;
    use std::collections::HashMap;

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

    #[test]
    // This test creates a linear set of free areas and free them.
    // When traversing it should break consecutive areas.
    fn split_correct_range_into_leaked_areas() {
        use rand::Rng;
        use rand::seq::IteratorRandom;

        let mut rng = crate::test_utils::seeded_rng();

        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        let num_areas = 10;

        // randomly insert areas into the nodestore
        let mut high_watermark = NodeStoreHeader::SIZE;
        let mut stored_areas = Vec::new();
        for _ in 0..num_areas {
            let (area_size_index, area_size) =
                AREA_SIZES.iter().enumerate().choose(&mut rng).unwrap();
            let area_addr = LinearAddress::new(high_watermark).unwrap();
            test_write_free_area(
                &nodestore,
                None,
                area_size_index as AreaIndex,
                high_watermark,
            );
            stored_areas.push((area_addr, area_size_index as AreaIndex, *area_size));
            high_watermark += *area_size;
        }
        test_write_header(&mut nodestore, high_watermark, None, FreeLists::default());

        // randomly pick some areas as leaked
        let mut leaked = Vec::new();
        let mut expected_free_areas = HashMap::new();
        let mut area_to_free = None;
        for (area_start, area_size_index, area_size) in &stored_areas {
            if rng.random::<f64>() < 0.5 {
                // we free this area
                expected_free_areas.insert(*area_start, *area_size_index);
                let area_to_free_start = area_to_free.map_or(*area_start, |(start, _)| start);
                let area_to_free_end = area_start.checked_add(*area_size).unwrap();
                area_to_free = Some((area_to_free_start, area_to_free_end));
            } else {
                // we are not freeing this area, free the aggregated areas before this one
                if let Some((start, end)) = area_to_free {
                    leaked.push(start..end);
                    area_to_free = None;
                }
            }
        }
        if let Some((start, end)) = area_to_free {
            leaked.push(start..end);
        }

        // check the leaked areas
        let leaked_areas: HashMap<_, _> = nodestore.split_all_leaked_ranges(leaked).collect();

        // assert that all leaked areas end up on the free list
        assert_eq!(leaked_areas, expected_free_areas);
    }

    #[test]
    // This test creates a linear set of free areas and free them.
    // When traversing it should break consecutive areas.
    fn split_range_of_zeros_into_leaked_areas() {
        let memstore = MemStore::new(vec![]);
        let nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        let expected_leaked_area_indices = vec![8u8, 7, 4, 2, 0];
        let expected_leaked_area_sizes = expected_leaked_area_indices
            .iter()
            .map(|i| AREA_SIZES[*i as usize])
            .collect::<Vec<_>>();
        assert_eq!(expected_leaked_area_sizes, vec![1024, 768, 128, 64, 16]);
        let expected_offsets = expected_leaked_area_sizes
            .iter()
            .scan(0, |acc, i| {
                let offset = *acc;
                *acc += i;
                LinearAddress::new(NodeStoreHeader::SIZE + offset)
            })
            .collect::<Vec<_>>();

        // write an zeroed area
        let leaked_range_size = expected_leaked_area_sizes.iter().sum();
        test_write_zeroed_area(&nodestore, leaked_range_size, NodeStoreHeader::SIZE);

        // check the leaked areas
        let leaked_range = nonzero!(NodeStoreHeader::SIZE)
            ..LinearAddress::new(
                NodeStoreHeader::SIZE
                    .checked_add(leaked_range_size)
                    .unwrap(),
            )
            .unwrap();
        let (leaked_areas_offsets, leaked_area_size_indices): (Vec<LinearAddress>, Vec<AreaIndex>) =
            nodestore
                .split_range_into_leaked_areas(leaked_range)
                .into_iter()
                .unzip();

        // assert that all leaked areas end up on the free list
        assert_eq!(leaked_areas_offsets, expected_offsets);
        assert_eq!(leaked_area_size_indices, expected_leaked_area_indices);
    }

    #[test]
    // With both valid and invalid areas in the range, return the valid areas until reaching one invalid area, then use heuristics to split the rest of the range.
    fn split_range_into_leaked_areas_test() {
        let memstore = MemStore::new(vec![]);
        let nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        // write two free areas
        let mut high_watermark = NodeStoreHeader::SIZE;
        test_write_free_area(&nodestore, None, 8, high_watermark); // 1024
        high_watermark += AREA_SIZES[8];
        test_write_free_area(&nodestore, None, 7, high_watermark); // 768
        high_watermark += AREA_SIZES[7];
        // write an zeroed area
        test_write_zeroed_area(&nodestore, 768, high_watermark);
        high_watermark += 768;
        // write another free area
        test_write_free_area(&nodestore, None, 8, high_watermark); // 1024
        high_watermark += AREA_SIZES[8];

        let expected_indices = vec![8, 7, 8, 7];
        let expected_sizes = expected_indices
            .iter()
            .map(|i| AREA_SIZES[*i as usize])
            .collect::<Vec<_>>();
        let expected_offsets = expected_sizes
            .iter()
            .scan(0, |acc, i| {
                let offset = *acc;
                *acc += i;
                LinearAddress::new(NodeStoreHeader::SIZE + offset)
            })
            .collect::<Vec<_>>();

        // check the leaked areas
        let leaked_range =
            nonzero!(NodeStoreHeader::SIZE)..LinearAddress::new(high_watermark).unwrap();
        let (leaked_areas_offsets, leaked_area_size_indices): (Vec<LinearAddress>, Vec<AreaIndex>) =
            nodestore
                .split_range_into_leaked_areas(leaked_range)
                .into_iter()
                .unzip();

        assert_eq!(leaked_areas_offsets, expected_offsets);
        assert_eq!(leaked_area_size_indices, expected_indices);
    }
}
