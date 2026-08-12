// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! High-level `eth_getProof`-compatible proof export.
//!
//! Wraps [`crate::api::DbView::single_key_proof`] and converts the resulting
//! firewood `ProofNode`s into the canonical Ethereum MPT-RLP byte form an
//! `eth_getProof` verifier expects. Always compiled, but gates at runtime on
//! [`firewood_storage::NodeHashAlgorithm::is_ethereum`] — calling against a
//! merkledb-mode database returns [`crate::api::Error::FeatureNotSupported`].
//!
//! Absent accounts return zero account fields plus the exclusion proof
//! bytes; callers translate that into the JSON-RPC default-fields shape
//! themselves. This matches go-ethereum's behavior and keeps a single
//! error type on the public API.

use firewood_storage::{
    NodeHashAlgorithm, PackedPathRef, PathComponent, TriePathFromPackedBytes, ValueDigest,
};
#[cfg(feature = "ethhash")]
use firewood_storage::{RlpList, TrieHash};

use crate::api::{DbView, Error, HashKey, HashKeyExt};
use crate::proofs::ProofError;
use crate::proofs::eth::{
    ACCOUNT_DEPTH_NIBBLES, AccountFields, account_storage_root_rlp, proof_node_to_mpt_rlp,
};
use crate::proofs::types::ProofNode;

/// Nibble depth at which a node represents a storage slot leaf
/// (account 64 nibbles + slot 64 nibbles).
const STORAGE_LEAF_DEPTH_NIBBLES: usize = ACCOUNT_DEPTH_NIBBLES + 64;

/// `keccak256("")` — the `codeHash` value for accounts with no contract code.
const KECCAK_EMPTY: [u8; 32] = [
    // "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
    0xc5, 0xd2, 0x46, 0x01, 0x86, 0xf7, 0x23, 0x3c, 0x92, 0x7e, 0x7d, 0xb2, 0xdc, 0xc7, 0x03, 0xc0,
    0xe5, 0x00, 0xb6, 0x53, 0xca, 0x82, 0x27, 0x3b, 0x7b, 0xfa, 0xd8, 0x04, 0x5d, 0x85, 0xa4, 0x70,
];

/// Extract a non-empty Ethereum account code hash from an RLP-encoded account value.
///
/// Returns `Ok(None)` for accounts with the empty-code hash.
///
/// # Errors
///
/// Returns [`ProofError::InvalidAccountValueFormat`] if `value` is not an
/// RLP-encoded Ethereum account, if the account has fewer than four fields, or
/// if any of the first four account fields is not encoded as bytes. Returns
/// [`ProofError::InvalidAccountCodeHashLength`] if the `codeHash` field is not
/// 32 bytes.
#[cfg(feature = "ethhash")]
pub fn account_code_hash(value: &[u8]) -> Result<Option<HashKey>, ProofError> {
    let code_hash_slice = RlpList::parse(value)
        .and_then(|list| list.nth_bytes(3))
        .map_err(|source| ProofError::InvalidAccountValueFormat { source })?;
    let code_hash = TrieHash::try_from(code_hash_slice)
        .map_err(|err| ProofError::InvalidAccountCodeHashLength { len: err.0 })?;
    if code_hash == TrieHash::from(KECCAK_EMPTY) {
        return Ok(None);
    }

    Ok(Some(code_hash))
}

/// One `eth_getProof` response, modulo JSON formatting.
#[derive(Debug, PartialEq, Eq)]
pub struct EthProof {
    /// Account transaction count.
    pub nonce: u64,
    /// Account balance, zero-padded big-endian.
    pub balance: [u8; 32],
    /// Keccak-256 of the account's contract code (empty-code hash if none).
    pub code_hash: [u8; 32],
    /// Storage trie root (as embedded in the account leaf, or the empty
    /// trie root for absent accounts).
    pub storage_hash: [u8; 32],
    /// MPT-RLP-encoded nodes proving the account exists or doesn't, in
    /// root-to-leaf order.
    pub account_proof: Box<[Box<[u8]>]>,
    /// Per-slot storage proofs, one entry per requested key (in input order).
    pub storage_proofs: Box<[EthStorageProof]>,
}

impl Default for EthProof {
    /// "Absent account" shape: zero `nonce` and `balance`, `code_hash` =
    /// `keccak("")` (the empty-code hash), `storage_hash` =
    /// [`HashKey::default_root_hash`] (the empty-trie root in eth mode).
    /// Matches what an ethereum verifier expects to see for a missing
    /// account.
    ///
    /// Under merkledb mode `default_root_hash` returns `None`, so
    /// `storage_hash` falls back to all zeros. Callers should not reach
    /// this case — [`eth_get_proof`] gates on `is_ethereum()` before any
    /// `EthProof` is constructed.
    fn default() -> Self {
        Self {
            nonce: 0,
            balance: [0u8; 32],
            code_hash: KECCAK_EMPTY,
            storage_hash: HashKey::default_root_hash()
                .as_deref()
                .copied()
                .unwrap_or_default(),
            account_proof: Box::default(),
            storage_proofs: Box::default(),
        }
    }
}

/// Per-slot result inside an [`EthProof`].
#[derive(Debug, Default, PartialEq, Eq)]
pub struct EthStorageProof {
    /// The requested 32-byte slot key (caller's input, echoed back).
    pub key: [u8; 32],
    /// The stored slot value if present, `None` for an exclusion proof.
    pub value: Option<Box<[u8]>>,
    /// MPT-RLP-encoded storage trie nodes, root-to-leaf. Empty when the
    /// account has no storage at all (empty trie root).
    pub proof: Box<[Box<[u8]>]>,
}

/// Produce an Ethereum-verifiable proof of `account_key` and each entry in
/// `storage_keys`.
///
/// Trie keys here are the already-keccak-hashed 32-byte forms (firewood
/// stores accounts at `keccak256(address)` and slots at
/// `keccak256(address) ++ keccak256(slot_key)` — callers do the hashing).
///
/// # Returns
///
/// An [`EthProof`] with one [`EthStorageProof`] per `storage_keys` entry,
/// in input order. Absent accounts come back with zero account scalars
/// plus the empty-code and empty-trie hashes; the proof bytes themselves
/// distinguish inclusion from exclusion to a verifier.
///
/// # Errors
///
/// - [`Error::FeatureNotSupported`] if the database is not running in
///   ethereum hash mode.
/// - Any [`Error`] surfaced by [`DbView::single_key_proof`] while fetching
///   the underlying firewood proofs.
/// - [`Error::IO`] (kind `InvalidData`) wrapping an account-RLP decode
///   error if the on-disk account value cannot be parsed.
pub fn eth_get_proof<V>(
    view: &V,
    account_key: &[u8; 32],
    storage_keys: &[[u8; 32]],
) -> Result<EthProof, Error>
where
    V: DbView + ?Sized,
{
    if !NodeHashAlgorithm::compile_option().is_ethereum() {
        return Err(Error::FeatureNotSupported(
            "eth_get_proof requires ethereum hash mode".into(),
        ));
    }

    // Merkle::prove returns ProofError::Empty only when the trie has no
    // root at all; absent keys in non-empty tries return Ok with an
    // exclusion proof. Treat the no-root case as "no nodes," which
    // funnels into the absent-account path below.
    let frozen = match view.single_key_proof(account_key.as_slice()) {
        Ok(p) => Some(p),
        Err(Error::ProofError(ProofError::Empty)) => None,
        Err(e) => return Err(e),
    };
    let account_nodes: &[ProofNode] = frozen.as_ref().map_or(&[], |p| p.as_ref());

    let account_proof: Box<[Box<[u8]>]> = account_nodes
        .iter()
        .flat_map(proof_node_to_mpt_rlp)
        .collect();

    // Inclusion: terminal node sits at account depth and carries a value.
    // Anything else is exclusion (terminal is a strict ancestor, or
    // a depth-64 branch with no account value).
    let account_terminal = account_nodes
        .last()
        .filter(|n| n.key.as_ref().len() == ACCOUNT_DEPTH_NIBBLES && n.value_digest.is_some());

    let fields = account_terminal
        .and_then(|n| n.value_digest.as_ref().and_then(ValueDigest::value))
        .map(AccountFields::from_rlp)
        .transpose()?;

    let storage_proofs: Box<[EthStorageProof]> = match (fields.as_ref(), account_terminal) {
        (Some(_), Some(account_node)) => storage_keys
            .iter()
            .map(|slot| build_storage_proof(view, account_key, slot, account_node))
            .collect::<Result<_, Error>>()?,
        _ => absent_storage_proofs(storage_keys),
    };

    // Default = absent-account shape (empty-code and empty-trie-root
    // hashes); the inner update fills the four account scalars on
    // inclusion. The outer update plugs in the proof bytes regardless.
    Ok(EthProof {
        account_proof,
        storage_proofs,
        ..fields
            .map(|f| EthProof {
                nonce: f.nonce,
                balance: f.balance,
                code_hash: f.code_hash,
                storage_hash: f.storage_root,
                ..EthProof::default()
            })
            .unwrap_or_default()
    })
}

/// Build one entry of [`EthProof::storage_proofs`] for `slot_key` under an
/// already-resolved `account_node`.
///
/// The storage-trie root's shape is fixed by how many distinct first storage
/// nibbles the account fans out to, not by the slot count:
///
///   * no child nibbles: the account has no storage, so the proof is empty
///     and the value absent.
///   * exactly 1 child nibble: the account's lone child is itself the storage
///     root (see [`account_storage_root_rlp`]).
///   * 2+ child nibbles: firewood folds the storage branch into the account
///     node, so there is no on-disk branch node; the root is synthesized from
///     the account's child hashes (see [`account_storage_root_rlp`]).
///
/// In every non-empty case the descent nodes returned by
/// [`DbView::single_key_proof`] follow the root, each referenced by hash.
///
/// # Returns
///
/// An [`EthStorageProof`] whose `proof` bytes verify against the account's
/// `storageRoot` and whose `value` is `Some` for inclusion or `None` for
/// exclusion.
///
/// # Errors
///
/// Propagates any [`Error`] from [`DbView::single_key_proof`] used while
/// fetching the storage path.
fn build_storage_proof<V>(
    view: &V,
    account_key: &[u8; 32],
    slot_key: &[u8; 32],
    account_node: &ProofNode,
) -> Result<EthStorageProof, Error>
where
    V: DbView + ?Sized,
{
    // No storage: empty proof, absent value. Returns early because the rest
    // of the function walks a storage descent there is no point fetching.
    if account_node.child_hashes.count() == 0 {
        return Ok(EthStorageProof {
            key: *slot_key,
            ..EthStorageProof::default()
        });
    }

    let full_key: Box<[u8]> = account_key.iter().chain(slot_key.iter()).copied().collect();
    let raw = view.single_key_proof(&full_key[..])?;
    let nodes: &[ProofNode] = raw.as_ref();
    let storage_nodes: Box<[&ProofNode]> = nodes
        .iter()
        .filter(|n| n.key.as_ref().len() > ACCOUNT_DEPTH_NIBBLES)
        .collect();

    let proof = construct_root_proof(view, account_key, account_node, &storage_nodes)?;

    let value = storage_nodes
        .last()
        .filter(|n| n.key.as_ref().len() == STORAGE_LEAF_DEPTH_NIBBLES)
        .filter(|n| nibbles_match_packed(n.key.as_ref(), &full_key))
        .and_then(|n| n.value_digest.as_ref().and_then(ValueDigest::value))
        .map(Into::into);

    Ok(EthStorageProof {
        key: *slot_key,
        value,
        proof,
    })
}

/// Build the storage-proof bytes for an account with at least one storage
/// child nibble: the storage-trie root followed by the descent nodes from
/// `storage_nodes`, in root-to-leaf order.
///
/// Dispatches on the root's shape (see [`build_storage_proof`]): a single
/// child nibble means the account's lone child is itself the root; 2+ child
/// nibbles means the root is a branch synthesized from the account's child
/// hashes ([`account_storage_root_rlp`]).
///
/// # Errors
///
/// Propagates any [`Error`] from [`fetch_only_storage_child`] when recovering
/// the lone child for a single-nibble exclusion proof.
fn construct_root_proof<V>(
    view: &V,
    account_key: &[u8; 32],
    account_node: &ProofNode,
    storage_nodes: &[&ProofNode],
) -> Result<Box<[Box<[u8]>]>, Error>
where
    V: DbView + ?Sized,
{
    if account_node.child_hashes.count() == 1 {
        // Single child nibble: the lone child is the storage root. When the
        // requested slot's first nibble differs from the stored child's, the
        // natural descent stops at the account node and never reaches that
        // root; fetch the lone child so the verifier still receives the root
        // needed to prove absence.
        let owned_root: Option<ProofNode> = if storage_nodes.is_empty() {
            fetch_only_storage_child(view, account_key, account_node)?
        } else {
            None
        };
        let storage_root = storage_nodes.first().copied().or(owned_root.as_ref());

        Ok(storage_root
            .map(|root| {
                // Firewood consumes one nibble at the account->child edge, so
                // this lone child's `ProofNode` reports its partial path one
                // nibble short. Fold that nibble back in (mirroring the
                // hasher's `children == 1` arm in
                // storage/src/hashers/ethhash.rs) before encoding, so the
                // emitted root's keccak matches the account's `storageHash`.
                let folded = ProofNode {
                    partial_len: root.partial_len.wrapping_sub(1),
                    ..root.clone()
                };
                proof_node_to_mpt_rlp(&folded)
            })
            .into_iter()
            .flatten()
            .chain(
                storage_nodes
                    .iter()
                    .copied()
                    .skip(1)
                    .flat_map(proof_node_to_mpt_rlp),
            )
            .collect())
    } else {
        // 2+ child nibbles: synthesized branch root, then the descent nodes.
        // `account_storage_root_rlp` is `Some` for any count >= 2.
        Ok(account_storage_root_rlp(account_node)
            .into_iter()
            .chain(
                storage_nodes
                    .iter()
                    .copied()
                    .flat_map(proof_node_to_mpt_rlp),
            )
            .collect())
    }
}

/// Build per-slot entries for the "account is absent / has no storage" case:
/// echo the requested key, no value, empty proof.
fn absent_storage_proofs(storage_keys: &[[u8; 32]]) -> Box<[EthStorageProof]> {
    storage_keys
        .iter()
        .map(|k| EthStorageProof {
            key: *k,
            ..EthStorageProof::default()
        })
        .collect()
}

/// Look up the lone depth-65+ storage child of an account whose
/// `child_hashes` has exactly one present nibble.
///
/// # Returns
///
/// `Ok(Some(child))` with the first depth-65+ node found, or `Ok(None)`
/// if no storage child is present in `account_node`.
///
/// # Errors
///
/// Propagates any [`Error`] from [`DbView::single_key_proof`] used to
/// descend into the storage child.
fn fetch_only_storage_child<V>(
    view: &V,
    account_key: &[u8; 32],
    account_node: &ProofNode,
) -> Result<Option<ProofNode>, Error>
where
    V: DbView + ?Sized,
{
    let Some((nibble, _)) = account_node.child_hashes.iter_present().next() else {
        return Ok(None);
    };

    // Build a synthetic 64-byte key whose 65th nibble matches the
    // account's lone storage child; the remaining nibbles are zero, so
    // the proof descends as far as the trie allows.
    let mut descent = [0u8; 64];
    let prefix = account_key
        .iter()
        .copied()
        .chain(std::iter::once(nibble.as_u8().wrapping_shl(4)));
    for (slot, b) in descent.iter_mut().zip(prefix) {
        *slot = b;
    }

    let raw = view.single_key_proof(&descent[..])?;
    Ok(raw
        .as_ref()
        .iter()
        .find(|n| n.key.as_ref().len() > ACCOUNT_DEPTH_NIBBLES)
        .cloned())
}

/// Return true iff `nibbles` is the nibble decomposition of `packed`.
///
/// Uses [`PackedPathRef`] — a zero-cost borrowed view over `packed` that
/// yields nibbles via its `IntoIterator` impl — so no allocation is needed
/// to bridge the two representations.
fn nibbles_match_packed(nibbles: &[PathComponent], packed: &[u8]) -> bool {
    let packed_ref: PackedPathRef<'_> = TriePathFromPackedBytes::path_from_packed_bytes(packed);
    nibbles.iter().copied().eq(packed_ref)
}

/// The negative half of [`eth_get_proof`]'s runtime mode gate: a merkledb-mode
/// (non-ethhash) build must refuse to emit eth proofs. The positive half lives
/// in the `ethhash`-gated `tests` module below.
#[cfg(all(test, not(feature = "ethhash")))]
mod merkledb_gate_tests {
    use super::*;
    use crate::merkle::tests::init_merkle;

    #[test]
    fn eth_get_proof_rejected_without_ethhash() {
        let merkle = init_merkle(std::iter::empty::<(&[u8], &[u8])>());
        let err = eth_get_proof(merkle.nodestore(), &[0u8; 32], &[])
            .expect_err("merkledb-mode database must not emit eth proofs");
        assert!(
            matches!(err, Error::FeatureNotSupported(_)),
            "expected FeatureNotSupported, got {err:?}"
        );
    }
}

#[cfg(all(test, feature = "ethhash"))]
#[expect(clippy::unwrap_used, clippy::indexing_slicing)]
mod tests {
    use super::*;
    use crate::merkle::tests::init_merkle;
    use firewood_storage::{NULL_RLP, NibblesIterator, RlpItem, RlpList, encode_list};
    use sha3::{Digest, Keccak256};

    fn keccak(bytes: &[u8]) -> [u8; 32] {
        Keccak256::digest(bytes).into()
    }

    fn account_rlp(nonce: u64, balance_be: &[u8]) -> Box<[u8]> {
        account_rlp_with_code_hash(nonce, balance_be, &KECCAK_EMPTY)
    }

    fn account_rlp_with_code_hash(nonce: u64, balance_be: &[u8], code_hash: &[u8]) -> Box<[u8]> {
        // storageRoot and codeHash get fixed up by firewood during hashing
        // if the input is stale; we supply real values here so the on-disk
        // codeHash matches what we'd expect after decoding.
        let nonce_be = nonce.to_be_bytes();
        let mut nonce_min: &[u8] = &nonce_be;
        while !nonce_min.is_empty() && nonce_min[0] == 0 {
            nonce_min = &nonce_min[1..];
        }
        encode_list(&[
            RlpItem::Bytes(nonce_min),
            RlpItem::Bytes(balance_be),
            RlpItem::Bytes(&[0u8; 32]), // storageRoot — fixed up by firewood
            RlpItem::Bytes(code_hash),
        ])
    }

    /// Confirm an ethhash build passes the runtime mode gate. The gate's
    /// *negative* path is covered by `merkledb_gate_tests` below, which only
    /// compiles without the `ethhash` feature.
    #[test]
    fn mode_gate_passes_under_ethhash() {
        assert!(NodeHashAlgorithm::compile_option().is_ethereum());
    }

    /// `KECCAK_EMPTY` is a hardcoded literal; confirm it really is
    /// `keccak256("")`, the empty-code hash an eth verifier expects.
    #[test]
    fn keccak_empty_matches_keccak_of_empty_input() {
        assert_eq!(keccak(b""), KECCAK_EMPTY);
    }

    #[test]
    fn account_code_hash_decodes_all_outcomes() {
        let non_empty_code_hash = [0x22; 32];
        let non_empty = account_rlp_with_code_hash(1, &[0x01], &non_empty_code_hash);
        assert_eq!(
            account_code_hash(&non_empty).unwrap(),
            Some(non_empty_code_hash.into())
        );

        let empty = account_rlp(1, &[0x01]);
        assert_eq!(account_code_hash(&empty).unwrap(), None);

        let bad_rlp = account_code_hash(b"not an account").unwrap_err();
        assert!(
            matches!(bad_rlp, ProofError::InvalidAccountValueFormat { .. }),
            "expected account value format error, got {bad_rlp:?}"
        );

        let wrong_length = account_rlp_with_code_hash(1, &[0x01], &[0x33; 31]);
        let wrong_length_err = account_code_hash(&wrong_length).unwrap_err();
        assert!(
            matches!(
                wrong_length_err,
                ProofError::InvalidAccountCodeHashLength { len: 31 }
            ),
            "expected account code hash length error, got {wrong_length_err:?}"
        );
    }

    #[test]
    fn absent_account_returns_zero_fields_and_proof_bytes() {
        // Empty trie; ask for any account.
        let merkle = init_merkle(std::iter::empty::<(&[u8], &[u8])>());
        let key = [0x11u8; 32];
        let proof = eth_get_proof(merkle.nodestore(), &key, &[]).unwrap();
        assert_eq!(proof.nonce, 0);
        assert_eq!(proof.balance, [0u8; 32]);
        assert_eq!(proof.code_hash, KECCAK_EMPTY);
        assert_eq!(proof.storage_hash, keccak(NULL_RLP));
        assert!(proof.storage_proofs.is_empty());
        // Account proof bytes for an empty trie is just the root's exclusion
        // proof. The non-empty case is exercised below.
    }

    #[test]
    fn present_account_no_storage_decodes_fields() {
        let key = [0x22u8; 32];
        let balance = [0x12, 0x34];
        let value = account_rlp(7, &balance);
        let merkle = init_merkle([(key.as_slice(), value.as_ref())]);
        let proof = eth_get_proof(merkle.nodestore(), &key, &[]).unwrap();
        assert_eq!(proof.nonce, 7);
        // balance is right-aligned in 32 bytes.
        assert_eq!(proof.balance[30..], balance);
        assert_eq!(proof.code_hash, KECCAK_EMPTY);
        // Account with no storage entries → storage root is the empty trie
        // root after firewood's hashing fixup.
        assert_eq!(proof.storage_hash, keccak(NULL_RLP));
        // Account proof has at least one entry (the root leaf).
        assert!(!proof.account_proof.is_empty());
    }

    #[test]
    fn present_account_one_slot_inclusion_and_exclusion() {
        let account_key = [0x33u8; 32];
        let acc_val = account_rlp(1, &[0x05]);
        let slot_key = [0xaau8; 32];
        let slot_val: Box<[u8]> = Box::from(b"stored-value".as_slice());

        let mut full_slot = account_key.to_vec();
        full_slot.extend_from_slice(&slot_key);

        let merkle = init_merkle([
            (account_key.as_slice(), acc_val.as_ref()),
            (full_slot.as_slice(), slot_val.as_ref()),
        ]);

        // Inclusion: requested slot equals stored slot.
        let proof = eth_get_proof(merkle.nodestore(), &account_key, &[slot_key]).unwrap();
        assert_eq!(proof.storage_proofs.len(), 1);
        let entry = &proof.storage_proofs[0];
        assert_eq!(entry.key, slot_key);
        assert_eq!(entry.value.as_deref(), Some(b"stored-value".as_slice()));
        assert_eq!(
            entry.proof.len(),
            1,
            "single-slot trie → storage root is the lone leaf node"
        );
        assert_storage_entry_verifies(entry, &proof.storage_hash);

        // Exclusion: ask for a different slot under the same account.
        let other = [0xbbu8; 32];
        let proof = eth_get_proof(merkle.nodestore(), &account_key, &[other]).unwrap();
        let entry = &proof.storage_proofs[0];
        assert_eq!(entry.value, None);
        assert_eq!(entry.proof.len(), 1, "still emits the lone leaf node");
        assert_storage_entry_verifies(entry, &proof.storage_hash);
    }

    /// Walk an MPT proof root-to-leaf, returning the value bytes recovered
    /// from the terminal leaf, or `None` for a valid exclusion proof.
    ///
    /// This is a minimal verifier: it follows hash references between
    /// successive nodes and decodes paths to confirm each step matches the
    /// requested key. Sufficient for tests; not production-grade.
    fn mpt_proof_value(
        mpt_proof_bytes: &[Box<[u8]>],
        expected_root: &[u8; 32],
        key: &[u8],
    ) -> Option<Box<[u8]>> {
        let all_nibbles: Box<[u8]> = NibblesIterator::new(key).collect();
        let mut nibbles: &[u8] = &all_nibbles;
        let mut next_hash = *expected_root;
        let hash_of = |child: &[u8]| -> [u8; 32] {
            <[u8; 32]>::try_from(child).expect("inline-RLP child not supported in test verifier")
        };
        for buf in mpt_proof_bytes {
            assert_eq!(
                keccak(buf),
                next_hash,
                "hash chain broken at next_hash {next_hash:?}"
            );
            let list = RlpList::parse(buf).unwrap();
            let fields = list.fields().unwrap();
            match fields.len() {
                2 => {
                    // Leaf or extension: first field is compact path.
                    let (path_nibbles, is_leaf) = decode_compact(fields[0]);
                    let rest = nibbles.strip_prefix(&*path_nibbles)?;
                    if is_leaf {
                        assert!(rest.is_empty(), "leaf consumed full key");
                        return Some(fields[1].into());
                    }
                    nibbles = rest;
                    next_hash = hash_of(fields[1]);
                }
                17 => {
                    let Some((&next_nibble, rest)) = nibbles.split_first() else {
                        // Branch's value slot.
                        return Some(fields[16].into());
                    };
                    let child = fields[next_nibble as usize];
                    if child.is_empty() {
                        return None;
                    }
                    nibbles = rest;
                    next_hash = hash_of(child);
                }
                n => panic!("unexpected MPT list length: {n}"),
            }
        }
        // Ran out of proof buffers before resolving — exclusion proof.
        None
    }

    /// Assert a storage proof entry verifies end-to-end against the account's
    /// `storage_hash`: every node hash-chains from the root, and walking the
    /// slot key recovers exactly the value the entry reports (the stored bytes
    /// for an inclusion proof, `None` for an exclusion proof).
    ///
    /// This is the full property a real `eth_getProof` verifier checks, and
    /// what these tests assert. A root-hash spot-check
    /// (`keccak(proof[0]) == storage_hash`) only proves the first node is
    /// right; walking through to the leaf is what proves the interior nodes
    /// actually connect the root to the claimed value.
    fn assert_storage_entry_verifies(entry: &EthStorageProof, storage_hash: &[u8; 32]) {
        if !entry.proof.is_empty() {
            assert_eq!(
                keccak(&entry.proof[0]),
                *storage_hash,
                "first storage proof node must hash to storageRoot for slot {:?}",
                entry.key
            );
        }
        let recovered = mpt_proof_value(&entry.proof, storage_hash, &entry.key);
        assert_eq!(
            recovered.as_deref(),
            entry.value.as_deref(),
            "storage proof for slot {:?} must verify to its reported value",
            entry.key
        );
    }

    /// Decode an MPT compact-encoded path. Returns the nibbles and a flag
    /// for whether it's a leaf (vs. extension).
    fn decode_compact(bytes: &[u8]) -> (Box<[u8]>, bool) {
        let (first, remainder) = bytes.split_first().unwrap();
        let is_leaf = first & 0x20 != 0;
        let odd = first & 0x10 != 0;
        let out = odd
            .then_some(first & 0x0f)
            .into_iter()
            .chain(remainder.iter().flat_map(|&b| [b >> 4, b & 0x0f]))
            .collect();
        (out, is_leaf)
    }

    #[test]
    fn absent_account_in_nonempty_trie_returns_exclusion_proof() {
        // Trie has two accounts; ask for a third key the trie doesn't
        // contain. Merkle::prove returns an Ok(exclusion proof), not
        // ProofError::Empty. We should surface zero fields, the empty-code
        // and empty-trie-root hashes, and a non-empty account_proof that
        // verifies as exclusion against root_hash.
        let acc1 = [0x10u8; 32];
        let acc2 = [0x20u8; 32];
        let missing = [0xffu8; 32];
        let v1 = account_rlp(1, &[]);
        let v2 = account_rlp(2, &[]);
        let merkle = init_merkle([
            (acc1.as_slice(), v1.as_ref()),
            (acc2.as_slice(), v2.as_ref()),
        ]);

        let proof = eth_get_proof(merkle.nodestore(), &missing, &[]).unwrap();
        assert_eq!(proof.nonce, 0);
        assert_eq!(proof.balance, [0u8; 32]);
        assert_eq!(proof.code_hash, KECCAK_EMPTY);
        assert_eq!(proof.storage_hash, keccak(NULL_RLP));
        assert!(
            !proof.account_proof.is_empty(),
            "non-empty trie should produce real exclusion proof bytes"
        );

        // The exclusion proof must hash-chain from the trie root, and
        // walking with the missing key must terminate without recovering a
        // value.
        let root: [u8; 32] = merkle
            .nodestore()
            .root_hash()
            .unwrap()
            .as_ref()
            .try_into()
            .unwrap();
        let recovered = mpt_proof_value(&proof.account_proof, &root, &missing);
        assert!(
            recovered.is_none(),
            "exclusion proof should not yield a value, got {recovered:?}"
        );
    }

    #[test]
    fn account_proof_walks_root_to_leaf() {
        // Two accounts so the state trie has real branching above each leaf.
        let acc1 = [0x10u8; 32];
        let acc2 = [0x20u8; 32];
        let v1 = account_rlp(11, &[]);
        let v2 = account_rlp(22, &[]);
        let merkle = init_merkle([
            (acc1.as_slice(), v1.as_ref()),
            (acc2.as_slice(), v2.as_ref()),
        ]);
        let root_hash = merkle.nodestore().root_hash().unwrap();
        let root: [u8; 32] = root_hash.as_ref().try_into().unwrap();
        let proof = eth_get_proof(merkle.nodestore(), &acc1, &[]).unwrap();
        let recovered =
            mpt_proof_value(&proof.account_proof, &root, &acc1).expect("acc1 should be present");
        // The recovered bytes should match what firewood persisted for the
        // account (with storageRoot fixed up to the empty trie root).
        let parsed = AccountFields::from_rlp(&recovered).unwrap();
        assert_eq!(parsed.nonce, 11);
    }

    #[test]
    fn present_account_two_slots_multi_child_proof() {
        let account_key = [0x44u8; 32];
        let acc_val = account_rlp(2, &[]);
        // Two slots with different first nibbles to force a real branch
        // node at the storage trie root.
        let slot_a = [0x00u8; 32];
        let slot_b = [0xffu8; 32];
        let mut full_a = account_key.to_vec();
        full_a.extend_from_slice(&slot_a);
        let mut full_b = account_key.to_vec();
        full_b.extend_from_slice(&slot_b);

        let merkle = init_merkle([
            (account_key.as_slice(), acc_val.as_ref()),
            (full_a.as_slice(), b"val-a".as_slice()),
            (full_b.as_slice(), b"val-b".as_slice()),
        ]);

        let proof = eth_get_proof(merkle.nodestore(), &account_key, &[slot_a, slot_b]).unwrap();
        assert_eq!(proof.storage_proofs.len(), 2);
        assert_eq!(
            proof.storage_proofs[0].value.as_deref(),
            Some(b"val-a".as_slice())
        );
        assert_eq!(
            proof.storage_proofs[1].value.as_deref(),
            Some(b"val-b".as_slice())
        );
        // Each storage proof must verify end-to-end against storage_hash.
        for entry in &proof.storage_proofs {
            assert!(!entry.proof.is_empty());
            assert_storage_entry_verifies(entry, &proof.storage_hash);
        }
    }

    #[test]
    fn present_account_two_slots_shared_first_nibble() {
        // Two storage slots that share their first storage nibble. Firewood
        // path-compresses them under a single account child, so the account
        // node has exactly one child nibble — but the storage trie root is a
        // real subtree (extension/branch), not a single leaf.
        const VAL_A: &[u8] = b"val-a";
        const VAL_B: &[u8] = b"val-b";

        let account_key = [0x55u8; 32];
        let acc_val = account_rlp(3, &[]);

        let slot_a = [0x00u8; 32];
        let mut slot_b = [0x00u8; 32];
        slot_b[0] = 0x01;
        assert_eq!(
            slot_a[0] >> 4,
            slot_b[0] >> 4,
            "slots must share their first storage nibble"
        );

        let mut full_a = account_key.to_vec();
        full_a.extend_from_slice(&slot_a);
        let mut full_b = account_key.to_vec();
        full_b.extend_from_slice(&slot_b);

        let merkle = init_merkle([
            (account_key.as_slice(), acc_val.as_ref()),
            (full_a.as_slice(), VAL_A),
            (full_b.as_slice(), VAL_B),
        ]);

        // Inclusion: both slots are present and must verify end-to-end.
        let proof = eth_get_proof(merkle.nodestore(), &account_key, &[slot_a, slot_b]).unwrap();
        assert_eq!(proof.storage_proofs.len(), 2);
        assert_eq!(
            proof.storage_proofs[0].value.as_deref(),
            Some(VAL_A),
            "slot_a is present and must yield an inclusion proof"
        );
        assert_eq!(
            proof.storage_proofs[1].value.as_deref(),
            Some(VAL_B),
            "slot_b is present and must yield an inclusion proof"
        );
        for entry in &proof.storage_proofs {
            assert!(
                !entry.proof.is_empty(),
                "present slot must carry a non-empty storage proof"
            );
            assert_storage_entry_verifies(entry, &proof.storage_hash);
        }

        // Exclusion in the same shape, two ways:
        //  * `miss_shared` shares the first storage nibble (0x0), so the
        //    natural descent enters the subtree and stops at the divergence.
        //  * `miss_other` has a different first nibble (0xf), so the descent
        //    stops at the account node and the lone subtree root must be
        //    fetched separately to prove absence.
        let mut miss_shared = [0x00u8; 32];
        miss_shared[0] = 0x02;
        let miss_other = [0xffu8; 32];
        let proof =
            eth_get_proof(merkle.nodestore(), &account_key, &[miss_shared, miss_other]).unwrap();
        for entry in &proof.storage_proofs {
            assert_eq!(
                entry.value, None,
                "missing slot {:?} has no value",
                entry.key
            );
            assert_storage_entry_verifies(entry, &proof.storage_hash);
        }
    }
}
