//! Platform VM transaction types.

use avalanche_ids::{Id, NodeId};
use avalanche_vm::{Result, VMError};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// UTXO (Unspent Transaction Output).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UTXO {
    /// Transaction ID
    pub tx_id: Id,
    /// Output index
    pub output_index: u32,
    /// Asset ID
    pub asset_id: Id,
    /// Output data
    pub output: Output,
}

/// Transaction output.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Output {
    /// Transferable output
    Transfer(TransferOutput),
    /// Staked output (locked)
    Staked(StakedOutput),
}

/// Transferable output.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransferOutput {
    /// Amount
    pub amount: u64,
    /// Lock time (0 = unlocked)
    pub locktime: u64,
    /// Threshold for spending
    pub threshold: u32,
    /// Addresses that can spend
    pub addresses: Vec<Vec<u8>>,
}

/// Staked output.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StakedOutput {
    /// Amount staked
    pub amount: u64,
    /// Locktime (when stake unlocks)
    pub locktime: u64,
    /// Addresses
    pub addresses: Vec<Vec<u8>>,
}

/// Transaction input.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Input {
    /// UTXO ID being spent
    pub utxo_id: (Id, u32),
    /// Signature indices
    pub sig_indices: Vec<u32>,
}

/// Transaction types.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum Transaction {
    /// Add a validator to the primary network
    AddValidator(AddValidatorTx),
    /// Add a delegator to an existing validator
    AddDelegator(AddDelegatorTx),
    /// Create a new subnet
    CreateSubnet(CreateSubnetTx),
    /// Create a new chain on a subnet
    CreateChain(CreateChainTx),
    /// Import AVAX from X/C chain
    Import(ImportTx),
    /// Export AVAX to X/C chain
    Export(ExportTx),
    /// Add a validator to a subnet
    AddSubnetValidator(AddSubnetValidatorTx),
    /// Reward a completed validator
    RewardValidator(RewardValidatorTx),
}

/// Add validator transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AddValidatorTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferOutput>,
    /// Inputs
    pub inputs: Vec<Input>,
    /// Memo
    pub memo: Vec<u8>,
    /// Validator info
    pub validator: ValidatorInfo,
    /// Stake outputs
    pub stake_outputs: Vec<StakedOutput>,
    /// Rewards owner
    pub rewards_owner: OutputOwner,
    /// Delegation shares (reward split)
    pub shares: u32,
}

/// Validator info for transactions.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ValidatorInfo {
    /// Node ID
    pub node_id: NodeId,
    /// Start time
    pub start_time: u64,
    /// End time
    pub end_time: u64,
    /// Weight (stake)
    pub weight: u64,
}

/// Output owner definition.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OutputOwner {
    /// Locktime
    pub locktime: u64,
    /// Threshold
    pub threshold: u32,
    /// Addresses
    pub addresses: Vec<Vec<u8>>,
}

/// Add delegator transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AddDelegatorTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferOutput>,
    /// Inputs
    pub inputs: Vec<Input>,
    /// Memo
    pub memo: Vec<u8>,
    /// Validator being delegated to
    pub validator: ValidatorInfo,
    /// Stake outputs
    pub stake_outputs: Vec<StakedOutput>,
    /// Rewards owner
    pub rewards_owner: OutputOwner,
}

/// Create subnet transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateSubnetTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferOutput>,
    /// Inputs
    pub inputs: Vec<Input>,
    /// Memo
    pub memo: Vec<u8>,
    /// Subnet owner
    pub owner: OutputOwner,
}

/// Create chain transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateChainTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferOutput>,
    /// Inputs
    pub inputs: Vec<Input>,
    /// Memo
    pub memo: Vec<u8>,
    /// Subnet ID
    pub subnet_id: Id,
    /// Chain name
    pub chain_name: String,
    /// VM ID
    pub vm_id: Id,
    /// Feature extensions
    pub fx_ids: Vec<Id>,
    /// Genesis data
    pub genesis_data: Vec<u8>,
    /// Subnet authorization
    pub subnet_auth: SubnetAuth,
}

/// Subnet authorization.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubnetAuth {
    /// Signature indices
    pub sig_indices: Vec<u32>,
}

/// Import transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ImportTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferOutput>,
    /// Inputs
    pub inputs: Vec<Input>,
    /// Memo
    pub memo: Vec<u8>,
    /// Source chain
    pub source_chain: Id,
    /// Imported inputs
    pub imported_inputs: Vec<Input>,
}

/// Export transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExportTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferOutput>,
    /// Inputs
    pub inputs: Vec<Input>,
    /// Memo
    pub memo: Vec<u8>,
    /// Destination chain
    pub destination_chain: Id,
    /// Exported outputs
    pub exported_outputs: Vec<TransferOutput>,
}

/// Add subnet validator transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AddSubnetValidatorTx {
    /// Network ID
    pub network_id: u32,
    /// Blockchain ID
    pub blockchain_id: Id,
    /// Outputs
    pub outputs: Vec<TransferOutput>,
    /// Inputs
    pub inputs: Vec<Input>,
    /// Memo
    pub memo: Vec<u8>,
    /// Validator
    pub validator: SubnetValidator,
    /// Subnet authorization
    pub subnet_auth: SubnetAuth,
}

/// Subnet validator info.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SubnetValidator {
    /// Node ID
    pub node_id: NodeId,
    /// Start time
    pub start_time: u64,
    /// End time
    pub end_time: u64,
    /// Weight
    pub weight: u64,
    /// Subnet ID
    pub subnet_id: Id,
}

/// Reward validator transaction.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RewardValidatorTx {
    /// Transaction ID of the AddValidator/AddDelegator tx
    pub tx_id: Id,
}

impl Transaction {
    /// Returns the transaction ID.
    pub fn id(&self) -> Id {
        // Would compute hash of serialized tx
        Id::default()
    }

    /// Validates the transaction.
    pub fn validate(&self) -> Result<()> {
        match self {
            Transaction::AddValidator(tx) => tx.validate(),
            Transaction::AddDelegator(tx) => tx.validate(),
            Transaction::CreateSubnet(tx) => tx.validate(),
            _ => Ok(()),
        }
    }
}

impl AddValidatorTx {
    /// Validates the add validator transaction.
    pub fn validate(&self) -> Result<()> {
        use crate::validator::stake;

        // Check stake amount
        let total_stake: u64 = self.stake_outputs.iter().map(|o| o.amount).sum();
        if total_stake < stake::MIN_VALIDATOR_STAKE {
            return Err(VMError::InvalidParameter(format!(
                "stake {} below minimum {}",
                total_stake,
                stake::MIN_VALIDATOR_STAKE
            )));
        }

        // Check duration
        let duration = self.validator.end_time - self.validator.start_time;
        if duration < stake::MIN_VALIDATION_DURATION_SECS {
            return Err(VMError::InvalidParameter("validation duration too short".to_string()));
        }
        if duration > stake::MAX_VALIDATION_DURATION_SECS {
            return Err(VMError::InvalidParameter("validation duration too long".to_string()));
        }

        // Check delegation fee
        if self.shares > 10000 {
            return Err(VMError::InvalidParameter("invalid delegation shares".to_string()));
        }

        Ok(())
    }
}

impl AddDelegatorTx {
    /// Validates the add delegator transaction.
    pub fn validate(&self) -> Result<()> {
        use crate::validator::stake;

        let total_stake: u64 = self.stake_outputs.iter().map(|o| o.amount).sum();
        if total_stake < stake::MIN_DELEGATOR_STAKE {
            return Err(VMError::InvalidParameter(format!(
                "stake {} below minimum {}",
                total_stake,
                stake::MIN_DELEGATOR_STAKE
            )));
        }

        Ok(())
    }
}

impl CreateSubnetTx {
    /// Validates the create subnet transaction.
    pub fn validate(&self) -> Result<()> {
        if self.owner.threshold == 0 {
            return Err(VMError::InvalidParameter("threshold must be > 0".to_string()));
        }
        if self.owner.addresses.is_empty() {
            return Err(VMError::InvalidParameter("must have at least one address".to_string()));
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_utxo() {
        let utxo = UTXO {
            tx_id: Id::default(),
            output_index: 0,
            asset_id: Id::default(),
            output: Output::Transfer(TransferOutput {
                amount: 1000,
                locktime: 0,
                threshold: 1,
                addresses: vec![vec![1, 2, 3]],
            }),
        };

        assert_eq!(utxo.output_index, 0);
    }

    #[test]
    fn test_validator_info() {
        let node_id = NodeId::from_slice(&[1; 20]).unwrap();
        let info = ValidatorInfo {
            node_id,
            start_time: 1000,
            end_time: 2000,
            weight: 100,
        };

        assert_eq!(info.weight, 100);
    }
}
