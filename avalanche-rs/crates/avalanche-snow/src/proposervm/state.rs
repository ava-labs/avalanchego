//! ProposerVM state management.
//!
//! Tracks proposer blocks, fork status, and block metadata.

use std::collections::HashMap;
use std::sync::Arc;

use parking_lot::RwLock;
use sha2::{Digest, Sha256};

use avalanche_db::{Database, MemDb};
use avalanche_ids::Id;

use crate::error::Result;
use crate::vm::{Block as VMBlock, VMError};

use super::block::ProposerBlock;

/// Prefix for block storage.
const BLOCK_PREFIX: &[u8] = b"proposer:block:";
/// Prefix for height index.
const HEIGHT_PREFIX: &[u8] = b"proposer:height:";
/// Key for last accepted block.
const LAST_ACCEPTED_KEY: &[u8] = b"proposer:last_accepted";
/// Key for fork height.
const FORK_HEIGHT_KEY: &[u8] = b"proposer:fork_height";

/// ProposerVM state.
pub struct ProposerState {
    /// Database.
    db: Arc<dyn Database>,
    /// In-memory block cache.
    block_cache: RwLock<HashMap<Id, ProposerBlock>>,
    /// Height to block ID index.
    height_index: RwLock<HashMap<u64, Id>>,
    /// Genesis block ID.
    genesis_block_id: Id,
    /// Last accepted block ID.
    last_accepted: RwLock<Id>,
    /// Fork height (when proposervm was activated).
    fork_height: u64,
}

impl ProposerState {
    /// Creates a new proposer state.
    pub fn new(db: Arc<dyn Database>) -> Result<Self> {
        // Compute genesis block ID
        let genesis_bytes = b"proposervm_genesis";
        let hash = Sha256::digest(genesis_bytes);
        let genesis_block_id = Id::from_slice(&hash).unwrap_or_default();

        // Try to load fork height from DB
        let fork_height = db
            .get(FORK_HEIGHT_KEY)
            .ok()
            .flatten()
            .and_then(|bytes| {
                if bytes.len() == 8 {
                    Some(u64::from_be_bytes(bytes.try_into().unwrap()))
                } else {
                    None
                }
            })
            .unwrap_or(0);

        // Try to load last accepted from DB
        let last_accepted = db
            .get(LAST_ACCEPTED_KEY)
            .ok()
            .flatten()
            .and_then(|bytes| Id::from_slice(&bytes).ok())
            .unwrap_or(genesis_block_id);

        Ok(Self {
            db,
            block_cache: RwLock::new(HashMap::new()),
            height_index: RwLock::new(HashMap::new()),
            genesis_block_id,
            last_accepted: RwLock::new(last_accepted),
            fork_height,
        })
    }

    /// Creates a proposer state with in-memory database.
    pub fn in_memory() -> Result<Self> {
        Self::new(Arc::new(MemDb::new()))
    }

    /// Returns the genesis block ID.
    pub fn genesis_block_id(&self) -> Id {
        self.genesis_block_id
    }

    /// Returns the last accepted block ID.
    pub fn last_accepted(&self) -> Id {
        *self.last_accepted.read()
    }

    /// Returns the fork height.
    pub fn fork_height(&self) -> u64 {
        self.fork_height
    }

    /// Sets the fork height.
    pub fn set_fork_height(&mut self, height: u64) -> Result<()> {
        self.fork_height = height;
        self.db.put(FORK_HEIGHT_KEY, &height.to_be_bytes())?;
        Ok(())
    }

    /// Stores a proposer block.
    pub fn put_block(&self, block: ProposerBlock) -> Result<()> {
        let id = block.id();
        let height = block.height();
        let bytes = block.bytes();

        // Store in DB
        let mut key = BLOCK_PREFIX.to_vec();
        key.extend_from_slice(id.as_bytes());
        self.db.put(&key, bytes)?;

        // Update height index
        let mut height_key = HEIGHT_PREFIX.to_vec();
        height_key.extend_from_slice(&height.to_be_bytes());
        self.db.put(&height_key, id.as_bytes())?;

        // Update caches
        self.height_index.write().insert(height, id);
        self.block_cache.write().insert(id, block);

        Ok(())
    }

    /// Retrieves a proposer block by ID.
    pub fn get_block(&self, id: &Id) -> Result<Option<ProposerBlock>> {
        // Check cache first
        if let Some(block) = self.block_cache.read().get(id) {
            return Ok(Some(block.clone()));
        }

        // Load from DB
        let mut key = BLOCK_PREFIX.to_vec();
        key.extend_from_slice(id.as_bytes());

        match self.db.get(&key)? {
            Some(bytes) => {
                let block = ProposerBlock::parse(&bytes)?;
                // Cache it
                self.block_cache.write().insert(*id, block.clone());
                Ok(Some(block))
            }
            None => Ok(None),
        }
    }

    /// Retrieves a proposer block by height.
    pub fn get_block_by_height(&self, height: u64) -> Result<Option<ProposerBlock>> {
        // Check height index cache
        if let Some(id) = self.height_index.read().get(&height) {
            return self.get_block(id);
        }

        // Load from DB
        let mut height_key = HEIGHT_PREFIX.to_vec();
        height_key.extend_from_slice(&height.to_be_bytes());

        match self.db.get(&height_key)? {
            Some(id_bytes) => {
                let id = Id::from_slice(&id_bytes)
                    .map_err(|_| -> crate::error::ConsensusError { VMError::InvalidBlock("invalid block ID".to_string()).into() })?;
                self.height_index.write().insert(height, id);
                self.get_block(&id)
            }
            None => Ok(None),
        }
    }

    /// Checks if a block exists.
    pub fn has_block(&self, id: &Id) -> Result<bool> {
        if self.block_cache.read().contains_key(id) {
            return Ok(true);
        }

        let mut key = BLOCK_PREFIX.to_vec();
        key.extend_from_slice(id.as_bytes());
        Ok(self.db.has(&key)?)
    }

    /// Deletes a proposer block.
    pub fn delete_block(&self, id: &Id) -> Result<()> {
        // Get block to find height
        if let Some(block) = self.get_block(id)? {
            let height = block.height();

            // Delete from DB
            let mut key = BLOCK_PREFIX.to_vec();
            key.extend_from_slice(id.as_bytes());
            self.db.delete(&key)?;

            // Delete height index
            let mut height_key = HEIGHT_PREFIX.to_vec();
            height_key.extend_from_slice(&height.to_be_bytes());
            self.db.delete(&height_key)?;

            // Remove from caches
            self.block_cache.write().remove(id);
            self.height_index.write().remove(&height);
        }

        Ok(())
    }

    /// Sets the last accepted block.
    pub fn set_last_accepted(&self, id: Id) -> Result<()> {
        *self.last_accepted.write() = id;
        self.db.put(LAST_ACCEPTED_KEY, id.as_bytes())?;
        Ok(())
    }

    /// Accepts a block and updates state.
    pub fn accept_block(&self, id: &Id) -> Result<()> {
        // Verify block exists
        if !self.has_block(id)? {
            return Err(VMError::NotFound(format!("block {} not found", id)).into());
        }

        // Update last accepted
        self.set_last_accepted(*id)?;

        Ok(())
    }

    /// Returns the current height (height of last accepted block).
    pub fn height(&self) -> Result<u64> {
        let last_accepted = self.last_accepted();
        if last_accepted == self.genesis_block_id {
            return Ok(0);
        }

        match self.get_block(&last_accepted)? {
            Some(block) => Ok(block.height()),
            None => Ok(0),
        }
    }

    /// Returns all block IDs in the cache.
    pub fn cached_block_ids(&self) -> Vec<Id> {
        self.block_cache.read().keys().copied().collect()
    }

    /// Clears the block cache.
    pub fn clear_cache(&self) {
        self.block_cache.write().clear();
        self.height_index.write().clear();
    }

    /// Returns statistics about the state.
    pub fn stats(&self) -> StateStats {
        StateStats {
            cached_blocks: self.block_cache.read().len(),
            indexed_heights: self.height_index.read().len(),
            fork_height: self.fork_height,
        }
    }
}

/// State statistics.
#[derive(Debug, Clone)]
pub struct StateStats {
    /// Number of cached blocks.
    pub cached_blocks: usize,
    /// Number of indexed heights.
    pub indexed_heights: usize,
    /// Fork height.
    pub fork_height: u64,
}

/// Block metadata for indexing.
#[derive(Debug, Clone)]
pub struct BlockMetadata {
    /// Block ID.
    pub id: Id,
    /// Block height.
    pub height: u64,
    /// Parent block ID.
    pub parent_id: Id,
    /// Block timestamp.
    pub timestamp: u64,
    /// Whether this is a post-fork block.
    pub is_post_fork: bool,
}

impl BlockMetadata {
    /// Creates metadata from a proposer block.
    pub fn from_block(block: &ProposerBlock) -> Self {
        Self {
            id: block.id(),
            height: block.height(),
            parent_id: block.parent(),
            timestamp: block.timestamp(),
            is_post_fork: block.is_post_fork(),
        }
    }

    /// Serializes metadata to bytes.
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut bytes = Vec::with_capacity(32 + 8 + 32 + 8 + 1);
        bytes.extend_from_slice(self.id.as_bytes());
        bytes.extend_from_slice(&self.height.to_be_bytes());
        bytes.extend_from_slice(self.parent_id.as_bytes());
        bytes.extend_from_slice(&self.timestamp.to_be_bytes());
        bytes.push(if self.is_post_fork { 1 } else { 0 });
        bytes
    }

    /// Deserializes metadata from bytes.
    pub fn from_bytes(bytes: &[u8]) -> Result<Self> {
        if bytes.len() < 81 {
            return Err(VMError::InvalidBlock("metadata too short".to_string()).into());
        }

        let id = Id::from_slice(&bytes[0..32])
            .map_err(|_| -> crate::error::ConsensusError { VMError::InvalidBlock("invalid ID".to_string()).into() })?;
        let height = u64::from_be_bytes(bytes[32..40].try_into().unwrap());
        let parent_id = Id::from_slice(&bytes[40..72])
            .map_err(|_| -> crate::error::ConsensusError { VMError::InvalidBlock("invalid parent ID".to_string()).into() })?;
        let timestamp = u64::from_be_bytes(bytes[72..80].try_into().unwrap());
        let is_post_fork = bytes[80] == 1;

        Ok(Self {
            id,
            height,
            parent_id,
            timestamp,
            is_post_fork,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proposervm::block::{PostForkBlock, PreForkBlock};
    use avalanche_ids::NodeId;

    #[test]
    fn test_state_creation() {
        let state = ProposerState::in_memory().unwrap();
        assert_eq!(state.fork_height(), 0);
        assert_eq!(state.last_accepted(), state.genesis_block_id());
    }

    #[test]
    fn test_block_storage() {
        let state = ProposerState::in_memory().unwrap();

        let inner = vec![1, 2, 3, 4, 5];
        let parent = Id::from_bytes([1; 32]);
        let inner_id = Id::from_bytes([2; 32]);

        let block = PreForkBlock::new(inner, inner_id, parent, 1);
        let id = block.id();

        let proposer_block = ProposerBlock::PreFork(block);
        state.put_block(proposer_block.clone()).unwrap();

        assert!(state.has_block(&id).unwrap());

        let loaded = state.get_block(&id).unwrap().unwrap();
        assert_eq!(loaded.id(), id);
        assert_eq!(loaded.height(), 1);
    }

    #[test]
    fn test_block_by_height() {
        let state = ProposerState::in_memory().unwrap();

        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let inner_id = Id::from_bytes([2; 32]);

        let block = PreForkBlock::new(inner, inner_id, parent, 42);
        let id = block.id();

        state.put_block(ProposerBlock::PreFork(block)).unwrap();

        let loaded = state.get_block_by_height(42).unwrap().unwrap();
        assert_eq!(loaded.id(), id);
    }

    #[test]
    fn test_post_fork_block_storage() {
        let state = ProposerState::in_memory().unwrap();

        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let proposer = NodeId::from_bytes([3; 20]);

        let block = PostForkBlock::new(parent, inner, 1000, 10, proposer);
        let id = block.id();

        state.put_block(ProposerBlock::PostFork(block)).unwrap();

        let loaded = state.get_block(&id).unwrap().unwrap();
        assert!(loaded.is_post_fork());
        assert_eq!(loaded.proposer(), Some(proposer));
    }

    #[test]
    fn test_last_accepted() {
        let state = ProposerState::in_memory().unwrap();

        let new_id = Id::from_bytes([5; 32]);
        state.set_last_accepted(new_id).unwrap();

        assert_eq!(state.last_accepted(), new_id);
    }

    #[test]
    fn test_delete_block() {
        let state = ProposerState::in_memory().unwrap();

        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let inner_id = Id::from_bytes([2; 32]);

        let block = PreForkBlock::new(inner, inner_id, parent, 1);
        let id = block.id();

        state.put_block(ProposerBlock::PreFork(block)).unwrap();
        assert!(state.has_block(&id).unwrap());

        state.delete_block(&id).unwrap();
        assert!(!state.has_block(&id).unwrap());
    }

    #[test]
    fn test_metadata_roundtrip() {
        let metadata = BlockMetadata {
            id: Id::from_bytes([1; 32]),
            height: 100,
            parent_id: Id::from_bytes([2; 32]),
            timestamp: 1234567890,
            is_post_fork: true,
        };

        let bytes = metadata.to_bytes();
        let parsed = BlockMetadata::from_bytes(&bytes).unwrap();

        assert_eq!(parsed.id, metadata.id);
        assert_eq!(parsed.height, metadata.height);
        assert_eq!(parsed.parent_id, metadata.parent_id);
        assert_eq!(parsed.timestamp, metadata.timestamp);
        assert_eq!(parsed.is_post_fork, metadata.is_post_fork);
    }

    #[test]
    fn test_stats() {
        let state = ProposerState::in_memory().unwrap();

        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let inner_id = Id::from_bytes([2; 32]);

        let block = PreForkBlock::new(inner, inner_id, parent, 1);
        state.put_block(ProposerBlock::PreFork(block)).unwrap();

        let stats = state.stats();
        assert_eq!(stats.cached_blocks, 1);
        assert_eq!(stats.indexed_heights, 1);
    }

    #[test]
    fn test_clear_cache() {
        let state = ProposerState::in_memory().unwrap();

        let inner = vec![1, 2, 3];
        let parent = Id::from_bytes([1; 32]);
        let inner_id = Id::from_bytes([2; 32]);

        let block = PreForkBlock::new(inner, inner_id, parent, 1);
        let id = block.id();
        state.put_block(ProposerBlock::PreFork(block)).unwrap();

        assert_eq!(state.cached_block_ids().len(), 1);

        state.clear_cache();
        assert_eq!(state.cached_block_ids().len(), 0);

        // Should still be able to load from DB
        assert!(state.has_block(&id).unwrap());
    }
}
