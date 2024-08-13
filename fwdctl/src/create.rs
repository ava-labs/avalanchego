// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::{value_parser, Args};
use firewood::{
    db::{Db, DbConfig},
    v2::api,
};

#[derive(Args)]
pub struct Options {
    /// DB Options
    #[arg(
        required = true,
        value_name = "NAME",
        help = "A name for the database. A good default name is firewood."
    )]
    pub name: String,

    #[arg(
        long,
        required = false,
        value_parser = value_parser!(bool),
        default_missing_value = "false",
        default_value_t = false,
        value_name = "TRUNCATE",
        help = "Whether to truncate the DB when opening it. If set, the DB will be reset and all its
    existing contents will be lost. [default: false]"
    )]
    pub truncate: bool,

    /// WAL Config
    #[arg(
        long,
        required = false,
        default_value_t = 22,
        value_name = "WAL_FILE_NBIT",
        help = "Size of WAL file."
    )]
    file_nbit: u64,

    #[arg(
        long,
        required = false,
        default_value_t = 100,
        value_name = "Wal_MAX_REVISIONS",
        help = "Number of revisions to keep from the past. This preserves a rolling window
    of the past N commits to the database."
    )]
    max_revisions: u32,
}

pub(super) fn new(opts: &Options) -> DbConfig {
    DbConfig::builder().truncate(opts.truncate).build()
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    let db_config = new(opts);
    log::debug!("database configuration parameters: \n{:?}\n", db_config);

    Db::new(opts.name.clone(), db_config).await?;
    println!("created firewood database in {:?}", opts.name);
    Ok(())
}
