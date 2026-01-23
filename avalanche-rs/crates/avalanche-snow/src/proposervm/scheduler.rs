//! Proposer scheduling for Snowman++ consensus.
//!
//! Implements windowed block proposal where validators take turns proposing
//! blocks within time slots, with fallback to open proposal after windows expire.

use std::time::Duration;

use sha2::{Digest, Sha256};

use avalanche_ids::{Id, NodeId};
use crate::validators::ValidatorSet;

/// Trait for proposer scheduling.
pub trait ProposerScheduler: Send + Sync {
    /// Gets the proposer for a given height and slot.
    fn get_proposer(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
        slot: u64,
    ) -> Option<NodeId>;

    /// Gets the delay before a node should propose.
    fn get_delay(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
        node_id: NodeId,
    ) -> Duration;

    /// Converts a timestamp to a slot number.
    fn timestamp_to_slot(&self, timestamp: u64) -> u64;
}

/// Window-based proposer scheduler.
///
/// Divides time into windows where specific validators can propose.
/// After all windows expire, anyone can propose (open window).
#[derive(Debug, Clone)]
pub struct WindowScheduler {
    /// Duration of each proposer window.
    window_duration: Duration,
    /// Number of dedicated windows before open proposal.
    num_windows: u64,
    /// Minimum delay between blocks.
    min_block_delay: Duration,
    /// Base timestamp for slot calculation.
    base_timestamp: u64,
}

impl WindowScheduler {
    /// Creates a new window scheduler.
    pub fn new(window_duration: Duration, num_windows: u64, min_block_delay: Duration) -> Self {
        Self {
            window_duration,
            num_windows,
            min_block_delay,
            base_timestamp: 0,
        }
    }

    /// Sets the base timestamp for slot calculation.
    pub fn set_base_timestamp(&mut self, timestamp: u64) {
        self.base_timestamp = timestamp;
    }

    /// Computes a deterministic proposer order for a block height.
    fn compute_proposer_order(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
    ) -> Vec<NodeId> {
        let validator_list = validators.validators();
        if validator_list.is_empty() {
            return vec![];
        }

        // Create deterministic seed from parent and height
        let mut hasher = Sha256::new();
        hasher.update(parent_id.as_bytes());
        hasher.update(height.to_be_bytes());
        let seed = hasher.finalize();

        // Sort validators by hash(seed || validator_id) for deterministic shuffling
        let mut scored_validators: Vec<(NodeId, [u8; 32])> = validator_list
            .iter()
            .map(|v| {
                let mut h = Sha256::new();
                h.update(&seed);
                h.update(v.node_id.as_bytes());
                let score: [u8; 32] = h.finalize().into();
                (v.node_id, score)
            })
            .collect();

        scored_validators.sort_by(|a, b| a.1.cmp(&b.1));
        scored_validators.into_iter().map(|(id, _)| id).collect()
    }

    /// Gets the slot number for a given timestamp.
    fn get_slot(&self, timestamp: u64) -> u64 {
        let elapsed = timestamp.saturating_sub(self.base_timestamp);
        elapsed / self.window_duration.as_secs()
    }
}

impl ProposerScheduler for WindowScheduler {
    fn get_proposer(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
        slot: u64,
    ) -> Option<NodeId> {
        // If we're past all dedicated windows, no specific proposer
        if slot >= self.num_windows {
            return None;
        }

        let order = self.compute_proposer_order(validators, parent_id, height);
        if order.is_empty() {
            return None;
        }

        // Get proposer for this slot
        let index = slot as usize % order.len();
        Some(order[index])
    }

    fn get_delay(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
        node_id: NodeId,
    ) -> Duration {
        let order = self.compute_proposer_order(validators, parent_id, height);
        if order.is_empty() {
            // No validators, delay until open window
            return self.window_duration * self.num_windows as u32;
        }

        // Find our position in the order
        let position = order.iter().position(|&id| id == node_id);

        match position {
            Some(pos) => {
                // We're in the validator set
                let slot = pos as u64;
                if slot < self.num_windows {
                    // We have a dedicated window
                    Duration::from_secs(slot) * self.window_duration.as_secs() as u32
                        + self.min_block_delay
                } else {
                    // We're past dedicated windows, wait for open window
                    self.window_duration * self.num_windows as u32 + self.min_block_delay
                }
            }
            None => {
                // Not a validator, wait for open window
                self.window_duration * self.num_windows as u32 + self.min_block_delay
            }
        }
    }

    fn timestamp_to_slot(&self, timestamp: u64) -> u64 {
        self.get_slot(timestamp)
    }
}

/// Stake-weighted proposer scheduler.
///
/// Like WindowScheduler but weights proposer selection by stake.
#[derive(Debug, Clone)]
pub struct StakeWeightedScheduler {
    /// Base scheduler.
    base: WindowScheduler,
}

impl StakeWeightedScheduler {
    /// Creates a new stake-weighted scheduler.
    pub fn new(window_duration: Duration, num_windows: u64, min_block_delay: Duration) -> Self {
        Self {
            base: WindowScheduler::new(window_duration, num_windows, min_block_delay),
        }
    }

    /// Computes stake-weighted proposer order.
    fn compute_weighted_order(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
    ) -> Vec<NodeId> {
        let validator_list = validators.validators();
        if validator_list.is_empty() {
            return vec![];
        }

        // Create deterministic seed
        let mut hasher = Sha256::new();
        hasher.update(parent_id.as_bytes());
        hasher.update(height.to_be_bytes());
        let seed = hasher.finalize();

        // Weight by stake - validators with more stake appear more often
        let total_stake: u64 = validator_list.iter().map(|v| v.weight).sum();
        if total_stake == 0 {
            return vec![];
        }

        // Create expanded list based on stake weight
        // Use sampling instead of full expansion to avoid memory issues
        let mut sampled: Vec<(NodeId, u64)> = Vec::new();

        for v in &validator_list {
            // Score based on stake and deterministic hash
            let mut h = Sha256::new();
            h.update(&seed);
            h.update(v.node_id.as_bytes());
            let hash: [u8; 32] = h.finalize().into();
            let hash_value = u64::from_be_bytes(hash[0..8].try_into().unwrap());

            // Higher stake = higher chance of lower score (earlier in order)
            // We divide hash by stake to give higher stake lower scores
            let score = hash_value / v.weight.max(1);
            sampled.push((v.node_id, score));
        }

        sampled.sort_by_key(|(_, score)| *score);
        sampled.into_iter().map(|(id, _)| id).collect()
    }
}

impl ProposerScheduler for StakeWeightedScheduler {
    fn get_proposer(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
        slot: u64,
    ) -> Option<NodeId> {
        if slot >= self.base.num_windows {
            return None;
        }

        let order = self.compute_weighted_order(validators, parent_id, height);
        if order.is_empty() {
            return None;
        }

        let index = slot as usize % order.len();
        Some(order[index])
    }

    fn get_delay(
        &self,
        validators: &dyn ValidatorSet,
        parent_id: Id,
        height: u64,
        node_id: NodeId,
    ) -> Duration {
        let order = self.compute_weighted_order(validators, parent_id, height);
        if order.is_empty() {
            return self.base.window_duration * self.base.num_windows as u32;
        }

        let position = order.iter().position(|&id| id == node_id);

        match position {
            Some(pos) => {
                let slot = pos as u64;
                if slot < self.base.num_windows {
                    Duration::from_secs(slot) * self.base.window_duration.as_secs() as u32
                        + self.base.min_block_delay
                } else {
                    self.base.window_duration * self.base.num_windows as u32
                        + self.base.min_block_delay
                }
            }
            None => {
                self.base.window_duration * self.base.num_windows as u32
                    + self.base.min_block_delay
            }
        }
    }

    fn timestamp_to_slot(&self, timestamp: u64) -> u64 {
        self.base.timestamp_to_slot(timestamp)
    }
}

/// Round-robin scheduler (simpler alternative).
#[derive(Debug, Clone)]
pub struct RoundRobinScheduler {
    /// Slot duration.
    slot_duration: Duration,
}

impl RoundRobinScheduler {
    /// Creates a new round-robin scheduler.
    pub fn new(slot_duration: Duration) -> Self {
        Self { slot_duration }
    }
}

impl ProposerScheduler for RoundRobinScheduler {
    fn get_proposer(
        &self,
        validators: &dyn ValidatorSet,
        _parent_id: Id,
        height: u64,
        _slot: u64,
    ) -> Option<NodeId> {
        let validator_list = validators.validators();
        if validator_list.is_empty() {
            return None;
        }

        // Simple round-robin based on height
        let index = height as usize % validator_list.len();
        Some(validator_list[index].node_id)
    }

    fn get_delay(
        &self,
        validators: &dyn ValidatorSet,
        _parent_id: Id,
        height: u64,
        node_id: NodeId,
    ) -> Duration {
        let validator_list = validators.validators();
        if validator_list.is_empty() {
            return Duration::MAX;
        }

        let current_proposer_idx = height as usize % validator_list.len();
        let our_idx = validator_list.iter().position(|v| v.node_id == node_id);

        match our_idx {
            Some(idx) => {
                if idx == current_proposer_idx {
                    Duration::ZERO
                } else {
                    // Calculate how many slots until our turn
                    let slots_until = if idx > current_proposer_idx {
                        idx - current_proposer_idx
                    } else {
                        validator_list.len() - current_proposer_idx + idx
                    };
                    self.slot_duration * slots_until as u32
                }
            }
            None => Duration::MAX,
        }
    }

    fn timestamp_to_slot(&self, timestamp: u64) -> u64 {
        timestamp / self.slot_duration.as_secs()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::validators::{Validator, ValidatorSetError};

    struct MockValidatorSet {
        validators: Vec<Validator>,
    }

    impl ValidatorSet for MockValidatorSet {
        fn validators(&self) -> Vec<Validator> {
            self.validators.clone()
        }

        fn get_validator(&self, node_id: NodeId) -> Option<Validator> {
            self.validators.iter().find(|v| v.node_id == node_id).cloned()
        }

        fn add_validator(&mut self, validator: Validator) -> std::result::Result<(), ValidatorSetError> {
            self.validators.push(validator);
            Ok(())
        }

        fn remove_validator(&mut self, node_id: NodeId) -> std::result::Result<(), ValidatorSetError> {
            self.validators.retain(|v| v.node_id != node_id);
            Ok(())
        }

        fn total_weight(&self) -> u64 {
            self.validators.iter().map(|v| v.weight).sum()
        }

        fn len(&self) -> usize {
            self.validators.len()
        }

        fn contains(&self, node_id: NodeId) -> bool {
            self.validators.iter().any(|v| v.node_id == node_id)
        }
    }

    fn create_validator(id: u8, weight: u64) -> Validator {
        Validator {
            node_id: NodeId::from_bytes([id; 20]),
            weight,
            start_time: 0,
            end_time: u64::MAX,
        }
    }

    #[test]
    fn test_window_scheduler_proposer_order() {
        let scheduler = WindowScheduler::new(
            Duration::from_secs(2),
            6,
            Duration::from_secs(1),
        );

        let validators = MockValidatorSet {
            validators: vec![
                create_validator(1, 100),
                create_validator(2, 100),
                create_validator(3, 100),
            ],
        };

        let parent = Id::from_bytes([0; 32]);

        // Slot 0 should return a proposer
        let proposer0 = scheduler.get_proposer(&validators, parent, 100, 0);
        assert!(proposer0.is_some());

        // Same inputs should give same proposer
        let proposer0_again = scheduler.get_proposer(&validators, parent, 100, 0);
        assert_eq!(proposer0, proposer0_again);

        // Different slots should give different proposers (usually)
        let proposer1 = scheduler.get_proposer(&validators, parent, 100, 1);
        let proposer2 = scheduler.get_proposer(&validators, parent, 100, 2);
        assert!(proposer1.is_some());
        assert!(proposer2.is_some());

        // Slot past num_windows should return None (open window)
        let proposer_open = scheduler.get_proposer(&validators, parent, 100, 10);
        assert!(proposer_open.is_none());
    }

    #[test]
    fn test_window_scheduler_delay() {
        let scheduler = WindowScheduler::new(
            Duration::from_secs(2),
            3,
            Duration::from_secs(1),
        );

        let validators = MockValidatorSet {
            validators: vec![
                create_validator(1, 100),
                create_validator(2, 100),
            ],
        };

        let parent = Id::from_bytes([0; 32]);

        // Get delays for both validators
        let delay1 = scheduler.get_delay(&validators, parent, 100, NodeId::from_bytes([1; 20]));
        let delay2 = scheduler.get_delay(&validators, parent, 100, NodeId::from_bytes([2; 20]));

        // One should have shorter delay than the other
        assert!(delay1 != delay2 || validators.validators.len() == 1);
    }

    #[test]
    fn test_round_robin_scheduler() {
        let scheduler = RoundRobinScheduler::new(Duration::from_secs(2));

        let validators = MockValidatorSet {
            validators: vec![
                create_validator(1, 100),
                create_validator(2, 100),
                create_validator(3, 100),
            ],
        };

        let parent = Id::from_bytes([0; 32]);

        // Height 0 -> validator 0
        // Height 1 -> validator 1
        // Height 2 -> validator 2
        // Height 3 -> validator 0 (wraps)

        let p0 = scheduler.get_proposer(&validators, parent, 0, 0).unwrap();
        let p1 = scheduler.get_proposer(&validators, parent, 1, 0).unwrap();
        let p2 = scheduler.get_proposer(&validators, parent, 2, 0).unwrap();
        let p3 = scheduler.get_proposer(&validators, parent, 3, 0).unwrap();

        assert_eq!(p0, p3); // Wraps around
        assert_ne!(p0, p1);
        assert_ne!(p1, p2);
    }

    #[test]
    fn test_stake_weighted_scheduler() {
        let scheduler = StakeWeightedScheduler::new(
            Duration::from_secs(2),
            6,
            Duration::from_secs(1),
        );

        let validators = MockValidatorSet {
            validators: vec![
                create_validator(1, 1000), // High stake
                create_validator(2, 100),  // Low stake
                create_validator(3, 500),  // Medium stake
            ],
        };

        let parent = Id::from_bytes([0; 32]);

        // Should return proposers
        let p0 = scheduler.get_proposer(&validators, parent, 100, 0);
        assert!(p0.is_some());

        // Past windows should return None
        let p_open = scheduler.get_proposer(&validators, parent, 100, 10);
        assert!(p_open.is_none());
    }

    #[test]
    fn test_empty_validator_set() {
        let scheduler = WindowScheduler::new(
            Duration::from_secs(2),
            6,
            Duration::from_secs(1),
        );

        let validators = MockValidatorSet {
            validators: vec![],
        };

        let parent = Id::from_bytes([0; 32]);

        // No proposer with empty set
        let proposer = scheduler.get_proposer(&validators, parent, 100, 0);
        assert!(proposer.is_none());
    }

    #[test]
    fn test_timestamp_to_slot() {
        let mut scheduler = WindowScheduler::new(
            Duration::from_secs(2),
            6,
            Duration::from_secs(1),
        );
        scheduler.set_base_timestamp(1000);

        assert_eq!(scheduler.timestamp_to_slot(1000), 0);
        assert_eq!(scheduler.timestamp_to_slot(1001), 0);
        assert_eq!(scheduler.timestamp_to_slot(1002), 1);
        assert_eq!(scheduler.timestamp_to_slot(1004), 2);
        assert_eq!(scheduler.timestamp_to_slot(1012), 6);
    }
}
