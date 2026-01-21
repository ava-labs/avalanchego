//! Validator types and management.

use avalanche_ids::NodeId;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// A validator in the primary network or a subnet.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Validator {
    /// Node ID of the validator
    pub node_id: NodeId,
    /// Start time of validation
    pub start_time: DateTime<Utc>,
    /// End time of validation
    pub end_time: DateTime<Utc>,
    /// Stake weight
    pub weight: u64,
    /// Reward address
    pub reward_address: Vec<u8>,
    /// Delegation fee (0-100)
    pub delegation_fee: u32,
    /// BLS public key (optional)
    pub bls_public_key: Option<Vec<u8>>,
}

impl Validator {
    /// Creates a new validator.
    pub fn new(
        node_id: NodeId,
        start_time: DateTime<Utc>,
        end_time: DateTime<Utc>,
        weight: u64,
        reward_address: Vec<u8>,
    ) -> Self {
        Self {
            node_id,
            start_time,
            end_time,
            weight,
            reward_address,
            delegation_fee: 20, // 20% default
            bls_public_key: None,
        }
    }

    /// Returns true if the validator is currently active.
    pub fn is_active(&self, now: DateTime<Utc>) -> bool {
        now >= self.start_time && now < self.end_time
    }

    /// Returns the remaining validation time.
    pub fn remaining_time(&self, now: DateTime<Utc>) -> chrono::Duration {
        if now >= self.end_time {
            chrono::Duration::zero()
        } else {
            self.end_time - now
        }
    }

    /// Returns the validation duration.
    pub fn duration(&self) -> chrono::Duration {
        self.end_time - self.start_time
    }
}

/// A delegator staking to a validator.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Delegator {
    /// Validator being delegated to
    pub validator_node_id: NodeId,
    /// Start time
    pub start_time: DateTime<Utc>,
    /// End time
    pub end_time: DateTime<Utc>,
    /// Delegated stake
    pub weight: u64,
    /// Reward address
    pub reward_address: Vec<u8>,
}

impl Delegator {
    /// Creates a new delegator.
    pub fn new(
        validator_node_id: NodeId,
        start_time: DateTime<Utc>,
        end_time: DateTime<Utc>,
        weight: u64,
        reward_address: Vec<u8>,
    ) -> Self {
        Self {
            validator_node_id,
            start_time,
            end_time,
            weight,
            reward_address,
        }
    }

    /// Returns true if the delegation is currently active.
    pub fn is_active(&self, now: DateTime<Utc>) -> bool {
        now >= self.start_time && now < self.end_time
    }
}

/// Validator uptime tracking.
#[derive(Debug, Clone, Default)]
pub struct UptimeTracker {
    /// Connected time in seconds
    pub connected_time: u64,
    /// Total time observed in seconds
    pub total_time: u64,
}

impl UptimeTracker {
    /// Creates a new uptime tracker.
    pub fn new() -> Self {
        Self::default()
    }

    /// Records connected time.
    pub fn record_connected(&mut self, seconds: u64) {
        self.connected_time += seconds;
        self.total_time += seconds;
    }

    /// Records disconnected time.
    pub fn record_disconnected(&mut self, seconds: u64) {
        self.total_time += seconds;
    }

    /// Returns the uptime percentage (0.0 - 1.0).
    pub fn uptime(&self) -> f64 {
        if self.total_time == 0 {
            1.0
        } else {
            self.connected_time as f64 / self.total_time as f64
        }
    }

    /// Returns true if uptime meets the threshold.
    pub fn meets_threshold(&self, threshold: f64) -> bool {
        self.uptime() >= threshold
    }
}

/// Minimum stake requirements.
pub mod stake {
    /// Minimum stake to become a validator (2000 AVAX)
    pub const MIN_VALIDATOR_STAKE: u64 = 2_000_000_000_000; // nAVAX

    /// Minimum stake to delegate (25 AVAX)
    pub const MIN_DELEGATOR_STAKE: u64 = 25_000_000_000; // nAVAX

    /// Maximum stake multiplier for delegators (5x validator stake)
    pub const MAX_DELEGATION_FACTOR: u64 = 5;

    /// Minimum validation duration (2 weeks)
    pub const MIN_VALIDATION_DURATION_SECS: u64 = 2 * 7 * 24 * 60 * 60;

    /// Maximum validation duration (1 year)
    pub const MAX_VALIDATION_DURATION_SECS: u64 = 365 * 24 * 60 * 60;

    /// Uptime threshold for rewards (80%)
    pub const UPTIME_THRESHOLD: f64 = 0.80;
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_node_id(byte: u8) -> NodeId {
        NodeId::from_slice(&[byte; 20]).unwrap()
    }

    #[test]
    fn test_validator_active() {
        let now = Utc::now();
        let start = now - chrono::Duration::hours(1);
        let end = now + chrono::Duration::hours(1);

        let v = Validator::new(
            make_node_id(1),
            start,
            end,
            1000,
            vec![1, 2, 3],
        );

        assert!(v.is_active(now));
        assert!(!v.is_active(start - chrono::Duration::hours(1)));
        assert!(!v.is_active(end + chrono::Duration::hours(1)));
    }

    #[test]
    fn test_uptime_tracker() {
        let mut tracker = UptimeTracker::new();

        tracker.record_connected(80);
        tracker.record_disconnected(20);

        assert_eq!(tracker.uptime(), 0.8);
        assert!(tracker.meets_threshold(0.8));
        assert!(!tracker.meets_threshold(0.9));
    }

    #[test]
    fn test_delegator() {
        let now = Utc::now();
        let d = Delegator::new(
            make_node_id(1),
            now - chrono::Duration::hours(1),
            now + chrono::Duration::hours(1),
            500,
            vec![],
        );

        assert!(d.is_active(now));
    }
}
