// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use shale::{CachedStore, ShaleError, Storable};
use std::{
    fmt::{self, Debug},
    io::{Cursor, Write},
};

pub const TRIE_HASH_LEN: usize = 32;

#[derive(PartialEq, Eq, Clone)]
pub struct TrieHash(pub [u8; TRIE_HASH_LEN]);

impl TrieHash {
    const MSIZE: u64 = 32;
}

impl std::ops::Deref for TrieHash {
    type Target = [u8; TRIE_HASH_LEN];
    fn deref(&self) -> &[u8; TRIE_HASH_LEN] {
        &self.0
    }
}

impl Storable for TrieHash {
    fn hydrate<T: CachedStore>(addr: usize, mem: &T) -> Result<Self, ShaleError> {
        let raw = mem
            .get_view(addr, Self::MSIZE)
            .ok_or(ShaleError::InvalidCacheView {
                offset: addr,
                size: Self::MSIZE,
            })?;
        Ok(Self(
            raw.as_deref()[..Self::MSIZE as usize].try_into().unwrap(),
        ))
    }

    fn dehydrated_len(&self) -> u64 {
        Self::MSIZE
    }

    fn dehydrate(&self, to: &mut [u8]) -> Result<(), ShaleError> {
        Cursor::new(to).write_all(&self.0).map_err(ShaleError::Io)
    }
}

impl Debug for TrieHash {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> Result<(), fmt::Error> {
        write!(f, "{}", hex::encode(self.0))
    }
}
