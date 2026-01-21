//! Transaction types for the AVM.

use avalanche_ids::Id;
use avalanche_vm::{Result, VMError};
use serde::{Deserialize, Serialize};

use crate::asset::{Asset, AssetState};
use crate::utxo::{Input, Output, TransferOutput, UTXOID};

/// Base transaction fields.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BaseTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferableOutput>,
    /// Inputs
    pub inputs: Vec<TransferableInput>,
    /// Memo
    pub memo: Vec<u8>,
}

impl BaseTx {
    /// Creates a new base transaction.
    pub fn new(network_id: u32, blockchain_id: Id) -> Self {
        Self {
            network_id,
            blockchain_id,
            outputs: Vec::new(),
            inputs: Vec::new(),
            memo: Vec::new(),
        }
    }

    /// Adds an output.
    pub fn add_output(&mut self, output: TransferableOutput) {
        self.outputs.push(output);
    }

    /// Adds an input.
    pub fn add_input(&mut self, input: TransferableInput) {
        self.inputs.push(input);
    }

    /// Sets the memo.
    pub fn with_memo(mut self, memo: Vec<u8>) -> Self {
        self.memo = memo;
        self
    }

    /// Validates the base transaction.
    pub fn validate(&self) -> Result<()> {
        // Check network ID
        if self.network_id == 0 {
            return Err(VMError::InvalidParameter("invalid network ID".to_string()));
        }

        // Check inputs not empty for non-create transactions
        // (create transactions can have no inputs)

        // Check outputs are valid
        for output in &self.outputs {
            if output.output.amount().unwrap_or(0) == 0 {
                return Err(VMError::InvalidParameter("output amount is zero".to_string()));
            }
        }

        // Check memo size
        if self.memo.len() > 256 {
            return Err(VMError::InvalidParameter("memo too large".to_string()));
        }

        Ok(())
    }
}

/// Transferable output (output with asset ID).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransferableOutput {
    /// Asset ID
    pub asset_id: Id,
    /// Output
    pub output: Output,
}

impl TransferableOutput {
    /// Creates a new transferable output.
    pub fn new(asset_id: Id, output: Output) -> Self {
        Self { asset_id, output }
    }

    /// Creates a simple transfer output.
    pub fn transfer(asset_id: Id, amount: u64, address: Vec<u8>) -> Self {
        Self {
            asset_id,
            output: Output::Transfer(TransferOutput::simple(amount, address)),
        }
    }
}

/// Transferable input (input with asset ID and UTXO reference).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransferableInput {
    /// UTXO being spent
    pub utxo_id: UTXOID,
    /// Asset ID
    pub asset_id: Id,
    /// Input
    pub input: Input,
}

impl TransferableInput {
    /// Creates a new transferable input.
    pub fn new(utxo_id: UTXOID, asset_id: Id, input: Input) -> Self {
        Self {
            utxo_id,
            asset_id,
            input,
        }
    }
}

/// Transaction types in the AVM.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Transaction {
    /// Base transaction (simple transfer)
    Base(BaseTx),
    /// Create asset transaction
    CreateAsset(CreateAssetTx),
    /// Operation transaction (mint, burn, etc.)
    Operation(OperationTx),
    /// Import transaction (from P-Chain or C-Chain)
    Import(ImportTx),
    /// Export transaction (to P-Chain or C-Chain)
    Export(ExportTx),
}

impl Transaction {
    /// Validates the transaction.
    pub fn validate(&self) -> Result<()> {
        match self {
            Transaction::Base(tx) => tx.validate(),
            Transaction::CreateAsset(tx) => tx.validate(),
            Transaction::Operation(tx) => tx.validate(),
            Transaction::Import(tx) => tx.validate(),
            Transaction::Export(tx) => tx.validate(),
        }
    }

    /// Returns the transaction ID.
    pub fn id(&self) -> Id {
        use sha2::{Digest, Sha256};
        let bytes = serde_json::to_vec(self).unwrap_or_default();
        let hash = Sha256::digest(&bytes);
        Id::from_slice(&hash).unwrap_or_default()
    }

    /// Returns the network ID.
    pub fn network_id(&self) -> u32 {
        match self {
            Transaction::Base(tx) => tx.network_id,
            Transaction::CreateAsset(tx) => tx.base.network_id,
            Transaction::Operation(tx) => tx.base.network_id,
            Transaction::Import(tx) => tx.base.network_id,
            Transaction::Export(tx) => tx.base.network_id,
        }
    }
}

/// Create asset transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateAssetTx {
    /// Base transaction
    pub base: BaseTx,
    /// Asset name
    pub name: String,
    /// Asset symbol
    pub symbol: String,
    /// Denomination
    pub denomination: u8,
    /// Initial states
    pub initial_states: Vec<AssetState>,
}

impl CreateAssetTx {
    /// Creates a new create asset transaction.
    pub fn new(
        base: BaseTx,
        name: impl Into<String>,
        symbol: impl Into<String>,
        denomination: u8,
    ) -> Self {
        Self {
            base,
            name: name.into(),
            symbol: symbol.into(),
            denomination,
            initial_states: Vec::new(),
        }
    }

    /// Adds an initial state.
    pub fn with_state(mut self, state: AssetState) -> Self {
        self.initial_states.push(state);
        self
    }

    /// Validates the transaction.
    pub fn validate(&self) -> Result<()> {
        self.base.validate()?;

        // Check name
        if self.name.is_empty() || self.name.len() > 128 {
            return Err(VMError::InvalidParameter("invalid asset name".to_string()));
        }

        // Check symbol
        if self.symbol.is_empty() || self.symbol.len() > 8 {
            return Err(VMError::InvalidParameter("invalid asset symbol".to_string()));
        }

        // Check denomination
        if self.denomination > 32 {
            return Err(VMError::InvalidParameter("invalid denomination".to_string()));
        }

        // Check initial states
        if self.initial_states.is_empty() {
            return Err(VMError::InvalidParameter("no initial states".to_string()));
        }

        Ok(())
    }

    /// Creates the asset from this transaction.
    pub fn to_asset(&self, tx_id: Id) -> Asset {
        Asset {
            id: tx_id,
            name: self.name.clone(),
            symbol: self.symbol.clone(),
            denomination: self.denomination,
            asset_type: crate::asset::AssetType::Fungible,
            initial_states: self.initial_states.clone(),
        }
    }
}

/// Operation transaction (for minting, etc.).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OperationTx {
    /// Base transaction
    pub base: BaseTx,
    /// Operations to perform
    pub operations: Vec<Operation>,
}

impl OperationTx {
    /// Validates the transaction.
    pub fn validate(&self) -> Result<()> {
        self.base.validate()?;

        if self.operations.is_empty() {
            return Err(VMError::InvalidParameter("no operations".to_string()));
        }

        Ok(())
    }
}

/// An operation on an asset.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Operation {
    /// Asset ID
    pub asset_id: Id,
    /// UTXOs being operated on
    pub utxo_ids: Vec<UTXOID>,
    /// Operation inputs
    pub inputs: Vec<Input>,
    /// Operation outputs
    pub outputs: Vec<Output>,
}

/// Import transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ImportTx {
    /// Base transaction
    pub base: BaseTx,
    /// Source chain ID
    pub source_chain: Id,
    /// Imported inputs
    pub imported_inputs: Vec<TransferableInput>,
}

impl ImportTx {
    /// Validates the transaction.
    pub fn validate(&self) -> Result<()> {
        self.base.validate()?;

        if self.source_chain == Id::default() {
            return Err(VMError::InvalidParameter("invalid source chain".to_string()));
        }

        if self.imported_inputs.is_empty() {
            return Err(VMError::InvalidParameter("no imported inputs".to_string()));
        }

        Ok(())
    }
}

/// Export transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExportTx {
    /// Base transaction
    pub base: BaseTx,
    /// Destination chain ID
    pub destination_chain: Id,
    /// Exported outputs
    pub exported_outputs: Vec<TransferableOutput>,
}

impl ExportTx {
    /// Validates the transaction.
    pub fn validate(&self) -> Result<()> {
        self.base.validate()?;

        if self.destination_chain == Id::default() {
            return Err(VMError::InvalidParameter("invalid destination chain".to_string()));
        }

        if self.exported_outputs.is_empty() {
            return Err(VMError::InvalidParameter("no exported outputs".to_string()));
        }

        Ok(())
    }
}

/// Signed transaction wrapper.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SignedTx {
    /// Unsigned transaction
    pub unsigned: Transaction,
    /// Credentials (signatures)
    pub credentials: Vec<Credential>,
}

impl SignedTx {
    /// Creates a new signed transaction.
    pub fn new(unsigned: Transaction, credentials: Vec<Credential>) -> Self {
        Self {
            unsigned,
            credentials,
        }
    }

    /// Returns the transaction ID.
    pub fn id(&self) -> Id {
        self.unsigned.id()
    }

    /// Validates the signed transaction.
    pub fn validate(&self) -> Result<()> {
        self.unsigned.validate()?;

        // In a real implementation, verify signatures
        if self.credentials.is_empty() {
            return Err(VMError::InvalidParameter("no credentials".to_string()));
        }

        Ok(())
    }
}

/// Credential (set of signatures).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Credential {
    /// Signatures
    pub signatures: Vec<Signature>,
}

/// A signature.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Signature {
    /// Signature bytes (65 bytes for secp256k1)
    pub bytes: Vec<u8>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_base_tx() {
        let tx = BaseTx::new(1, Id::default());
        assert_eq!(tx.network_id, 1);
        assert!(tx.inputs.is_empty());
        assert!(tx.outputs.is_empty());
    }

    #[test]
    fn test_create_asset_tx() {
        use crate::asset::{AssetState, FixedCapMint, MintOutput};

        let base = BaseTx::new(1, Id::default());
        let tx = CreateAssetTx::new(base, "Test Token", "TEST", 6)
            .with_state(AssetState::FixedCap(FixedCapMint {
                outputs: vec![MintOutput {
                    amount: 1000,
                    locktime: 0,
                    threshold: 1,
                    addresses: vec![vec![1, 2, 3]],
                }],
            }));

        assert_eq!(tx.name, "Test Token");
        assert_eq!(tx.symbol, "TEST");
        assert_eq!(tx.denomination, 6);
        assert!(!tx.initial_states.is_empty());
    }

    #[test]
    fn test_transaction_id() {
        let base = BaseTx::new(1, Id::default());
        let tx = Transaction::Base(base);
        let id = tx.id();
        assert_ne!(id, Id::default());
    }
}
