// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Benchmarks comparing the in-tree `firewood_storage::rlp` module against
//! the upstream `rlp = "0.6"` crate on the shapes firewood actually uses.
//!
//! Four workloads:
//!
//! 1. **`branch_encode`** — encode a 17-element list (the Ethereum MPT
//!    branch shape: 16 32-byte child hashes + an empty value).
//! 2. **`leaf_encode`** — encode a 2-element list (compact path + 32-byte
//!    value), the leaf shape.
//! 3. **`storage_root_replace`** — the hot account-rewrite path: take a
//!    pre-encoded `[nonce, balance, storageRoot, codeHash]` list and
//!    replace the 32-byte `storageRoot` field. This is where the new
//!    module should win biggest because it skips the upstream crate's
//!    `Vec<Vec<u8>>` round-trip.
//! 4. **`decode_field3`** — extract field 3 (the code-hash) from a
//!    pre-encoded account RLP. Mirrors the FFI proof code-hash extractor
//!    in `ffi/src/proofs/change.rs`.

#![expect(
    clippy::unwrap_used,
    clippy::indexing_slicing,
    reason = "benchmarks panic on bad fixtures, that's fine"
)]

use std::hint::black_box;

use criterion::{Criterion, criterion_group, criterion_main};
use firewood_storage::{RlpItem, RlpList, encode_list, replace_list_field};

// ---------- fixtures ----------

fn branch_children() -> [[u8; 32]; 16] {
    let mut out = [[0u8; 32]; 16];
    for (i, child) in out.iter_mut().enumerate() {
        child.fill(u8::try_from(i).unwrap_or(0xff));
    }
    out
}

fn account_value() -> Vec<u8> {
    // [nonce=1, balance=0x1234, storageRoot=0xaa.., codeHash=0xbb..]
    let nonce: &[u8] = &[0x01];
    let balance: &[u8] = &[0x12, 0x34];
    let storage_root = [0xaau8; 32];
    let code_hash = [0xbbu8; 32];

    let mut s = rlp::RlpStream::new_list(4);
    s.append(&nonce);
    s.append(&balance);
    s.append(&storage_root.as_slice());
    s.append(&code_hash.as_slice());
    s.out().to_vec()
}

// ---------- branch encode ----------

fn bench_branch_encode_upstream(c: &mut Criterion) {
    let children = branch_children();
    c.bench_function("branch_encode/upstream", |b| {
        b.iter(|| {
            let mut s = rlp::RlpStream::new_list(17);
            for child in black_box(&children) {
                s.append(&child.as_slice());
            }
            s.append_empty_data();
            black_box(s.out())
        });
    });
}

fn bench_branch_encode_ours(c: &mut Criterion) {
    let children = branch_children();
    c.bench_function("branch_encode/ours", |b| {
        b.iter(|| {
            let mut items = [RlpItem::Empty; 17];
            for (slot, child) in items.iter_mut().zip(black_box(&children).iter()) {
                *slot = RlpItem::Bytes(child.as_slice());
            }
            black_box(encode_list(&items))
        });
    });
}

// ---------- leaf encode ----------

fn bench_leaf_encode_upstream(c: &mut Criterion) {
    let path: &[u8] = &[0x20, 0x01, 0x02, 0x03, 0x04];
    let value = [0xccu8; 32];
    c.bench_function("leaf_encode/upstream", |b| {
        b.iter(|| {
            let mut s = rlp::RlpStream::new_list(2);
            s.append(&black_box(path));
            s.append(&black_box(value).as_slice());
            black_box(s.out())
        });
    });
}

fn bench_leaf_encode_ours(c: &mut Criterion) {
    let path: &[u8] = &[0x20, 0x01, 0x02, 0x03, 0x04];
    let value = [0xccu8; 32];
    c.bench_function("leaf_encode/ours", |b| {
        b.iter(|| {
            black_box(encode_list(&[
                RlpItem::Bytes(black_box(path)),
                RlpItem::Bytes(black_box(value).as_slice()),
            ]))
        });
    });
}

// ---------- storage root replace ----------
//
// This is the firewood-specific hot path: rewrite the storageRoot (field 2)
// of an account RLP. The upstream version mirrors the current
// `replace_hash` helper: decode to `Vec<Vec<u8>>`, mutate, re-encode.

fn bench_storage_root_replace_upstream(c: &mut Criterion) {
    let value = account_value();
    let new_root = [0xddu8; 32];
    c.bench_function("storage_root_replace/upstream", |b| {
        b.iter(|| {
            let bytes = black_box(&value);
            let mut list: Vec<Vec<u8>> = rlp::Rlp::new(bytes).as_list().unwrap();
            list[2] = black_box(new_root).to_vec();
            let mut s = rlp::RlpStream::new_list(list.len());
            for item in &list {
                s.append(item);
            }
            black_box(s.out())
        });
    });
}

fn bench_storage_root_replace_ours(c: &mut Criterion) {
    let value = account_value();
    let new_root = [0xddu8; 32];
    c.bench_function("storage_root_replace/ours", |b| {
        b.iter(|| {
            black_box(replace_list_field(black_box(&value), 2, black_box(&new_root)).unwrap())
        });
    });
}

// ---------- decode field 3 (FFI proof code-hash extraction) ----------

fn bench_decode_field_upstream(c: &mut Criterion) {
    let value = account_value();
    c.bench_function("decode_field3/upstream", |b| {
        b.iter(|| {
            let bytes = black_box(&value);
            let field = rlp::Rlp::new(bytes).at(3).and_then(|r| r.data()).unwrap();
            black_box(field)
        });
    });
}

fn bench_decode_field_ours(c: &mut Criterion) {
    let value = account_value();
    c.bench_function("decode_field3/ours", |b| {
        b.iter(|| {
            let bytes = black_box(&value);
            let list = RlpList::parse(bytes).unwrap();
            let field = list.nth_bytes(3).unwrap();
            black_box(field)
        });
    });
}

criterion_group!(
    benches,
    bench_branch_encode_upstream,
    bench_branch_encode_ours,
    bench_leaf_encode_upstream,
    bench_leaf_encode_ours,
    bench_storage_root_replace_upstream,
    bench_storage_root_replace_ours,
    bench_decode_field_upstream,
    bench_decode_field_ours,
);
criterion_main!(benches);
