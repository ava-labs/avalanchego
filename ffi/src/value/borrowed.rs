// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt;

use crate::value::KeyValuePair;

/// A type alias for a borrowed byte slice.
///
/// C callers can use this to pass in a byte slice that will not be freed by Rust
/// code.
///
/// C callers must ensure that the pointer, if not null, points to a valid slice
/// of bytes of length `len`. C callers must also ensure that the slice is valid
/// for the duration of the C function call that was passed this slice.
pub type BorrowedBytes<'a> = BorrowedSlice<'a, u8>;

/// A type alias for a borrowed slice of [`KeyValuePair`]s.
///
/// C callers can use this to pass in a slice of key-value pairs that will not
/// be freed by Rust code.
///
/// C callers must ensure that the pointer, if not null, points to a valid slice
/// of key-value pairs of length `len`. C callers must also ensure that the slice
/// is valid for the duration of the C function call that was passed this slice.
pub type BorrowedKeyValuePairs<'a> = BorrowedSlice<'a, KeyValuePair<'a>>;

/// A borrowed byte slice. Used to represent data that was passed in from C
/// callers and will not be freed or retained by Rust code.
#[derive(Debug, Clone, Copy)]
#[repr(C)]
pub struct BorrowedSlice<'a, T> {
    /// A pointer to the slice of bytes. This can be null if the slice is empty.
    ///
    /// If the pointer is not null, it must point to a valid slice of `len`
    /// elements sized and aligned for `T`.
    ///
    /// As a note, [`NonNull`] is not appropriate here because [`NonNull`] pointer
    /// provenance requires mutable access to the pointer, which is not an invariant
    /// we want to enforce here. We want (and require) the pointer to be immutable.
    ///
    /// [`NonNull`]: std::ptr::NonNull
    ptr: *const T,
    /// The length of the slice. It is ignored if the pointer is null; however,
    /// if the pointer is not null, it must be equal to the number of elements
    /// pointed to by `ptr`.
    len: usize,
    /// This is not exposed to C callers, but is used by Rust to track the
    /// lifetime of the slice passed in to C functions.
    marker: std::marker::PhantomData<&'a [T]>,
}

impl<'a, T> BorrowedSlice<'a, T> {
    /// Creates a slice from the given pointer and length.
    #[must_use]
    pub const fn as_slice(&self) -> &'a [T] {
        if self.ptr.is_null() {
            &[]
        } else {
            // SAFETY: if the pointer is not null, we are assuming the caller has
            // upheld the invariant that the pointer is valid for the length `len`
            // `T` aligned elements. The phantom marker ensures that the lifetime
            // of the returned slice is the same as the lifetime of the `BorrowedSlice`.
            unsafe { std::slice::from_raw_parts(self.ptr, self.len) }
        }
    }

    /// Creates a new `BorrowedSlice` from the given rust slice.
    #[must_use]
    pub const fn from_slice(slice: &'a [T]) -> Self {
        let len = slice.len();
        Self {
            ptr: std::ptr::from_ref(slice).cast(),
            len,
            marker: std::marker::PhantomData,
        }
    }

    /// Returns true if the pointer is null.
    /// This is used to differentiate between a nil slice and an empty slice.
    #[must_use]
    pub const fn is_null(&self) -> bool {
        self.ptr.is_null()
    }
}

impl<T> std::ops::Deref for BorrowedSlice<'_, T> {
    type Target = [T];

    fn deref(&self) -> &Self::Target {
        self.as_slice()
    }
}

impl<T> AsRef<[T]> for BorrowedSlice<'_, T> {
    fn as_ref(&self) -> &[T] {
        self.as_slice()
    }
}

impl<T> std::borrow::Borrow<[T]> for BorrowedSlice<'_, T> {
    fn borrow(&self) -> &[T] {
        self.as_slice()
    }
}

impl<T: std::hash::Hash> std::hash::Hash for BorrowedSlice<'_, T> {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        self.as_slice().hash(state);
    }
}

impl<T: PartialEq> PartialEq for BorrowedSlice<'_, T> {
    fn eq(&self, other: &Self) -> bool {
        self.as_slice() == other.as_slice()
    }
}

impl<T: Eq> Eq for BorrowedSlice<'_, T> {}

impl<T: PartialOrd> PartialOrd for BorrowedSlice<'_, T> {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        self.as_slice().partial_cmp(other.as_slice())
    }
}

impl<T: Ord> Ord for BorrowedSlice<'_, T> {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        self.as_slice().cmp(other.as_slice())
    }
}

impl fmt::Display for BorrowedBytes<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let precision = f.precision().unwrap_or(64);
        write!(f, "{:.precision$}", super::DisplayHex(self.as_slice()))
    }
}

impl fmt::Pointer for BorrowedBytes<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        self.ptr.fmt(f)
    }
}

impl<'a> BorrowedBytes<'a> {
    /// Creates a new [`str`] from this borrowed byte slice.
    ///
    /// # Errors
    ///
    /// If the slice is not valid UTF-8, an error is returned.
    pub const fn as_str(&self) -> Result<&'a str, std::str::Utf8Error> {
        // C callers are expected to pass a valid UTF-8 string for the path, even
        // on Windows. Go does not handle UTF-16 paths on Windows, like Rust does,
        // so we do not need to handle that here as well.
        std::str::from_utf8(self.as_slice())
    }
}

// send/sync rules for references apply here: the pointer is send and sync if and
// only if the value is sync. the value does not need to be send for the pointer
// to be send because this pointer is not moving the value across threads, only
// the reference to the value.

// SAFETY: described above
unsafe impl<T: Sync> Send for BorrowedSlice<'_, T> {}
// SAFETY: described above
unsafe impl<T: Sync> Sync for BorrowedSlice<'_, T> {}
