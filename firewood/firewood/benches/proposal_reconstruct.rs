// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use criterion::profiler::Profiler;
use criterion::{BatchSize, Criterion, criterion_group, criterion_main};
use firewood::api::{Db as _, DbView as _, Proposal as _, Reconstructible as _};
use firewood::db::{BatchOp, Db, DbConfig, UseParallel};
use firewood_storage::NodeHashAlgorithm;
use pprof::ProfilerGuard;
use rand::{RngExt, distr::Alphanumeric};
use std::fs::File;
use std::hint::black_box;
use std::iter::repeat_with;
use std::os::raw::c_int;
use std::path::Path;
use tempfile::TempDir;

const INITIAL_ITEMS: usize = 100;
const PROPOSAL_COUNT: usize = 2_048;
const PROPOSAL_ITEMS: usize = 100;
const KEY_LEN: usize = 16;
const VALUE_LEN: usize = 32;

type BenchOp = BatchOp<Vec<u8>, Vec<u8>>;
type BenchBatch = Vec<BenchOp>;

// To enable flamegraph output:
// cargo bench --bench proposal_reconstruct -- reconstructed_chain/nested --profile-time=5
enum FlamegraphProfiler {
    Init(c_int),
    Active(c_int, ProfilerGuard<'static>),
}

fn file_error_panic<T, U>(path: &Path) -> impl FnOnce(T) -> U {
    |_| panic!("Error on file `{}`", path.display())
}

impl Profiler for FlamegraphProfiler {
    #[expect(clippy::unwrap_used)]
    fn start_profiling(&mut self, _benchmark_id: &str, _benchmark_dir: &Path) {
        if let Self::Init(frequency) = self {
            let guard = ProfilerGuard::new(*frequency).unwrap();
            *self = Self::Active(*frequency, guard);
        }
    }

    #[expect(clippy::unwrap_used)]
    fn stop_profiling(&mut self, _benchmark_id: &str, benchmark_dir: &Path) {
        std::fs::create_dir_all(benchmark_dir).unwrap();
        let filename = "firewood-flamegraph.svg";
        let flamegraph_path = benchmark_dir.join(filename);
        let flamegraph_file =
            File::create(&flamegraph_path).unwrap_or_else(file_error_panic(&flamegraph_path));

        if let Self::Active(frequency, profiler) = self {
            profiler
                .report()
                .build()
                .unwrap()
                .flamegraph(flamegraph_file)
                .unwrap_or_else(file_error_panic(&flamegraph_path));
            *self = Self::Init(*frequency);
        }
    }
}

fn make_batch(rng: &firewood_storage::SeededRng, count: usize) -> BenchBatch {
    repeat_with(|| {
        let key: Vec<u8> = rng.sample_iter(&Alphanumeric).take(KEY_LEN).collect();
        let value: Vec<u8> = rng.sample_iter(&Alphanumeric).take(VALUE_LEN).collect();
        BatchOp::Put { key, value }
    })
    .take(count)
    .collect()
}

fn generate_batches() -> (BenchBatch, Vec<BenchBatch>) {
    let rng = &firewood_storage::SeededRng::from_option(Some(1234));
    let initial = make_batch(rng, INITIAL_ITEMS);
    let batches = (0..PROPOSAL_COUNT)
        .map(|_| make_batch(rng, PROPOSAL_ITEMS))
        .collect();
    (initial, batches)
}

#[expect(clippy::unwrap_used)]
fn bench_proposal_chain(criterion: &mut Criterion) {
    criterion
        .benchmark_group("proposal_chain")
        .sample_size(10)
        .warm_up_time(std::time::Duration::from_secs(5))
        .bench_function("nested", |b| {
            b.iter_batched(
                generate_batches,
                |(initial, batches)| {
                    let db_dir = TempDir::new().unwrap();
                    let db_path = db_dir.path().join("benchmark_db");
                    let cfg = DbConfig::builder()
                        .node_hash_algorithm(NodeHashAlgorithm::compile_option())
                        .truncate(true)
                        .build();
                    let db = Db::new(db_path, cfg).unwrap();

                    db.propose(initial).unwrap().commit().unwrap();

                    let mut batches_iter = batches.into_iter();
                    let first_batch = batches_iter.next().unwrap();
                    let mut proposal = db.propose(first_batch).unwrap();

                    for batch in batches_iter {
                        proposal = proposal.propose(batch).unwrap();
                    }

                    // Calculate the hash once at the end to ensure all work is actually done
                    black_box(proposal.root_hash());
                },
                BatchSize::SmallInput,
            );
        });
}

#[expect(clippy::unwrap_used)]
fn bench_reconstructed_chain(criterion: &mut Criterion) {
    criterion
        .benchmark_group("reconstructed_chain")
        .sample_size(10)
        .warm_up_time(std::time::Duration::from_secs(5))
        .bench_function("nested", |b| {
            b.iter_batched(
                generate_batches,
                |(initial, batches)| {
                    let db_dir = TempDir::new().unwrap();
                    let db_path = db_dir.path().join("benchmark_db");
                    // build with UseParallel::Never because we don't want the
                    // extra threads muddying up any flamegraphs when creating the
                    // initial proposal
                    let cfg = DbConfig::builder()
                        .node_hash_algorithm(NodeHashAlgorithm::compile_option())
                        .truncate(true)
                        .use_parallel(UseParallel::Never)
                        .build();
                    let db = Db::new(db_path, cfg).unwrap();

                    db.propose(initial).unwrap().commit().unwrap();

                    let mut batches_iter = batches.into_iter();
                    let first_batch = batches_iter.next().unwrap();
                    db.propose(first_batch).unwrap().commit().unwrap();
                    let root_hash = db.root_hash().unwrap();
                    let historical = db.committed_view(root_hash).unwrap();

                    let second_batch = batches_iter.next().unwrap();
                    let mut reconstructed =
                        db.reconstruct_from_view(&historical, second_batch).unwrap();

                    for batch in batches_iter {
                        reconstructed = reconstructed.reconstruct(batch).unwrap();
                    }

                    // Calculate the hash once at the end to ensure all work is actually done
                    black_box(reconstructed.root_hash());
                },
                BatchSize::SmallInput,
            );
        });
}

criterion_group! {
    name = benches;
    config = Criterion::default()
        .with_profiler(FlamegraphProfiler::Init(100))
        .measurement_time(std::time::Duration::from_secs(30))
        .warm_up_time(std::time::Duration::from_secs(5));
    targets = bench_proposal_chain, bench_reconstructed_chain
}

criterion_main!(benches);
