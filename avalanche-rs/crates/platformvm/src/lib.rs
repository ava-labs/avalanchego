//! Platform VM (P-Chain) implementation.
//!
//! The Platform VM manages the Avalanche network's validators, subnets,
//! and staking operations. It uses Snowman consensus for linear block finality.
//!
//! # Architecture
//!
//! - **State**: Validator sets, subnet tracking, UTXO management
//! - **Transactions**: Staking, subnet creation, chain creation
//! - **Blocks**: Standard, Proposal, Commit/Abort blocks
//!
//! # Example
//!
//! ```ignore
//! use platformvm::PlatformVM;
//!
//! let vm = PlatformVM::new();
//! ```

pub mod block;
pub mod genesis;
pub mod rewards;
pub mod state;
pub mod txs;
pub mod validator;

use std::sync::Arc;

use async_trait::async_trait;
use parking_lot::RwLock;

use avalanche_db::Database;
use avalanche_ids::{Id, NodeId};
use avalanche_vm::{
    Block, BuildBlockOptions, ChainVM, CommonVM, Context, HealthStatus,
    Result, VMError, Version,
};

use crate::state::PlatformState;

/// The Platform Virtual Machine.
pub struct PlatformVM {
    /// Execution context
    ctx: Option<Context>,
    /// Database
    db: Option<Arc<dyn Database>>,
    /// Platform state
    state: Option<PlatformState>,
    /// Preferred block ID
    preferred: RwLock<Id>,
    /// Last accepted block ID
    last_accepted: RwLock<Id>,
    /// Whether VM is initialized
    initialized: bool,
}

impl PlatformVM {
    /// Creates a new Platform VM instance.
    pub fn new() -> Self {
        Self {
            ctx: None,
            db: None,
            state: None,
            preferred: RwLock::new(Id::default()),
            last_accepted: RwLock::new(Id::default()),
            initialized: false,
        }
    }

    /// Returns the platform state.
    pub fn state(&self) -> Option<&PlatformState> {
        self.state.as_ref()
    }
}

impl Default for PlatformVM {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl CommonVM for PlatformVM {
    async fn initialize(
        &mut self,
        ctx: Context,
        db: Arc<dyn Database>,
        genesis_bytes: &[u8],
    ) -> Result<()> {
        if self.initialized {
            return Err(VMError::AlreadyInitialized);
        }

        // Parse genesis
        let genesis = genesis::Genesis::parse(genesis_bytes)?;

        // Initialize state
        let state = PlatformState::new(db.clone(), &genesis)?;
        let genesis_block_id = state.genesis_block_id();

        self.ctx = Some(ctx);
        self.db = Some(db);
        self.state = Some(state);
        *self.preferred.write() = genesis_block_id;
        *self.last_accepted.write() = genesis_block_id;
        self.initialized = true;

        Ok(())
    }

    async fn shutdown(&mut self) -> Result<()> {
        self.initialized = false;
        Ok(())
    }

    fn version(&self) -> Version {
        Version::new(1, 0, 0)
    }

    fn create_handlers(&self) -> Vec<Box<dyn avalanche_vm::AppHandler>> {
        vec![]
    }

    async fn health_check(&self) -> Result<HealthStatus> {
        if self.initialized {
            Ok(HealthStatus::healthy())
        } else {
            Ok(HealthStatus::unhealthy("not initialized"))
        }
    }

    async fn connected(&self, _node_id: &NodeId, _version: &str) -> Result<()> {
        Ok(())
    }

    async fn disconnected(&self, _node_id: &NodeId) -> Result<()> {
        Ok(())
    }
}

#[async_trait]
impl ChainVM for PlatformVM {
    async fn build_block(&self, _options: BuildBlockOptions) -> Result<Box<dyn Block>> {
        let state = self.state.as_ref().ok_or(VMError::NotInitialized)?;

        // Build a standard block with pending transactions
        let parent_id = *self.preferred.read();
        let block = state.build_block(parent_id)?;

        Ok(Box::new(block))
    }

    async fn parse_block(&self, bytes: &[u8]) -> Result<Box<dyn Block>> {
        let block = block::PlatformBlock::parse(bytes)?;
        Ok(Box::new(block))
    }

    async fn get_block(&self, id: Id) -> Result<Option<Box<dyn Block>>> {
        let state = self.state.as_ref().ok_or(VMError::NotInitialized)?;

        match state.get_block(&id)? {
            Some(block) => Ok(Some(Box::new(block))),
            None => Ok(None),
        }
    }

    async fn set_preference(&mut self, id: Id) -> Result<()> {
        *self.preferred.write() = id;
        Ok(())
    }

    fn last_accepted(&self) -> Id {
        *self.last_accepted.read()
    }

    fn preferred(&self) -> Id {
        *self.preferred.read()
    }

    async fn block_accepted(&mut self, block: &dyn Block) -> Result<()> {
        *self.last_accepted.write() = block.id();
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_platform_vm_new() {
        let vm = PlatformVM::new();
        assert!(!vm.initialized);
    }
}
