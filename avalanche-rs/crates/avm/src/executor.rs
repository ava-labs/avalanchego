//! Transaction executor for the AVM.
//!
//! This module handles complete transaction validation and execution:
//! - Input verification (UTXOs exist and are spendable)
//! - Signature verification (correct signatures for threshold)
//! - Fee validation (sufficient fee paid)
//! - State transitions (consume inputs, create outputs)

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use avalanche_crypto::secp256k1::RecoverableSignature;
use avalanche_ids::Id;
use parking_lot::RwLock;
use thiserror::Error;

use crate::txs::{BaseTx, Credential, SignedTx, Signature, Transaction, TransferableInput, TransferableOutput};
use crate::utxo::{Input, Output, TransferInput, TransferOutput, UTXO, UTXOID};
use crate::utxo_state::{UTXOChanges, UTXOState, UTXOStateError};

/// Transaction execution errors.
#[derive(Debug, Error)]
pub enum ExecutionError {
    #[error("UTXO not found: {0:?}")]
    UTXONotFound(UTXOID),

    #[error("UTXO already spent: {0:?}")]
    UTXODoubleSpend(UTXOID),

    #[error("UTXO locked until {locktime}, current time {current}")]
    UTXOLocked { locktime: u64, current: u64 },

    #[error("Insufficient input: need {need}, have {have}")]
    InsufficientInput { need: u64, have: u64 },

    #[error("Insufficient fee: need {need}, have {have}")]
    InsufficientFee { need: u64, have: u64 },

    #[error("Invalid signature at index {index}")]
    InvalidSignature { index: usize },

    #[error("Insufficient signatures: need {threshold}, have {count}")]
    InsufficientSignatures { threshold: u32, count: usize },

    #[error("Asset mismatch: expected {expected}, got {got}")]
    AssetMismatch { expected: Id, got: Id },

    #[error("Invalid transaction: {0}")]
    InvalidTransaction(String),

    #[error("State error: {0}")]
    StateError(#[from] UTXOStateError),
}

/// Configuration for the executor.
#[derive(Debug, Clone)]
pub struct ExecutorConfig {
    /// Base transaction fee in nAVAX.
    pub base_fee: u64,
    /// Fee per byte of transaction.
    pub fee_per_byte: u64,
    /// AVAX asset ID.
    pub avax_asset_id: Id,
    /// Whether to verify signatures (can be disabled for testing).
    pub verify_signatures: bool,
}

impl Default for ExecutorConfig {
    fn default() -> Self {
        Self {
            base_fee: 1_000_000,      // 0.001 AVAX
            fee_per_byte: 1_000,      // 0.000001 AVAX per byte
            avax_asset_id: Id::default(),
            verify_signatures: true,
        }
    }
}

/// Transaction executor.
pub struct Executor {
    config: ExecutorConfig,
    state: Arc<UTXOState>,
    /// Currently executing transactions (for double-spend detection).
    pending_spends: RwLock<HashSet<UTXOID>>,
}

impl Executor {
    /// Creates a new executor.
    pub fn new(config: ExecutorConfig, state: Arc<UTXOState>) -> Self {
        Self {
            config,
            state,
            pending_spends: RwLock::new(HashSet::new()),
        }
    }

    /// Validates a transaction without executing it.
    pub fn validate(&self, tx: &SignedTx, current_time: u64) -> Result<(), ExecutionError> {
        // Verify the transaction structure
        // Only do full signature validation if signatures are enabled
        if self.config.verify_signatures {
            tx.validate().map_err(|e| ExecutionError::InvalidTransaction(e.to_string()))?;
        } else {
            // Just validate the unsigned part
            tx.unsigned.validate().map_err(|e| ExecutionError::InvalidTransaction(e.to_string()))?;
        }

        match &tx.unsigned {
            Transaction::Base(base_tx) => self.validate_base_tx(base_tx, &tx.credentials, current_time),
            Transaction::CreateAsset(create_tx) => {
                self.validate_base_tx(&create_tx.base, &tx.credentials, current_time)
            }
            Transaction::Operation(op_tx) => {
                self.validate_base_tx(&op_tx.base, &tx.credentials, current_time)
            }
            Transaction::Import(import_tx) => {
                self.validate_base_tx(&import_tx.base, &tx.credentials, current_time)
            }
            Transaction::Export(export_tx) => {
                self.validate_base_tx(&export_tx.base, &tx.credentials, current_time)
            }
        }
    }

    /// Validates a base transaction.
    fn validate_base_tx(
        &self,
        tx: &BaseTx,
        credentials: &[crate::txs::Credential],
        current_time: u64,
    ) -> Result<(), ExecutionError> {
        // Collect all inputs and their UTXOs
        let mut input_utxos = Vec::new();
        let mut total_input: HashMap<Id, u64> = HashMap::new();

        for (i, input) in tx.inputs.iter().enumerate() {
            // Check for double spend within pending
            if self.pending_spends.read().contains(&input.utxo_id) {
                return Err(ExecutionError::UTXODoubleSpend(input.utxo_id));
            }

            // Fetch the UTXO
            let utxo = self.state.get(&input.utxo_id)?
                .ok_or(ExecutionError::UTXONotFound(input.utxo_id))?;

            // Check locktime
            if !utxo.is_spendable(current_time) {
                return Err(ExecutionError::UTXOLocked {
                    locktime: utxo.locktime(),
                    current: current_time,
                });
            }

            // Validate input amount matches UTXO
            if let Some(utxo_amount) = utxo.amount() {
                if let crate::utxo::Input::Transfer(transfer) = &input.input {
                    if transfer.amount != utxo_amount {
                        return Err(ExecutionError::InvalidTransaction(
                            format!("Input {} amount mismatch: {} vs {}", i, transfer.amount, utxo_amount)
                        ));
                    }
                }
            }

            // Verify signature if enabled
            if self.config.verify_signatures && i < credentials.len() {
                self.verify_credential(&utxo, &credentials[i], tx)?;
            }

            // Accumulate input amounts
            if let Some(amount) = utxo.amount() {
                *total_input.entry(utxo.asset_id).or_insert(0) += amount;
            }

            input_utxos.push(utxo);
        }

        // Calculate output amounts
        let mut total_output: HashMap<Id, u64> = HashMap::new();
        for output in &tx.outputs {
            if let Some(amount) = output.output.amount() {
                *total_output.entry(output.asset_id).or_insert(0) += amount;
            }
        }

        // Verify input >= output for each asset
        for (asset_id, output_amount) in &total_output {
            let input_amount = total_input.get(asset_id).copied().unwrap_or(0);
            if input_amount < *output_amount {
                return Err(ExecutionError::InsufficientInput {
                    need: *output_amount,
                    have: input_amount,
                });
            }
        }

        // Verify fee (difference in AVAX)
        let avax_in = total_input.get(&self.config.avax_asset_id).copied().unwrap_or(0);
        let avax_out = total_output.get(&self.config.avax_asset_id).copied().unwrap_or(0);
        let fee_paid = avax_in.saturating_sub(avax_out);
        let fee_required = self.calculate_fee(tx);

        if fee_paid < fee_required {
            return Err(ExecutionError::InsufficientFee {
                need: fee_required,
                have: fee_paid,
            });
        }

        Ok(())
    }

    /// Verifies a credential against a UTXO.
    fn verify_credential(
        &self,
        utxo: &UTXO,
        credential: &crate::txs::Credential,
        tx: &BaseTx,
    ) -> Result<(), ExecutionError> {
        let addresses = utxo.addresses();
        let threshold = utxo.threshold();

        // Get the transaction hash to verify against
        let tx_hash = tx.unsigned_hash();

        // Verify we have enough signatures
        if credential.signatures.len() < threshold as usize {
            return Err(ExecutionError::InsufficientSignatures {
                threshold,
                count: credential.signatures.len(),
            });
        }

        // Verify each signature
        let mut verified_count = 0;
        for (i, sig) in credential.signatures.iter().enumerate() {
            let sig_bytes = &sig.bytes;
            if sig_bytes.len() != 65 {
                return Err(ExecutionError::InvalidSignature { index: i });
            }

            // Parse recoverable signature
            let signature = match RecoverableSignature::from_bytes(sig_bytes) {
                Ok(s) => s,
                Err(_) => return Err(ExecutionError::InvalidSignature { index: i }),
            };

            // Recover public key
            let pubkey = match signature.recover(&tx_hash) {
                Ok(pk) => pk,
                Err(_) => return Err(ExecutionError::InvalidSignature { index: i }),
            };

            // Derive address from public key
            let address = pubkey.to_address().to_vec();

            // Check if this address is authorized
            if addresses.iter().any(|a| a == &address) {
                verified_count += 1;
            }
        }

        if verified_count < threshold as usize {
            return Err(ExecutionError::InsufficientSignatures {
                threshold,
                count: verified_count,
            });
        }

        Ok(())
    }

    /// Calculates the required fee for a transaction.
    pub fn calculate_fee(&self, tx: &BaseTx) -> u64 {
        // Estimate transaction size
        let tx_size = tx.inputs.len() * 64 + tx.outputs.len() * 80 + 100;
        self.config.base_fee + (tx_size as u64 * self.config.fee_per_byte)
    }

    /// Executes a validated transaction, updating state.
    pub fn execute(&self, tx: &SignedTx, current_time: u64) -> Result<ExecutionResult, ExecutionError> {
        // Validate first
        self.validate(tx, current_time)?;

        // Mark inputs as pending
        let inputs_to_spend = self.collect_inputs(tx);
        {
            let mut pending = self.pending_spends.write();
            for id in &inputs_to_spend {
                pending.insert(*id);
            }
        }

        // Execute based on transaction type
        let result = match &tx.unsigned {
            Transaction::Base(base_tx) => self.execute_base_tx(tx, base_tx),
            Transaction::CreateAsset(create_tx) => self.execute_create_asset_tx(tx, create_tx),
            Transaction::Operation(op_tx) => self.execute_operation_tx(tx, op_tx),
            Transaction::Import(import_tx) => self.execute_import_tx(tx, import_tx),
            Transaction::Export(export_tx) => self.execute_export_tx(tx, export_tx),
        };

        // Clear pending spends
        {
            let mut pending = self.pending_spends.write();
            for id in &inputs_to_spend {
                pending.remove(id);
            }
        }

        result
    }

    /// Collects all input UTXO IDs from a transaction.
    fn collect_inputs(&self, tx: &SignedTx) -> Vec<UTXOID> {
        match &tx.unsigned {
            Transaction::Base(base_tx) => base_tx.inputs.iter().map(|i| i.utxo_id).collect(),
            Transaction::CreateAsset(create_tx) => create_tx.base.inputs.iter().map(|i| i.utxo_id).collect(),
            Transaction::Operation(op_tx) => {
                let mut inputs: Vec<_> = op_tx.base.inputs.iter().map(|i| i.utxo_id).collect();
                for op in &op_tx.operations {
                    inputs.extend(op.utxo_ids.iter().cloned());
                }
                inputs
            }
            Transaction::Import(import_tx) => import_tx.base.inputs.iter().map(|i| i.utxo_id).collect(),
            Transaction::Export(export_tx) => export_tx.base.inputs.iter().map(|i| i.utxo_id).collect(),
        }
    }

    /// Executes a base transaction.
    fn execute_base_tx(&self, signed_tx: &SignedTx, tx: &BaseTx) -> Result<ExecutionResult, ExecutionError> {
        let tx_id = signed_tx.id();
        let mut changes = UTXOChanges::new();
        let mut fee_burned = 0u64;

        // Calculate total inputs
        let mut total_input: HashMap<Id, u64> = HashMap::new();
        for input in &tx.inputs {
            let utxo = self.state.get(&input.utxo_id)?.unwrap();
            if let Some(amount) = utxo.amount() {
                *total_input.entry(utxo.asset_id).or_insert(0) += amount;
            }
            changes.remove(input.utxo_id);
        }

        // Create output UTXOs
        let mut total_output: HashMap<Id, u64> = HashMap::new();
        for (idx, output) in tx.outputs.iter().enumerate() {
            let utxo = UTXO::new(
                UTXOID::new(tx_id, idx as u32),
                output.asset_id,
                output.output.clone(),
            );
            if let Some(amount) = output.output.amount() {
                *total_output.entry(output.asset_id).or_insert(0) += amount;
            }
            changes.add(utxo);
        }

        // Calculate burned fee
        let avax_in = total_input.get(&self.config.avax_asset_id).copied().unwrap_or(0);
        let avax_out = total_output.get(&self.config.avax_asset_id).copied().unwrap_or(0);
        fee_burned = avax_in.saturating_sub(avax_out);

        // Apply changes
        self.state.apply_batch(changes)?;

        Ok(ExecutionResult {
            tx_id,
            fee_burned,
            utxos_created: tx.outputs.len(),
            utxos_consumed: tx.inputs.len(),
        })
    }

    /// Executes a create asset transaction.
    fn execute_create_asset_tx(
        &self,
        signed_tx: &SignedTx,
        tx: &crate::txs::CreateAssetTx,
    ) -> Result<ExecutionResult, ExecutionError> {
        // For create asset, the asset ID is the transaction ID
        let tx_id = signed_tx.id();
        let mut changes = UTXOChanges::new();

        // Remove inputs
        for input in &tx.base.inputs {
            changes.remove(input.utxo_id);
        }

        // Create outputs (with the new asset ID = tx ID)
        for (idx, output) in tx.base.outputs.iter().enumerate() {
            let utxo = UTXO::new(
                UTXOID::new(tx_id, idx as u32),
                tx_id, // Asset ID is the tx ID for new assets
                output.output.clone(),
            );
            changes.add(utxo);
        }

        // Apply changes
        self.state.apply_batch(changes)?;

        Ok(ExecutionResult {
            tx_id,
            fee_burned: 0, // Would calculate from inputs - outputs
            utxos_created: tx.base.outputs.len(),
            utxos_consumed: tx.base.inputs.len(),
        })
    }

    /// Executes an operation transaction.
    fn execute_operation_tx(
        &self,
        signed_tx: &SignedTx,
        tx: &crate::txs::OperationTx,
    ) -> Result<ExecutionResult, ExecutionError> {
        let tx_id = signed_tx.id();
        let mut changes = UTXOChanges::new();
        let mut utxos_consumed = 0;

        // Remove base inputs
        for input in &tx.base.inputs {
            changes.remove(input.utxo_id);
            utxos_consumed += 1;
        }

        // Remove operation inputs
        for op in &tx.operations {
            for utxo_id in &op.utxo_ids {
                changes.remove(*utxo_id);
                utxos_consumed += 1;
            }
        }

        // Create outputs
        for (idx, output) in tx.base.outputs.iter().enumerate() {
            let utxo = UTXO::new(
                UTXOID::new(tx_id, idx as u32),
                output.asset_id,
                output.output.clone(),
            );
            changes.add(utxo);
        }

        self.state.apply_batch(changes)?;

        Ok(ExecutionResult {
            tx_id,
            fee_burned: 0,
            utxos_created: tx.base.outputs.len(),
            utxos_consumed,
        })
    }

    /// Executes an import transaction.
    fn execute_import_tx(
        &self,
        signed_tx: &SignedTx,
        tx: &crate::txs::ImportTx,
    ) -> Result<ExecutionResult, ExecutionError> {
        let tx_id = signed_tx.id();
        let mut changes = UTXOChanges::new();

        // Remove local inputs
        for input in &tx.base.inputs {
            changes.remove(input.utxo_id);
        }

        // Note: Imported UTXOs from source chain are verified separately
        // (would need cross-chain verification in production)

        // Create outputs
        for (idx, output) in tx.base.outputs.iter().enumerate() {
            let utxo = UTXO::new(
                UTXOID::new(tx_id, idx as u32),
                output.asset_id,
                output.output.clone(),
            );
            changes.add(utxo);
        }

        self.state.apply_batch(changes)?;

        Ok(ExecutionResult {
            tx_id,
            fee_burned: 0,
            utxos_created: tx.base.outputs.len(),
            utxos_consumed: tx.base.inputs.len() + tx.imported_inputs.len(),
        })
    }

    /// Executes an export transaction.
    fn execute_export_tx(
        &self,
        signed_tx: &SignedTx,
        tx: &crate::txs::ExportTx,
    ) -> Result<ExecutionResult, ExecutionError> {
        let tx_id = signed_tx.id();
        let mut changes = UTXOChanges::new();

        // Remove inputs
        for input in &tx.base.inputs {
            changes.remove(input.utxo_id);
        }

        // Create local outputs (exported outputs go to destination chain)
        for (idx, output) in tx.base.outputs.iter().enumerate() {
            let utxo = UTXO::new(
                UTXOID::new(tx_id, idx as u32),
                output.asset_id,
                output.output.clone(),
            );
            changes.add(utxo);
        }

        // Note: Exported outputs would be recorded for the destination chain
        // to pick up later

        self.state.apply_batch(changes)?;

        Ok(ExecutionResult {
            tx_id,
            fee_burned: 0,
            utxos_created: tx.base.outputs.len(),
            utxos_consumed: tx.base.inputs.len(),
        })
    }

    /// Commits all pending changes to the database.
    pub fn commit(&self) -> Result<[u8; 32], ExecutionError> {
        let root = self.state.commit()?;
        Ok(root)
    }
}

/// Result of executing a transaction.
#[derive(Debug, Clone)]
pub struct ExecutionResult {
    /// Transaction ID.
    pub tx_id: Id,
    /// Fee burned in nAVAX.
    pub fee_burned: u64,
    /// Number of UTXOs created.
    pub utxos_created: usize,
    /// Number of UTXOs consumed.
    pub utxos_consumed: usize,
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_db::MemDb;

    fn setup() -> (Arc<UTXOState>, Executor) {
        let db = Arc::new(MemDb::new());
        let state = Arc::new(UTXOState::new(db).unwrap());
        let config = ExecutorConfig {
            verify_signatures: false, // Disable for testing
            ..Default::default()
        };
        let executor = Executor::new(config, state.clone());
        (state, executor)
    }

    fn make_utxo(tx_byte: u8, index: u32, amount: u64, asset_id: Id, address: Vec<u8>) -> UTXO {
        let tx_id = Id::from_slice(&[tx_byte; 32]).unwrap();
        UTXO::new(
            UTXOID::new(tx_id, index),
            asset_id,
            Output::Transfer(TransferOutput::new(amount, 0, 1, vec![address])),
        )
    }

    fn dummy_sig() -> Signature {
        Signature { bytes: vec![0; 65] }
    }

    #[test]
    fn test_simple_transfer() {
        let (state, executor) = setup();
        let avax = executor.config.avax_asset_id;
        let address1 = vec![0x01; 20];
        let address2 = vec![0x02; 20];

        // Add initial UTXO
        let utxo = make_utxo(1, 0, 10_000_000_000, avax, address1.clone());
        state.add(utxo.clone()).unwrap();

        // Create transaction: spend 5 AVAX, keep 4.99 AVAX (0.01 fee)
        let tx = SignedTx {
            unsigned: Transaction::Base(BaseTx {
                network_id: 1,
                blockchain_id: Id::default(),
                inputs: vec![TransferableInput {
                    utxo_id: UTXOID::new(Id::from_slice(&[1; 32]).unwrap(), 0),
                    asset_id: avax,
                    input: Input::Transfer(TransferInput {
                        amount: 10_000_000_000,
                        sig_indices: vec![0],
                    }),
                }],
                outputs: vec![
                    TransferableOutput {
                        asset_id: avax,
                        output: Output::Transfer(TransferOutput::new(
                            5_000_000_000,
                            0,
                            1,
                            vec![address2],
                        )),
                    },
                    TransferableOutput {
                        asset_id: avax,
                        output: Output::Transfer(TransferOutput::new(
                            4_990_000_000, // Change minus fee
                            0,
                            1,
                            vec![address1],
                        )),
                    },
                ],
                memo: vec![],
            }),
            credentials: vec![Credential {
                signatures: vec![dummy_sig()],
            }],
        };

        // Execute
        let result = executor.execute(&tx, 0).unwrap();
        assert_eq!(result.utxos_consumed, 1);
        assert_eq!(result.utxos_created, 2);
        assert_eq!(result.fee_burned, 10_000_000);

        // Verify state
        assert!(!state.contains(&UTXOID::new(Id::from_slice(&[1; 32]).unwrap(), 0)).unwrap());
        assert_eq!(state.count(), 2);
    }

    #[test]
    fn test_insufficient_balance() {
        let (state, executor) = setup();
        let avax = executor.config.avax_asset_id;
        let address = vec![0x01; 20];

        // Add initial UTXO with small amount
        let utxo = make_utxo(1, 0, 1_000_000, avax, address.clone()); // 0.001 AVAX
        state.add(utxo).unwrap();

        // Try to spend more than we have
        let tx = SignedTx {
            unsigned: Transaction::Base(BaseTx {
                network_id: 1,
                blockchain_id: Id::default(),
                inputs: vec![TransferableInput {
                    utxo_id: UTXOID::new(Id::from_slice(&[1; 32]).unwrap(), 0),
                    asset_id: avax,
                    input: Input::Transfer(TransferInput {
                        amount: 1_000_000,
                        sig_indices: vec![0],
                    }),
                }],
                outputs: vec![TransferableOutput {
                    asset_id: avax,
                    output: Output::Transfer(TransferOutput::new(
                        2_000_000, // More than input
                        0,
                        1,
                        vec![address],
                    )),
                }],
                memo: vec![],
            }),
            credentials: vec![Credential { signatures: vec![dummy_sig()] }],
        };

        let result = executor.validate(&tx, 0);
        assert!(matches!(result, Err(ExecutionError::InsufficientInput { .. })));
    }

    #[test]
    fn test_double_spend_detection() {
        let (state, executor) = setup();
        let avax = executor.config.avax_asset_id;
        let address = vec![0x01; 20];

        // Add UTXO
        let utxo = make_utxo(1, 0, 10_000_000_000, avax, address.clone());
        state.add(utxo).unwrap();

        let utxo_id = UTXOID::new(Id::from_slice(&[1; 32]).unwrap(), 0);

        // Spend it
        let tx1 = SignedTx {
            unsigned: Transaction::Base(BaseTx {
                network_id: 1,
                blockchain_id: Id::default(),
                inputs: vec![TransferableInput {
                    utxo_id,
                    asset_id: avax,
                    input: Input::Transfer(TransferInput {
                        amount: 10_000_000_000,
                        sig_indices: vec![0],
                    }),
                }],
                outputs: vec![TransferableOutput {
                    asset_id: avax,
                    output: Output::Transfer(TransferOutput::new(
                        9_990_000_000,
                        0,
                        1,
                        vec![address.clone()],
                    )),
                }],
                memo: vec![],
            }),
            credentials: vec![Credential { signatures: vec![dummy_sig()] }],
        };

        executor.execute(&tx1, 0).unwrap();

        // Try to spend it again
        let tx2 = tx1.clone();
        let result = executor.validate(&tx2, 0);
        assert!(matches!(result, Err(ExecutionError::UTXONotFound(_))));
    }

    #[test]
    fn test_locked_utxo() {
        let (state, executor) = setup();
        let avax = executor.config.avax_asset_id;
        let address = vec![0x01; 20];

        // Add locked UTXO (locked until time 1000)
        let tx_id = Id::from_slice(&[1; 32]).unwrap();
        let utxo = UTXO::new(
            UTXOID::new(tx_id, 0),
            avax,
            Output::Transfer(TransferOutput::new(10_000_000_000, 1000, 1, vec![address.clone()])),
        );
        state.add(utxo).unwrap();

        let tx = SignedTx {
            unsigned: Transaction::Base(BaseTx {
                network_id: 1,
                blockchain_id: Id::default(),
                inputs: vec![TransferableInput {
                    utxo_id: UTXOID::new(tx_id, 0),
                    asset_id: avax,
                    input: Input::Transfer(TransferInput {
                        amount: 10_000_000_000,
                        sig_indices: vec![0],
                    }),
                }],
                outputs: vec![TransferableOutput {
                    asset_id: avax,
                    output: Output::Transfer(TransferOutput::new(
                        9_990_000_000,
                        0,
                        1,
                        vec![address],
                    )),
                }],
                memo: vec![],
            }),
            credentials: vec![Credential { signatures: vec![dummy_sig()] }],
        };

        // Should fail at time 500
        let result = executor.validate(&tx, 500);
        assert!(matches!(result, Err(ExecutionError::UTXOLocked { .. })));

        // Should succeed at time 1000
        let result = executor.validate(&tx, 1000);
        assert!(result.is_ok());
    }

    #[test]
    fn test_fee_calculation() {
        let (_, executor) = setup();

        let tx = BaseTx {
            network_id: 1,
            blockchain_id: Id::default(),
            inputs: vec![],
            outputs: vec![],
            memo: vec![],
        };

        let fee = executor.calculate_fee(&tx);
        assert!(fee > 0);
        assert!(fee >= executor.config.base_fee);
    }
}
