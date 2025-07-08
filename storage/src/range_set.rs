// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![warn(clippy::pedantic)]

use std::collections::BTreeMap;
use std::fmt::Debug;
use std::ops::Range;

use crate::nodestore::NodeStoreHeader;
use crate::{CheckerError, LinearAddress};

#[derive(Debug)]
// BTreeMap: range end --> range start
// To check if a value is in the range set, we will find the range with the smallest end that is greater than or equal to the given value
pub struct RangeSet<T>(BTreeMap<T, T>);

struct CoalescingRanges<'a, T> {
    prev_adjacent_range: Option<Range<&'a T>>,
    intersecting_ranges: Vec<Range<&'a T>>,
    next_adjacent_range: Option<Range<&'a T>>,
}

impl<T: Clone + Ord + Debug> RangeSet<T> {
    pub const fn new() -> Self {
        Self(BTreeMap::new())
    }

    // returns disjoint ranges that will coalesce with the given range in ascending order
    // We look at all ranges whose end is greater than or equal to the given range's start
    // For each range, there are 5 cases:
    // Case 1: The range is before the given range
    //      Range x in RangeSet: |----|
    //      Given Range:                  |--------|
    //      Result: this will not happen since the end of x is less than the start of the given range
    // Case 2: The end of the range is equal to the start of the given range
    //      Range x in RangeSet: |--------|
    //      Given Range:                  |--------|
    //      Result: prev_adjacent_range = Some(x)
    // Case 3: The range intersects with the given range
    //      Range x in RangeSet: |----------|
    //      Given Range:               |-----------|
    //      Result: intersecting_ranges = [x, ...]
    //     or:
    //      Range x in RangeSet: |----------------|
    //      Given Range:             |--------|
    //     or:
    //      Range x in RangeSet:     |--------|
    //      Given Range:         |-----------------|
    //     or:
    //      Range x in RangeSet:      |------------|
    //      Given Range:         |--------|
    //      Result: intersecting_ranges = [.., x, ..]
    // Case 4: The start of the range is equal to the end of the given range
    //      Range x in RangeSet:          |--------|
    //      Given Range:         |--------|
    //      Result: next_adjacent_range = Some(x), and we are done iterating through the ranges
    // Case 5: The range is after the given range
    //      Range x in RangeSet:              |--------|
    //      Given Range:         |--------|
    //      Result: We are done iterating through the ranges
    fn get_coalescing_ranges(&self, range: &Range<T>) -> CoalescingRanges<'_, T> {
        let mut prev_adjacent_range = None;
        let mut intersecting_ranges = Vec::new();
        let mut next_adjacent_range = None;

        let next_items = self.0.range(range.start.clone()..);
        // all ranges will have next_range_end >= range.start and next_range_end >= next_range_start
        for (next_range_end, next_range_start) in next_items {
            if next_range_end == &range.start {
                // Case 2: The end of the range is equal to the start of the given range - this can only happen to the first item
                prev_adjacent_range = Some(next_range_start..next_range_end);
            } else if next_range_start < &range.end {
                // Case 3: The range intersects with the given range
                intersecting_ranges.push(next_range_start..next_range_end);
            } else if next_range_start == &range.end {
                // Case 4: The start of the range is equal to the end of the given range
                next_adjacent_range = Some(next_range_start..next_range_end);
                break;
            } else {
                // Case 5: the range is after the given range
                break;
            }
        }

        CoalescingRanges {
            prev_adjacent_range,
            intersecting_ranges,
            next_adjacent_range,
        }
    }

    // Try inserting the range
    // if allow_intersecting is false and the range intersects with existing ranges, return error with the intersection
    fn insert_range_helper(
        &mut self,
        range: Range<T>,
        allow_intersecting: bool,
    ) -> Result<(), Vec<Range<T>>> {
        // if the range is empty, do nothing
        if range.is_empty() {
            return Ok(());
        }

        let CoalescingRanges {
            prev_adjacent_range: prev_consecutive_range,
            intersecting_ranges,
            next_adjacent_range: next_consecutive_range,
        } = self.get_coalescing_ranges(&range);

        // if the insert needs to be disjoint but we found intersecting ranges, return error with the intersection
        if !allow_intersecting && !intersecting_ranges.is_empty() {
            let intersections = intersecting_ranges
                .into_iter()
                .map(|intersecting_range| {
                    let start =
                        std::cmp::max(range.start.clone(), intersecting_range.start.clone());
                    let end = std::cmp::min(range.end.clone(), intersecting_range.end.clone());
                    start..end
                })
                .collect();
            return Err(intersections);
        }

        // find the new start and end after the coalescing
        let coalesced_start = match (&prev_consecutive_range, intersecting_ranges.first()) {
            (Some(prev_range), _) => prev_range.start.clone(),
            (None, Some(first_intersecting_range)) => {
                std::cmp::min(range.start, first_intersecting_range.start.clone())
            }
            (None, None) => range.start,
        };
        let coalesced_end = match (&next_consecutive_range, intersecting_ranges.last()) {
            (Some(next_range), _) => next_range.end.clone(),
            (None, Some(last_intersecting_range)) => {
                std::cmp::max(range.end, last_intersecting_range.end.clone())
            }
            (None, None) => range.end,
        };

        // remove the coalescing ranges
        let remove_ranges_iter = prev_consecutive_range
            .into_iter()
            .chain(intersecting_ranges)
            .chain(next_consecutive_range);
        let remove_keys = remove_ranges_iter
            .map(|range| range.end.clone())
            .collect::<Vec<_>>();
        for key in remove_keys {
            self.0.remove(&key);
        }

        // insert the new range after coalescing
        self.0.insert(coalesced_end, coalesced_start);
        Ok(())
    }

    /// Insert the range into the range set.
    #[cfg(test)]
    pub fn insert_range(&mut self, range: Range<T>) {
        self.insert_range_helper(range, true)
            .expect("insert range should always success if we allow intersecting area insert");
    }

    /// Insert the given range into the range set if the range does not intersect with existing ranges, otherwise return the error with the intersection
    pub fn insert_disjoint_range(&mut self, range: Range<T>) -> Result<(), Vec<Range<T>>> {
        self.insert_range_helper(range, false)
    }

    /// Returns the complement of the range set in the given range.
    pub fn complement(&self, min: &T, max: &T) -> Self {
        let mut complement_tree = BTreeMap::new();
        // first range will start from min
        let mut start = min;
        for (next_range_end, next_range_start) in &self.0 {
            // insert the range from the previous end to the current start
            if next_range_start > start {
                if next_range_start >= max {
                    // we have reached the max - we are done
                    break;
                }
                complement_tree.insert(next_range_start.clone(), start.clone());
            }
            start = std::cmp::max(start, next_range_end); // in case the entire range is smaller than min
        }

        // insert the last range before the max
        if start < max {
            complement_tree.insert(max.clone(), start.clone());
        }

        Self(complement_tree)
    }
}

impl<T: Debug> IntoIterator for RangeSet<T> {
    type Item = Range<T>;
    type IntoIter =
        std::iter::Map<<BTreeMap<T, T> as IntoIterator>::IntoIter, fn((T, T)) -> Self::Item>;

    fn into_iter(self) -> Self::IntoIter {
        self.0.into_iter().map(|(end, start)| Range { start, end })
    }
}

pub(super) struct LinearAddressRangeSet {
    range_set: RangeSet<LinearAddress>,
    max_addr: LinearAddress,
}

impl LinearAddressRangeSet {
    const NODE_STORE_ADDR_START: LinearAddress = LinearAddress::new(NodeStoreHeader::SIZE).unwrap();

    pub(super) fn new(db_size: u64) -> Result<Self, CheckerError> {
        if db_size < NodeStoreHeader::SIZE {
            return Err(CheckerError::InvalidDBSize {
                db_size,
                description: format!(
                    "db size should not be smaller than the header size ({})",
                    NodeStoreHeader::SIZE
                ),
            });
        }

        let max_addr =
            LinearAddress::new(db_size).expect("db size will be valid due to previous check");

        Ok(Self {
            range_set: RangeSet::new(),
            max_addr, // STORAGE_AREA_START..U64::MAX
        })
    }

    pub(super) fn insert_area(
        &mut self,
        addr: LinearAddress,
        size: u64,
    ) -> Result<(), CheckerError> {
        let start = addr;
        let end = start
            .checked_add(size)
            .ok_or(CheckerError::AreaOutOfBounds {
                start,
                size,
                bounds: Self::NODE_STORE_ADDR_START..self.max_addr,
            })?; // This can only happen due to overflow
        if addr < Self::NODE_STORE_ADDR_START || end > self.max_addr {
            return Err(CheckerError::AreaOutOfBounds {
                start: addr,
                size,
                bounds: Self::NODE_STORE_ADDR_START..self.max_addr,
            });
        }

        if let Err(intersection) = self.range_set.insert_disjoint_range(start..end) {
            return Err(CheckerError::AreaIntersects {
                start: addr,
                size,
                intersection,
            });
        }
        Ok(())
    }

    pub(super) fn complement(&self) -> Self {
        let complement_set = self
            .range_set
            .complement(&Self::NODE_STORE_ADDR_START, &self.max_addr);

        Self {
            range_set: complement_set,
            max_addr: self.max_addr,
        }
    }
}

impl IntoIterator for LinearAddressRangeSet {
    type Item = Range<LinearAddress>;
    type IntoIter = <RangeSet<LinearAddress> as IntoIterator>::IntoIter;

    fn into_iter(self) -> Self::IntoIter {
        self.range_set.into_iter()
    }
}

#[cfg(test)]
mod test_range_set {
    use super::*;

    #[test]
    fn test_create() {
        let range_set: RangeSet<u64> = RangeSet::new();
        assert_eq!(range_set.into_iter().collect::<Vec<_>>(), vec![]);
    }

    #[test]
    fn test_insert_range() {
        let mut range_set = RangeSet::new();
        range_set.insert_range(0..10);
        range_set.insert_range(20..30);
        assert_eq!(
            range_set.into_iter().collect::<Vec<_>>(),
            vec![0..10, 20..30]
        );
    }

    #[test]
    fn test_insert_empty_range() {
        let mut range_set = RangeSet::new();
        range_set.insert_range(0..0);
        range_set.insert_range(10..10);
        #[expect(clippy::reversed_empty_ranges)]
        range_set.insert_range(20..10);
        #[expect(clippy::reversed_empty_ranges)]
        range_set.insert_range(30..0);
        assert_eq!(range_set.into_iter().collect::<Vec<_>>(), vec![]);
    }

    #[test]
    fn test_insert_range_coalescing() {
        // coalesce ranges that are disjoint
        let mut range_set = RangeSet::new();
        range_set.insert_range(0..10);
        range_set.insert_range(20..30);
        range_set.insert_range(10..20);
        assert_eq!(range_set.into_iter().collect::<Vec<_>>(), vec![0..30]);

        // coalesce ranges that are partially overlapping
        let mut range_set = RangeSet::new();
        range_set.insert_range(0..10);
        range_set.insert_range(20..30);
        range_set.insert_range(5..25);
        assert_eq!(range_set.into_iter().collect::<Vec<_>>(), vec![0..30]);

        // coalesce multiple ranges
        let mut range_set = RangeSet::new();
        range_set.insert_range(5..10);
        range_set.insert_range(15..20);
        range_set.insert_range(25..30);
        range_set.insert_range(0..25);
        assert_eq!(range_set.into_iter().collect::<Vec<_>>(), vec![0..30]);
    }

    #[test]
    fn test_insert_disjoint_range() {
        // insert disjoint ranges
        let mut range_set = RangeSet::new();
        assert!(range_set.insert_disjoint_range(0..10).is_ok());
        assert!(range_set.insert_disjoint_range(20..30).is_ok());
        assert!(range_set.insert_disjoint_range(12..18).is_ok());
        assert_eq!(
            range_set.into_iter().collect::<Vec<_>>(),
            vec![0..10, 12..18, 20..30]
        );

        // insert consecutive ranges
        let mut range_set = RangeSet::new();
        assert!(range_set.insert_disjoint_range(0..10).is_ok());
        assert!(range_set.insert_disjoint_range(20..30).is_ok());
        assert!(range_set.insert_disjoint_range(10..20).is_ok());
        assert_eq!(range_set.into_iter().collect::<Vec<_>>(), vec![0..30]);

        // insert intersecting ranges
        let mut range_set = RangeSet::new();
        assert!(range_set.insert_disjoint_range(0..10).is_ok());
        assert!(range_set.insert_disjoint_range(20..30).is_ok());
        assert!(matches!(
            range_set.insert_disjoint_range(5..25),
            Err(intersections) if intersections == vec![5..10, 20..25]
        ));
        assert_eq!(
            range_set.into_iter().collect::<Vec<_>>(),
            vec![0..10, 20..30]
        );

        // insert with completely overlapping range
        let mut range_set = RangeSet::new();
        assert!(range_set.insert_disjoint_range(0..10).is_ok());
        assert!(range_set.insert_disjoint_range(20..30).is_ok());
        assert!(range_set.insert_disjoint_range(40..50).is_ok());
        assert!(matches!(
            range_set.insert_disjoint_range(5..45),
            Err(intersections) if intersections == vec![5..10, 20..30, 40..45]
        ));
        assert_eq!(
            range_set.into_iter().collect::<Vec<_>>(),
            vec![0..10, 20..30, 40..50]
        );
    }

    #[test]
    fn test_complement_with_empty() {
        let range_set = RangeSet::new();
        assert_eq!(
            range_set
                .complement(&0, &40)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![0..40]
        );
    }

    #[test]
    fn test_complement_with_different_bounds() {
        let mut range_set = RangeSet::new();
        range_set.insert_range(0..10);
        range_set.insert_range(20..30);
        range_set.insert_range(40..50);

        // all ranges within bound
        assert_eq!(
            range_set
                .complement(&0, &60)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![10..20, 30..40, 50..60]
        );

        // some ranges entirely out of bound
        assert_eq!(
            range_set
                .complement(&15, &35)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![15..20, 30..35]
        );

        // some ranges are partially out of bound
        assert_eq!(
            range_set
                .complement(&5, &45)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![10..20, 30..40]
        );

        // test all ranges out of bound
        assert_eq!(
            range_set
                .complement(&12, &18)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![12..18]
        );

        // test complement empty
        assert_eq!(
            range_set
                .complement(&20, &30)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![]
        );

        // test boundaries at range start and end
        assert_eq!(
            range_set
                .complement(&0, &30)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![10..20]
        );

        // test with equal bounds
        assert_eq!(
            range_set
                .complement(&35, &35)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![]
        );

        // test with reversed bounds
        assert_eq!(
            range_set
                .complement(&30, &0)
                .into_iter()
                .collect::<Vec<_>>(),
            vec![]
        );
    }

    #[test]
    fn test_many_small_ranges() {
        let mut range_set = RangeSet::new();
        // Insert many small ranges that will coalesce
        for i in 0..1000 {
            range_set.insert_range(i * 10..(i * 10 + 5));
        }
        // Should result in 1000 separate single-element ranges
        assert_eq!(range_set.into_iter().count(), 1000);
    }

    #[test]
    fn test_large_range_coalescing() {
        let mut range_set = RangeSet::new();
        // Insert ranges that will eventually all coalesce into one
        for i in 0..1000 {
            range_set.insert_range(i * 10..(i * 10 + 5));
        }
        // Fill the gaps
        range_set.insert_range(0..10000);
        assert_eq!(range_set.into_iter().collect::<Vec<_>>(), vec![0..10000]);
    }
}

#[cfg(test)]
#[expect(clippy::unwrap_used)]
mod test_linear_address_range_set {

    use super::*;

    #[test]
    fn test_empty() {
        let visited = LinearAddressRangeSet::new(0x1000).unwrap();
        let visited_ranges = visited
            .into_iter()
            .map(|range| (range.start, range.end))
            .collect::<Vec<_>>();
        assert_eq!(visited_ranges, vec![]);
    }

    #[test]
    fn test_insert_area() {
        let start = 2048;
        let size = 1024;

        let start_addr = LinearAddress::new(start).unwrap();
        let end_addr = LinearAddress::new(start + size).unwrap();

        let mut visited = LinearAddressRangeSet::new(0x1000).unwrap();
        visited
            .insert_area(start_addr, size)
            .expect("the given area should be within bounds");

        let visited_ranges = visited
            .into_iter()
            .map(|range| (range.start, range.end))
            .collect::<Vec<_>>();
        assert_eq!(visited_ranges, vec![(start_addr, end_addr)]);
    }

    #[test]
    fn test_consecutive_areas_merge() {
        let start1 = 2048;
        let size1 = 1024;
        let start2 = start1 + size1;
        let size2 = 1024;

        let start1_addr = LinearAddress::new(start1).unwrap();
        let start2_addr = LinearAddress::new(start2).unwrap();
        let end2_addr = LinearAddress::new(start2 + size2).unwrap();

        let mut visited = LinearAddressRangeSet::new(0x1000).unwrap();
        visited
            .insert_area(start1_addr, size1)
            .expect("the given area should be within bounds");

        visited
            .insert_area(start2_addr, size2)
            .expect("the given area should be within bounds");

        let visited_ranges = visited
            .into_iter()
            .map(|range| (range.start, range.end))
            .collect::<Vec<_>>();
        assert_eq!(visited_ranges, vec![(start1_addr, end2_addr),]);
    }

    #[test]
    fn test_intersecting_areas_will_fail() {
        let start1 = 2048;
        let size1 = 1024;
        let start2 = start1 + size1 - 1;
        let size2 = 1024;

        let start1_addr = LinearAddress::new(start1).unwrap();
        let end1_addr = LinearAddress::new(start1 + size1).unwrap();
        let start2_addr = LinearAddress::new(start2).unwrap();

        let mut visited = LinearAddressRangeSet::new(0x1000).unwrap();
        visited
            .insert_area(start1_addr, size1)
            .expect("the given area should be within bounds");

        let error = visited
            .insert_area(start2_addr, size2)
            .expect_err("the given area should intersect with the first area");

        assert!(
            matches!(error, CheckerError::AreaIntersects { start, size, intersection } if start == start2_addr && size == size2 && intersection == vec![start2_addr..end1_addr])
        );

        // try inserting in opposite order
        let mut visited2 = LinearAddressRangeSet::new(0x1000).unwrap();
        visited2
            .insert_area(start2_addr, size2)
            .expect("the given area should be within bounds");

        let error = visited2
            .insert_area(start1_addr, size1)
            .expect_err("the given area should intersect with the first area");

        assert!(
            matches!(error, CheckerError::AreaIntersects { start, size, intersection } if start == start1_addr && size == size1 && intersection == vec![start2_addr..end1_addr])
        );
    }

    #[test]
    fn test_complement() {
        let start1 = 3000;
        let size1 = 1024;
        let start2 = 4096;
        let size2 = 1024;
        let db_size = 0x2000;

        let db_begin = LinearAddressRangeSet::NODE_STORE_ADDR_START;
        let start1_addr = LinearAddress::new(start1).unwrap();
        let end1_addr = LinearAddress::new(start1 + size1).unwrap();
        let start2_addr = LinearAddress::new(start2).unwrap();
        let end2_addr = LinearAddress::new(start2 + size2).unwrap();
        let db_end = LinearAddress::new(db_size).unwrap();

        let mut visited = LinearAddressRangeSet::new(db_size).unwrap();
        visited.insert_area(start1_addr, size1).unwrap();
        visited.insert_area(start2_addr, size2).unwrap();

        let complement = visited.complement().into_iter().collect::<Vec<_>>();
        assert_eq!(
            complement,
            vec![
                db_begin..start1_addr,
                end1_addr..start2_addr,
                end2_addr..db_end,
            ]
        );
    }

    #[test]
    fn test_complement_with_full_range() {
        let db_size = 0x1000;
        let start = 2048;
        let size = db_size - start;

        let mut visited = LinearAddressRangeSet::new(db_size).unwrap();
        visited
            .insert_area(LinearAddress::new(start).unwrap(), size)
            .unwrap();
        let complement = visited.complement().into_iter().collect::<Vec<_>>();
        assert_eq!(complement, vec![]);
    }

    #[test]
    fn test_complement_with_empty() {
        let db_size = LinearAddressRangeSet::NODE_STORE_ADDR_START;
        let visited = LinearAddressRangeSet::new(db_size.get()).unwrap();
        let complement = visited.complement().into_iter().collect::<Vec<_>>();
        assert_eq!(complement, vec![]);
    }
}
