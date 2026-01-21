//! A generic set implementation.

use std::collections::HashSet;
use std::fmt;
use std::hash::Hash;
use std::iter::FromIterator;

use serde::{Deserialize, Deserializer, Serialize, Serializer};

/// A set of unique elements.
///
/// This is a wrapper around `HashSet` with additional convenience methods.
///
/// # Examples
///
/// ```
/// use avalanche_utils::Set;
///
/// let mut set = Set::new();
/// set.add(1);
/// set.add(2);
/// set.add(1); // Duplicate, ignored
///
/// assert_eq!(set.len(), 2);
/// assert!(set.contains(&1));
/// ```
#[derive(Clone, Default)]
pub struct Set<T: Eq + Hash> {
    inner: HashSet<T>,
}

impl<T: Eq + Hash> Set<T> {
    /// Creates a new empty set.
    #[must_use]
    pub fn new() -> Self {
        Self {
            inner: HashSet::new(),
        }
    }

    /// Creates a new set with the specified capacity.
    #[must_use]
    pub fn with_capacity(capacity: usize) -> Self {
        Self {
            inner: HashSet::with_capacity(capacity),
        }
    }

    /// Creates a set from an iterator of elements.
    pub fn of<I: IntoIterator<Item = T>>(iter: I) -> Self {
        Self {
            inner: iter.into_iter().collect(),
        }
    }

    /// Adds an element to the set.
    ///
    /// Returns `true` if the element was newly inserted.
    pub fn add(&mut self, value: T) -> bool {
        self.inner.insert(value)
    }

    /// Adds multiple elements to the set.
    pub fn add_all<I: IntoIterator<Item = T>>(&mut self, iter: I) {
        for item in iter {
            self.inner.insert(item);
        }
    }

    /// Returns `true` if the set contains the element.
    pub fn contains(&self, value: &T) -> bool {
        self.inner.contains(value)
    }

    /// Removes an element from the set.
    ///
    /// Returns `true` if the element was present.
    pub fn remove(&mut self, value: &T) -> bool {
        self.inner.remove(value)
    }

    /// Removes multiple elements from the set.
    pub fn remove_all<'a, I: IntoIterator<Item = &'a T>>(&mut self, iter: I)
    where
        T: 'a,
    {
        for item in iter {
            self.inner.remove(item);
        }
    }

    /// Returns the number of elements in the set.
    #[must_use]
    pub fn len(&self) -> usize {
        self.inner.len()
    }

    /// Returns `true` if the set is empty.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.inner.is_empty()
    }

    /// Removes all elements from the set.
    pub fn clear(&mut self) {
        self.inner.clear();
    }

    /// Returns an iterator over the elements.
    pub fn iter(&self) -> impl Iterator<Item = &T> {
        self.inner.iter()
    }

    /// Adds all elements from another set to this set.
    pub fn union(&mut self, other: &Self)
    where
        T: Clone,
    {
        for item in &other.inner {
            self.inner.insert(item.clone());
        }
    }

    /// Removes all elements that are in the other set.
    pub fn difference(&mut self, other: &Self) {
        self.inner.retain(|x| !other.inner.contains(x));
    }

    /// Returns `true` if this set and the other set have any elements in common.
    pub fn overlaps(&self, other: &Self) -> bool {
        if self.len() <= other.len() {
            self.inner.iter().any(|x| other.inner.contains(x))
        } else {
            other.inner.iter().any(|x| self.inner.contains(x))
        }
    }

    /// Returns `true` if this set and the other set contain the same elements.
    pub fn equals(&self, other: &Self) -> bool {
        self.inner == other.inner
    }

    /// Removes and returns an arbitrary element, if any.
    pub fn pop(&mut self) -> Option<T>
    where
        T: Clone,
    {
        let item = self.inner.iter().next().cloned();
        if let Some(ref i) = item {
            self.inner.remove(i);
        }
        item
    }

    /// Returns an arbitrary element without removing it, if any.
    pub fn peek(&self) -> Option<&T> {
        self.inner.iter().next()
    }

    /// Converts the set to a vector.
    pub fn to_vec(&self) -> Vec<T>
    where
        T: Clone,
    {
        self.inner.iter().cloned().collect()
    }
}

impl<T: Eq + Hash> FromIterator<T> for Set<T> {
    fn from_iter<I: IntoIterator<Item = T>>(iter: I) -> Self {
        Self::of(iter)
    }
}

impl<T: Eq + Hash> IntoIterator for Set<T> {
    type Item = T;
    type IntoIter = std::collections::hash_set::IntoIter<T>;

    fn into_iter(self) -> Self::IntoIter {
        self.inner.into_iter()
    }
}

impl<'a, T: Eq + Hash> IntoIterator for &'a Set<T> {
    type Item = &'a T;
    type IntoIter = std::collections::hash_set::Iter<'a, T>;

    fn into_iter(self) -> Self::IntoIter {
        self.inner.iter()
    }
}

impl<T: Eq + Hash + fmt::Debug> fmt::Debug for Set<T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_set().entries(self.inner.iter()).finish()
    }
}

impl<T: Eq + Hash> PartialEq for Set<T> {
    fn eq(&self, other: &Self) -> bool {
        self.inner == other.inner
    }
}

impl<T: Eq + Hash> Eq for Set<T> {}

impl<T: Eq + Hash + Serialize> Serialize for Set<T> {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        self.inner.serialize(serializer)
    }
}

impl<'de, T: Eq + Hash + Deserialize<'de>> Deserialize<'de> for Set<T> {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let inner = HashSet::deserialize(deserializer)?;
        Ok(Self { inner })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new() {
        let set: Set<i32> = Set::new();
        assert!(set.is_empty());
        assert_eq!(set.len(), 0);
    }

    #[test]
    fn test_add() {
        let mut set = Set::new();
        assert!(set.add(1));
        assert!(!set.add(1)); // Duplicate
        assert_eq!(set.len(), 1);
    }

    #[test]
    fn test_contains() {
        let mut set = Set::new();
        set.add(1);
        assert!(set.contains(&1));
        assert!(!set.contains(&2));
    }

    #[test]
    fn test_remove() {
        let mut set = Set::new();
        set.add(1);
        assert!(set.remove(&1));
        assert!(!set.remove(&1));
        assert!(set.is_empty());
    }

    #[test]
    fn test_of() {
        let set = Set::of(vec![1, 2, 3, 1]);
        assert_eq!(set.len(), 3);
    }

    #[test]
    fn test_union() {
        let mut set1 = Set::of(vec![1, 2]);
        let set2 = Set::of(vec![2, 3]);
        set1.union(&set2);
        assert_eq!(set1.len(), 3);
        assert!(set1.contains(&1));
        assert!(set1.contains(&2));
        assert!(set1.contains(&3));
    }

    #[test]
    fn test_difference() {
        let mut set1 = Set::of(vec![1, 2, 3]);
        let set2 = Set::of(vec![2, 3, 4]);
        set1.difference(&set2);
        assert_eq!(set1.len(), 1);
        assert!(set1.contains(&1));
    }

    #[test]
    fn test_overlaps() {
        let set1 = Set::of(vec![1, 2]);
        let set2 = Set::of(vec![2, 3]);
        let set3 = Set::of(vec![4, 5]);

        assert!(set1.overlaps(&set2));
        assert!(!set1.overlaps(&set3));
    }

    #[test]
    fn test_pop() {
        let mut set = Set::of(vec![1]);
        let item = set.pop();
        assert_eq!(item, Some(1));
        assert!(set.is_empty());
        assert!(set.pop().is_none());
    }

    #[test]
    fn test_clear() {
        let mut set = Set::of(vec![1, 2, 3]);
        set.clear();
        assert!(set.is_empty());
    }

    #[test]
    fn test_json_serialization() {
        let set = Set::of(vec![1, 2, 3]);
        let json = serde_json::to_string(&set).unwrap();
        let parsed: Set<i32> = serde_json::from_str(&json).unwrap();
        assert!(set.equals(&parsed));
    }
}
