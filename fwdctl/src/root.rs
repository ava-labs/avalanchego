// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;
use std::str;

use firewood::db::{Db, DbConfig};
use firewood::v2::api::{self, Db as _};

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

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    let cfg = DbConfig::builder().truncate(false);

    let db = Db::new(opts.db.clone(), cfg.build()).await?;

    let hash = db.root_hash().await?;

    println!("{hash:?}");
    Ok(())
}
