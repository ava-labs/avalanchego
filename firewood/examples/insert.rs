// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// This example isn't an actual benchmark, it's just an example of how to
// insert some random keys using the front-end API.

use clap::Parser;
use std::borrow::BorrowMut as _;
use std::collections::HashMap;
use std::error::Error;
use std::num::NonZeroUsize;
use std::ops::RangeInclusive;
use std::time::Instant;

use firewood::db::{BatchOp, Db, DbConfig};
use firewood::manager::RevisionManagerConfig;
use firewood::v2::api::{Db as _, DbView, KeyType, Proposal as _, ValueType};
use rand::{Rng, SeedableRng as _};
use rand_distr::Alphanumeric;

#[derive(Parser, Debug)]
struct Args {
    #[arg(short, long, default_value = "1-64", value_parser = string_to_range)]
    keylen: RangeInclusive<usize>,
    #[arg(short, long, default_value = "32", value_parser = string_to_range)]
    valuelen: RangeInclusive<usize>,
    #[arg(short, long, default_value_t = 1)]
    batch_size: usize,
    #[arg(short, long, default_value_t = 100)]
    number_of_batches: usize,
    #[arg(short = 'p', long, default_value_t = 0, value_parser = clap::value_parser!(u16).range(0..=100))]
    read_verify_percent: u16,
    #[arg(short, long)]
    seed: Option<u64>,
    #[arg(short, long, default_value_t = NonZeroUsize::new(20480).expect("is non-zero"))]
    cache_size: NonZeroUsize,
    #[arg(short, long, default_value_t = true)]
    truncate: bool,
    #[arg(short, long, default_value_t = 128)]
    revisions: usize,
}

fn string_to_range(input: &str) -> Result<RangeInclusive<usize>, Box<dyn Error + Sync + Send>> {
    //<usize as std::str::FromStr>::Err> {
    let parts: Vec<&str> = input.split('-').collect();
    #[expect(clippy::indexing_slicing)]
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

    let mgrcfg = RevisionManagerConfig::builder()
        .node_cache_size(args.cache_size)
        .max_revisions(args.revisions)
        .build();
    let cfg = DbConfig::builder()
        .truncate(args.truncate)
        .manager(mgrcfg)
        .build();

    let db = Db::new("rev_db", cfg)
        .await
        .expect("db initiation should succeed");

    let keys = args.batch_size;
    let start = Instant::now();

    let mut rng = if let Some(seed) = args.seed {
        rand::rngs::StdRng::seed_from_u64(seed)
    } else {
        rand::rngs::StdRng::from_os_rng()
    };

    for _ in 0..args.number_of_batches {
        let keylen = rng.random_range(args.keylen.clone());
        let valuelen = rng.random_range(args.valuelen.clone());
        let batch = (0..keys)
            .map(|_| {
                (
                    rng.borrow_mut()
                        .sample_iter(&Alphanumeric)
                        .take(keylen)
                        .collect::<Vec<u8>>(),
                    rng.borrow_mut()
                        .sample_iter(&Alphanumeric)
                        .take(valuelen)
                        .collect::<Vec<u8>>(),
                )
            })
            .map(|(key, value)| BatchOp::Put { key, value })
            .collect::<Vec<_>>();

        let verify = get_keys_to_verify(&batch, args.read_verify_percent);

        #[expect(clippy::unwrap_used)]
        let proposal = db.propose(batch.clone()).await.unwrap();
        proposal.commit().await?;
        verify_keys(&db, verify).await?;
    }

    let duration = start.elapsed();
    println!(
        "Generated and inserted {} batches of size {keys} in {duration:?}",
        args.number_of_batches
    );

    Ok(())
}

fn get_keys_to_verify<'a, K: KeyType + 'a, V: ValueType + 'a>(
    batch: impl IntoIterator<Item = &'a BatchOp<K, V>>,
    pct: u16,
) -> HashMap<&'a [u8], &'a [u8]> {
    if pct == 0 {
        HashMap::new()
    } else {
        batch
            .into_iter()
            .filter(|_last_key| rand::rng().random_range(0..=100u16.saturating_sub(pct)) == 0)
            .map(|op| {
                if let BatchOp::Put { key, value } = op {
                    (key.as_ref(), value.as_ref())
                } else {
                    unreachable!()
                }
            })
            .collect()
    }
}

async fn verify_keys(
    db: &impl firewood::v2::api::Db,
    verify: HashMap<&[u8], &[u8]>,
) -> Result<(), firewood::v2::api::Error> {
    if !verify.is_empty() {
        let hash = db.root_hash().await?.expect("root hash should exist");
        let revision = db.revision(hash).await?;
        for (key, value) in verify {
            assert_eq!(Some(value), revision.val(key).await?.as_deref());
        }
    }
    Ok(())
}
