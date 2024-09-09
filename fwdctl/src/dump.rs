// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;
use firewood::db::{Db, DbConfig};
use firewood::merkle::Key;
use firewood::stream::MerkleKeyValueStream;
use firewood::v2::api::{self, Db as _, DbView};
use futures_util::StreamExt;
use std::borrow::Cow;

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

    /// The key to start dumping from (if no key is provided, start from the beginning).
    /// Defaults to None.
    #[arg(
        required = false,
        value_name = "START_KEY",
        value_parser = key_parser,
        help = "Start dumping from this key (inclusive)."
    )]
    pub start_key: Option<Key>,
    #[arg(short, long, help = "Print the keys and values in hex format.")]
    pub hex: bool,
}

pub(super) async fn run(opts: &Options) -> Result<(), api::Error> {
    log::debug!("dump database {:?}", opts);
    let cfg = DbConfig::builder().truncate(false);

    let db = Db::new(opts.db.clone(), cfg.build()).await?;
    let latest_hash = db.root_hash().await?;
    let Some(latest_hash) = latest_hash else {
        println!("Database is empty");
        return Ok(());
    };
    let latest_rev = db.revision(latest_hash).await?;
    latest_rev.val("xxxx".as_bytes()).await?;
    let _start_key = opts.start_key.clone().unwrap_or(Box::new([]));
    let mut stream = MerkleKeyValueStream::from(&latest_rev);
    loop {
        match stream.next().await {
            None => break,
            Some(Ok((key, value))) => {
                if opts.hex {
                    println!("'{}': '{}'", hex::encode(&key), hex::encode(&value));
                } else {
                    println!("'{}': '{}'", u8_to_string(&key), u8_to_string(&value));
                }
            }
            Some(Err(e)) => return Err(e),
        }
    }
    Ok(())
}

fn u8_to_string(data: &[u8]) -> Cow<'_, str> {
    String::from_utf8_lossy(data)
}

fn key_parser(s: &str) -> Result<Box<[u8]>, std::io::Error> {
    return Ok(Box::from(s.as_bytes()));
}
