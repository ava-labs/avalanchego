// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt;

use crate::value::BorrowedBytes;
use crate::{OwnedBytes, OwnedSlice};
use firewood::v2::api;

/// A type alias for a rust-owned byte slice.
pub type OwnedKeyValueBatch = OwnedSlice<OwnedKeyValuePair>;

/// A batch operation passed to the FFI.
///
/// This is a tagged union that explicitly distinguishes between different
/// operation types instead of relying on nil vs empty pointer semantics.
#[derive(Debug, Clone, Copy)]
#[repr(C, usize)]
pub enum BatchOp<'a> {
    /// Insert or update a key with a value.
    /// The value may be empty (zero-length).
    Put {
        key: BorrowedBytes<'a>,
        value: BorrowedBytes<'a>,
    },
    /// Delete a specific key.
    Delete { key: BorrowedBytes<'a> },
    /// Delete all keys with a given prefix.
    DeleteRange { prefix: BorrowedBytes<'a> },
}

impl<'a> BatchOp<'a> {
    /// Creates a new `BatchOp::Put` operation.
    pub fn new((key, value): &'a (impl AsRef<[u8]>, impl AsRef<[u8]>)) -> Self {
        Self::Put {
            key: BorrowedBytes::from_slice(key.as_ref()),
            value: BorrowedBytes::from_slice(value.as_ref()),
        }
    }
}

impl fmt::Display for BatchOp<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let precision = f.precision().unwrap_or(64);
        match self {
            BatchOp::Put { key, value } => {
                write!(f, "Put(Key: {key:.precision$}, Value: {value:.precision$})")
            }
            BatchOp::Delete { key } => write!(f, "Delete(Key: {key:.precision$})"),
            BatchOp::DeleteRange { prefix } => {
                write!(f, "DeleteRange(Prefix: {prefix:.precision$})")
            }
        }
    }
}

impl<'a> api::TryIntoBatch for BatchOp<'a> {
    type Key = BorrowedBytes<'a>;
    type Value = BorrowedBytes<'a>;
    type Error = std::convert::Infallible;

    #[inline]
    fn try_into_batch(self) -> Result<api::BatchOp<Self::Key, Self::Value>, Self::Error> {
        Ok(match self {
            BatchOp::Put { key, value } => api::BatchOp::Put { key, value },
            BatchOp::Delete { key } => api::BatchOp::Delete { key },
            BatchOp::DeleteRange { prefix } => api::BatchOp::DeleteRange { prefix },
        })
    }
}

impl<'a> api::TryIntoBatch for &BatchOp<'a> {
    type Key = BorrowedBytes<'a>;
    type Value = BorrowedBytes<'a>;
    type Error = std::convert::Infallible;

    #[inline]
    fn try_into_batch(self) -> Result<api::BatchOp<Self::Key, Self::Value>, Self::Error> {
        (*self).try_into_batch()
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
