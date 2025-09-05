// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

mod borrowed;
mod display_hex;
mod hash_key;
mod kvp;
mod owned;
mod results;

pub use self::borrowed::{BorrowedBytes, BorrowedKeyValuePairs, BorrowedSlice};
use self::display_hex::DisplayHex;
pub use self::hash_key::HashKey;
pub use self::kvp::KeyValuePair;
pub use self::owned::{OwnedBytes, OwnedSlice};
pub(crate) use self::results::{CResult, NullHandleResult};
pub use self::results::{
    ChangeProofResult, HandleResult, HashResult, NextKeyRangeResult, RangeProofResult, ValueResult,
    VoidResult,
};

/// Maybe is a C-compatible optional type using a tagged union pattern.
///
/// FFI methods and types can use this to represent optional values where `Optional<T>`
/// does not work due to it not having C-compatible layout.
#[derive(Debug)]
#[repr(C)]
pub enum Maybe<T> {
    /// No value present.
    None,
    /// A value is present.
    Some(T),
}
