//! Transaction mempool implementation.
//!
//! The mempool holds unconfirmed transactions waiting to be included in blocks.
//! It provides:
//! - Transaction validation before acceptance
//! - Priority ordering (by fee, timestamp)
//! - Size limits and eviction policies
//! - Duplicate detection
//! - Conflict tracking

use std::collections::{BTreeMap, HashMap, HashSet};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, Instant};

use parking_lot::RwLock;

use avalanche_ids::Id;

/// Mempool configuration.
#[derive(Debug, Clone)]
pub struct MempoolConfig {
    /// Maximum number of transactions in the mempool.
    pub max_txs: usize,
    /// Maximum total size of transactions in bytes.
    pub max_bytes: usize,
    /// Maximum age of a transaction before eviction.
    pub max_tx_age: Duration,
    /// Minimum gas price (for fee-based ordering).
    pub min_gas_price: u64,
    /// Whether to allow replacement by higher fee.
    pub allow_replacement: bool,
    /// Minimum fee bump percentage for replacement.
    pub replacement_bump_percent: u64,
}

impl Default for MempoolConfig {
    fn default() -> Self {
        Self {
            max_txs: 10_000,
            max_bytes: 64 * 1024 * 1024, // 64 MB
            max_tx_age: Duration::from_secs(3600), // 1 hour
            min_gas_price: 1,
            allow_replacement: true,
            replacement_bump_percent: 10,
        }
    }
}

/// Priority for transaction ordering.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct TxPriority {
    /// Gas price (higher = more priority).
    pub gas_price: u64,
    /// Timestamp (earlier = more priority for same gas price).
    pub timestamp: u64,
    /// Sequence number for stable ordering.
    pub sequence: u64,
}

impl TxPriority {
    /// Creates a new priority.
    pub fn new(gas_price: u64, timestamp: u64, sequence: u64) -> Self {
        Self {
            gas_price,
            timestamp,
            sequence,
        }
    }
}

/// A transaction in the mempool.
#[derive(Debug, Clone)]
pub struct MempoolTx {
    /// Transaction ID.
    pub id: Id,
    /// Raw transaction bytes.
    pub bytes: Vec<u8>,
    /// Transaction size in bytes.
    pub size: usize,
    /// Gas price for priority ordering.
    pub gas_price: u64,
    /// When this transaction was added.
    pub added_at: Instant,
    /// Priority for ordering.
    pub priority: TxPriority,
    /// Inputs this transaction spends (for conflict detection).
    pub inputs: HashSet<Id>,
    /// Whether this transaction has been issued (sent to consensus).
    pub issued: bool,
}

impl MempoolTx {
    /// Creates a new mempool transaction.
    pub fn new(
        id: Id,
        bytes: Vec<u8>,
        gas_price: u64,
        inputs: HashSet<Id>,
        sequence: u64,
    ) -> Self {
        let size = bytes.len();
        let now = Instant::now();
        let timestamp = now.elapsed().as_nanos() as u64;

        Self {
            id,
            bytes,
            size,
            gas_price,
            added_at: now,
            priority: TxPriority::new(gas_price, timestamp, sequence),
            inputs,
            issued: false,
        }
    }

    /// Returns the age of this transaction.
    pub fn age(&self) -> Duration {
        self.added_at.elapsed()
    }
}

/// Result of adding a transaction to the mempool.
#[derive(Debug, Clone)]
pub enum AddResult {
    /// Transaction was added successfully.
    Added,
    /// Transaction replaced an existing one.
    Replaced(Id),
    /// Transaction was rejected.
    Rejected(RejectReason),
}

/// Reason for rejecting a transaction.
#[derive(Debug, Clone)]
pub enum RejectReason {
    /// Transaction already exists.
    Duplicate,
    /// Mempool is full.
    MempoolFull,
    /// Transaction is too large.
    TooLarge,
    /// Gas price too low.
    GasPriceTooLow,
    /// Conflicts with existing transaction.
    Conflict(Id),
    /// Transaction is invalid.
    Invalid(String),
    /// Replacement fee too low.
    ReplacementFeeTooLow,
}

/// The transaction mempool.
pub struct Mempool {
    /// Configuration.
    config: MempoolConfig,
    /// Transactions by ID.
    txs: RwLock<HashMap<Id, MempoolTx>>,
    /// Transactions ordered by priority (highest first).
    by_priority: RwLock<BTreeMap<std::cmp::Reverse<TxPriority>, Id>>,
    /// Input to transaction mapping (for conflict detection).
    by_input: RwLock<HashMap<Id, Id>>,
    /// Current total size in bytes.
    total_bytes: AtomicU64,
    /// Sequence counter for stable ordering.
    sequence: AtomicU64,
}

impl Mempool {
    /// Creates a new mempool with the given configuration.
    pub fn new(config: MempoolConfig) -> Self {
        Self {
            config,
            txs: RwLock::new(HashMap::new()),
            by_priority: RwLock::new(BTreeMap::new()),
            by_input: RwLock::new(HashMap::new()),
            total_bytes: AtomicU64::new(0),
            sequence: AtomicU64::new(0),
        }
    }

    /// Creates a new mempool with default configuration.
    pub fn with_defaults() -> Self {
        Self::new(MempoolConfig::default())
    }

    /// Returns the number of transactions in the mempool.
    pub fn len(&self) -> usize {
        self.txs.read().len()
    }

    /// Returns true if the mempool is empty.
    pub fn is_empty(&self) -> bool {
        self.txs.read().is_empty()
    }

    /// Returns the total size of transactions in bytes.
    pub fn total_bytes(&self) -> u64 {
        self.total_bytes.load(Ordering::Relaxed)
    }

    /// Checks if a transaction exists in the mempool.
    pub fn contains(&self, tx_id: &Id) -> bool {
        self.txs.read().contains_key(tx_id)
    }

    /// Gets a transaction by ID.
    pub fn get(&self, tx_id: &Id) -> Option<MempoolTx> {
        self.txs.read().get(tx_id).cloned()
    }

    /// Adds a transaction to the mempool.
    pub fn add(
        &self,
        id: Id,
        bytes: Vec<u8>,
        gas_price: u64,
        inputs: HashSet<Id>,
    ) -> AddResult {
        // Check size
        if bytes.len() > self.config.max_bytes / 10 {
            return AddResult::Rejected(RejectReason::TooLarge);
        }

        // Check gas price
        if gas_price < self.config.min_gas_price {
            return AddResult::Rejected(RejectReason::GasPriceTooLow);
        }

        let mut txs = self.txs.write();
        let mut by_priority = self.by_priority.write();
        let mut by_input = self.by_input.write();

        // Check for duplicate
        if txs.contains_key(&id) {
            return AddResult::Rejected(RejectReason::Duplicate);
        }

        // Check for conflicts
        for input in &inputs {
            if let Some(existing_id) = by_input.get(input) {
                if self.config.allow_replacement {
                    // Check if we can replace
                    if let Some(existing) = txs.get(existing_id) {
                        let min_replacement_price = existing.gas_price
                            + (existing.gas_price * self.config.replacement_bump_percent / 100);
                        if gas_price < min_replacement_price {
                            return AddResult::Rejected(RejectReason::ReplacementFeeTooLow);
                        }
                        // Will replace below
                    }
                } else {
                    return AddResult::Rejected(RejectReason::Conflict(*existing_id));
                }
            }
        }

        // Check capacity
        let new_size = bytes.len();
        let current_bytes = self.total_bytes.load(Ordering::Relaxed) as usize;

        if txs.len() >= self.config.max_txs || current_bytes + new_size > self.config.max_bytes {
            // Try to evict lowest priority transactions
            if !self.evict_for_space(&mut txs, &mut by_priority, &mut by_input, new_size, gas_price)
            {
                return AddResult::Rejected(RejectReason::MempoolFull);
            }
        }

        // Remove any conflicting transactions
        let mut replaced = None;
        for input in &inputs {
            if let Some(existing_id) = by_input.remove(input) {
                if let Some(existing) = txs.remove(&existing_id) {
                    by_priority.remove(&std::cmp::Reverse(existing.priority));
                    self.total_bytes
                        .fetch_sub(existing.size as u64, Ordering::Relaxed);
                    replaced = Some(existing_id);
                }
            }
        }

        // Add the transaction
        let sequence = self.sequence.fetch_add(1, Ordering::Relaxed);
        let tx = MempoolTx::new(id, bytes, gas_price, inputs.clone(), sequence);
        let priority = tx.priority;

        for input in &inputs {
            by_input.insert(*input, id);
        }

        by_priority.insert(std::cmp::Reverse(priority), id);
        self.total_bytes.fetch_add(new_size as u64, Ordering::Relaxed);
        txs.insert(id, tx);

        match replaced {
            Some(replaced_id) => AddResult::Replaced(replaced_id),
            None => AddResult::Added,
        }
    }

    /// Removes a transaction from the mempool.
    pub fn remove(&self, tx_id: &Id) -> Option<MempoolTx> {
        let mut txs = self.txs.write();
        let mut by_priority = self.by_priority.write();
        let mut by_input = self.by_input.write();

        if let Some(tx) = txs.remove(tx_id) {
            by_priority.remove(&std::cmp::Reverse(tx.priority));
            for input in &tx.inputs {
                by_input.remove(input);
            }
            self.total_bytes.fetch_sub(tx.size as u64, Ordering::Relaxed);
            Some(tx)
        } else {
            None
        }
    }

    /// Removes multiple transactions (e.g., when a block is accepted).
    pub fn remove_batch(&self, tx_ids: &[Id]) -> Vec<MempoolTx> {
        let mut txs = self.txs.write();
        let mut by_priority = self.by_priority.write();
        let mut by_input = self.by_input.write();

        let mut removed = Vec::with_capacity(tx_ids.len());

        for tx_id in tx_ids {
            if let Some(tx) = txs.remove(tx_id) {
                by_priority.remove(&std::cmp::Reverse(tx.priority));
                for input in &tx.inputs {
                    by_input.remove(input);
                }
                self.total_bytes.fetch_sub(tx.size as u64, Ordering::Relaxed);
                removed.push(tx);
            }
        }

        removed
    }

    /// Gets the highest priority transactions for block building.
    pub fn peek(&self, max_count: usize, max_bytes: usize) -> Vec<MempoolTx> {
        let txs = self.txs.read();
        let by_priority = self.by_priority.read();

        let mut result = Vec::with_capacity(max_count.min(txs.len()));
        let mut total_bytes = 0;

        for (_, tx_id) in by_priority.iter() {
            if result.len() >= max_count {
                break;
            }

            if let Some(tx) = txs.get(tx_id) {
                if !tx.issued && total_bytes + tx.size <= max_bytes {
                    total_bytes += tx.size;
                    result.push(tx.clone());
                }
            }
        }

        result
    }

    /// Marks transactions as issued (sent to consensus).
    pub fn mark_issued(&self, tx_ids: &[Id]) {
        let mut txs = self.txs.write();
        for tx_id in tx_ids {
            if let Some(tx) = txs.get_mut(tx_id) {
                tx.issued = true;
            }
        }
    }

    /// Unmarks transactions as issued (consensus rejected them).
    pub fn unmark_issued(&self, tx_ids: &[Id]) {
        let mut txs = self.txs.write();
        for tx_id in tx_ids {
            if let Some(tx) = txs.get_mut(tx_id) {
                tx.issued = false;
            }
        }
    }

    /// Removes expired transactions.
    pub fn evict_expired(&self) -> Vec<Id> {
        let mut txs = self.txs.write();
        let mut by_priority = self.by_priority.write();
        let mut by_input = self.by_input.write();

        let max_age = self.config.max_tx_age;
        let mut evicted = Vec::new();

        txs.retain(|id, tx| {
            if tx.age() > max_age {
                by_priority.remove(&std::cmp::Reverse(tx.priority));
                for input in &tx.inputs {
                    by_input.remove(input);
                }
                self.total_bytes.fetch_sub(tx.size as u64, Ordering::Relaxed);
                evicted.push(*id);
                false
            } else {
                true
            }
        });

        evicted
    }

    /// Evicts lowest priority transactions to make space.
    fn evict_for_space(
        &self,
        txs: &mut HashMap<Id, MempoolTx>,
        by_priority: &mut BTreeMap<std::cmp::Reverse<TxPriority>, Id>,
        by_input: &mut HashMap<Id, Id>,
        needed_bytes: usize,
        min_gas_price: u64,
    ) -> bool {
        let mut freed_bytes = 0;
        let mut to_evict = Vec::new();

        // Find lowest priority transactions to evict
        for (priority, tx_id) in by_priority.iter().rev() {
            // Don't evict transactions with higher gas price
            if priority.0.gas_price >= min_gas_price {
                break;
            }

            if let Some(tx) = txs.get(tx_id) {
                if !tx.issued {
                    freed_bytes += tx.size;
                    to_evict.push(*tx_id);

                    if freed_bytes >= needed_bytes {
                        break;
                    }
                }
            }
        }

        if freed_bytes < needed_bytes {
            return false;
        }

        // Actually evict
        for tx_id in to_evict {
            if let Some(tx) = txs.remove(&tx_id) {
                by_priority.remove(&std::cmp::Reverse(tx.priority));
                for input in &tx.inputs {
                    by_input.remove(input);
                }
                self.total_bytes.fetch_sub(tx.size as u64, Ordering::Relaxed);
            }
        }

        true
    }

    /// Gets all transaction IDs.
    pub fn tx_ids(&self) -> Vec<Id> {
        self.txs.read().keys().copied().collect()
    }

    /// Clears the mempool.
    pub fn clear(&self) {
        self.txs.write().clear();
        self.by_priority.write().clear();
        self.by_input.write().clear();
        self.total_bytes.store(0, Ordering::Relaxed);
    }
}

impl Default for Mempool {
    fn default() -> Self {
        Self::with_defaults()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_tx(id_byte: u8, gas_price: u64, size: usize) -> (Id, Vec<u8>, HashSet<Id>) {
        let id = Id::from_bytes([id_byte; 32]);
        let bytes = vec![0u8; size];
        let mut inputs = HashSet::new();
        inputs.insert(Id::from_bytes([id_byte + 100; 32])); // Unique input
        (id, bytes, inputs)
    }

    #[test]
    fn test_add_and_get() {
        let mempool = Mempool::with_defaults();
        let (id, bytes, inputs) = make_tx(1, 100, 1000);

        let result = mempool.add(id, bytes.clone(), 100, inputs);
        assert!(matches!(result, AddResult::Added));

        let tx = mempool.get(&id).unwrap();
        assert_eq!(tx.id, id);
        assert_eq!(tx.bytes, bytes);
        assert_eq!(tx.gas_price, 100);
    }

    #[test]
    fn test_duplicate_rejection() {
        let mempool = Mempool::with_defaults();
        let (id, bytes, inputs) = make_tx(1, 100, 1000);

        mempool.add(id, bytes.clone(), 100, inputs.clone());
        let result = mempool.add(id, bytes, 100, inputs);

        assert!(matches!(result, AddResult::Rejected(RejectReason::Duplicate)));
    }

    #[test]
    fn test_priority_ordering() {
        let mempool = Mempool::with_defaults();

        // Add transactions with different gas prices
        for i in 0..5 {
            let (id, bytes, inputs) = make_tx(i, (i as u64 + 1) * 10, 100);
            mempool.add(id, bytes, (i as u64 + 1) * 10, inputs);
        }

        // Peek should return highest gas price first
        let top = mempool.peek(3, 1000);
        assert_eq!(top.len(), 3);
        assert_eq!(top[0].gas_price, 50); // id=4, highest
        assert_eq!(top[1].gas_price, 40); // id=3
        assert_eq!(top[2].gas_price, 30); // id=2
    }

    #[test]
    fn test_remove() {
        let mempool = Mempool::with_defaults();
        let (id, bytes, inputs) = make_tx(1, 100, 1000);

        mempool.add(id, bytes, 100, inputs);
        assert!(mempool.contains(&id));
        assert_eq!(mempool.len(), 1);

        let removed = mempool.remove(&id);
        assert!(removed.is_some());
        assert!(!mempool.contains(&id));
        assert_eq!(mempool.len(), 0);
    }

    #[test]
    fn test_conflict_detection() {
        let mempool = Mempool::new(MempoolConfig {
            allow_replacement: false,
            ..Default::default()
        });

        let shared_input = Id::from_bytes([99; 32]);
        let mut inputs1 = HashSet::new();
        inputs1.insert(shared_input);

        let id1 = Id::from_bytes([1; 32]);
        mempool.add(id1, vec![0; 100], 100, inputs1.clone());

        // Second tx with same input should conflict
        let id2 = Id::from_bytes([2; 32]);
        let result = mempool.add(id2, vec![0; 100], 100, inputs1);

        assert!(matches!(
            result,
            AddResult::Rejected(RejectReason::Conflict(_))
        ));
    }

    #[test]
    fn test_replacement() {
        let mempool = Mempool::new(MempoolConfig {
            allow_replacement: true,
            replacement_bump_percent: 10,
            ..Default::default()
        });

        let shared_input = Id::from_bytes([99; 32]);
        let mut inputs = HashSet::new();
        inputs.insert(shared_input);

        let id1 = Id::from_bytes([1; 32]);
        mempool.add(id1, vec![0; 100], 100, inputs.clone());

        // Replacement with higher fee should work
        let id2 = Id::from_bytes([2; 32]);
        let result = mempool.add(id2, vec![0; 100], 115, inputs.clone()); // 15% bump

        assert!(matches!(result, AddResult::Replaced(_)));
        assert!(!mempool.contains(&id1));
        assert!(mempool.contains(&id2));
    }

    #[test]
    fn test_replacement_fee_too_low() {
        let mempool = Mempool::new(MempoolConfig {
            allow_replacement: true,
            replacement_bump_percent: 10,
            ..Default::default()
        });

        let shared_input = Id::from_bytes([99; 32]);
        let mut inputs = HashSet::new();
        inputs.insert(shared_input);

        let id1 = Id::from_bytes([1; 32]);
        mempool.add(id1, vec![0; 100], 100, inputs.clone());

        // Replacement with insufficient fee bump
        let id2 = Id::from_bytes([2; 32]);
        let result = mempool.add(id2, vec![0; 100], 105, inputs); // Only 5% bump

        assert!(matches!(
            result,
            AddResult::Rejected(RejectReason::ReplacementFeeTooLow)
        ));
    }

    #[test]
    fn test_mark_issued() {
        let mempool = Mempool::with_defaults();
        let (id, bytes, inputs) = make_tx(1, 100, 1000);

        mempool.add(id, bytes, 100, inputs);

        // Should appear in peek
        let top = mempool.peek(10, 10000);
        assert_eq!(top.len(), 1);

        // Mark as issued
        mempool.mark_issued(&[id]);

        // Should not appear in peek anymore
        let top = mempool.peek(10, 10000);
        assert_eq!(top.len(), 0);

        // Unmark
        mempool.unmark_issued(&[id]);

        // Should appear again
        let top = mempool.peek(10, 10000);
        assert_eq!(top.len(), 1);
    }

    #[test]
    fn test_remove_batch() {
        let mempool = Mempool::with_defaults();

        let mut ids = Vec::new();
        for i in 0..5 {
            let (id, bytes, inputs) = make_tx(i, 100, 100);
            mempool.add(id, bytes, 100, inputs);
            ids.push(id);
        }

        assert_eq!(mempool.len(), 5);

        mempool.remove_batch(&ids[..3]);

        assert_eq!(mempool.len(), 2);
    }

    #[test]
    fn test_total_bytes() {
        let mempool = Mempool::with_defaults();

        for i in 0..3 {
            let (id, bytes, inputs) = make_tx(i, 100, 1000);
            mempool.add(id, bytes, 100, inputs);
        }

        assert_eq!(mempool.total_bytes(), 3000);

        let (id, _, _) = make_tx(0, 100, 1000);
        mempool.remove(&id);

        assert_eq!(mempool.total_bytes(), 2000);
    }
}
