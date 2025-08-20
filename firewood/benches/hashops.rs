// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// hash benchmarks; run with 'cargo bench'

use criterion::profiler::Profiler;
use criterion::{BatchSize, Criterion, criterion_group, criterion_main};
use firewood::db::{BatchOp, DbConfig};
use firewood::merkle::Merkle;
use firewood::v2::api::{Db as _, Proposal as _};
use firewood_storage::{MemStore, NodeStore};
use pprof::ProfilerGuard;
use rand::{Rng, distr::Alphanumeric};
use std::fs::File;
use std::iter::repeat_with;
use std::os::raw::c_int;
use std::path::Path;
use std::sync::Arc;

// To enable flamegraph output
// cargo bench --bench hashops -- --profile-time=N
enum FlamegraphProfiler {
    Init(c_int),
    Active(ProfilerGuard<'static>),
}

fn file_error_panic<T, U>(path: &Path) -> impl FnOnce(T) -> U {
    |_| panic!("Error on file `{}`", path.display())
}

impl Profiler for FlamegraphProfiler {
    #[expect(clippy::unwrap_used)]
    fn start_profiling(&mut self, _benchmark_id: &str, _benchmark_dir: &Path) {
        if let Self::Init(frequency) = self {
            let guard = ProfilerGuard::new(*frequency).unwrap();
            *self = Self::Active(guard);
        }
    }

    #[expect(clippy::unwrap_used)]
    fn stop_profiling(&mut self, _benchmark_id: &str, benchmark_dir: &Path) {
        std::fs::create_dir_all(benchmark_dir).unwrap();
        let filename = "firewood-flamegraph.svg";
        let flamegraph_path = benchmark_dir.join(filename);
        let flamegraph_file =
            File::create(&flamegraph_path).unwrap_or_else(file_error_panic(&flamegraph_path));

        #[expect(clippy::unwrap_used)]
        if let Self::Active(profiler) = self {
            profiler
                .report()
                .build()
                .unwrap()
                .flamegraph(flamegraph_file)
                .unwrap_or_else(file_error_panic(&flamegraph_path));
        }
    }
}

// This benchmark peeks into the merkle layer and times how long it takes
// to insert NKEYS with a key length of KEYSIZE
fn bench_merkle<const NKEYS: usize, const KEYSIZE: usize>(criterion: &mut Criterion) {
    let rng = &firewood_storage::SeededRng::from_option(Some(1234));

    criterion
        .benchmark_group("Merkle")
        .sample_size(30)
        .bench_function("insert", |b| {
            b.iter_batched(
                || {
                    let store = Arc::new(MemStore::new(vec![]));
                    let nodestore = NodeStore::new_empty_proposal(store);
                    let merkle = Merkle::from(nodestore);

                    let keys: Vec<Vec<u8>> =
                        repeat_with(|| rng.sample_iter(&Alphanumeric).take(KEYSIZE).collect())
                            .take(NKEYS)
                            .collect();

                    (merkle, keys)
                },
                #[expect(clippy::unwrap_used)]
                |(mut merkle, keys)| {
                    for key in keys {
                        merkle.insert(&key, Box::new(*b"v")).unwrap();
                    }
                    let _frozen = merkle.hash();
                },
                BatchSize::SmallInput,
            );
        });
}

#[expect(clippy::unwrap_used)]
fn bench_db<const N: usize>(criterion: &mut Criterion) {
    const KEY_LEN: usize = 4;
    let rng = &firewood_storage::SeededRng::from_option(Some(1234));

    criterion
        .benchmark_group("Db")
        .sample_size(30)
        .bench_function("commit", |b| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter_batched(
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
                    |batch_ops| async {
                        let db_path = std::env::temp_dir();
                        let db_path = db_path.join("benchmark_db");
                        let cfg = DbConfig::builder();

                        let db = firewood::db::Db::new(db_path, cfg.clone().truncate(true).build())
                            .unwrap();

                        db.propose(batch_ops).unwrap().commit().unwrap();
                    },
                    BatchSize::SmallInput,
                );
        });
}

criterion_group! {
    name = benches;
    config = Criterion::default().with_profiler(FlamegraphProfiler::Init(100));
    targets = bench_merkle::<3, 4>, bench_merkle<3, 32>, bench_db::<100>
}

criterion_main!(benches);
