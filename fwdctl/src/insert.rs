// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use anyhow::{anyhow, Error, Result};
use clap::Args;
use firewood::db::{BatchOp, Db, DbConfig, WalConfig};
use log;

#[derive(Debug, Args)]
pub struct Options {
    /// The key to insert
    #[arg(required = true, value_name = "KEY", help = "Key to insert")]
    pub key: String,

    /// The value to insert
    #[arg(required = true, value_name = "VALUE", help = "Value to insert")]
    pub value: String,

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
    log::debug!("inserting key value pair {:?}", opts);
    let cfg = DbConfig::builder()
        .truncate(false)
        .wal(WalConfig::builder().max_revisions(10).build());

    let db = match Db::new(opts.db.as_str(), &cfg.build()) {
        Ok(db) => db,
        Err(_) => return Err(anyhow!("error opening database")),
    };

    let batch = vec![BatchOp::Put {
        key: &opts.key,
        value: opts.value.bytes().collect(),
    }];
    let proposal = db.new_proposal(batch).map_err(Error::msg)?;
    proposal.commit().map_err(Error::msg)?;

    println!("{}", opts.key);
    Ok(())
}
