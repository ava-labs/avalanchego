// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use clap::Args;
use firewood::api::{self, Db as _, Proposal as _};
use firewood::db::{BatchOp, Db, DbConfig};

use crate::{DatabasePath, key::KeyArgument};

#[derive(Debug, Args)]
pub struct Options {
    #[command(flatten)]
    pub database: DatabasePath,

    #[command(flatten)]
    pub key: KeyArgument,

    /// The value to insert
    #[arg(required = true, value_name = "VALUE", help = "Value to insert")]
    pub value: String,
}

pub(super) fn run(opts: &Options) -> Result<(), api::Error> {
    log::debug!("inserting key value pair {opts:?}");
    let key = opts.key.database_key()?;
    let hex_key = hex::encode(&key);
    let cfg = DbConfig::builder()
        .node_hash_algorithm(opts.database.node_hash_algorithm.into())
        .create_if_missing(false)
        .truncate(false);

    let db = Db::new(opts.database.dbpath.clone(), cfg.build())?;

    let batch: Vec<BatchOp<Vec<u8>, Vec<u8>>> = vec![BatchOp::Put {
        key,
        value: opts.value.bytes().collect(),
    }];
    let proposal = db.propose(batch)?;
    proposal.commit()?;

    println!("0x{hex_key}");
    db.close()
}
