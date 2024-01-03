// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

// This example isn't an actual benchmark, it's just an example of how to
// insert some random keys using the front-end API.

use clap::Parser;
use std::{
    borrow::BorrowMut as _, collections::HashMap, error::Error, ops::RangeInclusive, sync::Arc,
    time::Instant,
};

use firewood::{
    db::{Batch, BatchOp, Db, DbConfig},
    v2::api::{Db as _, DbView, Proposal},
};
use rand::{distributions::Alphanumeric, Rng, SeedableRng};

#[derive(Parser, Debug)]
struct Args {
    #[arg(short, long, default_value = "1-64", value_parser = string_to_range)]
    keylen: RangeInclusive<usize>,
    #[arg(short, long, default_value = "32", value_parser = string_to_range)]
    datalen: RangeInclusive<usize>,
    #[arg(short, long, default_value_t = 1)]
    batch_size: usize,
    #[arg(short, long, default_value_t = 100)]
    number_of_batches: usize,
    #[arg(short, long, default_value_t = 0, value_parser = clap::value_parser!(u16).range(0..=100))]
    read_verify_percent: u16,
    #[arg(short, long)]
    seed: Option<u64>,
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
    let cfg = DbConfig::builder().truncate(true).build();

    let args = Args::parse();

    let db = Db::new("rev_db", &cfg)
        .await
        .expect("db initiation should succeed");

    let keys = args.batch_size;
    let start = Instant::now();

    let mut rng = if let Some(seed) = args.seed {
        rand::rngs::StdRng::seed_from_u64(seed)
    } else {
        rand::rngs::StdRng::from_entropy()
    };

    for _ in 0..args.number_of_batches {
        let keylen = rng.gen_range(args.keylen.clone());
        let datalen = rng.gen_range(args.datalen.clone());
        let batch: Batch<Vec<u8>, Vec<u8>> = (0..keys)
            .map(|_| {
                (
                    rng.borrow_mut()
                        .sample_iter(&Alphanumeric)
                        .take(keylen)
                        .collect::<Vec<u8>>(),
                    rng.borrow_mut()
                        .sample_iter(&Alphanumeric)
                        .take(datalen)
                        .collect::<Vec<u8>>(),
                )
            })
            .map(|(key, value)| BatchOp::Put { key, value })
            .collect();

        let verify = get_keys_to_verify(&batch, args.read_verify_percent);

        #[allow(clippy::unwrap_used)]
        let proposal = Arc::new(db.propose(batch).await.unwrap());
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

fn get_keys_to_verify(batch: &Batch<Vec<u8>, Vec<u8>>, pct: u16) -> HashMap<Vec<u8>, Vec<u8>> {
    if pct == 0 {
        HashMap::new()
    } else {
        batch
            .iter()
            .filter(|_last_key| rand::thread_rng().gen_range(0..=(100 - pct)) == 0)
            .map(|op| {
                if let BatchOp::Put { key, value } = op {
                    (key.clone(), value.clone())
                } else {
                    unreachable!()
                }
            })
            .collect()
    }
}

async fn verify_keys(
    db: &impl firewood::v2::api::Db,
    verify: HashMap<Vec<u8>, Vec<u8>>,
) -> Result<(), firewood::v2::api::Error> {
    if !verify.is_empty() {
        let hash = db.root_hash().await?;
        let revision = db.revision(hash).await?;
        for (key, value) in verify {
            assert_eq!(Some(value), revision.val(key).await?);
        }
    }
    Ok(())
}
