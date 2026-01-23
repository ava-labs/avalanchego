//! Cross-chain atomic transactions.
//!
//! This module provides the infrastructure for atomic transactions
//! between Avalanche chains (X-Chain, P-Chain, C-Chain):
//! - SharedMemory: UTXO sharing between chains
//! - AtomicRequests: Pending import/export tracking
//! - AtomicState: Cross-chain state management

use std::collections::{HashMap, HashSet};
use std::sync::Arc;

use avalanche_db::Database;
use avalanche_ids::Id;
use parking_lot::RwLock;

/// A UTXO available for import from another chain.
#[derive(Debug, Clone)]
pub struct SharedUTXO {
    /// Source chain ID.
    pub source_chain: Id,
    /// UTXO ID (tx_id, output_index).
    pub utxo_id: (Id, u32),
    /// Asset ID.
    pub asset_id: Id,
    /// Output amount.
    pub amount: u64,
    /// Owner addresses.
    pub addresses: Vec<Vec<u8>>,
    /// Threshold required to spend.
    pub threshold: u32,
    /// Locktime (0 = no locktime).
    pub locktime: u64,
}

impl SharedUTXO {
    /// Creates a new shared UTXO.
    pub fn new(
        source_chain: Id,
        tx_id: Id,
        output_index: u32,
        asset_id: Id,
        amount: u64,
        addresses: Vec<Vec<u8>>,
    ) -> Self {
        Self {
            source_chain,
            utxo_id: (tx_id, output_index),
            asset_id,
            amount,
            addresses,
            threshold: 1,
            locktime: 0,
        }
    }

    /// Returns the unique identifier for this UTXO.
    pub fn id(&self) -> SharedUTXOId {
        SharedUTXOId {
            source_chain: self.source_chain,
            tx_id: self.utxo_id.0,
            output_index: self.utxo_id.1,
        }
    }

    /// Returns true if the UTXO can be spent at the given time.
    pub fn can_spend(&self, current_time: u64) -> bool {
        self.locktime == 0 || current_time >= self.locktime
    }
}

/// Identifier for a shared UTXO.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct SharedUTXOId {
    /// Source chain.
    pub source_chain: Id,
    /// Transaction ID.
    pub tx_id: Id,
    /// Output index.
    pub output_index: u32,
}

impl SharedUTXOId {
    /// Serializes to bytes.
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut bytes = Vec::with_capacity(68);
        bytes.extend_from_slice(self.source_chain.as_ref());
        bytes.extend_from_slice(self.tx_id.as_ref());
        bytes.extend_from_slice(&self.output_index.to_be_bytes());
        bytes
    }
}

/// Atomic export request.
#[derive(Debug, Clone)]
pub struct ExportRequest {
    /// Destination chain.
    pub destination_chain: Id,
    /// UTXOs to export.
    pub utxos: Vec<SharedUTXO>,
    /// Export transaction ID.
    pub tx_id: Id,
    /// Block height when exported.
    pub height: u64,
}

/// Atomic import request.
#[derive(Debug, Clone)]
pub struct ImportRequest {
    /// Source chain.
    pub source_chain: Id,
    /// UTXOs to import.
    pub utxo_ids: Vec<SharedUTXOId>,
    /// Import transaction ID.
    pub tx_id: Id,
}

/// Shared memory for cross-chain UTXO transfers.
pub struct SharedMemory {
    /// Database for persistent storage.
    db: Arc<dyn Database>,
    /// Pending exports (destination_chain -> utxos).
    pending_exports: RwLock<HashMap<Id, Vec<SharedUTXO>>>,
    /// Consumed exports (to prevent double-spending).
    consumed: RwLock<HashSet<SharedUTXOId>>,
    /// Index by address.
    address_index: RwLock<HashMap<Vec<u8>, HashSet<SharedUTXOId>>>,
}

impl SharedMemory {
    /// Creates a new shared memory instance.
    pub fn new(db: Arc<dyn Database>) -> Self {
        Self {
            db,
            pending_exports: RwLock::new(HashMap::new()),
            consumed: RwLock::new(HashSet::new()),
            address_index: RwLock::new(HashMap::new()),
        }
    }

    /// Exports UTXOs to another chain.
    pub fn export(&self, request: ExportRequest) -> Result<(), SharedMemoryError> {
        let destination = request.destination_chain;

        // Add to pending exports
        let mut pending = self.pending_exports.write();
        let exports = pending.entry(destination).or_insert_with(Vec::new);

        // Index by address
        let mut addr_index = self.address_index.write();
        for utxo in &request.utxos {
            let id = utxo.id();

            // Store in database
            let key = Self::utxo_key(&destination, &id);
            let value = self.serialize_utxo(utxo);
            self.db.put(&key, &value).map_err(|e| SharedMemoryError::Database(e.to_string()))?;

            // Index by address
            for addr in &utxo.addresses {
                addr_index
                    .entry(addr.clone())
                    .or_insert_with(HashSet::new)
                    .insert(id.clone());
            }

            exports.push(utxo.clone());
        }

        Ok(())
    }

    /// Gets available UTXOs for import from a source chain.
    pub fn get_available(&self, destination_chain: &Id) -> Vec<SharedUTXO> {
        self.pending_exports
            .read()
            .get(destination_chain)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .filter(|u| !self.consumed.read().contains(&u.id()))
            .collect()
    }

    /// Gets available UTXOs for a specific address.
    pub fn get_available_for_address(&self, destination_chain: &Id, address: &[u8]) -> Vec<SharedUTXO> {
        let addr_index = self.address_index.read();
        let utxo_ids = match addr_index.get(address) {
            Some(ids) => ids.clone(),
            None => return Vec::new(),
        };
        drop(addr_index);

        let consumed = self.consumed.read();
        let pending = self.pending_exports.read();

        pending
            .get(destination_chain)
            .cloned()
            .unwrap_or_default()
            .into_iter()
            .filter(|u| {
                let id = u.id();
                utxo_ids.contains(&id) && !consumed.contains(&id)
            })
            .collect()
    }

    /// Imports UTXOs from another chain (marks them as consumed).
    pub fn import(&self, request: ImportRequest) -> Result<Vec<SharedUTXO>, SharedMemoryError> {
        let source = request.source_chain;
        let mut imported = Vec::new();

        let pending = self.pending_exports.read();
        let utxos = pending.get(&source).cloned().unwrap_or_default();
        drop(pending);

        let mut consumed = self.consumed.write();

        for utxo_id in &request.utxo_ids {
            // Check not already consumed
            if consumed.contains(utxo_id) {
                return Err(SharedMemoryError::AlreadyConsumed(utxo_id.clone()));
            }

            // Find the UTXO
            let utxo = utxos
                .iter()
                .find(|u| u.id() == *utxo_id)
                .ok_or_else(|| SharedMemoryError::UTXONotFound(utxo_id.clone()))?;

            // Mark as consumed
            consumed.insert(utxo_id.clone());

            // Remove from database
            let key = Self::utxo_key(&source, utxo_id);
            let _ = self.db.delete(&key);

            imported.push(utxo.clone());
        }

        // Update address index
        let mut addr_index = self.address_index.write();
        for utxo in &imported {
            for addr in &utxo.addresses {
                if let Some(ids) = addr_index.get_mut(addr) {
                    ids.remove(&utxo.id());
                }
            }
        }

        Ok(imported)
    }

    /// Checks if a UTXO exists and is available.
    pub fn has_utxo(&self, destination_chain: &Id, utxo_id: &SharedUTXOId) -> bool {
        if self.consumed.read().contains(utxo_id) {
            return false;
        }

        self.pending_exports
            .read()
            .get(destination_chain)
            .map(|utxos| utxos.iter().any(|u| u.id() == *utxo_id))
            .unwrap_or(false)
    }

    /// Returns total amount available for import.
    pub fn available_amount(&self, destination_chain: &Id, asset_id: &Id) -> u64 {
        let consumed = self.consumed.read();
        self.pending_exports
            .read()
            .get(destination_chain)
            .map(|utxos| {
                utxos
                    .iter()
                    .filter(|u| &u.asset_id == asset_id && !consumed.contains(&u.id()))
                    .map(|u| u.amount)
                    .sum()
            })
            .unwrap_or(0)
    }

    // Helper methods

    fn utxo_key(chain: &Id, utxo_id: &SharedUTXOId) -> Vec<u8> {
        let mut key = Vec::with_capacity(100);
        key.extend_from_slice(b"atomic:");
        key.extend_from_slice(chain.as_ref());
        key.extend_from_slice(b":");
        key.extend_from_slice(&utxo_id.to_bytes());
        key
    }

    fn serialize_utxo(&self, utxo: &SharedUTXO) -> Vec<u8> {
        // Simple serialization
        let mut bytes = Vec::new();
        bytes.extend_from_slice(utxo.source_chain.as_ref());
        bytes.extend_from_slice(utxo.utxo_id.0.as_ref());
        bytes.extend_from_slice(&utxo.utxo_id.1.to_be_bytes());
        bytes.extend_from_slice(utxo.asset_id.as_ref());
        bytes.extend_from_slice(&utxo.amount.to_be_bytes());
        bytes.extend_from_slice(&utxo.threshold.to_be_bytes());
        bytes.extend_from_slice(&utxo.locktime.to_be_bytes());
        bytes.extend_from_slice(&(utxo.addresses.len() as u32).to_be_bytes());
        for addr in &utxo.addresses {
            bytes.extend_from_slice(&(addr.len() as u32).to_be_bytes());
            bytes.extend_from_slice(addr);
        }
        bytes
    }
}

/// Shared memory errors.
#[derive(Debug, Clone, thiserror::Error)]
pub enum SharedMemoryError {
    #[error("UTXO not found: {0:?}")]
    UTXONotFound(SharedUTXOId),

    #[error("UTXO already consumed: {0:?}")]
    AlreadyConsumed(SharedUTXOId),

    #[error("database error: {0}")]
    Database(String),

    #[error("invalid chain: {0}")]
    InvalidChain(Id),
}

/// Atomic state manager for a VM.
pub struct AtomicState {
    /// This chain's ID.
    chain_id: Id,
    /// Shared memory.
    shared_memory: Arc<SharedMemory>,
    /// Pending import requests.
    pending_imports: RwLock<Vec<ImportRequest>>,
    /// Pending export requests.
    pending_exports: RwLock<Vec<ExportRequest>>,
}

impl AtomicState {
    /// Creates a new atomic state.
    pub fn new(chain_id: Id, shared_memory: Arc<SharedMemory>) -> Self {
        Self {
            chain_id,
            shared_memory,
            pending_imports: RwLock::new(Vec::new()),
            pending_exports: RwLock::new(Vec::new()),
        }
    }

    /// Returns available UTXOs for import from a source chain.
    pub fn available_imports(&self, source_chain: &Id) -> Vec<SharedUTXO> {
        // When querying, we need to look for UTXOs exported TO our chain
        self.shared_memory.get_available(&self.chain_id)
            .into_iter()
            .filter(|u| &u.source_chain == source_chain)
            .collect()
    }

    /// Queues an export transaction.
    pub fn queue_export(&self, request: ExportRequest) {
        self.pending_exports.write().push(request);
    }

    /// Queues an import transaction.
    pub fn queue_import(&self, request: ImportRequest) {
        self.pending_imports.write().push(request);
    }

    /// Commits pending atomic operations.
    pub fn commit(&self) -> Result<(), SharedMemoryError> {
        // Process exports
        let exports = std::mem::take(&mut *self.pending_exports.write());
        for export in exports {
            self.shared_memory.export(export)?;
        }

        // Process imports
        let imports = std::mem::take(&mut *self.pending_imports.write());
        for import in imports {
            self.shared_memory.import(import)?;
        }

        Ok(())
    }

    /// Reverts pending atomic operations.
    pub fn revert(&self) {
        self.pending_exports.write().clear();
        self.pending_imports.write().clear();
    }

    /// Returns this chain's ID.
    pub fn chain_id(&self) -> Id {
        self.chain_id
    }
}

/// Known chain IDs for primary network.
pub mod chains {
    use std::str::FromStr;
    use avalanche_ids::Id;

    /// X-Chain ID (mainnet).
    pub const X_CHAIN_MAINNET: &str = "2oYMBNV4eNHyqk2fjjV5nVQLDbtmNJzq5s3qs3Lo6ftnC6FByM";

    /// P-Chain ID (all networks).
    pub const P_CHAIN: &str = "11111111111111111111111111111111LpoYY";

    /// C-Chain ID (mainnet).
    pub const C_CHAIN_MAINNET: &str = "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5";

    /// Parses a chain ID from string.
    pub fn parse_chain_id(s: &str) -> Option<Id> {
        Id::from_str(s).ok()
    }
}

/// Atomic transaction validator.
pub struct AtomicValidator {
    /// Shared memory.
    shared_memory: Arc<SharedMemory>,
}

impl AtomicValidator {
    /// Creates a new validator.
    pub fn new(shared_memory: Arc<SharedMemory>) -> Self {
        Self { shared_memory }
    }

    /// Validates an export transaction.
    pub fn validate_export(
        &self,
        destination_chain: Id,
        utxos: &[SharedUTXO],
    ) -> Result<(), SharedMemoryError> {
        // Basic validation
        if destination_chain == Id::default() {
            return Err(SharedMemoryError::InvalidChain(destination_chain));
        }

        if utxos.is_empty() {
            return Err(SharedMemoryError::Database("no UTXOs to export".to_string()));
        }

        // Validate each UTXO
        for utxo in utxos {
            if utxo.amount == 0 {
                return Err(SharedMemoryError::Database("UTXO amount is zero".to_string()));
            }
            if utxo.addresses.is_empty() {
                return Err(SharedMemoryError::Database("UTXO has no addresses".to_string()));
            }
        }

        Ok(())
    }

    /// Validates an import transaction.
    pub fn validate_import(
        &self,
        destination_chain: &Id,
        source_chain: &Id,
        utxo_ids: &[SharedUTXOId],
    ) -> Result<(), SharedMemoryError> {
        // Basic validation
        if *source_chain == Id::default() {
            return Err(SharedMemoryError::InvalidChain(*source_chain));
        }

        if utxo_ids.is_empty() {
            return Err(SharedMemoryError::Database("no UTXOs to import".to_string()));
        }

        // Check all UTXOs exist and are available
        for utxo_id in utxo_ids {
            if !self.shared_memory.has_utxo(destination_chain, utxo_id) {
                return Err(SharedMemoryError::UTXONotFound(utxo_id.clone()));
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_db::MemDb;

    fn make_id(byte: u8) -> Id {
        Id::from_slice(&[byte; 32]).unwrap()
    }

    fn make_utxo(source: u8, tx: u8, amount: u64) -> SharedUTXO {
        SharedUTXO::new(
            make_id(source),
            make_id(tx),
            0,
            make_id(10), // asset ID
            amount,
            vec![vec![1, 2, 3]],
        )
    }

    #[test]
    fn test_shared_utxo() {
        let utxo = make_utxo(1, 2, 1000);
        assert_eq!(utxo.amount, 1000);
        assert!(utxo.can_spend(0));

        let locked_utxo = SharedUTXO {
            locktime: 100,
            ..utxo.clone()
        };
        assert!(!locked_utxo.can_spend(50));
        assert!(locked_utxo.can_spend(100));
    }

    #[test]
    fn test_export_and_import() {
        let db = Arc::new(MemDb::new());
        let memory = SharedMemory::new(db);

        let source_chain = make_id(1);
        let dest_chain = make_id(2);

        // Export
        let utxo = make_utxo(1, 10, 500);
        let export_req = ExportRequest {
            destination_chain: dest_chain,
            utxos: vec![utxo.clone()],
            tx_id: make_id(100),
            height: 1,
        };
        memory.export(export_req).unwrap();

        // Check available
        let available = memory.get_available(&dest_chain);
        assert_eq!(available.len(), 1);
        assert_eq!(available[0].amount, 500);

        // Import
        let import_req = ImportRequest {
            source_chain: dest_chain,
            utxo_ids: vec![utxo.id()],
            tx_id: make_id(101),
        };
        let imported = memory.import(import_req).unwrap();
        assert_eq!(imported.len(), 1);

        // Check consumed
        let available = memory.get_available(&dest_chain);
        assert!(available.is_empty());
    }

    #[test]
    fn test_double_spend_prevention() {
        let db = Arc::new(MemDb::new());
        let memory = SharedMemory::new(db);

        let dest_chain = make_id(2);

        // Export
        let utxo = make_utxo(1, 10, 500);
        let utxo_id = utxo.id();
        memory.export(ExportRequest {
            destination_chain: dest_chain,
            utxos: vec![utxo],
            tx_id: make_id(100),
            height: 1,
        }).unwrap();

        // First import succeeds
        memory.import(ImportRequest {
            source_chain: dest_chain,
            utxo_ids: vec![utxo_id.clone()],
            tx_id: make_id(101),
        }).unwrap();

        // Second import fails
        let result = memory.import(ImportRequest {
            source_chain: dest_chain,
            utxo_ids: vec![utxo_id],
            tx_id: make_id(102),
        });
        assert!(matches!(result, Err(SharedMemoryError::AlreadyConsumed(_))));
    }

    #[test]
    fn test_available_amount() {
        let db = Arc::new(MemDb::new());
        let memory = SharedMemory::new(db);

        let dest_chain = make_id(2);
        let asset_id = make_id(10);

        // Export multiple UTXOs
        memory.export(ExportRequest {
            destination_chain: dest_chain,
            utxos: vec![
                make_utxo(1, 1, 100),
                make_utxo(1, 2, 200),
                make_utxo(1, 3, 300),
            ],
            tx_id: make_id(100),
            height: 1,
        }).unwrap();

        let total = memory.available_amount(&dest_chain, &asset_id);
        assert_eq!(total, 600);
    }

    #[test]
    fn test_atomic_state() {
        let db = Arc::new(MemDb::new());
        let memory = Arc::new(SharedMemory::new(db));

        let x_chain = make_id(1);
        let c_chain = make_id(2);

        let x_state = AtomicState::new(x_chain, memory.clone());

        // Queue export from X to C
        x_state.queue_export(ExportRequest {
            destination_chain: c_chain,
            utxos: vec![make_utxo(1, 10, 500)],
            tx_id: make_id(100),
            height: 1,
        });

        // Commit
        x_state.commit().unwrap();

        // Check C-chain can see the export
        let c_state = AtomicState::new(c_chain, memory);
        let available = c_state.available_imports(&x_chain);
        assert_eq!(available.len(), 1);
    }

    #[test]
    fn test_validator() {
        let db = Arc::new(MemDb::new());
        let memory = Arc::new(SharedMemory::new(db));

        let validator = AtomicValidator::new(memory);

        // Invalid export - empty UTXOs
        let result = validator.validate_export(make_id(2), &[]);
        assert!(result.is_err());

        // Invalid export - zero amount
        let mut bad_utxo = make_utxo(1, 1, 0);
        let result = validator.validate_export(make_id(2), &[bad_utxo]);
        assert!(result.is_err());

        // Valid export
        let good_utxo = make_utxo(1, 1, 100);
        let result = validator.validate_export(make_id(2), &[good_utxo]);
        assert!(result.is_ok());
    }

    #[test]
    fn test_address_index() {
        let db = Arc::new(MemDb::new());
        let memory = SharedMemory::new(db);

        let dest_chain = make_id(2);
        let addr = vec![1, 2, 3];

        // Export with specific address
        memory.export(ExportRequest {
            destination_chain: dest_chain,
            utxos: vec![SharedUTXO {
                addresses: vec![addr.clone()],
                ..make_utxo(1, 1, 100)
            }],
            tx_id: make_id(100),
            height: 1,
        }).unwrap();

        // Query by address
        let available = memory.get_available_for_address(&dest_chain, &addr);
        assert_eq!(available.len(), 1);

        // Query with wrong address returns empty
        let available = memory.get_available_for_address(&dest_chain, &vec![9, 9, 9]);
        assert!(available.is_empty());
    }
}
