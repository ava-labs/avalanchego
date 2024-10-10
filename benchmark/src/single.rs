// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

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
    async fn run(&self, db: &Db, args: &crate::Args) -> Result<(), Box<dyn Error>> {
        let start = Instant::now();
        let inner_key = Sha256::digest(0u64.to_ne_bytes()).to_vec();
        let mut batch_id = 0;

        while start.elapsed().as_secs() / 60 < args.global_opts.duration_minutes {
            let batch = vec![BatchOp::Put {
                key: inner_key.clone(),
                value: vec![batch_id as u8],
            }];
            let proposal = db.propose(batch).await.expect("proposal should succeed");
            proposal.commit().await?;

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
