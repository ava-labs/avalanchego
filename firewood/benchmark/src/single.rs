// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 2 occurrences after enabling the lint."
)]
#![expect(
    clippy::cast_sign_loss,
    reason = "Found 1 occurrences after enabling the lint."
)]

use crate::TestRunner;
use firewood::db::{BatchOp, Db};
use firewood::v2::api::{Db as _, Proposal as _};
use log::debug;
use pretty_duration::pretty_duration;
use sha2::{Digest, Sha256};
use std::error::Error;
use std::time::Instant;

#[derive(Clone)]
pub struct Single;

impl TestRunner for Single {
    fn run(&self, db: &Db, args: &crate::Args) -> Result<(), Box<dyn Error>> {
        let start = Instant::now();
        let inner_keys: Vec<_> = (0..args.global_opts.batch_size)
            .map(|i| Sha256::digest(i.to_ne_bytes()))
            .collect();
        let mut batch_id = 0;

        while start.elapsed().as_secs() / 60 < args.global_opts.duration_minutes {
            let batch = inner_keys.iter().map(|key| BatchOp::Put {
                key,
                value: vec![batch_id as u8],
            });
            let proposal = db.propose(batch).expect("proposal should succeed");
            proposal.commit()?;

            if log::log_enabled!(log::Level::Debug) && batch_id % 1000 == 999 {
                debug!(
                    "completed {} batches in {}",
                    1 + batch_id,
                    pretty_duration(&start.elapsed(), None)
                );
            }
            batch_id += 1;
        }
        Ok(())
    }
}
