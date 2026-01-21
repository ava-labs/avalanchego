//! Snowball consensus implementation.
//!
//! Snowball is the core consensus algorithm that forms the foundation
//! of the Avalanche consensus family. It's a binary consensus protocol
//! that achieves agreement through repeated random sampling.

use std::collections::{HashMap, HashSet};

use avalanche_ids::Id;

use super::{Bag, Consensus};
use crate::{ConsensusError, Parameters, Result};

/// Snowball consensus for binary decisions.
///
/// Snowball tracks multiple choices and converges to a single
/// preference through repeated polling of validators.
#[derive(Debug)]
pub struct Snowball {
    params: Parameters,
    /// All known choices
    choices: HashSet<Id>,
    /// Confidence counter for each choice
    confidence: HashMap<Id, usize>,
    /// Last successful vote count for each choice
    last_votes: HashMap<Id, usize>,
    /// Current preference
    preference: Option<Id>,
    /// Consecutive successful polls for current preference
    consecutive_successes: u64,
    /// Whether consensus has been reached
    finalized: bool,
    /// The finalized choice
    accepted: Option<Id>,
}

impl Snowball {
    /// Creates a new Snowball instance.
    pub fn new(params: Parameters) -> Self {
        Self {
            params,
            choices: HashSet::new(),
            confidence: HashMap::new(),
            last_votes: HashMap::new(),
            preference: None,
            consecutive_successes: 0,
            finalized: false,
            accepted: None,
        }
    }

    /// Returns the accepted choice if finalized.
    pub fn accepted(&self) -> Option<Id> {
        self.accepted
    }

    /// Returns all tracked choices.
    pub fn choices(&self) -> &HashSet<Id> {
        &self.choices
    }

    /// Returns the confidence for a choice.
    pub fn confidence(&self, id: &Id) -> usize {
        self.confidence.get(id).copied().unwrap_or(0)
    }

    fn update_preference(&mut self, id: Id, votes: usize) {
        // Update confidence based on votes
        if votes >= self.params.alpha {
            // Increment confidence for this choice
            let new_conf = {
                let conf = self.confidence.entry(id).or_insert(0);
                *conf += 1;
                *conf
            };

            // Update preference if this choice has higher confidence
            let should_update = self.preference.map_or(true, |pref| {
                let pref_conf = self.confidence.get(&pref).copied().unwrap_or(0);
                new_conf > pref_conf
            });

            if should_update {
                self.preference = Some(id);
            }
        }
    }

    fn check_finalization(&mut self) -> bool {
        if self.finalized {
            return false;
        }

        // Check if we've reached beta threshold
        let beta = if self.choices.len() == 1 {
            self.params.beta_virtuous
        } else {
            self.params.beta_rogue
        };

        if self.consecutive_successes as usize >= beta {
            self.finalized = true;
            self.accepted = self.preference;
            return true;
        }

        false
    }
}

impl Consensus for Snowball {
    fn initialize(&mut self, params: Parameters) -> Result<()> {
        params.validate().map_err(ConsensusError::InvalidParameters)?;
        self.params = params;
        Ok(())
    }

    fn add(&mut self, id: Id) -> Result<()> {
        if self.finalized {
            return Err(ConsensusError::AlreadyFinalized);
        }

        if self.choices.insert(id) {
            self.confidence.insert(id, 0);
            self.last_votes.insert(id, 0);

            // Set preference if this is the first choice
            if self.preference.is_none() {
                self.preference = Some(id);
            }
        }

        Ok(())
    }

    fn record_poll(&mut self, votes: &Bag) -> Result<bool> {
        if self.finalized {
            return Ok(false);
        }

        if votes.is_empty() {
            self.consecutive_successes = 0;
            return Ok(false);
        }

        // Find the choice with the most votes
        let (winner, winner_votes) = match votes.mode() {
            Some(m) => m,
            None => {
                self.consecutive_successes = 0;
                return Ok(false);
            }
        };

        // Check if winner is a known choice
        if !self.choices.contains(&winner) {
            self.consecutive_successes = 0;
            return Ok(false);
        }

        // Update state based on poll results
        let successful = winner_votes >= self.params.alpha;

        if successful {
            self.update_preference(winner, winner_votes);

            // Check if preference matches winner
            if self.preference == Some(winner) {
                self.consecutive_successes += 1;
            } else {
                self.consecutive_successes = 1;
                self.preference = Some(winner);
            }
        } else {
            self.consecutive_successes = 0;
        }

        // Update last votes
        for id in &self.choices {
            self.last_votes.insert(*id, votes.count(id));
        }

        // Check if we've finalized
        let changed = self.check_finalization();
        Ok(changed || successful)
    }

    fn finalized(&self) -> bool {
        self.finalized
    }

    fn preference(&self) -> Option<Id> {
        self.preference
    }

    fn num_successful_polls(&self) -> u64 {
        self.consecutive_successes
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_id(byte: u8) -> Id {
        Id::from_slice(&[byte; 32]).unwrap()
    }

    #[test]
    fn test_snowball_single_choice() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowball = Snowball::new(params);

        let id = make_id(1);
        snowball.add(id).unwrap();

        assert_eq!(snowball.preference(), Some(id));
        assert!(!snowball.finalized());

        // Successful polls
        for _ in 0..3 {
            let mut bag = Bag::new();
            bag.add_count(id, 4);
            snowball.record_poll(&bag).unwrap();
        }

        assert!(snowball.finalized());
        assert_eq!(snowball.accepted(), Some(id));
    }

    #[test]
    fn test_snowball_two_choices() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowball = Snowball::new(params);

        let id1 = make_id(1);
        let id2 = make_id(2);
        snowball.add(id1).unwrap();
        snowball.add(id2).unwrap();

        // Need beta_rogue (5) successful polls for conflicting choices
        for _ in 0..5 {
            let mut bag = Bag::new();
            bag.add_count(id1, 4);
            bag.add_count(id2, 1);
            snowball.record_poll(&bag).unwrap();
        }

        assert!(snowball.finalized());
        assert_eq!(snowball.accepted(), Some(id1));
    }

    #[test]
    fn test_snowball_reset_on_failure() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowball = Snowball::new(params);

        let id = make_id(1);
        snowball.add(id).unwrap();

        // Two successful polls
        for _ in 0..2 {
            let mut bag = Bag::new();
            bag.add_count(id, 4);
            snowball.record_poll(&bag).unwrap();
        }

        assert_eq!(snowball.num_successful_polls(), 2);
        assert!(!snowball.finalized());

        // Failed poll (not enough votes)
        let mut bag = Bag::new();
        bag.add_count(id, 2);
        snowball.record_poll(&bag).unwrap();

        assert_eq!(snowball.num_successful_polls(), 0);
        assert!(!snowball.finalized());
    }

    #[test]
    fn test_snowball_cannot_add_after_finalized() {
        let params = Parameters::new(5, 4, 3, 5);
        let mut snowball = Snowball::new(params);

        let id1 = make_id(1);
        snowball.add(id1).unwrap();

        // Finalize
        for _ in 0..3 {
            let mut bag = Bag::new();
            bag.add_count(id1, 4);
            snowball.record_poll(&bag).unwrap();
        }

        assert!(snowball.finalized());

        // Cannot add new choice
        let id2 = make_id(2);
        assert!(matches!(
            snowball.add(id2),
            Err(ConsensusError::AlreadyFinalized)
        ));
    }
}
