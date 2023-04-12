use clap::Parser;
use criterion::Criterion;
use firewood::db::{DBConfig, WALConfig, DB};

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
struct Args {
    #[arg(long)]
    nbatch: usize,
    #[arg(short, long)]
    batch_size: usize,
    #[arg(short, long, default_value_t = 0)]
    seed: u64,
    #[arg(short, long, default_value_t = false)]
    no_root_hash: bool,
}

fn main() {
    let args = Args::parse();

    let cfg = DBConfig::builder().wal(WALConfig::builder().max_revisions(10).build());
    {
        use rand::{Rng, SeedableRng};
        let mut c = Criterion::default();
        let mut group = c.benchmark_group("insert".to_string());
        let mut rng = rand::rngs::StdRng::seed_from_u64(args.seed);
        let nbatch = args.nbatch;
        let batch_size = args.batch_size;
        let total = nbatch * batch_size;
        let root_hash = !args.no_root_hash;
        let mut workload = Vec::new();
        for _ in 0..nbatch {
            let mut batch: Vec<(Vec<_>, Vec<_>)> = Vec::new();
            for _ in 0..batch_size {
                batch.push((rng.gen::<[u8; 32]>().into(), rng.gen::<[u8; 32]>().into()));
            }
            workload.push(batch);
        }
        println!("workload prepared");
        group.sampling_mode(criterion::SamplingMode::Flat).sample_size(10);
        group.throughput(criterion::Throughput::Elements(total as u64));
        group.bench_with_input(
            format!("nbatch={nbatch} batch_size={batch_size}"),
            &workload,
            |b, workload| {
                b.iter(|| {
                    let db = DB::new("benchmark_db", &cfg.clone().truncate(true).build()).unwrap();
                    for batch in workload.iter() {
                        let mut wb = db.new_writebatch();
                        for (k, v) in batch {
                            wb = wb.kv_insert(k, v.clone()).unwrap();
                        }
                        if !root_hash {
                            wb = wb.no_root_hash();
                        }
                        wb.commit();
                    }
                })
            },
        );
    }
}
