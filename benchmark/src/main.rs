// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// The idea behind this benchmark:
// Phase 1: (setup) Generate known keys from the SHA256 of the row number, starting at 0, for 1B keys
// Phase 2: (steady-state) Continuously insert, delete, and update keys in the database

// Phase 2 consists of:
// 1. 25% of batch size is inserting more rows like phase 1
// 2. 25% of batch size is deleting rows from the beginning
// 3. 50% of batch size is updating rows in the middle, but setting the value to the hash of the first row inserted
//

use clap::{Parser, Subcommand};
use firewood::logger::trace;
use log::LevelFilter;
use metrics_exporter_prometheus::PrometheusBuilder;
use metrics_util::MetricKindMask;
use sha2::{Digest, Sha256};
use std::error::Error;
use std::net::{Ipv4Addr, SocketAddr};
use std::num::NonZeroUsize;
use std::time::Duration;

use firewood::db::{BatchOp, Db, DbConfig};
use firewood::manager::RevisionManagerConfig;

#[derive(Parser, Debug)]
struct Args {
    #[arg(short, long, default_value_t = 10000)]
    batch_size: u64,
    #[arg(short, long, default_value_t = 1000)]
    number_of_batches: u64,
    #[arg(short, long, default_value_t = NonZeroUsize::new(1500000).expect("is non-zero"))]
    cache_size: NonZeroUsize,
    #[arg(short, long, default_value_t = 128)]
    revisions: usize,
    #[arg(short = 'p', long, default_value_t = 3000)]
    prometheus_port: u16,

    #[clap(flatten)]
    global_opts: GlobalOpts,

    #[clap(subcommand)]
    test_name: TestName,
}

#[derive(clap::Args, Debug)]
struct GlobalOpts {
    #[arg(
        long,
        short = 'l',
        required = false,
        help = "Log level. Respects RUST_LOG.",
        value_name = "LOG_LEVEL",
        num_args = 1,
        value_parser = ["debug", "info"],
        default_value_t = String::from("info"),
    )]
    log_level: String,
}

mod create;
mod single;
mod tenkrandom;
mod zipf;

#[derive(Debug, Subcommand)]
enum TestName {
    Create,
    TenKRandom,
    Zipf(zipf::Args),
    Single,
}

trait TestRunner {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>>;

    fn generate_inserts(start: u64, count: u64) -> impl Iterator<Item = BatchOp<Vec<u8>, Vec<u8>>> {
        (start..start + count)
            .map(|inner_key| {
                let digest = Sha256::digest(inner_key.to_ne_bytes()).to_vec();
                trace!(
                    "inserting {:?} with digest {}",
                    inner_key,
                    hex::encode(&digest),
                );
                (digest.clone(), digest)
            })
            .map(|(key, value)| BatchOp::Put { key, value })
            .collect::<Vec<_>>()
            .into_iter()
    }
}

#[global_allocator]
static GLOBAL: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

#[tokio::main(flavor = "multi_thread")]
async fn main() -> Result<(), Box<dyn Error>> {
    let args = Args::parse();

    env_logger::Builder::new()
        .filter_level(match args.global_opts.log_level.as_str() {
            "debug" => LevelFilter::Debug,
            "info" => LevelFilter::Info,
            _ => LevelFilter::Info,
        })
        .init();

    let builder = PrometheusBuilder::new();
    builder
        .with_http_listener(SocketAddr::new(
            Ipv4Addr::UNSPECIFIED.into(),
            args.prometheus_port,
        ))
        .idle_timeout(
            MetricKindMask::COUNTER | MetricKindMask::HISTOGRAM,
            Some(Duration::from_secs(10)),
        )
        .install()
        .expect("unable in run prometheusbuilder");

    let mgrcfg = RevisionManagerConfig::builder()
        .node_cache_size(args.cache_size)
        .free_list_cache_size(
            NonZeroUsize::new(2 * args.batch_size as usize).expect("batch size > 0"),
        )
        .max_revisions(args.revisions)
        .build();
    let cfg = DbConfig::builder()
        .truncate(matches!(args.test_name, TestName::Create))
        .manager(mgrcfg)
        .build();

    let db = Db::new("rev_db", cfg)
        .await
        .expect("db initiation should succeed");

    match args.test_name {
        TestName::Create => {
            let runner = create::Create;
            runner.run(&db, &args).await?;
        }
        TestName::TenKRandom => {
            let runner = tenkrandom::TenKRandom;
            runner.run(&db, &args).await?;
        }
        TestName::Zipf(_) => {
            let runner = zipf::Zipf;
            runner.run(&db, &args).await?;
        }
        TestName::Single => {
            let runner = single::Single;
            runner.run(&db, &args).await?;
        }
    }
    Ok(())
}
