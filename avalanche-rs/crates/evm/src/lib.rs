//! C-Chain (EVM) implementation for Avalanche.
//!
//! This module provides a full EVM implementation using revm, enabling:
//! - Smart contract deployment and execution
//! - Ethereum-compatible transactions
//! - State management with Merkle Patricia Trie
//! - Gas metering and fee handling
//!
//! # Architecture
//!
//! The C-Chain is Avalanche's contract chain, fully compatible with the
//! Ethereum Virtual Machine. It uses Snowman consensus for block finality.

pub mod state;
pub mod transaction;
pub mod executor;
pub mod block;
pub mod vm;
pub mod precompiles;

pub use state::{EvmState, Account, AccountInfo, Storage, StateSnapshot};
pub use transaction::{
    AccessList, AccessListItem, DynamicFeeTx, LegacyTx, Log, Receipt,
    SignedTransaction, TxType,
};
pub use executor::{BlockContext, ExecutionError, ExecutionOutcome, Executor, ExecutorConfig};
pub use block::{Block, BlockBuilder, Header, Withdrawal};
pub use vm::{EvmVM, GenesisAccount, GenesisConfig, VMConfig, VMError, VMState};
pub use precompiles::{Precompiles, PrecompileResult, PrecompileOutput, PrecompileError, addresses as precompile_addresses};

use alloy_primitives::Address;

/// Chain ID for Avalanche C-Chain mainnet.
pub const MAINNET_CHAIN_ID: u64 = 43114;

/// Chain ID for Fuji testnet C-Chain.
pub const FUJI_CHAIN_ID: u64 = 43113;

/// Chain ID for local testing.
pub const LOCAL_CHAIN_ID: u64 = 43112;

/// Gas limit for blocks.
pub const BLOCK_GAS_LIMIT: u64 = 8_000_000;

/// Minimum gas price (25 nAVAX).
pub const MIN_GAS_PRICE: u64 = 25_000_000_000;

/// Converts a 20-byte slice to an Address.
pub fn bytes_to_address(bytes: &[u8]) -> Address {
    let mut addr = [0u8; 20];
    addr.copy_from_slice(&bytes[..20.min(bytes.len())]);
    Address::from(addr)
}

/// Converts an Address to a 20-byte array.
pub fn address_to_bytes(addr: &Address) -> [u8; 20] {
    addr.0.0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_address_conversion() {
        let bytes = [1u8; 20];
        let addr = bytes_to_address(&bytes);
        let back = address_to_bytes(&addr);
        assert_eq!(bytes, back);
    }
}
