// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Shared code-hash iteration support for range and change proofs.
//!
//! Both proof types can yield the set of contract code hashes referenced by
//! their account values (Ethereum tries only). The extraction is pure RLP
//! parsing of bytes already in the proof — no verification is required.
//!
//! Per-proof-type entry points live in `range.rs` and `change.rs`; this
//! module owns the shared [`CodeIteratorHandle`], the per-element extraction
//! logic, and the type-agnostic FFI exports (`fwd_code_hash_iter_next`,
//! `fwd_code_hash_iter_free`).

#[cfg(feature = "ethhash")]
use firewood_storage::{RlpList, TrieHash};

#[cfg(feature = "ethhash")]
use firewood::ProofError;

use firewood::api::{self, BatchOp};

use crate::{HashKey, HashResult, VoidResult};

#[cfg(feature = "ethhash")]
const EMPTY_CODE_HASH: [u8; 32] = [
    // "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
    0xc5, 0xd2, 0x46, 0x01, 0x86, 0xf7, 0x23, 0x3c, 0x92, 0x7e, 0x7d, 0xb2, 0xdc, 0xc7, 0x03, 0xc0,
    0xe5, 0x00, 0xb6, 0x53, 0xca, 0x82, 0x27, 0x3b, 0x7b, 0xfa, 0xd8, 0x04, 0x5d, 0x85, 0xa4, 0x70,
];

#[non_exhaustive]
pub struct CodeIteratorHandle<'p> {
    #[cfg(feature = "ethhash")]
    inner: BoxCodeHashIter<'p>,
    // uninhabitable fields make the struct impossible to construct when the feature is disabled
    #[cfg(not(feature = "ethhash"))]
    void: std::convert::Infallible,
    #[cfg(not(feature = "ethhash"))]
    marker: std::marker::PhantomData<&'p ()>,
}

impl std::fmt::Debug for CodeIteratorHandle<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("CodeIteratorHandle").finish_non_exhaustive()
    }
}

type KeyValuePair = (Box<[u8]>, Box<[u8]>);

#[cfg(feature = "ethhash")]
type BoxCodeHashIter<'p> = Box<dyn Iterator<Item = Result<HashKey, api::Error>> + 'p>;

#[cfg(feature = "ethhash")]
fn extract_code_hash(key: &[u8], value: &[u8]) -> Option<Result<HashKey, api::Error>> {
    if key.len() != 32 {
        return None;
    }

    let Ok(code_hash_slice) = RlpList::parse(value).and_then(|l| l.nth_bytes(3)) else {
        return Some(Err(api::Error::ProofError(ProofError::InvalidValueFormat)));
    };
    let code_hash: HashKey = TrieHash::try_from(code_hash_slice).ok()?.into();
    if code_hash == TrieHash::from(EMPTY_CODE_HASH).into() {
        return None;
    }

    Some(Ok(code_hash))
}

impl Iterator for CodeIteratorHandle<'_> {
    type Item = Result<HashKey, api::Error>;

    fn next(&mut self) -> Option<Self::Item> {
        #[cfg(not(feature = "ethhash"))]
        match self.void {}

        #[cfg(feature = "ethhash")]
        self.inner.next()
    }
}

impl<'p> CodeIteratorHandle<'p> {
    /// Create a new code hash iterator from the given key/value pairs.
    /// The key/value pairs should be the raw entries from the
    /// underlying proof.
    ///
    /// The iterator must be freed after use.
    ///
    /// Arguments:
    /// - `key_values` - The key/value pairs from the proof.
    ///
    /// Returns:
    /// - `Ok(CodeIteratorHandle)` if the iterator was successfully created.
    /// - `Err(api::Error)` if the iterator could not be created.
    ///
    /// # Errors
    ///
    /// - Returns `api::Error::FeatureNotSupported` if the `ethhash` feature
    ///   is not enabled.
    #[cfg_attr(not(feature = "ethhash"), allow(unused_variables))]
    pub fn from_key_values(key_values: &'p [KeyValuePair]) -> Result<Self, api::Error> {
        #[cfg(not(feature = "ethhash"))]
        {
            Err(api::Error::FeatureNotSupported(
                "ethhash code hash iterator".to_owned(),
            ))
        }

        #[cfg(feature = "ethhash")]
        {
            Ok(CodeIteratorHandle {
                inner: Box::new(
                    key_values
                        .iter()
                        .filter_map(|(key, value)| extract_code_hash(key, value)),
                ),
            })
        }
    }

    /// Create a new code hash iterator from the given change-proof batch
    /// operations. Non-`Put` ops are skipped, and `Put` ops whose key is
    /// not 32 bytes or whose value does not RLP-decode as an account are
    /// also skipped.
    ///
    /// The iterator must be freed after use.
    ///
    /// Arguments:
    /// - `batch_ops` - The batch operations from a change proof.
    ///
    /// Returns:
    /// - `Ok(CodeIteratorHandle)` if the iterator was successfully created.
    /// - `Err(api::Error)` if the iterator could not be created.
    ///
    /// # Errors
    ///
    /// - Returns `api::Error::FeatureNotSupported` if the `ethhash` feature
    ///   is not enabled.
    #[cfg_attr(not(feature = "ethhash"), allow(unused_variables))]
    pub fn from_batch_ops(
        batch_ops: &'p [BatchOp<firewood::Key, firewood::Value>],
    ) -> Result<Self, api::Error> {
        #[cfg(not(feature = "ethhash"))]
        {
            Err(api::Error::FeatureNotSupported(
                "ethhash code hash iterator".to_owned(),
            ))
        }

        #[cfg(feature = "ethhash")]
        {
            Ok(CodeIteratorHandle {
                inner: Box::new(batch_ops.iter().filter_map(|op| match op {
                    BatchOp::Put { key, value } => extract_code_hash(key, value),
                    _ => None,
                })),
            })
        }
    }
}

/// Advances the code hash iterator and returns the next code hash.
///
/// # Arguments
///
/// - `iter` - A [`CodeIteratorHandle`] previously returned from a proof's
///   code-hash-iterator function.
///
/// # Returns
///
/// - [`HashResult::NullHandlePointer`] if the caller provided a null pointer.
/// - [`HashResult::Some`] containing the next code hash if successful.
/// - [`HashResult::None`] if there are no more code hashes to iterate over.
/// - [`HashResult::Err`] containing an error message if the next code hash could not be retrieved.
///
/// # Thread Safety
///
/// It is not safe to call this function concurrently with the same iterator
/// nor is it safe to call any other function that accesses the same iterator
/// concurrently. The caller must ensure exclusive access to the iterator
/// for the duration of the call.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_code_hash_iter_next<'a>(
    iter: Option<&'a mut CodeIteratorHandle<'a>>,
) -> HashResult {
    crate::invoke_with_handle(iter, CodeIteratorHandle::next)
}

/// Frees the memory associated with a `CodeIteratorHandle`.
///
/// # Arguments
///
/// - `iter` - The `CodeIteratorHandle` to free, previously returned from any Rust function.
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the memory was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_code_hash_iter_free(iter: Option<Box<CodeIteratorHandle>>) -> VoidResult {
    crate::invoke_with_handle(iter, drop)
}

impl crate::MetricsContextExt for CodeIteratorHandle<'_> {
    fn metrics_context(&self) -> Option<firewood_metrics::MetricsContext> {
        None
    }
}
