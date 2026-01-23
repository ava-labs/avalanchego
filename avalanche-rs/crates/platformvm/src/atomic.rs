//! Atomic transactions for cross-chain transfers.
//!
//! Enables AVAX and asset transfers between X, P, and C chains through:
//! - Export transactions (source chain)
//! - Import transactions (destination chain)
//! - Shared memory for UTXO coordination

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use parking_lot::RwLock;
use sha2::{Digest, Sha256};
use thiserror::Error;

use avalanche_ids::{Id, NodeId};

/// Atomic transaction errors.
#[derive(Debug, Error)]
pub enum AtomicError {
    #[error("invalid chain ID")]
    InvalidChainId,
    #[error("UTXO not found: {0}")]
    UtxoNotFound(String),
    #[error("UTXO already consumed: {0}")]
    UtxoAlreadyConsumed(String),
    #[error("insufficient funds: have {have}, need {need}")]
    InsufficientFunds { have: u64, need: u64 },
    #[error("invalid signature")]
    InvalidSignature,
    #[error("invalid input")]
    InvalidInput(String),
    #[error("chain not supported: {0}")]
    ChainNotSupported(String),
    #[error("import not allowed from this chain")]
    ImportNotAllowed,
    #[error("export not allowed to this chain")]
    ExportNotAllowed,
}

/// Result type for atomic operations.
pub type AtomicResult<T> = Result<T, AtomicError>;

/// Chain identifiers for atomic transfers.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ChainAlias {
    /// X-Chain (Exchange/AVM)
    X,
    /// P-Chain (Platform)
    P,
    /// C-Chain (Contract/EVM)
    C,
}

impl ChainAlias {
    /// Returns the chain name.
    pub fn name(&self) -> &'static str {
        match self {
            ChainAlias::X => "X-Chain",
            ChainAlias::P => "P-Chain",
            ChainAlias::C => "C-Chain",
        }
    }

    /// Returns allowed export destinations.
    pub fn allowed_exports(&self) -> Vec<ChainAlias> {
        match self {
            ChainAlias::X => vec![ChainAlias::P, ChainAlias::C],
            ChainAlias::P => vec![ChainAlias::X, ChainAlias::C],
            ChainAlias::C => vec![ChainAlias::X, ChainAlias::P],
        }
    }

    /// Returns allowed import sources.
    pub fn allowed_imports(&self) -> Vec<ChainAlias> {
        self.allowed_exports() // Symmetric
    }
}

/// A UTXO that can be transferred atomically.
#[derive(Debug, Clone)]
pub struct AtomicUtxo {
    /// UTXO ID.
    pub id: Id,
    /// Asset ID.
    pub asset_id: Id,
    /// Amount.
    pub amount: u64,
    /// Owner addresses (threshold signature).
    pub owners: Vec<[u8; 20]>,
    /// Required signature threshold.
    pub threshold: u32,
    /// Source chain.
    pub source_chain: Id,
    /// Locktime (0 = unlocked).
    pub locktime: u64,
}

impl AtomicUtxo {
    /// Creates a new atomic UTXO.
    pub fn new(
        asset_id: Id,
        amount: u64,
        owners: Vec<[u8; 20]>,
        threshold: u32,
        source_chain: Id,
    ) -> Self {
        let id = Self::compute_id(&asset_id, amount, &owners, &source_chain);
        Self {
            id,
            asset_id,
            amount,
            owners,
            threshold,
            source_chain,
            locktime: 0,
        }
    }

    /// Computes the UTXO ID.
    fn compute_id(asset_id: &Id, amount: u64, owners: &[[u8; 20]], source_chain: &Id) -> Id {
        let mut hasher = Sha256::new();
        hasher.update(asset_id.as_bytes());
        hasher.update(amount.to_be_bytes());
        for owner in owners {
            hasher.update(owner);
        }
        hasher.update(source_chain.as_bytes());
        // Add some randomness
        hasher.update(
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
                .to_be_bytes(),
        );
        let hash = hasher.finalize();
        Id::from_slice(&hash).unwrap_or_default()
    }

    /// Returns the UTXO value in bytes.
    pub fn serialize(&self) -> Vec<u8> {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(self.id.as_bytes());
        bytes.extend_from_slice(self.asset_id.as_bytes());
        bytes.extend_from_slice(&self.amount.to_be_bytes());
        bytes.extend_from_slice(&(self.owners.len() as u32).to_be_bytes());
        for owner in &self.owners {
            bytes.extend_from_slice(owner);
        }
        bytes.extend_from_slice(&self.threshold.to_be_bytes());
        bytes.extend_from_slice(self.source_chain.as_bytes());
        bytes.extend_from_slice(&self.locktime.to_be_bytes());
        bytes
    }
}

/// Export transaction - moves assets from source chain to shared memory.
#[derive(Debug, Clone)]
pub struct ExportTx {
    /// Transaction ID.
    pub tx_id: Id,
    /// Source chain ID.
    pub source_chain: Id,
    /// Destination chain ID.
    pub destination_chain: Id,
    /// Inputs being spent.
    pub inputs: Vec<TransferInput>,
    /// UTXOs being exported.
    pub exported_outputs: Vec<AtomicUtxo>,
    /// Memo data.
    pub memo: Vec<u8>,
}

impl ExportTx {
    /// Creates a new export transaction.
    pub fn new(
        source_chain: Id,
        destination_chain: Id,
        inputs: Vec<TransferInput>,
        outputs: Vec<AtomicUtxo>,
    ) -> Self {
        let tx_id = Self::compute_id(&source_chain, &destination_chain, &inputs, &outputs);
        Self {
            tx_id,
            source_chain,
            destination_chain,
            inputs,
            exported_outputs: outputs,
            memo: Vec::new(),
        }
    }

    /// Computes the transaction ID.
    fn compute_id(
        source: &Id,
        dest: &Id,
        inputs: &[TransferInput],
        outputs: &[AtomicUtxo],
    ) -> Id {
        let mut hasher = Sha256::new();
        hasher.update(b"export");
        hasher.update(source.as_bytes());
        hasher.update(dest.as_bytes());
        for input in inputs {
            hasher.update(input.utxo_id.as_bytes());
        }
        for output in outputs {
            hasher.update(output.id.as_bytes());
        }
        let hash = hasher.finalize();
        Id::from_slice(&hash).unwrap_or_default()
    }

    /// Returns total exported amount.
    pub fn total_exported(&self) -> u64 {
        self.exported_outputs.iter().map(|o| o.amount).sum()
    }
}

/// Import transaction - moves assets from shared memory to destination chain.
#[derive(Debug, Clone)]
pub struct ImportTx {
    /// Transaction ID.
    pub tx_id: Id,
    /// Source chain ID (where export happened).
    pub source_chain: Id,
    /// Destination chain ID (this chain).
    pub destination_chain: Id,
    /// UTXOs being imported.
    pub imported_inputs: Vec<AtomicUtxo>,
    /// Outputs on destination chain.
    pub outputs: Vec<TransferOutput>,
    /// Memo data.
    pub memo: Vec<u8>,
}

impl ImportTx {
    /// Creates a new import transaction.
    pub fn new(
        source_chain: Id,
        destination_chain: Id,
        inputs: Vec<AtomicUtxo>,
        outputs: Vec<TransferOutput>,
    ) -> Self {
        let tx_id = Self::compute_id(&source_chain, &destination_chain, &inputs);
        Self {
            tx_id,
            source_chain,
            destination_chain,
            imported_inputs: inputs,
            outputs,
            memo: Vec::new(),
        }
    }

    /// Computes the transaction ID.
    fn compute_id(source: &Id, dest: &Id, inputs: &[AtomicUtxo]) -> Id {
        let mut hasher = Sha256::new();
        hasher.update(b"import");
        hasher.update(source.as_bytes());
        hasher.update(dest.as_bytes());
        for input in inputs {
            hasher.update(input.id.as_bytes());
        }
        let hash = hasher.finalize();
        Id::from_slice(&hash).unwrap_or_default()
    }

    /// Returns total imported amount.
    pub fn total_imported(&self) -> u64 {
        self.imported_inputs.iter().map(|i| i.amount).sum()
    }
}

/// Transfer input referencing a UTXO.
#[derive(Debug, Clone)]
pub struct TransferInput {
    /// UTXO ID being spent.
    pub utxo_id: Id,
    /// Asset ID.
    pub asset_id: Id,
    /// Amount.
    pub amount: u64,
    /// Signature indices for multi-sig.
    pub sig_indices: Vec<u32>,
}

/// Transfer output creating a new UTXO.
#[derive(Debug, Clone)]
pub struct TransferOutput {
    /// Asset ID.
    pub asset_id: Id,
    /// Amount.
    pub amount: u64,
    /// Owner addresses.
    pub owners: Vec<[u8; 20]>,
    /// Threshold.
    pub threshold: u32,
    /// Locktime.
    pub locktime: u64,
}

/// Shared memory for atomic UTXO coordination.
pub struct SharedMemory {
    /// UTXOs indexed by (source_chain, dest_chain, utxo_id).
    utxos: RwLock<HashMap<(Id, Id, Id), AtomicUtxo>>,
    /// Consumed UTXOs.
    consumed: RwLock<HashSet<Id>>,
    /// Chain ID mappings.
    chain_ids: RwLock<HashMap<ChainAlias, Id>>,
}

impl SharedMemory {
    /// Creates new shared memory.
    pub fn new() -> Self {
        Self {
            utxos: RwLock::new(HashMap::new()),
            consumed: RwLock::new(HashSet::new()),
            chain_ids: RwLock::new(HashMap::new()),
        }
    }

    /// Registers a chain ID.
    pub fn register_chain(&self, alias: ChainAlias, chain_id: Id) {
        self.chain_ids.write().insert(alias, chain_id);
    }

    /// Gets chain ID by alias.
    pub fn get_chain_id(&self, alias: ChainAlias) -> Option<Id> {
        self.chain_ids.read().get(&alias).copied()
    }

    /// Adds UTXOs from an export transaction.
    pub fn add_export(&self, tx: &ExportTx) -> AtomicResult<()> {
        let mut utxos = self.utxos.write();

        for output in &tx.exported_outputs {
            let key = (tx.source_chain, tx.destination_chain, output.id);
            utxos.insert(key, output.clone());
        }

        Ok(())
    }

    /// Gets UTXOs available for import.
    pub fn get_importable_utxos(
        &self,
        source_chain: Id,
        dest_chain: Id,
    ) -> Vec<AtomicUtxo> {
        let utxos = self.utxos.read();
        let consumed = self.consumed.read();

        utxos
            .iter()
            .filter(|((src, dst, id), _)| {
                *src == source_chain && *dst == dest_chain && !consumed.contains(id)
            })
            .map(|(_, utxo)| utxo.clone())
            .collect()
    }

    /// Gets a specific UTXO.
    pub fn get_utxo(
        &self,
        source_chain: Id,
        dest_chain: Id,
        utxo_id: Id,
    ) -> Option<AtomicUtxo> {
        let utxos = self.utxos.read();
        let consumed = self.consumed.read();

        if consumed.contains(&utxo_id) {
            return None;
        }

        utxos.get(&(source_chain, dest_chain, utxo_id)).cloned()
    }

    /// Consumes UTXOs from an import transaction.
    pub fn consume_import(&self, tx: &ImportTx) -> AtomicResult<()> {
        let mut consumed = self.consumed.write();

        for input in &tx.imported_inputs {
            if consumed.contains(&input.id) {
                return Err(AtomicError::UtxoAlreadyConsumed(input.id.to_string()));
            }
            consumed.insert(input.id);
        }

        Ok(())
    }

    /// Checks if a UTXO is consumed.
    pub fn is_consumed(&self, utxo_id: &Id) -> bool {
        self.consumed.read().contains(utxo_id)
    }

    /// Returns count of pending UTXOs.
    pub fn pending_count(&self) -> usize {
        let consumed = self.consumed.read();
        self.utxos
            .read()
            .keys()
            .filter(|(_, _, id)| !consumed.contains(id))
            .count()
    }
}

impl Default for SharedMemory {
    fn default() -> Self {
        Self::new()
    }
}

/// Atomic transaction manager.
pub struct AtomicManager {
    /// Shared memory.
    shared_memory: Arc<SharedMemory>,
    /// This chain's ID.
    chain_id: Id,
    /// This chain's alias.
    chain_alias: ChainAlias,
}

impl AtomicManager {
    /// Creates a new atomic manager.
    pub fn new(chain_id: Id, chain_alias: ChainAlias, shared_memory: Arc<SharedMemory>) -> Self {
        shared_memory.register_chain(chain_alias, chain_id);
        Self {
            shared_memory,
            chain_id,
            chain_alias,
        }
    }

    /// Validates and executes an export transaction.
    pub fn execute_export(&self, tx: &ExportTx) -> AtomicResult<()> {
        // Verify source chain is this chain
        if tx.source_chain != self.chain_id {
            return Err(AtomicError::InvalidChainId);
        }

        // Verify destination is allowed
        let dest_alias = self.get_chain_alias(tx.destination_chain)?;
        if !self.chain_alias.allowed_exports().contains(&dest_alias) {
            return Err(AtomicError::ExportNotAllowed);
        }

        // Add to shared memory
        self.shared_memory.add_export(tx)?;

        Ok(())
    }

    /// Validates and executes an import transaction.
    pub fn execute_import(&self, tx: &ImportTx) -> AtomicResult<()> {
        // Verify destination chain is this chain
        if tx.destination_chain != self.chain_id {
            return Err(AtomicError::InvalidChainId);
        }

        // Verify source is allowed
        let source_alias = self.get_chain_alias(tx.source_chain)?;
        if !self.chain_alias.allowed_imports().contains(&source_alias) {
            return Err(AtomicError::ImportNotAllowed);
        }

        // Verify all inputs exist and are not consumed
        for input in &tx.imported_inputs {
            let utxo = self
                .shared_memory
                .get_utxo(tx.source_chain, tx.destination_chain, input.id)
                .ok_or_else(|| AtomicError::UtxoNotFound(input.id.to_string()))?;

            // Verify amounts match
            if utxo.amount != input.amount {
                return Err(AtomicError::InvalidInput(format!(
                    "amount mismatch: expected {}, got {}",
                    utxo.amount, input.amount
                )));
            }
        }

        // Consume inputs
        self.shared_memory.consume_import(tx)?;

        Ok(())
    }

    /// Gets importable UTXOs from a source chain.
    pub fn get_importable_utxos(&self, source_chain: Id) -> Vec<AtomicUtxo> {
        self.shared_memory
            .get_importable_utxos(source_chain, self.chain_id)
    }

    /// Creates an export transaction.
    pub fn create_export(
        &self,
        destination: ChainAlias,
        outputs: Vec<AtomicUtxo>,
        inputs: Vec<TransferInput>,
    ) -> AtomicResult<ExportTx> {
        let dest_chain = self
            .shared_memory
            .get_chain_id(destination)
            .ok_or(AtomicError::ChainNotSupported(destination.name().to_string()))?;

        if !self.chain_alias.allowed_exports().contains(&destination) {
            return Err(AtomicError::ExportNotAllowed);
        }

        Ok(ExportTx::new(self.chain_id, dest_chain, inputs, outputs))
    }

    /// Creates an import transaction.
    pub fn create_import(
        &self,
        source: ChainAlias,
        utxo_ids: Vec<Id>,
        outputs: Vec<TransferOutput>,
    ) -> AtomicResult<ImportTx> {
        let source_chain = self
            .shared_memory
            .get_chain_id(source)
            .ok_or(AtomicError::ChainNotSupported(source.name().to_string()))?;

        if !self.chain_alias.allowed_imports().contains(&source) {
            return Err(AtomicError::ImportNotAllowed);
        }

        // Gather UTXOs
        let mut inputs = Vec::new();
        for utxo_id in utxo_ids {
            let utxo = self
                .shared_memory
                .get_utxo(source_chain, self.chain_id, utxo_id)
                .ok_or_else(|| AtomicError::UtxoNotFound(utxo_id.to_string()))?;
            inputs.push(utxo);
        }

        Ok(ImportTx::new(source_chain, self.chain_id, inputs, outputs))
    }

    /// Gets the chain alias for a chain ID.
    fn get_chain_alias(&self, chain_id: Id) -> AtomicResult<ChainAlias> {
        let chain_ids = self.shared_memory.chain_ids.read();
        for (alias, id) in chain_ids.iter() {
            if *id == chain_id {
                return Ok(*alias);
            }
        }
        Err(AtomicError::InvalidChainId)
    }
}

/// C-Chain atomic transaction handler (for EVM integration).
pub struct CChainAtomicHandler {
    /// Atomic manager.
    manager: AtomicManager,
    /// AVAX asset ID.
    avax_asset_id: Id,
}

impl CChainAtomicHandler {
    /// Creates a new C-Chain atomic handler.
    pub fn new(shared_memory: Arc<SharedMemory>, chain_id: Id, avax_asset_id: Id) -> Self {
        let manager = AtomicManager::new(chain_id, ChainAlias::C, shared_memory);
        Self {
            manager,
            avax_asset_id,
        }
    }

    /// Imports AVAX from X-Chain to C-Chain address.
    pub fn import_avax(
        &self,
        utxo_ids: Vec<Id>,
        to_address: [u8; 20],
    ) -> AtomicResult<ImportTx> {
        let importable = self.manager.get_importable_utxos(
            self.manager
                .shared_memory
                .get_chain_id(ChainAlias::X)
                .ok_or(AtomicError::InvalidChainId)?,
        );

        // Filter for requested UTXOs that are AVAX
        let mut total = 0u64;
        let mut to_import = Vec::new();
        for utxo in &importable {
            if utxo_ids.contains(&utxo.id) && utxo.asset_id == self.avax_asset_id {
                total += utxo.amount;
                to_import.push(utxo.id);
            }
        }

        if to_import.is_empty() {
            return Err(AtomicError::UtxoNotFound("no matching UTXOs".to_string()));
        }

        // Create output to C-Chain address
        let output = TransferOutput {
            asset_id: self.avax_asset_id,
            amount: total,
            owners: vec![to_address],
            threshold: 1,
            locktime: 0,
        };

        self.manager.create_import(ChainAlias::X, to_import, vec![output])
    }

    /// Exports AVAX from C-Chain to X-Chain.
    pub fn export_avax(
        &self,
        amount: u64,
        to_addresses: Vec<[u8; 20]>,
        inputs: Vec<TransferInput>,
    ) -> AtomicResult<ExportTx> {
        let output = AtomicUtxo::new(
            self.avax_asset_id,
            amount,
            to_addresses,
            1,
            self.manager.chain_id,
        );

        self.manager.create_export(ChainAlias::X, vec![output], inputs)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_chain_id(byte: u8) -> Id {
        Id::from_bytes([byte; 32])
    }

    fn test_asset_id() -> Id {
        Id::from_bytes([0xAA; 32])
    }

    fn test_address(byte: u8) -> [u8; 20] {
        [byte; 20]
    }

    #[test]
    fn test_chain_alias() {
        assert_eq!(ChainAlias::X.name(), "X-Chain");
        assert!(ChainAlias::X.allowed_exports().contains(&ChainAlias::P));
        assert!(ChainAlias::X.allowed_exports().contains(&ChainAlias::C));
        assert!(!ChainAlias::X.allowed_exports().contains(&ChainAlias::X));
    }

    #[test]
    fn test_atomic_utxo() {
        let utxo = AtomicUtxo::new(
            test_asset_id(),
            1000,
            vec![test_address(1)],
            1,
            test_chain_id(1),
        );

        assert_eq!(utxo.amount, 1000);
        assert_eq!(utxo.threshold, 1);
        assert_eq!(utxo.owners.len(), 1);
    }

    #[test]
    fn test_export_tx() {
        let output = AtomicUtxo::new(
            test_asset_id(),
            500,
            vec![test_address(2)],
            1,
            test_chain_id(1),
        );

        let tx = ExportTx::new(
            test_chain_id(1),
            test_chain_id(2),
            vec![],
            vec![output],
        );

        assert_eq!(tx.total_exported(), 500);
    }

    #[test]
    fn test_shared_memory() {
        let shared = SharedMemory::new();
        shared.register_chain(ChainAlias::X, test_chain_id(1));
        shared.register_chain(ChainAlias::P, test_chain_id(2));

        let output = AtomicUtxo::new(
            test_asset_id(),
            1000,
            vec![test_address(1)],
            1,
            test_chain_id(1),
        );
        let output_id = output.id;

        let tx = ExportTx::new(
            test_chain_id(1),
            test_chain_id(2),
            vec![],
            vec![output],
        );

        shared.add_export(&tx).unwrap();

        // Should be importable
        let importable = shared.get_importable_utxos(test_chain_id(1), test_chain_id(2));
        assert_eq!(importable.len(), 1);
        assert_eq!(importable[0].id, output_id);
    }

    #[test]
    fn test_import_consumes_utxo() {
        let shared = Arc::new(SharedMemory::new());
        shared.register_chain(ChainAlias::X, test_chain_id(1));
        shared.register_chain(ChainAlias::P, test_chain_id(2));

        let output = AtomicUtxo::new(
            test_asset_id(),
            1000,
            vec![test_address(1)],
            1,
            test_chain_id(1),
        );

        let export_tx = ExportTx::new(
            test_chain_id(1),
            test_chain_id(2),
            vec![],
            vec![output.clone()],
        );

        shared.add_export(&export_tx).unwrap();

        let import_tx = ImportTx::new(
            test_chain_id(1),
            test_chain_id(2),
            vec![output.clone()],
            vec![],
        );

        shared.consume_import(&import_tx).unwrap();

        // Should no longer be importable
        assert!(shared.is_consumed(&output.id));
        let importable = shared.get_importable_utxos(test_chain_id(1), test_chain_id(2));
        assert_eq!(importable.len(), 0);
    }

    #[test]
    fn test_double_spend_prevention() {
        let shared = Arc::new(SharedMemory::new());
        shared.register_chain(ChainAlias::X, test_chain_id(1));
        shared.register_chain(ChainAlias::P, test_chain_id(2));

        let output = AtomicUtxo::new(
            test_asset_id(),
            1000,
            vec![test_address(1)],
            1,
            test_chain_id(1),
        );

        let export_tx = ExportTx::new(
            test_chain_id(1),
            test_chain_id(2),
            vec![],
            vec![output.clone()],
        );

        shared.add_export(&export_tx).unwrap();

        let import_tx = ImportTx::new(
            test_chain_id(1),
            test_chain_id(2),
            vec![output.clone()],
            vec![],
        );

        // First import succeeds
        shared.consume_import(&import_tx).unwrap();

        // Second import should fail
        let result = shared.consume_import(&import_tx);
        assert!(matches!(result, Err(AtomicError::UtxoAlreadyConsumed(_))));
    }

    #[test]
    fn test_atomic_manager_export() {
        let shared = Arc::new(SharedMemory::new());
        shared.register_chain(ChainAlias::X, test_chain_id(1));
        shared.register_chain(ChainAlias::P, test_chain_id(2));

        let manager = AtomicManager::new(test_chain_id(1), ChainAlias::X, shared.clone());

        let output = AtomicUtxo::new(
            test_asset_id(),
            500,
            vec![test_address(2)],
            1,
            test_chain_id(1),
        );

        let tx = manager
            .create_export(ChainAlias::P, vec![output], vec![])
            .unwrap();

        manager.execute_export(&tx).unwrap();

        // Check it's in shared memory
        assert_eq!(shared.pending_count(), 1);
    }

    #[test]
    fn test_atomic_manager_import() {
        let shared = Arc::new(SharedMemory::new());
        shared.register_chain(ChainAlias::X, test_chain_id(1));
        shared.register_chain(ChainAlias::P, test_chain_id(2));

        // Export from X-Chain
        let x_manager = AtomicManager::new(test_chain_id(1), ChainAlias::X, shared.clone());
        let output = AtomicUtxo::new(
            test_asset_id(),
            500,
            vec![test_address(2)],
            1,
            test_chain_id(1),
        );
        let output_id = output.id;

        let export_tx = x_manager
            .create_export(ChainAlias::P, vec![output], vec![])
            .unwrap();
        x_manager.execute_export(&export_tx).unwrap();

        // Import on P-Chain
        let p_manager = AtomicManager::new(test_chain_id(2), ChainAlias::P, shared.clone());
        let importable = p_manager.get_importable_utxos(test_chain_id(1));
        assert_eq!(importable.len(), 1);

        let import_output = TransferOutput {
            asset_id: test_asset_id(),
            amount: 500,
            owners: vec![test_address(3)],
            threshold: 1,
            locktime: 0,
        };

        let import_tx = p_manager
            .create_import(ChainAlias::X, vec![output_id], vec![import_output])
            .unwrap();

        p_manager.execute_import(&import_tx).unwrap();

        // Should be consumed now
        assert!(shared.is_consumed(&output_id));
    }

    #[test]
    fn test_export_not_allowed() {
        let shared = Arc::new(SharedMemory::new());
        shared.register_chain(ChainAlias::X, test_chain_id(1));

        let manager = AtomicManager::new(test_chain_id(1), ChainAlias::X, shared);

        // X-Chain cannot export to itself
        let result = manager.create_export(ChainAlias::X, vec![], vec![]);
        assert!(matches!(result, Err(AtomicError::ExportNotAllowed)));
    }
}
