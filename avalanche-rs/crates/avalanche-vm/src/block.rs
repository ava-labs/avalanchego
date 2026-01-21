//! Block abstractions.

use async_trait::async_trait;
use chrono::{DateTime, Utc};

use avalanche_ids::Id;

use crate::Result;

/// Status of a block in consensus.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum BlockStatus {
    /// Being processed by consensus
    Processing,
    /// Accepted and finalized
    Accepted,
    /// Rejected
    Rejected,
    /// Unknown (not in memory)
    Unknown,
}

impl BlockStatus {
    /// Returns true if the block has been decided (accepted or rejected).
    pub fn decided(&self) -> bool {
        matches!(self, BlockStatus::Accepted | BlockStatus::Rejected)
    }

    /// Returns true if the block was accepted.
    pub fn accepted(&self) -> bool {
        matches!(self, BlockStatus::Accepted)
    }

    /// Returns true if the block was rejected.
    pub fn rejected(&self) -> bool {
        matches!(self, BlockStatus::Rejected)
    }

    /// Returns true if the block is being processed.
    pub fn processing(&self) -> bool {
        matches!(self, BlockStatus::Processing)
    }
}

impl Default for BlockStatus {
    fn default() -> Self {
        BlockStatus::Unknown
    }
}

/// A stateless block (just data, no VM reference).
#[derive(Debug, Clone)]
pub struct StatelessBlock {
    /// Block ID
    pub id: Id,
    /// Parent block ID
    pub parent_id: Id,
    /// Block height
    pub height: u64,
    /// Block timestamp
    pub timestamp: DateTime<Utc>,
    /// Serialized block bytes
    pub bytes: Vec<u8>,
}

impl StatelessBlock {
    /// Creates a new stateless block.
    pub fn new(id: Id, parent_id: Id, height: u64, timestamp: DateTime<Utc>, bytes: Vec<u8>) -> Self {
        Self {
            id,
            parent_id,
            height,
            timestamp,
            bytes,
        }
    }
}

/// A block in the blockchain.
///
/// Blocks are the fundamental unit of consensus in Snowman-based chains.
#[async_trait]
pub trait Block: Send + Sync {
    /// Returns the unique identifier for this block.
    fn id(&self) -> Id;

    /// Returns the parent block's ID.
    fn parent(&self) -> Id;

    /// Returns the block's height.
    fn height(&self) -> u64;

    /// Returns the block's timestamp.
    fn timestamp(&self) -> DateTime<Utc>;

    /// Returns the block's byte representation.
    fn bytes(&self) -> &[u8];

    /// Returns the current status of this block.
    fn status(&self) -> BlockStatus;

    /// Verifies that this block is valid.
    ///
    /// This should check:
    /// - Block structure is valid
    /// - Parent exists and is valid
    /// - Transactions are valid
    /// - State transitions are valid
    async fn verify(&self) -> Result<()>;

    /// Accepts this block as finalized.
    ///
    /// Called when consensus has decided this block is accepted.
    /// The VM should:
    /// - Persist the block
    /// - Update state
    /// - Mark as accepted
    async fn accept(&mut self) -> Result<()>;

    /// Rejects this block.
    ///
    /// Called when consensus has decided this block is rejected.
    /// The VM should:
    /// - Remove from processing
    /// - Mark as rejected
    async fn reject(&mut self) -> Result<()>;
}

/// Options for building blocks.
#[derive(Debug, Clone, Default)]
pub struct BuildBlockOptions {
    /// Maximum transactions to include
    pub max_txs: Option<usize>,
    /// Maximum block size in bytes
    pub max_size: Option<usize>,
    /// Target timestamp
    pub timestamp: Option<DateTime<Utc>>,
}

impl BuildBlockOptions {
    /// Creates new build options.
    pub fn new() -> Self {
        Self::default()
    }

    /// Sets maximum transactions.
    pub fn with_max_txs(mut self, max: usize) -> Self {
        self.max_txs = Some(max);
        self
    }

    /// Sets maximum size.
    pub fn with_max_size(mut self, max: usize) -> Self {
        self.max_size = Some(max);
        self
    }

    /// Sets target timestamp.
    pub fn with_timestamp(mut self, ts: DateTime<Utc>) -> Self {
        self.timestamp = Some(ts);
        self
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_block_status() {
        assert!(BlockStatus::Accepted.decided());
        assert!(BlockStatus::Rejected.decided());
        assert!(!BlockStatus::Processing.decided());
        assert!(!BlockStatus::Unknown.decided());

        assert!(BlockStatus::Accepted.accepted());
        assert!(!BlockStatus::Rejected.accepted());

        assert!(BlockStatus::Processing.processing());
        assert!(!BlockStatus::Accepted.processing());
    }

    #[test]
    fn test_stateless_block() {
        let id = Id::from_slice(&[1; 32]).unwrap();
        let parent = Id::from_slice(&[0; 32]).unwrap();
        let block = StatelessBlock::new(id, parent, 1, Utc::now(), vec![1, 2, 3]);

        assert_eq!(block.id, id);
        assert_eq!(block.parent_id, parent);
        assert_eq!(block.height, 1);
        assert_eq!(block.bytes, vec![1, 2, 3]);
    }

    #[test]
    fn test_build_block_options() {
        let opts = BuildBlockOptions::new()
            .with_max_txs(100)
            .with_max_size(1024);

        assert_eq!(opts.max_txs, Some(100));
        assert_eq!(opts.max_size, Some(1024));
    }
}
