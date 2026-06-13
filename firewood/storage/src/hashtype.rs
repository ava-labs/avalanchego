// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[cfg(feature = "ethhash")]
mod ethhash;
mod trie_hash;

pub use trie_hash::{InvalidTrieHashLength, TrieHash};

/// The type of a hash. For ethereum compatible hashes, this might be a RLP encoded
/// value if it's small enough to fit in less than 32 bytes. For merkledb compatible
/// hashes, it's always a `TrieHash`.
#[cfg(feature = "ethhash")]
pub type HashType = ethhash::HashOrRlp;

#[cfg(not(feature = "ethhash"))]
/// The type of a hash. For non-ethereum compatible hashes, this is always a `TrieHash`.
pub type HashType = crate::TrieHash;

/// A trait to convert a value into a [`HashType`].
///
/// This is used to allow different hash types to be conditionally used, e.g., when the
/// `ethhash` feature is enabled. When not enabled, this suppresses the clippy warnings
/// about useless `.into()` calls.
pub trait IntoHashType {
    /// Converts the value into a `HashType`.
    #[must_use]
    fn into_hash_type(self) -> HashType;
}

impl IntoHashType for crate::TrieHash {
    #[inline]
    fn into_hash_type(self) -> HashType {
        #[cfg(feature = "ethhash")]
        {
            self.into()
        }

        #[cfg(not(feature = "ethhash"))]
        {
            self
        }
    }
}
