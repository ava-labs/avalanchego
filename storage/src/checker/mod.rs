// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod range_set;
pub(crate) use range_set::LinearAddressRangeSet;

use crate::logger::warn;
use crate::nodestore::alloc::{
    AREA_SIZES, AreaIndex, FreeAreaWithMetadata, area_size_to_index, size_from_area_index,
};
use crate::{
    CheckerError, Committed, HashType, HashedNodeReader, IntoHashType, LinearAddress, Node,
    NodeStore, Path, RootReader, StoredAreaParent, TrieNodeParent, WritableStorage,
};

#[cfg(not(feature = "ethhash"))]
use crate::hashednode::hash_node;

use std::cmp::Ordering;
use std::collections::HashMap;
use std::ops::Range;

use indicatif::ProgressBar;

const OS_PAGE_SIZE: u64 = 4096;

#[inline]
// return u64 since the start address may be 0
const fn page_start(addr: LinearAddress) -> u64 {
    addr.get() & !(OS_PAGE_SIZE - 1)
}

#[cfg(feature = "ethhash")]
fn is_valid_key(key: &Path) -> bool {
    const VALID_ETH_KEY_SIZES: [usize; 2] = [64, 128]; // in number of nibbles - two nibbles make a byte
    VALID_ETH_KEY_SIZES.contains(&key.0.len())
}

#[cfg(not(feature = "ethhash"))]
fn is_valid_key(key: &Path) -> bool {
    key.0.len() % 2 == 0
}

/// Options for the checker
#[derive(Debug)]
pub struct CheckOpt {
    /// Whether to check the hash of the nodes
    pub hash_check: bool,
    /// Optional progress bar to show the checker progress
    pub progress_bar: Option<ProgressBar>,
}

#[derive(Debug)]
/// Report of the checker results.
pub struct CheckerReport {
    /// The high watermark of the database
    pub high_watermark: u64,
    /// The physical number of bytes in the database returned through `stat`
    pub physical_bytes: u64,
    /// Statistics about the trie
    pub trie_stats: TrieStats,
    /// Statistics about the free list
    pub free_list_stats: FreeListsStats,
}

#[derive(Debug, Default, PartialEq)]
/// Statistics about the trie
pub struct TrieStats {
    /// The total number of bytes of compressed trie nodes
    pub trie_bytes: u64,
    /// The number of key-value pairs stored in the trie
    pub kv_count: u64,
    /// The total number of bytes of for key-value pairs stored in the trie
    pub kv_bytes: u64,
    /// Branching factor distribution of each branch node
    pub branching_factors: HashMap<usize, u64>,
    /// Depth distribution of each leaf node
    pub depths: HashMap<usize, u64>,
    /// The distribution of area sizes in the trie
    pub area_counts: HashMap<u64, u64>,
    /// The stored areas whose content can fit into a smaller area
    pub low_occupancy_area_count: u64,
    /// The number of stored areas that span multiple pages
    pub multi_page_area_count: u64,
}

#[derive(Debug, Default, PartialEq)]
/// Statistics about the free list
pub struct FreeListsStats {
    /// The distribution of area sizes in the free lists
    pub area_counts: HashMap<u64, u64>,
    /// The number of stored areas that span multiple pages
    pub multi_page_area_count: u64,
}

struct SubTrieMetadata {
    root_address: LinearAddress,
    root_hash: HashType,
    parent: TrieNodeParent,
    depth: usize,
    path_prefix: Path,
    #[cfg(feature = "ethhash")]
    has_peers: bool,
}

/// [`NodeStore`] checker
// TODO: S needs to be writeable if we ask checker to fix the issues
#[expect(clippy::result_large_err)]
impl<S: WritableStorage> NodeStore<Committed, S> {
    /// Go through the filebacked storage and check for any inconsistencies. It proceeds in the following steps:
    /// 1. Check the header
    /// 2. traverse the trie and check the nodes
    /// 3. check the free list
    /// 4. check leaked areas - what are the spaces between trie nodes and free lists we have traversed?
    /// # Errors
    /// Returns a [`CheckerError`] if the database is inconsistent.
    // TODO: report all errors, not just the first one
    pub fn check(&self, opt: CheckOpt) -> Result<CheckerReport, CheckerError> {
        // 1. Check the header
        let db_size = self.size();
        let physical_bytes = self.physical_size()?;

        let mut visited = LinearAddressRangeSet::new(db_size)?;

        // 2. traverse the trie and check the nodes
        if let Some(progress_bar) = &opt.progress_bar {
            progress_bar.set_length(db_size);
            progress_bar.set_message("Traversing the trie...");
        }
        let trie_stats = if let (Some(root), Some(root_hash)) =
            (self.root_as_maybe_persisted_node(), self.root_hash())
        {
            // the database is not empty, and has a physical address, so traverse the trie
            if let Some(root_address) = root.as_linear_address() {
                self.check_area_aligned(
                    root_address,
                    StoredAreaParent::TrieNode(TrieNodeParent::Root),
                )?;
                self.visit_trie(
                    root_address,
                    root_hash.into_hash_type(),
                    &mut visited,
                    opt.progress_bar.as_ref(),
                    opt.hash_check,
                )?
            } else {
                return Err(CheckerError::UnpersistedRoot);
            }
        } else {
            TrieStats::default()
        };

        // 3. check the free list - this can happen in parallel with the trie traversal
        if let Some(progress_bar) = &opt.progress_bar {
            progress_bar.set_message("Traversing free lists...");
        }
        let free_list_stats = self.visit_freelist(&mut visited, opt.progress_bar.as_ref())?;

        // 4. check leaked areas - what are the spaces between trie nodes and free lists we have traversed?
        if let Some(progress_bar) = &opt.progress_bar {
            progress_bar.set_message("Checking leaked areas...");
        }
        let leaked_ranges = visited.complement();
        if !leaked_ranges.is_empty() {
            warn!("Found leaked ranges: {leaked_ranges}");
        }
        let _leaked_areas = self.split_all_leaked_ranges(leaked_ranges, opt.progress_bar.as_ref());
        // TODO: add leaked areas to the free list

        Ok(CheckerReport {
            high_watermark: db_size,
            physical_bytes,
            trie_stats,
            free_list_stats,
        })
    }

    fn visit_trie(
        &self,
        root_address: LinearAddress,
        root_hash: HashType,
        visited: &mut LinearAddressRangeSet,
        progress_bar: Option<&ProgressBar>,
        hash_check: bool,
    ) -> Result<TrieStats, CheckerError> {
        let trie = SubTrieMetadata {
            root_address,
            root_hash,
            parent: TrieNodeParent::Root,
            depth: 0,
            path_prefix: Path::new(),
            #[cfg(feature = "ethhash")]
            has_peers: false,
        };
        let mut trie_stats = TrieStats::default();
        self.visit_trie_helper(trie, visited, &mut trie_stats, progress_bar, hash_check)?;
        Ok(trie_stats)
    }

    /// Recursively traverse the trie from the given root node.
    #[expect(clippy::too_many_lines)]
    fn visit_trie_helper(
        &self,
        subtrie: SubTrieMetadata,
        visited: &mut LinearAddressRangeSet,
        trie_stats: &mut TrieStats,
        progress_bar: Option<&ProgressBar>,
        hash_check: bool,
    ) -> Result<(), CheckerError> {
        let SubTrieMetadata {
            root_address: subtrie_root_address,
            root_hash: subtrie_root_hash,
            parent,
            depth,
            path_prefix,
            #[cfg(feature = "ethhash")]
            has_peers,
        } = subtrie;

        // check that address is aligned
        self.check_area_aligned(subtrie_root_address, StoredAreaParent::TrieNode(parent))?;

        // check that the area is within bounds and does not intersect with other areas
        let (area_index, area_size) = self.area_index_and_size(subtrie_root_address)?;
        visited.insert_area(
            subtrie_root_address,
            area_size,
            StoredAreaParent::TrieNode(parent),
        )?;

        // update the area count
        let area_count = trie_stats.area_counts.entry(area_size).or_insert(0);
        *area_count = area_count.saturating_add(1);

        // read the node from the disk - we avoid cache since we will never visit the same node twice
        let (node, node_bytes) = self.read_node_with_num_bytes_from_disk(subtrie_root_address)?;
        {
            // collect the trie bytes
            trie_stats.trie_bytes = trie_stats.trie_bytes.saturating_add(node_bytes);
            // collect low occupancy area count
            let smallest_area_index = area_size_to_index(node_bytes).map_err(|e| {
                self.file_io_error(
                    e,
                    subtrie_root_address.get(),
                    Some("area_size_to_index".to_string()),
                )
            })?;
            if smallest_area_index < area_index {
                trie_stats.low_occupancy_area_count =
                    trie_stats.low_occupancy_area_count.saturating_add(1);
            }
            // collect the multi-page area count
            if page_start(subtrie_root_address)
                != page_start(
                    subtrie_root_address
                        .advance(area_size)
                        .expect("impossible since we checked in visited.insert_area()"),
                )
            {
                trie_stats.multi_page_area_count =
                    trie_stats.multi_page_area_count.saturating_add(1);
            }
        }

        let mut current_path_prefix = path_prefix.clone();
        current_path_prefix.0.extend_from_slice(node.partial_path());
        if node.value().is_some() && !is_valid_key(&current_path_prefix) {
            return Err(CheckerError::InvalidKey {
                key: current_path_prefix,
                address: subtrie_root_address,
                parent,
            });
        }

        match node.as_ref() {
            Node::Branch(branch) => {
                let num_children = branch.children_iter().count();
                {
                    // collect the branching factor distribution
                    let branching_factor_count = trie_stats
                        .branching_factors
                        .entry(num_children)
                        .or_insert(0);
                    *branching_factor_count = branching_factor_count.saturating_add(1);
                }

                // this is an internal node, traverse the children
                for (nibble, (address, hash)) in branch.children_iter() {
                    let parent = TrieNodeParent::Parent(subtrie_root_address, nibble);
                    let mut child_path_prefix = current_path_prefix.clone();
                    child_path_prefix.0.push(nibble as u8);
                    let child_subtrie = SubTrieMetadata {
                        root_address: address,
                        root_hash: hash.clone(),
                        parent,
                        depth: depth.saturating_add(1),
                        path_prefix: child_path_prefix,
                        #[cfg(feature = "ethhash")]
                        has_peers: num_children != 1,
                    };
                    self.visit_trie_helper(
                        child_subtrie,
                        visited,
                        trie_stats,
                        progress_bar,
                        hash_check,
                    )?;
                }
            }
            Node::Leaf(leaf) => {
                // collect the depth distribution
                let depth_count = trie_stats.depths.entry(depth).or_insert(0);
                *depth_count = depth_count.saturating_add(1);
                // collect kv count
                trie_stats.kv_count = trie_stats.kv_count.saturating_add(1);
                // collect kv pair bytes - this is the minimum number of bytes needed to store the data
                let key_bytes = current_path_prefix.0.len().div_ceil(2);
                let value_bytes = leaf.value.len();
                trie_stats.kv_bytes = trie_stats
                    .kv_bytes
                    .saturating_add(key_bytes as u64)
                    .saturating_add(value_bytes as u64);
            }
        }

        // hash check - at this point all children hashes have been verified
        if hash_check {
            #[cfg(feature = "ethhash")]
            let hash = Self::compute_node_ethhash(&node, &path_prefix, has_peers);
            #[cfg(not(feature = "ethhash"))]
            let hash = hash_node(&node, &path_prefix);
            if hash != subtrie_root_hash {
                let mut path = path_prefix.clone();
                path.0.extend_from_slice(node.partial_path());
                return Err(CheckerError::HashMismatch {
                    path,
                    address: subtrie_root_address,
                    parent,
                    parent_stored_hash: subtrie_root_hash,
                    computed_hash: hash,
                });
            }
        }

        update_progress_bar(progress_bar, visited);

        Ok(())
    }

    /// Traverse all the free areas in the freelist
    fn visit_freelist(
        &self,
        visited: &mut LinearAddressRangeSet,
        progress_bar: Option<&ProgressBar>,
    ) -> Result<FreeListsStats, CheckerError> {
        let mut area_counts: HashMap<u64, u64> = HashMap::new();
        let mut multi_page_area_count = 0u64;

        let mut free_list_iter = self.free_list_iter(0);
        while let Some(free_area) = free_list_iter.next_with_metadata() {
            let FreeAreaWithMetadata {
                addr,
                area_index,
                free_list_id,
                parent,
            } = free_area?;
            self.check_area_aligned(addr, StoredAreaParent::FreeList(parent))?;
            let area_size = size_from_area_index(area_index);
            if free_list_id != area_index {
                return Err(CheckerError::FreelistAreaSizeMismatch {
                    address: addr,
                    size: area_size,
                    actual_free_list: free_list_id,
                    expected_free_list: area_index,
                    parent,
                });
            }
            visited.insert_area(addr, area_size, StoredAreaParent::FreeList(parent))?;
            {
                // collect the free lists area distribution
                let area_count = area_counts.entry(area_size).or_insert(0);
                *area_count = area_count.saturating_add(1);
                // collect the multi-page area count
                if page_start(addr)
                    != page_start(
                        addr.advance(area_size)
                            .expect("impossible since we checked in visited.insert_area()"),
                    )
                {
                    multi_page_area_count = multi_page_area_count.saturating_add(1);
                }
            }
            update_progress_bar(progress_bar, visited);
        }
        Ok(FreeListsStats {
            area_counts,
            multi_page_area_count,
        })
    }

    const fn check_area_aligned(
        &self,
        address: LinearAddress,
        parent: StoredAreaParent,
    ) -> Result<(), CheckerError> {
        if !address.is_aligned() {
            return Err(CheckerError::AreaMisaligned { address, parent });
        }
        Ok(())
    }

    /// Wrapper around `split_into_leaked_areas` that iterates over a collection of ranges.
    fn split_all_leaked_ranges(
        &self,
        leaked_ranges: impl IntoIterator<Item = Range<LinearAddress>>,
        progress_bar: Option<&ProgressBar>,
    ) -> impl Iterator<Item = (LinearAddress, AreaIndex)> {
        leaked_ranges
            .into_iter()
            .flat_map(move |range| self.split_range_into_leaked_areas(range, progress_bar))
    }

    /// Split a range of addresses into leaked areas that can be stored in the free list.
    /// We assume that all space within `leaked_range` are leaked and the stored areas are contiguous.
    /// Returns error if the last leaked area extends beyond the end of the range.
    fn split_range_into_leaked_areas(
        &self,
        leaked_range: Range<LinearAddress>,
        progress_bar: Option<&ProgressBar>,
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
                .advance(area_size)
                .expect("address overflow is impossible");
            match next_addr.cmp(&leaked_range.end) {
                Ordering::Equal => {
                    // we have reached the end of the leaked area, done
                    leaked.push((current_addr, area_index));
                    if let Some(progress_bar) = progress_bar {
                        progress_bar.inc(area_size);
                    }
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
                    if let Some(progress_bar) = progress_bar {
                        progress_bar.inc(area_size);
                    }
                    current_addr = next_addr;
                }
            }
        }

        // We encountered an error, split the rest of the leaked range into areas using heuristics
        // The heuristic is to split the leaked range into areas of the largest size possible - we assume `AREA_SIZE` is in ascending order
        for (area_index, area_size) in AREA_SIZES.iter().enumerate().rev() {
            loop {
                let next_addr = current_addr
                    .advance(*area_size)
                    .expect("address overflow is impossible");
                if next_addr <= leaked_range.end {
                    leaked.push((current_addr, area_index as AreaIndex));
                    if let Some(progress_bar) = progress_bar {
                        progress_bar.inc(*area_size);
                    }
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

fn update_progress_bar(progress_bar: Option<&ProgressBar>, range_set: &LinearAddressRangeSet) {
    if let Some(progress_bar) = progress_bar {
        progress_bar.set_position(
            range_set
                .bytes_in_set()
                .checked_add(crate::nodestore::NodeStoreHeader::SIZE)
                .expect("overflow can only happen if max_addr >= U64_MAX + NODE_STORE_START_ADDR"),
        );
    }
}

#[cfg(test)]
mod test {
    #![expect(clippy::unwrap_used)]
    #![expect(clippy::indexing_slicing)]

    use nonzero_ext::nonzero;

    use super::*;
    use crate::linear::memory::MemStore;
    use crate::nodestore::NodeStoreHeader;
    use crate::nodestore::alloc::test_utils::{
        test_write_free_area, test_write_header, test_write_new_node, test_write_zeroed_area,
    };
    use crate::nodestore::alloc::{AREA_SIZES, FreeLists};
    use crate::{BranchNode, Child, LeafNode, NodeStore, Path, hash_node};

    #[derive(Debug)]
    struct TestTrie {
        nodes: Vec<(Node, LinearAddress)>,
        high_watermark: u64,
        root_address: LinearAddress,
        root_hash: HashType,
        stats: TrieStats,
    }

    /// Generate a test trie with the following structure:
    ///
    #[cfg_attr(doc, aquamarine)]
    /// ```mermaid
    /// graph TD
    ///     Root["Root Node<br/>partial_path: [2]<br/>children: [0] -> Branch"]
    ///     Branch["Branch Node<br/>partial_path: [3]<br/>path: 0x203<br/>children: [1] -> Leaf"]
    ///     Leaf["Leaf Node<br/>partial_path: [4, 5]<br/>path: 0x2031454545...45 (32 bytes)<br/>value: [6, 7, 8]"]
    ///
    ///     Root -->|"nibble 0"| Branch
    ///     Branch -->|"nibble 1"| Leaf
    /// ```
    #[expect(clippy::arithmetic_side_effects)]
    fn gen_test_trie(nodestore: &mut NodeStore<Committed, MemStore>) -> TestTrie {
        let mut high_watermark = NodeStoreHeader::SIZE;
        let mut total_bytes_written = 0;
        let mut area_counts: HashMap<u64, u64> = HashMap::new();
        let leaf = Node::Leaf(LeafNode {
            partial_path: Path::from_nibbles_iterator(std::iter::repeat_n([4, 5], 30).flatten()),
            value: Box::new([6, 7, 8]),
        });
        let leaf_addr = LinearAddress::new(high_watermark).unwrap();
        let leaf_hash = hash_node(&leaf, &Path::from([2, 0, 3, 1]));
        let (bytes_written, stored_area_size) =
            test_write_new_node(nodestore, &leaf, high_watermark);
        high_watermark += stored_area_size;
        total_bytes_written += bytes_written;
        let area_count = area_counts.entry(stored_area_size).or_insert(0);
        *area_count = area_count.saturating_add(1);

        let mut branch_children = BranchNode::empty_children();
        branch_children[1] = Some(Child::AddressWithHash(leaf_addr, leaf_hash));
        let branch = Node::Branch(Box::new(BranchNode {
            partial_path: Path::from([3]),
            value: None,
            children: branch_children,
        }));
        let branch_addr = LinearAddress::new(high_watermark).unwrap();
        let branch_hash = hash_node(&branch, &Path::from([2, 0]));
        let (bytes_written, stored_area_size) =
            test_write_new_node(nodestore, &branch, high_watermark);
        high_watermark += stored_area_size;
        total_bytes_written += bytes_written;
        let area_count = area_counts.entry(stored_area_size).or_insert(0);
        *area_count = area_count.saturating_add(1);

        let mut root_children = BranchNode::empty_children();
        root_children[0] = Some(Child::AddressWithHash(branch_addr, branch_hash));
        let root = Node::Branch(Box::new(BranchNode {
            partial_path: Path::from([2]),
            value: None,
            children: root_children,
        }));
        let root_addr = LinearAddress::new(high_watermark).unwrap();
        let root_hash = hash_node(&root, &Path::new());
        let (bytes_written, stored_area_size) =
            test_write_new_node(nodestore, &root, high_watermark);
        high_watermark += stored_area_size;
        total_bytes_written += bytes_written;
        let area_count = area_counts.entry(stored_area_size).or_insert(0);
        *area_count = area_count.saturating_add(1);

        // write the header
        test_write_header(
            nodestore,
            high_watermark,
            Some(root_addr),
            FreeLists::default(),
        );

        let trie_stats = TrieStats {
            trie_bytes: total_bytes_written,
            kv_count: 1,
            kv_bytes: 32 + 3, // 32 bytes for the key, 3 bytes for the value
            area_counts,
            branching_factors: HashMap::from([(1, 2)]),
            depths: HashMap::from([(2, 1)]),
            low_occupancy_area_count: 0,
            multi_page_area_count: 0,
        };

        TestTrie {
            nodes: vec![(leaf, leaf_addr), (branch, branch_addr), (root, root_addr)],
            high_watermark,
            root_address: root_addr,
            root_hash,
            stats: trie_stats,
        }
    }

    use std::collections::HashMap;

    #[test]
    // This test creates a simple trie and checks that the checker traverses it correctly.
    // We use primitive calls here to do a low-level check.
    fn checker_traverse_correct_trie() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        let test_trie = gen_test_trie(&mut nodestore);
        // let (_, high_watermark, (root_addr, root_hash)) = gen_test_trie(&mut nodestore);

        // verify that all of the space is accounted for - since there is no free area
        let mut visited = LinearAddressRangeSet::new(test_trie.high_watermark).unwrap();
        let stats = nodestore
            .visit_trie(
                test_trie.root_address,
                test_trie.root_hash,
                &mut visited,
                None,
                true,
            )
            .unwrap();
        let complement = visited.complement();
        assert_eq!(complement.into_iter().collect::<Vec<_>>(), vec![]);
        assert_eq!(stats, test_trie.stats);
    }

    #[test]
    // This test permutes the simple trie with a wrong hash and checks that the checker detects it.
    fn checker_traverse_trie_with_wrong_hash() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        let mut test_trie = gen_test_trie(&mut nodestore);

        // find the root node and replace the branch hash with an incorrect (default) hash
        let (root_node, root_addr) = test_trie
            .nodes
            .iter_mut()
            .find(|(node, _)| matches!(node, Node::Branch(b) if *b.partial_path.0 == [2]))
            .unwrap();

        let root_branch = root_node.as_branch_mut().unwrap();

        // Get the branch address and original hash from the root's first child
        let (branch_addr, computed_hash) = root_branch.children[0]
            .as_ref()
            .unwrap()
            .persist_info()
            .unwrap();
        let computed_hash = computed_hash.clone();
        root_branch.children[0] = Some(Child::AddressWithHash(branch_addr, HashType::default()));

        // Replace the branch hash in the root node with a wrong hash
        if let Node::Branch(root_branch) = root_node {
            root_branch.children[0] =
                Some(Child::AddressWithHash(branch_addr, HashType::default()));
        }
        test_write_new_node(&nodestore, root_node, root_addr.get());

        // run the checker and verify that it returns the HashMismatch error
        let mut visited = LinearAddressRangeSet::new(test_trie.high_watermark).unwrap();
        let err = nodestore
            .visit_trie(
                test_trie.root_address,
                test_trie.root_hash,
                &mut visited,
                None,
                true,
            )
            .unwrap_err();

        let expected_error = CheckerError::HashMismatch {
            address: branch_addr,
            path: Path::from([2, 0, 3]),
            parent: TrieNodeParent::Parent(*root_addr, 0),
            parent_stored_hash: HashType::default(),
            computed_hash,
        };
        assert_eq!(err, expected_error);
    }

    #[test]
    fn traverse_correct_freelist() {
        use rand::Rng;

        let mut rng = crate::test_utils::seeded_rng();

        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into()).unwrap();

        // write free areas
        let mut high_watermark = NodeStoreHeader::SIZE;
        let mut free_area_counts: HashMap<u64, u64> = HashMap::new();
        let mut multi_page_area_count = 0u64;
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
                let start_addr = LinearAddress::new(high_watermark).unwrap();
                let end_addr = start_addr.advance(*area_size).unwrap();
                if page_start(start_addr) != page_start(end_addr) {
                    multi_page_area_count = multi_page_area_count.saturating_add(1);
                }
                high_watermark += area_size;
            }
            freelist[area_index] = next_free_block;
            if num_free_areas > 0 {
                free_area_counts.insert(*area_size, num_free_areas);
            }
        }
        let expected_free_lists_stats = FreeListsStats {
            area_counts: free_area_counts,
            multi_page_area_count,
        };

        // write header
        test_write_header(&mut nodestore, high_watermark, None, freelist);

        // test that the we traversed all the free areas
        let mut visited = LinearAddressRangeSet::new(high_watermark).unwrap();
        let actual_free_lists_stats = nodestore.visit_freelist(&mut visited, None).unwrap();
        let complement = visited.complement();
        assert_eq!(complement.into_iter().collect::<Vec<_>>(), vec![]);
        assert_eq!(actual_free_lists_stats, expected_free_lists_stats);
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
                    leaked.push(start..end.into());
                    area_to_free = None;
                }
            }
        }
        if let Some((start, end)) = area_to_free {
            leaked.push(start..end.into());
        }

        // check the leaked areas
        let leaked_areas: HashMap<_, _> = nodestore.split_all_leaked_ranges(leaked, None).collect();

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
        let leaked_range = nonzero!(NodeStoreHeader::SIZE).into()
            ..LinearAddress::new(
                NodeStoreHeader::SIZE
                    .checked_add(leaked_range_size)
                    .unwrap(),
            )
            .unwrap();
        let (leaked_areas_offsets, leaked_area_size_indices): (Vec<LinearAddress>, Vec<AreaIndex>) =
            nodestore
                .split_range_into_leaked_areas(leaked_range, None)
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
            nonzero!(NodeStoreHeader::SIZE).into()..LinearAddress::new(high_watermark).unwrap();
        let (leaked_areas_offsets, leaked_area_size_indices): (Vec<LinearAddress>, Vec<AreaIndex>) =
            nodestore
                .split_range_into_leaked_areas(leaked_range, None)
                .into_iter()
                .unzip();

        assert_eq!(leaked_areas_offsets, expected_offsets);
        assert_eq!(leaked_area_size_indices, expected_indices);
    }
}
