//! Subnet creation and management.
//!
//! This module provides functionality for creating and managing Avalanche subnets,
//! including subnet validators, permissions, and cross-subnet communication.

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use chrono::{DateTime, Utc};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use thiserror::Error;

use avalanche_ids::{Id, NodeId};

use crate::validator::Validator;

/// Errors that can occur during subnet operations.
#[derive(Debug, Error, Clone)]
pub enum SubnetError {
    #[error("subnet not found: {0}")]
    NotFound(Id),

    #[error("subnet already exists: {0}")]
    AlreadyExists(Id),

    #[error("invalid subnet configuration: {0}")]
    InvalidConfig(String),

    #[error("unauthorized: {0}")]
    Unauthorized(String),

    #[error("validator already exists in subnet: {0}")]
    ValidatorExists(NodeId),

    #[error("validator not found in subnet: {0}")]
    ValidatorNotFound(NodeId),

    #[error("chain not found: {0}")]
    ChainNotFound(Id),

    #[error("chain already exists: {0}")]
    ChainExists(Id),

    #[error("insufficient stake: need {need}, have {have}")]
    InsufficientStake { need: u64, have: u64 },
}

/// Result type for subnet operations.
pub type Result<T> = std::result::Result<T, SubnetError>;

/// Subnet type classification.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum SubnetType {
    /// Primary network (P, X, C chains).
    Primary,
    /// Permissioned subnet with owner control.
    Permissioned,
    /// Elastic subnet with dynamic validator set.
    Elastic,
}

/// Subnet configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubnetConfig {
    /// Subnet ID.
    pub id: Id,
    /// Subnet type.
    pub subnet_type: SubnetType,
    /// Owner addresses (control keys).
    pub owners: Vec<Vec<u8>>,
    /// Threshold for owner operations (M-of-N).
    pub threshold: u32,
    /// Minimum validator stake.
    pub min_validator_stake: u64,
    /// Maximum validator stake.
    pub max_validator_stake: u64,
    /// Minimum delegation stake.
    pub min_delegation_stake: u64,
    /// Minimum stake duration.
    pub min_stake_duration: chrono::Duration,
    /// Maximum stake duration.
    pub max_stake_duration: chrono::Duration,
    /// Delegation fee (basis points, 100 = 1%).
    pub delegation_fee_bps: u32,
    /// Whether the subnet is active.
    pub active: bool,
    /// Creation timestamp.
    pub created_at: DateTime<Utc>,
}

impl SubnetConfig {
    /// Creates a new subnet configuration.
    pub fn new(id: Id, owners: Vec<Vec<u8>>, threshold: u32) -> Self {
        Self {
            id,
            subnet_type: SubnetType::Permissioned,
            owners,
            threshold,
            min_validator_stake: 2_000_000_000_000, // 2000 AVAX
            max_validator_stake: 3_000_000_000_000_000_000, // 3M AVAX
            min_delegation_stake: 25_000_000_000, // 25 AVAX
            min_stake_duration: chrono::Duration::days(14),
            max_stake_duration: chrono::Duration::days(365),
            delegation_fee_bps: 200, // 2%
            active: true,
            created_at: Utc::now(),
        }
    }

    /// Creates a primary network subnet config.
    pub fn primary_network() -> Self {
        Self {
            id: Id::default(),
            subnet_type: SubnetType::Primary,
            owners: Vec::new(),
            threshold: 0,
            min_validator_stake: 2_000_000_000_000,
            max_validator_stake: 3_000_000_000_000_000_000,
            min_delegation_stake: 25_000_000_000,
            min_stake_duration: chrono::Duration::days(14),
            max_stake_duration: chrono::Duration::days(365),
            delegation_fee_bps: 200,
            active: true,
            created_at: Utc::now(),
        }
    }

    /// Creates an elastic subnet config.
    pub fn elastic(id: Id) -> Self {
        let mut config = Self::new(id, Vec::new(), 0);
        config.subnet_type = SubnetType::Elastic;
        config
    }

    /// Validates the subnet configuration.
    pub fn validate(&self) -> Result<()> {
        if self.subnet_type != SubnetType::Primary {
            if self.threshold == 0 || self.threshold as usize > self.owners.len() {
                return Err(SubnetError::InvalidConfig(
                    "invalid threshold for owner count".into(),
                ));
            }

            if self.owners.is_empty() {
                return Err(SubnetError::InvalidConfig("no owners specified".into()));
            }
        }

        if self.min_validator_stake > self.max_validator_stake {
            return Err(SubnetError::InvalidConfig(
                "min stake > max stake".into(),
            ));
        }

        if self.min_stake_duration > self.max_stake_duration {
            return Err(SubnetError::InvalidConfig(
                "min duration > max duration".into(),
            ));
        }

        if self.delegation_fee_bps > 10000 {
            return Err(SubnetError::InvalidConfig(
                "delegation fee > 100%".into(),
            ));
        }

        Ok(())
    }

    /// Returns true if the address is an owner.
    pub fn is_owner(&self, address: &[u8]) -> bool {
        self.owners.iter().any(|o| o == address)
    }
}

/// A blockchain running on a subnet.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubnetChain {
    /// Chain ID.
    pub chain_id: Id,
    /// Subnet ID this chain belongs to.
    pub subnet_id: Id,
    /// VM ID.
    pub vm_id: Id,
    /// Chain name.
    pub name: String,
    /// Genesis data.
    pub genesis: Vec<u8>,
    /// Whether the chain is active.
    pub active: bool,
    /// Creation timestamp.
    pub created_at: DateTime<Utc>,
}

impl SubnetChain {
    /// Creates a new subnet chain.
    pub fn new(
        chain_id: Id,
        subnet_id: Id,
        vm_id: Id,
        name: String,
        genesis: Vec<u8>,
    ) -> Self {
        Self {
            chain_id,
            subnet_id,
            vm_id,
            name,
            genesis,
            active: true,
            created_at: Utc::now(),
        }
    }
}

/// Subnet validator info.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubnetValidator {
    /// Node ID.
    pub node_id: NodeId,
    /// Subnet ID.
    pub subnet_id: Id,
    /// Stake weight.
    pub weight: u64,
    /// Start time.
    pub start_time: DateTime<Utc>,
    /// End time.
    pub end_time: DateTime<Utc>,
    /// Whether this is a primary network validator.
    pub is_primary: bool,
}

impl SubnetValidator {
    /// Creates a new subnet validator.
    pub fn new(
        node_id: NodeId,
        subnet_id: Id,
        weight: u64,
        start_time: DateTime<Utc>,
        end_time: DateTime<Utc>,
    ) -> Self {
        Self {
            node_id,
            subnet_id,
            weight,
            start_time,
            end_time,
            is_primary: subnet_id == Id::default(),
        }
    }

    /// Returns true if the validator is currently active.
    pub fn is_active(&self, now: DateTime<Utc>) -> bool {
        now >= self.start_time && now < self.end_time
    }

    /// Returns the remaining stake duration.
    pub fn remaining_duration(&self, now: DateTime<Utc>) -> chrono::Duration {
        if now >= self.end_time {
            chrono::Duration::zero()
        } else {
            self.end_time - now
        }
    }
}

/// Manager for subnets and their validators.
pub struct SubnetManager {
    /// Subnet configurations by ID.
    subnets: RwLock<HashMap<Id, SubnetConfig>>,
    /// Subnet validators by subnet ID.
    validators: RwLock<HashMap<Id, HashMap<NodeId, SubnetValidator>>>,
    /// Chains by chain ID.
    chains: RwLock<HashMap<Id, SubnetChain>>,
    /// Chains by subnet ID.
    chains_by_subnet: RwLock<HashMap<Id, HashSet<Id>>>,
}

impl SubnetManager {
    /// Creates a new subnet manager.
    pub fn new() -> Self {
        let mut manager = Self {
            subnets: RwLock::new(HashMap::new()),
            validators: RwLock::new(HashMap::new()),
            chains: RwLock::new(HashMap::new()),
            chains_by_subnet: RwLock::new(HashMap::new()),
        };

        // Add primary network
        let primary = SubnetConfig::primary_network();
        manager.subnets.write().insert(Id::default(), primary);
        manager.validators.write().insert(Id::default(), HashMap::new());
        manager.chains_by_subnet.write().insert(Id::default(), HashSet::new());

        manager
    }

    /// Creates a new subnet.
    pub fn create_subnet(&self, config: SubnetConfig) -> Result<Id> {
        config.validate()?;

        let mut subnets = self.subnets.write();
        if subnets.contains_key(&config.id) {
            return Err(SubnetError::AlreadyExists(config.id));
        }

        let subnet_id = config.id;
        subnets.insert(subnet_id, config);
        self.validators.write().insert(subnet_id, HashMap::new());
        self.chains_by_subnet.write().insert(subnet_id, HashSet::new());

        Ok(subnet_id)
    }

    /// Returns a subnet configuration.
    pub fn get_subnet(&self, subnet_id: Id) -> Option<SubnetConfig> {
        self.subnets.read().get(&subnet_id).cloned()
    }

    /// Returns all subnet IDs.
    pub fn subnet_ids(&self) -> Vec<Id> {
        self.subnets.read().keys().cloned().collect()
    }

    /// Adds a validator to a subnet.
    pub fn add_validator(&self, validator: SubnetValidator) -> Result<()> {
        let subnet_id = validator.subnet_id;

        // Check subnet exists
        let config = self.subnets.read().get(&subnet_id).cloned();
        let config = config.ok_or(SubnetError::NotFound(subnet_id))?;

        // Validate stake
        if validator.weight < config.min_validator_stake {
            return Err(SubnetError::InsufficientStake {
                need: config.min_validator_stake,
                have: validator.weight,
            });
        }

        if validator.weight > config.max_validator_stake {
            return Err(SubnetError::InvalidConfig(
                "stake exceeds maximum".into(),
            ));
        }

        // For non-primary subnets, validator must be in primary network
        if subnet_id != Id::default() {
            let primary_validators = self.validators.read();
            let primary = primary_validators.get(&Id::default());
            if !primary.map(|v| v.contains_key(&validator.node_id)).unwrap_or(false) {
                return Err(SubnetError::Unauthorized(
                    "must be primary network validator".into(),
                ));
            }
        }

        let mut validators = self.validators.write();
        let subnet_validators = validators.entry(subnet_id).or_insert_with(HashMap::new);

        if subnet_validators.contains_key(&validator.node_id) {
            return Err(SubnetError::ValidatorExists(validator.node_id));
        }

        subnet_validators.insert(validator.node_id, validator);
        Ok(())
    }

    /// Removes a validator from a subnet.
    pub fn remove_validator(&self, subnet_id: Id, node_id: NodeId) -> Result<SubnetValidator> {
        let mut validators = self.validators.write();
        let subnet_validators = validators
            .get_mut(&subnet_id)
            .ok_or(SubnetError::NotFound(subnet_id))?;

        subnet_validators
            .remove(&node_id)
            .ok_or(SubnetError::ValidatorNotFound(node_id))
    }

    /// Returns validators for a subnet.
    pub fn get_validators(&self, subnet_id: Id) -> Result<Vec<SubnetValidator>> {
        let validators = self.validators.read();
        let subnet_validators = validators
            .get(&subnet_id)
            .ok_or(SubnetError::NotFound(subnet_id))?;

        Ok(subnet_validators.values().cloned().collect())
    }

    /// Returns active validators for a subnet.
    pub fn get_active_validators(&self, subnet_id: Id, now: DateTime<Utc>) -> Result<Vec<SubnetValidator>> {
        let validators = self.get_validators(subnet_id)?;
        Ok(validators.into_iter().filter(|v| v.is_active(now)).collect())
    }

    /// Returns the total stake for a subnet.
    pub fn total_stake(&self, subnet_id: Id, now: DateTime<Utc>) -> Result<u64> {
        let validators = self.get_active_validators(subnet_id, now)?;
        Ok(validators.iter().map(|v| v.weight).sum())
    }

    /// Checks if a node is a validator for a subnet.
    pub fn is_validator(&self, subnet_id: Id, node_id: NodeId) -> bool {
        self.validators
            .read()
            .get(&subnet_id)
            .map(|v| v.contains_key(&node_id))
            .unwrap_or(false)
    }

    /// Creates a new chain on a subnet.
    pub fn create_chain(&self, chain: SubnetChain) -> Result<Id> {
        // Check subnet exists
        if !self.subnets.read().contains_key(&chain.subnet_id) {
            return Err(SubnetError::NotFound(chain.subnet_id));
        }

        let chain_id = chain.chain_id;
        let subnet_id = chain.subnet_id;

        let mut chains = self.chains.write();
        if chains.contains_key(&chain_id) {
            return Err(SubnetError::ChainExists(chain_id));
        }

        chains.insert(chain_id, chain);
        self.chains_by_subnet
            .write()
            .entry(subnet_id)
            .or_insert_with(HashSet::new)
            .insert(chain_id);

        Ok(chain_id)
    }

    /// Returns a chain.
    pub fn get_chain(&self, chain_id: Id) -> Option<SubnetChain> {
        self.chains.read().get(&chain_id).cloned()
    }

    /// Returns chains for a subnet.
    pub fn get_subnet_chains(&self, subnet_id: Id) -> Vec<SubnetChain> {
        let chain_ids = self
            .chains_by_subnet
            .read()
            .get(&subnet_id)
            .cloned()
            .unwrap_or_default();

        let chains = self.chains.read();
        chain_ids
            .iter()
            .filter_map(|id| chains.get(id).cloned())
            .collect()
    }

    /// Validates that a set of signatures meets the subnet threshold.
    pub fn validate_owner_signatures(
        &self,
        subnet_id: Id,
        signers: &[Vec<u8>],
    ) -> Result<bool> {
        let config = self
            .subnets
            .read()
            .get(&subnet_id)
            .cloned()
            .ok_or(SubnetError::NotFound(subnet_id))?;

        let valid_signers: usize = signers
            .iter()
            .filter(|s| config.is_owner(s))
            .count();

        Ok(valid_signers >= config.threshold as usize)
    }

    /// Returns subnet statistics.
    pub fn stats(&self, subnet_id: Id) -> Option<SubnetStats> {
        let config = self.subnets.read().get(&subnet_id).cloned()?;
        let validators = self.validators.read();
        let subnet_validators = validators.get(&subnet_id)?;
        let chains = self.chains_by_subnet.read().get(&subnet_id).cloned().unwrap_or_default();

        let now = Utc::now();
        let active_validators: Vec<_> = subnet_validators
            .values()
            .filter(|v| v.is_active(now))
            .collect();

        Some(SubnetStats {
            subnet_id,
            subnet_type: config.subnet_type,
            total_validators: subnet_validators.len(),
            active_validators: active_validators.len(),
            total_stake: active_validators.iter().map(|v| v.weight).sum(),
            chain_count: chains.len(),
            is_active: config.active,
        })
    }
}

impl Default for SubnetManager {
    fn default() -> Self {
        Self::new()
    }
}

/// Subnet statistics.
#[derive(Debug, Clone)]
pub struct SubnetStats {
    pub subnet_id: Id,
    pub subnet_type: SubnetType,
    pub total_validators: usize,
    pub active_validators: usize,
    pub total_stake: u64,
    pub chain_count: usize,
    pub is_active: bool,
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_manager() -> SubnetManager {
        SubnetManager::new()
    }

    #[test]
    fn test_primary_network_exists() {
        let manager = create_test_manager();
        let primary = manager.get_subnet(Id::default());
        assert!(primary.is_some());
        assert_eq!(primary.unwrap().subnet_type, SubnetType::Primary);
    }

    #[test]
    fn test_create_subnet() {
        let manager = create_test_manager();

        let config = SubnetConfig::new(
            Id::from_bytes([1; 32]),
            vec![vec![1; 20], vec![2; 20]],
            2,
        );

        let result = manager.create_subnet(config.clone());
        assert!(result.is_ok());

        let subnet = manager.get_subnet(config.id);
        assert!(subnet.is_some());
        assert_eq!(subnet.unwrap().threshold, 2);
    }

    #[test]
    fn test_duplicate_subnet_fails() {
        let manager = create_test_manager();

        let config = SubnetConfig::new(
            Id::from_bytes([1; 32]),
            vec![vec![1; 20]],
            1,
        );

        manager.create_subnet(config.clone()).unwrap();
        let result = manager.create_subnet(config);
        assert!(matches!(result, Err(SubnetError::AlreadyExists(_))));
    }

    #[test]
    fn test_add_validator_to_primary() {
        let manager = create_test_manager();

        let validator = SubnetValidator::new(
            NodeId::from_bytes([1; 20]),
            Id::default(),
            2_500_000_000_000, // 2500 AVAX
            Utc::now(),
            Utc::now() + chrono::Duration::days(30),
        );

        let result = manager.add_validator(validator);
        assert!(result.is_ok());

        assert!(manager.is_validator(Id::default(), NodeId::from_bytes([1; 20])));
    }

    #[test]
    fn test_insufficient_stake() {
        let manager = create_test_manager();

        let validator = SubnetValidator::new(
            NodeId::from_bytes([1; 20]),
            Id::default(),
            1_000_000_000, // 1 AVAX (too low)
            Utc::now(),
            Utc::now() + chrono::Duration::days(30),
        );

        let result = manager.add_validator(validator);
        assert!(matches!(result, Err(SubnetError::InsufficientStake { .. })));
    }

    #[test]
    fn test_subnet_validator_requires_primary() {
        let manager = create_test_manager();

        // Create a subnet
        let subnet_id = Id::from_bytes([1; 32]);
        let config = SubnetConfig::new(subnet_id, vec![vec![1; 20]], 1);
        manager.create_subnet(config).unwrap();

        // Try to add validator to subnet without being in primary
        let validator = SubnetValidator::new(
            NodeId::from_bytes([1; 20]),
            subnet_id,
            2_500_000_000_000,
            Utc::now(),
            Utc::now() + chrono::Duration::days(30),
        );

        let result = manager.add_validator(validator);
        assert!(matches!(result, Err(SubnetError::Unauthorized(_))));
    }

    #[test]
    fn test_create_chain() {
        let manager = create_test_manager();

        let chain = SubnetChain::new(
            Id::from_bytes([1; 32]),
            Id::default(), // Primary network
            Id::from_bytes([2; 32]), // VM ID
            "TestChain".to_string(),
            vec![1, 2, 3],
        );

        let result = manager.create_chain(chain.clone());
        assert!(result.is_ok());

        let retrieved = manager.get_chain(chain.chain_id);
        assert!(retrieved.is_some());
        assert_eq!(retrieved.unwrap().name, "TestChain");
    }

    #[test]
    fn test_subnet_stats() {
        let manager = create_test_manager();

        // Add validators to primary
        for i in 1..=5 {
            let validator = SubnetValidator::new(
                NodeId::from_bytes([i; 20]),
                Id::default(),
                2_500_000_000_000,
                Utc::now(),
                Utc::now() + chrono::Duration::days(30),
            );
            manager.add_validator(validator).unwrap();
        }

        let stats = manager.stats(Id::default()).unwrap();
        assert_eq!(stats.total_validators, 5);
        assert_eq!(stats.active_validators, 5);
        assert_eq!(stats.total_stake, 5 * 2_500_000_000_000);
    }

    #[test]
    fn test_owner_signatures() {
        let manager = create_test_manager();

        let config = SubnetConfig::new(
            Id::from_bytes([1; 32]),
            vec![vec![1; 20], vec![2; 20], vec![3; 20]],
            2, // 2-of-3
        );
        manager.create_subnet(config.clone()).unwrap();

        // 2 valid signers should pass
        let signers = vec![vec![1; 20], vec![2; 20]];
        assert!(manager.validate_owner_signatures(config.id, &signers).unwrap());

        // 1 valid signer should fail
        let signers = vec![vec![1; 20]];
        assert!(!manager.validate_owner_signatures(config.id, &signers).unwrap());
    }

    #[test]
    fn test_validator_is_active() {
        let now = Utc::now();
        let validator = SubnetValidator::new(
            NodeId::from_bytes([1; 20]),
            Id::default(),
            2_500_000_000_000,
            now - chrono::Duration::hours(1),
            now + chrono::Duration::hours(1),
        );

        assert!(validator.is_active(now));
        assert!(!validator.is_active(now + chrono::Duration::hours(2)));
        assert!(!validator.is_active(now - chrono::Duration::hours(2)));
    }
}
