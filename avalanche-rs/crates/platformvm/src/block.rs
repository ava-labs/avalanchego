//! Platform VM block types.

use async_trait::async_trait;
use avalanche_ids::Id;
use avalanche_vm::{BlockStatus, Result, VMError};
use chrono::{DateTime, Utc};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::txs::Transaction;

/// Platform block header.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BlockHeader {
    /// Parent block ID
    pub parent_id: Id,
    /// Block height
    pub height: u64,
    /// Block timestamp
    pub timestamp: u64,
}

/// Types of platform blocks.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum PlatformBlockType {
    /// Standard block with transactions
    Standard(StandardBlock),
    /// Proposal block (proposes a validator change)
    Proposal(ProposalBlock),
    /// Commit block (commits proposed change)
    Commit(CommitBlock),
    /// Abort block (aborts proposed change)
    Abort(AbortBlock),
}

/// Standard block containing transactions.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StandardBlock {
    /// Block header
    pub header: BlockHeader,
    /// Transactions
    pub txs: Vec<Transaction>,
}

/// Proposal block for validator changes.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProposalBlock {
    /// Block header
    pub header: BlockHeader,
    /// The proposed transaction
    pub tx: Transaction,
}

/// Commit block - accepts the parent proposal.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommitBlock {
    /// Block header
    pub header: BlockHeader,
}

/// Abort block - rejects the parent proposal.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AbortBlock {
    /// Block header
    pub header: BlockHeader,
}

/// A platform block.
pub struct PlatformBlock {
    /// Block ID
    pub id: Id,
    /// Block type and data
    pub block_type: PlatformBlockType,
    /// Current status
    status: RwLock<BlockStatus>,
    /// Serialized bytes
    pub bytes: Vec<u8>,
}

impl std::fmt::Debug for PlatformBlock {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PlatformBlock")
            .field("id", &self.id)
            .field("block_type", &self.block_type)
            .field("status", &*self.status.read())
            .finish_non_exhaustive()
    }
}

impl PlatformBlock {
    /// Creates a new platform block from block type.
    pub fn new(block_type: PlatformBlockType) -> Result<Self> {
        let bytes = serde_json::to_vec(&block_type)
            .map_err(|e| VMError::InvalidBlock(e.to_string()))?;
        let hash = Sha256::digest(&bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        Ok(Self {
            id,
            block_type,
            status: RwLock::new(BlockStatus::Processing),
            bytes,
        })
    }

    /// Creates a new standard block.
    pub fn new_standard(
        parent_id: Id,
        height: u64,
        timestamp: DateTime<Utc>,
        txs: Vec<Transaction>,
    ) -> Result<Self> {
        let block_type = PlatformBlockType::Standard(StandardBlock {
            header: BlockHeader {
                parent_id,
                height,
                timestamp: timestamp.timestamp() as u64,
            },
            txs,
        });
        Self::new(block_type)
    }

    /// Parses a platform block from bytes.
    pub fn parse(bytes: &[u8]) -> Result<Self> {
        let block_type: PlatformBlockType = serde_json::from_slice(bytes)
            .map_err(|e| VMError::InvalidBlock(format!("failed to parse block: {}", e)))?;

        let hash = Sha256::digest(bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        Ok(Self {
            id,
            block_type,
            status: RwLock::new(BlockStatus::Processing),
            bytes: bytes.to_vec(),
        })
    }

    /// Returns the block header.
    pub fn header(&self) -> &BlockHeader {
        match &self.block_type {
            PlatformBlockType::Standard(b) => &b.header,
            PlatformBlockType::Proposal(b) => &b.header,
            PlatformBlockType::Commit(b) => &b.header,
            PlatformBlockType::Abort(b) => &b.header,
        }
    }

    /// Returns the block type.
    pub fn block_type(&self) -> &PlatformBlockType {
        &self.block_type
    }

    /// Returns transactions if this is a standard block.
    pub fn transactions(&self) -> Option<&[Transaction]> {
        match &self.block_type {
            PlatformBlockType::Standard(b) => Some(&b.txs),
            _ => None,
        }
    }

    /// Returns the proposed transaction if this is a proposal block.
    pub fn proposed_tx(&self) -> Option<&Transaction> {
        match &self.block_type {
            PlatformBlockType::Proposal(b) => Some(&b.tx),
            _ => None,
        }
    }

    /// Verifies all transactions in the block.
    fn verify_transactions(&self) -> Result<()> {
        match &self.block_type {
            PlatformBlockType::Standard(b) => {
                for tx in &b.txs {
                    tx.validate()?;
                }
            }
            PlatformBlockType::Proposal(b) => {
                b.tx.validate()?;
            }
            _ => {}
        }
        Ok(())
    }
}

#[async_trait]
impl avalanche_vm::Block for PlatformBlock {
    fn id(&self) -> Id {
        self.id
    }

    fn parent(&self) -> Id {
        self.header().parent_id
    }

    fn height(&self) -> u64 {
        self.header().height
    }

    fn timestamp(&self) -> DateTime<Utc> {
        DateTime::from_timestamp(self.header().timestamp as i64, 0)
            .unwrap_or_else(Utc::now)
    }

    fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    fn status(&self) -> BlockStatus {
        *self.status.read()
    }

    async fn verify(&self) -> Result<()> {
        // Check block hasn't already been decided
        let status = *self.status.read();
        if status.decided() {
            return if status.accepted() {
                Ok(())
            } else {
                Err(VMError::InvalidBlock("block was rejected".to_string()))
            };
        }

        // Verify timestamp is reasonable
        let header = self.header();
        if header.timestamp == 0 {
            return Err(VMError::InvalidBlock("invalid timestamp".to_string()));
        }

        // Verify transactions
        self.verify_transactions()?;

        Ok(())
    }

    async fn accept(&mut self) -> Result<()> {
        *self.status.write() = BlockStatus::Accepted;
        Ok(())
    }

    async fn reject(&mut self) -> Result<()> {
        *self.status.write() = BlockStatus::Rejected;
        Ok(())
    }
}

/// Builder for creating platform blocks.
pub struct BlockBuilder {
    parent_id: Id,
    height: u64,
    timestamp: u64,
    txs: Vec<Transaction>,
}

impl BlockBuilder {
    /// Creates a new block builder.
    pub fn new(parent_id: Id, height: u64) -> Self {
        Self {
            parent_id,
            height,
            timestamp: Utc::now().timestamp() as u64,
            txs: Vec::new(),
        }
    }

    /// Sets the timestamp.
    pub fn with_timestamp(mut self, timestamp: u64) -> Self {
        self.timestamp = timestamp;
        self
    }

    /// Adds a transaction.
    pub fn with_transaction(mut self, tx: Transaction) -> Self {
        self.txs.push(tx);
        self
    }

    /// Adds multiple transactions.
    pub fn with_transactions(mut self, txs: Vec<Transaction>) -> Self {
        self.txs.extend(txs);
        self
    }

    /// Builds a standard block.
    pub fn build_standard(self) -> PlatformBlockType {
        PlatformBlockType::Standard(StandardBlock {
            header: BlockHeader {
                parent_id: self.parent_id,
                height: self.height,
                timestamp: self.timestamp,
            },
            txs: self.txs,
        })
    }

    /// Builds a proposal block.
    pub fn build_proposal(self, tx: Transaction) -> PlatformBlockType {
        PlatformBlockType::Proposal(ProposalBlock {
            header: BlockHeader {
                parent_id: self.parent_id,
                height: self.height,
                timestamp: self.timestamp,
            },
            tx,
        })
    }

    /// Builds a commit block.
    pub fn build_commit(self) -> PlatformBlockType {
        PlatformBlockType::Commit(CommitBlock {
            header: BlockHeader {
                parent_id: self.parent_id,
                height: self.height,
                timestamp: self.timestamp,
            },
        })
    }

    /// Builds an abort block.
    pub fn build_abort(self) -> PlatformBlockType {
        PlatformBlockType::Abort(AbortBlock {
            header: BlockHeader {
                parent_id: self.parent_id,
                height: self.height,
                timestamp: self.timestamp,
            },
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_block_builder() {
        let parent = Id::default();
        let block_type = BlockBuilder::new(parent, 1)
            .with_timestamp(12345)
            .build_standard();

        match block_type {
            PlatformBlockType::Standard(b) => {
                assert_eq!(b.header.parent_id, parent);
                assert_eq!(b.header.height, 1);
                assert_eq!(b.header.timestamp, 12345);
                assert!(b.txs.is_empty());
            }
            _ => panic!("expected standard block"),
        }
    }

    #[test]
    fn test_commit_abort_blocks() {
        let parent = Id::default();

        let commit = BlockBuilder::new(parent, 2).build_commit();
        assert!(matches!(commit, PlatformBlockType::Commit(_)));

        let abort = BlockBuilder::new(parent, 2).build_abort();
        assert!(matches!(abort, PlatformBlockType::Abort(_)));
    }
}
