// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::node::ExtendableBytes;
use crate::node::branch::Serializable;
use sha2::digest::generic_array::GenericArray;
use sha2::digest::typenum;
use std::fmt::{self, Debug, Display, Formatter};

/// An error that occurs when trying to convert a slice to a `TrieHash`
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, thiserror::Error)]
#[error("could not convert a slice of {0} bytes to TrieHash (an array of 32 bytes)")]
#[non_exhaustive]
pub struct InvalidTrieHashLength(pub usize);

/// A hash value inside a merkle trie
/// We use the same type as returned by sha2 here to avoid copies
#[derive(PartialEq, Eq, Clone, Hash)]
pub struct TrieHash(GenericArray<u8, typenum::U32>);

/// Intentionally, there is no [`Default`] implementation for [`TrieHash`] to force
/// the user to explicitly decide between an empty RLP hash or a hash of all zeros.
///
/// These unfortunately cannot be `const` because the [`GenericArray`] type does
/// provide a const constructor.
impl TrieHash {
    /// Creates a new `TrieHash` from the default value, which is the all zeros.
    ///
    /// ```
    /// assert_eq!(
    ///     firewood_storage::TrieHash::empty(),
    ///     firewood_storage::TrieHash::from([0; 32]),
    /// )
    /// ```
    #[must_use]
    pub fn empty() -> Self {
        TrieHash([0; TRIE_HASH_LEN].into())
    }
}

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

impl From<[u8; TRIE_HASH_LEN]> for TrieHash {
    fn from(value: [u8; TRIE_HASH_LEN]) -> Self {
        TrieHash(value.into())
    }
}

impl From<TrieHash> for [u8; TRIE_HASH_LEN] {
    fn from(value: TrieHash) -> Self {
        value.0.into()
    }
}

impl TryFrom<&[u8]> for TrieHash {
    type Error = InvalidTrieHashLength;

    fn try_from(value: &[u8]) -> Result<Self, Self::Error> {
        match value.try_into() {
            Ok(array) => Ok(Self::from_bytes(array)),
            Err(_) => Err(InvalidTrieHashLength(value.len())),
        }
    }
}

impl From<GenericArray<u8, typenum::U32>> for TrieHash {
    fn from(value: GenericArray<u8, typenum::U32>) -> Self {
        TrieHash(value)
    }
}

impl TrieHash {
    /// Some code needs a `TrieHash` even though it only has a `HashType`.
    /// This function is a no-op, as `HashType` is a `TrieHash` in this context.
    #[must_use]
    pub const fn into_triehash(self) -> Self {
        self
    }

    /// Creates a new `TrieHash` from an array of bytes.
    #[must_use]
    pub fn from_bytes(bytes: [u8; TRIE_HASH_LEN]) -> Self {
        bytes.into()
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
