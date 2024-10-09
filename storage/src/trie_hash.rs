// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt::{self, Debug};

use serde::{de::Visitor, Deserialize, Serialize};
use sha2::digest::{generic_array::GenericArray, typenum};

/// A hash value inside a merkle trie
/// We use the same type as returned by sha2 here to avoid copies
#[derive(PartialEq, Eq, Clone, Default, Hash)]
pub struct TrieHash(GenericArray<u8, typenum::U32>);

impl std::ops::Deref for TrieHash {
    type Target = GenericArray<u8, typenum::U32>;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl std::ops::DerefMut for TrieHash {
    fn deref_mut(&mut self) -> &mut Self::Target {
        &mut self.0
    }
}

impl AsRef<[u8]> for TrieHash {
    fn as_ref(&self) -> &[u8] {
        &self.0
    }
}

impl Debug for TrieHash {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        let width = f.precision().unwrap_or_default();
        write!(f, "{:.*}", width, hex::encode(self.0))
    }
}

impl From<[u8; 32]> for TrieHash {
    fn from(value: [u8; Self::len()]) -> Self {
        TrieHash(value.into())
    }
}

impl From<GenericArray<u8, typenum::U32>> for TrieHash {
    fn from(value: GenericArray<u8, typenum::U32>) -> Self {
        TrieHash(value)
    }
}

impl TrieHash {
    /// Return the length of a TrieHash
    const fn len() -> usize {
        std::mem::size_of::<TrieHash>()
    }

    /// Returns true iff each element in this hash is 0.
    pub fn is_empty(&self) -> bool {
        *self == TrieHash::default()
    }
}

impl Serialize for TrieHash {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        serializer.serialize_bytes(&self.0)
    }
}

impl<'de> Deserialize<'de> for TrieHash {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        deserializer.deserialize_bytes(TrieVisitor)
    }
}

struct TrieVisitor;

impl<'de> Visitor<'de> for TrieVisitor {
    type Value = TrieHash;

    fn expecting(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("an array of u8 hash bytes")
    }

    fn visit_bytes<E>(self, v: &[u8]) -> Result<Self::Value, E>
    where
        E: serde::de::Error,
    {
        let mut hash = TrieHash::default();
        if v.len() == hash.0.len() {
            hash.0.copy_from_slice(v);
            Ok(hash)
        } else {
            Err(E::invalid_length(v.len(), &self))
        }
    }
}
