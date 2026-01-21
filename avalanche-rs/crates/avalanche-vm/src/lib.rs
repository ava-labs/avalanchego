//! Avalanche Virtual Machine framework.
//!
//! This crate provides the core traits and abstractions for implementing
//! virtual machines that run on the Avalanche network.
//!
//! # Architecture
//!
//! - **ChainVM**: Main trait for linear chain VMs (Snowman-based)
//! - **Block**: Block abstraction for consensus
//! - **Context**: Execution context provided to VMs
//!
//! # Example
//!
//! ```ignore
//! use avalanche_vm::{ChainVM, Block, Context};
//!
//! struct MyVM { /* ... */ }
//!
//! #[async_trait]
//! impl ChainVM for MyVM {
//!     // Implement VM methods...
//! }
//! ```

mod block;
mod context;
mod error;
mod state;
mod vm;

pub use block::{Block, BlockStatus, BuildBlockOptions, StatelessBlock};
pub use context::Context;
pub use error::{Result, VMError};
pub use state::{State, StateManager};
pub use vm::{AppHandler, ChainVM, CommonVM, Connector, CrossChainAppHandler, HealthStatus, Version};

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_block_status() {
        assert!(!BlockStatus::Processing.decided());
        assert!(BlockStatus::Accepted.decided());
        assert!(BlockStatus::Rejected.decided());
    }
}
