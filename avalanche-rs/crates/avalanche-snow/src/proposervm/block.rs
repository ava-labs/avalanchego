//! ProposerVM block types.
//!
//! Supports both pre-fork (simple wrapper) and post-fork (with proposer info) blocks.

use std::time::{SystemTime, UNIX_EPOCH};

use sha2::{Digest, Sha256};

use avalanche_ids::{Id, NodeId};

use crate::error::Result;
use crate::vm::{Block as VMBlock, BlockStatus, VMError};

/// A ProposerVM block - either pre-fork or post-fork.
#[derive(Debug, Clone)]
pub enum ProposerBlock {
    /// Pre-fork block (simple wrapper).
    PreFork(PreForkBlock),
    /// Post-fork block (with proposer certificate).
    PostFork(PostForkBlock),
}

impl ProposerBlock {
    /// Parses a ProposerBlock from bytes.
    pub fn parse(bytes: &[u8]) -> Result<Self> {
        if bytes.is_empty() {
            return Err(VMError::InvalidBlock("empty block bytes".to_string()));
        }

        // Check version byte
        match bytes[0] {
            0 => {
                // Pre-fork block
                let block = PreForkBlock::parse(&bytes[1..])?;
                Ok(ProposerBlock::PreFork(block))
            }
            1 => {
                // Post-fork block
                let block = PostForkBlock::parse(&bytes[1..])?;
                Ok(ProposerBlock::PostFork(block))
            }
            v => Err(VMError::InvalidBlock(format!("unknown block version: {}", v))),
        }
    }

    /// Returns the inner block bytes.
    pub fn inner_bytes(&self) -> &[u8] {
        match self {
            ProposerBlock::PreFork(b) => b.inner_bytes(),
            ProposerBlock::PostFork(b) => b.inner_bytes(),
        }
    }

    /// Returns whether this is a post-fork block.
    pub fn is_post_fork(&self) -> bool {
        matches!(self, ProposerBlock::PostFork(_))
    }

    /// Returns the proposer if post-fork.
    pub fn proposer(&self) -> Option<NodeId> {
        match self {
            ProposerBlock::PreFork(_) => None,
            ProposerBlock::PostFork(b) => Some(b.proposer()),
        }
    }
}

impl VMBlock for ProposerBlock {
    fn id(&self) -> Id {
        match self {
            ProposerBlock::PreFork(b) => b.id(),
            ProposerBlock::PostFork(b) => b.id(),
        }
    }

    fn parent(&self) -> Id {
        match self {
            ProposerBlock::PreFork(b) => b.parent(),
            ProposerBlock::PostFork(b) => b.parent(),
        }
    }

    fn height(&self) -> u64 {
        match self {
            ProposerBlock::PreFork(b) => b.height(),
            ProposerBlock::PostFork(b) => b.height(),
        }
    }

    fn timestamp(&self) -> u64 {
        match self {
            ProposerBlock::PreFork(b) => b.timestamp(),
            ProposerBlock::PostFork(b) => b.timestamp(),
        }
    }

    fn bytes(&self) -> &[u8] {
        match self {
            ProposerBlock::PreFork(b) => b.bytes(),
            ProposerBlock::PostFork(b) => b.bytes(),
        }
    }

    fn verify(&self) -> Result<()> {
        match self {
            ProposerBlock::PreFork(b) => b.verify(),
            ProposerBlock::PostFork(b) => b.verify(),
        }
    }

    fn accept(&mut self) -> Result<()> {
        match self {
            ProposerBlock::PreFork(b) => b.accept(),
            ProposerBlock::PostFork(b) => b.accept(),
        }
    }

    fn reject(&mut self) -> Result<()> {
        match self {
            ProposerBlock::PreFork(b) => b.reject(),
            ProposerBlock::PostFork(b) => b.reject(),
        }
    }

    fn status(&self) -> BlockStatus {
        match self {
            ProposerBlock::PreFork(b) => b.status(),
            ProposerBlock::PostFork(b) => b.status(),
        }
    }
}

/// Pre-fork block - simple wrapper around inner block.
#[derive(Debug, Clone)]
pub struct PreForkBlock {
    /// Block ID.
    id: Id,
    /// Parent block ID.
    parent_id: Id,
    /// Block height.
    height: u64,
    /// Inner block bytes.
    inner_bytes: Vec<u8>,
    /// Serialized bytes.
    bytes: Vec<u8>,
    /// Block status.
    status: BlockStatus,
    /// Timestamp.
    timestamp: u64,
}

impl PreForkBlock {
    /// Creates a new pre-fork block.
    pub fn new(inner_bytes: Vec<u8>, inner_id: Id, parent_id: Id, height: u64) -> Self {
        // Serialize: version (1 byte) + height (8 bytes) + parent (32 bytes) + inner
        let mut bytes = Vec::with_capacity(1 + 8 + 32 + inner_bytes.len());
        bytes.push(0); // version = 0 for pre-fork
        bytes.extend_from_slice(&height.to_be_bytes());
        bytes.extend_from_slice(parent_id.as_bytes());
        bytes.extend_from_slice(&inner_bytes);

        // Compute ID from bytes
        let hash = Sha256::digest(&bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        Self {
            id,
            parent_id,
            height,
            inner_bytes,
            bytes,
            status: BlockStatus::Processing,
            timestamp,
        }
    }

    /// Parses a pre-fork block from bytes (without version byte).
    pub fn parse(bytes: &[u8]) -> Result<Self> {
        if bytes.len() < 40 {
            return Err(VMError::InvalidBlock("pre-fork block too short".to_string()));
        }

        let height = u64::from_be_bytes(bytes[0..8].try_into().unwrap());
        let parent_id = Id::from_slice(&bytes[8..40])
            .map_err(|_| VMError::InvalidBlock("invalid parent ID".to_string()))?;
        let inner_bytes = bytes[40..].to_vec();

        // Recompute full bytes with version
        let mut full_bytes = Vec::with_capacity(1 + bytes.len());
        full_bytes.push(0);
        full_bytes.extend_from_slice(bytes);

        let hash = Sha256::digest(&full_bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        // Compute inner ID
        let inner_hash = Sha256::digest(&inner_bytes);
        let _inner_id = Id::from_slice(&inner_hash).unwrap_or_default();

        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        Ok(Self {
            id,
            parent_id,
            height,
            inner_bytes,
            bytes: full_bytes,
            status: BlockStatus::Processing,
            timestamp,
        })
    }

    /// Returns the inner block bytes.
    pub fn inner_bytes(&self) -> &[u8] {
        &self.inner_bytes
    }
}

impl VMBlock for PreForkBlock {
    fn id(&self) -> Id {
        self.id
    }

    fn parent(&self) -> Id {
        self.parent_id
    }

    fn height(&self) -> u64 {
        self.height
    }

    fn timestamp(&self) -> u64 {
        self.timestamp
    }

    fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    fn verify(&self) -> Result<()> {
        // Pre-fork blocks have minimal verification
        if self.inner_bytes.is_empty() {
            return Err(VMError::InvalidBlock("empty inner block".to_string()));
        }
        Ok(())
    }

    fn accept(&mut self) -> Result<()> {
        self.status = BlockStatus::Accepted;
        Ok(())
    }

    fn reject(&mut self) -> Result<()> {
        self.status = BlockStatus::Rejected;
        Ok(())
    }

    fn status(&self) -> BlockStatus {
        self.status
    }
}

/// Post-fork block with proposer certificate.
#[derive(Debug, Clone)]
pub struct PostForkBlock {
    /// Block ID.
    id: Id,
    /// Parent block ID.
    parent_id: Id,
    /// Inner block bytes.
    inner_bytes: Vec<u8>,
    /// Serialized bytes.
    bytes: Vec<u8>,
    /// Block timestamp (Unix seconds).
    timestamp: u64,
    /// P-chain height at time of proposal.
    pchain_height: u64,
    /// Proposer node ID.
    proposer: NodeId,
    /// Proposer certificate signature.
    certificate: Vec<u8>,
    /// Block status.
    status: BlockStatus,
}

impl PostForkBlock {
    /// Creates a new post-fork block.
    pub fn new(
        parent_id: Id,
        inner_bytes: Vec<u8>,
        timestamp: u64,
        pchain_height: u64,
        proposer: NodeId,
    ) -> Self {
        // Serialize: version (1) + timestamp (8) + pchain_height (8) + parent (32) + proposer (20) + inner
        let mut bytes = Vec::with_capacity(1 + 8 + 8 + 32 + 20 + inner_bytes.len());
        bytes.push(1); // version = 1 for post-fork
        bytes.extend_from_slice(&timestamp.to_be_bytes());
        bytes.extend_from_slice(&pchain_height.to_be_bytes());
        bytes.extend_from_slice(parent_id.as_bytes());
        bytes.extend_from_slice(proposer.as_bytes());
        bytes.extend_from_slice(&inner_bytes);

        // Compute ID from bytes
        let hash = Sha256::digest(&bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        Self {
            id,
            parent_id,
            inner_bytes,
            bytes,
            timestamp,
            pchain_height,
            proposer,
            certificate: Vec::new(),
            status: BlockStatus::Processing,
        }
    }

    /// Creates a signed post-fork block.
    pub fn new_signed(
        parent_id: Id,
        inner_bytes: Vec<u8>,
        timestamp: u64,
        pchain_height: u64,
        proposer: NodeId,
        certificate: Vec<u8>,
    ) -> Self {
        let mut block = Self::new(parent_id, inner_bytes, timestamp, pchain_height, proposer);
        block.certificate = certificate;
        // Recompute ID with certificate
        block.bytes.extend_from_slice(&block.certificate);
        let hash = Sha256::digest(&block.bytes);
        block.id = Id::from_slice(&hash).unwrap_or_default();
        block
    }

    /// Parses a post-fork block from bytes (without version byte).
    pub fn parse(bytes: &[u8]) -> Result<Self> {
        if bytes.len() < 68 {
            // 8 + 8 + 32 + 20 = 68 minimum
            return Err(VMError::InvalidBlock("post-fork block too short".to_string()));
        }

        let timestamp = u64::from_be_bytes(bytes[0..8].try_into().unwrap());
        let pchain_height = u64::from_be_bytes(bytes[8..16].try_into().unwrap());
        let parent_id = Id::from_slice(&bytes[16..48])
            .map_err(|_| VMError::InvalidBlock("invalid parent ID".to_string()))?;
        let proposer = NodeId::from_slice(&bytes[48..68])
            .map_err(|_| VMError::InvalidBlock("invalid proposer ID".to_string()))?;
        let inner_bytes = bytes[68..].to_vec();

        // Recompute full bytes with version
        let mut full_bytes = Vec::with_capacity(1 + bytes.len());
        full_bytes.push(1);
        full_bytes.extend_from_slice(bytes);

        let hash = Sha256::digest(&full_bytes);
        let id = Id::from_slice(&hash).unwrap_or_default();

        Ok(Self {
            id,
            parent_id,
            inner_bytes,
            bytes: full_bytes,
            timestamp,
            pchain_height,
            proposer,
            certificate: Vec::new(),
            status: BlockStatus::Processing,
        })
    }

    /// Returns the inner block bytes.
    pub fn inner_bytes(&self) -> &[u8] {
        &self.inner_bytes
    }

    /// Returns the P-chain height.
    pub fn pchain_height(&self) -> u64 {
        self.pchain_height
    }

    /// Returns the proposer.
    pub fn proposer(&self) -> NodeId {
        self.proposer
    }

    /// Returns the certificate.
    pub fn certificate(&self) -> &[u8] {
        &self.certificate
    }

    /// Returns the parent ID.
    pub fn parent_id(&self) -> Id {
        self.parent_id
    }

    /// Computes height from inner block (would need to parse inner).
    /// For now returns pchain_height as approximation.
    pub fn height(&self) -> u64 {
        self.pchain_height + 1
    }
}

impl VMBlock for PostForkBlock {
    fn id(&self) -> Id {
        self.id
    }

    fn parent(&self) -> Id {
        self.parent_id
    }

    fn height(&self) -> u64 {
        self.height()
    }

    fn timestamp(&self) -> u64 {
        self.timestamp
    }

    fn bytes(&self) -> &[u8] {
        &self.bytes
    }

    fn verify(&self) -> Result<()> {
        // Verify inner block exists
        if self.inner_bytes.is_empty() {
            return Err(VMError::InvalidBlock("empty inner block".to_string()));
        }

        // Verify timestamp is not in far future
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_secs();

        if self.timestamp > now + 60 {
            return Err(VMError::InvalidBlock(format!(
                "timestamp {} too far in future (now={})",
                self.timestamp, now
            )));
        }

        // Certificate verification would happen here with actual signature verification
        // For now, we accept blocks without certificates in testing

        Ok(())
    }

    fn accept(&mut self) -> Result<()> {
        self.status = BlockStatus::Accepted;
        Ok(())
    }

    fn reject(&mut self) -> Result<()> {
        self.status = BlockStatus::Rejected;
        Ok(())
    }

    fn status(&self) -> BlockStatus {
        self.status
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_pre_fork_block_roundtrip() {
        let inner = vec![1, 2, 3, 4, 5];
        let parent = Id::from_bytes([1; 32]);
        let inner_id = Id::from_bytes([2; 32]);

        let block = PreForkBlock::new(inner.clone(), inner_id, parent, 100);

        assert_eq!(block.height(), 100);
        assert_eq!(block.parent(), parent);
        assert_eq!(block.inner_bytes(), &inner);
        assert_eq!(block.status(), BlockStatus::Processing);
    }

    #[test]
    fn test_post_fork_block_roundtrip() {
        let inner = vec![1, 2, 3, 4, 5];
        let parent = Id::from_bytes([1; 32]);
        let proposer = NodeId::from_bytes([3; 20]);

        let block = PostForkBlock::new(parent, inner.clone(), 1000, 99, proposer);

        assert_eq!(block.parent(), parent);
        assert_eq!(block.timestamp(), 1000);
        assert_eq!(block.pchain_height(), 99);
        assert_eq!(block.proposer(), proposer);
        assert_eq!(block.inner_bytes(), &inner);
    }

    #[test]
    fn test_proposer_block_parse_pre_fork() {
        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let inner_id = Id::from_bytes([2; 32]);

        let block = PreForkBlock::new(inner, inner_id, parent, 50);
        let bytes = block.bytes();

        let parsed = ProposerBlock::parse(bytes).unwrap();
        assert!(!parsed.is_post_fork());
        assert_eq!(parsed.height(), 50);
    }

    #[test]
    fn test_proposer_block_parse_post_fork() {
        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let proposer = NodeId::from_bytes([3; 20]);

        let block = PostForkBlock::new(parent, inner, 2000, 99, proposer);
        let bytes = block.bytes();

        let parsed = ProposerBlock::parse(bytes).unwrap();
        assert!(parsed.is_post_fork());
        assert_eq!(parsed.proposer(), Some(proposer));
    }

    #[test]
    fn test_block_verification() {
        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let proposer = NodeId::from_bytes([3; 20]);

        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();

        let block = PostForkBlock::new(parent, inner, now, 99, proposer);
        assert!(block.verify().is_ok());

        // Empty inner block should fail
        let empty_block = PostForkBlock::new(parent, vec![], now, 99, proposer);
        assert!(empty_block.verify().is_err());
    }
}
