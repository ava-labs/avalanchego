// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;

use firewood::db::{Db, DbConfig};
use firewood::v2::api::{self, Db as _};

use crate::DatabasePath;

#[derive(Debug, Args)]
pub struct Options {
    #[command(flatten)]
    pub database: DatabasePath,
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    let cfg = DbConfig::builder().create_if_missing(false).truncate(false);

    let db = Db::new(opts.database.dbpath.clone(), cfg.build()).await?;

    let hash = db.root_hash().await?;

    println!("{hash:?}");
    Ok(())
}
