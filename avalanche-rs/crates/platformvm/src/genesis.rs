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

    /// X-Chain VM ID.
    pub fn x_chain_vm_id() -> Id {
        // avm
        Id::default()
    }

    /// C-Chain VM ID (EVM).
    pub fn c_chain_vm_id() -> Id {
        // evm
        Id::default()
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
