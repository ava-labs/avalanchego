// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// hash benchmarks; run with 'cargo bench'

use criterion::{criterion_group, criterion_main, profiler::Profiler, BatchSize, Criterion};
use firewood::merkle::Merkle;
use pprof::ProfilerGuard;
use rand::{distributions::Alphanumeric, rngs::StdRng, Rng, SeedableRng};
use std::sync::Arc;
use std::{fs::File, iter::repeat_with, os::raw::c_int, path::Path};
use storage::{MemStore, NodeStore};

// To enable flamegraph output
// cargo bench --bench shale-bench -- --profile-time=N
enum FlamegraphProfiler {
    Init(c_int),
    Active(ProfilerGuard<'static>),
}

fn file_error_panic<T, U>(path: &Path) -> impl FnOnce(T) -> U + '_ {
    |_| panic!("Error on file `{}`", path.display())
}

impl Profiler for FlamegraphProfiler {
    #[allow(clippy::unwrap_used)]
    fn start_profiling(&mut self, _benchmark_id: &str, _benchmark_dir: &Path) {
        if let Self::Init(frequency) = self {
            let guard = ProfilerGuard::new(*frequency).unwrap();
            *self = Self::Active(guard);
        }
    }

    #[allow(clippy::unwrap_used)]
    fn stop_profiling(&mut self, _benchmark_id: &str, benchmark_dir: &Path) {
        std::fs::create_dir_all(benchmark_dir).unwrap();
        let filename = "firewood-flamegraph.svg";
        let flamegraph_path = benchmark_dir.join(filename);
        #[allow(clippy::unwrap_used)]
        let flamegraph_file =
            File::create(&flamegraph_path).unwrap_or_else(file_error_panic(&flamegraph_path));

        #[allow(clippy::unwrap_used)]
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

// TODO danlaine use or remove
// fn bench_trie_hash(criterion: &mut Criterion) {
//     let mut to = [1u8; TRIE_HASH_LEN];
//     let mut store = InMemLinearStore::new(TRIE_HASH_LEN as u64, 0u8);
//     store.write(0, &*ZERO_HASH).expect("write should succeed");

//     #[allow(clippy::unwrap_used)]
//     criterion
//         .benchmark_group("TrieHash")
//         .bench_function("dehydrate", |b| {
//             b.iter(|| ZERO_HASH.serialize(&mut to).unwrap());
//         })
//         .bench_function("hydrate", |b| {
//             b.iter(|| TrieHash::deserialize(0, &store).unwrap());
//         });
// }

// This benchmark peeks into the merkle layer and times how long it takes
// to insert NKEYS with a key length of KEYSIZE
#[allow(clippy::unwrap_used)]
fn bench_merkle<const NKEYS: usize, const KEYSIZE: usize>(criterion: &mut Criterion) {
    let mut rng = StdRng::seed_from_u64(1234);

    criterion
        .benchmark_group("Merkle")
        .sample_size(30)
        .bench_function("insert", |b| {
            b.iter_batched(
                || {
                    let store = Arc::new(MemStore::new(vec![]));
                    let nodestore = NodeStore::new_empty_proposal(store);
                    let merkle = Merkle::from(nodestore);

                    let keys: Vec<Vec<u8>> = repeat_with(|| {
                        (&mut rng)
                            .sample_iter(&Alphanumeric)
                            .take(KEYSIZE)
                            .collect()
                    })
                    .take(NKEYS)
                    .collect();

                    (merkle, keys)
                },
                #[allow(clippy::unwrap_used)]
                |(mut merkle, keys)| {
                    keys.into_iter()
                        .for_each(|key| merkle.insert(&key, Box::new(*b"v")).unwrap());
                    let _frozen = merkle.hash();
                },
                BatchSize::SmallInput,
            );
        });
}

// This bechmark does the same thing as bench_merkle except it uses the revision manager
// TODO: Enable again once the revision manager is stable
// fn _bench_db<const N: usize>(criterion: &mut Criterion) {
//     const KEY_LEN: usize = 4;
//     let mut rng = StdRng::seed_from_u64(1234);

//     #[allow(clippy::unwrap_used)]
//     criterion
//         .benchmark_group("Db")
//         .sample_size(30)
//         .bench_function("commit", |b| {
//             b.to_async(tokio::runtime::Runtime::new().unwrap())
//                 .iter_batched(
//                     || {
//                         let batch_ops: Vec<_> = repeat_with(|| {
//                             (&mut rng)
//                                 .sample_iter(&Alphanumeric)
//                                 .take(KEY_LEN)
//                                 .collect()
//                         })
//                         .map(|key: Vec<_>| BatchOp::Put {
//                             key,
//                             value: vec![b'v'],
//                         })
//                         .take(N)
//                         .collect();
//                         batch_ops
//                     },
//                     |batch_ops| async {
//                         let db_path = std::env::temp_dir();
//                         let db_path = db_path.join("benchmark_db");
//                         let cfg = DbConfig::builder();

//                         #[allow(clippy::unwrap_used)]
//                         let db = firewood::db::Db::new(db_path, cfg.clone().truncate(true).build())
//                             .await
//                             .unwrap();

//                         #[allow(clippy::unwrap_used)]
//                         db.propose(batch_ops).await.unwrap().commit().await.unwrap()
//                     },
//                     BatchSize::SmallInput,
//                 );
//         });
// }

criterion_group! {
    name = benches;
    config = Criterion::default().with_profiler(FlamegraphProfiler::Init(100));
    // targets = bench_trie_hash, bench_merkle::<3, 32>, bench_db::<100>
    targets = bench_merkle::<3, 4>, bench_merkle<3, 32>
}

criterion_main!(benches);
