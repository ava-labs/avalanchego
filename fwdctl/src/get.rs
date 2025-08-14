// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;

use firewood::db::{Db, DbConfig};
use firewood::v2::api::{self, Db as _, DbView as _};

use crate::DatabasePath;

#[derive(Debug, Args)]
pub struct Options {
    #[command(flatten)]
    pub database: DatabasePath,

    /// The key to get the value for
    #[arg(required = true, value_name = "KEY", help = "Key to get")]
    pub key: String,
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    log::debug!("get key value pair {opts:?}");
    let cfg = DbConfig::builder().create_if_missing(false).truncate(false);

    let db = Db::new(opts.database.dbpath.clone(), cfg.build())?;

    let hash = db.root_hash().await?;

    let Some(hash) = hash else {
        println!("Database is empty");
        return Ok(());
    };

    let rev = db.revision(hash).await?;

    match rev.val(opts.key.as_bytes()).await {
        Ok(Some(val)) => {
            let s = String::from_utf8_lossy(val.as_ref());
            println!("{s:?}");
            Ok(())
        }
        Ok(None) => {
            eprintln!("Key '{}' not found", opts.key);
            Ok(())
        }
        Err(e) => Err(e),
    }
}
