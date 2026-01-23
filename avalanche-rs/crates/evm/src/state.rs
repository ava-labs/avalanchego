//! EVM state management.
//!
//! This module provides the state layer for EVM execution:
//! - Account state (balance, nonce, code)
//! - Contract storage
//! - State trie integration

use std::collections::HashMap;
use std::sync::Arc;

use alloy_primitives::{Address, B256, Bytes, U256};
use parking_lot::RwLock;
use revm::primitives::{AccountInfo as RevmAccountInfo, Bytecode, KECCAK_EMPTY};
use sha3::{Digest, Keccak256};

use avalanche_db::Database;
use avalanche_trie::{HashKey, MemoryTrieDb, Trie};

/// Account information.
#[derive(Debug, Clone, Default)]
pub struct AccountInfo {
    /// Account balance in wei.
    pub balance: U256,
    /// Account nonce (transaction count).
    pub nonce: u64,
    /// Code hash (KECCAK_EMPTY for EOAs).
    pub code_hash: B256,
    /// Contract code (empty for EOAs).
    pub code: Bytes,
}

impl AccountInfo {
    /// Creates a new empty account.
    pub fn new() -> Self {
        Self {
            balance: U256::ZERO,
            nonce: 0,
            code_hash: KECCAK_EMPTY,
            code: Bytes::new(),
        }
    }

    /// Creates an account with a balance.
    pub fn with_balance(balance: U256) -> Self {
        Self {
            balance,
            nonce: 0,
            code_hash: KECCAK_EMPTY,
            code: Bytes::new(),
        }
    }

    /// Creates a contract account with code.
    pub fn with_code(code: Bytes) -> Self {
        let code_hash = keccak256(&code);
        Self {
            balance: U256::ZERO,
            nonce: 1, // Contracts start with nonce 1
            code_hash,
            code,
        }
    }

    /// Returns true if this is a contract account.
    pub fn is_contract(&self) -> bool {
        self.code_hash != KECCAK_EMPTY
    }

    /// Converts to revm AccountInfo.
    pub fn to_revm(&self) -> RevmAccountInfo {
        RevmAccountInfo {
            balance: self.balance,
            nonce: self.nonce,
            code_hash: self.code_hash,
            code: if self.code.is_empty() {
                None
            } else {
                Some(Bytecode::new_raw(self.code.clone()))
            },
        }
    }

    /// Creates from revm AccountInfo.
    pub fn from_revm(info: &RevmAccountInfo, code: Bytes) -> Self {
        Self {
            balance: info.balance,
            nonce: info.nonce,
            code_hash: info.code_hash,
            code,
        }
    }
}

/// Account with storage.
#[derive(Debug, Clone, Default)]
pub struct Account {
    /// Account information.
    pub info: AccountInfo,
    /// Contract storage.
    pub storage: Storage,
}

impl Account {
    /// Creates a new empty account.
    pub fn new() -> Self {
        Self::default()
    }

    /// Creates an account with info.
    pub fn with_info(info: AccountInfo) -> Self {
        Self {
            info,
            storage: Storage::new(),
        }
    }
}

/// Contract storage.
#[derive(Debug, Clone, Default)]
pub struct Storage {
    /// Storage slots.
    slots: HashMap<U256, U256>,
}

impl Storage {
    /// Creates new empty storage.
    pub fn new() -> Self {
        Self::default()
    }

    /// Gets a storage value.
    pub fn get(&self, key: &U256) -> U256 {
        self.slots.get(key).copied().unwrap_or(U256::ZERO)
    }

    /// Sets a storage value.
    pub fn set(&mut self, key: U256, value: U256) {
        if value == U256::ZERO {
            self.slots.remove(&key);
        } else {
            self.slots.insert(key, value);
        }
    }

    /// Returns all non-zero storage slots.
    pub fn iter(&self) -> impl Iterator<Item = (&U256, &U256)> {
        self.slots.iter()
    }

    /// Returns the number of non-zero storage slots.
    pub fn len(&self) -> usize {
        self.slots.len()
    }

    /// Returns true if storage is empty.
    pub fn is_empty(&self) -> bool {
        self.slots.is_empty()
    }
}

/// EVM state database.
pub struct EvmState {
    /// Underlying database.
    db: Arc<dyn Database>,
    /// State trie for account state root.
    state_trie: Trie<MemoryTrieDb>,
    /// In-memory account cache.
    accounts: RwLock<HashMap<Address, Account>>,
    /// Code cache (code_hash -> code).
    code_cache: RwLock<HashMap<B256, Bytes>>,
    /// Dirty accounts that need to be written.
    dirty: RwLock<HashMap<Address, Account>>,
    /// Block number.
    block_number: RwLock<u64>,
}

impl EvmState {
    /// Creates a new EVM state.
    pub fn new(db: Arc<dyn Database>) -> Self {
        let trie_db = Arc::new(MemoryTrieDb::new());
        Self {
            db,
            state_trie: Trie::new(trie_db),
            accounts: RwLock::new(HashMap::new()),
            code_cache: RwLock::new(HashMap::new()),
            dirty: RwLock::new(HashMap::new()),
            block_number: RwLock::new(0),
        }
    }

    /// Returns the state root hash.
    pub fn state_root(&self) -> HashKey {
        self.state_trie.root_hash()
    }

    /// Returns the current block number.
    pub fn block_number(&self) -> u64 {
        *self.block_number.read()
    }

    /// Sets the block number.
    pub fn set_block_number(&self, number: u64) {
        *self.block_number.write() = number;
    }

    /// Gets account info.
    pub fn get_account(&self, address: &Address) -> AccountInfo {
        // Check dirty first
        if let Some(account) = self.dirty.read().get(address) {
            return account.info.clone();
        }

        // Check cache
        if let Some(account) = self.accounts.read().get(address) {
            return account.info.clone();
        }

        // Load from database
        self.load_account(address)
            .map(|a| a.info)
            .unwrap_or_default()
    }

    /// Gets account storage.
    pub fn get_storage(&self, address: &Address, key: &U256) -> U256 {
        // Check dirty first
        if let Some(account) = self.dirty.read().get(address) {
            return account.storage.get(key);
        }

        // Check cache
        if let Some(account) = self.accounts.read().get(address) {
            return account.storage.get(key);
        }

        // Load from database
        self.load_storage(address, key)
    }

    /// Gets contract code.
    pub fn get_code(&self, code_hash: &B256) -> Bytes {
        if *code_hash == KECCAK_EMPTY {
            return Bytes::new();
        }

        // Check code cache
        if let Some(code) = self.code_cache.read().get(code_hash) {
            return code.clone();
        }

        // Load from database
        self.load_code(code_hash).unwrap_or_default()
    }

    /// Sets account info.
    pub fn set_account(&self, address: Address, info: AccountInfo) {
        let mut dirty = self.dirty.write();
        let account = dirty.entry(address).or_insert_with(|| {
            self.accounts.read().get(&address).cloned().unwrap_or_default()
        });
        account.info = info;
    }

    /// Sets account storage.
    pub fn set_storage(&self, address: Address, key: U256, value: U256) {
        let mut dirty = self.dirty.write();
        let account = dirty.entry(address).or_insert_with(|| {
            self.accounts.read().get(&address).cloned().unwrap_or_default()
        });
        account.storage.set(key, value);
    }

    /// Sets contract code.
    pub fn set_code(&self, code_hash: B256, code: Bytes) {
        if code_hash != KECCAK_EMPTY {
            self.code_cache.write().insert(code_hash, code);
        }
    }

    /// Increments account nonce.
    pub fn increment_nonce(&self, address: &Address) {
        let mut dirty = self.dirty.write();
        let account = dirty.entry(*address).or_insert_with(|| {
            self.accounts.read().get(address).cloned().unwrap_or_default()
        });
        account.info.nonce += 1;
    }

    /// Transfers balance between accounts.
    pub fn transfer(&self, from: &Address, to: &Address, amount: U256) -> bool {
        let from_info = self.get_account(from);
        if from_info.balance < amount {
            return false;
        }

        let to_info = self.get_account(to);

        self.set_account(*from, AccountInfo {
            balance: from_info.balance - amount,
            ..from_info
        });

        self.set_account(*to, AccountInfo {
            balance: to_info.balance + amount,
            ..to_info
        });

        true
    }

    /// Commits dirty state to the trie and database.
    pub fn commit(&self) -> HashKey {
        let dirty = std::mem::take(&mut *self.dirty.write());

        for (address, account) in dirty {
            // Update trie
            let key = address.as_slice();
            let value = self.encode_account(&account);
            let _ = self.state_trie.insert(key, value);

            // Store code
            if account.info.is_contract() && !account.info.code.is_empty() {
                let code_key = format!("code:{}", hex::encode(account.info.code_hash));
                let _ = self.db.put(code_key.as_bytes(), &account.info.code);
            }

            // Store storage
            for (slot, value) in account.storage.iter() {
                let storage_key = format!(
                    "storage:{}:{}",
                    hex::encode(address),
                    hex::encode(slot.to_be_bytes::<32>())
                );
                let _ = self.db.put(storage_key.as_bytes(), &value.to_be_bytes::<32>());
            }

            // Move to cache
            self.accounts.write().insert(address, account);
        }

        self.state_trie.commit();
        self.state_root()
    }

    /// Creates a snapshot for rollback.
    pub fn snapshot(&self) -> StateSnapshot {
        StateSnapshot {
            root: self.state_root(),
            block_number: self.block_number(),
            dirty_count: self.dirty.read().len(),
        }
    }

    /// Reverts to a snapshot (clears dirty state).
    pub fn revert(&self, _snapshot: StateSnapshot) {
        self.dirty.write().clear();
    }

    // Helper methods

    fn load_account(&self, address: &Address) -> Option<Account> {
        let key = address.as_slice();
        let data = self.state_trie.get(key).ok()??;
        self.decode_account(&data)
    }

    fn load_storage(&self, address: &Address, slot: &U256) -> U256 {
        let storage_key = format!(
            "storage:{}:{}",
            hex::encode(address),
            hex::encode(slot.to_be_bytes::<32>())
        );
        match self.db.get(storage_key.as_bytes()) {
            Ok(Some(bytes)) if bytes.len() == 32 => {
                U256::from_be_slice(&bytes)
            }
            _ => U256::ZERO,
        }
    }

    fn load_code(&self, code_hash: &B256) -> Option<Bytes> {
        let code_key = format!("code:{}", hex::encode(code_hash));
        self.db.get(code_key.as_bytes())
            .ok()
            .flatten()
            .map(Bytes::from)
    }

    fn encode_account(&self, account: &Account) -> Vec<u8> {
        // Simple encoding: balance (32) + nonce (8) + code_hash (32)
        let mut data = Vec::with_capacity(72);
        data.extend_from_slice(&account.info.balance.to_be_bytes::<32>());
        data.extend_from_slice(&account.info.nonce.to_be_bytes());
        data.extend_from_slice(account.info.code_hash.as_slice());
        data
    }

    fn decode_account(&self, data: &[u8]) -> Option<Account> {
        if data.len() < 72 {
            return None;
        }

        let balance = U256::from_be_slice(&data[0..32]);
        let nonce = u64::from_be_bytes(data[32..40].try_into().ok()?);
        let code_hash = B256::from_slice(&data[40..72]);

        let code = if code_hash != KECCAK_EMPTY {
            self.load_code(&code_hash).unwrap_or_default()
        } else {
            Bytes::new()
        };

        Some(Account {
            info: AccountInfo {
                balance,
                nonce,
                code_hash,
                code,
            },
            storage: Storage::new(), // Storage loaded on demand
        })
    }
}

/// State snapshot for rollback.
#[derive(Debug, Clone)]
pub struct StateSnapshot {
    pub root: HashKey,
    pub block_number: u64,
    pub dirty_count: usize,
}

/// Computes keccak256 hash.
fn keccak256(data: &[u8]) -> B256 {
    let mut hasher = Keccak256::new();
    hasher.update(data);
    B256::from_slice(&hasher.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_db::MemDb;

    fn make_address(byte: u8) -> Address {
        Address::from([byte; 20])
    }

    #[test]
    fn test_account_info() {
        let info = AccountInfo::new();
        assert_eq!(info.balance, U256::ZERO);
        assert_eq!(info.nonce, 0);
        assert!(!info.is_contract());

        let info = AccountInfo::with_balance(U256::from(1000));
        assert_eq!(info.balance, U256::from(1000));

        let code = Bytes::from(vec![0x60, 0x00]);
        let info = AccountInfo::with_code(code.clone());
        assert!(info.is_contract());
        assert_eq!(info.code, code);
    }

    #[test]
    fn test_storage() {
        let mut storage = Storage::new();
        assert!(storage.is_empty());

        storage.set(U256::from(1), U256::from(100));
        assert_eq!(storage.get(&U256::from(1)), U256::from(100));
        assert_eq!(storage.get(&U256::from(2)), U256::ZERO);

        storage.set(U256::from(1), U256::ZERO);
        assert!(storage.is_empty());
    }

    #[test]
    fn test_evm_state() {
        let db = Arc::new(MemDb::new());
        let state = EvmState::new(db);

        let addr = make_address(1);
        let info = state.get_account(&addr);
        assert_eq!(info.balance, U256::ZERO);

        state.set_account(addr, AccountInfo::with_balance(U256::from(1000)));
        let info = state.get_account(&addr);
        assert_eq!(info.balance, U256::from(1000));
    }

    #[test]
    fn test_transfer() {
        let db = Arc::new(MemDb::new());
        let state = EvmState::new(db);

        let alice = make_address(1);
        let bob = make_address(2);

        state.set_account(alice, AccountInfo::with_balance(U256::from(1000)));

        assert!(state.transfer(&alice, &bob, U256::from(400)));
        assert_eq!(state.get_account(&alice).balance, U256::from(600));
        assert_eq!(state.get_account(&bob).balance, U256::from(400));

        // Insufficient balance
        assert!(!state.transfer(&alice, &bob, U256::from(1000)));
    }

    #[test]
    fn test_commit() {
        let db = Arc::new(MemDb::new());
        let state = EvmState::new(db);

        let addr = make_address(1);
        state.set_account(addr, AccountInfo::with_balance(U256::from(1000)));
        state.set_storage(addr, U256::from(1), U256::from(42));

        let root1 = state.commit();

        // Verify state persisted
        let info = state.get_account(&addr);
        assert_eq!(info.balance, U256::from(1000));

        let storage = state.get_storage(&addr, &U256::from(1));
        assert_eq!(storage, U256::from(42));

        // Modify and commit again
        state.set_account(addr, AccountInfo::with_balance(U256::from(2000)));
        let root2 = state.commit();

        assert_ne!(root1, root2);
    }
}
