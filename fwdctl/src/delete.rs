// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;
use firewood::db::{BatchOp, Db, DbConfig};
use firewood::v2::api::{self, Db as _, Proposal as _};

use crate::DatabasePath;

#[derive(Debug, Args)]
pub struct Options {
    #[command(flatten)]
    pub database: DatabasePath,

    /// The key to delete
    #[arg(required = true, value_name = "KEY", help = "Key to delete")]
    pub key: String,
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    log::debug!("deleting key {opts:?}");
    let cfg = DbConfig::builder().create_if_missing(false).truncate(false);

    let db = Db::new(opts.database.dbpath.clone(), cfg.build()).await?;

    let batch: Vec<BatchOp<String, String>> = vec![BatchOp::Delete {
        key: opts.key.clone(),
    }];
    let proposal = db.propose(batch).await?;
    proposal.commit().await?;

    println!("key {} deleted successfully", opts.key);
    Ok(())
}
