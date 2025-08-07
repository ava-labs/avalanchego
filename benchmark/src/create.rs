// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![expect(
    clippy::arithmetic_side_effects,
    reason = "Found 1 occurrences after enabling the lint."
)]

use std::error::Error;
use std::time::Instant;

use fastrace::prelude::SpanContext;
use fastrace::{Span, func_path};
use firewood::db::Db;
use firewood::v2::api::{Db as _, Proposal as _};
use log::info;

use pretty_duration::pretty_duration;

use crate::{Args, TestRunner};

#[derive(Clone)]
pub struct Create;

impl TestRunner for Create {
    async fn run(&self, db: &Db, args: &Args) -> Result<(), Box<dyn Error>> {
        let keys = args.global_opts.batch_size;
        let start = Instant::now();

        for key in 0..args.global_opts.number_of_batches {
            let root = Span::root(func_path!(), SpanContext::random());
            let _guard = root.set_local_parent();

            let batch = Self::generate_inserts(key * keys, args.global_opts.batch_size);

            let proposal = db.propose(batch).await.expect("proposal should succeed");
            proposal.commit().await?;
        }
        let duration = start.elapsed();
        info!(
            "Generated and inserted {} batches of size {keys} in {}",
            args.global_opts.number_of_batches,
            pretty_duration(&duration, None)
        );

        Ok(())
    }
}
