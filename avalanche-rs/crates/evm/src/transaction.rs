//! EVM transaction types.
//!
//! This module provides Ethereum transaction types:
//! - Legacy transactions
//! - EIP-2930 access list transactions
//! - EIP-1559 dynamic fee transactions

use alloy_primitives::{Address, Bytes, B256, U256};
use alloy_rlp::{Encodable, RlpDecodable, RlpEncodable};
use sha3::{Digest, Keccak256};

/// Transaction type identifier.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum TxType {
    /// Legacy transaction (pre-EIP-2718).
    Legacy = 0,
    /// EIP-2930 access list transaction.
    AccessList = 1,
    /// EIP-1559 dynamic fee transaction.
    DynamicFee = 2,
}

impl TxType {
    /// Returns the transaction type from the first byte.
    pub fn from_byte(byte: u8) -> Option<Self> {
        match byte {
            0 => Some(TxType::Legacy),
            1 => Some(TxType::AccessList),
            2 => Some(TxType::DynamicFee),
            _ => None,
        }
    }
}

/// Access list entry (address and storage keys).
#[derive(Debug, Clone, Default, PartialEq, Eq, RlpEncodable, RlpDecodable)]
pub struct AccessListItem {
    /// Account address.
    pub address: Address,
    /// Storage keys.
    pub storage_keys: Vec<B256>,
}

/// Access list for EIP-2930 transactions.
pub type AccessList = Vec<AccessListItem>;

/// Legacy Ethereum transaction.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct LegacyTx {
    /// Transaction nonce.
    pub nonce: u64,
    /// Gas price in wei.
    pub gas_price: U256,
    /// Gas limit.
    pub gas_limit: u64,
    /// Recipient address (None for contract creation).
    pub to: Option<Address>,
    /// Value in wei.
    pub value: U256,
    /// Input data.
    pub data: Bytes,
    /// Chain ID (EIP-155).
    pub chain_id: Option<u64>,
}

impl LegacyTx {
    /// RLP encodes the transaction for signing.
    pub fn encode_for_signing(&self, out: &mut Vec<u8>) {
        // Build payload first
        let mut payload = Vec::new();
        self.nonce.encode(&mut payload);
        self.gas_price.encode(&mut payload);
        self.gas_limit.encode(&mut payload);
        match &self.to {
            Some(addr) => addr.encode(&mut payload),
            None => payload.push(0x80), // empty bytes
        }
        self.value.encode(&mut payload);
        self.data.encode(&mut payload);

        if let Some(chain_id) = self.chain_id {
            chain_id.encode(&mut payload);
            0u8.encode(&mut payload);
            0u8.encode(&mut payload);
        }

        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(out);
        out.extend_from_slice(&payload);
    }
}

/// EIP-1559 dynamic fee transaction.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct DynamicFeeTx {
    /// Chain ID.
    pub chain_id: u64,
    /// Transaction nonce.
    pub nonce: u64,
    /// Max priority fee per gas (tip).
    pub max_priority_fee_per_gas: U256,
    /// Max fee per gas.
    pub max_fee_per_gas: U256,
    /// Gas limit.
    pub gas_limit: u64,
    /// Recipient address (None for contract creation).
    pub to: Option<Address>,
    /// Value in wei.
    pub value: U256,
    /// Input data.
    pub data: Bytes,
    /// Access list.
    pub access_list: AccessList,
}

impl DynamicFeeTx {
    /// RLP encodes the transaction for signing (without type prefix).
    pub fn encode_for_signing(&self, out: &mut Vec<u8>) {
        // Type prefix
        out.push(TxType::DynamicFee as u8);

        // Encode fields
        let mut payload = Vec::new();
        self.chain_id.encode(&mut payload);
        self.nonce.encode(&mut payload);
        self.max_priority_fee_per_gas.encode(&mut payload);
        self.max_fee_per_gas.encode(&mut payload);
        self.gas_limit.encode(&mut payload);
        match &self.to {
            Some(addr) => addr.encode(&mut payload),
            None => payload.push(0x80),
        }
        self.value.encode(&mut payload);
        self.data.encode(&mut payload);
        encode_access_list(&self.access_list, &mut payload);

        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(out);
        out.extend_from_slice(&payload);
    }
}

/// Signed Ethereum transaction.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SignedTransaction {
    /// Transaction type.
    pub tx_type: TxType,
    /// Transaction hash.
    pub hash: B256,
    /// Sender address (recovered from signature).
    pub from: Address,
    /// Nonce.
    pub nonce: u64,
    /// Gas price (for legacy) or max fee (for EIP-1559).
    pub gas_price: U256,
    /// Max priority fee (only for EIP-1559).
    pub max_priority_fee: Option<U256>,
    /// Gas limit.
    pub gas_limit: u64,
    /// Recipient.
    pub to: Option<Address>,
    /// Value.
    pub value: U256,
    /// Input data.
    pub data: Bytes,
    /// Chain ID.
    pub chain_id: u64,
    /// Signature V.
    pub v: u64,
    /// Signature R.
    pub r: U256,
    /// Signature S.
    pub s: U256,
    /// Access list (for EIP-2930+).
    pub access_list: AccessList,
}

impl SignedTransaction {
    /// Returns the effective gas price given a base fee.
    pub fn effective_gas_price(&self, base_fee: U256) -> U256 {
        match self.tx_type {
            TxType::Legacy | TxType::AccessList => self.gas_price,
            TxType::DynamicFee => {
                let priority_fee = self.max_priority_fee.unwrap_or(U256::ZERO);
                let max_fee = self.gas_price;
                // effective = min(max_fee, base_fee + priority_fee)
                let fee_with_priority = base_fee + priority_fee;
                if max_fee < fee_with_priority {
                    max_fee
                } else {
                    fee_with_priority
                }
            }
        }
    }

    /// Returns the maximum cost of this transaction.
    pub fn max_cost(&self) -> U256 {
        self.gas_price * U256::from(self.gas_limit) + self.value
    }

    /// Returns true if this is a contract creation.
    pub fn is_contract_creation(&self) -> bool {
        self.to.is_none()
    }

    /// Returns the RLP-encoded transaction.
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        self.encode_to(&mut out);
        out
    }

    /// RLP encodes the transaction.
    fn encode_to(&self, out: &mut Vec<u8>) {
        match self.tx_type {
            TxType::Legacy => {
                self.encode_legacy(out);
            }
            TxType::AccessList => {
                out.push(TxType::AccessList as u8);
                self.encode_access_list_tx(out);
            }
            TxType::DynamicFee => {
                out.push(TxType::DynamicFee as u8);
                self.encode_dynamic_fee_tx(out);
            }
        }
    }

    fn encode_legacy(&self, out: &mut Vec<u8>) {
        let mut payload = Vec::new();
        self.nonce.encode(&mut payload);
        self.gas_price.encode(&mut payload);
        self.gas_limit.encode(&mut payload);
        match &self.to {
            Some(addr) => addr.encode(&mut payload),
            None => payload.push(0x80),
        }
        self.value.encode(&mut payload);
        self.data.encode(&mut payload);
        self.v.encode(&mut payload);
        self.r.encode(&mut payload);
        self.s.encode(&mut payload);

        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(out);
        out.extend_from_slice(&payload);
    }

    fn encode_access_list_tx(&self, out: &mut Vec<u8>) {
        let mut payload = Vec::new();
        self.chain_id.encode(&mut payload);
        self.nonce.encode(&mut payload);
        self.gas_price.encode(&mut payload);
        self.gas_limit.encode(&mut payload);
        match &self.to {
            Some(addr) => addr.encode(&mut payload),
            None => payload.push(0x80),
        }
        self.value.encode(&mut payload);
        self.data.encode(&mut payload);
        encode_access_list(&self.access_list, &mut payload);
        // Signature with y_parity
        (self.v - self.chain_id * 2 - 35).encode(&mut payload);
        self.r.encode(&mut payload);
        self.s.encode(&mut payload);

        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(out);
        out.extend_from_slice(&payload);
    }

    fn encode_dynamic_fee_tx(&self, out: &mut Vec<u8>) {
        let mut payload = Vec::new();
        self.chain_id.encode(&mut payload);
        self.nonce.encode(&mut payload);
        self.max_priority_fee.unwrap_or(U256::ZERO).encode(&mut payload);
        self.gas_price.encode(&mut payload);
        self.gas_limit.encode(&mut payload);
        match &self.to {
            Some(addr) => addr.encode(&mut payload),
            None => payload.push(0x80),
        }
        self.value.encode(&mut payload);
        self.data.encode(&mut payload);
        encode_access_list(&self.access_list, &mut payload);
        // Signature with y_parity
        (self.v - self.chain_id * 2 - 35).encode(&mut payload);
        self.r.encode(&mut payload);
        self.s.encode(&mut payload);

        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(out);
        out.extend_from_slice(&payload);
    }
}

/// Transaction receipt.
#[derive(Debug, Clone)]
pub struct Receipt {
    /// Transaction type.
    pub tx_type: TxType,
    /// Transaction succeeded.
    pub success: bool,
    /// Cumulative gas used in the block.
    pub cumulative_gas_used: u64,
    /// Logs bloom filter.
    pub logs_bloom: [u8; 256],
    /// Transaction logs.
    pub logs: Vec<Log>,
    /// Gas used by this transaction.
    pub gas_used: u64,
    /// Contract address created (if any).
    pub contract_address: Option<Address>,
}

impl Default for Receipt {
    fn default() -> Self {
        Self {
            tx_type: TxType::Legacy,
            success: false,
            cumulative_gas_used: 0,
            logs_bloom: [0u8; 256],
            logs: Vec::new(),
            gas_used: 0,
            contract_address: None,
        }
    }
}

impl Default for TxType {
    fn default() -> Self {
        TxType::Legacy
    }
}

impl Receipt {
    /// Returns the transaction hash.
    pub fn hash(&self) -> B256 {
        let encoded = self.encode();
        keccak256(&encoded)
    }

    /// RLP encodes the receipt.
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();

        // Status (1 = success, 0 = failure)
        let status: u8 = if self.success { 1 } else { 0 };

        let mut payload = Vec::new();
        status.encode(&mut payload);
        self.cumulative_gas_used.encode(&mut payload);
        payload.extend_from_slice(&self.logs_bloom);
        encode_logs(&self.logs, &mut payload);

        match self.tx_type {
            TxType::Legacy => {}
            TxType::AccessList => out.push(TxType::AccessList as u8),
            TxType::DynamicFee => out.push(TxType::DynamicFee as u8),
        }

        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(&mut out);
        out.extend_from_slice(&payload);
        out
    }
}

/// Transaction log entry.
#[derive(Debug, Clone, Default)]
pub struct Log {
    /// Contract address.
    pub address: Address,
    /// Log topics.
    pub topics: Vec<B256>,
    /// Log data.
    pub data: Bytes,
}

impl Log {
    /// RLP encodes the log.
    pub fn encode(&self, out: &mut Vec<u8>) {
        let mut payload = Vec::new();
        self.address.encode(&mut payload);

        // Encode topics
        let mut topics_payload = Vec::new();
        for topic in &self.topics {
            topic.encode(&mut topics_payload);
        }
        let topics_header = alloy_rlp::Header {
            list: true,
            payload_length: topics_payload.len(),
        };
        topics_header.encode(&mut payload);
        payload.extend_from_slice(&topics_payload);

        self.data.encode(&mut payload);

        let header = alloy_rlp::Header {
            list: true,
            payload_length: payload.len(),
        };
        header.encode(out);
        out.extend_from_slice(&payload);
    }
}

// Helper functions

fn encode_access_list(list: &AccessList, out: &mut Vec<u8>) {
    let mut payload = Vec::new();
    for item in list {
        let mut item_payload = Vec::new();
        item.address.encode(&mut item_payload);

        // Encode storage keys
        let mut keys_payload = Vec::new();
        for key in &item.storage_keys {
            key.encode(&mut keys_payload);
        }
        let keys_header = alloy_rlp::Header {
            list: true,
            payload_length: keys_payload.len(),
        };
        keys_header.encode(&mut item_payload);
        item_payload.extend_from_slice(&keys_payload);

        let item_header = alloy_rlp::Header {
            list: true,
            payload_length: item_payload.len(),
        };
        item_header.encode(&mut payload);
        payload.extend_from_slice(&item_payload);
    }

    let header = alloy_rlp::Header {
        list: true,
        payload_length: payload.len(),
    };
    header.encode(out);
    out.extend_from_slice(&payload);
}

fn encode_logs(logs: &[Log], out: &mut Vec<u8>) {
    let mut payload = Vec::new();
    for log in logs {
        log.encode(&mut payload);
    }
    let header = alloy_rlp::Header {
        list: true,
        payload_length: payload.len(),
    };
    header.encode(out);
    out.extend_from_slice(&payload);
}

fn keccak256(data: &[u8]) -> B256 {
    let mut hasher = Keccak256::new();
    hasher.update(data);
    B256::from_slice(&hasher.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tx_type() {
        assert_eq!(TxType::from_byte(0), Some(TxType::Legacy));
        assert_eq!(TxType::from_byte(1), Some(TxType::AccessList));
        assert_eq!(TxType::from_byte(2), Some(TxType::DynamicFee));
        assert_eq!(TxType::from_byte(3), None);
    }

    #[test]
    fn test_legacy_tx_signing() {
        let tx = LegacyTx {
            nonce: 0,
            gas_price: U256::from(20_000_000_000u64),
            gas_limit: 21000,
            to: Some(Address::from([0x11; 20])),
            value: U256::from(1_000_000_000_000_000_000u64), // 1 ETH
            data: Bytes::new(),
            chain_id: Some(43114), // Avalanche C-Chain
        };

        let mut encoded = Vec::new();
        tx.encode_for_signing(&mut encoded);
        assert!(!encoded.is_empty());
    }

    #[test]
    fn test_dynamic_fee_tx_signing() {
        let tx = DynamicFeeTx {
            chain_id: 43114,
            nonce: 5,
            max_priority_fee_per_gas: U256::from(2_000_000_000u64),
            max_fee_per_gas: U256::from(100_000_000_000u64),
            gas_limit: 21000,
            to: Some(Address::from([0x22; 20])),
            value: U256::from(500_000_000_000_000_000u64),
            data: Bytes::new(),
            access_list: vec![],
        };

        let mut encoded = Vec::new();
        tx.encode_for_signing(&mut encoded);
        assert!(!encoded.is_empty());
        assert_eq!(encoded[0], TxType::DynamicFee as u8);
    }

    #[test]
    fn test_effective_gas_price() {
        let tx = SignedTransaction {
            tx_type: TxType::DynamicFee,
            hash: B256::ZERO,
            from: Address::ZERO,
            nonce: 0,
            gas_price: U256::from(100_000_000_000u64), // max fee
            max_priority_fee: Some(U256::from(2_000_000_000u64)),
            gas_limit: 21000,
            to: Some(Address::ZERO),
            value: U256::ZERO,
            data: Bytes::new(),
            chain_id: 43114,
            v: 0,
            r: U256::ZERO,
            s: U256::ZERO,
            access_list: vec![],
        };

        let base_fee = U256::from(25_000_000_000u64);
        let effective = tx.effective_gas_price(base_fee);
        assert_eq!(effective, U256::from(27_000_000_000u64)); // base + priority
    }

    #[test]
    fn test_receipt_encoding() {
        let receipt = Receipt {
            tx_type: TxType::Legacy,
            success: true,
            cumulative_gas_used: 21000,
            logs_bloom: [0; 256],
            logs: vec![],
            gas_used: 21000,
            contract_address: None,
        };

        let encoded = receipt.encode();
        assert!(!encoded.is_empty());
    }

    #[test]
    fn test_log_encoding() {
        let log = Log {
            address: Address::from([0x33; 20]),
            topics: vec![B256::ZERO],
            data: Bytes::from(vec![0x01, 0x02, 0x03]),
        };

        let mut encoded = Vec::new();
        log.encode(&mut encoded);
        assert!(!encoded.is_empty());
    }
}
