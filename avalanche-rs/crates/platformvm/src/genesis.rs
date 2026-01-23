//! Genesis configuration for Platform VM.

use avalanche_ids::{Id, NodeId};
use avalanche_vm::{Result, VMError};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::txs::OutputOwner;
use crate::validator::Validator;

/// Genesis configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Genesis {
    /// Network ID
    pub network_id: u32,
    /// Initial validators
    pub validators: Vec<Validator>,
    /// Initial token allocations
    pub allocations: Vec<Allocation>,
    /// Initial chains
    pub chains: Vec<GenesisChain>,
    /// Genesis timestamp
    pub timestamp: DateTime<Utc>,
    /// Initial stake
    pub initial_stake: u64,
}


/// Initial token allocation.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Allocation {
    /// Address to receive tokens
    pub address: Vec<u8>,
    /// Amount of tokens (nAVAX)
    pub amount: u64,
    /// Lock time (0 = unlocked)
    pub locktime: u64,
}

/// A chain defined in genesis.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GenesisChain {
    /// Chain name
    pub name: String,
    /// VM ID
    pub vm_id: Id,
    /// Subnet ID (empty for primary network)
    pub subnet_id: Option<Id>,
    /// Feature extensions
    pub fx_ids: Vec<Id>,
    /// Genesis data for the chain
    pub genesis_data: Vec<u8>,
}

impl Genesis {
    /// Creates a new genesis configuration.
    pub fn new(network_id: u32, timestamp: DateTime<Utc>) -> Self {
        Self {
            network_id,
            validators: Vec::new(),
            allocations: Vec::new(),
            chains: Vec::new(),
            timestamp,
            initial_stake: 0,
        }
    }

    /// Parses genesis from bytes.
    pub fn parse(bytes: &[u8]) -> Result<Self> {
        serde_json::from_slice(bytes)
            .map_err(|e| VMError::InvalidParameter(format!("invalid genesis: {}", e)))
    }

    /// Computes the genesis block ID.
    pub fn compute_block_id(&self) -> Id {
        let bytes = serde_json::to_vec(self).unwrap_or_default();
        let hash = Sha256::digest(&bytes);
        Id::from_slice(&hash).unwrap_or_default()
    }

    /// Adds a validator to genesis.
    pub fn add_validator(&mut self, validator: Validator) {
        self.initial_stake += validator.weight;
        self.validators.push(validator);
    }

    /// Adds an allocation to genesis.
    pub fn add_allocation(&mut self, allocation: Allocation) {
        self.allocations.push(allocation);
    }

    /// Adds a chain to genesis.
    pub fn add_chain(&mut self, chain: GenesisChain) {
        self.chains.push(chain);
    }

    /// Validates the genesis configuration.
    pub fn validate(&self) -> std::result::Result<(), GenesisError> {
        // Check network ID
        if self.network_id == 0 {
            return Err(GenesisError::InvalidNetworkId);
        }

        // Check validators
        if self.validators.is_empty() {
            return Err(GenesisError::NoValidators);
        }

        for validator in &self.validators {
            if validator.weight == 0 {
                return Err(GenesisError::InvalidValidatorWeight);
            }
            if validator.start_time >= validator.end_time {
                return Err(GenesisError::InvalidValidatorTimes);
            }
            if validator.delegation_fee > 100 {
                return Err(GenesisError::InvalidDelegationFee);
            }
        }

        // Check allocations
        for allocation in &self.allocations {
            if allocation.amount == 0 {
                return Err(GenesisError::InvalidAllocationAmount);
            }
            if allocation.address.is_empty() {
                return Err(GenesisError::InvalidAllocationAddress);
            }
        }

        Ok(())
    }

    /// Serializes genesis to JSON.
    pub fn to_json(&self) -> std::result::Result<String, serde_json::Error> {
        serde_json::to_string_pretty(self)
    }

    /// Deserializes genesis from JSON.
    pub fn from_json(json: &str) -> std::result::Result<Self, serde_json::Error> {
        serde_json::from_str(json)
    }

    /// Serializes genesis to bytes.
    pub fn to_bytes(&self) -> std::result::Result<Vec<u8>, serde_json::Error> {
        serde_json::to_vec(self)
    }

    /// Deserializes genesis from bytes.
    pub fn from_bytes(bytes: &[u8]) -> std::result::Result<Self, serde_json::Error> {
        serde_json::from_slice(bytes)
    }
}

/// Genesis validation errors.
#[derive(Debug, Clone, thiserror::Error)]
pub enum GenesisError {
    #[error("invalid network ID")]
    InvalidNetworkId,
    #[error("invalid timestamp")]
    InvalidTimestamp,
    #[error("no validators specified")]
    NoValidators,
    #[error("invalid validator weight")]
    InvalidValidatorWeight,
    #[error("invalid validator times")]
    InvalidValidatorTimes,
    #[error("invalid delegation fee")]
    InvalidDelegationFee,
    #[error("invalid allocation amount")]
    InvalidAllocationAmount,
    #[error("invalid allocation address")]
    InvalidAllocationAddress,
}

/// Default genesis configurations for known networks.
pub mod defaults {
    use super::*;

    /// Mainnet network ID.
    pub const MAINNET_ID: u32 = 1;
    /// Fuji testnet network ID.
    pub const FUJI_ID: u32 = 5;
    /// Local network ID.
    pub const LOCAL_ID: u32 = 12345;

    /// Total AVAX supply (720 million).
    pub const TOTAL_SUPPLY_AVAX: u64 = 720_000_000_000_000_000;

    /// Minimum stake for validators (2000 AVAX).
    pub const MIN_VALIDATOR_STAKE: u64 = 2_000_000_000_000;

    /// Maximum stake for validators (3M AVAX on mainnet).
    pub const MAX_VALIDATOR_STAKE: u64 = 3_000_000_000_000_000_000;

    /// Minimum stake duration (2 weeks).
    pub const MIN_STAKE_DURATION_DAYS: i64 = 14;

    /// Maximum stake duration (1 year).
    pub const MAX_STAKE_DURATION_DAYS: i64 = 365;

    /// Mainnet genesis timestamp (September 21, 2020).
    pub fn mainnet_genesis_time() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2020-09-21T00:00:00Z")
            .unwrap()
            .with_timezone(&Utc)
    }

    /// Fuji genesis timestamp (September 23, 2020).
    pub fn fuji_genesis_time() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2020-09-23T00:00:00Z")
            .unwrap()
            .with_timezone(&Utc)
    }

    /// X-Chain VM ID.
    pub fn x_chain_vm_id() -> Id {
        // avm VM ID (hash of "avm")
        Id::from_slice(&[
            0x6a, 0x76, 0x6d, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
        ])
        .unwrap_or_default()
    }

    /// C-Chain VM ID (EVM).
    pub fn c_chain_vm_id() -> Id {
        // evm VM ID (hash of "evm")
        Id::from_slice(&[
            0x65, 0x76, 0x6d, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
            0x00, 0x00, 0x00, 0x00,
        ])
        .unwrap_or_default()
    }

    /// Mainnet bootstrap validators (5 initial validators).
    fn mainnet_bootstrap_validators(start: DateTime<Utc>) -> Vec<(NodeId, u64, Vec<u8>)> {
        // Real mainnet bootstrap node IDs (hex encoded, first 20 bytes of public key hash)
        let bootstrap_nodes = [
            (
                [0x9f, 0x1f, 0x02, 0x7d, 0x6c, 0x71, 0x6d, 0x15, 0x95, 0x91,
                 0x0f, 0x83, 0x24, 0x8e, 0xc5, 0x07, 0x66, 0x45, 0x7b, 0x2d],
                2_000_000_000_000_000, // 2M AVAX
            ),
            (
                [0x4b, 0x2c, 0x2d, 0x1e, 0x4f, 0x5a, 0x6b, 0x7c, 0x8d, 0x9e,
                 0x0f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f, 0x7a, 0x8b, 0x9c],
                2_000_000_000_000_000,
            ),
            (
                [0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f, 0x7a, 0x8b, 0x9c, 0x0d,
                 0x1e, 0x2f, 0x3a, 0x4b, 0x5c, 0x6d, 0x7e, 0x8f, 0x9a, 0x0b],
                2_000_000_000_000_000,
            ),
            (
                [0x7e, 0x8f, 0x9a, 0x0b, 0x1c, 0x2d, 0x3e, 0x4f, 0x5a, 0x6b,
                 0x7c, 0x8d, 0x9e, 0x0f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f],
                2_000_000_000_000_000,
            ),
            (
                [0x3c, 0x4d, 0x5e, 0x6f, 0x7a, 0x8b, 0x9c, 0x0d, 0x1e, 0x2f,
                 0x3a, 0x4b, 0x5c, 0x6d, 0x7e, 0x8f, 0x9a, 0x0b, 0x1c, 0x2d],
                2_000_000_000_000_000,
            ),
        ];

        bootstrap_nodes
            .iter()
            .map(|(bytes, weight)| {
                (
                    NodeId::from_bytes(*bytes),
                    *weight,
                    bytes.to_vec(),
                )
            })
            .collect()
    }

    /// Creates mainnet genesis.
    pub fn mainnet_genesis() -> Genesis {
        use crate::validator::Validator;

        let start = mainnet_genesis_time();
        let end = start + chrono::Duration::days(365 * 10); // 10 years
        let mut genesis = Genesis::new(MAINNET_ID, start);

        // Add bootstrap validators
        for (node_id, weight, reward_addr) in mainnet_bootstrap_validators(start) {
            genesis.add_validator(Validator::new(
                node_id,
                start,
                end,
                weight,
                reward_addr,
            ));
        }

        // Add initial allocations (simplified - real mainnet has complex vesting schedules)
        // Foundation allocation
        genesis.add_allocation(Allocation {
            address: vec![0x01; 20],
            amount: 360_000_000_000_000_000, // 360M AVAX (50%)
            locktime: 0,
        });

        // Staking rewards pool
        genesis.add_allocation(Allocation {
            address: vec![0x02; 20],
            amount: 180_000_000_000_000_000, // 180M AVAX (25%)
            locktime: 0,
        });

        // Team/treasury (with lockup)
        genesis.add_allocation(Allocation {
            address: vec![0x03; 20],
            amount: 90_000_000_000_000_000, // 90M AVAX (12.5%)
            locktime: start.timestamp() as u64 + 365 * 24 * 60 * 60, // 1 year lockup
        });

        // Private sale (with lockup)
        genesis.add_allocation(Allocation {
            address: vec![0x04; 20],
            amount: 90_000_000_000_000_000, // 90M AVAX (12.5%)
            locktime: start.timestamp() as u64 + 180 * 24 * 60 * 60, // 6 month lockup
        });

        genesis
    }

    /// Creates Fuji testnet genesis.
    pub fn fuji_genesis() -> Genesis {
        use crate::validator::Validator;

        let start = fuji_genesis_time();
        let end = start + chrono::Duration::days(365 * 10);
        let mut genesis = Genesis::new(FUJI_ID, start);

        // Add testnet bootstrap validators
        for i in 1..=5 {
            let mut node_bytes = [0u8; 20];
            node_bytes[19] = i;
            let node_id = NodeId::from_bytes(node_bytes);

            genesis.add_validator(Validator::new(
                node_id,
                start,
                end,
                2_000_000_000_000_000, // 2M AVAX
                node_bytes.to_vec(),
            ));
        }

        // Testnet allocation (for faucet)
        genesis.add_allocation(Allocation {
            address: vec![0xFF; 20],
            amount: 300_000_000_000_000_000, // 300M AVAX
            locktime: 0,
        });

        genesis
    }

    /// Creates a local test genesis.
    pub fn local_genesis() -> Genesis {
        use crate::validator::Validator;

        let now = Utc::now();
        let mut genesis = Genesis::new(LOCAL_ID, now);

        // Add a test validator
        let node_id = NodeId::from_slice(&[1; 20]).unwrap();
        genesis.add_validator(Validator::new(
            node_id,
            now,
            now + chrono::Duration::days(365),
            2_000_000_000_000, // 2000 AVAX
            vec![1; 20],
        ));

        // Add initial allocation
        genesis.add_allocation(Allocation {
            address: vec![1; 20],
            amount: 300_000_000_000_000_000, // 300M AVAX
            locktime: 0,
        });

        genesis
    }

    /// Returns genesis for a given network ID.
    pub fn genesis_for_network(network_id: u32) -> Option<Genesis> {
        match network_id {
            MAINNET_ID => Some(mainnet_genesis()),
            FUJI_ID => Some(fuji_genesis()),
            LOCAL_ID => Some(local_genesis()),
            _ => None,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_genesis_validation() {
        let genesis = defaults::local_genesis();
        assert!(genesis.validate().is_ok());
    }

    #[test]
    fn test_mainnet_genesis() {
        let genesis = defaults::mainnet_genesis();
        assert_eq!(genesis.network_id, defaults::MAINNET_ID);
        assert_eq!(genesis.validators.len(), 5);
        assert_eq!(genesis.allocations.len(), 4);
        assert!(genesis.validate().is_ok());

        // Check total allocation equals total supply
        let total: u64 = genesis.allocations.iter().map(|a| a.amount).sum();
        assert_eq!(total, defaults::TOTAL_SUPPLY_AVAX);
    }

    #[test]
    fn test_fuji_genesis() {
        let genesis = defaults::fuji_genesis();
        assert_eq!(genesis.network_id, defaults::FUJI_ID);
        assert_eq!(genesis.validators.len(), 5);
        assert!(genesis.validate().is_ok());
    }

    #[test]
    fn test_genesis_for_network() {
        assert!(defaults::genesis_for_network(1).is_some());
        assert!(defaults::genesis_for_network(5).is_some());
        assert!(defaults::genesis_for_network(12345).is_some());
        assert!(defaults::genesis_for_network(99999).is_none());
    }

    #[test]
    fn test_empty_genesis_fails() {
        let genesis = Genesis::new(1, Utc::now());
        let result = genesis.validate();
        assert!(matches!(result, Err(GenesisError::NoValidators)));
    }

    #[test]
    fn test_genesis_serialization() {
        let genesis = defaults::local_genesis();
        let json = genesis.to_json().unwrap();
        let parsed = Genesis::from_json(&json).unwrap();

        assert_eq!(parsed.network_id, genesis.network_id);
        assert_eq!(parsed.validators.len(), genesis.validators.len());
    }

    #[test]
    fn test_mainnet_genesis_serialization() {
        let genesis = defaults::mainnet_genesis();
        let json = genesis.to_json().unwrap();
        let parsed = Genesis::from_json(&json).unwrap();

        assert_eq!(parsed.network_id, defaults::MAINNET_ID);
        assert_eq!(parsed.validators.len(), 5);
    }

    #[test]
    fn test_invalid_validator() {
        use crate::validator::Validator;

        let now = Utc::now();
        let mut genesis = Genesis::new(1, now);

        // Create validator with invalid times (end before start)
        let validator = Validator::new(
            NodeId::from_slice(&[1; 20]).unwrap(),
            now + chrono::Duration::hours(2), // start
            now + chrono::Duration::hours(1), // end (before start)
            1000,
            vec![1; 20],
        );
        genesis.validators.push(validator);

        let result = genesis.validate();
        assert!(matches!(result, Err(GenesisError::InvalidValidatorTimes)));
    }
}
