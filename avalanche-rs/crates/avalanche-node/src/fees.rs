//! Dynamic fee handling for Avalanche.
//!
//! Implements:
//! - Base fee calculation (EIP-1559 style for C-Chain)
//! - Priority fees
//! - Fee burning
//! - Congestion-based fee adjustment

use std::collections::VecDeque;
use std::sync::Arc;
use std::time::{Duration, Instant};

use parking_lot::RwLock;

/// Minimum gas price (25 nAVAX).
pub const MIN_GAS_PRICE: u64 = 25_000_000_000;

/// Target gas usage per block (15M).
pub const TARGET_GAS: u64 = 15_000_000;

/// Maximum gas per block (30M).
pub const MAX_GAS: u64 = 30_000_000;

/// Base fee change denominator (8 = 12.5% max change).
pub const BASE_FEE_CHANGE_DENOMINATOR: u64 = 8;

/// Fee configuration.
#[derive(Debug, Clone)]
pub struct FeeConfig {
    /// Minimum base fee.
    pub min_base_fee: u64,
    /// Maximum base fee.
    pub max_base_fee: u64,
    /// Target gas per block.
    pub target_gas: u64,
    /// Maximum gas per block.
    pub max_gas: u64,
    /// Base fee change denominator.
    pub base_fee_change_denominator: u64,
    /// Block gas limit.
    pub block_gas_limit: u64,
    /// Minimum priority fee.
    pub min_priority_fee: u64,
}

impl Default for FeeConfig {
    fn default() -> Self {
        Self {
            min_base_fee: MIN_GAS_PRICE,
            max_base_fee: 1_000_000_000_000, // 1000 gwei
            target_gas: TARGET_GAS,
            max_gas: MAX_GAS,
            base_fee_change_denominator: BASE_FEE_CHANGE_DENOMINATOR,
            block_gas_limit: MAX_GAS,
            min_priority_fee: 0,
        }
    }
}

/// Dynamic fee calculator (EIP-1559 style).
pub struct FeeCalculator {
    /// Configuration.
    config: FeeConfig,
    /// Current base fee.
    base_fee: RwLock<u64>,
    /// Recent block gas usage history.
    gas_history: RwLock<VecDeque<u64>>,
    /// History window size.
    history_size: usize,
}

impl FeeCalculator {
    /// Creates a new fee calculator.
    pub fn new(config: FeeConfig) -> Self {
        Self {
            base_fee: RwLock::new(config.min_base_fee),
            gas_history: RwLock::new(VecDeque::new()),
            history_size: 256,
            config,
        }
    }

    /// Creates with default configuration.
    pub fn default_mainnet() -> Self {
        Self::new(FeeConfig::default())
    }

    /// Gets the current base fee.
    pub fn base_fee(&self) -> u64 {
        *self.base_fee.read()
    }

    /// Sets the base fee directly.
    pub fn set_base_fee(&self, fee: u64) {
        *self.base_fee.write() = fee.clamp(self.config.min_base_fee, self.config.max_base_fee);
    }

    /// Calculates the next base fee based on gas usage.
    pub fn calculate_next_base_fee(&self, gas_used: u64) -> u64 {
        let current = *self.base_fee.read();

        if gas_used == self.config.target_gas {
            return current;
        }

        if gas_used > self.config.target_gas {
            // Increase base fee
            let gas_delta = gas_used - self.config.target_gas;
            let fee_delta = current
                .saturating_mul(gas_delta)
                .saturating_div(self.config.target_gas)
                .saturating_div(self.config.base_fee_change_denominator);
            let new_fee = current.saturating_add(fee_delta.max(1));
            new_fee.min(self.config.max_base_fee)
        } else {
            // Decrease base fee
            let gas_delta = self.config.target_gas - gas_used;
            let fee_delta = current
                .saturating_mul(gas_delta)
                .saturating_div(self.config.target_gas)
                .saturating_div(self.config.base_fee_change_denominator);
            let new_fee = current.saturating_sub(fee_delta);
            new_fee.max(self.config.min_base_fee)
        }
    }

    /// Updates the base fee after a block.
    pub fn update_base_fee(&self, gas_used: u64) {
        let new_fee = self.calculate_next_base_fee(gas_used);
        *self.base_fee.write() = new_fee;

        // Track history
        let mut history = self.gas_history.write();
        history.push_back(gas_used);
        if history.len() > self.history_size {
            history.pop_front();
        }
    }

    /// Calculates the effective gas price for a transaction.
    pub fn effective_gas_price(&self, max_fee_per_gas: u64, max_priority_fee: Option<u64>) -> u64 {
        let base_fee = self.base_fee();
        let priority_fee = max_priority_fee.unwrap_or(0);

        // Effective price = min(max_fee, base_fee + priority_fee)
        let price = base_fee.saturating_add(priority_fee);
        price.min(max_fee_per_gas)
    }

    /// Calculates the priority fee from effective price.
    pub fn priority_fee(&self, effective_price: u64) -> u64 {
        let base_fee = self.base_fee();
        effective_price.saturating_sub(base_fee)
    }

    /// Validates a transaction's gas price.
    pub fn validate_gas_price(&self, max_fee_per_gas: u64) -> Result<(), FeeError> {
        let base_fee = self.base_fee();

        if max_fee_per_gas < base_fee {
            return Err(FeeError::MaxFeeTooLow {
                max_fee: max_fee_per_gas,
                base_fee,
            });
        }

        if max_fee_per_gas < self.config.min_base_fee {
            return Err(FeeError::BelowMinimum {
                min: self.config.min_base_fee,
                got: max_fee_per_gas,
            });
        }

        Ok(())
    }

    /// Estimates gas price for a transaction.
    pub fn suggest_gas_price(&self) -> GasPriceSuggestion {
        let base_fee = self.base_fee();
        let avg_gas = self.average_gas_usage();

        // Suggest priority fee based on congestion
        let congestion_factor = avg_gas as f64 / self.config.target_gas as f64;
        let suggested_priority = if congestion_factor > 1.0 {
            // High congestion: suggest higher priority fee
            (base_fee as f64 * 0.1 * congestion_factor) as u64
        } else {
            // Low congestion: minimal priority fee
            self.config.min_priority_fee
        };

        GasPriceSuggestion {
            base_fee,
            suggested_priority_fee: suggested_priority,
            suggested_max_fee: base_fee.saturating_add(suggested_priority).saturating_mul(2),
            congestion_factor,
        }
    }

    /// Returns average gas usage from history.
    pub fn average_gas_usage(&self) -> u64 {
        let history = self.gas_history.read();
        if history.is_empty() {
            return self.config.target_gas;
        }
        let sum: u64 = history.iter().sum();
        sum / history.len() as u64
    }

    /// Returns the configuration.
    pub fn config(&self) -> &FeeConfig {
        &self.config
    }
}

/// Gas price suggestion.
#[derive(Debug, Clone)]
pub struct GasPriceSuggestion {
    /// Current base fee.
    pub base_fee: u64,
    /// Suggested priority fee.
    pub suggested_priority_fee: u64,
    /// Suggested max fee per gas.
    pub suggested_max_fee: u64,
    /// Congestion factor (1.0 = target utilization).
    pub congestion_factor: f64,
}

/// Fee errors.
#[derive(Debug, Clone, thiserror::Error)]
pub enum FeeError {
    #[error("max fee {max_fee} below base fee {base_fee}")]
    MaxFeeTooLow { max_fee: u64, base_fee: u64 },

    #[error("gas price {got} below minimum {min}")]
    BelowMinimum { min: u64, got: u64 },

    #[error("priority fee exceeds max")]
    PriorityFeeTooHigh,
}

/// Fee burn tracker.
pub struct FeeBurnTracker {
    /// Total burned.
    total_burned: RwLock<u128>,
    /// Burned per block (recent history).
    block_burns: RwLock<VecDeque<(u64, u128)>>, // (block_number, amount)
}

impl FeeBurnTracker {
    /// Creates a new fee burn tracker.
    pub fn new() -> Self {
        Self {
            total_burned: RwLock::new(0),
            block_burns: RwLock::new(VecDeque::new()),
        }
    }

    /// Records a fee burn.
    pub fn record_burn(&self, block_number: u64, amount: u64) {
        *self.total_burned.write() += amount as u128;

        let mut burns = self.block_burns.write();
        burns.push_back((block_number, amount as u128));

        // Keep only last 1000 blocks
        while burns.len() > 1000 {
            burns.pop_front();
        }
    }

    /// Returns total burned.
    pub fn total_burned(&self) -> u128 {
        *self.total_burned.read()
    }

    /// Returns burns for recent blocks.
    pub fn recent_burns(&self, blocks: usize) -> Vec<(u64, u128)> {
        let burns = self.block_burns.read();
        burns.iter().rev().take(blocks).cloned().collect()
    }

    /// Calculates burn rate (per block average).
    pub fn burn_rate(&self) -> u128 {
        let burns = self.block_burns.read();
        if burns.is_empty() {
            return 0;
        }
        let total: u128 = burns.iter().map(|(_, amt)| amt).sum();
        total / burns.len() as u128
    }
}

impl Default for FeeBurnTracker {
    fn default() -> Self {
        Self::new()
    }
}

/// Transaction fee breakdown.
#[derive(Debug, Clone)]
pub struct FeeBreakdown {
    /// Base fee portion (burned).
    pub base_fee_cost: u64,
    /// Priority fee portion (to validator).
    pub priority_fee_cost: u64,
    /// Total fee.
    pub total_fee: u64,
    /// Gas used.
    pub gas_used: u64,
    /// Effective gas price.
    pub effective_gas_price: u64,
}

impl FeeBreakdown {
    /// Creates a fee breakdown for a transaction.
    pub fn calculate(gas_used: u64, base_fee: u64, priority_fee: u64) -> Self {
        let base_fee_cost = gas_used.saturating_mul(base_fee);
        let priority_fee_cost = gas_used.saturating_mul(priority_fee);
        let total_fee = base_fee_cost.saturating_add(priority_fee_cost);

        Self {
            base_fee_cost,
            priority_fee_cost,
            total_fee,
            gas_used,
            effective_gas_price: base_fee.saturating_add(priority_fee),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_fee_config_default() {
        let config = FeeConfig::default();
        assert_eq!(config.min_base_fee, MIN_GAS_PRICE);
        assert_eq!(config.target_gas, TARGET_GAS);
    }

    #[test]
    fn test_fee_calculator_creation() {
        let calc = FeeCalculator::default_mainnet();
        assert_eq!(calc.base_fee(), MIN_GAS_PRICE);
    }

    #[test]
    fn test_base_fee_increase() {
        let calc = FeeCalculator::default_mainnet();
        calc.set_base_fee(MIN_GAS_PRICE);

        // Use more than target gas
        let next = calc.calculate_next_base_fee(TARGET_GAS * 2);
        assert!(next > MIN_GAS_PRICE);
    }

    #[test]
    fn test_base_fee_decrease() {
        let calc = FeeCalculator::default_mainnet();
        calc.set_base_fee(MIN_GAS_PRICE * 2);

        // Use less than target gas
        let next = calc.calculate_next_base_fee(TARGET_GAS / 2);
        assert!(next < MIN_GAS_PRICE * 2);
        assert!(next >= MIN_GAS_PRICE);
    }

    #[test]
    fn test_base_fee_stable_at_target() {
        let calc = FeeCalculator::default_mainnet();
        calc.set_base_fee(MIN_GAS_PRICE * 2);

        let next = calc.calculate_next_base_fee(TARGET_GAS);
        assert_eq!(next, MIN_GAS_PRICE * 2);
    }

    #[test]
    fn test_effective_gas_price() {
        let calc = FeeCalculator::default_mainnet();
        calc.set_base_fee(MIN_GAS_PRICE);

        let priority = MIN_GAS_PRICE / 10;
        let max_fee = MIN_GAS_PRICE * 2;

        let effective = calc.effective_gas_price(max_fee, Some(priority));
        assert_eq!(effective, MIN_GAS_PRICE + priority);
    }

    #[test]
    fn test_effective_gas_price_capped() {
        let calc = FeeCalculator::default_mainnet();
        calc.set_base_fee(MIN_GAS_PRICE);

        let priority = MIN_GAS_PRICE;
        let max_fee = MIN_GAS_PRICE + 1000; // Low cap

        let effective = calc.effective_gas_price(max_fee, Some(priority));
        assert_eq!(effective, max_fee);
    }

    #[test]
    fn test_validate_gas_price() {
        let calc = FeeCalculator::default_mainnet();
        calc.set_base_fee(MIN_GAS_PRICE);

        // Valid price
        assert!(calc.validate_gas_price(MIN_GAS_PRICE * 2).is_ok());

        // Too low
        assert!(matches!(
            calc.validate_gas_price(MIN_GAS_PRICE / 2),
            Err(FeeError::BelowMinimum { .. })
        ));
    }

    #[test]
    fn test_gas_price_suggestion() {
        let calc = FeeCalculator::default_mainnet();
        calc.set_base_fee(MIN_GAS_PRICE);

        let suggestion = calc.suggest_gas_price();
        assert_eq!(suggestion.base_fee, MIN_GAS_PRICE);
        assert!(suggestion.suggested_max_fee >= suggestion.base_fee);
    }

    #[test]
    fn test_fee_burn_tracker() {
        let tracker = FeeBurnTracker::new();

        tracker.record_burn(1, 1000);
        tracker.record_burn(2, 2000);
        tracker.record_burn(3, 1500);

        assert_eq!(tracker.total_burned(), 4500);
        assert_eq!(tracker.recent_burns(2).len(), 2);
        assert_eq!(tracker.burn_rate(), 1500);
    }

    #[test]
    fn test_fee_breakdown() {
        let breakdown = FeeBreakdown::calculate(21000, MIN_GAS_PRICE, MIN_GAS_PRICE / 10);

        assert_eq!(breakdown.gas_used, 21000);
        assert_eq!(breakdown.base_fee_cost, 21000 * MIN_GAS_PRICE);
        assert_eq!(breakdown.priority_fee_cost, 21000 * MIN_GAS_PRICE / 10);
        assert_eq!(
            breakdown.total_fee,
            breakdown.base_fee_cost + breakdown.priority_fee_cost
        );
    }

    #[test]
    fn test_update_base_fee_tracks_history() {
        let calc = FeeCalculator::default_mainnet();

        for i in 0..10 {
            calc.update_base_fee(TARGET_GAS + i * 1_000_000);
        }

        let avg = calc.average_gas_usage();
        assert!(avg > TARGET_GAS);
    }
}
