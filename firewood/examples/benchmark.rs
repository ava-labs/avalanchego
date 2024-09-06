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
use metrics_exporter_prometheus::PrometheusBuilder;
use metrics_util::MetricKindMask;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::error::Error;
use std::net::{Ipv4Addr, SocketAddr};
use std::num::NonZeroUsize;
use std::ops::RangeInclusive;
use std::time::{Duration, Instant};

use firewood::db::{Batch, BatchOp, Db, DbConfig};
use firewood::manager::RevisionManagerConfig;
use firewood::v2::api::{Db as _, DbView, Proposal as _};
use rand::Rng;

#[derive(Parser, Debug)]
struct Args {
    #[arg(short, long, default_value = "32", value_parser = string_to_range)]
    valuelen: RangeInclusive<usize>,
    #[arg(short, long, default_value_t = 1)]
    batch_size: u64,
    #[arg(short, long, default_value_t = 100)]
    number_of_batches: u64,
    #[arg(short = 'p', long, default_value_t = 0, value_parser = clap::value_parser!(u16).range(0..=100))]
    read_verify_percent: u16,
    #[arg(
        short,
        long,
        help = "Only initialize the database, do not do the insert/delete/update loop"
    )]
    initialize_only: bool,
    #[arg(short, long, default_value_t = NonZeroUsize::new(20480).expect("is non-zero"))]
    cache_size: NonZeroUsize,
    #[arg(short, long)]
    assume_preloaded_rows: Option<u64>,
    #[arg(short, long, default_value_t = 128)]
    revisions: usize,
    #[arg(short = 'l', long, default_value_t = 3000)]
    prometheus_port: u16,
}

fn string_to_range(input: &str) -> Result<RangeInclusive<usize>, Box<dyn Error + Sync + Send>> {
    //<usize as std::str::FromStr>::Err> {
    let parts: Vec<&str> = input.split('-').collect();
    #[allow(clippy::indexing_slicing)]
    match parts.len() {
        1 => Ok(input.parse()?..=input.parse()?),
        2 => Ok(parts[0].parse()?..=parts[1].parse()?),
        _ => Err("Too many dashes in input string".into()),
    }
}

/// cargo run --release --example insert
#[tokio::main(flavor = "multi_thread")]
async fn main() -> Result<(), Box<dyn Error>> {
    let args = Args::parse();

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
        .max_revisions(args.revisions)
        .build();
    let cfg = DbConfig::builder()
        .truncate(args.assume_preloaded_rows.is_none())
        .manager(mgrcfg)
        .build();

    let db = Db::new("rev_db", cfg)
        .await
        .expect("db initiation should succeed");

    let keys = args.batch_size;
    let start = Instant::now();

    if args.assume_preloaded_rows.is_none() {
        for key in 0..args.number_of_batches {
            let batch = generate_inserts(key * keys, args.batch_size).collect();

            let verify = get_keys_to_verify(&batch, args.read_verify_percent);

            let proposal = db.propose(batch).await.expect("proposal should succeed");
            proposal.commit().await?;
            verify_keys(&db, verify).await?;
        }

        let duration = start.elapsed();
        println!(
            "Generated and inserted {} batches of size {keys} in {duration:?}",
            args.number_of_batches
        );
    }

    let current_hash = db.root_hash().await?.expect("root hash should exist");

    if args.initialize_only {
        println!("Completed initialization with hash of {:?}", current_hash);

        return Ok(());
    }

    // batches consist of
    // 1. 25% deletes from low
    // 2. 25% new insertions from high
    // 3. 50% updates from the middle

    println!(
        "Starting inner loop with database hash of {:?}",
        current_hash
    );

    let mut low = 0;
    let mut high = args
        .assume_preloaded_rows
        .unwrap_or(args.number_of_batches * args.batch_size);
    let twenty_five_pct = args.batch_size / 4;

    loop {
        let batch: Vec<BatchOp<_, _>> = generate_inserts(high, twenty_five_pct)
            .chain(generate_deletes(low, twenty_five_pct))
            .chain(generate_updates(low + high / 2, twenty_five_pct * 2, low))
            .collect();
        let proposal = db.propose(batch).await.expect("proposal should succeed");
        proposal.commit().await?;
        low += twenty_five_pct;
        high += twenty_five_pct;
    }
}

fn generate_inserts(start: u64, count: u64) -> impl Iterator<Item = BatchOp<Vec<u8>, Vec<u8>>> {
    (start..start + count)
        .map(|inner_key| {
            let digest = Sha256::digest(inner_key.to_ne_bytes()).to_vec();
            (digest.clone(), digest)
        })
        .map(|(key, value)| BatchOp::Put { key, value })
        .collect::<Vec<_>>()
        .into_iter()
}

fn generate_deletes(start: u64, count: u64) -> impl Iterator<Item = BatchOp<Vec<u8>, Vec<u8>>> {
    (start..start + count)
        .map(|key| Sha256::digest(key.to_ne_bytes()).to_vec())
        .map(|key| BatchOp::Delete { key })
        .collect::<Vec<_>>()
        .into_iter()
}

fn generate_updates(
    start: u64,
    count: u64,
    low: u64,
) -> impl Iterator<Item = BatchOp<Vec<u8>, Vec<u8>>> {
    let hash_of_low = Sha256::digest(low.to_ne_bytes()).to_vec();
    (start..start + count)
        .map(|inner_key| {
            let digest = Sha256::digest(inner_key.to_ne_bytes()).to_vec();
            (digest, hash_of_low.clone())
        })
        .map(|(key, value)| BatchOp::Put { key, value })
        .collect::<Vec<_>>()
        .into_iter()
}

fn get_keys_to_verify(batch: &Batch<Vec<u8>, Vec<u8>>, pct: u16) -> HashMap<Vec<u8>, Box<[u8]>> {
    if pct == 0 {
        HashMap::new()
    } else {
        batch
            .iter()
            .filter(|_last_key| rand::thread_rng().gen_range(0..=(100 - pct)) == 0)
            .map(|op| {
                if let BatchOp::Put { key, value } = op {
                    (key.clone(), value.clone().into_boxed_slice())
                } else {
                    unreachable!()
                }
            })
            .collect()
    }
}

async fn verify_keys(
    db: &impl firewood::v2::api::Db,
    verify: HashMap<Vec<u8>, Box<[u8]>>,
) -> Result<(), firewood::v2::api::Error> {
    if !verify.is_empty() {
        let hash = db.root_hash().await?.expect("root hash should exist");
        let revision = db.revision(hash).await?;
        for (key, value) in verify {
            assert_eq!(Some(value), revision.val(key).await?);
        }
    }
    Ok(())
}
