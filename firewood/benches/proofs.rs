// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//! Proof generation and verification throughput.
//!
//! Builds a hashed in-memory 16-ary trie of 1000 random 32-byte keys (so branch
//! nodes near the root are densely populated) and benchmarks single-key and range
//! proof generation and verification. Guards the `ProofNode.child_hashes`
//! representation change against throughput regressions.

use std::iter::repeat_with;
use std::num::NonZeroUsize;
use std::sync::Arc;

use criterion::{Criterion, criterion_group, criterion_main};
use firewood::Merkle;
use firewood::api::DbView as _;
use firewood::verify_range_proof_structure;
use firewood_storage::{DeletedNodeTracking, MemStore, NodeStore, SeededRng};
use rand::{RngExt, distr::Alphanumeric};

#[expect(clippy::unwrap_used, clippy::indexing_slicing)]
fn bench_proofs(criterion: &mut Criterion) {
    // Fixture: 1000 random 32-byte keys, 1-byte values, hashed and frozen.
    let rng = &SeededRng::from_option(Some(1234));
    let store = Arc::new(MemStore::default());
    let nodestore = NodeStore::new_empty_proposal(store, DeletedNodeTracking::Enabled);
    let mut merkle = Merkle::from(nodestore);

    let mut keys: Vec<Vec<u8>> =
        repeat_with(|| rng.sample_iter(&Alphanumeric).take(32).collect::<Vec<u8>>())
            .take(1000)
            .collect();
    keys.sort();
    keys.dedup();
    for key in &keys {
        merkle.insert(key, Box::new(*b"v")).unwrap();
    }

    let frozen = merkle.hash();
    let view = frozen.nodestore();
    let root_hash = view.root_hash().expect("a non-empty trie has a root hash");

    let first = keys.first().expect("at least one key").as_slice();
    let last = keys.last().expect("at least one key").as_slice();
    let mid = keys[keys.len() / 2].as_slice();
    let limit = NonZeroUsize::new(500);

    let mut group = criterion.benchmark_group("Proofs");
    group.sample_size(30);

    group.bench_function("single_key_proof/generate", |b| {
        b.iter(|| view.single_key_proof(mid).unwrap());
    });
    group.bench_function("single_key_proof/verify", |b| {
        let proof = view.single_key_proof(mid).unwrap();
        b.iter(|| {
            proof
                .verify(mid, Some(b"v".as_slice()), &root_hash)
                .unwrap();
        });
    });
    group.bench_function("range_proof/generate", |b| {
        b.iter(|| view.range_proof(Some(first), Some(last), limit).unwrap());
    });
    group.bench_function("range_proof/verify", |b| {
        let proof = view.range_proof(Some(first), Some(last), limit).unwrap();
        b.iter(|| {
            verify_range_proof_structure(&proof, root_hash.clone(), Some(first), Some(last), limit)
                .unwrap();
        });
    });

    group.finish();
}

criterion_group!(benches, bench_proofs);
criterion_main!(benches);
