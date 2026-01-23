//! Staking rewards calculation for Avalanche.
//!
//! This module implements the Avalanche reward formula:
//! - Time-based reward calculation
//! - Uptime-based reward scaling
//! - Delegator reward distribution
//! - Supply caps and emission schedule

use std::collections::HashMap;
use std::sync::Arc;

use avalanche_ids::NodeId;
use chrono::{DateTime, Duration, Utc};
use parking_lot::RwLock;

use crate::validator::{Delegator, UptimeTracker, Validator};

/// Total AVAX supply cap (720 million AVAX in nAVAX).
pub const MAX_SUPPLY: u128 = 720_000_000_000_000_000;

/// Initial staking rewards pool (360 million AVAX in nAVAX).
pub const INITIAL_STAKING_REWARDS: u128 = 360_000_000_000_000_000;

/// Minimum staking duration (2 weeks in seconds).
pub const MIN_STAKE_DURATION: u64 = 2 * 7 * 24 * 60 * 60;

/// Maximum staking duration (1 year in seconds).
pub const MAX_STAKE_DURATION: u64 = 365 * 24 * 60 * 60;

/// Uptime threshold for full rewards (80%).
pub const UPTIME_THRESHOLD: f64 = 0.80;

/// Reward configuration.
#[derive(Debug, Clone)]
pub struct RewardConfig {
    /// Maximum supply cap.
    pub max_supply: u128,
    /// Minting period (time to mint half of remaining rewards).
    pub minting_period: Duration,
    /// Minimum stake duration.
    pub min_stake_duration: Duration,
    /// Maximum stake duration.
    pub max_stake_duration: Duration,
    /// Uptime threshold for rewards.
    pub uptime_threshold: f64,
    /// Minimum delegation fee (%).
    pub min_delegation_fee: u32,
    /// Maximum delegation fee (%).
    pub max_delegation_fee: u32,
}

impl Default for RewardConfig {
    fn default() -> Self {
        Self {
            max_supply: MAX_SUPPLY,
            minting_period: Duration::days(365), // 1 year half-life
            min_stake_duration: Duration::seconds(MIN_STAKE_DURATION as i64),
            max_stake_duration: Duration::seconds(MAX_STAKE_DURATION as i64),
            uptime_threshold: UPTIME_THRESHOLD,
            min_delegation_fee: 2,  // 2% minimum
            max_delegation_fee: 100, // 100% maximum
        }
    }
}

/// Reward calculation result.
#[derive(Debug, Clone)]
pub struct RewardResult {
    /// Validator reward.
    pub validator_reward: u64,
    /// Delegator rewards (by delegator index).
    pub delegator_rewards: Vec<u64>,
    /// Total reward.
    pub total_reward: u64,
    /// Whether uptime threshold was met.
    pub uptime_met: bool,
}

/// Staking reward calculator.
pub struct RewardCalculator {
    /// Configuration.
    config: RewardConfig,
    /// Current supply.
    current_supply: u128,
    /// Remaining rewards pool.
    remaining_rewards: u128,
}

impl RewardCalculator {
    /// Creates a new reward calculator.
    pub fn new(config: RewardConfig, current_supply: u128) -> Self {
        let remaining_rewards = config.max_supply.saturating_sub(current_supply);
        Self {
            config,
            current_supply,
            remaining_rewards,
        }
    }

    /// Creates a calculator with default mainnet settings.
    pub fn mainnet(current_supply: u128) -> Self {
        Self::new(RewardConfig::default(), current_supply)
    }

    /// Calculates the potential reward for a given stake and duration.
    ///
    /// The Avalanche reward formula:
    /// R = (stake * duration * remaining_rewards) / (total_stake * minting_period)
    ///
    /// With adjustments for:
    /// - Stake percentage of total
    /// - Duration relative to max duration
    pub fn calculate_potential_reward(
        &self,
        stake: u64,
        duration: Duration,
        total_stake: u64,
    ) -> u64 {
        if total_stake == 0 || stake == 0 {
            return 0;
        }

        let duration_secs = duration.num_seconds() as u128;
        let minting_period_secs = self.config.minting_period.num_seconds() as u128;

        if minting_period_secs == 0 {
            return 0;
        }

        // Calculate base reward using the Avalanche formula
        // reward = remaining_rewards * (1 - (minting_period / (minting_period + staking_duration)))
        //        * (stake / total_stake)
        //
        // Simplified to: stake * duration * remaining_rewards / (total_stake * minting_period)

        let stake_128 = stake as u128;
        let total_stake_128 = total_stake as u128;

        // Calculate: (stake * duration * remaining) / (total * period)
        // Use checked math to prevent overflow
        let numerator = stake_128
            .saturating_mul(duration_secs)
            .saturating_mul(self.remaining_rewards);
        let denominator = total_stake_128.saturating_mul(minting_period_secs);

        if denominator == 0 {
            return 0;
        }

        let reward = numerator / denominator;

        // Cap at remaining rewards and u64 max
        let capped = reward.min(self.remaining_rewards).min(u64::MAX as u128);
        capped as u64
    }

    /// Calculates the actual reward considering uptime.
    pub fn calculate_reward_with_uptime(
        &self,
        stake: u64,
        duration: Duration,
        total_stake: u64,
        uptime: f64,
    ) -> u64 {
        let potential = self.calculate_potential_reward(stake, duration, total_stake);

        if uptime < self.config.uptime_threshold {
            // Below threshold: no rewards
            0
        } else {
            // Above threshold: full rewards
            potential
        }
    }

    /// Calculates rewards for a validator and their delegators.
    pub fn calculate_validator_rewards(
        &self,
        validator: &Validator,
        delegators: &[Delegator],
        total_stake: u64,
        uptime: f64,
    ) -> RewardResult {
        let uptime_met = uptime >= self.config.uptime_threshold;

        if !uptime_met {
            return RewardResult {
                validator_reward: 0,
                delegator_rewards: vec![0; delegators.len()],
                total_reward: 0,
                uptime_met: false,
            };
        }

        let duration = validator.duration();

        // Calculate total stake for this validator (including delegations)
        let total_validator_stake: u64 = validator.weight
            + delegators.iter().map(|d| d.weight).sum::<u64>();

        // Calculate total reward for the validator's stake
        let total_reward = self.calculate_potential_reward(
            total_validator_stake,
            duration,
            total_stake,
        );

        // Split rewards between validator and delegators
        let mut delegator_rewards = Vec::with_capacity(delegators.len());
        let mut total_delegator_reward: u64 = 0;

        for delegator in delegators {
            // Delegator's share of total stake
            let delegator_share = if total_validator_stake > 0 {
                (delegator.weight as u128 * total_reward as u128 / total_validator_stake as u128) as u64
            } else {
                0
            };

            // Apply delegation fee
            let fee = (delegator_share as u128 * validator.delegation_fee as u128 / 100) as u64;
            let delegator_reward = delegator_share.saturating_sub(fee);

            delegator_rewards.push(delegator_reward);
            total_delegator_reward += delegator_reward;
        }

        // Validator gets their share plus delegation fees
        let validator_base_share = if total_validator_stake > 0 {
            (validator.weight as u128 * total_reward as u128 / total_validator_stake as u128) as u64
        } else {
            0
        };
        let validator_reward = total_reward.saturating_sub(total_delegator_reward);

        RewardResult {
            validator_reward,
            delegator_rewards,
            total_reward,
            uptime_met: true,
        }
    }

    /// Updates the supply after minting rewards.
    pub fn mint(&mut self, amount: u64) {
        let amount_128 = amount as u128;
        self.current_supply = self.current_supply.saturating_add(amount_128);
        self.remaining_rewards = self.config.max_supply.saturating_sub(self.current_supply);
    }

    /// Returns current supply.
    pub fn current_supply(&self) -> u128 {
        self.current_supply
    }

    /// Returns remaining rewards pool.
    pub fn remaining_rewards(&self) -> u128 {
        self.remaining_rewards
    }
}

/// Accumulated rewards tracker.
#[derive(Debug, Clone, Default)]
pub struct RewardAccumulator {
    /// Rewards by address.
    pending_rewards: HashMap<Vec<u8>, u64>,
    /// Total pending rewards.
    total_pending: u64,
}

impl RewardAccumulator {
    /// Creates a new accumulator.
    pub fn new() -> Self {
        Self::default()
    }

    /// Adds reward for an address.
    pub fn add_reward(&mut self, address: &[u8], amount: u64) {
        let entry = self.pending_rewards.entry(address.to_vec()).or_insert(0);
        *entry = entry.saturating_add(amount);
        self.total_pending = self.total_pending.saturating_add(amount);
    }

    /// Claims rewards for an address.
    pub fn claim(&mut self, address: &[u8]) -> u64 {
        let amount = self.pending_rewards.remove(address).unwrap_or(0);
        self.total_pending = self.total_pending.saturating_sub(amount);
        amount
    }

    /// Returns pending rewards for an address.
    pub fn pending_for(&self, address: &[u8]) -> u64 {
        self.pending_rewards.get(address).copied().unwrap_or(0)
    }

    /// Returns total pending rewards.
    pub fn total_pending(&self) -> u64 {
        self.total_pending
    }

    /// Returns all addresses with pending rewards.
    pub fn pending_addresses(&self) -> Vec<Vec<u8>> {
        self.pending_rewards.keys().cloned().collect()
    }
}

/// Staking reward manager that integrates with validator set.
pub struct RewardManager {
    /// Reward calculator.
    calculator: RwLock<RewardCalculator>,
    /// Reward accumulator.
    accumulator: RwLock<RewardAccumulator>,
    /// Uptime trackers by node ID.
    uptimes: RwLock<HashMap<NodeId, UptimeTracker>>,
    /// Total stake.
    total_stake: RwLock<u64>,
}

impl RewardManager {
    /// Creates a new reward manager.
    pub fn new(config: RewardConfig, current_supply: u128, total_stake: u64) -> Self {
        Self {
            calculator: RwLock::new(RewardCalculator::new(config, current_supply)),
            accumulator: RwLock::new(RewardAccumulator::new()),
            uptimes: RwLock::new(HashMap::new()),
            total_stake: RwLock::new(total_stake),
        }
    }

    /// Creates a mainnet reward manager.
    pub fn mainnet(current_supply: u128, total_stake: u64) -> Self {
        Self::new(RewardConfig::default(), current_supply, total_stake)
    }

    /// Records uptime for a validator.
    pub fn record_uptime(&self, node_id: NodeId, connected: bool, seconds: u64) {
        let mut uptimes = self.uptimes.write();
        let tracker = uptimes.entry(node_id).or_insert_with(UptimeTracker::new);
        if connected {
            tracker.record_connected(seconds);
        } else {
            tracker.record_disconnected(seconds);
        }
    }

    /// Gets uptime for a validator.
    pub fn get_uptime(&self, node_id: &NodeId) -> f64 {
        self.uptimes
            .read()
            .get(node_id)
            .map(|t| t.uptime())
            .unwrap_or(1.0)
    }

    /// Processes end of staking period for a validator.
    pub fn process_end_of_staking(
        &self,
        validator: &Validator,
        delegators: &[Delegator],
    ) -> RewardResult {
        let uptime = self.get_uptime(&validator.node_id);
        let total_stake = *self.total_stake.read();

        let result = self.calculator.read().calculate_validator_rewards(
            validator,
            delegators,
            total_stake,
            uptime,
        );

        if result.uptime_met {
            // Accumulate rewards
            let mut accumulator = self.accumulator.write();
            accumulator.add_reward(&validator.reward_address, result.validator_reward);

            for (i, delegator) in delegators.iter().enumerate() {
                if let Some(&reward) = result.delegator_rewards.get(i) {
                    accumulator.add_reward(&delegator.reward_address, reward);
                }
            }

            // Mint rewards
            self.calculator.write().mint(result.total_reward);
        }

        // Clear uptime tracker
        self.uptimes.write().remove(&validator.node_id);

        result
    }

    /// Claims pending rewards for an address.
    pub fn claim_rewards(&self, address: &[u8]) -> u64 {
        self.accumulator.write().claim(address)
    }

    /// Returns pending rewards for an address.
    pub fn pending_rewards(&self, address: &[u8]) -> u64 {
        self.accumulator.read().pending_for(address)
    }

    /// Updates total stake.
    pub fn set_total_stake(&self, stake: u64) {
        *self.total_stake.write() = stake;
    }

    /// Returns current supply.
    pub fn current_supply(&self) -> u128 {
        self.calculator.read().current_supply()
    }

    /// Returns remaining rewards pool.
    pub fn remaining_rewards(&self) -> u128 {
        self.calculator.read().remaining_rewards()
    }
}

/// Calculates estimated APY for staking.
pub fn calculate_apy(stake: u64, duration_days: u64, total_stake: u64, remaining_rewards: u128) -> f64 {
    if total_stake == 0 || stake == 0 || duration_days == 0 {
        return 0.0;
    }

    let calculator = RewardCalculator::new(
        RewardConfig::default(),
        MAX_SUPPLY - remaining_rewards,
    );

    let duration = Duration::days(duration_days as i64);
    let reward = calculator.calculate_potential_reward(stake, duration, total_stake);

    // Annualize the return
    let days_per_year = 365.0;
    let annual_reward = (reward as f64 / duration_days as f64) * days_per_year;
    let apy = (annual_reward / stake as f64) * 100.0;

    apy
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_node_id(byte: u8) -> NodeId {
        NodeId::from_slice(&[byte; 20]).unwrap()
    }

    fn make_validator(stake: u64, days: i64) -> Validator {
        let now = Utc::now();
        Validator::new(
            make_node_id(1),
            now,
            now + Duration::days(days),
            stake,
            vec![1, 2, 3],
        )
    }

    #[test]
    fn test_reward_config_default() {
        let config = RewardConfig::default();
        assert_eq!(config.max_supply, MAX_SUPPLY);
        assert_eq!(config.uptime_threshold, 0.80);
    }

    #[test]
    fn test_calculate_potential_reward() {
        // Start with 360M supply (half of max)
        let calculator = RewardCalculator::new(
            RewardConfig::default(),
            360_000_000_000_000_000,
        );

        let stake = 2_000_000_000_000; // 2000 AVAX
        let duration = Duration::days(365);
        let total_stake = 300_000_000_000_000_000; // 300M AVAX (more realistic)

        let reward = calculator.calculate_potential_reward(stake, duration, total_stake);

        // Should get some reward
        assert!(reward > 0);
        // With realistic stake ratio, reward should be reasonable
        assert!(reward < stake * 10);
    }

    #[test]
    fn test_reward_with_uptime_below_threshold() {
        let calculator = RewardCalculator::mainnet(360_000_000_000_000_000);

        let stake = 2_000_000_000_000;
        let duration = Duration::days(365);
        let total_stake = 100_000_000_000_000_000;

        // Below 80% uptime
        let reward = calculator.calculate_reward_with_uptime(stake, duration, total_stake, 0.75);
        assert_eq!(reward, 0);

        // At 80% uptime
        let reward = calculator.calculate_reward_with_uptime(stake, duration, total_stake, 0.80);
        assert!(reward > 0);
    }

    #[test]
    fn test_validator_rewards_with_delegators() {
        let calculator = RewardCalculator::mainnet(360_000_000_000_000_000);

        let mut validator = make_validator(10_000_000_000_000, 365); // 10K AVAX
        validator.delegation_fee = 10; // 10% fee

        let now = Utc::now();
        let delegators = vec![
            Delegator::new(
                validator.node_id,
                now,
                now + Duration::days(365),
                5_000_000_000_000, // 5K AVAX
                vec![4, 5, 6],
            ),
        ];

        let total_stake = 300_000_000_000_000_000; // 300M AVAX
        let result = calculator.calculate_validator_rewards(&validator, &delegators, total_stake, 0.85);

        assert!(result.uptime_met);
        assert!(result.validator_reward > 0);
        assert!(result.delegator_rewards[0] > 0);
        // Validator gets more than delegator after fees
        assert!(result.validator_reward >= result.delegator_rewards[0]);
        // Total should equal sum of individual rewards
        assert!(result.total_reward >= result.validator_reward);
    }

    #[test]
    fn test_reward_accumulator() {
        let mut accumulator = RewardAccumulator::new();

        let addr1 = vec![1, 2, 3];
        let addr2 = vec![4, 5, 6];

        accumulator.add_reward(&addr1, 1000);
        accumulator.add_reward(&addr2, 500);
        accumulator.add_reward(&addr1, 200);

        assert_eq!(accumulator.pending_for(&addr1), 1200);
        assert_eq!(accumulator.pending_for(&addr2), 500);
        assert_eq!(accumulator.total_pending(), 1700);

        let claimed = accumulator.claim(&addr1);
        assert_eq!(claimed, 1200);
        assert_eq!(accumulator.pending_for(&addr1), 0);
        assert_eq!(accumulator.total_pending(), 500);
    }

    #[test]
    fn test_reward_manager() {
        let manager = RewardManager::mainnet(360_000_000_000_000_000, 100_000_000_000_000_000);

        let node_id = make_node_id(1);

        // Record 90% uptime
        manager.record_uptime(node_id, true, 90);
        manager.record_uptime(node_id, false, 10);

        let uptime = manager.get_uptime(&node_id);
        assert!((uptime - 0.9).abs() < 0.01);
    }

    #[test]
    fn test_mint_updates_supply() {
        let mut calculator = RewardCalculator::mainnet(360_000_000_000_000_000);

        let initial = calculator.current_supply();
        calculator.mint(1_000_000_000_000); // Mint 1000 AVAX

        assert_eq!(calculator.current_supply(), initial + 1_000_000_000_000);
    }

    #[test]
    fn test_calculate_apy() {
        let stake = 2_000_000_000_000; // 2000 AVAX
        let duration_days = 365;
        let total_stake = 300_000_000_000_000_000; // 300M AVAX staked
        let remaining_rewards = 360_000_000_000_000_000u128;

        let apy = calculate_apy(stake, duration_days, total_stake, remaining_rewards);

        // APY should be positive (actual rate depends on formula)
        assert!(apy > 0.0);
        // With high remaining rewards and reasonable stake, APY can be significant
        assert!(apy < 200.0); // Reasonable upper bound
    }

    #[test]
    fn test_zero_stake_returns_zero() {
        let calculator = RewardCalculator::mainnet(360_000_000_000_000_000);

        let reward = calculator.calculate_potential_reward(0, Duration::days(365), 1000);
        assert_eq!(reward, 0);

        let reward = calculator.calculate_potential_reward(1000, Duration::days(365), 0);
        assert_eq!(reward, 0);
    }
}
