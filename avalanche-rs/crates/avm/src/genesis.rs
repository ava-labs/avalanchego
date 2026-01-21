//! Genesis configuration for the AVM.

use avalanche_ids::Id;
use avalanche_vm::{Result, VMError};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::asset::Asset;
use crate::utxo::{TransferOutput, UTXO, UTXOID, Output};

/// Genesis configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Genesis {
    /// Network ID
    pub network_id: u32,
    /// Genesis timestamp
    pub timestamp: DateTime<Utc>,
    /// Initial assets
    pub assets: Vec<GenesisAsset>,
}

/// Asset defined in genesis.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GenesisAsset {
    /// Asset name
    pub name: String,
    /// Asset symbol
    pub symbol: String,
    /// Denomination
    pub denomination: u8,
    /// Initial allocations
    pub allocations: Vec<Allocation>,
}

/// Initial allocation of an asset.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Allocation {
    /// Address to receive tokens
    pub address: Vec<u8>,
    /// Amount of tokens
    pub amount: u64,
    /// Locktime (0 = unlocked)
    pub locktime: u64,
}

impl Genesis {
    /// Creates a new genesis configuration.
    pub fn new(network_id: u32, timestamp: DateTime<Utc>) -> Self {
        Self {
            network_id,
            timestamp,
            assets: Vec::new(),
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

    /// Adds an asset to genesis.
    pub fn add_asset(&mut self, asset: GenesisAsset) {
        self.assets.push(asset);
    }

    /// Validates the genesis configuration.
    pub fn validate(&self) -> std::result::Result<(), GenesisError> {
        if self.network_id == 0 {
            return Err(GenesisError::InvalidNetworkId);
        }

        if self.assets.is_empty() {
            return Err(GenesisError::NoAssets);
        }

        for asset in &self.assets {
            if asset.name.is_empty() {
                return Err(GenesisError::InvalidAssetName);
            }
            if asset.symbol.is_empty() {
                return Err(GenesisError::InvalidAssetSymbol);
            }
            if asset.allocations.is_empty() {
                return Err(GenesisError::NoAllocations);
            }
        }

        Ok(())
    }

    /// Converts genesis assets to Asset and UTXO structures.
    pub fn to_assets_and_utxos(&self) -> (Vec<Asset>, Vec<UTXO>) {
        let mut assets = Vec::new();
        let mut utxos = Vec::new();

        for (asset_idx, genesis_asset) in self.assets.iter().enumerate() {
            // Compute asset ID from genesis data
            let mut hasher = Sha256::new();
            hasher.update(genesis_asset.name.as_bytes());
            hasher.update(genesis_asset.symbol.as_bytes());
            hasher.update(asset_idx.to_be_bytes());
            let hash = hasher.finalize();
            let asset_id = Id::from_slice(&hash).unwrap_or_default();

            let asset = Asset::new_fungible(
                asset_id,
                genesis_asset.name.clone(),
                genesis_asset.symbol.clone(),
                genesis_asset.denomination,
            );
            assets.push(asset);

            // Create UTXOs for allocations
            for (alloc_idx, alloc) in genesis_asset.allocations.iter().enumerate() {
                let tx_id = asset_id; // Use asset ID as "genesis tx" ID
                let utxo_id = UTXOID::new(tx_id, alloc_idx as u32);
                let output = Output::Transfer(TransferOutput::new(
                    alloc.amount,
                    alloc.locktime,
                    1,
                    vec![alloc.address.clone()],
                ));
                utxos.push(UTXO::new(utxo_id, asset_id, output));
            }
        }

        (assets, utxos)
    }

    /// Serializes genesis to JSON.
    pub fn to_json(&self) -> std::result::Result<String, serde_json::Error> {
        serde_json::to_string_pretty(self)
    }

    /// Deserializes genesis from JSON.
    pub fn from_json(json: &str) -> std::result::Result<Self, serde_json::Error> {
        serde_json::from_str(json)
    }
}

/// Genesis validation errors.
#[derive(Debug, Clone, thiserror::Error)]
pub enum GenesisError {
    #[error("invalid network ID")]
    InvalidNetworkId,
    #[error("no assets defined")]
    NoAssets,
    #[error("invalid asset name")]
    InvalidAssetName,
    #[error("invalid asset symbol")]
    InvalidAssetSymbol,
    #[error("no allocations for asset")]
    NoAllocations,
}

/// Default genesis configurations.
pub mod defaults {
    use super::*;

    /// Local network ID.
    pub const LOCAL_ID: u32 = 12345;

    /// Creates a local test genesis with AVAX.
    pub fn local_genesis() -> Genesis {
        let now = Utc::now();
        let mut genesis = Genesis::new(LOCAL_ID, now);

        genesis.add_asset(GenesisAsset {
            name: "Avalanche".to_string(),
            symbol: "AVAX".to_string(),
            denomination: 9,
            allocations: vec![
                Allocation {
                    address: vec![1; 20],
                    amount: 300_000_000_000_000_000, // 300M AVAX
                    locktime: 0,
                },
            ],
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
        assert!(matches!(result, Err(GenesisError::NoAssets)));
    }

    #[test]
    fn test_genesis_to_assets() {
        let genesis = defaults::local_genesis();
        let (assets, utxos) = genesis.to_assets_and_utxos();

        assert_eq!(assets.len(), 1);
        assert_eq!(assets[0].name, "Avalanche");
        assert_eq!(utxos.len(), 1);
    }

    #[test]
    fn test_genesis_serialization() {
        let genesis = defaults::local_genesis();
        let json = genesis.to_json().unwrap();
        let parsed = Genesis::from_json(&json).unwrap();

        assert_eq!(parsed.network_id, genesis.network_id);
        assert_eq!(parsed.assets.len(), genesis.assets.len());
    }
}
