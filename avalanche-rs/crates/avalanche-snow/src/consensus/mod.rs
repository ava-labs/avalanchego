//! Consensus protocols.

pub mod snowball;
pub mod snowman;

use std::collections::HashMap;

use avalanche_ids::Id;

use crate::{Parameters, Result};

/// A choice that can be decided upon.
pub trait Decidable: Send + Sync {
    /// Returns the unique identifier for this choice.
    fn id(&self) -> Id;

    /// Returns the status of this choice.
    fn status(&self) -> Status;

    /// Accepts this choice as finalized.
    fn accept(&mut self) -> Result<()>;

    /// Rejects this choice.
    fn reject(&mut self) -> Result<()>;
}

/// Status of a decidable item.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Status {
    /// Being processed
    Processing,
    /// Accepted and finalized
    Accepted,
    /// Rejected
    Rejected,
    /// Unknown status
    Unknown,
}

impl Status {
    /// Returns true if decided (accepted or rejected).
    pub fn decided(&self) -> bool {
        matches!(self, Status::Accepted | Status::Rejected)
    }

    /// Returns true if the item was accepted.
    pub fn accepted(&self) -> bool {
        matches!(self, Status::Accepted)
    }
}

/// A bag of IDs with counts.
#[derive(Debug, Clone, Default)]
pub struct Bag {
    counts: HashMap<Id, usize>,
    size: usize,
}

impl Bag {
    /// Creates a new empty bag.
    pub fn new() -> Self {
        Self::default()
    }

    /// Adds an ID to the bag.
    pub fn add(&mut self, id: Id) {
        *self.counts.entry(id).or_insert(0) += 1;
        self.size += 1;
    }

    /// Adds an ID multiple times.
    pub fn add_count(&mut self, id: Id, count: usize) {
        *self.counts.entry(id).or_insert(0) += count;
        self.size += count;
    }

    /// Returns the count for an ID.
    pub fn count(&self, id: &Id) -> usize {
        self.counts.get(id).copied().unwrap_or(0)
    }

    /// Returns the total number of items.
    pub fn len(&self) -> usize {
        self.size
    }

    /// Returns true if empty.
    pub fn is_empty(&self) -> bool {
        self.size == 0
    }

    /// Returns the mode (most common ID).
    pub fn mode(&self) -> Option<(Id, usize)> {
        self.counts
            .iter()
            .max_by_key(|(_, count)| *count)
            .map(|(id, count)| (*id, *count))
    }

    /// Returns all IDs and their counts.
    pub fn iter(&self) -> impl Iterator<Item = (&Id, &usize)> {
        self.counts.iter()
    }
}

/// Core consensus trait.
pub trait Consensus: Send + Sync {
    /// Initialize consensus with parameters.
    fn initialize(&mut self, params: Parameters) -> Result<()>;

    /// Add a new choice to be decided.
    fn add(&mut self, id: Id) -> Result<()>;

    /// Record the results of a poll.
    /// Returns true if consensus state changed.
    fn record_poll(&mut self, votes: &Bag) -> Result<bool>;

    /// Returns true if consensus has finalized.
    fn finalized(&self) -> bool;

    /// Returns the current preference.
    fn preference(&self) -> Option<Id>;

    /// Returns the number of successful consecutive polls.
    fn num_successful_polls(&self) -> u64;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_bag() {
        let mut bag = Bag::new();
        let id1 = Id::from_slice(&[1; 32]).unwrap();
        let id2 = Id::from_slice(&[2; 32]).unwrap();

        bag.add(id1);
        bag.add(id1);
        bag.add(id2);

        assert_eq!(bag.len(), 3);
        assert_eq!(bag.count(&id1), 2);
        assert_eq!(bag.count(&id2), 1);
        assert_eq!(bag.mode(), Some((id1, 2)));
    }

    #[test]
    fn test_status() {
        assert!(!Status::Processing.decided());
        assert!(Status::Accepted.decided());
        assert!(Status::Rejected.decided());
        assert!(Status::Accepted.accepted());
        assert!(!Status::Rejected.accepted());
    }
}
