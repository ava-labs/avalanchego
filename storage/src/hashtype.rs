// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod ethhash;
mod trie_hash;

pub use ethhash::HashOrRlp;
pub use trie_hash::{InvalidTrieHashLength, TrieHash};

/// The type of a hash. For ethereum compatible hashes, this might be a RLP encoded
/// value if it's small enough to fit in less than 32 bytes. For merkledb compatible
/// hashes, it's always a `TrieHash` (carried as the `Hash` variant).
pub type HashType = HashOrRlp;

/// A trait to convert a value into a [`HashType`].
pub trait IntoHashType {
    /// Converts the value into a `HashType`.
    #[must_use]
    fn into_hash_type(self) -> HashType;
}

impl IntoHashType for crate::TrieHash {
    #[inline]
    fn into_hash_type(self) -> HashType {
        self.into()
    }
}
