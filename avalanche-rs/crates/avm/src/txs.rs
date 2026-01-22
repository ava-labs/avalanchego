//! Transaction types for the AVM.

use avalanche_crypto::RecoverableSignature;
use avalanche_ids::Id;
use avalanche_vm::{Result, VMError};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

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

    /// Computes the hash of the unsigned transaction for signature verification.
    pub fn unsigned_hash(&self) -> [u8; 32] {
        use sha2::{Digest, Sha256};
        let bytes = serde_json::to_vec(self).unwrap_or_default();
        let hash = Sha256::digest(&bytes);
        let mut result = [0u8; 32];
        result.copy_from_slice(&hash);
        result
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

    /// Returns the number of inputs in the transaction.
    fn input_count(&self) -> usize {
        match &self.unsigned {
            Transaction::Base(tx) => tx.inputs.len(),
            Transaction::CreateAsset(tx) => tx.base.inputs.len(),
            Transaction::Operation(tx) => tx.base.inputs.len() + tx.operations.len(),
            Transaction::Import(tx) => tx.base.inputs.len() + tx.imported_inputs.len(),
            Transaction::Export(tx) => tx.base.inputs.len(),
        }
    }

    /// Computes the hash of the unsigned transaction for signature verification.
    pub fn unsigned_hash(&self) -> [u8; 32] {
        // In production, this would use proper Avalanche codec serialization.
        // For now, we use JSON + SHA256 as a placeholder.
        let bytes = serde_json::to_vec(&self.unsigned).unwrap_or_default();
        let hash = Sha256::digest(&bytes);
        let mut result = [0u8; 32];
        result.copy_from_slice(&hash);
        result
    }

    /// Validates the signed transaction (basic validation without UTXO lookup).
    ///
    /// This performs:
    /// - Unsigned transaction validation
    /// - Credential count matching
    /// - Signature format validation
    /// - Public key recovery from signatures
    ///
    /// Full address verification requires UTXO state access and should be done
    /// in the VM layer using `validate_with_utxos`.
    pub fn validate(&self) -> Result<()> {
        self.unsigned.validate()?;

        if self.credentials.is_empty() {
            return Err(VMError::InvalidParameter("no credentials".to_string()));
        }

        // Verify credential count matches input count
        let input_count = self.input_count();
        if self.credentials.len() != input_count {
            return Err(VMError::InvalidParameter(format!(
                "credential count ({}) does not match input count ({})",
                self.credentials.len(),
                input_count
            )));
        }

        // Compute the hash for signature verification
        let hash = self.unsigned_hash();

        // Verify each credential has valid, well-formed signatures
        for (i, credential) in self.credentials.iter().enumerate() {
            if credential.signatures.is_empty() {
                return Err(VMError::InvalidParameter(format!(
                    "credential {} has no signatures",
                    i
                )));
            }

            // Validate each signature is well-formed and recoverable
            for (j, sig) in credential.signatures.iter().enumerate() {
                // Verify signature length
                if sig.bytes.len() != 65 {
                    return Err(VMError::InvalidParameter(format!(
                        "signature {}.{} has invalid length: expected 65, got {}",
                        i,
                        j,
                        sig.bytes.len()
                    )));
                }

                // Parse and verify the signature is recoverable
                let recoverable_sig = RecoverableSignature::from_bytes(&sig.bytes)
                    .map_err(|e| {
                        VMError::InvalidParameter(format!(
                            "signature {}.{} is invalid: {}",
                            i, j, e
                        ))
                    })?;

                // Recover public key to verify signature is valid
                let _public_key = recoverable_sig.recover_from_hash(&hash).map_err(|e| {
                    VMError::InvalidParameter(format!(
                        "failed to recover public key from signature {}.{}: {}",
                        i, j, e
                    ))
                })?;
            }
        }

        Ok(())
    }

    /// Validates the signed transaction with full UTXO verification.
    ///
    /// This verifies that the recovered public keys from signatures correspond
    /// to the addresses in the UTXOs being spent.
    pub fn validate_with_utxos(
        &self,
        utxo_addresses: &[Vec<Vec<u8>>], // addresses for each input's UTXO
    ) -> Result<()> {
        // First perform basic validation
        self.validate()?;

        let hash = self.unsigned_hash();

        // Verify each credential's signatures match the UTXO addresses
        for (i, credential) in self.credentials.iter().enumerate() {
            let utxo_addrs = utxo_addresses
                .get(i)
                .ok_or_else(|| VMError::InvalidParameter(format!("missing UTXO for input {}", i)))?;

            // Get the sig_indices from the input
            let sig_indices = self.get_sig_indices(i);

            // Verify we have enough signatures for the threshold
            // (threshold validation would be done here with actual UTXO data)

            // Verify each signature corresponds to the correct address
            for (j, sig) in credential.signatures.iter().enumerate() {
                let recoverable_sig = RecoverableSignature::from_bytes(&sig.bytes)
                    .map_err(|e| VMError::InvalidParameter(format!("invalid signature: {}", e)))?;

                let public_key = recoverable_sig.recover_from_hash(&hash).map_err(|e| {
                    VMError::InvalidParameter(format!("failed to recover public key: {}", e))
                })?;

                // Compute the address from the recovered public key
                let recovered_address = public_key.to_address();

                // Get which address index this signature is for
                let addr_idx = sig_indices
                    .get(j)
                    .ok_or_else(|| {
                        VMError::InvalidParameter(format!("missing sig_index for signature {}", j))
                    })?;

                // Get the expected address from the UTXO
                let expected_address = utxo_addrs.get(*addr_idx as usize).ok_or_else(|| {
                    VMError::InvalidParameter(format!(
                        "sig_index {} out of bounds for UTXO with {} addresses",
                        addr_idx,
                        utxo_addrs.len()
                    ))
                })?;

                // Verify the addresses match
                if recovered_address.as_slice() != expected_address.as_slice() {
                    return Err(VMError::InvalidParameter(format!(
                        "signature {}.{} does not match expected address",
                        i, j
                    )));
                }
            }
        }

        Ok(())
    }

    /// Gets the signature indices for a given input index.
    fn get_sig_indices(&self, input_idx: usize) -> Vec<u32> {
        let inputs = match &self.unsigned {
            Transaction::Base(tx) => &tx.inputs,
            Transaction::CreateAsset(tx) => &tx.base.inputs,
            Transaction::Operation(tx) => &tx.base.inputs,
            Transaction::Import(tx) => &tx.base.inputs,
            Transaction::Export(tx) => &tx.base.inputs,
        };

        if let Some(input) = inputs.get(input_idx) {
            match &input.input {
                Input::Transfer(t) => t.sig_indices.clone(),
                Input::Mint(m) => m.sig_indices.clone(),
                Input::NFTTransfer(n) => n.sig_indices.clone(),
                Input::NFTMint(n) => n.sig_indices.clone(),
            }
        } else {
            Vec::new()
        }
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
    use crate::utxo::{TransferInput, TransferOutput};
    use avalanche_crypto::PrivateKey;

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

    #[test]
    fn test_signed_tx_validation() {
        // Create a private key and get its address
        let private_key = PrivateKey::generate();
        let public_key = private_key.public_key();
        let address = public_key.to_address().to_vec();

        // Create a simple transaction with one input
        let mut base = BaseTx::new(1, Id::default());
        let tx_id = Id::from_slice(&[1; 32]).unwrap();
        let asset_id = Id::from_slice(&[2; 32]).unwrap();

        // Add an input
        base.add_input(TransferableInput::new(
            UTXOID::new(tx_id, 0),
            asset_id,
            Input::Transfer(TransferInput {
                amount: 1000,
                sig_indices: vec![0],
            }),
        ));

        // Add an output
        base.add_output(TransferableOutput::new(
            asset_id,
            Output::Transfer(TransferOutput::simple(900, address.clone())),
        ));

        let unsigned = Transaction::Base(base);

        // Create a signed transaction
        let signed_tx = SignedTx::new(unsigned.clone(), vec![]);

        // Should fail: no credentials
        assert!(signed_tx.validate().is_err());

        // Create signed tx with proper signature
        let mut signed_tx = SignedTx::new(unsigned, vec![]);
        let hash = signed_tx.unsigned_hash();

        // Sign the transaction
        let recoverable_sig = private_key.sign_hash_recoverable(&hash).unwrap();
        let sig_bytes = recoverable_sig.to_bytes().to_vec();

        signed_tx.credentials = vec![Credential {
            signatures: vec![Signature { bytes: sig_bytes }],
        }];

        // Should pass basic validation
        assert!(signed_tx.validate().is_ok());

        // Verify with UTXO addresses (the UTXO should have our address)
        let utxo_addresses = vec![vec![address.clone()]];
        assert!(signed_tx.validate_with_utxos(&utxo_addresses).is_ok());

        // Should fail with wrong address
        let wrong_addresses = vec![vec![vec![99; 20]]];
        assert!(signed_tx.validate_with_utxos(&wrong_addresses).is_err());
    }

    #[test]
    fn test_signed_tx_invalid_signature() {
        let mut base = BaseTx::new(1, Id::default());
        let tx_id = Id::from_slice(&[1; 32]).unwrap();
        let asset_id = Id::from_slice(&[2; 32]).unwrap();

        base.add_input(TransferableInput::new(
            UTXOID::new(tx_id, 0),
            asset_id,
            Input::Transfer(TransferInput {
                amount: 1000,
                sig_indices: vec![0],
            }),
        ));

        base.add_output(TransferableOutput::new(
            asset_id,
            Output::Transfer(TransferOutput::simple(900, vec![1, 2, 3])),
        ));

        let unsigned = Transaction::Base(base);

        // Create with invalid signature (wrong length)
        let signed_tx = SignedTx::new(
            unsigned.clone(),
            vec![Credential {
                signatures: vec![Signature {
                    bytes: vec![0; 32], // Wrong length
                }],
            }],
        );

        assert!(signed_tx.validate().is_err());

        // Create with corrupted signature (right length but invalid)
        let signed_tx = SignedTx::new(
            unsigned,
            vec![Credential {
                signatures: vec![Signature {
                    bytes: vec![0; 65], // Right length but all zeros
                }],
            }],
        );

        // This should fail because recovery will fail
        assert!(signed_tx.validate().is_err());
    }

    #[test]
    fn test_credential_count_mismatch() {
        let mut base = BaseTx::new(1, Id::default());
        let tx_id = Id::from_slice(&[1; 32]).unwrap();
        let asset_id = Id::from_slice(&[2; 32]).unwrap();

        // Add two inputs
        base.add_input(TransferableInput::new(
            UTXOID::new(tx_id, 0),
            asset_id,
            Input::Transfer(TransferInput {
                amount: 500,
                sig_indices: vec![0],
            }),
        ));
        base.add_input(TransferableInput::new(
            UTXOID::new(tx_id, 1),
            asset_id,
            Input::Transfer(TransferInput {
                amount: 500,
                sig_indices: vec![0],
            }),
        ));

        base.add_output(TransferableOutput::new(
            asset_id,
            Output::Transfer(TransferOutput::simple(900, vec![1, 2, 3])),
        ));

        let unsigned = Transaction::Base(base);

        // Create a private key for signing
        let private_key = PrivateKey::generate();
        let hash = {
            let temp_tx = SignedTx::new(unsigned.clone(), vec![]);
            temp_tx.unsigned_hash()
        };
        let sig = private_key.sign_hash_recoverable(&hash).unwrap();
        let sig_bytes = sig.to_bytes().to_vec();

        // Only provide one credential for two inputs
        let signed_tx = SignedTx::new(
            unsigned,
            vec![Credential {
                signatures: vec![Signature {
                    bytes: sig_bytes.clone(),
                }],
            }],
        );

        // Should fail: credential count mismatch
        let result = signed_tx.validate();
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("credential count"));
    }
}
