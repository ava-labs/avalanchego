// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use anyhow::{Error, Result};
use clap::Args;
use firewood::db::{DBConfig, WALConfig, DB};
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
    let cfg = DBConfig::builder()
        .truncate(false)
        .wal(WALConfig::builder().max_revisions(10).build());

    let db = DB::new(opts.db.as_str(), &cfg.build()).map_err(Error::msg)?;
    db.new_writebatch()
        .kv_remove(&opts.key)
        .map_err(Error::msg)?;
    println!("key {} deleted successfully", opts.key);
    Ok(())
}
