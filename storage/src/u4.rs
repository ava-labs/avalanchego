// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! The 4-bit hexary-trie index type and its conversion error, re-exported from
//! [`arity_arrays`].
//!
//! [`TryFromIntError`] is `#[non_exhaustive]`, so it is obtained from
//! [`U4`]'s `TryFrom<u8>` at the conversion sites in
//! [`PathComponent`](crate::PathComponent) rather than constructed directly.

pub use arity_arrays::index::{TryFromIntError, U4};
