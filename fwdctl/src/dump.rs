// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::borrow::Cow;

use clap::Args;
use firewood::{
    db::{Db, DbConfig, WalConfig},
    v2::api::{self, Db as _},
};
use futures_util::StreamExt;
use log;

#[derive(Debug, Args)]
pub struct Options {
    /// The database path (if no path is provided, return an error). Defaults to firewood.
    #[arg(
        required = true,
        value_name = "DB_NAME",
        default_value_t = String::from("firewood"),
        help = "Name of the database"
    )]
    pub db: String,
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    log::debug!("dump database {:?}", opts);
    let cfg = DbConfig::builder()
        .truncate(false)
        .wal(WalConfig::builder().max_revisions(10).build());

    let db = Db::new(opts.db.clone(), &cfg.build()).await?;
    let latest_hash = db.root_hash().await?;
    let latest_rev = db.revision(latest_hash).await?;
    let mut stream = latest_rev.stream::<Vec<_>>(None)?;
    loop {
        match stream.next().await {
            None => break,
            Some(Ok((key, value))) => {
                println!("'{}': '{}'", u8_to_string(&key), u8_to_string(&value));
            }
            Some(Err(e)) => return Err(e),
        }
    }
    Ok(())
}
fn u8_to_string(data: &[u8]) -> Cow<'_, str> {
    String::from_utf8_lossy(data)
}
