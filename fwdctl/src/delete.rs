// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use anyhow::{Error, Result};
use clap::Args;
use firewood::db::{BatchOp, Db, DbConfig, WalConfig};
use log;

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

pub fn run(opts: &Options) -> Result<()> {
    log::debug!("deleting key {:?}", opts);
    let cfg = DbConfig::builder()
        .truncate(false)
        .wal(WalConfig::builder().max_revisions(10).build());

    let db = Db::new(opts.db.as_str(), &cfg.build()).map_err(Error::msg)?;

    let batch = vec![BatchOp::Delete { key: &opts.key }];
    let proposal = db.new_proposal(batch).map_err(Error::msg)?;
    proposal.commit().map_err(Error::msg)?;

    println!("key {} deleted successfully", opts.key);
    Ok(())
}
