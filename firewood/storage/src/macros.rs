// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#[macro_export]
#[cfg(test)]
/// Macro to create an `AreaIndex` from a literal value at compile time.
/// This macro performs bounds checking at compile time and panics if the value is out of bounds.
///
/// Usage:
///   `area_index!(0)` - creates an `AreaIndex` with value 0
///   `area_index!(23)` - creates an `AreaIndex` with value 23
///
/// The macro will panic at compile time if the value is negative or >= `NUM_AREA_SIZES`.
macro_rules! area_index {
    ($v:expr) => {
        const {
            match $crate::nodestore::primitives::AreaIndex::new($v as u8) {
                Some(v) => v,
                None => panic!("Constant area index out of bounds"),
            }
        }
    };
}
