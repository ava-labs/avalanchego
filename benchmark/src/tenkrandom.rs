// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::error::Error;

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
        let mut high = args.number_of_batches * args.batch_size;
        let twenty_five_pct = args.batch_size / 4;

        loop {
            let batch: Vec<BatchOp<_, _>> = Self::generate_inserts(high, twenty_five_pct)
                .chain(generate_deletes(low, twenty_five_pct))
                .chain(generate_updates(low + high / 2, twenty_five_pct * 2, low))
                .collect();
            let proposal = db.propose(batch).await.expect("proposal should succeed");
            proposal.commit().await?;
            low += twenty_five_pct;
            high += twenty_five_pct;
        }
    }
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
