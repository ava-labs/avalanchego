//! Consensus parameters.

use std::time::Duration;

/// Parameters for Snow consensus protocols.
#[derive(Debug, Clone)]
pub struct Parameters {
    /// Sample size (k) - number of validators to poll
    pub k: usize,

    /// Quorum size (alpha) - number of votes needed for a successful poll
    pub alpha: usize,

    /// Beta for virtuous transactions - consecutive successes needed
    pub beta_virtuous: usize,

    /// Beta for rogue transactions - consecutive successes needed when conflicting
    pub beta_rogue: usize,

    /// Maximum number of items to track simultaneously
    pub max_outstanding_items: usize,

    /// Maximum time to process a single item
    pub max_item_processing_time: Duration,

    /// Concurrent polls allowed
    pub concurrent_repolls: usize,

    /// Whether to optimize for virtuous transactions
    pub optimize_virtuous: bool,
}

impl Default for Parameters {
    fn default() -> Self {
        Self {
            k: 20,
            alpha: 15,
            beta_virtuous: 15,
            beta_rogue: 20,
            max_outstanding_items: 1024,
            max_item_processing_time: Duration::from_secs(30),
            concurrent_repolls: 4,
            optimize_virtuous: true,
        }
    }
}

impl Parameters {
    /// Creates new parameters with the given values.
    pub fn new(k: usize, alpha: usize, beta_virtuous: usize, beta_rogue: usize) -> Self {
        Self {
            k,
            alpha,
            beta_virtuous,
            beta_rogue,
            ..Default::default()
        }
    }

    /// Validates the parameters.
    pub fn validate(&self) -> Result<(), String> {
        if self.k == 0 {
            return Err("k must be positive".to_string());
        }
        if self.alpha == 0 {
            return Err("alpha must be positive".to_string());
        }
        if self.alpha > self.k {
            return Err("alpha must be <= k".to_string());
        }
        if self.beta_virtuous == 0 {
            return Err("beta_virtuous must be positive".to_string());
        }
        if self.beta_rogue == 0 {
            return Err("beta_rogue must be positive".to_string());
        }
        if self.beta_rogue < self.beta_virtuous {
            return Err("beta_rogue must be >= beta_virtuous".to_string());
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_valid() {
        let params = Parameters::default();
        assert!(params.validate().is_ok());
    }

    #[test]
    fn test_invalid_k() {
        let params = Parameters {
            k: 0,
            ..Default::default()
        };
        assert!(params.validate().is_err());
    }

    #[test]
    fn test_invalid_alpha() {
        let params = Parameters {
            alpha: 25,
            k: 20,
            ..Default::default()
        };
        assert!(params.validate().is_err());
    }

    #[test]
    fn test_invalid_beta() {
        let params = Parameters {
            beta_virtuous: 20,
            beta_rogue: 15,
            ..Default::default()
        };
        assert!(params.validate().is_err());
    }
}
