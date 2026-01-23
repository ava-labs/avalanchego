//! C-Chain EVM Virtual Machine implementation.
//!
//! This module provides the main EVM VM that integrates with Avalanche:
//! - Block building and verification
//! - State management
//! - Transaction execution
//! - Consensus integration

use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use alloy_primitives::{Address, Bytes, B256, U256};
use parking_lot::RwLock;
use thiserror::Error;

use avalanche_db::Database;
use avalanche_trie::HashKey;

use crate::block::{Block, BlockBuilder, Header};
use crate::executor::{BlockContext, ExecutionError, Executor, ExecutorConfig};
use crate::state::{AccountInfo, EvmState};
use crate::transaction::{Receipt, SignedTransaction};

/// C-Chain VM configuration.
#[derive(Debug, Clone)]
pub struct VMConfig {
    /// Chain ID.
    pub chain_id: u64,
    /// Network ID.
    pub network_id: u32,
    /// Block gas limit.
    pub block_gas_limit: u64,
    /// Target block time.
    pub block_time: Duration,
    /// Minimum base fee.
    pub min_base_fee: U256,
    /// Fee recipient address.
    pub fee_recipient: Address,
}

impl Default for VMConfig {
    fn default() -> Self {
        Self {
            chain_id: 43114, // Avalanche C-Chain mainnet
            network_id: 1,
            block_gas_limit: 8_000_000,
            block_time: Duration::from_secs(2),
            min_base_fee: U256::from(25_000_000_000u64), // 25 gwei
            fee_recipient: Address::ZERO,
        }
    }
}

/// Genesis configuration.
#[derive(Debug, Clone, Default)]
pub struct GenesisConfig {
    /// Chain ID.
    pub chain_id: u64,
    /// Timestamp.
    pub timestamp: u64,
    /// Initial allocations.
    pub alloc: HashMap<Address, GenesisAccount>,
    /// Extra data.
    pub extra_data: Bytes,
}

/// Genesis account allocation.
#[derive(Debug, Clone, Default)]
pub struct GenesisAccount {
    /// Initial balance.
    pub balance: U256,
    /// Initial nonce.
    pub nonce: u64,
    /// Contract code.
    pub code: Bytes,
    /// Storage.
    pub storage: HashMap<U256, U256>,
}

/// VM state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VMState {
    /// VM is initializing.
    Initializing,
    /// VM is bootstrapping (catching up).
    Bootstrapping,
    /// VM is ready and running normally.
    Running,
    /// VM is stopped.
    Stopped,
}

/// C-Chain EVM Virtual Machine.
pub struct EvmVM {
    /// Configuration.
    config: VMConfig,
    /// EVM state database.
    state: Arc<EvmState>,
    /// Transaction executor.
    executor: RwLock<Executor>,
    /// Pending transactions.
    pending_txs: RwLock<Vec<SignedTransaction>>,
    /// Block cache by hash.
    blocks_by_hash: RwLock<HashMap<B256, Block>>,
    /// Block cache by number.
    blocks_by_number: RwLock<HashMap<u64, B256>>,
    /// Current head block.
    head: RwLock<Option<Block>>,
    /// Genesis block.
    genesis: RwLock<Option<Block>>,
    /// VM state.
    vm_state: RwLock<VMState>,
    /// Receipts by transaction hash.
    receipts: RwLock<HashMap<B256, Receipt>>,
}

impl EvmVM {
    /// Creates a new EVM VM.
    pub fn new(config: VMConfig, db: Arc<dyn Database>) -> Self {
        let state = Arc::new(EvmState::new(db));
        let executor_config = ExecutorConfig {
            chain_id: config.chain_id,
            block_gas_limit: config.block_gas_limit,
            ..Default::default()
        };
        let executor = Executor::new(executor_config, state.clone());

        Self {
            config,
            state,
            executor: RwLock::new(executor),
            pending_txs: RwLock::new(Vec::new()),
            blocks_by_hash: RwLock::new(HashMap::new()),
            blocks_by_number: RwLock::new(HashMap::new()),
            head: RwLock::new(None),
            genesis: RwLock::new(None),
            vm_state: RwLock::new(VMState::Initializing),
            receipts: RwLock::new(HashMap::new()),
        }
    }

    /// Initializes the VM with genesis.
    pub fn initialize(&self, genesis: GenesisConfig) -> Result<Block, VMError> {
        // Apply genesis allocations
        for (address, account) in &genesis.alloc {
            let info = AccountInfo {
                balance: account.balance,
                nonce: account.nonce,
                code_hash: if account.code.is_empty() {
                    revm::primitives::KECCAK_EMPTY
                } else {
                    let hash = keccak256(&account.code);
                    self.state.set_code(hash, account.code.clone());
                    hash
                },
                code: account.code.clone(),
            };
            self.state.set_account(*address, info);

            // Set storage
            for (key, value) in &account.storage {
                self.state.set_storage(*address, *key, *value);
            }
        }

        // Commit genesis state
        let state_root = self.state.commit();

        // Create genesis block
        let header = Header {
            parent_hash: B256::ZERO,
            ommers_hash: Header::EMPTY_OMMERS_HASH,
            coinbase: Address::ZERO,
            state_root: hash_key_to_b256(state_root),
            transactions_root: Header::EMPTY_TRANSACTIONS_ROOT,
            receipts_root: Header::EMPTY_RECEIPTS_ROOT,
            logs_bloom: [0; 256],
            difficulty: U256::ZERO,
            number: 0,
            gas_limit: self.config.block_gas_limit,
            gas_used: 0,
            timestamp: genesis.timestamp,
            extra_data: genesis.extra_data.clone(),
            mix_hash: B256::ZERO,
            nonce: alloy_primitives::B64::ZERO,
            base_fee: self.config.min_base_fee,
            withdrawals_root: None,
            blob_gas_used: None,
            excess_blob_gas: None,
            parent_beacon_block_root: None,
        };

        let block = Block::new(header, vec![]);
        let hash = block.hash();

        // Store genesis
        self.blocks_by_hash.write().insert(hash, block.clone());
        self.blocks_by_number.write().insert(0, hash);
        *self.genesis.write() = Some(block.clone());
        *self.head.write() = Some(block.clone());
        *self.vm_state.write() = VMState::Running;

        Ok(block)
    }

    /// Returns the current head block.
    pub fn head(&self) -> Option<Block> {
        self.head.read().clone()
    }

    /// Returns a block by hash.
    pub fn get_block(&self, hash: &B256) -> Option<Block> {
        self.blocks_by_hash.read().get(hash).cloned()
    }

    /// Returns a block by number.
    pub fn get_block_by_number(&self, number: u64) -> Option<Block> {
        let hash = self.blocks_by_number.read().get(&number).copied()?;
        self.get_block(&hash)
    }

    /// Returns the genesis block.
    pub fn genesis(&self) -> Option<Block> {
        self.genesis.read().clone()
    }

    /// Returns the current state root.
    pub fn state_root(&self) -> HashKey {
        self.state.state_root()
    }

    /// Returns account information.
    pub fn get_account(&self, address: &Address) -> AccountInfo {
        self.state.get_account(address)
    }

    /// Returns account balance.
    pub fn get_balance(&self, address: &Address) -> U256 {
        self.get_account(address).balance
    }

    /// Returns account nonce.
    pub fn get_nonce(&self, address: &Address) -> u64 {
        self.get_account(address).nonce
    }

    /// Returns contract code.
    pub fn get_code(&self, address: &Address) -> Bytes {
        let info = self.get_account(address);
        self.state.get_code(&info.code_hash)
    }

    /// Returns storage value.
    pub fn get_storage(&self, address: &Address, key: &U256) -> U256 {
        self.state.get_storage(address, key)
    }

    /// Submits a transaction to the pending pool.
    pub fn submit_transaction(&self, tx: SignedTransaction) -> Result<B256, VMError> {
        // Basic validation
        if tx.chain_id != self.config.chain_id {
            return Err(VMError::InvalidChainId);
        }

        let sender_info = self.state.get_account(&tx.from);

        // Check nonce
        let pending_nonce = self.pending_nonce(&tx.from);
        if tx.nonce < pending_nonce {
            return Err(VMError::NonceTooLow);
        }
        if tx.nonce > pending_nonce {
            return Err(VMError::NonceTooHigh);
        }

        // Check balance
        let max_cost = tx.max_cost();
        if sender_info.balance < max_cost {
            return Err(VMError::InsufficientBalance);
        }

        let hash = tx.hash;
        self.pending_txs.write().push(tx);

        Ok(hash)
    }

    /// Returns pending transaction count for an address.
    fn pending_nonce(&self, address: &Address) -> u64 {
        let base_nonce = self.state.get_account(address).nonce;
        let pending = self.pending_txs.read();
        let pending_count = pending.iter().filter(|tx| &tx.from == address).count();
        base_nonce + pending_count as u64
    }

    /// Returns pending transactions.
    pub fn pending_transactions(&self) -> Vec<SignedTransaction> {
        self.pending_txs.read().clone()
    }

    /// Builds a new block from pending transactions.
    pub fn build_block(&self) -> Result<Block, VMError> {
        let head = self.head().ok_or(VMError::NoHead)?;
        let timestamp = current_timestamp();

        let mut builder = BlockBuilder::new(
            head.header.clone(),
            self.config.fee_recipient,
            timestamp,
        );

        // Set block context for executor
        let block_ctx = BlockContext {
            number: head.header.number + 1,
            timestamp,
            coinbase: self.config.fee_recipient,
            base_fee: head.header.next_base_fee(
                head.header.gas_limit / 2,
                8,
            ),
            gas_limit: self.config.block_gas_limit,
            ..Default::default()
        };
        self.executor.write().set_block_context(block_ctx);

        // Execute pending transactions
        let pending = std::mem::take(&mut *self.pending_txs.write());
        let mut executed = Vec::new();

        for tx in pending {
            // Check if we have room
            if builder.remaining_gas() < tx.gas_limit {
                // Return to pending pool
                self.pending_txs.write().push(tx);
                continue;
            }

            // Execute transaction
            match self.executor.write().execute(&tx) {
                Ok(outcome) => {
                    builder.add_transaction(tx.clone(), outcome.receipt.clone());
                    self.receipts.write().insert(tx.hash, outcome.receipt);
                    executed.push(tx);
                }
                Err(e) => {
                    tracing::warn!("Transaction execution failed: {:?}", e);
                    // Don't re-add failed transactions
                }
            }
        }

        // Commit state changes
        let state_root = self.state.commit();

        // Build block
        let block = builder.build(hash_key_to_b256(state_root));

        Ok(block)
    }

    /// Verifies and applies a block.
    pub fn verify_block(&self, block: &Block) -> Result<(), VMError> {
        let head = self.head().ok_or(VMError::NoHead)?;

        // Check parent hash
        if block.header.parent_hash != head.hash() {
            return Err(VMError::InvalidParentHash);
        }

        // Check block number
        if block.header.number != head.header.number + 1 {
            return Err(VMError::InvalidBlockNumber);
        }

        // Check timestamp
        if block.header.timestamp <= head.header.timestamp {
            return Err(VMError::InvalidTimestamp);
        }

        // Check gas limit
        if block.header.gas_limit > self.config.block_gas_limit {
            return Err(VMError::InvalidGasLimit);
        }

        // Verify transactions
        let block_ctx = BlockContext {
            number: block.header.number,
            timestamp: block.header.timestamp,
            coinbase: block.header.coinbase,
            base_fee: block.header.base_fee,
            gas_limit: block.header.gas_limit,
            ..Default::default()
        };
        self.executor.write().set_block_context(block_ctx);

        // Execute all transactions
        let mut receipts = Vec::new();
        for tx in &block.transactions {
            let outcome = self.executor.write().execute(tx)?;
            receipts.push(outcome.receipt);
        }

        // Verify receipts root
        let computed_receipts_root = block.compute_receipts_root(&receipts);
        if computed_receipts_root != block.header.receipts_root {
            return Err(VMError::InvalidReceiptsRoot);
        }

        // Verify transactions root
        let computed_tx_root = block.compute_transactions_root();
        if computed_tx_root != block.header.transactions_root {
            return Err(VMError::InvalidTransactionsRoot);
        }

        // Commit and verify state root
        let state_root = self.state.commit();
        if hash_key_to_b256(state_root) != block.header.state_root {
            return Err(VMError::InvalidStateRoot);
        }

        Ok(())
    }

    /// Accepts a verified block.
    pub fn accept_block(&self, block: Block) -> Result<(), VMError> {
        let hash = block.hash();
        let number = block.header.number;

        // Store receipts
        for tx in &block.transactions {
            if let Some(receipt) = self.receipts.read().get(&tx.hash).cloned() {
                self.receipts.write().insert(tx.hash, receipt);
            }
        }

        // Store block
        self.blocks_by_hash.write().insert(hash, block.clone());
        self.blocks_by_number.write().insert(number, hash);
        *self.head.write() = Some(block);

        // Remove executed transactions from pending
        let tx_hashes: std::collections::HashSet<_> = self.head()
            .map(|b| b.transactions.iter().map(|t| t.hash).collect())
            .unwrap_or_default();
        self.pending_txs.write().retain(|tx| !tx_hashes.contains(&tx.hash));

        Ok(())
    }

    /// Calls a contract (eth_call).
    pub fn call(
        &self,
        from: Address,
        to: Address,
        data: Bytes,
        value: U256,
        gas_limit: Option<u64>,
    ) -> Result<Bytes, VMError> {
        self.executor
            .read()
            .call(from, to, data, value, gas_limit)
            .map_err(VMError::Execution)
    }

    /// Estimates gas for a transaction.
    pub fn estimate_gas(&self, tx: &SignedTransaction) -> Result<u64, VMError> {
        self.executor
            .read()
            .estimate_gas(tx)
            .map_err(VMError::Execution)
    }

    /// Returns a transaction receipt.
    pub fn get_receipt(&self, tx_hash: &B256) -> Option<Receipt> {
        self.receipts.read().get(tx_hash).cloned()
    }

    /// Returns the VM state.
    pub fn state(&self) -> VMState {
        *self.vm_state.read()
    }

    /// Returns the chain ID.
    pub fn chain_id(&self) -> u64 {
        self.config.chain_id
    }

    /// Returns the current block number.
    pub fn block_number(&self) -> u64 {
        self.head().map(|b| b.header.number).unwrap_or(0)
    }

    /// Returns the current gas price (base fee).
    pub fn gas_price(&self) -> U256 {
        self.head()
            .map(|b| b.header.base_fee)
            .unwrap_or(self.config.min_base_fee)
    }
}

/// VM errors.
#[derive(Debug, Error)]
pub enum VMError {
    #[error("invalid chain ID")]
    InvalidChainId,

    #[error("nonce too low")]
    NonceTooLow,

    #[error("nonce too high")]
    NonceTooHigh,

    #[error("insufficient balance")]
    InsufficientBalance,

    #[error("no head block")]
    NoHead,

    #[error("invalid parent hash")]
    InvalidParentHash,

    #[error("invalid block number")]
    InvalidBlockNumber,

    #[error("invalid timestamp")]
    InvalidTimestamp,

    #[error("invalid gas limit")]
    InvalidGasLimit,

    #[error("invalid state root")]
    InvalidStateRoot,

    #[error("invalid transactions root")]
    InvalidTransactionsRoot,

    #[error("invalid receipts root")]
    InvalidReceiptsRoot,

    #[error("execution error: {0}")]
    Execution(#[from] ExecutionError),

    #[error("database error: {0}")]
    Database(String),
}

// Helper functions

fn keccak256(data: &[u8]) -> B256 {
    use sha3::{Digest, Keccak256};
    let mut hasher = Keccak256::new();
    hasher.update(data);
    B256::from_slice(&hasher.finalize())
}

fn hash_key_to_b256(key: HashKey) -> B256 {
    B256::from_slice(&key)
}

fn current_timestamp() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_db::MemDb;

    fn setup_vm() -> EvmVM {
        let db = Arc::new(MemDb::new());
        EvmVM::new(VMConfig::default(), db)
    }

    #[test]
    fn test_vm_initialization() {
        let vm = setup_vm();

        let genesis = GenesisConfig {
            chain_id: 43114,
            timestamp: 1609459200,
            alloc: HashMap::new(),
            extra_data: Bytes::new(),
        };

        let block = vm.initialize(genesis).unwrap();
        assert_eq!(block.number(), 0);
        assert_eq!(vm.state(), VMState::Running);
    }

    #[test]
    fn test_genesis_allocation() {
        let vm = setup_vm();

        let mut alloc = HashMap::new();
        let addr = Address::from([0x11; 20]);
        alloc.insert(addr, GenesisAccount {
            balance: U256::from(1_000_000_000_000_000_000u64), // 1 ETH
            nonce: 0,
            code: Bytes::new(),
            storage: HashMap::new(),
        });

        let genesis = GenesisConfig {
            chain_id: 43114,
            timestamp: 1609459200,
            alloc,
            extra_data: Bytes::new(),
        };

        vm.initialize(genesis).unwrap();

        let balance = vm.get_balance(&addr);
        assert_eq!(balance, U256::from(1_000_000_000_000_000_000u64));
    }

    #[test]
    fn test_get_block() {
        let vm = setup_vm();

        let genesis = GenesisConfig {
            chain_id: 43114,
            timestamp: 1609459200,
            alloc: HashMap::new(),
            extra_data: Bytes::new(),
        };

        let block = vm.initialize(genesis).unwrap();
        let hash = block.hash();

        let retrieved = vm.get_block(&hash).unwrap();
        assert_eq!(retrieved.hash(), hash);

        let by_number = vm.get_block_by_number(0).unwrap();
        assert_eq!(by_number.hash(), hash);
    }

    #[test]
    fn test_chain_id() {
        let config = VMConfig {
            chain_id: 43113, // Fuji
            ..Default::default()
        };
        let db = Arc::new(MemDb::new());
        let vm = EvmVM::new(config, db);

        assert_eq!(vm.chain_id(), 43113);
    }

    #[test]
    fn test_vm_state() {
        let vm = setup_vm();
        assert_eq!(vm.state(), VMState::Initializing);

        vm.initialize(GenesisConfig::default()).unwrap();
        assert_eq!(vm.state(), VMState::Running);
    }

    #[test]
    fn test_gas_price() {
        let vm = setup_vm();
        vm.initialize(GenesisConfig::default()).unwrap();

        let gas_price = vm.gas_price();
        assert_eq!(gas_price, U256::from(25_000_000_000u64));
    }
}
