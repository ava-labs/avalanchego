// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::node::ExtendableBytes;
use crate::node::branch::Serializable;
use sha2::digest::generic_array::GenericArray;
use sha2::digest::typenum;
use std::fmt::{self, Debug, Display, Formatter};

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

impl AsRef<[u8]> for TrieHash {
    fn as_ref(&self) -> &[u8] {
        &self.0
    }
}

impl Debug for TrieHash {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        let width = f.precision().unwrap_or(64);
        write!(f, "{:.*}", width, hex::encode(self.0))
    }
}
impl Display for TrieHash {
    fn fmt(&self, f: &mut Formatter<'_>) -> Result<(), fmt::Error> {
        let width = f.precision().unwrap_or(64);
        write!(f, "{:.*}", width, hex::encode(self.0))
    }
}

const TRIE_HASH_LEN: usize = std::mem::size_of::<TrieHash>();

impl From<[u8; 32]> for TrieHash {
    fn from(value: [u8; TRIE_HASH_LEN]) -> Self {
        TrieHash(value.into())
    }
}

impl TryFrom<&[u8]> for TrieHash {
    type Error = &'static str;

    fn try_from(value: &[u8]) -> Result<Self, Self::Error> {
        if value.len() == Self::len() {
            let mut hash = TrieHash::default();
            hash.0.copy_from_slice(value);
            Ok(hash)
        } else {
            Err("Invalid length")
        }
    }
}

impl From<GenericArray<u8, typenum::U32>> for TrieHash {
    fn from(value: GenericArray<u8, typenum::U32>) -> Self {
        TrieHash(value)
    }
}

impl TrieHash {
    /// Return the length of a `TrieHash`
    pub(crate) const fn len() -> usize {
        std::mem::size_of::<TrieHash>()
    }

    /// Some code needs a `TrieHash` even though it only has a `HashType`.
    /// This function is a no-op, as `HashType` is a `TrieHash` in this context.
    #[must_use]
    pub const fn into_triehash(self) -> Self {
        self
    }
}

impl Serializable for TrieHash {
    fn write_to<W: ExtendableBytes>(&self, vec: &mut W) {
        vec.extend_from_slice(&self.0);
    }

    fn from_reader<R: std::io::Read>(mut reader: R) -> Result<Self, std::io::Error>
    where
        Self: Sized,
    {
        let mut buf = [0u8; 32];
        reader.read_exact(&mut buf)?;
        Ok(TrieHash::from(buf))
    }
}
