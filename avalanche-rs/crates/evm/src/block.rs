//! EVM block structures.
//!
//! This module provides Ethereum block types:
//! - Block headers
//! - Full blocks with transactions
//! - Block building

use std::collections::HashMap;

use alloy_primitives::{Address, Bytes, B256, B64, U256};
use alloy_rlp::Encodable;
use sha3::{Digest, Keccak256};

use crate::transaction::{Receipt, SignedTransaction};

/// Block header.
#[derive(Debug, Clone)]
pub struct Header {
    /// Parent block hash.
    pub parent_hash: B256,
    /// Ommers/uncles hash (always empty post-merge).
    pub ommers_hash: B256,
    /// Coinbase/miner/fee recipient.
    pub coinbase: Address,
    /// State root hash.
    pub state_root: B256,
    /// Transactions root hash.
    pub transactions_root: B256,
    /// Receipts root hash.
    pub receipts_root: B256,
    /// Logs bloom filter.
    pub logs_bloom: [u8; 256],
    /// Difficulty (0 post-merge).
    pub difficulty: U256,
    /// Block number.
    pub number: u64,
    /// Gas limit.
    pub gas_limit: u64,
    /// Gas used.
    pub gas_used: u64,
    /// Block timestamp.
    pub timestamp: u64,
    /// Extra data.
    pub extra_data: Bytes,
    /// Mix hash / prevrandao.
    pub mix_hash: B256,
    /// Nonce (0 post-merge).
    pub nonce: B64,
    /// Base fee per gas (EIP-1559).
    pub base_fee: U256,
    /// Withdrawals root (EIP-4895).
    pub withdrawals_root: Option<B256>,
    /// Blob gas used (EIP-4844).
    pub blob_gas_used: Option<u64>,
    /// Excess blob gas (EIP-4844).
    pub excess_blob_gas: Option<u64>,
    /// Parent beacon block root (EIP-4788).
    pub parent_beacon_block_root: Option<B256>,
}

impl Default for Header {
    fn default() -> Self {
        Self {
            parent_hash: B256::ZERO,
            ommers_hash: Self::EMPTY_OMMERS_HASH,
            coinbase: Address::ZERO,
            state_root: B256::ZERO,
            transactions_root: Self::EMPTY_TRANSACTIONS_ROOT,
            receipts_root: Self::EMPTY_RECEIPTS_ROOT,
            logs_bloom: [0u8; 256],
            difficulty: U256::ZERO,
            number: 0,
            gas_limit: 8_000_000,
            gas_used: 0,
            timestamp: 0,
            extra_data: Bytes::new(),
            mix_hash: B256::ZERO,
            nonce: B64::ZERO,
            base_fee: U256::ZERO,
            withdrawals_root: None,
            blob_gas_used: None,
            excess_blob_gas: None,
            parent_beacon_block_root: None,
        }
    }
}

impl Header {
    /// Empty ommers hash (keccak256 of empty RLP list).
    pub const EMPTY_OMMERS_HASH: B256 = B256::new([
        0x1d, 0xcc, 0x4d, 0xe8, 0xde, 0xc7, 0x5d, 0x7a,
        0xab, 0x85, 0xb5, 0x67, 0xb6, 0xcc, 0xd4, 0x1a,
        0xd3, 0x12, 0x45, 0x1b, 0x94, 0x8a, 0x74, 0x13,
        0xf0, 0xa1, 0x42, 0xfd, 0x40, 0xd4, 0x93, 0x47,
    ]);

    /// Empty transactions root.
    pub const EMPTY_TRANSACTIONS_ROOT: B256 = B256::new([
        0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6,
        0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0, 0xf8, 0x6e,
        0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0,
        0x01, 0x62, 0x2f, 0xb5, 0xe3, 0x63, 0xb4, 0x21,
    ]);

    /// Empty receipts root.
    pub const EMPTY_RECEIPTS_ROOT: B256 = B256::new([
        0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6,
        0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0, 0xf8, 0x6e,
        0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0,
        0x01, 0x62, 0x2f, 0xb5, 0xe3, 0x63, 0xb4, 0x21,
    ]);

    /// Computes the block hash.
    pub fn hash(&self) -> B256 {
        let encoded = self.encode_rlp();
        keccak256(&encoded)
    }

    /// RLP encodes the header.
    pub fn encode_rlp(&self) -> Vec<u8> {
        let mut payload = Vec::new();

        self.parent_hash.encode(&mut payload);
        self.ommers_hash.encode(&mut payload);
        self.coinbase.encode(&mut payload);
        self.state_root.encode(&mut payload);
        self.transactions_root.encode(&mut payload);
        self.receipts_root.encode(&mut payload);
        payload.extend_from_slice(&self.logs_bloom);
        self.difficulty.encode(&mut payload);
        self.number.encode(&mut payload);
        self.gas_limit.encode(&mut payload);
        self.gas_used.encode(&mut payload);
        self.timestamp.encode(&mut payload);
        self.extra_data.encode(&mut payload);
        self.mix_hash.encode(&mut payload);
        self.nonce.encode(&mut payload);
        self.base_fee.encode(&mut payload);

        // Optional fields (post-Shanghai)
        if let Some(root) = &self.withdrawals_root {
            root.encode(&mut payload);
        }
        if let Some(blob_gas) = self.blob_gas_used {
            blob_gas.encode(&mut payload);
        }
        if let Some(excess) = self.excess_blob_gas {
            excess.encode(&mut payload);
        }
        if let Some(root) = &self.parent_beacon_block_root {
            root.encode(&mut payload);
        }

        let mut out = Vec::new();
        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(&mut out);
        out.extend_from_slice(&payload);
        out
    }

    /// Creates a genesis block header.
    pub fn genesis(chain_id: u64) -> Self {
        Self {
            parent_hash: B256::ZERO,
            ommers_hash: Self::EMPTY_OMMERS_HASH,
            coinbase: Address::ZERO,
            state_root: B256::ZERO, // Will be set after genesis state
            transactions_root: Self::EMPTY_TRANSACTIONS_ROOT,
            receipts_root: Self::EMPTY_RECEIPTS_ROOT,
            logs_bloom: [0; 256],
            difficulty: U256::ZERO,
            number: 0,
            gas_limit: 8_000_000,
            gas_used: 0,
            timestamp: 0,
            extra_data: Bytes::from(format!("Avalanche C-Chain {}", chain_id).into_bytes()),
            mix_hash: B256::ZERO,
            nonce: B64::ZERO,
            base_fee: U256::from(25_000_000_000u64), // 25 gwei
            withdrawals_root: None,
            blob_gas_used: None,
            excess_blob_gas: None,
            parent_beacon_block_root: None,
        }
    }

    /// Calculates the next base fee using EIP-1559 formula.
    pub fn next_base_fee(&self, target_gas: u64, elasticity: u64) -> U256 {
        let parent_gas_used = self.gas_used;
        let parent_gas_target = target_gas;
        let parent_base_fee = self.base_fee;

        if parent_gas_used == parent_gas_target {
            return parent_base_fee;
        }

        if parent_gas_used > parent_gas_target {
            // Increase base fee
            let gas_delta = parent_gas_used - parent_gas_target;
            let base_fee_delta = parent_base_fee
                * U256::from(gas_delta)
                / U256::from(parent_gas_target)
                / U256::from(elasticity);
            let base_fee_delta = base_fee_delta.max(U256::from(1));
            parent_base_fee + base_fee_delta
        } else {
            // Decrease base fee
            let gas_delta = parent_gas_target - parent_gas_used;
            let base_fee_delta = parent_base_fee
                * U256::from(gas_delta)
                / U256::from(parent_gas_target)
                / U256::from(elasticity);
            if parent_base_fee > base_fee_delta {
                parent_base_fee - base_fee_delta
            } else {
                U256::ZERO
            }
        }
    }
}

/// Full block with transactions.
#[derive(Debug, Clone, Default)]
pub struct Block {
    /// Block header.
    pub header: Header,
    /// Block transactions.
    pub transactions: Vec<SignedTransaction>,
    /// Ommers/uncles (always empty post-merge).
    pub ommers: Vec<Header>,
    /// Withdrawals (post-Shanghai).
    pub withdrawals: Option<Vec<Withdrawal>>,
}

impl Block {
    /// Creates a new block.
    pub fn new(header: Header, transactions: Vec<SignedTransaction>) -> Self {
        Self {
            header,
            transactions,
            ommers: vec![],
            withdrawals: None,
        }
    }

    /// Returns the block hash.
    pub fn hash(&self) -> B256 {
        self.header.hash()
    }

    /// Returns the block number.
    pub fn number(&self) -> u64 {
        self.header.number
    }

    /// Computes the transactions root.
    pub fn compute_transactions_root(&self) -> B256 {
        if self.transactions.is_empty() {
            return Header::EMPTY_TRANSACTIONS_ROOT;
        }

        // Build trie of transactions
        let mut trie_data: HashMap<Vec<u8>, Vec<u8>> = HashMap::new();
        for (i, tx) in self.transactions.iter().enumerate() {
            let key = encode_index(i);
            let value = tx.encode();
            trie_data.insert(key, value);
        }

        compute_trie_root(&trie_data)
    }

    /// Computes the receipts root.
    pub fn compute_receipts_root(&self, receipts: &[Receipt]) -> B256 {
        if receipts.is_empty() {
            return Header::EMPTY_RECEIPTS_ROOT;
        }

        let mut trie_data: HashMap<Vec<u8>, Vec<u8>> = HashMap::new();
        for (i, receipt) in receipts.iter().enumerate() {
            let key = encode_index(i);
            let value = receipt.encode();
            trie_data.insert(key, value);
        }

        compute_trie_root(&trie_data)
    }

    /// Combines logs from multiple receipts into a single bloom.
    pub fn compute_logs_bloom(&self, receipts: &[Receipt]) -> [u8; 256] {
        let mut bloom = [0u8; 256];
        for receipt in receipts {
            for i in 0..256 {
                bloom[i] |= receipt.logs_bloom[i];
            }
        }
        bloom
    }
}

/// Withdrawal (EIP-4895).
#[derive(Debug, Clone, Default)]
pub struct Withdrawal {
    /// Withdrawal index.
    pub index: u64,
    /// Validator index.
    pub validator_index: u64,
    /// Withdrawal address.
    pub address: Address,
    /// Withdrawal amount in Gwei.
    pub amount: u64,
}

impl Withdrawal {
    /// RLP encodes the withdrawal.
    pub fn encode_rlp(&self) -> Vec<u8> {
        let mut payload = Vec::new();
        self.index.encode(&mut payload);
        self.validator_index.encode(&mut payload);
        self.address.encode(&mut payload);
        self.amount.encode(&mut payload);

        let mut out = Vec::new();
        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(&mut out);
        out.extend_from_slice(&payload);
        out
    }
}

/// Block builder for assembling new blocks.
pub struct BlockBuilder {
    /// Parent block header.
    parent: Header,
    /// Coinbase address.
    coinbase: Address,
    /// Timestamp.
    timestamp: u64,
    /// Extra data.
    extra_data: Bytes,
    /// Transactions to include.
    transactions: Vec<SignedTransaction>,
    /// Receipts from execution.
    receipts: Vec<Receipt>,
    /// Gas used.
    gas_used: u64,
    /// Gas limit.
    gas_limit: u64,
}

impl BlockBuilder {
    /// Creates a new block builder.
    pub fn new(parent: Header, coinbase: Address, timestamp: u64) -> Self {
        let gas_limit = parent.gas_limit; // For now, keep same limit

        Self {
            parent,
            coinbase,
            timestamp,
            extra_data: Bytes::new(),
            transactions: vec![],
            receipts: vec![],
            gas_used: 0,
            gas_limit,
        }
    }

    /// Sets extra data.
    pub fn extra_data(mut self, data: Bytes) -> Self {
        self.extra_data = data;
        self
    }

    /// Adds a transaction with its receipt.
    pub fn add_transaction(&mut self, tx: SignedTransaction, receipt: Receipt) {
        self.gas_used += receipt.gas_used;
        self.transactions.push(tx);
        self.receipts.push(receipt);
    }

    /// Returns remaining gas capacity.
    pub fn remaining_gas(&self) -> u64 {
        self.gas_limit.saturating_sub(self.gas_used)
    }

    /// Builds the block.
    pub fn build(self, state_root: B256) -> Block {
        let block = Block {
            header: Header::default(),
            transactions: self.transactions,
            ommers: vec![],
            withdrawals: None,
        };

        let transactions_root = block.compute_transactions_root();
        let receipts_root = block.compute_receipts_root(&self.receipts);
        let logs_bloom = block.compute_logs_bloom(&self.receipts);

        // Calculate next base fee
        let target_gas = self.parent.gas_limit / 2; // EIP-1559 target is 50%
        let base_fee = self.parent.next_base_fee(target_gas, 8); // Elasticity of 8

        Block {
            header: Header {
                parent_hash: self.parent.hash(),
                ommers_hash: Header::EMPTY_OMMERS_HASH,
                coinbase: self.coinbase,
                state_root,
                transactions_root,
                receipts_root,
                logs_bloom,
                difficulty: U256::ZERO,
                number: self.parent.number + 1,
                gas_limit: self.gas_limit,
                gas_used: self.gas_used,
                timestamp: self.timestamp,
                extra_data: self.extra_data,
                mix_hash: B256::ZERO, // Would be prevrandao
                nonce: B64::ZERO,
                base_fee,
                withdrawals_root: None,
                blob_gas_used: None,
                excess_blob_gas: None,
                parent_beacon_block_root: None,
            },
            transactions: block.transactions,
            ommers: vec![],
            withdrawals: None,
        }
    }
}

// Helper functions

fn keccak256(data: &[u8]) -> B256 {
    let mut hasher = Keccak256::new();
    hasher.update(data);
    B256::from_slice(&hasher.finalize())
}

fn encode_index(index: usize) -> Vec<u8> {
    if index == 0 {
        vec![0x80] // RLP empty bytes
    } else {
        let mut encoded = Vec::new();
        (index as u64).encode(&mut encoded);
        encoded
    }
}

fn compute_trie_root(data: &HashMap<Vec<u8>, Vec<u8>>) -> B256 {
    // Simplified: in production would use full Merkle Patricia Trie
    // For now, hash all key-value pairs together
    let mut hasher = Keccak256::new();
    let mut keys: Vec<_> = data.keys().collect();
    keys.sort();
    for key in keys {
        hasher.update(key);
        hasher.update(data.get(key).unwrap());
    }
    B256::from_slice(&hasher.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_header_hash() {
        let header = Header::genesis(43114);
        let hash = header.hash();
        assert_ne!(hash, B256::ZERO);

        // Same header should produce same hash
        let hash2 = header.hash();
        assert_eq!(hash, hash2);
    }

    #[test]
    fn test_genesis_header() {
        let header = Header::genesis(43114);
        assert_eq!(header.number, 0);
        assert_eq!(header.parent_hash, B256::ZERO);
        assert_eq!(header.difficulty, U256::ZERO);
    }

    #[test]
    fn test_next_base_fee_increase() {
        let header = Header {
            gas_used: 10_000_000,
            gas_limit: 8_000_000,
            base_fee: U256::from(25_000_000_000u64),
            ..Default::default()
        };

        let target = 4_000_000; // 50% of limit
        let next = header.next_base_fee(target, 8);
        assert!(next > header.base_fee);
    }

    #[test]
    fn test_next_base_fee_decrease() {
        let header = Header {
            gas_used: 2_000_000,
            gas_limit: 8_000_000,
            base_fee: U256::from(25_000_000_000u64),
            ..Default::default()
        };

        let target = 4_000_000;
        let next = header.next_base_fee(target, 8);
        assert!(next < header.base_fee);
    }

    #[test]
    fn test_block_builder() {
        let parent = Header::genesis(43114);
        let builder = BlockBuilder::new(
            parent.clone(),
            Address::from([0x11; 20]),
            1234567890,
        );

        assert_eq!(builder.remaining_gas(), parent.gas_limit);

        let state_root = B256::ZERO;
        let block = builder.build(state_root);

        assert_eq!(block.number(), 1);
        assert_eq!(block.header.parent_hash, parent.hash());
    }

    #[test]
    fn test_empty_block_roots() {
        let block = Block::default();

        let tx_root = block.compute_transactions_root();
        assert_eq!(tx_root, Header::EMPTY_TRANSACTIONS_ROOT);

        let receipts_root = block.compute_receipts_root(&[]);
        assert_eq!(receipts_root, Header::EMPTY_RECEIPTS_ROOT);
    }

    #[test]
    fn test_withdrawal_encoding() {
        let withdrawal = Withdrawal {
            index: 0,
            validator_index: 12345,
            address: Address::from([0x22; 20]),
            amount: 32_000_000_000, // 32 ETH in Gwei
        };

        let encoded = withdrawal.encode_rlp();
        assert!(!encoded.is_empty());
    }
}
