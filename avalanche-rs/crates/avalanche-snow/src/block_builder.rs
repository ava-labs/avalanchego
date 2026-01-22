//! Block building implementation.
//!
//! The block builder is responsible for:
//! - Selecting transactions from the mempool
//! - Assembling them into a valid block
//! - Managing block size limits
//! - Handling parent block selection

use std::sync::Arc;
use std::time::{Duration, Instant};

use parking_lot::RwLock;

use avalanche_ids::Id;

use crate::mempool::{Mempool, MempoolTx};

/// Block builder configuration.
#[derive(Debug, Clone)]
pub struct BlockBuilderConfig {
    /// Maximum number of transactions per block.
    pub max_block_txs: usize,
    /// Maximum block size in bytes.
    pub max_block_bytes: usize,
    /// Target block time.
    pub target_block_time: Duration,
    /// Minimum time between blocks.
    pub min_block_gap: Duration,
    /// Maximum time to wait for transactions.
    pub max_block_delay: Duration,
}

impl Default for BlockBuilderConfig {
    fn default() -> Self {
        Self {
            max_block_txs: 1000,
            max_block_bytes: 2 * 1024 * 1024, // 2 MB
            target_block_time: Duration::from_secs(2),
            min_block_gap: Duration::from_millis(500),
            max_block_delay: Duration::from_secs(10),
        }
    }
}

/// A built block ready for proposal.
#[derive(Debug, Clone)]
pub struct BuiltBlock {
    /// Parent block ID.
    pub parent_id: Id,
    /// Block height.
    pub height: u64,
    /// Block timestamp (unix nanos).
    pub timestamp: u64,
    /// Transactions included in this block.
    pub txs: Vec<BlockTx>,
    /// Total size in bytes.
    pub size: usize,
}

impl BuiltBlock {
    /// Returns the number of transactions.
    pub fn tx_count(&self) -> usize {
        self.txs.len()
    }

    /// Returns the transaction IDs.
    pub fn tx_ids(&self) -> Vec<Id> {
        self.txs.iter().map(|tx| tx.id).collect()
    }

    /// Returns true if the block is empty.
    pub fn is_empty(&self) -> bool {
        self.txs.is_empty()
    }
}

/// A transaction included in a block.
#[derive(Debug, Clone)]
pub struct BlockTx {
    /// Transaction ID.
    pub id: Id,
    /// Transaction bytes.
    pub bytes: Vec<u8>,
    /// Gas price.
    pub gas_price: u64,
}

impl From<MempoolTx> for BlockTx {
    fn from(tx: MempoolTx) -> Self {
        Self {
            id: tx.id,
            bytes: tx.bytes,
            gas_price: tx.gas_price,
        }
    }
}

/// Block building state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BuilderState {
    /// Idle, waiting for transactions.
    Idle,
    /// Building a block.
    Building,
    /// Block built, waiting for proposal.
    Ready,
}

/// The block builder.
pub struct BlockBuilder {
    /// Configuration.
    config: BlockBuilderConfig,
    /// Reference to the mempool.
    mempool: Arc<Mempool>,
    /// Current builder state.
    state: RwLock<BuilderState>,
    /// Last block time.
    last_block_time: RwLock<Option<Instant>>,
    /// Current preferred parent.
    preferred_parent: RwLock<Id>,
    /// Current block height.
    current_height: RwLock<u64>,
    /// Pending built block.
    pending_block: RwLock<Option<BuiltBlock>>,
}

impl BlockBuilder {
    /// Creates a new block builder.
    pub fn new(config: BlockBuilderConfig, mempool: Arc<Mempool>) -> Self {
        Self {
            config,
            mempool,
            state: RwLock::new(BuilderState::Idle),
            last_block_time: RwLock::new(None),
            preferred_parent: RwLock::new(Id::default()),
            current_height: RwLock::new(0),
            pending_block: RwLock::new(None),
        }
    }

    /// Creates a new block builder with default configuration.
    pub fn with_defaults(mempool: Arc<Mempool>) -> Self {
        Self::new(BlockBuilderConfig::default(), mempool)
    }

    /// Returns the current builder state.
    pub fn state(&self) -> BuilderState {
        *self.state.read()
    }

    /// Sets the preferred parent for the next block.
    pub fn set_preferred(&self, parent_id: Id, height: u64) {
        *self.preferred_parent.write() = parent_id;
        *self.current_height.write() = height;
    }

    /// Returns true if we should build a block now.
    pub fn should_build(&self) -> bool {
        // Check if we have transactions
        if self.mempool.is_empty() {
            return false;
        }

        // Check minimum block gap
        if let Some(last_time) = *self.last_block_time.read() {
            if last_time.elapsed() < self.config.min_block_gap {
                return false;
            }
        }

        // Check if we have enough transactions or waited long enough
        let tx_count = self.mempool.len();
        let has_enough_txs = tx_count >= self.config.max_block_txs / 2;
        let waited_long_enough = self
            .last_block_time
            .read()
            .map(|t| t.elapsed() >= self.config.target_block_time)
            .unwrap_or(true);

        has_enough_txs || waited_long_enough
    }

    /// Builds a block from mempool transactions.
    pub fn build(&self) -> Option<BuiltBlock> {
        *self.state.write() = BuilderState::Building;

        let parent_id = *self.preferred_parent.read();
        let height = *self.current_height.read() + 1;

        // Select transactions from mempool
        let mempool_txs = self
            .mempool
            .peek(self.config.max_block_txs, self.config.max_block_bytes);

        if mempool_txs.is_empty() {
            *self.state.write() = BuilderState::Idle;
            return None;
        }

        // Convert to block transactions
        let txs: Vec<BlockTx> = mempool_txs.iter().map(|tx| BlockTx::from(tx.clone())).collect();

        let size: usize = txs.iter().map(|tx| tx.bytes.len()).sum();

        // Mark transactions as issued
        let tx_ids: Vec<Id> = txs.iter().map(|tx| tx.id).collect();
        self.mempool.mark_issued(&tx_ids);

        // Create the built block
        let timestamp = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos() as u64;

        let block = BuiltBlock {
            parent_id,
            height,
            timestamp,
            txs,
            size,
        };

        *self.pending_block.write() = Some(block.clone());
        *self.state.write() = BuilderState::Ready;

        Some(block)
    }

    /// Called when a block is accepted.
    pub fn on_block_accepted(&self, block_id: Id, tx_ids: &[Id]) {
        // Remove accepted transactions from mempool
        self.mempool.remove_batch(tx_ids);

        // Update preferred parent
        let height = *self.current_height.read();
        self.set_preferred(block_id, height + 1);

        // Clear pending block
        *self.pending_block.write() = None;
        *self.last_block_time.write() = Some(Instant::now());
        *self.state.write() = BuilderState::Idle;
    }

    /// Called when a block is rejected.
    pub fn on_block_rejected(&self, tx_ids: &[Id]) {
        // Unmark transactions as issued so they can be re-selected
        self.mempool.unmark_issued(tx_ids);

        // Clear pending block
        *self.pending_block.write() = None;
        *self.state.write() = BuilderState::Idle;
    }

    /// Returns the pending block if any.
    pub fn pending_block(&self) -> Option<BuiltBlock> {
        self.pending_block.read().clone()
    }

    /// Clears any pending block.
    pub fn clear_pending(&self) {
        if let Some(block) = self.pending_block.write().take() {
            // Unmark transactions
            let tx_ids: Vec<Id> = block.txs.iter().map(|tx| tx.id).collect();
            self.mempool.unmark_issued(&tx_ids);
        }
        *self.state.write() = BuilderState::Idle;
    }

    /// Forces a build attempt, even if conditions aren't ideal.
    pub fn force_build(&self) -> Option<BuiltBlock> {
        if self.mempool.is_empty() {
            return None;
        }
        self.build()
    }
}

/// Trait for types that can build blocks.
pub trait BlockProducer: Send + Sync {
    /// Returns true if a block should be built.
    fn should_build_block(&self) -> bool;

    /// Builds a new block.
    fn build_block(&self) -> Option<BuiltBlock>;

    /// Notifies that a block was accepted.
    fn notify_block_accepted(&self, block_id: Id, tx_ids: &[Id]);

    /// Notifies that a block was rejected.
    fn notify_block_rejected(&self, tx_ids: &[Id]);
}

impl BlockProducer for BlockBuilder {
    fn should_build_block(&self) -> bool {
        self.should_build()
    }

    fn build_block(&self) -> Option<BuiltBlock> {
        self.build()
    }

    fn notify_block_accepted(&self, block_id: Id, tx_ids: &[Id]) {
        self.on_block_accepted(block_id, tx_ids);
    }

    fn notify_block_rejected(&self, tx_ids: &[Id]) {
        self.on_block_rejected(tx_ids);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet;

    fn setup() -> (Arc<Mempool>, BlockBuilder) {
        let mempool = Arc::new(Mempool::with_defaults());
        let builder = BlockBuilder::with_defaults(mempool.clone());
        (mempool, builder)
    }

    fn add_tx(mempool: &Mempool, id_byte: u8, gas_price: u64, size: usize) -> Id {
        let id = Id::from_bytes([id_byte; 32]);
        let bytes = vec![0u8; size];
        let mut inputs = HashSet::new();
        inputs.insert(Id::from_bytes([id_byte + 100; 32]));
        mempool.add(id, bytes, gas_price, inputs);
        id
    }

    #[test]
    fn test_build_empty_mempool() {
        let (_, builder) = setup();
        assert!(!builder.should_build());
        assert!(builder.build().is_none());
    }

    #[test]
    fn test_build_with_transactions() {
        let (mempool, builder) = setup();

        // Add some transactions
        let id1 = add_tx(&mempool, 1, 100, 500);
        let id2 = add_tx(&mempool, 2, 200, 500);
        let id3 = add_tx(&mempool, 3, 150, 500);

        builder.set_preferred(Id::from_bytes([0; 32]), 0);

        let block = builder.build().unwrap();

        assert_eq!(block.tx_count(), 3);
        assert_eq!(block.height, 1);

        // Highest gas price should be first
        assert_eq!(block.txs[0].gas_price, 200);
        assert_eq!(block.txs[1].gas_price, 150);
        assert_eq!(block.txs[2].gas_price, 100);
    }

    #[test]
    fn test_on_block_accepted() {
        let (mempool, builder) = setup();

        let id1 = add_tx(&mempool, 1, 100, 500);
        let id2 = add_tx(&mempool, 2, 200, 500);

        builder.set_preferred(Id::from_bytes([0; 32]), 0);
        let block = builder.build().unwrap();
        let block_id = Id::from_bytes([99; 32]);
        let tx_ids = block.tx_ids();

        builder.on_block_accepted(block_id, &tx_ids);

        // Transactions should be removed from mempool
        assert_eq!(mempool.len(), 0);
        assert!(builder.pending_block().is_none());
        assert_eq!(builder.state(), BuilderState::Idle);
    }

    #[test]
    fn test_on_block_rejected() {
        let (mempool, builder) = setup();

        add_tx(&mempool, 1, 100, 500);
        add_tx(&mempool, 2, 200, 500);

        builder.set_preferred(Id::from_bytes([0; 32]), 0);
        let block = builder.build().unwrap();
        let tx_ids = block.tx_ids();

        builder.on_block_rejected(&tx_ids);

        // Transactions should still be in mempool and unmarked
        assert_eq!(mempool.len(), 2);
        let top = mempool.peek(10, 10000);
        assert_eq!(top.len(), 2); // All should be available again
    }

    #[test]
    fn test_block_size_limit() {
        let (mempool, builder) = setup();

        let config = BlockBuilderConfig {
            max_block_bytes: 1000,
            ..Default::default()
        };
        let builder = BlockBuilder::new(config, mempool.clone());

        // Add transactions that exceed block size
        add_tx(&mempool, 1, 100, 400);
        add_tx(&mempool, 2, 200, 400);
        add_tx(&mempool, 3, 150, 400); // This one won't fit

        builder.set_preferred(Id::from_bytes([0; 32]), 0);
        let block = builder.build().unwrap();

        // Only first two should fit (800 bytes)
        assert_eq!(block.tx_count(), 2);
        assert!(block.size <= 1000);
    }

    #[test]
    fn test_state_transitions() {
        let (mempool, builder) = setup();

        assert_eq!(builder.state(), BuilderState::Idle);

        add_tx(&mempool, 1, 100, 500);
        builder.set_preferred(Id::from_bytes([0; 32]), 0);

        let block = builder.build().unwrap();
        assert_eq!(builder.state(), BuilderState::Ready);

        builder.on_block_accepted(Id::from_bytes([99; 32]), &block.tx_ids());
        assert_eq!(builder.state(), BuilderState::Idle);
    }

    #[test]
    fn test_clear_pending() {
        let (mempool, builder) = setup();

        add_tx(&mempool, 1, 100, 500);
        builder.set_preferred(Id::from_bytes([0; 32]), 0);

        builder.build();
        assert!(builder.pending_block().is_some());

        builder.clear_pending();
        assert!(builder.pending_block().is_none());

        // Transaction should be available again
        let top = mempool.peek(10, 10000);
        assert_eq!(top.len(), 1);
    }

    #[test]
    fn test_min_block_gap() {
        let (mempool, builder) = setup();

        add_tx(&mempool, 1, 100, 500);
        builder.set_preferred(Id::from_bytes([0; 32]), 0);

        // First block
        let block = builder.build().unwrap();
        builder.on_block_accepted(Id::from_bytes([1; 32]), &block.tx_ids());

        // Add more transactions
        add_tx(&mempool, 2, 100, 500);

        // Should not build immediately due to min_block_gap
        assert!(!builder.should_build());
    }
}
