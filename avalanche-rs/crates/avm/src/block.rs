//! AVM block types.

use async_trait::async_trait;
use avalanche_ids::Id;
use avalanche_vm::{BlockStatus, Result, VMError};
use chrono::{DateTime, Utc};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::txs::SignedTx;

/// AVM block header.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BlockHeader {
    /// Parent block ID
    pub parent_id: Id,
    /// Block height
    pub height: u64,
    /// Block timestamp
    pub timestamp: u64,
}

/// AVM block data.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AVMBlockData {
    /// Block header
    pub header: BlockHeader,
    /// Transactions in this block
    pub txs: Vec<SignedTx>,
}

/// An AVM block.
pub struct AVMBlock {
    /// Block ID
    pub id: Id,
    /// Block data
    pub data: AVMBlockData,
    /// Current status
    status: RwLock<BlockStatus>,
    /// Serialized bytes
    pub bytes: Vec<u8>,
}

impl std::fmt::Debug for AVMBlock {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("AVMBlock")
            .field("id", &self.id)
            .field("height", &self.data.header.height)
            .field("tx_count", &self.data.txs.len())
            .field("status", &*self.status.read())
            .finish_non_exhaustive()
    }
}

impl AVMBlock {
    /// Creates a new AVM block.
    pub fn new(data: AVMBlockData) -> Result<Self> {
        let bytes = serde_json::to_vec(&data)
            .map_err(|e| VMError::InvalidBlock(e.to_string()))?;
        let hash = Sha256::digest(&bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        Ok(Self {
            id,
            data,
            status: RwLock::new(BlockStatus::Processing),
            bytes,
        })
    }

    /// Creates a new standard block.
    pub fn new_standard(
        parent_id: Id,
        height: u64,
        timestamp: DateTime<Utc>,
        txs: Vec<SignedTx>,
    ) -> Result<Self> {
        let data = AVMBlockData {
            header: BlockHeader {
                parent_id,
                height,
                timestamp: timestamp.timestamp() as u64,
            },
            txs,
        };
        Self::new(data)
    }

    /// Parses an AVM block from bytes.
    pub fn parse(bytes: &[u8]) -> Result<Self> {
        let data: AVMBlockData = serde_json::from_slice(bytes)
            .map_err(|e| VMError::InvalidBlock(format!("failed to parse block: {}", e)))?;

        let hash = Sha256::digest(bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        Ok(Self {
            id,
            data,
            status: RwLock::new(BlockStatus::Processing),
            bytes: bytes.to_vec(),
        })
    }

    /// Returns the block header.
    pub fn header(&self) -> &BlockHeader {
        &self.data.header
    }

    /// Returns the transactions.
    pub fn transactions(&self) -> &[SignedTx] {
        &self.data.txs
    }

    /// Returns the number of transactions.
    pub fn tx_count(&self) -> usize {
        self.data.txs.len()
    }

    /// Verifies all transactions in the block.
    fn verify_transactions(&self) -> Result<()> {
        for tx in &self.data.txs {
            tx.validate()?;
        }
        Ok(())
    }
}

#[async_trait]
impl avalanche_vm::Block for AVMBlock {
    fn id(&self) -> Id {
        self.id
    }

    fn parent(&self) -> Id {
        self.data.header.parent_id
    }

    fn height(&self) -> u64 {
        self.data.header.height
    }

    fn timestamp(&self) -> DateTime<Utc> {
        DateTime::from_timestamp(self.data.header.timestamp as i64, 0)
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
        if self.data.header.timestamp == 0 {
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

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_vm::Block as BlockTrait;

    #[test]
    fn test_block_creation() {
        let block = AVMBlock::new_standard(
            Id::default(),
            1,
            Utc::now(),
            vec![],
        ).unwrap();

        assert_eq!(BlockTrait::height(&block), 1);
        assert_eq!(block.tx_count(), 0);
        assert_eq!(BlockTrait::status(&block), BlockStatus::Processing);
    }

    #[test]
    fn test_block_parse() {
        let original = AVMBlock::new_standard(
            Id::default(),
            5,
            Utc::now(),
            vec![],
        ).unwrap();

        let parsed = AVMBlock::parse(&original.bytes).unwrap();
        assert_eq!(BlockTrait::height(&parsed), 5);
        assert_eq!(parsed.id, original.id);
    }
}
