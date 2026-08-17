// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Shared code-hash iteration support for range and change proofs.
//!
//! Both proof types can yield the set of contract code hashes referenced by
//! their account values (Ethereum tries only). The extraction is pure RLP
//! parsing of bytes already in the proof — no verification is required.
//! Malformed account values are surfaced as iterator errors; entries with
//! non-account keys, empty code hashes, or invalid code-hash lengths are skipped
//! to preserve the C API's historical iteration behavior.
//!
//! Per-proof-type entry points live in `range.rs` and `change.rs`; this
//! module owns the shared [`CodeIteratorHandle`], the per-element extraction
//! logic, and the type-agnostic FFI exports (`fwd_code_hash_iter_next`,
//! `fwd_code_hash_iter_free`).

#[cfg(feature = "ethhash")]
use firewood::ProofError;

use firewood::api::{self, BatchOp};

use crate::{HashKey, HashResult, VoidResult};

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

    match firewood::account_code_hash(value) {
        Ok(Some(code_hash)) => Some(Ok(code_hash.into())),
        Ok(None) => None,
        Err(ProofError::InvalidAccountCodeHashLength { .. }) => {
            // Keep the C iterator's historical contract: a wrong-length codeHash
            // skips this account and allows later valid code hashes to be yielded.
            None
        }
        Err(err) => Some(Err(api::Error::ProofError(err))),
    }
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
    /// operations. Non-`Put` ops, `Put` ops whose key is not 32 bytes, empty
    /// code hashes, and invalid code-hash lengths are skipped. Malformed
    /// account values are surfaced as iterator errors.
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

#[cfg(all(test, feature = "ethhash"))]
mod tests {
    use super::extract_code_hash;

    #[test]
    fn wrong_length_code_hash_is_skipped() {
        let key = [0; 32];

        // RLP account list: [nonce="", balance="", storageRoot=[0; 32],
        // codeHash=[0x33; 31]]. The final field is intentionally one byte
        // short so this exercises the wrong-length skip path.
        let mut account_rlp_with_short_code_hash = vec![
            0xf8, // Long-form RLP list with a one-byte payload length.
            0x43, // List payload length: 67 bytes.
            0x80, // Empty nonce.
            0x80, // Empty balance.
            0xa0, // storageRoot is a 32-byte string.
        ];
        account_rlp_with_short_code_hash.extend([0; 32]); // Zero storageRoot bytes.
        account_rlp_with_short_code_hash.push(0x9f); // codeHash is a 31-byte string.
        account_rlp_with_short_code_hash.extend([0x33; 31]); // Short codeHash bytes.

        assert!(extract_code_hash(&key, &account_rlp_with_short_code_hash).is_none());
    }

    #[test]
    fn malformed_account_value_is_returned_as_error() {
        let key = [0; 32];
        let result = extract_code_hash(&key, b"not an account").expect("account key is valid");

        assert_eq!(
            result
                .expect_err("malformed account value should return an iterator error")
                .to_string(),
            "proof error: invalid Ethereum account value format: expected a list, found a byte string"
        );
    }
}
