// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Ethhash-shaped proof fuzzer.
//!
//! Generic proof fuzzers only hit the depth-64 account and per-account-storage
//! structures incidentally. This fuzzer *deliberately* generates Ethereum-shaped
//! state (accounts at depth 64 with RLP `[nonce, balance, storageRoot, codeHash]`
//! values and per-account storage tries), so the storage-trie-root fold and the
//! partial-storage reconcile are exercised on every iteration, across both range
//! and change proofs.
//!
//! Coverage: random shaped state (weighted storage-child counts up to a full
//! 16-child branch, deep storage slots, varied value lengths, random code
//! hashes and storageRoot placeholders), with guaranteed K=1 (fold) and K>=3
//! (partial-storage) accounts. Each iteration verifies both proof kinds across
//! targeted boundary scenarios (full, storage cut, fold, both edges inside one
//! account's storage, account-only, outer, random probes), sometimes through a
//! serialization round-trip. Each scenario asserts tampered proofs are
//! rejected (forged value, forged codeHash, dropped interior storage leaf) and
//! that a forged storageRoot is still accepted, since the verifier recomputes
//! that field from the storage children.

use std::collections::{BTreeMap, HashSet};
use std::time::{Duration, Instant};

use super::change::fuzz_common::{
    build_change_proof, build_range_proof, change_proof_rejected,
    maybe_serialize_round_trip_change, maybe_serialize_round_trip_range,
};
use super::ethhash::{account_storage_key, empty_code_hash, rlp_encode_account};
use crate::api::{
    BatchOp, Db as DbTrait, DbView, FrozenChangeProof, FrozenRangeProof, HashKey, Proposal as _,
};
use crate::db::{Db, DbConfig};
use crate::merkle::{Key, Value, verify_change_proof_root_hash, verify_range_proof};
use crate::verify_change_proof_structure;
use firewood_storage::{NodeHashAlgorithm, SeededRng, replace_list_field};
use rand::seq::SliceRandom;

/// An account's key paired with its sorted, distinct storage-slot bytes (byte
/// 32 of its 64-byte storage keys). The generator only emits slot bytes of the
/// form `nibble << 4`, so each distinct byte is one depth-65 storage child.
type AccountSlots = ([u8; 32], Box<[u8]>);

/// A randomly generated Ethereum-shaped fixture: a committed start state, a
/// committed end state, and the sorted key set of the end state (for boundary
/// selection).
struct ShapedFixture {
    db: Db,
    /// Keeps the temp dir alive while `db` is in use.
    _dir: tempfile::TempDir,
    start_root: HashKey,
    end_root: HashKey,
    /// Sorted, distinct keys present in the end state.
    end_keys: Box<[Box<[u8]>]>,
    /// Each end-state account's storage slots, for aiming a boundary inside a
    /// chosen account's storage trie.
    storage_layout: Box<[AccountSlots]>,
}

/// Pick `k` distinct storage-child high nibbles (0..16): shuffle all 16 and
/// take the first `k`. Each lands in a distinct account-branch child slot, so
/// `k` is the storage-child count the fold keys off.
fn pick_distinct_nibbles(rng: &SeededRng, k: usize) -> Vec<u8> {
    let mut nibbles: Vec<u8> = (0u8..16).collect();
    nibbles.shuffle(&mut &*rng);
    nibbles.truncate(k);
    nibbles
}

/// RLP-encode an account value with random nonce/balance, a code hash that is
/// empty or random, and a deliberately wrong storageRoot placeholder (zeros or
/// garbage). Live hashing recomputes storageRoot from the account's storage
/// children, so the wrong placeholder also exercises the recompute of stale
/// stored values.
fn random_account_value(rng: &SeededRng) -> Vec<u8> {
    let nonce = u64::from(rng.random_range(0..1_000_000_u32));
    let balance = u64::from(rng.random_range(0..1_000_000_u32));
    let storage_root: [u8; 32] = if rng.random_range(0..2_u32) == 0 {
        [0; 32]
    } else {
        rng.random()
    };
    let code_hash: [u8; 32] = if rng.random_range(0..2_u32) == 0 {
        empty_code_hash()
    } else {
        rng.random()
    };
    rlp_encode_account(nonce, balance, &storage_root, &code_hash).to_vec()
}

/// RLP-encode a storage value of random length (1..=32 bytes), like geth's
/// leading-zero-trimmed storage words, so leaf encodings vary in size.
fn random_storage_value(rng: &SeededRng) -> Vec<u8> {
    use rlp::RlpStream;
    let len = rng.random_range(1..=32_usize);
    let word: Vec<u8> = (0..len).map(|_| rng.random()).collect();
    let mut rlp = RlpStream::new();
    rlp.append(&word.as_slice());
    rlp.out().to_vec()
}

/// Sample a storage-child count K (distinct depth-65 child nibbles), weighted
/// toward the proof-shaping cases: 10% K=0 (account only), 25% K=1 (the fold),
/// 60% K in 2..=15 favoring small counts (where a boundary most easily lands
/// mid-storage), 5% K=16 (fully saturated branch).
fn sample_child_count(rng: &SeededRng) -> usize {
    match rng.random_range(0..100_u32) {
        0..10 => 0,
        10..35 => 1,
        35..95 => {
            // Quadratic bias: r is uniform, so r * r lands low far more often
            // than high. The result is that K=2 gets ~27% of this band, while
            // K=15 only gets ~3%.
            let r = rng.random_range(0..100_usize);
            2_usize.saturating_add(r.saturating_mul(r).saturating_mul(14) / 10_000)
        }
        _ => 16,
    }
}

/// Generate the end state's mutation delta: a sorted, one-op-per-key map
/// (`None` = delete), which is what `propose` and the proofs expect. The
/// mutations preserve the anchor accounts' K=1 / K>=3 child counts so the
/// targeted scenarios always have an eligible account, and re-value one
/// storage child per anchor so both stay in the change set.
fn random_mutation_delta(
    rng: &SeededRng,
    accounts: &[[u8; 32]],
    state: &BTreeMap<Vec<u8>, Vec<u8>>,
    k1_account: Option<&([u8; 32], u8)>,
    multi_account: Option<&([u8; 32], Vec<u8>)>,
) -> BTreeMap<Vec<u8>, Option<Vec<u8>>> {
    // Account/storage keys whose deletion would change a protected count.
    let mut protected: HashSet<Vec<u8>> = HashSet::new();
    if let Some((account, slot)) = k1_account {
        protected.insert(account.to_vec());
        protected.insert(account_storage_key(account, *slot).to_vec());
    }
    if let Some((account, slots)) = multi_account {
        protected.insert(account.to_vec());
        for &slot in slots {
            protected.insert(account_storage_key(account, slot).to_vec());
        }
    }
    let k1_acct = k1_account.map(|(account, _)| *account);

    // Attempts, not a guaranteed op count: protected draws no-op and account
    // deletes cascade. Enough for substantive change proofs while keeping the
    // delta small against the ~20-50 key state.
    let n_mut: usize = rng.random_range(3..=12);
    let mut delta: BTreeMap<Vec<u8>, Option<Vec<u8>>> = BTreeMap::new();
    let existing_keys: Vec<Vec<u8>> = state.keys().cloned().collect();
    for _ in 0..n_mut {
        match rng.random_range(0..3_u32) {
            // Add (or overwrite) a storage slot under a random account, but not
            // the K=1 account (a new child would break its count) and not an
            // account this delta already deleted (no orphaned storage).
            0 => {
                let account = accounts[rng.random_range(0..accounts.len())];
                if Some(account) == k1_acct || matches!(delta.get(account.as_slice()), Some(None)) {
                    continue;
                }
                let nibble: u8 = rng.random_range(0..16);
                let storage_key = account_storage_key(&account, nibble << 4).to_vec();
                delta.insert(storage_key, Some(random_storage_value(rng)));
            }
            // Modify a random account value (no child-count change).
            1 => {
                let account = accounts[rng.random_range(0..accounts.len())];
                delta.insert(account.to_vec(), Some(random_account_value(rng)));
            }
            // Delete a random existing key, skipping the protected ones.
            // Deleting an account also deletes its storage children (both
            // pre-existing and ones added earlier in this delta), so the end
            // state never holds storage under a missing account.
            _ => {
                let key = existing_keys[rng.random_range(0..existing_keys.len())].clone();
                if protected.contains(&key) {
                    continue;
                }
                if key.len() == 32 {
                    let children: Vec<Vec<u8>> = existing_keys
                        .iter()
                        .chain(delta.keys())
                        .filter(|k| k.len() == 64 && k.starts_with(&key))
                        .cloned()
                        .collect();
                    for child in children {
                        delta.insert(child, None);
                    }
                }
                delta.insert(key, None);
            }
        }
    }

    // Keep the targeted accounts in the change set so the change-proof
    // fold/reconcile paths fire, without altering their counts: re-value the
    // K=1 account's child and one of the multi-child account's children.
    if let Some((account, slot)) = k1_account {
        delta.insert(
            account_storage_key(account, *slot).to_vec(),
            Some(random_storage_value(rng)),
        );
    }
    if let Some((account, slots)) = multi_account {
        let slot = slots[rng.random_range(0..slots.len())];
        delta.insert(
            account_storage_key(account, slot).to_vec(),
            Some(random_storage_value(rng)),
        );
    }
    delta
}

/// Build a random Ethereum-shaped DB: several accounts, each with a weighted
/// random storage-child count, *guaranteeing* at least one K=1 account (forces
/// the storage-trie-root fold) and at least one K>=3 account (enables the
/// partial-storage and both-edges scenarios). Then apply a random batch of
/// storage/account mutations to produce the end state.
fn build_shaped_db(rng: &SeededRng) -> ShapedFixture {
    // ── Distinct account keys ─────────────────────────────────────
    let n_accounts = rng.random_range(4..=10);
    // Distinct keys in (random) generation order, so the index-0 and index-1
    // anchors picked below sit at random positions in the key space.
    let mut accounts: Vec<[u8; 32]> = Vec::with_capacity(n_accounts);
    while accounts.len() < n_accounts {
        let key: [u8; 32] = rng.random();
        if !accounts.contains(&key) {
            accounts.push(key);
        }
    }

    // ── Start state (account + storage children per account) ──────
    // `state` is the source of truth; the start batch is committed from it and
    // the end key set is read back from it after mutation.
    let mut state: BTreeMap<Vec<u8>, Vec<u8>> = BTreeMap::new();
    // The anchor accounts. The end-state mutations must not change their
    // storage-child counts (the targeted scenarios depend on them), so each
    // is recorded as (account key, storage-slot bytes) for the checks below.
    let mut k1_account: Option<([u8; 32], u8)> = None;
    let mut multi_account: Option<([u8; 32], Vec<u8>)> = None;
    for (i, account) in accounts.iter().enumerate() {
        state.insert(account.to_vec(), random_account_value(rng));
        // Account 0 gets exactly one storage child (fold), account 1 at least
        // three (partial-storage and both-edges); the rest follow the
        // weighted distribution. Account 1 is capped at 8 to limit the
        // number of protected slots.
        let k = match i {
            0 => 1,
            1 => rng.random_range(3..=8),
            _ => sample_child_count(rng),
        };
        let slots: Vec<u8> = pick_distinct_nibbles(rng, k)
            .iter()
            .map(|&n| n << 4)
            .collect();
        for &slot in &slots {
            let storage_key = account_storage_key(account, slot).to_vec();
            state.insert(storage_key, random_storage_value(rng));
            // Sometimes deepen a child: a second key differing only at byte 33
            // lands under the same depth-65 child (same slot byte) and adds a
            // branch inside it instead of a new child, so storage tries are
            // not uniformly one level deep. Anchor accounts (0 and 1) stay
            // one-key-per-child so the targeted-scenario guarantees hold.
            if i >= 2 && rng.random_range(0..4_u32) == 0 {
                let mut deep_key = account_storage_key(account, slot).to_vec();
                deep_key[33] = 1;
                state.insert(deep_key, random_storage_value(rng));
            }
        }
        match i {
            0 => k1_account = Some((*account, slots[0])),
            1 => multi_account = Some((*account, slots)),
            _ => {}
        }
    }
    let dir = tempfile::tempdir().unwrap();
    let db = Db::new(dir.path(), DbConfig::builder().build()).unwrap();
    let start_batch: Vec<BatchOp<Vec<u8>, Vec<u8>>> = state
        .iter()
        .map(|(k, v)| BatchOp::Put {
            key: k.clone(),
            value: v.clone(),
        })
        .collect();
    db.propose(start_batch).unwrap().commit().unwrap();
    let start_root = db.root_hash().unwrap();

    // ── End state ─────────────────────────────────────────────────
    let delta = random_mutation_delta(
        rng,
        &accounts,
        &state,
        k1_account.as_ref(),
        multi_account.as_ref(),
    );

    let mut end_batch: Vec<BatchOp<Vec<u8>, Vec<u8>>> = Vec::new();
    for (key, op) in &delta {
        if let Some(value) = op {
            state.insert(key.clone(), value.clone());
            end_batch.push(BatchOp::Put {
                key: key.clone(),
                value: value.clone(),
            });
        } else {
            state.remove(key);
            end_batch.push(BatchOp::Delete { key: key.clone() });
        }
    }
    // `delta` is never empty: the anchor re-values above always add a Put.
    db.propose(end_batch).unwrap().commit().unwrap();
    let end_root = db.root_hash().unwrap();

    // A storage key is `account(32) ++ [slot, ...]`, so byte 32 is its slot;
    // `state` iterates in sorted key order, so each account's slots arrive
    // already sorted. Deep keys share their shallow sibling's slot byte, so
    // adjacent duplicates are skipped: distinct slots equal depth-65 children.
    let mut layout: BTreeMap<[u8; 32], Vec<u8>> = BTreeMap::new();
    for key in state.keys() {
        if key.len() == 64 {
            let account: [u8; 32] = key[0..32].try_into().unwrap();
            let slots = layout.entry(account).or_default();
            if slots.last() != Some(&key[32]) {
                slots.push(key[32]);
            }
        }
    }

    ShapedFixture {
        db,
        _dir: dir,
        start_root,
        end_root,
        end_keys: state.keys().map(|k| k.clone().into_boxed_slice()).collect(),
        storage_layout: layout
            .into_iter()
            .map(|(account, slots)| (account, slots.into_boxed_slice()))
            .collect(),
    }
}

/// Bounds that cut strictly inside some account's storage trie, between two
/// adjacent present storage slots, so a proof over `[start, end]` holds a
/// proper subset of that account's storage children (the partial-storage
/// trigger). `None` if no account has >= 2 storage children.
fn storage_cut_bounds(rng: &SeededRng, layout: &[AccountSlots]) -> Option<(Vec<u8>, Vec<u8>)> {
    let multi: Box<[&AccountSlots]> = layout.iter().filter(|(_, s)| s.len() >= 2).collect();
    if multi.is_empty() {
        return None;
    }
    let (account, slots) = multi[rng.random_range(0..multi.len())];
    // Drop the largest slot, then cut just above one of the rest. Slots are
    // multiples of 0x10, so `lo + 1` is below every higher slot: children at
    // slots <= lo stay in range while at least the largest one is excluded.
    let (_, lower_slots) = slots.split_last()?;
    let lo = lower_slots[rng.random_range(0..lower_slots.len())];
    let cut = lo.saturating_add(1);
    Some((account.to_vec(), account_storage_key(account, cut).to_vec()))
}

/// Bounds spanning a single-storage-child account from its account key through
/// its lone child's first key, so the child is in range and the verifier must
/// fold it as a standalone storage-trie root. `None` if no account has exactly
/// one child.
fn fold_bounds(rng: &SeededRng, layout: &[AccountSlots]) -> Option<(Vec<u8>, Vec<u8>)> {
    let singles: Box<[&AccountSlots]> = layout.iter().filter(|(_, s)| s.len() == 1).collect();
    if singles.is_empty() {
        return None;
    }
    let (account, slots) = singles[rng.random_range(0..singles.len())];
    Some((
        account.to_vec(),
        account_storage_key(account, slots[0]).to_vec(),
    ))
}

/// Bounds landing strictly inside one account's storage trie on both sides:
/// start just above the lowest slot and end just above the second-highest, so
/// the proof holds a middle subset of that account's children (a left
/// exclusion and a right truncation in the same storage trie). `None` if no
/// account has >= 3 storage children.
fn both_edges_bounds(rng: &SeededRng, layout: &[AccountSlots]) -> Option<(Vec<u8>, Vec<u8>)> {
    let wide: Box<[&AccountSlots]> = layout.iter().filter(|(_, s)| s.len() >= 3).collect();
    if wide.is_empty() {
        return None;
    }
    let (account, slots) = wide[rng.random_range(0..wide.len())];
    let lo = slots.first()?.saturating_add(1);
    let hi = slots[slots.len().saturating_sub(2)].saturating_add(1);
    Some((
        account_storage_key(account, lo).to_vec(),
        account_storage_key(account, hi).to_vec(),
    ))
}

/// A single-key range over one account that has storage children, so every
/// child is out of range: the safe truncated-at-the-account case, which must
/// verify without folding or reconciling anything.
fn account_only_bounds(rng: &SeededRng, layout: &[AccountSlots]) -> Option<(Vec<u8>, Vec<u8>)> {
    if layout.is_empty() {
        return None;
    }
    let (account, _) = &layout[rng.random_range(0..layout.len())];
    Some((account.to_vec(), account.to_vec()))
}

/// Run `check` over every boundary scenario for one fixture: the full range,
/// the targeted bounds (storage cut, fold, both edges, account only), the
/// outer existing bounds, and a few random existing-key pairs.
fn check_all_scenarios(
    rng: &SeededRng,
    storage_layout: &[AccountSlots],
    end_keys: &[Box<[u8]>],
    check: impl Fn(Option<&[u8]>, Option<&[u8]>, &str),
) {
    // Full range.
    check(None, None, "full");

    // Targeted: a bound cutting inside a multi-child account's storage
    // (partial-storage reconcile), and a range spanning a single-child
    // account's lone child (fold). The protected anchor accounts make both
    // bounds always available; `None` means the fixture builder regressed.
    let (start, end) = storage_cut_bounds(rng, storage_layout)
        .expect("fixture guarantees an account with at least two storage children");
    check(Some(&start), Some(&end), "storage_cut");
    let (start, end) = fold_bounds(rng, storage_layout)
        .expect("fixture guarantees an account with exactly one storage child");
    check(Some(&start), Some(&end), "fold");
    let (start, end) = both_edges_bounds(rng, storage_layout)
        .expect("fixture guarantees an account with at least three storage children");
    check(Some(&start), Some(&end), "both_edges");
    let (start, end) = account_only_bounds(rng, storage_layout)
        .expect("fixture guarantees an account with storage children");
    check(Some(&start), Some(&end), "account_only");

    // Breadth: the outer existing bounds and a few random existing-key pairs.
    if end_keys.len() >= 2 {
        let first = end_keys.first().unwrap().as_ref();
        let last = end_keys.last().unwrap().as_ref();
        check(Some(first), Some(last), "outer");

        // A random number of random existing-key bound pairs, on top of the
        // targeted scenarios.
        for probe in 0..rng.random_range(1..=4_u32) {
            let i = rng.random_range(0..end_keys.len());
            let j = rng.random_range(0..end_keys.len());
            let (lo, hi) = if i <= j { (i, j) } else { (j, i) };
            check(
                Some(end_keys[lo].as_ref()),
                Some(end_keys[hi].as_ref()),
                &format!("random_bounds_{probe}"),
            );
        }
    }
}

/// Generate and verify a valid range proof over `root` for the given bounds
/// (sometimes through a serialization round-trip), returning it for tampering.
fn check_valid_range_proof(
    db: &Db,
    root: &HashKey,
    first: Option<&[u8]>,
    last: Option<&[u8]>,
    rng: &SeededRng,
    locator: &str,
) -> FrozenRangeProof {
    let view = db.revision(root.clone()).unwrap();
    let range_proof = view
        .range_proof(first, last, None)
        .unwrap_or_else(|e| panic!("range_proof should succeed ({locator}): {e}"));
    let range_proof = maybe_serialize_round_trip_range(rng, range_proof);
    // This fuzzer is ethhash-gated, so every proof it builds is Ethereum-mode.
    verify_range_proof(first, last, root, NodeHashAlgorithm::Ethereum, &range_proof)
        .unwrap_or_else(|e| panic!("valid range proof should verify ({locator}): {e}"));
    range_proof
}

/// Generate and verify a valid change proof from `start_root` to `end_root` for
/// the given bounds (sometimes through a serialization round-trip), returning
/// it for tampering.
fn check_valid_change_proof(
    db: &Db,
    start_root: &HashKey,
    end_root: &HashKey,
    first: Option<&[u8]>,
    last: Option<&[u8]>,
    rng: &SeededRng,
    locator: &str,
) -> FrozenChangeProof {
    let change_proof = db
        .change_proof(start_root.clone(), end_root.clone(), first, last, None)
        .unwrap_or_else(|e| panic!("change_proof should succeed ({locator}): {e}"));
    let change_proof = maybe_serialize_round_trip_change(rng, change_proof);
    let ctx = verify_change_proof_structure(
        &change_proof,
        end_root.clone(),
        first,
        last,
        NodeHashAlgorithm::Ethereum,
        None,
    )
    .unwrap_or_else(|e| panic!("valid change proof structure should verify ({locator}): {e}"));
    let parent = db.revision(start_root.clone()).unwrap();
    let proposal = db
        .apply_change_proof_to_parent(&change_proof, &*parent)
        .unwrap_or_else(|e| panic!("apply_change_proof_to_parent should succeed ({locator}): {e}"));
    verify_change_proof_root_hash(&change_proof, &ctx, &proposal)
        .unwrap_or_else(|e| panic!("valid change proof root hash should verify ({locator}): {e}"));
    change_proof
}

/// Tamper a change proof by flipping one `Put`'s value, so the verifier must
/// reject it. `None` if the proof has no `Put`.
///
/// `^= 0xFF` on byte 0 (the RLP prefix) keeps the must-reject assertion sound on
/// two fronts. The non-zero mask guarantees the value actually changes (an
/// unchanged value would still verify). And byte 0 is never an account's
/// `storageRoot` field, which the verifier recomputes from children and accepts
/// even when tampered.
fn forge_a_put_value(proof: &FrozenChangeProof, rng: &SeededRng) -> Option<FrozenChangeProof> {
    let mut ops = proof.batch_ops().to_vec();
    let put_indices: Vec<usize> = ops
        .iter()
        .enumerate()
        .filter(|(_, op)| matches!(op, BatchOp::Put { .. }))
        .map(|(i, _)| i)
        .collect();
    if put_indices.is_empty() {
        return None;
    }
    let idx = put_indices[rng.random_range(0..put_indices.len())];
    let BatchOp::Put { key, value } = &ops[idx] else {
        return None;
    };
    let mut new_value = value.to_vec();
    if new_value.is_empty() {
        new_value.push(0);
    }
    new_value[0] ^= 0xFF;
    ops[idx] = BatchOp::Put {
        key: key.clone(),
        value: new_value.into(),
    };
    Some(build_change_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        ops,
    ))
}

/// Drop one storage-leaf `Put` (64-byte key) that is neither the first nor the
/// last batch op, so the verifier must reject the now-incomplete change proof.
/// `None` if there is no such op.
///
/// A dropped in-range `Put` must not be silently accepted. The dropped key must
/// be a storage leaf, not an account: an account `Put` that only restates its
/// `storageRoot` is non-authoritative (the verifier recomputes that field from
/// the storage children), so omitting it leaves the root unchanged and is
/// correctly accepted. A storage leaf is authoritative, so omitting it changes
/// the account's recomputed `storageRoot` and the root no longer matches.
/// Interior-only keeps the omission inside the boundary proofs.
fn drop_an_interior_put(proof: &FrozenChangeProof, rng: &SeededRng) -> Option<FrozenChangeProof> {
    let ops = proof.batch_ops();
    if ops.len() < 3 {
        return None;
    }
    let last_idx = ops.len().saturating_sub(1);
    let storage_puts: Box<[usize]> = (1..last_idx)
        .filter(|&i| matches!(&ops[i], BatchOp::Put { key, .. } if key.len() == 64))
        .collect();
    if storage_puts.is_empty() {
        return None;
    }
    let drop_idx = storage_puts[rng.random_range(0..storage_puts.len())];
    let mut kept = ops.to_vec();
    kept.remove(drop_idx);
    Some(build_change_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        kept,
    ))
}

/// Tamper a range proof by flipping one key-value pair's value, so the verifier
/// must reject it. `None` if the proof has no pairs. As in [`forge_a_put_value`],
/// `^= 0xFF` on byte 0 guarantees the value really changes and never touches an
/// account's `storageRoot` field (which the verifier recomputes from children).
fn forge_a_range_value(proof: &FrozenRangeProof, rng: &SeededRng) -> Option<FrozenRangeProof> {
    let mut kvs = proof.key_values().to_vec();
    if kvs.is_empty() {
        return None;
    }
    let idx = rng.random_range(0..kvs.len());
    let mut new_value = kvs[idx].1.to_vec();
    if new_value.is_empty() {
        new_value.push(0);
    }
    new_value[0] ^= 0xFF;
    kvs[idx].1 = new_value.into();
    Some(build_range_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        kvs,
    ))
}

/// Drop one storage-leaf pair (64-byte key) that is neither the first nor the
/// last, so the reconstructed trie is missing an interior leaf and no longer
/// hashes to the root. `None` if there is no such pair. Restricting to a storage
/// leaf keeps the dropped value authoritative: omitting it changes the account's
/// recomputed `storageRoot`, so the root always mismatches. Interior-only keeps
/// the omission inside the boundary proofs.
fn drop_an_interior_kv(proof: &FrozenRangeProof, rng: &SeededRng) -> Option<FrozenRangeProof> {
    let kvs = proof.key_values();
    if kvs.len() < 3 {
        return None;
    }
    let last_idx = kvs.len().saturating_sub(1);
    let storage_kvs: Box<[usize]> = (1..last_idx).filter(|&i| kvs[i].0.len() == 64).collect();
    if storage_kvs.is_empty() {
        return None;
    }
    let drop_idx = storage_kvs[rng.random_range(0..storage_kvs.len())];
    let mut kept = kvs.to_vec();
    kept.remove(drop_idx);
    Some(build_range_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        kept,
    ))
}

/// Index of a random interior account `Put` (32-byte key, neither the first
/// nor the last op), or `None`. Interior keeps the op clear of the
/// boundary-key consistency checks, so a forged value is judged only by
/// reconcile and the root-hash check.
fn pick_interior_account_put(ops: &[BatchOp<Key, Value>], rng: &SeededRng) -> Option<usize> {
    if ops.len() < 3 {
        return None;
    }
    let last_idx = ops.len().saturating_sub(1);
    let candidates: Box<[usize]> = (1..last_idx)
        .filter(|&i| matches!(&ops[i], BatchOp::Put { key, .. } if key.len() == 32))
        .collect();
    if candidates.is_empty() {
        return None;
    }
    Some(candidates[rng.random_range(0..candidates.len())])
}

/// Index of a random interior account pair (32-byte key, neither the first
/// nor the last) that is not an ancestor of either boundary key. An ancestor
/// account's true value digest rides the boundary proof, so excluding them
/// makes a storageRoot forgery depend only on the hash-time recompute.
fn pick_interior_account_kv(kvs: &[(Key, Value)], rng: &SeededRng) -> Option<usize> {
    if kvs.len() < 3 {
        return None;
    }
    let first_key = kvs.first()?.0.as_ref();
    let last_key = kvs.last()?.0.as_ref();
    let last_idx = kvs.len().saturating_sub(1);
    let candidates: Box<[usize]> = (1..last_idx)
        .filter(|&i| {
            let key = kvs[i].0.as_ref();
            key.len() == 32 && !first_key.starts_with(key) && !last_key.starts_with(key)
        })
        .collect();
    if candidates.is_empty() {
        return None;
    }
    Some(candidates[rng.random_range(0..candidates.len())])
}

/// Forge the storageRoot field (RLP field 2) of one interior account `Put`.
/// The verifier recomputes storageRoot from the account's storage children at
/// hash time, so the forged proof must still be ACCEPTED: the reconcile
/// relaxation ignores exactly this field and nothing else.
fn forge_a_storage_root(proof: &FrozenChangeProof, rng: &SeededRng) -> Option<FrozenChangeProof> {
    let mut ops = proof.batch_ops().to_vec();
    let idx = pick_interior_account_put(&ops, rng)?;
    let BatchOp::Put { key, value } = &ops[idx] else {
        return None;
    };
    let forged = replace_list_field(value, 2, &rng.random::<[u8; 32]>()).ok()?;
    ops[idx] = BatchOp::Put {
        key: key.clone(),
        value: forged,
    };
    Some(build_change_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        ops,
    ))
}

/// Forge the codeHash field (RLP field 3) of one interior account `Put` with a
/// guaranteed-different value, so the proof must be REJECTED: every account
/// field except storageRoot is authoritative.
fn forge_a_code_hash(proof: &FrozenChangeProof, rng: &SeededRng) -> Option<FrozenChangeProof> {
    let mut ops = proof.batch_ops().to_vec();
    let idx = pick_interior_account_put(&ops, rng)?;
    let BatchOp::Put { key, value } = &ops[idx] else {
        return None;
    };
    let mut forged = replace_list_field(value, 3, &[0xAB; 32]).ok()?;
    if forged.as_ref() == value.as_ref() {
        forged = replace_list_field(value, 3, &[0xCD; 32]).ok()?;
    }
    ops[idx] = BatchOp::Put {
        key: key.clone(),
        value: forged,
    };
    Some(build_change_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        ops,
    ))
}

/// Range-proof twin of [`forge_a_storage_root`]: forge the storageRoot of one
/// interior account pair; the rebuilt trie recomputes that field at hash time,
/// so the forged proof must still verify.
fn forge_range_storage_root(proof: &FrozenRangeProof, rng: &SeededRng) -> Option<FrozenRangeProof> {
    let mut kvs = proof.key_values().to_vec();
    let idx = pick_interior_account_kv(&kvs, rng)?;
    let forged = replace_list_field(&kvs[idx].1, 2, &rng.random::<[u8; 32]>()).ok()?;
    kvs[idx].1 = forged;
    Some(build_range_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        kvs,
    ))
}

/// Range-proof twin of [`forge_a_code_hash`]: forge the codeHash of one
/// interior account pair with a guaranteed-different value; the rebuilt trie
/// hashes the authoritative field, so the proof must be rejected.
fn forge_range_code_hash(proof: &FrozenRangeProof, rng: &SeededRng) -> Option<FrozenRangeProof> {
    let mut kvs = proof.key_values().to_vec();
    let idx = pick_interior_account_kv(&kvs, rng)?;
    let mut forged = replace_list_field(&kvs[idx].1, 3, &[0xAB; 32]).ok()?;
    if forged.as_ref() == kvs[idx].1.as_ref() {
        forged = replace_list_field(&kvs[idx].1, 3, &[0xCD; 32]).ok()?;
    }
    kvs[idx].1 = forged;
    Some(build_range_proof(
        proof.start_proof().as_ref().to_vec(),
        proof.end_proof().as_ref().to_vec(),
        kvs,
    ))
}

#[test]
fn test_slow_ethhash_proof_fuzz() {
    let run_one = |run: usize, seed: u64| {
        eprintln!("run {run}: seed={seed} (export FIREWOOD_TEST_SEED={seed} to reproduce)");
        let rng = SeededRng::new(seed);

        let fixture = build_shaped_db(&rng);
        let ShapedFixture {
            db,
            start_root,
            end_root,
            end_keys,
            storage_layout,
            ..
        } = &fixture;

        let check = |first: Option<&[u8]>, last: Option<&[u8]>, label: &str| {
            let locator = format!("seed={seed}, run={run}, scenario={label}");
            // Positive: valid range and change proofs verify.
            let range = check_valid_range_proof(db, end_root, first, last, &rng, &locator);
            let change =
                check_valid_change_proof(db, start_root, end_root, first, last, &rng, &locator);

            // Negative: every tamper of the valid change proof must be rejected.
            for (kind, tampered) in [
                ("forge", forge_a_put_value(&change, &rng)),
                ("forge_code_hash", forge_a_code_hash(&change, &rng)),
                ("drop", drop_an_interior_put(&change, &rng)),
            ] {
                if let Some(tampered) = tampered {
                    assert!(
                        change_proof_rejected(db, &tampered, start_root, end_root, first, last),
                        "tampered change proof must be rejected ({kind}, {locator})"
                    );
                }
            }

            // Positive: a forged storageRoot must still be ACCEPTED. The
            // verifier recomputes that field from the storage children at hash
            // time, pinning the reconcile relaxation to exactly that field.
            if let Some(forged) = forge_a_storage_root(&change, &rng) {
                assert!(
                    !change_proof_rejected(db, &forged, start_root, end_root, first, last),
                    "forged storageRoot change proof must be accepted ({locator})"
                );
            }

            // Negative: every tamper of the valid range proof must be rejected.
            for (kind, tampered) in [
                ("forge", forge_a_range_value(&range, &rng)),
                ("forge_code_hash", forge_range_code_hash(&range, &rng)),
                ("drop", drop_an_interior_kv(&range, &rng)),
            ] {
                if let Some(tampered) = tampered {
                    assert!(
                        verify_range_proof(
                            first,
                            last,
                            end_root,
                            NodeHashAlgorithm::Ethereum,
                            &tampered
                        )
                        .is_err(),
                        "tampered range proof must be rejected ({kind}, {locator})"
                    );
                }
            }

            // Positive: as for change proofs, a forged storageRoot in a range
            // proof must still be accepted (recomputed at hash time).
            if let Some(forged) = forge_range_storage_root(&range, &rng) {
                assert!(
                    verify_range_proof(first, last, end_root, NodeHashAlgorithm::Ethereum, &forged)
                        .is_ok(),
                    "forged storageRoot range proof must be accepted ({locator})"
                );
            }
        };

        check_all_scenarios(&rng, storage_layout, end_keys, check);
    };

    if let Ok(s) = std::env::var("FIREWOOD_TEST_SEED") {
        // Run only this seed, for reproducing CI failures. Takes precedence
        // over the soak knob.
        run_one(0, s.parse().expect("FIREWOOD_TEST_SEED must be a u64"));
    } else if let Ok(s) = std::env::var("FIREWOOD_TEST_SOAK_SECONDS") {
        // Soak: fresh random seeds until the deadline has passed. Useful for
        // long local soak runs.
        let secs: u64 = s.parse().expect("FIREWOOD_TEST_SOAK_SECONDS must be a u64");
        let deadline = Instant::now() + Duration::from_secs(secs);
        let soak_rng = SeededRng::from_random();
        for run in 0.. {
            if Instant::now() >= deadline {
                break;
            }
            run_one(run, soak_rng.next_u64());
        }
    } else {
        // Debug assertions significantly slow down each iteration; use fewer
        // iterations in debug builds so the test finishes in reasonable time.
        let iterations = if cfg!(debug_assertions) { 40 } else { 250 };
        let outer_rng = SeededRng::from_random();
        for run in 0..iterations {
            run_one(run, outer_rng.next_u64());
        }
    }
}
