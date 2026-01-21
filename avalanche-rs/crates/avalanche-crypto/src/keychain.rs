//! Keychain management for Avalanche wallets.
//!
//! Provides secure key storage and management for multiple keys.

use std::collections::HashMap;

use parking_lot::RwLock;
use zeroize::Zeroizing;

use crate::secp256k1::{PrivateKey, PublicKey};
use crate::{CryptoError, Result};

/// A keychain for managing multiple keys.
pub struct Keychain {
    /// Keys indexed by address
    keys: RwLock<HashMap<[u8; 20], KeyEntry>>,
}

/// An entry in the keychain.
struct KeyEntry {
    /// The private key (zeroized on drop)
    private_key: Zeroizing<[u8; 32]>,
    /// The public key
    public_key: PublicKey,
    /// Whether this key is locked
    locked: bool,
}

impl Keychain {
    /// Creates a new empty keychain.
    pub fn new() -> Self {
        Self {
            keys: RwLock::new(HashMap::new()),
        }
    }

    /// Generates a new key and adds it to the keychain.
    pub fn generate(&self) -> Result<PublicKey> {
        let private_key = PrivateKey::generate();
        let public_key = private_key.public_key();
        let address = public_key.to_address();

        let entry = KeyEntry {
            private_key: Zeroizing::new(private_key.to_bytes()),
            public_key: public_key.clone(),
            locked: false,
        };

        self.keys.write().insert(address, entry);
        Ok(public_key)
    }

    /// Imports a private key into the keychain.
    pub fn import(&self, private_key: &PrivateKey) -> Result<PublicKey> {
        let public_key = private_key.public_key();
        let address = public_key.to_address();

        if self.keys.read().contains_key(&address) {
            return Err(CryptoError::InvalidKey("key already exists".to_string()));
        }

        let entry = KeyEntry {
            private_key: Zeroizing::new(private_key.to_bytes()),
            public_key: public_key.clone(),
            locked: false,
        };

        self.keys.write().insert(address, entry);
        Ok(public_key)
    }

    /// Imports a private key from raw bytes.
    pub fn import_bytes(&self, bytes: &[u8]) -> Result<PublicKey> {
        let private_key = PrivateKey::from_bytes(bytes)?;
        self.import(&private_key)
    }

    /// Removes a key from the keychain.
    pub fn remove(&self, address: &[u8; 20]) -> bool {
        self.keys.write().remove(address).is_some()
    }

    /// Returns all public keys in the keychain.
    pub fn public_keys(&self) -> Vec<PublicKey> {
        self.keys.read().values().map(|e| e.public_key.clone()).collect()
    }

    /// Returns all addresses in the keychain.
    pub fn addresses(&self) -> Vec<[u8; 20]> {
        self.keys.read().keys().copied().collect()
    }

    /// Checks if an address is in the keychain.
    pub fn contains(&self, address: &[u8; 20]) -> bool {
        self.keys.read().contains_key(address)
    }

    /// Returns the number of keys in the keychain.
    pub fn len(&self) -> usize {
        self.keys.read().len()
    }

    /// Checks if the keychain is empty.
    pub fn is_empty(&self) -> bool {
        self.keys.read().is_empty()
    }

    /// Gets the public key for an address.
    pub fn get_public_key(&self, address: &[u8; 20]) -> Option<PublicKey> {
        self.keys.read().get(address).map(|e| e.public_key.clone())
    }

    /// Signs a message with the key for an address.
    pub fn sign(&self, address: &[u8; 20], message: &[u8]) -> Result<crate::secp256k1::Signature> {
        let keys = self.keys.read();
        let entry = keys.get(address).ok_or_else(|| {
            CryptoError::InvalidKey("address not in keychain".to_string())
        })?;

        if entry.locked {
            return Err(CryptoError::InvalidKey("key is locked".to_string()));
        }

        let private_key = PrivateKey::from_bytes(&*entry.private_key)?;
        private_key.sign(message)
    }

    /// Signs a message with recoverable signature.
    pub fn sign_recoverable(
        &self,
        address: &[u8; 20],
        message: &[u8],
    ) -> Result<crate::secp256k1::RecoverableSignature> {
        let keys = self.keys.read();
        let entry = keys.get(address).ok_or_else(|| {
            CryptoError::InvalidKey("address not in keychain".to_string())
        })?;

        if entry.locked {
            return Err(CryptoError::InvalidKey("key is locked".to_string()));
        }

        let private_key = PrivateKey::from_bytes(&*entry.private_key)?;
        private_key.sign_recoverable(message)
    }

    /// Locks a key (prevents signing).
    pub fn lock(&self, address: &[u8; 20]) -> bool {
        if let Some(entry) = self.keys.write().get_mut(address) {
            entry.locked = true;
            true
        } else {
            false
        }
    }

    /// Unlocks a key (allows signing).
    pub fn unlock(&self, address: &[u8; 20]) -> bool {
        if let Some(entry) = self.keys.write().get_mut(address) {
            entry.locked = false;
            true
        } else {
            false
        }
    }

    /// Checks if a key is locked.
    pub fn is_locked(&self, address: &[u8; 20]) -> Option<bool> {
        self.keys.read().get(address).map(|e| e.locked)
    }

    /// Exports a private key (use with caution).
    pub fn export(&self, address: &[u8; 20]) -> Result<PrivateKey> {
        let keys = self.keys.read();
        let entry = keys.get(address).ok_or_else(|| {
            CryptoError::InvalidKey("address not in keychain".to_string())
        })?;

        PrivateKey::from_bytes(&*entry.private_key)
    }

    /// Finds the key that can sign for a public key.
    pub fn find_by_public_key(&self, public_key: &PublicKey) -> Option<[u8; 20]> {
        let address = public_key.to_address();
        if self.contains(&address) {
            Some(address)
        } else {
            None
        }
    }

    /// Signs with any key that matches the given set of addresses.
    pub fn sign_with_any(
        &self,
        addresses: &[[u8; 20]],
        message: &[u8],
    ) -> Result<([u8; 20], crate::secp256k1::Signature)> {
        for addr in addresses {
            if self.contains(addr) {
                let sig = self.sign(addr, message)?;
                return Ok((*addr, sig));
            }
        }
        Err(CryptoError::InvalidKey("no matching key found".to_string()))
    }
}

impl Default for Keychain {
    fn default() -> Self {
        Self::new()
    }
}

impl std::fmt::Debug for Keychain {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Keychain")
            .field("num_keys", &self.len())
            .field("addresses", &self.addresses())
            .finish()
    }
}

/// Keychain-specific errors (re-exported from CryptoError).
pub type KeychainError = CryptoError;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_keychain_generate() {
        let keychain = Keychain::new();
        assert!(keychain.is_empty());

        let pk1 = keychain.generate().unwrap();
        let pk2 = keychain.generate().unwrap();

        assert_eq!(keychain.len(), 2);
        assert!(keychain.contains(&pk1.to_address()));
        assert!(keychain.contains(&pk2.to_address()));
    }

    #[test]
    fn test_keychain_import() {
        let keychain = Keychain::new();
        let private_key = PrivateKey::generate();
        let expected_pk = private_key.public_key();

        let pk = keychain.import(&private_key).unwrap();
        assert_eq!(pk.to_address(), expected_pk.to_address());
        assert!(keychain.contains(&pk.to_address()));
    }

    #[test]
    fn test_keychain_sign() {
        let keychain = Keychain::new();
        let pk = keychain.generate().unwrap();
        let address = pk.to_address();

        let message = b"test message";
        let signature = keychain.sign(&address, message).unwrap();

        assert!(pk.verify(message, &signature).is_ok());
    }

    #[test]
    fn test_keychain_lock_unlock() {
        let keychain = Keychain::new();
        let pk = keychain.generate().unwrap();
        let address = pk.to_address();

        // Should be able to sign
        let message = b"test";
        assert!(keychain.sign(&address, message).is_ok());

        // Lock the key
        keychain.lock(&address);
        assert_eq!(keychain.is_locked(&address), Some(true));
        assert!(keychain.sign(&address, message).is_err());

        // Unlock
        keychain.unlock(&address);
        assert_eq!(keychain.is_locked(&address), Some(false));
        assert!(keychain.sign(&address, message).is_ok());
    }

    #[test]
    fn test_keychain_remove() {
        let keychain = Keychain::new();
        let pk = keychain.generate().unwrap();
        let address = pk.to_address();

        assert!(keychain.contains(&address));
        assert!(keychain.remove(&address));
        assert!(!keychain.contains(&address));
    }

    #[test]
    fn test_keychain_export() {
        let keychain = Keychain::new();
        let original = PrivateKey::generate();
        let pk = keychain.import(&original).unwrap();

        let exported = keychain.export(&pk.to_address()).unwrap();
        assert_eq!(original.to_bytes(), exported.to_bytes());
    }

    #[test]
    fn test_sign_with_any() {
        let keychain = Keychain::new();
        let pk1 = keychain.generate().unwrap();
        let pk2 = keychain.generate().unwrap();

        let unknown = [0u8; 20];
        let addresses = [unknown, pk1.to_address(), pk2.to_address()];

        let message = b"test";
        let (addr, sig) = keychain.sign_with_any(&addresses, message).unwrap();

        // Should have signed with pk1 (first matching)
        assert_eq!(addr, pk1.to_address());
        assert!(pk1.verify(message, &sig).is_ok());
    }
}
