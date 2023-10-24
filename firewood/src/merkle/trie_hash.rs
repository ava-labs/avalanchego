// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use shale::{CachedStore, ShaleError, Storable};
use std::{
    fmt::{self, Debug},
    io::Write,
};

pub const TRIE_HASH_LEN: usize = 32;
const U64_TRIE_HASH_LEN: u64 = TRIE_HASH_LEN as u64;

#[derive(PartialEq, Eq, Clone)]
pub struct TrieHash(pub [u8; TRIE_HASH_LEN]);

impl std::ops::Deref for TrieHash {
    type Target = [u8; TRIE_HASH_LEN];
    fn deref(&self) -> &[u8; TRIE_HASH_LEN] {
        &self.0
    }
}

impl Storable for TrieHash {
    fn hydrate<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, U64_TRIE_HASH_LEN)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: U64_TRIE_HASH_LEN,
            })?;

        Ok(Self(raw.as_deref()[..TRIE_HASH_LEN].try_into().unwrap()))
    }

    fn dehydrated_len(&self) -> u64 {
        U64_TRIE_HASH_LEN
    }

    fn dehydrate(&self, mut to: &mut [u8]) -> Result<(), ShaleError> {
        to.write_all(&self.0).map_err(ShaleError::Io)
    }
}

impl Debug for TrieHash {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "{}", hex::encode(self.0))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_dehydrate() {
        let zero_hash = TrieHash([0u8; TRIE_HASH_LEN]);

        let mut to = [1u8; TRIE_HASH_LEN];
        zero_hash.dehydrate(&mut to).unwrap();

        assert_eq!(&to, &zero_hash.0);
    }
}
