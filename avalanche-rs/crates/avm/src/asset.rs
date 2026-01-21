//! Asset definitions for the AVM.

use avalanche_ids::Id;
use serde::{Deserialize, Serialize};

/// Asset types supported by the AVM.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum AssetType {
    /// Fungible token (divisible)
    Fungible,
    /// Non-fungible token (unique)
    NFT,
    /// Variable cap asset
    VariableCap,
}

/// Asset definition.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Asset {
    /// Asset ID (derived from creation tx)
    pub id: Id,
    /// Asset name
    pub name: String,
    /// Asset symbol
    pub symbol: String,
    /// Number of decimal places
    pub denomination: u8,
    /// Asset type
    pub asset_type: AssetType,
    /// Initial states (minters, etc.)
    pub initial_states: Vec<AssetState>,
}

impl Asset {
    /// Creates a new fungible asset.
    pub fn new_fungible(
        id: Id,
        name: impl Into<String>,
        symbol: impl Into<String>,
        denomination: u8,
    ) -> Self {
        Self {
            id,
            name: name.into(),
            symbol: symbol.into(),
            denomination,
            asset_type: AssetType::Fungible,
            initial_states: Vec::new(),
        }
    }

    /// Creates a new NFT asset.
    pub fn new_nft(id: Id, name: impl Into<String>, symbol: impl Into<String>) -> Self {
        Self {
            id,
            name: name.into(),
            symbol: symbol.into(),
            denomination: 0,
            asset_type: AssetType::NFT,
            initial_states: Vec::new(),
        }
    }

    /// Creates a new variable cap asset.
    pub fn new_variable_cap(
        id: Id,
        name: impl Into<String>,
        symbol: impl Into<String>,
        denomination: u8,
    ) -> Self {
        Self {
            id,
            name: name.into(),
            symbol: symbol.into(),
            denomination,
            asset_type: AssetType::VariableCap,
            initial_states: Vec::new(),
        }
    }

    /// Adds an initial state.
    pub fn with_state(mut self, state: AssetState) -> Self {
        self.initial_states.push(state);
        self
    }

    /// Returns true if this is a fungible asset.
    pub fn is_fungible(&self) -> bool {
        self.asset_type == AssetType::Fungible
    }

    /// Returns true if this is an NFT.
    pub fn is_nft(&self) -> bool {
        self.asset_type == AssetType::NFT
    }

    /// Converts amount to base units.
    pub fn to_base_units(&self, amount: f64) -> u64 {
        let multiplier = 10u64.pow(self.denomination as u32);
        (amount * multiplier as f64) as u64
    }

    /// Converts base units to display amount.
    pub fn from_base_units(&self, amount: u64) -> f64 {
        let divisor = 10u64.pow(self.denomination as u32);
        amount as f64 / divisor as f64
    }
}

/// Asset state for initial minting configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum AssetState {
    /// Fixed cap mint (one-time mint)
    FixedCap(FixedCapMint),
    /// Variable cap mint (can mint more)
    VariableCap(VariableCapMint),
    /// NFT mint state
    NFT(NFTMint),
}

/// Fixed cap mint state.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FixedCapMint {
    /// Output owners who receive the initial mint
    pub outputs: Vec<MintOutput>,
}

/// Variable cap mint state.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VariableCapMint {
    /// Addresses that can mint more
    pub minters: Vec<Vec<u8>>,
    /// Threshold required to mint
    pub threshold: u32,
}

/// NFT mint state.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NFTMint {
    /// Group ID for this NFT collection
    pub group_id: u32,
    /// Addresses that can mint NFTs
    pub minters: Vec<Vec<u8>>,
    /// Threshold required to mint
    pub threshold: u32,
}

/// Output for initial minting.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MintOutput {
    /// Amount to mint
    pub amount: u64,
    /// Locktime
    pub locktime: u64,
    /// Threshold for spending
    pub threshold: u32,
    /// Addresses that can spend
    pub addresses: Vec<Vec<u8>>,
}

/// AVAX token constants.
pub mod avax {
    use super::*;

    /// AVAX asset ID (all zeros on mainnet).
    pub fn asset_id() -> Id {
        Id::default()
    }

    /// AVAX denomination (9 = nAVAX).
    pub const DENOMINATION: u8 = 9;

    /// 1 AVAX in nAVAX.
    pub const ONE_AVAX: u64 = 1_000_000_000;

    /// Creates the AVAX asset definition.
    pub fn avax_asset() -> Asset {
        Asset::new_fungible(asset_id(), "Avalanche", "AVAX", DENOMINATION)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_fungible_asset() {
        let asset = Asset::new_fungible(Id::default(), "Test Token", "TEST", 6);
        assert!(asset.is_fungible());
        assert!(!asset.is_nft());
        assert_eq!(asset.denomination, 6);
    }

    #[test]
    fn test_nft_asset() {
        let asset = Asset::new_nft(Id::default(), "Test NFT", "TNFT");
        assert!(asset.is_nft());
        assert!(!asset.is_fungible());
        assert_eq!(asset.denomination, 0);
    }

    #[test]
    fn test_unit_conversion() {
        let asset = Asset::new_fungible(Id::default(), "Test", "T", 6);

        // 1.5 tokens = 1_500_000 base units
        assert_eq!(asset.to_base_units(1.5), 1_500_000);
        assert_eq!(asset.from_base_units(1_500_000), 1.5);
    }

    #[test]
    fn test_avax_constants() {
        assert_eq!(avax::DENOMINATION, 9);
        assert_eq!(avax::ONE_AVAX, 1_000_000_000);

        let avax = avax::avax_asset();
        assert_eq!(avax.to_base_units(1.0), avax::ONE_AVAX);
    }
}
