//! Avalanche consensus implementation (Snow family).
//!
//! This crate provides the Snowball and Snowman consensus protocols used by Avalanche.
//!
//! # Architecture
//!
//! - **Snowball**: Core binary consensus algorithm
//! - **Snowman**: Linear chain consensus built on Snowball
//! - **Engine**: State machine managing consensus lifecycle
//! - **Validators**: Validator set management and sampling
//!
//! # Example
//!
//! ```
//! use avalanche_snow::{Parameters, Snowball};
//!
//! let params = Parameters::default();
//! let mut snowball = Snowball::new(params);
//! ```

mod consensus;
mod engine;
mod error;
mod parameters;
mod validators;

pub use consensus::{snowball::Snowball, snowman::Snowman, Consensus, Decidable};
pub use engine::{Engine, EngineState};
pub use error::{ConsensusError, Result};
pub use parameters::Parameters;
pub use validators::{Validator, ValidatorSet};

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parameters_default() {
        let params = Parameters::default();
        assert!(params.k > 0);
        assert!(params.alpha > 0);
        assert!(params.alpha <= params.k);
    }
}
