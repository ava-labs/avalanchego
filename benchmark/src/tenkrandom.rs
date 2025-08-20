// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 7 occurrences after enabling the lint."
)]

use std::error::Error;
use std::time::Instant;

use firewood::db::{BatchOp, Db};
use firewood::logger::debug;
use firewood::v2::api::{Db as _, Proposal as _};

use crate::{Args, TestRunner};
use sha2::{Digest, Sha256};

#[derive(Clone, Default)]
pub struct TenKRandom;

impl TestRunner for TenKRandom {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>> {
        let mut low = 0;
        let mut high = args.global_opts.number_of_batches * args.global_opts.batch_size;
        let twenty_five_pct = args.global_opts.batch_size / 4;

        let start = Instant::now();

        while start.elapsed().as_secs() / 60 < args.global_opts.duration_minutes {
            let batch: Vec<BatchOp<_, _>> = Self::generate_inserts(high, twenty_five_pct)
                .chain(generate_deletes(low, twenty_five_pct))
                .chain(generate_updates(low + high / 2, twenty_five_pct * 2, low))
                .collect();
            let proposal = db.propose(batch).expect("proposal should succeed");
            proposal.commit()?;
            low += twenty_five_pct;
            high += twenty_five_pct;
        }
        Ok(())
    }
}
fn generate_updates(
    start: u64,
    count: u64,
    low: u64,
) -> impl Iterator<Item = BatchOp<Box<[u8]>, Box<[u8]>>> {
    let hash_of_low: Box<[u8]> = Sha256::digest(low.to_ne_bytes())[..].into();
    (start..start + count)
        .map(|inner_key| {
            let digest = Sha256::digest(inner_key.to_ne_bytes())[..].into();
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
fn generate_deletes(start: u64, count: u64) -> impl Iterator<Item = BatchOp<Box<[u8]>, Box<[u8]>>> {
    (start..start + count)
        .map(|key| {
            let digest = Sha256::digest(key.to_ne_bytes())[..].into();
            debug!("deleting {:?} with digest {}", key, hex::encode(&digest));
            digest
        })
        .map(|key| BatchOp::Delete { key })
        .collect::<Vec<_>>()
        .into_iter()
}
