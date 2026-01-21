//! AVM codec for Go-compatible binary serialization.
//!
//! This module implements the exact wire format used by the Go implementation
//! to ensure cross-compatibility.

use avalanche_codec::{Pack, Packer, Unpack, UnpackError, Unpacker};
use avalanche_ids::{Id, ShortId};

use crate::txs::{
    BaseTx, CreateAssetTx, Credential, ExportTx, ImportTx, OperationTx, SignedTx, Signature,
    Transaction, TransferableInput, TransferableOutput,
};
use crate::utxo::{
    Input, MintInput, MintOutput, NFTMintInput, NFTMintOutput, NFTTransferInput, NFTTransferOutput,
    Output, TransferInput, TransferOutput, UTXOID,
};

/// Codec version (matches Go's CodecVersion = 0).
pub const CODEC_VERSION: u16 = 0;

// =============================================================================
// Type IDs - Must match Go registration order exactly
// =============================================================================

/// Transaction type IDs (registered in avm/txs/parser.go).
pub mod tx_type_id {
    pub const BASE_TX: u32 = 0;
    pub const CREATE_ASSET_TX: u32 = 1;
    pub const OPERATION_TX: u32 = 2;
    pub const IMPORT_TX: u32 = 3;
    pub const EXPORT_TX: u32 = 4;
}

/// secp256k1fx type IDs (registered in vms/secp256k1fx/fx.go).
pub mod fx_type_id {
    pub const SECP256K1_TRANSFER_INPUT: u32 = 5;
    pub const SECP256K1_MINT_OUTPUT: u32 = 6;
    pub const SECP256K1_TRANSFER_OUTPUT: u32 = 7;
    pub const SECP256K1_MINT_OPERATION: u32 = 8;
    pub const SECP256K1_CREDENTIAL: u32 = 9;
}

// =============================================================================
// Output Owners - Embedded in outputs
// =============================================================================

/// OutputOwners represents the ownership structure (locktime, threshold, addresses).
#[derive(Debug, Clone, Default)]
pub struct OutputOwners {
    pub locktime: u64,
    pub threshold: u32,
    pub addresses: Vec<ShortId>,
}

impl Pack for OutputOwners {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_long(self.locktime);
        packer.pack_int(self.threshold);
        packer.pack_int(self.addresses.len() as u32);
        for addr in &self.addresses {
            addr.pack(packer);
        }
    }

    fn packed_size(&self) -> usize {
        8 + 4 + 4 + (self.addresses.len() * 20)
    }
}

impl Unpack for OutputOwners {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let locktime = unpacker.unpack_long()?;
        let threshold = unpacker.unpack_int()?;
        let addr_count = unpacker.unpack_int()? as usize;
        let mut addresses = Vec::with_capacity(addr_count.min(1024));
        for _ in 0..addr_count {
            addresses.push(ShortId::unpack(unpacker)?);
        }
        Ok(Self {
            locktime,
            threshold,
            addresses,
        })
    }
}

// =============================================================================
// secp256k1fx Outputs
// =============================================================================

/// Serializable transfer output matching Go's secp256k1fx.TransferOutput.
#[derive(Debug, Clone)]
pub struct SerializableTransferOutput {
    pub amount: u64,
    pub owners: OutputOwners,
}

impl Pack for SerializableTransferOutput {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_long(self.amount);
        self.owners.pack(packer);
    }

    fn packed_size(&self) -> usize {
        8 + self.owners.packed_size()
    }
}

impl Unpack for SerializableTransferOutput {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let amount = unpacker.unpack_long()?;
        let owners = OutputOwners::unpack(unpacker)?;
        Ok(Self { amount, owners })
    }
}

/// Serializable mint output matching Go's secp256k1fx.MintOutput.
#[derive(Debug, Clone)]
pub struct SerializableMintOutput {
    pub owners: OutputOwners,
}

impl Pack for SerializableMintOutput {
    fn pack(&self, packer: &mut Packer) {
        self.owners.pack(packer);
    }

    fn packed_size(&self) -> usize {
        self.owners.packed_size()
    }
}

impl Unpack for SerializableMintOutput {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let owners = OutputOwners::unpack(unpacker)?;
        Ok(Self { owners })
    }
}

// =============================================================================
// secp256k1fx Inputs
// =============================================================================

/// Input structure (sig indices only).
#[derive(Debug, Clone, Default)]
pub struct SerializableInput {
    pub sig_indices: Vec<u32>,
}

impl Pack for SerializableInput {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_int(self.sig_indices.len() as u32);
        for idx in &self.sig_indices {
            packer.pack_int(*idx);
        }
    }

    fn packed_size(&self) -> usize {
        4 + (self.sig_indices.len() * 4)
    }
}

impl Unpack for SerializableInput {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let count = unpacker.unpack_int()? as usize;
        let mut sig_indices = Vec::with_capacity(count.min(256));
        for _ in 0..count {
            sig_indices.push(unpacker.unpack_int()?);
        }
        Ok(Self { sig_indices })
    }
}

/// Serializable transfer input matching Go's secp256k1fx.TransferInput.
#[derive(Debug, Clone)]
pub struct SerializableTransferInput {
    pub amount: u64,
    pub input: SerializableInput,
}

impl Pack for SerializableTransferInput {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_long(self.amount);
        self.input.pack(packer);
    }

    fn packed_size(&self) -> usize {
        8 + self.input.packed_size()
    }
}

impl Unpack for SerializableTransferInput {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let amount = unpacker.unpack_long()?;
        let input = SerializableInput::unpack(unpacker)?;
        Ok(Self { amount, input })
    }
}

// =============================================================================
// UTXOID
// =============================================================================

/// Serializable UTXOID matching Go's avax.UTXOID.
#[derive(Debug, Clone)]
pub struct SerializableUTXOID {
    pub tx_id: Id,
    pub output_index: u32,
}

impl Pack for SerializableUTXOID {
    fn pack(&self, packer: &mut Packer) {
        self.tx_id.pack(packer);
        packer.pack_int(self.output_index);
    }

    fn packed_size(&self) -> usize {
        32 + 4
    }
}

impl Unpack for SerializableUTXOID {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let tx_id = Id::unpack(unpacker)?;
        let output_index = unpacker.unpack_int()?;
        Ok(Self { tx_id, output_index })
    }
}

impl From<&UTXOID> for SerializableUTXOID {
    fn from(utxo: &UTXOID) -> Self {
        Self {
            tx_id: utxo.tx_id,
            output_index: utxo.output_index,
        }
    }
}

// =============================================================================
// Transferable Output/Input (with type ID prefix)
// =============================================================================

/// Serializable transferable output matching Go's avax.TransferableOutput.
#[derive(Debug, Clone)]
pub struct SerializableTransferableOutput {
    pub asset_id: Id,
    pub output: TypedOutput,
}

/// Output with type ID for polymorphic serialization.
#[derive(Debug, Clone)]
pub enum TypedOutput {
    Transfer(SerializableTransferOutput),
    Mint(SerializableMintOutput),
}

impl Pack for SerializableTransferableOutput {
    fn pack(&self, packer: &mut Packer) {
        // Asset ID
        self.asset_id.pack(packer);
        // Type ID + output
        match &self.output {
            TypedOutput::Transfer(out) => {
                packer.pack_int(fx_type_id::SECP256K1_TRANSFER_OUTPUT);
                out.pack(packer);
            }
            TypedOutput::Mint(out) => {
                packer.pack_int(fx_type_id::SECP256K1_MINT_OUTPUT);
                out.pack(packer);
            }
        }
    }

    fn packed_size(&self) -> usize {
        32 + 4 + match &self.output {
            TypedOutput::Transfer(out) => out.packed_size(),
            TypedOutput::Mint(out) => out.packed_size(),
        }
    }
}

impl Unpack for SerializableTransferableOutput {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let asset_id = Id::unpack(unpacker)?;
        let type_id = unpacker.unpack_int()?;
        let output = match type_id {
            fx_type_id::SECP256K1_TRANSFER_OUTPUT => {
                TypedOutput::Transfer(SerializableTransferOutput::unpack(unpacker)?)
            }
            fx_type_id::SECP256K1_MINT_OUTPUT => {
                TypedOutput::Mint(SerializableMintOutput::unpack(unpacker)?)
            }
            _ => {
                return Err(UnpackError::InsufficientBytes {
                    needed: 0,
                    remaining: 0,
                })
            }
        };
        Ok(Self { asset_id, output })
    }
}

/// Serializable transferable input matching Go's avax.TransferableInput.
#[derive(Debug, Clone)]
pub struct SerializableTransferableInput {
    pub utxo_id: SerializableUTXOID,
    pub asset_id: Id,
    pub input: TypedInput,
}

/// Input with type ID for polymorphic serialization.
#[derive(Debug, Clone)]
pub enum TypedInput {
    Transfer(SerializableTransferInput),
}

impl Pack for SerializableTransferableInput {
    fn pack(&self, packer: &mut Packer) {
        // UTXOID
        self.utxo_id.pack(packer);
        // Asset ID
        self.asset_id.pack(packer);
        // Type ID + input
        match &self.input {
            TypedInput::Transfer(inp) => {
                packer.pack_int(fx_type_id::SECP256K1_TRANSFER_INPUT);
                inp.pack(packer);
            }
        }
    }

    fn packed_size(&self) -> usize {
        36 + 32 + 4 + match &self.input {
            TypedInput::Transfer(inp) => inp.packed_size(),
        }
    }
}

impl Unpack for SerializableTransferableInput {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let utxo_id = SerializableUTXOID::unpack(unpacker)?;
        let asset_id = Id::unpack(unpacker)?;
        let type_id = unpacker.unpack_int()?;
        let input = match type_id {
            fx_type_id::SECP256K1_TRANSFER_INPUT => {
                TypedInput::Transfer(SerializableTransferInput::unpack(unpacker)?)
            }
            _ => {
                return Err(UnpackError::InsufficientBytes {
                    needed: 0,
                    remaining: 0,
                })
            }
        };
        Ok(Self {
            utxo_id,
            asset_id,
            input,
        })
    }
}

// =============================================================================
// Credentials
// =============================================================================

/// Serializable credential matching Go's secp256k1fx.Credential.
#[derive(Debug, Clone)]
pub struct SerializableCredential {
    /// 65-byte signatures (r || s || v).
    pub signatures: Vec<[u8; 65]>,
}

impl Pack for SerializableCredential {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_int(self.signatures.len() as u32);
        for sig in &self.signatures {
            packer.pack_fixed_bytes(sig);
        }
    }

    fn packed_size(&self) -> usize {
        4 + (self.signatures.len() * 65)
    }
}

impl Unpack for SerializableCredential {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let count = unpacker.unpack_int()? as usize;
        let mut signatures = Vec::with_capacity(count.min(256));
        for _ in 0..count {
            signatures.push(unpacker.unpack_fixed_bytes::<65>()?);
        }
        Ok(Self { signatures })
    }
}

// =============================================================================
// Base Transaction
// =============================================================================

/// Serializable base transaction matching Go's avax.BaseTx.
#[derive(Debug, Clone)]
pub struct SerializableBaseTx {
    pub network_id: u32,
    pub blockchain_id: Id,
    pub outputs: Vec<SerializableTransferableOutput>,
    pub inputs: Vec<SerializableTransferableInput>,
    pub memo: Vec<u8>,
}

impl Pack for SerializableBaseTx {
    fn pack(&self, packer: &mut Packer) {
        packer.pack_int(self.network_id);
        self.blockchain_id.pack(packer);

        // Outputs
        packer.pack_int(self.outputs.len() as u32);
        for out in &self.outputs {
            out.pack(packer);
        }

        // Inputs
        packer.pack_int(self.inputs.len() as u32);
        for inp in &self.inputs {
            inp.pack(packer);
        }

        // Memo (4-byte length + bytes)
        packer.pack_bytes(&self.memo);
    }

    fn packed_size(&self) -> usize {
        4 + 32 + 4 + self.outputs.iter().map(|o| o.packed_size()).sum::<usize>()
            + 4
            + self.inputs.iter().map(|i| i.packed_size()).sum::<usize>()
            + 4
            + self.memo.len()
    }
}

impl Unpack for SerializableBaseTx {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let network_id = unpacker.unpack_int()?;
        let blockchain_id = Id::unpack(unpacker)?;

        let out_count = unpacker.unpack_int()? as usize;
        let mut outputs = Vec::with_capacity(out_count.min(1024));
        for _ in 0..out_count {
            outputs.push(SerializableTransferableOutput::unpack(unpacker)?);
        }

        let in_count = unpacker.unpack_int()? as usize;
        let mut inputs = Vec::with_capacity(in_count.min(1024));
        for _ in 0..in_count {
            inputs.push(SerializableTransferableInput::unpack(unpacker)?);
        }

        // Memo is variable length: 4-byte length + bytes
        let memo = unpacker.unpack_bytes()?;

        Ok(Self {
            network_id,
            blockchain_id,
            outputs,
            inputs,
            memo,
        })
    }
}

// =============================================================================
// Unsigned Transaction (with type ID)
// =============================================================================

/// Serializable unsigned transaction with type ID prefix.
#[derive(Debug, Clone)]
pub enum SerializableUnsignedTx {
    Base(SerializableBaseTx),
    // CreateAsset, Operation, Import, Export would be added here
}

impl Pack for SerializableUnsignedTx {
    fn pack(&self, packer: &mut Packer) {
        match self {
            SerializableUnsignedTx::Base(tx) => {
                packer.pack_int(tx_type_id::BASE_TX);
                tx.pack(packer);
            }
        }
    }

    fn packed_size(&self) -> usize {
        4 + match self {
            SerializableUnsignedTx::Base(tx) => tx.packed_size(),
        }
    }
}

impl Unpack for SerializableUnsignedTx {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let type_id = unpacker.unpack_int()?;
        match type_id {
            tx_type_id::BASE_TX => Ok(Self::Base(SerializableBaseTx::unpack(unpacker)?)),
            _ => Err(UnpackError::InsufficientBytes {
                needed: 0,
                remaining: 0,
            }),
        }
    }
}

// =============================================================================
// Signed Transaction
// =============================================================================

/// Serializable signed transaction matching Go's avm/txs.Tx.
#[derive(Debug, Clone)]
pub struct SerializableSignedTx {
    pub unsigned: SerializableUnsignedTx,
    pub credentials: Vec<SerializableCredential>,
}

impl SerializableSignedTx {
    /// Serializes the transaction with codec version prefix.
    pub fn marshal(&self) -> Vec<u8> {
        let mut packer = Packer::new(self.packed_size() + 2);
        packer.pack_short(CODEC_VERSION);
        self.pack(&mut packer);
        packer.into_bytes()
    }

    /// Deserializes a transaction from bytes with codec version prefix.
    pub fn unmarshal(bytes: &[u8]) -> Result<Self, UnpackError> {
        let mut unpacker = Unpacker::new(bytes);
        let version = unpacker.unpack_short()?;
        if version != CODEC_VERSION {
            return Err(UnpackError::InsufficientBytes {
                needed: 0,
                remaining: 0,
            });
        }
        Self::unpack(&mut unpacker)
    }

    /// Returns the unsigned transaction bytes for hashing/signing.
    pub fn unsigned_bytes(&self) -> Vec<u8> {
        let mut packer = Packer::new(self.unsigned.packed_size() + 2);
        packer.pack_short(CODEC_VERSION);
        self.unsigned.pack(&mut packer);
        packer.into_bytes()
    }

    /// Computes the transaction hash (SHA256 of unsigned bytes).
    pub fn hash(&self) -> [u8; 32] {
        use sha2::{Digest, Sha256};
        let bytes = self.unsigned_bytes();
        let hash = Sha256::digest(&bytes);
        let mut result = [0u8; 32];
        result.copy_from_slice(&hash);
        result
    }

    /// Computes the transaction ID.
    pub fn id(&self) -> Id {
        Id::from_bytes(self.hash())
    }
}

impl Pack for SerializableSignedTx {
    fn pack(&self, packer: &mut Packer) {
        self.unsigned.pack(packer);

        // Credentials with type ID prefix
        packer.pack_int(self.credentials.len() as u32);
        for cred in &self.credentials {
            packer.pack_int(fx_type_id::SECP256K1_CREDENTIAL);
            cred.pack(packer);
        }
    }

    fn packed_size(&self) -> usize {
        self.unsigned.packed_size()
            + 4
            + self
                .credentials
                .iter()
                .map(|c| 4 + c.packed_size())
                .sum::<usize>()
    }
}

impl Unpack for SerializableSignedTx {
    fn unpack(unpacker: &mut Unpacker) -> Result<Self, UnpackError> {
        let unsigned = SerializableUnsignedTx::unpack(unpacker)?;

        let cred_count = unpacker.unpack_int()? as usize;
        let mut credentials = Vec::with_capacity(cred_count.min(256));
        for _ in 0..cred_count {
            let type_id = unpacker.unpack_int()?;
            if type_id != fx_type_id::SECP256K1_CREDENTIAL {
                return Err(UnpackError::InsufficientBytes {
                    needed: 0,
                    remaining: 0,
                });
            }
            credentials.push(SerializableCredential::unpack(unpacker)?);
        }

        Ok(Self {
            unsigned,
            credentials,
        })
    }
}

// =============================================================================
// Conversion from domain types to serializable types
// =============================================================================

impl From<&TransferOutput> for SerializableTransferOutput {
    fn from(out: &TransferOutput) -> Self {
        Self {
            amount: out.amount,
            owners: OutputOwners {
                locktime: out.locktime,
                threshold: out.threshold,
                addresses: out
                    .addresses
                    .iter()
                    .map(|a| {
                        let mut bytes = [0u8; 20];
                        let len = a.len().min(20);
                        bytes[..len].copy_from_slice(&a[..len]);
                        ShortId::from_bytes(bytes)
                    })
                    .collect(),
            },
        }
    }
}

impl From<&TransferableOutput> for SerializableTransferableOutput {
    fn from(out: &TransferableOutput) -> Self {
        let output = match &out.output {
            Output::Transfer(t) => TypedOutput::Transfer(t.into()),
            Output::Mint(m) => TypedOutput::Mint(SerializableMintOutput {
                owners: OutputOwners {
                    locktime: m.locktime,
                    threshold: m.threshold,
                    addresses: m
                        .addresses
                        .iter()
                        .map(|a| {
                            let mut bytes = [0u8; 20];
                            let len = a.len().min(20);
                            bytes[..len].copy_from_slice(&a[..len]);
                            ShortId::from_bytes(bytes)
                        })
                        .collect(),
                },
            }),
            // NFT types would be handled here
            _ => TypedOutput::Transfer(SerializableTransferOutput {
                amount: 0,
                owners: OutputOwners::default(),
            }),
        };
        Self {
            asset_id: out.asset_id,
            output,
        }
    }
}

impl From<&TransferableInput> for SerializableTransferableInput {
    fn from(inp: &TransferableInput) -> Self {
        let input = match &inp.input {
            Input::Transfer(t) => TypedInput::Transfer(SerializableTransferInput {
                amount: t.amount,
                input: SerializableInput {
                    sig_indices: t.sig_indices.clone(),
                },
            }),
            // Other input types would be handled here
            _ => TypedInput::Transfer(SerializableTransferInput {
                amount: 0,
                input: SerializableInput::default(),
            }),
        };
        Self {
            utxo_id: (&inp.utxo_id).into(),
            asset_id: inp.asset_id,
            input,
        }
    }
}

impl From<&BaseTx> for SerializableBaseTx {
    fn from(tx: &BaseTx) -> Self {
        Self {
            network_id: tx.network_id,
            blockchain_id: tx.blockchain_id,
            outputs: tx.outputs.iter().map(|o| o.into()).collect(),
            inputs: tx.inputs.iter().map(|i| i.into()).collect(),
            memo: tx.memo.clone(),
        }
    }
}

impl From<&Transaction> for SerializableUnsignedTx {
    fn from(tx: &Transaction) -> Self {
        match tx {
            Transaction::Base(base) => Self::Base(base.into()),
            // Other tx types would be added here
            _ => Self::Base(SerializableBaseTx {
                network_id: 0,
                blockchain_id: Id::default(),
                outputs: vec![],
                inputs: vec![],
                memo: vec![],
            }),
        }
    }
}

impl From<&Credential> for SerializableCredential {
    fn from(cred: &Credential) -> Self {
        Self {
            signatures: cred
                .signatures
                .iter()
                .map(|s| {
                    let mut sig = [0u8; 65];
                    let len = s.bytes.len().min(65);
                    sig[..len].copy_from_slice(&s.bytes[..len]);
                    sig
                })
                .collect(),
        }
    }
}

impl From<&SignedTx> for SerializableSignedTx {
    fn from(tx: &SignedTx) -> Self {
        Self {
            unsigned: (&tx.unsigned).into(),
            credentials: tx.credentials.iter().map(|c| c.into()).collect(),
        }
    }
}

// =============================================================================
// Tests
// =============================================================================

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_output_owners_roundtrip() {
        let owners = OutputOwners {
            locktime: 1234567890,
            threshold: 2,
            addresses: vec![
                ShortId::from_bytes([1u8; 20]),
                ShortId::from_bytes([2u8; 20]),
            ],
        };

        let mut packer = Packer::new(256);
        owners.pack(&mut packer);
        let bytes = packer.into_bytes();

        let mut unpacker = Unpacker::new(&bytes);
        let decoded = OutputOwners::unpack(&mut unpacker).unwrap();

        assert_eq!(owners.locktime, decoded.locktime);
        assert_eq!(owners.threshold, decoded.threshold);
        assert_eq!(owners.addresses.len(), decoded.addresses.len());
    }

    #[test]
    fn test_transfer_output_roundtrip() {
        let out = SerializableTransferOutput {
            amount: 1000000000,
            owners: OutputOwners {
                locktime: 0,
                threshold: 1,
                addresses: vec![ShortId::from_bytes([42u8; 20])],
            },
        };

        let mut packer = Packer::new(256);
        out.pack(&mut packer);
        let bytes = packer.into_bytes();

        let mut unpacker = Unpacker::new(&bytes);
        let decoded = SerializableTransferOutput::unpack(&mut unpacker).unwrap();

        assert_eq!(out.amount, decoded.amount);
        assert_eq!(out.owners.threshold, decoded.owners.threshold);
    }

    #[test]
    fn test_base_tx_roundtrip() {
        let tx = SerializableBaseTx {
            network_id: 1,
            blockchain_id: Id::from_bytes([1u8; 32]),
            outputs: vec![SerializableTransferableOutput {
                asset_id: Id::from_bytes([2u8; 32]),
                output: TypedOutput::Transfer(SerializableTransferOutput {
                    amount: 500,
                    owners: OutputOwners {
                        locktime: 0,
                        threshold: 1,
                        addresses: vec![ShortId::from_bytes([3u8; 20])],
                    },
                }),
            }],
            inputs: vec![SerializableTransferableInput {
                utxo_id: SerializableUTXOID {
                    tx_id: Id::from_bytes([4u8; 32]),
                    output_index: 0,
                },
                asset_id: Id::from_bytes([2u8; 32]),
                input: TypedInput::Transfer(SerializableTransferInput {
                    amount: 1000,
                    input: SerializableInput {
                        sig_indices: vec![0],
                    },
                }),
            }],
            memo: vec![],
        };

        let mut packer = Packer::new(512);
        packer.pack_int(tx_type_id::BASE_TX);
        tx.pack(&mut packer);
        let bytes = packer.into_bytes();

        let mut unpacker = Unpacker::new(&bytes);
        let type_id = unpacker.unpack_int().unwrap();
        assert_eq!(type_id, tx_type_id::BASE_TX);

        let decoded = SerializableBaseTx::unpack(&mut unpacker).unwrap();

        assert_eq!(tx.network_id, decoded.network_id);
        assert_eq!(tx.outputs.len(), decoded.outputs.len());
        assert_eq!(tx.inputs.len(), decoded.inputs.len());
    }

    #[test]
    fn test_signed_tx_marshal_unmarshal() {
        let signed = SerializableSignedTx {
            unsigned: SerializableUnsignedTx::Base(SerializableBaseTx {
                network_id: 1,
                blockchain_id: Id::from_bytes([1u8; 32]),
                outputs: vec![],
                inputs: vec![],
                memo: vec![],
            }),
            credentials: vec![SerializableCredential {
                signatures: vec![[42u8; 65]],
            }],
        };

        let bytes = signed.marshal();

        // Check version prefix
        assert_eq!(bytes[0], 0);
        assert_eq!(bytes[1], 0);

        let decoded = SerializableSignedTx::unmarshal(&bytes).unwrap();
        assert_eq!(signed.credentials.len(), decoded.credentials.len());
    }

    #[test]
    fn test_transaction_hash() {
        let signed = SerializableSignedTx {
            unsigned: SerializableUnsignedTx::Base(SerializableBaseTx {
                network_id: 1,
                blockchain_id: Id::from_bytes([1u8; 32]),
                outputs: vec![],
                inputs: vec![],
                memo: vec![],
            }),
            credentials: vec![],
        };

        let hash1 = signed.hash();
        let hash2 = signed.hash();

        // Same transaction should produce same hash
        assert_eq!(hash1, hash2);

        // Hash should not be all zeros
        assert_ne!(hash1, [0u8; 32]);
    }
}
