//! Avalanche utility types and functions.
//!
//! This crate provides common utilities used throughout Avalanche:
//! - [`Set`]: A generic set implementation
//! - [`Bag`]: A multiset (bag) implementation with threshold support
//! - [`logging`]: Logging configuration utilities
//! - [`errors`]: Common error handling utilities

pub mod bag;
pub mod errors;
pub mod logging;
pub mod set;
pub mod timer;

pub use bag::Bag;
pub use set::Set;

/// Returns the zero value for a type.
///
/// This is equivalent to Go's zero value semantics.
#[must_use]
pub fn zero<T: Default>() -> T {
    T::default()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_zero() {
        assert_eq!(zero::<i32>(), 0);
        assert_eq!(zero::<String>(), "");
        assert!(zero::<Option<i32>>().is_none());
    }
}
