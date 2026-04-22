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

#[derive(Clone)]
pub enum HashOrRlp {
    Hash(TrieHash),
    // TODO: this slice is never larger than 32 bytes so smallvec is probably not our best container
    // the length is stored in a `usize` but it could be in a `u8` and it will never overflow
    Rlp(SmallVec<[u8; 32]>),
}

/// Manual implementation of [`Debug`](std::fmt::Debug) so that the RLP bytes
/// are displayed as hex rather than raw bytes, which is more useful for
/// debugging purposes.
impl std::fmt::Debug for HashOrRlp {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            HashOrRlp::Hash(h) => write!(f, "Hash({h})"),
            HashOrRlp::Rlp(r) => write!(f, "Rlp({})", hex::encode(r)),
        }
    }
}

impl HashOrRlp {
    /// Creates a new `TrieHash` from the default value, which is the all zeros.
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

    pub fn as_slice(&self) -> &[u8] {
        self
    }

    pub fn into_triehash(self) -> TrieHash {
        self.into()
    }
}

impl PartialEq<TrieHash> for HashOrRlp {
    fn eq(&self, other: &TrieHash) -> bool {
        match self {
            HashOrRlp::Hash(h) => h == other,
            HashOrRlp::Rlp(r) => Keccak256::digest(r.as_ref()).as_slice() == other.as_ref(),
        }
    }
}

impl PartialEq<HashOrRlp> for TrieHash {
    fn eq(&self, other: &HashOrRlp) -> bool {
        match other {
            HashOrRlp::Hash(h) => h == self,
            HashOrRlp::Rlp(r) => Keccak256::digest(r.as_ref()).as_slice() == self.as_ref(),
        }
    }
}

impl PartialEq for HashOrRlp {
    fn eq(&self, other: &Self) -> bool {
        match (self, other) {
            // if both are hash or rlp, we can skip hashing
            (HashOrRlp::Hash(h1), HashOrRlp::Hash(h2)) => h1 == h2,
            (HashOrRlp::Rlp(r1), HashOrRlp::Rlp(r2)) => r1 == r2,
            // otherwise, one is a hash and the other isn't, so hash the rlp and compare
            (HashOrRlp::Hash(h), HashOrRlp::Rlp(r)) | (HashOrRlp::Rlp(r), HashOrRlp::Hash(h)) => {
                // avoid copying by comparing the hash directly; copying into a
                // TrieHash adds a noticeable overhead
                Keccak256::digest(r).as_slice() == h.as_ref()
            }
        }
    }
}

impl Eq for HashOrRlp {}

impl std::hash::Hash for HashOrRlp {
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

impl Serializable for HashOrRlp {
    fn write_to<W: ExtendableBytes>(&self, vec: &mut W) {
        match self {
            HashOrRlp::Hash(h) => {
                vec.push(0);
                vec.extend_from_slice(h.as_ref());
            }
            HashOrRlp::Rlp(r) => {
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
            return Ok(HashOrRlp::Hash(TrieHash::from(bytes)));
        };

        let Some(bytes_mut) = bytes.get_mut(..len.get() as usize) else {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("invalid RLP length; expected strictly less than 32, got {len}"),
            ));
        };

        reader.read_exact(bytes_mut)?;
        Ok(HashOrRlp::Rlp(SmallVec::from_buf_and_len(
            bytes,
            len.get() as usize,
        )))
    }
}

impl From<HashOrRlp> for TrieHash {
    fn from(val: HashOrRlp) -> Self {
        match val {
            HashOrRlp::Hash(h) => h,
            HashOrRlp::Rlp(r) => Keccak256::digest(&r).into(),
        }
    }
}

impl From<&HashOrRlp> for TrieHash {
    fn from(val: &HashOrRlp) -> Self {
        match val {
            HashOrRlp::Hash(h) => h.clone(),
            HashOrRlp::Rlp(r) => Keccak256::digest(r).into(),
        }
    }
}

impl From<TrieHash> for HashOrRlp {
    fn from(val: TrieHash) -> Self {
        HashOrRlp::Hash(val)
    }
}

impl From<[u8; 32]> for HashOrRlp {
    fn from(value: [u8; 32]) -> Self {
        HashOrRlp::Hash(TrieHash::into(value.into()))
    }
}

impl TryFrom<&[u8]> for HashOrRlp {
    type Error = crate::InvalidTrieHashLength;

    fn try_from(value: &[u8]) -> Result<Self, Self::Error> {
        value.try_into().map(HashOrRlp::Hash)
    }
}

impl AsRef<[u8]> for HashOrRlp {
    fn as_ref(&self) -> &[u8] {
        match self {
            HashOrRlp::Hash(h) => h.as_ref(),
            HashOrRlp::Rlp(r) => r.as_ref(),
        }
    }
}

impl std::ops::Deref for HashOrRlp {
    type Target = [u8];
    fn deref(&self) -> &Self::Target {
        match self {
            HashOrRlp::Hash(h) => h.as_slice(),
            HashOrRlp::Rlp(r) => r,
        }
    }
}

impl Display for HashOrRlp {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            HashOrRlp::Hash(h) => write!(f, "{h}"),
            HashOrRlp::Rlp(r) => {
                let width = f.precision().unwrap_or(32);
                write!(f, "{:.*}", width, hex::encode(r))
            }
        }
    }
}
