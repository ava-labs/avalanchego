// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::{fmt, ptr::NonNull};

/// A type alias for a rust-owned byte slice.
pub type OwnedBytes = OwnedSlice<u8>;

/// A Rust-owned vector of bytes that can be passed to C code.
///
/// C callers must free this memory using the respective FFI function for the
/// concrete type (but not using the `free` function from the C standard library).
#[derive(Debug)]
#[repr(C)]
pub struct OwnedSlice<T> {
    ptr: Option<NonNull<T>>,
    len: usize,
}

impl<T> OwnedSlice<T> {
    /// Dereferences the pointer to the owned slice, returning a slice of the contained type.
    #[must_use]
    pub const fn as_slice(&self) -> &[T] {
        match self.ptr {
            // SAFETY: if the pointer is not null, we are assuming the caller has not changed
            // it from when we originally created the `OwnedSlice`.
            Some(ptr) => unsafe { std::slice::from_raw_parts(ptr.as_ptr(), self.len) },
            None => &[],
        }
    }

    /// Returns a mutable slice of the owned slice, allowing modification of the contained type.
    #[must_use]
    pub const fn as_mut_slice(&mut self) -> &mut [T] {
        match self.ptr {
            // SAFETY: if the pointer is not null, we are assuming the caller has not changed
            // it from when we originally created the `OwnedSlice`.
            Some(ptr) => unsafe { std::slice::from_raw_parts_mut(ptr.as_ptr(), self.len) },
            None => &mut [],
        }
    }

    /// Transforms the owned slice into a boxed slice, consuming the original.
    #[must_use]
    pub fn into_boxed_slice(self) -> Box<[T]> {
        self.into()
    }

    fn take_box(&mut self) -> Box<[T]> {
        match self.ptr.take() {
            // SAFETY: if the pointer is not null, we are assuming the caller has not changed
            // it from when we originally created the `OwnedSlice`. The owned slice was created
            // from a `Box::leak`, so we can safely convert it back using `Box::from_raw`.
            Some(ptr) => unsafe {
                Box::from_raw(std::slice::from_raw_parts_mut(ptr.as_ptr(), self.len))
            },
            None => Box::new([]),
        }
    }
}

impl<T: Clone> Clone for OwnedSlice<T> {
    fn clone(&self) -> Self {
        self.as_slice().to_vec().into()
    }
}

impl<T> AsRef<[T]> for OwnedSlice<T> {
    fn as_ref(&self) -> &[T] {
        self.as_slice()
    }
}

impl<T> AsMut<[T]> for OwnedSlice<T> {
    fn as_mut(&mut self) -> &mut [T] {
        self.as_mut_slice()
    }
}

impl<T> std::borrow::Borrow<[T]> for OwnedSlice<T> {
    fn borrow(&self) -> &[T] {
        self.as_slice()
    }
}

impl<T> std::borrow::BorrowMut<[T]> for OwnedSlice<T> {
    fn borrow_mut(&mut self) -> &mut [T] {
        self.as_mut_slice()
    }
}

impl<T: std::hash::Hash> std::hash::Hash for OwnedSlice<T> {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.as_slice().hash(state);
    }
}

impl<T: PartialEq> PartialEq for OwnedSlice<T> {
    fn eq(&self, other: &Self) -> bool {
        self.as_slice() == other.as_slice()
    }
}

impl<T: Eq> Eq for OwnedSlice<T> {}

impl<T: PartialOrd> PartialOrd for OwnedSlice<T> {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        self.as_slice().partial_cmp(other.as_slice())
    }
}

impl<T: Ord> Ord for OwnedSlice<T> {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.as_slice().cmp(other.as_slice())
    }
}

impl<T> From<Box<[T]>> for OwnedSlice<T> {
    fn from(data: Box<[T]>) -> Self {
        let len = data.len();
        let ptr = NonNull::from(Box::leak(data)).cast::<T>();
        Self {
            ptr: Some(ptr),
            len,
        }
    }
}

impl<T> From<Vec<T>> for OwnedSlice<T> {
    fn from(data: Vec<T>) -> Self {
        data.into_boxed_slice().into()
    }
}

impl<T> From<OwnedSlice<T>> for Box<[T]> {
    fn from(mut owned: OwnedSlice<T>) -> Self {
        owned.take_box()
    }
}

impl<T> Drop for OwnedSlice<T> {
    fn drop(&mut self) {
        drop(self.take_box());
    }
}

impl fmt::Display for OwnedBytes {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        super::DisplayHex(self.as_slice()).fmt(f)
    }
}

// send/sync rules for owned values apply here: if the value is send, the pointer
// is send. if the value is sync, the pointer is sync. This is because we own the
// value and whether or not the pointer can traverse threads is determined by
// the value itself.

// SAFETY: described above
unsafe impl<T: Send> Send for OwnedSlice<T> {}
// SAFETY: described above
unsafe impl<T: Sync> Sync for OwnedSlice<T> {}
