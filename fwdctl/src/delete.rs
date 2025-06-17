// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;
use firewood::db::{BatchOp, Db, DbConfig};
use firewood::v2::api::{self, Db as _, Proposal as _};

#[derive(Debug, Args)]
pub struct Options {
    /// The key to delete
    #[arg(required = true, value_name = "KEY", help = "Key to delete")]
    pub key: String,

    /// The database path (if no path is provided, return an error). Defaults to firewood.
    #[arg(
        long,
        required = false,
        value_name = "DB_NAME",
        default_value_t = String::from("firewood"),
        help = "Name of the database"
    )]
    pub db: String,
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    log::debug!("deleting key {opts:?}");
    let cfg = DbConfig::builder().truncate(false);

    let db = Db::new(opts.db.clone(), cfg.build()).await?;

    let batch: Vec<BatchOp<String, String>> = vec![BatchOp::Delete {
        key: opts.key.clone(),
    }];
    let proposal = db.propose(batch).await?;
    proposal.commit().await?;

    println!("key {} deleted successfully", opts.key);
    Ok(())
}
