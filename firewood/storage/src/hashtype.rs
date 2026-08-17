// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use sha2::Digest as _;
use sha3::Keccak256;
use smallvec::SmallVec;
use std::{
    fmt::{Display, Formatter},
    io::Read,
};

use crate::{
    TrieHash,
    node::{ExtendableBytes, branch::Serializable},
};

/// The type of a hash. For ethereum compatible hashes, this might be a RLP encoded
/// value if it's small enough to fit in less than 32 bytes. For merkledb compatible
/// hashes, it's always a `TrieHash` (carried as the `Hash` variant).
///
/// This is an unconditional enum so that both hashing schemes can be compiled
/// into a single binary. The on-disk byte layout per scheme is governed by the
/// per-mode codec on [`HashMode`](crate::HashMode), not by this type directly.
#[derive(Clone)]
pub enum HashType {
    /// A full 32-byte trie hash. This is the only variant produced under
    /// merkledb-compatible hashing.
    Hash(TrieHash),
    // TODO(rkuris): this slice is never larger than 32 bytes so smallvec is probably not our best container
    // the length is stored in a `usize` but it could be in a `u8` and it will never overflow
    /// An inline RLP encoding, used for ethereum-compatible hashes small enough
    /// (less than 32 bytes) to be stored in place of a hash.
    Rlp(SmallVec<[u8; 32]>),
}

/// Manual implementation of [`Debug`](std::fmt::Debug) so that the RLP bytes
/// are displayed as hex rather than raw bytes, which is more useful for
/// debugging purposes.
impl std::fmt::Debug for HashType {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            HashType::Hash(h) => write!(f, "Hash({h})"),
            HashType::Rlp(r) => write!(f, "Rlp({})", hex::encode(r)),
        }
    }
}

impl HashType {
    /// Creates a new `HashType` from the default value, which is all zeros (the
    /// `Hash` variant wrapping a zeroed `TrieHash`).
    ///
    /// ```
    /// assert_eq!(
    ///     firewood_storage::HashType::empty(),
    ///     firewood_storage::HashType::from([0; 32]),
    /// )
    /// ```
    #[must_use]
    pub fn empty() -> Self {
        TrieHash::empty().into()
    }

    /// Returns the underlying bytes as a slice: the raw hash for the `Hash`
    /// variant, or the RLP bytes for the `Rlp` variant.
    #[must_use]
    pub fn as_slice(&self) -> &[u8] {
        self
    }

    /// Converts into a [`TrieHash`], hashing the RLP bytes with Keccak-256 for
    /// the `Rlp` variant.
    #[must_use]
    pub fn into_triehash(self) -> TrieHash {
        self.into()
    }
}

impl PartialEq<TrieHash> for HashType {
    fn eq(&self, other: &TrieHash) -> bool {
        match self {
            HashType::Hash(h) => h == other,
            HashType::Rlp(r) => Keccak256::digest(r.as_ref()).as_slice() == other.as_ref(),
        }
    }
}

impl PartialEq<HashType> for TrieHash {
    fn eq(&self, other: &HashType) -> bool {
        match other {
            HashType::Hash(h) => h == self,
            HashType::Rlp(r) => Keccak256::digest(r.as_ref()).as_slice() == self.as_ref(),
        }
    }
}

impl PartialEq for HashType {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            // if both are hash or rlp, we can skip hashing
            (HashType::Hash(h1), HashType::Hash(h2)) => h1 == h2,
            (HashType::Rlp(r1), HashType::Rlp(r2)) => r1 == r2,
            // otherwise, one is a hash and the other isn't, so hash the rlp and compare
            (HashType::Hash(h), HashType::Rlp(r)) | (HashType::Rlp(r), HashType::Hash(h)) => {
                // avoid copying by comparing the hash directly; copying into a
                // TrieHash adds a noticeable overhead
                Keccak256::digest(r).as_slice() == h.as_ref()
            }
        }
    }
}

impl Eq for HashType {}

impl std::hash::Hash for HashType {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        // contract on `Hash` and `PartialEq` requires that if `a == b` then `hash(a) == hash(b)`
        // and since `PartialEq` may require hashing, we must always convert to `TrieHash` here
        // and use it's hash implementation
        match self {
            Self::Hash(h) => h.hash(state),
            // NB: same `hash` impl as [u8; 32] which TrieHash also uses, prevents copying twice
            Self::Rlp(r) => Keccak256::digest(r.as_ref()).hash(state),
        }
    }
}

impl Serializable for HashType {
    fn write_to<W: ExtendableBytes>(&self, vec: &mut W) {
        match self {
            HashType::Hash(h) => {
                vec.push(0);
                vec.extend_from_slice(h.as_ref());
            }
            HashType::Rlp(r) => {
                debug_assert!(!r.is_empty());
                debug_assert!(r.len() < 32);
                vec.push(r.len() as u8);
                vec.extend_from_slice(r.as_ref());
            }
        }
    }

    fn from_reader<R: Read>(mut reader: R) -> Result<Self, std::io::Error> {
        let mut bytes = [0; 32];

        reader.read_exact(&mut bytes[0..1])?;
        let Some(len) = std::num::NonZeroU8::new(bytes[0]) else {
            // length is zero, so it's a full trie hash
            reader.read_exact(&mut bytes)?;
            return Ok(HashType::Hash(TrieHash::from(bytes)));
        };

        let Some(bytes_mut) = bytes.get_mut(..len.get() as usize) else {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("invalid RLP length; expected strictly less than 32, got {len}"),
            ));
        };

        reader.read_exact(bytes_mut)?;
        Ok(HashType::Rlp(SmallVec::from_buf_and_len(
            bytes,
            len.get() as usize,
        )))
    }
}

impl From<HashType> for TrieHash {
    fn from(val: HashType) -> Self {
        match val {
            HashType::Hash(h) => h,
            HashType::Rlp(r) => Keccak256::digest(&r).into(),
        }
    }
}

impl From<&HashType> for TrieHash {
    fn from(val: &HashType) -> Self {
        match val {
            HashType::Hash(h) => h.clone(),
            HashType::Rlp(r) => Keccak256::digest(r).into(),
        }
    }
}

impl From<TrieHash> for HashType {
    fn from(val: TrieHash) -> Self {
        HashType::Hash(val)
    }
}

impl From<[u8; 32]> for HashType {
    fn from(value: [u8; 32]) -> Self {
        HashType::Hash(TrieHash::into(value.into()))
    }
}

impl TryFrom<&[u8]> for HashType {
    type Error = crate::InvalidTrieHashLength;

    fn try_from(value: &[u8]) -> Result<Self, Self::Error> {
        value.try_into().map(HashType::Hash)
    }
}

impl AsRef<[u8]> for HashType {
    fn as_ref(&self) -> &[u8] {
        match self {
            HashType::Hash(h) => h.as_ref(),
            HashType::Rlp(r) => r.as_ref(),
        }
    }
}

impl std::ops::Deref for HashType {
    type Target = [u8];
    fn deref(&self) -> &Self::Target {
        match self {
            HashType::Hash(h) => h.as_slice(),
            HashType::Rlp(r) => r,
        }
    }
}

impl Display for HashType {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            HashType::Hash(h) => write!(f, "{h}"),
            HashType::Rlp(r) => {
                let width = f.precision().unwrap_or(32);
                write!(f, "{:.*}", width, hex::encode(r))
            }
        }
    }
}
