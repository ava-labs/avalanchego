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

    /// Mainnet network ID.
    pub const MAINNET_ID: u32 = 1;
    /// Fuji testnet network ID.
    pub const FUJI_ID: u32 = 5;
    /// Local network ID.
    pub const LOCAL_ID: u32 = 12345;

    /// AVAX denomination (nanoAVAX = 10^-9 AVAX).
    pub const AVAX_DENOMINATION: u8 = 9;

    /// Mainnet genesis timestamp.
    pub fn mainnet_genesis_time() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2020-09-21T00:00:00Z")
            .unwrap()
            .with_timezone(&Utc)
    }

    /// Fuji genesis timestamp.
    pub fn fuji_genesis_time() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2020-09-23T00:00:00Z")
            .unwrap()
            .with_timezone(&Utc)
    }

    /// AVAX asset ID on X-Chain.
    /// This is the hash of the genesis asset definition.
    pub fn avax_asset_id() -> Id {
        Id::from_slice(&[
            0x21, 0xe6, 0x73, 0x17, 0xcb, 0xc4, 0xbe, 0x2a, 0xeb, 0x00, 0x67, 0x7a, 0xd6, 0x46,
            0x27, 0x78, 0xa8, 0xf5, 0x25, 0x74, 0xe8, 0x15, 0x2a, 0x0a, 0x91, 0xbe, 0xd6, 0xa2,
            0x50, 0x00, 0x00, 0x00,
        ])
        .unwrap_or_default()
    }

    /// Creates mainnet X-Chain genesis.
    pub fn mainnet_genesis() -> Genesis {
        let timestamp = mainnet_genesis_time();
        let mut genesis = Genesis::new(MAINNET_ID, timestamp);

        // AVAX is the native asset on X-Chain
        genesis.add_asset(GenesisAsset {
            name: "Avalanche".to_string(),
            symbol: "AVAX".to_string(),
            denomination: AVAX_DENOMINATION,
            allocations: vec![
                // Foundation
                Allocation {
                    address: vec![0x01; 20],
                    amount: 180_000_000_000_000_000, // 180M AVAX for X-Chain
                    locktime: 0,
                },
                // Ecosystem fund
                Allocation {
                    address: vec![0x02; 20],
                    amount: 90_000_000_000_000_000, // 90M AVAX
                    locktime: 0,
                },
                // Team (with vesting)
                Allocation {
                    address: vec![0x03; 20],
                    amount: 45_000_000_000_000_000, // 45M AVAX
                    locktime: timestamp.timestamp() as u64 + 365 * 24 * 60 * 60,
                },
            ],
        });

        genesis
    }

    /// Creates Fuji testnet X-Chain genesis.
    pub fn fuji_genesis() -> Genesis {
        let timestamp = fuji_genesis_time();
        let mut genesis = Genesis::new(FUJI_ID, timestamp);

        // Testnet AVAX
        genesis.add_asset(GenesisAsset {
            name: "Avalanche".to_string(),
            symbol: "AVAX".to_string(),
            denomination: AVAX_DENOMINATION,
            allocations: vec![
                // Faucet allocation
                Allocation {
                    address: vec![0xFF; 20],
                    amount: 300_000_000_000_000_000, // 300M AVAX
                    locktime: 0,
                },
            ],
        });

        genesis
    }

    /// Creates a local test genesis with AVAX.
    pub fn local_genesis() -> Genesis {
        let now = Utc::now();
        let mut genesis = Genesis::new(LOCAL_ID, now);

        genesis.add_asset(GenesisAsset {
            name: "Avalanche".to_string(),
            symbol: "AVAX".to_string(),
            denomination: AVAX_DENOMINATION,
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
