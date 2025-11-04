// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt;

use crate::value::BorrowedBytes;
use crate::{OwnedBytes, OwnedSlice};
use firewood::v2::api;

/// A type alias for a rust-owned byte slice.
pub type OwnedKeyValueBatch = OwnedSlice<OwnedKeyValuePair>;

/// A `KeyValue` represents a key-value pair, passed to the FFI.
#[repr(C)]
#[derive(Debug, Clone, Copy)]
pub struct KeyValuePair<'a> {
    pub key: BorrowedBytes<'a>,
    pub value: BorrowedBytes<'a>,
}

impl<'a> KeyValuePair<'a> {
    pub fn new((key, value): &'a (impl AsRef<[u8]>, impl AsRef<[u8]>)) -> Self {
        Self {
            key: BorrowedBytes::from_slice(key.as_ref()),
            value: BorrowedBytes::from_slice(value.as_ref()),
        }
    }
}

impl fmt::Display for KeyValuePair<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let precision = f.precision().unwrap_or(64);
        write!(
            f,
            "Key: {:.precision$}, Value: {:.precision$}",
            self.key, self.value
        )
    }
}

impl<'a> api::KeyValuePair for KeyValuePair<'a> {
    type Key = BorrowedBytes<'a>;
    type Value = BorrowedBytes<'a>;

    #[inline]
    fn into_batch(self) -> api::BatchOp<Self::Key, Self::Value> {
        // Check if the value pointer is null (nil slice in Go)
        // vs non-null but empty (empty slice []byte{} in Go)
        if self.value.is_null() {
            api::BatchOp::DeleteRange { prefix: self.key }
        } else {
            api::BatchOp::Put {
                key: self.key,
                value: self.value,
            }
        }
    }
}

impl<'a> api::KeyValuePair for &KeyValuePair<'a> {
    type Key = BorrowedBytes<'a>;
    type Value = BorrowedBytes<'a>;

    #[inline]
    fn into_batch(self) -> api::BatchOp<Self::Key, Self::Value> {
        (*self).into_batch()
    }
}

/// Owned version of `KeyValuePair`, returned to ffi callers.
///
/// C callers must free this using [`crate::fwd_free_owned_kv_pair`],
/// not the C standard library's `free` function.
#[repr(C)]
#[derive(Debug, Clone)]
pub struct OwnedKeyValuePair {
    pub key: OwnedBytes,
    pub value: OwnedBytes,
}

impl From<(Box<[u8]>, Box<[u8]>)> for OwnedKeyValuePair {
    fn from(value: (Box<[u8]>, Box<[u8]>)) -> Self {
        OwnedKeyValuePair {
            key: value.0.into(),
            value: value.1.into(),
        }
    }
}
