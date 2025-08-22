// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt;

/// A database hash key, used in FFI functions that require hashes.
/// This type requires no allocation and can be copied freely and
/// dropped without any additional overhead.
///
/// This is useful because it is the same size as 4 words which is equivalent
/// to 2 heap-allocated slices (pointer + length each), or 1.5 vectors (which
/// uses an extra word for allocation capacity) and it can be passed around
/// without needing to allocate or deallocate memory.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Default)]
// Must use `repr(C)` instead of `repr(transparent)` to ensure it is a struct
// with one field instead of a type alias of an array of 32 element, which is
// necessary for FFI compatibility so that `HashKey` can be passed by value;
// otherwise, it would look like a pointer to an array of 32 bytes.
#[repr(C)]
pub struct HashKey([u8; 32]);

impl fmt::Display for HashKey {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        super::DisplayHex(&self.0).fmt(f)
    }
}

impl From<firewood::v2::api::HashKey> for HashKey {
    fn from(value: firewood::v2::api::HashKey) -> Self {
        Self(value.into())
    }
}

impl From<HashKey> for firewood::v2::api::HashKey {
    fn from(value: HashKey) -> Self {
        value.0.into()
    }
}
