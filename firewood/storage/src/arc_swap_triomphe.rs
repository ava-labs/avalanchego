// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! `RefCnt` bridge so that `arc_swap` can swap `triomphe::Arc` values.
//!
//! `arc-swap` ships `RefCnt` impls for `std::sync::Arc` and `std::rc::Rc` but
//! not for `triomphe::Arc`, which is what `SharedNode` is built on. The orphan
//! rule prevents us from implementing `RefCnt` for `triomphe::Arc` directly,
//! so this module exposes a transparent newtype, [`TriompheArc`], that wraps
//! it and implements `RefCnt` itself.
//!
//! This module is the *only* place in the storage crate that opts out of
//! `deny(unsafe_code)`. The unsafe surface is intentionally minimal: each
//! `RefCnt` method forwards to the matching `triomphe::Arc` raw-pointer API.
//!
//! Soundness rests on three properties that `triomphe::Arc` shares with
//! `std::sync::Arc`:
//!
//! 1. `into_raw` consumes the `Arc` without touching the refcount and returns
//!    a stable pointer to the inner `T`.
//! 2. `from_raw(p)` rehydrates ownership of the same refcount slot, given a
//!    pointer that came from a prior `into_raw`.
//! 3. `as_ptr` returns the same pointer `into_raw` would, without affecting
//!    the refcount.
//!
//! The pointer is non-null for any sized `T` we use here (`Node` is non-ZST),
//! which is what `arc_swap`'s `RefCnt for Option<T>` relies on to distinguish
//! `Some` from `None`.

#![allow(
    unsafe_code,
    reason = "arc_swap::RefCnt requires unsafe trait methods; sealed bridge for triomphe::Arc"
)]

use std::ops::Deref;

use arc_swap::RefCnt;

/// Transparent wrapper around `triomphe::Arc<T>` that implements
/// [`arc_swap::RefCnt`] so the value can live inside an `ArcSwapAny`.
#[repr(transparent)]
pub(crate) struct TriompheArc<T>(triomphe::Arc<T>);

impl<T> TriompheArc<T> {
    pub(crate) const fn new(arc: triomphe::Arc<T>) -> Self {
        Self(arc)
    }

    pub(crate) fn into_inner(self) -> triomphe::Arc<T> {
        self.0
    }
}

impl<T> Clone for TriompheArc<T> {
    fn clone(&self) -> Self {
        Self(triomphe::Arc::clone(&self.0))
    }
}

impl<T> Deref for TriompheArc<T> {
    type Target = triomphe::Arc<T>;
    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

impl<T: std::fmt::Debug> std::fmt::Debug for TriompheArc<T> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.0.fmt(f)
    }
}

// SAFETY: forwards to triomphe::Arc's raw-pointer API, which provides the same
// refcount-stable, ABI-stable guarantees that std::sync::Arc does (see module docs).
unsafe impl<T> RefCnt for TriompheArc<T> {
    type Base = T;

    fn into_ptr(me: Self) -> *mut Self::Base {
        triomphe::Arc::into_raw(me.0).cast_mut()
    }

    fn as_ptr(me: &Self) -> *mut Self::Base {
        triomphe::Arc::as_ptr(&me.0).cast_mut()
    }

    unsafe fn from_ptr(ptr: *const Self::Base) -> Self {
        // SAFETY: `ptr` originated from `Self::into_ptr` (i.e. `triomphe::Arc::into_raw`)
        // per the `RefCnt::from_ptr` contract.
        Self(unsafe { triomphe::Arc::from_raw(ptr) })
    }
}
