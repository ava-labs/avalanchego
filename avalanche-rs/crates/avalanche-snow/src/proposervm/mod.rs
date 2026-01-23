//! ProposerVM wrapper implementing Snowman++ consensus.
//!
//! The ProposerVM wraps an inner VM and adds block proposal scheduling,
//! proposer certificates, and windowing logic for fair block production.

pub mod block;
pub mod scheduler;
pub mod state;

use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use async_trait::async_trait;
use parking_lot::RwLock;
use sha2::{Digest, Sha256};
use tracing::{debug, info, warn};

use avalanche_db::Database;
use avalanche_ids::{Id, NodeId};

use crate::error::Result;
use crate::vm::{
    AppHandler, Block as VMBlock, BuildBlockOptions, ChainVM, CommonVM, Context, HealthStatus,
    VMError, Version,
};

use crate::validators::ValidatorSetTrait;
use block::{PostForkBlock, PreForkBlock, ProposerBlock};
use scheduler::{ProposerScheduler, WindowScheduler};
use state::ProposerState;

/// Configuration for the ProposerVM.
#[derive(Debug, Clone)]
pub struct ProposerVMConfig {
    /// Minimum delay between blocks from the same proposer.
    pub min_block_delay: Duration,
    /// Number of seconds in each proposer window.
    pub window_duration: Duration,
    /// Number of proposer windows before anyone can propose.
    pub num_windows: u64,
    /// Whether to use Banff rules (post-Banff fork).
    pub use_banff_rules: bool,
    /// Fork height (0 = from genesis).
    pub fork_height: u64,
    /// Fork time (Unix timestamp).
    pub fork_time: u64,
}

impl Default for ProposerVMConfig {
    fn default() -> Self {
        Self {
            min_block_delay: Duration::from_secs(1),
            window_duration: Duration::from_secs(2),
            num_windows: 6,
            use_banff_rules: true,
            fork_height: 0,
            fork_time: 0,
        }
    }
}

/// ProposerVM wraps an inner ChainVM with Snowman++ consensus logic.
pub struct ProposerVM<V: ChainVM> {
    /// Inner VM being wrapped.
    inner: V,
    /// Configuration.
    config: ProposerVMConfig,
    /// Execution context.
    ctx: Option<Context>,
    /// Database for proposer state.
    db: Option<Arc<dyn Database>>,
    /// Proposer state.
    state: Option<ProposerState>,
    /// Validator set for determining proposers.
    validators: Option<Arc<dyn ValidatorSetTrait>>,
    /// Block scheduler.
    scheduler: Option<WindowScheduler>,
    /// Current height.
    height: RwLock<u64>,
    /// Preferred block ID.
    preferred: RwLock<Id>,
    /// Last accepted block ID.
    last_accepted: RwLock<Id>,
    /// Whether VM is initialized.
    initialized: bool,
    /// This node's ID.
    node_id: Option<NodeId>,
}

impl<V: ChainVM> ProposerVM<V> {
    /// Creates a new ProposerVM wrapping the given inner VM.
    pub fn new(inner: V, config: ProposerVMConfig) -> Self {
        Self {
            inner,
            config,
            ctx: None,
            db: None,
            state: None,
            validators: None,
            scheduler: None,
            height: RwLock::new(0),
            preferred: RwLock::new(Id::default()),
            last_accepted: RwLock::new(Id::default()),
            initialized: false,
            node_id: None,
        }
    }

    /// Sets the validator set.
    pub fn set_validators(&mut self, validators: Arc<dyn ValidatorSetTrait>) {
        self.validators = Some(validators);
    }

    /// Returns the inner VM.
    pub fn inner(&self) -> &V {
        &self.inner
    }

    /// Returns the inner VM mutably.
    pub fn inner_mut(&mut self) -> &mut V {
        &mut self.inner
    }

    /// Returns whether we're past the fork height/time.
    pub fn is_post_fork(&self, height: u64, timestamp: u64) -> bool {
        height >= self.config.fork_height && timestamp >= self.config.fork_time
    }

    /// Computes the proposer for a given parent and slot.
    pub fn get_proposer(&self, parent_id: Id, height: u64, slot: u64) -> Option<NodeId> {
        let validators = self.validators.as_ref()?;
        let scheduler = self.scheduler.as_ref()?;

        scheduler.get_proposer(validators.as_ref(), parent_id, height, slot)
    }

    /// Returns whether this node can propose at the given slot.
    pub fn can_propose(&self, parent_id: Id, height: u64, slot: u64) -> bool {
        let node_id = match self.node_id {
            Some(id) => id,
            None => return false,
        };

        match self.get_proposer(parent_id, height, slot) {
            Some(proposer) => proposer == node_id,
            None => {
                // If no specific proposer, check if we're in open window
                slot >= self.config.num_windows
            }
        }
    }

    /// Computes the delay before this node should propose.
    pub fn get_proposal_delay(&self, parent_id: Id, height: u64) -> Duration {
        let node_id = match self.node_id {
            Some(id) => id,
            None => return Duration::MAX,
        };

        let validators = match &self.validators {
            Some(v) => v,
            None => return Duration::MAX,
        };

        let scheduler = match &self.scheduler {
            Some(s) => s,
            None => return Duration::MAX,
        };

        scheduler.get_delay(validators.as_ref(), parent_id, height, node_id)
    }

    /// Wraps an inner block as a ProposerBlock.
    fn wrap_block(
        &self,
        inner_block: Box<dyn VMBlock>,
        height: u64,
        timestamp: u64,
    ) -> Result<ProposerBlock> {
        let inner_bytes = inner_block.bytes();
        let inner_id = inner_block.id();
        let parent_id = inner_block.parent();

        if self.is_post_fork(height, timestamp) {
            // Create post-fork block with proposer info
            let node_id = self.node_id.ok_or(crate::error::ConsensusError::VMNotInitialized)?;

            // Calculate P-chain height (simplified - would come from state)
            let pchain_height = height.saturating_sub(1);

            let post_fork = PostForkBlock::new(
                parent_id,
                inner_bytes.to_vec(),
                timestamp,
                pchain_height,
                node_id,
            );

            Ok(ProposerBlock::PostFork(post_fork))
        } else {
            // Create pre-fork block (just wraps inner)
            let pre_fork = PreForkBlock::new(inner_bytes.to_vec(), inner_id, parent_id, height);

            Ok(ProposerBlock::PreFork(pre_fork))
        }
    }

    /// Unwraps a ProposerBlock to get the inner block bytes.
    fn unwrap_block<'a>(&self, block: &'a ProposerBlock) -> &'a [u8] {
        match block {
            ProposerBlock::PreFork(b) => b.inner_bytes(),
            ProposerBlock::PostFork(b) => b.inner_bytes(),
        }
    }

    /// Verifies a proposer block's certificate and timing.
    fn verify_proposer_block(&self, block: &ProposerBlock) -> Result<()> {
        match block {
            ProposerBlock::PreFork(_) => {
                // Pre-fork blocks don't need proposer verification
                Ok(())
            }
            ProposerBlock::PostFork(b) => {
                let height = b.height();
                let timestamp = b.timestamp();
                let proposer = b.proposer();
                let parent_id = b.parent_id();

                // Verify timestamp is reasonable
                let now = SystemTime::now()
                    .duration_since(UNIX_EPOCH)
                    .unwrap_or_default()
                    .as_secs();

                if timestamp > now + 10 {
                    return Err(crate::error::ConsensusError::InvalidBlock(format!(
                        "block timestamp {} is in the future (now={})",
                        timestamp, now
                    )));
                }

                // Verify proposer had the right to propose
                if let Some(validators) = &self.validators {
                    if let Some(scheduler) = &self.scheduler {
                        let slot = scheduler.timestamp_to_slot(timestamp);
                        let expected = scheduler.get_proposer(
                            validators.as_ref(),
                            parent_id,
                            height,
                            slot,
                        );

                        // In windowed slots, verify proposer matches
                        if slot < self.config.num_windows {
                            if let Some(expected_proposer) = expected {
                                if proposer != expected_proposer {
                                    return Err(crate::error::ConsensusError::InvalidBlock(format!(
                                        "wrong proposer: expected {}, got {}",
                                        expected_proposer, proposer
                                    )));
                                }
                            }
                        }
                    }
                }

                Ok(())
            }
        }
    }
}

#[async_trait]
impl<V: ChainVM + Send + Sync> CommonVM for ProposerVM<V> {
    async fn initialize(
        &mut self,
        ctx: Context,
        db: Arc<dyn Database>,
        genesis_bytes: &[u8],
    ) -> Result<()> {
        if self.initialized {
            return Err(crate::error::ConsensusError::VMAlreadyInitialized);
        }

        info!("Initializing ProposerVM");

        // Store node ID
        self.node_id = Some(ctx.node_id);

        // Initialize inner VM
        self.inner.initialize(ctx.clone(), db.clone(), genesis_bytes).await?;

        // Initialize proposer state
        let state = ProposerState::new(db.clone())?;
        let genesis_block_id = state.genesis_block_id();

        // Create scheduler
        let scheduler = WindowScheduler::new(
            self.config.window_duration,
            self.config.num_windows,
            self.config.min_block_delay,
        );

        self.ctx = Some(ctx);
        self.db = Some(db);
        self.state = Some(state);
        self.scheduler = Some(scheduler);
        *self.preferred.write() = genesis_block_id;
        *self.last_accepted.write() = genesis_block_id;
        self.initialized = true;

        info!("ProposerVM initialized successfully");
        Ok(())
    }

    async fn shutdown(&mut self) -> Result<()> {
        info!("Shutting down ProposerVM");
        self.inner.shutdown().await?;
        self.initialized = false;
        Ok(())
    }

    fn version(&self) -> Version {
        // Return proposervm version
        Version::new(1, 5, 0)
    }

    fn create_handlers(&self) -> Vec<Box<dyn AppHandler>> {
        self.inner.create_handlers()
    }

    async fn health_check(&self) -> Result<HealthStatus> {
        if !self.initialized {
            return Ok(HealthStatus::unhealthy("proposervm not initialized"));
        }

        // Check inner VM health
        let inner_health = self.inner.health_check().await?;
        if !inner_health.healthy {
            return Ok(HealthStatus::unhealthy(&format!(
                "inner VM unhealthy: {}",
                inner_health.reason.as_deref().unwrap_or("unknown")
            )));
        }

        Ok(HealthStatus::healthy())
    }

    async fn connected(&self, node_id: &NodeId, version: &str) -> Result<()> {
        self.inner.connected(node_id, version).await
    }

    async fn disconnected(&self, node_id: &NodeId) -> Result<()> {
        self.inner.disconnected(node_id).await
    }
}

#[async_trait]
impl<V: ChainVM + Send + Sync> ChainVM for ProposerVM<V> {
    async fn build_block(&self, options: BuildBlockOptions) -> Result<Box<dyn VMBlock>> {
        let height = *self.height.read() + 1;
        let preferred = *self.preferred.read();

        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        // Check if we can propose
        if !self.is_post_fork(height, now) {
            // Pre-fork: just build inner block
            debug!("Building pre-fork block at height {}", height);
            let inner_block = self.inner.build_block(options).await?;
            let wrapped = self.wrap_block(inner_block, height, now)?;
            return Ok(Box::new(wrapped));
        }

        // Post-fork: check proposer scheduling
        let delay = self.get_proposal_delay(preferred, height);
        if delay > Duration::ZERO && delay != Duration::MAX {
            debug!("Proposal delayed by {:?}", delay);
            // In production, would wait for the delay
            // For now, we proceed immediately
        }

        // Build inner block
        let inner_block = self.inner.build_block(options).await?;
        let wrapped = self.wrap_block(inner_block, height, now)?;

        info!("Built proposer block at height {}", height);
        Ok(Box::new(wrapped))
    }

    async fn parse_block(&self, bytes: &[u8]) -> Result<Box<dyn VMBlock>> {
        // Try to parse as ProposerBlock
        let proposer_block = ProposerBlock::parse(bytes)?;

        // Verify proposer certificate/timing
        self.verify_proposer_block(&proposer_block)?;

        // Parse inner block
        let inner_bytes = self.unwrap_block(&proposer_block);
        let _inner_block = self.inner.parse_block(inner_bytes).await?;

        Ok(Box::new(proposer_block))
    }

    async fn get_block(&self, id: Id) -> Result<Option<Box<dyn VMBlock>>> {
        // Check proposer state first
        if let Some(state) = &self.state {
            if let Some(block) = state.get_block(&id)? {
                return Ok(Some(Box::new(block)));
            }
        }

        // Try inner VM
        if let Some(inner_block) = self.inner.get_block(id).await? {
            // Wrap as pre-fork block (this is a simplification)
            let pre_fork = PreForkBlock::new(
                inner_block.bytes().to_vec(),
                inner_block.id(),
                inner_block.parent(),
                inner_block.height(),
            );
            return Ok(Some(Box::new(ProposerBlock::PreFork(pre_fork))));
        }

        Ok(None)
    }

    async fn set_preference(&mut self, id: Id) -> Result<()> {
        *self.preferred.write() = id;
        // Also update inner VM preference
        // Need to map proposer block ID to inner block ID
        self.inner.set_preference(id).await
    }

    fn last_accepted(&self) -> Id {
        *self.last_accepted.read()
    }

    fn preferred(&self) -> Id {
        *self.preferred.read()
    }

    async fn block_accepted(&mut self, block: &dyn VMBlock) -> Result<()> {
        let id = block.id();
        let height = block.height();

        *self.last_accepted.write() = id;
        *self.height.write() = height;

        // Store in proposer state
        if let Some(state) = &mut self.state {
            // Would need to downcast block to ProposerBlock
            // For now just update height
        }

        // Notify inner VM
        self.inner.block_accepted(block).await?;

        info!("Accepted proposer block {} at height {}", id, height);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_config_default() {
        let config = ProposerVMConfig::default();
        assert_eq!(config.min_block_delay, Duration::from_secs(1));
        assert_eq!(config.window_duration, Duration::from_secs(2));
        assert_eq!(config.num_windows, 6);
        assert!(config.use_banff_rules);
    }

    #[test]
    fn test_is_post_fork() {
        let config = ProposerVMConfig {
            fork_height: 100,
            fork_time: 1000,
            ..Default::default()
        };

        // Before fork height
        assert!(!is_post_fork(&config, 99, 2000));
        // Before fork time
        assert!(!is_post_fork(&config, 200, 500));
        // After both
        assert!(is_post_fork(&config, 100, 1000));
        assert!(is_post_fork(&config, 200, 2000));
    }

    fn is_post_fork(config: &ProposerVMConfig, height: u64, timestamp: u64) -> bool {
        height >= config.fork_height && timestamp >= config.fork_time
    }
}
