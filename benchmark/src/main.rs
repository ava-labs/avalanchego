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

use clap::Parser;
use firewood::logger::{debug, trace};
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
    #[arg(short, long, default_value_t = 100000)]
    number_of_batches: u64,
    #[arg(short = 'p', long, default_value_t = 0, value_parser = clap::value_parser!(u16).range(0..=100))]
    read_verify_percent: u16,
    #[arg(
        short,
        long,
        help = "Only initialize the database, do not do the insert/delete/update loop"
    )]
    initialize_only: bool,
    #[arg(short, long, default_value_t = NonZeroUsize::new(1500000).expect("is non-zero"))]
    cache_size: NonZeroUsize,
    #[arg(short, long, default_value_t = 128)]
    revisions: usize,
    #[arg(short = 'l', long, default_value_t = 3000)]
    prometheus_port: u16,
    #[arg(short, long)]
    test_name: TestName,
}

#[derive(clap::ValueEnum, Clone, Debug)]
enum TestName {
    Create,
    TenKRandom,
}

trait TestRunner {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>>;
    fn generate_updates(
        start: u64,
        count: u64,
        low: u64,
    ) -> impl Iterator<Item = BatchOp<Vec<u8>, Vec<u8>>> {
        let hash_of_low = Sha256::digest(low.to_ne_bytes()).to_vec();
        (start..start + count)
            .map(|inner_key| {
                let digest = Sha256::digest(inner_key.to_ne_bytes()).to_vec();
                debug!(
                    "updating {:?} with digest {} to {}",
                    inner_key,
                    hex::encode(&digest),
                    hex::encode(&hash_of_low)
                );
                (digest, hash_of_low.clone())
            })
            .map(|(key, value)| BatchOp::Put { key, value })
            .collect::<Vec<_>>()
            .into_iter()
    }
    fn generate_deletes(start: u64, count: u64) -> impl Iterator<Item = BatchOp<Vec<u8>, Vec<u8>>> {
        (start..start + count)
            .map(|key| {
                let digest = Sha256::digest(key.to_ne_bytes()).to_vec();
                debug!("deleting {:?} with digest {}", key, hex::encode(&digest));
                #[allow(clippy::let_and_return)]
                digest
            })
            .map(|key| BatchOp::Delete { key })
            .collect::<Vec<_>>()
            .into_iter()
    }
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

mod create;
mod tenkrandom;

#[global_allocator]
static GLOBAL: tikv_jemallocator::Jemalloc = tikv_jemallocator::Jemalloc;

#[tokio::main(flavor = "multi_thread")]
async fn main() -> Result<(), Box<dyn Error>> {
    let args = Args::parse();

    env_logger::init();

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
    }
    Ok(())
}

