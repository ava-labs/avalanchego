//! AVM state management.

use std::collections::HashMap;
use std::sync::Arc;

use avalanche_db::Database;
use avalanche_ids::Id;
use avalanche_vm::{Result, VMError};
use chrono::{DateTime, Utc};
use parking_lot::RwLock;

use crate::asset::Asset;
use crate::block::AVMBlock;
use crate::genesis::Genesis;
use crate::txs::Transaction;
use crate::utxo::{UTXOSet, UTXO, UTXOID};

/// Keys for AVM state.
pub mod keys {
    pub const ASSETS: &[u8] = b"assets";
    pub const UTXOS: &[u8] = b"utxos";
    pub const BLOCKS: &[u8] = b"blocks";
    pub const LAST_ACCEPTED: &[u8] = b"lastAccepted";
    pub const GENESIS_BLOCK: &[u8] = b"genesisBlock";
}

/// AVM state manager.
pub struct AVMState {
    /// Database
    db: Arc<dyn Database>,
    /// Assets indexed by ID
    assets: RwLock<HashMap<Id, Asset>>,
    /// UTXO set
    utxos: RwLock<UTXOSet>,
    /// Genesis block ID
    genesis_block_id: Id,
    /// Current timestamp
    timestamp: RwLock<DateTime<Utc>>,
}

impl std::fmt::Debug for AVMState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("AVMState")
            .field("genesis_block_id", &self.genesis_block_id)
            .field("asset_count", &self.assets.read().len())
            .field("utxo_count", &self.utxos.read().len())
            .finish_non_exhaustive()
    }
}

impl AVMState {
    /// Creates a new AVM state from genesis.
    pub fn new(db: Arc<dyn Database>, genesis: &Genesis) -> Result<Self> {
        let genesis_block_id = genesis.compute_block_id();

        // Convert genesis to assets and UTXOs
        let (genesis_assets, genesis_utxos) = genesis.to_assets_and_utxos();

        let mut assets = HashMap::new();
        for asset in genesis_assets {
            assets.insert(asset.id, asset);
        }

        let mut utxos = UTXOSet::new();
        for utxo in genesis_utxos {
            utxos.add(utxo);
        }

        let state = Self {
            db,
            assets: RwLock::new(assets),
            utxos: RwLock::new(utxos),
            genesis_block_id,
            timestamp: RwLock::new(genesis.timestamp),
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

    /// Returns current timestamp.
    pub fn timestamp(&self) -> DateTime<Utc> {
        *self.timestamp.read()
    }

    /// Gets an asset by ID.
    pub fn get_asset(&self, id: &Id) -> Option<Asset> {
        self.assets.read().get(id).cloned()
    }

    /// Adds an asset.
    pub fn add_asset(&self, asset: Asset) -> Result<()> {
        let id = asset.id;
        if self.assets.read().contains_key(&id) {
            return Err(VMError::InvalidParameter("asset already exists".to_string()));
        }
        self.assets.write().insert(id, asset);
        Ok(())
    }

    /// Returns all assets.
    pub fn all_assets(&self) -> Vec<Asset> {
        self.assets.read().values().cloned().collect()
    }

    /// Gets a UTXO by ID.
    pub fn get_utxo(&self, id: &UTXOID) -> Option<UTXO> {
        self.utxos.read().get(id).cloned()
    }

    /// Adds a UTXO.
    pub fn add_utxo(&self, utxo: UTXO) {
        self.utxos.write().add(utxo);
    }

    /// Removes a UTXO.
    pub fn remove_utxo(&self, id: &UTXOID) -> Option<UTXO> {
        self.utxos.write().remove(id)
    }

    /// Returns UTXOs for an address.
    pub fn utxos_for_address(&self, address: &[u8]) -> Vec<UTXO> {
        self.utxos.read().by_address(address).into_iter().cloned().collect()
    }

    /// Returns the balance of an asset for an address.
    pub fn balance(&self, address: &[u8], asset_id: &Id) -> u64 {
        self.utxos
            .read()
            .by_address(address)
            .iter()
            .filter(|u| &u.asset_id == asset_id)
            .filter_map(|u| u.amount())
            .sum()
    }

    /// Returns total supply of an asset.
    pub fn total_supply(&self, asset_id: &Id) -> u64 {
        self.utxos.read().total_amount(asset_id)
    }

    /// Builds a new block.
    pub fn build_block(&self, parent_id: Id) -> Result<AVMBlock> {
        let timestamp = *self.timestamp.read();
        let height = 1; // Would compute from parent

        AVMBlock::new_standard(parent_id, height, timestamp, vec![])
    }

    /// Gets a block by ID.
    pub fn get_block(&self, id: &Id) -> Result<Option<AVMBlock>> {
        let key = [keys::BLOCKS, id.as_ref()].concat();
        match self.db.get(&key)? {
            Some(bytes) => {
                let block = AVMBlock::parse(&bytes)?;
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

    /// Stores a block.
    pub fn put_block(&self, id: &Id, bytes: &[u8]) -> Result<()> {
        let key = [keys::BLOCKS, id.as_ref()].concat();
        self.db.put(&key, bytes)?;
        Ok(())
    }

    /// Applies a transaction to state.
    pub fn apply_transaction(&self, tx: &Transaction) -> Result<()> {
        match tx {
            Transaction::Base(base_tx) => {
                // Remove spent UTXOs
                for input in &base_tx.inputs {
                    if self.remove_utxo(&input.utxo_id).is_none() {
                        return Err(VMError::InvalidParameter("UTXO not found".to_string()));
                    }
                }

                // Add new UTXOs
                let tx_id = tx.id();
                for (idx, output) in base_tx.outputs.iter().enumerate() {
                    let utxo = UTXO::new(
                        UTXOID::new(tx_id, idx as u32),
                        output.asset_id,
                        output.output.clone(),
                    );
                    self.add_utxo(utxo);
                }
            }
            Transaction::CreateAsset(create_tx) => {
                let tx_id = tx.id();
                let asset = create_tx.to_asset(tx_id);
                self.add_asset(asset)?;

                // Add outputs as UTXOs
                for (idx, output) in create_tx.base.outputs.iter().enumerate() {
                    let utxo = UTXO::new(
                        UTXOID::new(tx_id, idx as u32),
                        tx_id, // Asset ID is the tx ID
                        output.output.clone(),
                    );
                    self.add_utxo(utxo);
                }
            }
            Transaction::Operation(op_tx) => {
                // Handle mint and other operations
                for op in &op_tx.operations {
                    // Remove spent UTXOs
                    for utxo_id in &op.utxo_ids {
                        self.remove_utxo(utxo_id);
                    }
                }
            }
            Transaction::Import(import_tx) => {
                // Add imported UTXOs
                let tx_id = tx.id();
                for (idx, output) in import_tx.base.outputs.iter().enumerate() {
                    let utxo = UTXO::new(
                        UTXOID::new(tx_id, idx as u32),
                        output.asset_id,
                        output.output.clone(),
                    );
                    self.add_utxo(utxo);
                }
            }
            Transaction::Export(export_tx) => {
                // Remove exported UTXOs
                for input in &export_tx.base.inputs {
                    self.remove_utxo(&input.utxo_id);
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_db::MemDb;
    use crate::genesis::defaults;

    #[test]
    fn test_avm_state_new() {
        let db = Arc::new(MemDb::new());
        let genesis = defaults::local_genesis();
        let state = AVMState::new(db, &genesis).unwrap();

        assert_eq!(state.all_assets().len(), 1);
        assert!(state.utxos.read().len() > 0);
    }

    #[test]
    fn test_balance() {
        let db = Arc::new(MemDb::new());
        let genesis = defaults::local_genesis();
        let state = AVMState::new(db, &genesis).unwrap();

        let address = vec![1; 20];
        let asset_id = state.all_assets()[0].id;
        let balance = state.balance(&address, &asset_id);

        assert_eq!(balance, 300_000_000_000_000_000);
    }

    #[test]
    fn test_asset_management() {
        let db = Arc::new(MemDb::new());
        let genesis = defaults::local_genesis();
        let state = AVMState::new(db, &genesis).unwrap();

        let new_asset = Asset::new_fungible(
            Id::from_slice(&[99; 32]).unwrap(),
            "Test Token",
            "TEST",
            6,
        );
        state.add_asset(new_asset.clone()).unwrap();

        let retrieved = state.get_asset(&new_asset.id);
        assert!(retrieved.is_some());
        assert_eq!(retrieved.unwrap().name, "Test Token");
    }
}
