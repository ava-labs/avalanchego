extern crate firewood_shale as shale;

use criterion::{
    black_box, criterion_group, criterion_main, profiler::Profiler, Bencher, Criterion,
};
use pprof::ProfilerGuard;
use rand::Rng;
use shale::{
    cached::{DynamicMem, PlainMem},
    compact::CompactSpaceHeader,
    disk_address::DiskAddress,
    CachedStore, Obj, StoredView,
};
use std::{fs::File, os::raw::c_int, path::Path};

const BENCH_MEM_SIZE: usize = 2_000_000;

// To enable flamegraph output
// cargo bench --bench shale-bench -- --profile-time=N
pub struct FlamegraphProfiler<'a> {
    frequency: c_int,
    active_profiler: Option<ProfilerGuard<'a>>,
}

impl<'a> FlamegraphProfiler<'a> {
    #[allow(dead_code)]
    pub fn new(frequency: c_int) -> Self {
        FlamegraphProfiler {
            frequency,
            active_profiler: None,
        }
    }
}

impl<'a> Profiler for FlamegraphProfiler<'a> {
    fn start_profiling(&mut self, _benchmark_id: &str, _benchmark_dir: &Path) {
        self.active_profiler = Some(ProfilerGuard::new(self.frequency).unwrap());
    }

    fn stop_profiling(&mut self, _benchmark_id: &str, benchmark_dir: &Path) {
        std::fs::create_dir_all(benchmark_dir).unwrap();
        let flamegraph_path = benchmark_dir.join("flamegraph.svg");
        let flamegraph_file =
            File::create(flamegraph_path).expect("File system error while creating flamegraph.svg");
        if let Some(profiler) = self.active_profiler.take() {
            profiler
                .report()
                .build()
                .unwrap()
                .flamegraph(flamegraph_file)
                .expect("Error writing flamegraph");
        }
    }
}

fn get_view<C: CachedStore>(b: &mut Bencher, mut cached: C) {
    let mut rng = rand::thread_rng();

    b.iter(|| {
        let len = rng.gen_range(0..26);
        let rdata = black_box(&"abcdefghijklmnopqrstuvwxyz".as_bytes()[..len]);

        let offset = rng.gen_range(0..BENCH_MEM_SIZE - len);

        cached.write(offset, rdata);
        let view = cached
            .get_view(offset, rdata.len().try_into().unwrap())
            .unwrap();

        serialize(&cached);
        assert_eq!(view.as_deref(), rdata);
    });
}

fn serialize<T: CachedStore>(m: &T) {
    let compact_header_obj: DiskAddress = DiskAddress::from(0x0);
    let _: Obj<CompactSpaceHeader> =
        StoredView::ptr_to_obj(m, compact_header_obj, shale::compact::CompactHeader::MSIZE)
            .unwrap();
}

fn bench_cursors(c: &mut Criterion) {
    let mut group = c.benchmark_group("shale-bench");
    group.bench_function("PlainMem", |b| {
        let mem = PlainMem::new(BENCH_MEM_SIZE as u64, 0);
        get_view(b, mem)
    });
    group.bench_function("DynamicMem", |b| {
        let mem = DynamicMem::new(BENCH_MEM_SIZE as u64, 0);
        get_view(b, mem)
    });
}

criterion_group! {
    name = benches;
    config = Criterion::default().with_profiler(FlamegraphProfiler::new(100));
    targets = bench_cursors
}

criterion_main!(benches);
