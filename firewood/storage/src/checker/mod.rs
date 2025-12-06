// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod range_set;
pub(crate) use range_set::LinearAddressRangeSet;

use crate::logger::warn;
use crate::nodestore::alloc::FreeAreaWithMetadata;
use crate::nodestore::primitives::{AreaIndex, area_size_iter};
use crate::{
    CheckerError, Committed, FileIoError, HashType, HashedNodeReader, ImmutableProposal,
    IntoHashType, LinearAddress, MutableProposal, Node, NodeReader, NodeStore, Path,
    ReadableStorage, RootReader, StoredAreaParent, TrieNodeParent, WritableStorage,
};

#[cfg(not(feature = "ethhash"))]
use crate::hashednode::hash_node;

use std::cmp::Ordering;
use std::collections::BTreeMap;
use std::ops::Range;
use std::sync::Arc;

use indicatif::ProgressBar;

const OS_PAGE_SIZE: u64 = 4096;

#[inline]
// return u64 since the start address may be 0
const fn page_number(addr: LinearAddress) -> u64 {
    addr.get() / OS_PAGE_SIZE
}

fn extra_read_pages(addr: LinearAddress, bytes: u64) -> Option<u64> {
    let start_page = page_number(addr);
    let end_page = page_number(addr.advance(bytes.saturating_sub(1))?);
    let pages_read = end_page.saturating_sub(start_page).saturating_add(1); // Include the first page
    let min_pages = bytes.saturating_add(OS_PAGE_SIZE - 1) / OS_PAGE_SIZE; // Round up to the nearest page
    Some(pages_read.saturating_sub(min_pages))
}

#[cfg(feature = "ethhash")]
fn is_valid_key(key: &Path) -> bool {
    const VALID_ETH_KEY_SIZES: [usize; 2] = [64, 128]; // in number of nibbles - two nibbles make a byte
    VALID_ETH_KEY_SIZES.contains(&key.0.len())
}

#[cfg(not(feature = "ethhash"))]
fn is_valid_key(key: &Path) -> bool {
    key.0.len().is_multiple_of(2)
}

/// Options for the checker
#[derive(Debug)]
pub struct CheckOpt {
    /// Whether to check the hash of the nodes
    pub hash_check: bool,
    /// Optional progress bar to show the checker progress
    pub progress_bar: Option<ProgressBar>,
}

/// Report of the checker results.
#[derive(Debug)]
pub struct CheckerReport {
    /// Errors encountered during the check
    pub errors: Vec<CheckerError>,
    /// Statistics about the database
    pub db_stats: DBStats,
}

/// All statistics about the given database image
#[derive(Debug, Default)]
pub struct DBStats {
    /// The high watermark of the database
    pub high_watermark: u64,
    /// Statistics about the trie
    pub trie_stats: TrieStats,
    /// Statistics about the free list
    pub free_list_stats: FreeListsStats,
}

/// Statistics about the trie
#[derive(Debug, PartialEq)]
pub struct TrieStats {
    /// The total number of bytes of compressed branch nodes
    pub branch_bytes: u64,
    /// The total number of bytes of compressed leaf nodes
    pub leaf_bytes: u64,
    /// The number of key-value pairs stored in the trie
    pub kv_count: u64,
    /// The total number of bytes of for key-value pairs stored in the trie
    pub kv_bytes: u64,
    /// Branching factor distribution of each branch node
    pub branching_factors: BTreeMap<usize, u64>,
    /// Depth distribution of each leaf node
    pub depths: BTreeMap<usize, u64>,
    /// The distribution of area sizes for branch nodes
    pub branch_area_counts: BTreeMap<u64, u64>,
    /// The distribution of area sizes for leaf nodes
    pub leaf_area_counts: BTreeMap<u64, u64>,
    /// Branches that can fit into a smaller area
    pub low_occupancy_branch_area_count: u64,
    /// Leaves that can fit into a smaller area
    pub low_occupancy_leaf_area_count: u64,
    /// The number of areas that span an extra page due to not being aligned
    pub area_extra_unaligned_page: u64,
    /// The number of nodes that span an extra page due to not being aligned
    pub node_extra_unaligned_page: u64,
}

impl Default for TrieStats {
    fn default() -> Self {
        Self {
            branch_bytes: 0,
            leaf_bytes: 0,
            kv_count: 0,
            kv_bytes: 0,
            branching_factors: BTreeMap::new(),
            depths: BTreeMap::new(),
            branch_area_counts: area_size_iter().map(|(_, size)| (size, 0)).collect(),
            leaf_area_counts: area_size_iter().map(|(_, size)| (size, 0)).collect(),
            low_occupancy_branch_area_count: 0,
            low_occupancy_leaf_area_count: 0,
            area_extra_unaligned_page: 0,
            node_extra_unaligned_page: 0,
        }
    }
}

/// Statistics about the free list
#[derive(Debug, PartialEq)]
pub struct FreeListsStats {
    /// The distribution of area sizes in the free lists
    pub area_counts: BTreeMap<u64, u64>,
    /// The number of free areas that span an extra page due to not being aligned
    pub area_extra_unaligned_page: u64,
}

impl Default for FreeListsStats {
    fn default() -> Self {
        Self {
            area_counts: area_size_iter().map(|(_, size)| (size, 0)).collect(),
            area_extra_unaligned_page: 0,
        }
    }
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
#[expect(clippy::result_large_err)]
impl<T, S: ReadableStorage> NodeStore<T, S>
where
    NodeStore<T, S>: HashedNodeReader,
{
    /// Go through the filebacked storage and check for any inconsistencies. It proceeds in the following steps:
    /// 1. Check the header
    /// 2. traverse the trie and check the nodes
    /// 3. check the free list
    /// 4. report leaked areas (areas that have not yet been visited)
    /// # Errors
    /// Returns a [`CheckerError`] if the database is inconsistent.
    pub fn check(&self, opt: CheckOpt) -> CheckerReport {
        // 1. Check the header
        let high_watermark = self.size();
        let mut visited = match LinearAddressRangeSet::new(high_watermark) {
            Ok(visited) => visited,
            Err(e) => {
                return CheckerReport {
                    errors: vec![e],
                    db_stats: DBStats::default(),
                };
            }
        };

        let mut errors = Vec::new();

        // 2. traverse the trie and check the nodes
        if let Some(progress_bar) = &opt.progress_bar {
            progress_bar.set_length(high_watermark);
            progress_bar.set_message("Traversing the trie...");
        }
        let trie_stats = self
            .root_as_maybe_persisted_node()
            .and_then(|node| self.root_hash().map(|root_hash| (node, root_hash)))
            .and_then(|(root, root_hash)| {
                if let Some(root_address) = root.as_linear_address() {
                    let (trie_stats, trie_errors) = self.visit_trie(
                        root_address,
                        root_hash.into_hash_type(),
                        &mut visited,
                        opt.progress_bar.as_ref(),
                        opt.hash_check,
                    );
                    errors.extend(trie_errors);
                    Some(trie_stats)
                } else {
                    errors.push(CheckerError::UnpersistedRoot);
                    None
                }
            })
            .unwrap_or_default();

        // 3. check the free list - this can happen in parallel with the trie traversal
        if let Some(progress_bar) = &opt.progress_bar {
            progress_bar.set_message("Traversing free lists...");
        }
        let (free_list_stats, free_list_traverse_errors) =
            self.visit_freelist(&mut visited, opt.progress_bar.as_ref());
        errors.extend(free_list_traverse_errors);

        // 4. report leaked areas (areas that have not yet been visited)
        if let Some(progress_bar) = &opt.progress_bar {
            progress_bar.set_message("Checking leaked areas...");
        }
        let leaked_ranges = visited.complement();
        if !leaked_ranges.is_empty() {
            warn!("Found leaked ranges: {leaked_ranges}");
            errors.push(CheckerError::AreaLeaks(leaked_ranges));
        }

        CheckerReport {
            errors,
            db_stats: DBStats {
                high_watermark,
                trie_stats,
                free_list_stats,
            },
        }
    }

    fn visit_trie(
        &self,
        root_address: LinearAddress,
        root_hash: HashType,
        visited: &mut LinearAddressRangeSet,
        progress_bar: Option<&ProgressBar>,
        hash_check: bool,
    ) -> (TrieStats, Vec<CheckerError>) {
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
        let errors = self
            .visit_trie_helper(trie, visited, &mut trie_stats, progress_bar, hash_check)
            .err()
            .unwrap_or_default();
        (trie_stats, errors)
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
    ) -> Result<(), Vec<CheckerError>> {
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

        // read the node from the disk - we avoid cache since we will never visit the same node twice
        let (area_index, area_size) =
            self.area_index_and_size(subtrie_root_address)
                .map_err(|e| {
                    vec![CheckerError::IO {
                        error: e,
                        parent: StoredAreaParent::TrieNode(parent),
                    }]
                })?;
        let (node, node_bytes) = self
            .read_node_with_num_bytes_from_disk(subtrie_root_address)
            .map_err(|e| {
                vec![CheckerError::IO {
                    error: e,
                    parent: StoredAreaParent::TrieNode(parent),
                }]
            })?;

        // check if the node fits in the area, equal is not allowed due to 1-byte area size index
        if node_bytes > area_size {
            return Err(vec![CheckerError::NodeLargerThanArea {
                area_start: subtrie_root_address,
                area_size,
                node_bytes,
                parent,
            }]);
        }

        // if the node has a value, check that the key is valid
        let mut current_path_prefix = path_prefix.clone();
        current_path_prefix.0.extend_from_slice(node.partial_path());
        if node.value().is_some() && !is_valid_key(&current_path_prefix) {
            return Err(vec![CheckerError::InvalidKey {
                key: current_path_prefix,
                address: subtrie_root_address,
                parent,
            }]);
        }

        // compute the hash of the node and check it against the stored hash
        if hash_check {
            #[cfg(feature = "ethhash")]
            let hash = Self::compute_node_ethhash(&node, &path_prefix, has_peers);
            #[cfg(not(feature = "ethhash"))]
            let hash = hash_node(&node, &path_prefix);
            if hash != subtrie_root_hash {
                return Err(vec![CheckerError::HashMismatch {
                    path: current_path_prefix,
                    address: subtrie_root_address,
                    parent,
                    parent_stored_hash: subtrie_root_hash,
                    computed_hash: hash,
                }]);
            }
        }

        // check that the area is within bounds and does not intersect with other areas and mark it as visiteds
        visited.insert_area(
            subtrie_root_address,
            area_size,
            StoredAreaParent::TrieNode(parent),
        )?;

        // at this point we have checked this area is correct - update progress bar, collect stats, and recursively traverse the children
        update_progress_bar(progress_bar, visited);

        // collect trie stats
        {
            if let Some(value) = node.value() {
                // collect kv count
                trie_stats.kv_count = trie_stats.kv_count.saturating_add(1);
                // collect kv pair bytes - this is the minimum number of bytes needed to store the data
                let key_bytes = current_path_prefix.0.len().div_ceil(2);
                let value_bytes = value.len();
                trie_stats.kv_bytes = trie_stats
                    .kv_bytes
                    .saturating_add(key_bytes as u64)
                    .saturating_add(value_bytes as u64);
            }
            // collect the number of areas that requires reading an extra page due to not being aligned
            if extra_read_pages(subtrie_root_address, area_size)
                .expect("impossible since we checked in visited.insert_area()")
                > 0
            {
                trie_stats.area_extra_unaligned_page =
                    trie_stats.area_extra_unaligned_page.saturating_add(1);
            }
            // collect the number of nodes that requires reading an extra page due to not being aligned
            if extra_read_pages(subtrie_root_address, node_bytes)
                .expect("impossible since we checked in visited.insert_area()")
                > 0
            {
                trie_stats.node_extra_unaligned_page =
                    trie_stats.node_extra_unaligned_page.saturating_add(1);
            }
        }

        // collect more stats and recursively traverse the children
        let mut errors = Vec::new();
        match node.as_ref() {
            Node::Branch(branch) => {
                let persist_info = branch.persist_info();
                let num_children = persist_info.count();
                {
                    // update the branch area count
                    let branch_area_count = trie_stats
                        .branch_area_counts
                        .get_mut(&area_size)
                        .expect("area size is initialized when trie_stats is created and we checked that node_bytes <= area_size");
                    *branch_area_count = branch_area_count.saturating_add(1);
                    // collect the branch bytes
                    trie_stats.branch_bytes = trie_stats.branch_bytes.saturating_add(node_bytes);
                    // collect low occupancy area count, add 1 for the area size index byte
                    let smallest_area_index = AreaIndex::from_size(node_bytes)
                        .expect("impossible since we checked that node_bytes <= area_size");
                    if smallest_area_index < area_index {
                        trie_stats.low_occupancy_branch_area_count =
                            trie_stats.low_occupancy_branch_area_count.saturating_add(1);
                    }
                    // collect the branching factor distribution
                    let branching_factor_count = trie_stats
                        .branching_factors
                        .entry(num_children)
                        .or_insert(0);
                    *branching_factor_count = branching_factor_count.saturating_add(1);
                }

                // this is an internal node, traverse the children
                for (nibble, (address, hash)) in persist_info
                    .into_iter()
                    .filter_map(|(idx, slot)| slot.map(|child| (idx, child)))
                {
                    let parent = TrieNodeParent::Parent(subtrie_root_address, nibble);
                    let mut child_path_prefix = current_path_prefix.clone();
                    child_path_prefix.0.push(nibble.as_u8());
                    let child_subtrie = SubTrieMetadata {
                        root_address: address,
                        root_hash: hash.clone(),
                        parent,
                        depth: depth.saturating_add(1),
                        path_prefix: child_path_prefix,
                        #[cfg(feature = "ethhash")]
                        has_peers: num_children != 1,
                    };
                    if let Err(e) = self.visit_trie_helper(
                        child_subtrie,
                        visited,
                        trie_stats,
                        progress_bar,
                        hash_check,
                    ) {
                        errors.extend(e);
                    }
                }
            }
            Node::Leaf(_) => {
                // update the leaf area count
                let leaf_area_count = trie_stats
                 .leaf_area_counts
                 .get_mut(&area_size)
                 .expect("area size is initialized when trie_stats is created and we checked that node_bytes <= area_size");
                *leaf_area_count = leaf_area_count.saturating_add(1);
                // collect the leaf bytes
                trie_stats.leaf_bytes = trie_stats.leaf_bytes.saturating_add(node_bytes);
                // collect low occupancy area count, add 1 for the area size index byte
                let smallest_area_index = AreaIndex::from_size(node_bytes)
                    .expect("impossible since we checked that node_bytes <= area_size");
                if smallest_area_index < area_index {
                    trie_stats.low_occupancy_leaf_area_count =
                        trie_stats.low_occupancy_leaf_area_count.saturating_add(1);
                }
                // collect the depth distribution
                let depth_count = trie_stats.depths.entry(depth).or_insert(0);
                *depth_count = depth_count.saturating_add(1);
            }
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(errors)
        }
    }

    /// Traverse all the free areas in the freelist
    fn visit_freelist(
        &self,
        visited: &mut LinearAddressRangeSet,
        progress_bar: Option<&ProgressBar>,
    ) -> (FreeListsStats, Vec<CheckerError>) {
        let mut area_counts: BTreeMap<u64, u64> =
            area_size_iter().map(|(_, size)| (size, 0)).collect();
        let mut area_extra_unaligned_page = 0u64;
        let mut errors = Vec::new();

        let mut free_list_iter = self.free_list_iter(AreaIndex::MIN);
        while let Some((free_area, parent)) = free_list_iter.next_with_metadata() {
            let FreeAreaWithMetadata {
                addr,
                area_index,
                free_list_id,
            } = match free_area {
                Ok(free_area) => free_area,
                Err(e) => {
                    errors.push(CheckerError::IO {
                        error: e,
                        parent: StoredAreaParent::FreeList(parent),
                    });
                    free_list_iter.move_to_next_free_list();
                    continue;
                }
            };

            // check that the area is aligned
            if let Err(e) = self.check_area_aligned(addr, StoredAreaParent::FreeList(parent)) {
                errors.push(e);
                free_list_iter.move_to_next_free_list();
                continue;
            }

            // check that the area size matches the free list id (if it is in the correct free list)
            let area_size = area_index.size();
            if free_list_id != area_index {
                errors.push(CheckerError::FreelistAreaSizeMismatch {
                    address: addr,
                    size: area_size,
                    actual_free_list: free_list_id,
                    expected_free_list: area_index,
                    parent,
                });
                free_list_iter.move_to_next_free_list();
                continue;
            }

            // check the free area is within bounds and does not intersect with other areas
            if let Err(e) = visited.insert_area(addr, area_size, StoredAreaParent::FreeList(parent))
            {
                errors.push(e);
                free_list_iter.move_to_next_free_list();
                continue;
            }
            update_progress_bar(progress_bar, visited);

            // collect the free list stats
            {
                // collect the free lists area distribution
                let area_count = area_counts
                    .get_mut(&area_size)
                    .expect("area size is initialized when free_list_stats is created");
                *area_count = area_count.saturating_add(1);
                // collect the multi-page area count
                if extra_read_pages(addr, area_size)
                    .expect("impossible since we checked in visited.insert_area()")
                    > 0
                {
                    area_extra_unaligned_page = area_extra_unaligned_page.saturating_add(1);
                }
            }
        }

        (
            FreeListsStats {
                area_counts,
                area_extra_unaligned_page,
            },
            errors,
        )
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
}

/// Report of the fix operation
#[derive(Debug)]
pub struct FixReport {
    /// Errors that were fixed
    pub fixed: Vec<CheckerError>,
    /// Errors that were not fixed, either because they are unfixable or because an IO error occurred
    pub unfixable: Vec<(CheckerError, Option<FileIoError>)>,
    /// Statistics about the database
    pub db_stats: DBStats,
}

impl<S: WritableStorage> NodeStore<Committed, S> {
    /// Check the node store and fix any errors found.
    /// Returns a report of the fix operation.
    // TODO: return a committed revision instead of an immutable proposal
    pub fn check_and_fix(
        &self,
        opt: CheckOpt,
    ) -> (
        Result<NodeStore<Arc<ImmutableProposal>, S>, FileIoError>,
        FixReport,
    ) {
        let check_report = self.check(opt);
        let mut proposal = match NodeStore::<MutableProposal, S>::new(self) {
            Ok(proposal) => proposal,
            Err(e) => {
                let report = FixReport {
                    fixed: Vec::new(),
                    unfixable: check_report.errors.into_iter().map(|e| (e, None)).collect(),
                    db_stats: check_report.db_stats,
                };
                return (Err(e), report);
            }
        };
        let fix_report = proposal.fix(check_report);
        let immutable_proposal = NodeStore::<Arc<ImmutableProposal>, S>::try_from(proposal);
        (immutable_proposal, fix_report)
    }
}

impl<S: WritableStorage> NodeStore<MutableProposal, S> {
    fn fix(&mut self, check_report: CheckerReport) -> FixReport {
        let mut fixed = Vec::new();
        let mut unfixable = Vec::new();

        for error in check_report.errors {
            match error.parent() {
                Some(StoredAreaParent::TrieNode(_)) => {
                    warn!("Fix for trie node error not yet implemented");
                    unfixable.push((error, None));
                }
                Some(StoredAreaParent::FreeList(free_list_parent)) => {
                    if let Err(e) = self.truncate_free_list(free_list_parent) {
                        unfixable.push((error, Some(e)));
                    } else {
                        fixed.push(error);
                    }
                }
                None => {
                    if let CheckerError::AreaLeaks(ranges) = &error {
                        {
                            let _leaked_areas = self.split_all_leaked_ranges(ranges, None);
                            // TODO: add _leaked_areas to the free list
                        }
                        unfixable.push((error, None));
                    } else {
                        unfixable.push((error, None));
                    }
                }
            }
        }

        FixReport {
            fixed,
            unfixable,
            db_stats: check_report.db_stats,
        }
    }
}

impl<T, S: WritableStorage> NodeStore<T, S>
where
    NodeStore<T, S>: NodeReader,
{
    /// Wrapper around `split_into_leaked_areas` that iterates over a collection of ranges.
    fn split_all_leaked_ranges<'a>(
        &self,
        leaked_ranges: impl IntoIterator<Item = Range<&'a LinearAddress>>,
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
        leaked_range: Range<&LinearAddress>,
        progress_bar: Option<&ProgressBar>,
    ) -> Vec<(LinearAddress, AreaIndex)> {
        let mut leaked = Vec::new();
        let mut current_addr = *leaked_range.start;

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
            match next_addr.cmp(leaked_range.end) {
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
        for (area_index, area_size) in area_size_iter().rev() {
            loop {
                let next_addr = current_addr
                    .advance(area_size)
                    .expect("address overflow is impossible");
                if next_addr <= *leaked_range.end {
                    leaked.push((current_addr, area_index));
                    if let Some(progress_bar) = progress_bar {
                        progress_bar.inc(area_size);
                    }
                    current_addr = next_addr;
                } else {
                    break;
                }
            }
        }

        // we assume that all areas are aligned to `MIN_AREA_SIZE`, in which case leaked ranges can always be split into free areas perfectly
        debug_assert!(current_addr == *leaked_range.end);
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
    use crate::nodestore::alloc::FreeLists;
    use crate::nodestore::alloc::test_utils::{
        test_write_free_area, test_write_header, test_write_new_node, test_write_zeroed_area,
    };
    use crate::nodestore::primitives::area_size_iter;
    use crate::{
        BranchNode, Child, Children, FreeListParent, LeafNode, NodeStore, Path, PathComponent,
        area_index, hash_node,
    };

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
    #[cfg_attr(doc, aquamarine::aquamarine)]
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
        let mut total_branch_bytes_written = 0;
        let mut total_leaf_bytes_written = 0;
        let mut branch_area_counts: BTreeMap<u64, u64> =
            area_size_iter().map(|(_, size)| (size, 0)).collect();
        let mut leaf_area_counts: BTreeMap<u64, u64> =
            area_size_iter().map(|(_, size)| (size, 0)).collect();
        let leaf = Node::Leaf(LeafNode {
            partial_path: Path::from_nibbles_iterator(std::iter::repeat_n([4, 5], 30).flatten()),
            value: Box::new([6, 7, 8]),
        });
        let leaf_addr = LinearAddress::new(high_watermark).unwrap();
        let leaf_hash = hash_node(&leaf, &Path::from([2, 0, 3, 1]));
        let (bytes_written, stored_area_size) =
            test_write_new_node(nodestore, &leaf, high_watermark);
        high_watermark += stored_area_size;
        total_leaf_bytes_written += bytes_written;
        let area_count = leaf_area_counts.get_mut(&stored_area_size).unwrap();
        *area_count = area_count.saturating_add(1);

        let mut branch_children = Children::new();
        branch_children[PathComponent::ALL[1]] = Some(Child::AddressWithHash(leaf_addr, leaf_hash));
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
        total_branch_bytes_written += bytes_written;
        let area_count = branch_area_counts.get_mut(&stored_area_size).unwrap();
        *area_count = area_count.saturating_add(1);

        let mut root_children = Children::new();
        root_children[PathComponent::ALL[0]] =
            Some(Child::AddressWithHash(branch_addr, branch_hash));
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
        total_branch_bytes_written += bytes_written;
        let area_count = branch_area_counts.get_mut(&stored_area_size).unwrap();
        *area_count = area_count.saturating_add(1);

        // write the header
        test_write_header(
            nodestore,
            high_watermark,
            Some(root_addr),
            FreeLists::default(),
        );

        let trie_stats = TrieStats {
            branch_bytes: total_branch_bytes_written,
            leaf_bytes: total_leaf_bytes_written,
            kv_count: 1,
            kv_bytes: 32 + 3, // 32 bytes for the key, 3 bytes for the value
            branch_area_counts,
            leaf_area_counts,
            branching_factors: BTreeMap::from([(1, 2)]),
            depths: BTreeMap::from([(2, 1)]),
            low_occupancy_branch_area_count: 0,
            low_occupancy_leaf_area_count: 0,
            area_extra_unaligned_page: 0,
            node_extra_unaligned_page: 0,
        };

        TestTrie {
            nodes: vec![(leaf, leaf_addr), (branch, branch_addr), (root, root_addr)],
            high_watermark,
            root_address: root_addr,
            root_hash,
            stats: trie_stats,
        }
    }

    #[derive(Debug)]
    struct TestFreelist {
        high_watermark: u64,
        free_ranges: Vec<Range<LinearAddress>>,
        errors: Vec<CheckerError>,
        stats: FreeListsStats,
    }

    // Free list 1: 2048 bytes
    // Free list 2: 8192 bytes
    // Free list 3: 96 bytes
    // ----------------------------------------------------------------------------------------------------------------------------------------------------
    // | header | empty | free_list1_area3 | free_list1_area2 | overlap | free_list1_area1 | free_list2_area1 | free_list2_area2 | gap | free_list3_area1 |
    // ----------------------------------------------------------------------------------------------------------------------------------------------------
    //                                                             ^ free_list1_area1 and free_list1_area2 overlap by 16 bytes      ^ 1 byte
    //              ^ 16 empty bytes to ensure that free_list1_area1, free_list1_area2, and free_list2_area1 are page-aligned                ^ missaligned
    #[expect(clippy::arithmetic_side_effects)]
    fn gen_test_freelist_with_errors(
        nodestore: &mut NodeStore<Committed, MemStore>,
    ) -> TestFreelist {
        const AREA_INDEX1: AreaIndex = area_index!(9); // 2048
        const AREA_INDEX2: AreaIndex = area_index!(12); // 16384
        const AREA_INDEX3: AreaIndex = area_index!(3); // 96

        let mut free_lists = FreeLists::default();
        let mut high_watermark = NodeStoreHeader::SIZE + 16; // + 16 to create overlap

        // first free list
        let area_size1 = AREA_INDEX1.size();
        let mut next_free_block1 = None;

        test_write_free_area(nodestore, next_free_block1, AREA_INDEX1, high_watermark);
        let free_list1_area3 = LinearAddress::new(high_watermark).unwrap();
        next_free_block1 = Some(free_list1_area3);
        high_watermark += area_size1;

        test_write_free_area(nodestore, next_free_block1, AREA_INDEX1, high_watermark);
        let free_list1_area2 = LinearAddress::new(high_watermark).unwrap();
        next_free_block1 = Some(free_list1_area2);
        high_watermark += area_size1;

        let intersection_end = LinearAddress::new(high_watermark).unwrap();
        high_watermark -= 16; // create an overlap with free_list1_area2 and restore to page-aligned address
        let intersection_start = LinearAddress::new(high_watermark).unwrap();
        test_write_free_area(nodestore, next_free_block1, AREA_INDEX1, high_watermark);
        let free_list1_area1 = LinearAddress::new(high_watermark).unwrap();
        next_free_block1 = Some(free_list1_area1);
        high_watermark += area_size1;

        free_lists[AREA_INDEX1.as_usize()] = next_free_block1;

        // second free list
        let area_size2 = AREA_INDEX2.size();
        let mut next_free_block2 = None;

        test_write_free_area(nodestore, next_free_block2, AREA_INDEX2, high_watermark);
        let free_list2_area2 = LinearAddress::new(high_watermark).unwrap();
        next_free_block2 = Some(free_list2_area2);
        high_watermark += area_size2;

        test_write_free_area(nodestore, next_free_block2, AREA_INDEX2, high_watermark);
        let free_list2_area1 = LinearAddress::new(high_watermark).unwrap();
        next_free_block2 = Some(free_list2_area1);
        high_watermark += area_size2;
        let expected_high_watermark = high_watermark;

        free_lists[AREA_INDEX2.as_usize()] = next_free_block2;

        // third free list
        high_watermark += 1; // + 1 to create gap
        let area_size3 = AREA_INDEX3.size();
        let mut next_free_block3 = None;
        test_write_free_area(nodestore, next_free_block3, AREA_INDEX3, high_watermark);
        let free_list3_area1 = LinearAddress::new(high_watermark).unwrap();
        next_free_block3 = Some(free_list3_area1);
        high_watermark += area_size3;

        free_lists[AREA_INDEX3.as_usize()] = next_free_block3;

        // write header
        test_write_header(nodestore, high_watermark, None, free_lists);

        let expected_start_addr = free_lists[AREA_INDEX1.as_usize()].unwrap();
        let expected_end_addr = LinearAddress::new(expected_high_watermark).unwrap();
        let expected_free_areas = vec![expected_start_addr..expected_end_addr];
        let expected_freelist_errors = vec![
            CheckerError::AreaMisaligned {
                address: free_list3_area1,
                parent: StoredAreaParent::FreeList(FreeListParent::FreeListHead(AREA_INDEX3)),
            },
            CheckerError::AreaIntersects {
                start: free_list1_area2,
                size: area_size1,
                intersection: vec![intersection_start..intersection_end],
                parent: StoredAreaParent::FreeList(FreeListParent::PrevFreeArea {
                    area_size_idx: AREA_INDEX1,
                    parent_addr: free_list1_area1,
                }),
            },
        ];
        let mut area_counts = area_size_iter()
            .map(|(_, size)| (size, 0))
            .collect::<BTreeMap<_, _>>();
        area_counts.insert(area_size1, 1);
        area_counts.insert(area_size2, 2);
        let expected_free_lists_stats = FreeListsStats {
            area_counts,
            area_extra_unaligned_page: 0,
        };

        TestFreelist {
            high_watermark,
            free_ranges: expected_free_areas,
            errors: expected_freelist_errors,
            stats: expected_free_lists_stats,
        }
    }

    use std::collections::HashMap;

    #[test]
    // This test creates a simple trie and checks that the checker traverses it correctly.
    // We use primitive calls here to do a low-level check.
    fn checker_traverse_correct_trie() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into());

        let test_trie = gen_test_trie(&mut nodestore);
        // let (_, high_watermark, (root_addr, root_hash)) = gen_test_trie(&mut nodestore);

        // verify that all of the space is accounted for - since there is no free area
        let mut visited = LinearAddressRangeSet::new(test_trie.high_watermark).unwrap();
        let (stats, errors) = nodestore.visit_trie(
            test_trie.root_address,
            test_trie.root_hash,
            &mut visited,
            None,
            true,
        );
        let complement = visited.complement();
        assert_eq!(complement.into_iter().collect::<Vec<_>>(), vec![]);
        assert_eq!(stats, test_trie.stats);
        assert_eq!(errors, vec![]);
    }

    #[test]
    // This test permutes the simple trie with a wrong hash and checks that the checker detects it.
    fn checker_traverse_trie_with_wrong_hash() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into());

        let mut test_trie = gen_test_trie(&mut nodestore);
        let root_addr = test_trie.root_address;

        // find the root node and replace the branch hash with an incorrect (default) hash
        let (branch_node, branch_addr) = test_trie
            .nodes
            .iter_mut()
            .find(|(node, _)| matches!(node, Node::Branch(b) if *b.partial_path.0 == [3]))
            .unwrap();

        let branch = branch_node.as_branch_mut().unwrap();
        let branch_addr = *branch_addr;

        // Replace branch hash with a wrong hash
        let (leaf_addr, _) = branch.children[PathComponent::ALL[1]]
            .as_ref()
            .unwrap()
            .persist_info()
            .unwrap();
        branch.children[PathComponent::ALL[1]] =
            Some(Child::AddressWithHash(leaf_addr, HashType::empty()));
        test_write_new_node(&nodestore, branch_node, branch_addr.get());

        // Compute the current branch hash
        #[cfg(feature = "ethhash")]
        let computed_hash = NodeStore::<Committed, MemStore>::compute_node_ethhash(
            branch_node,
            &Path::from([2, 0]),
            false,
        );
        #[cfg(not(feature = "ethhash"))]
        let computed_hash = hash_node(branch_node, &Path::from([2, 0]));

        // Get parent stored hash
        let (root_node, _) = test_trie
            .nodes
            .iter()
            .find(|(node, _)| matches!(node, Node::Branch(b) if *b.partial_path.0 == [2]))
            .unwrap();
        let root_branch = root_node.as_branch().unwrap();
        let (_, parent_stored_hash) = root_branch.children[PathComponent::ALL[0]]
            .as_ref()
            .unwrap()
            .persist_info()
            .unwrap();
        let parent_stored_hash = parent_stored_hash.clone();

        // run the checker and verify that it returns the HashMismatch error
        let mut visited = LinearAddressRangeSet::new(test_trie.high_watermark).unwrap();
        let (_, errors) = nodestore.visit_trie(
            test_trie.root_address,
            test_trie.root_hash,
            &mut visited,
            None,
            true,
        );

        let expected_error = CheckerError::HashMismatch {
            address: branch_addr,
            path: Path::from([2, 0, 3]),
            parent: TrieNodeParent::Parent(root_addr, PathComponent::ALL[0]),
            parent_stored_hash,
            computed_hash,
        };
        assert_eq!(errors, vec![expected_error]);
    }

    #[test]
    fn traverse_correct_freelist() {
        let rng = crate::SeededRng::from_env_or_random();

        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into());

        // write free areas
        let mut high_watermark = NodeStoreHeader::SIZE;
        let mut free_area_counts: BTreeMap<u64, u64> =
            area_size_iter().map(|(_, size)| (size, 0)).collect();
        let mut extra_unaligned_page_read = 0u64;
        let mut freelist = FreeLists::default();
        for (area_index, area_size) in area_size_iter() {
            let mut next_free_block = None;
            let num_free_areas = rng.random_range(0..4);
            for _ in 0..num_free_areas {
                test_write_free_area(&nodestore, next_free_block, area_index, high_watermark);
                next_free_block = Some(LinearAddress::new(high_watermark).unwrap());
                let start_addr = LinearAddress::new(high_watermark).unwrap();
                if extra_read_pages(start_addr, area_size).unwrap() > 0 {
                    extra_unaligned_page_read = extra_unaligned_page_read.saturating_add(1);
                }
                high_watermark += area_size;
            }
            freelist[area_index.as_usize()] = next_free_block;
            if num_free_areas > 0 {
                free_area_counts.insert(area_size, num_free_areas);
            }
        }
        let expected_free_lists_stats = FreeListsStats {
            area_counts: free_area_counts,
            area_extra_unaligned_page: extra_unaligned_page_read,
        };

        // write header
        test_write_header(&mut nodestore, high_watermark, None, freelist);

        // test that the we traversed all the free areas
        let mut visited = LinearAddressRangeSet::new(high_watermark).unwrap();
        let (actual_free_lists_stats, free_list_errors) =
            nodestore.visit_freelist(&mut visited, None);
        let complement = visited.complement();
        assert_eq!(complement.into_iter().collect::<Vec<_>>(), vec![]);
        assert_eq!(free_list_errors, vec![]);
        assert_eq!(actual_free_lists_stats, expected_free_lists_stats);
    }

    #[test]
    fn traverse_freelist_should_skip_offspring_of_incorrect_areas() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into());
        let TestFreelist {
            high_watermark,
            free_ranges,
            errors,
            stats,
        } = gen_test_freelist_with_errors(&mut nodestore);

        // test that the we traversed all the free areas
        let mut visited = LinearAddressRangeSet::new(high_watermark).unwrap();
        let (actual_free_lists_stats, free_list_errors) =
            nodestore.visit_freelist(&mut visited, None);
        assert_eq!(visited.into_iter().collect::<Vec<_>>(), free_ranges);
        assert_eq!(actual_free_lists_stats, stats);
        assert_eq!(free_list_errors, errors);
    }

    #[test]
    fn fix_freelist_with_overlap() {
        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into());
        let TestFreelist {
            high_watermark,
            free_ranges: _,
            errors,
            stats,
        } = gen_test_freelist_with_errors(&mut nodestore);
        let expected_error_num = errors.len();

        // fix the freelist
        let mut proposal = NodeStore::<MutableProposal, _>::new(&nodestore).unwrap();
        let fix_report = proposal.fix(CheckerReport {
            errors,
            db_stats: DBStats {
                high_watermark,
                trie_stats: TrieStats::default(),
                free_list_stats: stats,
            },
        });
        assert_eq!(fix_report.fixed.len(), expected_error_num);
        assert_eq!(fix_report.unfixable.len(), 0);

        let immutable_proposal =
            NodeStore::<Arc<ImmutableProposal>, _>::try_from(proposal).unwrap();
        let mut visited = LinearAddressRangeSet::new(high_watermark).unwrap();
        let (_, free_list_errors) = immutable_proposal.visit_freelist(&mut visited, None);
        assert_eq!(free_list_errors, vec![]);
    }

    #[test]
    // This test creates a linear set of free areas and free them.
    // When traversing it should break consecutive areas.
    fn split_correct_range_into_leaked_areas() {
        use rand::seq::IteratorRandom;

        let mut rng = crate::SeededRng::from_env_or_random();

        let memstore = MemStore::new(vec![]);
        let mut nodestore = NodeStore::new_empty_committed(memstore.into());

        let num_areas = 10;

        // randomly insert areas into the nodestore
        let mut high_watermark = NodeStoreHeader::SIZE;
        let mut stored_areas = Vec::new();
        for _ in 0..num_areas {
            let (area_size_index, area_size) = area_size_iter().choose(&mut rng).unwrap();
            let area_addr = LinearAddress::new(high_watermark).unwrap();
            test_write_free_area(&nodestore, None, area_size_index, high_watermark);
            stored_areas.push((area_addr, area_size_index, area_size));
            high_watermark += area_size;
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
        let leaked_ranges = leaked
            .iter()
            .map(|Range { start, end }| Range { start, end });
        let leaked_areas: HashMap<_, _> = nodestore
            .split_all_leaked_ranges(leaked_ranges, None)
            .collect();

        // assert that all leaked areas end up on the free list
        assert_eq!(leaked_areas, expected_free_areas);
    }

    #[test]
    // This test creates a linear set of free areas and free them.
    // When traversing it should break consecutive areas.
    #[expect(clippy::arithmetic_side_effects)]
    fn split_range_of_zeros_into_leaked_areas() {
        let memstore = MemStore::new(vec![]);
        let nodestore = NodeStore::new_empty_committed(memstore.into());

        let expected_leaked_area_indices = vec![
            area_index!(8),
            area_index!(7),
            area_index!(4),
            area_index!(2),
            AreaIndex::MIN,
        ];
        let expected_leaked_area_sizes = expected_leaked_area_indices
            .iter()
            .map(|i| i.size())
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
        let leaked_range = &nonzero!(NodeStoreHeader::SIZE).into()
            ..&LinearAddress::new(
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
    #[expect(clippy::arithmetic_side_effects)]
    fn split_range_into_leaked_areas_test() {
        let memstore = MemStore::new(vec![]);
        let nodestore = NodeStore::new_empty_committed(memstore.into());

        // write two free areas
        let mut high_watermark = NodeStoreHeader::SIZE;
        test_write_free_area(&nodestore, None, area_index!(8), high_watermark); // 1024
        high_watermark += area_index!(8).size();
        test_write_free_area(&nodestore, None, area_index!(7), high_watermark); // 768
        high_watermark += area_index!(7).size();
        // write an zeroed area
        test_write_zeroed_area(&nodestore, 768, high_watermark);
        high_watermark += 768;
        // write another free area
        test_write_free_area(&nodestore, None, area_index!(8), high_watermark); // 1024
        high_watermark += area_index!(8).size();

        let expected_indices = vec![
            area_index!(8),
            area_index!(7),
            area_index!(8),
            area_index!(7),
        ];
        let expected_sizes = expected_indices
            .iter()
            .map(|i| i.size())
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
            &nonzero!(NodeStoreHeader::SIZE).into()..&LinearAddress::new(high_watermark).unwrap();
        let (leaked_areas_offsets, leaked_area_size_indices): (Vec<LinearAddress>, Vec<AreaIndex>) =
            nodestore
                .split_range_into_leaked_areas(leaked_range, None)
                .into_iter()
                .unzip();

        assert_eq!(leaked_areas_offsets, expected_offsets);
        assert_eq!(leaked_area_size_indices, expected_indices);
    }
}
