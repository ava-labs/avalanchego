//! A multiset (bag) implementation with threshold support.

use std::collections::HashMap;
use std::fmt;
use std::hash::Hash;

use crate::set::Set;

/// A multiset (bag) that tracks the count of each element.
///
/// Bags support threshold tracking, which allows efficiently querying
/// which elements have been added at least a certain number of times.
///
/// # Examples
///
/// ```
/// use avalanche_utils::Bag;
///
/// let mut bag = Bag::new();
/// bag.add(1);
/// bag.add(1);
/// bag.add(2);
///
/// assert_eq!(bag.count(&1), 2);
/// assert_eq!(bag.count(&2), 1);
/// assert_eq!(bag.len(), 3);
///
/// // Set a threshold
/// bag.set_threshold(2);
/// let met = bag.threshold();
/// assert!(met.contains(&1));
/// assert!(!met.contains(&2));
/// ```
#[derive(Clone)]
pub struct Bag<T: Eq + Hash + Clone> {
    counts: HashMap<T, usize>,
    size: usize,
    threshold: usize,
    met_threshold: Set<T>,
}

impl<T: Eq + Hash + Clone> Default for Bag<T> {
    fn default() -> Self {
        Self::new()
    }
}

impl<T: Eq + Hash + Clone> Bag<T> {
    /// Creates a new empty bag.
    #[must_use]
    pub fn new() -> Self {
        Self {
            counts: HashMap::with_capacity(16),
            size: 0,
            threshold: 0,
            met_threshold: Set::new(),
        }
    }

    /// Creates a bag from an iterator of elements.
    pub fn of<I: IntoIterator<Item = T>>(iter: I) -> Self {
        let mut bag = Self::new();
        for item in iter {
            bag.add(item);
        }
        bag
    }

    /// Sets the threshold for the threshold set.
    ///
    /// Elements that have been added at least `threshold` times
    /// will be included in the threshold set.
    pub fn set_threshold(&mut self, threshold: usize) {
        if self.threshold == threshold {
            return;
        }

        self.threshold = threshold;
        self.met_threshold.clear();

        for (item, &count) in &self.counts {
            if count >= threshold {
                self.met_threshold.add(item.clone());
            }
        }
    }

    /// Adds a single element to the bag.
    pub fn add(&mut self, item: T) {
        self.add_count(item, 1);
    }

    /// Adds multiple elements to the bag.
    pub fn add_all<I: IntoIterator<Item = T>>(&mut self, iter: I) {
        for item in iter {
            self.add(item);
        }
    }

    /// Adds an element with a specific count.
    ///
    /// If `count` is 0, this is a no-op.
    pub fn add_count(&mut self, item: T, count: usize) {
        if count == 0 {
            return;
        }

        let total_count = self.counts.entry(item.clone()).or_insert(0);
        *total_count += count;
        self.size += count;

        if *total_count >= self.threshold && self.threshold > 0 {
            self.met_threshold.add(item);
        }
    }

    /// Returns the count of the given element.
    #[must_use]
    pub fn count(&self, item: &T) -> usize {
        self.counts.get(item).copied().unwrap_or(0)
    }

    /// Returns the total number of elements (including duplicates).
    #[must_use]
    pub fn len(&self) -> usize {
        self.size
    }

    /// Returns `true` if the bag is empty.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.size == 0
    }

    /// Returns a list of unique elements in the bag.
    pub fn list(&self) -> Vec<T> {
        self.counts.keys().cloned().collect()
    }

    /// Returns true if this bag contains the same elements with the same counts as the other bag.
    pub fn equals(&self, other: &Self) -> bool {
        self.size == other.size && self.counts == other.counts
    }

    /// Returns the most common element and its count.
    ///
    /// If there's a tie, any of the tied elements may be returned.
    /// Returns `None` if the bag is empty.
    pub fn mode(&self) -> Option<(T, usize)> {
        self.counts
            .iter()
            .max_by_key(|(_, &count)| count)
            .map(|(item, &count)| (item.clone(), count))
    }

    /// Returns the set of elements that have met the threshold.
    #[must_use]
    pub fn threshold(&self) -> &Set<T> {
        &self.met_threshold
    }

    /// Returns a new bag containing only elements that satisfy the predicate.
    pub fn filter<F: Fn(&T) -> bool>(&self, predicate: F) -> Self {
        let mut new_bag = Self::new();
        for (item, &count) in &self.counts {
            if predicate(item) {
                new_bag.add_count(item.clone(), count);
            }
        }
        new_bag
    }

    /// Splits the bag into two bags based on a predicate.
    ///
    /// Returns a tuple where:
    /// - The first bag contains elements where the predicate returns `false`
    /// - The second bag contains elements where the predicate returns `true`
    pub fn split<F: Fn(&T) -> bool>(&self, predicate: F) -> (Self, Self) {
        let mut false_bag = Self::new();
        let mut true_bag = Self::new();

        for (item, &count) in &self.counts {
            if predicate(item) {
                true_bag.add_count(item.clone(), count);
            } else {
                false_bag.add_count(item.clone(), count);
            }
        }

        (false_bag, true_bag)
    }

    /// Removes all instances of an element from the bag.
    pub fn remove(&mut self, item: &T) {
        if let Some(count) = self.counts.remove(item) {
            self.size -= count;
            self.met_threshold.remove(item);
        }
    }

    /// Creates a clone of this bag.
    pub fn duplicate(&self) -> Self {
        self.clone()
    }
}

impl<T: Eq + Hash + Clone + fmt::Debug> fmt::Debug for Bag<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "Bag(size={}): {{", self.size)?;
        let mut first = true;
        for (item, count) in &self.counts {
            if !first {
                write!(f, ", ")?;
            }
            write!(f, "{:?}: {}", item, count)?;
            first = false;
        }
        write!(f, "}}")
    }
}

impl<T: Eq + Hash + Clone + fmt::Display> fmt::Display for Bag<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        writeln!(f, "Bag (Size = {}):", self.size)?;
        for (item, count) in &self.counts {
            writeln!(f, "    {}: {}", item, count)?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new() {
        let bag: Bag<i32> = Bag::new();
        assert!(bag.is_empty());
        assert_eq!(bag.len(), 0);
    }

    #[test]
    fn test_add() {
        let mut bag = Bag::new();
        bag.add(1);
        bag.add(1);
        bag.add(2);

        assert_eq!(bag.count(&1), 2);
        assert_eq!(bag.count(&2), 1);
        assert_eq!(bag.count(&3), 0);
        assert_eq!(bag.len(), 3);
    }

    #[test]
    fn test_add_count() {
        let mut bag = Bag::new();
        bag.add_count(1, 5);
        bag.add_count(2, 3);

        assert_eq!(bag.count(&1), 5);
        assert_eq!(bag.count(&2), 3);
        assert_eq!(bag.len(), 8);
    }

    #[test]
    fn test_add_count_zero() {
        let mut bag = Bag::new();
        bag.add_count(1, 0);
        assert!(bag.is_empty());
    }

    #[test]
    fn test_of() {
        let bag = Bag::of(vec![1, 2, 2, 3, 3, 3]);
        assert_eq!(bag.count(&1), 1);
        assert_eq!(bag.count(&2), 2);
        assert_eq!(bag.count(&3), 3);
        assert_eq!(bag.len(), 6);
    }

    #[test]
    fn test_threshold() {
        let mut bag = Bag::new();
        bag.add_count(1, 5);
        bag.add_count(2, 3);
        bag.add_count(3, 1);

        bag.set_threshold(3);
        let met = bag.threshold();

        assert!(met.contains(&1));
        assert!(met.contains(&2));
        assert!(!met.contains(&3));
    }

    #[test]
    fn test_threshold_update() {
        let mut bag = Bag::new();
        bag.set_threshold(2);
        bag.add(1);
        assert!(!bag.threshold().contains(&1));
        bag.add(1);
        assert!(bag.threshold().contains(&1));
    }

    #[test]
    fn test_mode() {
        let bag = Bag::of(vec![1, 2, 2, 3, 3, 3]);
        let (mode, count) = bag.mode().unwrap();
        assert_eq!(mode, 3);
        assert_eq!(count, 3);
    }

    #[test]
    fn test_mode_empty() {
        let bag: Bag<i32> = Bag::new();
        assert!(bag.mode().is_none());
    }

    #[test]
    fn test_filter() {
        let bag = Bag::of(vec![1, 2, 3, 4, 5]);
        let even = bag.filter(|&x| x % 2 == 0);
        assert_eq!(even.len(), 2);
        assert_eq!(even.count(&2), 1);
        assert_eq!(even.count(&4), 1);
    }

    #[test]
    fn test_split() {
        let bag = Bag::of(vec![1, 2, 3, 4, 5]);
        let (odd, even) = bag.split(|&x| x % 2 == 0);
        assert_eq!(odd.len(), 3);
        assert_eq!(even.len(), 2);
    }

    #[test]
    fn test_remove() {
        let mut bag = Bag::of(vec![1, 1, 2, 2, 2]);
        bag.remove(&2);
        assert_eq!(bag.len(), 2);
        assert_eq!(bag.count(&2), 0);
    }

    #[test]
    fn test_list() {
        let bag = Bag::of(vec![1, 2, 2, 3, 3, 3]);
        let list = bag.list();
        assert_eq!(list.len(), 3);
    }

    #[test]
    fn test_equals() {
        let bag1 = Bag::of(vec![1, 2, 2]);
        let bag2 = Bag::of(vec![1, 2, 2]);
        let bag3 = Bag::of(vec![1, 2, 3]);

        assert!(bag1.equals(&bag2));
        assert!(!bag1.equals(&bag3));
    }
}
