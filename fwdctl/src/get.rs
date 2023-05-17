// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use anyhow::{anyhow, bail, Error, Result};
use clap::Args;
use firewood::db::{Db, DbConfig, DbError, WalConfig};
use log;
use std::str;

#[derive(Debug, Args)]
pub struct Options {
    /// The key to get the value for
    #[arg(required = true, value_name = "KEY", help = "Key to get")]
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
    log::debug!("get key value pair {:?}", opts);
    let cfg = DbConfig::builder()
        .truncate(false)
        .wal(WalConfig::builder().max_revisions(10).build());

    let db = Db::new(opts.db.as_str(), &cfg.build()).map_err(Error::msg)?;

    match db.kv_get(opts.key.as_bytes()) {
        Ok(val) => {
            let s = match str::from_utf8(&val) {
                Ok(v) => v,
                Err(e) => return Err(anyhow!("Invalid UTF-8 sequence: {}", e)),
            };
            println!("{:?}", s);
            if val.is_empty() {
                bail!("no value found for key");
            }
            Ok(())
        }
        Err(DbError::KeyNotFound) => bail!("key not found"),
        Err(e) => bail!(e),
    }
}
