//! EVM transaction executor using revm.
//!
//! This module provides transaction execution:
//! - Execute transactions against state
//! - Generate receipts and logs
//! - Handle contract creation and calls

use std::sync::Arc;

use alloy_primitives::{Address, Bytes, B256, U256};
use revm::{
    db::{CacheDB, EmptyDB},
    primitives::{
        AccountInfo, BlockEnv, CfgEnv, Env, ExecutionResult, Output,
        ResultAndState, SpecId, TransactTo, TxEnv,
    },
    Evm,
};
use sha3::{Digest, Keccak256};

use crate::state::EvmState;
use crate::transaction::{AccessList, Log, Receipt, SignedTransaction};

/// EVM executor configuration.
#[derive(Debug, Clone)]
pub struct ExecutorConfig {
    /// Chain ID.
    pub chain_id: u64,
    /// EVM specification to use.
    pub spec_id: SpecId,
    /// Block gas limit.
    pub block_gas_limit: u64,
}

impl Default for ExecutorConfig {
    fn default() -> Self {
        Self {
            chain_id: 43114, // Avalanche C-Chain mainnet
            spec_id: SpecId::CANCUN,
            block_gas_limit: 8_000_000,
        }
    }
}

/// Block context for execution.
#[derive(Debug, Clone)]
pub struct BlockContext {
    /// Block number.
    pub number: u64,
    /// Block timestamp.
    pub timestamp: u64,
    /// Block coinbase (fee recipient).
    pub coinbase: Address,
    /// Block difficulty (0 post-merge).
    pub difficulty: U256,
    /// Previous block hash (randomness source).
    pub prev_randao: B256,
    /// Block gas limit.
    pub gas_limit: u64,
    /// Base fee per gas (EIP-1559).
    pub base_fee: U256,
}

impl Default for BlockContext {
    fn default() -> Self {
        Self {
            number: 0,
            timestamp: 0,
            coinbase: Address::ZERO,
            difficulty: U256::ZERO,
            prev_randao: B256::ZERO,
            gas_limit: 8_000_000,
            base_fee: U256::from(25_000_000_000u64), // 25 gwei
        }
    }
}

/// Execution result with receipt.
#[derive(Debug)]
pub struct ExecutionOutcome {
    /// Transaction receipt.
    pub receipt: Receipt,
    /// Gas refund amount.
    pub gas_refund: u64,
    /// Logs generated.
    pub logs: Vec<Log>,
    /// Contract address (if created).
    pub contract_address: Option<Address>,
    /// Output data (return value or revert reason).
    pub output: Bytes,
}

/// EVM transaction executor.
pub struct Executor {
    /// Configuration.
    config: ExecutorConfig,
    /// State database.
    state: Arc<EvmState>,
    /// Current block context.
    block_ctx: BlockContext,
    /// Cumulative gas used in current block.
    cumulative_gas: u64,
}

impl Executor {
    /// Creates a new executor.
    pub fn new(config: ExecutorConfig, state: Arc<EvmState>) -> Self {
        Self {
            config,
            state,
            block_ctx: BlockContext::default(),
            cumulative_gas: 0,
        }
    }

    /// Sets the block context.
    pub fn set_block_context(&mut self, ctx: BlockContext) {
        self.block_ctx = ctx;
        self.cumulative_gas = 0;
    }

    /// Executes a transaction.
    pub fn execute(&mut self, tx: &SignedTransaction) -> Result<ExecutionOutcome, ExecutionError> {
        // Validate transaction
        self.validate_transaction(tx)?;

        // Build revm environment
        let env = self.build_env(tx);

        // Create revm instance with our state
        let mut cache_db = CacheDB::new(EmptyDB::default());

        // Load required accounts into cache
        self.preload_accounts(&mut cache_db, tx);

        // Execute with revm
        let mut evm = Evm::builder()
            .with_env(Box::new(env))
            .with_db(&mut cache_db)
            .build();

        let result = evm.transact().map_err(|e| ExecutionError::Evm(format!("{:?}", e)))?;

        // Process result
        self.process_result(tx, result)
    }

    /// Validates a transaction before execution.
    fn validate_transaction(&self, tx: &SignedTransaction) -> Result<(), ExecutionError> {
        // Check chain ID
        if tx.chain_id != self.config.chain_id {
            return Err(ExecutionError::WrongChainId {
                expected: self.config.chain_id,
                got: tx.chain_id,
            });
        }

        // Check gas limit against block gas limit
        if tx.gas_limit > self.config.block_gas_limit {
            return Err(ExecutionError::GasLimitExceeded {
                limit: self.config.block_gas_limit,
                got: tx.gas_limit,
            });
        }

        // Check if adding this tx would exceed block gas limit
        if self.cumulative_gas + tx.gas_limit > self.config.block_gas_limit {
            return Err(ExecutionError::BlockGasLimitExceeded);
        }

        // Get sender account
        let sender_info = self.state.get_account(&tx.from);

        // Check nonce
        if tx.nonce != sender_info.nonce {
            return Err(ExecutionError::InvalidNonce {
                expected: sender_info.nonce,
                got: tx.nonce,
            });
        }

        // Check balance for max cost
        let max_cost = tx.max_cost();
        if sender_info.balance < max_cost {
            return Err(ExecutionError::InsufficientBalance {
                required: max_cost,
                available: sender_info.balance,
            });
        }

        // Check gas price against base fee
        let effective_gas_price = tx.effective_gas_price(self.block_ctx.base_fee);
        if effective_gas_price < self.block_ctx.base_fee {
            return Err(ExecutionError::GasPriceTooLow {
                base_fee: self.block_ctx.base_fee,
                gas_price: effective_gas_price,
            });
        }

        Ok(())
    }

    /// Builds the revm environment.
    fn build_env(&self, tx: &SignedTransaction) -> Env {
        let mut env = Env::default();

        // Configure chain
        env.cfg = CfgEnv::default();
        env.cfg.chain_id = self.config.chain_id;

        // Block environment
        env.block = BlockEnv {
            number: U256::from(self.block_ctx.number),
            coinbase: self.block_ctx.coinbase,
            timestamp: U256::from(self.block_ctx.timestamp),
            gas_limit: U256::from(self.block_ctx.gas_limit),
            basefee: self.block_ctx.base_fee,
            difficulty: self.block_ctx.difficulty,
            prevrandao: Some(self.block_ctx.prev_randao),
            blob_excess_gas_and_price: None,
        };

        // Transaction environment
        env.tx = TxEnv {
            caller: tx.from,
            gas_limit: tx.gas_limit,
            gas_price: tx.effective_gas_price(self.block_ctx.base_fee),
            transact_to: match tx.to {
                Some(addr) => TransactTo::Call(addr),
                None => TransactTo::Create,
            },
            value: tx.value,
            data: tx.data.clone(),
            nonce: Some(tx.nonce),
            chain_id: Some(tx.chain_id),
            access_list: self.convert_access_list(&tx.access_list),
            gas_priority_fee: tx.max_priority_fee,
            blob_hashes: vec![],
            max_fee_per_blob_gas: None,
            authorization_list: None,
        };

        env
    }

    /// Preloads accounts needed for execution into the cache.
    fn preload_accounts(&self, cache_db: &mut CacheDB<EmptyDB>, tx: &SignedTransaction) {
        // Load sender
        let sender_info = self.state.get_account(&tx.from);
        cache_db.insert_account_info(tx.from, sender_info.to_revm());

        // Load recipient if exists
        if let Some(to) = tx.to {
            let to_info = self.state.get_account(&to);
            let code = self.state.get_code(&to_info.code_hash);
            cache_db.insert_account_info(to, AccountInfo {
                balance: to_info.balance,
                nonce: to_info.nonce,
                code_hash: to_info.code_hash,
                code: if code.is_empty() {
                    None
                } else {
                    Some(revm::primitives::Bytecode::new_raw(code))
                },
            });
        }

        // Load coinbase
        let coinbase_info = self.state.get_account(&self.block_ctx.coinbase);
        cache_db.insert_account_info(self.block_ctx.coinbase, coinbase_info.to_revm());

        // Load access list accounts
        for item in &tx.access_list {
            let info = self.state.get_account(&item.address);
            cache_db.insert_account_info(item.address, info.to_revm());
        }
    }

    /// Processes the execution result.
    fn process_result(
        &mut self,
        tx: &SignedTransaction,
        result: ResultAndState,
    ) -> Result<ExecutionOutcome, ExecutionError> {
        let ResultAndState { result, state: state_changes } = result;

        // Extract execution details
        let (success, gas_used, gas_refund, output, logs) = match result {
            ExecutionResult::Success { gas_used, gas_refunded, output, logs, .. } => {
                let output_bytes = match output {
                    Output::Call(bytes) => bytes,
                    Output::Create(bytes, _) => bytes,
                };
                (true, gas_used, gas_refunded, output_bytes, logs)
            }
            ExecutionResult::Revert { gas_used, output } => {
                (false, gas_used, 0, output, vec![])
            }
            ExecutionResult::Halt { reason, .. } => {
                return Err(ExecutionError::Halt(format!("{:?}", reason)));
            }
        };

        // Apply state changes
        for (address, account) in state_changes {
            if account.is_touched() {
                let info = crate::state::AccountInfo {
                    balance: account.info.balance,
                    nonce: account.info.nonce,
                    code_hash: account.info.code_hash,
                    code: account.info.code.as_ref().map(|c| c.bytes().clone()).unwrap_or_default(),
                };
                self.state.set_account(address, info);

                // Apply storage changes
                for (slot, value) in account.storage {
                    self.state.set_storage(address, slot, value.present_value);
                }
            }
        }

        // Update cumulative gas
        self.cumulative_gas += gas_used;

        // Increment sender nonce
        self.state.increment_nonce(&tx.from);

        // Calculate contract address if created
        let contract_address = if tx.is_contract_creation() && success {
            Some(self.compute_create_address(&tx.from, tx.nonce))
        } else {
            None
        };

        // Convert logs
        let converted_logs: Vec<Log> = logs
            .iter()
            .map(|log| Log {
                address: log.address,
                topics: log.topics().to_vec(),
                data: log.data.data.clone(),
            })
            .collect();

        // Build receipt
        let receipt = Receipt {
            tx_type: tx.tx_type,
            success,
            cumulative_gas_used: self.cumulative_gas,
            logs_bloom: self.compute_logs_bloom(&converted_logs),
            logs: converted_logs.clone(),
            gas_used,
            contract_address,
        };

        Ok(ExecutionOutcome {
            receipt,
            gas_refund,
            logs: converted_logs,
            contract_address,
            output,
        })
    }

    /// Converts access list to revm format.
    fn convert_access_list(
        &self,
        list: &AccessList,
    ) -> Vec<revm::primitives::AccessListItem> {
        list.iter()
            .map(|item| revm::primitives::AccessListItem {
                address: item.address,
                storage_keys: item.storage_keys.clone(),
            })
            .collect()
    }

    /// Computes CREATE address.
    fn compute_create_address(&self, sender: &Address, nonce: u64) -> Address {
        use alloy_rlp::Encodable;

        let mut payload = Vec::new();
        sender.encode(&mut payload);
        nonce.encode(&mut payload);

        let mut encoded = Vec::new();
        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(&mut encoded);
        encoded.extend_from_slice(&payload);

        let hash = keccak256(&encoded);
        Address::from_slice(&hash[12..])
    }

    /// Computes the logs bloom filter.
    fn compute_logs_bloom(&self, logs: &[Log]) -> [u8; 256] {
        let mut bloom = [0u8; 256];

        for log in logs {
            // Add address to bloom
            self.add_to_bloom(&mut bloom, log.address.as_slice());

            // Add topics to bloom
            for topic in &log.topics {
                self.add_to_bloom(&mut bloom, topic.as_slice());
            }
        }

        bloom
    }

    /// Adds data to bloom filter.
    fn add_to_bloom(&self, bloom: &mut [u8; 256], data: &[u8]) {
        let hash = keccak256(data);

        for i in 0..3 {
            let bit = (u16::from(hash[i * 2]) << 8 | u16::from(hash[i * 2 + 1])) & 0x7FF;
            let byte_idx = 255 - (bit / 8) as usize;
            let bit_idx = bit % 8;
            bloom[byte_idx] |= 1 << bit_idx;
        }
    }

    /// Returns cumulative gas used in current block.
    pub fn cumulative_gas(&self) -> u64 {
        self.cumulative_gas
    }

    /// Estimates gas for a transaction (dry run).
    pub fn estimate_gas(&self, tx: &SignedTransaction) -> Result<u64, ExecutionError> {
        // Clone state for dry run
        let env = self.build_env(tx);
        let mut cache_db = CacheDB::new(EmptyDB::default());
        self.preload_accounts(&mut cache_db, tx);

        let mut evm = Evm::builder()
            .with_env(Box::new(env))
            .with_db(&mut cache_db)
            .build();

        let result = evm.transact().map_err(|e| ExecutionError::Evm(format!("{:?}", e)))?;

        match result.result {
            ExecutionResult::Success { gas_used, .. } => Ok(gas_used),
            ExecutionResult::Revert { gas_used, .. } => Ok(gas_used),
            ExecutionResult::Halt { reason, .. } => {
                Err(ExecutionError::Halt(format!("{:?}", reason)))
            }
        }
    }

    /// Calls a contract without modifying state (eth_call).
    pub fn call(
        &self,
        from: Address,
        to: Address,
        data: Bytes,
        value: U256,
        gas_limit: Option<u64>,
    ) -> Result<Bytes, ExecutionError> {
        let mut env = Env::default();
        env.cfg.chain_id = self.config.chain_id;
        env.block = BlockEnv {
            number: U256::from(self.block_ctx.number),
            coinbase: self.block_ctx.coinbase,
            timestamp: U256::from(self.block_ctx.timestamp),
            gas_limit: U256::from(self.block_ctx.gas_limit),
            basefee: self.block_ctx.base_fee,
            difficulty: self.block_ctx.difficulty,
            prevrandao: Some(self.block_ctx.prev_randao),
            blob_excess_gas_and_price: None,
        };
        env.tx = TxEnv {
            caller: from,
            gas_limit: gas_limit.unwrap_or(self.config.block_gas_limit),
            gas_price: self.block_ctx.base_fee,
            transact_to: TransactTo::Call(to),
            value,
            data,
            nonce: None,
            chain_id: Some(self.config.chain_id),
            access_list: vec![],
            gas_priority_fee: None,
            blob_hashes: vec![],
            max_fee_per_blob_gas: None,
            authorization_list: None,
        };

        let mut cache_db = CacheDB::new(EmptyDB::default());

        // Load accounts
        let from_info = self.state.get_account(&from);
        cache_db.insert_account_info(from, from_info.to_revm());

        let to_info = self.state.get_account(&to);
        let code = self.state.get_code(&to_info.code_hash);
        cache_db.insert_account_info(to, AccountInfo {
            balance: to_info.balance,
            nonce: to_info.nonce,
            code_hash: to_info.code_hash,
            code: if code.is_empty() {
                None
            } else {
                Some(revm::primitives::Bytecode::new_raw(code))
            },
        });

        let mut evm = Evm::builder()
            .with_env(Box::new(env))
            .with_db(&mut cache_db)
            .build();

        let result = evm.transact().map_err(|e| ExecutionError::Evm(format!("{:?}", e)))?;

        match result.result {
            ExecutionResult::Success { output, .. } => {
                Ok(match output {
                    Output::Call(bytes) => bytes,
                    Output::Create(bytes, _) => bytes,
                })
            }
            ExecutionResult::Revert { output, .. } => {
                Err(ExecutionError::Revert(output))
            }
            ExecutionResult::Halt { reason, .. } => {
                Err(ExecutionError::Halt(format!("{:?}", reason)))
            }
        }
    }
}

/// Execution errors.
#[derive(Debug, Clone, thiserror::Error)]
pub enum ExecutionError {
    #[error("wrong chain ID: expected {expected}, got {got}")]
    WrongChainId { expected: u64, got: u64 },

    #[error("gas limit exceeded: limit {limit}, got {got}")]
    GasLimitExceeded { limit: u64, got: u64 },

    #[error("block gas limit exceeded")]
    BlockGasLimitExceeded,

    #[error("invalid nonce: expected {expected}, got {got}")]
    InvalidNonce { expected: u64, got: u64 },

    #[error("insufficient balance: required {required}, available {available}")]
    InsufficientBalance { required: U256, available: U256 },

    #[error("gas price too low: base fee {base_fee}, gas price {gas_price}")]
    GasPriceTooLow { base_fee: U256, gas_price: U256 },

    #[error("EVM error: {0}")]
    Evm(String),

    #[error("execution halted: {0}")]
    Halt(String),

    #[error("execution reverted: {0:?}")]
    Revert(Bytes),
}

fn keccak256(data: &[u8]) -> B256 {
    let mut hasher = Keccak256::new();
    hasher.update(data);
    B256::from_slice(&hasher.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::transaction::TxType;
    use avalanche_db::MemDb;

    fn setup_executor() -> Executor {
        let db = Arc::new(MemDb::new());
        let state = Arc::new(EvmState::new(db));
        Executor::new(ExecutorConfig::default(), state)
    }

    #[test]
    fn test_executor_config() {
        let config = ExecutorConfig::default();
        assert_eq!(config.chain_id, 43114);
        assert_eq!(config.spec_id, SpecId::CANCUN);
    }

    #[test]
    fn test_block_context() {
        let mut executor = setup_executor();
        let ctx = BlockContext {
            number: 100,
            timestamp: 1234567890,
            coinbase: Address::from([0x11; 20]),
            base_fee: U256::from(30_000_000_000u64),
            ..Default::default()
        };

        executor.set_block_context(ctx.clone());
        assert_eq!(executor.block_ctx.number, 100);
        assert_eq!(executor.cumulative_gas(), 0);
    }

    #[test]
    fn test_create_address_computation() {
        let executor = setup_executor();
        let sender = Address::from([0x11; 20]);

        // Different nonces should produce different addresses
        let addr1 = executor.compute_create_address(&sender, 0);
        let addr2 = executor.compute_create_address(&sender, 1);
        assert_ne!(addr1, addr2);
    }

    #[test]
    fn test_bloom_filter() {
        let executor = setup_executor();
        let logs = vec![Log {
            address: Address::from([0x11; 20]),
            topics: vec![B256::ZERO],
            data: Bytes::from(vec![0x01, 0x02]),
        }];

        let bloom = executor.compute_logs_bloom(&logs);
        // Should have some bits set
        assert!(bloom.iter().any(|&b| b != 0));
    }

    #[test]
    fn test_validation_wrong_chain_id() {
        let executor = setup_executor();
        let tx = SignedTransaction {
            tx_type: TxType::Legacy,
            hash: B256::ZERO,
            from: Address::from([0x11; 20]),
            nonce: 0,
            gas_price: U256::from(30_000_000_000u64),
            max_priority_fee: None,
            gas_limit: 21000,
            to: Some(Address::from([0x22; 20])),
            value: U256::ZERO,
            data: Bytes::new(),
            chain_id: 1, // Wrong chain ID
            v: 0,
            r: U256::ZERO,
            s: U256::ZERO,
            access_list: vec![],
        };

        let result = executor.validate_transaction(&tx);
        assert!(matches!(result, Err(ExecutionError::WrongChainId { .. })));
    }

    #[test]
    fn test_validation_gas_limit_exceeded() {
        let executor = setup_executor();
        let tx = SignedTransaction {
            tx_type: TxType::Legacy,
            hash: B256::ZERO,
            from: Address::from([0x11; 20]),
            nonce: 0,
            gas_price: U256::from(30_000_000_000u64),
            max_priority_fee: None,
            gas_limit: 100_000_000, // Way over block limit
            to: Some(Address::from([0x22; 20])),
            value: U256::ZERO,
            data: Bytes::new(),
            chain_id: 43114,
            v: 0,
            r: U256::ZERO,
            s: U256::ZERO,
            access_list: vec![],
        };

        let result = executor.validate_transaction(&tx);
        assert!(matches!(result, Err(ExecutionError::GasLimitExceeded { .. })));
    }
}
