//! Platform state management.

use std::collections::HashMap;
use std::sync::Arc;

use avalanche_db::Database;
use avalanche_ids::{Id, NodeId};
use avalanche_vm::{Result, VMError};
use chrono::{DateTime, Utc};
use parking_lot::RwLock;

use crate::block::PlatformBlock;
use crate::genesis::Genesis;
use crate::validator::Validator;

/// Keys for platform state.
pub mod keys {
    pub const CURRENT_VALIDATORS: &[u8] = b"currentValidators";
    pub const PENDING_VALIDATORS: &[u8] = b"pendingValidators";
    pub const DELEGATORS: &[u8] = b"delegators";
    pub const SUBNETS: &[u8] = b"subnets";
    pub const CHAINS: &[u8] = b"chains";
    pub const UTXOS: &[u8] = b"utxos";
    pub const BLOCKS: &[u8] = b"blocks";
    pub const LAST_ACCEPTED: &[u8] = b"lastAccepted";
    pub const GENESIS_BLOCK: &[u8] = b"genesisBlock";
}

/// Subnet information.
#[derive(Debug, Clone)]
pub struct Subnet {
    /// Subnet ID
    pub id: Id,
    /// Owner addresses
    pub owner: Vec<Vec<u8>>,
    /// Control keys required
    pub threshold: u32,
}

/// Chain information.
#[derive(Debug, Clone)]
pub struct Chain {
    /// Chain ID
    pub id: Id,
    /// Subnet this chain belongs to
    pub subnet_id: Id,
    /// VM ID
    pub vm_id: Id,
    /// Genesis data
    pub genesis_data: Vec<u8>,
    /// Chain name
    pub name: String,
}

/// Platform state manager.
pub struct PlatformState {
    /// Database
    db: Arc<dyn Database>,
    /// Current validator set
    current_validators: RwLock<HashMap<NodeId, Validator>>,
    /// Pending validators (to be added)
    pending_validators: RwLock<HashMap<NodeId, Validator>>,
    /// Subnets
    subnets: RwLock<HashMap<Id, Subnet>>,
    /// Chains
    chains: RwLock<HashMap<Id, Chain>>,
    /// Genesis block ID
    genesis_block_id: Id,
    /// Current timestamp
    timestamp: RwLock<DateTime<Utc>>,
    /// Total staked amount
    total_stake: RwLock<u64>,
}

impl std::fmt::Debug for PlatformState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PlatformState")
            .field("genesis_block_id", &self.genesis_block_id)
            .field("total_stake", &*self.total_stake.read())
            .finish_non_exhaustive()
    }
}

impl PlatformState {
    /// Creates a new platform state from genesis.
    pub fn new(db: Arc<dyn Database>, genesis: &Genesis) -> Result<Self> {
        let genesis_block_id = genesis.compute_block_id();

        let mut current_validators = HashMap::new();
        for v in &genesis.validators {
            current_validators.insert(v.node_id, v.clone());
        }

        let state = Self {
            db,
            current_validators: RwLock::new(current_validators),
            pending_validators: RwLock::new(HashMap::new()),
            subnets: RwLock::new(HashMap::new()),
            chains: RwLock::new(HashMap::new()),
            genesis_block_id,
            timestamp: RwLock::new(genesis.timestamp),
            total_stake: RwLock::new(genesis.initial_stake),
        };

        // Store genesis block ID
        state.db.put(keys::GENESIS_BLOCK, genesis_block_id.as_ref())?;
        state.db.put(keys::LAST_ACCEPTED, genesis_block_id.as_ref())?;

        Ok(state)
    }

    /// Returns the genesis block ID.
    pub fn genesis_block_id(&self) -> Id {
        self.genesis_block_id
    }

    /// Returns current validators.
    pub fn current_validators(&self) -> Vec<Validator> {
        self.current_validators.read().values().cloned().collect()
    }

    /// Returns pending validators.
    pub fn pending_validators(&self) -> Vec<Validator> {
        self.pending_validators.read().values().cloned().collect()
    }

    /// Gets a validator by node ID.
    pub fn get_validator(&self, node_id: &NodeId) -> Option<Validator> {
        self.current_validators.read().get(node_id).cloned()
    }

    /// Adds a pending validator.
    pub fn add_pending_validator(&self, validator: Validator) -> Result<()> {
        let node_id = validator.node_id;

        // Check not already a validator
        if self.current_validators.read().contains_key(&node_id) {
            return Err(VMError::InvalidParameter(
                "already a current validator".to_string(),
            ));
        }

        self.pending_validators.write().insert(node_id, validator);
        Ok(())
    }

    /// Promotes pending validators to current.
    pub fn advance_timestamp(&self, new_time: DateTime<Utc>) -> Result<Vec<Validator>> {
        let mut promoted = Vec::new();
        let mut pending = self.pending_validators.write();
        let mut current = self.current_validators.write();

        // Find validators whose start time has passed
        let to_promote: Vec<NodeId> = pending
            .iter()
            .filter(|(_, v)| v.start_time <= new_time)
            .map(|(id, _)| *id)
            .collect();

        for node_id in to_promote {
            if let Some(validator) = pending.remove(&node_id) {
                promoted.push(validator.clone());
                current.insert(node_id, validator);
            }
        }

        // Remove expired validators
        let expired: Vec<NodeId> = current
            .iter()
            .filter(|(_, v)| v.end_time <= new_time)
            .map(|(id, _)| *id)
            .collect();

        for node_id in expired {
            current.remove(&node_id);
        }

        *self.timestamp.write() = new_time;
        Ok(promoted)
    }

    /// Returns current timestamp.
    pub fn timestamp(&self) -> DateTime<Utc> {
        *self.timestamp.read()
    }

    /// Returns total staked amount.
    pub fn total_stake(&self) -> u64 {
        *self.total_stake.read()
    }

    /// Adds stake amount.
    pub fn add_stake(&self, amount: u64) {
        *self.total_stake.write() += amount;
    }

    /// Removes stake amount.
    pub fn remove_stake(&self, amount: u64) {
        let mut stake = self.total_stake.write();
        *stake = stake.saturating_sub(amount);
    }

    /// Gets a subnet by ID.
    pub fn get_subnet(&self, id: &Id) -> Option<Subnet> {
        self.subnets.read().get(id).cloned()
    }

    /// Adds a subnet.
    pub fn add_subnet(&self, subnet: Subnet) -> Result<()> {
        self.subnets.write().insert(subnet.id, subnet);
        Ok(())
    }

    /// Gets a chain by ID.
    pub fn get_chain(&self, id: &Id) -> Option<Chain> {
        self.chains.read().get(id).cloned()
    }

    /// Adds a chain.
    pub fn add_chain(&self, chain: Chain) -> Result<()> {
        self.chains.write().insert(chain.id, chain);
        Ok(())
    }

    /// Builds a new block.
    pub fn build_block(&self, parent_id: Id) -> Result<PlatformBlock> {
        let timestamp = *self.timestamp.read();
        let height = 1; // Would compute from parent

        PlatformBlock::new_standard(parent_id, height, timestamp, vec![])
    }

    /// Gets a block by ID.
    pub fn get_block(&self, id: &Id) -> Result<Option<PlatformBlock>> {
        let key = [keys::BLOCKS, id.as_ref()].concat();
        match self.db.get(&key)? {
            Some(bytes) => {
                let block = PlatformBlock::parse(&bytes)?;
                Ok(Some(block))
            }
            None => Ok(None),
        }
    }

    /// Checks if a block exists.
    pub fn has_block(&self, id: &Id) -> Result<bool> {
        let key = [keys::BLOCKS, id.as_ref()].concat();
        Ok(self.db.has(&key)?)
    }

    /// Stores a block by ID and bytes.
    pub fn put_block(&self, id: &Id, bytes: &[u8]) -> Result<()> {
        let key = [keys::BLOCKS, id.as_ref()].concat();
        self.db.put(&key, bytes)?;
        Ok(())
    }

    /// Applies a transaction to state.
    pub fn apply_transaction(&self, tx: &crate::txs::Transaction) -> Result<()> {
        use crate::txs::Transaction;

        match tx {
            Transaction::AddValidator(add_tx) => {
                // Create a validator from the transaction
                let validator = Validator::new(
                    add_tx.validator.node_id,
                    DateTime::from_timestamp(add_tx.validator.start_time as i64, 0)
                        .unwrap_or_else(Utc::now),
                    DateTime::from_timestamp(add_tx.validator.end_time as i64, 0)
                        .unwrap_or_else(Utc::now),
                    add_tx.validator.weight,
                    vec![],
                );
                self.add_pending_validator(validator)?;
            }
            Transaction::CreateSubnet(subnet_tx) => {
                let subnet_id = subnet_tx.blockchain_id; // Simplified
                let subnet = Subnet {
                    id: subnet_id,
                    owner: subnet_tx.owner.addresses.clone(),
                    threshold: subnet_tx.owner.threshold,
                };
                self.add_subnet(subnet)?;
            }
            Transaction::CreateChain(chain_tx) => {
                let chain = Chain {
                    id: chain_tx.vm_id, // Simplified
                    subnet_id: chain_tx.subnet_id,
                    vm_id: chain_tx.vm_id,
                    genesis_data: chain_tx.genesis_data.clone(),
                    name: chain_tx.chain_name.clone(),
                };
                self.add_chain(chain)?;
            }
            _ => {
                // Other transaction types not yet implemented
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_db::MemDb;

    fn make_genesis() -> Genesis {
        Genesis {
            network_id: 1,
            timestamp: Utc::now(),
            initial_stake: 1_000_000,
            validators: vec![],
            chains: vec![],
            allocations: vec![],
        }
    }

    #[test]
    fn test_platform_state_new() {
        let db = Arc::new(MemDb::new());
        let genesis = make_genesis();
        let state = PlatformState::new(db, &genesis).unwrap();

        assert_eq!(state.total_stake(), 1_000_000);
        assert!(state.current_validators().is_empty());
    }

    #[test]
    fn test_stake_management() {
        let db = Arc::new(MemDb::new());
        let genesis = make_genesis();
        let state = PlatformState::new(db, &genesis).unwrap();

        state.add_stake(500_000);
        assert_eq!(state.total_stake(), 1_500_000);

        state.remove_stake(200_000);
        assert_eq!(state.total_stake(), 1_300_000);
    }

    #[test]
    fn test_subnet_management() {
        let db = Arc::new(MemDb::new());
        let genesis = make_genesis();
        let state = PlatformState::new(db, &genesis).unwrap();

        let subnet_id = Id::from_slice(&[1; 32]).unwrap();
        let subnet = Subnet {
            id: subnet_id,
            owner: vec![vec![1, 2, 3]],
            threshold: 1,
        };

        state.add_subnet(subnet).unwrap();
        assert!(state.get_subnet(&subnet_id).is_some());
    }
}
