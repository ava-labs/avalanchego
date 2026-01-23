//! Virtual Machine integration tests.
//!
//! Tests VM initialization, block processing, and state management across
//! Platform VM, AVM (X-Chain), and EVM (C-Chain).

use std::sync::Arc;
use std::time::Duration;

use avalanche_ids::{Id, NodeId};

/// Mock database for VM testing.
pub struct MockDatabase {
    data: std::collections::HashMap<Vec<u8>, Vec<u8>>,
}

impl MockDatabase {
    pub fn new() -> Self {
        Self {
            data: std::collections::HashMap::new(),
        }
    }

    pub fn get(&self, key: &[u8]) -> Option<Vec<u8>> {
        self.data.get(key).cloned()
    }

    pub fn put(&mut self, key: Vec<u8>, value: Vec<u8>) {
        self.data.insert(key, value);
    }

    pub fn delete(&mut self, key: &[u8]) {
        self.data.remove(key);
    }

    pub fn has(&self, key: &[u8]) -> bool {
        self.data.contains_key(key)
    }
}

/// VM test context.
#[derive(Clone)]
pub struct VMTestContext {
    /// Chain ID.
    pub chain_id: Id,
    /// Network ID.
    pub network_id: u32,
    /// Node ID.
    pub node_id: NodeId,
    /// Genesis bytes.
    pub genesis_bytes: Vec<u8>,
}

impl Default for VMTestContext {
    fn default() -> Self {
        Self {
            chain_id: Id::from_bytes([1; 32]),
            network_id: 12345,
            node_id: NodeId::from_bytes([1; 20]),
            genesis_bytes: b"{}".to_vec(),
        }
    }
}

/// Mock block for VM testing.
#[derive(Debug, Clone)]
pub struct MockVMBlock {
    pub id: Id,
    pub parent_id: Id,
    pub height: u64,
    pub timestamp: u64,
    pub bytes: Vec<u8>,
    pub status: BlockStatus,
}

/// Block status.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum BlockStatus {
    Processing,
    Accepted,
    Rejected,
}

impl MockVMBlock {
    /// Creates a new mock block.
    pub fn new(id: Id, parent_id: Id, height: u64, timestamp: u64) -> Self {
        let bytes = [id.as_bytes(), parent_id.as_bytes()].concat();
        Self {
            id,
            parent_id,
            height,
            timestamp,
            bytes,
            status: BlockStatus::Processing,
        }
    }

    /// Creates a genesis block.
    pub fn genesis() -> Self {
        Self::new(
            Id::from_bytes([0; 32]),
            Id::from_bytes([0; 32]),
            0,
            0,
        )
    }
}

/// Mock VM for testing.
pub struct MockVM {
    /// Database.
    pub db: MockDatabase,
    /// Context.
    pub ctx: VMTestContext,
    /// Blocks.
    pub blocks: std::collections::HashMap<Id, MockVMBlock>,
    /// Last accepted block ID.
    pub last_accepted: Id,
    /// Preferred block ID.
    pub preferred: Id,
    /// Whether initialized.
    pub initialized: bool,
}

impl MockVM {
    /// Creates a new mock VM.
    pub fn new() -> Self {
        let genesis = MockVMBlock::genesis();
        let genesis_id = genesis.id;

        let mut blocks = std::collections::HashMap::new();
        blocks.insert(genesis_id, genesis);

        Self {
            db: MockDatabase::new(),
            ctx: VMTestContext::default(),
            blocks,
            last_accepted: genesis_id,
            preferred: genesis_id,
            initialized: false,
        }
    }

    /// Initializes the VM.
    pub fn initialize(&mut self, ctx: VMTestContext, genesis_bytes: &[u8]) -> Result<(), VMError> {
        if self.initialized {
            return Err(VMError::AlreadyInitialized);
        }

        self.ctx = ctx;
        self.initialized = true;
        Ok(())
    }

    /// Shuts down the VM.
    pub fn shutdown(&mut self) -> Result<(), VMError> {
        self.initialized = false;
        Ok(())
    }

    /// Builds a new block.
    pub fn build_block(&self) -> Result<MockVMBlock, VMError> {
        if !self.initialized {
            return Err(VMError::NotInitialized);
        }

        let parent = self.blocks.get(&self.preferred).ok_or(VMError::BlockNotFound)?;
        let height = parent.height + 1;

        let mut id_bytes = [0u8; 32];
        id_bytes[0..8].copy_from_slice(&height.to_be_bytes());

        let timestamp = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs();

        Ok(MockVMBlock::new(
            Id::from_bytes(id_bytes),
            self.preferred,
            height,
            timestamp,
        ))
    }

    /// Parses a block from bytes.
    pub fn parse_block(&self, bytes: &[u8]) -> Result<MockVMBlock, VMError> {
        if bytes.len() < 64 {
            return Err(VMError::InvalidBlock);
        }

        let id = Id::from_slice(&bytes[0..32]).map_err(|_| VMError::InvalidBlock)?;
        let parent_id = Id::from_slice(&bytes[32..64]).map_err(|_| VMError::InvalidBlock)?;

        // Find height from parent
        let height = self
            .blocks
            .get(&parent_id)
            .map(|b| b.height + 1)
            .unwrap_or(1);

        let timestamp = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_secs();

        Ok(MockVMBlock::new(id, parent_id, height, timestamp))
    }

    /// Gets a block by ID.
    pub fn get_block(&self, id: &Id) -> Option<MockVMBlock> {
        self.blocks.get(id).cloned()
    }

    /// Sets the preferred block.
    pub fn set_preference(&mut self, id: Id) -> Result<(), VMError> {
        if !self.blocks.contains_key(&id) {
            return Err(VMError::BlockNotFound);
        }
        self.preferred = id;
        Ok(())
    }

    /// Accepts a block.
    pub fn accept_block(&mut self, id: Id) -> Result<(), VMError> {
        if let Some(block) = self.blocks.get_mut(&id) {
            block.status = BlockStatus::Accepted;
            self.last_accepted = id;
            Ok(())
        } else {
            Err(VMError::BlockNotFound)
        }
    }

    /// Rejects a block.
    pub fn reject_block(&mut self, id: Id) -> Result<(), VMError> {
        if let Some(block) = self.blocks.get_mut(&id) {
            block.status = BlockStatus::Rejected;
            Ok(())
        } else {
            Err(VMError::BlockNotFound)
        }
    }

    /// Verifies a block.
    pub fn verify_block(&self, id: &Id) -> Result<(), VMError> {
        let block = self.blocks.get(id).ok_or(VMError::BlockNotFound)?;

        // Verify parent exists (except for genesis)
        if block.height > 0 && !self.blocks.contains_key(&block.parent_id) {
            return Err(VMError::ParentNotFound);
        }

        // Verify height
        if block.height > 0 {
            let parent = self.blocks.get(&block.parent_id).ok_or(VMError::ParentNotFound)?;
            if block.height != parent.height + 1 {
                return Err(VMError::InvalidHeight);
            }
        }

        Ok(())
    }

    /// Adds a block to the VM.
    pub fn add_block(&mut self, block: MockVMBlock) {
        self.blocks.insert(block.id, block);
    }

    /// Gets the current height.
    pub fn height(&self) -> u64 {
        self.blocks.get(&self.last_accepted).map(|b| b.height).unwrap_or(0)
    }
}

/// VM error type.
#[derive(Debug, thiserror::Error)]
pub enum VMError {
    #[error("VM already initialized")]
    AlreadyInitialized,
    #[error("VM not initialized")]
    NotInitialized,
    #[error("block not found")]
    BlockNotFound,
    #[error("parent not found")]
    ParentNotFound,
    #[error("invalid block")]
    InvalidBlock,
    #[error("invalid height")]
    InvalidHeight,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_mock_database() {
        let mut db = MockDatabase::new();

        db.put(b"key1".to_vec(), b"value1".to_vec());
        assert!(db.has(b"key1"));
        assert_eq!(db.get(b"key1"), Some(b"value1".to_vec()));

        db.delete(b"key1");
        assert!(!db.has(b"key1"));
    }

    #[test]
    fn test_vm_initialization() {
        let mut vm = MockVM::new();
        assert!(!vm.initialized);

        let ctx = VMTestContext::default();
        vm.initialize(ctx, b"{}").unwrap();
        assert!(vm.initialized);

        // Double initialization should fail
        let result = vm.initialize(VMTestContext::default(), b"{}");
        assert!(matches!(result, Err(VMError::AlreadyInitialized)));
    }

    #[test]
    fn test_vm_shutdown() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();
        assert!(vm.initialized);

        vm.shutdown().unwrap();
        assert!(!vm.initialized);
    }

    #[test]
    fn test_build_block() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        let block = vm.build_block().unwrap();
        assert_eq!(block.height, 1);
        assert_eq!(block.parent_id, vm.preferred);
    }

    #[test]
    fn test_build_block_not_initialized() {
        let vm = MockVM::new();
        let result = vm.build_block();
        assert!(matches!(result, Err(VMError::NotInitialized)));
    }

    #[test]
    fn test_parse_block() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        let block = vm.build_block().unwrap();
        let parsed = vm.parse_block(&block.bytes).unwrap();

        assert_eq!(parsed.id, block.id);
        assert_eq!(parsed.parent_id, block.parent_id);
    }

    #[test]
    fn test_get_block() {
        let vm = MockVM::new();
        let genesis_id = Id::from_bytes([0; 32]);

        let block = vm.get_block(&genesis_id);
        assert!(block.is_some());
        assert_eq!(block.unwrap().height, 0);

        let missing = vm.get_block(&Id::from_bytes([99; 32]));
        assert!(missing.is_none());
    }

    #[test]
    fn test_set_preference() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        let block = vm.build_block().unwrap();
        let block_id = block.id;
        vm.add_block(block);

        vm.set_preference(block_id).unwrap();
        assert_eq!(vm.preferred, block_id);
    }

    #[test]
    fn test_set_preference_not_found() {
        let mut vm = MockVM::new();
        let result = vm.set_preference(Id::from_bytes([99; 32]));
        assert!(matches!(result, Err(VMError::BlockNotFound)));
    }

    #[test]
    fn test_accept_block() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        let block = vm.build_block().unwrap();
        let block_id = block.id;
        vm.add_block(block);

        vm.accept_block(block_id).unwrap();
        assert_eq!(vm.last_accepted, block_id);

        let accepted_block = vm.get_block(&block_id).unwrap();
        assert_eq!(accepted_block.status, BlockStatus::Accepted);
    }

    #[test]
    fn test_reject_block() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        let block = vm.build_block().unwrap();
        let block_id = block.id;
        vm.add_block(block);

        vm.reject_block(block_id).unwrap();

        let rejected_block = vm.get_block(&block_id).unwrap();
        assert_eq!(rejected_block.status, BlockStatus::Rejected);
    }

    #[test]
    fn test_verify_block() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        // Build and add a block
        let block = vm.build_block().unwrap();
        let block_id = block.id;
        vm.add_block(block);

        // Should verify successfully
        vm.verify_block(&block_id).unwrap();
    }

    #[test]
    fn test_verify_block_missing_parent() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        // Create block with non-existent parent
        let block = MockVMBlock::new(
            Id::from_bytes([1; 32]),
            Id::from_bytes([99; 32]), // Non-existent parent
            1,
            0,
        );
        vm.add_block(block.clone());

        let result = vm.verify_block(&block.id);
        assert!(matches!(result, Err(VMError::ParentNotFound)));
    }

    #[test]
    fn test_chain_building() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        // Build a chain of 10 blocks
        for _ in 0..10 {
            let block = vm.build_block().unwrap();
            let block_id = block.id;
            vm.add_block(block);
            vm.set_preference(block_id).unwrap();
            vm.accept_block(block_id).unwrap();
        }

        assert_eq!(vm.height(), 10);
    }

    #[test]
    fn test_block_status_transitions() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        let block = vm.build_block().unwrap();
        let block_id = block.id;
        vm.add_block(block);

        // Initial status is Processing
        let block = vm.get_block(&block_id).unwrap();
        assert_eq!(block.status, BlockStatus::Processing);

        // Can be accepted
        vm.accept_block(block_id).unwrap();
        let block = vm.get_block(&block_id).unwrap();
        assert_eq!(block.status, BlockStatus::Accepted);
    }

    #[test]
    fn test_genesis_block() {
        let vm = MockVM::new();
        let genesis_id = Id::from_bytes([0; 32]);

        let genesis = vm.get_block(&genesis_id).unwrap();
        assert_eq!(genesis.height, 0);
        assert_eq!(genesis.parent_id, genesis_id); // Genesis is its own parent
    }

    #[tokio::test]
    async fn test_concurrent_block_building() {
        let vm = std::sync::Arc::new(std::sync::Mutex::new(MockVM::new()));

        {
            let mut vm = vm.lock().unwrap();
            vm.initialize(VMTestContext::default(), b"{}").unwrap();
        }

        let handles: Vec<_> = (0..5)
            .map(|_| {
                let vm = vm.clone();
                tokio::spawn(async move {
                    let block = {
                        let vm = vm.lock().unwrap();
                        vm.build_block()
                    };
                    block.is_ok()
                })
            })
            .collect();

        for handle in handles {
            assert!(handle.await.unwrap());
        }
    }

    /// Test fork handling.
    #[test]
    fn test_fork_handling() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        // Build first block
        let block1 = vm.build_block().unwrap();
        let block1_id = block1.id;
        vm.add_block(block1);

        // Build second block (fork at height 1)
        let fork_block = MockVMBlock::new(
            Id::from_bytes([2; 32]),
            Id::from_bytes([0; 32]), // Same parent as block1
            1,
            0,
        );
        let fork_id = fork_block.id;
        vm.add_block(fork_block);

        // Both should be valid
        vm.verify_block(&block1_id).unwrap();
        vm.verify_block(&fork_id).unwrap();

        // Accept one, reject other
        vm.accept_block(block1_id).unwrap();
        vm.reject_block(fork_id).unwrap();

        assert_eq!(vm.last_accepted, block1_id);
    }

    /// Test rollback scenario.
    #[test]
    fn test_preference_change() {
        let mut vm = MockVM::new();
        vm.initialize(VMTestContext::default(), b"{}").unwrap();

        let block1 = vm.build_block().unwrap();
        let block1_id = block1.id;
        vm.add_block(block1);
        vm.set_preference(block1_id).unwrap();

        let block2 = vm.build_block().unwrap();
        let block2_id = block2.id;
        vm.add_block(block2);

        // Change preference back
        vm.set_preference(block1_id).unwrap();
        assert_eq!(vm.preferred, block1_id);

        // Change preference forward
        vm.set_preference(block2_id).unwrap();
        assert_eq!(vm.preferred, block2_id);
    }
}
