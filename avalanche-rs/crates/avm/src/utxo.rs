//! UTXO (Unspent Transaction Output) types for the AVM.

use avalanche_ids::Id;
use serde::{Deserialize, Serialize};

/// UTXO identifier.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct UTXOID {
    /// Transaction ID that created this UTXO
    pub tx_id: Id,
    /// Output index within the transaction
    pub output_index: u32,
}

impl UTXOID {
    /// Creates a new UTXO ID.
    pub fn new(tx_id: Id, output_index: u32) -> Self {
        Self { tx_id, output_index }
    }

    /// Computes a unique ID for this UTXO.
    pub fn id(&self) -> Id {
        use sha2::{Digest, Sha256};
        let mut hasher = Sha256::new();
        hasher.update(self.tx_id.as_ref());
        hasher.update(self.output_index.to_be_bytes());
        let hash = hasher.finalize();
        Id::from_slice(&hash).unwrap_or_default()
    }
}

/// A UTXO with its output data.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UTXO {
    /// UTXO identifier
    pub id: UTXOID,
    /// Asset ID
    pub asset_id: Id,
    /// Output data
    pub output: Output,
}

impl UTXO {
    /// Creates a new UTXO.
    pub fn new(id: UTXOID, asset_id: Id, output: Output) -> Self {
        Self { id, asset_id, output }
    }

    /// Returns the amount if this is a transfer output.
    pub fn amount(&self) -> Option<u64> {
        match &self.output {
            Output::Transfer(t) => Some(t.amount),
            Output::Mint(_) => None,
            Output::NFTTransfer(_) => None,
            Output::NFTMint(_) => None,
        }
    }

    /// Returns the locktime of this output.
    pub fn locktime(&self) -> u64 {
        match &self.output {
            Output::Transfer(t) => t.locktime,
            Output::Mint(m) => m.locktime,
            Output::NFTTransfer(n) => n.locktime,
            Output::NFTMint(n) => n.locktime,
        }
    }

    /// Returns the addresses that can spend this output.
    pub fn addresses(&self) -> &[Vec<u8>] {
        match &self.output {
            Output::Transfer(t) => &t.addresses,
            Output::Mint(m) => &m.addresses,
            Output::NFTTransfer(n) => &n.addresses,
            Output::NFTMint(n) => &n.addresses,
        }
    }

    /// Returns the threshold required to spend.
    pub fn threshold(&self) -> u32 {
        match &self.output {
            Output::Transfer(t) => t.threshold,
            Output::Mint(m) => m.threshold,
            Output::NFTTransfer(n) => n.threshold,
            Output::NFTMint(n) => n.threshold,
        }
    }

    /// Checks if this UTXO is spendable at the given time.
    pub fn is_spendable(&self, current_time: u64) -> bool {
        self.locktime() <= current_time
    }
}

/// Output types.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Output {
    /// Standard transfer output
    Transfer(TransferOutput),
    /// Mint output (can mint more tokens)
    Mint(MintOutput),
    /// NFT transfer output
    NFTTransfer(NFTTransferOutput),
    /// NFT mint output
    NFTMint(NFTMintOutput),
}

impl Output {
    /// Returns the amount if this is a transfer output.
    pub fn amount(&self) -> Option<u64> {
        match self {
            Output::Transfer(t) => Some(t.amount),
            _ => None,
        }
    }
}

/// Standard transfer output.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransferOutput {
    /// Amount of tokens
    pub amount: u64,
    /// Locktime (unix timestamp)
    pub locktime: u64,
    /// Number of signatures required
    pub threshold: u32,
    /// Addresses that can spend
    pub addresses: Vec<Vec<u8>>,
}

impl TransferOutput {
    /// Creates a new transfer output.
    pub fn new(amount: u64, locktime: u64, threshold: u32, addresses: Vec<Vec<u8>>) -> Self {
        Self {
            amount,
            locktime,
            threshold,
            addresses,
        }
    }

    /// Creates a simple output to a single address.
    pub fn simple(amount: u64, address: Vec<u8>) -> Self {
        Self {
            amount,
            locktime: 0,
            threshold: 1,
            addresses: vec![address],
        }
    }
}

/// Mint output (variable cap).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MintOutput {
    /// Locktime
    pub locktime: u64,
    /// Threshold
    pub threshold: u32,
    /// Minter addresses
    pub addresses: Vec<Vec<u8>>,
}

/// NFT transfer output.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NFTTransferOutput {
    /// Group ID
    pub group_id: u32,
    /// NFT payload
    pub payload: Vec<u8>,
    /// Locktime
    pub locktime: u64,
    /// Threshold
    pub threshold: u32,
    /// Owner addresses
    pub addresses: Vec<Vec<u8>>,
}

/// NFT mint output.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NFTMintOutput {
    /// Group ID
    pub group_id: u32,
    /// Locktime
    pub locktime: u64,
    /// Threshold
    pub threshold: u32,
    /// Minter addresses
    pub addresses: Vec<Vec<u8>>,
}

/// Input types for spending UTXOs.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Input {
    /// Standard transfer input
    Transfer(TransferInput),
    /// Mint input
    Mint(MintInput),
    /// NFT transfer input
    NFTTransfer(NFTTransferInput),
    /// NFT mint input
    NFTMint(NFTMintInput),
}

/// Standard transfer input.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransferInput {
    /// Amount being spent
    pub amount: u64,
    /// Signature indices (which addresses are signing)
    pub sig_indices: Vec<u32>,
}

/// Mint input.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MintInput {
    /// Signature indices
    pub sig_indices: Vec<u32>,
}

/// NFT transfer input.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NFTTransferInput {
    /// Signature indices
    pub sig_indices: Vec<u32>,
}

/// NFT mint input.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NFTMintInput {
    /// Group ID
    pub group_id: u32,
    /// NFT payload
    pub payload: Vec<u8>,
    /// Signature indices
    pub sig_indices: Vec<u32>,
}

/// UTXO set for tracking unspent outputs.
#[derive(Debug, Clone, Default)]
pub struct UTXOSet {
    /// UTXOs indexed by ID
    utxos: std::collections::HashMap<UTXOID, UTXO>,
}

impl UTXOSet {
    /// Creates a new empty UTXO set.
    pub fn new() -> Self {
        Self::default()
    }

    /// Adds a UTXO to the set.
    pub fn add(&mut self, utxo: UTXO) {
        self.utxos.insert(utxo.id, utxo);
    }

    /// Removes a UTXO from the set.
    pub fn remove(&mut self, id: &UTXOID) -> Option<UTXO> {
        self.utxos.remove(id)
    }

    /// Gets a UTXO by ID.
    pub fn get(&self, id: &UTXOID) -> Option<&UTXO> {
        self.utxos.get(id)
    }

    /// Checks if a UTXO exists.
    pub fn contains(&self, id: &UTXOID) -> bool {
        self.utxos.contains_key(id)
    }

    /// Returns all UTXOs for an asset.
    pub fn by_asset(&self, asset_id: &Id) -> Vec<&UTXO> {
        self.utxos
            .values()
            .filter(|u| &u.asset_id == asset_id)
            .collect()
    }

    /// Returns all UTXOs spendable by an address.
    pub fn by_address(&self, address: &[u8]) -> Vec<&UTXO> {
        self.utxos
            .values()
            .filter(|u| u.addresses().iter().any(|a| a == address))
            .collect()
    }

    /// Returns total amount of an asset.
    pub fn total_amount(&self, asset_id: &Id) -> u64 {
        self.by_asset(asset_id)
            .iter()
            .filter_map(|u| u.amount())
            .sum()
    }

    /// Returns the number of UTXOs.
    pub fn len(&self) -> usize {
        self.utxos.len()
    }

    /// Returns true if the set is empty.
    pub fn is_empty(&self) -> bool {
        self.utxos.is_empty()
    }

    /// Returns all UTXOs as a vector.
    pub fn all(&self) -> Vec<&UTXO> {
        self.utxos.values().collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_utxo_id() {
        let tx_id = Id::from_slice(&[1; 32]).unwrap();
        let utxo_id = UTXOID::new(tx_id, 0);
        let computed = utxo_id.id();
        assert_ne!(computed, Id::default());
    }

    #[test]
    fn test_transfer_output() {
        let output = TransferOutput::simple(1000, vec![1, 2, 3]);
        assert_eq!(output.amount, 1000);
        assert_eq!(output.locktime, 0);
        assert_eq!(output.threshold, 1);
    }

    #[test]
    fn test_utxo_set() {
        let mut set = UTXOSet::new();
        assert!(set.is_empty());

        let tx_id = Id::from_slice(&[1; 32]).unwrap();
        let asset_id = Id::from_slice(&[2; 32]).unwrap();
        let utxo_id = UTXOID::new(tx_id, 0);

        let utxo = UTXO::new(
            utxo_id,
            asset_id,
            Output::Transfer(TransferOutput::simple(500, vec![1, 2, 3])),
        );

        set.add(utxo);
        assert_eq!(set.len(), 1);
        assert!(set.contains(&utxo_id));
        assert_eq!(set.total_amount(&asset_id), 500);
    }

    #[test]
    fn test_utxo_spendable() {
        let tx_id = Id::from_slice(&[1; 32]).unwrap();
        let asset_id = Id::default();
        let utxo_id = UTXOID::new(tx_id, 0);

        let locked_utxo = UTXO::new(
            utxo_id,
            asset_id,
            Output::Transfer(TransferOutput::new(100, 1000, 1, vec![vec![1]])),
        );

        assert!(!locked_utxo.is_spendable(500));
        assert!(locked_utxo.is_spendable(1000));
        assert!(locked_utxo.is_spendable(2000));
    }
}
