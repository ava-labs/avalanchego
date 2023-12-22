// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// hash benchmarks; run with 'cargo bench'

use criterion::{criterion_group, criterion_main, profiler::Profiler, BatchSize, Criterion};
use firewood::{
    db::{BatchOp, DbConfig},
    merkle::{MerkleWithEncoder, TrieHash, TRIE_HASH_LEN},
    shale::{
        cached::PlainMem,
        compact::{CompactHeader, CompactSpace},
        disk_address::DiskAddress,
        CachedStore, ObjCache, Storable, StoredView,
    },
    storage::WalConfig,
    v2::api::{Db, Proposal},
};
use pprof::ProfilerGuard;
use rand::{distributions::Alphanumeric, rngs::StdRng, Rng, SeedableRng};
use std::{fs::File, iter::repeat_with, ops::Deref, os::raw::c_int, path::Path, sync::Arc};

const ZERO_HASH: TrieHash = TrieHash([0u8; TRIE_HASH_LEN]);

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
    fn start_profiling(&mut self, _benchmark_id: &str, _benchmark_dir: &Path) {
        if let Self::Init(frequency) = self {
            let guard = ProfilerGuard::new(*frequency).unwrap();
            *self = Self::Active(guard);
        }
    }

    fn stop_profiling(&mut self, _benchmark_id: &str, benchmark_dir: &Path) {
        std::fs::create_dir_all(benchmark_dir).unwrap();
        let filename = "firewood-flamegraph.svg";
        let flamegraph_path = benchmark_dir.join(filename);
        let flamegraph_file =
            File::create(&flamegraph_path).unwrap_or_else(file_error_panic(&flamegraph_path));

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

fn bench_trie_hash(criterion: &mut Criterion) {
    let mut to = [1u8; TRIE_HASH_LEN];
    let mut store = PlainMem::new(TRIE_HASH_LEN as u64, 0u8);
    store.write(0, ZERO_HASH.deref());

    criterion
        .benchmark_group("TrieHash")
        .bench_function("dehydrate", |b| {
            b.iter(|| ZERO_HASH.serialize(&mut to).unwrap());
        })
        .bench_function("hydrate", |b| {
            b.iter(|| TrieHash::deserialize(0, &store).unwrap());
        });
}

fn bench_merkle<const N: usize>(criterion: &mut Criterion) {
    const TEST_MEM_SIZE: u64 = 20_000_000;
    const KEY_LEN: usize = 4;
    let mut rng = StdRng::seed_from_u64(1234);

    criterion
        .benchmark_group("Merkle")
        .sample_size(30)
        .bench_function("insert", |b| {
            b.iter_batched(
                || {
                    let merkle_payload_header = DiskAddress::from(0);

                    let merkle_payload_header_ref = StoredView::ptr_to_obj(
                        &PlainMem::new(2 * CompactHeader::MSIZE, 9),
                        merkle_payload_header,
                        CompactHeader::MSIZE,
                    )
                    .unwrap();

                    let store = CompactSpace::new(
                        PlainMem::new(TEST_MEM_SIZE, 0),
                        PlainMem::new(TEST_MEM_SIZE, 1),
                        merkle_payload_header_ref,
                        ObjCache::new(1 << 20),
                        4096,
                        4096,
                    )
                    .unwrap();

                    let merkle = MerkleWithEncoder::new(Box::new(store));
                    let root = merkle.init_root().unwrap();

                    let keys: Vec<Vec<u8>> = repeat_with(|| {
                        (&mut rng)
                            .sample_iter(&Alphanumeric)
                            .take(KEY_LEN)
                            .collect()
                    })
                    .take(N)
                    .collect();

                    (merkle, root, keys)
                },
                |(mut merkle, root, keys)| {
                    keys.into_iter()
                        .for_each(|key| merkle.insert(key, vec![b'v'], root).unwrap())
                },
                BatchSize::SmallInput,
            );
        });
}

fn bench_db<const N: usize>(criterion: &mut Criterion) {
    const KEY_LEN: usize = 4;
    let mut rng = StdRng::seed_from_u64(1234);

    criterion
        .benchmark_group("Db")
        .sample_size(30)
        .bench_function("commit", |b| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter_batched(
                    || {
                        let batch_ops: Vec<_> = repeat_with(|| {
                            (&mut rng)
                                .sample_iter(&Alphanumeric)
                                .take(KEY_LEN)
                                .collect()
                        })
                        .map(|key: Vec<_>| BatchOp::Put {
                            key,
                            value: vec![b'v'],
                        })
                        .take(N)
                        .collect();
                        batch_ops
                    },
                    |batch_ops| async {
                        let db_path = dbg!(std::env::temp_dir());
                        let db_path = db_path.join("benchmark_db");
                        let cfg =
                            DbConfig::builder().wal(WalConfig::builder().max_revisions(10).build());

                        let db =
                            firewood::db::Db::new(db_path, &cfg.clone().truncate(true).build())
                                .await
                                .unwrap();

                        Arc::new(db.propose(batch_ops).await.unwrap())
                            .commit()
                            .await
                            .unwrap()
                    },
                    BatchSize::SmallInput,
                );
        });
}

criterion_group! {
    name = benches;
    config = Criterion::default().with_profiler(FlamegraphProfiler::Init(100));
    targets = bench_trie_hash, bench_merkle::<3>, bench_db::<100>
}

criterion_main!(benches);
