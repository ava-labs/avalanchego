// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use criterion::{Criterion, criterion_group, criterion_main};
use firewood::db::{BatchOp, Db, DbConfig};
use firewood::v2::api::{Db as _, Proposal as _};
use firewood_storage::NodeHashAlgorithm;
use rand::{RngExt, distr::Alphanumeric};
use std::iter::repeat_with;
use std::num::NonZeroU64;

#[expect(clippy::unwrap_used)]
fn bench_deferred_persistence<const N: usize, const COMMIT_COUNT: u64>(criterion: &mut Criterion) {
    const KEY_LEN: usize = 4;
    let rng = &firewood_storage::SeededRng::from_option(Some(1234));
    let commit_count = NonZeroU64::new(COMMIT_COUNT).unwrap();

    criterion
        .benchmark_group("deferred_persistence")
        .sample_size(20)
        .bench_function(format!("commit_count_{COMMIT_COUNT}"), |b| {
            b.iter_batched(
                || {
                    let batch_ops: Vec<_> =
                        repeat_with(|| rng.sample_iter(&Alphanumeric).take(KEY_LEN).collect())
                            .map(|key: Vec<_>| BatchOp::Put {
                                key,
                                value: vec![b'v'],
                            })
                            .take(N)
                            .collect();
                    batch_ops
                },
                |batch_ops| {
                    let tmpdir = tempfile::tempdir().unwrap();
                    let dbcfg = DbConfig::builder()
                        .node_hash_algorithm(NodeHashAlgorithm::compile_option())
                        .deferred_persistence_commit_count(commit_count)
                        .build();
                    let db = Db::new(tmpdir, dbcfg).unwrap();

                    for op in batch_ops {
                        let proposal = db.propose(vec![op]).unwrap();
                        proposal.commit().unwrap();
                    }

                    db.close().unwrap();
                },
                criterion::BatchSize::SmallInput,
            );
        });
}

// Commit count values span powers of 10 (1, 10, 100, 1_000) to show the
// performance curve from persisting every commit to persisting just once.
criterion_group! {
    name = benches;
    config = Criterion::default();
    targets = bench_deferred_persistence::<1_000, 1>,
              bench_deferred_persistence::<1_000, 10>,
              bench_deferred_persistence::<1_000, 100>,
              bench_deferred_persistence::<1_000, 1_000>
}

criterion_main!(benches);
