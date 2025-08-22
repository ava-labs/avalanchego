// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt;

use firewood::v2::api;

use crate::{HashKey, OwnedBytes};

/// The result type returned from an FFI function that returns no value but may
/// return an error.
#[derive(Debug)]
#[repr(C)]
pub enum VoidResult {
    /// The caller provided a null pointer to the input handle.
    NullHandlePointer,

    /// The operation was successful and no error occurred.
    Ok,

    /// An error occurred and the message is returned as an [`OwnedBytes`]. Its
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl From<()> for VoidResult {
    fn from((): ()) -> Self {
        VoidResult::Ok
    }
}

impl<E: fmt::Display> From<Result<(), E>> for VoidResult {
    fn from(value: Result<(), E>) -> Self {
        match value {
            Ok(()) => VoidResult::Ok,
            Err(err) => VoidResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// The result type returned from the open or create database functions.
#[derive(Debug)]
#[repr(C)]
pub enum HandleResult {
    /// The database was opened or created successfully and the handle is
    /// returned as an opaque pointer.
    ///
    /// The caller must ensure that [`fwd_close_db`] is called to free resources
    /// associated with this handle when it is no longer needed.
    ///
    /// [`fwd_close_db`]: crate::fwd_close_db
    Ok(Box<crate::DatabaseHandle<'static>>),

    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl<E: fmt::Display> From<Result<crate::DatabaseHandle<'static>, E>> for HandleResult {
    fn from(value: Result<crate::DatabaseHandle<'static>, E>) -> Self {
        match value {
            Ok(handle) => HandleResult::Ok(Box::new(handle)),
            Err(err) => HandleResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// A result type returned from FFI functions return the database root hash. This
/// may or may not be after a mutation.
#[derive(Debug)]
#[repr(C)]
pub enum HashResult {
    /// The caller provided a null pointer to a database handle.
    NullHandlePointer,
    /// The proposal resulted in an empty database or the database currently has
    /// no root hash.
    None,
    /// The mutation was successful and the root hash is returned, if this result
    /// was from a mutation. Otherwise, this is the current root hash of the
    /// database.
    Some(HashKey),
    /// An error occurred and the message is returned as an [`OwnedBytes`]. If
    /// value is guaranteed to contain only valid UTF-8.
    ///
    /// The caller must call [`fwd_free_owned_bytes`] to free the memory
    /// associated with this error.
    ///
    /// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
    Err(OwnedBytes),
}

impl<E: fmt::Display> From<Result<Option<api::HashKey>, E>> for HashResult {
    fn from(value: Result<Option<api::HashKey>, E>) -> Self {
        match value {
            Ok(None) => HashResult::None,
            Ok(Some(hash)) => HashResult::Some(HashKey::from(hash)),
            Err(err) => HashResult::Err(err.to_string().into_bytes().into()),
        }
    }
}

/// Helper trait to handle the different result types returned from FFI functions.
///
/// Once Try trait is stable, we can use that instead of this trait:
///
/// ```ignore
/// impl std::ops::FromResidual<Option<std::convert::Infallible>> for VoidResult {
///     #[inline]
///     fn from_residual(residual: Option<std::convert::Infallible>) -> Self {
///         match residual {
///             None => VoidResult::NullHandlePointer,
///             // no other branches are needed because `std::convert::Infallible` is uninhabited
///             // this compiles without error because the compiler knows that Some(_) is impossible
///             // see: https://github.com/rust-lang/rust/blob/3fb1b53a9dbfcdf37a4b67d35cde373316829930/library/core/src/option.rs#L2627-L2631
///             // and: https://doc.rust-lang.org/nomicon/exotic-sizes.html#empty-types
///         }
///     }
/// }
/// ```
pub(crate) trait NullHandleResult: CResult {
    fn null_handle_pointer_error() -> Self;
}

pub(crate) trait CResult: Sized {
    fn from_err(err: impl ToString) -> Self;

    fn from_panic(panic: Box<dyn std::any::Any + Send>) -> Self
    where
        Self: Sized,
    {
        Self::from_err(Panic::from(panic))
    }
}

impl NullHandleResult for VoidResult {
    fn null_handle_pointer_error() -> Self {
        Self::NullHandlePointer
    }
}

impl CResult for VoidResult {
    fn from_err(err: impl ToString) -> Self {
        Self::Err(err.to_string().into_bytes().into())
    }
}

impl CResult for HandleResult {
    fn from_err(err: impl ToString) -> Self {
        Self::Err(err.to_string().into_bytes().into())
    }
}

impl NullHandleResult for HashResult {
    fn null_handle_pointer_error() -> Self {
        Self::NullHandlePointer
    }
}

impl CResult for HashResult {
    fn from_err(err: impl ToString) -> Self {
        Self::Err(err.to_string().into_bytes().into())
    }
}

enum Panic {
    Static(&'static str),
    Formatted(String),
    SendSyncErr(Box<dyn std::error::Error + Send + Sync>),
    SendErr(Box<dyn std::error::Error + Send>),
    Unknown(#[expect(unused)] Box<dyn std::any::Any + Send>),
    // TODO: add variant to capture backtrace with panic hook
    // https://doc.rust-lang.org/stable/std/panic/fn.set_hook.html
}

impl From<Box<dyn std::any::Any + Send>> for Panic {
    fn from(panic: Box<dyn std::any::Any + Send>) -> Self {
        macro_rules! downcast {
            ($Variant:ident($panic:ident)) => {
                let $panic = match $panic.downcast() {
                    Ok(panic) => return Panic::$Variant(*panic),
                    Err(panic) => panic,
                };
            };
        }

        downcast!(Static(panic));
        downcast!(Formatted(panic));
        downcast!(SendSyncErr(panic));
        downcast!(SendErr(panic));

        Self::Unknown(panic)
    }
}

impl fmt::Display for Panic {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Panic::Static(msg) => f.pad(msg),
            Panic::Formatted(msg) => f.pad(msg),
            Panic::SendSyncErr(err) => err.fmt(f),
            Panic::SendErr(err) => err.fmt(f),
            Panic::Unknown(_) => f.pad("unknown panic type recovered"),
        }
    }
}
