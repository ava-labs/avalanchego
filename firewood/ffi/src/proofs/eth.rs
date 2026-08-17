// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::api;
use firewood_metrics::MetricsContext;

use crate::reconstructed::ReconstructedHandle;
use crate::revision::RevisionHandle;
use crate::{
    BorrowedBytes, BorrowedBytes2D, EthProofResult, Maybe, OwnedBytes, OwnedSlice, VoidResult,
};

/// An owned, C-friendly `eth_getProof` result.
///
/// Mirrors [`firewood::EthProof`] but with FFI-safe owned buffers. All byte
/// arrays are heap-owned by Rust and must be freed by passing the enclosing
/// value to [`fwd_free_eth_proof`].
#[derive(Debug)]
#[repr(C)]
pub struct EthProofOwned {
    /// Account transaction count.
    pub nonce: u64,
    /// Account balance, zero-padded big-endian.
    pub balance: [u8; 32],
    /// Keccak-256 of the account's contract code (empty-code hash if none).
    pub code_hash: [u8; 32],
    /// Storage trie root as embedded in the account leaf (empty-trie root for
    /// absent accounts).
    pub storage_hash: [u8; 32],
    /// MPT-RLP-encoded account-trie nodes, root-to-leaf order.
    pub account_proof: OwnedSlice<OwnedBytes>,
    /// Per-slot storage proofs, one entry per requested key, in input order.
    pub storage_proofs: OwnedSlice<EthStorageProofOwned>,
}

/// An owned, C-friendly per-slot storage proof inside an [`EthProofOwned`].
#[derive(Debug)]
#[repr(C)]
pub struct EthStorageProofOwned {
    /// The requested 32-byte slot key, echoed back from the caller's input.
    pub key: [u8; 32],
    /// The stored slot value, or [`Maybe::None`] for an exclusion proof.
    pub value: Maybe<OwnedBytes>,
    /// MPT-RLP-encoded storage-trie nodes, root-to-leaf order. Empty when the
    /// account has no storage at all.
    pub proof: OwnedSlice<OwnedBytes>,
}

impl From<firewood::EthProof> for EthProofOwned {
    fn from(proof: firewood::EthProof) -> Self {
        Self {
            nonce: proof.nonce,
            balance: proof.balance,
            code_hash: proof.code_hash,
            storage_hash: proof.storage_hash,
            account_proof: proof
                .account_proof
                .into_iter()
                .map(OwnedBytes::from)
                .collect(),
            storage_proofs: proof
                .storage_proofs
                .into_iter()
                .map(EthStorageProofOwned::from)
                .collect(),
        }
    }
}

impl From<firewood::EthStorageProof> for EthStorageProofOwned {
    fn from(storage: firewood::EthStorageProof) -> Self {
        Self {
            key: storage.key,
            value: storage.value.map(OwnedBytes::from).into(),
            proof: storage.proof.into_iter().map(OwnedBytes::from).collect(),
        }
    }
}

impl crate::MetricsContextExt for EthProofOwned {
    fn metrics_context(&self) -> Option<MetricsContext> {
        None
    }
}

/// Coerce a borrowed key into the fixed 32-byte trie-key form firewood expects.
fn to_trie_key(bytes: &[u8]) -> Result<[u8; 32], api::Error> {
    <[u8; 32]>::try_from(bytes).map_err(|_| {
        api::Error::IO(std::io::Error::new(
            std::io::ErrorKind::InvalidData,
            format!("expected a 32-byte key, got {} bytes", bytes.len()),
        ))
    })
}

/// Shared body for the `fwd_eth_get_proof*` entry points: parse the keys and
/// call the core proof function against `handle`. Returns
/// [`EthProofResult::NullHandlePointer`] when `handle` is `None`.
fn eth_get_proof_on<H>(
    handle: Option<&H>,
    account_key: BorrowedBytes<'_>,
    storage_keys: BorrowedBytes2D<'_>,
) -> EthProofResult
where
    H: api::DbView + crate::MetricsContextExt,
{
    crate::invoke_with_handle(
        handle,
        move |view| -> Result<firewood::EthProof, api::Error> {
            let account_key = to_trie_key(account_key.as_slice())?;
            let storage_keys = storage_keys
                .iter()
                .map(|key| to_trie_key(key.as_slice()))
                .collect::<Result<Vec<[u8; 32]>, _>>()?;
            firewood::eth_get_proof(view, &account_key, &storage_keys)
        },
    )
}

/// Produce an `eth_getProof`-compatible proof for an account and a set of
/// storage slots against the given revision.
///
/// The returned proof bytes are canonical RLP-encoded Ethereum MPT nodes that a
/// verifier such as go-ethereum's `trie.VerifyProof` accepts. Absent accounts
/// come back with zero account scalars plus the empty-code and empty-trie
/// hashes; the proof bytes distinguish inclusion from exclusion.
///
/// # Arguments
///
/// - `revision` - The revision handle to prove against.
/// - `account_key` - The account's 32-byte trie key (`keccak256(address)`).
/// - `storage_keys` - An array of 32-byte slot trie keys
///   (`keccak256(slot)`), in the order the proofs should be returned.
///
/// Callers are responsible for keccak-hashing addresses and slots into their
/// 32-byte trie-key forms before calling this function.
///
/// # Returns
///
/// - [`EthProofResult::NullHandlePointer`] if `revision` is null.
/// - [`EthProofResult::NotSupported`] if the database is not running in
///   ethereum hash mode.
/// - [`EthProofResult::Ok`] containing the proof on success.
/// - [`EthProofResult::Err`] containing an error message otherwise (including
///   when a key is not exactly 32 bytes long).
///
/// # Safety
///
/// The caller must:
/// * ensure that `revision` is a valid pointer to a [`RevisionHandle`].
/// * ensure that `account_key` is a valid [`BorrowedBytes`] and each entry of
///   `storage_keys` is a valid [`BorrowedBytes`].
/// * call [`fwd_free_eth_proof`] to free a returned [`EthProofOwned`], and
///   [`fwd_free_owned_bytes`] to free a returned error message.
///
/// [`fwd_free_owned_bytes`]: crate::fwd_free_owned_bytes
#[unsafe(no_mangle)]
pub extern "C" fn fwd_eth_get_proof(
    revision: Option<&RevisionHandle<'_>>,
    account_key: BorrowedBytes,
    storage_keys: BorrowedBytes2D,
) -> EthProofResult {
    eth_get_proof_on(revision, account_key, storage_keys)
}

/// Produce an `eth_getProof`-compatible proof against a reconstructed view
/// rather than a committed revision.
///
/// See [`fwd_eth_get_proof`] for the proof format, arguments, return values,
/// and key-encoding requirements.
///
/// # Safety
///
/// As [`fwd_eth_get_proof`], except `reconstructed` must be a valid pointer to
/// a [`ReconstructedHandle`].
#[unsafe(no_mangle)]
pub extern "C" fn fwd_eth_get_proof_on_reconstructed(
    reconstructed: Option<&ReconstructedHandle<'_>>,
    account_key: BorrowedBytes,
    storage_keys: BorrowedBytes2D,
) -> EthProofResult {
    eth_get_proof_on(reconstructed, account_key, storage_keys)
}

/// Frees the memory associated with an [`EthProofOwned`].
///
/// # Arguments
///
/// * `proof` - The [`EthProofOwned`] to free, previously returned from
///   [`fwd_eth_get_proof`].
///
/// # Returns
///
/// - [`VoidResult::Ok`] if the memory was successfully freed.
/// - [`VoidResult::Err`] if the process panics while freeing the memory.
#[unsafe(no_mangle)]
pub extern "C" fn fwd_free_eth_proof(proof: Option<Box<EthProofOwned>>) -> VoidResult {
    crate::invoke_with_handle(proof, drop)
}
