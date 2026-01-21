//! State management for virtual machines.

use std::sync::Arc;

use avalanche_db::{Database, Result as DbResult};
use avalanche_ids::Id;
use parking_lot::RwLock;

use crate::Result;

/// State represents the VM's current state.
pub trait State: Send + Sync {
    /// Returns the current state root hash.
    fn root(&self) -> Id;

    /// Commits pending changes to persistent storage.
    fn commit(&mut self) -> Result<()>;

    /// Aborts pending changes.
    fn abort(&mut self);

    /// Returns true if there are uncommitted changes.
    fn is_dirty(&self) -> bool;
}

/// Manages state across block boundaries.
pub struct StateManager {
    /// The underlying database
    db: Arc<dyn Database>,
    /// Current state root
    current_root: RwLock<Id>,
    /// Last accepted state root
    accepted_root: RwLock<Id>,
    /// Whether state has uncommitted changes
    dirty: RwLock<bool>,
}

impl std::fmt::Debug for StateManager {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("StateManager")
            .field("current_root", &*self.current_root.read())
            .field("accepted_root", &*self.accepted_root.read())
            .field("dirty", &*self.dirty.read())
            .finish_non_exhaustive()
    }
}

impl StateManager {
    /// Creates a new state manager.
    pub fn new(db: Arc<dyn Database>) -> Self {
        Self {
            db,
            current_root: RwLock::new(Id::default()),
            accepted_root: RwLock::new(Id::default()),
            dirty: RwLock::new(false),
        }
    }

    /// Returns the underlying database.
    pub fn database(&self) -> Arc<dyn Database> {
        self.db.clone()
    }

    /// Returns the current state root.
    pub fn current_root(&self) -> Id {
        *self.current_root.read()
    }

    /// Returns the last accepted state root.
    pub fn accepted_root(&self) -> Id {
        *self.accepted_root.read()
    }

    /// Sets the current state root.
    pub fn set_current_root(&self, root: Id) {
        *self.current_root.write() = root;
        *self.dirty.write() = true;
    }

    /// Commits the current state as accepted.
    pub fn commit(&self) -> Result<()> {
        let current = *self.current_root.read();
        *self.accepted_root.write() = current;
        *self.dirty.write() = false;
        Ok(())
    }

    /// Rolls back to the accepted state.
    pub fn rollback(&self) {
        let accepted = *self.accepted_root.read();
        *self.current_root.write() = accepted;
        *self.dirty.write() = false;
    }

    /// Returns true if state has uncommitted changes.
    pub fn is_dirty(&self) -> bool {
        *self.dirty.read()
    }

    /// Gets a value from state.
    pub fn get(&self, key: &[u8]) -> DbResult<Option<Vec<u8>>> {
        self.db.get(key)
    }

    /// Checks if a key exists in state.
    pub fn has(&self, key: &[u8]) -> DbResult<bool> {
        self.db.has(key)
    }

    /// Puts a value into state.
    pub fn put(&self, key: &[u8], value: &[u8]) -> DbResult<()> {
        self.db.put(key, value)?;
        *self.dirty.write() = true;
        Ok(())
    }

    /// Deletes a key from state.
    pub fn delete(&self, key: &[u8]) -> DbResult<()> {
        self.db.delete(key)?;
        *self.dirty.write() = true;
        Ok(())
    }
}

/// Keys for common state entries.
pub mod state_keys {
    /// Key for the last accepted block ID
    pub const LAST_ACCEPTED: &[u8] = b"lastAccepted";
    /// Key for the genesis block ID
    pub const GENESIS: &[u8] = b"genesis";
    /// Key for block height index prefix
    pub const HEIGHT_PREFIX: &[u8] = b"height:";
    /// Key for block data prefix
    pub const BLOCK_PREFIX: &[u8] = b"block:";
}

/// Helper to build state keys.
pub fn height_key(height: u64) -> Vec<u8> {
    let mut key = state_keys::HEIGHT_PREFIX.to_vec();
    key.extend_from_slice(&height.to_be_bytes());
    key
}

/// Helper to build block keys.
pub fn block_key(id: &Id) -> Vec<u8> {
    let mut key = state_keys::BLOCK_PREFIX.to_vec();
    key.extend_from_slice(id.as_ref());
    key
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_db::MemDb;

    #[test]
    fn test_state_manager() {
        let db = Arc::new(MemDb::new());
        let state = StateManager::new(db);

        assert!(!state.is_dirty());

        // Write some data
        state.put(b"key", b"value").unwrap();
        assert!(state.is_dirty());

        // Read it back
        assert_eq!(state.get(b"key").unwrap(), Some(b"value".to_vec()));
        assert!(state.has(b"key").unwrap());

        // Commit
        let root = Id::from_slice(&[1; 32]).unwrap();
        state.set_current_root(root);
        state.commit().unwrap();

        assert!(!state.is_dirty());
        assert_eq!(state.accepted_root(), root);
    }

    #[test]
    fn test_state_rollback() {
        let db = Arc::new(MemDb::new());
        let state = StateManager::new(db);

        let root1 = Id::from_slice(&[1; 32]).unwrap();
        let root2 = Id::from_slice(&[2; 32]).unwrap();

        // Commit initial state
        state.set_current_root(root1);
        state.commit().unwrap();

        // Change state
        state.set_current_root(root2);
        assert_eq!(state.current_root(), root2);
        assert!(state.is_dirty());

        // Rollback
        state.rollback();
        assert_eq!(state.current_root(), root1);
        assert!(!state.is_dirty());
    }

    #[test]
    fn test_key_helpers() {
        let height_key = height_key(12345);
        assert!(height_key.starts_with(state_keys::HEIGHT_PREFIX));

        let id = Id::from_slice(&[42; 32]).unwrap();
        let blk_key = block_key(&id);
        assert!(blk_key.starts_with(state_keys::BLOCK_PREFIX));
    }
}
