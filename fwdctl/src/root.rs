// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use anyhow::{Error, Result};
use clap::Args;
use firewood::db::{Db, DbConfig, WalConfig};
use log;
use std::str;

#[derive(Debug, Args)]
pub struct Options {
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
    log::debug!("root hash {:?}", opts);
    let cfg = DbConfig::builder()
        .truncate(false)
        .wal(WalConfig::builder().max_revisions(10).build());

    let db = Db::new(opts.db.as_str(), &cfg.build()).map_err(Error::msg)?;

    let root = db.kv_root_hash().map_err(Error::msg)?;
    println!("{:X?}", *root);
    Ok(())
}
