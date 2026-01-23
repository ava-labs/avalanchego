//! Block execution pipeline.
//!
//! This module provides the full block lifecycle: verify → execute → commit.
//! It coordinates between the VM, consensus, and state layers.

use std::collections::{HashMap, VecDeque};
use std::sync::Arc;
use std::time::{Duration, Instant};

use async_trait::async_trait;
use parking_lot::RwLock;
use tokio::sync::mpsc;
use tracing::{debug, error, info, warn};

use avalanche_ids::{Id, NodeId};

use crate::{ConsensusError, Result};

/// Block status in the execution pipeline.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BlockStatus {
    /// Block is unknown.
    Unknown,
    /// Block is being fetched.
    Fetching,
    /// Block has been received but not verified.
    Pending,
    /// Block is being verified.
    Verifying,
    /// Block has been verified and is valid.
    Verified,
    /// Block verification failed.
    Rejected,
    /// Block is being executed.
    Executing,
    /// Block has been executed.
    Executed,
    /// Block has been committed to storage.
    Committed,
    /// Block has been accepted by consensus.
    Accepted,
}

/// A block in the execution pipeline.
#[derive(Debug, Clone)]
pub struct ExecutableBlock {
    /// Block ID.
    pub id: Id,
    /// Parent block ID.
    pub parent_id: Id,
    /// Block height.
    pub height: u64,
    /// Block timestamp.
    pub timestamp: u64,
    /// Raw block bytes.
    pub bytes: Vec<u8>,
    /// Current status.
    pub status: BlockStatus,
    /// When the block was received.
    pub received_at: Instant,
    /// Verification result (if verified).
    pub verification_result: Option<std::result::Result<(), String>>,
    /// Execution result (state root after execution).
    pub execution_result: Option<std::result::Result<Id, String>>,
}

impl ExecutableBlock {
    /// Creates a new executable block.
    pub fn new(id: Id, parent_id: Id, height: u64, timestamp: u64, bytes: Vec<u8>) -> Self {
        Self {
            id,
            parent_id,
            height,
            timestamp,
            bytes,
            status: BlockStatus::Pending,
            received_at: Instant::now(),
            verification_result: None,
            execution_result: None,
        }
    }

    /// Returns true if the block is ready for verification.
    pub fn is_ready_for_verification(&self) -> bool {
        self.status == BlockStatus::Pending
    }

    /// Returns true if the block is ready for execution.
    pub fn is_ready_for_execution(&self) -> bool {
        self.status == BlockStatus::Verified
    }

    /// Returns true if the block is ready for commit.
    pub fn is_ready_for_commit(&self) -> bool {
        self.status == BlockStatus::Executed
    }
}

/// Trait for verifying blocks.
#[async_trait]
pub trait BlockVerifier: Send + Sync {
    /// Verifies a block's structure and signatures.
    async fn verify(&self, block: &ExecutableBlock) -> std::result::Result<(), String>;

    /// Returns the parent block if available.
    async fn get_parent(&self, parent_id: Id) -> Option<ExecutableBlock>;
}

/// Trait for executing blocks.
#[async_trait]
pub trait BlockExecutor: Send + Sync {
    /// Executes a block and returns the new state root.
    async fn execute(&self, block: &ExecutableBlock) -> std::result::Result<Id, String>;

    /// Commits a block to persistent storage.
    async fn commit(&self, block: &ExecutableBlock, state_root: Id) -> std::result::Result<(), String>;

    /// Rolls back a failed execution.
    async fn rollback(&self, block: &ExecutableBlock) -> std::result::Result<(), String>;
}

/// Trait for accepting blocks into consensus.
#[async_trait]
pub trait BlockAcceptor: Send + Sync {
    /// Called when a block is accepted by consensus.
    async fn on_accepted(&self, block_id: Id, height: u64) -> std::result::Result<(), String>;

    /// Called when a block is rejected by consensus.
    async fn on_rejected(&self, block_id: Id) -> std::result::Result<(), String>;
}

/// Configuration for the execution pipeline.
#[derive(Debug, Clone)]
pub struct ExecutorConfig {
    /// Maximum blocks to process concurrently.
    pub max_concurrent: usize,
    /// Verification timeout.
    pub verify_timeout: Duration,
    /// Execution timeout.
    pub execute_timeout: Duration,
    /// Maximum pending blocks.
    pub max_pending: usize,
}

impl Default for ExecutorConfig {
    fn default() -> Self {
        Self {
            max_concurrent: 4,
            verify_timeout: Duration::from_secs(10),
            execute_timeout: Duration::from_secs(30),
            max_pending: 1024,
        }
    }
}

/// Events emitted by the execution pipeline.
#[derive(Debug, Clone)]
pub enum ExecutorEvent {
    /// Block was verified successfully.
    BlockVerified { block_id: Id, height: u64 },
    /// Block verification failed.
    BlockVerificationFailed { block_id: Id, error: String },
    /// Block was executed successfully.
    BlockExecuted { block_id: Id, state_root: Id },
    /// Block execution failed.
    BlockExecutionFailed { block_id: Id, error: String },
    /// Block was committed.
    BlockCommitted { block_id: Id },
    /// Block was accepted by consensus.
    BlockAccepted { block_id: Id, height: u64 },
    /// Block was rejected by consensus.
    BlockRejected { block_id: Id },
}

/// The execution pipeline manages the full block lifecycle.
pub struct ExecutionPipeline<V, E, A>
where
    V: BlockVerifier,
    E: BlockExecutor,
    A: BlockAcceptor,
{
    /// Block verifier.
    verifier: Arc<V>,
    /// Block executor.
    executor: Arc<E>,
    /// Block acceptor.
    acceptor: Arc<A>,
    /// Configuration.
    config: ExecutorConfig,
    /// Pending blocks by ID.
    pending_blocks: RwLock<HashMap<Id, ExecutableBlock>>,
    /// Blocks ordered by height for sequential processing.
    height_queue: RwLock<VecDeque<Id>>,
    /// Last accepted block ID.
    last_accepted: RwLock<Id>,
    /// Last accepted height.
    last_accepted_height: RwLock<u64>,
    /// Event sender.
    event_tx: mpsc::Sender<ExecutorEvent>,
    /// Event receiver (taken when run() is called).
    event_rx: RwLock<Option<mpsc::Receiver<ExecutorEvent>>>,
}

impl<V, E, A> ExecutionPipeline<V, E, A>
where
    V: BlockVerifier + 'static,
    E: BlockExecutor + 'static,
    A: BlockAcceptor + 'static,
{
    /// Creates a new execution pipeline.
    pub fn new(
        verifier: Arc<V>,
        executor: Arc<E>,
        acceptor: Arc<A>,
        config: ExecutorConfig,
        genesis_id: Id,
    ) -> Self {
        let (event_tx, event_rx) = mpsc::channel(256);
        Self {
            verifier,
            executor,
            acceptor,
            config,
            pending_blocks: RwLock::new(HashMap::new()),
            height_queue: RwLock::new(VecDeque::new()),
            last_accepted: RwLock::new(genesis_id),
            last_accepted_height: RwLock::new(0),
            event_tx,
            event_rx: RwLock::new(Some(event_rx)),
        }
    }

    /// Returns the event sender for subscribing to events.
    pub fn event_sender(&self) -> mpsc::Sender<ExecutorEvent> {
        self.event_tx.clone()
    }

    /// Submits a block to the execution pipeline.
    pub fn submit_block(&self, block: ExecutableBlock) -> Result<()> {
        let mut pending = self.pending_blocks.write();

        if pending.len() >= self.config.max_pending {
            return Err(ConsensusError::Internal("too many pending blocks".into()));
        }

        if pending.contains_key(&block.id) {
            return Ok(()); // Already have this block
        }

        let id = block.id;
        let height = block.height;
        pending.insert(id, block);

        // Add to height queue for ordered processing
        let mut queue = self.height_queue.write();
        queue.push_back(id);

        debug!("Block {} at height {} submitted to pipeline", id, height);
        Ok(())
    }

    /// Returns the status of a block.
    pub fn block_status(&self, block_id: Id) -> BlockStatus {
        self.pending_blocks
            .read()
            .get(&block_id)
            .map(|b| b.status)
            .unwrap_or(BlockStatus::Unknown)
    }

    /// Returns the last accepted block ID.
    pub fn last_accepted(&self) -> Id {
        *self.last_accepted.read()
    }

    /// Returns the last accepted height.
    pub fn last_accepted_height(&self) -> u64 {
        *self.last_accepted_height.read()
    }

    /// Processes a single block through verification.
    async fn verify_block(&self, block_id: Id) -> Result<()> {
        let block = {
            let pending = self.pending_blocks.read();
            pending.get(&block_id).cloned()
        };

        let block = match block {
            Some(b) if b.is_ready_for_verification() => b,
            _ => return Ok(()),
        };

        // Update status to verifying
        {
            let mut pending = self.pending_blocks.write();
            if let Some(b) = pending.get_mut(&block_id) {
                b.status = BlockStatus::Verifying;
            }
        }

        debug!("Verifying block {} at height {}", block.id, block.height);

        // Check parent is accepted or verified
        let last_accepted = *self.last_accepted.read();
        if block.parent_id != last_accepted {
            // Check if parent is in pipeline and verified
            let parent_status = self.block_status(block.parent_id);
            if parent_status != BlockStatus::Verified
                && parent_status != BlockStatus::Executed
                && parent_status != BlockStatus::Committed
                && parent_status != BlockStatus::Accepted
            {
                // Parent not ready, re-queue
                let mut pending = self.pending_blocks.write();
                if let Some(b) = pending.get_mut(&block_id) {
                    b.status = BlockStatus::Pending;
                }
                return Ok(());
            }
        }

        // Perform verification with timeout
        let result = tokio::time::timeout(
            self.config.verify_timeout,
            self.verifier.verify(&block),
        )
        .await;

        let verification_result = match result {
            Ok(Ok(())) => Ok(()),
            Ok(Err(e)) => Err(e),
            Err(_) => Err("verification timeout".to_string()),
        };

        // Update block status
        {
            let mut pending = self.pending_blocks.write();
            if let Some(b) = pending.get_mut(&block_id) {
                b.verification_result = Some(verification_result.clone());
                b.status = if verification_result.is_ok() {
                    BlockStatus::Verified
                } else {
                    BlockStatus::Rejected
                };
            }
        }

        // Emit event
        let event = match &verification_result {
            Ok(()) => {
                info!("Block {} verified successfully", block_id);
                ExecutorEvent::BlockVerified {
                    block_id,
                    height: block.height,
                }
            }
            Err(e) => {
                warn!("Block {} verification failed: {}", block_id, e);
                ExecutorEvent::BlockVerificationFailed {
                    block_id,
                    error: e.clone(),
                }
            }
        };

        let _ = self.event_tx.send(event).await;
        Ok(())
    }

    /// Executes a verified block.
    async fn execute_block(&self, block_id: Id) -> Result<()> {
        let block = {
            let pending = self.pending_blocks.read();
            pending.get(&block_id).cloned()
        };

        let block = match block {
            Some(b) if b.is_ready_for_execution() => b,
            _ => return Ok(()),
        };

        // Update status
        {
            let mut pending = self.pending_blocks.write();
            if let Some(b) = pending.get_mut(&block_id) {
                b.status = BlockStatus::Executing;
            }
        }

        debug!("Executing block {} at height {}", block.id, block.height);

        // Execute with timeout
        let result = tokio::time::timeout(
            self.config.execute_timeout,
            self.executor.execute(&block),
        )
        .await;

        let execution_result = match result {
            Ok(Ok(state_root)) => Ok(state_root),
            Ok(Err(e)) => Err(e),
            Err(_) => Err("execution timeout".to_string()),
        };

        // Update block status
        let state_root = {
            let mut pending = self.pending_blocks.write();
            if let Some(b) = pending.get_mut(&block_id) {
                b.execution_result = Some(execution_result.clone());
                b.status = if execution_result.is_ok() {
                    BlockStatus::Executed
                } else {
                    BlockStatus::Rejected
                };
            }
            execution_result
        };

        // Emit event
        let event = match state_root {
            Ok(root) => {
                info!("Block {} executed, state_root={}", block_id, root);
                ExecutorEvent::BlockExecuted {
                    block_id,
                    state_root: root,
                }
            }
            Err(e) => {
                error!("Block {} execution failed: {}", block_id, e);
                // Rollback on failure
                let _ = self.executor.rollback(&block).await;
                ExecutorEvent::BlockExecutionFailed {
                    block_id,
                    error: e,
                }
            }
        };

        let _ = self.event_tx.send(event).await;
        Ok(())
    }

    /// Commits an executed block.
    async fn commit_block(&self, block_id: Id) -> Result<()> {
        let (block, state_root) = {
            let pending = self.pending_blocks.read();
            let block = pending.get(&block_id).cloned();
            let state_root = block.as_ref().and_then(|b| {
                b.execution_result
                    .as_ref()
                    .and_then(|r| r.as_ref().ok().copied())
            });
            (block, state_root)
        };

        let block = match block {
            Some(b) if b.is_ready_for_commit() => b,
            _ => return Ok(()),
        };

        let state_root = match state_root {
            Some(r) => r,
            None => return Ok(()),
        };

        debug!("Committing block {} at height {}", block.id, block.height);

        // Commit to storage
        if let Err(e) = self.executor.commit(&block, state_root).await {
            error!("Failed to commit block {}: {}", block_id, e);
            return Err(ConsensusError::Database(e));
        }

        // Update status
        {
            let mut pending = self.pending_blocks.write();
            if let Some(b) = pending.get_mut(&block_id) {
                b.status = BlockStatus::Committed;
            }
        }

        let _ = self
            .event_tx
            .send(ExecutorEvent::BlockCommitted { block_id })
            .await;

        info!("Block {} committed", block_id);
        Ok(())
    }

    /// Accepts a block after consensus agreement.
    pub async fn accept_block(&self, block_id: Id) -> Result<()> {
        let block = {
            let pending = self.pending_blocks.read();
            pending.get(&block_id).cloned()
        };

        let block = match block {
            Some(b) if b.status == BlockStatus::Committed => b,
            _ => return Err(ConsensusError::BlockNotFound(block_id.to_string())),
        };

        debug!("Accepting block {} at height {}", block.id, block.height);

        // Notify acceptor
        if let Err(e) = self.acceptor.on_accepted(block_id, block.height).await {
            error!("Acceptor failed for block {}: {}", block_id, e);
            return Err(ConsensusError::Internal(e));
        }

        // Update last accepted
        {
            let mut last = self.last_accepted.write();
            let mut height = self.last_accepted_height.write();
            *last = block_id;
            *height = block.height;
        }

        // Update status and remove from pending
        {
            let mut pending = self.pending_blocks.write();
            if let Some(b) = pending.get_mut(&block_id) {
                b.status = BlockStatus::Accepted;
            }
            // Keep recently accepted blocks for a short time for queries
            // In production, you'd move them to an archive
        }

        let _ = self
            .event_tx
            .send(ExecutorEvent::BlockAccepted {
                block_id,
                height: block.height,
            })
            .await;

        info!("Block {} accepted at height {}", block_id, block.height);
        Ok(())
    }

    /// Rejects a block.
    pub async fn reject_block(&self, block_id: Id) -> Result<()> {
        debug!("Rejecting block {}", block_id);

        // Notify acceptor
        let _ = self.acceptor.on_rejected(block_id).await;

        // Remove from pending
        {
            let mut pending = self.pending_blocks.write();
            pending.remove(&block_id);
        }

        let _ = self
            .event_tx
            .send(ExecutorEvent::BlockRejected { block_id })
            .await;

        Ok(())
    }

    /// Processes pending blocks through the pipeline.
    pub async fn process_pending(&self) -> Result<()> {
        // Get blocks that need processing
        let blocks_to_process: Vec<(Id, BlockStatus)> = {
            let pending = self.pending_blocks.read();
            pending
                .iter()
                .map(|(id, b)| (*id, b.status))
                .collect()
        };

        for (block_id, status) in blocks_to_process {
            match status {
                BlockStatus::Pending => {
                    self.verify_block(block_id).await?;
                }
                BlockStatus::Verified => {
                    self.execute_block(block_id).await?;
                }
                BlockStatus::Executed => {
                    self.commit_block(block_id).await?;
                }
                _ => {}
            }
        }

        Ok(())
    }

    /// Runs the execution pipeline main loop.
    pub async fn run(&self, mut shutdown_rx: tokio::sync::broadcast::Receiver<()>) -> Result<()> {
        let mut process_interval = tokio::time::interval(Duration::from_millis(50));

        loop {
            tokio::select! {
                _ = process_interval.tick() => {
                    if let Err(e) = self.process_pending().await {
                        error!("Error processing pending blocks: {}", e);
                    }

                    // Cleanup old rejected blocks
                    let mut pending = self.pending_blocks.write();
                    pending.retain(|_, b| {
                        b.status != BlockStatus::Rejected ||
                        b.received_at.elapsed() < Duration::from_secs(60)
                    });
                }

                _ = shutdown_rx.recv() => {
                    info!("Execution pipeline shutting down");
                    break;
                }
            }
        }

        Ok(())
    }

    /// Returns statistics about the pipeline.
    pub fn stats(&self) -> PipelineStats {
        let pending = self.pending_blocks.read();
        let mut stats = PipelineStats::default();

        for block in pending.values() {
            match block.status {
                BlockStatus::Pending => stats.pending += 1,
                BlockStatus::Verifying => stats.verifying += 1,
                BlockStatus::Verified => stats.verified += 1,
                BlockStatus::Executing => stats.executing += 1,
                BlockStatus::Executed => stats.executed += 1,
                BlockStatus::Committed => stats.committed += 1,
                BlockStatus::Accepted => stats.accepted += 1,
                BlockStatus::Rejected => stats.rejected += 1,
                _ => {}
            }
        }

        stats.last_accepted_height = *self.last_accepted_height.read();
        stats
    }
}

/// Pipeline statistics.
#[derive(Debug, Clone, Default)]
pub struct PipelineStats {
    pub pending: usize,
    pub verifying: usize,
    pub verified: usize,
    pub executing: usize,
    pub executed: usize,
    pub committed: usize,
    pub accepted: usize,
    pub rejected: usize,
    pub last_accepted_height: u64,
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU64, Ordering};

    struct MockVerifier {
        should_fail: bool,
    }

    #[async_trait]
    impl BlockVerifier for MockVerifier {
        async fn verify(&self, _block: &ExecutableBlock) -> std::result::Result<(), String> {
            if self.should_fail {
                Err("mock verification failure".into())
            } else {
                Ok(())
            }
        }

        async fn get_parent(&self, _parent_id: Id) -> Option<ExecutableBlock> {
            None
        }
    }

    struct MockExecutor {
        exec_count: AtomicU64,
    }

    #[async_trait]
    impl BlockExecutor for MockExecutor {
        async fn execute(&self, block: &ExecutableBlock) -> std::result::Result<Id, String> {
            self.exec_count.fetch_add(1, Ordering::SeqCst);
            Ok(Id::from_bytes([block.height as u8; 32]))
        }

        async fn commit(&self, _block: &ExecutableBlock, _state_root: Id) -> std::result::Result<(), String> {
            Ok(())
        }

        async fn rollback(&self, _block: &ExecutableBlock) -> std::result::Result<(), String> {
            Ok(())
        }
    }

    struct MockAcceptor;

    #[async_trait]
    impl BlockAcceptor for MockAcceptor {
        async fn on_accepted(&self, _block_id: Id, _height: u64) -> std::result::Result<(), String> {
            Ok(())
        }

        async fn on_rejected(&self, _block_id: Id) -> std::result::Result<(), String> {
            Ok(())
        }
    }

    #[tokio::test]
    async fn test_block_verification() {
        let verifier = Arc::new(MockVerifier { should_fail: false });
        let executor = Arc::new(MockExecutor {
            exec_count: AtomicU64::new(0),
        });
        let acceptor = Arc::new(MockAcceptor);
        let genesis_id = Id::from_bytes([0; 32]);

        let pipeline = ExecutionPipeline::new(
            verifier,
            executor,
            acceptor,
            ExecutorConfig::default(),
            genesis_id,
        );

        let block = ExecutableBlock::new(
            Id::from_bytes([1; 32]),
            genesis_id,
            1,
            1000,
            vec![1, 2, 3],
        );

        pipeline.submit_block(block.clone()).unwrap();

        // Process the block
        pipeline.process_pending().await.unwrap();

        // Should be verified
        assert_eq!(pipeline.block_status(block.id), BlockStatus::Verified);
    }

    #[tokio::test]
    async fn test_block_execution() {
        let verifier = Arc::new(MockVerifier { should_fail: false });
        let executor = Arc::new(MockExecutor {
            exec_count: AtomicU64::new(0),
        });
        let acceptor = Arc::new(MockAcceptor);
        let genesis_id = Id::from_bytes([0; 32]);

        let pipeline = ExecutionPipeline::new(
            verifier,
            executor.clone(),
            acceptor,
            ExecutorConfig::default(),
            genesis_id,
        );

        let block = ExecutableBlock::new(
            Id::from_bytes([1; 32]),
            genesis_id,
            1,
            1000,
            vec![1, 2, 3],
        );

        pipeline.submit_block(block.clone()).unwrap();

        // Process verify + execute
        pipeline.process_pending().await.unwrap();
        pipeline.process_pending().await.unwrap();

        assert_eq!(executor.exec_count.load(Ordering::SeqCst), 1);
        assert_eq!(pipeline.block_status(block.id), BlockStatus::Executed);
    }

    #[tokio::test]
    async fn test_block_acceptance() {
        let verifier = Arc::new(MockVerifier { should_fail: false });
        let executor = Arc::new(MockExecutor {
            exec_count: AtomicU64::new(0),
        });
        let acceptor = Arc::new(MockAcceptor);
        let genesis_id = Id::from_bytes([0; 32]);

        let pipeline = ExecutionPipeline::new(
            verifier,
            executor,
            acceptor,
            ExecutorConfig::default(),
            genesis_id,
        );

        let block = ExecutableBlock::new(
            Id::from_bytes([1; 32]),
            genesis_id,
            1,
            1000,
            vec![1, 2, 3],
        );

        pipeline.submit_block(block.clone()).unwrap();

        // Process through to commit
        for _ in 0..4 {
            pipeline.process_pending().await.unwrap();
        }

        // Accept the block
        pipeline.accept_block(block.id).await.unwrap();

        assert_eq!(pipeline.last_accepted(), block.id);
        assert_eq!(pipeline.last_accepted_height(), 1);
    }

    #[tokio::test]
    async fn test_verification_failure() {
        let verifier = Arc::new(MockVerifier { should_fail: true });
        let executor = Arc::new(MockExecutor {
            exec_count: AtomicU64::new(0),
        });
        let acceptor = Arc::new(MockAcceptor);
        let genesis_id = Id::from_bytes([0; 32]);

        let pipeline = ExecutionPipeline::new(
            verifier,
            executor,
            acceptor,
            ExecutorConfig::default(),
            genesis_id,
        );

        let block = ExecutableBlock::new(
            Id::from_bytes([1; 32]),
            genesis_id,
            1,
            1000,
            vec![1, 2, 3],
        );

        pipeline.submit_block(block.clone()).unwrap();
        pipeline.process_pending().await.unwrap();

        assert_eq!(pipeline.block_status(block.id), BlockStatus::Rejected);
    }

    #[test]
    fn test_pipeline_stats() {
        let verifier = Arc::new(MockVerifier { should_fail: false });
        let executor = Arc::new(MockExecutor {
            exec_count: AtomicU64::new(0),
        });
        let acceptor = Arc::new(MockAcceptor);
        let genesis_id = Id::from_bytes([0; 32]);

        let pipeline = ExecutionPipeline::new(
            verifier,
            executor,
            acceptor,
            ExecutorConfig::default(),
            genesis_id,
        );

        for i in 1..=5 {
            let block = ExecutableBlock::new(
                Id::from_bytes([i; 32]),
                genesis_id,
                i as u64,
                1000,
                vec![i],
            );
            pipeline.submit_block(block).unwrap();
        }

        let stats = pipeline.stats();
        assert_eq!(stats.pending, 5);
    }
}
